package collector

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPostgresListener_NotifyAndReceive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping db-dependent test in short mode")
	}

	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5433"
	}
	connStr := fmt.Sprintf("postgres://postgres:postgres@%s:%s/sims?sslmode=disable", host, port)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Verify DB connectivity
	testConn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		t.Skipf("cannot connect to postgres: %v", err)
	}
	defer testConn.Close(ctx)

	listener := NewPostgresListener(connStr, 2*time.Second, nil)
	if err := listener.Start(ctx); err != nil {
		t.Fatalf("failed to start listener: %v", err)
	}
	defer listener.Close()

	// Wait briefly for LISTEN to establish
	time.Sleep(100 * time.Millisecond)

	// Send NOTIFY
	payload := `{"vcenter_id": "vc-12345", "action": "create"}`
	_, err = testConn.Exec(ctx, "SELECT pg_notify($1, $2)", VCenterEventsChannel, payload)
	if err != nil {
		t.Fatalf("failed to send pg_notify: %v", err)
	}

	// Receive event
	select {
	case ev := <-listener.Events():
		if ev.VCenterID != "vc-12345" || ev.Action != "create" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for NOTIFY event")
	}
}
