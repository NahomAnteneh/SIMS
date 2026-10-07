package telemetry_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/infrastructure/sims/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTracer_W3CPropagation(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp, cleanup, err := telemetry.InitTracer("sims-test", "test", exp)
	if err != nil {
		t.Fatalf("failed to init tracer: %v", err)
	}
	defer func() { _ = cleanup(context.Background()) }()

	// Create root span
	ctx, rootSpan := telemetry.StartSpan(context.Background(), "root-operation")
	traceID := rootSpan.SpanContext().TraceID().String()

	// Inject W3C carrier
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://example.com/api/test", nil)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	traceparent := req.Header.Get("traceparent")
	if traceparent == "" {
		t.Fatal("expected traceparent header to be injected by W3C propagator")
	}

	// Extract in another context
	extractedCtx := otel.GetTextMapPropagator().Extract(context.Background(), propagation.HeaderCarrier(req.Header))
	extractedSpan := trace.SpanFromContext(extractedCtx)
	if extractedSpan.SpanContext().TraceID().String() != traceID {
		t.Fatalf("expected trace ID %s, got %s", traceID, extractedSpan.SpanContext().TraceID().String())
	}
	rootSpan.End()
	_ = tp.ForceFlush(context.Background())
}

func TestTracer_SecretSanitizationInvariant(t *testing.T) {
	// Critical Invariant: Passwords, API tokens, and vCenter session tokens must never appear in span attributes
	tests := []struct {
		name     string
		attr     attribute.KeyValue
		expected string
	}{
		{
			name:     "password key redacted",
			attr:     attribute.String("vcenter.password", "SuperSecretPass123!"),
			expected: "[REDACTED]",
		},
		{
			name:     "token key redacted",
			attr:     attribute.String("api.token", "bearer_xyz_98765"),
			expected: "[REDACTED]",
		},
		{
			name:     "session_token key redacted",
			attr:     attribute.String("vmware_session_token", "vmware-session-ticket-abc"),
			expected: "[REDACTED]",
		},
		{
			name:     "url with credentials redacted",
			attr:     attribute.String("vcenter.endpoint", "https://administrator%40vsphere.local:MySecretPassword@vcsa.corp.local/sdk"),
			expected: "https://administrator%40vsphere.local:[REDACTED]@vcsa.corp.local/sdk",
		},
		{
			name:     "bearer auth header redacted",
			attr:     attribute.String("http.header", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"),
			expected: "Bearer [REDACTED]",
		},
		{
			name:     "harmless key preserved",
			attr:     attribute.String("vcenter.datacenter", "datacenter-1"),
			expected: "datacenter-1",
		},
		{
			name:     "vm moref preserved",
			attr:     attribute.String("vm.moref", "vm-42"),
			expected: "vm-42",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sanitized := telemetry.SanitizeAttribute(tc.attr)
			if sanitized.Value.AsString() != tc.expected {
				t.Fatalf("expected attribute %s value to be %q, got %q", tc.attr.Key, tc.expected, sanitized.Value.AsString())
			}
		})
	}
}

func TestHTTPTraceMiddleware(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp, cleanup, err := telemetry.InitTracer("sims-test-http", "test", exp)
	if err != nil {
		t.Fatalf("failed to init tracer: %v", err)
	}
	defer func() { _ = cleanup(context.Background()) }()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if !span.SpanContext().IsValid() {
			t.Error("expected valid span context in HTTP request")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	mw := telemetry.HTTPTraceMiddleware("test-service")(handler)

	req := httptest.NewRequest("GET", "/api/v1/vms", nil)
	req.Header.Set("User-Agent", "TestClient/1.0")
	rec := httptest.NewRecorder()

	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	_ = tp.ForceFlush(context.Background())
	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least 1 span to be recorded by HTTPTraceMiddleware")
	}

	httpSpan := spans[len(spans)-1]
	if httpSpan.Name != "HTTP GET /api/v1/vms" {
		t.Fatalf("expected span name 'HTTP GET /api/v1/vms', got %s", httpSpan.Name)
	}

	foundMethod := false
	foundStatus := false
	for _, attr := range httpSpan.Attributes {
		if attr.Key == "http.method" && attr.Value.AsString() == "GET" {
			foundMethod = true
		}
		if attr.Key == "http.status_code" && attr.Value.AsInt64() == 200 {
			foundStatus = true
		}
	}

	if !foundMethod || !foundStatus {
		t.Fatalf("missing expected http attributes: method=%v, status=%v", foundMethod, foundStatus)
	}
}

func TestStartSpan_SanitizesStartAttributes(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	ctx, span := telemetry.StartSpan(context.Background(), "test.start_span",
		trace.WithAttributes(
			attribute.String("vcenter.password", "leaked-secret"),
			attribute.String("safe.key", "hello"),
		),
	)
	span.End()

	_ = tp.ForceFlush(ctx)
	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected span")
	}
	for _, attr := range spans[0].Attributes {
		if attr.Key == "vcenter.password" && attr.Value.AsString() != telemetry.RedactedVal {
			t.Fatalf("expected password attribute to be redacted, got %s", attr.Value.AsString())
		}
	}
}

func TestRecordSafeError_RedactsSecrets(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	ctx, span := telemetry.StartSpan(context.Background(), "test.error_span")
	telemetry.RecordSafeError(span, errors.New("connection failed to https://user:secret123@vcenter.corp.local/sdk"))
	span.End()

	_ = tp.ForceFlush(ctx)
	spans := exp.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected span")
	}
	statusDesc := spans[0].Status.Description
	if strings.Contains(statusDesc, "secret123") {
		t.Fatalf("status description leaked credentials: %s", statusDesc)
	}
	if !strings.Contains(statusDesc, "[REDACTED]") {
		t.Fatalf("status description did not contain [REDACTED]: %s", statusDesc)
	}
}
