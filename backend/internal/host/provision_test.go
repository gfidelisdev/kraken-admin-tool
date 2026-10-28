package host

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateProvisionKeyRoundTrip(t *testing.T) {
	privPEM, line, err := generateProvisionKey()
	if err != nil {
		t.Fatal(err)
	}
	derived, signer, err := authorizedKeyFromPrivate(privPEM)
	if err != nil || signer == nil {
		t.Fatalf("parse generated key: %v", err)
	}
	if derived != line {
		t.Errorf("authorized line mismatch:\n%s\n%s", derived, line)
	}
	if !strings.HasPrefix(line, "ssh-ed25519 ") || !strings.HasSuffix(line, provisionKeyComment) {
		t.Errorf("unexpected authorized line: %s", line)
	}
}

func TestProvisionKeyAccessValidation(t *testing.T) {
	res := ProvisionKeyAccess(context.Background(), ProvisionKeyRequest{ServerHost: "h", SSHUser: "x; rm -rf /", SSHPassword: "p"})
	if res.Success || res.Code != "invalid_request" {
		t.Fatalf("expected invalid_request, got %+v", res)
	}
	res = ProvisionKeyAccess(context.Background(), ProvisionKeyRequest{})
	if res.Success || res.Code != "invalid_request" {
		t.Fatalf("expected invalid_request, got %+v", res)
	}
}

// startFakeSSHServer runs an in-process SSH server that accepts a password and any key listed in
// $HOME/.ssh/authorized_keys, executing exec requests with the local shell using the given HOME.
func startFakeSSHServer(t *testing.T, home, password string) (host, port string) {
	t.Helper()
	_, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hostSigner, _ := ssh.NewSignerFromKey(hostPriv)

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if string(pw) == password {
				return nil, nil
			}
			return nil, os.ErrPermission
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			data, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
			if err != nil {
				return nil, os.ErrPermission
			}
			for _, l := range strings.Split(string(data), "\n") {
				if l == "" {
					continue
				}
				k, _, _, _, err := ssh.ParseAuthorizedKey([]byte(l))
				if err == nil && string(k.Marshal()) == string(key.Marshal()) {
					return nil, nil
				}
			}
			return nil, os.ErrPermission
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				sc, chans, reqs, err := ssh.NewServerConn(conn, cfg)
				if err != nil {
					return
				}
				defer sc.Close()
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					if nc.ChannelType() != "session" {
						_ = nc.Reject(ssh.UnknownChannelType, "no")
						continue
					}
					ch, creqs, _ := nc.Accept()
					go func() {
						for rq := range creqs {
							if rq.Type != "exec" {
								_ = rq.Reply(false, nil)
								continue
							}
							cmdStr := string(rq.Payload[4:])
							_ = rq.Reply(true, nil)
							c := exec.Command("sh", "-c", cmdStr)
							c.Env = append(os.Environ(), "HOME="+home)
							c.Stdout, c.Stderr = ch, ch.Stderr()
							status := byte(0)
							if err := c.Run(); err != nil {
								status = 1
							}
							_, _ = ch.SendRequest("exit-status", false, []byte{0, 0, 0, status})
							_ = ch.Close()
							return
						}
					}()
				}
			}()
		}
	}()
	h, p, _ := net.SplitHostPort(ln.Addr().String())
	return h, p
}

func TestProvisionKeyAccessAgainstFakeServer(t *testing.T) {
	home := t.TempDir()
	h, p := startFakeSSHServer(t, home, "secret")

	res := ProvisionKeyAccess(context.Background(), ProvisionKeyRequest{
		ServerHost: h, ServerPort: p, SSHUser: "tester", SSHPassword: "secret",
	})
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	if res.PrivateKey == "" || res.Fingerprint == "" {
		t.Errorf("missing key/fingerprint: %+v", res)
	}
	data, err := os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if err != nil || !strings.Contains(string(data), res.PublicKey) {
		t.Fatalf("public key not installed: %v %q", err, data)
	}
	// Idempotent when reusing the same key
	res2 := ProvisionKeyAccess(context.Background(), ProvisionKeyRequest{
		ServerHost: h, ServerPort: p, SSHUser: "tester", SSHPassword: "secret", PrivateKey: res.PrivateKey,
	})
	if !res2.Success {
		t.Fatalf("second run failed: %+v", res2)
	}
	data, _ = os.ReadFile(filepath.Join(home, ".ssh", "authorized_keys"))
	if strings.Count(string(data), res.PublicKey) != 1 {
		t.Errorf("key duplicated in authorized_keys: %q", data)
	}
}

func TestProvisionKeyAccessWrongPassword(t *testing.T) {
	home := t.TempDir()
	h, p := startFakeSSHServer(t, home, "secret")
	res := ProvisionKeyAccess(context.Background(), ProvisionKeyRequest{
		ServerHost: h, ServerPort: p, SSHUser: "tester", SSHPassword: "nope",
	})
	if res.Success || res.Code != "auth_failed" {
		t.Fatalf("expected auth_failed, got %+v", res)
	}
	if res.PrivateKey != "" {
		t.Error("private key must not be returned on auth failure")
	}
}
