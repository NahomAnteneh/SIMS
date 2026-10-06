package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/search"
)

// TopologyItem represents a general topology entity.
type TopologyItem struct {
	ID        string `json:"id"`
	VCenterID string `json:"vcenter_id"`
	Name      string `json:"name"`
	MoRef     string `json:"moref,omitempty"`
}

// TopologyListResponse encapsulates lists of topology entities.
type TopologyListResponse struct {
	Items []TopologyItem `json:"items"`
}

// FolderItem represents an inventory folder with VM count.
type FolderItem struct {
	ID        string `json:"id"`
	VCenterID string `json:"vcenter_id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	VMCount   int64  `json:"vm_count"`
}

// FolderListResponse encapsulates folder list results.
type FolderListResponse struct {
	Items []FolderItem `json:"items"`
}

func (h *Handler) handleListTopology(table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var scope *search.ScopeConstraint
		if id, ok := auth.FromContext(r.Context()); ok && id != nil {
			scope = id.ToScopeConstraint()
		}

		query := fmt.Sprintf("SELECT id, vcenter_id, name, moref FROM %s WHERE 1=1", table)
		var args []any
		if scope != nil && len(scope.AllowedVCenterIDs) > 0 {
			params := make([]string, len(scope.AllowedVCenterIDs))
			for i, vcid := range scope.AllowedVCenterIDs {
				args = append(args, vcid)
				params[i] = fmt.Sprintf("$%d", len(args))
			}
			query += fmt.Sprintf(" AND vcenter_id IN (%s)", strings.Join(params, ", "))
		}
		query += " ORDER BY name ASC LIMIT 100"

		rows, err := h.pool.Query(r.Context(), query, args...)
		if err != nil {
			h.logger.Error("failed to query topology", "table", table, "error", err)
			h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "database error", "database_error")
			return
		}
		defer rows.Close()

		items := make([]TopologyItem, 0)
		for rows.Next() {
			var id, vcenterID uuid.UUID
			var name, moref string
			if err := rows.Scan(&id, &vcenterID, &name, &moref); err != nil {
				h.logger.Error("failed to scan topology item", "error", err)
				h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "scan error", "database_error")
				return
			}
			items = append(items, TopologyItem{
				ID:        id.String(),
				VCenterID: vcenterID.String(),
				Name:      name,
				MoRef:     moref,
			})
		}

		h.writeJSON(w, http.StatusOK, TopologyListResponse{Items: items})
	}
}

func (h *Handler) handleGetTopologyByID(table string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := uuid.Parse(idStr)
		if err != nil {
			h.writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid uuid format", "invalid_id")
			return
		}

		var scope *search.ScopeConstraint
		if authID, ok := auth.FromContext(r.Context()); ok && authID != nil {
			scope = authID.ToScopeConstraint()
		}

		query := fmt.Sprintf("SELECT id, vcenter_id, name, moref FROM %s WHERE id = $1", table)
		args := []any{id}
		if scope != nil && len(scope.AllowedVCenterIDs) > 0 {
			params := make([]string, len(scope.AllowedVCenterIDs))
			for i, vcid := range scope.AllowedVCenterIDs {
				args = append(args, vcid)
				params[i] = fmt.Sprintf("$%d", len(args))
			}
			query += fmt.Sprintf(" AND vcenter_id IN (%s)", strings.Join(params, ", "))
		}

		var item TopologyItem
		var uid, vcid uuid.UUID
		err = h.pool.QueryRow(r.Context(), query, args...).Scan(&uid, &vcid, &item.Name, &item.MoRef)
		if err != nil {
			h.writeProblem(w, http.StatusNotFound, "Not Found", fmt.Sprintf("%s not found", table), "not_found")
			return
		}

		item.ID = uid.String()
		item.VCenterID = vcid.String()
		h.writeJSON(w, http.StatusOK, item)
	}
}

func (h *Handler) handleListFolders(w http.ResponseWriter, r *http.Request) {
	var scope *search.ScopeConstraint
	if id, ok := auth.FromContext(r.Context()); ok && id != nil {
		scope = id.ToScopeConstraint()
	}

	parentIDStr := strings.TrimSpace(r.URL.Query().Get("parent_id"))

	query := `SELECT f.id, f.vcenter_id, f.name, f.path::text, count(vm.id)
		FROM inventory_folders f
		LEFT JOIN virtual_machines vm ON vm.folder_id = f.id AND vm.deleted_at IS NULL
		WHERE 1=1`

	var args []any
	if parentIDStr != "" {
		if pID, err := uuid.Parse(parentIDStr); err == nil {
			args = append(args, pID)
			query += fmt.Sprintf(" AND f.parent_id = $%d", len(args))
		}
	}

	if scope != nil && len(scope.AllowedVCenterIDs) > 0 {
		params := make([]string, len(scope.AllowedVCenterIDs))
		for i, vcid := range scope.AllowedVCenterIDs {
			args = append(args, vcid)
			params[i] = fmt.Sprintf("$%d", len(args))
		}
		query += fmt.Sprintf(" AND f.vcenter_id IN (%s)", strings.Join(params, ", "))
	}

	query += ` GROUP BY f.id, f.vcenter_id, f.name, f.path ORDER BY f.path ASC LIMIT 100`

	rows, err := h.pool.Query(r.Context(), query, args...)
	if err != nil {
		h.logger.Error("failed to list folders", "error", err)
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "database error", "database_error")
		return
	}
	defer rows.Close()

	items := make([]FolderItem, 0)
	for rows.Next() {
		var id, vcenterID uuid.UUID
		var name, path string
		var vmCount int64
		if err := rows.Scan(&id, &vcenterID, &name, &path, &vmCount); err != nil {
			h.logger.Error("failed to scan folder item", "error", err)
			h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "scan error", "database_error")
			return
		}
		items = append(items, FolderItem{
			ID:        id.String(),
			VCenterID: vcenterID.String(),
			Name:      name,
			Path:      path,
			VMCount:   vmCount,
		})
	}

	h.writeJSON(w, http.StatusOK, FolderListResponse{Items: items})
}

func (h *Handler) handleGetFolderByID(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid uuid format", "invalid_id")
		return
	}

	query := `SELECT f.id, f.vcenter_id, f.name, f.path::text, count(vm.id)
		FROM inventory_folders f
		LEFT JOIN virtual_machines vm ON vm.folder_id = f.id AND vm.deleted_at IS NULL
		WHERE f.id = $1
		GROUP BY f.id, f.vcenter_id, f.name, f.path`

	var item FolderItem
	var uid, vcid uuid.UUID
	err = h.pool.QueryRow(r.Context(), query, id).Scan(&uid, &vcid, &item.Name, &item.Path, &item.VMCount)
	if err != nil {
		h.writeProblem(w, http.StatusNotFound, "Not Found", "folder not found", "not_found")
		return
	}

	item.ID = uid.String()
	item.VCenterID = vcid.String()
	h.writeJSON(w, http.StatusOK, item)
}

func (h *Handler) handleListTopologyVMs(topologyType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		targetID, err := uuid.Parse(idStr)
		if err != nil {
			h.writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid uuid format", "invalid_id")
			return
		}

		var filter *search.FilterNode
		switch topologyType {
		case "datacenter":
			filter = &search.FilterNode{Op: search.OpEQ, Field: "datacenter_id", Value: targetID.String()}
		case "cluster":
			filter = &search.FilterNode{Op: search.OpEQ, Field: "cluster_id", Value: targetID.String()}
		case "host":
			filter = &search.FilterNode{Op: search.OpEQ, Field: "host_id", Value: targetID.String()}
		case "datastore":
			// Datastore attachment lookup
			vmIDs, err := h.lookupVMIDsByDatastore(r.Context(), targetID)
			if err != nil {
				h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "datastore lookup failed", "database_error")
				return
			}
			if len(vmIDs) == 0 {
				h.writeJSON(w, http.StatusOK, SearchVMsResponse{Items: []VMListItem{}})
				return
			}
			var anyIDs []any
			for _, id := range vmIDs {
				anyIDs = append(anyIDs, id)
			}
			filter = &search.FilterNode{Op: search.OpIN, Field: "id", Values: anyIDs}
		case "network":
			// Network attachment lookup
			vmIDs, err := h.lookupVMIDsByNetwork(r.Context(), targetID)
			if err != nil {
				h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "network lookup failed", "database_error")
				return
			}
			if len(vmIDs) == 0 {
				h.writeJSON(w, http.StatusOK, SearchVMsResponse{Items: []VMListItem{}})
				return
			}
			var anyIDs []any
			for _, id := range vmIDs {
				anyIDs = append(anyIDs, id)
			}
			filter = &search.FilterNode{Op: search.OpIN, Field: "id", Values: anyIDs}
		case "folder":
			filter = &search.FilterNode{Op: search.OpEQ, Field: "folder_id", Value: targetID.String()}
		}

		h.executeSearch(w, r, filter, search.SortSpec{Field: "name", Direction: "asc"}, DefaultPageLimit, "", topologyType+":"+targetID.String())
	}
}

func (h *Handler) lookupVMIDsByDatastore(ctx context.Context, dsID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := h.pool.Query(ctx, "SELECT vm_id FROM vm_datastores WHERE datastore_id = $1", dsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (h *Handler) lookupVMIDsByNetwork(ctx context.Context, netID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := h.pool.Query(ctx, "SELECT DISTINCT vm_id FROM vm_network_interfaces WHERE network_id = $1", netID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
