package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kraken/admin-tool/backend/internal/audit"
	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/config"
	krakencrypto "github.com/kraken/admin-tool/backend/internal/crypto"
	"github.com/kraken/admin-tool/backend/internal/models"
)

const k8sSecretsName = "kraken-secrets"

func maskSecretHint(val string) string {
	trimmed := strings.TrimSpace(val)
	if trimmed == "" {
		return ""
	}
	if len(trimmed) <= 6 {
		return fmt.Sprintf("•••• (%d chars)", len(trimmed))
	}
	return fmt.Sprintf("••••%s (%d chars)", trimmed[len(trimmed)-4:], len(trimmed))
}

func quotePgIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

func quotePgLiteral(val string) string {
	return `'` + strings.ReplaceAll(val, `'`, `''`) + `'`
}

func (h *Handler) effectivePluginsDir() string {
	h.clientMu.RLock()
	dir := h.pluginsDir
	h.clientMu.RUnlock()
	if strings.TrimSpace(dir) != "" {
		return dir
	}
	if envDir := strings.TrimSpace(os.Getenv("PLUGINS_DIR")); envDir != "" {
		return envDir
	}
	return "./data/plugins"
}

func (h *Handler) buildSystemSecretsResponse(ctx context.Context) *models.SystemSecretsResponse {
	pluginsDir := h.effectivePluginsDir()
	stored, err := config.LoadPersistentSecrets(pluginsDir)
	hasPersisted := err == nil

	resolveSource := func(envKey, persistedVal string, required bool) (bool, string, string) {
		envVal := strings.TrimSpace(os.Getenv(envKey))
		if envVal != "" {
			if persistedVal != "" && envVal == persistedVal {
				if stored.ConfiguredByAdmin {
					return true, "persisted", maskSecretHint(envVal)
				}
				return true, "auto_generated", maskSecretHint(envVal)
			}
			return true, "environment", maskSecretHint(envVal)
		}
		if persistedVal != "" {
			if stored.ConfiguredByAdmin {
				return true, "persisted", maskSecretHint(persistedVal)
			}
			return true, "auto_generated", maskSecretHint(persistedVal)
		}
		return false, "unset", ""
	}

	jwtCfg, jwtSrc, jwtHint := resolveSource("JWT_SECRET", stored.JWTSecret, true)
	encCfg, encSrc, encHint := resolveSource("ENCRYPTION_KEY", stored.EncryptionKey, true)
	prevCfg, prevSrc, prevHint := resolveSource("ENCRYPTION_KEY_PREVIOUS", stored.EncryptionKeyPrevious, false)
	recCfg, recSrc, recHint := resolveSource("ADMIN_RECOVERY_KEY", stored.AdminRecoveryKey, false)
	dbCfg, dbSrc, dbHint := resolveSource("DB_PASSWORD", stored.DBPassword, false)

	items := []models.SecretStatusItem{
		{
			Key:         "JWT_SECRET",
			Label:       "JWT Signing Secret",
			Description: "32+ byte secret used to sign & verify operator HS256 session tokens.",
			Required:    true,
			Configured:  jwtCfg,
			Source:      jwtSrc,
			MaskedHint:  jwtHint,
		},
		{
			Key:         "ENCRYPTION_KEY",
			Label:       "Database Master Encryption Key",
			Description: "32+ byte AES-256-GCM key protecting stored SSH keys, host passwords, TOTP secrets, and TLS keys.",
			Required:    true,
			Configured:  encCfg,
			Source:      encSrc,
			MaskedHint:  encHint,
		},
		{
			Key:         "DB_PASSWORD",
			Label:       "PostgreSQL Database Password",
			Description: "Password for the Kraken PostgreSQL database user (updates live PostgreSQL & Kubernetes Secret).",
			Required:    false,
			Configured:  dbCfg,
			Source:      dbSrc,
			MaskedHint:  dbHint,
		},
		{
			Key:         "ADMIN_RECOVERY_KEY",
			Label:       "Emergency Admin Recovery Key",
			Description: "Optional 32+ byte X-Master-Key enabling emergency HTTP admin password recovery.",
			Required:    false,
			Configured:  recCfg,
			Source:      recSrc,
			MaskedHint:  recHint,
		},
		{
			Key:         "ENCRYPTION_KEY_PREVIOUS",
			Label:       "Previous Encryption Key (Rotation)",
			Description: "Automatically populated when rotating ENCRYPTION_KEY for zero-downtime decryption of older rows.",
			Required:    false,
			Configured:  prevCfg,
			Source:      prevSrc,
			MaskedHint:  prevHint,
		},
	}

	cli, ns, k8sConnected := h.resolveK8sClient()
	k8sSecretPresent := false
	if k8sConnected && cli != nil {
		if _, getErr := cli.CoreV1().Secrets(ns).Get(ctx, k8sSecretsName, metav1.GetOptions{}); getErr == nil {
			k8sSecretPresent = true
		}
	}

	msClientID := strings.TrimSpace(os.Getenv("MICROSOFT_CLIENT_ID"))
	if msClientID == "" {
		msClientID = "04b07795-8ddb-461a-bbee-02f9e1bf7b46"
	}
	msTenantID := strings.TrimSpace(os.Getenv("MICROSOFT_TENANT_ID"))
	if msTenantID == "" {
		msTenantID = "common"
	}

	return &models.SystemSecretsResponse{
		Secrets:           items,
		RuntimeMode:       h.detectRuntimeMode(),
		K8sConnected:      k8sConnected,
		K8sNamespace:      ns,
		K8sSecretPresent:  k8sSecretPresent,
		ConfiguredByAdmin: hasPersisted && stored.ConfiguredByAdmin,
		MicrosoftClientID: msClientID,
		MicrosoftTenantID: msTenantID,
	}
}

// ApplySystemSecrets validates, persists, applies in-process, and syncs system secrets to PostgreSQL and Kubernetes.
func (h *Handler) ApplySystemSecrets(
	ctx context.Context,
	req models.UpdateSystemSecretsRequest,
	actor string,
	userID uint,
) (*models.SystemSecretsResponse, error) {
	h.netMu.Lock()
	defer h.netMu.Unlock()

	pluginsDir := h.effectivePluginsDir()
	stored, _ := config.LoadPersistentSecrets(pluginsDir)

	if stored.JWTSecret == "" {
		stored.JWTSecret = strings.TrimSpace(os.Getenv("JWT_SECRET"))
	}
	if stored.EncryptionKey == "" {
		stored.EncryptionKey = strings.TrimSpace(os.Getenv("ENCRYPTION_KEY"))
	}
	if stored.EncryptionKeyPrevious == "" {
		stored.EncryptionKeyPrevious = strings.TrimSpace(os.Getenv("ENCRYPTION_KEY_PREVIOUS"))
	}
	if stored.AdminRecoveryKey == "" {
		stored.AdminRecoveryKey = strings.TrimSpace(os.Getenv("ADMIN_RECOVERY_KEY"))
	}
	if stored.DBPassword == "" {
		stored.DBPassword = strings.TrimSpace(os.Getenv("DB_PASSWORD"))
	}

	newJWT := strings.TrimSpace(req.JWTSecret)
	if newJWT != "" {
		if config.IsInsecureSecret(newJWT) {
			return nil, errors.New("JWT_SECRET must be at least 32 characters and not a known placeholder")
		}
		stored.JWTSecret = newJWT
		_ = os.Setenv("JWT_SECRET", newJWT)
	}

	newEnc := strings.TrimSpace(req.EncryptionKey)
	if newEnc != "" {
		if config.IsInsecureSecret(newEnc) {
			return nil, errors.New("ENCRYPTION_KEY must be at least 32 characters and not a known placeholder")
		}
		if stored.EncryptionKey != "" && stored.EncryptionKey != newEnc {
			if stored.EncryptionKeyPrevious != "" {
				stored.EncryptionKeyPrevious = stored.EncryptionKey + "," + stored.EncryptionKeyPrevious
			} else {
				stored.EncryptionKeyPrevious = stored.EncryptionKey
			}
			_ = os.Setenv("ENCRYPTION_KEY_PREVIOUS", stored.EncryptionKeyPrevious)
		}
		stored.EncryptionKey = newEnc
		_ = os.Setenv("ENCRYPTION_KEY", newEnc)
		krakencrypto.ReloadKeys()
	}

	newRec := strings.TrimSpace(req.AdminRecoveryKey)
	if newRec != "" {
		if config.IsInsecureSecret(newRec) || newRec == stored.JWTSecret {
			return nil, errors.New("ADMIN_RECOVERY_KEY must be at least 32 characters and distinct from JWT_SECRET")
		}
		stored.AdminRecoveryKey = newRec
		_ = os.Setenv("ADMIN_RECOVERY_KEY", newRec)
	}

	newDBPass := strings.TrimSpace(req.DBPassword)
	if newDBPass != "" {
		if len(newDBPass) < 8 {
			return nil, errors.New("DB_PASSWORD must be at least 8 characters long")
		}
		if h.DBHandle != nil && h.DBHandle.DB != nil && h.DBHandle.DBType == "PostgreSQL" {
			dbUser := strings.TrimSpace(os.Getenv("DB_USER"))
			if dbUser == "" {
				dbUser = "kraken"
			}
			alterSQL := fmt.Sprintf("ALTER USER %s WITH PASSWORD %s", quotePgIdent(dbUser), quotePgLiteral(newDBPass))
			if err := h.DBHandle.DB.Exec(alterSQL).Error; err != nil {
				return nil, fmt.Errorf("failed to update PostgreSQL user password: %w", err)
			}
		}
		stored.DBPassword = newDBPass
		_ = os.Setenv("DB_PASSWORD", newDBPass)
		_ = os.Setenv("POSTGRES_PASSWORD", newDBPass)
	}

	msClient := strings.TrimSpace(req.MicrosoftClientID)
	msTenant := strings.TrimSpace(req.MicrosoftTenantID)
	if msClient != "" {
		_ = os.Setenv("MICROSOFT_CLIENT_ID", msClient)
	}
	if msTenant != "" {
		_ = os.Setenv("MICROSOFT_TENANT_ID", msTenant)
	}

	stored.ConfiguredByAdmin = true
	if err := config.SavePersistentSecrets(pluginsDir, stored); err != nil {
		return nil, fmt.Errorf("failed to persist secrets: %w", err)
	}

	// Update live in-process auth handler callbacks
	h.clientMu.RLock()
	jwtFn := h.onJWTSecretUpdated
	recFn := h.onRecoveryUpdated
	msFn := h.onMSConfigUpdated
	h.clientMu.RUnlock()

	if newJWT != "" && jwtFn != nil {
		jwtFn(newJWT)
	}
	if newRec != "" && recFn != nil {
		recFn(newRec)
	}
	if (msClient != "" || msTenant != "") && msFn != nil {
		msFn(msClient, msTenant)
	}

	// Sync to Kubernetes Secret/kraken-secrets and ConfigMap/kraken-config if connected
	if cli, ns, connected := h.resolveK8sClient(); connected && cli != nil {
		secData := map[string][]byte{
			"JWT_SECRET":              []byte(stored.JWTSecret),
			"ENCRYPTION_KEY":          []byte(stored.EncryptionKey),
			"ENCRYPTION_KEY_PREVIOUS": []byte(stored.EncryptionKeyPrevious),
			"ADMIN_RECOVERY_KEY":      []byte(stored.AdminRecoveryKey),
			"DB_PASSWORD":             []byte(stored.DBPassword),
			"POSTGRES_PASSWORD":       []byte(stored.DBPassword),
		}
		if existingSec, getErr := cli.CoreV1().Secrets(ns).Get(ctx, k8sSecretsName, metav1.GetOptions{}); getErr == nil {
			if existingSec.Data == nil {
				existingSec.Data = map[string][]byte{}
			}
			for k, v := range secData {
				if len(v) > 0 {
					existingSec.Data[k] = v
				}
			}
			if _, updErr := cli.CoreV1().Secrets(ns).Update(ctx, existingSec, metav1.UpdateOptions{}); updErr != nil {
				return nil, fmt.Errorf("failed to update Kubernetes Secret %s/%s: %w", ns, k8sSecretsName, updErr)
			}
		} else if apierrors.IsNotFound(getErr) {
			newSec := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      k8sSecretsName,
					Namespace: ns,
					Labels: map[string]string{
						"app.kubernetes.io/part-of": "kraken-admin-tool",
						"app.kubernetes.io/name":    k8sSecretsName,
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: secData,
			}
			if _, crtErr := cli.CoreV1().Secrets(ns).Create(ctx, newSec, metav1.CreateOptions{}); crtErr != nil {
				return nil, fmt.Errorf("failed to create Kubernetes Secret %s/%s: %w", ns, k8sSecretsName, crtErr)
			}
		}

		if msClient != "" || msTenant != "" {
			if cm, cmErr := cli.CoreV1().ConfigMaps(ns).Get(ctx, k8sConfigMapName, metav1.GetOptions{}); cmErr == nil {
				if cm.Data == nil {
					cm.Data = map[string]string{}
				}
				if msClient != "" {
					cm.Data["MICROSOFT_CLIENT_ID"] = msClient
				}
				if msTenant != "" {
					cm.Data["MICROSOFT_TENANT_ID"] = msTenant
				}
				if _, updErr := cli.CoreV1().ConfigMaps(ns).Update(ctx, cm, metav1.UpdateOptions{}); updErr != nil {
					log.Printf("[SYSTEM-SECRETS] Warning: failed to update ConfigMap %s/%s: %v", ns, k8sConfigMapName, updErr)
				}
			}
		}

		if req.RestartDeployments != nil && *req.RestartDeployments {
			nowStr := time.Now().UTC().Format(time.RFC3339)
			restartDep := func(name string) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if dep, err := cli.AppsV1().Deployments(ns).Get(bgCtx, name, metav1.GetOptions{}); err == nil {
					if dep.Spec.Template.Annotations == nil {
						dep.Spec.Template.Annotations = make(map[string]string)
					}
					dep.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = nowStr
					_, _ = cli.AppsV1().Deployments(ns).Update(bgCtx, dep, metav1.UpdateOptions{})
				}
			}
			if h.backendRestartDelay > 0 {
				go func() {
					time.Sleep(h.backendRestartDelay)
					restartDep(k8sBackendName)
				}()
			} else {
				restartDep(k8sBackendName)
			}
		}
	}

	if h.DBHandle != nil && h.DBHandle.DB != nil {
		audit.Log(h.DBHandle.DB, userID, actor, "SYSTEM_SECRETS_UPDATE", "secrets", "system",
			"Updated system secrets and synchronized runtime configuration", "SUCCESS")
	}

	return h.buildSystemSecretsResponse(ctx), nil
}

// GetSecretsStatusHandler handles GET /api/system/secrets (Admin only)
func (h *Handler) GetSecretsStatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.buildSystemSecretsResponse(r.Context()))
}

// UpdateSecretsHandler handles PUT /api/system/secrets (Admin only)
func (h *Handler) UpdateSecretsHandler(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateSystemSecretsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request payload"})
		return
	}

	actor := "admin"
	var userID uint
	if claims := auth.GetUserFromContext(r.Context()); claims != nil {
		actor = claims.Username
		userID = claims.UserID
	}

	resp, err := h.ApplySystemSecrets(r.Context(), req, actor, userID)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
