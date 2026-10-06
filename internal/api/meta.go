package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/search"
)

// VMFieldMeta describes metadata for a queryable field.
type VMFieldMeta struct {
	Name             string   `json:"name"`
	Type             string   `json:"type"`
	AllowedOperators []string `json:"allowed_operators"`
	Description      string   `json:"description"`
}

// VMFieldsMetadataResponse encapsulates the fields dictionary.
type VMFieldsMetadataResponse struct {
	Fields []VMFieldMeta `json:"fields"`
}

// TagCatalogItem represents a tag entry with VM usage counts.
type TagCatalogItem struct {
	CategoryName string `json:"category_name"`
	TagName      string `json:"tag_name"`
	VMCount      int64  `json:"vm_count"`
}

// TagListResponse is the envelope for GET /v1/tags.
type TagListResponse struct {
	Items []TagCatalogItem `json:"items"`
}

// CustomAttributeCatalogItem represents an attribute with counts and sample values.
type CustomAttributeCatalogItem struct {
	Name         string   `json:"name"`
	VMCount      int64    `json:"vm_count"`
	SampleValues []string `json:"sample_values"`
}

// CustomAttributeListResponse is the envelope for GET /v1/custom-attributes.
type CustomAttributeListResponse struct {
	Items []CustomAttributeCatalogItem `json:"items"`
}

// MeResponse represents the caller's identity and authorized scopes.
type MeResponse struct {
	Subject     string      `json:"subject"`
	Role        string      `json:"role"`
	VCenterIDs  []uuid.UUID `json:"vcenter_ids"`
	FolderPaths []string    `json:"folder_paths"`
}

func (h *Handler) handleGetVMFields(w http.ResponseWriter, r *http.Request) {
	fields := make([]VMFieldMeta, 0, len(search.AllowedFields))
	for _, def := range search.AllowedFields {
		ops := make([]string, len(def.AllowedOperators))
		for i, op := range def.AllowedOperators {
			ops[i] = string(op)
		}
		fields = append(fields, VMFieldMeta{
			Name:             def.Name,
			Type:             string(def.Type),
			AllowedOperators: ops,
			Description:      def.Description,
		})
	}

	sort.Slice(fields, func(i, j int) bool {
		return fields[i].Name < fields[j].Name
	})

	h.writeJSON(w, http.StatusOK, VMFieldsMetadataResponse{Fields: fields})
}

func (h *Handler) handleListTags(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	var scope *search.ScopeConstraint
	if id, ok := auth.FromContext(r.Context()); ok && id != nil {
		scope = id.ToScopeConstraint()
	}
	scopeSQL, scopeArgs := buildScopeWhere(scope)

	query := `SELECT vt.category_name, vt.tag_name, count(DISTINCT vt.vm_id)
		FROM vm_tags vt
		JOIN virtual_machines vm ON vt.vm_id = vm.id
		WHERE vm.deleted_at IS NULL` + scopeSQL

	args := append([]any{}, scopeArgs...)
	if category != "" {
		args = append(args, category)
		query += fmt.Sprintf(" AND vt.category_name = $%d", len(args))
	}

	args = append(args, limit)
	query += fmt.Sprintf(" GROUP BY vt.category_name, vt.tag_name ORDER BY vt.category_name ASC, vt.tag_name ASC LIMIT $%d", len(args))

	rows, err := h.pool.Query(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to list tags", "error", err)
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to list tags", "database_error")
		return
	}
	defer rows.Close()

	items := make([]TagCatalogItem, 0)
	for rows.Next() {
		var item TagCatalogItem
		if err := rows.Scan(&item.CategoryName, &item.TagName, &item.VMCount); err != nil {
			h.logger.Error("failed to scan tag item", "error", err)
			h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "scan error", "database_error")
			return
		}
		items = append(items, item)
	}

	h.writeJSON(w, http.StatusOK, TagListResponse{Items: items})
}

func (h *Handler) handleListCustomAttributes(w http.ResponseWriter, r *http.Request) {
	attrName := strings.TrimSpace(r.URL.Query().Get("name"))
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	var scope *search.ScopeConstraint
	if id, ok := auth.FromContext(r.Context()); ok && id != nil {
		scope = id.ToScopeConstraint()
	}
	scopeSQL, scopeArgs := buildScopeWhere(scope)

	query := `SELECT vca.name, count(DISTINCT vca.vm_id), array_agg(DISTINCT vca.value)
		FROM vm_custom_attributes vca
		JOIN virtual_machines vm ON vca.vm_id = vm.id
		WHERE vm.deleted_at IS NULL` + scopeSQL

	args := append([]any{}, scopeArgs...)
	if attrName != "" {
		args = append(args, attrName)
		query += fmt.Sprintf(" AND vca.name = $%d", len(args))
	}

	args = append(args, limit)
	query += fmt.Sprintf(" GROUP BY vca.name ORDER BY vca.name ASC LIMIT $%d", len(args))

	rows, err := h.pool.Query(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to list custom attributes", "error", err)
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to list attributes", "database_error")
		return
	}
	defer rows.Close()

	items := make([]CustomAttributeCatalogItem, 0)
	for rows.Next() {
		var item CustomAttributeCatalogItem
		var rawValues []string
		if err := rows.Scan(&item.Name, &item.VMCount, &rawValues); err != nil {
			h.logger.Error("failed to scan attribute item", "error", err)
			h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "scan error", "database_error")
			return
		}
		if len(rawValues) > 5 {
			rawValues = rawValues[:5]
		}
		item.SampleValues = rawValues
		items = append(items, item)
	}

	h.writeJSON(w, http.StatusOK, CustomAttributeListResponse{Items: items})
}

func (h *Handler) handleGetMe(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.FromContext(r.Context())
	if !ok || id == nil {
		h.writeProblem(w, http.StatusUnauthorized, "Unauthorized", "no caller identity in context", "unauthorized")
		return
	}

	res := MeResponse{
		Subject:     id.Subject,
		Role:        string(id.Role),
		VCenterIDs:  id.AllowedVCenterIDs,
		FolderPaths: id.AllowedFolderPaths,
	}
	if res.VCenterIDs == nil {
		res.VCenterIDs = []uuid.UUID{}
	}
	if res.FolderPaths == nil {
		res.FolderPaths = []string{}
	}

	h.writeJSON(w, http.StatusOK, res)
}
