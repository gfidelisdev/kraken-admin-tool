package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kraken/admin-tool/backend/internal/models"
)

func TestRevokedTokenRejectedByMiddleware(t *testing.T) {
	secret := "test-secret"
	user := &models.User{Username: "u", Role: "viewer"}
	user.ID = 42
	token, err := GenerateToken(user, secret, AccessTokenTTL)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateToken(token, secret)
	if err != nil || claims.ID == "" {
		t.Fatalf("expected token with jti, got %v %v", claims, err)
	}

	h := AuthMiddleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	call := func() int {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := call(); code != http.StatusOK {
		t.Fatalf("expected 200 before revocation, got %d", code)
	}
	DefaultRevocations.RevokeToken(claims.ID, claims.ExpiresAt.Time)
	if code := call(); code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after revocation, got %d", code)
	}
}

func TestRevokeUserTokens(t *testing.T) {
	r := NewTokenRevocations()
	user := &models.User{Username: "u", Role: "viewer"}
	user.ID = 7
	token, _ := GenerateToken(user, "s", time.Minute)
	claims, _ := ValidateToken(token, "s")
	if r.IsRevoked(claims) {
		t.Fatal("unexpected revoked")
	}
	r.RevokeUserTokens(7)
	if !r.IsRevoked(claims) {
		t.Fatal("expected revoked after user cutoff")
	}
}

func TestQueryStringTokenRejected(t *testing.T) {
	secret := "test-secret"
	user := &models.User{Username: "u", Role: "viewer"}
	user.ID = 99
	token, _ := GenerateToken(user, secret, AccessTokenTTL)
	h := AuthMiddleware(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/?token="+token, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for ?token=, got %d", rec.Code)
	}
}
