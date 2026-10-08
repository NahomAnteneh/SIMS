package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/search"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SearchVMsRequest represents structured search payload.
type SearchVMsRequest struct {
	Filter *search.FilterNode `json:"filter"`
	Sort   *search.SortSpec   `json:"sort,omitempty"`
	Limit  int                `json:"limit,omitempty"`
	Cursor string             `json:"cursor,omitempty"`
}

// SearchVMsResponse represents the search output envelope.
type SearchVMsResponse struct {
	Items      []VMListItem `json:"items"`
	NextCursor string       `json:"nextCursor,omitempty"`
	HasMore    bool         `json:"hasMore"`
}

func (h *Handler) handleQuickSearchVMs(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "query parameter 'q' is required", "invalid_parameter")
		return
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

	// Classify search query
	classification := search.Classify(q)
	var filter *search.FilterNode

	switch classification.Class {
	case search.ClassUUID:
		filter = &search.FilterNode{
			Op: search.OpOr,
			Operands: []*search.FilterNode{
				{Op: search.OpEQ, Field: "instance_uuid", Value: classification.Normalized},
				{Op: search.OpEQ, Field: "bios_uuid", Value: classification.Normalized},
				{Op: search.OpEQ, Field: "id", Value: classification.Normalized},
			},
		}

	case search.ClassMoRef:
		filter = &search.FilterNode{
			Op: search.OpOr,
			Operands: []*search.FilterNode{
				{Op: search.OpEQ, Field: "moref", Value: classification.Normalized},
				{Op: search.OpContains, Field: "name", Value: classification.Normalized},
			},
		}

	case search.ClassIPv4, search.ClassIPv6:
		filter = &search.FilterNode{
			Op: search.OpOr,
			Operands: []*search.FilterNode{
				{Op: search.OpCIDRContains, Field: "guest_ip_address", Value: classification.Normalized},
				{Op: search.OpContains, Field: "name", Value: classification.Normalized},
			},
		}

	case search.ClassMAC:
		// Lookup VM IDs for MAC address
		vmIDs, err := h.lookupVMIDsByMAC(r.Context(), classification.Normalized)
		if err != nil {
			h.logger.Error("failed to lookup VM IDs by MAC", "error", err)
			h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to search by MAC", "internal_error")
			return
		}
		if len(vmIDs) == 0 {
			// Zero matches
			h.writeJSON(w, http.StatusOK, SearchVMsResponse{Items: []VMListItem{}, HasMore: false})
			return
		}
		var anyIDs []any
		for _, id := range vmIDs {
			anyIDs = append(anyIDs, id)
		}
		filter = &search.FilterNode{
			Op: search.OpIN, Field: "id", Values: anyIDs,
		}

	case search.ClassExactName:
		filter = &search.FilterNode{
			Op: search.OpEQ, Field: "name", Value: classification.Normalized,
		}

	case search.ClassHostname:
		filter = &search.FilterNode{
			Op: search.OpOr,
			Operands: []*search.FilterNode{
				{Op: search.OpEQ, Field: "guest_host_name", Value: classification.Normalized},
				{Op: search.OpContains, Field: "name", Value: classification.Normalized},
			},
		}

	default: // search.ClassSubstring
		operands := []*search.FilterNode{
			{Op: search.OpContains, Field: "name", Value: classification.Normalized},
			{Op: search.OpContains, Field: "guest_host_name", Value: classification.Normalized},
			{Op: search.OpContains, Field: "moref", Value: classification.Normalized},
		}
		if hasDigitOrDot(classification.Normalized) {
			operands = append(operands, &search.FilterNode{
				Op: search.OpContains, Field: "guest_ip_address", Value: classification.Normalized,
			})
		}
		filter = &search.FilterNode{
			Op:       search.OpOr,
			Operands: operands,
		}
	}

	h.executeSearch(w, r, filter, search.SortSpec{Field: "name", Direction: "asc"}, limit, rawCursor, "quick:"+classification.Normalized)
}

func (h *Handler) handleStructuredSearchVMs(w http.ResponseWriter, r *http.Request) {
	var req SearchVMsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "malformed json request body", "invalid_request")
		return
	}

	if req.Filter == nil {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "filter object is required", "missing_filter")
		return
	}

	if err := search.Validate(req.Filter); err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error(), "invalid_filter")
		return
	}

	limit := req.Limit
	if limit == 0 {
		limit = DefaultPageLimit
	} else if limit < 0 {
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", "limit must be a positive integer", "invalid_parameter")
		return
	} else if limit > MaxPageLimit {
		limit = MaxPageLimit
	}

	sortSpec := search.SortSpec{Field: "name", Direction: "asc"}
	if req.Sort != nil && req.Sort.Field != "" {
		sortSpec.Field = req.Sort.Field
		if strings.ToLower(req.Sort.Direction) == "desc" {
			sortSpec.Direction = "desc"
		}
	}

	h.executeSearch(w, r, req.Filter, sortSpec, limit, req.Cursor, "structured")
}

func (h *Handler) executeSearch(w http.ResponseWriter, r *http.Request, filter *search.FilterNode, sortSpec search.SortSpec, limit int, rawCursor string, queryTag string) {
	queryHash := ComputeQueryHash(queryTag, sortSpec.Field, sortSpec.Direction)
	var cursorValues *search.CursorValues

	if rawCursor != "" {
		c, err := defaultSigner.Decode(rawCursor, queryHash)
		if err != nil {
			if errors.Is(err, ErrCursorTampered) || errors.Is(err, ErrCursorExpired) || errors.Is(err, ErrQueryMismatch) || errors.Is(err, ErrInvalidCursor) {
				h.writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error(), "invalid_cursor")
				return
			}
			h.writeProblem(w, http.StatusBadRequest, "Bad Request", "invalid cursor", "invalid_cursor")
			return
		}
		if c != nil {
			cursorValues = &search.CursorValues{
				SortField: c.SortField,
				LastValue: c.LastValue,
				LastID:    c.LastID,
			}
		}
	}

	// Extract tenant authorization scope from context
	var scope *search.ScopeConstraint
	if id, ok := auth.FromContext(r.Context()); ok && id != nil {
		scope = id.ToScopeConstraint()
	}

	// Compile query
	opts := search.CompileOptions{
		Scope:  scope,
		Sort:   sortSpec,
		Cursor: cursorValues,
		Limit:  limit + 1, // Request 1 extra to determine hasMore
	}

	_, compileSpan := telemetry.StartSpan(r.Context(), "search.ast_compile",
		trace.WithAttributes(
			attribute.String("search.query_tag", queryTag),
			attribute.String("search.sort_field", sortSpec.Field),
			attribute.String("search.sort_dir", sortSpec.Direction),
		),
	)
	compiled, err := search.CompileVMQuery(filter, opts)
	if err != nil {
		telemetry.RecordSafeError(compileSpan, err)
		compileSpan.End()
		h.logger.Error("failed to compile search query", "error", err)
		h.writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error(), "invalid_filter")
		return
	}
	compileSpan.End()

	queryCtx, querySpan := telemetry.StartSpan(r.Context(), "search.db_query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "SELECT"),
			attribute.Int("search.limit", limit),
		),
	)
	defer querySpan.End()

	rows, err := h.pool.Query(queryCtx, compiled.SQL, compiled.Args...)
	if err != nil {
		telemetry.RecordSafeError(querySpan, err)
		h.logger.Error("search query execution failed", "error", err, "sql", compiled.SQL)
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "search query execution failed", "database_error")
		return
	}
	defer rows.Close()

	items := make([]VMListItem, 0, limit)
	for rows.Next() {
		item, err := scanVMListItemDirect(rows)
		if err != nil {
			h.logger.Error("failed to scan VM row", "error", err)
			h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "failed to scan VM results", "scan_error")
			return
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		h.logger.Error("row iteration error", "error", err)
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "row iteration error", "database_error")
		return
	}

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	var nextCursor string
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		var lastVal string
		switch sortSpec.Field {
		case "updated_at":
			lastVal = last.UpdatedAt
		case "first_seen_at":
			lastVal = last.FirstSeenAt
		default:
			lastVal = last.Name
		}
		c := &Cursor{
			Version:   CurrentCursorVersion,
			QueryHash: queryHash,
			SortField: sortSpec.Field,
			SortDir:   sortSpec.Direction,
			LastValue: lastVal,
			LastID:    last.ID,
		}
		nextCursor = defaultSigner.Encode(c)
	}

	h.writeJSON(w, http.StatusOK, SearchVMsResponse{
		Items:      items,
		NextCursor: nextCursor,
		HasMore:    hasMore,
	})
}

func (h *Handler) lookupVMIDsByMAC(ctx context.Context, mac string) ([]uuid.UUID, error) {
	pgUUIDs, err := h.queries.GetVMIDsByMACAddress(ctx, mac)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(pgUUIDs))
	for i, u := range pgUUIDs {
		ids[i] = u.Bytes
	}
	return ids, nil
}

// scanVMListItemDirect scans VM columns directly without reflection.
func scanVMListItemDirect(rows pgx.Rows) (VMListItem, error) {
	var (
		id, vcenterID                                                     uuid.UUID
		moref, instanceUUID, name, powerState, connState                  string
		biosUUID, folderPath, guestOS, guestHostName, guestIP, annotation *string
		folderID, hostID, clusterID, datacenterID                         *uuid.UUID
		cpuCores                                                          int32
		memoryMB                                                          int64
		createdAt, updatedAt, firstSeenAt                                 time.Time
		committedStorageBytes, provisionedStorageBytes                    int64
	)

	err := rows.Scan(
		&id, &vcenterID, &moref, &instanceUUID, &biosUUID,
		&name, &powerState, &connState, &cpuCores, &memoryMB,
		&folderID, &folderPath, &hostID, &clusterID, &datacenterID,
		&guestOS, &guestHostName, &guestIP, &annotation,
		&createdAt, &updatedAt, &firstSeenAt,
		&committedStorageBytes, &provisionedStorageBytes,
	)
	if err != nil {
		return VMListItem{}, err
	}

	item := VMListItem{
		ID:                      id.String(),
		VCenterID:               vcenterID.String(),
		MoRef:                   moref,
		InstanceUUID:            instanceUUID,
		Name:                    name,
		PowerState:              powerState,
		ConnectionState:         connState,
		CPUCores:                cpuCores,
		MemoryMB:                memoryMB,
		CommittedStorageBytes:   committedStorageBytes,
		ProvisionedStorageBytes: provisionedStorageBytes,
		CreatedAt:               createdAt.Format(time.RFC3339),
		UpdatedAt:               updatedAt.Format(time.RFC3339),
		FirstSeenAt:             firstSeenAt.Format(time.RFC3339),
	}

	if biosUUID != nil {
		item.BiosUUID = *biosUUID
	}
	if folderID != nil {
		item.FolderID = folderID.String()
	}
	if folderPath != nil {
		item.FolderPath = *folderPath
	}
	if hostID != nil {
		item.HostID = hostID.String()
	}
	if clusterID != nil {
		item.ClusterID = clusterID.String()
	}
	if datacenterID != nil {
		item.DatacenterID = datacenterID.String()
	}
	if guestOS != nil {
		item.GuestOS = *guestOS
	}
	if guestHostName != nil {
		item.GuestHostName = *guestHostName
	}
	if guestIP != nil {
		item.GuestIPAddress = *guestIP
	}
	if annotation != nil {
		item.Annotation = *annotation
	}

	return item, nil
}

func hasDigitOrDot(s string) bool {
	for _, c := range s {
		if (c >= '0' && c <= '9') || c == '.' || c == ':' {
			return true
		}
	}
	return false
}
