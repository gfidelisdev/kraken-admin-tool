package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
)

const encPrefix = "enc:v1:"

var (
	masterKeyOnce sync.Once
	masterKey     []byte
)

func getMasterKey() []byte {
	masterKeyOnce.Do(func() {
		secret := strings.TrimSpace(os.Getenv("ENCRYPTION_KEY"))
		if secret == "" {
			secret = strings.TrimSpace(os.Getenv("JWT_SECRET"))
		}
		if secret == "" {
			b := make([]byte, 32)
			_, _ = rand.Read(b)
			masterKey = b
			return
		}
		hash := sha256.Sum256([]byte(secret))
		masterKey = hash[:]
	})
	return masterKey
}

// Encrypt encrypts a string using AES-256-GCM and returns a prefixed base64 string
func Encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	// Already encrypted
	if strings.HasPrefix(plaintext, encPrefix) {
		return plaintext, nil
	}

	key := getMasterKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decryptWithKey(ciphertext, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("malformed ciphertext: length smaller than nonce size")
	}

	nonce, actualCiphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintextBytes, err := gcm.Open(nil, nonce, actualCiphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintextBytes), nil
}

// Decrypt decrypts a string if prefixed with encPrefix, otherwise returns as-is.
// Supports seamless key rotation via ENCRYPTION_KEY_PREVIOUS (comma-separated list of prior keys).
func Decrypt(data string) (string, error) {
	if data == "" {
		return "", nil
	}
	if !strings.HasPrefix(data, encPrefix) {
		return data, nil // Return unencrypted data as-is for backward compatibility
	}

	rawB64 := strings.TrimPrefix(data, encPrefix)
	ciphertext, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		return "", err
	}

	plaintext, err := decryptWithKey(ciphertext, getMasterKey())
	if err == nil {
		return plaintext, nil
	}

	if prevRaw := strings.TrimSpace(os.Getenv("ENCRYPTION_KEY_PREVIOUS")); prevRaw != "" {
		for _, prevSecret := range strings.Split(prevRaw, ",") {
			prevSecret = strings.TrimSpace(prevSecret)
			if prevSecret == "" {
				continue
			}
			h := sha256.Sum256([]byte(prevSecret))
			if pt, prevErr := decryptWithKey(ciphertext, h[:]); prevErr == nil {
				return pt, nil
			}
		}
	}

	return "", err
}
