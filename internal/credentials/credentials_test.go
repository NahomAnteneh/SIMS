package credentials

import (
	"os"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	orig := "test-admin@vsphere.local"
	pass := "Complex#Pass!2026"

	enc, err := Encrypt(orig, pass)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	if enc == "" {
		t.Fatal("expected non-empty ciphertext")
	}

	if enc == orig || enc == pass {
		t.Fatal("ciphertext exposed credentials in plaintext")
	}

	u, p, err := Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if u != orig {
		t.Errorf("expected username %q, got %q", orig, u)
	}
	if p != pass {
		t.Errorf("expected password %q, got %q", pass, p)
	}
}

func TestDecryptNotEncrypted(t *testing.T) {
	_, _, err := Decrypt("ref://sims-vault/12345")
	if err != ErrNotEncrypted {
		t.Errorf("expected ErrNotEncrypted, got %v", err)
	}

	_, _, err = Decrypt("")
	if err != ErrNotEncrypted {
		t.Errorf("expected ErrNotEncrypted, got %v", err)
	}
}

func TestCustomSecretKey(t *testing.T) {
	os.Setenv("SIMS_CREDENTIAL_KEY", "custom-secret-key-1234")
	defer os.Unsetenv("SIMS_CREDENTIAL_KEY")

	u := "custom-user"
	p := "custom-pass"

	enc, err := Encrypt(u, p)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	decU, decP, err := Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}

	if decU != u || decP != p {
		t.Fatalf("mismatch with custom key")
	}

	// Change key -> decryption must fail
	os.Setenv("SIMS_CREDENTIAL_KEY", "different-secret-key-5678")
	_, _, err = Decrypt(enc)
	if err == nil {
		t.Fatal("expected decryption to fail with different key")
	}
}
