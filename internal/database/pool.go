package database

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolType designates the operational domain and isolation budget of a database connection pool.
type PoolType string

const (
	// PoolQueryRO is dedicated to read-only search and API queries.
	PoolQueryRO PoolType = "query_ro"

	// PoolCollectorRW is dedicated to high-throughput collector event and batch ingestion.
	PoolCollectorRW PoolType = "collector_rw"

	// PoolReconciliation is an isolated pool preventing reconciliation sweeps from starving ingestion.
	PoolReconciliation PoolType = "reconciliation"

	// PoolAdminAudit is for administrative tasks, target management, and audit logging.
	PoolAdminAudit PoolType = "admin_audit"
)

// PoolBudget defines connection quotas and lifecycle boundaries for a pool type.
type PoolBudget struct {
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ApplicationName   string
	DefaultUser       string
}

// DefaultBudgets returns hard-bounded connection quotas tailored to prevent resource starvation.
func DefaultBudgets() map[PoolType]PoolBudget {
	return map[PoolType]PoolBudget{
		PoolQueryRO: {
			MaxConns:          25,
			MinConns:          3,
			MaxConnLifetime:   1 * time.Hour,
			MaxConnIdleTime:   15 * time.Minute,
			HealthCheckPeriod: 30 * time.Second,
			ApplicationName:   "sims_query_ro",
			DefaultUser:       "sims",
		},
		PoolCollectorRW: {
			MaxConns:          15,
			MinConns:          3,
			MaxConnLifetime:   1 * time.Hour,
			MaxConnIdleTime:   15 * time.Minute,
			HealthCheckPeriod: 30 * time.Second,
			ApplicationName:   "sims_collector_rw",
			DefaultUser:       "sims",
		},
		PoolReconciliation: {
			MaxConns:          5,
			MinConns:          1,
			MaxConnLifetime:   30 * time.Minute,
			MaxConnIdleTime:   5 * time.Minute,
			HealthCheckPeriod: 30 * time.Second,
			ApplicationName:   "sims_reconciler",
			DefaultUser:       "sims",
		},
		PoolAdminAudit: {
			MaxConns:          5,
			MinConns:          1,
			MaxConnLifetime:   30 * time.Minute,
			MaxConnIdleTime:   5 * time.Minute,
			HealthCheckPeriod: 30 * time.Second,
			ApplicationName:   "sims_admin",
			DefaultUser:       "sims",
		},
	}
}

// NewBudgetedPool constructs a pgxpool with isolated budgets and connection parameters.
func NewBudgetedPool(ctx context.Context, connURL string, pType PoolType, budgetOverrides ...PoolBudget) (*pgxpool.Pool, error) {
	budget, ok := DefaultBudgets()[pType]
	if !ok {
		return nil, fmt.Errorf("unknown pool type %q", pType)
	}
	if len(budgetOverrides) > 0 {
		o := budgetOverrides[0]
		if o.MaxConns > 0 {
			budget.MaxConns = o.MaxConns
		}
		if o.MinConns > 0 {
			budget.MinConns = o.MinConns
		}
		if o.MaxConnLifetime > 0 {
			budget.MaxConnLifetime = o.MaxConnLifetime
		}
		if o.MaxConnIdleTime > 0 {
			budget.MaxConnIdleTime = o.MaxConnIdleTime
		}
		if o.ApplicationName != "" {
			budget.ApplicationName = o.ApplicationName
		}
	}

	// Parse base configuration
	parsedURL, err := url.Parse(connURL)
	if err != nil {
		return nil, fmt.Errorf("invalid connection URL: %w", err)
	}

	// Ensure application_name query param is set
	q := parsedURL.Query()
	q.Set("application_name", budget.ApplicationName)
	parsedURL.RawQuery = q.Encode()

	poolCfg, err := pgxpool.ParseConfig(parsedURL.String())
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgxpool config: %w", err)
	}

	poolCfg.MaxConns = budget.MaxConns
	poolCfg.MinConns = budget.MinConns
	poolCfg.MaxConnLifetime = budget.MaxConnLifetime
	poolCfg.MaxConnIdleTime = budget.MaxConnIdleTime
	poolCfg.HealthCheckPeriod = budget.HealthCheckPeriod

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize pool for %s: %w", pType, err)
	}

	return pool, nil
}

// MultiPoolManager manages isolated pools across workloads to prevent cross-service resource contention.
type MultiPoolManager struct {
	mu    sync.RWMutex
	pools map[PoolType]*pgxpool.Pool
}

// NewMultiPoolManager constructs an empty manager.
func NewMultiPoolManager() *MultiPoolManager {
	return &MultiPoolManager{
		pools: make(map[PoolType]*pgxpool.Pool),
	}
}

// Register registers a budgeted pool under a specific type.
func (m *MultiPoolManager) Register(pType PoolType, pool *pgxpool.Pool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pools[pType] = pool
}

// Get returns the registered pool for a specific pool type.
func (m *MultiPoolManager) Get(pType PoolType) (*pgxpool.Pool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pools[pType]
	return p, ok
}

// CloseAll cleanly closes all registered connection pools.
func (m *MultiPoolManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.pools {
		if p != nil {
			p.Close()
		}
	}
	m.pools = make(map[PoolType]*pgxpool.Pool)
}

// Status returns connection stats across all registered pools.
func (m *MultiPoolManager) Status() map[PoolType]map[string]int32 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	stats := make(map[PoolType]map[string]int32)
	for pt, p := range m.pools {
		if p != nil {
			s := p.Stat()
			stats[pt] = map[string]int32{
				"total_conns":    s.TotalConns(),
				"idle_conns":     s.IdleConns(),
				"acquired_conns": s.AcquiredConns(),
				"max_conns":      s.MaxConns(),
			}
		}
	}
	return stats
}

// SanitizeConnString removes credentials from connection strings before logging or tracing.
func SanitizeConnString(connStr string) string {
	u, err := url.Parse(connStr)
	if err != nil {
		return "[invalid-url]"
	}
	if u.User != nil {
		u.User = url.UserPassword(u.User.Username(), "REDACTED")
	}
	// Also ensure password in query params (if any) is stripped
	q := u.Query()
	for k := range q {
		if strings.Contains(strings.ToLower(k), "pass") || strings.Contains(strings.ToLower(k), "token") {
			q.Set(k, "REDACTED")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
