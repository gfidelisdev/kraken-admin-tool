package docker

import (
	"context"

	"github.com/kraken/admin-tool/backend/internal/models"
)

// CredentialResolver is the core contract for resolving server credentials dynamically.
// This interface allows plugins or external vault providers (e.g. Azure Key Vault,
// HashiCorp Vault, AWS Secrets Manager) to handle credentials without storing
// plaintext or local master-key encrypted secrets in the Kraken database.
type CredentialResolver interface {
	// ResolveCredentials fetches dynamic secrets (SSH private key, password, passphrase, TLS key)
	// from the configured plugin or vault provider just-in-time when connecting.
	ResolveCredentials(ctx context.Context, req models.ResolveSecretRequest) (*models.ResolvedCredentials, error)

	// ListSecrets queries existing secrets in the vault (e.g. to bind to a server in the UI).
	ListSecrets(ctx context.Context, req models.ListVaultSecretsRequest) (*models.ListVaultSecretsResponse, error)

	// PutSecret creates or updates a secret in the vault (e.g. storing a newly generated SSH key).
	PutSecret(ctx context.Context, req models.PutVaultSecretRequest) (*models.VaultSecretItem, error)

	// TestVaultConnection tests connectivity and authentication with the external vault.
	TestVaultConnection(ctx context.Context, req models.TestVaultConnectionRequest) (*models.TestVaultConnectionResponse, error)
}
