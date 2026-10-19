package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config holds all backend configuration parameters
type Config struct {
	Port        string
	DBHost      string
	DBPort      string
	DBUser      string
	DBPassword  string
	DBName      string
	DBSSLMode   string
	JWTSecret   string
	DockerHost  string
	Kubeconfig  string
	CorsOrigins       []string
	Environment       string
	AllowRegistration bool
}

// Load loads the configuration from environment variables with sensible defaults
func Load() *Config {
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

	env := getEnv("ENVIRONMENT", "development")
	jwtSecret := getEnv("JWT_SECRET", "kraken-secret-key-super-secure-change-in-production")
	if strings.ToLower(env) == "production" {
		if jwtSecret == "kraken-secret-key-super-secure-change-in-production" || jwtSecret == "kraken-production-jwt-secret-key-32bytes-long!" {
			fmt.Println("[SECURITY WARNING] Default insecure JWT_SECRET detected in production! Provide a unique, strong JWT_SECRET environment variable.")
		}
	}

	return &Config{
		Port:        getEnv("PORT", "8080"),
		DBHost:      getEnv("DB_HOST", "localhost"),
		DBPort:      getEnv("DB_PORT", "5432"),
		DBUser:      getEnv("DB_USER", "postgres"),
		DBPassword:  getEnv("DB_PASSWORD", "postgres"),
		DBName:      getEnv("DB_NAME", "kraken_db"),
		DBSSLMode:   getEnv("DB_SSLMODE", "disable"),
		JWTSecret:   jwtSecret,
		DockerHost:  getEnv("DOCKER_HOST", ""),
		Kubeconfig:  getEnv("KUBECONFIG", defaultKubeconfig),
		CorsOrigins:       origins,
		Environment:       env,
		AllowRegistration: getEnv("ALLOW_REGISTRATION", "false") == "true",
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
