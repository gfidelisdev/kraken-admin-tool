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

	log.Printf("[PLUGIN:%s] Intercepted host adapter operation: '%s' (user: %s)", pluginID, operation, username)

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
		return m.executeHostsList(ctx)

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

// executeHostsList returns list of monitored hosts
func (m *Manager) executeHostsList(ctx context.Context) (HostAdapterResponse, error) {
	var servers []models.DockerServer
	if err := m.db.Find(&servers).Error; err != nil {
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
		return HostAdapterResponse{
			Status:    "error",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Error:     fmt.Sprintf("Server '%s' not found", targetHost),
		}, nil
	}

	out, runErr := m.execOnServer(ctx, srv, cmd)
	exitCode := 0
	if runErr != nil {
		exitCode = 1
	}

	audit.Log(m.db, userID, username, "HOST_COMMAND_EXEC", "host", srv.Name,
		fmt.Sprintf("Plugin '%s' executed command '%s' on %s", pluginID, cmd, srv.Name), "SUCCESS")

	return HostAdapterResponse{
		Status:    "success",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Data: map[string]interface{}{
			"stdout":    out,
			"exit_code": exitCode,
		},
	}, nil
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
