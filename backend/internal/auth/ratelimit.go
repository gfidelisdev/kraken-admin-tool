package auth

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// IPRateLimiter tracks token-bucket limiters per client IP address
type IPRateLimiter struct {
	mu      sync.Mutex
	ips     map[string]*ipLimiter
	rate    rate.Limit
	burst   int
	cleanup time.Duration
}

// NewIPRateLimiter creates an IP-based rate limiter
// Example: NewIPRateLimiter(rate.Every(time.Second*6), 5) -> allows burst of 5, then 1 request per 6s (10/min)
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	limiter := &IPRateLimiter{
		ips:     make(map[string]*ipLimiter),
		rate:    r,
		burst:   b,
		cleanup: 5 * time.Minute,
	}
	go limiter.cleanupLoop()
	return limiter
}

func (i *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	lim, exists := i.ips[ip]
	if !exists {
		lim = &ipLimiter{
			limiter:  rate.NewLimiter(i.rate, i.burst),
			lastSeen: time.Now(),
		}
		i.ips[ip] = lim
		return lim.limiter
	}

	lim.lastSeen = time.Now()
	return lim.limiter
}

func (i *IPRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(i.cleanup)
	for range ticker.C {
		i.mu.Lock()
		cutoff := time.Now().Add(-10 * time.Minute)
		for ip, lim := range i.ips {
			if lim.lastSeen.Before(cutoff) {
				delete(i.ips, ip)
			}
		}
		i.mu.Unlock()
	}
}

// Middleware returns a Chi/net/http middleware enforcing the rate limit
func (i *IPRateLimiter) Middleware(message string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)
			lim := i.getLimiter(ip)
			if !lim.Allow() {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(http.StatusTooManyRequests)
				if message == "" {
					message = "Too many requests. Please wait before trying again."
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		return strings.TrimSpace(xrip)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
