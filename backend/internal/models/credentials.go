package models

// Credential source types for DockerServer
const (
	CredentialSourceLocal  = "local"  // Local encrypted storage via AES-256-GCM
	CredentialSourcePlugin = "plugin" // Dynamically resolved via Key Vault / Plugin
	CredentialSourceVault  = "vault"  // Alias for plugin / vault provider
)

// Standard Plugin Actions for Vault & Credential Resolution
const (
	PluginActionVaultResolveSecret  = "vault.resolve_secret"
	PluginActionVaultListSecrets    = "vault.list_secrets"
	PluginActionVaultPutSecret      = "vault.put_secret"
	PluginActionVaultTestConnection = "vault.test_connection"
)

// ResolveSecretRequest is sent to the CredentialResolver / Plugin
type ResolveSecretRequest struct {
	ServerID           uint                   `json:"server_id,omitempty"`
	ServerName         string                 `json:"server_name,omitempty"`
	HostURL            string                 `json:"host_url,omitempty"`
	SSHUser            string                 `json:"ssh_user,omitempty"`
	CredentialPluginID string                 `json:"credential_plugin_id"`
	SecretReference    string                 `json:"secret_reference"`
	SecretVersion      string                 `json:"secret_version,omitempty"`
	VaultConfig        map[string]interface{} `json:"vault_config,omitempty"`
	Purpose            string                 `json:"purpose"` // "ssh_connect" | "docker_connect" | "sudo_exec"
}

// ResolvedCredentials is the response payload returned by the CredentialResolver / Plugin
type ResolvedCredentials struct {
	SSHUser          string `json:"ssh_user,omitempty"`
	SSHAuthMethod    string `json:"ssh_auth_method,omitempty"` // "key" | "password"
	SSHPassword      string `json:"ssh_password,omitempty"`
	SSHPrivateKey    string `json:"ssh_private_key,omitempty"`
	SSHKeyPassphrase string `json:"ssh_key_passphrase,omitempty"`
	TLSKey           string `json:"tls_key,omitempty"`
	TLSCert          string `json:"tls_cert,omitempty"`
	TLSCACert        string `json:"tls_ca_cert,omitempty"`
	SudoPassword     string `json:"sudo_password,omitempty"`
}

// VaultSecretItem represents a secret stored in an external Key Vault (e.g. Azure Key Vault)
type VaultSecretItem struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version,omitempty"`
	ContentType string            `json:"content_type,omitempty"` // e.g. "ssh-private-key", "password", "generic"
	VaultURI    string            `json:"vault_uri,omitempty"`
	Enabled     bool              `json:"enabled"`
	CreatedAt   string            `json:"created_at,omitempty"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
	ExpiresAt   string            `json:"expires_at,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// ListVaultSecretsRequest is sent to query secrets in the vault
type ListVaultSecretsRequest struct {
	CredentialPluginID string                 `json:"credential_plugin_id"`
	VaultConfig        map[string]interface{} `json:"vault_config,omitempty"`
	Filter             string                 `json:"filter,omitempty"`
	ContentType        string                 `json:"content_type,omitempty"`
}

// ListVaultSecretsResponse is the list of secrets found in the vault
type ListVaultSecretsResponse struct {
	Secrets []VaultSecretItem `json:"secrets"`
}

// PutVaultSecretRequest creates or updates a secret in the vault
type PutVaultSecretRequest struct {
	CredentialPluginID string                 `json:"credential_plugin_id"`
	Name               string                 `json:"name"`
	Value              string                 `json:"value"`
	ContentType        string                 `json:"content_type,omitempty"` // "ssh-private-key" | "password"
	VaultConfig        map[string]interface{} `json:"vault_config,omitempty"`
	Tags               map[string]string      `json:"tags,omitempty"`
}

// TestVaultConnectionRequest tests connectivity/authentication with the vault
type TestVaultConnectionRequest struct {
	CredentialPluginID string                 `json:"credential_plugin_id"`
	VaultConfig        map[string]interface{} `json:"vault_config,omitempty"`
}

// TestVaultConnectionResponse indicates if test succeeded
type TestVaultConnectionResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Vault   string `json:"vault,omitempty"`
	User    string `json:"user,omitempty"`
}
