package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ListActiveVMMorefs returns all active (non-soft-deleted) VM morefs for a vCenter.
func (p *BulkPersister) ListActiveVMMorefs(ctx context.Context, vcenterID string) ([]string, error) {
	vcUUID, err := uuid.Parse(vcenterID)
	if err != nil {
		return nil, fmt.Errorf("invalid vcenter uuid %q: %w", vcenterID, err)
	}

	return p.queries.ListActiveVMMorefs(ctx, pgtype.UUID{Bytes: vcUUID, Valid: true})
}

// GetVMTags returns a map of moref -> []domain.TagRecord for all active VMs in a vCenter.
func (p *BulkPersister) GetVMTags(ctx context.Context, vcenterID string) (map[string][]domain.TagRecord, error) {
	vcUUID, err := uuid.Parse(vcenterID)
	if err != nil {
		return nil, fmt.Errorf("invalid vcenter uuid %q: %w", vcenterID, err)
	}

	rows, err := p.queries.ListAllVMTags(ctx, pgtype.UUID{Bytes: vcUUID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("failed to query vm tags: %w", err)
	}

	res := make(map[string][]domain.TagRecord)
	for _, r := range rows {
		res[r.Moref] = append(res[r.Moref], domain.TagRecord{
			TagID:        r.TagID,
			CategoryName: r.CategoryName,
			TagName:      r.TagName,
		})
	}
	return res, nil
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

	var synced pgtype.Timestamptz
	if lastSyncedAt != nil {
		synced = pgtype.Timestamptz{Time: *lastSyncedAt, Valid: true}
	}

	err = p.queries.UpsertTagsState(ctx, db.UpsertTagsStateParams{
		VcenterID:        pgtype.UUID{Bytes: vcUUID, Valid: true},
		TagsState:        state,
		TagsLastSyncedAt: synced,
	})
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

	row, err := p.queries.GetTagsState(ctx, pgtype.UUID{Bytes: vcUUID, Valid: true})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "unavailable", nil, nil
		}
		return "", nil, fmt.Errorf("failed to query tags state: %w", err)
	}

	var lastSynced *time.Time
	if row.TagsLastSyncedAt.Valid {
		t := row.TagsLastSyncedAt.Time
		lastSynced = &t
	}

	return row.TagsState, lastSynced, nil
}
