package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// LoginRequest defines credentials for local authentication.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse returns an issued JWT token and authenticated user metadata.
type LoginResponse struct {
	Token     string       `json:"token"`
	TokenType string       `json:"token_type"`
	ExpiresAt string       `json:"expires_at"`
	User      UserResponse `json:"user"`
}

// CreateUserRequest defines attributes to provision a new user.
type CreateUserRequest struct {
	Username           string      `json:"username"`
	Password           string      `json:"password"`
	Email              *string     `json:"email,omitempty"`
	Role               string      `json:"role"`
	AllowedVCenterIDs  []uuid.UUID `json:"allowed_vcenter_ids,omitempty"`
	AllowedFolderPaths []string    `json:"allowed_folder_paths,omitempty"`
}

// UpdateUserRequest allows updating an existing user's credentials or role.
type UpdateUserRequest struct {
	Password *string `json:"password,omitempty"`
}

// UserResponse represents user profile data without exposing sensitive credentials.
type UserResponse struct {
	ID                 string      `json:"id"`
	Username           string      `json:"username"`
	Email              *string     `json:"email,omitempty"`
	Role               string      `json:"role"`
	AllowedVCenterIDs  []uuid.UUID `json:"allowed_vcenter_ids"`
	AllowedFolderPaths []string    `json:"allowed_folder_paths"`
	CreatedAt          string      `json:"created_at"`
	UpdatedAt          string      `json:"updated_at"`
}

// UserListResponse contains a list of provisioned users.
type UserListResponse struct {
	Items []UserResponse `json:"items"`
}

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if h.userStore == nil || h.tokenSigner == nil {
		h.writeProblem(w, http.StatusNotImplemented, "Not Implemented", "local authentication not configured", "not_implemented")
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "malformed request payload", "invalid_payload")
		return
	}

	if req.Username == "" || req.Password == "" {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "username and password are required", "invalid_credentials")
		return
	}

	identity, err := h.userStore.AuthenticateUser(r.Context(), req.Username, req.Password)
	if err != nil {
		h.writeProblem(w, http.StatusUnauthorized, "Unauthorized", "invalid username or password", "unauthorized")
		return
	}

	token, expiresAt, err := h.tokenSigner.IssueToken(identity)
	if err != nil {
		h.logger.Error("failed to issue jwt token", slog.String("error", err.Error()))
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to issue authentication token", "internal_error")
		return
	}

	// Fetch user details for profile response
	userRow, err := h.queries.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		h.logger.Error("failed to retrieve user profile", slog.String("error", err.Error()))
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to retrieve user details", "internal_error")
		return
	}

	var emailPtr *string
	if userRow.Email.Valid {
		emailPtr = &userRow.Email.String
	}

	var vcenterIDs []uuid.UUID
	for _, id := range userRow.AllowedVcenterIds {
		if id.Valid {
			vcenterIDs = append(vcenterIDs, id.Bytes)
		}
	}

	resp := LoginResponse{
		Token:     token,
		TokenType: "Bearer",
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
		User: UserResponse{
			ID:                 uuid.UUID(userRow.ID.Bytes).String(),
			Username:           userRow.Username,
			Email:              emailPtr,
			Role:               userRow.Role,
			AllowedVCenterIDs:  vcenterIDs,
			AllowedFolderPaths: userRow.AllowedFolderPaths,
			CreatedAt:          userRow.CreatedAt.Time.UTC().Format(time.RFC3339),
			UpdatedAt:          userRow.UpdatedAt.Time.UTC().Format(time.RFC3339),
		},
	}

	actor := getCallerKey(r)
	h.recordAudit(r.Context(), actor, "auth.login", userRow.Username, "SUCCESS", "", map[string]any{"username": req.Username})
	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !requireInventoryAdmin(r) {
		h.writeProblem(w, http.StatusForbidden, "Forbidden", "admin role required to create users", "forbidden")
		return
	}

	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "malformed request payload", "invalid_payload")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "username is required", "invalid_username")
		return
	}

	if len(req.Password) < 6 {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "password must be at least 6 characters long", "weak_password")
		return
	}

	normRole := strings.ToLower(strings.TrimSpace(req.Role))
	if normRole == "" || normRole == "user" || normRole == "viewer" {
		req.Role = string(auth.RoleUser)
	} else if normRole == "admin" || normRole == "inventory-admin" {
		req.Role = string(auth.RoleAdmin)
	} else {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "role must be either 'admin' or 'user'", "invalid_role")
		return
	}

	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		h.logger.Error("failed to hash password", slog.String("error", err.Error()))
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to hash password", "internal_error")
		return
	}

	var emailText pgtype.Text
	if req.Email != nil && *req.Email != "" {
		emailText = pgtype.Text{String: *req.Email, Valid: true}
	}

	var pgVCenterIDs []pgtype.UUID
	for _, id := range req.AllowedVCenterIDs {
		pgVCenterIDs = append(pgVCenterIDs, pgtype.UUID{Bytes: id, Valid: true})
	}

	user, err := h.queries.CreateUser(r.Context(), db.CreateUserParams{
		Username:           req.Username,
		PasswordHash:       passwordHash,
		Email:              emailText,
		Role:               req.Role,
		AllowedVcenterIds:  pgVCenterIDs,
		AllowedFolderPaths: req.AllowedFolderPaths,
	})
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			h.writeProblem(w, http.StatusConflict, "Conflict", fmt.Sprintf("username %q already exists", req.Username), "conflict")
			return
		}
		h.logger.Error("failed to create user in database", slog.String("error", err.Error()))
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to create user", "internal_error")
		return
	}

	var emailPtr *string
	if user.Email.Valid {
		emailPtr = &user.Email.String
	}

	var respVCenterIDs []uuid.UUID
	for _, id := range user.AllowedVcenterIds {
		if id.Valid {
			respVCenterIDs = append(respVCenterIDs, id.Bytes)
		}
	}

	resp := UserResponse{
		ID:                 uuid.UUID(user.ID.Bytes).String(),
		Username:           user.Username,
		Email:              emailPtr,
		Role:               user.Role,
		AllowedVCenterIDs:  respVCenterIDs,
		AllowedFolderPaths: user.AllowedFolderPaths,
		CreatedAt:          user.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:          user.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}

	actor := getCallerKey(r)
	h.recordAudit(r.Context(), actor, "user.create", user.Username, "SUCCESS", "", map[string]any{"username": user.Username, "role": user.Role})
	h.writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if !requireInventoryAdmin(r) {
		h.writeProblem(w, http.StatusForbidden, "Forbidden", "admin role required to list users", "forbidden")
		return
	}

	users, err := h.queries.ListUsers(r.Context())
	if err != nil {
		h.logger.Error("failed to list users", slog.String("error", err.Error()))
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to list users", "internal_error")
		return
	}

	items := make([]UserResponse, 0, len(users))
	for _, u := range users {
		var emailPtr *string
		if u.Email.Valid {
			emailPtr = &u.Email.String
		}

		var vcenterIDs []uuid.UUID
		for _, id := range u.AllowedVcenterIds {
			if id.Valid {
				vcenterIDs = append(vcenterIDs, id.Bytes)
			}
		}

		items = append(items, UserResponse{
			ID:                 uuid.UUID(u.ID.Bytes).String(),
			Username:           u.Username,
			Email:              emailPtr,
			Role:               u.Role,
			AllowedVCenterIDs:  vcenterIDs,
			AllowedFolderPaths: u.AllowedFolderPaths,
			CreatedAt:          u.CreatedAt.Time.UTC().Format(time.RFC3339),
			UpdatedAt:          u.UpdatedAt.Time.UTC().Format(time.RFC3339),
		})
	}

	h.writeJSON(w, http.StatusOK, UserListResponse{Items: items})
}

func (h *Handler) handleGetUser(w http.ResponseWriter, r *http.Request) {
	if !requireInventoryAdmin(r) {
		h.writeProblem(w, http.StatusForbidden, "Forbidden", "admin role required", "forbidden")
		return
	}

	idStr := r.PathValue("id")
	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid user ID format", "invalid_id")
		return
	}

	u, err := h.queries.GetUserByID(r.Context(), pgtype.UUID{Bytes: parsedID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.writeProblem(w, http.StatusNotFound, "Not Found", "user not found", "not_found")
			return
		}
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to retrieve user", "internal_error")
		return
	}

	var emailPtr *string
	if u.Email.Valid {
		emailPtr = &u.Email.String
	}

	var vcenterIDs []uuid.UUID
	for _, id := range u.AllowedVcenterIds {
		if id.Valid {
			vcenterIDs = append(vcenterIDs, id.Bytes)
		}
	}

	resp := UserResponse{
		ID:                 uuid.UUID(u.ID.Bytes).String(),
		Username:           u.Username,
		Email:              emailPtr,
		Role:               u.Role,
		AllowedVCenterIDs:  vcenterIDs,
		AllowedFolderPaths: u.AllowedFolderPaths,
		CreatedAt:          u.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:          u.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}

	h.writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !requireInventoryAdmin(r) {
		h.writeProblem(w, http.StatusForbidden, "Forbidden", "admin role required to delete users", "forbidden")
		return
	}

	idStr := r.PathValue("id")
	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid user ID format", "invalid_id")
		return
	}

	// Prevent self-deletion if identity matches
	if callerID, ok := auth.FromContext(r.Context()); ok && callerID != nil {
		existing, err := h.queries.GetUserByID(r.Context(), pgtype.UUID{Bytes: parsedID, Valid: true})
		if err == nil && existing.Username == callerID.Subject {
			h.writeProblem(w, http.StatusBadRequest, "Bad Request", "cannot delete own user account", "cannot_delete_self")
			return
		}
	}

	err = h.queries.DeleteUser(r.Context(), pgtype.UUID{Bytes: parsedID, Valid: true})
	if err != nil {
		h.logger.Error("failed to delete user", slog.String("error", err.Error()))
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to delete user", "internal_error")
		return
	}

	actor := getCallerKey(r)
	h.recordAudit(r.Context(), actor, "user.delete", idStr, "SUCCESS", "", map[string]any{"id": idStr})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !requireInventoryAdmin(r) {
		h.writeProblem(w, http.StatusForbidden, "Forbidden", "admin role required", "forbidden")
		return
	}

	idStr := r.PathValue("id")
	parsedID, err := uuid.Parse(idStr)
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid user ID format", "invalid_id")
		return
	}

	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "malformed request payload", "invalid_payload")
		return
	}

	if req.Password != nil {
		if len(*req.Password) < 6 {
			h.writeProblem(w, http.StatusBadRequest, "Bad Request", "password must be at least 6 characters long", "weak_password")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to hash password", "internal_error")
			return
		}
		err = h.queries.UpdateUserPassword(r.Context(), db.UpdateUserPasswordParams{
			ID:           pgtype.UUID{Bytes: parsedID, Valid: true},
			PasswordHash: hash,
		})
		if err != nil {
			h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to update password", "internal_error")
			return
		}
	}

	u, err := h.queries.GetUserByID(r.Context(), pgtype.UUID{Bytes: parsedID, Valid: true})
	if err != nil {
		h.writeProblem(w, http.StatusNotFound, "Not Found", "user not found", "not_found")
		return
	}

	var emailPtr *string
	if u.Email.Valid {
		emailPtr = &u.Email.String
	}

	var vcenterIDs []uuid.UUID
	for _, id := range u.AllowedVcenterIds {
		if id.Valid {
			vcenterIDs = append(vcenterIDs, id.Bytes)
		}
	}

	resp := UserResponse{
		ID:                 uuid.UUID(u.ID.Bytes).String(),
		Username:           u.Username,
		Email:              emailPtr,
		Role:               u.Role,
		AllowedVCenterIDs:  vcenterIDs,
		AllowedFolderPaths: u.AllowedFolderPaths,
		CreatedAt:          u.CreatedAt.Time.UTC().Format(time.RFC3339),
		UpdatedAt:          u.UpdatedAt.Time.UTC().Format(time.RFC3339),
	}

	actor := getCallerKey(r)
	h.recordAudit(r.Context(), actor, "user.update", u.Username, "SUCCESS", "", map[string]any{"username": u.Username})
	h.writeJSON(w, http.StatusOK, resp)
}
