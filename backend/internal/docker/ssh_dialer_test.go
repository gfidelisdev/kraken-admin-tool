package docker

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSSH_IsSSHHost(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"ssh://localhost", true},
		{"SSH://root@host:22", true},
		{"  ssh://192.168.1.100:22/var/run/docker.sock  ", true},
		{"ssh://admin:secret@remote.server:2222", true},
		{"tcp://192.168.1.100:2375", false},
		{"unix:///var/run/docker.sock", false},
		{"http://localhost", false},
		{"https://docker.example.com", false},
		{"", false},
		{"   ", false},
		{"ssh-custom://localhost", false},
	}

	for _, tt := range tests {
		got := IsSSHHost(tt.url)
		if got != tt.expected {
			t.Errorf("IsSSHHost(%q) = %v; want %v", tt.url, got, tt.expected)
		}
	}
}

func TestSSH_ParseSSHHost(t *testing.T) {
	t.Run("basic host", func(t *testing.T) {
		parsed, err := ParseSSHHost("ssh://myhost", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.User != "root" {
			t.Errorf("expected User 'root', got %q", parsed.User)
		}
		if parsed.Host != "myhost" {
			t.Errorf("expected Host 'myhost', got %q", parsed.Host)
		}
		if parsed.Port != "22" {
			t.Errorf("expected Port '22', got %q", parsed.Port)
		}
		if parsed.SocketPath != "/var/run/docker.sock" {
			t.Errorf("expected SocketPath '/var/run/docker.sock', got %q", parsed.SocketPath)
		}
	})

	t.Run("user in url", func(t *testing.T) {
		parsed, err := ParseSSHHost("ssh://ubuntu@remote.server:2222", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.User != "ubuntu" {
			t.Errorf("expected User 'ubuntu', got %q", parsed.User)
		}
		if parsed.Host != "remote.server" {
			t.Errorf("expected Host 'remote.server', got %q", parsed.Host)
		}
		if parsed.Port != "2222" {
			t.Errorf("expected Port '2222', got %q", parsed.Port)
		}
		if parsed.SocketPath != "/var/run/docker.sock" {
			t.Errorf("expected SocketPath '/var/run/docker.sock', got %q", parsed.SocketPath)
		}
	})

	t.Run("override user precedence", func(t *testing.T) {
		parsed, err := ParseSSHHost("ssh://user_in_url@myhost:22", "override_user")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.User != "override_user" {
			t.Errorf("expected User 'override_user', got %q", parsed.User)
		}
	})

	t.Run("custom socket path", func(t *testing.T) {
		parsed, err := ParseSSHHost("ssh://root@localhost/run/user/1000/docker.sock", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.SocketPath != "/run/user/1000/docker.sock" {
			t.Errorf("expected custom socket path, got %q", parsed.SocketPath)
		}
	})

	t.Run("root path resets to default socket", func(t *testing.T) {
		parsed, err := ParseSSHHost("ssh://root@localhost/", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.SocketPath != "/var/run/docker.sock" {
			t.Errorf("expected default socket path, got %q", parsed.SocketPath)
		}
	})

	t.Run("ipv6 host", func(t *testing.T) {
		parsed, err := ParseSSHHost("ssh://root@[::1]:2222/custom.sock", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.Host != "::1" {
			t.Errorf("expected Host '::1', got %q", parsed.Host)
		}
		if parsed.Port != "2222" {
			t.Errorf("expected Port '2222', got %q", parsed.Port)
		}
		if parsed.SocketPath != "/custom.sock" {
			t.Errorf("expected custom.sock, got %q", parsed.SocketPath)
		}
	})

	t.Run("without ssh prefix auto-prepended", func(t *testing.T) {
		parsed, err := ParseSSHHost("192.168.1.200:2200", "admin")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if parsed.Host != "192.168.1.200" || parsed.Port != "2200" || parsed.User != "admin" {
			t.Errorf("unexpected parsed host: %+v", parsed)
		}
	})

	t.Run("error cases", func(t *testing.T) {
		errorCases := []string{
			"",
			"   ",
			"ssh://",
			"ssh://:22",
			"tcp://192.168.1.1:2375",
			"http://example.com",
			"://invalid-url",
		}

		for _, tc := range errorCases {
			_, err := ParseSSHHost(tc, "")
			if err == nil {
				t.Errorf("ParseSSHHost(%q) expected error, got nil", tc)
			}
		}
	})
}

func generateTestED25519Key(t *testing.T) (string, ssh.PublicKey) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate ed25519 key: %v", err)
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("failed to marshal pkcs8 private key: %v", err)
	}

	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: privBytes,
	}

	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("failed to create ssh public key: %v", err)
	}

	return string(pem.EncodeToMemory(pemBlock)), sshPub
}

func generateTestRSAKey(t *testing.T) string {
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate rsa key: %v", err)
	}

	privBytes := x509.MarshalPKCS1PrivateKey(rsaPriv)
	pemBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privBytes,
	}

	return string(pem.EncodeToMemory(pemBlock))
}

func TestSSH_BuildSSHDialContext_KeyValidation(t *testing.T) {
	t.Run("valid ed25519 key", func(t *testing.T) {
		edKeyPEM, _ := generateTestED25519Key(t)
		cfg := SSHConfig{
			HostURL:    "ssh://127.0.0.1:22",
			User:       "testuser",
			PrivateKey: edKeyPEM,
		}
		dialer, err := BuildSSHDialContext(cfg)
		if err != nil {
			t.Fatalf("BuildSSHDialContext failed: %v", err)
		}
		if dialer == nil {
			t.Fatal("expected non-nil dialer function")
		}
	})

	t.Run("valid rsa key", func(t *testing.T) {
		rsaKeyPEM := generateTestRSAKey(t)
		cfg := SSHConfig{
			HostURL:    "ssh://127.0.0.1:22",
			User:       "testuser",
			PrivateKey: rsaKeyPEM,
		}
		dialer, err := BuildSSHDialContext(cfg)
		if err != nil {
			t.Fatalf("BuildSSHDialContext failed: %v", err)
		}
		if dialer == nil {
			t.Fatal("expected non-nil dialer function")
		}
	})

	t.Run("invalid private key returns error immediately", func(t *testing.T) {
		cfg := SSHConfig{
			HostURL:    "ssh://127.0.0.1:22",
			User:       "testuser",
			PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\ninvalid_content\n-----END OPENSSH PRIVATE KEY-----",
		}
		_, err := BuildSSHDialContext(cfg)
		if err == nil {
			t.Fatal("expected error for invalid private key, got nil")
		}
		if !strings.Contains(err.Error(), "failed to parse ssh private key") {
			t.Errorf("expected parse error message, got: %v", err)
		}
	})

	t.Run("password extracted from URL", func(t *testing.T) {
		cfg := SSHConfig{
			HostURL: "ssh://alice:embeddedSecret@127.0.0.1:2222",
		}
		dialer, err := BuildSSHDialContext(cfg)
		if err != nil {
			t.Fatalf("BuildSSHDialContext failed: %v", err)
		}
		if dialer == nil {
			t.Fatal("expected non-nil dialer function")
		}
	})
}

type testSSHServerOpts struct {
	user            string
	password        string
	clientPubKey    ssh.PublicKey
	allowUnixSocket bool
	allowStdio      bool
}

func startTestSSHServer(t *testing.T, opts testSSHServerOpts) (string, string, func()) {
	t.Helper()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate host key: %v", err)
	}

	hostSigner, err := ssh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatalf("failed to create host signer: %v", err)
	}

	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if opts.user != "" && c.User() != opts.user {
				return nil, fmt.Errorf("user mismatch: %s != %s", c.User(), opts.user)
			}
			if opts.password != "" && string(pass) == opts.password {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected")
		},
		KeyboardInteractiveCallback: func(c ssh.ConnMetadata, client ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			if opts.user != "" && c.User() != opts.user {
				return nil, fmt.Errorf("user mismatch")
			}
			answers, err := client(c.User(), "", []string{"Password: "}, []bool{false})
			if err != nil {
				return nil, err
			}
			if len(answers) > 0 && answers[0] == opts.password {
				return nil, nil
			}
			return nil, fmt.Errorf("keyboard-interactive rejected")
		},
	}

	if opts.clientPubKey != nil {
		config.PublicKeyCallback = func(c ssh.ConnMetadata, pubKey ssh.PublicKey) (*ssh.Permissions, error) {
			if opts.user != "" && c.User() != opts.user {
				return nil, fmt.Errorf("user mismatch")
			}
			if bytes.Equal(pubKey.Marshal(), opts.clientPubKey.Marshal()) {
				return nil, nil
			}
			return nil, fmt.Errorf("public key rejected")
		}
	}

	config.AddHostKey(hostSigner)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on tcp: %v", err)
	}

	tcpAddr := listener.Addr().(*net.TCPAddr)
	host := tcpAddr.IP.String()
	port := strconv.Itoa(tcpAddr.Port)

	stopCh := make(chan struct{})

	go func() {
		for {
			tcpConn, err := listener.Accept()
			if err != nil {
				select {
				case <-stopCh:
					return
				default:
					continue
				}
			}

			go func(c net.Conn) {
				defer c.Close()
				sshConn, chans, reqs, err := ssh.NewServerConn(c, config)
				if err != nil {
					return
				}
				defer sshConn.Close()

				go ssh.DiscardRequests(reqs)

				for newCh := range chans {
					chType := newCh.ChannelType()
					if chType == "direct-streamlocal@openssh.com" {
						if !opts.allowUnixSocket {
							_ = newCh.Reject(ssh.Prohibited, "unix socket forwarding not allowed")
							continue
						}
						channel, requests, err := newCh.Accept()
						if err != nil {
							continue
						}
						go ssh.DiscardRequests(requests)
						go handleEcho(channel)
					} else if chType == "session" {
						if !opts.allowStdio {
							_ = newCh.Reject(ssh.Prohibited, "sessions not allowed")
							continue
						}
						channel, requests, err := newCh.Accept()
						if err != nil {
							continue
						}
						go func(ch ssh.Channel, inReqs <-chan *ssh.Request) {
							defer ch.Close()
							for req := range inReqs {
								if req.Type == "exec" {
									var execPayload struct {
										Command string
									}
									_ = ssh.Unmarshal(req.Payload, &execPayload)
									if strings.Contains(execPayload.Command, "docker system dial-stdio") {
										_ = req.Reply(true, nil)
										handleEcho(ch)
										return
									}
									_ = req.Reply(false, nil)
								} else {
									_ = req.Reply(false, nil)
								}
							}
						}(channel, requests)
					} else {
						_ = newCh.Reject(ssh.UnknownChannelType, "unsupported channel type")
					}
				}
			}(tcpConn)
		}
	}()

	cleanup := func() {
		close(stopCh)
		_ = listener.Close()
	}

	return host, port, cleanup
}

func handleEcho(rw io.ReadWriteCloser) {
	defer rw.Close()
	buf := make([]byte, 1024)
	for {
		n, err := rw.Read(buf)
		if n > 0 {
			_, _ = rw.Write(append([]byte("ECHO:"), buf[:n]...))
		}
		if err != nil {
			return
		}
	}
}

func TestSSH_Tunneling_DirectUnixSocket(t *testing.T) {
	host, port, cleanup := startTestSSHServer(t, testSSHServerOpts{
		user:            "dockeruser",
		password:        "secretpassword",
		allowUnixSocket: true,
	})
	defer cleanup()

	cfg := SSHConfig{
		HostURL:    fmt.Sprintf("ssh://dockeruser@%s:%s/var/run/docker.sock", host, port),
		AuthMethod: "password",
		Password:   "secretpassword",
	}

	dialer, err := BuildSSHDialContext(cfg)
	if err != nil {
		t.Fatalf("BuildSSHDialContext failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialer(ctx, "tcp", "docker")
	if err != nil {
		t.Fatalf("dialer failed: %v", err)
	}
	defer conn.Close()

	if _, ok := conn.(*sshConnWrapper); !ok {
		t.Errorf("expected *sshConnWrapper, got %T", conn)
	}

	// Test bidirectional communication
	msg := []byte("hello-docker")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("failed to write to conn: %v", err)
	}

	readBuf := make([]byte, 64)
	n, err := conn.Read(readBuf)
	if err != nil {
		t.Fatalf("failed to read from conn: %v", err)
	}

	expected := "ECHO:hello-docker"
	if string(readBuf[:n]) != expected {
		t.Errorf("expected %q, got %q", expected, string(readBuf[:n]))
	}
}

func TestSSH_Tunneling_StdioFallback(t *testing.T) {
	// Disallow unix socket, allow stdio session fallback
	host, port, cleanup := startTestSSHServer(t, testSSHServerOpts{
		user:            "fallbackuser",
		password:        "fallbackpass",
		allowUnixSocket: false,
		allowStdio:      true,
	})
	defer cleanup()

	cfg := SSHConfig{
		HostURL:    fmt.Sprintf("ssh://%s:%s", host, port),
		User:       "fallbackuser",
		AuthMethod: "password",
		Password:   "fallbackpass",
	}

	dialer, err := BuildSSHDialContext(cfg)
	if err != nil {
		t.Fatalf("BuildSSHDialContext failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialer(ctx, "tcp", "docker")
	if err != nil {
		t.Fatalf("dialer failed to fallback: %v", err)
	}
	defer conn.Close()

	if _, ok := conn.(*sshStdioConn); !ok {
		t.Errorf("expected *sshStdioConn, got %T", conn)
	}

	// Verify LocalAddr and RemoteAddr
	if conn.LocalAddr() == nil || conn.RemoteAddr() == nil {
		t.Errorf("expected non-nil addresses on stdio conn")
	}

	// Test bidirectional communication
	msg := []byte("ping-stdio")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("failed to write to stdio conn: %v", err)
	}

	readBuf := make([]byte, 64)
	n, err := conn.Read(readBuf)
	if err != nil {
		t.Fatalf("failed to read from stdio conn: %v", err)
	}

	expected := "ECHO:ping-stdio"
	if string(readBuf[:n]) != expected {
		t.Errorf("expected %q, got %q", expected, string(readBuf[:n]))
	}
}

func TestSSH_Tunneling_PublicKeyAuth(t *testing.T) {
	keyPEM, pubKey := generateTestED25519Key(t)

	host, port, cleanup := startTestSSHServer(t, testSSHServerOpts{
		user:            "keyuser",
		clientPubKey:    pubKey,
		allowUnixSocket: true,
	})
	defer cleanup()

	cfg := SSHConfig{
		HostURL:    fmt.Sprintf("ssh://keyuser@%s:%s", host, port),
		AuthMethod: "key",
		PrivateKey: keyPEM,
	}

	dialer, err := BuildSSHDialContext(cfg)
	if err != nil {
		t.Fatalf("BuildSSHDialContext failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := dialer(ctx, "tcp", "docker")
	if err != nil {
		t.Fatalf("dialer with public key failed: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("key-auth-test")); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}

	if string(buf[:n]) != "ECHO:key-auth-test" {
		t.Errorf("unexpected response: %s", string(buf[:n]))
	}
}

func TestSSH_Tunneling_AuthFailure(t *testing.T) {
	host, port, cleanup := startTestSSHServer(t, testSSHServerOpts{
		user:            "realuser",
		password:        "correctpassword",
		allowUnixSocket: true,
	})
	defer cleanup()

	cfg := SSHConfig{
		HostURL:    fmt.Sprintf("ssh://realuser@%s:%s", host, port),
		AuthMethod: "password",
		Password:   "wrongpassword",
	}

	dialer, err := BuildSSHDialContext(cfg)
	if err != nil {
		t.Fatalf("BuildSSHDialContext failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err = dialer(ctx, "tcp", "docker")
	if err == nil {
		t.Fatal("expected authentication error, got nil")
	}
	if !strings.Contains(err.Error(), "ssh handshake failed") {
		t.Errorf("expected handshake failed error, got: %v", err)
	}
}

func TestSSH_Tunneling_ContextCancelled(t *testing.T) {
	host, port, cleanup := startTestSSHServer(t, testSSHServerOpts{
		user:            "testuser",
		password:        "testpass",
		allowUnixSocket: true,
	})
	defer cleanup()

	cfg := SSHConfig{
		HostURL:  fmt.Sprintf("ssh://testuser@%s:%s", host, port),
		Password: "testpass",
	}

	dialer, err := BuildSSHDialContext(cfg)
	if err != nil {
		t.Fatalf("BuildSSHDialContext failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately before dial

	_, err = dialer(ctx, "tcp", "docker")
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}
