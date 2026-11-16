package config

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Config holds all backend configuration parameters
type Config struct {
	Port              string
	DBHost            string
	DBPort            string
	DBUser            string
	DBPassword        string
	DBName            string
	DBSSLMode         string
	JWTSecret         string
	EncryptionKey     string
	AdminRecoveryKey  string
	TrustedProxies    []string
	DockerHost        string
	Kubeconfig        string
	CorsOrigins       []string
	Environment       string
	RuntimeMode       string
	K8sNamespace      string
	AllowRegistration    bool
	PluginsDir           string
	MicrosoftClientID    string
	MicrosoftTenantID    string
	SecretsPreconfigured bool
	MissingEnvSecrets    []string
}

var knownInsecureSecrets = map[string]bool{
	"kraken-secret-key-super-secure-change-in-production":           true,
	"kraken-production-jwt-secret-key-32bytes-long!":                true,
	"kraken-default-internal-encryption-key-must-change":            true,
	"kraken-admin-master-recovery":                                  true,
	"replace_with_a_random_jwt_secret_at_least_32_bytes!":           true,
	"replace_with_a_unique_random_jwt_secret_at_least_32_bytes":     true,
	"replace_with_a_unique_random_encryption_key_at_least_32_bytes": true,
	"changeme": true,
	"secret":   true,
}

// IsInsecureSecret checks whether a secret is empty, too short (<32 bytes), or matches a known placeholder
func IsInsecureSecret(s string) bool {
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 32 {
		return true
	}
	return knownInsecureSecrets[trimmed]
}

// LoadDotEnv reads key=value pairs from the first existing .env file and sets any
// environment variables that are not already set in the process environment.
func LoadDotEnv(paths ...string) {
	if len(paths) == 0 {
		paths = []string{".env", "../.env", "backend/.env"}
	}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			line = strings.TrimPrefix(line, "export ")
			idx := strings.IndexByte(line, '=')
			if idx <= 0 {
				continue
			}
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
				val = val[1 : len(val)-1]
			}
			if _, exists := os.LookupEnv(key); !exists {
				_ = os.Setenv(key, val)
			}
		}
		_ = f.Close()
	}
}

// PersistentSecrets stores cryptographic and database secrets persisted on disk across restarts.
type PersistentSecrets struct {
	JWTSecret             string `json:"jwt_secret"`
	EncryptionKey         string `json:"encryption_key"`
	EncryptionKeyPrevious string `json:"encryption_key_previous,omitempty"`
	AdminRecoveryKey      string `json:"admin_recovery_key,omitempty"`
	DBPassword            string `json:"db_password,omitempty"`
	ConfiguredByAdmin     bool   `json:"configured_by_admin,omitempty"`
}

func generateEphemeralSecret() string {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to generate random secret: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// LoadPersistentSecrets reads the persisted secrets JSON file from pluginsDir if present.
func LoadPersistentSecrets(pluginsDir string) (PersistentSecrets, error) {
	var stored PersistentSecrets
	if pluginsDir == "" {
		return stored, os.ErrNotExist
	}
	secretsPath := filepath.Join(pluginsDir, ".kraken_secrets.json")
	raw, err := os.ReadFile(secretsPath)
	if err != nil {
		return stored, err
	}
	if err := json.Unmarshal(raw, &stored); err != nil {
		return stored, err
	}
	return stored, nil
}

// SavePersistentSecrets writes the persistent secrets JSON file with 0600 permissions.
func SavePersistentSecrets(pluginsDir string, s PersistentSecrets) error {
	if pluginsDir == "" {
		return fmt.Errorf("pluginsDir is empty")
	}
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pluginsDir, ".kraken_secrets.json"), encoded, 0600)
}

func loadOrGeneratePersistentSecrets(pluginsDir string) (PersistentSecrets, bool) {
	stored, _ := LoadPersistentSecrets(pluginsDir)

	dirty := false
	if IsInsecureSecret(stored.JWTSecret) {
		stored.JWTSecret = generateEphemeralSecret()
		dirty = true
	}
	if IsInsecureSecret(stored.EncryptionKey) || stored.EncryptionKey == stored.JWTSecret {
		stored.EncryptionKey = generateEphemeralSecret()
		dirty = true
	}

	if dirty && pluginsDir != "" {
		if err := SavePersistentSecrets(pluginsDir, stored); err != nil {
			log.Printf("[SECURITY] Warning: failed to persist auto-generated secrets to %s/.kraken_secrets.json: %v", pluginsDir, err)
		}
	}

	return stored, dirty
}

func detectRuntimeMode() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("KRAKEN_RUNTIME_MODE")))
	if mode == "kubernetes" || mode == "k8s" {
		return "kubernetes"
	}
	if mode == "docker" {
		return "docker"
	}
	if strings.TrimSpace(os.Getenv("KUBERNETES_SERVICE_HOST")) != "" {
		return "kubernetes"
	}
	return "docker"
}

func detectK8sNamespace() string {
	if ns := strings.TrimSpace(os.Getenv("KRAKEN_NAMESPACE")); ns != "" {
		return ns
	}
	if raw, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		if ns := strings.TrimSpace(string(raw)); ns != "" {
			return ns
		}
	}
	return "default"
}

// Load loads the configuration from .env files and environment variables
func Load() *Config {
	LoadDotEnv()

	homeDir, _ := os.UserHomeDir()
	defaultKubeconfig := ""
	if homeDir != "" {
		candidate := filepath.Join(homeDir, ".kube", "config")
		if _, err := os.Stat(candidate); err == nil {
			defaultKubeconfig = candidate
		}
	}

	corsEnv := getEnv("CORS_ORIGINS", "http://localhost:3000,http://127.0.0.1:3000")
	var origins []string
	if corsEnv == "*" {
		origins = []string{"*"}
	} else {
		for _, o := range strings.Split(corsEnv, ",") {
			trimmed := strings.TrimSpace(o)
			if trimmed != "" {
				origins = append(origins, trimmed)
			}
		}
	}

	var trustedProxies []string
	if tpEnv := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES")); tpEnv != "" {
		for _, p := range strings.Split(tpEnv, ",") {
			if trimmed := strings.TrimSpace(p); trimmed != "" {
				trustedProxies = append(trustedProxies, trimmed)
			}
		}
	}

	env := getEnv("ENVIRONMENT", "development")
	pluginsDir := getEnv("PLUGINS_DIR", "./data/plugins")
	jwtSecret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	encryptionKey := strings.TrimSpace(os.Getenv("ENCRYPTION_KEY"))
	dbPassword := strings.TrimSpace(os.Getenv("DB_PASSWORD"))
	adminRecoveryKey := strings.TrimSpace(os.Getenv("ADMIN_RECOVERY_KEY"))

	var missingEnv []string
	if IsInsecureSecret(jwtSecret) {
		missingEnv = append(missingEnv, "JWT_SECRET")
	}
	if IsInsecureSecret(encryptionKey) {
		missingEnv = append(missingEnv, "ENCRYPTION_KEY")
	}
	if dbPassword == "" {
		missingEnv = append(missingEnv, "DB_PASSWORD")
	}
	if adminRecoveryKey == "" {
		missingEnv = append(missingEnv, "ADMIN_RECOVERY_KEY")
	}

	secretsPreconfigured := !IsInsecureSecret(jwtSecret) && !IsInsecureSecret(encryptionKey)

	if IsInsecureSecret(jwtSecret) || IsInsecureSecret(encryptionKey) || dbPassword == "" || adminRecoveryKey == "" {
		var stored PersistentSecrets
		// Avoid creating ./data/plugins inside source directories during unit tests unless PLUGINS_DIR is explicitly set.
		if flag.Lookup("test.v") != nil && strings.TrimSpace(os.Getenv("PLUGINS_DIR")) == "" {
			stored = PersistentSecrets{
				JWTSecret:     generateEphemeralSecret(),
				EncryptionKey: generateEphemeralSecret(),
			}
		} else {
			stored, _ = loadOrGeneratePersistentSecrets(pluginsDir)
			if stored.ConfiguredByAdmin {
				secretsPreconfigured = true
			}
			log.Printf("[SECURITY] Persistent cryptographic keys initialized in %s/.kraken_secrets.json (set JWT_SECRET and ENCRYPTION_KEY explicitly to override).", pluginsDir)
		}

		if IsInsecureSecret(jwtSecret) {
			jwtSecret = stored.JWTSecret
			_ = os.Setenv("JWT_SECRET", jwtSecret)
		}
		if IsInsecureSecret(encryptionKey) {
			encryptionKey = stored.EncryptionKey
			_ = os.Setenv("ENCRYPTION_KEY", encryptionKey)
		}
		if strings.TrimSpace(os.Getenv("ENCRYPTION_KEY_PREVIOUS")) == "" && stored.EncryptionKeyPrevious != "" {
			_ = os.Setenv("ENCRYPTION_KEY_PREVIOUS", stored.EncryptionKeyPrevious)
		}
		if dbPassword == "" && stored.DBPassword != "" {
			dbPassword = stored.DBPassword
			_ = os.Setenv("DB_PASSWORD", dbPassword)
		}
		if adminRecoveryKey == "" && stored.AdminRecoveryKey != "" {
			adminRecoveryKey = stored.AdminRecoveryKey
			_ = os.Setenv("ADMIN_RECOVERY_KEY", adminRecoveryKey)
		}
	}

	if adminRecoveryKey != "" {
		if IsInsecureSecret(adminRecoveryKey) || adminRecoveryKey == jwtSecret {
			log.Println("[SECURITY WARNING] ADMIN_RECOVERY_KEY is weak, a known default, or identical to JWT_SECRET; disabling HTTP admin recovery endpoint.")
			adminRecoveryKey = ""
		}
	}

	return &Config{
		Port:                 getEnv("BACKEND_PORT", getEnv("PORT", "8080")),
		DBHost:               getEnv("DB_HOST", "localhost"),
		DBPort:               getEnv("DB_PORT", "5432"),
		DBUser:               getEnv("DB_USER", "postgres"),
		DBPassword:           dbPassword,
		DBName:               getEnv("DB_NAME", "kraken_db"),
		DBSSLMode:            getEnv("DB_SSLMODE", "disable"),
		JWTSecret:            jwtSecret,
		EncryptionKey:        encryptionKey,
		AdminRecoveryKey:     adminRecoveryKey,
		TrustedProxies:       trustedProxies,
		DockerHost:           getEnv("DOCKER_HOST", ""),
		Kubeconfig:           getEnv("KUBECONFIG", defaultKubeconfig),
		CorsOrigins:          origins,
		Environment:          env,
		RuntimeMode:          detectRuntimeMode(),
		K8sNamespace:         detectK8sNamespace(),
		AllowRegistration:    getEnv("ALLOW_REGISTRATION", "false") == "true",
		PluginsDir:           pluginsDir,
		MicrosoftClientID:    getEnv("MICROSOFT_CLIENT_ID", "04b07795-8ddb-461a-bbee-02f9e1bf7b46"),
		MicrosoftTenantID:    getEnv("MICROSOFT_TENANT_ID", "common"),
		SecretsPreconfigured: secretsPreconfigured,
		MissingEnvSecrets:    missingEnv,
	}
}

// PostgresDSN returns the connection string for PostgreSQL
func (c *Config) PostgresDSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=UTC",
		c.DBHost, c.DBUser, c.DBPassword, c.DBName, c.DBPort, c.DBSSLMode)
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return defaultVal
}
