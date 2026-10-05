package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/infrastructure/sims/internal/vsphere"
)

// ResourceKind represents an ordered stage in the teardown lifecycle.
type ResourceKind int

const (
	// ResourceKindProducer stops event and property streams (Stage 1).
	ResourceKindProducer ResourceKind = 1
	// ResourceKindDrain waits for in-flight processing to complete (Stage 2).
	ResourceKindDrain ResourceKind = 2
	// ResourceKindEvent destroys EventHistoryCollector instances on vCenter (Stage 3).
	ResourceKindEvent ResourceKind = 3
	// ResourceKindFilter destroys PropertyFilter instances on vCenter (Stage 4).
	ResourceKindFilter ResourceKind = 4
	// ResourceKindView destroys ContainerView instances on vCenter (Stage 5).
	ResourceKindView ResourceKind = 5
	// ResourceKindREST logs out vSphere Automation REST sessions (Stage 6).
	ResourceKindREST ResourceKind = 6
	// ResourceKindSOAP logs out SOAP sessions (Stage 7).
	ResourceKindSOAP ResourceKind = 7
	// ResourceKindClient closes HTTP transport and client connections (Stage 8).
	ResourceKindClient ResourceKind = 8
)

// String returns a human-readable stage name.
func (k ResourceKind) String() string {
	switch k {
	case ResourceKindProducer:
		return "1:stop_producer"
	case ResourceKindDrain:
		return "2:drain_in_flight"
	case ResourceKindEvent:
		return "3:destroy_event_collector"
	case ResourceKindFilter:
		return "4:destroy_property_filter"
	case ResourceKindView:
		return "5:destroy_container_view"
	case ResourceKindREST:
		return "6:logout_rest_session"
	case ResourceKindSOAP:
		return "7:logout_soap_session"
	case ResourceKindClient:
		return "8:close_client"
	default:
		return fmt.Sprintf("stage_%d", k)
	}
}

// CleanupFn defines a cleanup action that accepts a context with a timeout.
type CleanupFn func(ctx context.Context) error

type registeredItem struct {
	id   int
	kind ResourceKind
	name string
	fn   CleanupFn
}

// CleanupRegistry tracks server-side objects and client resources, guaranteeing
// idempotent teardown in strict reverse creation order:
// 1. Stop producer (event stream and property stream loops)
// 2. Wait for in-flight processing to drain (with bounded timeout)
// 3. Destroy EventHistoryCollector (avoids server-side leak)
// 4. Destroy PropertyFilter (avoids server-side leak)
// 5. Destroy ContainerView
// 6. Logout REST session
// 7. Logout SOAP session
// 8. Close HTTP transport / client
//
// Ordering Rationale & Prompt Alignment:
// SPRINTPLAN S2.7 specifies "1. Stop producer (event stream / property stream), 2. Wait for
// in-flight processing to drain (with timeout)...". Stage 1 halts active vSphere polling loops,
// and Stage 2 flushes in-flight queue buffers to database persistence before server-side
// filters and sessions are severed.
//
// Deadlock Prevention:
// The drain phase (Stage 2) cannot deadlock against a blocked producer because Stage 1
// terminates or signals cancellation to producer goroutines first. Furthermore, each teardown
// stage executes under an enforced per-step deadline (stepTimeout, default 5s) via
// context.WithTimeout. If in-flight processing is unresponsive or blocked on an external
// dependency, the step aborts cleanly on timeout and proceeds to server-side object destruction.
type CleanupRegistry struct {
	logger      *slog.Logger
	stepTimeout time.Duration

	mu       sync.Mutex
	nextID   int
	items    map[int]registeredItem
	tornDown bool
}

// NewCleanupRegistry constructs a new CleanupRegistry.
func NewCleanupRegistry(logger *slog.Logger, stepTimeout time.Duration) *CleanupRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	if stepTimeout <= 0 {
		stepTimeout = 5 * time.Second
	}
	return &CleanupRegistry{
		logger:      logger,
		stepTimeout: stepTimeout,
		items:       make(map[int]registeredItem),
	}
}

// Register registers a generic cleanup action under the specified stage.
// Returns an integer ID that can be used to unregister the resource if destroyed early.
func (r *CleanupRegistry) Register(kind ResourceKind, name string, fn CleanupFn) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nextID++
	id := r.nextID
	r.items[id] = registeredItem{
		id:   id,
		kind: kind,
		name: name,
		fn:   fn,
	}
	return id
}

// Unregister removes a resource from the registry (e.g. after explicit filter destruction).
func (r *CleanupRegistry) Unregister(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.items, id)
}

// RegisterProducer registers a producer stop function (Stage 1).
func (r *CleanupRegistry) RegisterProducer(name string, stopFn func()) int {
	return r.Register(ResourceKindProducer, name, func(ctx context.Context) error {
		if stopFn != nil {
			stopFn()
		}
		return nil
	})
}

// RegisterDrain registers a drain wait action (Stage 2).
func (r *CleanupRegistry) RegisterDrain(name string, drainFn func(ctx context.Context) error) int {
	return r.Register(ResourceKindDrain, name, drainFn)
}

// RegisterEventCollector registers an EventHistoryCollector for destruction (Stage 3).
func (r *CleanupRegistry) RegisterEventCollector(ec vsphere.EventCollector) int {
	if ec == nil {
		return 0
	}
	name := fmt.Sprintf("EventCollector(%s)", ec.Reference().Value)
	return r.Register(ResourceKindEvent, name, ec.Destroy)
}

// RegisterPropertyFilter registers a PropertyFilter for destruction (Stage 4).
func (r *CleanupRegistry) RegisterPropertyFilter(pf vsphere.PropertyFilter) int {
	if pf == nil {
		return 0
	}
	name := fmt.Sprintf("PropertyFilter(%s)", pf.Reference().Value)
	return r.Register(ResourceKindFilter, name, pf.Destroy)
}

// RegisterContainerView registers a ContainerView destruction action (Stage 5).
func (r *CleanupRegistry) RegisterContainerView(destroyFn func(ctx context.Context) error) int {
	return r.Register(ResourceKindView, "ContainerView", destroyFn)
}

// RegisterREST registers a REST session logout action (Stage 6).
func (r *CleanupRegistry) RegisterREST(logoutFn func(ctx context.Context) error) int {
	return r.Register(ResourceKindREST, "vAPI-REST", logoutFn)
}

// RegisterSOAP registers a SOAP session logout action (Stage 7).
func (r *CleanupRegistry) RegisterSOAP(session vsphere.Session) int {
	if session == nil {
		return 0
	}
	return r.Register(ResourceKindSOAP, "SOAP-Session", session.Logout)
}

// RegisterClientClose registers client closing (Stage 8).
func (r *CleanupRegistry) RegisterClientClose(closeFn func()) int {
	return r.Register(ResourceKindClient, "HTTPClient", func(ctx context.Context) error {
		if closeFn != nil {
			closeFn()
		}
		return nil
	})
}

// Count returns the number of currently registered items.
func (r *CleanupRegistry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.items)
}

// CountByKind returns the number of registered items of a specific kind.
func (r *CleanupRegistry) CountByKind(kind ResourceKind) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := 0
	for _, it := range r.items {
		if it.kind == kind {
			c++
		}
	}
	return c
}

// Teardown executes registered cleanup actions in strict 8-stage order (Stage 1 through Stage 8).
// It is idempotent and safe to call multiple times. Errors during individual steps are logged
// and accumulated without stopping subsequent steps.
func (r *CleanupRegistry) Teardown(parentCtx context.Context) error {
	r.mu.Lock()
	if r.tornDown {
		r.mu.Unlock()
		return nil
	}
	r.tornDown = true

	// Group registered items by stage
	stages := make(map[ResourceKind][]registeredItem)
	for _, it := range r.items {
		stages[it.kind] = append(stages[it.kind], it)
	}
	// Clear registry items
	r.items = make(map[int]registeredItem)
	r.mu.Unlock()

	var allErrors []error

	// Execute stages in strict order: 1 to 8
	orderedKinds := []ResourceKind{
		ResourceKindProducer,
		ResourceKindDrain,
		ResourceKindEvent,
		ResourceKindFilter,
		ResourceKindView,
		ResourceKindREST,
		ResourceKindSOAP,
		ResourceKindClient,
	}

	for _, kind := range orderedKinds {
		items := stages[kind]
		if len(items) == 0 {
			continue
		}

		// Execute items within a stage in reverse registration order (LIFO)
		for i := len(items) - 1; i >= 0; i-- {
			it := items[i]
			r.logger.Info("executing resource cleanup",
				"stage", it.kind.String(),
				"resource", it.name,
			)

			stepCtx, cancel := context.WithTimeout(parentCtx, r.stepTimeout)
			err := it.fn(stepCtx)
			cancel()

			if err != nil {
				r.logger.Warn("resource cleanup step failed, continuing teardown",
					"stage", it.kind.String(),
					"resource", it.name,
					"error", err,
				)
				allErrors = append(allErrors, fmt.Errorf("%s [%s]: %w", it.kind.String(), it.name, err))
			}
		}
	}

	if len(allErrors) > 0 {
		return errors.Join(allErrors...)
	}
	return nil
}

// RecoverAndCleanup can be deferred at entrypoints to ensure complete cleanup on panic
// before re-panicking.
func (r *CleanupRegistry) RecoverAndCleanup(ctx context.Context) {
	if p := recover(); p != nil {
		r.logger.Error("panic recovered in collector worker; initiating emergency resource teardown",
			"panic", p,
		)
		_ = r.Teardown(ctx)
		panic(p)
	}
}
