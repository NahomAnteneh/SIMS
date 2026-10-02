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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	DefaultPageLimit = 20
	MaxPageLimit     = 100
)

// Handler serves Query API endpoints.
type Handler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	logger  *slog.Logger
}

// NewHandler creates a new API Handler instance.
func NewHandler(pool *pgxpool.Pool, logger *slog.Logger) *Handler {
	return &Handler{
		pool:    pool,
		queries: db.New(pool),
		logger:  logger,
	}
}

// RegisterRoutes registers the /v1/vms endpoints onto the provided ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/vms", h.handleListVMs)
	mux.HandleFunc("GET /v1/vms/{id}", h.handleGetVM)
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
	h.writeJSON(w, status, ErrorResponse{
		Error:   errTitle,
		Message: errMsg,
		Code:    status,
	})
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
	var vcenterID pgtype.UUID
	vcenterIDStr := q.Get("vcenter_id")
	if vcenterIDStr != "" {
		parsedUUID, err := uuid.Parse(vcenterIDStr)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "Bad Request", "invalid vcenter_id format, expected UUID")
			return
		}
		vcenterID = pgtype.UUID{Bytes: parsedUUID, Valid: true}
	}

	nameFilter := strings.TrimSpace(q.Get("name"))

	// 3. Parse opaque cursor
	rawCursor := q.Get("cursor")
	cursor, err := DecodeCursor(rawCursor)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "Bad Request", "malformed pagination cursor")
		return
	}

	// Fetch limit + 1 to detect whether a next page exists
	fetchLimit := int32(limit + 1)

	var vms []db.VirtualMachine
	if vcenterID.Valid {
		if cursor == nil {
			vms, err = h.queries.ListVMsByVCenterFirstPage(ctx, db.ListVMsByVCenterFirstPageParams{
				VcenterID:  vcenterID,
				NameFilter: nameFilter,
				PageLimit:  fetchLimit,
			})
		} else {
			cursorUUID, parseErr := uuid.Parse(cursor.ID)
			if parseErr != nil {
				h.writeError(w, http.StatusBadRequest, "Bad Request", "malformed cursor ID")
				return
			}
			vms, err = h.queries.ListVMsByVCenterNextPage(ctx, db.ListVMsByVCenterNextPageParams{
				VcenterID:  vcenterID,
				NameFilter: nameFilter,
				CursorName: cursor.Name,
				CursorID:   pgtype.UUID{Bytes: cursorUUID, Valid: true},
				PageLimit:  fetchLimit,
			})
		}
	} else {
		if cursor == nil {
			vms, err = h.queries.ListVMsFirstPage(ctx, db.ListVMsFirstPageParams{
				NameFilter: nameFilter,
				PageLimit:  fetchLimit,
			})
		} else {
			cursorUUID, parseErr := uuid.Parse(cursor.ID)
			if parseErr != nil {
				h.writeError(w, http.StatusBadRequest, "Bad Request", "malformed cursor ID")
				return
			}
			vms, err = h.queries.ListVMsNextPage(ctx, db.ListVMsNextPageParams{
				NameFilter: nameFilter,
				CursorName: cursor.Name,
				CursorID:   pgtype.UUID{Bytes: cursorUUID, Valid: true},
				PageLimit:  fetchLimit,
			})
		}
	}

	if err != nil {
		h.logger.Error("failed to list virtual machines", slog.String("error", err.Error()))
		h.writeError(w, http.StatusInternalServerError, "Internal Server Error", "database query failed")
		return
	}

	hasMore := len(vms) > limit
	if hasMore {
		vms = vms[:limit]
	}

	items := make([]VMListItem, len(vms))
	for i, vm := range vms {
		items[i] = mapDBVMToListItem(vm)
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

	detail := VMDetailResponse{
		VMListItem:        mapDBVMToListItem(vm),
		Datastores:        dsAttachments,
		NetworkInterfaces: nicAttachments,
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
