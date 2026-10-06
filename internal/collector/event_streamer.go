package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi/vim25/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// V1EventTypes defines the minimal, verified v1 event set per docs/events-v1.md.
var V1EventTypes = []string{
	"VmCreatedEvent",
	"VmClonedEvent",
	"VmDeployedEvent",
	"VmRegisteredEvent",
	"VmDiscoveredEvent",
	"VmRemovedEvent",
	"VmMigratedEvent",
	"VmRelocatedEvent",
}

// EventRecord represents a normalized vSphere inventory event for ingestion.
type EventRecord struct {
	EventKey    int64
	ChainID     int64
	EventType   string
	EntityMoref string
	EntityName  string
	EntityType  string
	UserName    string
	Message     string
	CreatedTime time.Time
	Payload     []byte
}

// EventPersister defines the database contract required by EventStreamer.
type EventPersister interface {
	GetEventWatermark(ctx context.Context, vcenterID string) (int64, *time.Time, error)
	SaveEventWatermark(ctx context.Context, vcenterID string, lastEventKey int64, lastEventTime time.Time) error
	PersistEvent(ctx context.Context, vcenterID string, event EventRecord) error
}

// EventStreamerConfig defines tuning parameters for the EventHistoryCollector streamer.
type EventStreamerConfig struct {
	VCenterID                 string
	PageSize                  int32
	PollInterval              time.Duration
	OverlapWindow             time.Duration
	CallDeadline              time.Duration
	ReconcileOnGap            bool
	OnReconciliationRequested func()
}

// DefaultEventStreamerConfig returns production-safe defaults.
func DefaultEventStreamerConfig(vcenterID string) EventStreamerConfig {
	return EventStreamerConfig{
		VCenterID:      vcenterID,
		PageSize:       100,
		PollInterval:   2 * time.Second,
		OverlapWindow:  5 * time.Minute,
		CallDeadline:   30 * time.Second,
		ReconcileOnGap: true,
	}
}

// EventStreamer manages the lifecycle of an EventHistoryCollector session,
// ingesting lifecycle and provenance events into PostgreSQL with deduplication and replay semantics.
type EventStreamer struct {
	em         vsphere.EventManager
	persister  EventPersister
	cfg        EventStreamerConfig
	metrics    *telemetry.Metrics
	logger     *slog.Logger
	cleanupReg *CleanupRegistry
}

// NewEventStreamer creates a new EventStreamer.
func NewEventStreamer(
	em vsphere.EventManager,
	persister EventPersister,
	cfg EventStreamerConfig,
	metrics *telemetry.Metrics,
	logger *slog.Logger,
	cleanupReg *CleanupRegistry,
) *EventStreamer {
	if cfg.PageSize <= 0 {
		cfg.PageSize = 100
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.OverlapWindow <= 0 {
		cfg.OverlapWindow = 5 * time.Minute
	}
	if cfg.CallDeadline <= 0 {
		cfg.CallDeadline = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &EventStreamer{
		em:         em,
		persister:  persister,
		cfg:        cfg,
		metrics:    metrics,
		logger:     logger.With("component", "event_streamer", "vcenter_id", cfg.VCenterID),
		cleanupReg: cleanupReg,
	}
}

// Run executes the event streaming loop until ctx is canceled or a fatal error occurs.
func (s *EventStreamer) Run(ctx context.Context) error {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if s.cleanupReg != nil {
		s.cleanupReg.RegisterProducer("event-producer", cancel)
	}

	// 1. Retrieve last known event watermark
	var lastKey int64
	var lastTime *time.Time
	if s.persister != nil {
		var err error
		lastKey, lastTime, err = s.persister.GetEventWatermark(streamCtx, s.cfg.VCenterID)
		if err != nil {
			s.logger.Warn("failed to fetch event watermark, starting fresh", "error", err)
		}
	}

	// 2. Build EventFilterSpec for v1 event set
	filter := types.EventFilterSpec{
		EventTypeId: V1EventTypes,
	}

	// If resuming from a known timestamp, apply overlap lookback window (ADR-010)
	if lastTime != nil && !lastTime.IsZero() {
		beginTime := lastTime.Add(-s.cfg.OverlapWindow)
		filter.Time = &types.EventFilterSpecByTime{
			BeginTime: &beginTime,
		}
		s.logger.Info("resuming event streaming with overlap window",
			"last_event_time", lastTime.Format(time.RFC3339),
			"overlap_begin", beginTime.Format(time.RFC3339),
			"last_event_key", lastKey,
		)
	}

	// 3. Create server-side EventHistoryCollector
	createCtx, cancelCreate := context.WithTimeout(streamCtx, s.cfg.CallDeadline)
	collector, err := s.em.CreateCollectorForEvents(createCtx, filter)
	cancelCreate()
	if err != nil {
		return fmt.Errorf("failed to create EventHistoryCollector: %w", err)
	}

	// Register with CleanupRegistry (Stage 3)
	var regID int
	if s.cleanupReg != nil {
		regID = s.cleanupReg.RegisterEventCollector(collector)
		defer func() {
			if !s.cleanupReg.IsTornDown() {
				s.cleanupReg.Unregister(regID)
				_ = collector.Destroy(context.Background())
			}
		}()
	} else {
		defer func() {
			_ = collector.Destroy(context.Background())
		}()
	}

	firstBatch := true

	// 4. Ingestion loop
	for {
		select {
		case <-streamCtx.Done():
			s.logger.Info("event streaming loop stopping: context canceled")
			return streamCtx.Err()
		default:
		}

		readCtx, cancelRead := context.WithTimeout(streamCtx, s.cfg.CallDeadline)
		_, eventSpan := telemetry.StartSpan(readCtx, "collector.event_retrieval",
			trace.WithAttributes(
				attribute.String("vcenter.id", s.cfg.VCenterID),
				attribute.Int("batch.size_limit", int(s.cfg.PageSize)),
			),
		)
		events, readErr := collector.ReadNextEvents(readCtx, s.cfg.PageSize)
		cancelRead()
		if readErr != nil {
			telemetry.RecordSafeError(eventSpan, readErr)
		} else {
			eventSpan.SetAttributes(attribute.Int("events.retrieved_count", len(events)))
		}
		eventSpan.End()

		if readErr != nil {
			if errors.Is(readErr, context.Canceled) || streamCtx.Err() != nil {
				return streamCtx.Err()
			}
			s.logger.Error("error reading events from collector", "error", readErr)
			select {
			case <-streamCtx.Done():
				return streamCtx.Err()
			case <-time.After(s.cfg.PollInterval):
				continue
			}
		}

		if len(events) == 0 {
			// No new events; wait for poll interval without clearing firstBatch flag
			select {
			case <-streamCtx.Done():
				return streamCtx.Err()
			case <-time.After(s.cfg.PollInterval):
				continue
			}
		}

		// Check for retention gap on first batch when resuming (S2.9)
		if firstBatch && lastTime != nil && !lastTime.IsZero() {
			firstBatch = false
			oldestEvent := events[0].GetEvent()
			// If oldest event returned is newer than last known time, intermediate events were lost
			if oldestEvent.CreatedTime.After(*lastTime) {
				s.logger.Warn("event retention gap detected: oldest returned event is newer than last recorded watermark",
					"oldest_event_time", oldestEvent.CreatedTime.Format(time.RFC3339),
					"last_event_time", lastTime.Format(time.RFC3339),
				)
				if s.metrics != nil {
					s.metrics.EventGapDetected.WithLabelValues(s.cfg.VCenterID).Inc()
				}
				if s.cfg.ReconcileOnGap {
					if s.metrics != nil {
						s.metrics.EventReconciliationRequested.WithLabelValues(s.cfg.VCenterID).Inc()
					}
					if s.cfg.OnReconciliationRequested != nil {
						s.cfg.OnReconciliationRequested()
					}
				}
			}
		}

		// Process and persist event batch
		for _, baseEvent := range events {
			rawEvent := baseEvent.GetEvent()
			eventType := EventTypeName(baseEvent)

			var entityMoref, entityName string
			if rawEvent.Vm != nil {
				entityMoref = rawEvent.Vm.Vm.Value
				entityName = rawEvent.Vm.Name
			}

			payloadBytes, _ := json.Marshal(baseEvent)

			record := EventRecord{
				EventKey:    int64(rawEvent.Key),
				ChainID:     int64(rawEvent.ChainId),
				EventType:   eventType,
				EntityMoref: entityMoref,
				EntityName:  entityName,
				EntityType:  "VirtualMachine",
				UserName:    rawEvent.UserName,
				Message:     rawEvent.FullFormattedMessage,
				CreatedTime: rawEvent.CreatedTime,
				Payload:     payloadBytes,
			}

			if s.persister != nil {
				if persistErr := s.persister.PersistEvent(streamCtx, s.cfg.VCenterID, record); persistErr != nil {
					s.logger.Error("failed to persist event", "event_key", record.EventKey, "error", persistErr)
				}
			}

			// Advance in-memory watermark
			lastKey = record.EventKey
			t := record.CreatedTime
			lastTime = &t

			if s.metrics != nil {
				s.metrics.EventsIngestedTotal.WithLabelValues(s.cfg.VCenterID, eventType).Inc()
				lag := time.Since(record.CreatedTime).Seconds()
				if lag >= 0 {
					s.metrics.EventsStreamLagSeconds.WithLabelValues(s.cfg.VCenterID).Set(lag)
				}
			}
		}

		// Persist updated watermark to collector_state
		if s.persister != nil && lastTime != nil {
			_, wmSpan := telemetry.StartSpan(streamCtx, "collector.watermark_save",
				trace.WithAttributes(
					attribute.String("vcenter.id", s.cfg.VCenterID),
					attribute.Int64("watermark.key", lastKey),
				),
			)
			if saveErr := s.persister.SaveEventWatermark(streamCtx, s.cfg.VCenterID, lastKey, *lastTime); saveErr != nil {
				telemetry.RecordSafeError(wmSpan, saveErr)
				s.logger.Error("failed to update event watermark in collector_state", "error", saveErr)
			}
			wmSpan.End()
		}
	}
}

// EventTypeName returns the concrete vSphere event type name from BaseEvent.
func EventTypeName(be types.BaseEvent) string {
	if be == nil {
		return "UnknownEvent"
	}
	t := reflect.TypeOf(be)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}
