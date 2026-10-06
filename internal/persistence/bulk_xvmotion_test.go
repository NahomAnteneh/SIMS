package persistence

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/domain"
)

func TestBulkPersister_XvMotion_BothOrders(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	queries := db.New(pool)
	persister := NewBulkPersister(pool, WriterConfig{FlushInterval: 10 * time.Millisecond, FlushSize: 100}, nil)

	// Create 2 test vcenters
	vcA, err := queries.CreateVCenter(ctx, db.CreateVCenterParams{
		Name:         fmt.Sprintf("vc-a-%s", uuid.NewString()[:8]),
		ApiUrl:       "https://vca.local/sdk",
		Version:      "8.0.3",
		Build:        "24000000",
		Status:       "AVAILABLE",
		Priority:     100,
		Enabled:      true,
		Capabilities: []byte("{}"),
		TlsSettings:  []byte("{}"),
	})
	if err != nil {
		t.Fatalf("failed to create vcA: %v", err)
	}

	vcB, err := queries.CreateVCenter(ctx, db.CreateVCenterParams{
		Name:         fmt.Sprintf("vc-b-%s", uuid.NewString()[:8]),
		ApiUrl:       "https://vcb.local/sdk",
		Version:      "8.0.3",
		Build:        "24000000",
		Status:       "AVAILABLE",
		Priority:     100,
		Enabled:      true,
		Capabilities: []byte("{}"),
		TlsSettings:  []byte("{}"),
	})
	if err != nil {
		t.Fatalf("failed to create vcB: %v", err)
	}

	defer func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM collector_targets WHERE vcenter_id IN ($1, $2)", vcA.ID, vcB.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM virtual_machines WHERE vcenter_id IN ($1, $2)", vcA.ID, vcB.ID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM vcenters WHERE id IN ($1, $2)", vcA.ID, vcB.ID)
	}()

	vcAIDStr := uuid.UUID(vcA.ID.Bytes).String()
	vcBIDStr := uuid.UUID(vcB.ID.Bytes).String()

	t.Run("Order1_LeaveThenEnter", func(t *testing.T) {
		instUUID := uuid.NewString()
		morefA := "vm-bulk-a-1"
		morefB := "vm-bulk-b-1"

		// Initial creation on vCenter A
		_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{
			{
				VCenterID:       vcAIDStr,
				MoRef:           morefA,
				InstanceUUID:    instUUID,
				Name:            "vm-bulk-order1",
				PowerState:      "poweredOn",
				ConnectionState: "connected",
			},
		})
		if err != nil {
			t.Fatalf("initial insert failed: %v", err)
		}

		origVM, err := queries.GetVMByInstanceUUIDAnyStatus(ctx, instUUID)
		if err != nil {
			t.Fatalf("lookup failed: %v", err)
		}

		// Leave on vCenter A
		now := time.Now()
		_, err = persister.PersistVMBatch(ctx, []domain.VMRecord{
			{
				VCenterID:       vcAIDStr,
				MoRef:           morefA,
				InstanceUUID:    instUUID,
				Name:            "vm-bulk-order1",
				PowerState:      "poweredOff",
				ConnectionState: "connected",
				DeletedAt:       &now,
			},
		})
		if err != nil {
			t.Fatalf("soft delete failed: %v", err)
		}

		// Enter on vCenter B
		_, err = persister.PersistVMBatch(ctx, []domain.VMRecord{
			{
				VCenterID:       vcBIDStr,
				MoRef:           morefB,
				InstanceUUID:    instUUID,
				Name:            "vm-bulk-order1-migrated",
				PowerState:      "poweredOn",
				ConnectionState: "connected",
			},
		})
		if err != nil {
			t.Fatalf("enter on vCenter B failed: %v", err)
		}

		migratedVM, err := queries.GetVMByInstanceUUIDAnyStatus(ctx, instUUID)
		if err != nil {
			t.Fatalf("lookup migrated failed: %v", err)
		}

		if migratedVM.ID != origVM.ID {
			t.Fatalf("expected synthetic ID %v to be preserved, got %v", origVM.ID, migratedVM.ID)
		}
		if migratedVM.VcenterID.Bytes != vcB.ID.Bytes {
			t.Fatalf("expected vcenter_id to be %v, got %v", vcB.ID.Bytes, migratedVM.VcenterID.Bytes)
		}
		if migratedVM.Moref != morefB {
			t.Fatalf("expected moref to be %s, got %s", morefB, migratedVM.Moref)
		}
		if migratedVM.DeletedAt.Valid {
			t.Fatalf("expected deleted_at to be NULL")
		}
	})

	t.Run("Order2_EnterThenLeave", func(t *testing.T) {
		instUUID := uuid.NewString()
		morefA := "vm-bulk-a-2"
		morefB := "vm-bulk-b-2"

		// Initial creation on vCenter A
		_, err := persister.PersistVMBatch(ctx, []domain.VMRecord{
			{
				VCenterID:       vcAIDStr,
				MoRef:           morefA,
				InstanceUUID:    instUUID,
				Name:            "vm-bulk-order2",
				PowerState:      "poweredOn",
				ConnectionState: "connected",
			},
		})
		if err != nil {
			t.Fatalf("initial insert failed: %v", err)
		}

		origVM, err := queries.GetVMByInstanceUUIDAnyStatus(ctx, instUUID)
		if err != nil {
			t.Fatalf("lookup failed: %v", err)
		}

		// Enter on vCenter B while vCenter A is still active
		_, err = persister.PersistVMBatch(ctx, []domain.VMRecord{
			{
				VCenterID:       vcBIDStr,
				MoRef:           morefB,
				InstanceUUID:    instUUID,
				Name:            "vm-bulk-order2-migrated",
				PowerState:      "poweredOn",
				ConnectionState: "connected",
			},
		})
		if err != nil {
			t.Fatalf("enter on vCenter B before leave failed: %v", err)
		}

		migratedVM, err := queries.GetVMByInstanceUUIDAnyStatus(ctx, instUUID)
		if err != nil {
			t.Fatalf("lookup migrated failed: %v", err)
		}
		if migratedVM.ID != origVM.ID {
			t.Fatalf("expected synthetic ID to be preserved")
		}
		if migratedVM.VcenterID.Bytes != vcB.ID.Bytes {
			t.Fatalf("expected vcenter_id to be destination vcB")
		}

		// Subsequent stale Leave on vCenter A
		now := time.Now()
		_, err = persister.PersistVMBatch(ctx, []domain.VMRecord{
			{
				VCenterID:       vcAIDStr,
				MoRef:           morefA,
				InstanceUUID:    instUUID,
				Name:            "vm-bulk-order2",
				PowerState:      "poweredOff",
				ConnectionState: "connected",
				DeletedAt:       &now,
			},
		})
		if err != nil {
			t.Fatalf("stale leave failed: %v", err)
		}

		// Verify VM on vCenter B is STILL active
		finalVM, err := queries.GetVMByInstanceUUIDAnyStatus(ctx, instUUID)
		if err != nil {
			t.Fatalf("final lookup failed: %v", err)
		}
		if finalVM.DeletedAt.Valid {
			t.Fatalf("stale leave should not have deleted migrated VM on vCenter B")
		}
		if finalVM.VcenterID.Bytes != vcB.ID.Bytes {
			t.Fatalf("final vcenter_id corrupted")
		}
	})
}
