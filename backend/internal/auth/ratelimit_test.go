package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestIPRateLimiter(t *testing.T) {
	// Allow 2 requests burst, then 1 every second
	limiter := NewIPRateLimiter(rate.Every(time.Second), 2)
	middleware := limiter.Middleware("Rate limit reached")

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	// Request 1: should pass
	req1 := httptest.NewRequest("POST", "/api/auth/login", nil)
	req1.RemoteAddr = "192.0.2.1:1234"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec1.Code)
	}

	// Request 2: should pass (burst = 2)
	req2 := httptest.NewRequest("POST", "/api/auth/login", nil)
	req2.RemoteAddr = "192.0.2.1:1234"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec2.Code)
	}

	// Request 3: should be blocked (burst exhausted)
	req3 := httptest.NewRequest("POST", "/api/auth/login", nil)
	req3.RemoteAddr = "192.0.2.1:1234"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 Too Many Requests, got %d", rec3.Code)
	}

	// Request from different IP should pass
	reqOther := httptest.NewRequest("POST", "/api/auth/login", nil)
	reqOther.RemoteAddr = "192.0.2.2:1234"
	recOther := httptest.NewRecorder()
	handler.ServeHTTP(recOther, reqOther)
	if recOther.Code != http.StatusOK {
		t.Errorf("expected 200 for different IP, got %d", recOther.Code)
	}
}
