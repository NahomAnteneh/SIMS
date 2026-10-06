package benchmark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/search"
	"github.com/jackc/pgx/v5/pgxpool"
)

// QueryType identifies the specific pattern in the query workload model.
type QueryType string

const (
	QueryExactUUID        QueryType = "exact_uuid"
	QueryMoRef            QueryType = "moref"
	QueryExactName        QueryType = "exact_name"
	QueryNamePrefix       QueryType = "name_prefix"
	QueryNameSubstring    QueryType = "name_substring"
	QueryHostname         QueryType = "hostname"
	QueryIP               QueryType = "exact_ip"
	QueryCIDR             QueryType = "cidr_subnet"
	QueryMAC              QueryType = "mac_address"
	QueryVLANOverlap      QueryType = "vlan_overlap"
	QueryDatastore        QueryType = "datastore"
	QueryCluster          QueryType = "cluster"
	QueryFolderSubtree    QueryType = "folder_subtree"
	QueryTag              QueryType = "tag"
	QueryCustomAttr       QueryType = "custom_attribute"
	QueryFacetCounts      QueryType = "facet_counts"
	QueryKeysetPagination QueryType = "keyset_pagination"
)

// DefaultWeights returns realistic production query distribution weightings.
func DefaultWeights() map[QueryType]int {
	return map[QueryType]int{
		QueryNamePrefix:       20,
		QueryNameSubstring:    15,
		QueryHostname:         10,
		QueryIP:               10,
		QueryCIDR:             8,
		QueryMAC:              7,
		QueryExactUUID:        5,
		QueryMoRef:            5,
		QueryExactName:        5,
		QueryFolderSubtree:    5,
		QueryCluster:          4,
		QueryDatastore:        3,
		QueryTag:              3,
		QueryCustomAttr:       2,
		QueryKeysetPagination: 2,
		QueryFacetCounts:      1,
	}
}

// WorkloadConfig configures a mixed query benchmark execution.
type WorkloadConfig struct {
	Concurrency  int
	Duration     time.Duration
	TotalQueries int
	Weights      map[QueryType]int
	HTTPClient   *http.Client
	APIBaseURL   string // If set, executes against HTTP API instead of raw SQL
	Seed         int64
}

// QueryTypeMetrics tracks latency and error metrics per query pattern.
type QueryTypeMetrics struct {
	Count     int64
	Errors    int64
	Latencies []time.Duration
	P50       time.Duration
	P90       time.Duration
	P95       time.Duration
	P99       time.Duration
	Min       time.Duration
	Max       time.Duration
	Mean      time.Duration
}

// WorkloadResult aggregates overall and per-type benchmark results.
type WorkloadResult struct {
	TotalQueries   int64
	SuccessCount   int64
	ErrorCount     int64
	Duration       time.Duration
	QPS            float64
	P50            time.Duration
	P90            time.Duration
	P95            time.Duration
	P99            time.Duration
	Min            time.Duration
	Max            time.Duration
	Mean           time.Duration
	QueryBreakdown map[QueryType]*QueryTypeMetrics
}

// WorkloadRunner coordinates concurrent execution of the mixed query workload.
type WorkloadRunner struct {
	pool  *pgxpool.Pool
	cfg   WorkloadConfig
	stats *DatasetStats
	rnd   *rand.Rand
}

// NewWorkloadRunner creates a new workload execution runner.
func NewWorkloadRunner(pool *pgxpool.Pool, cfg WorkloadConfig, stats *DatasetStats) *WorkloadRunner {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 10
	}
	if cfg.Duration <= 0 && cfg.TotalQueries <= 0 {
		cfg.Duration = 5 * time.Second
	}
	if len(cfg.Weights) == 0 {
		cfg.Weights = DefaultWeights()
	}
	if cfg.Seed == 0 {
		cfg.Seed = time.Now().UnixNano()
	}
	// #nosec G404 -- pseudo-random generator is intentionally used for workload simulation
	return &WorkloadRunner{
		pool:  pool,
		cfg:   cfg,
		stats: stats,
		rnd:   rand.New(rand.NewSource(cfg.Seed)),
	}
}

// Run executes the mixed query workload against PostgreSQL or HTTP API and aggregates metrics.
func (r *WorkloadRunner) Run(ctx context.Context) (*WorkloadResult, error) {
	// Build weighted selection list
	var weightedList []QueryType
	for qt, weight := range r.cfg.Weights {
		for i := 0; i < weight; i++ {
			weightedList = append(weightedList, qt)
		}
	}
	if len(weightedList) == 0 {
		return nil, fmt.Errorf("no weighted query types defined")
	}

	type executionSample struct {
		qType   QueryType
		latency time.Duration
		err     error
	}

	samplesCh := make(chan executionSample, 10000)
	var wg sync.WaitGroup
	var completedCount int64

	runCtx := ctx
	var cancel context.CancelFunc
	if r.cfg.Duration > 0 {
		runCtx, cancel = context.WithTimeout(ctx, r.cfg.Duration)
		defer cancel()
	}

	startTime := time.Now()

	// Launch worker pool
	for w := 0; w < r.cfg.Concurrency; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			// #nosec G404 -- pseudo-random generator is intentionally used for workload simulation
			localRnd := rand.New(rand.NewSource(r.cfg.Seed + int64(workerID)*997))

			for {
				select {
				case <-runCtx.Done():
					return
				default:
				}

				if r.cfg.TotalQueries > 0 {
					if atomic.AddInt64(&completedCount, 1) > int64(r.cfg.TotalQueries) {
						return
					}
				}

				qType := weightedList[localRnd.Intn(len(weightedList))]
				qStart := time.Now()
				err := r.executeQuery(runCtx, qType, localRnd)
				latency := time.Since(qStart)

				samplesCh <- executionSample{
					qType:   qType,
					latency: latency,
					err:     err,
				}
			}
		}(w)
	}

	// Collector goroutine
	collectorDone := make(chan struct{})
	var allLatencies []time.Duration
	perTypeLatencies := make(map[QueryType][]time.Duration)
	perTypeErrors := make(map[QueryType]int64)
	var successCount, errorCount int64

	go func() {
		defer close(collectorDone)
		for s := range samplesCh {
			if s.err != nil {
				if errors.Is(s.err, context.DeadlineExceeded) || errors.Is(s.err, context.Canceled) {
					// Run has timed out; ignore in-flight query cancellation
					continue
				}
				errorCount++
				perTypeErrors[s.qType]++
				if errorCount <= 5 {
					fmt.Printf("[Workload Warning] Query %s failed: %v\n", s.qType, s.err)
				}
			} else {
				successCount++
				allLatencies = append(allLatencies, s.latency)
				perTypeLatencies[s.qType] = append(perTypeLatencies[s.qType], s.latency)
			}
		}
	}()

	wg.Wait()
	close(samplesCh)
	<-collectorDone

	totalDuration := time.Since(startTime)
	totalQueries := successCount + errorCount

	result := &WorkloadResult{
		TotalQueries:   totalQueries,
		SuccessCount:   successCount,
		ErrorCount:     errorCount,
		Duration:       totalDuration,
		QueryBreakdown: make(map[QueryType]*QueryTypeMetrics),
	}

	if totalDuration.Seconds() > 0 {
		result.QPS = float64(totalQueries) / totalDuration.Seconds()
	}

	// Compute overall percentiles
	if len(allLatencies) > 0 {
		sort.Slice(allLatencies, func(i, j int) bool { return allLatencies[i] < allLatencies[j] })
		result.Min = allLatencies[0]
		result.Max = allLatencies[len(allLatencies)-1]
		result.P50 = allLatencies[int(float64(len(allLatencies))*0.50)]
		result.P90 = allLatencies[int(float64(len(allLatencies))*0.90)]
		result.P95 = allLatencies[int(float64(len(allLatencies))*0.95)]
		p99Idx := int(float64(len(allLatencies)) * 0.99)
		if p99Idx >= len(allLatencies) {
			p99Idx = len(allLatencies) - 1
		}
		result.P99 = allLatencies[p99Idx]

		var sum time.Duration
		for _, d := range allLatencies {
			sum += d
		}
		result.Mean = sum / time.Duration(len(allLatencies))
	}

	// Compute per-type percentiles
	for qt, latencies := range perTypeLatencies {
		m := &QueryTypeMetrics{
			Count:  int64(len(latencies)),
			Errors: perTypeErrors[qt],
		}
		if len(latencies) > 0 {
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			m.Min = latencies[0]
			m.Max = latencies[len(latencies)-1]
			m.P50 = latencies[int(float64(len(latencies))*0.50)]
			m.P90 = latencies[int(float64(len(latencies))*0.90)]
			m.P95 = latencies[int(float64(len(latencies))*0.95)]
			p99Idx := int(float64(len(latencies)) * 0.99)
			if p99Idx >= len(latencies) {
				p99Idx = len(latencies) - 1
			}
			m.P99 = latencies[p99Idx]

			var sum time.Duration
			for _, d := range latencies {
				sum += d
			}
			m.Mean = sum / time.Duration(len(latencies))
		}
		result.QueryBreakdown[qt] = m
	}

	return result, nil
}

func (r *WorkloadRunner) executeQuery(ctx context.Context, qType QueryType, rnd *rand.Rand) error {
	if r.cfg.APIBaseURL != "" && r.cfg.HTTPClient != nil {
		return r.executeHTTPQuery(ctx, qType, rnd)
	}
	return r.executeSQLQuery(ctx, qType, rnd)
}

func (r *WorkloadRunner) executeSQLQuery(ctx context.Context, qType QueryType, rnd *rand.Rand) error {
	var sql string
	var args []any

	switch qType {
	case QueryExactUUID:
		vmID := uuid.New()
		if len(r.stats.SampleVMIDs) > 0 {
			vmID = r.stats.SampleVMIDs[rnd.Intn(len(r.stats.SampleVMIDs))]
		}
		sql = `SELECT id, name, power_state FROM virtual_machines WHERE id = $1 AND deleted_at IS NULL;`
		args = []any{vmID}

	case QueryMoRef:
		vcID := uuid.New()
		moref := "vm-1"
		if len(r.stats.VCenterIDs) > 0 {
			vcID = r.stats.VCenterIDs[rnd.Intn(len(r.stats.VCenterIDs))]
		}
		if len(r.stats.SampleMoRefs) > 0 {
			moref = r.stats.SampleMoRefs[rnd.Intn(len(r.stats.SampleMoRefs))]
		}
		sql = `SELECT id, name, power_state FROM virtual_machines WHERE vcenter_id = $1 AND moref = $2 AND deleted_at IS NULL;`
		args = []any{vcID, moref}

	case QueryExactName:
		name := "prod-web-frontend-00001"
		if len(r.stats.SampleVMNames) > 0 {
			name = r.stats.SampleVMNames[rnd.Intn(len(r.stats.SampleVMNames))]
		}
		sql = `SELECT id, name, power_state FROM virtual_machines WHERE name = $1 AND deleted_at IS NULL;`
		args = []any{name}

	case QueryNamePrefix:
		prefix := "prod-web-"
		sql = `SELECT id, name, power_state FROM virtual_machines WHERE name LIKE $1 || '%' AND deleted_at IS NULL LIMIT $2;`
		args = []any{prefix, 25}

	case QueryNameSubstring:
		substring := "frontend"
		sql = `SELECT id, name, power_state FROM virtual_machines WHERE name ILIKE '%' || $1 || '%' AND deleted_at IS NULL LIMIT $2;`
		args = []any{substring, 25}

	case QueryHostname:
		pattern := "corp.local"
		sql = `SELECT id, name, guest_host_name FROM virtual_machines WHERE guest_host_name ILIKE '%' || $1 || '%' AND deleted_at IS NULL LIMIT $2;`
		args = []any{pattern, 25}

	case QueryIP:
		ip := "10.0.0.1"
		if len(r.stats.SampleIPs) > 0 {
			ip = r.stats.SampleIPs[rnd.Intn(len(r.stats.SampleIPs))]
		}
		sql = `SELECT id, name, guest_ip_address FROM virtual_machines WHERE guest_ip_address = $1::inet AND deleted_at IS NULL;`
		args = []any{ip}

	case QueryCIDR:
		cidr := "10.0.0.0/8"
		if len(r.stats.SampleCIDRs) > 0 {
			cidr = r.stats.SampleCIDRs[rnd.Intn(len(r.stats.SampleCIDRs))]
		}
		sql = `SELECT id, name, guest_ip_address FROM virtual_machines WHERE guest_ip_address <<= $1::inet AND deleted_at IS NULL LIMIT $2;`
		args = []any{cidr, 25}

	case QueryMAC:
		mac := "00:50:56:00:00:01"
		if len(r.stats.SampleMACs) > 0 {
			mac = r.stats.SampleMACs[rnd.Intn(len(r.stats.SampleMACs))]
		}
		sql = `SELECT v.id, v.name, n.mac_address FROM virtual_machines v JOIN vm_network_interfaces n ON v.id = n.vm_id WHERE n.mac_address = $1 AND v.deleted_at IS NULL;`
		args = []any{mac}

	case QueryVLANOverlap:
		vlanSpec := "{[100,100]}"
		if len(r.stats.SampleVLANMultiranges) > 0 {
			vlanSpec = r.stats.SampleVLANMultiranges[rnd.Intn(len(r.stats.SampleVLANMultiranges))]
		}
		sql = `SELECT v.id, v.name, n.vlan_ids FROM virtual_machines v JOIN vm_network_interfaces n ON v.id = n.vm_id WHERE n.vlan_ids && $1::int4multirange AND v.deleted_at IS NULL LIMIT $2;`
		args = []any{vlanSpec, 25}

	case QueryDatastore:
		dsID := uuid.New()
		if len(r.stats.DatastoreIDs) > 0 {
			dsID = r.stats.DatastoreIDs[rnd.Intn(len(r.stats.DatastoreIDs))]
		}
		sql = `SELECT v.id, v.name FROM virtual_machines v JOIN vm_datastores d ON v.id = d.vm_id WHERE d.datastore_id = $1 AND v.deleted_at IS NULL LIMIT $2;`
		args = []any{dsID, 25}

	case QueryCluster:
		clusterID := uuid.New()
		if len(r.stats.ClusterIDs) > 0 {
			clusterID = r.stats.ClusterIDs[rnd.Intn(len(r.stats.ClusterIDs))]
		}
		sql = `SELECT id, name, power_state FROM virtual_machines WHERE cluster_id = $1 AND deleted_at IS NULL LIMIT $2;`
		args = []any{clusterID, 25}

	case QueryFolderSubtree:
		folderPath := "dc1.vm"
		if len(r.stats.FolderPaths) > 0 {
			folderPath = r.stats.FolderPaths[rnd.Intn(len(r.stats.FolderPaths))]
		}
		sql = `SELECT id, name, folder_path FROM virtual_machines WHERE folder_path <@ $1::ltree AND deleted_at IS NULL LIMIT $2;`
		args = []any{folderPath, 25}

	case QueryTag:
		cat := "Environment"
		tag := "production"
		if len(r.stats.SampleTags) > 0 {
			pair := r.stats.SampleTags[rnd.Intn(len(r.stats.SampleTags))]
			cat, tag = pair[0], pair[1]
		}
		sql = `SELECT v.id, v.name FROM virtual_machines v JOIN vm_tags t ON v.id = t.vm_id WHERE t.category_name = $1 AND t.tag_name = $2 AND v.deleted_at IS NULL LIMIT $3;`
		args = []any{cat, tag, 25}

	case QueryCustomAttr:
		attrName := "CostCenter"
		attrVal := "CC-1001"
		if len(r.stats.SampleCustomAttrs) > 0 {
			pair := r.stats.SampleCustomAttrs[rnd.Intn(len(r.stats.SampleCustomAttrs))]
			attrName, attrVal = pair[0], pair[1]
		}
		sql = `SELECT v.id, v.name FROM virtual_machines v JOIN vm_custom_attributes a ON v.id = a.vm_id WHERE a.name = $1 AND a.value = $2 AND v.deleted_at IS NULL LIMIT $3;`
		args = []any{attrName, attrVal, 25}

	case QueryFacetCounts:
		sql = `SELECT power_state, count(*) FROM virtual_machines WHERE deleted_at IS NULL GROUP BY power_state;`
		args = nil

	case QueryKeysetPagination:
		lastName := "prod-web-frontend-00001"
		lastID := uuid.New()
		if len(r.stats.SampleVMNames) > 0 {
			lastName = r.stats.SampleVMNames[rnd.Intn(len(r.stats.SampleVMNames))]
		}
		if len(r.stats.SampleVMIDs) > 0 {
			lastID = r.stats.SampleVMIDs[rnd.Intn(len(r.stats.SampleVMIDs))]
		}
		sql = `SELECT id, name FROM virtual_machines WHERE (name, id) > ($1, $2) AND deleted_at IS NULL ORDER BY name ASC, id ASC LIMIT $3;`
		args = []any{lastName, lastID, 25}

	default:
		sql = `SELECT id, name FROM virtual_machines WHERE deleted_at IS NULL LIMIT 25;`
	}

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		// Scan to drain results
	}
	return rows.Err()
}

func (r *WorkloadRunner) executeHTTPQuery(ctx context.Context, qType QueryType, rnd *rand.Rand) error {
	var reqBody []byte

	switch qType {
	case QueryExactUUID:
		vmID := uuid.New().String()
		if len(r.stats.SampleVMIDs) > 0 {
			vmID = r.stats.SampleVMIDs[rnd.Intn(len(r.stats.SampleVMIDs))].String()
		}
		payload := map[string]any{
			"filter": map[string]any{
				"op":    string(search.OpEQ),
				"field": "id",
				"value": vmID,
			},
			"limit": 25,
		}
		reqBody, _ = json.Marshal(payload)

	case QueryNamePrefix:
		payload := map[string]any{
			"filter": map[string]any{
				"op":    string(search.OpPrefix),
				"field": "name",
				"value": "prod-web-",
			},
			"limit": 25,
		}
		reqBody, _ = json.Marshal(payload)

	case QueryNameSubstring:
		payload := map[string]any{
			"filter": map[string]any{
				"op":    string(search.OpContains),
				"field": "name",
				"value": "frontend",
			},
			"limit": 25,
		}
		reqBody, _ = json.Marshal(payload)

	case QueryHostname:
		payload := map[string]any{
			"filter": map[string]any{
				"op":    string(search.OpContains),
				"field": "guest_host_name",
				"value": "corp.local",
			},
			"limit": 25,
		}
		reqBody, _ = json.Marshal(payload)

	case QueryIP:
		ip := "10.0.0.1"
		if len(r.stats.SampleIPs) > 0 {
			ip = r.stats.SampleIPs[rnd.Intn(len(r.stats.SampleIPs))]
		}
		payload := map[string]any{
			"filter": map[string]any{
				"op":    string(search.OpEQ),
				"field": "guest_ip_address",
				"value": ip,
			},
			"limit": 25,
		}
		reqBody, _ = json.Marshal(payload)

	case QueryCIDR:
		payload := map[string]any{
			"filter": map[string]any{
				"op":    string(search.OpCIDRContains),
				"field": "guest_ip_address",
				"value": "10.0.0.0/8",
			},
			"limit": 25,
		}
		reqBody, _ = json.Marshal(payload)

	case QueryTag:
		cat := "Environment"
		tag := "production"
		if len(r.stats.SampleTags) > 0 {
			pair := r.stats.SampleTags[rnd.Intn(len(r.stats.SampleTags))]
			cat, tag = pair[0], pair[1]
		}
		payload := map[string]any{
			"filter": map[string]any{
				"op":       string(search.OpHasTag),
				"category": cat,
				"tag":      tag,
			},
			"limit": 25,
		}
		reqBody, _ = json.Marshal(payload)

	case QueryCustomAttr:
		attrName := "CostCenter"
		attrVal := "CC-1001"
		if len(r.stats.SampleCustomAttrs) > 0 {
			pair := r.stats.SampleCustomAttrs[rnd.Intn(len(r.stats.SampleCustomAttrs))]
			attrName, attrVal = pair[0], pair[1]
		}
		payload := map[string]any{
			"filter": map[string]any{
				"op":    string(search.OpHasAttribute),
				"name":  attrName,
				"value": attrVal,
			},
			"limit": 25,
		}
		reqBody, _ = json.Marshal(payload)

	default:
		payload := map[string]any{
			"filter": map[string]any{
				"op":    string(search.OpContains),
				"field": "name",
				"value": "web",
			},
			"limit": 25,
		}
		reqBody, _ = json.Marshal(payload)
	}

	url := fmt.Sprintf("%s/v1/search", r.cfg.APIBaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP request failed with status: %d", resp.StatusCode)
	}
	return nil
}
