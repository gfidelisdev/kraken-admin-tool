package plugins_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/models"
	"github.com/kraken/admin-tool/backend/internal/plugins"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupIsolationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dbPath := t.TempDir() + "/test_isolation.db"
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&models.AuditLog{},
		&models.InstalledPlugin{},
		&models.UserPluginState{},
		&models.PluginKVStore{},
		&models.DockerServer{},
		&models.UserDashboardPreference{},
	); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return db
}

func TestPluginManager_PerUserIsolation(t *testing.T) {
	db := setupIsolationTestDB(t)

	manifest := models.PluginManifest{
		ID:          "com.example.isolated-plugin",
		Name:        "Isolated Plugin",
		Version:     "1.0.0",
		Permissions: []string{"storage:kv"},
		Runtime: models.PluginRuntimeConfig{
			Type: "none",
		},
		UIExtensions: models.PluginUIExtensions{
			Sidebar: []models.PluginSidebarExtension{
				{ID: "iso-tab", Title: "Isolated Tab", Icon: "Shield"},
			},
			DashboardWidgets: []models.PluginDashboardExtension{
				{ID: "iso-widget", Title: "Isolated Widget"},
			},
		},
	}
	manifestBytes, _ := json.Marshal(manifest)
	permsBytes, _ := json.Marshal(manifest.Permissions)

	installed := models.InstalledPlugin{
		PluginID:           manifest.ID,
		Name:               manifest.Name,
		Version:            manifest.Version,
		Status:             "installed",
		ManifestJSON:       string(manifestBytes),
		GrantedPermissions: string(permsBytes),
		InstallPath:        t.TempDir(),
	}
	db.Create(&installed)

	mgr, err := plugins.NewManager(context.Background(), db, nil, t.TempDir())
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	userA := uint(101)
	userB := uint(102)

	// Step 1: User A and User B list plugins initially
	listA, err := mgr.ListPluginsForUser(userA)
	if err != nil {
		t.Fatalf("ListPluginsForUser(userA) failed: %v", err)
	}
	if len(listA) != 1 || listA[0].Status != "disabled" {
		t.Fatalf("expected 1 disabled plugin for userA, got %+v", listA)
	}

	listB, err := mgr.ListPluginsForUser(userB)
	if err != nil {
		t.Fatalf("ListPluginsForUser(userB) failed: %v", err)
	}
	if len(listB) != 1 || listB[0].Status != "disabled" {
		t.Fatalf("expected 1 disabled plugin for userB, got %+v", listB)
	}

	// Step 2: User A enables plugin
	err = mgr.EnablePlugin(context.Background(), manifest.ID, "user_a", userA)
	if err != nil {
		t.Fatalf("userA EnablePlugin failed: %v", err)
	}

	// Verify User A now sees status "enabled"
	listA, _ = mgr.ListPluginsForUser(userA)
	if listA[0].Status != "enabled" {
		t.Fatalf("expected userA status 'enabled', got '%s'", listA[0].Status)
	}

	// Verify User B STILL sees status "disabled"
	listB, _ = mgr.ListPluginsForUser(userB)
	if listB[0].Status != "disabled" {
		t.Fatalf("expected userB status 'disabled', got '%s'", listB[0].Status)
	}

	// Step 3: Verify UI extensions are visible ONLY to user A
	extsA, err := mgr.GetActiveUIExtensions(userA)
	if err != nil {
		t.Fatalf("GetActiveUIExtensions(userA) failed: %v", err)
	}
	if len(extsA.Sidebar) != 1 || len(extsA.DashboardWidgets) != 1 {
		t.Fatalf("expected userA to have 1 sidebar and 1 widget, got %+v", extsA)
	}

	extsB, err := mgr.GetActiveUIExtensions(userB)
	if err != nil {
		t.Fatalf("GetActiveUIExtensions(userB) failed: %v", err)
	}
	if len(extsB.Sidebar) != 0 || len(extsB.DashboardWidgets) != 0 {
		t.Fatalf("expected userB to have 0 extensions, got %+v", extsB)
	}

	// Step 4: Verify KV store isolation between User A and User B
	_, err = mgr.CallPlugin(context.Background(), manifest.ID, "kv_set", map[string]interface{}{
		"key":   "app_config",
		"value": "user_a_secret_val",
	}, "user_a", "operator", userA)
	if err != nil {
		t.Fatalf("userA kv_set failed: %v", err)
	}

	// User B enables plugin and writes different value to the same key
	_ = mgr.EnablePlugin(context.Background(), manifest.ID, "user_b", userB)
	_, err = mgr.CallPlugin(context.Background(), manifest.ID, "kv_set", map[string]interface{}{
		"key":   "app_config",
		"value": "user_b_different_val",
	}, "user_b", "operator", userB)
	if err != nil {
		t.Fatalf("userB kv_set failed: %v", err)
	}

	// User A reads key -> gets user A's value
	valA, err := mgr.CallPlugin(context.Background(), manifest.ID, "kv_get", "app_config", "user_a", "operator", userA)
	if err != nil {
		t.Fatalf("userA kv_get failed: %v", err)
	}
	if valA != "user_a_secret_val" {
		t.Fatalf("expected 'user_a_secret_val', got '%v'", valA)
	}

	// User B reads key -> gets user B's value
	valB, err := mgr.CallPlugin(context.Background(), manifest.ID, "kv_get", "app_config", "user_b", "operator", userB)
	if err != nil {
		t.Fatalf("userB kv_get failed: %v", err)
	}
	if valB != "user_b_different_val" {
		t.Fatalf("expected 'user_b_different_val', got '%v'", valB)
	}

	// Step 5: Verify a legacy user_id=0 row NEVER leaks to a brand-new User C
	db.Create(&models.PluginKVStore{
		PluginID: manifest.ID,
		Key:      "azure_kv_config",
		Value:    `{"accounts":[{"email":"leaked@personal.com","access_token":"secret"}]}`,
		UserID:   0,
	})
	userC := uint(103)
	_ = mgr.EnablePlugin(context.Background(), manifest.ID, "user_c", userC)
	valC, err := mgr.CallPlugin(context.Background(), manifest.ID, "kv_get", "azure_kv_config", "user_c", "operator", userC)
	if err != nil {
		t.Fatalf("userC kv_get failed: %v", err)
	}
	if valC != nil {
		t.Fatalf("SECURITY FAILURE: legacy user_id=0 KV leaked to brand-new userC: %v", valC)
	}

	// Step 6: Verify auth.logout host adapter wipes userA's KV entries and revokes session for userA
	var revokedUser uint
	mgr.SetMicrosoftSessionRevoker(func(uid uint, sid string) {
		revokedUser = uid
	})
	_, err = mgr.ResolveHostAdapter(
		context.Background(),
		map[string]interface{}{
			"data": map[string]interface{}{
				"operation": "auth.logout",
			},
		},
		map[string]bool{"storage:kv": true},
		manifest.ID,
		"user_a",
		userA,
	)
	if err != nil {
		t.Fatalf("ResolveHostAdapter(auth.logout) failed: %v", err)
	}
	if revokedUser != userA {
		t.Fatalf("expected revokedUser=%d, got %d", userA, revokedUser)
	}
	valAAfterLogout, _ := mgr.CallPlugin(context.Background(), manifest.ID, "kv_get", "app_config", "user_a", "operator", userA)
	if valAAfterLogout != nil {
		t.Fatalf("expected userA KV to be wiped after auth.logout, got %v", valAAfterLogout)
	}
}
