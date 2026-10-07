package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/search"
	"github.com/jackc/pgx/v5/pgtype"
)

// TopologyItem represents a general topology entity.
type TopologyItem struct {
	ID            string  `json:"id"`
	VCenterID     string  `json:"vcenter_id"`
	Name          string  `json:"name"`
	MoRef         string  `json:"moref,omitempty"`
	DatacenterID  *string `json:"datacenter_id,omitempty"`
	ClusterID     *string `json:"cluster_id,omitempty"`
	Type          *string `json:"type,omitempty"`
	CapacityBytes *int64  `json:"capacity_bytes,omitempty"`
	FreeBytes     *int64  `json:"free_bytes,omitempty"`
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

		if scope != nil && len(scope.AllowedVCenterIDs) == 0 {
			h.writeJSON(w, http.StatusOK, TopologyListResponse{Items: []TopologyItem{}})
			return
		}

		var query string
		switch table {
		case "clusters":
			query = "SELECT id, vcenter_id, name, moref, datacenter_id FROM clusters WHERE 1=1"
		case "hosts":
			query = "SELECT id, vcenter_id, name, moref, cluster_id, datacenter_id FROM hosts WHERE 1=1"
		case "datastores":
			query = "SELECT id, vcenter_id, name, moref, datacenter_id, type, capacity_bytes, free_bytes FROM datastores WHERE 1=1"
		case "networks":
			query = "SELECT id, vcenter_id, name, moref, datacenter_id FROM networks WHERE 1=1"
		default:
			query = fmt.Sprintf("SELECT id, vcenter_id, name, moref FROM %s WHERE 1=1", table)
		}

		var args []any
		if scope != nil && len(scope.AllowedVCenterIDs) > 0 {
			params := make([]string, len(scope.AllowedVCenterIDs))
			for i, vcid := range scope.AllowedVCenterIDs {
				args = append(args, vcid)
				params[i] = fmt.Sprintf("$%d", len(args))
			}
			query += fmt.Sprintf(" AND vcenter_id IN (%s)", strings.Join(params, ", "))
		}
		query += " ORDER BY name ASC LIMIT 250"

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
			item := TopologyItem{}

			switch table {
			case "clusters":
				var dcID *uuid.UUID
				if err := rows.Scan(&id, &vcenterID, &name, &moref, &dcID); err != nil {
					h.logger.Error("failed to scan cluster item", "error", err)
					h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "scan error", "database_error")
					return
				}
				if dcID != nil {
					s := dcID.String()
					item.DatacenterID = &s
				}
			case "hosts":
				var clID, dcID *uuid.UUID
				if err := rows.Scan(&id, &vcenterID, &name, &moref, &clID, &dcID); err != nil {
					h.logger.Error("failed to scan host item", "error", err)
					h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "scan error", "database_error")
					return
				}
				if clID != nil {
					s := clID.String()
					item.ClusterID = &s
				}
				if dcID != nil {
					s := dcID.String()
					item.DatacenterID = &s
				}
			case "datastores":
				var dcID *uuid.UUID
				var dsType string
				var capBytes, freeBytes int64
				if err := rows.Scan(&id, &vcenterID, &name, &moref, &dcID, &dsType, &capBytes, &freeBytes); err != nil {
					h.logger.Error("failed to scan datastore item", "error", err)
					h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "scan error", "database_error")
					return
				}
				if dcID != nil {
					s := dcID.String()
					item.DatacenterID = &s
				}
				item.Type = &dsType
				item.CapacityBytes = &capBytes
				item.FreeBytes = &freeBytes
			case "networks":
				var dcID *uuid.UUID
				if err := rows.Scan(&id, &vcenterID, &name, &moref, &dcID); err != nil {
					h.logger.Error("failed to scan network item", "error", err)
					h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "scan error", "database_error")
					return
				}
				if dcID != nil {
					s := dcID.String()
					item.DatacenterID = &s
				}
			default:
				if err := rows.Scan(&id, &vcenterID, &name, &moref); err != nil {
					h.logger.Error("failed to scan topology item", "error", err)
					h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "scan error", "database_error")
					return
				}
			}

			item.ID = id.String()
			item.VCenterID = vcenterID.String()
			item.Name = name
			item.MoRef = moref
			items = append(items, item)
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

		if scope != nil && len(scope.AllowedVCenterIDs) == 0 {
			h.writeProblem(w, http.StatusNotFound, "Not Found", fmt.Sprintf("%s not found", table), "not_found")
			return
		}

		var query string
		switch table {
		case "clusters":
			query = "SELECT id, vcenter_id, name, moref, datacenter_id FROM clusters WHERE id = $1"
		case "hosts":
			query = "SELECT id, vcenter_id, name, moref, cluster_id, datacenter_id FROM hosts WHERE id = $1"
		case "datastores":
			query = "SELECT id, vcenter_id, name, moref, datacenter_id, type, capacity_bytes, free_bytes FROM datastores WHERE id = $1"
		case "networks":
			query = "SELECT id, vcenter_id, name, moref, datacenter_id FROM networks WHERE id = $1"
		default:
			query = fmt.Sprintf("SELECT id, vcenter_id, name, moref FROM %s WHERE id = $1", table)
		}

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

		switch table {
		case "clusters":
			var dcID *uuid.UUID
			err = h.pool.QueryRow(r.Context(), query, args...).Scan(&uid, &vcid, &item.Name, &item.MoRef, &dcID)
			if dcID != nil {
				s := dcID.String()
				item.DatacenterID = &s
			}
		case "hosts":
			var clID, dcID *uuid.UUID
			err = h.pool.QueryRow(r.Context(), query, args...).Scan(&uid, &vcid, &item.Name, &item.MoRef, &clID, &dcID)
			if clID != nil {
				s := clID.String()
				item.ClusterID = &s
			}
			if dcID != nil {
				s := dcID.String()
				item.DatacenterID = &s
			}
		case "datastores":
			var dcID *uuid.UUID
			var dsType string
			var capBytes, freeBytes int64
			err = h.pool.QueryRow(r.Context(), query, args...).Scan(&uid, &vcid, &item.Name, &item.MoRef, &dcID, &dsType, &capBytes, &freeBytes)
			if dcID != nil {
				s := dcID.String()
				item.DatacenterID = &s
			}
			item.Type = &dsType
			item.CapacityBytes = &capBytes
			item.FreeBytes = &freeBytes
		case "networks":
			var dcID *uuid.UUID
			err = h.pool.QueryRow(r.Context(), query, args...).Scan(&uid, &vcid, &item.Name, &item.MoRef, &dcID)
			if dcID != nil {
				s := dcID.String()
				item.DatacenterID = &s
			}
		default:
			err = h.pool.QueryRow(r.Context(), query, args...).Scan(&uid, &vcid, &item.Name, &item.MoRef)
		}

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

	if scope != nil && len(scope.AllowedVCenterIDs) == 0 && len(scope.AllowedFolderPaths) == 0 {
		h.writeJSON(w, http.StatusOK, FolderListResponse{Items: []FolderItem{}})
		return
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

	if scope != nil {
		if len(scope.AllowedVCenterIDs) > 0 {
			params := make([]string, len(scope.AllowedVCenterIDs))
			for i, vcid := range scope.AllowedVCenterIDs {
				args = append(args, vcid)
				params[i] = fmt.Sprintf("$%d", len(args))
			}
			query += fmt.Sprintf(" AND f.vcenter_id IN (%s)", strings.Join(params, ", "))
		}
		if len(scope.AllowedFolderPaths) > 0 {
			folderParts := make([]string, len(scope.AllowedFolderPaths))
			for i, p := range scope.AllowedFolderPaths {
				args = append(args, p)
				folderParts[i] = fmt.Sprintf("(f.path <@ $%d::ltree OR $%d::ltree <@ f.path)", len(args), len(args))
			}
			query += fmt.Sprintf(" AND (%s)", strings.Join(folderParts, " OR "))
		}
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

	// Enforce tenant authorization boundaries to eliminate BOLA/IDOR
	if callerID, ok := auth.FromContext(r.Context()); ok && callerID != nil {
		if !callerID.CanAccessVCenter(vcid) {
			h.writeProblem(w, http.StatusNotFound, "Not Found", "folder not found", "not_found")
			return
		}
		if len(callerID.AllowedFolderPaths) > 0 {
			allowed := false
			for _, p := range callerID.AllowedFolderPaths {
				if item.Path == p || strings.HasPrefix(item.Path, p+".") || strings.HasPrefix(p, item.Path+".") {
					allowed = true
					break
				}
			}
			if !allowed {
				h.writeProblem(w, http.StatusNotFound, "Not Found", "folder not found", "not_found")
				return
			}
		}
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

		limit := DefaultPageLimit
		if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
			parsed, err := strconv.Atoi(limitStr)
			if err != nil || parsed <= 0 {
				h.writeProblem(w, http.StatusBadRequest, "Bad Request", "limit must be a positive integer", "invalid_parameter")
				return
			}
			if parsed > MaxPageLimit {
				limit = MaxPageLimit
			} else {
				limit = parsed
			}
		}
		rawCursor := r.URL.Query().Get("cursor")

		h.executeSearch(w, r, filter, search.SortSpec{Field: "name", Direction: "asc"}, limit, rawCursor, topologyType+":"+targetID.String())
	}
}

func (h *Handler) lookupVMIDsByDatastore(ctx context.Context, dsID uuid.UUID) ([]uuid.UUID, error) {
	pgUUIDs, err := h.queries.GetVMIDsByDatastoreID(ctx, pgtype.UUID{Bytes: dsID, Valid: true})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(pgUUIDs))
	for i, u := range pgUUIDs {
		ids[i] = u.Bytes
	}
	return ids, nil
}

func (h *Handler) lookupVMIDsByNetwork(ctx context.Context, netID uuid.UUID) ([]uuid.UUID, error) {
	pgUUIDs, err := h.queries.GetVMIDsByNetworkID(ctx, pgtype.UUID{Bytes: netID, Valid: true})
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(pgUUIDs))
	for i, u := range pgUUIDs {
		ids[i] = u.Bytes
	}
	return ids, nil
}
