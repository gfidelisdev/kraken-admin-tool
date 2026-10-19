package auth

import (
	"testing"
	"time"

	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestGenerateAndValidateToken(t *testing.T) {
	secret := "test-secret-key-12345"
	user := &models.User{
		ID:       42,
		Username: "operator",
		Role:     "admin",
	}

	token, err := GenerateToken(user, secret, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error generating token: %v", err)
	}

	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := ValidateToken(token, secret)
	if err != nil {
		t.Fatalf("unexpected error validating token: %v", err)
	}

	if claims.UserID != 42 {
		t.Errorf("expected UserID 42, got %d", claims.UserID)
	}
	if claims.Username != "operator" {
		t.Errorf("expected Username operator, got %s", claims.Username)
	}
	if claims.Role != "admin" {
		t.Errorf("expected Role admin, got %s", claims.Role)
	}
}

func TestValidateTokenInvalidSecret(t *testing.T) {
	secret := "secret-one"
	user := &models.User{ID: 1, Username: "test", Role: "admin"}

	token, err := GenerateToken(user, secret, 1*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error generating token: %v", err)
	}

	_, err = ValidateToken(token, "wrong-secret")
	if err == nil {
		t.Fatal("expected validation to fail with wrong secret, but succeeded")
	}
}

func TestValidateTokenExpired(t *testing.T) {
	secret := "secret-one"
	user := &models.User{ID: 1, Username: "test", Role: "admin"}

	// Generate already expired token
	token, err := GenerateToken(user, secret, -1*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error generating token: %v", err)
	}

	_, err = ValidateToken(token, secret)
	if err == nil {
		t.Fatal("expected validation to fail for expired token, but succeeded")
	}
}
