package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kraken/admin-tool/backend/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func TestTOTP_Lifecycle(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret failed: %v", err)
	}
	if len(secret) < 16 {
		t.Fatalf("secret too short: %s", secret)
	}

	authURL := GenerateTOTPAuthURL("Kraken", "testuser", secret)
	if !strings.HasPrefix(authURL, "otpauth://totp/Kraken:testuser?") {
		t.Errorf("unexpected authURL format: %s", authURL)
	}
	if !strings.Contains(authURL, "secret="+secret) {
		t.Errorf("authURL missing secret: %s", authURL)
	}

	now := time.Now()
	code, err := ComputeTOTP(secret, now)
	if err != nil {
		t.Fatalf("ComputeTOTP failed: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("expected 6 digits, got: %s", code)
	}

	// Code should validate currently
	if !ValidateTOTPCode(secret, code) {
		t.Errorf("expected code %s to be valid for secret %s", code, secret)
	}

	// Code from 2 minutes ago should fail (outside ±1 window)
	oldCode, err := ComputeTOTP(secret, now.Add(-120*time.Second))
	if err == nil && oldCode != code {
		if ValidateTOTPCode(secret, oldCode) {
			t.Errorf("expected 2-minute old code to be rejected, but was accepted")
		}
	}

	// Bogus code should fail
	if ValidateTOTPCode(secret, "999999") && code != "999999" {
		t.Errorf("expected bogus code to be rejected")
	}

	// Non-6-digit code should fail
	if ValidateTOTPCode(secret, "123") || ValidateTOTPCode(secret, "abcdef") {
		t.Errorf("expected invalid format code to be rejected")
	}
}

func TestOTP_EndpointsAndRecovery(t *testing.T) {
	db := setupTestDB(t)
	h := NewHandler(db, "test-secret", false)

	hash, _ := bcrypt.GenerateFromPassword([]byte("old-password-123"), bcrypt.DefaultCost)
	user := models.User{
		Username:     "bob",
		Email:        "bob@test.com",
		PasswordHash: string(hash),
		Role:         "operator",
		Status:       "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}

	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
	}

	// 1. Setup OTP
	setupReq := httptest.NewRequest("GET", "/api/auth/otp/setup", nil)
	setupReq = setupReq.WithContext(context.WithValue(setupReq.Context(), UserContextKey, claims))
	setupRec := httptest.NewRecorder()
	h.SetupOTP(setupRec, setupReq)

	if setupRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on SetupOTP, got %d: %s", setupRec.Code, setupRec.Body.String())
	}

	var setupRes models.OTPSetupResponse
	_ = json.NewDecoder(setupRec.Body).Decode(&setupRes)
	if setupRes.Secret == "" || !strings.Contains(setupRes.AuthURL, "otpauth://") {
		t.Fatalf("invalid setup response: %+v", setupRes)
	}

	// 2. Verify with wrong code should fail
	verifyReqBody, _ := json.Marshal(models.OTPVerifyRequest{Code: "000000"})
	verifyReq := httptest.NewRequest("POST", "/api/auth/otp/verify-setup", bytes.NewReader(verifyReqBody))
	verifyReq = verifyReq.WithContext(context.WithValue(verifyReq.Context(), UserContextKey, claims))
	verifyRec := httptest.NewRecorder()
	h.VerifyOTPSetup(verifyRec, verifyReq)

	if verifyRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request with wrong code, got %d", verifyRec.Code)
	}

	// 3. Verify with valid code should succeed
	validCode, cErr := ComputeTOTP(setupRes.Secret, time.Now())
	if cErr != nil {
		t.Fatalf("failed to compute TOTP code: %v", cErr)
	}

	verifyReqBody, _ = json.Marshal(models.OTPVerifyRequest{Code: validCode})
	verifyReq = httptest.NewRequest("POST", "/api/auth/otp/verify-setup", bytes.NewReader(verifyReqBody))
	verifyReq = verifyReq.WithContext(context.WithValue(verifyReq.Context(), UserContextKey, claims))
	verifyRec = httptest.NewRecorder()
	h.VerifyOTPSetup(verifyRec, verifyReq)

	if verifyRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK with valid code, got %d: %s", verifyRec.Code, verifyRec.Body.String())
	}

	// Check DB user has OTP enabled
	var updatedUser models.User
	db.First(&updatedUser, user.ID)
	if !updatedUser.OTPEnabled || updatedUser.OTPSecret == "" {
		t.Fatalf("user does not have OTPEnabled in DB: %+v", updatedUser)
	}

	// 4. Recover password via OTP
	recoverCode, _ := ComputeTOTP(setupRes.Secret, time.Now())
	recoverReqBody, _ := json.Marshal(models.RecoverPasswordOTPRequest{
		UsernameOrEmail: "bob",
		Code:            recoverCode,
		NewPassword:     "new-secret-password-456",
	})
	recoverReq := httptest.NewRequest("POST", "/api/auth/recover-password/otp", bytes.NewReader(recoverReqBody))
	recoverRec := httptest.NewRecorder()
	h.RecoverPasswordViaOTP(recoverRec, recoverReq)

	if recoverRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on RecoverPasswordViaOTP, got %d: %s", recoverRec.Code, recoverRec.Body.String())
	}

	// Verify login works with new password
	loginReqBody, _ := json.Marshal(models.LoginRequest{
		Username: "bob",
		Password: "new-secret-password-456",
	})
	loginReq := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(loginReqBody))
	loginRec := httptest.NewRecorder()
	h.Login(loginRec, loginReq)

	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on Login with new password, got %d: %s", loginRec.Code, loginRec.Body.String())
	}

	var authRes models.AuthResponse
	_ = json.NewDecoder(loginRec.Body).Decode(&authRes)
	if !authRes.User.OTPEnabled {
		t.Errorf("expected User.OTPEnabled to be true in login response")
	}

	// 5. Disable OTP
	disableCode, _ := ComputeTOTP(setupRes.Secret, time.Now())
	disableReqBody, _ := json.Marshal(models.OTPDisableRequest{
		Password: "new-secret-password-456",
		Code:     disableCode,
	})
	disableReq := httptest.NewRequest("POST", "/api/auth/otp/disable", bytes.NewReader(disableReqBody))
	disableReq = disableReq.WithContext(context.WithValue(disableReq.Context(), UserContextKey, claims))
	disableRec := httptest.NewRecorder()
	h.DisableOTP(disableRec, disableReq)

	if disableRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on DisableOTP, got %d: %s", disableRec.Code, disableRec.Body.String())
	}

	db.First(&updatedUser, user.ID)
	if updatedUser.OTPEnabled {
		t.Fatalf("expected OTPEnabled to be false after disable")
	}
}
