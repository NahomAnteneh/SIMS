package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/logging"
	"github.com/infrastructure/sims/internal/search"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DefaultPageLimit = search.DefaultPageLimit
	MaxPageLimit     = search.MaxPageLimit
)

// Handler serves Query API endpoints.
type Handler struct {
	pool        *pgxpool.Pool
	queries     *db.Queries
	logger      *slog.Logger
	tokenSigner *auth.TokenSigner
	userStore   auth.UserStore
}

// NewHandler creates a new API Handler instance.
func NewHandler(pool *pgxpool.Pool, logger *slog.Logger) *Handler {
	return &Handler{
		pool:    pool,
		queries: db.New(pool),
		logger:  logger,
	}
}

// SetAuth configures the token signer and user credential store.
func (h *Handler) SetAuth(signer *auth.TokenSigner, store auth.UserStore) {
	h.tokenSigner = signer
	h.userStore = store
}

// RegisterRoutes registers Query API endpoints onto the provided ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Authentication & User Management
	mux.HandleFunc("POST /v1/auth/login", h.handleLogin)
	mux.HandleFunc("GET /v1/users", h.handleListUsers)
	mux.HandleFunc("POST /v1/users", h.handleCreateUser)
	mux.HandleFunc("GET /v1/users/{id}", h.handleGetUser)
	mux.HandleFunc("PATCH /v1/users/{id}", h.handleUpdateUser)
	mux.HandleFunc("DELETE /v1/users/{id}", h.handleDeleteUser)

	// Virtual Machines & Search
	mux.HandleFunc("GET /v1/vms", h.handleListVMs)
	mux.HandleFunc("GET /v1/vms/{id}", h.handleGetVM)
	mux.HandleFunc("GET /v1/search/vms", h.handleQuickSearchVMs)
	mux.HandleFunc("POST /v1/vms/search", h.handleStructuredSearchVMs)

	// Facets & Metadata
	mux.HandleFunc("GET /v1/vms/facets", h.handleGetVMFacets)
	mux.HandleFunc("GET /v1/meta/vm-fields", h.handleGetVMFields)
	mux.HandleFunc("GET /v1/tags", h.handleListTags)
	mux.HandleFunc("GET /v1/custom-attributes", h.handleListCustomAttributes)
	mux.HandleFunc("GET /v1/me", h.handleGetMe)

	// Topology: Datacenters
	mux.HandleFunc("GET /v1/datacenters", h.handleListTopology("datacenters"))
	mux.HandleFunc("GET /v1/datacenters/{id}", h.handleGetTopologyByID("datacenters"))
	mux.HandleFunc("GET /v1/datacenters/{id}/vms", h.handleListTopologyVMs("datacenter"))

	// Topology: Clusters
	mux.HandleFunc("GET /v1/clusters", h.handleListTopology("clusters"))
	mux.HandleFunc("GET /v1/clusters/{id}", h.handleGetTopologyByID("clusters"))
	mux.HandleFunc("GET /v1/clusters/{id}/vms", h.handleListTopologyVMs("cluster"))

	// Topology: Hosts
	mux.HandleFunc("GET /v1/hosts", h.handleListTopology("hosts"))
	mux.HandleFunc("GET /v1/hosts/{id}", h.handleGetTopologyByID("hosts"))
	mux.HandleFunc("GET /v1/hosts/{id}/vms", h.handleListTopologyVMs("host"))

	// Topology: Datastores
	mux.HandleFunc("GET /v1/datastores", h.handleListTopology("datastores"))
	mux.HandleFunc("GET /v1/datastores/{id}", h.handleGetTopologyByID("datastores"))
	mux.HandleFunc("GET /v1/datastores/{id}/vms", h.handleListTopologyVMs("datastore"))

	// Topology: Networks
	mux.HandleFunc("GET /v1/networks", h.handleListTopology("networks"))
	mux.HandleFunc("GET /v1/networks/{id}", h.handleGetTopologyByID("networks"))
	mux.HandleFunc("GET /v1/networks/{id}/vms", h.handleListTopologyVMs("network"))

	// Topology: Folders
	mux.HandleFunc("GET /v1/folders", h.handleListFolders)
	mux.HandleFunc("GET /v1/folders/{id}", h.handleGetFolderByID)
	mux.HandleFunc("GET /v1/folders/{id}/vms", h.handleListTopologyVMs("folder"))

	// vCenter Management & Auditing
	mux.HandleFunc("POST /v1/vcenters/test-connection", h.handleTestVCenterConnection)
	mux.HandleFunc("GET /v1/vcenters", h.handleListVCenters)
	mux.HandleFunc("POST /v1/vcenters", h.handleCreateVCenter)
	mux.HandleFunc("GET /v1/vcenters/{id}", h.handleGetVCenter)
	mux.HandleFunc("PATCH /v1/vcenters/{id}", h.handleUpdateVCenter)
	mux.HandleFunc("DELETE /v1/vcenters/{id}", h.handleDeleteVCenter)
	mux.HandleFunc("POST /v1/vcenters/{id}/resync", h.handleResyncVCenter)
	mux.HandleFunc("GET /v1/audit-events", h.handleListAuditEvents)
}

// ErrorResponse represents a standardized API error shape.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (h *Handler) writeError(w http.ResponseWriter, status int, errTitle, errMsg string) {
	h.writeProblem(w, status, errTitle, errMsg, "bad_request")
}

func (h *Handler) writeProblem(w http.ResponseWriter, status int, title, detail, code string) {
	h.writeProblemWithDetails(w, status, title, detail, code, nil)
}

func (h *Handler) writeProblemWithDetails(w http.ResponseWriter, status int, title, detail, code string, details map[string]any) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	redactedDetail := logging.RedactString(detail)
	payload := map[string]any{
		"type":       "about:blank",
		"title":      title,
		"status":     status,
		"detail":     redactedDetail,
		"code":       status,
		"error_code": code,
		"error":      title,
		"message":    redactedDetail,
	}
	for k, v := range details {
		if s, ok := v.(string); ok {
			payload[k] = logging.RedactString(s)
		} else {
			payload[k] = v
		}
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// VMListItem represents a VM entry in list pagination responses.
type VMListItem struct {
	ID              string `json:"id"`
	VCenterID       string `json:"vcenter_id"`
	MoRef           string `json:"moref"`
	InstanceUUID    string `json:"instance_uuid"`
	BiosUUID        string `json:"bios_uuid,omitempty"`
	Name            string `json:"name"`
	PowerState      string `json:"power_state"`
	ConnectionState string `json:"connection_state"`
	CPUCores        int32  `json:"cpu_cores"`
	MemoryMB        int64  `json:"memory_mb"`
	FolderID        string `json:"folder_id,omitempty"`
	FolderPath      string `json:"folder_path,omitempty"`
	HostID          string `json:"host_id,omitempty"`
	ClusterID       string `json:"cluster_id,omitempty"`
	DatacenterID    string `json:"datacenter_id,omitempty"`
	GuestOS         string `json:"guest_os,omitempty"`
	GuestHostName   string `json:"guest_host_name,omitempty"`
	GuestIPAddress  string `json:"guest_ip_address,omitempty"`
	Annotation      string `json:"annotation,omitempty"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	FirstSeenAt     string `json:"first_seen_at,omitempty"`
}

// PaginationMeta contains keyset pagination metadata.
type PaginationMeta struct {
	Limit      int    `json:"limit"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// ListVMsResponse is the envelope for GET /v1/vms.
type ListVMsResponse struct {
	Items      []VMListItem   `json:"items"`
	Pagination PaginationMeta `json:"pagination"`
}

func (h *Handler) handleListVMs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	// 1. Parse limit
	limit := DefaultPageLimit
	if limitStr := q.Get("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed <= 0 {
			h.writeError(w, http.StatusBadRequest, "Bad Request", "limit must be a positive integer")
			return
		}
		if parsed > MaxPageLimit {
			limit = MaxPageLimit
		} else {
			limit = parsed
		}
	}

	// 2. Parse optional vcenter_id
	var vcenterUUID *uuid.UUID
	vcenterIDStr := q.Get("vcenter_id")
	if vcenterIDStr != "" {
		parsedUUID, err := uuid.Parse(vcenterIDStr)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "Bad Request", "invalid vcenter_id format, expected UUID")
			return
		}
		vcenterUUID = &parsedUUID
	}

	nameFilter := strings.TrimSpace(q.Get("name"))

	// Enforce tenant boundary scoping
	var scope *search.ScopeConstraint
	callerID, hasAuth := auth.FromContext(r.Context())
	if hasAuth && callerID != nil {
		if vcenterUUID != nil && !callerID.CanAccessVCenter(*vcenterUUID) {
			h.writeProblem(w, http.StatusForbidden, "Forbidden", "access to requested vCenter denied", "forbidden")
			return
		}
		scope = callerID.ToScopeConstraint()
	}

	// 3. Parse opaque cursor
	rawCursor := q.Get("cursor")
	cursor, err := DecodeCursor(rawCursor)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Bad Request", "malformed pagination cursor")
		return
	}

	var cursorValues *search.CursorValues
	if cursor != nil {
		cursorValues = &search.CursorValues{
			SortField: "name",
			LastValue: cursor.Name,
			LastID:    cursor.ID,
		}
	}

	var filterNodes []*search.FilterNode
	if vcenterUUID != nil {
		filterNodes = append(filterNodes, &search.FilterNode{
			Op:    search.OpEQ,
			Field: "vcenter_id",
			Value: vcenterUUID.String(),
		})
	}
	if nameFilter != "" {
		filterNodes = append(filterNodes, &search.FilterNode{
			Op:    search.OpContains,
			Field: "name",
			Value: nameFilter,
		})
	}

	var rootFilter *search.FilterNode
	if len(filterNodes) == 1 {
		rootFilter = filterNodes[0]
	} else if len(filterNodes) > 1 {
		rootFilter = &search.FilterNode{
			Op:       search.OpAnd,
			Operands: filterNodes,
		}
	}

	opts := search.CompileOptions{
		Scope:  scope,
		Sort:   search.SortSpec{Field: "name", Direction: "asc"},
		Cursor: cursorValues,
		Limit:  limit + 1,
	}

	compiled, err := search.CompileVMQuery(rootFilter, opts)
	if err != nil {
		h.logger.Error("failed to compile list vms query", "error", err)
		h.writeError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	rows, err := h.pool.Query(ctx, compiled.SQL, compiled.Args...)
	if err != nil {
		h.logger.Error("failed to list virtual machines", slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "Internal Server Error", "database query failed")
		return
	}
	defer rows.Close()

	items := make([]VMListItem, 0, limit)
	for rows.Next() {
		item, err := scanVMListItemDirect(rows)
		if err != nil {
			h.logger.Error("failed to scan VM row", slog.String("error", err.Error()))
			h.writeError(w, http.StatusInternalServerError, "Internal Server Error", "failed to scan VM results")
			return
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		h.logger.Error("row iteration error", slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "Internal Server Error", "row iteration error")
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = EncodeCursor(last.Name, last.ID)
	}

	resp := ListVMsResponse{
		Items: items,
		Pagination: PaginationMeta{
			Limit:      limit,
			NextCursor: nextCursor,
			HasMore:    hasMore,
		},
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// VMDetailResponse is the detailed envelope for GET /v1/vms/{id}.
type VMDetailResponse struct {
	VMListItem
	Datastores        []DatastoreAttachment `json:"datastores"`
	NetworkInterfaces []NICAttachment       `json:"network_interfaces"`
	Tags              []TagItem             `json:"tags"`
	CustomAttributes  []CustomAttributeItem `json:"custom_attributes"`
}

type TagItem struct {
	TagID        string `json:"tag_id"`
	CategoryName string `json:"category_name"`
	TagName      string `json:"tag_name"`
}

type CustomAttributeItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type DatastoreAttachment struct {
	ID            string `json:"id"`
	Moref         string `json:"moref"`
	Name          string `json:"name"`
	Type          string `json:"type"`
	CapacityBytes int64  `json:"capacity_bytes"`
	FreeBytes     int64  `json:"free_bytes"`
}

type NICAttachment struct {
	ID          string `json:"id"`
	DeviceKey   int32  `json:"device_key"`
	MacAddress  string `json:"mac_address"`
	IPAddress   string `json:"ip_address,omitempty"`
	NetworkID   string `json:"network_id,omitempty"`
	PortgroupID string `json:"portgroup_id,omitempty"`
}

func (h *Handler) handleGetVM(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.PathValue("id")

	parsedUUID, err := uuid.Parse(idStr)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Bad Request", "invalid VM ID, expected UUID")
		return
	}

	pgID := pgtype.UUID{Bytes: parsedUUID, Valid: true}

	vm, err := h.queries.GetVMByID(ctx, pgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.writeError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("virtual machine with ID %s not found", idStr))
			return
		}
		h.logger.Error("failed to query VM by ID", slog.String("id", idStr), slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "Internal Server Error", "database query failed")
		return
	}

	// Enforce tenant scope checking to eliminate BOLA/IDOR vulnerability
	if id, ok := auth.FromContext(r.Context()); ok && id != nil {
		vcUUID, err := uuid.FromBytes(vm.VcenterID.Bytes[:])
		if err == nil && !id.CanAccessVCenter(vcUUID) {
			h.writeProblem(w, http.StatusForbidden, "Forbidden", "access to requested virtual machine denied", "forbidden")
			return
		}
		if len(id.AllowedFolderPaths) > 0 {
			if !vm.FolderPath.Valid || vm.FolderPath.String == "" {
				h.writeProblem(w, http.StatusForbidden, "Forbidden", "access to requested virtual machine folder denied", "forbidden")
				return
			}
			allowed := false
			for _, p := range id.AllowedFolderPaths {
				if vm.FolderPath.String == p || strings.HasPrefix(vm.FolderPath.String, p+".") {
					allowed = true
					break
				}
			}
			if !allowed {
				h.writeProblem(w, http.StatusForbidden, "Forbidden", "access to requested virtual machine folder denied", "forbidden")
				return
			}
		}
	}

	// Retrieve attached datastores
	datastores, err := h.queries.ListDatastoresForVM(ctx, pgID)
	if err != nil {
		h.logger.Error("failed to list datastores for VM", slog.String("id", idStr), slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "Internal Server Error", "failed to retrieve attached datastores")
		return
	}

	dsAttachments := make([]DatastoreAttachment, len(datastores))
	for i, ds := range datastores {
		dsAttachments[i] = DatastoreAttachment{
			ID:            formatUUID(ds.ID),
			Moref:         ds.Moref,
			Name:          ds.Name,
			Type:          ds.Type,
			CapacityBytes: ds.CapacityBytes,
			FreeBytes:     ds.FreeBytes,
		}
	}

	// Retrieve attached network interfaces
	nics, err := h.queries.ListNetworkInterfacesForVM(ctx, pgID)
	if err != nil {
		h.logger.Error("failed to list NICs for VM", slog.String("id", idStr), slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "Internal Server Error", "failed to retrieve attached NICs")
		return
	}

	nicAttachments := make([]NICAttachment, len(nics))
	for i, nic := range nics {
		ip := ""
		if nic.IpAddress != nil {
			ip = nic.IpAddress.String()
		}
		nicAttachments[i] = NICAttachment{
			ID:          formatUUID(nic.ID),
			DeviceKey:   nic.DeviceKey,
			MacAddress:  nic.MacAddress,
			IPAddress:   ip,
			NetworkID:   formatUUID(nic.NetworkID),
			PortgroupID: formatUUID(nic.PortgroupID),
		}
	}

	// Retrieve attached tags
	tags, err := h.queries.ListTagsForVM(ctx, pgID)
	if err != nil {
		h.logger.Error("failed to list tags for VM", slog.String("id", idStr), slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "Internal Server Error", "failed to retrieve attached tags")
		return
	}

	tagItems := make([]TagItem, len(tags))
	for i, t := range tags {
		tagItems[i] = TagItem{
			TagID:        t.TagID,
			CategoryName: t.CategoryName,
			TagName:      t.TagName,
		}
	}

	// Retrieve attached custom attributes
	attrs, err := h.queries.ListCustomAttributesForVM(ctx, pgID)
	if err != nil {
		h.logger.Error("failed to list custom attributes for VM", slog.String("id", idStr), slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "Internal Server Error", "failed to retrieve custom attributes")
		return
	}

	attrItems := make([]CustomAttributeItem, len(attrs))
	for i, a := range attrs {
		attrItems[i] = CustomAttributeItem{
			Name:  a.Name,
			Value: a.Value,
		}
	}

	detail := VMDetailResponse{
		VMListItem:        mapDBVMToListItem(vm),
		Datastores:        dsAttachments,
		NetworkInterfaces: nicAttachments,
		Tags:              tagItems,
		CustomAttributes:  attrItems,
	}

	h.writeJSON(w, http.StatusOK, detail)
}

func mapDBVMToListItem(vm db.VirtualMachine) VMListItem {
	ip := ""
	if vm.GuestIpAddress != nil {
		ip = vm.GuestIpAddress.String()
	}

	return VMListItem{
		ID:              formatUUID(vm.ID),
		VCenterID:       formatUUID(vm.VcenterID),
		MoRef:           vm.Moref,
		InstanceUUID:    vm.InstanceUuid,
		BiosUUID:        vm.BiosUuid.String,
		Name:            vm.Name,
		PowerState:      vm.PowerState,
		ConnectionState: vm.ConnectionState,
		CPUCores:        vm.CpuCores,
		MemoryMB:        vm.MemoryMb,
		FolderID:        formatUUID(vm.FolderID),
		FolderPath:      vm.FolderPath.String,
		HostID:          formatUUID(vm.HostID),
		ClusterID:       formatUUID(vm.ClusterID),
		DatacenterID:    formatUUID(vm.DatacenterID),
		GuestOS:         vm.GuestOs,
		GuestHostName:   vm.GuestHostName,
		GuestIPAddress:  ip,
		Annotation:      vm.Annotation,
		CreatedAt:       formatTime(vm.CreatedAt.Time),
		UpdatedAt:       formatTime(vm.UpdatedAt.Time),
	}
}

func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	id, err := uuid.FromBytes(u.Bytes[:])
	if err != nil {
		return ""
	}
	return id.String()
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
