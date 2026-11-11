package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	totpReplayMu    sync.Mutex
	totpReplayCache = make(map[string]time.Time)
)

const (
	// TOTPPeriod defines the time step in seconds (RFC 6238 standard is 30s)
	TOTPPeriod = 30
	// TOTPDigits defines the length of OTP codes (standard 6 digits)
	TOTPDigits = 6
)

// GenerateTOTPSecret creates a random 20-byte base32 encoded secret key
func GenerateTOTPSecret() (string, error) {
	bytes := make([]byte, 20)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes for TOTP: %w", err)
	}
	// RFC 4648 base32 without padding
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(bytes)
	return secret, nil
}

// GenerateTOTPAuthURL formats a standard otpauth URI compatible with Google Authenticator, Proton, Authy, etc.
func GenerateTOTPAuthURL(issuer, accountName, secret string) string {
	cleanIssuer := strings.TrimSpace(issuer)
	if cleanIssuer == "" {
		cleanIssuer = "Kraken"
	}
	cleanAccount := strings.TrimSpace(accountName)
	label := fmt.Sprintf("%s:%s", cleanIssuer, cleanAccount)

	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", cleanIssuer)
	params.Set("algorithm", "SHA1")
	params.Set("digits", fmt.Sprintf("%d", TOTPDigits))
	params.Set("period", fmt.Sprintf("%d", TOTPPeriod))

	return fmt.Sprintf("otpauth://totp/%s?%s", url.PathEscape(label), params.Encode())
}

// ComputeTOTP calculates the current 6-digit TOTP code for a secret at a given timestamp
func ComputeTOTP(secret string, t time.Time) (string, error) {
	cleanSecret := strings.ToUpper(strings.TrimSpace(secret))
	// Pad if needed for base32 standard decoding
	if m := len(cleanSecret) % 8; m != 0 {
		cleanSecret += strings.Repeat("=", 8-m)
	}

	key, err := base32.StdEncoding.DecodeString(cleanSecret)
	if err != nil {
		return "", fmt.Errorf("invalid base32 secret: %w", err)
	}

	counter := uint64(t.Unix() / TOTPPeriod)
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	hash := mac.Sum(nil)

	// Dynamic truncation (RFC 4226)
	offset := hash[len(hash)-1] & 0x0f
	truncatedHash := binary.BigEndian.Uint32(hash[offset : offset+4])
	truncatedHash &= 0x7fffffff

	code := truncatedHash % 1000000
	return fmt.Sprintf("%06d", code), nil
}

// ValidateTOTPCode checks if the given 6-digit code is valid within a ±1 step time window (30s drift tolerance)
func ValidateTOTPCode(secret, code string) bool {
	cleanCode := strings.TrimSpace(code)
	if len(cleanCode) != TOTPDigits {
		return false
	}

	now := time.Now()
	// Check previous step, current step, and next step
	for offset := -1; offset <= 1; offset++ {
		t := now.Add(time.Duration(offset*TOTPPeriod) * time.Second)
		expected, err := ComputeTOTP(secret, t)
		if err == nil && subtle.ConstantTimeCompare([]byte(cleanCode), []byte(expected)) == 1 {
			return true
		}
	}

	return false
}

// ConsumeTOTPCodeForAction validates a 6-digit TOTP code and records it in an anti-replay cache
// for the given action over the ±1 time-step window (RFC 6238 §5.2).
func ConsumeTOTPCodeForAction(action, secret, code string) bool {
	if !ValidateTOTPCode(secret, code) {
		return false
	}

	cleanCode := strings.TrimSpace(code)
	digest := sha256.Sum256([]byte(action + ":" + strings.ToUpper(strings.TrimSpace(secret)) + ":" + cleanCode))
	key := hex.EncodeToString(digest[:])

	totpReplayMu.Lock()
	defer totpReplayMu.Unlock()

	now := time.Now()
	for k, exp := range totpReplayCache {
		if now.After(exp) {
			delete(totpReplayCache, k)
		}
	}

	if exp, exists := totpReplayCache[key]; exists && now.Before(exp) {
		return false
	}

	totpReplayCache[key] = now.Add(3 * TOTPPeriod * time.Second)
	return true
}
