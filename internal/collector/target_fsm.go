package collector

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/infrastructure/sims/generated/db"
)

// Target state constants defined in S4.3
const (
	TargetStateAvailable   = "AVAILABLE"
	TargetStateAssigned    = "ASSIGNED"
	TargetStateRunning     = "RUNNING"
	TargetStateDegraded    = "DEGRADED"
	TargetStateReassigning = "REASSIGNING"
)

// AllowedTransitions map defines valid state transitions
var AllowedTransitions = map[string]map[string]bool{
	TargetStateAvailable: {
		TargetStateAssigned: true,
	},
	TargetStateAssigned: {
		TargetStateRunning:     true,
		TargetStateDegraded:    true,
		TargetStateReassigning: true,
	},
	TargetStateRunning: {
		TargetStateDegraded:    true,
		TargetStateReassigning: true,
	},
	TargetStateDegraded: {
		TargetStateRunning:     true,
		TargetStateReassigning: true,
	},
	TargetStateReassigning: {
		TargetStateAvailable: true,
		TargetStateAssigned:  true,
	},
}

// TargetFSM manages target assignment states and transitions (S4.3)
type TargetFSM struct {
	queries *db.Queries
	logger  *slog.Logger
	mu      sync.Mutex
}

func NewTargetFSM(queries *db.Queries, logger *slog.Logger) *TargetFSM {
	if logger == nil {
		logger = slog.Default()
	}
	return &TargetFSM{
		queries: queries,
		logger:  logger.With(slog.String("component", "target-fsm")),
	}
}

// IsValidTransition verifies whether moving from fromState to toState is allowed
func IsValidTransition(fromState, toState string) bool {
	if fromState == toState {
		return true
	}
	allowed, ok := AllowedTransitions[fromState]
	if !ok {
		// If unknown fromState (e.g. legacy 'INIT'), allow transition to initial states
		return toState == TargetStateAvailable || toState == TargetStateAssigned
	}
	return allowed[toState]
}

// Transition validates and updates the target status in database
func (fsm *TargetFSM) Transition(ctx context.Context, targetID uuid.UUID, toState string, errMsg string) error {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	pgTargetID := pgtype.UUID{Bytes: targetID, Valid: true}
	existing, err := fsm.queries.GetCollectorStateByTargetID(ctx, pgTargetID)
	currentState := TargetStateAvailable
	if err == nil && existing.Status != "" {
		currentState = existing.Status
	}

	if !IsValidTransition(currentState, toState) {
		return fmt.Errorf("illegal state transition from %s to %s for target %s", currentState, toState, targetID)
	}

	var errText *string
	if errMsg != "" {
		errText = &errMsg
	}

	var errorPgText pgtype.Text
	if errText != nil {
		errorPgText = pgtype.Text{String: *errText, Valid: true}
	}

	_, err = fsm.queries.UpsertCollectorState(ctx, db.UpsertCollectorStateParams{
		TargetID:     pgTargetID,
		Status:       toState,
		ErrorMessage: errorPgText,
	})
	if err != nil {
		return fmt.Errorf("failed to persist collector state transition: %w", err)
	}

	fsm.logger.Info("target state transitioned",
		slog.String("target_id", targetID.String()),
		slog.String("from", currentState),
		slog.String("to", toState),
	)

	return nil
}

// RecordSync updates the last_sync_at timestamp and sync token on successful sync
func (fsm *TargetFSM) RecordSync(ctx context.Context, targetID uuid.UUID, syncToken string) error {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()

	pgTargetID := pgtype.UUID{Bytes: targetID, Valid: true}
	var tokenPgText pgtype.Text
	if syncToken != "" {
		tokenPgText = pgtype.Text{String: syncToken, Valid: true}
	}

	_, err := fsm.queries.UpsertCollectorState(ctx, db.UpsertCollectorStateParams{
		TargetID:   pgTargetID,
		Status:     TargetStateRunning,
		LastSyncAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		SyncToken:  tokenPgText,
	})
	if err != nil {
		return fmt.Errorf("failed to record sync timestamp: %w", err)
	}

	return nil
}

// GetState retrieves current collector state for the target
func (fsm *TargetFSM) GetState(ctx context.Context, targetID uuid.UUID) (*db.CollectorState, error) {
	pgTargetID := pgtype.UUID{Bytes: targetID, Valid: true}
	st, err := fsm.queries.GetCollectorStateByTargetID(ctx, pgTargetID)
	if err != nil {
		return nil, err
	}
	return &st, nil
}
