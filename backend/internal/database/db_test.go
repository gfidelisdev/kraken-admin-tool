package database

import (
	"os"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/config"
	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestInitDBSQLiteFallbackAndSeed(t *testing.T) {
	t.Setenv("INITIAL_ADMIN_PASSWORD", "test-initial-admin-password-123")

	// Point to unreachable postgres port to ensure fallback works cleanly
	cfg := &config.Config{
		DBHost:     "127.0.0.1",
		DBPort:     "54321",
		DBUser:     "postgres",
		DBPassword: "password",
		DBName:     "kraken_test",
		DBSSLMode:  "disable",
	}

	defer func() {
		_ = os.Remove("kraken_data.db")
	}()

	handle, err := InitDB(cfg)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	if handle.DBType != "SQLite" {
		t.Errorf("expected SQLite fallback, got %s", handle.DBType)
	}

	if err := handle.CheckHealth(); err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}

	// Verify admin seed
	var admin models.User
	if err := handle.DB.Where("username = ?", "admin").First(&admin).Error; err != nil {
		t.Fatalf("admin user not seeded: %v", err)
	}

	if admin.Role != "admin" {
		t.Errorf("expected admin role, got %s", admin.Role)
	}
}

func TestInitDBProductionPostgresFailureDoesNotFallbackToSQLite(t *testing.T) {
	// In production, failure to connect to PostgreSQL must return an error and NOT fallback to SQLite
	t.Setenv("DB_MAX_RETRIES", "2")
	t.Setenv("DB_RETRY_INTERVAL_MS", "10")

	cfg := &config.Config{
		Environment: "production",
		DBHost:      "127.0.0.1",
		DBPort:      "54321",
		DBUser:      "postgres",
		DBPassword:  "password",
		DBName:      "kraken_test",
		DBSSLMode:   "disable",
	}

	handle, err := InitDB(cfg)
	if err == nil {
		t.Fatalf("expected InitDB to return an error in production when postgres is unavailable, but got handle: %+v", handle)
	}

	// Verify that kraken_data.db was NOT created
	if _, statErr := os.Stat("kraken_data.db"); statErr == nil {
		_ = os.Remove("kraken_data.db")
		t.Errorf("kraken_data.db should not have been created as a fallback in production")
	}
}
