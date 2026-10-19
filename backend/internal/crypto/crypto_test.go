package crypto

import (
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	cases := []string{
		"superSecretPassword123!",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAA...\n-----END OPENSSH PRIVATE KEY-----",
		"passphrase@with$pecial&chars",
		"",
	}

	for _, text := range cases {
		enc, err := Encrypt(text)
		if err != nil {
			t.Fatalf("failed to encrypt %q: %v", text, err)
		}

		if text != "" && enc == text {
			t.Errorf("ciphertext should not match plaintext for %q", text)
		}

		dec, err := Decrypt(enc)
		if err != nil {
			t.Fatalf("failed to decrypt %q: %v", text, err)
		}

		if dec != text {
			t.Errorf("expected decrypted text %q, got %q", text, dec)
		}
	}
}

func TestDecryptUnencryptedFallback(t *testing.T) {
	legacyData := "legacy-unencrypted-password"
	dec, err := Decrypt(legacyData)
	if err != nil {
		t.Fatalf("unexpected error decrypting legacy data: %v", err)
	}
	if dec != legacyData {
		t.Errorf("expected %q, got %q", legacyData, dec)
	}
}
