package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrInvalidCursor = errors.New("invalid pagination cursor")

// PageCursor holds the values of the keyset pagination tuple (name, id).
type PageCursor struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// EncodeCursor produces an opaque, URL-safe base64 string from the keyset tuple.
func EncodeCursor(name, id string) string {
	c := PageCursor{Name: name, ID: id}
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

// DecodeCursor parses an opaque base64 cursor string into PageCursor.
// It returns ErrInvalidCursor if the cursor is malformed.
func DecodeCursor(s string) (*PageCursor, error) {
	if s == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		// Fallback to standard URLEncoding if padded
		data, err = base64.URLEncoding.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid base64 encoding", ErrInvalidCursor)
		}
	}

	var c PageCursor
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("%w: malformed json payload", ErrInvalidCursor)
	}
	if c.ID == "" {
		return nil, fmt.Errorf("%w: missing id in cursor", ErrInvalidCursor)
	}
	return &c, nil
}
