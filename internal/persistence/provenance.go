package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	db "github.com/infrastructure/sims/generated/db"
	"github.com/jackc/pgx/v5/pgtype"
)

// UpdateVMProvenance records the immutable created_at and created_by fields from the Event layer (S2.10).
// Invariant: Once set, created_at and created_by cannot be overwritten by subsequent PropertyCollector updates.
func (p *BulkPersister) UpdateVMProvenance(ctx context.Context, vcenterID string, moref string, createdAt time.Time, createdBy string) error {
	vcUUID, err := uuid.Parse(vcenterID)
	if err != nil {
		return fmt.Errorf("invalid vcenter uuid %q: %w", vcenterID, err)
	}

	err = p.queries.UpdateVMProvenance(ctx, db.UpdateVMProvenanceParams{
		VcenterID: pgtype.UUID{Bytes: vcUUID, Valid: true},
		Moref:     moref,
		CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
		CreatedBy: createdBy,
	})
	if err != nil {
		return fmt.Errorf("failed to update VM provenance: %w", err)
	}
	return nil
}
