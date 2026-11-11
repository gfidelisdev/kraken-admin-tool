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
	mu             sync.Mutex
	ips            map[string]*ipLimiter
	rate           rate.Limit
	burst          int
	cleanup        time.Duration
	trustedProxies []*net.IPNet
	trustedIPs     map[string]bool
}

// NewIPRateLimiter creates an IP-based rate limiter
// Example: NewIPRateLimiter(rate.Every(time.Second*6), 5) -> allows burst of 5, then 1 request per 6s (10/min)
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	limiter := &IPRateLimiter{
		ips:        make(map[string]*ipLimiter),
		rate:       r,
		burst:      b,
		cleanup:    5 * time.Minute,
		trustedIPs: make(map[string]bool),
	}
	go limiter.cleanupLoop()
	return limiter
}

// SetTrustedProxies configures reverse proxy IPs or CIDRs allowed to supply X-Forwarded-For / X-Real-IP
func (i *IPRateLimiter) SetTrustedProxies(proxies []string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.trustedProxies = nil
	i.trustedIPs = make(map[string]bool)
	for _, entry := range proxies {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if _, cidr, err := net.ParseCIDR(entry); err == nil {
			i.trustedProxies = append(i.trustedProxies, cidr)
			continue
		}
		if ip := net.ParseIP(entry); ip != nil {
			i.trustedIPs[ip.String()] = true
		}
	}
}

func (i *IPRateLimiter) isTrustedProxy(remoteHost string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.trustedProxies) == 0 && len(i.trustedIPs) == 0 {
		return false
	}
	ip := net.ParseIP(remoteHost)
	if ip == nil {
		return false
	}
	if i.trustedIPs[ip.String()] {
		return true
	}
	for _, cidr := range i.trustedProxies {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
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
			ip := i.extractClientIP(r)
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

func (i *IPRateLimiter) extractClientIP(r *http.Request) string {
	remoteHost := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remoteHost = host
	}

	if i.isTrustedProxy(remoteHost) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if candidate := strings.TrimSpace(parts[0]); candidate != "" {
				return candidate
			}
		}
		if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
			return xrip
		}
	}

	return remoteHost
}

func getClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
