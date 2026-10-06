package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrInvalidCredentials is returned when authentication fails.
	ErrInvalidCredentials = errors.New("invalid username or password")
)

// HashPassword generates a secure bcrypt hash of the provided plaintext password.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// CheckPassword compares a plaintext password against a stored bcrypt hash.
func CheckPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
