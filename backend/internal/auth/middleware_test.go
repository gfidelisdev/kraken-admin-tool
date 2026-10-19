package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireRoles(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	tests := []struct {
		name         string
		claims       *Claims
		allowedRoles []string
		expectedCode int
	}{
		{
			name:         "Admin user accessing admin-only endpoint",
			claims:       &Claims{Username: "alice", Role: "admin"},
			allowedRoles: []string{"admin"},
			expectedCode: http.StatusOK,
		},
		{
			name:         "Operator user accessing admin-only endpoint",
			claims:       &Claims{Username: "bob", Role: "operator"},
			allowedRoles: []string{"admin"},
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "Viewer user accessing operator-or-admin endpoint",
			claims:       &Claims{Username: "carol", Role: "viewer"},
			allowedRoles: []string{"admin", "operator"},
			expectedCode: http.StatusForbidden,
		},
		{
			name:         "Operator user accessing operator-or-admin endpoint",
			claims:       &Claims{Username: "bob", Role: "operator"},
			allowedRoles: []string{"admin", "operator"},
			expectedCode: http.StatusOK,
		},
		{
			name:         "Unauthenticated user accessing protected endpoint",
			claims:       nil,
			allowedRoles: []string{"admin"},
			expectedCode: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.claims != nil {
				ctx := context.WithValue(req.Context(), UserContextKey, tt.claims)
				req = req.WithContext(ctx)
			}
			rec := httptest.NewRecorder()

			middleware := RequireRoles(tt.allowedRoles...)
			handler := middleware(dummyHandler)
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectedCode {
				t.Errorf("expected status code %d, got %d (body: %s)", tt.expectedCode, rec.Code, rec.Body.String())
			}
		})
	}
}
