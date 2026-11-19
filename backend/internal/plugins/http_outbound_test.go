package plugins

import (
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOutboundHTTP_ProxiesAllowedHost(t *testing.T) {
	orig := outboundIPBlocked
	outboundIPBlocked = func(net.IP) bool { return false }
	defer func() { outboundIPBlocked = orig }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept header not forwarded: %q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"sensor":"rack-1","temp":22.5}]`))
	}))
	defer srv.Close()

	res, err := executeOutboundHTTP(context.Background(), []string{"127.0.0.1"}, map[string]interface{}{
		"url":     srv.URL + "/readings",
		"headers": map[string]interface{}{"Accept": "application/json", "Host": "evil"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	body, _ := base64.StdEncoding.DecodeString(res.BodyBase64)
	if res.Status != 200 || !strings.Contains(string(body), "rack-1") || res.Headers["content-type"] != "application/json" {
		t.Fatalf("unexpected response: %+v body=%s", res, body)
	}
}

func TestOutboundHTTP_RejectsDisallowedDestinations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	cases := []struct {
		name    string
		allowed []string
		url     string
	}{
		{"host not in allowed_hosts", []string{"sensors.example.com"}, srv.URL},
		{"no allowed_hosts", nil, srv.URL},
		{"loopback blocked even with wildcard", []string{"*"}, srv.URL},
		{"metadata endpoint blocked", []string{"*"}, "http://169.254.169.254/latest/meta-data"},
		{"non-http scheme", []string{"*"}, "file:///etc/passwd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := executeOutboundHTTP(context.Background(), tc.allowed, map[string]interface{}{"url": tc.url}); err == nil {
				t.Fatal("expected request to be rejected")
			}
		})
	}
}

func TestOutboundHTTP_RedirectMustStayOnAllowedHosts(t *testing.T) {
	orig := outboundIPBlocked
	outboundIPBlocked = func(net.IP) bool { return false }
	defer func() { outboundIPBlocked = orig }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://other.internal/secret", http.StatusFound)
	}))
	defer srv.Close()

	if _, err := executeOutboundHTTP(context.Background(), []string{"127.0.0.1"}, map[string]interface{}{"url": srv.URL}); err == nil {
		t.Fatal("expected redirect to a non-allowed host to fail")
	}
}

func TestHostAllowedWildcardSubdomain(t *testing.T) {
	mk := func(raw string) *http.Request { r, _ := http.NewRequest("GET", raw, nil); return r }
	allowed := []string{"*.dc.example.com"}
	if !hostAllowed(allowed, mk("https://sensor1.dc.example.com/x").URL) {
		t.Fatal("subdomain should match wildcard")
	}
	if hostAllowed(allowed, mk("https://dc.example.com.evil.io/x").URL) || hostAllowed(allowed, mk("https://dc.example.com/x").URL) {
		t.Fatal("wildcard must only match true subdomains")
	}
}
