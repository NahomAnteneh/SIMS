package credentials

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	prefix       = "enc:v1:"
	fallbackSalt = "sims-entropy-salt-v1"
)

// ErrNotEncrypted indicates that the credential reference is not an encrypted token.
var ErrNotEncrypted = errors.New("credential reference is not encrypted")

func getEncryptionKey() []byte {
	k := os.Getenv("SIMS_CREDENTIAL_KEY")
	if k == "" {
		k = os.Getenv("SIMS_SECRET_KEY")
	}
	if k == "" {
		k = os.Getenv("SECRET_KEY")
	}
	if k == "" {
		dbURL := os.Getenv("DATABASE_URL")
		if dbURL != "" {
			k = fallbackSalt + ":" + dbURL
		} else {
			k = fallbackSalt
		}
	}
	hash := sha256.Sum256([]byte(k))
	return hash[:]
}

// Encrypt encrypts the username and password using AES-256-GCM authenticated encryption.
func Encrypt(username, password string) (string, error) {
	if username == "" && password == "" {
		return "", nil
	}
	pair := []string{username, password}
	plaintext, err := json.Marshal(pair)
	if err != nil {
		return "", fmt.Errorf("failed to marshal credentials: %w", err)
	}

	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	return prefix + base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts an authenticated credential string back to username and password.
// Returns ErrNotEncrypted if the reference string is not prefixed with enc:v1:.
func Decrypt(ref string) (username, password string, err error) {
	if !strings.HasPrefix(ref, prefix) {
		return "", "", ErrNotEncrypted
	}

	encoded := strings.TrimPrefix(ref, prefix)
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", "", fmt.Errorf("invalid base64 credential data: %w", err)
	}

	key := getEncryptionKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", "", fmt.Errorf("failed to create gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", "", errors.New("malformed ciphertext: length less than nonce")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", "", fmt.Errorf("credential decryption failed: %w", err)
	}

	var pair []string
	if err := json.Unmarshal(plaintext, &pair); err != nil || len(pair) < 2 {
		return "", "", fmt.Errorf("failed to unmarshal credential payload: %w", err)
	}

	return pair[0], pair[1], nil
}
