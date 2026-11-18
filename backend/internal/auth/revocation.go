package auth

import (
	"sync"
	"time"
)

// AccessTokenTTL is the lifetime of an access token. Sessions are kept alive via /api/auth/refresh.
const AccessTokenTTL = 15 * time.Minute

// TokenRevocations tracks revoked JWTs by jti (until their exp) and per-user cutoffs.
type TokenRevocations struct {
	mu          sync.RWMutex
	jtis        map[string]time.Time
	userCutoffs map[uint]time.Time
}

// DefaultRevocations is the process-wide denylist consulted by the auth middleware.
var DefaultRevocations = NewTokenRevocations()

func NewTokenRevocations() *TokenRevocations {
	return &TokenRevocations{
		jtis:        make(map[string]time.Time),
		userCutoffs: make(map[uint]time.Time),
	}
}

// RevokeToken denies the given jti until expiresAt, after which the token is invalid anyway.
func (r *TokenRevocations) RevokeToken(jti string, expiresAt time.Time) {
	if jti == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for id, exp := range r.jtis {
		if !exp.After(now) {
			delete(r.jtis, id)
		}
	}
	r.jtis[jti] = expiresAt
}

// RevokeUserTokens invalidates every token of the user issued at or before now.
func (r *TokenRevocations) RevokeUserTokens(userID uint) {
	if userID == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userCutoffs[userID] = time.Now()
}

// IsRevoked reports whether the claims belong to a revoked token.
func (r *TokenRevocations) IsRevoked(claims *Claims) bool {
	if claims == nil {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if claims.ID != "" {
		if exp, ok := r.jtis[claims.ID]; ok && exp.After(time.Now()) {
			return true
		}
	}
	if cutoff, ok := r.userCutoffs[claims.UserID]; ok {
		if claims.IssuedAt == nil || claims.IssuedAt.Time.Unix() <= cutoff.Unix() {
			return true
		}
	}
	return false
}
