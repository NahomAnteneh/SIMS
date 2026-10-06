package collector

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// GlobalLimiter manages global concurrency across all targets (S4.5)
type GlobalLimiter struct {
	hydrationSem chan struct{}
}

func NewGlobalLimiter(maxConcurrentHydrations int) *GlobalLimiter {
	if maxConcurrentHydrations <= 0 {
		maxConcurrentHydrations = 2
	}
	return &GlobalLimiter{
		hydrationSem: make(chan struct{}, maxConcurrentHydrations),
	}
}

// AcquireHydration blocks until a hydration permit is acquired or ctx is canceled
func (gl *GlobalLimiter) AcquireHydration(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case gl.hydrationSem <- struct{}{}:
		return nil
	}
}

// ReleaseHydration releases an acquired hydration permit
func (gl *GlobalLimiter) ReleaseHydration() {
	select {
	case <-gl.hydrationSem:
	default:
	}
}

// VCenterLimiter manages per-vCenter API operation budgets (S4.5)
type VCenterLimiter struct {
	vcenterID uuid.UUID
	soapSem   chan struct{}
	restSem   chan struct{}
}

func NewVCenterLimiter(vcenterID uuid.UUID, maxSOAP, maxREST int) *VCenterLimiter {
	if maxSOAP <= 0 {
		maxSOAP = 10
	}
	if maxREST <= 0 {
		maxREST = 4
	}
	return &VCenterLimiter{
		vcenterID: vcenterID,
		soapSem:   make(chan struct{}, maxSOAP),
		restSem:   make(chan struct{}, maxREST),
	}
}

// AcquireSOAP acquires a concurrency permit for SOAP operations
func (vl *VCenterLimiter) AcquireSOAP(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case vl.soapSem <- struct{}{}:
		return nil
	}
}

// ReleaseSOAP releases a SOAP permit
func (vl *VCenterLimiter) ReleaseSOAP() {
	select {
	case <-vl.soapSem:
	default:
	}
}

// AcquireREST acquires a concurrency permit for REST/Tag operations (S4.5)
func (vl *VCenterLimiter) AcquireREST(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case vl.restSem <- struct{}{}:
		return nil
	}
}

// ReleaseREST releases a REST/Tag permit
func (vl *VCenterLimiter) ReleaseREST() {
	select {
	case <-vl.restSem:
	default:
	}
}

// ConcurrencyRegistry holds global and per-vCenter limiters
type ConcurrencyRegistry struct {
	global    *GlobalLimiter
	maxSOAP   int
	maxREST   int
	mu        sync.RWMutex
	perTarget map[uuid.UUID]*VCenterLimiter
}

func NewConcurrencyRegistry(maxHydrations, maxSOAP, maxREST int) *ConcurrencyRegistry {
	return &ConcurrencyRegistry{
		global:    NewGlobalLimiter(maxHydrations),
		maxSOAP:   maxSOAP,
		maxREST:   maxREST,
		perTarget: make(map[uuid.UUID]*VCenterLimiter),
	}
}

func (cr *ConcurrencyRegistry) Global() *GlobalLimiter {
	return cr.global
}

func (cr *ConcurrencyRegistry) ForVCenter(vcenterID uuid.UUID) *VCenterLimiter {
	cr.mu.RLock()
	vl, ok := cr.perTarget[vcenterID]
	cr.mu.RUnlock()
	if ok {
		return vl
	}

	cr.mu.Lock()
	defer cr.mu.Unlock()
	if vl, ok = cr.perTarget[vcenterID]; ok {
		return vl
	}

	vl = NewVCenterLimiter(vcenterID, cr.maxSOAP, cr.maxREST)
	cr.perTarget[vcenterID] = vl
	return vl
}

// Close releases resources
func (cr *ConcurrencyRegistry) Close() error {
	cr.mu.Lock()
	defer cr.mu.Unlock()
	cr.perTarget = make(map[uuid.UUID]*VCenterLimiter)
	return nil
}

func (cr *ConcurrencyRegistry) String() string {
	return fmt.Sprintf("ConcurrencyRegistry(maxSOAP=%d, maxREST=%d)", cr.maxSOAP, cr.maxREST)
}
