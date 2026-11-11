package plugins_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/models"
	"github.com/kraken/admin-tool/backend/internal/plugins"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupPluginTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := t.TempDir() + "/test_plugins.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.InstalledPlugin{},
		&models.PluginKVStore{},
		&models.PluginTrustedKey{},
		&models.DockerServer{},
	); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return db
}

func TestPluginManager_VaultKVProvider(t *testing.T) {
	db := setupPluginTestDB(t)

	manifest := models.PluginManifest{
		ID:          "com.example.mock-vault",
		Name:        "Mock Key Vault",
		Version:     "1.0.0",
		Permissions: []string{"credentials:resolve", "credentials:manage", "storage:kv"},
		Runtime: models.PluginRuntimeConfig{
			Type: "none",
		},
	}
	manifestBytes, _ := json.Marshal(manifest)
	permsBytes, _ := json.Marshal(manifest.Permissions)

	installed := models.InstalledPlugin{
		PluginID:           manifest.ID,
		Name:               manifest.Name,
		Version:            manifest.Version,
		Status:             "enabled",
		GrantedPermissions: string(permsBytes),
		ManifestJSON:       string(manifestBytes),
	}
	db.Create(&installed)

	mgr, err := plugins.NewManager(context.Background(), db, nil, t.TempDir())
	if err != nil {
		t.Fatalf("failed to create plugin manager: %v", err)
	}

	ctx := context.Background()

	// 1. Test PutSecret
	putRes, err := mgr.PutSecret(ctx, models.PutVaultSecretRequest{
		CredentialPluginID: manifest.ID,
		Name:               "prod-web-key",
		Value:              "-----BEGIN OPENSSH PRIVATE KEY-----\nVAULTSECRETKEY\n-----END OPENSSH PRIVATE KEY-----",
		ContentType:        "ssh-private-key",
	})
	if err != nil {
		t.Fatalf("PutSecret failed: %v", err)
	}
	if putRes.Name != "prod-web-key" {
		t.Errorf("expected secret name 'prod-web-key', got '%s'", putRes.Name)
	}

	// 2. Test ListSecrets
	listRes, err := mgr.ListSecrets(ctx, models.ListVaultSecretsRequest{
		CredentialPluginID: manifest.ID,
	})
	if err != nil {
		t.Fatalf("ListSecrets failed: %v", err)
	}
	if len(listRes.Secrets) != 1 || listRes.Secrets[0].Name != "prod-web-key" {
		t.Fatalf("expected 1 secret named 'prod-web-key', got: %+v", listRes.Secrets)
	}

	// 3. Test ResolveCredentials
	resolved, err := mgr.ResolveCredentials(ctx, models.ResolveSecretRequest{
		CredentialPluginID: manifest.ID,
		SecretReference:    "prod-web-key",
		Purpose:            "ssh_connect",
	})
	if err != nil {
		t.Fatalf("ResolveCredentials failed: %v", err)
	}
	if resolved.SSHAuthMethod != "key" {
		t.Errorf("expected auth method 'key', got '%s'", resolved.SSHAuthMethod)
	}
	if resolved.SSHPrivateKey != "-----BEGIN OPENSSH PRIVATE KEY-----\nVAULTSECRETKEY\n-----END OPENSSH PRIVATE KEY-----" {
		t.Errorf("unexpected resolved private key: %s", resolved.SSHPrivateKey)
	}

	// 4. Test TestVaultConnection
	testConn, err := mgr.TestVaultConnection(ctx, models.TestVaultConnectionRequest{
		CredentialPluginID: manifest.ID,
	})
	if err != nil {
		t.Fatalf("TestVaultConnection failed: %v", err)
	}
	if !testConn.Success {
		t.Errorf("expected test connection to succeed")
	}
}

func TestPluginManager_VaultPermissionsEnforced(t *testing.T) {
	db := setupPluginTestDB(t)

	// Plugin WITHOUT credentials:resolve or credentials:manage permissions
	manifest := models.PluginManifest{
		ID:          "com.example.unauthorized-vault",
		Name:        "Unauthorized Vault",
		Version:     "1.0.0",
		Permissions: []string{"storage:kv"},
		Runtime: models.PluginRuntimeConfig{
			Type: "none",
		},
	}
	manifestBytes, _ := json.Marshal(manifest)
	permsBytes, _ := json.Marshal(manifest.Permissions)

	installed := models.InstalledPlugin{
		PluginID:           manifest.ID,
		Name:               manifest.Name,
		Version:            manifest.Version,
		Status:             "enabled",
		GrantedPermissions: string(permsBytes),
		ManifestJSON:       string(manifestBytes),
	}
	db.Create(&installed)

	mgr, err := plugins.NewManager(context.Background(), db, nil, t.TempDir())
	if err != nil {
		t.Fatalf("failed to create plugin manager: %v", err)
	}

	ctx := context.Background()

	// ResolveCredentials should be rejected due to lack of 'credentials:resolve'
	_, err = mgr.ResolveCredentials(ctx, models.ResolveSecretRequest{
		CredentialPluginID: manifest.ID,
		SecretReference:    "some-secret",
	})
	if err == nil {
		t.Fatalf("expected error due to missing credentials:resolve permission, got nil")
	}

	// PutSecret should be rejected due to lack of 'credentials:manage'
	_, err = mgr.PutSecret(ctx, models.PutVaultSecretRequest{
		CredentialPluginID: manifest.ID,
		Name:               "new-secret",
		Value:              "secret123",
	})
	if err == nil {
		t.Fatalf("expected error due to missing credentials:manage permission, got nil")
	}
}

func TestPluginManager_ServerBindingsSyncToDockerServers(t *testing.T) {
	db := setupPluginTestDB(t)
	_ = db.AutoMigrate(&models.DockerServer{})

	srv := models.DockerServer{
		Name:             "srv-maquinasvirtuais-p04poa-dev01",
		HostURL:          "ssh://root@10.4.83.8:22",
		SSHUser:          "root",
		SSHAuthMethod:    "password",
		CredentialSource: "local",
	}
	if err := db.Create(&srv).Error; err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	manifest := models.PluginManifest{
		ID:          "com.kraken.azure-keyvault",
		Name:        "Azure Key Vault Integration",
		Version:     "1.0.0",
		Permissions: []string{"storage:kv", "credentials:resolve", "credentials:manage"},
		Runtime: models.PluginRuntimeConfig{
			Type: "none",
		},
	}
	manifestBytes, _ := json.Marshal(manifest)
	permsBytes, _ := json.Marshal(manifest.Permissions)

	installed := models.InstalledPlugin{
		PluginID:           manifest.ID,
		Name:               manifest.Name,
		Version:            manifest.Version,
		Status:             "enabled",
		GrantedPermissions: string(permsBytes),
		ManifestJSON:       string(manifestBytes),
	}
	db.Create(&installed)

	mgr, err := plugins.NewManager(context.Background(), db, nil, t.TempDir())
	if err != nil {
		t.Fatalf("failed to create plugin manager: %v", err)
	}

	cfgJSON := `{"vault_name":"Vault-PRT04-TI","server_bindings":{"10.4.83.8":"maquinasvirtuais-p04poa-dev01"}}`
	_, err = mgr.CallPlugin(context.Background(), manifest.ID, "kv_set", map[string]interface{}{
		"key":   "azure_kv_config",
		"value": cfgJSON,
	}, "admin", "admin")
	if err != nil {
		t.Fatalf("kv_set failed: %v", err)
	}

	var updated models.DockerServer
	if err := db.First(&updated, srv.ID).Error; err != nil {
		t.Fatalf("failed to reload server: %v", err)
	}
	if updated.CredentialSource != "plugin" {
		t.Fatalf("expected credential_source='plugin', got '%s'", updated.CredentialSource)
	}
	if updated.CredentialPluginID != "com.kraken.azure-keyvault" {
		t.Fatalf("expected credential_plugin_id='com.kraken.azure-keyvault', got '%s'", updated.CredentialPluginID)
	}
	if updated.SecretReference != "maquinasvirtuais-p04poa-dev01" {
		t.Fatalf("expected secret_reference='maquinasvirtuais-p04poa-dev01', got '%s'", updated.SecretReference)
	}
}
