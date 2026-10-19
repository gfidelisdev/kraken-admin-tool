package docker

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/kraken/admin-tool/backend/internal/models"
	"golang.org/x/crypto/ssh"
)

// ServerDiagnosticLog represents a timestamped progress message during connection tests
type ServerDiagnosticLog struct {
	Level      string                 `json:"level"` // "info", "success", "warning", "error"
	Message    string                 `json:"message"`
	Timestamp  string                 `json:"timestamp"`
	Suggestion string                 `json:"suggestion,omitempty"`
	Extra      map[string]interface{} `json:"extra,omitempty"`
}

// ServerDiagnosticResult contains the final output of running connection tests
type ServerDiagnosticResult struct {
	Status     string                `json:"status"` // "CONNECTED", "UNREACHABLE"
	Version    string                `json:"version,omitempty"`
	APIVersion string                `json:"api_version,omitempty"`
	OSArch     string                `json:"os_arch,omitempty"`
	Message    string                `json:"message"`
	Logs       []ServerDiagnosticLog `json:"logs"`
}

// RunServerDiagnostics performs step-by-step connection checks with detailed feedback
func RunServerDiagnostics(
	ctx context.Context,
	req models.TestServerRequest,
	onLog func(log ServerDiagnosticLog),
) (*ServerDiagnosticResult, error) {
	var logs []ServerDiagnosticLog

	emit := func(level, msg string, suggestion string, extra map[string]interface{}) {
		entry := ServerDiagnosticLog{
			Level:      level,
			Message:    msg,
			Timestamp:  time.Now().Format("15:04:05"),
			Suggestion: suggestion,
			Extra:      extra,
		}
		logs = append(logs, entry)
		if onLog != nil {
			onLog(entry)
		}
	}

	// 1. Resolve host URL from structured fields if needed
	hostURL := strings.TrimSpace(req.HostURL)
	serverHost := strings.TrimSpace(req.ServerHost)
	serverPort := strings.TrimSpace(req.ServerPort)

	if hostURL == "" && serverHost != "" {
		port := serverPort
		if port == "" {
			port = "22"
		}
		user := strings.TrimSpace(req.SSHUser)
		if user == "" {
			user = "root"
		}
		hostURL = fmt.Sprintf("ssh://%s@%s:%s", user, serverHost, port)
	}

	if hostURL == "" {
		err := errors.New("host URL or server hostname/IP is required")
		emit("error", err.Error(), "Informe o nome ou IP do servidor.", nil)
		return &ServerDiagnosticResult{
			Status:  "UNREACHABLE",
			Message: err.Error(),
			Logs:    logs,
		}, err
	}

	emit("info", fmt.Sprintf("Iniciando diagnóstico de conexão para: %s", hostURL), "", nil)

	// Check if this is an SSH host
	if IsSSHHost(hostURL) {
		return runSSHDiagnostics(ctx, hostURL, req, emit, logs)
	}

	// Check if this is a local socket
	if strings.HasPrefix(strings.ToLower(hostURL), "unix://") {
		return runLocalSocketDiagnostics(ctx, hostURL, emit, logs)
	}

	// Standard TCP / TLS diagnostics
	return runTCPDiagnostics(ctx, hostURL, req, emit, logs)
}

// ValidateHostSafety prevents Server-Side Request Forgery (SSRF) against sensitive internal endpoints
func ValidateHostSafety(host string) error {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		return errors.New("empty host")
	}

	lower := strings.ToLower(trimmed)
	if lower == "metadata.google.internal" || lower == "169.254.169.254" || strings.HasPrefix(lower, "169.254.") {
		return errors.New("access to cloud metadata service is prohibited")
	}

	ips, err := net.LookupIP(trimmed)
	if err == nil {
		for _, ip := range ips {
			if ip.Equal(net.ParseIP("169.254.169.254")) || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				return fmt.Errorf("access to link-local/metadata address (%s) is prohibited", ip.String())
			}
		}
	}
	return nil
}

func runSSHDiagnostics(
	ctx context.Context,
	hostURL string,
	req models.TestServerRequest,
	emit func(level, msg string, suggestion string, extra map[string]interface{}),
	logs []ServerDiagnosticLog,
) (*ServerDiagnosticResult, error) {
	parsed, err := ParseSSHHost(hostURL, req.SSHUser)
	if err != nil {
		emit("error", fmt.Sprintf("Formato de URL SSH inválido: %v", err), "Exemplo de formato: ssh://usuario@192.168.1.100:22", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: err.Error(), Logs: logs}, err
	}

	if err := ValidateHostSafety(parsed.Host); err != nil {
		emit("error", fmt.Sprintf("Alvo inválido ou proibido (SSRF Protection): %v", err), "Endereços de link-local e metadados de nuvem são bloqueados por segurança.", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: err.Error(), Logs: logs}, err
	}

	targetAddr := net.JoinHostPort(parsed.Host, parsed.Port)
	emit("info", fmt.Sprintf("[1/4] Verificando alcance de rede TCP para %s...", targetAddr), "", nil)

	// Step 1: TCP Dial with strict timeout
	tcpTimeout := 5 * time.Second
	tcpDialer := net.Dialer{Timeout: tcpTimeout}
	tcpConn, err := tcpDialer.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		errDesc := fmt.Sprintf("Falha ao abrir conexão TCP com %s: %v", targetAddr, err)
		emit("error", errDesc, "Verifique se o IP ou domínio está correto, se a porta SSH está aberta e se não há bloqueio de firewall ou Security Group.", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: errDesc, Logs: logs}, err
	}
	_ = tcpConn.Close()
	emit("success", fmt.Sprintf("Porta TCP %s acessível com sucesso.", targetAddr), "", nil)

	// Step 2: SSH Handshake and Authentication
	authMethodDesc := "senha"
	if strings.ToLower(req.SSHAuthMethod) == "key" || (req.SSHAuthMethod == "" && strings.TrimSpace(req.SSHPrivateKey) != "") {
		authMethodDesc = "chave privada / certificado"
	}
	emit("info", fmt.Sprintf("[2/4] Realizando handshake SSH e autenticando usuário '%s' (%s)...", parsed.User, authMethodDesc), "", nil)

	sshCfg := SSHConfig{
		HostURL:       hostURL,
		User:          parsed.User,
		AuthMethod:    req.SSHAuthMethod,
		Password:      req.SSHPassword,
		PrivateKey:    req.SSHPrivateKey,
		KeyPassphrase: req.SSHKeyPassphrase,
	}

	dialContext, err := BuildSSHDialContext(sshCfg)
	if err != nil {
		emit("error", fmt.Sprintf("Configuração SSH inválida: %v", err), "Verifique se a chave privada está no formato PEM/OpenSSH válido.", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: err.Error(), Logs: logs}, err
	}

	// Create test SSH client to run remote inspection
	sshClient, err := DialDirectSSHClient(ctx, parsed, sshCfg)
	if err != nil {
		emit("error", fmt.Sprintf("Falha na autenticação SSH com o host %s: %v", targetAddr, err), "Confirme o usuário, senha ou chave SSH. Verifique se o SSH permite esse método em /etc/ssh/sshd_config.", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: err.Error(), Logs: logs}, err
	}
	defer sshClient.Close()
	emit("success", fmt.Sprintf("Autenticação SSH realizada com sucesso para o usuário '%s'.", parsed.User), "", nil)

	// Step 3: Remote Environment Inspection (groups, sockets, docker command)
	emit("info", "[3/4] Inspecionando ambiente remoto (permissões, sockets e serviço Docker)...", "", nil)

	session, sErr := sshClient.NewSession()
	if sErr == nil {
		var outBuf, errBuf bytes.Buffer
		session.Stdout = &outBuf
		session.Stderr = &errBuf

		cmdCtx, cancelCmd := context.WithTimeout(ctx, 6*time.Second)
		defer cancelCmd()

		stdin, _ := session.StdinPipe()

		// Run inspection command through universal sh -s
		inspectCmd := "id -u; echo '---GROUPS---'; id -Gn; echo '---DOCKER_BIN---'; which docker 2>/dev/null || echo 'NOT_FOUND'; echo '---SOCKETS---'; ls -ld /var/run/docker.sock /run/docker.sock 2>/dev/null || echo 'NONE'\n"
		if err := session.Start("sh -s"); err == nil {
			go func() {
				if stdin != nil {
					_, _ = io.WriteString(stdin, inspectCmd)
					_ = stdin.Close()
				}
			}()
		} else {
			_ = session.Start(inspectCmd)
		}

		done := make(chan error, 1)
		go func() {
			done <- session.Wait()
		}()

		select {
		case <-cmdCtx.Done():
			_ = session.Close()
			emit("warning", "Tempo limite excedido ao inspecionar comandos remotos. Prosseguindo com teste de socket...", "", nil)
		case <-done:
			outStr := outBuf.String()
			parseRemoteInspection(outStr, parsed.User, emit)
		}
	}

	// Step 4: Test Docker Socket Forwarding & API Ping
	emit("info", fmt.Sprintf("[4/4] Testando comunicação com o socket do Docker (%s)...", parsed.SocketPath), "", nil)

	tr := &http.Transport{
		DialContext: dialContext,
	}
	httpClient := &http.Client{
		Transport: tr,
		Timeout:   10 * time.Second,
	}

	pingReq, _ := http.NewRequestWithContext(ctx, "GET", "http://api.moby.localhost/_ping", nil)
	pingResp, pingErr := httpClient.Do(pingReq)
	if pingErr != nil {
		errDesc := fmt.Sprintf("Não foi possível conectar ao daemon Docker via socket SSH: %v", pingErr)
		emit("error", errDesc, fmt.Sprintf("Certifique-se de que o Docker está em execução (sudo systemctl status docker) e que o usuário '%s' pertence ao grupo 'docker': sudo usermod -aG docker %s", parsed.User, parsed.User), nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: errDesc, Logs: logs}, pingErr
	}
	_ = pingResp.Body.Close()

	if pingResp.StatusCode != http.StatusOK {
		errDesc := fmt.Sprintf("Docker daemon respondeu com status HTTP inesperado: %d", pingResp.StatusCode)
		emit("error", errDesc, "", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: errDesc, Logs: logs}, errors.New(errDesc)
	}

	emit("success", "Socket do Docker respondeu com sucesso ao comando Ping!", "", nil)

	// Fetch Version
	versionStr := "connected"
	apiVersionStr := ""
	osArchStr := ""

	verReq, _ := http.NewRequestWithContext(ctx, "GET", "http://api.moby.localhost/version", nil)
	verResp, verErr := httpClient.Do(verReq)
	if verErr == nil && verResp.StatusCode == http.StatusOK {
		var verData struct {
			Version    string `json:"Version"`
			APIVersion string `json:"ApiVersion"`
			Os         string `json:"Os"`
			Arch       string `json:"Arch"`
		}
		if decErr := json.NewDecoder(verResp.Body).Decode(&verData); decErr == nil {
			versionStr = verData.Version
			apiVersionStr = verData.APIVersion
			osArchStr = fmt.Sprintf("%s/%s", verData.Os, verData.Arch)
			emit("success", fmt.Sprintf("Docker Engine v%s detectado (API v%s, OS: %s)!", versionStr, apiVersionStr, osArchStr), "", nil)
		}
		_ = verResp.Body.Close()
	} else if verResp != nil {
		_ = verResp.Body.Close()
	}

	emit("success", "Conexão verificada com sucesso! O host está pronto para ser registrado.", "", nil)

	return &ServerDiagnosticResult{
		Status:     "CONNECTED",
		Version:    versionStr,
		APIVersion: apiVersionStr,
		OSArch:     osArchStr,
		Message:    fmt.Sprintf("Docker %s conectado com sucesso", versionStr),
		Logs:       logs,
	}, nil
}

func parseRemoteInspection(output string, user string, emit func(level, msg string, suggestion string, extra map[string]interface{})) {
	lines := strings.Split(output, "\n")
	var section string
	var uid string
	var groups []string
	var dockerBin string
	var socketsFound []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "---") && strings.HasSuffix(trimmed, "---") {
			section = trimmed
			continue
		}
		if section == "" && uid == "" && trimmed != "" {
			uid = trimmed
			continue
		}
		switch section {
		case "---GROUPS---":
			if trimmed != "" {
				groups = append(groups, strings.Fields(trimmed)...)
			}
		case "---DOCKER_BIN---":
			if trimmed != "" && trimmed != "NOT_FOUND" {
				dockerBin = trimmed
			}
		case "---SOCKETS---":
			if trimmed != "" && trimmed != "NONE" {
				socketsFound = append(socketsFound, trimmed)
			}
		}
	}

	if uid != "" {
		isRoot := uid == "0"
		inDockerGroup := false
		for _, g := range groups {
			if g == "docker" {
				inDockerGroup = true
				break
			}
		}

		if isRoot {
			emit("info", "Usuário remoto possui privilégios de root (UID 0).", "", nil)
		} else if inDockerGroup {
			emit("info", fmt.Sprintf("Usuário '%s' (UID %s) pertence ao grupo 'docker'.", user, uid), "", nil)
		} else {
			emit("warning", fmt.Sprintf("Atenção: O usuário '%s' NÃO pertence ao grupo 'docker' no host remoto.", user),
				fmt.Sprintf("Para permitir acesso sem sudo, execute no host remoto: sudo usermod -aG docker %s && sudo systemctl restart docker", user),
				map[string]interface{}{"groups": groups, "uid": uid},
			)
		}
	}

	if dockerBin != "" {
		emit("info", fmt.Sprintf("Binário do Docker encontrado no host remoto: %s", dockerBin), "", nil)
	}

	if len(socketsFound) > 0 {
		emit("info", fmt.Sprintf("Sockets detectados no host remoto: %s", strings.Join(socketsFound, "; ")), "", nil)
	}
}

// DialDirectSSHClient creates an authenticated *ssh.Client using the provided configuration
func DialDirectSSHClient(ctx context.Context, parsed *ParsedSSHHost, cfg SSHConfig) (*ssh.Client, error) {
	var authMethods []ssh.AuthMethod

	password := cfg.Password
	if password != "" {
		authMethods = append(authMethods,
			ssh.Password(password),
			ssh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = password
				}
				return answers, nil
			}),
		)
	}

	if strings.TrimSpace(cfg.PrivateKey) != "" {
		keyBytes := []byte(cfg.PrivateKey)
		var signer ssh.Signer
		var err error
		if cfg.KeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyBytes, []byte(cfg.KeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(keyBytes)
		}
		if err == nil {
			authMethods = append(authMethods, ssh.PublicKeys(signer))
		}
	}

	var hostKeyCallback ssh.HostKeyCallback
	expectedFP := strings.TrimSpace(cfg.ExpectedHostKeyFingerprint)
	if expectedFP != "" {
		hostKeyCallback = func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			actualFP := ssh.FingerprintSHA256(key)
			cleanExpected := strings.TrimPrefix(expectedFP, "SHA256:")
			cleanActual := strings.TrimPrefix(actualFP, "SHA256:")
			if !strings.EqualFold(cleanExpected, cleanActual) {
				return fmt.Errorf("ssh host key verification failed: expected %s, got %s", expectedFP, actualFP)
			}
			return nil
		}
	} else {
		hostKeyCallback = ssh.InsecureIgnoreHostKey()
	}

	clientConfig := &ssh.ClientConfig{
		User:            parsed.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         8 * time.Second,
	}

	targetAddr := net.JoinHostPort(parsed.Host, parsed.Port)
	dialer := net.Dialer{Timeout: 6 * time.Second}
	tcpConn, err := dialer.DialContext(ctx, "tcp", targetAddr)
	if err != nil {
		return nil, fmt.Errorf("tcp dial failed: %w", err)
	}

	connDeadline := time.Now().Add(8 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(connDeadline) {
		connDeadline = d
	}
	_ = tcpConn.SetDeadline(connDeadline)

	c, chans, reqs, err := ssh.NewClientConn(tcpConn, targetAddr, clientConfig)
	if err != nil {
		_ = tcpConn.Close()
		return nil, err
	}
	_ = tcpConn.SetDeadline(time.Time{})

	return ssh.NewClient(c, chans, reqs), nil
}

func runLocalSocketDiagnostics(
	ctx context.Context,
	hostURL string,
	emit func(level, msg string, suggestion string, extra map[string]interface{}),
	logs []ServerDiagnosticLog,
) (*ServerDiagnosticResult, error) {
	socketPath := strings.TrimPrefix(hostURL, "unix://")
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}

	emit("info", fmt.Sprintf("Verificando arquivo de socket local em %s...", socketPath), "", nil)
	if stat, err := os.Stat(socketPath); err != nil {
		emit("error", fmt.Sprintf("Arquivo de socket local não encontrado: %v", err), "Verifique se o daemon Docker local está rodando (sudo systemctl status docker).", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: err.Error(), Logs: logs}, err
	} else if stat.Mode()&os.ModeSocket == 0 {
		errDesc := fmt.Sprintf("%s existe mas não é um socket Unix", socketPath)
		emit("error", errDesc, "", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: errDesc, Logs: logs}, errors.New(errDesc)
	}

	emit("success", "Arquivo de socket local existe.", "", nil)

	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	httpClient := &http.Client{Transport: tr, Timeout: 5 * time.Second}

	resp, err := httpClient.Get("http://localhost/_ping")
	if err != nil {
		emit("error", fmt.Sprintf("Falha ao comunicar com o socket local: %v", err), "Verifique as permissões de acesso ao /var/run/docker.sock.", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: err.Error(), Logs: logs}, err
	}
	_ = resp.Body.Close()

	emit("success", "Daemon Docker local respondeu ao comando Ping com sucesso!", "", nil)
	return &ServerDiagnosticResult{
		Status:  "CONNECTED",
		Version: "local",
		Message: "Daemon Docker local conectado com sucesso",
		Logs:    logs,
	}, nil
}

func runTCPDiagnostics(
	ctx context.Context,
	hostURL string,
	req models.TestServerRequest,
	emit func(level, msg string, suggestion string, extra map[string]interface{}),
	logs []ServerDiagnosticLog,
) (*ServerDiagnosticResult, error) {
	u, err := url.Parse(hostURL)
	if err != nil {
		emit("error", fmt.Sprintf("URL inválida: %v", err), "", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: err.Error(), Logs: logs}, err
	}

	hostPort := u.Host
	hostname := u.Hostname()
	if err := ValidateHostSafety(hostname); err != nil {
		emit("error", fmt.Sprintf("Alvo inválido ou proibido (SSRF Protection): %v", err), "Endereços de link-local e metadados de nuvem são bloqueados por segurança.", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: err.Error(), Logs: logs}, err
	}

	if !strings.Contains(hostPort, ":") {
		if req.TLSVerify {
			hostPort = net.JoinHostPort(hostPort, "2376")
		} else {
			hostPort = net.JoinHostPort(hostPort, "2375")
		}
	}

	emit("info", fmt.Sprintf("Testando alcance de rede TCP para %s...", hostPort), "", nil)
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", hostPort)
	if err != nil {
		emit("error", fmt.Sprintf("Falha ao conectar via TCP a %s: %v", hostPort, err), "Verifique se a porta 2375/2376 está aberta e acessível.", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: err.Error(), Logs: logs}, err
	}
	_ = conn.Close()
	emit("success", fmt.Sprintf("Porta TCP %s aberta e acessível.", hostPort), "", nil)

	var tr *http.Transport
	if req.TLSVerify {
		emit("info", "Configurando certificados TLS / mTLS...", "", nil)
		tlsConfig := &tls.Config{InsecureSkipVerify: false}
		if req.TLSCACert != "" {
			caPool := x509.NewCertPool()
			caPool.AppendCertsFromPEM([]byte(req.TLSCACert))
			tlsConfig.RootCAs = caPool
		} else {
			tlsConfig.InsecureSkipVerify = true
		}
		if req.TLSCert != "" && req.TLSKey != "" {
			keyPair, err := tls.X509KeyPair([]byte(req.TLSCert), []byte(req.TLSKey))
			if err == nil {
				tlsConfig.Certificates = []tls.Certificate{keyPair}
			}
		}
		tr = &http.Transport{TLSClientConfig: tlsConfig}
	} else {
		tr = &http.Transport{}
	}

	httpClient := &http.Client{Transport: tr, Timeout: 8 * time.Second}
	scheme := "http"
	if req.TLSVerify {
		scheme = "https"
	}

	pingURL := fmt.Sprintf("%s://%s/_ping", scheme, hostPort)
	emit("info", fmt.Sprintf("Enviando requisição Ping para %s...", pingURL), "", nil)

	pReq, _ := http.NewRequestWithContext(ctx, "GET", pingURL, nil)
	resp, pErr := httpClient.Do(pReq)
	if pErr != nil {
		emit("error", fmt.Sprintf("Falha na resposta do Docker daemon: %v", pErr), "Verifique a configuração de TLS ou o binding do Docker daemon em 0.0.0.0.", nil)
		return &ServerDiagnosticResult{Status: "UNREACHABLE", Message: pErr.Error(), Logs: logs}, pErr
	}
	_ = resp.Body.Close()

	emit("success", "Docker daemon respondeu com sucesso ao comando Ping!", "", nil)
	return &ServerDiagnosticResult{
		Status:  "CONNECTED",
		Version: "remote",
		Message: "Docker daemon remoto conectado com sucesso",
		Logs:    logs,
	}, nil
}
