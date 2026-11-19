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

func TestMicrosoftTokenRejectedWhenBrokerSessionGone(t *testing.T) {
	secret := "test-secret"
	broker := NewMicrosoftSessionBroker()
	SetSessionValidator(func(c *Claims) bool {
		return c.AuthProvider != "microsoft" || broker.IsSessionActive(c.ID)
	})
	t.Cleanup(func() { SetSessionValidator(nil) })

	user := &models.User{Username: "ms", Role: "viewer"}
	user.ID = 77
	sessionID := broker.CreateSession(user.ID, "ms@example.com", "MS", "", "", "rt", "", "", 0, time.Minute)
	token, err := GenerateTokenWithSession(user, secret, AccessTokenTTL, "microsoft", sessionID)
	if err != nil {
		t.Fatal(err)
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
		t.Fatalf("expected 200 with live broker session, got %d", code)
	}
	broker.DeleteSession(sessionID)
	if code := call(); code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after broker session loss, got %d", code)
	}
}

func TestMicrosoftSessionExtendSlidesExpiry(t *testing.T) {
	broker := NewMicrosoftSessionBroker()
	sessionID := broker.CreateSession(1, "a@b.c", "A", "", "", "rt", "", "", 0, 50*time.Millisecond)
	if !broker.ExtendSession(sessionID, time.Hour) {
		t.Fatal("expected active session to be extended")
	}
	time.Sleep(80 * time.Millisecond)
	if !broker.IsSessionActive(sessionID) {
		t.Fatal("expected session to survive past its original TTL after extension")
	}
	if broker.ExtendSession("missing", time.Hour) {
		t.Fatal("expected extension of unknown session to fail")
	}
}
