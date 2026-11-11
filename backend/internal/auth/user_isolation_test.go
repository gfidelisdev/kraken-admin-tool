package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/docker"
	"github.com/kraken/admin-tool/backend/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := t.TempDir() + "/test_auth_isolation.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.User{},
		&models.AuditLog{},
		&models.DockerServer{},
		&models.UserDashboardPreference{},
	); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return db
}

func TestDashboardLayout_UserIsolation(t *testing.T) {
	db := setupTestDB(t)
	handler := &auth.Handler{DB: db}

	userA := uint(101)
	userB := uint(102)

	// User A saves layout
	payloadA := map[string]string{"layout_json": `{"order":["metrics","audit_logs"]}`}
	bodyA, _ := json.Marshal(payloadA)
	reqA := httptest.NewRequest(http.MethodPut, "/api/users/me/dashboard", bytes.NewReader(bodyA))
	reqA = reqA.WithContext(context.WithValue(reqA.Context(), auth.UserContextKey, &auth.Claims{UserID: userA, Username: "user_a"}))
	wA := httptest.NewRecorder()
	handler.SaveDashboardLayout(wA, reqA)
	if wA.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wA.Code)
	}

	// User B saves different layout
	payloadB := map[string]string{"layout_json": `{"order":["quick_nav","infra_health"]}`}
	bodyB, _ := json.Marshal(payloadB)
	reqB := httptest.NewRequest(http.MethodPut, "/api/users/me/dashboard", bytes.NewReader(bodyB))
	reqB = reqB.WithContext(context.WithValue(reqB.Context(), auth.UserContextKey, &auth.Claims{UserID: userB, Username: "user_b"}))
	wB := httptest.NewRecorder()
	handler.SaveDashboardLayout(wB, reqB)
	if wB.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wB.Code)
	}

	// User A retrieves dashboard
	getReqA := httptest.NewRequest(http.MethodGet, "/api/users/me/dashboard", nil)
	getReqA = getReqA.WithContext(context.WithValue(getReqA.Context(), auth.UserContextKey, &auth.Claims{UserID: userA, Username: "user_a"}))
	getWA := httptest.NewRecorder()
	handler.GetDashboardLayout(getWA, getReqA)
	if getWA.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getWA.Code)
	}
	var respA map[string]interface{}
	_ = json.Unmarshal(getWA.Body.Bytes(), &respA)
	if respA["layout_json"] != payloadA["layout_json"] {
		t.Fatalf("expected layoutA %s, got %v", payloadA["layout_json"], respA["layout_json"])
	}

	// User B retrieves dashboard
	getReqB := httptest.NewRequest(http.MethodGet, "/api/users/me/dashboard", nil)
	getReqB = getReqB.WithContext(context.WithValue(getReqB.Context(), auth.UserContextKey, &auth.Claims{UserID: userB, Username: "user_b"}))
	getWB := httptest.NewRecorder()
	handler.GetDashboardLayout(getWB, getReqB)
	if getWB.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getWB.Code)
	}
	var respB map[string]interface{}
	_ = json.Unmarshal(getWB.Body.Bytes(), &respB)
	if respB["layout_json"] != payloadB["layout_json"] {
		t.Fatalf("expected layoutB %s, got %v", payloadB["layout_json"], respB["layout_json"])
	}
}

func TestDockerServer_UserIsolation(t *testing.T) {
	db := setupTestDB(t)
	svc := docker.NewDockerService("unix:///var/run/docker.sock", db)

	// 1. Seed base default server (shared by all)
	defaultSrv := models.DockerServer{
		Name:      "Local Daemon",
		HostURL:   "unix:///var/run/docker.sock",
		IsDefault: true,
		UserID:    0,
	}
	db.Create(&defaultSrv)

	// 2. User A creates server A
	srvA := models.DockerServer{
		Name:      "Server User A",
		HostURL:   "ssh://user@srv-a:22",
		IsDefault: false,
		UserID:    101,
	}
	db.Create(&srvA)

	// 3. User B creates server B
	srvB := models.DockerServer{
		Name:      "Server User B",
		HostURL:   "ssh://user@srv-b:22",
		IsDefault: false,
		UserID:    102,
	}
	db.Create(&srvB)

	// List servers for User A: should only see User A's server (strict isolation)
	serversA, err := svc.ListServersForUser(context.Background(), 101, false)
	if err != nil {
		t.Fatalf("ListServersForUser(101) failed: %v", err)
	}
	if len(serversA) != 1 {
		t.Fatalf("expected 1 server for user A (only own server, no automatic default host), got %d", len(serversA))
	}
	if serversA[0].UserID != 101 {
		t.Errorf("user A should only see server with UserID 101, got %d", serversA[0].UserID)
	}

	// List servers for User B: should only see User B's server (strict isolation)
	serversB, err := svc.ListServersForUser(context.Background(), 102, false)
	if err != nil {
		t.Fatalf("ListServersForUser(102) failed: %v", err)
	}
	if len(serversB) != 1 {
		t.Fatalf("expected 1 server for user B (only own server, no automatic default host), got %d", len(serversB))
	}
	if serversB[0].UserID != 102 {
		t.Errorf("user B should only see server with UserID 102, got %d", serversB[0].UserID)
	}

	// User C has no servers: should see 0 servers
	serversC, err := svc.ListServersForUser(context.Background(), 103, false)
	if err != nil {
		t.Fatalf("ListServersForUser(103) failed: %v", err)
	}
	if len(serversC) != 0 {
		t.Fatalf("expected 0 servers for user C without servers, got %d", len(serversC))
	}
}
