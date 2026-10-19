package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHConfig struct {
	HostURL                    string
	User                       string
	AuthMethod                 string // "password" | "key" | ""
	Password                   string
	PrivateKey                 string
	KeyPassphrase              string
	ExpectedHostKeyFingerprint string
}

type ParsedSSHHost struct {
	User       string
	Host       string
	Port       string
	SocketPath string
}

// IsSSHHost returns true if the given hostURL starts with "ssh://" (case-insensitive).
func IsSSHHost(hostURL string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(hostURL)), "ssh://")
}

// ParseSSHHost parses an SSH host URL of the form ssh://[user@]host[:port][/path/to/docker.sock].
// If overrideUser is provided, it takes precedence over the user in the URL.
// If user is empty in both, it defaults to "root".
// Default port is "22" and default SocketPath is "/var/run/docker.sock".
func ParseSSHHost(hostURL string, overrideUser string) (*ParsedSSHHost, error) {
	trimmed := strings.TrimSpace(hostURL)
	if trimmed == "" {
		return nil, errors.New("empty ssh host URL")
	}

	if !strings.HasPrefix(strings.ToLower(trimmed), "ssh://") {
		if strings.Contains(trimmed, "://") {
			return nil, fmt.Errorf("invalid scheme for ssh host URL: %s", hostURL)
		}
		trimmed = "ssh://" + trimmed
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ssh URL: %w", err)
	}

	host := u.Hostname()
	if host == "" {
		return nil, errors.New("empty ssh host")
	}

	port := u.Port()
	if port == "" {
		port = "22"
	}

	user := strings.TrimSpace(overrideUser)
	if user == "" && u.User != nil {
		user = u.User.Username()
	}
	if user == "" {
		user = "root"
	}

	socketPath := u.Path
	if socketPath == "" || socketPath == "/" {
		socketPath = "/var/run/docker.sock"
	}

	return &ParsedSSHHost{
		User:       user,
		Host:       host,
		Port:       port,
		SocketPath: socketPath,
	}, nil
}

// BuildSSHDialContext parses the SSHConfig, builds the ssh.AuthMethod list, and returns
// a dial function compatible with net.DialContext (used by http.Transport and Docker client).
func BuildSSHDialContext(cfg SSHConfig) (func(ctx context.Context, network, addr string) (net.Conn, error), error) {
	parsed, err := ParseSSHHost(cfg.HostURL, cfg.User)
	if err != nil {
		return nil, err
	}

	password := cfg.Password
	if password == "" {
		trimmed := strings.TrimSpace(cfg.HostURL)
		if !strings.HasPrefix(strings.ToLower(trimmed), "ssh://") && !strings.Contains(trimmed, "://") {
			trimmed = "ssh://" + trimmed
		}
		if u, err := url.Parse(trimmed); err == nil && u.User != nil {
			if pass, ok := u.User.Password(); ok {
				password = pass
			}
		}
	}

	var authMethods []ssh.AuthMethod

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
		if err != nil {
			return nil, fmt.Errorf("failed to parse ssh private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	if len(authMethods) == 0 {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			defaultKeys := []string{
				filepath.Join(home, ".ssh", "id_ed25519"),
				filepath.Join(home, ".ssh", "id_rsa"),
			}
			var signers []ssh.Signer
			for _, keyPath := range defaultKeys {
				if data, err := os.ReadFile(keyPath); err == nil {
					if signer, err := ssh.ParsePrivateKey(data); err == nil {
						signers = append(signers, signer)
					}
				}
			}
			if len(signers) > 0 {
				authMethods = append(authMethods, ssh.PublicKeys(signers...))
			}
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

	sshClientConfig := &ssh.ClientConfig{
		User:            parsed.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         8 * time.Second,
	}

	dialContext := func(ctx context.Context, network, addr string) (net.Conn, error) {
		d := net.Dialer{Timeout: 8 * time.Second}
		tcpConn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(parsed.Host, parsed.Port))
		if err != nil {
			return nil, fmt.Errorf("ssh dial tcp failed: %w", err)
		}

		if deadline, ok := ctx.Deadline(); ok {
			_ = tcpConn.SetDeadline(deadline)
		} else {
			_ = tcpConn.SetDeadline(time.Now().Add(8 * time.Second))
		}

		c, chans, reqs, err := ssh.NewClientConn(tcpConn, net.JoinHostPort(parsed.Host, parsed.Port), sshClientConfig)
		if err != nil {
			_ = tcpConn.Close()
			return nil, fmt.Errorf("ssh handshake failed: %w", err)
		}
		_ = tcpConn.SetDeadline(time.Time{})

		sshClient := ssh.NewClient(c, chans, reqs)

		if ctx.Err() != nil {
			_ = sshClient.Close()
			return nil, ctx.Err()
		}

		// Try direct unix socket dial with timeout
		type dialRes struct {
			conn net.Conn
			err  error
		}
		tryUnixDial := func(sock string) (net.Conn, error) {
			ch := make(chan dialRes, 1)
			go func() {
				c, e := sshClient.Dial("unix", sock)
				ch <- dialRes{conn: c, err: e}
			}()
			select {
			case r := <-ch:
				return r.conn, r.err
			case <-time.After(4 * time.Second):
				return nil, errors.New("timeout dialing unix socket over ssh")
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		unixConn, err := tryUnixDial(parsed.SocketPath)
		if err != nil && parsed.SocketPath == "/var/run/docker.sock" {
			unixConn, err = tryUnixDial("/run/docker.sock")
		}
		if err == nil {
			return &sshConnWrapper{
				Conn:      unixConn,
				sshClient: sshClient,
			}, nil
		}

		// Fallback: spawn a session running "docker system dial-stdio"
		session, sErr := sshClient.NewSession()
		if sErr != nil {
			_ = sshClient.Close()
			return nil, fmt.Errorf("ssh dial unix failed (%v) and ssh session creation failed: %w", err, sErr)
		}

		stdin, inErr := session.StdinPipe()
		if inErr != nil {
			_ = session.Close()
			_ = sshClient.Close()
			return nil, fmt.Errorf("ssh stdin pipe failed: %w", inErr)
		}

		stdout, outErr := session.StdoutPipe()
		if outErr != nil {
			_ = session.Close()
			_ = sshClient.Close()
			return nil, fmt.Errorf("ssh stdout pipe failed: %w", outErr)
		}

		cmd := "PATH=$PATH:/usr/local/bin:/usr/bin:/bin:/snap/bin docker system dial-stdio"
		if cmdErr := session.Start(cmd); cmdErr != nil {
			_ = session.Close()
			_ = sshClient.Close()
			return nil, fmt.Errorf("ssh start dial-stdio failed: %w", cmdErr)
		}

		return &sshStdioConn{
			reader:     stdout,
			writer:     stdin,
			session:    session,
			sshClient:  sshClient,
			localAddr:  sshClient.LocalAddr(),
			remoteAddr: sshClient.RemoteAddr(),
		}, nil
	}

	return dialContext, nil
}

type sshConnWrapper struct {
	net.Conn
	sshClient *ssh.Client
	closeOnce sync.Once
}

func (w *sshConnWrapper) Close() error {
	var err error
	w.closeOnce.Do(func() {
		err1 := w.Conn.Close()
		err2 := w.sshClient.Close()
		if err1 != nil {
			err = err1
		} else {
			err = err2
		}
	})
	return err
}

type sshStdioConn struct {
	reader       io.Reader
	writer       io.WriteCloser
	session      *ssh.Session
	sshClient    *ssh.Client
	closeOnce    sync.Once
	localAddr    net.Addr
	remoteAddr   net.Addr
	readDeadline time.Time
	mu           sync.Mutex
}

func (c *sshStdioConn) Read(b []byte) (int, error) {
	c.mu.Lock()
	deadline := c.readDeadline
	c.mu.Unlock()

	if !deadline.IsZero() && time.Now().After(deadline) {
		return 0, os.ErrDeadlineExceeded
	}

	type readRes struct {
		n   int
		err error
	}
	ch := make(chan readRes, 1)
	go func() {
		n, err := c.reader.Read(b)
		ch <- readRes{n: n, err: err}
	}()

	if deadline.IsZero() {
		res := <-ch
		return res.n, res.err
	}

	select {
	case res := <-ch:
		return res.n, res.err
	case <-time.After(time.Until(deadline)):
		return 0, os.ErrDeadlineExceeded
	}
}

func (c *sshStdioConn) Write(b []byte) (int, error) {
	return c.writer.Write(b)
}

func (c *sshStdioConn) Close() error {
	var err error
	c.closeOnce.Do(func() {
		var errs []error
		if c.writer != nil {
			if e := c.writer.Close(); e != nil {
				errs = append(errs, e)
			}
		}
		if c.session != nil {
			if e := c.session.Close(); e != nil && !errors.Is(e, io.EOF) {
				errs = append(errs, e)
			}
		}
		if c.sshClient != nil {
			if e := c.sshClient.Close(); e != nil {
				errs = append(errs, e)
			}
		}
		if len(errs) > 0 {
			err = errs[0]
		}
	})
	return err
}

func (c *sshStdioConn) LocalAddr() net.Addr {
	if c.localAddr != nil {
		return c.localAddr
	}
	return &sshAddr{network: "ssh", str: "local"}
}

func (c *sshStdioConn) RemoteAddr() net.Addr {
	if c.remoteAddr != nil {
		return c.remoteAddr
	}
	return &sshAddr{network: "ssh", str: "remote"}
}

func (c *sshStdioConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	c.mu.Unlock()
	return nil
}

func (c *sshStdioConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	c.mu.Unlock()
	return nil
}

func (c *sshStdioConn) SetWriteDeadline(t time.Time) error { return nil }

type sshAddr struct {
	network string
	str     string
}

func (a *sshAddr) Network() string { return a.network }
func (a *sshAddr) String() string  { return a.str }

var _ net.Conn = (*sshConnWrapper)(nil)
var _ net.Conn = (*sshStdioConn)(nil)
