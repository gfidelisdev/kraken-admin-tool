package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type contextKey string

const UserContextKey contextKey = "user"

// AuthMiddleware creates a Chi-compatible middleware that validates JWT tokens
// Supports token in Authorization header (Bearer <token>) or ?token=<token> query parameter (for WebSockets)
func AuthMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenString := ""

			// Check single-use WebSocket ticket (?ticket=)
			if ticket := r.URL.Query().Get("ticket"); ticket != "" {
				claims := DefaultTicketStore.ConsumeTicket(ticket)
				if claims != nil {
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

			// Fallback: Check query param ?token= (legacy support for browser WebSocket connections)
			if tokenString == "" {
				tokenString = r.URL.Query().Get("token")
			}

			if tokenString == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "Authorization token required"})
				return
			}

			claims, err := ValidateToken(tokenString, secret)
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
		tokenString = r.URL.Query().Get("token")
	}
	if tokenString == "" {
		return nil
	}
	claims, err := ValidateToken(tokenString, secret)
	if err != nil {
		return nil
	}
	return claims
}

// GetUserFromContext retrieves authenticated user claims from context
func GetUserFromContext(ctx context.Context) *Claims {
	if claims, ok := ctx.Value(UserContextKey).(*Claims); ok {
		return claims
	}
	return nil
}
