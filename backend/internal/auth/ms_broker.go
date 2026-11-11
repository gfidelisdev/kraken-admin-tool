package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kraken/admin-tool/backend/internal/models"
)

const (
	DefaultMicrosoftClientID = "04b07795-8ddb-461a-bbee-02f9e1bf7b46"
	DefaultMicrosoftTenantID = "common"
)

var allowedMicrosoftScopes = map[string]bool{
	"https://vault.azure.net/.default":      true,
	"https://management.azure.com/.default": true,
	"https://graph.microsoft.com/.default":  true,
}

// msTokenPayload represents the OAuth2 token response from Microsoft Entra ID
type msTokenPayload struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

type ScopedTokenEntry struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

type MicrosoftSessionEntry struct {
	SessionID        string
	UserID           uint
	Email            string
	Name             string
	TenantID         string
	ClientID         string
	RefreshToken     string
	ScopedTokens     map[string]ScopedTokenEntry
	ExpiresAt        time.Time
	mu               sync.Mutex
}

type MicrosoftSessionBroker struct {
	mu           sync.RWMutex
	sessions     map[string]*MicrosoftSessionEntry
	EntraBaseURL string
	HTTPClient   *http.Client
}

func NewMicrosoftSessionBroker() *MicrosoftSessionBroker {
	return &MicrosoftSessionBroker{
		sessions:     make(map[string]*MicrosoftSessionEntry),
		EntraBaseURL: "https://login.microsoftonline.com",
		HTTPClient:   &http.Client{Timeout: 15 * time.Second},
	}
}

func (b *MicrosoftSessionBroker) getClient() *http.Client {
	if b.HTTPClient != nil {
		return b.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (b *MicrosoftSessionBroker) getBaseURL() string {
	if strings.TrimSpace(b.EntraBaseURL) != "" {
		return strings.TrimRight(strings.TrimSpace(b.EntraBaseURL), "/")
	}
	return "https://login.microsoftonline.com"
}

// CreateSession sweeps expired sessions, generates a random session ID, caches the session entry, and returns sessionID
func (b *MicrosoftSessionBroker) CreateSession(
	userID uint,
	email, name, tenantID, clientID, refreshToken, initialScope, initialAccessToken string,
	expiresIn int,
	sessionTTL time.Duration,
) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Opportunistic sweep of expired sessions
	now := time.Now()
	for sid, entry := range b.sessions {
		if now.After(entry.ExpiresAt) {
			delete(b.sessions, sid)
		}
	}

	bytes := make([]byte, 16)
	_, _ = rand.Read(bytes)
	sessionID := hex.EncodeToString(bytes)

	if sessionTTL <= 0 {
		sessionTTL = 24 * time.Hour
	}

	scopedMap := make(map[string]ScopedTokenEntry)
	if initialScope != "" && initialAccessToken != "" {
		expSeconds := expiresIn
		if expSeconds <= 0 {
			expSeconds = 3600
		}
		scopedMap[initialScope] = ScopedTokenEntry{
			AccessToken: initialAccessToken,
			ExpiresAt:   now.Add(time.Duration(expSeconds) * time.Second),
		}
	}

	if clientID == "" {
		clientID = DefaultMicrosoftClientID
	}
	if tenantID == "" {
		tenantID = DefaultMicrosoftTenantID
	}

	b.sessions[sessionID] = &MicrosoftSessionEntry{
		SessionID:    sessionID,
		UserID:       userID,
		Email:        email,
		Name:         name,
		TenantID:     tenantID,
		ClientID:     clientID,
		RefreshToken: refreshToken,
		ScopedTokens: scopedMap,
		ExpiresAt:    now.Add(sessionTTL),
	}

	return sessionID
}

// GetAccountMetadata returns public Microsoft session metadata without exposing raw OAuth2 tokens
func (b *MicrosoftSessionBroker) GetAccountMetadata(sessionID string) *models.MicrosoftSessionAccount {
	if sessionID == "" {
		return nil
	}

	b.mu.RLock()
	entry, exists := b.sessions[sessionID]
	b.mu.RUnlock()

	if !exists {
		return nil
	}

	if time.Now().After(entry.ExpiresAt) {
		b.DeleteSession(sessionID)
		return nil
	}

	return &models.MicrosoftSessionAccount{
		ID:       "kraken-ms-" + entry.Email,
		Email:    entry.Email,
		Name:     entry.Name,
		TenantID: entry.TenantID,
		Source:   "kraken_session",
	}
}

// GetScopedToken retrieves a non-expired access token for the given scope, refreshing it via Microsoft Entra if needed
func (b *MicrosoftSessionBroker) GetScopedToken(ctx context.Context, sessionID string, scope string) (string, *models.MicrosoftSessionAccount, error) {
	if sessionID == "" {
		return "", nil, errors.New("empty session id")
	}
	if !allowedMicrosoftScopes[scope] {
		return "", nil, fmt.Errorf("scope '%s' is not allowed", scope)
	}

	b.mu.RLock()
	entry, exists := b.sessions[sessionID]
	b.mu.RUnlock()

	if !exists {
		return "", nil, errors.New("microsoft session not found")
	}

	if time.Now().After(entry.ExpiresAt) {
		b.DeleteSession(sessionID)
		return "", nil, errors.New("microsoft session expired")
	}

	accountMeta := &models.MicrosoftSessionAccount{
		ID:       "kraken-ms-" + entry.Email,
		Email:    entry.Email,
		Name:     entry.Name,
		TenantID: entry.TenantID,
		Source:   "kraken_session",
	}

	// Session-level mutex avoids blocking the whole broker while doing outbound HTTP calls
	entry.mu.Lock()
	defer entry.mu.Unlock()

	now := time.Now()
	if tok, ok := entry.ScopedTokens[scope]; ok {
		if now.Add(5 * time.Minute).Before(tok.ExpiresAt) {
			return tok.AccessToken, accountMeta, nil
		}
	}

	if entry.RefreshToken == "" {
		return "", accountMeta, errors.New("no refresh token available in session")
	}

	// Perform refresh exchange with Microsoft Entra ID
	tokenURL := fmt.Sprintf("%s/%s/oauth2/v2.0/token", b.getBaseURL(), entry.TenantID)
	data := url.Values{}
	data.Set("client_id", entry.ClientID)
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", entry.RefreshToken)
	data.Set("scope", scope+" offline_access openid profile email")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", accountMeta, fmt.Errorf("failed to create token refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.getClient().Do(req)
	if err != nil {
		return "", accountMeta, fmt.Errorf("token refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", accountMeta, fmt.Errorf("failed to read token refresh response: %w", err)
	}

	var payload msTokenPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return "", accountMeta, fmt.Errorf("invalid json in token refresh: %w (body: %q)", err, string(bodyBytes))
	}

	if payload.Error != "" || payload.AccessToken == "" {
		return "", accountMeta, fmt.Errorf("token refresh error: %s (%s)", payload.Error, payload.ErrorDesc)
	}

	expSeconds := payload.ExpiresIn
	if expSeconds <= 0 {
		expSeconds = 3600
	}

	entry.ScopedTokens[scope] = ScopedTokenEntry{
		AccessToken: payload.AccessToken,
		ExpiresAt:   time.Now().Add(time.Duration(expSeconds) * time.Second),
	}

	if payload.RefreshToken != "" {
		entry.RefreshToken = payload.RefreshToken
	}

	return payload.AccessToken, accountMeta, nil
}

// DeleteSession wipes tokens and removes session
func (b *MicrosoftSessionBroker) DeleteSession(sessionID string) {
	if sessionID == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	if entry, ok := b.sessions[sessionID]; ok {
		entry.mu.Lock()
		entry.RefreshToken = ""
		for k := range entry.ScopedTokens {
			delete(entry.ScopedTokens, k)
		}
		entry.mu.Unlock()
		delete(b.sessions, sessionID)
	}
}

// DeleteUserSessions wipes tokens and removes all Microsoft sessions for a given userID
func (b *MicrosoftSessionBroker) DeleteUserSessions(userID uint) {
	if userID == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	for sid, entry := range b.sessions {
		if entry.UserID == userID {
			entry.mu.Lock()
			entry.RefreshToken = ""
			for k := range entry.ScopedTokens {
				delete(entry.ScopedTokens, k)
			}
			entry.mu.Unlock()
			delete(b.sessions, sid)
		}
	}
}

// StartDeviceCodeFlow initiates the Microsoft OAuth2 Device Code flow
func (b *MicrosoftSessionBroker) StartDeviceCodeFlow(ctx context.Context, tenantID, clientID string) (*models.MicrosoftDeviceCodeResponse, error) {
	if clientID == "" {
		clientID = DefaultMicrosoftClientID
	}
	if tenantID == "" {
		tenantID = DefaultMicrosoftTenantID
	}

	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/devicecode", b.getBaseURL(), tenantID)
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("scope", "openid profile email offline_access https://vault.azure.net/.default")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to build devicecode request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.getClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("devicecode request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read devicecode response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errObj map[string]interface{}
		_ = json.Unmarshal(bodyBytes, &errObj)
		return nil, fmt.Errorf("devicecode endpoint returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var raw struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
		Message         string `json:"message"`
	}
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse devicecode response json: %w", err)
	}

	interval := raw.Interval
	if interval <= 0 {
		interval = 5
	}

	return &models.MicrosoftDeviceCodeResponse{
		DeviceCode:      raw.DeviceCode,
		UserCode:        raw.UserCode,
		VerificationURI: raw.VerificationURI,
		ExpiresIn:       raw.ExpiresIn,
		Interval:        interval,
		Message:         raw.Message,
	}, nil
}

// PollDeviceCodeToken polls Entra ID to check if user completed authentication
func (b *MicrosoftSessionBroker) PollDeviceCodeToken(ctx context.Context, tenantID, clientID, deviceCode string) (*msTokenPayload, string, int, error) {
	if clientID == "" {
		clientID = DefaultMicrosoftClientID
	}
	if tenantID == "" {
		tenantID = DefaultMicrosoftTenantID
	}

	endpoint := fmt.Sprintf("%s/%s/oauth2/v2.0/token", b.getBaseURL(), tenantID)
	data := url.Values{}
	data.Set("client_id", clientID)
	data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
	data.Set("device_code", deviceCode)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, "error", 0, fmt.Errorf("failed to build token poll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.getClient().Do(req)
	if err != nil {
		return nil, "error", 0, fmt.Errorf("token poll request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "error", 0, fmt.Errorf("failed to read token poll response: %w", err)
	}

	var payload msTokenPayload
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil, "error", 0, fmt.Errorf("invalid json in token poll response: %w", err)
	}

	if payload.Error != "" {
		switch payload.Error {
		case "authorization_pending":
			return &payload, "pending_authorization", 0, nil
		case "slow_down":
			// RFC 8628 §3.5: client must back off polling interval by 5 seconds
			return &payload, "slow_down", 5, nil
		default:
			return &payload, "error", 0, fmt.Errorf("oauth error: %s (%s)", payload.Error, payload.ErrorDesc)
		}
	}

	return &payload, "completed", 0, nil
}

// decodeJWTPayload decodes the unverified base64url JSON claims segment of a JWT
func decodeJWTPayload(tokenStr string) (map[string]interface{}, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) < 2 {
		return nil, errors.New("invalid jwt format")
	}
	payloadSegment := parts[1]
	// Handle URL base64 padding
	switch len(payloadSegment) % 4 {
	case 2:
		payloadSegment += "=="
	case 3:
		payloadSegment += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(payloadSegment)
	if err != nil {
		return nil, fmt.Errorf("failed to base64url decode jwt payload: %w", err)
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, fmt.Errorf("failed to unmarshal jwt claims: %w", err)
	}
	return claims, nil
}

// ExtractMicrosoftIdentity deterministically decodes claims from IDToken and AccessToken
func ExtractMicrosoftIdentity(tokenPayload *msTokenPayload, fallbackTenant string) (id, email, name, tenantID string, err error) {
	if tokenPayload == nil {
		return "", "", "", "", errors.New("nil token payload")
	}

	claims := make(map[string]interface{})

	if tokenPayload.IDToken != "" {
		if idClaims, err := decodeJWTPayload(tokenPayload.IDToken); err == nil {
			for k, v := range idClaims {
				claims[k] = v
			}
		}
	}

	if tokenPayload.AccessToken != "" {
		if accessClaims, err := decodeJWTPayload(tokenPayload.AccessToken); err == nil {
			for k, v := range accessClaims {
				if _, exists := claims[k]; !exists {
					claims[k] = v
				}
			}
		}
	}

	getStringClaim := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := claims[k]; ok {
				if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
					return strings.TrimSpace(s)
				}
			}
		}
		return ""
	}

	// 1. Email: first claim with '@'
	for _, k := range []string{"email", "preferred_username", "upn", "unique_name"} {
		val := getStringClaim(k)
		if strings.Contains(val, "@") {
			email = strings.ToLower(val)
			break
		}
	}
	if email == "" {
		return "", "", "", "", errors.New("no email claim found in microsoft token")
	}

	// 2. Name
	name = getStringClaim("name", "given_name")
	if name == "" {
		parts := strings.Split(email, "@")
		name = parts[0]
	}

	// 3. Tenant ID
	tenantID = getStringClaim("tid")
	if tenantID == "" {
		tenantID = fallbackTenant
	}
	if tenantID == "" {
		tenantID = DefaultMicrosoftTenantID
	}

	// 4. ID
	id = getStringClaim("oid", "sub")
	if id == "" {
		id = "kraken-ms-" + email
	}

	return id, email, name, tenantID, nil
}
