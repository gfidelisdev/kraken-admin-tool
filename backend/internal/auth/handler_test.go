package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kraken/admin-tool/backend/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	dbFile := t.TempDir() + "/test.db"
	db, err := gorm.Open(sqlite.Open(dbFile), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.AuditLog{}); err != nil {
		t.Fatalf("failed to migrate database: %v", err)
	}
	return db
}

func TestHandler_Register_Bootstrap(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, "test-secret", false)

	reqBody, _ := json.Marshal(models.RegisterRequest{
		Username: "firstadmin",
		Email:    "first@admin.local",
		Password: "password123",
	})
	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()

	h.Register(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var res models.AuthResponse
	_ = json.NewDecoder(w.Body).Decode(&res)
	if res.User.Role != "admin" {
		t.Errorf("expected role 'admin' for first user, got '%s'", res.User.Role)
	}
}

func TestHandler_Register_RestrictedWhenUsersExist(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, "test-secret", false)

	// Seed first user
	firstUser := models.User{
		Username:     "existingadmin",
		Email:        "admin@test.com",
		PasswordHash: "xyz",
		Role:         "admin",
	}
	db.Create(&firstUser)

	// Attempt unauthenticated registration with AllowRegistration = false
	reqBody, _ := json.Marshal(models.RegisterRequest{
		Username: "newuser",
		Email:    "new@test.com",
		Password: "password123",
	})
	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()

	h.Register(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 Forbidden, got %d: %s", w.Code, w.Body.String())
	}

	// Attempt with AllowRegistration = true
	h.AllowRegistration = true
	req2 := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(reqBody))
	w2 := httptest.NewRecorder()
	h.Register(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created with AllowRegistration, got %d: %s", w2.Code, w2.Body.String())
	}

	var res models.AuthResponse
	_ = json.NewDecoder(w2.Body).Decode(&res)
	if res.User.Role != "viewer" {
		t.Errorf("expected public registered user to have 'viewer' role, got '%s'", res.User.Role)
	}
}

func TestHandler_ChangePassword(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, "test-secret", false)

	hash, _ := bcrypt.GenerateFromPassword([]byte("oldpassword123"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: string(hash),
		Role:         "operator",
	}
	db.Create(&user)

	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
	}

	// 1. Wrong current password
	badReqBody, _ := json.Marshal(models.ChangePasswordRequest{
		CurrentPassword: "wrongpassword",
		NewPassword:     "newpassword123",
	})
	req1 := httptest.NewRequest("POST", "/api/auth/change-password", bytes.NewReader(badReqBody))
	req1 = req1.WithContext(context.WithValue(req1.Context(), UserContextKey, claims))
	w1 := httptest.NewRecorder()

	h.ChangePassword(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for wrong password, got %d", w1.Code)
	}

	// 2. Short new password
	shortReqBody, _ := json.Marshal(models.ChangePasswordRequest{
		CurrentPassword: "oldpassword123",
		NewPassword:     "short",
	})
	req2 := httptest.NewRequest("POST", "/api/auth/change-password", bytes.NewReader(shortReqBody))
	req2 = req2.WithContext(context.WithValue(req2.Context(), UserContextKey, claims))
	w2 := httptest.NewRecorder()

	h.ChangePassword(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for short password, got %d", w2.Code)
	}

	// 3. Successful change
	goodReqBody, _ := json.Marshal(models.ChangePasswordRequest{
		CurrentPassword: "oldpassword123",
		NewPassword:     "freshNewPassword123",
	})
	req3 := httptest.NewRequest("POST", "/api/auth/change-password", bytes.NewReader(goodReqBody))
	req3 = req3.WithContext(context.WithValue(req3.Context(), UserContextKey, claims))
	w3 := httptest.NewRecorder()

	h.ChangePassword(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for successful password change, got %d: %s", w3.Code, w3.Body.String())
	}

	// Verify new password in DB
	var updated models.User
	db.First(&updated, user.ID)
	if err := bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("freshNewPassword123")); err != nil {
		t.Errorf("failed to verify updated password hash: %v", err)
	}
}
