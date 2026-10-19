package database

import (
	"os"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/config"
	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestInitDBSQLiteFallbackAndSeed(t *testing.T) {
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
