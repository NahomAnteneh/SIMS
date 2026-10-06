package collector

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/generated/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	// ErrEmptyInstanceUUID is returned when an instance_uuid is not provided.
	ErrEmptyInstanceUUID = errors.New("instance_uuid cannot be empty")
)

// XvMotionHandler coordinates cross-vCenter VM migration (xvMotion) and preserves
// logical VM identity, synthetic UUIDs, first_seen_at provenance, and event history
// regardless of event arrival order (Leave -> Enter or Enter -> Leave).
type XvMotionHandler struct {
	queries *db.Queries
}

// NewXvMotionHandler constructs a new XvMotionHandler.
func NewXvMotionHandler(queries *db.Queries) *XvMotionHandler {
	return &XvMotionHandler{
		queries: queries,
	}
}

// HandleEnter processes a VM discovery or registration on destination vCenter.
// If a VM with matching instance_uuid exists in any status (active or tombstoned),
// it is migrated to destVCenterID with destMoref, and deleted_at is cleared.
// Returns the VM and true if this was an xvMotion migration; false if it's a new or existing local VM.
func (h *XvMotionHandler) HandleEnter(ctx context.Context, destVCenterID uuid.UUID, destMoref string, instanceUUID string) (db.VirtualMachine, bool, error) {
	if instanceUUID == "" {
		return db.VirtualMachine{}, false, ErrEmptyInstanceUUID
	}

	existing, err := h.queries.GetVMByInstanceUUIDAnyStatus(ctx, instanceUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// VM does not exist yet in DB; caller should proceed with regular creation/upsert.
			return db.VirtualMachine{}, false, nil
		}
		return db.VirtualMachine{}, false, fmt.Errorf("failed to lookup vm by instance_uuid: %w", err)
	}

	existingVCID, err := uuid.FromBytes(existing.VcenterID.Bytes[:])
	if err != nil {
		return db.VirtualMachine{}, false, fmt.Errorf("invalid existing vcenter_id: %w", err)
	}

	// If already on the same vcenter and moref and not deleted, no migration needed.
	if existingVCID == destVCenterID && existing.Moref == destMoref && !existing.DeletedAt.Valid {
		return existing, false, nil
	}

	// VM migrated from another vCenter or had its moref changed or was tombstoned.
	var destVCPgUUID pgtype.UUID
	copy(destVCPgUUID.Bytes[:], destVCenterID[:])
	destVCPgUUID.Valid = true

	migrated, err := h.queries.MigrateVMToVCenter(ctx, db.MigrateVMToVCenterParams{
		ID:        existing.ID,
		VcenterID: destVCPgUUID,
		Moref:     destMoref,
	})
	if err != nil {
		return db.VirtualMachine{}, false, fmt.Errorf("failed to migrate vm to destination vcenter: %w", err)
	}

	return migrated, true, nil
}

// HandleLeave processes a VM leave, unregister, or destroy event on srcVCenterID.
// If the VM has already migrated to another vCenter (Enter -> Leave order) and is active there,
// this stale leave event is ignored without soft-deleting the migrated active VM.
// Otherwise, the VM on srcVCenterID is soft-deleted.
func (h *XvMotionHandler) HandleLeave(ctx context.Context, srcVCenterID uuid.UUID, moref string, instanceUUID string) error {
	var srcVCPgUUID pgtype.UUID
	copy(srcVCPgUUID.Bytes[:], srcVCenterID[:])
	srcVCPgUUID.Valid = true

	if instanceUUID != "" {
		existing, err := h.queries.GetVMByInstanceUUIDAnyStatus(ctx, instanceUUID)
		if err == nil {
			existingVCID, parseErr := uuid.FromBytes(existing.VcenterID.Bytes[:])
			if parseErr == nil && existingVCID != srcVCenterID && !existing.DeletedAt.Valid {
				// VM has already migrated to another vCenter and is active there!
				// Safely ignore stale leave event.
				return nil
			}
		}
	}

	// Normal leave: soft-delete VM on srcVCenterID with matching moref
	return h.queries.SoftDeleteVM(ctx, db.SoftDeleteVMParams{
		VcenterID: srcVCPgUUID,
		Moref:     moref,
	})
}
