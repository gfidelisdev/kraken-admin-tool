package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/kraken/admin-tool/backend/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var dbCounter uint64

func setupTestDB(t *testing.T) *gorm.DB {
	idx := atomic.AddUint64(&dbCounter, 1)
	dsn := fmt.Sprintf("file:memdb%d?mode=memory&cache=shared", idx)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to connect database: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.AuditLog{}, &models.PasswordResetRequest{}); err != nil {
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
	if res.User.Status != "active" {
		t.Errorf("expected status 'active' for bootstrap admin, got '%s'", res.User.Status)
	}
	if res.Token == "" {
		t.Errorf("expected token for bootstrap admin, got empty")
	}
}

func TestHandler_Register_PublicUserPendingApproval(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, "test-secret", false)

	// Seed existing admin
	firstUser := models.User{
		Username:     "existingadmin",
		Email:        "admin@test.com",
		PasswordHash: "xyz",
		Role:         "admin",
		Status:       "active",
	}
	db.Create(&firstUser)

	// Public self-registration attempt
	reqBody, _ := json.Marshal(models.RegisterRequest{
		Username: "pendinguser",
		Email:    "pending@test.com",
		Password: "password123",
	})
	req := httptest.NewRequest("POST", "/api/auth/register", bytes.NewReader(reqBody))
	w := httptest.NewRecorder()

	h.Register(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created for registration, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "pending" {
		t.Errorf("expected status 'pending', got %v", resp["status"])
	}

	// Try logging in before approval -> MUST be forbidden with pending message
	loginBody, _ := json.Marshal(models.LoginRequest{
		Username: "pendinguser",
		Password: "password123",
	})
	loginReq := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	h.Login(loginRec, loginReq)

	if loginRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for pending user login, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
}

func TestHandler_AdminApproveUser_WithRole(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, "test-secret", false)

	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	pendingUser := models.User{
		Username:     "alice",
		Email:        "alice@test.local",
		PasswordHash: string(hash),
		Role:         "pending",
		Status:       "pending",
	}
	db.Create(&pendingUser)

	adminClaims := &Claims{
		UserID:   1,
		Username: "admin",
		Role:     "admin",
	}

	// Admin approves user and attributes "operator" role
	approveBody, _ := json.Marshal(models.ApproveUserRequest{
		Role: "operator",
	})
	r := chi.NewRouter()
	r.Post("/api/auth/users/{id}/approve", h.ApproveUser)

	approveReq := httptest.NewRequest("POST", fmt.Sprintf("/api/auth/users/%d/approve", pendingUser.ID), bytes.NewReader(approveBody))
	approveReq = approveReq.WithContext(context.WithValue(approveReq.Context(), UserContextKey, adminClaims))
	approveRec := httptest.NewRecorder()

	r.ServeHTTP(approveRec, approveReq)

	if approveRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for approval, got %d: %s", approveRec.Code, approveRec.Body.String())
	}

	var updatedUser models.User
	db.First(&updatedUser, pendingUser.ID)
	if updatedUser.Status != "active" {
		t.Errorf("expected status 'active', got '%s'", updatedUser.Status)
	}
	if updatedUser.Role != "operator" {
		t.Errorf("expected attributed role 'operator', got '%s'", updatedUser.Role)
	}

	// Now alice can log in
	loginBody, _ := json.Marshal(models.LoginRequest{
		Username: "alice",
		Password: "password123",
	})
	loginReq := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	h.Login(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for approved user login, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	var loginRes models.AuthResponse
	_ = json.NewDecoder(loginRec.Body).Decode(&loginRes)
	if loginRes.User.Role != "operator" {
		t.Errorf("expected user role 'operator', got '%s'", loginRes.User.Role)
	}
}

func TestHandler_AdminRejectUser(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, "test-secret", false)

	adminUser := models.User{
		Username:     "admin",
		Email:        "admin@test.local",
		PasswordHash: "xyz",
		Role:         "admin",
		Status:       "active",
	}
	db.Create(&adminUser)

	hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
	pendingUser := models.User{
		Username:     "bob",
		Email:        "bob@test.local",
		PasswordHash: string(hash),
		Role:         "pending",
		Status:       "pending",
	}
	db.Create(&pendingUser)

	adminClaims := &Claims{
		UserID:   adminUser.ID,
		Username: "admin",
		Role:     "admin",
	}

	r := chi.NewRouter()
	r.Post("/api/auth/users/{id}/reject", h.RejectUser)

	rejectReq := httptest.NewRequest("POST", fmt.Sprintf("/api/auth/users/%d/reject", pendingUser.ID), nil)
	rejectReq = rejectReq.WithContext(context.WithValue(rejectReq.Context(), UserContextKey, adminClaims))
	rejectRec := httptest.NewRecorder()

	r.ServeHTTP(rejectRec, rejectReq)

	if rejectRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for rejection, got %d: %s", rejectRec.Code, rejectRec.Body.String())
	}

	var user models.User
	db.First(&user, pendingUser.ID)
	if user.Status != "rejected" {
		t.Errorf("expected status 'rejected', got '%s'", user.Status)
	}

	// Login attempt must fail
	loginBody, _ := json.Marshal(models.LoginRequest{
		Username: "bob",
		Password: "password123",
	})
	loginReq := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	h.Login(loginRec, loginReq)

	if loginRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for rejected user login, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
}

func TestHandler_PasswordRecovery_Flow(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, "test-secret", false)

	hash, _ := bcrypt.GenerateFromPassword([]byte("oldpassword123"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "carol",
		Email:        "carol@kraken.local",
		PasswordHash: string(hash),
		Role:         "operator",
		Status:       "active",
	}
	db.Create(&user)

	// 1. User requests password recovery from admin
	recoveryBody, _ := json.Marshal(models.RecoverPasswordRequest{
		UsernameOrEmail: "carol@kraken.local",
		Note:            "Forgot my password on Monday",
	})
	recReq := httptest.NewRequest("POST", "/api/auth/recover-password", bytes.NewReader(recoveryBody))
	recRes := httptest.NewRecorder()
	h.RequestPasswordRecovery(recRes, recReq)

	if recRes.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for recovery request, got %d: %s", recRes.Code, recRes.Body.String())
	}

	// 2. Admin lists password recovery requests
	listReq := httptest.NewRequest("GET", "/api/auth/password-recovery-requests", nil)
	listRec := httptest.NewRecorder()
	h.ListPasswordRecoveryRequests(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for list requests, got %d: %s", listRec.Code, listRec.Body.String())
	}

	var requests []models.PasswordResetRequest
	_ = json.NewDecoder(listRec.Body).Decode(&requests)
	if len(requests) != 1 || requests[0].Username != "carol" {
		t.Fatalf("expected 1 recovery request for carol, got %v", requests)
	}

	// 3. Admin resolves request by setting new password
	adminClaims := &Claims{
		UserID:   1,
		Username: "admin",
		Role:     "admin",
	}

	r := chi.NewRouter()
	r.Post("/api/auth/password-recovery-requests/{id}/resolve", h.ResolvePasswordRecoveryRequest)

	resolveBody, _ := json.Marshal(models.AdminResetPasswordRequest{
		NewPassword: "newsecretpassword123",
	})
	resolveReq := httptest.NewRequest("POST", fmt.Sprintf("/api/auth/password-recovery-requests/%d/resolve", requests[0].ID), bytes.NewReader(resolveBody))
	resolveReq = resolveReq.WithContext(context.WithValue(resolveReq.Context(), UserContextKey, adminClaims))
	resolveRec := httptest.NewRecorder()

	r.ServeHTTP(resolveRec, resolveReq)

	if resolveRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for resolving request, got %d: %s", resolveRec.Code, resolveRec.Body.String())
	}

	// 4. Carol logs in with newly assigned password
	loginBody, _ := json.Marshal(models.LoginRequest{
		Username: "carol",
		Password: "newsecretpassword123",
	})
	loginReq := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	h.Login(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for login with new password, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
}

func TestHandler_AdminResetPassword_AutoGenerate(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, "test-secret", false)

	hash, _ := bcrypt.GenerateFromPassword([]byte("originalpass123"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "david",
		Email:        "david@kraken.local",
		PasswordHash: string(hash),
		Role:         "viewer",
		Status:       "active",
	}
	db.Create(&user)

	adminClaims := &Claims{
		UserID:   1,
		Username: "admin",
		Role:     "admin",
	}

	r := chi.NewRouter()
	r.Post("/api/auth/users/{id}/reset-password", h.AdminResetPassword)

	// Admin calls reset-password with empty new_password to auto-generate
	resetReq := httptest.NewRequest("POST", fmt.Sprintf("/api/auth/users/%d/reset-password", user.ID), bytes.NewReader([]byte("{}")))
	resetReq = resetReq.WithContext(context.WithValue(resetReq.Context(), UserContextKey, adminClaims))
	resetRec := httptest.NewRecorder()

	r.ServeHTTP(resetRec, resetReq)

	if resetRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for auto-generated password reset, got %d: %s", resetRec.Code, resetRec.Body.String())
	}

	var res map[string]interface{}
	_ = json.NewDecoder(resetRec.Body).Decode(&res)
	generatedPass, ok := res["new_password"].(string)
	if !ok || len(generatedPass) < 8 {
		t.Fatalf("expected generated password of length >= 8, got '%v'", generatedPass)
	}

	// Verify user can login with generated password
	loginBody, _ := json.Marshal(models.LoginRequest{
		Username: "david",
		Password: generatedPass,
	})
	loginReq := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	h.Login(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for login with auto-generated password, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
}

func TestHandler_MasterAdminRecovery(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, "my-super-secret-jwt-key-32-bytes-minimum!!", false)

	hash, _ := bcrypt.GenerateFromPassword([]byte("lostpass123"), bcrypt.DefaultCost)
	adminUser := models.User{
		Username:     "admin",
		Email:        "admin@kraken.local",
		PasswordHash: string(hash),
		Role:         "admin",
		Status:       "active",
	}
	db.Create(&adminUser)

	recoveryBody, _ := json.Marshal(models.MasterRecoveryRequest{
		Username:    "admin",
		NewPassword: "newemergencyadminpassword123",
	})

	// 1. Disabled by default when ADMIN_RECOVERY_KEY is not configured -> 403
	h.SetAdminRecoveryKey("")
	disabledReq := httptest.NewRequest("POST", "/api/auth/admin-recovery", bytes.NewReader(recoveryBody))
	disabledReq.Header.Set("X-Master-Key", "kraken-admin-master-recovery")
	disabledRec := httptest.NewRecorder()
	h.MasterAdminRecovery(disabledRec, disabledReq)
	if disabledRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden when ADMIN_RECOVERY_KEY is unset, got %d", disabledRec.Code)
	}

	// 2. Configure dedicated strong ADMIN_RECOVERY_KEY
	const dedicatedRecoveryKey = "dedicated-admin-recovery-key-at-least-32-chars-long!"
	h.SetAdminRecoveryKey(dedicatedRecoveryKey)

	// Attempt with wrong key (or JWT_SECRET) -> 401
	badReq := httptest.NewRequest("POST", "/api/auth/admin-recovery", bytes.NewReader(recoveryBody))
	badReq.Header.Set("X-Master-Key", "my-super-secret-jwt-key-32-bytes-minimum!!")
	badRec := httptest.NewRecorder()
	h.MasterAdminRecovery(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized when passing JWT_SECRET instead of ADMIN_RECOVERY_KEY, got %d", badRec.Code)
	}

	// 3. Attempt with valid dedicated ADMIN_RECOVERY_KEY -> 200
	goodReq := httptest.NewRequest("POST", "/api/auth/admin-recovery", bytes.NewReader(recoveryBody))
	goodReq.Header.Set("X-Master-Key", dedicatedRecoveryKey)
	goodRec := httptest.NewRecorder()
	h.MasterAdminRecovery(goodRec, goodReq)
	if goodRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid master recovery, got %d: %s", goodRec.Code, goodRec.Body.String())
	}

	// Login with recovered admin password
	loginBody, _ := json.Marshal(models.LoginRequest{
		Username: "admin",
		Password: "newemergencyadminpassword123",
	})
	loginReq := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginBody))
	loginRec := httptest.NewRecorder()
	h.Login(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for admin login after recovery, got %d: %s", loginRec.Code, loginRec.Body.String())
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
		Status:       "active",
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
