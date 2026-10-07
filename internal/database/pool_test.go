package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestSanitizeConnString(t *testing.T) {
	raw := "postgres://myuser:secretpassword123@db.corp.internal:5432/sims?sslmode=verify-full&auth_token=tok123"
	sanitized := SanitizeConnString(raw)

	if sanitized == raw {
		t.Errorf("expected sanitized string to differ from raw")
	}
	if contains(sanitized, "secretpassword123") {
		t.Errorf("sanitized string leaked password: %s", sanitized)
	}
	if contains(sanitized, "tok123") {
		t.Errorf("sanitized string leaked auth_token: %s", sanitized)
	}
	if !contains(sanitized, "myuser") {
		t.Errorf("expected username preserved: %s", sanitized)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) > 0 && (substr == s[:len(substr)] || (len(s) > len(substr) && (s[len(s)-len(substr):] == substr || (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()))))
}

func TestDefaultBudgets(t *testing.T) {
	budgets := DefaultBudgets()
	if b, ok := budgets[PoolQueryRO]; !ok || b.MaxConns != 25 {
		t.Errorf("unexpected query_ro budget: %+v", b)
	}
	if b, ok := budgets[PoolCollectorRW]; !ok || b.MaxConns != 15 {
		t.Errorf("unexpected collector_rw budget: %+v", b)
	}
	if b, ok := budgets[PoolReconciliation]; !ok || b.MaxConns != 5 {
		t.Errorf("unexpected reconciliation budget: %+v", b)
	}
	if b, ok := budgets[PoolAdminAudit]; !ok || b.MaxConns != 5 {
		t.Errorf("unexpected admin budget: %+v", b)
	}
}

func TestBudgetedPoolCreationAndIsolation(t *testing.T) {
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5433"
	}
	connURL := fmt.Sprintf("postgres://postgres:postgres@%s:%s/sims?sslmode=disable", host, port)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mgr := NewMultiPoolManager()
	defer mgr.CloseAll()

	pQuery, err := NewBudgetedPool(ctx, connURL, PoolQueryRO)
	if err != nil {
		t.Skipf("Skipping test; DB unreachable: %v", err)
		return
	}
	mgr.Register(PoolQueryRO, pQuery)

	pReconcile, err := NewBudgetedPool(ctx, connURL, PoolReconciliation)
	if err != nil {
		t.Fatalf("failed to create reconciliation pool: %v", err)
	}
	mgr.Register(PoolReconciliation, pReconcile)

	if pQuery.Stat().MaxConns() != 25 {
		t.Errorf("expected maxConns 25 for query_ro, got %d", pQuery.Stat().MaxConns())
	}
	if pReconcile.Stat().MaxConns() != 5 {
		t.Errorf("expected maxConns 5 for reconciliation, got %d", pReconcile.Stat().MaxConns())
	}

	status := mgr.Status()
	if _, ok := status[PoolQueryRO]; !ok {
		t.Errorf("missing query_ro in status")
	}
	if _, ok := status[PoolReconciliation]; !ok {
		t.Errorf("missing reconciliation in status")
	}
}
