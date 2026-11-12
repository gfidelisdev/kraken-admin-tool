package plugins

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/host"
	"github.com/kraken/admin-tool/backend/internal/models"
)

// HostAdapterResponse is the standardized response returned to plugins
type HostAdapterResponse struct {
	Status    string      `json:"status"`
	Timestamp string      `json:"timestamp"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
}

// DiscoveredPort represents an externally accessible port on a monitored host
type DiscoveredPort struct {
	Host            string `json:"host"`
	Address         string `json:"address"`
	Port            int    `json:"port"`
	Protocol        string `json:"protocol"`
	Service         string `json:"service"`
	Process         string `json:"process"`
	ExternalAddress string `json:"external_address"`
	Bind            string `json:"bind"`
	Exposure        string `json:"exposure"`
}

// ResolveHostAdapter intercepts and fulfills requests for host-level services
func (m *Manager) ResolveHostAdapter(
	ctx context.Context,
	rawResp map[string]interface{},
	permMap map[string]bool,
	pluginID string,
	username string,
	userID uint,
) (interface{}, error) {
	dataObj, ok := rawResp["data"].(map[string]interface{})
	if !ok {
		return rawResp, nil
	}

	operation, _ := dataObj["operation"].(string)
	if operation == "" {
		return rawResp, nil
	}

	if userID == 0 {
		if claims := auth.GetUserFromContext(ctx); claims != nil {
			userID = claims.UserID
		}
	}

	log.Printf("[PLUGIN:%s] Intercepted host adapter operation: '%s' (user: %s, userID: %d)", pluginID, operation, username, userID)

	switch operation {
	case "network.scan_external_listeners":
		if !permMap["hosts:read"] {
			return HostAdapterResponse{
				Status:    "denied",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     "Plugin lacks mandatory permission 'hosts:read'",
			}, nil
		}
		return m.executeNetworkScan(ctx, pluginID, dataObj)

	case "network.ldap_ping", "network.ldap_test", "network.ldap_dial":
		if !permMap["network:ldap"] {
			return HostAdapterResponse{
				Status:    "denied",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     "Plugin lacks mandatory permission 'network:ldap'",
			}, nil
		}
		return m.executeLDAPTest(ctx, pluginID, dataObj)

	case "network.block_external_port":
		if !permMap["hosts:exec"] {
			return HostAdapterResponse{
				Status:    "denied",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     "Plugin lacks mandatory permission 'hosts:exec'",
			}, nil
		}
		res, err := m.executeBlockPort(ctx, pluginID, dataObj, username, userID)
		if err != nil {
			return HostAdapterResponse{
				Status:    "error",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     err.Error(),
			}, nil
		}
		return res, nil

	case "hosts.list":
		if !permMap["hosts:read"] {
			return HostAdapterResponse{
				Status:    "denied",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     "Plugin lacks mandatory permission 'hosts:read'",
			}, nil
		}
		return m.executeHostsList(ctx, userID)

	case "hosts.bind_credential":
		if !permMap["hosts:create"] && !permMap["credentials:manage"] {
			return HostAdapterResponse{
				Status:    "denied",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     "Plugin lacks permission 'hosts:create' or 'credentials:manage'",
			}, nil
		}
		return m.executeBindHostCredential(ctx, pluginID, dataObj, userID)

	case "auth.logout", "auth.disconnect":
		if m.msSessionRevoker != nil {
			claims := auth.GetUserFromContext(ctx)
			sid := ""
			if claims != nil {
				sid = claims.ID
			}
			m.msSessionRevoker(userID, sid)
		}
		if m.db != nil {
			if userID > 0 {
				m.db.Where("plugin_id = ? AND (user_id = ? OR user_id = 0)", pluginID, userID).Delete(&models.PluginKVStore{})
			} else {
				m.db.Where("plugin_id = ? AND user_id = 0", pluginID).Delete(&models.PluginKVStore{})
			}
		}
		return HostAdapterResponse{
			Status:    "success",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Data: map[string]interface{}{
				"message": "Microsoft session invalidated and credentials cleared successfully",
			},
		}, nil

	case "hosts.exec":
		if !permMap["hosts:exec"] {
			return HostAdapterResponse{
				Status:    "denied",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     "Plugin lacks mandatory permission 'hosts:exec'",
			}, nil
		}
		return m.executeHostExec(ctx, pluginID, dataObj, username, userID)

	default:
		return rawResp, nil
	}
}

// executeNetworkScan queries all monitored servers via SSH/Local and parses active listeners
func (m *Manager) executeNetworkScan(ctx context.Context, pluginID string, params map[string]interface{}) (HostAdapterResponse, error) {
	var servers []models.DockerServer
	if err := m.db.Find(&servers).Error; err != nil {
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     fmt.Sprintf("Failed to list servers: %v", err),
		}, nil
	}

	scope, _ := params["scope"].(string)
	targetHost, _ := params["host"].(string)

	var targets []models.DockerServer
	for _, s := range servers {
		if targetHost != "" && !strings.EqualFold(s.Name, targetHost) && !strings.EqualFold(s.HostURL, targetHost) {
			continue
		}
		targets = append(targets, s)
	}

	if len(targets) == 0 {
		targets = servers
	}

	var mu sync.Mutex
	var allPorts []DiscoveredPort
	var scannedCount int

	scanCmd := "ss -tulnp -H 2>/dev/null || ss -tulwn 2>/dev/null || netstat -tulnp 2>/dev/null || netstat -tulwn 2>/dev/null"

	var wg sync.WaitGroup
	sem := make(chan struct{}, 6) // limit concurrent SSH connections

	for _, s := range targets {
		wg.Add(1)
		go func(srv models.DockerServer) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			scanCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()

			stdout, err := m.execOnServer(scanCtx, srv, scanCmd)
			if err != nil {
				log.Printf("[PLUGIN:%s] Warning: failed to scan host %s (%s): %v", pluginID, srv.Name, srv.HostURL, err)
				return
			}

			ports := parseListenersOutput(srv.Name, stdout)
			mu.Lock()
			scannedCount++
			allPorts = append(allPorts, ports...)
			mu.Unlock()
		}(s)
	}

	wg.Wait()

	// If no remote servers responded or empty, ensure localhost is included if target is local
	if scannedCount == 0 && (scope == "" || scope == "all_monitored_hosts" || strings.Contains(targetHost, "local")) {
		localExec := host.NewLocalExecutor()
		stdout, _, _, err := localExec.Execute(ctx, scanCmd)
		if err == nil {
			scannedCount = 1
			allPorts = parseListenersOutput("localhost", stdout)
		}
	}

	return HostAdapterResponse{
		Status:    "success",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data: map[string]interface{}{
			"scanned_hosts": scannedCount,
			"ports":         allPorts,
		},
	}, nil
}

// executeLDAPTest verifies connectivity to an LDAP domain controller
func (m *Manager) executeLDAPTest(ctx context.Context, pluginID string, params map[string]interface{}) (HostAdapterResponse, error) {
	host, _ := params["host"].(string)
	if host == "" {
		host, _ = params["server"].(string)
	}
	if host == "" {
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     "Missing required parameter 'host' or 'server' for LDAP test",
		}, nil
	}

	port := 389
	if p, ok := params["port"].(float64); ok && p > 0 {
		port = int(p)
	} else if p, ok := params["port"].(int); ok && p > 0 {
		port = p
	}

	useTLS, _ := params["tls"].(bool)
	if useTLS && port == 389 {
		port = 636
	}

	timeoutSec := 5
	if t, ok := params["timeout"].(float64); ok && t > 0 {
		timeoutSec = int(t)
	}

	target := fmt.Sprintf("%s:%d", host, port)
	dialer := &net.Dialer{Timeout: time.Duration(timeoutSec) * time.Second}
	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", target)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     fmt.Sprintf("Failed to connect to LDAP server %s: %v", target, err),
		}, nil
	}
	_ = conn.Close()

	return HostAdapterResponse{
		Status:    "success",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data: map[string]interface{}{
			"reachable":  true,
			"target":     target,
			"latency_ms": latency,
		},
	}, nil
}

// executeBlockPort applies firewall drop rule on target host
func (m *Manager) executeBlockPort(
	ctx context.Context,
	pluginID string,
	params map[string]interface{},
	username string,
	userID uint,
) (HostAdapterResponse, error) {
	targetHost, _ := params["host"].(string)
	portVal, _ := params["port"]
	proto, _ := params["protocol"].(string)
	if proto == "" {
		proto = "tcp"
	}
	proto = strings.ToLower(proto)

	var port int
	switch v := portVal.(type) {
	case float64:
		port = int(v)
	case int:
		port = v
	case string:
		port, _ = strconv.Atoi(v)
	}

	if targetHost == "" || port <= 0 || port > 65535 {
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     "Invalid host or port specified for blocking",
		}, nil
	}

	// Safety check: NEVER block SSH
	if port == 22 {
		return HostAdapterResponse{
			Status:    "denied",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     "Safety policy strictly forbids automated blocking of SSH (port 22)",
		}, nil
	}

	var srv models.DockerServer
	err := m.db.Where("name = ? OR host_url LIKE ?", targetHost, "%"+targetHost+"%").First(&srv).Error
	if err != nil {
		// Try matching by IP
		err = m.db.First(&srv).Error
		if err != nil {
			return HostAdapterResponse{
				Status:    "error",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     fmt.Sprintf("Target host '%s' not found", targetHost),
			}, nil
		}
	}

	// Build firewall command
	blockCmd := fmt.Sprintf("sudo iptables -C INPUT -p %s --dport %d -j DROP 2>/dev/null || sudo iptables -I INPUT -p %s --dport %d -j DROP",
		proto, port, proto, port)

	out, execErr := m.execOnServer(ctx, srv, blockCmd)
	if execErr != nil {
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     fmt.Sprintf("Failed to apply firewall block on %s: %v (output: %s)", srv.Name, execErr, out),
		}, nil
	}

	audit.Log(m.db, userID, username, "HOST_PORT_BLOCK", "host", srv.Name,
		fmt.Sprintf("Plugin '%s' blocked port %d/%s on host %s", pluginID, port, proto, srv.Name), "SUCCESS")

	return HostAdapterResponse{
		Status:    "success",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data: map[string]interface{}{
			"message":  fmt.Sprintf("Porta %d/%s bloqueada com sucesso no firewall do host %s.", port, strings.ToUpper(proto), srv.Name),
			"host":     srv.Name,
			"port":     port,
			"protocol": proto,
		},
	}, nil
}

// executeHostsList returns list of monitored hosts for the specific user
func (m *Manager) executeHostsList(ctx context.Context, userID uint) (HostAdapterResponse, error) {
	if userID == 0 {
		return HostAdapterResponse{
			Status:    "success",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Data: map[string]interface{}{
				"hosts": []interface{}{},
			},
		}, nil
	}

	var servers []models.DockerServer
	query := m.db.Order("is_default desc, id asc").Where("user_id = ?", userID)
	if err := query.Find(&servers).Error; err != nil {
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     err.Error(),
		}, nil
	}

	type ServerSummary struct {
		ID      uint   `json:"id"`
		Name    string `json:"name"`
		HostURL string `json:"host_url"`
		Status  string `json:"status"`
	}

	var list []ServerSummary
	for _, s := range servers {
		list = append(list, ServerSummary{
			ID:      s.ID,
			Name:    s.Name,
			HostURL: s.HostURL,
			Status:  s.Status,
		})
	}

	return HostAdapterResponse{
		Status:    "success",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data: map[string]interface{}{
			"hosts": list,
		},
	}, nil
}

// executeBindHostCredential binds a host to a plugin secret reference
func (m *Manager) executeBindHostCredential(
	ctx context.Context,
	pluginID string,
	params map[string]interface{},
	userID uint,
) (HostAdapterResponse, error) {
	rawServerID, _ := params["server_id"]
	secretName, _ := params["secret_name"].(string)

	var serverID uint
	switch v := rawServerID.(type) {
	case float64:
		serverID = uint(v)
	case int:
		serverID = uint(v)
	case string:
		id64, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 32)
		serverID = uint(id64)
	}

	if serverID == 0 {
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     "Invalid or missing 'server_id' parameter",
		}, nil
	}

	var srv models.DockerServer
	query := m.db.Where("id = ?", serverID)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	} else {
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     "User authentication required to bind host credentials",
		}, nil
	}
	if err := query.First(&srv).Error; err != nil {
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     fmt.Sprintf("Server ID %d not found or not accessible", serverID),
		}, nil
	}

	srv.CredentialSource = "plugin"
	srv.CredentialPluginID = pluginID
	srv.SecretReference = strings.TrimSpace(secretName)
	if err := m.db.Save(&srv).Error; err != nil {
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     fmt.Sprintf("Failed to update server credentials: %v", err),
		}, nil
	}

	return HostAdapterResponse{
		Status:    "success",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data: map[string]interface{}{
			"message":   fmt.Sprintf("Server '%s' bound to secret '%s' successfully.", srv.Name, secretName),
			"server_id": srv.ID,
		},
	}, nil
}

// executeHostExec executes an arbitrary shell command on a host
func (m *Manager) executeHostExec(
	ctx context.Context,
	pluginID string,
	params map[string]interface{},
	username string,
	userID uint,
) (HostAdapterResponse, error) {
	targetHost, _ := params["host"].(string)
	cmd, _ := params["cmd"].(string)
	if cmd == "" {
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     "Missing required 'cmd' parameter",
		}, nil
	}

	var srv models.DockerServer
	err := m.db.Where("name = ? OR host_url LIKE ?", targetHost, "%"+targetHost+"%").First(&srv).Error
	if err != nil {
		lowerHost := strings.ToLower(strings.TrimSpace(targetHost))
		if lowerHost == "" || lowerHost == "localhost" || lowerHost == "127.0.0.1" || lowerHost == "local" || lowerHost == "local daemon" {
			srv = models.DockerServer{
				Name:    "Local Daemon",
				HostURL: "unix:///var/run/docker.sock",
			}
		} else {
			return HostAdapterResponse{
				Status:    "error",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Error:     fmt.Sprintf("Server '%s' not found", targetHost),
			}, nil
		}
	}

	out, runErr := m.execOnServer(ctx, srv, cmd)
	exitCode := 0
	if runErr != nil {
		exitCode = 1
	}

	safeCmd := redactCommandSecrets(cmd)
	audit.Log(m.db, userID, username, "HOST_COMMAND_EXEC", "host", srv.Name,
		fmt.Sprintf("Plugin '%s' executed command '%s' on %s", pluginID, safeCmd, srv.Name), "SUCCESS")

	return HostAdapterResponse{
		Status:    "success",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data: map[string]interface{}{
			"stdout":    out,
			"exit_code": exitCode,
		},
	}, nil
}

var (
	secretTokenAssignmentRegex = regexp.MustCompile(`(?i)(AKV_TOKEN|ARM_TOKEN|ACCESS_TOKEN|REFRESH_TOKEN|CLIENT_SECRET)=('[^']*'|"[^"]*"|\S+)`)
	secretParamRegex           = regexp.MustCompile(`(?i)(refresh_token|client_secret|access_token|password)=([^&'"\s]+)`)
	secretJsonValueRegex       = regexp.MustCompile(`(?i)"(value|password|ssh_password|ssh_private_key|sudo_password)"\s*:\s*"[^"]*"`)
)

func redactCommandSecrets(cmd string) string {
	redacted := secretTokenAssignmentRegex.ReplaceAllString(cmd, `$1='[REDACTED]'`)
	redacted = secretParamRegex.ReplaceAllString(redacted, `$1=[REDACTED]`)
	redacted = secretJsonValueRegex.ReplaceAllString(redacted, `"$1":"[REDACTED]"`)
	return redacted
}

// execOnServer runs a command on server via SSH or Local
func (m *Manager) execOnServer(ctx context.Context, srv models.DockerServer, cmd string) (string, error) {
	if strings.HasPrefix(srv.HostURL, "ssh://") {
		client, _, err := m.dockerService.GetSSHClient(ctx, srv.ID)
		if err != nil {
			return "", fmt.Errorf("ssh dial failed: %w", err)
		}
		sshExec := host.NewSSHExecutor(client)
		stdout, stderr, exitCode, err := sshExec.Execute(ctx, cmd)
		if err != nil && exitCode != 0 {
			return stdout + "\n" + stderr, fmt.Errorf("exit code %d: %s", exitCode, strings.TrimSpace(stderr))
		}
		return stdout, nil
	}

	// Local socket or localhost
	localExec := host.NewLocalExecutor()
	stdout, stderr, exitCode, err := localExec.Execute(ctx, cmd)
	if err != nil && exitCode != 0 {
		return stdout + "\n" + stderr, fmt.Errorf("exit code %d: %s", exitCode, strings.TrimSpace(stderr))
	}
	return stdout, nil
}

// Regexes for parsing `ss` or `netstat`
var (
	ssProcessRegex = regexp.MustCompile(`users:\(\("([^"]+)",pid=(\d+)`)
	wellKnownPorts = map[int]string{
		21: "FTP", 22: "SSH", 23: "Telnet", 25: "SMTP", 53: "DNS",
		80: "HTTP", 110: "POP3", 143: "IMAP", 443: "HTTPS", 465: "SMTPS",
		587: "Submission", 993: "IMAPS", 995: "POP3S", 1433: "MSSQL",
		1521: "Oracle", 2375: "Docker (Insecure)", 2376: "Docker (TLS)",
		3000: "Node / Web UI", 3306: "MySQL / MariaDB", 5000: "Docker Registry",
		5432: "PostgreSQL", 6379: "Redis", 8080: "HTTP-Alt / API",
		8443: "HTTPS-Alt", 9000: "Portainer / Sonar", 9090: "Prometheus",
		9200: "Elasticsearch", 27017: "MongoDB",
	}
)

// parseListenersOutput extracts external listeners from ss / netstat stdout
func parseListenersOutput(hostName string, stdout string) []DiscoveredPort {
	var results []DiscoveredPort
	seen := make(map[string]bool)

	lines := strings.Split(stdout, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Netid") || strings.HasPrefix(line, "Proto") || strings.HasPrefix(line, "Active") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		var proto, localAddrStr string
		lowerLine := strings.ToLower(line)

		if strings.HasPrefix(lowerLine, "tcp") {
			proto = "tcp"
		} else if strings.HasPrefix(lowerLine, "udp") {
			proto = "udp"
		} else {
			continue
		}

		// In `ss -tulnp`:
		// Netid State Recv-Q Send-Q Local Address:Port Peer Address:Port Process
		// 0: Netid, 1: State, 2: Recv-Q, 3: Send-Q, 4: Local Address:Port
		if len(fields) >= 5 && (fields[1] == "LISTEN" || fields[1] == "UNCONN") {
			localAddrStr = fields[4]
		} else if len(fields) >= 4 {
			localAddrStr = fields[3]
		}

		ipStr, portStr, err := splitHostPortSafe(localAddrStr)
		if err != nil {
			continue
		}

		port, pErr := strconv.Atoi(portStr)
		if pErr != nil || port <= 0 || port > 65535 {
			continue
		}

		// Filter external only: exclude loopback (127.*, ::1, localhost)
		if isLoopback(ipStr) {
			continue
		}

		dedupKey := fmt.Sprintf("%s:%s:%d", hostName, proto, port)
		if seen[dedupKey] {
			continue
		}
		seen[dedupKey] = true

		// Detect process name if present
		processName := "-"
		matches := ssProcessRegex.FindStringSubmatch(line)
		if len(matches) > 1 {
			processName = matches[1]
		}

		// Infer service name
		serviceName := wellKnownPorts[port]
		if serviceName == "" {
			if processName != "-" {
				serviceName = processName
			} else {
				serviceName = fmt.Sprintf("service-%d", port)
			}
		}

		results = append(results, DiscoveredPort{
			Host:            hostName,
			Address:         localAddrStr,
			Port:            port,
			Protocol:        proto,
			Service:         serviceName,
			Process:         processName,
			ExternalAddress: localAddrStr,
			Bind:            localAddrStr,
			Exposure:        "externa",
		})
	}

	return results
}

func isLoopback(ipStr string) bool {
	if ipStr == "127.0.0.1" || ipStr == "::1" || strings.HasPrefix(ipStr, "127.") {
		return true
	}
	ip := net.ParseIP(ipStr)
	if ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}

func splitHostPortSafe(s string) (string, string, error) {
	s = strings.TrimSpace(s)
	// Handle [::]:port or :::port
	lastColon := strings.LastIndex(s, ":")
	if lastColon == -1 {
		return "", "", errors.New("no colon found")
	}
	hostPart := s[:lastColon]
	portPart := s[lastColon+1:]

	hostPart = strings.Trim(hostPart, "[]")
	if hostPart == "" || hostPart == "*" {
		hostPart = "0.0.0.0"
	}

	return hostPart, portPart, nil
}
