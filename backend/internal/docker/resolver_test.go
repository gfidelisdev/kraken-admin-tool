package docker_test

import (
	"context"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// mockCredentialResolver implements docker.CredentialResolver for testing
type mockCredentialResolver struct {
	resolvedCreds *models.ResolvedCredentials
	resolveErr    error
	lastRequest   models.ResolveSecretRequest
	secretsList   []models.VaultSecretItem
}

func (m *mockCredentialResolver) ResolveCredentials(ctx context.Context, req models.ResolveSecretRequest) (*models.ResolvedCredentials, error) {
	m.lastRequest = req
	if m.resolveErr != nil {
		return nil, m.resolveErr
	}
	return m.resolvedCreds, nil
}

func (m *mockCredentialResolver) ListSecrets(ctx context.Context, req models.ListVaultSecretsRequest) (*models.ListVaultSecretsResponse, error) {
	return &models.ListVaultSecretsResponse{Secrets: m.secretsList}, nil
}

func (m *mockCredentialResolver) PutSecret(ctx context.Context, req models.PutVaultSecretRequest) (*models.VaultSecretItem, error) {
	return &models.VaultSecretItem{
		ID:          req.Name,
		Name:        req.Name,
		ContentType: req.ContentType,
		Enabled:     true,
	}, nil
}

func (m *mockCredentialResolver) TestVaultConnection(ctx context.Context, req models.TestVaultConnectionRequest) (*models.TestVaultConnectionResponse, error) {
	return &models.TestVaultConnectionResponse{
		Success: true,
		Message: "Connected to mock vault",
		Vault:   "mock-vault",
	}, nil
}

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&models.DockerServer{}); err != nil {
		t.Fatalf("failed to migrate DockerServer: %v", err)
	}
	return db
}

func TestCredentialResolver_PluginResolution(t *testing.T) {
	db := setupTestDB(t)
	svc := docker.NewDockerService("", db)

	mockResolver := &mockCredentialResolver{
		resolvedCreds: &models.ResolvedCredentials{
			SSHUser:       "azureuser",
			SSHAuthMethod: "key",
			SSHPrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nMOCKKEY\n-----END OPENSSH PRIVATE KEY-----",
		},
	}
	svc.SetCredentialResolver(mockResolver)

	server := models.DockerServer{
		Name:               "Azure VM Host",
		HostURL:            "ssh://azureuser@10.0.0.5:22",
		SSHUser:            "azureuser",
		CredentialSource:   models.CredentialSourcePlugin,
		CredentialPluginID: "com.azure.keyvault",
		SecretReference:    "https://myvault.vault.azure.net/secrets/prod-ssh-key",
		VaultConfig:        `{"tenant_id":"test-tenant","vault_name":"myvault"}`,
	}
	if err := db.Create(&server).Error; err != nil {
		t.Fatalf("failed to insert server: %v", err)
	}

	creds, err := svc.ResolveServerCredentials(context.Background(), &server, "ssh_connect")
	if err != nil {
		t.Fatalf("unexpected error resolving credentials: %v", err)
	}

	if creds.SSHUser != "azureuser" {
		t.Errorf("expected SSHUser 'azureuser', got '%s'", creds.SSHUser)
	}
	if creds.SSHAuthMethod != "key" {
		t.Errorf("expected SSHAuthMethod 'key', got '%s'", creds.SSHAuthMethod)
	}
	if creds.SSHPrivateKey != mockResolver.resolvedCreds.SSHPrivateKey {
		t.Errorf("expected SSHPrivateKey to match mock, got '%s'", creds.SSHPrivateKey)
	}

	// Verify request passed to resolver
	if mockResolver.lastRequest.CredentialPluginID != "com.azure.keyvault" {
		t.Errorf("expected plugin ID 'com.azure.keyvault', got '%s'", mockResolver.lastRequest.CredentialPluginID)
	}
	if mockResolver.lastRequest.SecretReference != "https://myvault.vault.azure.net/secrets/prod-ssh-key" {
		t.Errorf("unexpected secret reference: '%s'", mockResolver.lastRequest.SecretReference)
	}
	if mockResolver.lastRequest.VaultConfig["tenant_id"] != "test-tenant" {
		t.Errorf("expected tenant_id 'test-tenant' in vault config")
	}
}

func TestCredentialResolver_LocalFallback(t *testing.T) {
	db := setupTestDB(t)
	svc := docker.NewDockerService("", db)

	// Server without plugin uses local credentials
	server := models.DockerServer{
		Name:             "Local Server",
		HostURL:          "ssh://root@192.168.1.100:22",
		SSHUser:          "root",
		SSHAuthMethod:    "password",
		CredentialSource: models.CredentialSourceLocal,
	}

	creds, err := svc.ResolveServerCredentials(context.Background(), &server, "ssh_connect")
	if err != nil {
		t.Fatalf("unexpected error on local fallback: %v", err)
	}

	if creds.SSHUser != "root" {
		t.Errorf("expected SSHUser 'root', got '%s'", creds.SSHUser)
	}
	if creds.SSHAuthMethod != "password" {
		t.Errorf("expected SSHAuthMethod 'password', got '%s'", creds.SSHAuthMethod)
	}
}

func TestCredentialResolver_PrepareTestServerRequest(t *testing.T) {
	db := setupTestDB(t)
	svc := docker.NewDockerService("", db)

	mockResolver := &mockCredentialResolver{
		resolvedCreds: &models.ResolvedCredentials{
			SSHUser:       "ubuntu",
			SSHAuthMethod: "password",
			SSHPassword:   "vault-super-secret",
		},
	}
	svc.SetCredentialResolver(mockResolver)

	req := models.TestServerRequest{
		HostURL:            "ssh://ubuntu@10.0.0.10:22",
		CredentialSource:   models.CredentialSourcePlugin,
		CredentialPluginID: "com.hashicorp.vault",
		SecretReference:    "secret/data/hosts/ubuntu-1",
	}

	if err := svc.PrepareTestServerRequest(context.Background(), &req); err != nil {
		t.Fatalf("unexpected error preparing test server request: %v", err)
	}

	if req.SSHPassword != "vault-super-secret" {
		t.Errorf("expected resolved password 'vault-super-secret', got '%s'", req.SSHPassword)
	}
	if req.SSHAuthMethod != "password" {
		t.Errorf("expected SSHAuthMethod 'password', got '%s'", req.SSHAuthMethod)
	}
}
