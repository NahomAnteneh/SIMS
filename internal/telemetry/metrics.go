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

	// Collector & Persistence Metrics
	HydrationDuration     *prometheus.HistogramVec
	HydrationObjectsTotal *prometheus.CounterVec
	BatchSize             *prometheus.HistogramVec
	FlushDuration         *prometheus.HistogramVec
	CopyDuration          *prometheus.HistogramVec
	MergeDuration         *prometheus.HistogramVec
	CompactionRatio       *prometheus.HistogramVec

	// Tag & Custom Attribute Metrics
	TagFetchDuration          *prometheus.HistogramVec
	TagFetchFailures          *prometheus.CounterVec
	TagsPersisted             *prometheus.CounterVec
	CustomAttributesPersisted *prometheus.CounterVec

	// Session Guardian & Collector Lifecycle Metrics (Sprint 2)
	VCenterInfo                     *prometheus.GaugeVec
	KeepaliveTotal                  *prometheus.CounterVec
	KeepaliveErrorsTotal            *prometheus.CounterVec
	SessionState                    *prometheus.GaugeVec
	CollectorState                  *prometheus.GaugeVec
	CollectorStateTransitionsTotal  *prometheus.CounterVec

	serviceName string
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

	hydrationDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sims_hydration_duration_seconds",
			Help:    "Duration of vSphere inventory hydration runs.",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		},
		[]string{"vcenter_id"},
	)

	hydrationObjectsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sims_hydration_objects_total",
			Help: "Total count of objects hydrated from vSphere.",
		},
		[]string{"vcenter_id", "kind"},
	)

	batchSize := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sims_persistence_batch_size",
			Help:    "Number of records in persistence flush batches.",
			Buckets: []float64{10, 50, 100, 250, 500, 1000, 2500, 5000},
		},
		[]string{"vcenter_id"},
	)

	flushDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sims_persistence_flush_duration_seconds",
			Help:    "Total wall-clock duration of persistence batch flush.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0},
		},
		[]string{"vcenter_id"},
	)

	copyDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sims_persistence_copy_duration_seconds",
			Help:    "Duration of binary COPY FROM STDIN to staging table.",
			Buckets: []float64{0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1.0},
		},
		[]string{"vcenter_id"},
	)

	mergeDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sims_persistence_merge_duration_seconds",
			Help:    "Duration of set-based merge from staging into production tables.",
			Buckets: []float64{0.005, 0.01, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5},
		},
		[]string{"vcenter_id"},
	)

	compactionRatio := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sims_persistence_compaction_ratio",
			Help:    "Ratio of deduplicated records in batch compaction: (raw - compacted) / raw.",
			Buckets: []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.75, 0.9},
		},
		[]string{"vcenter_id"},
	)

	tagFetchDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "sims_tag_fetch_duration_seconds",
			Help:    "Duration of vSphere Automation REST tag fetch operations.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0},
		},
		[]string{"vcenter_id"},
	)

	tagFetchFailures := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sims_tag_fetch_failures_total",
			Help: "Total count of vSphere Automation REST tag fetch failures.",
		},
		[]string{"vcenter_id"},
	)

	tagsPersisted := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sims_tags_persisted_total",
			Help: "Total count of VM tags persisted into database.",
		},
		[]string{"vcenter_id"},
	)

	customAttributesPersisted := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sims_custom_attributes_persisted_total",
			Help: "Total count of VM custom attributes persisted into database.",
		},
		[]string{"vcenter_id"},
	)

	vcenterInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sims_vcenter_info",
			Help: "Information gauge reporting vCenter connection metadata with major.minor.",
		},
		[]string{"vcenter_id", "major_minor"},
	)

	keepaliveTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sims_collector_keepalive_total",
			Help: "Total count of proactive session keepalive probes executed.",
		},
		[]string{"vcenter_id"},
	)

	keepaliveErrorsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sims_collector_keepalive_errors_total",
			Help: "Total count of failed proactive session keepalive probes.",
		},
		[]string{"vcenter_id"},
	)

	sessionState := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sims_collector_session_state",
			Help: "Current state of vCenter session: 0=disconnected, 1=connected, 2=degraded.",
		},
		[]string{"vcenter_id"},
	)

	collectorState := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "sims_collector_state",
			Help: "Current state of collector worker: 0=stopped, 1=connecting, 2=hydrating, 3=streaming, 4=resyncing, 5=reconnecting, 6=paused.",
		},
		[]string{"vcenter_id"},
	)

	collectorStateTransitionsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "sims_collector_state_transitions_total",
			Help: "Total count of collector worker state machine transitions.",
		},
		[]string{"vcenter_id", "from_state", "to_state"},
	)

	reg.MustRegister(requestsTotal)
	reg.MustRegister(requestDuration)
	reg.MustRegister(buildInfo)
	reg.MustRegister(hydrationDuration)
	reg.MustRegister(hydrationObjectsTotal)
	reg.MustRegister(batchSize)
	reg.MustRegister(flushDuration)
	reg.MustRegister(copyDuration)
	reg.MustRegister(mergeDuration)
	reg.MustRegister(compactionRatio)
	reg.MustRegister(tagFetchDuration)
	reg.MustRegister(tagFetchFailures)
	reg.MustRegister(tagsPersisted)
	reg.MustRegister(customAttributesPersisted)
	reg.MustRegister(vcenterInfo)
	reg.MustRegister(keepaliveTotal)
	reg.MustRegister(keepaliveErrorsTotal)
	reg.MustRegister(sessionState)
	reg.MustRegister(collectorState)
	reg.MustRegister(collectorStateTransitionsTotal)

	buildInfo.WithLabelValues(serviceName, version).Set(1)

	return &Metrics{
		Registry:                       reg,
		RequestsTotal:                  requestsTotal,
		RequestDuration:                requestDuration,
		BuildInfo:                      buildInfo,
		HydrationDuration:              hydrationDuration,
		HydrationObjectsTotal:          hydrationObjectsTotal,
		BatchSize:                      batchSize,
		FlushDuration:                  flushDuration,
		CopyDuration:                   copyDuration,
		MergeDuration:                  mergeDuration,
		CompactionRatio:                compactionRatio,
		TagFetchDuration:               tagFetchDuration,
		TagFetchFailures:               tagFetchFailures,
		TagsPersisted:                  tagsPersisted,
		CustomAttributesPersisted:      customAttributesPersisted,
		VCenterInfo:                    vcenterInfo,
		KeepaliveTotal:                 keepaliveTotal,
		KeepaliveErrorsTotal:           keepaliveErrorsTotal,
		SessionState:                   sessionState,
		CollectorState:                 collectorState,
		CollectorStateTransitionsTotal: collectorStateTransitionsTotal,
		serviceName:                    serviceName,
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
