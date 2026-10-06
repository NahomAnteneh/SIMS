package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTokenSigner_IssueAndVerify(t *testing.T) {
	signer := NewTokenSigner([]byte("test-secret-key-32-bytes-long!!"), time.Hour)

	vcenterID := uuid.New()
	id := &Identity{
		Subject:            "operator1",
		Email:              "operator1@corp.local",
		Role:               RoleViewer,
		AllowedVCenterIDs:  []uuid.UUID{vcenterID},
		AllowedFolderPaths: []string{"dc1.vm.workloads"},
	}

	token, expiresAt, err := signer.IssueToken(id)
	if err != nil {
		t.Fatalf("IssueToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("empty token returned")
	}
	if expiresAt.Before(time.Now()) {
		t.Fatal("expiration should be in future")
	}

	verified, err := signer.VerifyToken(token)
	if err != nil {
		t.Fatalf("VerifyToken failed: %v", err)
	}

	if verified.Subject != id.Subject {
		t.Errorf("subject mismatch: got %s, want %s", verified.Subject, id.Subject)
	}
	if verified.Role != id.Role {
		t.Errorf("role mismatch: got %s, want %s", verified.Role, id.Role)
	}
	if len(verified.AllowedVCenterIDs) != 1 || verified.AllowedVCenterIDs[0] != vcenterID {
		t.Errorf("vcenter IDs mismatch: got %v", verified.AllowedVCenterIDs)
	}
	if len(verified.AllowedFolderPaths) != 1 || verified.AllowedFolderPaths[0] != "dc1.vm.workloads" {
		t.Errorf("folder paths mismatch: got %v", verified.AllowedFolderPaths)
	}
}

func TestTokenSigner_TamperedToken(t *testing.T) {
	signer := NewTokenSigner([]byte("test-secret-key-32-bytes-long!!"), time.Hour)
	id := &Identity{
		Subject: "admin",
		Role:    RoleInventoryAdmin,
	}

	token, _, _ := signer.IssueToken(id)
	tampered := token[:len(token)-3] + "abc"

	_, err := signer.VerifyToken(tampered)
	if err == nil {
		t.Fatal("expected error on tampered token")
	}
}

func TestTokenSigner_ExpiredToken(t *testing.T) {
	signer := NewTokenSigner([]byte("test-secret-key-32-bytes-long!!"), time.Hour)
	id := &Identity{
		Subject: "admin",
		Role:    RoleInventoryAdmin,
	}

	token, _, _ := signer.IssueTokenWithTTL(id, -1*time.Minute)
	_, err := signer.VerifyToken(token)
	if err != ErrTokenExpired {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}
