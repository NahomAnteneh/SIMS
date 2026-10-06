package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrTokenExpired indicates the token validity period has lapsed.
	ErrTokenExpired = errors.New("token has expired")
	// ErrTokenInvalid indicates a corrupted or tampered token.
	ErrTokenInvalid = errors.New("token signature is invalid or malformed")

	// DefaultJWTSecret provides a fallback HMAC key for development environments.
	DefaultJWTSecret = []byte("sims-local-auth-jwt-secret-key-32b-secure!")
)

// TokenClaims represents standard and custom JWT claims for local authentication.
type TokenClaims struct {
	Subject            string      `json:"sub"`
	Username           string      `json:"username"`
	Email              string      `json:"email,omitempty"`
	Role               Role        `json:"role"`
	AllowedVCenterIDs  []uuid.UUID `json:"vcenter_ids,omitempty"`
	AllowedFolderPaths []string    `json:"folder_paths,omitempty"`
	IssuedAt           int64       `json:"iat"`
	ExpiresAt          int64       `json:"exp"`
}

// TokenSigner generates and verifies HS256 JWT tokens for local users.
type TokenSigner struct {
	secret []byte
	ttl    time.Duration
}

// NewTokenSigner initializes a TokenSigner with the given secret and default TTL.
func NewTokenSigner(secret []byte, ttl time.Duration) *TokenSigner {
	if len(secret) == 0 {
		secret = DefaultJWTSecret
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	return &TokenSigner{
		secret: secret,
		ttl:    ttl,
	}
}

// IssueToken mints a signed HS256 JWT token using the default signer TTL.
func (s *TokenSigner) IssueToken(id *Identity) (string, time.Time, error) {
	return s.IssueTokenWithTTL(id, s.ttl)
}

// IssueTokenWithTTL mints a signed HS256 JWT token with a specific expiration TTL.
func (s *TokenSigner) IssueTokenWithTTL(id *Identity, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(ttl)

	claims := TokenClaims{
		Subject:            id.Subject,
		Username:           id.Subject,
		Email:              id.Email,
		Role:               id.Role,
		AllowedVCenterIDs:  id.AllowedVCenterIDs,
		AllowedFolderPaths: id.AllowedFolderPaths,
		IssuedAt:           now.Unix(),
		ExpiresAt:          expiresAt.Unix(),
	}

	headerJSON := []byte(`{"alg":"HS256","typ":"JWT"}`)
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("failed to marshal token claims: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)
	unsigned := headerB64 + "." + claimsB64

	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(unsigned))
	sigB64 := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	token := unsigned + "." + sigB64
	return token, expiresAt, nil
}

// VerifyToken validates the signature and expiration of an HS256 JWT token.
func (s *TokenSigner) VerifyToken(tokenStr string) (*Identity, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, ErrTokenInvalid
	}

	unsigned := parts[0] + "." + parts[1]
	providedSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrTokenInvalid
	}

	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(unsigned))
	expectedSig := mac.Sum(nil)

	if !hmac.Equal(providedSig, expectedSig) {
		return nil, ErrTokenInvalid
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrTokenInvalid
	}

	var claims TokenClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, ErrTokenInvalid
	}

	if time.Now().Unix() > claims.ExpiresAt {
		return nil, ErrTokenExpired
	}

	return &Identity{
		Subject:            claims.Subject,
		Email:              claims.Email,
		Role:               claims.Role,
		AllowedVCenterIDs:  claims.AllowedVCenterIDs,
		AllowedFolderPaths: claims.AllowedFolderPaths,
	}, nil
}
