package docker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/client"
	krakencrypto "github.com/kraken/admin-tool/backend/internal/crypto"
	"github.com/kraken/admin-tool/backend/internal/models"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

var (
	validContainerIDRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)
	validImageRefRegex    = regexp.MustCompile(`^[a-zA-Z0-9_./:@-]+$`)
)

func validateContainerID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" || !validContainerIDRegex.MatchString(id) {
		return errors.New("invalid container identifier: must be alphanumeric or standard container name/ID")
	}
	return nil
}

func validateImageRef(image string) error {
	image = strings.TrimSpace(image)
	if image == "" || !validImageRefRegex.MatchString(image) {
		return errors.New("invalid image reference: must be a valid image name or tag")
	}
	return nil
}

func validateHostURLSafety(hostURL string, sshUser string) error {
	trimmed := strings.TrimSpace(hostURL)
	if trimmed == "" {
		return errors.New("host URL is required")
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "unix://") {
		return nil
	}
	if IsSSHHost(trimmed) {
		parsed, err := ParseSSHHost(trimmed, sshUser)
		if err != nil {
			return err
		}
		return ValidateHostSafety(parsed.Host)
	}
	u, err := url.Parse(trimmed)
	if err == nil && u.Host != "" {
		host, _, err := net.SplitHostPort(u.Host)
		if err == nil {
			return ValidateHostSafety(host)
		}
		return ValidateHostSafety(u.Host)
	}
	return ValidateHostSafety(trimmed)
}

// DockerService manages interactions with local or remote Docker daemons
type DockerService struct {
	db                 *gorm.DB
	defaultHost        string
	defaultCli         *client.Client
	connected          bool
	errMsg             string
	mu                 sync.RWMutex
	clients            map[uint]*client.Client
	clientLocks        sync.Map // uint -> *sync.Mutex
	credentialResolver CredentialResolver
}

// NewDockerService creates and initializes a multi-host Docker service with default fallback
func NewDockerService(dockerHost string, db *gorm.DB) *DockerService {
	defaultCli, err := createDockerClient(dockerHost, false, "", "", "")
	connected := false
	errMsg := ""

	if err != nil {
		log.Printf("[DOCKER] Failed to instantiate default Docker client: %v", err)
		errMsg = err.Error()
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err = defaultCli.Ping(ctx)
		if err != nil {
			log.Printf("[DOCKER] Warning: Default Docker daemon unreachable: %v. Running in degraded mode.", err)
			errMsg = err.Error()
		} else {
			log.Printf("[DOCKER] Successfully connected to default Docker daemon.")
			connected = true
		}
	}

	return &DockerService{
		db:          db,
		defaultHost: dockerHost,
		defaultCli:  defaultCli,
		connected:   connected,
		errMsg:      errMsg,
		clients:     make(map[uint]*client.Client),
	}
}

// createDockerClient instantiates a Docker SDK client with optional TLS configuration
func createDockerClient(hostURL string, tlsVerify bool, caCert, cert, key string) (*client.Client, error) {
	return createDockerClientWithSSH(hostURL, tlsVerify, caCert, cert, key, SSHConfig{HostURL: hostURL})
}

// createDockerClientWithSSH instantiates a Docker SDK client with optional TLS or SSH configuration
func createDockerClientWithSSH(hostURL string, tlsVerify bool, caCert, cert, key string, sshCfg SSHConfig) (*client.Client, error) {
	cleanHost := strings.TrimSpace(hostURL)

	if IsSSHHost(cleanHost) {
		sshCfg.HostURL = cleanHost
		dialContext, err := BuildSSHDialContext(sshCfg)
		if err != nil {
			return nil, err
		}

		tr := &http.Transport{
			DialContext: dialContext,
		}
		httpClient := &http.Client{
			Transport: tr,
			Timeout:   30 * time.Second,
		}

		opts := []client.Opt{
			client.WithAPIVersionNegotiation(),
			client.WithHost("http://docker"),
			client.WithHTTPClient(httpClient),
			client.WithDialContext(dialContext),
		}
		return client.NewClientWithOpts(opts...)
	}

	opts := []client.Opt{
		client.WithAPIVersionNegotiation(),
	}

	if cleanHost != "" {
		opts = append(opts, client.WithHost(cleanHost))
	} else {
		opts = append(opts, client.FromEnv)
	}

	if tlsVerify {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: false,
		}

		if caCert != "" {
			caPool := x509.NewCertPool()
			if ok := caPool.AppendCertsFromPEM([]byte(caCert)); ok {
				tlsConfig.RootCAs = caPool
			}
		} else {
			tlsConfig.InsecureSkipVerify = true
		}

		if cert != "" && key != "" {
			keyPair, err := tls.X509KeyPair([]byte(cert), []byte(key))
			if err == nil {
				tlsConfig.Certificates = []tls.Certificate{keyPair}
			}
		}

		tr := &http.Transport{
			TLSClientConfig: tlsConfig,
		}
		opts = append(opts, client.WithHTTPClient(&http.Client{Transport: tr, Timeout: 30 * time.Second}))
	}

	return client.NewClientWithOpts(opts...)
}

// SetDB associates the database handle after initialization if needed
func (s *DockerService) SetDB(db *gorm.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
}

// SetCredentialResolver configures the dynamic secret/credential provider (e.g. plugins or Key Vault)
func (s *DockerService) SetCredentialResolver(resolver CredentialResolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credentialResolver = resolver
}

// GetCredentialResolver returns the active credential resolver
func (s *DockerService) GetCredentialResolver() CredentialResolver {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.credentialResolver
}

// ResolveServerCredentials dynamically obtains credentials for a server
// either via an external Key Vault plugin or via local AES-GCM decryption.
func (s *DockerService) ResolveServerCredentials(ctx context.Context, server *models.DockerServer, purpose string) (*models.ResolvedCredentials, error) {
	if server == nil {
		return nil, errors.New("server is nil")
	}

	pluginID := strings.TrimSpace(server.CredentialPluginID)
	usePlugin := server.CredentialSource == models.CredentialSourcePlugin ||
		server.CredentialSource == models.CredentialSourceVault ||
		strings.TrimSpace(server.SecretReference) != ""
	if usePlugin && pluginID == "" {
		pluginID = "com.kraken.azure-keyvault"
	}

	if usePlugin && pluginID != "" {
		s.mu.RLock()
		resolver := s.credentialResolver
		s.mu.RUnlock()

		if resolver == nil {
			return nil, fmt.Errorf("credential resolver not configured to handle plugin '%s'", pluginID)
		}

		var vaultCfg map[string]interface{}
		if strings.TrimSpace(server.VaultConfig) != "" {
			_ = json.Unmarshal([]byte(server.VaultConfig), &vaultCfg)
		}

		req := models.ResolveSecretRequest{
			ServerID:           server.ID,
			ServerName:         server.Name,
			HostURL:            server.HostURL,
			SSHUser:            server.SSHUser,
			CredentialPluginID: pluginID,
			SecretReference:    server.SecretReference,
			SecretVersion:      server.SecretVersion,
			VaultConfig:        vaultCfg,
			Purpose:            purpose,
		}

		resolved, err := resolver.ResolveCredentials(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve secret from plugin '%s': %w", pluginID, err)
		}

		// Ensure defaults from server model if not overridden by plugin
		if resolved.SSHUser == "" {
			resolved.SSHUser = server.SSHUser
		}
		if resolved.SSHAuthMethod == "" {
			if resolved.SSHPrivateKey != "" {
				resolved.SSHAuthMethod = "key"
			} else if resolved.SSHPassword != "" {
				resolved.SSHAuthMethod = "password"
			} else {
				resolved.SSHAuthMethod = server.SSHAuthMethod
			}
		}
		if resolved.TLSCACert == "" {
			resolved.TLSCACert = server.TLSCACert
		}
		if resolved.TLSCert == "" {
			resolved.TLSCert = server.TLSCert
		}

		return resolved, nil
	}

	// Local fallback: decrypt locally stored credentials
	tlsKey, _ := krakencrypto.Decrypt(server.TLSKey)
	sshPassword, _ := krakencrypto.Decrypt(server.SSHPassword)
	sshPrivateKey, _ := krakencrypto.Decrypt(server.SSHPrivateKey)
	sshPassphrase, _ := krakencrypto.Decrypt(server.SSHKeyPassphrase)
	sudoPassword, _ := krakencrypto.Decrypt(server.SudoPassword)

	// If local SSH server has no stored password/key, attempt JIT resolution via Azure Key Vault plugin if bound
	if strings.HasPrefix(server.HostURL, "ssh://") && sshPassword == "" && sshPrivateKey == "" {
		s.mu.RLock()
		resolver := s.credentialResolver
		s.mu.RUnlock()
		if resolver != nil {
			req := models.ResolveSecretRequest{
				ServerID:           server.ID,
				ServerName:         server.Name,
				HostURL:            server.HostURL,
				SSHUser:            server.SSHUser,
				CredentialPluginID: "com.kraken.azure-keyvault",
				SecretReference:    server.SecretReference,
				Purpose:            purpose,
			}
			if resolved, err := resolver.ResolveCredentials(ctx, req); err == nil && (resolved.SSHPassword != "" || resolved.SSHPrivateKey != "") {
				if resolved.SSHUser == "" {
					resolved.SSHUser = server.SSHUser
				}
				return resolved, nil
			}
		}
	}

	return &models.ResolvedCredentials{
		SSHUser:          server.SSHUser,
		SSHAuthMethod:    server.SSHAuthMethod,
		SSHPassword:      sshPassword,
		SSHPrivateKey:    sshPrivateKey,
		SSHKeyPassphrase: sshPassphrase,
		TLSKey:           tlsKey,
		TLSCACert:        server.TLSCACert,
		TLSCert:          server.TLSCert,
		SudoPassword:     sudoPassword,
	}, nil
}

// PrepareTestServerRequest resolves dynamic credentials into the TestServerRequest if configured
func (s *DockerService) PrepareTestServerRequest(ctx context.Context, req *models.TestServerRequest) error {
	if req == nil {
		return nil
	}

	if (req.CredentialSource == models.CredentialSourcePlugin || req.CredentialSource == models.CredentialSourceVault) && strings.TrimSpace(req.CredentialPluginID) == "" {
		req.CredentialPluginID = "com.kraken.azure-keyvault"
	}

	if (req.CredentialSource == models.CredentialSourcePlugin || req.CredentialSource == models.CredentialSourceVault) && req.CredentialPluginID != "" {
		s.mu.RLock()
		resolver := s.credentialResolver
		s.mu.RUnlock()

		if resolver == nil {
			return fmt.Errorf("credential resolver not configured to handle plugin '%s'", req.CredentialPluginID)
		}

		var vaultCfg map[string]interface{}
		if strings.TrimSpace(req.VaultConfig) != "" {
			_ = json.Unmarshal([]byte(req.VaultConfig), &vaultCfg)
		}

		hostURL := strings.TrimSpace(req.HostURL)
		if hostURL == "" && strings.TrimSpace(req.ServerHost) != "" {
			port := strings.TrimSpace(req.ServerPort)
			if port == "" {
				port = "22"
			}
			user := strings.TrimSpace(req.SSHUser)
			if user == "" {
				user = "root"
			}
			hostURL = fmt.Sprintf("ssh://%s@%s:%s", user, strings.TrimSpace(req.ServerHost), port)
		}

		resolved, err := resolver.ResolveCredentials(ctx, models.ResolveSecretRequest{
			HostURL:            hostURL,
			SSHUser:            req.SSHUser,
			CredentialPluginID: req.CredentialPluginID,
			SecretReference:    req.SecretReference,
			SecretVersion:      req.SecretVersion,
			VaultConfig:        vaultCfg,
			Purpose:            "ssh_connect",
		})
		if err != nil {
			return fmt.Errorf("failed to resolve secret from plugin '%s': %w", req.CredentialPluginID, err)
		}

		if resolved.SSHPassword != "" {
			req.SSHPassword = resolved.SSHPassword
		}
		if resolved.SSHPrivateKey != "" {
			req.SSHPrivateKey = resolved.SSHPrivateKey
		}
		if resolved.SSHKeyPassphrase != "" {
			req.SSHKeyPassphrase = resolved.SSHKeyPassphrase
		}
		if resolved.TLSKey != "" {
			req.TLSKey = resolved.TLSKey
		}
		if resolved.SSHAuthMethod != "" {
			req.SSHAuthMethod = resolved.SSHAuthMethod
		}
		if resolved.SSHUser != "" && req.SSHUser == "" {
			req.SSHUser = resolved.SSHUser
		}
	}

	return nil
}

// GetSystemClient returns the underlying system Docker client (used for plugin runners and internal builders)
func (s *DockerService) GetSystemClient() (*client.Client, error) {
	if s.defaultCli != nil && s.connected {
		return s.defaultCli, nil
	}
	if s.defaultCli != nil {
		return s.defaultCli, nil
	}
	return nil, errors.New("system docker daemon is currently unavailable")
}

// GetClient retrieves the Docker client for a specific serverID.
// For strict multi-tenant isolation, a serverID of 0 does not fall back to the host daemon when a DB is attached.
func (s *DockerService) GetClient(ctx context.Context, serverID uint) (*client.Client, error) {
	if serverID == 0 {
		if s.db == nil && s.defaultCli != nil {
			return s.defaultCli, nil
		}
		return nil, errors.New("no docker server specified")
	}

	// Check cached client first
	s.mu.RLock()
	cli, exists := s.clients[serverID]
	s.mu.RUnlock()
	if exists && cli != nil {
		return cli, nil
	}

	// Per-server lock to serialize creation for this specific serverID without blocking other servers
	val, _ := s.clientLocks.LoadOrStore(serverID, &sync.Mutex{})
	serverMu := val.(*sync.Mutex)
	serverMu.Lock()
	defer serverMu.Unlock()

	// Double check after acquiring server lock
	s.mu.RLock()
	cli, exists = s.clients[serverID]
	s.mu.RUnlock()
	if exists && cli != nil {
		return cli, nil
	}

	if s.db == nil {
		if s.defaultCli != nil {
			return s.defaultCli, nil
		}
		return nil, errors.New("database not available to resolve server")
	}

	var server models.DockerServer
	if err := s.db.First(&server, serverID).Error; err != nil {
		return nil, fmt.Errorf("server %d not found: %w", serverID, err)
	}

	resolved, err := s.ResolveServerCredentials(ctx, &server, "docker_connect")
	if err != nil {
		return nil, fmt.Errorf("failed to resolve server credentials for %s: %w", server.Name, err)
	}

	newCli, err := createDockerClientWithSSH(
		server.HostURL,
		server.TLSVerify,
		resolved.TLSCACert,
		resolved.TLSCert,
		resolved.TLSKey,
		SSHConfig{
			HostURL:       server.HostURL,
			User:          resolved.SSHUser,
			AuthMethod:    resolved.SSHAuthMethod,
			Password:      resolved.SSHPassword,
			PrivateKey:    resolved.SSHPrivateKey,
			KeyPassphrase: resolved.SSHKeyPassphrase,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for server %s: %w", server.Name, err)
	}

	// Test connection ping with bounded timeout
	timeout := 3 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	pingCtx, pingCancel := context.WithTimeout(ctx, timeout)
	defer pingCancel()

	if _, err := newCli.Ping(pingCtx); err != nil {
		s.db.Model(&server).Updates(map[string]interface{}{
			"status":  "UNREACHABLE",
			"err_msg": err.Error(),
		})
		return nil, fmt.Errorf("server %s unreachable: %w", server.Name, err)
	}

	s.db.Model(&server).Updates(map[string]interface{}{
		"status":  "CONNECTED",
		"err_msg": "",
	})

	s.mu.Lock()
	s.clients[serverID] = newCli
	s.mu.Unlock()

	return newCli, nil
}

// InvalidateClientCache evicts a cached client (used after server edit/delete)
func (s *DockerService) InvalidateClientCache(serverID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cli, ok := s.clients[serverID]; ok && cli != nil {
		_ = cli.Close()
	}
	delete(s.clients, serverID)
}

// GetServer fetches the DockerServer record by ID
func (s *DockerService) GetServer(serverID uint) (*models.DockerServer, error) {
	if s.db == nil {
		return nil, errors.New("database not available")
	}
	var server models.DockerServer
	if err := s.db.First(&server, serverID).Error; err != nil {
		return nil, fmt.Errorf("server %d not found: %w", serverID, err)
	}
	server.HasSavedSudoPassword = (server.SudoPassword != "")
	return &server, nil
}

// GetSSHClient establishes an authenticated SSH client connection for the specified serverID
func (s *DockerService) GetSSHClient(ctx context.Context, serverID uint, overrides ...SSHConfig) (*ssh.Client, *models.DockerServer, error) {
	server, err := s.GetServer(serverID)
	if err != nil {
		return nil, nil, err
	}

	cleanHost := strings.TrimSpace(server.HostURL)
	if !IsSSHHost(cleanHost) {
		return nil, server, errors.New("server is not configured for SSH")
	}

	parsed, err := ParseSSHHost(cleanHost, server.SSHUser)
	if err != nil {
		return nil, server, fmt.Errorf("invalid SSH host url: %w", err)
	}

	resolved, err := s.ResolveServerCredentials(ctx, server, "ssh_connect")
	if err != nil {
		return nil, server, fmt.Errorf("failed to resolve server credentials: %w", err)
	}

	sshPassword := resolved.SSHPassword
	sshPrivateKey := resolved.SSHPrivateKey
	sshPassphrase := resolved.SSHKeyPassphrase
	if resolved.SSHAuthMethod != "" {
		server.SSHAuthMethod = resolved.SSHAuthMethod
	}
	if resolved.SSHUser != "" {
		parsed.User = resolved.SSHUser
	}

	if len(overrides) > 0 {
		if overrides[0].Password != "" {
			sshPassword = overrides[0].Password
		}
		if overrides[0].PrivateKey != "" {
			sshPrivateKey = overrides[0].PrivateKey
		}
		if overrides[0].KeyPassphrase != "" {
			sshPassphrase = overrides[0].KeyPassphrase
		}
		if overrides[0].User != "" {
			parsed.User = overrides[0].User
		}
	}

	if server.CredentialPolicy == "always_prompt" && sshPassword == "" && sshPrivateKey == "" {
		return nil, server, errors.New("host credentials required: server configured in 'always prompt' mode")
	}

	cfg := SSHConfig{
		HostURL:       cleanHost,
		User:          parsed.User,
		AuthMethod:    server.SSHAuthMethod,
		Password:      sshPassword,
		PrivateKey:    sshPrivateKey,
		KeyPassphrase: sshPassphrase,
	}

	client, err := DialDirectSSHClient(ctx, parsed, cfg)
	if err != nil {
		return nil, server, fmt.Errorf("failed to establish SSH connection to %s: %w", cleanHost, err)
	}

	return client, server, nil
}

// --- Server Management (CRUD) ---

// ServerStatusResult represents the connectivity probe outcome for a single server
type ServerStatusResult struct {
	ID              uint   `json:"server_id"`
	Name            string `json:"name"`
	HostURL         string `json:"host_url"`
	Status          string `json:"status"` // CONNECTED, UNREACHABLE, CONNECTING
	Version         string `json:"version,omitempty"`
	ContainersCount int    `json:"containers_count"`
	ImagesCount     int    `json:"images_count"`
	ErrMsg          string `json:"err_msg,omitempty"`
}

// ProbeServer checks connectivity to a single server and refreshes its status and counts
func (s *DockerService) ProbeServer(ctx context.Context, serverID uint) ServerStatusResult {
	res := ServerStatusResult{
		ID:     serverID,
		Status: "UNREACHABLE",
	}

	var server models.DockerServer
	if s.db != nil {
		if err := s.db.First(&server, serverID).Error; err == nil {
			res.Name = server.Name
			res.HostURL = server.HostURL
			res.ContainersCount = server.Containers
			res.ImagesCount = server.Images
			res.Version = server.Version
		}
	}

	probeCtx, cancel := context.WithTimeout(ctx, 3500*time.Millisecond)
	defer cancel()

	cli, err := s.GetClient(probeCtx, serverID)
	if err != nil || cli == nil {
		res.Status = "UNREACHABLE"
		if err != nil {
			res.ErrMsg = err.Error()
		}
		if s.db != nil {
			s.db.Model(&models.DockerServer{}).Where("id = ?", serverID).Updates(map[string]interface{}{
				"status":  res.Status,
				"err_msg": res.ErrMsg,
			})
		}
		return res
	}

	ver, verErr := cli.ServerVersion(probeCtx)
	if verErr != nil {
		res.Status = "UNREACHABLE"
		res.ErrMsg = verErr.Error()
		if s.db != nil {
			s.db.Model(&models.DockerServer{}).Where("id = ?", serverID).Updates(map[string]interface{}{
				"status":  res.Status,
				"err_msg": res.ErrMsg,
			})
		}
		return res
	}

	res.Status = "CONNECTED"
	res.Version = ver.Version
	res.ErrMsg = ""

	cList, _ := cli.ContainerList(probeCtx, container.ListOptions{All: true})
	res.ContainersCount = len(cList)
	imgList, _ := cli.ImageList(probeCtx, image.ListOptions{All: false})
	res.ImagesCount = len(imgList)

	if s.db != nil {
		s.db.Model(&models.DockerServer{}).Where("id = ?", serverID).Updates(map[string]interface{}{
			"status":     res.Status,
			"version":    res.Version,
			"containers": res.ContainersCount,
			"images":     res.ImagesCount,
			"err_msg":    "",
		})
	}

	return res
}

// ListServers returns all registered Docker servers.
// If probe is true, it verifies connectivity and counts concurrently in parallel.
// If probe is false or omitted, it returns the database records immediately without blocking.
func (s *DockerService) ListServers(ctx context.Context, probe ...bool) ([]models.DockerServer, error) {
	return s.ListServersForUser(ctx, 0, probe...)
}

// ListServersForUser returns Docker servers accessible by a specific user.
// Servers are strictly isolated: a user only sees hosts they explicitly own (user_id = userID).
// The system host is never shown automatically unless the user explicitly owns it.
// If userID is 0, an empty slice is returned.
func (s *DockerService) ListServersForUser(ctx context.Context, userID uint, probe ...bool) ([]models.DockerServer, error) {
	if userID == 0 || s.db == nil {
		return []models.DockerServer{}, nil
	}

	var servers []models.DockerServer
	query := s.db.Order("is_default desc, id asc").Where("user_id = ?", userID)
	if err := query.Find(&servers).Error; err != nil {
		return nil, err
	}

	for i := range servers {
		if servers[i].CredentialPolicy == "" {
			servers[i].CredentialPolicy = "save_securely"
		}
		servers[i].HasSavedSudoPassword = (servers[i].SudoPassword != "")
	}

	shouldProbe := len(probe) > 0 && probe[0]
	if !shouldProbe {
		return servers, nil
	}

	// Probe all servers in parallel
	var wg sync.WaitGroup
	for i := range servers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res := s.ProbeServer(ctx, servers[idx].ID)
			servers[idx].Status = res.Status
			servers[idx].Version = res.Version
			servers[idx].Containers = res.ContainersCount
			servers[idx].Images = res.ImagesCount
			servers[idx].ErrMsg = res.ErrMsg
		}(i)
	}
	wg.Wait()

	return servers, nil
}

// CreateServer registers a new Docker host
func (s *DockerService) CreateServer(ctx context.Context, req models.CreateServerRequest, userIDs ...uint) (*models.DockerServer, error) {
	if s.db == nil {
		return nil, errors.New("database unavailable")
	}

	var userID uint
	if len(userIDs) > 0 {
		userID = userIDs[0]
	}

	name := strings.TrimSpace(req.Name)
	hostURL := strings.TrimSpace(req.HostURL)
	if hostURL == "" && strings.TrimSpace(req.ServerHost) != "" {
		port := strings.TrimSpace(req.ServerPort)
		if port == "" {
			port = "22"
		}
		user := strings.TrimSpace(req.SSHUser)
		if user == "" {
			user = "root"
		}
		hostURL = fmt.Sprintf("ssh://%s@%s:%s", user, strings.TrimSpace(req.ServerHost), port)
	}

	if name == "" || hostURL == "" {
		return nil, errors.New("server name and host URL are required")
	}

	if err := validateHostURLSafety(hostURL, req.SSHUser); err != nil {
		return nil, fmt.Errorf("prohibited host URL: %w", err)
	}

	sshUser := strings.TrimSpace(req.SSHUser)
	sshAuthMethod := strings.TrimSpace(req.SSHAuthMethod)
	if sshAuthMethod == "" && IsSSHHost(hostURL) {
		if req.SSHPassword != "" {
			sshAuthMethod = "password"
		} else {
			sshAuthMethod = "key"
		}
	}

	credSource := strings.ToLower(strings.TrimSpace(req.CredentialSource))
	if credSource == "" {
		credSource = models.CredentialSourceLocal
	}

	// Test connection
	status := "CONNECTED"
	errMsg := ""
	version := ""
	testVer, testErr := s.TestServer(ctx, models.TestServerRequest{
		HostURL:            hostURL,
		TLSVerify:          req.TLSVerify,
		TLSCACert:          req.TLSCACert,
		TLSCert:            req.TLSCert,
		TLSKey:             req.TLSKey,
		SSHUser:            sshUser,
		SSHAuthMethod:      sshAuthMethod,
		SSHPassword:        req.SSHPassword,
		SSHPrivateKey:      req.SSHPrivateKey,
		SSHKeyPassphrase:   req.SSHKeyPassphrase,
		CredentialSource:   credSource,
		CredentialPluginID: strings.TrimSpace(req.CredentialPluginID),
		SecretReference:    strings.TrimSpace(req.SecretReference),
		SecretVersion:      strings.TrimSpace(req.SecretVersion),
		VaultConfig:        strings.TrimSpace(req.VaultConfig),
	})
	if testErr != nil {
		status = "UNREACHABLE"
		errMsg = testErr.Error()
	} else {
		version = testVer
	}

	// If marked default, unset other defaults
	if req.IsDefault {
		s.db.Model(&models.DockerServer{}).Where("is_default = ?", true).Update("is_default", false)
	}

	// Credential Policy: "save_securely" (default) or "always_prompt"
	credPolicy := strings.ToLower(strings.TrimSpace(req.CredentialPolicy))
	if credPolicy != "always_prompt" {
		credPolicy = "save_securely"
	}

	var encTLSKey, encSSHPassword, encSSHPrivateKey, encSSHKeyPassphrase string
	if credPolicy == "save_securely" {
		encTLSKey, _ = krakencrypto.Encrypt(req.TLSKey)
		encSSHPassword, _ = krakencrypto.Encrypt(req.SSHPassword)
		encSSHPrivateKey, _ = krakencrypto.Encrypt(req.SSHPrivateKey)
		encSSHKeyPassphrase, _ = krakencrypto.Encrypt(req.SSHKeyPassphrase)
	}

	// Sudo Policy: "save_securely" or "always_prompt"
	sudoPolicy := strings.ToLower(strings.TrimSpace(req.SudoPolicy))
	var encSudoPassword string
	if sudoPolicy == "save_securely" && req.SudoPassword != "" {
		encSudoPassword, _ = krakencrypto.Encrypt(req.SudoPassword)
	} else if sudoPolicy != "always_prompt" && req.SudoPassword != "" {
		sudoPolicy = "save_securely"
		encSudoPassword, _ = krakencrypto.Encrypt(req.SudoPassword)
	}

	server := models.DockerServer{
		UserID:             userID,
		Name:               name,
		HostURL:            hostURL,
		TLSVerify:          req.TLSVerify,
		TLSCACert:          req.TLSCACert,
		TLSCert:            req.TLSCert,
		TLSKey:             encTLSKey,
		SSHUser:            sshUser,
		SSHAuthMethod:      sshAuthMethod,
		SSHPassword:        encSSHPassword,
		SSHPrivateKey:      encSSHPrivateKey,
		SSHKeyPassphrase:   encSSHKeyPassphrase,
		CredentialPolicy:   credPolicy,
		CredentialSource:   credSource,
		CredentialPluginID: strings.TrimSpace(req.CredentialPluginID),
		SecretReference:    strings.TrimSpace(req.SecretReference),
		SecretVersion:      strings.TrimSpace(req.SecretVersion),
		VaultConfig:        strings.TrimSpace(req.VaultConfig),
		SudoPolicy:         sudoPolicy,
		SudoPassword:       encSudoPassword,
		PackageManager:     strings.ToLower(strings.TrimSpace(req.PackageManager)),
		IsDefault:          req.IsDefault,
		Status:             status,
		ErrMsg:             errMsg,
		Version:            version,
	}

	if err := s.db.Create(&server).Error; err != nil {
		return nil, err
	}

	return &server, nil
}

// UpdateServer modifies an existing Docker host
func (s *DockerService) UpdateServer(ctx context.Context, id uint, req models.UpdateServerRequest) (*models.DockerServer, error) {
	if s.db == nil {
		return nil, errors.New("database unavailable")
	}

	var server models.DockerServer
	if err := s.db.First(&server, id).Error; err != nil {
		return nil, err
	}

	name := strings.TrimSpace(req.Name)
	hostURL := strings.TrimSpace(req.HostURL)
	if hostURL == "" && strings.TrimSpace(req.ServerHost) != "" {
		port := strings.TrimSpace(req.ServerPort)
		if port == "" {
			port = "22"
		}
		user := strings.TrimSpace(req.SSHUser)
		if user == "" {
			user = "root"
		}
		hostURL = fmt.Sprintf("ssh://%s@%s:%s", user, strings.TrimSpace(req.ServerHost), port)
	}
	if name != "" {
		server.Name = name
	}
	if hostURL != "" {
		if err := validateHostURLSafety(hostURL, req.SSHUser); err != nil {
			return nil, fmt.Errorf("prohibited host URL: %w", err)
		}
		server.HostURL = hostURL
	}
	server.TLSVerify = req.TLSVerify
	if req.TLSCACert != "" {
		server.TLSCACert = req.TLSCACert
	}
	if req.TLSCert != "" {
		server.TLSCert = req.TLSCert
	}
	if req.TLSKey != "" {
		server.TLSKey, _ = krakencrypto.Encrypt(req.TLSKey)
	}

	server.SSHUser = strings.TrimSpace(req.SSHUser)
	if req.SSHAuthMethod != "" {
		server.SSHAuthMethod = req.SSHAuthMethod
	}
	if req.SSHPassword != "" {
		server.SSHPassword, _ = krakencrypto.Encrypt(req.SSHPassword)
	}
	if req.SSHPrivateKey != "" {
		server.SSHPrivateKey, _ = krakencrypto.Encrypt(req.SSHPrivateKey)
	}
	if req.SSHKeyPassphrase != "" {
		server.SSHKeyPassphrase, _ = krakencrypto.Encrypt(req.SSHKeyPassphrase)
	}

	if req.CredentialPolicy != "" {
		cleanPolicy := strings.ToLower(strings.TrimSpace(req.CredentialPolicy))
		if cleanPolicy == "always_prompt" {
			server.CredentialPolicy = "always_prompt"
			server.TLSKey = ""
			server.SSHPassword = ""
			server.SSHPrivateKey = ""
			server.SSHKeyPassphrase = ""
		} else if cleanPolicy == "save_securely" {
			server.CredentialPolicy = "save_securely"
		}
	}

	if req.SudoPolicy != "" {
		cleanSudoPolicy := strings.ToLower(strings.TrimSpace(req.SudoPolicy))
		if cleanSudoPolicy == "always_prompt" {
			server.SudoPolicy = "always_prompt"
			server.SudoPassword = ""
		} else if cleanSudoPolicy == "save_securely" {
			server.SudoPolicy = "save_securely"
			if req.SudoPassword != "" {
				server.SudoPassword, _ = krakencrypto.Encrypt(req.SudoPassword)
			}
		}
	} else if req.SudoPassword != "" {
		server.SudoPolicy = "save_securely"
		server.SudoPassword, _ = krakencrypto.Encrypt(req.SudoPassword)
	}

	if req.CredentialSource != "" {
		server.CredentialSource = strings.ToLower(strings.TrimSpace(req.CredentialSource))
		server.CredentialPluginID = strings.TrimSpace(req.CredentialPluginID)
		server.SecretReference = strings.TrimSpace(req.SecretReference)
		server.SecretVersion = strings.TrimSpace(req.SecretVersion)
		server.VaultConfig = strings.TrimSpace(req.VaultConfig)
	}

	if req.PackageManager != "" {
		server.PackageManager = strings.ToLower(strings.TrimSpace(req.PackageManager))
	}

	if req.IsDefault {
		s.db.Model(&models.DockerServer{}).Where("id != ?", id).Update("is_default", false)
		server.IsDefault = true
	}

	// Test connection (decrypt stored local secrets first if not using plugin)
	decTLSKey, _ := krakencrypto.Decrypt(server.TLSKey)
	decSSHPassword, _ := krakencrypto.Decrypt(server.SSHPassword)
	decSSHPrivateKey, _ := krakencrypto.Decrypt(server.SSHPrivateKey)
	decSSHPassphrase, _ := krakencrypto.Decrypt(server.SSHKeyPassphrase)

	testVer, testErr := s.TestServer(ctx, models.TestServerRequest{
		HostURL:            server.HostURL,
		TLSVerify:          server.TLSVerify,
		TLSCACert:          server.TLSCACert,
		TLSCert:            server.TLSCert,
		TLSKey:             decTLSKey,
		SSHUser:            server.SSHUser,
		SSHAuthMethod:      server.SSHAuthMethod,
		SSHPassword:        decSSHPassword,
		SSHPrivateKey:      decSSHPrivateKey,
		SSHKeyPassphrase:   decSSHPassphrase,
		CredentialSource:   server.CredentialSource,
		CredentialPluginID: server.CredentialPluginID,
		SecretReference:    server.SecretReference,
		SecretVersion:      server.SecretVersion,
		VaultConfig:        server.VaultConfig,
	})
	if testErr != nil {
		server.Status = "UNREACHABLE"
		server.ErrMsg = testErr.Error()
	} else {
		server.Status = "CONNECTED"
		server.Version = testVer
		server.ErrMsg = ""
	}

	if err := s.db.Save(&server).Error; err != nil {
		return nil, err
	}

	s.InvalidateClientCache(id)
	return &server, nil
}

// DeleteServer removes a registered Docker server
func (s *DockerService) DeleteServer(ctx context.Context, id uint) error {
	if s.db == nil {
		return errors.New("database unavailable")
	}

	var count int64
	s.db.Model(&models.DockerServer{}).Count(&count)
	if count <= 1 {
		return errors.New("cannot delete the only registered server")
	}

	var server models.DockerServer
	if err := s.db.First(&server, id).Error; err != nil {
		return err
	}

	if server.IsDefault {
		// Designate another server as default before deleting
		var other models.DockerServer
		if err := s.db.Where("id != ?", id).First(&other).Error; err == nil {
			s.db.Model(&other).Update("is_default", true)
		}
	}

	if err := s.db.Delete(&server).Error; err != nil {
		return err
	}

	s.InvalidateClientCache(id)
	return nil
}

// SetDefaultServer sets a server as the active default
func (s *DockerService) SetDefaultServer(ctx context.Context, id uint) error {
	if s.db == nil {
		return errors.New("database unavailable")
	}

	s.db.Model(&models.DockerServer{}).Where("is_default = ?", true).Update("is_default", false)
	return s.db.Model(&models.DockerServer{}).Where("id = ?", id).Update("is_default", true).Error
}

// TestServer verifies connectivity to a target Docker host and returns its version
func (s *DockerService) TestServer(ctx context.Context, req models.TestServerRequest) (string, error) {
	testCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if err := s.PrepareTestServerRequest(testCtx, &req); err != nil {
		return "", err
	}

	res, err := RunServerDiagnostics(testCtx, req, nil)
	if err != nil {
		return "", err
	}
	if res.Status != "CONNECTED" {
		return "", errors.New(res.Message)
	}
	return res.Version, nil
}

// --- Backward-compatible wrappers (default server) ---

func (s *DockerService) IsConnected() bool {
	return s.connected && s.defaultCli != nil
}

func (s *DockerService) GetVersion(ctx context.Context) string {
	return s.GetVersionOnServer(ctx, 0)
}

func (s *DockerService) GetCounts(ctx context.Context) (int, int) {
	return s.GetCountsOnServer(ctx, 0)
}

func (s *DockerService) ListContainers(ctx context.Context, all bool) ([]models.ContainerSummary, error) {
	return s.ListContainersOnServer(ctx, 0, all)
}

func (s *DockerService) StartContainer(ctx context.Context, id string) error {
	return s.StartContainerOnServer(ctx, 0, id)
}

func (s *DockerService) StopContainer(ctx context.Context, id string) error {
	return s.StopContainerOnServer(ctx, 0, id)
}

func (s *DockerService) RestartContainer(ctx context.Context, id string) error {
	return s.RestartContainerOnServer(ctx, 0, id)
}

func (s *DockerService) RemoveContainer(ctx context.Context, id string, force bool) error {
	return s.RemoveContainerOnServer(ctx, 0, id, force)
}

func (s *DockerService) ListImages(ctx context.Context) ([]models.ImageSummary, error) {
	return s.ListImagesOnServer(ctx, 0)
}

func (s *DockerService) SearchImages(ctx context.Context, term string) ([]models.ImageSearchResult, error) {
	return s.SearchImagesOnServer(ctx, 0, term)
}

func (s *DockerService) PullImageStream(ctx context.Context, imageName string) (io.ReadCloser, error) {
	return s.PullImageStreamOnServer(ctx, 0, imageName)
}

func (s *DockerService) PullImage(ctx context.Context, imageName string) error {
	return s.PullImageOnServer(ctx, 0, imageName)
}

func (s *DockerService) RemoveImage(ctx context.Context, id string, force bool) error {
	return s.RemoveImageOnServer(ctx, 0, id, force)
}

func (s *DockerService) StreamContainerLogs(ctx context.Context, id string, follow bool, tail string) (io.ReadCloser, error) {
	return s.StreamContainerLogsOnServer(ctx, 0, id, follow, tail)
}

func (s *DockerService) IsContainerTTY(ctx context.Context, id string) (bool, error) {
	return s.IsContainerTTYOnServer(ctx, 0, id)
}

// --- Multi-Host Methods (*OnServer) ---

func (s *DockerService) GetVersionOnServer(ctx context.Context, serverID uint) string {
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return "unavailable"
	}
	ver, err := cli.ServerVersion(ctx)
	if err != nil {
		return "unknown"
	}
	return ver.Version
}

func (s *DockerService) GetCountsOnServer(ctx context.Context, serverID uint) (int, int) {
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return 0, 0
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	containerCount := len(containers)
	if err != nil {
		containerCount = 0
	}

	images, err := cli.ImageList(ctx, image.ListOptions{All: true})
	imageCount := len(images)
	if err != nil {
		imageCount = 0
	}

	return containerCount, imageCount
}

func (s *DockerService) ListContainersOnServer(ctx context.Context, serverID uint, all bool) ([]models.ContainerSummary, error) {
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return []models.ContainerSummary{}, errors.New("docker daemon is currently unavailable")
	}

	rawList, err := cli.ContainerList(ctx, container.ListOptions{All: all})
	if err != nil {
		return nil, err
	}

	result := make([]models.ContainerSummary, 0, len(rawList))
	for _, c := range rawList {
		ports := make([]models.PortSummary, 0, len(c.Ports))
		for _, p := range c.Ports {
			ports = append(ports, models.PortSummary{
				IP:          p.IP,
				PrivatePort: p.PrivatePort,
				PublicPort:  p.PublicPort,
				Type:        p.Type,
			})
		}

		result = append(result, models.ContainerSummary{
			ID:      c.ID,
			Names:   c.Names,
			Image:   c.Image,
			Command: c.Command,
			Created: c.Created,
			State:   c.State,
			Status:  c.Status,
			Ports:   ports,
		})
	}

	return result, nil
}

func (s *DockerService) StartContainerOnServer(ctx context.Context, serverID uint, id string) error {
	if err := validateContainerID(id); err != nil {
		return err
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return errors.New("docker daemon is currently unavailable")
	}
	return cli.ContainerStart(ctx, id, container.StartOptions{})
}

func (s *DockerService) StopContainerOnServer(ctx context.Context, serverID uint, id string) error {
	if err := validateContainerID(id); err != nil {
		return err
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return errors.New("docker daemon is currently unavailable")
	}
	timeout := 10
	return cli.ContainerStop(ctx, id, container.StopOptions{Timeout: &timeout})
}

func (s *DockerService) RestartContainerOnServer(ctx context.Context, serverID uint, id string) error {
	if err := validateContainerID(id); err != nil {
		return err
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return errors.New("docker daemon is currently unavailable")
	}
	timeout := 10
	return cli.ContainerRestart(ctx, id, container.StopOptions{Timeout: &timeout})
}

func (s *DockerService) RemoveContainerOnServer(ctx context.Context, serverID uint, id string, force bool) error {
	if err := validateContainerID(id); err != nil {
		return err
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return errors.New("docker daemon is currently unavailable")
	}
	return cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: force})
}

func (s *DockerService) ListImagesOnServer(ctx context.Context, serverID uint) ([]models.ImageSummary, error) {
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return []models.ImageSummary{}, errors.New("docker daemon is currently unavailable")
	}

	rawList, err := cli.ImageList(ctx, image.ListOptions{All: false})
	if err != nil {
		return nil, err
	}

	result := make([]models.ImageSummary, 0, len(rawList))
	for _, img := range rawList {
		result = append(result, models.ImageSummary{
			ID:       img.ID,
			RepoTags: img.RepoTags,
			Size:     img.Size,
			Created:  img.Created,
		})
	}

	return result, nil
}

func (s *DockerService) SearchImagesOnServer(ctx context.Context, serverID uint, term string) ([]models.ImageSearchResult, error) {
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return nil, errors.New("docker daemon is currently unavailable")
	}

	results, err := cli.ImageSearch(ctx, term, registry.SearchOptions{Limit: 25})
	if err != nil {
		return nil, err
	}

	out := make([]models.ImageSearchResult, 0, len(results))
	for _, r := range results {
		out = append(out, models.ImageSearchResult{
			Name:        r.Name,
			Description: r.Description,
			StarCount:   r.StarCount,
			IsOfficial:  r.IsOfficial,
		})
	}
	return out, nil
}

func (s *DockerService) PullImageStreamOnServer(ctx context.Context, serverID uint, imageName string) (io.ReadCloser, error) {
	if err := validateImageRef(imageName); err != nil {
		return nil, err
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return nil, errors.New("docker daemon is currently unavailable")
	}

	return cli.ImagePull(ctx, imageName, image.PullOptions{})
}

func (s *DockerService) PullImageOnServer(ctx context.Context, serverID uint, imageName string) error {
	if err := validateImageRef(imageName); err != nil {
		return err
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return errors.New("docker daemon is currently unavailable")
	}

	out, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(io.Discard, out)
	return err
}

func (s *DockerService) RemoveImageOnServer(ctx context.Context, serverID uint, id string, force bool) error {
	if err := validateImageRef(id); err != nil {
		return err
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return errors.New("docker daemon is currently unavailable")
	}
	_, err = cli.ImageRemove(ctx, id, image.RemoveOptions{Force: force, PruneChildren: true})
	return err
}

func (s *DockerService) StreamContainerLogsOnServer(ctx context.Context, serverID uint, id string, follow bool, tail string) (io.ReadCloser, error) {
	if err := validateContainerID(id); err != nil {
		return nil, err
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return nil, errors.New("docker daemon is currently unavailable")
	}

	if tail == "" {
		tail = "100"
	}

	return cli.ContainerLogs(ctx, id, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Tail:       tail,
		Timestamps: true,
	})
}

func (s *DockerService) IsContainerTTYOnServer(ctx context.Context, serverID uint, id string) (bool, error) {
	if err := validateContainerID(id); err != nil {
		return false, err
	}
	cli, err := s.GetClient(ctx, serverID)
	if err != nil || cli == nil {
		return false, errors.New("docker daemon is currently unavailable")
	}
	inspect, err := cli.ContainerInspect(ctx, id)
	if err != nil {
		return false, err
	}
	return inspect.Config.Tty, nil
}
