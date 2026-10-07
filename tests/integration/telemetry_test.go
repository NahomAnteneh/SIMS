//go:build integration
// +build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	db "github.com/infrastructure/sims/generated/db"
	"github.com/infrastructure/sims/internal/api"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/persistence"
	"github.com/infrastructure/sims/internal/server"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOpenTelemetry_EndToEndTracingAndSanitization(t *testing.T) {
	ctx := context.Background()

	// 1. Initialize In-Memory OpenTelemetry Exporter
	exporter := tracetest.NewInMemoryExporter()
	tp, cleanup, err := telemetry.InitTracer("sims-otel-test", "integration", exporter)
	if err != nil {
		t.Fatalf("failed to initialize tracer: %v", err)
	}
	defer func() { _ = cleanup(ctx) }()

	pool, err := pgxpool.New(ctx, getConnString("collector_rw", "collector_rw_pass"))
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer pool.Close()

	// 2. Test HTTP Search Tracing with W3C Tracecontext
	srv := server.New("sims-api", "v1.0.0-test", "0", testLogger())
	apiHandler := api.NewHandler(pool, testLogger())
	apiHandler.RegisterRoutes(srv.Mux)

	clientTraceID := "4bf92f3577b34da6a3ce929d0e0e4736"
	clientSpanID := "00f067aa0ba902b7"
	traceparentHeader := "00-" + clientTraceID + "-" + clientSpanID + "-01"

	req := httptest.NewRequest(http.MethodGet, "/v1/search/vms?q=test", nil)
	req.Header.Set("traceparent", traceparentHeader)
	req.Header.Set("Authorization", "Bearer sensitive-jwt-secret-token")
	rec := httptest.NewRecorder()

	srv.Handler().ServeHTTP(rec, req)

	// Flush spans
	_ = tp.ForceFlush(ctx)
	spans := exporter.GetSpans()

	var foundHTTPSpan, foundCompileSpan, foundDBSpan bool
	for _, s := range spans {
		// Verify W3C propagation linked to client's Trace ID
		if s.SpanContext.TraceID().String() == clientTraceID {
			if strings.HasPrefix(s.Name, "HTTP GET /v1/search/vms") {
				foundHTTPSpan = true
			}
			if s.Name == "search.ast_compile" {
				foundCompileSpan = true
			}
			if s.Name == "search.db_query" {
				foundDBSpan = true
			}
		}
	}

	if !foundHTTPSpan {
		t.Error("expected HTTP server span linked to incoming W3C traceparent")
	}
	if !foundCompileSpan {
		t.Error("expected search.ast_compile span to be recorded")
	}
	if !foundDBSpan {
		t.Error("expected search.db_query span to be recorded")
	}

	// 3. Test Collector Persistence Spans
	exporter.Reset()
	persister := persistence.NewBulkPersister(pool, persistence.WriterConfig{
		FlushInterval: 100 * time.Millisecond,
		FlushSize:     10,
	}, nil)

	testVCenterID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO vcenters (id, name, api_url, credential_ref, status)
		VALUES ($1, 'test-vc-otel', $2, 'cred-ref-1', 'active')
		ON CONFLICT (id) DO NOTHING
	`, testVCenterID, "https://vcenter-otel-"+testVCenterID.String()+".test/sdk")
	if err != nil {
		t.Fatalf("failed to insert test vcenter: %v", err)
	}

	testRecords := []domain.VMRecord{
		{
			VCenterID:       testVCenterID.String(),
			MoRef:           "vm-otel-1",
			Name:            "vm-otel-tracing-test",
			PowerState:      "poweredOn",
			ConnectionState: "connected",
			CPUCores:        2,
			MemoryMB:        4096,
		},
	}

	_, err = persister.PersistVMBatch(ctx, testRecords)
	if err != nil {
		t.Fatalf("PersistVMBatch failed: %v", err)
	}

	_ = tp.ForceFlush(ctx)
	persistSpans := exporter.GetSpans()

	var foundFlushSpan, foundCopySpan, foundMergeSpan bool
	for _, s := range persistSpans {
		if s.Name == "persistence.batch_flush" {
			foundFlushSpan = true
		}
		if s.Name == "persistence.binary_copy" {
			foundCopySpan = true
		}
		if s.Name == "persistence.merge_transaction" {
			foundMergeSpan = true
		}
	}

	if !foundFlushSpan {
		t.Error("expected persistence.batch_flush span to be recorded")
	}
	if !foundCopySpan {
		t.Error("expected persistence.binary_copy span to be recorded")
	}
	if !foundMergeSpan {
		t.Error("expected persistence.merge_transaction span to be recorded")
	}

	// 4. Test Lease Coordinator Spans
	exporter.Reset()
	targetID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO collector_targets (id, vcenter_id, enabled)
		VALUES ($1, $2, true)
	`, targetID, testVCenterID)
	if err != nil {
		t.Fatalf("failed to insert test collector_target: %v", err)
	}

	queries := db.New(pool)
	leaseCoord := collector.NewLeaseCoordinator(pool, queries, collector.LeaseConfig{
		WorkerID: "worker-otel-test",
		LeaseTTL: 5 * time.Second,
	}, testLogger())

	activeLease, err := leaseCoord.Acquire(ctx, targetID)
	if err != nil {
		t.Fatalf("lease acquire failed: %v", err)
	}
	err = leaseCoord.Renew(ctx, activeLease)
	if err != nil {
		t.Fatalf("lease renew failed: %v", err)
	}
	err = leaseCoord.Release(ctx, targetID)
	if err != nil {
		t.Fatalf("lease release failed: %v", err)
	}

	_ = tp.ForceFlush(ctx)
	leaseSpans := exporter.GetSpans()
	var foundAcquire, foundRenew, foundRelease bool
	for _, s := range leaseSpans {
		if s.Name == "lease.acquire" {
			foundAcquire = true
		}
		if s.Name == "lease.renew" {
			foundRenew = true
		}
		if s.Name == "lease.release" {
			foundRelease = true
		}
	}
	if !foundAcquire || !foundRenew || !foundRelease {
		t.Errorf("expected lease spans (acquire=%v, renew=%v, release=%v)", foundAcquire, foundRenew, foundRelease)
	}

	// 5. CRITICAL INVARIANT: Zero Secret Leaks Across ALL Spans
	allSpans := append(spans, persistSpans...)
	allSpans = append(allSpans, leaseSpans...)
	forbiddenSubstrings := []string{
		"sensitive-jwt-secret-token",
		"SuperSecretPassword",
		"vmware-session-ticket",
	}

	for _, s := range allSpans {
		for _, attr := range s.Attributes {
			valStr := attr.Value.AsString()
			for _, forbidden := range forbiddenSubstrings {
				if strings.Contains(valStr, forbidden) {
					t.Fatalf("CRITICAL SECURITY INVARIANT VIOLATION: span %s leaked secret %q in attribute %s=%q",
						s.Name, forbidden, attr.Key, valStr)
				}
			}
			if telemetry.IsSensitiveKey(string(attr.Key)) && valStr != telemetry.RedactedVal {
				t.Fatalf("CRITICAL SECURITY INVARIANT VIOLATION: sensitive key %s was not redacted, value=%q",
					attr.Key, valStr)
			}
		}

		for _, evt := range s.Events {
			for _, attr := range evt.Attributes {
				valStr := attr.Value.AsString()
				for _, forbidden := range forbiddenSubstrings {
					if strings.Contains(valStr, forbidden) {
						t.Fatalf("CRITICAL SECURITY INVARIANT VIOLATION: span event leaked secret in %s: %s",
							s.Name, valStr)
					}
				}
			}
		}
	}

	t.Log("SUCCESS: OpenTelemetry end-to-end tracing and secret sanitization verified with 0 leaks.")
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Suppress unused import errors
var _ = json.Marshal
var _ = propagation.TraceContext{}
var _ = otel.GetTracerProvider
