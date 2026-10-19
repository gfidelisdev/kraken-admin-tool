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
		secret := os.Getenv("ENCRYPTION_KEY")
		if strings.TrimSpace(secret) == "" {
			secret = os.Getenv("JWT_SECRET")
		}
		if strings.TrimSpace(secret) == "" {
			secret = "kraken-default-internal-encryption-key-must-change"
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

// Decrypt decrypts a string if prefixed with encPrefix, otherwise returns as-is
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

	key := getMasterKey()
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
