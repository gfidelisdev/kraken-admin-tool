package plugins

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Built-in action names that proxy an HTTP request on behalf of a plugin holding "http:outbound".
// Running the request server-side avoids the browser's CORS/CSP restrictions for plugin workers.
var httpOutboundActions = map[string]bool{
	"http.fetch":   true,
	"http_fetch":   true,
	"http.request": true,
}

const (
	outboundMaxResponseBytes = 5 << 20
	outboundMaxRequestBytes  = 1 << 20
	outboundDefaultTimeout   = 15 * time.Second
	outboundMaxTimeout       = 60 * time.Second
	outboundMaxRedirects     = 5
)

var outboundAllowedMethods = map[string]bool{
	http.MethodGet: true, http.MethodHead: true, http.MethodPost: true,
	http.MethodPut: true, http.MethodPatch: true, http.MethodDelete: true,
}

// Headers a plugin must not control (hop-by-hop or connection-level).
var outboundForbiddenHeaders = map[string]bool{
	"host": true, "connection": true, "content-length": true, "transfer-encoding": true,
	"upgrade": true, "te": true, "trailer": true, "keep-alive": true, "proxy-authorization": true,
	"proxy-connection": true,
}

type outboundRequest struct {
	URL       string
	Method    string
	Headers   map[string]string
	Body      []byte
	TimeoutMs int
}

// OutboundResponse is returned to the plugin worker; the body is base64-encoded so binary payloads survive JSON.
type OutboundResponse struct {
	Status     int               `json:"status"`
	StatusText string            `json:"status_text"`
	Headers    map[string]string `json:"headers"`
	BodyBase64 string            `json:"body_base64"`
	URL        string            `json:"url"`
}

func parseOutboundPayload(payload interface{}) (*outboundRequest, error) {
	m, ok := payload.(map[string]interface{})
	if !ok {
		return nil, errors.New("http.fetch requires an object payload {url, method?, headers?, body?}")
	}
	req := &outboundRequest{Method: http.MethodGet, Headers: map[string]string{}}
	req.URL, _ = m["url"].(string)
	if strings.TrimSpace(req.URL) == "" {
		return nil, errors.New("http.fetch requires 'url'")
	}
	if method, ok := m["method"].(string); ok && method != "" {
		req.Method = strings.ToUpper(method)
	}
	if !outboundAllowedMethods[req.Method] {
		return nil, fmt.Errorf("http method %q is not allowed", req.Method)
	}
	if hdrs, ok := m["headers"].(map[string]interface{}); ok {
		for k, v := range hdrs {
			if outboundForbiddenHeaders[strings.ToLower(k)] {
				continue
			}
			req.Headers[k] = fmt.Sprintf("%v", v)
		}
	}
	if b64, ok := m["body_base64"].(string); ok && b64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, errors.New("invalid body_base64")
		}
		req.Body = decoded
	} else if body, ok := m["body"].(string); ok {
		req.Body = []byte(body)
	}
	if len(req.Body) > outboundMaxRequestBytes {
		return nil, fmt.Errorf("request body exceeds %d bytes", outboundMaxRequestBytes)
	}
	if t, ok := m["timeout_ms"].(float64); ok && t > 0 {
		req.TimeoutMs = int(t)
	}
	return req, nil
}

// hostAllowed checks a hostname against manifest allowed_hosts entries:
// "*" (any host), exact hostnames, "host:port", or "*.example.com" wildcards.
func hostAllowed(allowed []string, u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	hostPort := strings.ToLower(u.Host)
	for _, entry := range allowed {
		e := strings.ToLower(strings.TrimSpace(entry))
		switch {
		case e == "":
			continue
		case e == "*":
			return true
		case strings.HasPrefix(e, "*."):
			suffix := e[1:]
			if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
				return true
			}
		case e == host || e == hostPort:
			return true
		}
	}
	return false
}

// isBlockedOutboundIP rejects destinations that would let a plugin reach the Kraken host itself or
// cloud metadata endpoints. Private ranges stay allowed because intranet sensor/appliance APIs are a core use case.
// outboundIPBlocked is a variable so tests can reach httptest servers on loopback.
var outboundIPBlocked = isBlockedOutboundIP

func isBlockedOutboundIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast()
}

func validateOutboundURL(allowed []string, u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme %q (only http/https)", u.Scheme)
	}
	if u.Hostname() == "" {
		return errors.New("URL has no host")
	}
	if !hostAllowed(allowed, u) {
		return fmt.Errorf("host %q is not listed in the plugin's allowed_hosts", u.Hostname())
	}
	return nil
}

func newOutboundClient(allowed []string) *http.Client {
	dialer := &net.Dialer{
		Timeout: 10 * time.Second,
		// Checked on the resolved address so DNS names pointing at loopback/metadata are rejected too.
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip != nil && outboundIPBlocked(ip) {
				return fmt.Errorf("destination %s is not allowed for plugins", ip)
			}
			return nil
		},
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= outboundMaxRedirects {
				return errors.New("too many redirects")
			}
			return validateOutboundURL(allowed, req.URL)
		},
	}
}

// executeOutboundHTTP performs the plugin's HTTP request server-side, enforcing allowed_hosts and size/time limits.
func executeOutboundHTTP(ctx context.Context, allowed []string, payload interface{}) (*OutboundResponse, error) {
	req, err := parseOutboundPayload(payload)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if err := validateOutboundURL(allowed, u); err != nil {
		return nil, err
	}

	timeout := outboundDefaultTimeout
	if req.TimeoutMs > 0 {
		timeout = time.Duration(req.TimeoutMs) * time.Millisecond
		if timeout > outboundMaxTimeout {
			timeout = outboundMaxTimeout
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var body io.Reader
	if len(req.Body) > 0 {
		body = strings.NewReader(string(req.Body))
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, u.String(), body)
	if err != nil {
		return nil, err
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	if httpReq.Header.Get("User-Agent") == "" {
		httpReq.Header.Set("User-Agent", "Kraken-Plugin-Proxy/1.0")
	}

	resp, err := newOutboundClient(allowed).Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", u.Host, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, outboundMaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if len(data) > outboundMaxResponseBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", outboundMaxResponseBytes)
	}

	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if strings.EqualFold(k, "Set-Cookie") {
			continue
		}
		headers[strings.ToLower(k)] = strings.Join(v, ", ")
	}

	return &OutboundResponse{
		Status:     resp.StatusCode,
		StatusText: http.StatusText(resp.StatusCode),
		Headers:    headers,
		BodyBase64: base64.StdEncoding.EncodeToString(data),
		URL:        resp.Request.URL.String(),
	}, nil
}
