package auth

import (
	"context"

	"github.com/google/uuid"
	db "github.com/infrastructure/sims/generated/db"
)

// UserStore defines credential verification against persistent storage.
type UserStore interface {
	AuthenticateUser(ctx context.Context, username, password string) (*Identity, error)
}

// DBUserStore implements UserStore using the PostgreSQL database via sqlc queries.
type DBUserStore struct {
	queries *db.Queries
}

// NewDBUserStore creates a new DBUserStore with the provided queries.
func NewDBUserStore(queries *db.Queries) *DBUserStore {
	return &DBUserStore{queries: queries}
}

// AuthenticateUser verifies username and password, returning caller Identity on success.
func (s *DBUserStore) AuthenticateUser(ctx context.Context, username, password string) (*Identity, error) {
	user, err := s.queries.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, ErrInvalidCredentials
	}

	if !CheckPassword(password, user.PasswordHash) {
		return nil, ErrInvalidCredentials
	}

	var vcenterIDs []uuid.UUID
	for _, id := range user.AllowedVcenterIds {
		if id.Valid {
			vcenterIDs = append(vcenterIDs, id.Bytes)
		}
	}

	return &Identity{
		Subject:            user.Username,
		Email:              user.Email.String,
		Role:               Role(user.Role),
		AllowedVCenterIDs:  vcenterIDs,
		AllowedFolderPaths: user.AllowedFolderPaths,
	}, nil
}
