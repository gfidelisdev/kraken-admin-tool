package sidecarauth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const (
	// DefaultSecretEntropyBytes defines minimum bytes of crypto entropy for instance secrets (32 bytes = 256 bits)
	DefaultSecretEntropyBytes = 32
	// DefaultRotationGraceWindow is the maximum period (60 seconds) an old secret is accepted after rotation
	DefaultRotationGraceWindow = 60 * time.Second
)

// GenerateInstanceSecret generates a cryptographically secure 32-byte secret (Base64URL encoded)
func GenerateInstanceSecret() (string, error) {
	b := make([]byte, DefaultSecretEntropyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to read secure random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// GenerateNonce generates a cryptographically secure random 16-byte nonce
func GenerateNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// GenerateRequestID generates a UUID v4 string for X-Kraken-Request-ID and JTI
func GenerateRequestID() string {
	return uuid.NewString()
}

// GenerateInstallationID generates a unique instance ID for a plugin installation
func GenerateInstallationID() string {
	return uuid.NewString()
}

// IsSecretRotationGraceActive checks if the previous secret is still valid within the grace window (<= 60s)
func IsSecretRotationGraceActive(rotatedAt *time.Time, now time.Time) bool {
	if rotatedAt == nil || rotatedAt.IsZero() {
		return false
	}
	diff := now.Sub(*rotatedAt)
	return diff >= 0 && diff <= DefaultRotationGraceWindow
}

// RotateSecret generates a new secret and returns the previous secret alongside the rotation timestamp
func RotateSecret(currentSecret string) (newSecret string, previousSecret string, rotatedAt time.Time, err error) {
	if currentSecret == "" {
		return "", "", time.Time{}, errors.New("cannot rotate empty secret")
	}
	newSec, err := GenerateInstanceSecret()
	if err != nil {
		return "", "", time.Time{}, err
	}
	now := time.Now().UTC()
	return newSec, currentSecret, now, nil
}
