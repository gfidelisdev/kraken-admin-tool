package docker

import (
	"context"
	"os"
	"testing"
)

func TestVerifyLocalHostCredentials(t *testing.T) {
	ctx := context.Background()

	// 1. Empty credentials must fail
	if err := verifyLocalHostCredentials(ctx, "", ""); err == nil {
		t.Errorf("expected error for empty credentials, got nil")
	}
	if err := verifyLocalHostCredentials(ctx, "root", ""); err == nil {
		t.Errorf("expected error for empty password, got nil")
	}
	if err := verifyLocalHostCredentials(ctx, "", "somepass"); err == nil {
		t.Errorf("expected error for empty user, got nil")
	}

	// 2. Mock environment skip flag
	os.Setenv("KRAKEN_SKIP_HOST_AUTH_VERIFY", "true")
	defer os.Unsetenv("KRAKEN_SKIP_HOST_AUTH_VERIFY")

	if err := verifyLocalHostCredentials(ctx, "root", "anypass"); err != nil {
		t.Errorf("expected nil when KRAKEN_SKIP_HOST_AUTH_VERIFY=true, got %v", err)
	}
}

func TestVerifyLocalHostCredentials_Invalid(t *testing.T) {
	ctx := context.Background()
	os.Unsetenv("KRAKEN_SKIP_HOST_AUTH_VERIFY")

	// Invalid characters in username
	if err := verifyLocalHostCredentials(ctx, "user;rm -rf /", "pass"); err == nil {
		t.Errorf("expected error for malformed username, got nil")
	}

	// Nonexistent user
	err := verifyLocalHostCredentials(ctx, "invalid_user_xyz", "invalid_pass_123")
	if err == nil {
		t.Errorf("expected error for invalid credentials, got nil")
	} else {
		t.Logf("Got expected error for invalid user: %v", err)
	}

	// Root user when locked
	if err := verifyLocalHostCredentials(ctx, "root", "any_random_pass"); err == nil {
		t.Errorf("expected error for root on locked host or wrong pass, got nil")
	} else {
		t.Logf("Got expected error for root: %v", err)
	}
}
