package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/credentials"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/types"
)

// Persister defines the contract for persisting topology and inventory batches
type Persister interface {
	PersistTopology(ctx context.Context, snap *TopologySnapshot) error
	PersistVMBatch(ctx context.Context, batch []domain.VMRecord) (int, error)
	ApplyVMIncrementalUpdate(ctx context.Context, vcenterID, moref string, mask VMFieldMask) error
	ApplyVMLeave(ctx context.Context, vcenterID, moref string) error
}

// FleetConfig configures the Multi-Target Fleet Manager (S4.5)
type FleetConfig struct {
	WorkerID                string
	MaxConcurrentHydrations int
	MaxConcurrentSOAP       int
	MaxConcurrentREST       int
	TargetEvalInterval      time.Duration
	FallbackSweepInterval   time.Duration
	DefaultVCenterUsername  string
	DefaultVCenterPassword  string
	DefaultInsecureTLS      bool
	StreamerWaitSeconds     int32
}

func (c *FleetConfig) defaults() {
	if c.WorkerID == "" {
		c.WorkerID = fmt.Sprintf("worker-%s", uuid.NewString()[:8])
	}
	if c.MaxConcurrentHydrations <= 0 {
		c.MaxConcurrentHydrations = 2
	}
	if c.MaxConcurrentSOAP <= 0 {
		c.MaxConcurrentSOAP = 10
	}
	if c.MaxConcurrentREST <= 0 {
		c.MaxConcurrentREST = 4
	}
	if c.TargetEvalInterval <= 0 {
		c.TargetEvalInterval = 10 * time.Second
	}
	if c.FallbackSweepInterval <= 0 {
		c.FallbackSweepInterval = 60 * time.Second
	}
}

type runningTarget struct {
	targetID  uuid.UUID
	vcenterID uuid.UUID
	lease     *ActiveLease
	cancel    context.CancelFunc
	done      chan struct{}
}

// FleetManager dynamically orchestrates collection across multiple vCenter targets (S4.5)
type FleetManager struct {
	cfg              FleetConfig
	pool             *pgxpool.Pool
	queries          *db.Queries
	leaseCoordinator *LeaseCoordinator
	targetFSM        *TargetFSM
	listener         *PostgresListener
	limiter          *ConcurrencyRegistry
	persister        Persister
	logger           *slog.Logger
	metrics          *telemetry.Metrics

	mu        sync.RWMutex
	targets   map[uuid.UUID]*runningTarget
	running   bool
	cancelMgr context.CancelFunc
	wg        sync.WaitGroup
}

func NewFleetManager(
	pool *pgxpool.Pool,
	queries *db.Queries,
	leaseCoordinator *LeaseCoordinator,
	targetFSM *TargetFSM,
	listener *PostgresListener,
	limiter *ConcurrencyRegistry,
	persister Persister,
	cfg FleetConfig,
	metrics *telemetry.Metrics,
	logger *slog.Logger,
) *FleetManager {
	cfg.defaults()
	if logger == nil {
		logger = slog.Default()
	}
	if limiter == nil {
		limiter = NewConcurrencyRegistry(cfg.MaxConcurrentHydrations, cfg.MaxConcurrentSOAP, cfg.MaxConcurrentREST)
	}
	return &FleetManager{
		cfg:              cfg,
		pool:             pool,
		queries:          queries,
		leaseCoordinator: leaseCoordinator,
		targetFSM:        targetFSM,
		listener:         listener,
		limiter:          limiter,
		persister:        persister,
		logger:           logger.With(slog.String("component", "fleet-manager"), slog.String("worker_id", cfg.WorkerID)),
		metrics:          metrics,
		targets:          make(map[uuid.UUID]*runningTarget),
	}
}

// Start boots the listener and begins dynamic target discovery and assignment loop
func (fm *FleetManager) Start(ctx context.Context) error {
	fm.mu.Lock()
	if fm.running {
		fm.mu.Unlock()
		return errors.New("fleet manager already running")
	}
	fm.running = true
	mgrCtx, cancel := context.WithCancel(ctx)
	fm.cancelMgr = cancel
	fm.mu.Unlock()

	// Start PostgreSQL notification listener if provided
	if fm.listener != nil {
		if err := fm.listener.Start(mgrCtx); err != nil {
			fm.logger.Warn("failed to start listener; falling back to periodic polling", slog.String("error", err.Error()))
		}
	}

	fm.wg.Add(1)
	go fm.orchestrationLoop(mgrCtx)

	fm.logger.Info("fleet manager started successfully",
		slog.Int("max_concurrent_hydrations", fm.cfg.MaxConcurrentHydrations),
		slog.Duration("eval_interval", fm.cfg.TargetEvalInterval),
	)
	return nil
}

func (fm *FleetManager) orchestrationLoop(ctx context.Context) {
	defer fm.wg.Done()

	// Initial immediate sweep
	fm.evaluateTargets(ctx)

	evalTicker := time.NewTicker(fm.cfg.TargetEvalInterval)
	defer evalTicker.Stop()

	var eventsChan <-chan VCenterEvent
	var sweepsChan <-chan struct{}
	if fm.listener != nil {
		eventsChan = fm.listener.Events()
		sweepsChan = fm.listener.Sweeps()
	}

	for {
		select {
		case <-ctx.Done():
			return

		case ev := <-eventsChan:
			fm.logger.Debug("fleet received event notification", slog.String("action", ev.Action), slog.String("vcenter_id", ev.VCenterID))
			if ev.Action == "disable" || ev.Action == "delete" {
				vcUUID, err := uuid.Parse(ev.VCenterID)
				if err == nil {
					fm.stopTargetByVCenterID(vcUUID)
				}
			} else {
				if ev.Action == "update" || ev.Action == "resync" {
					if vcUUID, err := uuid.Parse(ev.VCenterID); err == nil {
						fm.stopTargetByVCenterID(vcUUID)
					}
				}
				fm.evaluateTargets(ctx)
			}

		case <-sweepsChan:
			fm.logger.Debug("fleet fallback sweep triggered")
			fm.evaluateTargets(ctx)

		case <-evalTicker.C:
			fm.evaluateTargets(ctx)
		}
	}
}

// evaluateTargets scans for available targets and attempts atomic lease acquisition
func (fm *FleetManager) evaluateTargets(ctx context.Context) {
	available, err := fm.leaseCoordinator.ListAvailableTargets(ctx)
	if err != nil {
		fm.logger.Warn("failed to list available targets", slog.String("error", err.Error()))
		return
	}

	for _, t := range available {
		targetUUID, err := uuid.FromBytes(t.TargetID.Bytes[:])
		if err != nil {
			continue
		}
		vcenterUUID, err := uuid.FromBytes(t.VcenterID.Bytes[:])
		if err != nil {
			continue
		}

		fm.mu.RLock()
		_, alreadyRunning := fm.targets[targetUUID]
		fm.mu.RUnlock()
		if alreadyRunning {
			continue
		}

		// Attempt lease acquisition
		lease, err := fm.leaseCoordinator.Acquire(ctx, targetUUID)
		if err != nil {
			if errors.Is(err, ErrLeaseHeld) {
				continue
			}
			fm.logger.Warn("failed to acquire lease for candidate target",
				slog.String("target_id", targetUUID.String()),
				slog.String("error", err.Error()),
			)
			continue
		}

		// Lease claimed! Spawn pipeline goroutine
		fm.spawnTargetPipeline(targetUUID, vcenterUUID, lease)
	}
}

func (fm *FleetManager) spawnTargetPipeline(targetID, vcenterID uuid.UUID, lease *ActiveLease) {
	fm.mu.Lock()
	if _, exists := fm.targets[targetID]; exists {
		fm.mu.Unlock()
		return
	}

	targetCtx, cancel := context.WithCancel(context.Background())
	rt := &runningTarget{
		targetID:  targetID,
		vcenterID: vcenterID,
		lease:     lease,
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	fm.targets[targetID] = rt
	fm.mu.Unlock()

	fm.logger.Info("spawning target collection pipeline",
		slog.String("target_id", targetID.String()),
		slog.String("vcenter_id", vcenterID.String()),
		slog.Int64("lease_version", lease.Version),
	)

	fm.wg.Add(1)
	go func() {
		defer fm.wg.Done()
		defer close(rt.done)
		defer fm.cleanupTarget(targetID)

		fm.runPipeline(targetCtx, rt)
	}()
}

func (fm *FleetManager) runPipeline(ctx context.Context, rt *runningTarget) {
	targetID := rt.targetID
	vcenterID := rt.vcenterID

	// 1. Mark target ASSIGNED
	_ = fm.targetFSM.Transition(ctx, targetID, TargetStateAssigned, "")

	// 2. Fetch vCenter details from DB
	vc, err := fm.queries.GetVCenterByID(ctx, pgtype.UUID{Bytes: vcenterID, Valid: true})
	if err != nil {
		fm.logger.Error("failed to load vCenter record", slog.String("vcenter_id", vcenterID.String()), slog.String("error", err.Error()))
		_ = fm.targetFSM.Transition(ctx, targetID, TargetStateDegraded, err.Error())
		return
	}

	// Resolve target credentials or fall back to fleet defaults
	username := fm.cfg.DefaultVCenterUsername
	password := fm.cfg.DefaultVCenterPassword
	if vc.CredentialRef != "" {
		if decUser, decPass, err := credentials.Decrypt(vc.CredentialRef); err == nil && decUser != "" {
			username = decUser
			password = decPass
		}
	}

	// Parse TLS settings
	insecure := fm.cfg.DefaultInsecureTLS
	var rootCAs []byte
	if len(vc.TlsSettings) > 0 {
		var tlsMap struct {
			Verify *bool  `json:"verify"`
			CAPEM  string `json:"ca_pem"`
		}
		if err := json.Unmarshal(vc.TlsSettings, &tlsMap); err == nil {
			if tlsMap.Verify != nil && !*tlsMap.Verify {
				insecure = true
			}
			if tlsMap.CAPEM != "" {
				rootCAs = []byte(tlsMap.CAPEM)
			}
		}
	}

	// Session configuration
	sessCfg := vsphere.SessionConfig{
		URL:            vc.ApiUrl,
		Username:       username,
		Password:       password,
		Insecure:       insecure,
		RootCAs:        rootCAs,
		RequestTimeout: 30 * time.Second,
	}

	session, err := vsphere.NewSessionManager(sessCfg)
	if err != nil {
		fm.logger.Error("failed to create session manager", slog.String("vcenter_id", vcenterID.String()), slog.String("error", err.Error()))
		_ = fm.targetFSM.Transition(ctx, targetID, TargetStateDegraded, err.Error())
		return
	}

	if err := session.Connect(ctx); err != nil {
		fm.logger.Warn("vCenter connect failed; pipeline degraded", slog.String("vcenter_id", vcenterID.String()), slog.String("error", err.Error()))
		_ = fm.targetFSM.Transition(ctx, targetID, TargetStateDegraded, err.Error())
		return
	}
	if err := session.Login(ctx); err != nil {
		fm.logger.Warn("vCenter login failed; pipeline degraded", slog.String("vcenter_id", vcenterID.String()), slog.String("error", err.Error()))
		_ = fm.targetFSM.Transition(ctx, targetID, TargetStateDegraded, err.Error())
		return
	}
	defer func() {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = session.Logout(logoutCtx)
	}()

	client := session.Client()
	vcenterIDStr := vcenterID.String()

	// 3. Hydration under global concurrency limiter (S4.5)
	if err := fm.limiter.Global().AcquireHydration(ctx); err != nil {
		fm.logger.Warn("hydration acquisition canceled", slog.String("vcenter_id", vcenterIDStr))
		return
	}

	hydrator := NewHydrator(session, HydratorConfig{MaxObjects: 500})
	tagFetcher := NewRestTagFetcher(
		client.Client,
		vcenterIDStr,
		TagFetcherConfig{Timeout: 15 * time.Second, BatchSize: 500},
		fm.metrics,
	)
	if err := tagFetcher.Init(ctx, session.UserInfo()); err == nil {
		hydrator.SetTagFetcher(tagFetcher)
	}
	defer func() {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tagFetcher.Close(logoutCtx)
	}()

	persister := fm.persister
	if persister == nil {
		fm.limiter.Global().ReleaseHydration()
		fm.logger.Error("no persister configured for fleet manager")
		return
	}

	snap, err := hydrator.HydrateTopology(ctx, vcenterIDStr)
	if err != nil {
		fm.limiter.Global().ReleaseHydration()
		fm.logger.Warn("topology hydration failed", slog.String("vcenter_id", vcenterIDStr), slog.String("error", err.Error()))
		_ = fm.targetFSM.Transition(ctx, targetID, TargetStateDegraded, err.Error())
		return
	}

	if err := persister.PersistTopology(ctx, snap); err != nil {
		fm.limiter.Global().ReleaseHydration()
		fm.logger.Error("topology persistence failed", slog.String("vcenter_id", vcenterIDStr), slog.String("error", err.Error()))
		_ = fm.targetFSM.Transition(ctx, targetID, TargetStateDegraded, err.Error())
		return
	}

	err = hydrator.HydrateInventory(ctx, vcenterIDStr, snap, func(batch []domain.VMRecord) error {
		_, err := persister.PersistVMBatch(ctx, batch)
		return err
	})
	fm.limiter.Global().ReleaseHydration()

	if err != nil {
		fm.logger.Warn("inventory hydration failed", slog.String("vcenter_id", vcenterIDStr), slog.String("error", err.Error()))
		_ = fm.targetFSM.Transition(ctx, targetID, TargetStateDegraded, err.Error())
		return
	}

	// 4. Mark target RUNNING
	_ = fm.targetFSM.RecordSync(ctx, targetID, "")
	_ = fm.targetFSM.Transition(ctx, targetID, TargetStateRunning, "")

	fm.logger.Info("target pipeline running and listening for events",
		slog.String("target_id", targetID.String()),
		slog.String("vcenter_id", vcenterIDStr),
	)

	// Setup pipeline streaming components
	pipelineCtx, cancelPipeline := context.WithCancel(ctx)
	defer cancelPipeline()

	reg := NewCleanupRegistry(fm.logger, 5*time.Second)
	defer func() {
		_ = reg.Teardown(context.Background())
	}()

	viewMgr := view.NewManager(client.Client)
	cv, err := viewMgr.CreateContainerView(pipelineCtx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		fm.logger.Warn("failed to create container view", slog.String("vcenter_id", vcenterIDStr), slog.String("error", err.Error()))
		_ = fm.targetFSM.Transition(ctx, targetID, TargetStateDegraded, err.Error())
		return
	}
	reg.RegisterContainerView(cv.Destroy)

	spec := BuildVMPropertyFilterSpec(cv.Reference())
	streamerCfg := DefaultStreamerConfig(vcenterIDStr)
	if fm.cfg.StreamerWaitSeconds > 0 {
		streamerCfg.MaxWaitSeconds = fm.cfg.StreamerWaitSeconds
	}
	govmomiPC := vsphere.NewGovmomiPropertyCollector(client)
	propStreamer := NewPropertyStreamer(govmomiPC, spec, streamerCfg, fm.metrics, fm.logger, reg)
	if err := propStreamer.InitFilter(pipelineCtx); err != nil {
		fm.logger.Warn("failed to init property filter", slog.String("vcenter_id", vcenterIDStr), slog.String("error", err.Error()))
		_ = fm.targetFSM.Transition(ctx, targetID, TargetStateDegraded, err.Error())
		return
	}

	queue := NewBoundedQueue[types.ObjectUpdate](vcenterIDStr, 1000, fm.metrics)
	applier := NewIncrementalApplier(persister, snap, fm.logger)
	batcherCfg := BatcherConfig{
		VCenterID:     vcenterIDStr,
		MaxBatchSize:  100,
		FlushInterval: 100 * time.Millisecond,
		DrainTimeout:  5 * time.Second,
	}
	batcher := NewStreamBatcher(batcherCfg, queue, applier, fm.metrics, fm.logger, reg)

	var streamWg sync.WaitGroup

	// Run StreamBatcher consumer
	streamWg.Add(1)
	go func() {
		defer streamWg.Done()
		_ = batcher.Run(pipelineCtx)
	}()

	// Run PropertyStreamer producer
	streamWg.Add(1)
	go func() {
		defer streamWg.Done()
		_ = propStreamer.Run(pipelineCtx, func(pCtx context.Context, updates []types.ObjectUpdate) error {
			return queue.PushAll(pCtx, updates)
		})
	}()

	// Run EventStreamer if persister supports events
	if eventPersister, ok := persister.(EventPersister); ok {
		eventMgr := vsphere.NewGovmomiEventManager(client)
		eventStreamer := NewEventStreamer(eventMgr, eventPersister, DefaultEventStreamerConfig(vcenterIDStr), fm.metrics, fm.logger, reg)
		streamWg.Add(1)
		go func() {
			defer streamWg.Done()
			_ = eventStreamer.Run(pipelineCtx)
		}()
	}

	// Run DifferentialReconciler on periodic baseline
	scanner := NewGovmomiReferenceScanner(client.Client)
	reconciler := NewDifferentialReconciler(fm.pool, fm.queries, func(_ context.Context, _ uuid.UUID, _ []string) error {
		return nil
	}, fm.logger)
	streamWg.Add(1)
	go func() {
		defer streamWg.Done()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-pipelineCtx.Done():
				return
			case <-ticker.C:
				_, _ = reconciler.Reconcile(pipelineCtx, vcenterID, scanner)
			}
		}
	}()

	// Pipeline event wait loop: runs until ctx is canceled or lease is lost
	select {
	case <-pipelineCtx.Done():
		fm.logger.Info("target pipeline context canceled; stopping", slog.String("target_id", targetID.String()))
	case <-rt.lease.LostChan:
		fm.logger.Warn("lease lost for target; terminating pipeline", slog.String("target_id", targetID.String()))
	}

	// Stop streamers and wait for goroutines to exit before teardown
	cancelPipeline()
	streamWg.Wait()

	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 5*time.Second)
	_ = batcher.Drain(drainCtx)
	cancelDrain()

	// 5. Transition to REASSIGNING
	_ = fm.targetFSM.Transition(context.Background(), targetID, TargetStateReassigning, "pipeline shutdown")
}

func (fm *FleetManager) cleanupTarget(targetID uuid.UUID) {
	fm.mu.Lock()
	delete(fm.targets, targetID)
	fm.mu.Unlock()

	relCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = fm.leaseCoordinator.Release(relCtx, targetID)
	_ = fm.targetFSM.Transition(relCtx, targetID, TargetStateAvailable, "")
}

func (fm *FleetManager) stopTargetByVCenterID(vcenterID uuid.UUID) {
	fm.mu.RLock()
	var toStop *runningTarget
	for _, rt := range fm.targets {
		if rt.vcenterID == vcenterID {
			toStop = rt
			break
		}
	}
	fm.mu.RUnlock()

	if toStop != nil {
		fm.logger.Info("stopping target pipeline on vCenter change",
			slog.String("target_id", toStop.targetID.String()),
			slog.String("vcenter_id", vcenterID.String()),
		)
		toStop.cancel()
	}
}

// Stop terminates all pipelines, releases leases, and shuts down fleet manager
func (fm *FleetManager) Stop(ctx context.Context) error {
	fm.mu.Lock()
	if !fm.running {
		fm.mu.Unlock()
		return nil
	}
	fm.running = false
	if fm.cancelMgr != nil {
		fm.cancelMgr()
	}

	// Cancel all active pipelines
	for _, rt := range fm.targets {
		rt.cancel()
	}
	fm.mu.Unlock()

	if fm.listener != nil {
		_ = fm.listener.Close()
	}

	// Wait for pipelines to finish
	doneChan := make(chan struct{})
	go func() {
		fm.wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
	case <-ctx.Done():
		fm.logger.Warn("timeout waiting for target pipelines to exit")
	}

	// Release all leases
	return fm.leaseCoordinator.ReleaseAll(ctx)
}

// ActiveTargetCount returns the number of targets currently running
func (fm *FleetManager) ActiveTargetCount() int {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return len(fm.targets)
}

// IsRunning reports whether the fleet manager is actively running.
func (fm *FleetManager) IsRunning() bool {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return fm.running
}
