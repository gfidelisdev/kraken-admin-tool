package sidecarauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	HeaderPluginID       = "X-Kraken-Plugin-ID"
	HeaderInstallationID = "X-Kraken-Installation-ID"
	HeaderRequestID      = "X-Kraken-Request-ID"
	HeaderTimestamp      = "X-Kraken-Timestamp"
	HeaderNonce          = "X-Kraken-Nonce"
	HeaderUserContext    = "X-Kraken-User-Context"
	HeaderContentSHA256  = "X-Kraken-Content-SHA256"
	HeaderSignature      = "X-Kraken-Signature"

	MaxResponseBodyLimit = 16 * 1024 * 1024 // 16 MiB
	DefaultClientTimeout = 25 * time.Second
)

// Transport encapsulates the signing and transmission of requests from Kraken backend to plugin sidecars
type Transport struct {
	httpClient *http.Client
}

// NewTransport creates a hardened HTTP transport for communicating with sidecars
func NewTransport(customTimeout ...time.Duration) *Transport {
	timeout := DefaultClientTimeout
	if len(customTimeout) > 0 && customTimeout[0] > 0 {
		timeout = customTimeout[0]
	}

	return &Transport{
		httpClient: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Prevent redirects (SSRF mitigation)
				return errors.New("redirects are forbidden on internal plugin sidecar calls")
			},
		},
	}
}

// CallParams encapsulates the context needed to construct and sign a request
type CallParams struct {
	TargetURL       string
	Path            string
	PluginID        string
	InstallationID  string
	InstanceSecret  string
	UserID          uint
	Username        string
	SessionID       string
	Roles           []string
	Permissions     []string
	Action          string
	RequestBody     []byte
	CustomRequestID string // optional override for testing
	CustomNonce     string // optional override for testing
	CustomTime      *time.Time // optional override for testing
}

// PrepareAndSignRequest builds the HTTP request with all required headers and HMAC signatures
func PrepareAndSignRequest(ctx context.Context, params CallParams) (*http.Request, string, error) {
	if params.TargetURL == "" {
		return nil, "", errors.New("target URL cannot be empty")
	}
	if params.InstanceSecret == "" {
		return nil, "", errors.New("instance secret cannot be empty")
	}
	if params.PluginID == "" {
		return nil, "", errors.New("pluginID cannot be empty")
	}
	if params.InstallationID == "" {
		return nil, "", errors.New("installationID cannot be empty")
	}

	now := time.Now().UTC()
	if params.CustomTime != nil {
		now = *params.CustomTime
	}

	reqID := params.CustomRequestID
	if reqID == "" {
		reqID = GenerateRequestID()
	}

	nonce := params.CustomNonce
	if nonce == "" {
		nonce = GenerateNonce()
	}

	timestampStr := now.Format(time.RFC3339)

	// 1. Compute Content Hash
	contentHash := HashBytes(params.RequestBody)

	// 2. Issue Internal Context JWT
	claims, err := NewSidecarClaims(
		params.PluginID,
		params.InstallationID,
		params.UserID,
		params.Username,
		params.SessionID,
		params.Roles,
		params.Permissions,
		params.Action,
		reqID,
		now,
	)
	if err != nil {
		return nil, "", fmt.Errorf("failed to build sidecar claims: %w", err)
	}

	jwtStr, err := CreateSidecarJWT(params.InstanceSecret, claims)
	if err != nil {
		return nil, "", fmt.Errorf("failed to sign sidecar JWT: %w", err)
	}

	// 3. Compute Context Hash
	contextHash := HashBytes([]byte(jwtStr))

	// 4. Construct Canonical Request
	path := params.Path
	if path == "" {
		path = "/call"
	}
	canonical := BuildCanonicalRequest(
		"POST",
		path,
		params.PluginID,
		params.InstallationID,
		reqID,
		timestampStr,
		nonce,
		contentHash,
		contextHash,
	)

	// 5. Generate HMAC-SHA256 signature
	signature := SignCanonicalRequest(params.InstanceSecret, canonical)

	// 6. Build HTTP Request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", params.TargetURL, bytes.NewReader(params.RequestBody))
	if err != nil {
		return nil, "", err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(HeaderPluginID, params.PluginID)
	httpReq.Header.Set(HeaderInstallationID, params.InstallationID)
	httpReq.Header.Set(HeaderRequestID, reqID)
	httpReq.Header.Set(HeaderTimestamp, timestampStr)
	httpReq.Header.Set(HeaderNonce, nonce)
	httpReq.Header.Set(HeaderUserContext, jwtStr)
	httpReq.Header.Set(HeaderContentSHA256, contentHash)
	httpReq.Header.Set(HeaderSignature, signature)

	// Explicitly DO NOT set X-Kraken-User or X-Kraken-Role
	httpReq.Header.Del("X-Kraken-User")
	httpReq.Header.Del("X-Kraken-Role")

	return httpReq, reqID, nil
}

// Call executes the signed call to the plugin sidecar and parses the response
func (t *Transport) Call(ctx context.Context, params CallParams) (interface{}, error) {
	httpReq, reqID, err := PrepareAndSignRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to communicate with plugin sidecar (request_id=%s): %w", reqID, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBodyLimit))
	if err != nil {
		return nil, fmt.Errorf("failed to read response from sidecar: %w", err)
	}

	if resp.StatusCode >= 400 {
		var problem ProblemDetails
		if jsonErr := json.Unmarshal(respBytes, &problem); jsonErr == nil && problem.Code != "" {
			return nil, fmt.Errorf("sidecar rejected request [%s]: %s (status %d)", problem.Code, problem.Title, problem.Status)
		}
		return nil, fmt.Errorf("plugin sidecar returned HTTP %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed interface{}
	if err := json.Unmarshal(respBytes, &parsed); err == nil {
		return parsed, nil
	}

	return string(respBytes), nil
}

// RedactSensitiveMap removes passwords, tokens, or temporary secrets from logging payloads
func RedactSensitiveMap(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}
	redacted := make(map[string]interface{}, len(data))
	sensitiveKeys := map[string]bool{
		"password":           true,
		"temporary_password": true,
		"token":              true,
		"access_token":       true,
		"refresh_token":      true,
		"client_secret":      true,
		"secret":             true,
		"instance_secret":    true,
	}

	for k, v := range data {
		lowerK := strings.ToLower(k)
		if sensitiveKeys[lowerK] {
			redacted[k] = "[REDACTED]"
		} else if nested, ok := v.(map[string]interface{}); ok {
			redacted[k] = RedactSensitiveMap(nested)
		} else {
			redacted[k] = v
		}
	}
	return redacted
}
