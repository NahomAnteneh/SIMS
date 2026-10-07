package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/search"
)

// FacetBucket represents a single facet distribution entry.
type FacetBucket struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// FacetsResponse encapsulates facet counts grouped by requested fields.
type FacetsResponse struct {
	Facets map[string][]FacetBucket `json:"facets"`
}

var allowedFacetFields = map[string]bool{
	"power_state":      true,
	"connection_state": true,
	"cluster":          true,
	"datacenter":       true,
	"host":             true,
	"vcenter":          true,
	"tag":              true,
	"guest_os":         true,
}

func (h *Handler) handleGetVMFacets(w http.ResponseWriter, r *http.Request) {
	fieldsParam := r.URL.Query().Get("fields")
	if fieldsParam == "" {
		fieldsParam = "power_state,cluster,datacenter,tag"
	}

	limit := 20
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}

	requestedFields := strings.Split(fieldsParam, ",")
	for _, f := range requestedFields {
		fTrim := strings.TrimSpace(f)
		if !allowedFacetFields[fTrim] {
			h.writeProblem(w, http.StatusBadRequest, "Bad Request", fmt.Sprintf("unsupported facet field %q", fTrim), "invalid_facet_field")
			return
		}
	}

	// Extract tenant scope
	var scope *search.ScopeConstraint
	if id, ok := auth.FromContext(r.Context()); ok && id != nil {
		scope = id.ToScopeConstraint()
	}

	scopeSQL, scopeArgs := buildScopeWhere(scope)

	res := FacetsResponse{
		Facets: make(map[string][]FacetBucket),
	}

	for _, f := range requestedFields {
		fTrim := strings.TrimSpace(f)
		buckets, err := h.computeFieldFacets(r.Context(), fTrim, scopeSQL, scopeArgs, limit)
		if err != nil {
			h.logger.Error("failed to compute facet", "field", fTrim, "error", err)
			h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to compute facets", "database_error")
			return
		}
		res.Facets[fTrim] = buckets
	}

	h.writeJSON(w, http.StatusOK, res)
}

func buildScopeWhere(scope *search.ScopeConstraint) (string, []any) {
	if scope == nil {
		return "", nil
	}

	var parts []string
	var args []any
	argIdx := 1

	if len(scope.AllowedVCenterIDs) > 0 {
		params := make([]string, len(scope.AllowedVCenterIDs))
		for i, id := range scope.AllowedVCenterIDs {
			params[i] = fmt.Sprintf("$%d", argIdx)
			args = append(args, id)
			argIdx++
		}
		parts = append(parts, fmt.Sprintf("vm.vcenter_id IN (%s)", strings.Join(params, ", ")))
	}

	if len(scope.AllowedFolderPaths) > 0 {
		folderParts := make([]string, len(scope.AllowedFolderPaths))
		for i, p := range scope.AllowedFolderPaths {
			folderParts[i] = fmt.Sprintf("vm.folder_path <@ $%d::ltree", argIdx)
			args = append(args, p)
			argIdx++
		}
		if len(folderParts) == 1 {
			parts = append(parts, folderParts[0])
		} else {
			parts = append(parts, fmt.Sprintf("(%s)", strings.Join(folderParts, " OR ")))
		}
	}

	if len(parts) == 0 {
		return " AND 1 = 0", nil
	}
	return " AND " + strings.Join(parts, " AND "), args
}

func (h *Handler) computeFieldFacets(ctx context.Context, field, scopeSQL string, scopeArgs []any, limit int) ([]FacetBucket, error) {
	var query string

	limitArg := fmt.Sprintf("$%d", len(scopeArgs)+1)
	args := append([]any{}, scopeArgs...)
	args = append(args, limit)

	switch field {
	case "power_state", "connection_state", "guest_os":
		query = fmt.Sprintf(`SELECT COALESCE(vm.%s, 'unknown'), count(*)
			FROM virtual_machines vm
			WHERE vm.deleted_at IS NULL %s
			GROUP BY vm.%s
			ORDER BY count(*) DESC
			LIMIT %s`, field, scopeSQL, field, limitArg)

	case "cluster":
		query = fmt.Sprintf(`SELECT COALESCE(c.name, 'unassigned'), count(*)
			FROM virtual_machines vm
			LEFT JOIN clusters c ON vm.cluster_id = c.id
			WHERE vm.deleted_at IS NULL %s
			GROUP BY c.name
			ORDER BY count(*) DESC
			LIMIT %s`, scopeSQL, limitArg)

	case "datacenter":
		query = fmt.Sprintf(`SELECT COALESCE(dc.name, 'unassigned'), count(*)
			FROM virtual_machines vm
			LEFT JOIN datacenters dc ON vm.datacenter_id = dc.id
			WHERE vm.deleted_at IS NULL %s
			GROUP BY dc.name
			ORDER BY count(*) DESC
			LIMIT %s`, scopeSQL, limitArg)

	case "host":
		query = fmt.Sprintf(`SELECT COALESCE(h.name, 'unassigned'), count(*)
			FROM virtual_machines vm
			LEFT JOIN hosts h ON vm.host_id = h.id
			WHERE vm.deleted_at IS NULL %s
			GROUP BY h.name
			ORDER BY count(*) DESC
			LIMIT %s`, scopeSQL, limitArg)

	case "vcenter":
		query = fmt.Sprintf(`SELECT vc.name, count(*)
			FROM virtual_machines vm
			JOIN vcenters vc ON vm.vcenter_id = vc.id
			WHERE vm.deleted_at IS NULL %s
			GROUP BY vc.name
			ORDER BY count(*) DESC
			LIMIT %s`, scopeSQL, limitArg)

	case "tag":
		query = fmt.Sprintf(`SELECT vt.category_name || ':' || vt.tag_name, count(DISTINCT vt.vm_id)
			FROM vm_tags vt
			JOIN virtual_machines vm ON vt.vm_id = vm.id
			WHERE vm.deleted_at IS NULL %s
			GROUP BY vt.category_name, vt.tag_name
			ORDER BY count(DISTINCT vt.vm_id) DESC
			LIMIT %s`, scopeSQL, limitArg)

	default:
		return nil, fmt.Errorf("unknown facet field %s", field)
	}

	rows, err := h.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []FacetBucket
	for rows.Next() {
		var val string
		var count int64
		if err := rows.Scan(&val, &count); err != nil {
			return nil, err
		}
		buckets = append(buckets, FacetBucket{Value: val, Count: count})
	}

	if buckets == nil {
		buckets = []FacetBucket{}
	}
	return buckets, rows.Err()
}
