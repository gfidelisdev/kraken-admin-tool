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
	AllowRegistration bool
	PluginsDir        string
	MicrosoftClientID string
	MicrosoftTenantID string
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

type persistentSecrets struct {
	JWTSecret     string `json:"jwt_secret"`
	EncryptionKey string `json:"encryption_key"`
}

func generateEphemeralSecret() string {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to generate random secret: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func loadOrGeneratePersistentSecrets(pluginsDir string) (string, string) {
	secretsPath := filepath.Join(pluginsDir, ".kraken_secrets.json")
	var stored persistentSecrets
	if raw, err := os.ReadFile(secretsPath); err == nil {
		_ = json.Unmarshal(raw, &stored)
	}

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
		if err := os.MkdirAll(pluginsDir, 0755); err == nil {
			if encoded, err := json.MarshalIndent(stored, "", "  "); err == nil {
				if err := os.WriteFile(secretsPath, encoded, 0600); err != nil {
					log.Printf("[SECURITY] Warning: failed to persist auto-generated secrets to %s: %v", secretsPath, err)
				}
			}
		} else {
			log.Printf("[SECURITY] Warning: failed to create plugins directory %s for secret persistence: %v", pluginsDir, err)
		}
	}

	return stored.JWTSecret, stored.EncryptionKey
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

	if IsInsecureSecret(jwtSecret) || IsInsecureSecret(encryptionKey) {
		var persistedJWT, persistedEnc string
		// Avoid creating ./data/plugins inside source directories during unit tests unless PLUGINS_DIR is explicitly set.
		if flag.Lookup("test.v") != nil && strings.TrimSpace(os.Getenv("PLUGINS_DIR")) == "" {
			persistedJWT = generateEphemeralSecret()
			persistedEnc = generateEphemeralSecret()
		} else {
			persistedJWT, persistedEnc = loadOrGeneratePersistentSecrets(pluginsDir)
			log.Printf("[SECURITY] Persistent cryptographic keys initialized in %s/.kraken_secrets.json (set JWT_SECRET and ENCRYPTION_KEY explicitly to override).", pluginsDir)
		}

		if IsInsecureSecret(jwtSecret) {
			jwtSecret = persistedJWT
			_ = os.Setenv("JWT_SECRET", jwtSecret)
		}
		if IsInsecureSecret(encryptionKey) {
			encryptionKey = persistedEnc
			_ = os.Setenv("ENCRYPTION_KEY", encryptionKey)
		}
	}

	adminRecoveryKey := strings.TrimSpace(os.Getenv("ADMIN_RECOVERY_KEY"))
	if adminRecoveryKey != "" {
		if IsInsecureSecret(adminRecoveryKey) || adminRecoveryKey == jwtSecret {
			log.Println("[SECURITY WARNING] ADMIN_RECOVERY_KEY is weak, a known default, or identical to JWT_SECRET; disabling HTTP admin recovery endpoint.")
			adminRecoveryKey = ""
		}
	}

	return &Config{
		Port:              getEnv("BACKEND_PORT", getEnv("PORT", "8080")),
		DBHost:            getEnv("DB_HOST", "localhost"),
		DBPort:            getEnv("DB_PORT", "5432"),
		DBUser:            getEnv("DB_USER", "postgres"),
		DBPassword:        getEnv("DB_PASSWORD", ""),
		DBName:            getEnv("DB_NAME", "kraken_db"),
		DBSSLMode:         getEnv("DB_SSLMODE", "disable"),
		JWTSecret:         jwtSecret,
		EncryptionKey:     encryptionKey,
		AdminRecoveryKey:  adminRecoveryKey,
		TrustedProxies:    trustedProxies,
		DockerHost:        getEnv("DOCKER_HOST", ""),
		Kubeconfig:        getEnv("KUBECONFIG", defaultKubeconfig),
		CorsOrigins:       origins,
		Environment:       env,
		RuntimeMode:       detectRuntimeMode(),
		K8sNamespace:      detectK8sNamespace(),
		AllowRegistration: getEnv("ALLOW_REGISTRATION", "false") == "true",
		PluginsDir:        pluginsDir,
		MicrosoftClientID: getEnv("MICROSOFT_CLIENT_ID", "04b07795-8ddb-461a-bbee-02f9e1bf7b46"),
		MicrosoftTenantID: getEnv("MICROSOFT_TENANT_ID", "common"),
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
