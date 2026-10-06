package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/auth"
	"github.com/infrastructure/sims/internal/vsphere"
)

// Idempotency cache for create requests
type idempotencyRecord struct {
	status int
	body   []byte
	header http.Header
	exp    time.Time
}

var (
	idempotencyMu    sync.Mutex
	idempotencyCache = make(map[string]idempotencyRecord)
)

// Rate limiter for administrative requests
type callerRateLimiter struct {
	mu     sync.Mutex
	tokens map[string][]time.Time
}

var vcenterRateLimiter = &callerRateLimiter{
	tokens: make(map[string][]time.Time),
}

func (rl *callerRateLimiter) allow(key string, limit int, window time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	var valid []time.Time
	for _, t := range rl.tokens[key] {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}

	if len(valid) >= limit {
		rl.tokens[key] = valid
		return false
	}

	rl.tokens[key] = append(valid, now)
	return true
}

func getCallerKey(r *http.Request) string {
	if id, ok := auth.FromContext(r.Context()); ok && id.Subject != "" {
		return id.Subject
	}
	return r.RemoteAddr
}

func requireInventoryAdmin(r *http.Request) bool {
	id, ok := auth.FromContext(r.Context())
	if !ok {
		return true // dev mode / auth not enabled
	}
	return id.Role == auth.RoleInventoryAdmin
}

// Request and Response schemas

type TestConnectionRequest struct {
	Host     string                      `json:"host"`
	Port     int                         `json:"port"`
	Username string                      `json:"username"`
	Password string                      `json:"password"`
	TLS      vsphere.TLSValidationConfig `json:"tls"`
}

type VCenterVersion struct {
	Product string `json:"product"`
	API     string `json:"api"`
	Build   string `json:"build"`
}

type TestConnectionResponse struct {
	Status       string            `json:"status"`
	Version      VCenterVersion    `json:"version"`
	Capabilities map[string]string `json:"capabilities"`
}

type CreateVCenterRequest struct {
	Name     string                      `json:"name"`
	Host     string                      `json:"host"`
	Port     int                         `json:"port"`
	Username string                      `json:"username"`
	Password string                      `json:"password"`
	TLS      vsphere.TLSValidationConfig `json:"tls"`
	Enabled  *bool                       `json:"enabled,omitempty"`
	Priority *int                        `json:"priority,omitempty"`
}

type UpdateVCenterRequest struct {
	Name     *string                      `json:"name,omitempty"`
	Username *string                      `json:"username,omitempty"`
	Password *string                      `json:"password,omitempty"`
	TLS      *vsphere.TLSValidationConfig `json:"tls,omitempty"`
	Enabled  *bool                        `json:"enabled,omitempty"`
	Priority *int                         `json:"priority,omitempty"`
}

type VCenterResponse struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Host         string            `json:"host"`
	Port         int               `json:"port"`
	Version      VCenterVersion    `json:"version"`
	Capabilities map[string]string `json:"capabilities"`
	Enabled      bool              `json:"enabled"`
	Status       string            `json:"status"`
	Priority     int               `json:"priority"`
	CreatedAt    string            `json:"created_at"`
	UpdatedAt    string            `json:"updated_at"`
}

type VCenterListResponse struct {
	Items []VCenterResponse `json:"items"`
}

type AuditEventItem struct {
	ID        string         `json:"id"`
	Actor     string         `json:"actor"`
	Action    string         `json:"action"`
	TargetID  string         `json:"target_id"`
	Outcome   string         `json:"outcome"`
	RequestID string         `json:"request_id"`
	Details   map[string]any `json:"details"`
	CreatedAt string         `json:"created_at"`
}

type AuditEventListResponse struct {
	Items []AuditEventItem `json:"items"`
}

func (h *Handler) recordAudit(ctx context.Context, actor, action, targetID, outcome, requestID string, details map[string]any) {
	if h.pool == nil {
		return
	}
	cleanDetails := make(map[string]any)
	for k, v := range details {
		kLower := strings.ToLower(k)
		if strings.Contains(kLower, "pass") || strings.Contains(kLower, "token") || strings.Contains(kLower, "secret") {
			continue
		}
		cleanDetails[k] = v
	}
	detailsJSON, _ := json.Marshal(cleanDetails)

	q := db.New(h.pool)
	_, err := q.InsertAuditEvent(ctx, db.InsertAuditEventParams{
		Actor:     actor,
		Action:    action,
		TargetID:  targetID,
		Outcome:   outcome,
		RequestID: requestID,
		Details:   detailsJSON,
	})
	if err != nil {
		h.logger.Warn("failed to record audit event", slog.String("action", action), slog.String("error", err.Error()))
	}
}

// POST /v1/vcenters/test-connection
func (h *Handler) handleTestVCenterConnection(w http.ResponseWriter, r *http.Request) {
	callerKey := getCallerKey(r)
	if !vcenterRateLimiter.allow(callerKey, 30, time.Minute) {
		h.writeProblem(w, http.StatusTooManyRequests, "Too Many Requests", "Rate limit exceeded for test-connection", "rate_limited")
		return
	}

	var req TestConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Invalid Request", "Malformed JSON body", "invalid_json")
		return
	}

	if strings.TrimSpace(req.Host) == "" || strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.Password) == "" {
		h.writeProblem(w, http.StatusBadRequest, "Invalid Request", "Host, username, and password are required", "missing_fields")
		return
	}

	actor := "anonymous"
	if id, ok := auth.FromContext(r.Context()); ok && id.Subject != "" {
		actor = id.Subject
	}

	valResult, err := vsphere.ValidateConnection(r.Context(), vsphere.ValidationParams{
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		Password: req.Password,
		TLS:      req.TLS,
	})

	if err != nil {
		h.recordAudit(r.Context(), actor, "vcenter.test_connection", req.Host, "failure", "", map[string]any{
			"host": req.Host,
			"port": req.Port,
		})

		var valErr *vsphere.ValidationError
		if errors.As(err, &valErr) {
			if valErr.Code == "tls_untrusted" && valErr.Fingerprint != "" {
				h.writeProblemWithDetails(w, http.StatusBadRequest, "Connection Test Failed", valErr.Message, valErr.Code, map[string]any{
					"fingerprint": valErr.Fingerprint,
				})
				return
			}
			h.writeProblem(w, http.StatusBadRequest, "Connection Test Failed", valErr.Message, valErr.Code)
			return
		}

		h.writeProblem(w, http.StatusBadRequest, "Connection Test Failed", err.Error(), "unreachable")
		return
	}

	h.recordAudit(r.Context(), actor, "vcenter.test_connection", req.Host, "success", "", map[string]any{
		"host":    req.Host,
		"port":    req.Port,
		"version": valResult.ProductVersion,
	})

	resp := TestConnectionResponse{
		Status: "connected",
		Version: VCenterVersion{
			Product: valResult.ProductName,
			API:     valResult.APIVersion,
			Build:   valResult.Build,
		},
		Capabilities: map[string]string{
			"tags": valResult.TagsCapability,
		},
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// POST /v1/vcenters
func (h *Handler) handleCreateVCenter(w http.ResponseWriter, r *http.Request) {
	if !requireInventoryAdmin(r) {
		h.writeProblem(w, http.StatusForbidden, "Forbidden", "Registration requires inventory-admin role", "forbidden")
		return
	}

	callerKey := getCallerKey(r)
	if !vcenterRateLimiter.allow(callerKey, 30, time.Minute) {
		h.writeProblem(w, http.StatusTooManyRequests, "Too Many Requests", "Rate limit exceeded for vCenter registration", "rate_limited")
		return
	}

	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey != "" {
		idempotencyMu.Lock()
		if rec, ok := idempotencyCache[idempotencyKey]; ok && time.Now().Before(rec.exp) {
			idempotencyMu.Unlock()
			for k, v := range rec.header {
				for _, val := range v {
					w.Header().Add(k, val)
				}
			}
			w.WriteHeader(rec.status)
			_, _ = w.Write(rec.body)
			return
		}
		idempotencyMu.Unlock()
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Invalid Request", "Failed to read request body", "invalid_body")
		return
	}

	var req CreateVCenterRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Invalid Request", "Malformed JSON body", "invalid_json")
		return
	}

	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Host) == "" || strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.Password) == "" {
		h.writeProblem(w, http.StatusBadRequest, "Invalid Request", "Name, host, username, and password are required", "missing_fields")
		return
	}

	port := req.Port
	if port == 0 {
		port = 443
	}

	apiURL := fmt.Sprintf("https://%s:%d/sdk", req.Host, port)

	// Check if already registered
	existing, err := h.queries.GetActiveVCenterByURL(r.Context(), apiURL)
	if err == nil && existing.ID.Valid {
		h.writeProblem(w, http.StatusConflict, "Conflict", fmt.Sprintf("vCenter host %q already registered", req.Host), "conflict")
		return
	}

	actor := "anonymous"
	if id, ok := auth.FromContext(r.Context()); ok && id.Subject != "" {
		actor = id.Subject
	}

	// Validate connection
	valResult, err := vsphere.ValidateConnection(r.Context(), vsphere.ValidationParams{
		Host:     req.Host,
		Port:     port,
		Username: req.Username,
		Password: req.Password,
		TLS:      req.TLS,
	})

	if err != nil {
		h.recordAudit(r.Context(), actor, "vcenter.create", req.Host, "failure", "", map[string]any{
			"host": req.Host,
			"port": port,
			"name": req.Name,
		})

		var valErr *vsphere.ValidationError
		if errors.As(err, &valErr) {
			if valErr.Code == "tls_untrusted" && valErr.Fingerprint != "" {
				h.writeProblemWithDetails(w, http.StatusBadRequest, "Registration Failed", valErr.Message, valErr.Code, map[string]any{
					"fingerprint": valErr.Fingerprint,
				})
				return
			}
			h.writeProblem(w, http.StatusBadRequest, "Registration Failed", valErr.Message, valErr.Code)
			return
		}

		h.writeProblem(w, http.StatusBadRequest, "Registration Failed", err.Error(), "unreachable")
		return
	}

	priority := int32(100)
	if req.Priority != nil {
		priority = int32(*req.Priority)
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	capabilitiesJSON, _ := json.Marshal(map[string]string{"tags": valResult.TagsCapability})
	tlsSettingsJSON, _ := json.Marshal(map[string]any{
		"verify":             req.TLS.Verify,
		"ca_pem":             req.TLS.CAPEM,
		"pinned_fingerprint": req.TLS.PinnedFingerprint,
	})

	credRef := fmt.Sprintf("ref://sims-vault/%s", uuid.NewString())

	var instUUID pgtype.Text
	if valResult.InstanceUUID != "" {
		instUUID = pgtype.Text{String: valResult.InstanceUUID, Valid: true}
	}

	newVC, err := h.queries.CreateVCenter(r.Context(), db.CreateVCenterParams{
		Name:           req.Name,
		ApiUrl:         apiURL,
		InstanceUuid:   instUUID,
		Version:        valResult.ProductVersion,
		Build:          valResult.Build,
		ProductVersion: valResult.ProductVersion,
		ApiVersion:     valResult.APIVersion,
		Capabilities:   capabilitiesJSON,
		TlsSettings:    tlsSettingsJSON,
		CredentialRef:  credRef,
		Status:         "PENDING_ASSIGNMENT",
		Priority:       priority,
		Enabled:        enabled,
	})
	if err != nil {
		h.logger.Error("failed to create vCenter in database", slog.String("error", err.Error()))
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Database insertion failed", "internal_error")
		return
	}

	// Insert collector target
	_, err = h.queries.InsertCollectorTarget(r.Context(), db.InsertCollectorTargetParams{
		VcenterID: newVC.ID,
		Enabled:   enabled,
	})
	if err != nil {
		h.logger.Warn("failed to insert collector target", slog.String("error", err.Error()))
	}

	vcIDStr := uuidFromPg(newVC.ID)
	h.recordAudit(r.Context(), actor, "vcenter.create", vcIDStr, "success", "", map[string]any{
		"name":    req.Name,
		"host":    req.Host,
		"port":    port,
		"version": valResult.ProductVersion,
	})

	resp := formatVCenterResponse(newVC, req.Host, port)

	respBytes, _ := json.Marshal(resp)
	if idempotencyKey != "" {
		idempotencyMu.Lock()
		idempotencyCache[idempotencyKey] = idempotencyRecord{
			status: http.StatusCreated,
			body:   respBytes,
			header: w.Header().Clone(),
			exp:    time.Now().Add(24 * time.Hour),
		}
		idempotencyMu.Unlock()
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write(respBytes)
}

// GET /v1/vcenters
func (h *Handler) handleListVCenters(w http.ResponseWriter, r *http.Request) {
	id, ok := auth.FromContext(r.Context())

	vcs, err := h.queries.ListActiveVCenters(r.Context())
	if err != nil {
		h.logger.Error("failed to list vCenters", slog.String("error", err.Error()))
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Failed to retrieve vCenters", "internal_error")
		return
	}

	items := make([]VCenterResponse, 0, len(vcs))
	for _, vc := range vcs {
		parsedID, err := uuid.FromBytes(vc.ID.Bytes[:])
		if err != nil {
			continue
		}
		if ok && !id.CanAccessVCenter(parsedID) {
			continue
		}
		host, port := parseHostPortFromURL(vc.ApiUrl)
		items = append(items, formatVCenterResponse(vc, host, port))
	}

	h.writeJSON(w, http.StatusOK, VCenterListResponse{Items: items})
}

// GET /v1/vcenters/{id}
func (h *Handler) handleGetVCenter(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	parsedUUID, err := uuid.Parse(idStr)
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Invalid UUID", "Malformed vCenter ID", "invalid_uuid")
		return
	}

	id, ok := auth.FromContext(r.Context())
	if ok && !id.CanAccessVCenter(parsedUUID) {
		h.writeProblem(w, http.StatusForbidden, "Forbidden", "Access to requested vCenter not permitted", "forbidden")
		return
	}

	pgUUID := pgtype.UUID{Bytes: parsedUUID, Valid: true}

	vc, err := h.queries.GetActiveVCenterByID(r.Context(), pgUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.writeProblem(w, http.StatusNotFound, "Not Found", "vCenter not found", "not_found")
			return
		}
		h.logger.Error("failed to get vCenter", slog.String("error", err.Error()))
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Failed to get vCenter", "internal_error")
		return
	}

	host, port := parseHostPortFromURL(vc.ApiUrl)
	h.writeJSON(w, http.StatusOK, formatVCenterResponse(vc, host, port))
}

// PATCH /v1/vcenters/{id}
func (h *Handler) handleUpdateVCenter(w http.ResponseWriter, r *http.Request) {
	if !requireInventoryAdmin(r) {
		h.writeProblem(w, http.StatusForbidden, "Forbidden", "Update requires inventory-admin role", "forbidden")
		return
	}

	idStr := r.PathValue("id")
	pgUUID, err := parsePgUUID(idStr)
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Invalid UUID", "Malformed vCenter ID", "invalid_uuid")
		return
	}

	existing, err := h.queries.GetActiveVCenterByID(r.Context(), pgUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.writeProblem(w, http.StatusNotFound, "Not Found", "vCenter not found", "not_found")
			return
		}
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Database error", "internal_error")
		return
	}

	var req UpdateVCenterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Invalid Request", "Malformed JSON body", "invalid_json")
		return
	}

	name := existing.Name
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		name = strings.TrimSpace(*req.Name)
	}

	enabled := existing.Enabled
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	priority := existing.Priority
	if req.Priority != nil {
		priority = int32(*req.Priority)
	}

	tlsSettings := existing.TlsSettings
	if req.TLS != nil {
		tlsSettings, _ = json.Marshal(req.TLS)
	}

	credRef := existing.CredentialRef
	if req.Username != nil && req.Password != nil && *req.Password != "" {
		credRef = fmt.Sprintf("ref://sims-vault/%s", uuid.NewString())
	}

	updatedVC, err := h.queries.UpdateVCenter(r.Context(), db.UpdateVCenterParams{
		ID:            pgUUID,
		Name:          name,
		Enabled:       enabled,
		Priority:      priority,
		CredentialRef: credRef,
		TlsSettings:   tlsSettings,
	})
	if err != nil {
		h.logger.Error("failed to update vCenter", slog.String("error", err.Error()))
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Failed to update vCenter", "internal_error")
		return
	}

	if req.Enabled != nil {
		if enabled {
			_, _ = h.queries.InsertCollectorTarget(r.Context(), db.InsertCollectorTargetParams{
				VcenterID: pgUUID,
				Enabled:   true,
			})
		} else {
			_ = h.queries.DisableCollectorTarget(r.Context(), pgUUID)
		}
	}

	actor := "anonymous"
	if id, ok := auth.FromContext(r.Context()); ok && id.Subject != "" {
		actor = id.Subject
	}
	h.recordAudit(r.Context(), actor, "vcenter.update", idStr, "success", "", map[string]any{
		"name":    name,
		"enabled": enabled,
	})

	host, port := parseHostPortFromURL(updatedVC.ApiUrl)
	h.writeJSON(w, http.StatusOK, formatVCenterResponse(updatedVC, host, port))
}

// DELETE /v1/vcenters/{id}
func (h *Handler) handleDeleteVCenter(w http.ResponseWriter, r *http.Request) {
	if !requireInventoryAdmin(r) {
		h.writeProblem(w, http.StatusForbidden, "Forbidden", "Decommission requires inventory-admin role", "forbidden")
		return
	}

	idStr := r.PathValue("id")
	pgUUID, err := parsePgUUID(idStr)
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Invalid UUID", "Malformed vCenter ID", "invalid_uuid")
		return
	}

	// 1. Disable collector target
	_ = h.queries.DisableCollectorTarget(r.Context(), pgUUID)

	// 2. Tombstone inventory
	_ = h.queries.TombstoneVCenterVMs(r.Context(), pgUUID)

	// 3. Soft-delete vCenter
	_ = h.queries.SoftDeleteVCenter(r.Context(), pgUUID)

	actor := "anonymous"
	if id, ok := auth.FromContext(r.Context()); ok && id.Subject != "" {
		actor = id.Subject
	}
	h.recordAudit(r.Context(), actor, "vcenter.delete", idStr, "success", "", map[string]any{
		"vcenter_id": idStr,
	})

	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/vcenters/{id}/resync
func (h *Handler) handleResyncVCenter(w http.ResponseWriter, r *http.Request) {
	if !requireInventoryAdmin(r) {
		h.writeProblem(w, http.StatusForbidden, "Forbidden", "Resync requires inventory-admin role", "forbidden")
		return
	}

	idStr := r.PathValue("id")
	pgUUID, err := parsePgUUID(idStr)
	if err != nil {
		h.writeProblem(w, http.StatusBadRequest, "Invalid UUID", "Malformed vCenter ID", "invalid_uuid")
		return
	}

	vc, err := h.queries.GetActiveVCenterByID(r.Context(), pgUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.writeProblem(w, http.StatusNotFound, "Not Found", "vCenter not found", "not_found")
			return
		}
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Database error", "internal_error")
		return
	}

	actor := "anonymous"
	if id, ok := auth.FromContext(r.Context()); ok && id.Subject != "" {
		actor = id.Subject
	}
	h.recordAudit(r.Context(), actor, "vcenter.resync", idStr, "accepted", "", map[string]any{
		"name": vc.Name,
	})

	w.WriteHeader(http.StatusAccepted)
}

func uuidFromPg(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	parsed, err := uuid.FromBytes(u.Bytes[:])
	if err != nil {
		return ""
	}
	return parsed.String()
}

// GET /v1/audit-events
func (h *Handler) handleListAuditEvents(w http.ResponseWriter, r *http.Request) {
	if !requireInventoryAdmin(r) {
		h.writeProblem(w, http.StatusForbidden, "Forbidden", "Audit logs require inventory-admin role", "forbidden")
		return
	}

	events, err := h.queries.ListAuditEvents(r.Context(), 100)
	if err != nil {
		h.logger.Error("failed to list audit events", slog.String("error", err.Error()))
		h.writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Failed to retrieve audit events", "internal_error")
		return
	}

	items := make([]AuditEventItem, 0, len(events))
	for _, ev := range events {
		var details map[string]any
		if len(ev.Details) > 0 {
			_ = json.Unmarshal(ev.Details, &details)
		}
		if details == nil {
			details = make(map[string]any)
		}

		items = append(items, AuditEventItem{
			ID:        uuidFromPg(ev.ID),
			Actor:     ev.Actor,
			Action:    ev.Action,
			TargetID:  ev.TargetID,
			Outcome:   ev.Outcome,
			RequestID: ev.RequestID,
			Details:   details,
			CreatedAt: ev.CreatedAt.Time.Format(time.RFC3339),
		})
	}

	h.writeJSON(w, http.StatusOK, AuditEventListResponse{Items: items})
}

// Helpers

func parseHostPortFromURL(rawURL string) (string, int) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL, 443
	}
	host := u.Hostname()
	portStr := u.Port()
	if portStr == "" {
		return host, 443
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return host, 443
	}
	return host, port
}

func parsePgUUID(s string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

func formatVCenterResponse(vc db.Vcenter, host string, port int) VCenterResponse {
	var caps map[string]string
	if len(vc.Capabilities) > 0 {
		_ = json.Unmarshal(vc.Capabilities, &caps)
	}
	if caps == nil {
		caps = map[string]string{"tags": "unavailable"}
	}

	createdAt := ""
	if vc.CreatedAt.Valid {
		createdAt = vc.CreatedAt.Time.Format(time.RFC3339)
	}
	updatedAt := ""
	if vc.UpdatedAt.Valid {
		updatedAt = vc.UpdatedAt.Time.Format(time.RFC3339)
	}

	return VCenterResponse{
		ID:   uuidFromPg(vc.ID),
		Name: vc.Name,
		Host: host,
		Port: port,
		Version: VCenterVersion{
			Product: vc.ProductVersion,
			API:     vc.ApiVersion,
			Build:   vc.Build,
		},
		Capabilities: caps,
		Enabled:      vc.Enabled,
		Status:       vc.Status,
		Priority:     int(vc.Priority),
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}
}
