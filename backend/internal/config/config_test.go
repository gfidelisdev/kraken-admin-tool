package config

import (
	"os"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	os.Clearenv()

	cfg := Load()
	if cfg.Port != "8080" {
		t.Errorf("expected port 8080, got %s", cfg.Port)
	}
	if cfg.DBHost != "localhost" {
		t.Errorf("expected db host localhost, got %s", cfg.DBHost)
	}
	if cfg.DBName != "kraken_db" {
		t.Errorf("expected db name kraken_db, got %s", cfg.DBName)
	}
	if cfg.JWTSecret == "" {
		t.Error("expected non-empty JWT secret")
	}
}

func TestLoadCustomEnv(t *testing.T) {
	os.Setenv("PORT", "9090")
	os.Setenv("DB_HOST", "db.example.com")
	os.Setenv("CORS_ORIGINS", "http://localhost:3000,http://example.com")
	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("DB_HOST")
		os.Unsetenv("CORS_ORIGINS")
	}()

	cfg := Load()
	if cfg.Port != "9090" {
		t.Errorf("expected port 9090, got %s", cfg.Port)
	}
	if cfg.DBHost != "db.example.com" {
		t.Errorf("expected db host db.example.com, got %s", cfg.DBHost)
	}
	if len(cfg.CorsOrigins) != 2 || cfg.CorsOrigins[0] != "http://localhost:3000" {
		t.Errorf("expected CORS origins parsed, got %v", cfg.CorsOrigins)
	}
}

func TestPostgresDSN(t *testing.T) {
	cfg := &Config{
		DBHost:     "localhost",
		DBPort:     "5432",
		DBUser:     "user",
		DBPassword: "password",
		DBName:     "mydb",
		DBSSLMode:  "disable",
	}

	expected := "host=localhost user=user password=password dbname=mydb port=5432 sslmode=disable TimeZone=UTC"
	if cfg.PostgresDSN() != expected {
		t.Errorf("expected %s, got %s", expected, cfg.PostgresDSN())
	}
}
