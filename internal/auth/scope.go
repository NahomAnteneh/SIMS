package auth

import (
	"context"
	"net/http"
	"slices"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/search"
)

type Role string

const (
	RoleUser  Role = "user"
	RoleAdmin Role = "admin"

	// Backward-compatibility aliases for legacy tokens
	RoleViewer         Role = RoleUser
	RoleInventoryAdmin Role = RoleAdmin
)

// Identity represents the authenticated caller and their tenant authorization boundaries.
type Identity struct {
	Subject            string      `json:"subject"`
	Email              string      `json:"email,omitempty"`
	Role               Role        `json:"role"`
	AllowedVCenterIDs  []uuid.UUID `json:"vcenter_ids,omitempty"`
	AllowedFolderPaths []string    `json:"folder_paths,omitempty"`
}

// HasRole checks if identity has the specified role or higher.
func (id *Identity) HasRole(r Role) bool {
	if id.Role == RoleAdmin || id.Role == RoleInventoryAdmin {
		return true
	}
	return id.Role == r || (id.Role == RoleUser && r == RoleViewer)
}

// CanAccessVCenter returns true if the vcenter is allowed for this identity.
func (id *Identity) CanAccessVCenter(vcenterID uuid.UUID) bool {
	if id.Role == RoleAdmin || id.Role == RoleInventoryAdmin {
		if len(id.AllowedVCenterIDs) == 0 {
			return true
		}
		return slices.Contains(id.AllowedVCenterIDs, vcenterID)
	}
	if len(id.AllowedVCenterIDs) == 0 {
		return false
	}
	return slices.Contains(id.AllowedVCenterIDs, vcenterID)
}

// ToScopeConstraint builds the tenant boundary for search compiler injection.
func (id *Identity) ToScopeConstraint() *search.ScopeConstraint {
	if (id.Role == RoleAdmin || id.Role == RoleInventoryAdmin) && len(id.AllowedVCenterIDs) == 0 && len(id.AllowedFolderPaths) == 0 {
		// Admin with no restrictions
		return nil
	}
	return &search.ScopeConstraint{
		AllowedVCenterIDs:  id.AllowedVCenterIDs,
		AllowedFolderPaths: id.AllowedFolderPaths,
	}
}

type contextKey struct{ name string }

var identityContextKey = &contextKey{name: "identity"}

// WithIdentity injects Identity into the context.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityContextKey, id)
}

// FromContext extracts Identity from the context.
func FromContext(ctx context.Context) (*Identity, bool) {
	id, ok := ctx.Value(identityContextKey).(*Identity)
	return id, ok && id != nil
}

// RequireRole returns middleware ensuring caller has the requested role.
func RequireRole(role Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := FromContext(r.Context())
			if !ok {
				http.Error(w, `{"type":"about:blank","title":"Unauthorized","status":401,"detail":"authentication required","code":"unauthorized"}`, http.StatusUnauthorized)
				return
			}

			if !id.HasRole(role) {
				http.Error(w, `{"type":"about:blank","title":"Forbidden","status":403,"detail":"insufficient role privileges","code":"forbidden"}`, http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
