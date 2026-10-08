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
	VMCount       *int64  `json:"vm_count,omitempty"`
	VlanIDs       *string `json:"vlan_ids,omitempty"`
	SwitchName    *string `json:"switch_name,omitempty"`
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
			query = `SELECT 
				h.id, h.vcenter_id, h.name, h.moref, h.cluster_id, h.datacenter_id,
				COUNT(DISTINCT vm.id) AS vm_count
			FROM hosts h
			LEFT JOIN virtual_machines vm ON vm.host_id = h.id AND vm.deleted_at IS NULL
			WHERE 1=1`
		case "datastores":
			query = `SELECT 
				ds.id, ds.vcenter_id, ds.name, ds.moref,
				COALESCE(ds.datacenter_id, (SELECT id FROM datacenters WHERE vcenter_id = ds.vcenter_id LIMIT 1)) AS datacenter_id,
				ds.type, ds.capacity_bytes, ds.free_bytes,
				COUNT(DISTINCT vmd.vm_id) AS vm_count
			FROM datastores ds
			LEFT JOIN vm_datastores vmd ON ds.id = vmd.datastore_id
			WHERE 1=1`
		case "networks":
			query = `SELECT
				id, vcenter_id, name, moref, datacenter_id, type, vlan_ids, switch_name, vm_count
			FROM (
				SELECT
					p.id,
					p.vcenter_id,
					CASE WHEN p.name = '' THEN p.moref ELSE p.name END AS name,
					p.moref,
					COALESCE(ds.datacenter_id, (SELECT id FROM datacenters WHERE vcenter_id = p.vcenter_id LIMIT 1)) AS datacenter_id,
					'Distributed Virtual Portgroup' AS type,
					p.vlan_ids::text AS vlan_ids,
					ds.name AS switch_name,
					COUNT(DISTINCT vnic.vm_id) AS vm_count
				FROM portgroups p
				LEFT JOIN distributed_switches ds ON p.switch_id = ds.id
				LEFT JOIN vm_network_interfaces vnic ON vnic.portgroup_id = p.id
				GROUP BY p.id, p.vcenter_id, p.name, p.moref, ds.datacenter_id, ds.name
				UNION ALL
				SELECT
					n.id,
					n.vcenter_id,
					CASE WHEN n.name = '' THEN n.moref ELSE n.name END AS name,
					n.moref,
					COALESCE(n.datacenter_id, (SELECT id FROM datacenters WHERE vcenter_id = n.vcenter_id LIMIT 1)) AS datacenter_id,
					'Standard Network' AS type,
					n.vlan_ids::text AS vlan_ids,
					NULL AS switch_name,
					COUNT(DISTINCT vnic.vm_id) AS vm_count
				FROM networks n
				LEFT JOIN vm_network_interfaces vnic ON vnic.network_id = n.id
				WHERE n.moref NOT IN (SELECT moref FROM portgroups WHERE vcenter_id = n.vcenter_id)
				GROUP BY n.id, n.vcenter_id, n.name, n.moref, n.datacenter_id
			) combined_networks
			WHERE 1=1`
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
			if table == "datastores" {
				query += fmt.Sprintf(" AND ds.vcenter_id IN (%s)", strings.Join(params, ", "))
			} else if table == "hosts" {
				query += fmt.Sprintf(" AND h.vcenter_id IN (%s)", strings.Join(params, ", "))
			} else {
				query += fmt.Sprintf(" AND vcenter_id IN (%s)", strings.Join(params, ", "))
			}
		}

		if table == "datastores" {
			query += " GROUP BY ds.id, ds.vcenter_id, ds.name, ds.moref, ds.datacenter_id, ds.type, ds.capacity_bytes, ds.free_bytes ORDER BY vm_count DESC, name ASC LIMIT 250"
		} else if table == "hosts" {
			query += " GROUP BY h.id, h.vcenter_id, h.name, h.moref, h.cluster_id, h.datacenter_id ORDER BY vm_count DESC, h.name ASC LIMIT 250"
		} else if table == "networks" {
			query += " ORDER BY vm_count DESC, name ASC LIMIT 250"
		} else {
			query += " ORDER BY name ASC LIMIT 250"
		}

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
				var vmCount int64
				if err := rows.Scan(&id, &vcenterID, &name, &moref, &clID, &dcID, &vmCount); err != nil {
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
				item.VMCount = &vmCount
			case "datastores":
				var dcID *uuid.UUID
				var dsType string
				var capBytes, freeBytes int64
				var vmCount int64
				if err := rows.Scan(&id, &vcenterID, &name, &moref, &dcID, &dsType, &capBytes, &freeBytes, &vmCount); err != nil {
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
				item.VMCount = &vmCount
			case "networks":
				var dcID *uuid.UUID
				var netType string
				var vlanIDs, switchName *string
				var vmCount int64
				if err := rows.Scan(&id, &vcenterID, &name, &moref, &dcID, &netType, &vlanIDs, &switchName, &vmCount); err != nil {
					h.logger.Error("failed to scan network item", "error", err)
					h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "scan error", "database_error")
					return
				}
				if dcID != nil {
					s := dcID.String()
					item.DatacenterID = &s
				}
				item.Type = &netType
				item.VlanIDs = vlanIDs
				item.SwitchName = switchName
				item.VMCount = &vmCount
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
			query = `SELECT 
				h.id, h.vcenter_id, h.name, h.moref, h.cluster_id, h.datacenter_id,
				COUNT(DISTINCT vm.id) AS vm_count
			FROM hosts h
			LEFT JOIN virtual_machines vm ON vm.host_id = h.id AND vm.deleted_at IS NULL
			WHERE h.id = $1`
		case "datastores":
			query = `SELECT 
				ds.id, ds.vcenter_id, ds.name, ds.moref,
				COALESCE(ds.datacenter_id, (SELECT id FROM datacenters WHERE vcenter_id = ds.vcenter_id LIMIT 1)) AS datacenter_id,
				ds.type, ds.capacity_bytes, ds.free_bytes,
				COUNT(DISTINCT vmd.vm_id) AS vm_count
			FROM datastores ds
			LEFT JOIN vm_datastores vmd ON ds.id = vmd.datastore_id
			WHERE ds.id = $1`
		case "networks":
			query = `SELECT
				id, vcenter_id, name, moref, datacenter_id, type, vlan_ids, switch_name, vm_count
			FROM (
				SELECT
					p.id,
					p.vcenter_id,
					CASE WHEN p.name = '' THEN p.moref ELSE p.name END AS name,
					p.moref,
					COALESCE(ds.datacenter_id, (SELECT id FROM datacenters WHERE vcenter_id = p.vcenter_id LIMIT 1)) AS datacenter_id,
					'Distributed Virtual Portgroup' AS type,
					p.vlan_ids::text AS vlan_ids,
					ds.name AS switch_name,
					COUNT(DISTINCT vnic.vm_id) AS vm_count
				FROM portgroups p
				LEFT JOIN distributed_switches ds ON p.switch_id = ds.id
				LEFT JOIN vm_network_interfaces vnic ON vnic.portgroup_id = p.id
				WHERE p.id = $1
				GROUP BY p.id, p.vcenter_id, p.name, p.moref, ds.datacenter_id, ds.name
				UNION ALL
				SELECT
					n.id,
					n.vcenter_id,
					CASE WHEN n.name = '' THEN n.moref ELSE n.name END AS name,
					n.moref,
					COALESCE(n.datacenter_id, (SELECT id FROM datacenters WHERE vcenter_id = n.vcenter_id LIMIT 1)) AS datacenter_id,
					'Standard Network' AS type,
					n.vlan_ids::text AS vlan_ids,
					NULL AS switch_name,
					COUNT(DISTINCT vnic.vm_id) AS vm_count
				FROM networks n
				LEFT JOIN vm_network_interfaces vnic ON vnic.network_id = n.id
				WHERE n.id = $1 AND n.moref NOT IN (SELECT moref FROM portgroups WHERE vcenter_id = n.vcenter_id)
				GROUP BY n.id, n.vcenter_id, n.name, n.moref, n.datacenter_id
			) combined_networks WHERE 1=1`
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
			if table == "datastores" {
				query += fmt.Sprintf(" AND ds.vcenter_id IN (%s)", strings.Join(params, ", "))
			} else if table == "hosts" {
				query += fmt.Sprintf(" AND h.vcenter_id IN (%s)", strings.Join(params, ", "))
			} else {
				query += fmt.Sprintf(" AND vcenter_id IN (%s)", strings.Join(params, ", "))
			}
		}

		if table == "datastores" {
			query += " GROUP BY ds.id, ds.vcenter_id, ds.name, ds.moref, ds.datacenter_id, ds.type, ds.capacity_bytes, ds.free_bytes"
		} else if table == "hosts" {
			query += " GROUP BY h.id, h.vcenter_id, h.name, h.moref, h.cluster_id, h.datacenter_id"
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
			var vmCount int64
			err = h.pool.QueryRow(r.Context(), query, args...).Scan(&uid, &vcid, &item.Name, &item.MoRef, &clID, &dcID, &vmCount)
			if clID != nil {
				s := clID.String()
				item.ClusterID = &s
			}
			if dcID != nil {
				s := dcID.String()
				item.DatacenterID = &s
			}
			item.VMCount = &vmCount
		case "datastores":
			var dcID *uuid.UUID
			var dsType string
			var capBytes, freeBytes int64
			var vmCount int64
			err = h.pool.QueryRow(r.Context(), query, args...).Scan(&uid, &vcid, &item.Name, &item.MoRef, &dcID, &dsType, &capBytes, &freeBytes, &vmCount)
			if dcID != nil {
				s := dcID.String()
				item.DatacenterID = &s
			}
			item.Type = &dsType
			item.CapacityBytes = &capBytes
			item.FreeBytes = &freeBytes
			item.VMCount = &vmCount
		case "networks":
			var dcID *uuid.UUID
			var netType string
			var vlanIDs, switchName *string
			var vmCount int64
			err = h.pool.QueryRow(r.Context(), query, args...).Scan(&uid, &vcid, &item.Name, &item.MoRef, &dcID, &netType, &vlanIDs, &switchName, &vmCount)
			if dcID != nil {
				s := dcID.String()
				item.DatacenterID = &s
			}
			item.Type = &netType
			item.VlanIDs = vlanIDs
			item.SwitchName = switchName
			item.VMCount = &vmCount
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
