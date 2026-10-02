package telemetry

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds the registered Prometheus metrics collectors.
type Metrics struct {
	Registry        *prometheus.Registry
	RequestsTotal   *prometheus.CounterVec
	RequestDuration *prometheus.HistogramVec
	BuildInfo       *prometheus.GaugeVec
	serviceName     string
}

// NewMetrics initializes Prometheus metrics for the given service.
func NewMetrics(serviceName, version string) *Metrics {
	reg := prometheus.NewRegistry()

	// Register standard Go runtime and process metrics
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	requestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests processed, partitioned by service, method, path, and status code.",
		},
		[]string{"service", "method", "path", "status"},
	)

	requestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Latency of HTTP requests in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "method", "path"},
	)

	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sims_build_info",
			Help: "Constant 1 value labeled with build and version information.",
		},
		[]string{"service", "version"},
	)

	reg.MustRegister(requestsTotal)
	reg.MustRegister(requestDuration)
	reg.MustRegister(buildInfo)

	buildInfo.WithLabelValues(serviceName, version).Set(1)

	return &Metrics{
		Registry:        reg,
		RequestsTotal:   requestsTotal,
		RequestDuration: requestDuration,
		BuildInfo:       buildInfo,
		serviceName:     serviceName,
	}
}

// Handler returns the HTTP handler for exposing Prometheus metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.statusCode = code
	rec.ResponseWriter.WriteHeader(code)
}

// Middleware wraps an http.Handler to automatically record HTTP metrics.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(rec, r)

		duration := time.Since(start).Seconds()
		path := r.URL.Path
		method := r.Method
		status := strconv.Itoa(rec.statusCode)

		m.RequestsTotal.WithLabelValues(m.serviceName, method, path, status).Inc()
		m.RequestDuration.WithLabelValues(m.serviceName, method, path).Observe(duration)
	})
}
