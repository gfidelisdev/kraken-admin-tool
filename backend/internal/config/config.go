package config

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
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

func generateEphemeralSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to generate random secret: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(b)
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
	jwtSecret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if IsInsecureSecret(jwtSecret) {
		if strings.EqualFold(env, "production") {
			log.Fatalf("[SECURITY FATAL] Refusing to start in production: JWT_SECRET is missing, shorter than 32 bytes, or set to a known default placeholder. Configure a strong JWT_SECRET in your .env file.")
		}
		jwtSecret = generateEphemeralSecret()
		log.Println("[SECURITY] Generated random ephemeral JWT_SECRET for non-production session. Set JWT_SECRET in .env to persist sessions across restarts.")
	}

	encryptionKey := strings.TrimSpace(os.Getenv("ENCRYPTION_KEY"))
	if encryptionKey != "" && IsInsecureSecret(encryptionKey) {
		if strings.EqualFold(env, "production") {
			log.Fatalf("[SECURITY FATAL] Refusing to start in production: ENCRYPTION_KEY is shorter than 32 bytes or set to a known placeholder.")
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
		Port:              getEnv("PORT", "8080"),
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
		AllowRegistration: getEnv("ALLOW_REGISTRATION", "false") == "true",
		PluginsDir:        getEnv("PLUGINS_DIR", "./data/plugins"),
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
