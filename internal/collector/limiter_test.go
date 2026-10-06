package collector

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestGlobalLimiter_Concurrency(t *testing.T) {
	gl := NewGlobalLimiter(2)

	ctx := context.Background()
	// Acquire 2 permits
	if err := gl.AcquireHydration(ctx); err != nil {
		t.Fatalf("failed to acquire permit 1: %v", err)
	}
	if err := gl.AcquireHydration(ctx); err != nil {
		t.Fatalf("failed to acquire permit 2: %v", err)
	}

	// 3rd attempt with short timeout must fail
	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	err := gl.AcquireHydration(timeoutCtx)
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded on 3rd permit, got %v", err)
	}

	// Release 1 and acquire again
	gl.ReleaseHydration()
	if err := gl.AcquireHydration(ctx); err != nil {
		t.Fatalf("expected permit after release, got %v", err)
	}
}

func TestVCenterLimiter_SOAPAndREST(t *testing.T) {
	vcID := uuid.New()
	vl := NewVCenterLimiter(vcID, 1, 1)

	ctx := context.Background()
	if err := vl.AcquireSOAP(ctx); err != nil {
		t.Fatalf("failed to acquire SOAP permit: %v", err)
	}
	if err := vl.AcquireREST(ctx); err != nil {
		t.Fatalf("failed to acquire REST permit: %v", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	if err := vl.AcquireSOAP(timeoutCtx); err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded on 2nd SOAP permit")
	}

	vl.ReleaseSOAP()
	vl.ReleaseREST()
}

func TestConcurrencyRegistry(t *testing.T) {
	cr := NewConcurrencyRegistry(3, 5, 2)
	vcID1 := uuid.New()
	vcID2 := uuid.New()

	vl1 := cr.ForVCenter(vcID1)
	vl2 := cr.ForVCenter(vcID2)

	if vl1 == nil || vl2 == nil {
		t.Fatalf("expected valid limiters from registry")
	}
	if vl1 == vl2 {
		t.Fatalf("limiters for different vCenters must be distinct")
	}
	if cr.ForVCenter(vcID1) != vl1 {
		t.Fatalf("registry should return cached limiter instance")
	}
}
