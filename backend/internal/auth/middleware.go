package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
)

type contextKey string

const UserContextKey contextKey = "user"

var sessionValidator atomic.Pointer[func(*Claims) bool]

// SetSessionValidator registers a check that rejects otherwise-valid JWTs whose backing server-side session
// is gone (e.g. a Microsoft broker session lost to expiry or a backend restart), forcing the client to log in again.
func SetSessionValidator(fn func(*Claims) bool) {
	if fn == nil {
		sessionValidator.Store(nil)
		return
	}
	sessionValidator.Store(&fn)
}

func isSessionValid(claims *Claims) bool {
	if fn := sessionValidator.Load(); fn != nil {
		return (*fn)(claims)
	}
	return true
}

// AuthMiddleware creates a Chi-compatible middleware that validates JWT tokens
// Supports token in Authorization header (Bearer <token>) or a single-use ?ticket= (for WebSockets)
func AuthMiddleware(secret string) func(http.Handler) http.Handler {
	return AuthMiddlewareWithProvider(func() string { return secret })
}

// AuthMiddlewareWithProvider validates JWT tokens using a dynamic secret provider so live JWT_SECRET rotation takes effect immediately.
func AuthMiddlewareWithProvider(secretProvider func() string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenString := ""

			// Check single-use WebSocket ticket (?ticket=)
			if ticket := r.URL.Query().Get("ticket"); ticket != "" {
				claims := DefaultTicketStore.ConsumeTicket(ticket)
				if claims != nil && isSessionValid(claims) {
					ctx := context.WithValue(r.Context(), UserContextKey, claims)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Check Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" {
				parts := strings.SplitN(authHeader, " ", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
					tokenString = strings.TrimSpace(parts[1])
				}
			}

			if tokenString == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Authorization token required"})
				return
			}

			claims, err := ValidateToken(tokenString, secretProvider())
			if err == nil && DefaultRevocations.IsRevoked(claims) {
				err = errors.New("token revoked")
			}
			if err == nil && !isSessionValid(claims) {
				err = errors.New("session expired")
			}
			if err != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Invalid or expired authorization token"})
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRoles ensures that the authenticated user possesses at least one of the specified roles
func RequireRoles(allowedRoles ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims := GetUserFromContext(r.Context())
			if claims == nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Authentication required"})
				return
			}
			for _, role := range allowedRoles {
				if strings.EqualFold(claims.Role, role) {
					next.ServeHTTP(w, r)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "Insufficient permissions: required role not held"})
		})
	}
}

// RequireRole ensures that the authenticated user possesses the specified role
func RequireRole(requiredRole string) func(http.Handler) http.Handler {
	return RequireRoles(requiredRole)
}

// ExtractTokenClaims extracts and validates JWT claims from request headers or query params
func ExtractTokenClaims(r *http.Request, secret string) *Claims {
	if ticket := r.URL.Query().Get("ticket"); ticket != "" {
		if claims := DefaultTicketStore.ConsumeTicket(ticket); claims != nil {
			return claims
		}
	}

	tokenString := ""
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			tokenString = strings.TrimSpace(parts[1])
		}
	}
	if tokenString == "" {
		return nil
	}
	claims, err := ValidateToken(tokenString, secret)
	if err != nil || DefaultRevocations.IsRevoked(claims) || !isSessionValid(claims) {
		return nil
	}
	return claims
}

// GetUserFromContext retrieves authenticated user claims from context
func GetUserFromContext(ctx context.Context) *Claims {
	if ctx == nil {
		return nil
	}
	if claims, ok := ctx.Value(UserContextKey).(*Claims); ok {
		return claims
	}
	return nil
}

// WithUserContext returns a new context with user claims attached
func WithUserContext(ctx context.Context, claims *Claims) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, UserContextKey, claims)
}
