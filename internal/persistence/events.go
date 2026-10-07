package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/jackc/pgx/v5"
)

// GetEventWatermark returns the latest recorded event watermark for a vCenter.
// Returns (0, nil, nil) if no watermark is recorded.
func (p *BulkPersister) GetEventWatermark(ctx context.Context, vcenterID string) (int64, *time.Time, error) {
	vcUUID, err := uuid.Parse(vcenterID)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid vcenter uuid %q: %w", vcenterID, err)
	}

	var lastKey *int64
	var lastTime *time.Time

	err = p.pool.QueryRow(ctx, `
		SELECT last_event_key, last_event_time
		FROM collector_state
		WHERE vcenter_id = $1
	`, vcUUID).Scan(&lastKey, &lastTime)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, nil
		}
		return 0, nil, fmt.Errorf("failed to query event watermark: %w", err)
	}

	var k int64
	if lastKey != nil {
		k = *lastKey
	}
	return k, lastTime, nil
}

// SaveEventWatermark updates or creates the event watermark for a vCenter in collector_state.
func (p *BulkPersister) SaveEventWatermark(ctx context.Context, vcenterID string, lastEventKey int64, lastEventTime time.Time) error {
	vcUUID, err := uuid.Parse(vcenterID)
	if err != nil {
		return fmt.Errorf("invalid vcenter uuid %q: %w", vcenterID, err)
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO collector_state (vcenter_id, last_event_key, last_event_time, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (vcenter_id) WHERE vcenter_id IS NOT NULL DO UPDATE
		SET last_event_key = EXCLUDED.last_event_key,
		    last_event_time = EXCLUDED.last_event_time,
		    updated_at = NOW()
	`, vcUUID, lastEventKey, lastEventTime)
	if err != nil {
		return fmt.Errorf("failed to save event watermark: %w", err)
	}
	return nil
}

// PersistEvent persists an inventory event idempotently and enforces field ownership:
// 1. Inserts event with ON CONFLICT (vcenter_id, event_key) DO NOTHING
// 2. If creation event: sets VM created_at = event.CreatedTime and created_by = event.UserName
// 3. If VmRemovedEvent: soft-deletes the VM (deleted_at = NOW())
func (p *BulkPersister) PersistEvent(ctx context.Context, vcenterID string, event collector.EventRecord) error {
	vcUUID, err := uuid.Parse(vcenterID)
	if err != nil {
		return fmt.Errorf("invalid vcenter uuid %q: %w", vcenterID, err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for event persistence: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Insert into inventory_events idempotently
	payload := event.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}

	entityType := event.EntityType
	if entityType == "" {
		entityType = "VirtualMachine"
	}

	var vmInstanceUUID *string
	if event.VMInstanceUUID != "" {
		vmInstanceUUID = &event.VMInstanceUUID
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO inventory_events (
			vcenter_id, event_key, chain_id, event_type, vm_moref, vm_instance_uuid,
			user_name, entity_type, entity_moref, entity_name, payload, message,
			created_time, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12,
			$13, NOW()
		)
		ON CONFLICT (vcenter_id, event_key) DO NOTHING
	`,
		vcUUID, event.EventKey, event.ChainID, event.EventType, event.EntityMoref, vmInstanceUUID,
		event.UserName, entityType, event.EntityMoref, event.EntityName, payload, event.Message,
		event.CreatedTime,
	)
	if err != nil {
		return fmt.Errorf("failed to insert inventory event: %w", err)
	}

	// 2. Field Ownership: Event layer owns created_at and created_by (S2.10)
	switch event.EventType {
	case "VmCreatedEvent", "VmClonedEvent", "VmDeployedEvent", "VmRegisteredEvent", "VmDiscoveredEvent":
		if event.EntityMoref != "" {
			_, err = tx.Exec(ctx, `
				UPDATE virtual_machines
				SET created_at = $3,
				    created_by = $4,
				    updated_at = NOW()
				WHERE vcenter_id = $1 AND moref = $2 AND deleted_at IS NULL
			`, vcUUID, event.EntityMoref, event.CreatedTime, event.UserName)
			if err != nil {
				return fmt.Errorf("failed to update VM provenance from creation event: %w", err)
			}
		}

	case "VmRemovedEvent":
		// Event-driven soft delete
		if event.EntityMoref != "" {
			_, err = tx.Exec(ctx, `
				UPDATE virtual_machines
				SET deleted_at = NOW(),
				    updated_at = NOW()
				WHERE vcenter_id = $1 AND moref = $2 AND deleted_at IS NULL
			`, vcUUID, event.EntityMoref)
			if err != nil {
				return fmt.Errorf("failed to soft-delete VM on VmRemovedEvent: %w", err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit event persistence transaction: %w", err)
	}

	return nil
}
