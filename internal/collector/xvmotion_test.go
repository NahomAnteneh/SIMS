package collector

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/generated/db"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestXvMotion_Order1_LeaveThenEnter(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	queries := db.New(pool)
	handler := NewXvMotionHandler(queries)

	vcAID, targetAID := createTestVCenterAndTarget(t, pool)
	defer cleanupTestTarget(pool, targetAID)
	vcBID, targetBID := createTestVCenterAndTarget(t, pool)
	defer cleanupTestTarget(pool, targetBID)

	instUUID := uuid.NewString()
	morefA := "vm-source-100"
	morefB := "vm-dest-200"

	// 1. Initial creation of VM on vCenter A
	pgVcA := pgtype.UUID{Bytes: vcAID, Valid: true}
	initialVM, err := queries.UpsertVM(ctx, db.UpsertVMParams{
		VcenterID:    pgVcA,
		Moref:        morefA,
		InstanceUuid: instUUID,
		Name:         "migrating-vm",
		PowerState:   "poweredOn",
	})
	if err != nil {
		t.Fatalf("failed to insert initial VM: %v", err)
	}

	// 2. Order 1: Leave(vCenter A) first
	err = handler.HandleLeave(ctx, vcAID, morefA, instUUID)
	if err != nil {
		t.Fatalf("HandleLeave failed: %v", err)
	}

	// Verify VM is tombstoned
	tombstoned, err := queries.GetVMByInstanceUUIDAnyStatus(ctx, instUUID)
	if err != nil {
		t.Fatalf("GetVMByInstanceUUIDAnyStatus failed: %v", err)
	}
	if !tombstoned.DeletedAt.Valid {
		t.Fatalf("expected VM to be deleted/tombstoned after leave")
	}

	// 3. Enter(vCenter B)
	migratedVM, isMigration, err := handler.HandleEnter(ctx, vcBID, morefB, instUUID)
	if err != nil {
		t.Fatalf("HandleEnter failed: %v", err)
	}
	if !isMigration {
		t.Fatalf("expected isMigration to be true")
	}

	// Verify logical record continuity: synthetic ID preserved, first_seen_at preserved, updated vcenter_id and moref
	if migratedVM.ID != initialVM.ID {
		t.Fatalf("expected synthetic ID %v to be preserved, got %v", initialVM.ID, migratedVM.ID)
	}
	if migratedVM.VcenterID.Bytes != vcBID {
		t.Fatalf("expected vcenter_id to be %v, got %v", vcBID, migratedVM.VcenterID.Bytes)
	}
	if migratedVM.Moref != morefB {
		t.Fatalf("expected moref to be %s, got %s", morefB, migratedVM.Moref)
	}
	if migratedVM.DeletedAt.Valid {
		t.Fatalf("expected deleted_at to be NULL on migrated active VM")
	}
}

func TestXvMotion_Order2_EnterThenLeave(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	queries := db.New(pool)
	handler := NewXvMotionHandler(queries)

	vcAID, targetAID := createTestVCenterAndTarget(t, pool)
	defer cleanupTestTarget(pool, targetAID)
	vcBID, targetBID := createTestVCenterAndTarget(t, pool)
	defer cleanupTestTarget(pool, targetBID)

	instUUID := uuid.NewString()
	morefA := "vm-source-300"
	morefB := "vm-dest-400"

	// 1. Initial creation of VM on vCenter A
	pgVcA := pgtype.UUID{Bytes: vcAID, Valid: true}
	initialVM, err := queries.UpsertVM(ctx, db.UpsertVMParams{
		VcenterID:    pgVcA,
		Moref:        morefA,
		InstanceUuid: instUUID,
		Name:         "migrating-vm-fast",
		PowerState:   "poweredOn",
	})
	if err != nil {
		t.Fatalf("failed to insert initial VM: %v", err)
	}

	// 2. Order 2: Enter(vCenter B) happens BEFORE Leave(vCenter A)
	migratedVM, isMigration, err := handler.HandleEnter(ctx, vcBID, morefB, instUUID)
	if err != nil {
		t.Fatalf("HandleEnter failed: %v", err)
	}
	if !isMigration {
		t.Fatalf("expected isMigration to be true")
	}

	if migratedVM.ID != initialVM.ID {
		t.Fatalf("expected synthetic ID to be preserved")
	}
	if migratedVM.VcenterID.Bytes != vcBID {
		t.Fatalf("expected vcenter_id to be destination %v", vcBID)
	}
	if migratedVM.Moref != morefB {
		t.Fatalf("expected moref to be %s", morefB)
	}

	// 3. Subsequently, stale Leave(vCenter A) arrives
	err = handler.HandleLeave(ctx, vcAID, morefA, instUUID)
	if err != nil {
		t.Fatalf("HandleLeave should succeed (ignore stale leave): %v", err)
	}

	// Verify VM on vCenter B is STILL active and NOT deleted
	currentVM, err := queries.GetVMByInstanceUUIDAnyStatus(ctx, instUUID)
	if err != nil {
		t.Fatalf("GetVMByInstanceUUIDAnyStatus failed: %v", err)
	}
	if currentVM.DeletedAt.Valid {
		t.Fatalf("stale leave erroneously deleted the migrated VM on vCenter B!")
	}
	if currentVM.VcenterID.Bytes != vcBID {
		t.Fatalf("VM vcenter_id corrupted by stale leave")
	}
}

func TestXvMotion_NewVMNotMigration(t *testing.T) {
	pool := getTestPool(t)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	queries := db.New(pool)
	handler := NewXvMotionHandler(queries)

	vcID, targetID := createTestVCenterAndTarget(t, pool)
	defer cleanupTestTarget(pool, targetID)

	instUUID := uuid.NewString()
	_, isMigration, err := handler.HandleEnter(ctx, vcID, "vm-brand-new", instUUID)
	if err != nil {
		t.Fatalf("HandleEnter failed on brand new VM: %v", err)
	}
	if isMigration {
		t.Fatalf("expected isMigration to be false for brand new VM")
	}
}
