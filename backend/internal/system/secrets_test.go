package system

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/config"
	krakencrypto "github.com/kraken/admin-tool/backend/internal/crypto"
	"github.com/kraken/admin-tool/backend/internal/database"
	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestSystemSecrets_GetAndApplyAndRotateEncryptionKey(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:memsecretstest?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("failed to migrate audit_logs: %v", err)
	}

	tmpPluginsDir := t.TempDir()
	mockAPI := newMockSystemK8sServer(t)

	// Start with an initial encryption key and encrypt a sample secret
	initialEncKey := "initial-encryption-key-at-least-32-chars-12345"
	os.Setenv("ENCRYPTION_KEY", initialEncKey)
	os.Setenv("JWT_SECRET", "initial-jwt-secret-at-least-32-chars-67890")
	defer func() {
		os.Unsetenv("ENCRYPTION_KEY")
		os.Unsetenv("ENCRYPTION_KEY_PREVIOUS")
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("ADMIN_RECOVERY_KEY")
		os.Unsetenv("DB_PASSWORD")
		os.Unsetenv("POSTGRES_PASSWORD")
		krakencrypto.ReloadKeys()
	}()
	krakencrypto.ReloadKeys()

	cipherBeforeRotation, err := krakencrypto.Encrypt("super-sensitive-ssh-password")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	h := NewHandler(&database.Handle{DB: db, DBType: "SQLite"}, nil, nil, time.Now())
	h.SetK8sClient(mockAPI.client, "kraken")

	var updatedJWT, updatedRec string
	h.SetSecretsCallbacks(
		tmpPluginsDir,
		func(s string) { updatedJWT = s },
		func(s string) { updatedRec = s },
		nil,
	)

	// 1. GET /api/system/secrets when kraken-secrets does NOT exist yet in K8s
	getReq := httptest.NewRequest("GET", "/api/system/secrets", nil)
	getRec := httptest.NewRecorder()
	h.GetSecretsStatusHandler(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", getRec.Code, getRec.Body.String())
	}

	var statusResp models.SystemSecretsResponse
	if err := json.NewDecoder(getRec.Body).Decode(&statusResp); err != nil {
		t.Fatalf("failed to decode secrets response: %v", err)
	}
	if statusResp.K8sSecretPresent {
		t.Errorf("expected K8sSecretPresent=false before creating secret")
	}
	if len(statusResp.Secrets) < 4 {
		t.Errorf("expected at least 4 secret items, got %d", len(statusResp.Secrets))
	}

	// 2. PUT /api/system/secrets: rotate ENCRYPTION_KEY, set JWT_SECRET, DB_PASSWORD, ADMIN_RECOVERY_KEY
	newJWT := "rotated-jwt-secret-at-least-32-characters-long!"
	newEnc := "rotated-encryption-key-at-least-32-chars-long!"
	newRec := "dedicated-recovery-key-at-least-32-chars-long!"
	newDBPass := "strong-pg-password-123"

	body, _ := json.Marshal(models.UpdateSystemSecretsRequest{
		JWTSecret:        newJWT,
		EncryptionKey:    newEnc,
		AdminRecoveryKey: newRec,
		DBPassword:       newDBPass,
	})
	putReq := httptest.NewRequest("PUT", "/api/system/secrets", bytes.NewReader(body))
	putReq = putReq.WithContext(auth.WithUserContext(putReq.Context(), &auth.Claims{
		UserID:   1,
		Username: "admin",
		Role:     "admin",
	}))
	putRec := httptest.NewRecorder()
	h.UpdateSecretsHandler(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", putRec.Code, putRec.Body.String())
	}

	var afterResp models.SystemSecretsResponse
	if err := json.NewDecoder(putRec.Body).Decode(&afterResp); err != nil {
		t.Fatalf("failed to decode updated secrets response: %v", err)
	}
	if !afterResp.K8sSecretPresent {
		t.Errorf("expected K8sSecretPresent=true after creating kraken-secrets")
	}
	if !afterResp.ConfiguredByAdmin {
		t.Errorf("expected ConfiguredByAdmin=true")
	}
	if updatedJWT != newJWT || updatedRec != newRec {
		t.Errorf("expected live callbacks invoked with updated secrets")
	}

	// 3. Verify zero-downtime key rotation: ciphertext encrypted with old key still decrypts!
	plaintext, decErr := krakencrypto.Decrypt(cipherBeforeRotation)
	if decErr != nil || plaintext != "super-sensitive-ssh-password" {
		t.Fatalf("expected old ciphertext to decrypt via ENCRYPTION_KEY_PREVIOUS, got %q (err=%v)", plaintext, decErr)
	}

	// 4. Verify Secret/kraken-secrets created in Kubernetes with all keys
	sec := mockAPI.secrets["kraken/kraken-secrets"]
	if sec == nil {
		t.Fatalf("expected Secret kraken/kraken-secrets created")
	}
	if string(sec.Data["JWT_SECRET"]) != newJWT ||
		string(sec.Data["ENCRYPTION_KEY"]) != newEnc ||
		string(sec.Data["ENCRYPTION_KEY_PREVIOUS"]) != initialEncKey ||
		string(sec.Data["DB_PASSWORD"]) != newDBPass ||
		string(sec.Data["ADMIN_RECOVERY_KEY"]) != newRec {
		t.Errorf("unexpected Secret/kraken-secrets data: %+v", sec.Data)
	}

	// 5. Verify persisted file on disk
	persisted, pErr := config.LoadPersistentSecrets(tmpPluginsDir)
	if pErr != nil {
		t.Fatalf("expected .kraken_secrets.json persisted: %v", pErr)
	}
	if !persisted.ConfiguredByAdmin || persisted.JWTSecret != newJWT || persisted.DBPassword != newDBPass {
		t.Errorf("unexpected persisted secrets: %+v", persisted)
	}
}
