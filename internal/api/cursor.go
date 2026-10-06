package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidCursor  = errors.New("invalid pagination cursor")
	ErrCursorTampered = errors.New("cursor signature verification failed")
	ErrCursorExpired  = errors.New("cursor has expired")
	ErrQueryMismatch  = errors.New("cursor query hash mismatch")
)

const (
	CurrentCursorVersion = 1
	DefaultCursorTTL     = 24 * time.Hour
)

// DefaultSigningKey is used as fallback when no secret is explicitly configured.
var DefaultSigningKey = []byte("sims-cursor-hmac-sha256-secret-key-32b")

// Cursor represents an opaque keyset pagination state token.
type Cursor struct {
	Version   int    `json:"v"`
	QueryHash string `json:"qh,omitempty"`
	SortField string `json:"sf"`
	SortDir   string `json:"sd"`
	LastValue string `json:"lv"`
	LastID    string `json:"id"`
	ExpiresAt int64  `json:"exp"`
	Signature string `json:"sig,omitempty"`

	// Legacy compatibility fields
	Name string `json:"name,omitempty"`
	ID   string `json:"legacy_id,omitempty"`
}

// PageCursor provides backward compatibility with Sprint 1 callers.
type PageCursor struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// ComputeQueryHash calculates a deterministic SHA-256 hex digest of the query criteria.
func ComputeQueryHash(queryParts ...string) string {
	h := sha256.New()
	for _, p := range queryParts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CursorSigner generates and validates tamper-evident cursors using HMAC-SHA256.
type CursorSigner struct {
	secret []byte
	ttl    time.Duration
}

// NewCursorSigner initializes a CursorSigner.
func NewCursorSigner(secret []byte, ttl time.Duration) *CursorSigner {
	if len(secret) == 0 {
		secret = DefaultSigningKey
	}
	if ttl <= 0 {
		ttl = DefaultCursorTTL
	}
	return &CursorSigner{
		secret: secret,
		ttl:    ttl,
	}
}

func (s *CursorSigner) sign(c *Cursor) string {
	mac := hmac.New(sha256.New, s.secret)
	payload := fmt.Sprintf("%d|%s|%s|%s|%s|%s|%d",
		c.Version, c.QueryHash, c.SortField, c.SortDir, c.LastValue, c.LastID, c.ExpiresAt)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

// Encode serializes and signs a Cursor into a URL-safe base64 string.
func (s *CursorSigner) Encode(c *Cursor) string {
	if c.Version == 0 {
		c.Version = CurrentCursorVersion
	}
	if c.ExpiresAt == 0 {
		c.ExpiresAt = time.Now().Add(s.ttl).Unix()
	}
	c.Signature = s.sign(c)

	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

// Decode validates signature, expiration, and query hash, returning the deserialized Cursor.
func (s *CursorSigner) Decode(raw string, expectedQueryHash string) (*Cursor, error) {
	if raw == "" {
		return nil, nil
	}

	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid base64 encoding", ErrInvalidCursor)
		}
	}

	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%w: malformed json payload", ErrInvalidCursor)
	}

	// Handle legacy format fallback (e.g. {"name":"vm-1","id":"uuid"})
	if c.Version == 0 && c.Signature == "" {
		if c.LastValue == "" && c.Name != "" {
			c.LastValue = c.Name
		}
		if c.LastID == "" && c.ID != "" {
			c.LastID = c.ID
		}
		if c.LastID == "" {
			return nil, fmt.Errorf("%w: missing id in cursor", ErrInvalidCursor)
		}
		if _, err := uuid.Parse(c.LastID); err != nil {
			return nil, fmt.Errorf("%w: invalid id uuid in cursor", ErrInvalidCursor)
		}
		c.SortField = "name"
		c.SortDir = "asc"
		return &c, nil
	}

	// 1. Check ID presence and format
	if c.LastID == "" {
		return nil, fmt.Errorf("%w: missing id in cursor", ErrInvalidCursor)
	}
	if _, err := uuid.Parse(c.LastID); err != nil {
		return nil, fmt.Errorf("%w: invalid id uuid in cursor", ErrInvalidCursor)
	}

	// 2. Validate HMAC signature
	expectedSig := s.sign(&c)
	if !hmac.Equal([]byte(c.Signature), []byte(expectedSig)) {
		return nil, ErrCursorTampered
	}

	// 3. Validate expiration
	if c.ExpiresAt > 0 && time.Now().Unix() > c.ExpiresAt {
		return nil, ErrCursorExpired
	}

	// 4. Validate query hash if provided
	if expectedQueryHash != "" && c.QueryHash != "" && c.QueryHash != expectedQueryHash {
		return nil, ErrQueryMismatch
	}

	return &c, nil
}

// Global default signer for package-level convenience.
var defaultSigner = NewCursorSigner(DefaultSigningKey, DefaultCursorTTL)

// EncodeCursor produces a signed base64 cursor string for (name, id).
func EncodeCursor(name, id string) string {
	c := &Cursor{
		Version:   CurrentCursorVersion,
		SortField: "name",
		SortDir:   "asc",
		LastValue: name,
		LastID:    id,
		Name:      name,
	}
	return defaultSigner.Encode(c)
}

// DecodeCursor parses an opaque cursor string into PageCursor.
func DecodeCursor(s string) (*PageCursor, error) {
	c, err := defaultSigner.Decode(s, "")
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, nil
	}
	return &PageCursor{
		Name: c.LastValue,
		ID:   c.LastID,
	}, nil
}
