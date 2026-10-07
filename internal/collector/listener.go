package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

const VCenterEventsChannel = "sims_vcenter_events"

// VCenterEvent represents configuration update or resync notification (S4.4)
type VCenterEvent struct {
	VCenterID string `json:"vcenter_id"`
	Action    string `json:"action"` // "create", "update", "disable", "delete", "resync"
}

// PostgresListener handles real-time PostgreSQL LISTEN/NOTIFY and fallback sweeps (S4.4)
type PostgresListener struct {
	databaseURL   string
	sweepInterval time.Duration
	logger        *slog.Logger

	eventsChan chan VCenterEvent
	sweepsChan chan struct{}

	mu     sync.Mutex
	conn   *pgx.Conn
	cancel context.CancelFunc
	done   chan struct{}
	wg     sync.WaitGroup
}

func NewPostgresListener(databaseURL string, sweepInterval time.Duration, logger *slog.Logger) *PostgresListener {
	if sweepInterval <= 0 {
		sweepInterval = 60 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &PostgresListener{
		databaseURL:   databaseURL,
		sweepInterval: sweepInterval,
		logger:        logger.With(slog.String("component", "postgres-listener")),
		eventsChan:    make(chan VCenterEvent, 100),
		sweepsChan:    make(chan struct{}, 10),
		done:          make(chan struct{}),
	}
}

// Events returns the channel receiving real-time NOTIFY events
func (l *PostgresListener) Events() <-chan VCenterEvent {
	return l.eventsChan
}

// Sweeps returns the channel receiving periodic fallback sweep signals
func (l *PostgresListener) Sweeps() <-chan struct{} {
	return l.sweepsChan
}

// Start connects dedicated pgx.Conn, issues LISTEN, and launches notification + sweep loops
func (l *PostgresListener) Start(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	loopCtx, cancel := context.WithCancel(ctx)
	l.cancel = cancel

	// Connect dedicated connection
	conn, err := pgx.Connect(loopCtx, l.databaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect dedicated listener connection: %w", err)
	}
	l.conn = conn

	_, err = conn.Exec(loopCtx, "LISTEN "+VCenterEventsChannel)
	if err != nil {
		_ = conn.Close(context.Background())
		return fmt.Errorf("failed to issue LISTEN %s: %w", VCenterEventsChannel, err)
	}

	l.logger.Info("listening on PostgreSQL notification channel", slog.String("channel", VCenterEventsChannel))

	// Notification receiver loop
	l.wg.Add(2)
	go func() {
		defer l.wg.Done()
		l.listenLoop(loopCtx)
	}()

	// Periodic fallback sweep loop (S4.4)
	go func() {
		defer l.wg.Done()
		l.sweepLoop(loopCtx)
	}()

	return nil
}

func (l *PostgresListener) listenLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		l.mu.Lock()
		conn := l.conn
		l.mu.Unlock()

		if conn == nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		notification, err := conn.WaitForNotification(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			l.logger.Warn("listener connection lost; reconnecting", slog.String("error", err.Error()))
			l.reconnect(ctx)
			continue
		}

		if notification == nil {
			continue
		}

		var ev VCenterEvent
		if err := json.Unmarshal([]byte(notification.Payload), &ev); err != nil {
			l.logger.Warn("malformed notification payload",
				slog.String("payload", notification.Payload),
				slog.String("error", err.Error()),
			)
			continue
		}

		l.logger.Debug("received vCenter notification",
			slog.String("vcenter_id", ev.VCenterID),
			slog.String("action", ev.Action),
		)

		select {
		case l.eventsChan <- ev:
		default:
			l.logger.Warn("event channel full; dropping notification to prevent blocking")
		}
	}
}

func (l *PostgresListener) reconnect(ctx context.Context) {
	backoff := 500 * time.Millisecond
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		conn, err := pgx.Connect(ctx, l.databaseURL)
		if err != nil {
			l.logger.Warn("reconnect failed; backing off", slog.String("error", err.Error()))
			backoff *= 2
			if backoff > 15*time.Second {
				backoff = 15 * time.Second
			}
			continue
		}

		_, err = conn.Exec(ctx, "LISTEN "+VCenterEventsChannel)
		if err != nil {
			_ = conn.Close(context.Background())
			continue
		}

		l.mu.Lock()
		if l.conn != nil {
			_ = l.conn.Close(context.Background())
		}
		l.conn = conn
		l.mu.Unlock()

		l.logger.Info("reconnected and re-issued LISTEN successfully", slog.String("channel", VCenterEventsChannel))
		return
	}
}

func (l *PostgresListener) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(l.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			select {
			case l.sweepsChan <- struct{}{}:
			default:
			}
		}
	}
}

// Close terminates the listener loops and connection
func (l *PostgresListener) Close() error {
	l.mu.Lock()
	if l.cancel != nil {
		l.cancel()
	}
	l.mu.Unlock()

	l.wg.Wait()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = l.conn.Close(ctx)
		l.conn = nil
	}
	return nil
}
