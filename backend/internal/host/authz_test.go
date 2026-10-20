package host

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/kraken/admin-tool/backend/internal/auth"
	"github.com/kraken/admin-tool/backend/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestRouter(t *testing.T, db *gorm.DB, jwtSecret string) (*chi.Mux, *Handler) {
	r := chi.NewRouter()
	handler := NewHandler(db, nil)

	r.Group(func(protected chi.Router) {
		protected.Use(auth.AuthMiddleware(jwtSecret))

		// Package operations: admin and operator
		protected.Group(func(opRouter chi.Router) {
			opRouter.Use(auth.RequireRoles("admin", "operator"))
			opRouter.Post("/api/hosts/{id}/packages/check", handler.CheckPackagesHandler)
			opRouter.Post("/api/hosts/{id}/packages/upgrade", handler.UpgradePackagesHandler)
			opRouter.Post("/api/hosts/{id}/packages/upgrade/stream", handler.UpgradePackagesStreamHandler)
			opRouter.Put("/api/hosts/{id}/package-manager", handler.UpdatePackageManagerHandler)
		})

		// Sudo policy & password clearing: admin only
		protected.Group(func(adminRouter chi.Router) {
			adminRouter.Use(auth.RequireRole("admin"))
			adminRouter.Post("/api/hosts/{id}/sudo-policy", handler.UpdateSudoPolicyHandler)
			adminRouter.Delete("/api/hosts/{id}/sudo-password", handler.ClearSudoPasswordHandler)
		})
	})

	return r, handler
}

func TestAuthorizationBypass_PrivilegeEscalationDefense(t *testing.T) {
	jwtSecret := "test-secret-key-32-bytes-long!!!"
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&models.DockerServer{}, &models.HostPackageUpdate{}, &models.AuditLog{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	server := models.DockerServer{
		ID:               1,
		Name:             "test-host",
		HostURL:          "unix:///var/run/docker.sock",
		CredentialPolicy: "save_securely",
	}
	db.Create(&server)

	r, _ := setupTestRouter(t, db, jwtSecret)

	// Generate tokens for viewer, operator, and admin
	viewerToken, _ := auth.GenerateToken(&models.User{ID: 10, Username: "viewer_user", Role: "viewer"}, jwtSecret, time.Hour)
	operatorToken, _ := auth.GenerateToken(&models.User{ID: 20, Username: "operator_user", Role: "operator"}, jwtSecret, time.Hour)
	adminToken, _ := auth.GenerateToken(&models.User{ID: 30, Username: "admin_user", Role: "admin"}, jwtSecret, time.Hour)

	t.Run("viewer cannot trigger package check (403)", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/hosts/1/packages/check", bytes.NewBufferString("{}"))
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("SECURITY RISK: Expected 403 Forbidden for viewer, got %d", rec.Code)
		}
	})

	t.Run("operator cannot update sudo policy (403)", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/hosts/1/sudo-policy", bytes.NewBufferString(`{"sudo_policy":"save_securely"}`))
		req.Header.Set("Authorization", "Bearer "+operatorToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("SECURITY RISK: Expected 403 Forbidden for operator modifying sudo policy, got %d", rec.Code)
		}
	})

	t.Run("operator cannot clear sudo password (403)", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/api/hosts/1/sudo-password", nil)
		req.Header.Set("Authorization", "Bearer "+operatorToken)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("SECURITY RISK: Expected 403 Forbidden for operator clearing sudo password, got %d", rec.Code)
		}
	})

	t.Run("unauthenticated request rejected (401)", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/hosts/1/sudo-policy", bytes.NewBufferString(`{"sudo_policy":"save_securely"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("SECURITY RISK: Expected 401 Unauthorized for unauthenticated request, got %d", rec.Code)
		}
	})

	t.Run("admin can update sudo policy (200)", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/hosts/1/sudo-policy", bytes.NewBufferString(`{"sudo_policy":"save_securely","sudo_password":"admin-pass-123"}`))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected 200 OK for admin updating sudo policy, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("operator can update package manager (200)", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/hosts/1/package-manager", bytes.NewBufferString(`{"package_manager":"apt"}`))
		req.Header.Set("Authorization", "Bearer "+operatorToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Expected 200 OK for operator updating package manager, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("viewer cannot update package manager (403)", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/hosts/1/package-manager", bytes.NewBufferString(`{"package_manager":"dnf"}`))
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("Expected 403 Forbidden for viewer updating package manager, got %d", rec.Code)
		}
	})

	t.Run("invalid package manager rejected (400)", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/api/hosts/1/package-manager", bytes.NewBufferString(`{"package_manager":"malicious_mgr"}`))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 Bad Request for invalid package manager, got %d", rec.Code)
		}
	})

	t.Run("viewer cannot trigger stream upgrade (403)", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/hosts/1/packages/upgrade/stream", bytes.NewBufferString(`{}`))
		req.Header.Set("Authorization", "Bearer "+viewerToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("SECURITY RISK: Expected 403 Forbidden for viewer stream upgrade, got %d", rec.Code)
		}
	})

	t.Run("operator can access stream upgrade (checked or challenged, not 403)", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/hosts/1/packages/upgrade/stream", bytes.NewBufferString(`{}`))
		req.Header.Set("Authorization", "Bearer "+operatorToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code == http.StatusForbidden {
			t.Errorf("Operator should have access to stream upgrade, but got 403 Forbidden")
		}
	})
}
