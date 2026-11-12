package sidecarauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

const SignatureHeaderPrefix = "v1="

// SignCanonicalRequest generates the HMAC-SHA256 signature for the canonical request
func SignCanonicalRequest(secret string, canonicalRequest string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(canonicalRequest))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return SignatureHeaderPrefix + sig
}

// VerifySignature verifies the signature header against the canonical request using constant-time comparison.
// It also checks the previous secret if rotation grace period is active.
func VerifySignature(
	signatureHeader string,
	canonicalRequest string,
	primarySecret string,
	previousSecret string,
	rotatedAt *time.Time,
	now time.Time,
) error {
	if !strings.HasPrefix(signatureHeader, SignatureHeaderPrefix) {
		return fmt.Errorf("invalid signature format: missing '%s' prefix", SignatureHeaderPrefix)
	}

	rawSig := strings.TrimPrefix(signatureHeader, SignatureHeaderPrefix)
	decodedReceived, err := base64.RawURLEncoding.DecodeString(rawSig)
	if err != nil {
		return fmt.Errorf("malformed base64url signature: %w", err)
	}

	// 1. Check primary secret
	macPrimary := hmac.New(sha256.New, []byte(primarySecret))
	macPrimary.Write([]byte(canonicalRequest))
	expectedPrimary := macPrimary.Sum(nil)

	if hmac.Equal(decodedReceived, expectedPrimary) {
		return nil
	}

	// 2. Check previous secret if rotation grace period is active
	if previousSecret != "" && IsSecretRotationGraceActive(rotatedAt, now) {
		macPrevious := hmac.New(sha256.New, []byte(previousSecret))
		macPrevious.Write([]byte(canonicalRequest))
		expectedPrevious := macPrevious.Sum(nil)

		if hmac.Equal(decodedReceived, expectedPrevious) {
			return nil
		}
	}

	return errors.New("signature mismatch: HMAC-SHA256 verification failed")
}
