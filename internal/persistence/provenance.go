package persistence

import (
	"context"
	"fmt"
	"time"
)

// UpdateVMProvenance records the immutable created_at and created_by fields from the Event layer (S2.10).
// Invariant: Once set, created_at and created_by cannot be overwritten by subsequent PropertyCollector updates.
func (p *BulkPersister) UpdateVMProvenance(ctx context.Context, vcenterID string, moref string, createdAt time.Time, createdBy string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE virtual_machines
		SET created_at = CASE 
				WHEN created_at = '0001-01-01 00:00:00+00'::timestamptz THEN $3
				WHEN created_by = '' THEN $3
				ELSE created_at 
			END,
		    created_by = CASE WHEN created_by = '' THEN $4 ELSE created_by END,
		    updated_at = NOW()
		WHERE vcenter_id = $1 AND moref = $2 AND deleted_at IS NULL
	`, vcenterID, moref, createdAt, createdBy)
	if err != nil {
		return fmt.Errorf("failed to update VM provenance: %w", err)
	}
	return nil
}
