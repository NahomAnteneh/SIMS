package auth

import (
	"testing"
)

func TestPasswordHashing(t *testing.T) {
	password := "admin123"
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	t.Logf("Hash for admin123: %s", hash)

	if !CheckPassword(password, hash) {
		t.Fatalf("CheckPassword expected true for correct password")
	}

	if CheckPassword("wrongpassword", hash) {
		t.Fatalf("CheckPassword expected false for wrong password")
	}
}
