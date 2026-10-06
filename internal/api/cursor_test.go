package api

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestCursor_RoundTripAndSigning(t *testing.T) {
	signer := NewCursorSigner([]byte("test-secret-key-32-bytes-long!"), time.Hour)

	queryHash := ComputeQueryHash("WHERE power_state = 'poweredOn'", "ORDER BY name ASC")
	c := &Cursor{
		QueryHash: queryHash,
		SortField: "name",
		SortDir:   "asc",
		LastValue: "vm-app-01",
		LastID:    "550e8400-e29b-41d4-a716-446655440000",
	}

	encoded := signer.Encode(c)
	if encoded == "" {
		t.Fatalf("expected non-empty cursor string")
	}

	decoded, err := signer.Decode(encoded, queryHash)
	if err != nil {
		t.Fatalf("failed to decode cursor: %v", err)
	}

	if decoded.LastValue != c.LastValue {
		t.Errorf("expected LastValue %q, got %q", c.LastValue, decoded.LastValue)
	}
	if decoded.LastID != c.LastID {
		t.Errorf("expected LastID %q, got %q", c.LastID, decoded.LastID)
	}
	if decoded.SortField != c.SortField {
		t.Errorf("expected SortField %q, got %q", c.SortField, decoded.SortField)
	}
}

func TestCursor_TamperDetection(t *testing.T) {
	signer := NewCursorSigner([]byte("test-secret-key-32-bytes-long!"), time.Hour)

	c := &Cursor{
		SortField: "name",
		SortDir:   "asc",
		LastValue: "vm-app-01",
		LastID:    "550e8400-e29b-41d4-a716-446655440000",
	}

	encoded := signer.Encode(c)

	// Tamper with payload by altering LastValue
	rawBytes, _ := base64.RawURLEncoding.DecodeString(encoded)
	var mapObj map[string]any
	_ = json.Unmarshal(rawBytes, &mapObj)
	mapObj["lv"] = "vm-hacked-02" // Tamper

	tamperedBytes, _ := json.Marshal(mapObj)
	tamperedEncoded := base64.RawURLEncoding.EncodeToString(tamperedBytes)

	_, err := signer.Decode(tamperedEncoded, "")
	if err != ErrCursorTampered {
		t.Fatalf("expected ErrCursorTampered, got: %v", err)
	}
}

func TestCursor_QueryMismatch(t *testing.T) {
	signer := NewCursorSigner([]byte("test-secret-key-32-bytes-long!"), time.Hour)

	hash1 := ComputeQueryHash("query 1")
	hash2 := ComputeQueryHash("query 2")

	c := &Cursor{
		QueryHash: hash1,
		SortField: "name",
		SortDir:   "asc",
		LastValue: "vm-app-01",
		LastID:    "550e8400-e29b-41d4-a716-446655440000",
	}

	encoded := signer.Encode(c)

	_, err := signer.Decode(encoded, hash2)
	if err != ErrQueryMismatch {
		t.Fatalf("expected ErrQueryMismatch, got: %v", err)
	}
}

func TestCursor_Expired(t *testing.T) {
	signer := NewCursorSigner([]byte("test-secret-key-32-bytes-long!"), time.Hour)

	c := &Cursor{
		SortField: "name",
		SortDir:   "asc",
		LastValue: "vm-app-01",
		LastID:    "550e8400-e29b-41d4-a716-446655440000",
		ExpiresAt: time.Now().Add(-time.Hour).Unix(),
	}

	encoded := signer.Encode(c)

	_, err := signer.Decode(encoded, "")
	if err != ErrCursorExpired {
		t.Fatalf("expected ErrCursorExpired, got: %v", err)
	}
}

func TestCursor_BackwardCompatibility(t *testing.T) {
	legacy := `{"name":"vm-legacy","id":"550e8400-e29b-41d4-a716-446655440000"}`
	encoded := base64.RawURLEncoding.EncodeToString([]byte(legacy))

	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("expected legacy decode to succeed, got: %v", err)
	}

	if decoded.Name != "vm-legacy" {
		t.Errorf("expected Name 'vm-legacy', got %q", decoded.Name)
	}
	if decoded.ID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("expected ID '550e8400-e29b-41d4-a716-446655440000', got %q", decoded.ID)
	}
}
