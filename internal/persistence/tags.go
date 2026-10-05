package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/jackc/pgx/v5"
)

// ListActiveVMMorefs returns all active (non-soft-deleted) VM morefs for a vCenter.
func (p *BulkPersister) ListActiveVMMorefs(ctx context.Context, vcenterID string) ([]string, error) {
	vcUUID, err := uuid.Parse(vcenterID)
	if err != nil {
		return nil, fmt.Errorf("invalid vcenter uuid %q: %w", vcenterID, err)
	}

	rows, err := p.pool.Query(ctx, `
		SELECT moref FROM virtual_machines
		WHERE vcenter_id = $1 AND deleted_at IS NULL
		ORDER BY moref ASC
	`, vcUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to query active vm morefs: %w", err)
	}
	defer rows.Close()

	var morefs []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			return nil, err
		}
		morefs = append(morefs, m)
	}
	return morefs, rows.Err()
}

// GetVMTags returns a map of moref -> []domain.TagRecord for all active VMs in a vCenter.
func (p *BulkPersister) GetVMTags(ctx context.Context, vcenterID string) (map[string][]domain.TagRecord, error) {
	vcUUID, err := uuid.Parse(vcenterID)
	if err != nil {
		return nil, fmt.Errorf("invalid vcenter uuid %q: %w", vcenterID, err)
	}

	rows, err := p.pool.Query(ctx, `
		SELECT vm.moref, vt.tag_id, vt.category_name, vt.tag_name
		FROM vm_tags vt
		JOIN virtual_machines vm ON vt.vm_id = vm.id
		WHERE vm.vcenter_id = $1 AND vm.deleted_at IS NULL
		ORDER BY vm.moref ASC, vt.category_name ASC, vt.tag_name ASC
	`, vcUUID)
	if err != nil {
		return nil, fmt.Errorf("failed to query vm tags: %w", err)
	}
	defer rows.Close()

	res := make(map[string][]domain.TagRecord)
	for rows.Next() {
		var moref, tagID, catName, tagName string
		if err := rows.Scan(&moref, &tagID, &catName, &tagName); err != nil {
			return nil, err
		}
		res[moref] = append(res[moref], domain.TagRecord{
			TagID:        tagID,
			CategoryName: catName,
			TagName:      tagName,
		})
	}
	return res, rows.Err()
}

// UpdateVMTags updates the tags assigned to a specific VM using atomic replace-set semantics.
func (p *BulkPersister) UpdateVMTags(ctx context.Context, vcenterID string, moref string, tags []domain.TagRecord) error {
	vcUUID, err := uuid.Parse(vcenterID)
	if err != nil {
		return fmt.Errorf("invalid vcenter uuid %q: %w", vcenterID, err)
	}

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Locate VM ID
	var vmID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM virtual_machines
		WHERE vcenter_id = $1 AND moref = $2 AND deleted_at IS NULL
	`, vcUUID, moref).Scan(&vmID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // VM might have been deleted, skip
		}
		return fmt.Errorf("failed to locate VM for tag update: %w", err)
	}

	// 2. Delete existing tags for this VM
	if _, err := tx.Exec(ctx, "DELETE FROM vm_tags WHERE vm_id = $1", vmID); err != nil {
		return fmt.Errorf("failed to delete existing tags: %w", err)
	}

	// 3. Insert new tag records
	for _, t := range tags {
		_, err := tx.Exec(ctx, `
			INSERT INTO vm_tags (vm_id, tag_id, category_name, tag_name, created_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (vm_id, tag_id) DO NOTHING
		`, vmID, t.TagID, t.CategoryName, t.TagName)
		if err != nil {
			return fmt.Errorf("failed to insert tag %s: %w", t.TagID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit tag update transaction: %w", err)
	}

	return nil
}

// UpdateTagsState updates the tag synchronization state and last synced timestamp in collector_state.
func (p *BulkPersister) UpdateTagsState(ctx context.Context, vcenterID string, state string, lastSyncedAt *time.Time) error {
	vcUUID, err := uuid.Parse(vcenterID)
	if err != nil {
		return fmt.Errorf("invalid vcenter uuid %q: %w", vcenterID, err)
	}

	_, err = p.pool.Exec(ctx, `
		INSERT INTO collector_state (vcenter_id, tags_state, tags_last_synced_at, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (vcenter_id) WHERE vcenter_id IS NOT NULL DO UPDATE
		SET tags_state = EXCLUDED.tags_state,
		    tags_last_synced_at = EXCLUDED.tags_last_synced_at,
		    updated_at = NOW()
	`, vcUUID, state, lastSyncedAt)
	if err != nil {
		return fmt.Errorf("failed to update tags state: %w", err)
	}
	return nil
}

// GetTagsState retrieves the current tags state and last synced timestamp from collector_state.
func (p *BulkPersister) GetTagsState(ctx context.Context, vcenterID string) (string, *time.Time, error) {
	vcUUID, err := uuid.Parse(vcenterID)
	if err != nil {
		return "", nil, fmt.Errorf("invalid vcenter uuid %q: %w", vcenterID, err)
	}

	var state string
	var lastSynced *time.Time

	err = p.pool.QueryRow(ctx, `
		SELECT tags_state, tags_last_synced_at
		FROM collector_state
		WHERE vcenter_id = $1
	`, vcUUID).Scan(&state, &lastSynced)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "unavailable", nil, nil
		}
		return "", nil, fmt.Errorf("failed to query tags state: %w", err)
	}

	return state, lastSynced, nil
}
