package docker

import (
	"context"
	"strings"
	"testing"

	"github.com/docker/docker/client"
	krakencrypto "github.com/kraken/admin-tool/backend/internal/crypto"
	"github.com/kraken/admin-tool/backend/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDockerServiceOfflineGraceful(t *testing.T) {
	// Point to non-existent socket / host to verify resilience
	svc := NewDockerService("tcp://127.0.0.1:49999", nil)
	if svc.IsConnected() {
		t.Error("expected docker service to be offline for non-existent daemon host")
	}

	ctx := context.Background()
	containers, err := svc.ListContainers(ctx, false)
	if err == nil {
		t.Error("expected error when listing containers on offline service")
	}
	if len(containers) != 0 {
		t.Errorf("expected 0 containers, got %d", len(containers))
	}

	images, err := svc.ListImages(ctx)
	if err == nil {
		t.Error("expected error when listing images on offline service")
	}
	if len(images) != 0 {
		t.Errorf("expected 0 images, got %d", len(images))
	}
}

func TestCreateDockerClientWithSSH(t *testing.T) {
	t.Run("invalid private key returns error", func(t *testing.T) {
		cfg := SSHConfig{
			HostURL:    "ssh://user@localhost:2222",
			PrivateKey: "not-a-valid-pem-key",
		}
		cli, err := createDockerClientWithSSH(cfg.HostURL, false, "", "", "", cfg)
		if err == nil {
			if cli != nil {
				_ = cli.Close()
			}
			t.Fatal("expected error parsing invalid private key, got nil")
		}
	})

	t.Run("ssh with password builds client", func(t *testing.T) {
		cfg := SSHConfig{
			HostURL:    "ssh://user@localhost:2222",
			Password:   "secret",
			AuthMethod: "password",
		}
		cli, err := createDockerClientWithSSH(cfg.HostURL, false, "", "", "", cfg)
		if err != nil {
			t.Fatalf("unexpected error building client: %v", err)
		}
		if cli == nil {
			t.Fatal("expected non-nil client")
		}
		_ = cli.Close()
	})

	t.Run("backward compatible createDockerClient", func(t *testing.T) {
		cli, err := createDockerClient("tcp://127.0.0.1:2375", false, "", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cli == nil {
			t.Fatal("expected non-nil client")
		}
		_ = cli.Close()
	})
}

func TestDockerService_SSHServerCRUD(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite in memory: %v", err)
	}
	if err := db.AutoMigrate(&models.DockerServer{}); err != nil {
		t.Fatalf("failed to migrate models: %v", err)
	}

	svc := &DockerService{
		db:      db,
		clients: make(map[uint]*client.Client),
	}

	ctx := context.Background()

	// Create with auto-detected password auth method
	req := models.CreateServerRequest{
		Name:             "Remote SSH Server",
		HostURL:          "ssh://user@remote-box:2222",
		SSHUser:          "admin",
		SSHPassword:      "mypassword",
		SSHKeyPassphrase: "mypassphrase",
	}

	created, err := svc.CreateServer(ctx, req)
	if err != nil {
		t.Fatalf("CreateServer failed: %v", err)
	}
	if created.SSHUser != "admin" {
		t.Errorf("expected SSHUser 'admin', got %q", created.SSHUser)
	}
	if created.SSHAuthMethod != "password" {
		t.Errorf("expected auto-detected SSHAuthMethod 'password', got %q", created.SSHAuthMethod)
	}
	// Verify sensitive fields are stored encrypted in the model
	if !strings.HasPrefix(created.SSHPassword, "enc:v1:") {
		t.Errorf("expected SSHPassword to be encrypted with prefix 'enc:v1:', got %q", created.SSHPassword)
	}
	decPass, err := krakencrypto.Decrypt(created.SSHPassword)
	if err != nil || decPass != "mypassword" {
		t.Errorf("expected decrypted SSHPassword 'mypassword', got %q (err: %v)", decPass, err)
	}

	if !strings.HasPrefix(created.SSHKeyPassphrase, "enc:v1:") {
		t.Errorf("expected SSHKeyPassphrase to be encrypted with prefix 'enc:v1:', got %q", created.SSHKeyPassphrase)
	}
	decPassphrase, err := krakencrypto.Decrypt(created.SSHKeyPassphrase)
	if err != nil || decPassphrase != "mypassphrase" {
		t.Errorf("expected decrypted SSHKeyPassphrase 'mypassphrase', got %q (err: %v)", decPassphrase, err)
	}

	if created.Status != "UNREACHABLE" {
		t.Errorf("expected status 'UNREACHABLE' since host is unreachable in unit test, got %s", created.Status)
	}

	// Update server to key authentication
	updateReq := models.UpdateServerRequest{
		Name:          "Updated SSH Server",
		SSHUser:       "deploy",
		SSHAuthMethod: "key",
		SSHPrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----",
	}

	updated, err := svc.UpdateServer(ctx, created.ID, updateReq)
	if err != nil {
		t.Fatalf("UpdateServer failed: %v", err)
	}
	if updated.Name != "Updated SSH Server" {
		t.Errorf("expected Name 'Updated SSH Server', got %q", updated.Name)
	}
	if updated.SSHUser != "deploy" {
		t.Errorf("expected SSHUser 'deploy', got %q", updated.SSHUser)
	}
	if updated.SSHAuthMethod != "key" {
		t.Errorf("expected SSHAuthMethod 'key', got %q", updated.SSHAuthMethod)
	}
	decKey, err := krakencrypto.Decrypt(updated.SSHPrivateKey)
	if err != nil || decKey != updateReq.SSHPrivateKey {
		t.Errorf("expected decrypted SSHPrivateKey to match updateReq, got %q (err: %v)", decKey, err)
	}
	// Password should be preserved because updateReq.SSHPassword was empty
	decPreservedPass, err := krakencrypto.Decrypt(updated.SSHPassword)
	if err != nil || decPreservedPass != "mypassword" {
		t.Errorf("expected preserved decrypted SSHPassword 'mypassword', got %q (err: %v)", decPreservedPass, err)
	}
}

func TestDockerService_SSRFProtection(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory db: %v", err)
	}
	_ = db.AutoMigrate(&models.DockerServer{})

	svc := NewDockerService("unix:///var/run/docker.sock", db)
	ctx := context.Background()

	maliciousHosts := []string{
		"ssh://user@169.254.169.254:22",
		"ssh://user@metadata.google.internal:22",
		"tcp://169.254.169.254:2375",
	}

	for _, host := range maliciousHosts {
		_, err := svc.CreateServer(ctx, models.CreateServerRequest{
			Name:    "Malicious Server",
			HostURL: host,
		})
		if err == nil {
			t.Errorf("expected SSRF error for host %s, got nil", host)
		}
	}
}

func TestDockerService_IdentifierValidation(t *testing.T) {
	svc := NewDockerService("unix:///var/run/docker.sock", nil)
	ctx := context.Background()

	invalidIDs := []string{
		"../../bin/sh",
		"container;id",
		"test\nline",
		"",
	}

	for _, id := range invalidIDs {
		if err := svc.StartContainer(ctx, id); err == nil {
			t.Errorf("expected error starting container with invalid id %q", id)
		}
		if err := svc.StopContainer(ctx, id); err == nil {
			t.Errorf("expected error stopping container with invalid id %q", id)
		}
		if err := svc.RestartContainer(ctx, id); err == nil {
			t.Errorf("expected error restarting container with invalid id %q", id)
		}
		if err := svc.RemoveContainer(ctx, id, false); err == nil {
			t.Errorf("expected error removing container with invalid id %q", id)
		}
	}

	invalidImages := []string{
		"image;evil",
		"image name with spaces",
		"image\ninjection",
		"",
	}

	for _, img := range invalidImages {
		if err := svc.PullImageOnServer(ctx, 0, img); err == nil {
			t.Errorf("expected error pulling invalid image %q", img)
		}
	}
}

