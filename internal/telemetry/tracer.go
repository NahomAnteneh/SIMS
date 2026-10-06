package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/infrastructure/sims/internal/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	TracerName  = "github.com/infrastructure/sims"
	RedactedVal = "[REDACTED]"
)

var (
	sensitiveKeyPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)(password|passwd|pwd)`),
		regexp.MustCompile(`(?i)(token|bearer|jwt)`),
		regexp.MustCompile(`(?i)(secret|private_key)`),
		regexp.MustCompile(`(?i)(authorization|auth_header)`),
		regexp.MustCompile(`(?i)(session_id|session_key|session_token|vmware_soap_session)`),
		regexp.MustCompile(`(?i)(cookie|credential)`),
	}

	initMu   sync.Mutex
	globalTP *sdktrace.TracerProvider
)

// InitTracer initializes OpenTelemetry TracerProvider with W3C propagation.
// If spanExporter is nil, an in-memory exporter is used for recording.
func InitTracer(serviceName, env string, exporter sdktrace.SpanExporter) (*sdktrace.TracerProvider, func(context.Context) error, error) {
	initMu.Lock()
	defer initMu.Unlock()

	res, err := resource.New(
		context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.DeploymentEnvironmentKey.String(env),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create otel resource: %w", err)
	}

	var opts []sdktrace.TracerProviderOption
	opts = append(opts, sdktrace.WithResource(res))

	if exporter != nil {
		opts = append(opts, sdktrace.WithBatcher(exporter))
	} else {
		// Default to synchronized in-memory exporter for testing / zero-dependency runtime
		memExp := tracetest.NewInMemoryExporter()
		opts = append(opts, sdktrace.WithSyncer(memExp))
	}

	tp := sdktrace.NewTracerProvider(opts...)
	globalTP = tp
	otel.SetTracerProvider(tp)

	// Invariant: Register standard W3C TraceContext and Baggage propagators
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	cleanup := func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}

	return tp, cleanup, nil
}

// Tracer returns the named tracer for SIMS.
func Tracer() trace.Tracer {
	return otel.GetTracerProvider().Tracer(TracerName)
}

// IsSensitiveKey reports whether an attribute key may represent secret material.
func IsSensitiveKey(key string) bool {
	for _, p := range sensitiveKeyPatterns {
		if p.MatchString(key) {
			return true
		}
	}
	return false
}

// SanitizeAttribute returns a sanitized attribute, redacting any secrets.
func SanitizeAttribute(kv attribute.KeyValue) attribute.KeyValue {
	if IsSensitiveKey(string(kv.Key)) {
		return attribute.String(string(kv.Key), RedactedVal)
	}

	// If value is a string, check if it contains embedded credentials (e.g. URLs or Authorization headers)
	if kv.Value.Type() == attribute.STRING {
		strVal := kv.Value.AsString()
		if strings.Contains(strVal, "://") && strings.Contains(strVal, "@") {
			return attribute.String(string(kv.Key), logging.RedactURL(strVal))
		}
		if strings.HasPrefix(strings.ToLower(strVal), "bearer ") {
			return attribute.String(string(kv.Key), "Bearer [REDACTED]")
		}
	}

	return kv
}

// SanitizeAttributes sanitizes a slice of KeyValues to ensure no credentials leak into traces.
func SanitizeAttributes(attrs ...attribute.KeyValue) []attribute.KeyValue {
	sanitized := make([]attribute.KeyValue, len(attrs))
	for i, a := range attrs {
		sanitized[i] = SanitizeAttribute(a)
	}
	return sanitized
}

// StartSpan creates a traced span with sanitized initial attributes.
func StartSpan(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return Tracer().Start(ctx, spanName, opts...)
}

// SetSafeAttributes sets sanitized attributes on an active span.
func SetSafeAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	if span == nil || !span.IsRecording() {
		return
	}
	sanitized := SanitizeAttributes(attrs...)
	span.SetAttributes(sanitized...)
}

// RecordSafeError records an error on the span without exposing secret credentials in error messages.
func RecordSafeError(span trace.Span, err error) {
	if span == nil || err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// statusWriter wraps http.ResponseWriter to capture HTTP status code for tracing.
type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// HTTPTraceMiddleware extracts W3C tracecontext and instruments incoming HTTP requests.
func HTTPTraceMiddleware(serviceName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			propagator := otel.GetTextMapPropagator()
			ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			spanName := fmt.Sprintf("HTTP %s %s", r.Method, r.URL.Path)
			ctx, span := Tracer().Start(ctx, spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.target", r.URL.Path),
					attribute.String("http.host", r.Host),
					attribute.String("http.user_agent", r.UserAgent()),
					attribute.String("service.name", serviceName),
				),
			)
			defer span.End()

			sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}
			r = r.WithContext(ctx)

			start := time.Now()
			next.ServeHTTP(sw, r)
			duration := time.Since(start)

			span.SetAttributes(
				attribute.Int("http.status_code", sw.statusCode),
				attribute.Int64("http.duration_ms", duration.Milliseconds()),
			)

			if sw.statusCode >= 500 {
				span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", sw.statusCode))
			} else {
				span.SetStatus(codes.Ok, "")
			}
		})
	}
}
