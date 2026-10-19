package audit

import (
	"testing"
	"time"

	"github.com/kraken/admin-tool/backend/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAuditLogAndList(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite in-memory: %v", err)
	}

	sqlDB, err := db.DB()
	if err == nil {
		sqlDB.SetMaxOpenConns(1)
	}

	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	Log(db, 1, "admin", "START_CONTAINER", "docker_container", "c-123", "Started", "SUCCESS")
	Log(db, 1, "admin", "STOP_CONTAINER", "docker_container", "c-123", "Stopped", "SUCCESS")

	// Wait briefly for asynchronous goroutines
	time.Sleep(100 * time.Millisecond)

	logs, total, err := List(db, 1, 10)
	if err != nil {
		t.Fatalf("List returned error: %v", err)
	}

	if total != 2 {
		t.Errorf("expected 2 audit entries, got %d", total)
	}

	if len(logs) != 2 {
		t.Errorf("expected 2 logs returned, got %d", len(logs))
	}
}
