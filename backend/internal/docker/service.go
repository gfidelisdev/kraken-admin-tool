package docker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
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
	db          *gorm.DB
	defaultHost string
	defaultCli  *client.Client
	connected   bool
	errMsg      string
	mu          sync.RWMutex
	clients     map[uint]*client.Client
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

// GetClient retrieves the Docker client for a specific serverID (or default if 0)
func (s *DockerService) GetClient(ctx context.Context, serverID uint) (*client.Client, error) {
	// If serverID is 0, find default server in DB or fallback
	if serverID == 0 {
		if s.db != nil {
			var defaultServer models.DockerServer
			if err := s.db.Where("is_default = ?", true).First(&defaultServer).Error; err == nil {
				return s.GetClient(ctx, defaultServer.ID)
			}
		}
		if s.defaultCli != nil && s.connected {
			return s.defaultCli, nil
		}
		if s.defaultCli != nil {
			return s.defaultCli, nil
		}
		return nil, errors.New("docker daemon is currently unavailable")
	}

	// Check cached client
	s.mu.RLock()
	cli, exists := s.clients[serverID]
	s.mu.RUnlock()
	if exists && cli != nil {
		return cli, nil
	}

	// Lock to create new client
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double check after lock
	if cli, exists := s.clients[serverID]; exists && cli != nil {
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

	tlsKey, _ := krakencrypto.Decrypt(server.TLSKey)
	sshPassword, _ := krakencrypto.Decrypt(server.SSHPassword)
	sshPrivateKey, _ := krakencrypto.Decrypt(server.SSHPrivateKey)
	sshPassphrase, _ := krakencrypto.Decrypt(server.SSHKeyPassphrase)

	newCli, err := createDockerClientWithSSH(
		server.HostURL,
		server.TLSVerify,
		server.TLSCACert,
		server.TLSCert,
		tlsKey,
		SSHConfig{
			HostURL:       server.HostURL,
			User:          server.SSHUser,
			AuthMethod:    server.SSHAuthMethod,
			Password:      sshPassword,
			PrivateKey:    sshPrivateKey,
			KeyPassphrase: sshPassphrase,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for server %s: %w", server.Name, err)
	}

	// Test connection ping
	pingCtx, pingCancel := context.WithTimeout(ctx, 3*time.Second)
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

	s.clients[serverID] = newCli
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
	return &server, nil
}

// GetSSHClient establishes an authenticated SSH client connection for the specified serverID
func (s *DockerService) GetSSHClient(ctx context.Context, serverID uint) (*ssh.Client, *models.DockerServer, error) {
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

	sshPassword, _ := krakencrypto.Decrypt(server.SSHPassword)
	sshPrivateKey, _ := krakencrypto.Decrypt(server.SSHPrivateKey)
	sshPassphrase, _ := krakencrypto.Decrypt(server.SSHKeyPassphrase)

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

// ListServers returns all registered Docker servers with updated status and counts
func (s *DockerService) ListServers(ctx context.Context) ([]models.DockerServer, error) {
	if s.db == nil {
		// Return single default server mock if no DB
		return []models.DockerServer{
			{
				ID:        1,
				Name:      "Local Daemon",
				HostURL:   s.defaultHost,
				IsDefault: true,
				Status:    map[bool]string{true: "CONNECTED", false: "UNREACHABLE"}[s.connected],
			},
		}, nil
	}

	var servers []models.DockerServer
	if err := s.db.Order("is_default desc, id asc").Find(&servers).Error; err != nil {
		return nil, err
	}

	// Enrich servers with container/image counts and versions
	for i := range servers {
		cli, err := s.GetClient(ctx, servers[i].ID)
		if err == nil && cli != nil {
			pingCtx, pingCancel := context.WithTimeout(ctx, 2*time.Second)
			ver, verErr := cli.ServerVersion(pingCtx)
			pingCancel()

			if verErr == nil {
				servers[i].Status = "CONNECTED"
				servers[i].Version = ver.Version
				servers[i].ErrMsg = ""

				// Fetch quick counts
				cList, _ := cli.ContainerList(ctx, container.ListOptions{All: true})
				servers[i].Containers = len(cList)
				imgList, _ := cli.ImageList(ctx, image.ListOptions{All: false})
				servers[i].Images = len(imgList)
			} else {
				servers[i].Status = "UNREACHABLE"
				servers[i].ErrMsg = verErr.Error()
			}
		} else {
			servers[i].Status = "UNREACHABLE"
			if err != nil {
				servers[i].ErrMsg = err.Error()
			}
		}
	}

	return servers, nil
}

// CreateServer registers a new Docker host
func (s *DockerService) CreateServer(ctx context.Context, req models.CreateServerRequest) (*models.DockerServer, error) {
	if s.db == nil {
		return nil, errors.New("database unavailable")
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

	// Test connection
	status := "CONNECTED"
	errMsg := ""
	version := ""
	testVer, testErr := s.TestServer(ctx, models.TestServerRequest{
		HostURL:          hostURL,
		TLSVerify:        req.TLSVerify,
		TLSCACert:        req.TLSCACert,
		TLSCert:          req.TLSCert,
		TLSKey:           req.TLSKey,
		SSHUser:          sshUser,
		SSHAuthMethod:    sshAuthMethod,
		SSHPassword:      req.SSHPassword,
		SSHPrivateKey:    req.SSHPrivateKey,
		SSHKeyPassphrase: req.SSHKeyPassphrase,
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

	// Encrypt sensitive credentials before saving to database
	encTLSKey, _ := krakencrypto.Encrypt(req.TLSKey)
	encSSHPassword, _ := krakencrypto.Encrypt(req.SSHPassword)
	encSSHPrivateKey, _ := krakencrypto.Encrypt(req.SSHPrivateKey)
	encSSHKeyPassphrase, _ := krakencrypto.Encrypt(req.SSHKeyPassphrase)

	server := models.DockerServer{
		Name:             name,
		HostURL:          hostURL,
		TLSVerify:        req.TLSVerify,
		TLSCACert:        req.TLSCACert,
		TLSCert:          req.TLSCert,
		TLSKey:           encTLSKey,
		SSHUser:          sshUser,
		SSHAuthMethod:    sshAuthMethod,
		SSHPassword:      encSSHPassword,
		SSHPrivateKey:    encSSHPrivateKey,
		SSHKeyPassphrase: encSSHKeyPassphrase,
		IsDefault:        req.IsDefault,
		Status:           status,
		ErrMsg:           errMsg,
		Version:          version,
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

	if req.IsDefault {
		s.db.Model(&models.DockerServer{}).Where("id != ?", id).Update("is_default", false)
		server.IsDefault = true
	}

	// Test connection
	testVer, testErr := s.TestServer(ctx, models.TestServerRequest{
		HostURL:          server.HostURL,
		TLSVerify:        server.TLSVerify,
		TLSCACert:        server.TLSCACert,
		TLSCert:          server.TLSCert,
		TLSKey:           server.TLSKey,
		SSHUser:          server.SSHUser,
		SSHAuthMethod:    server.SSHAuthMethod,
		SSHPassword:      server.SSHPassword,
		SSHPrivateKey:    server.SSHPrivateKey,
		SSHKeyPassphrase: server.SSHKeyPassphrase,
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
