package benchmark

import (
	"context"
	"fmt"
	"math/rand"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/vlan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DatasetConfig configures synthetic performance dataset generation.
type DatasetConfig struct {
	VMCount      int
	VCenterCount int
	BatchSize    int
	Seed         int64
	CleanFirst   bool
	OnProgress   func(completedVMs int, elapsed time.Duration)
}

// DefaultDatasetConfig returns default configuration for benchmarking.
func DefaultDatasetConfig() DatasetConfig {
	return DatasetConfig{
		VMCount:      10000,
		VCenterCount: 1,
		BatchSize:    5000,
		Seed:         42,
		CleanFirst:   false,
	}
}

// DatasetStats captures execution metadata and sample keys for testing and benchmarks.
type DatasetStats struct {
	VMCount              int
	NICCount             int
	DatastoreAttachCount int
	TagCount             int
	CustomAttrCount      int
	Duration             time.Duration
	ThroughputVMsPerSec  float64

	// Sample IDs and keys for workload verification
	VCenterIDs            []uuid.UUID
	DatacenterIDs         []uuid.UUID
	ClusterIDs            []uuid.UUID
	HostIDs               []uuid.UUID
	DatastoreIDs          []uuid.UUID
	FolderPaths           []string
	SampleVMIDs           []uuid.UUID
	SampleVMNames         []string
	SampleInstanceUUIDs   []string
	SampleMoRefs          []string
	SampleIPs             []string
	SampleCIDRs           []string
	SampleMACs            []string
	SampleVLANMultiranges []string
	SampleTags            [][2]string // [category, tag]
	SampleCustomAttrs     [][2]string // [name, value]
}

// Generator produces large-scale synthetic datasets via binary PostgreSQL COPY.
type Generator struct {
	pool *pgxpool.Pool
	cfg  DatasetConfig
	rnd  *rand.Rand
}

// NewGenerator constructs a new synthetic dataset generator.
func NewGenerator(pool *pgxpool.Pool, cfg DatasetConfig) *Generator {
	if cfg.VMCount <= 0 {
		cfg.VMCount = 1000
	}
	if cfg.VCenterCount <= 0 {
		cfg.VCenterCount = 1
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 5000
	}
	if cfg.Seed == 0 {
		cfg.Seed = time.Now().UnixNano()
	}
	// #nosec G404 -- pseudo-random generator is intentionally used for reproducible synthetic benchmarks
	return &Generator{
		pool: pool,
		cfg:  cfg,
		rnd:  rand.New(rand.NewSource(cfg.Seed)),
	}
}

// TopologyMetadata holds generated topology IDs in memory for VM foreign-key mapping.
type TopologyMetadata struct {
	VCenterIDs        []uuid.UUID
	DatacenterIDs     []uuid.UUID
	ClusterIDs        []uuid.UUID
	HostIDs           []uuid.UUID
	DatastoreIDs      []uuid.UUID
	NetworkIDs        []uuid.UUID
	PortgroupIDs      []uuid.UUID
	FolderIDs         []uuid.UUID
	FolderPaths       []string
	ClusterDatacenter map[uuid.UUID]uuid.UUID
	HostCluster       map[uuid.UUID]uuid.UUID
	HostDatacenter    map[uuid.UUID]uuid.UUID
}

// CleanDatabase deletes previous benchmark test data.
func (g *Generator) CleanDatabase(ctx context.Context) error {
	queries := []string{
		"DELETE FROM vm_tags;",
		"DELETE FROM vm_custom_attributes;",
		"DELETE FROM vm_network_interfaces;",
		"DELETE FROM vm_datastores;",
		"DELETE FROM virtual_machines;",
		"DELETE FROM hosts;",
		"DELETE FROM clusters;",
		"DELETE FROM portgroups;",
		"DELETE FROM distributed_switches;",
		"DELETE FROM networks;",
		"DELETE FROM datastores;",
		"DELETE FROM inventory_folders;",
		"DELETE FROM datacenters;",
		"DELETE FROM vcenters;",
	}
	for _, q := range queries {
		if _, err := g.pool.Exec(ctx, q); err != nil {
			return fmt.Errorf("failed executing cleanup query %q: %w", q, err)
		}
	}
	return nil
}

// SeedTopology populates hierarchical topology entities (vCenters, datacenters, folders, clusters, hosts, datastores, switches, networks).
func (g *Generator) SeedTopology(ctx context.Context) (*TopologyMetadata, error) {
	meta := &TopologyMetadata{
		ClusterDatacenter: make(map[uuid.UUID]uuid.UUID),
		HostCluster:       make(map[uuid.UUID]uuid.UUID),
		HostDatacenter:    make(map[uuid.UUID]uuid.UUID),
	}

	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin topology transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for vcIdx := 0; vcIdx < g.cfg.VCenterCount; vcIdx++ {
		vcID := uuid.New()
		vcName := fmt.Sprintf("vcenter-perf-%02d.corp.local", vcIdx+1)
		var actualVCID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO vcenters (id, name, api_url, instance_uuid, version, build, product_version, api_version, credential_ref, status, enabled, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'ASSIGNED', true, NOW(), NOW())
			ON CONFLICT (api_url) DO UPDATE SET updated_at = NOW()
			RETURNING id
		`, vcID, vcName, fmt.Sprintf("https://%s/sdk", vcName), fmt.Sprintf("vc-uuid-%d", vcIdx+1), "8.0.3", "24022515", "8.0.3", "8.0.3.0", "ref-123").Scan(&actualVCID)
		if err != nil {
			return nil, fmt.Errorf("failed inserting vcenter: %w", err)
		}
		meta.VCenterIDs = append(meta.VCenterIDs, actualVCID)

		// 2 Datacenters per vCenter
		for dcIdx := 0; dcIdx < 2; dcIdx++ {
			dcID := uuid.New()
			dcMoRef := fmt.Sprintf("datacenter-%d-%d", vcIdx+1, dcIdx+1)
			dcName := fmt.Sprintf("DC_%02d_%02d", vcIdx+1, dcIdx+1)
			var actualDCID uuid.UUID
			err := tx.QueryRow(ctx, `
				INSERT INTO datacenters (id, vcenter_id, moref, name, created_at, updated_at)
				VALUES ($1, $2, $3, $4, NOW(), NOW())
				ON CONFLICT (vcenter_id, moref) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()
				RETURNING id
			`, dcID, actualVCID, dcMoRef, dcName).Scan(&actualDCID)
			if err != nil {
				return nil, fmt.Errorf("failed inserting datacenter: %w", err)
			}
			meta.DatacenterIDs = append(meta.DatacenterIDs, actualDCID)

			// Folder tree (ltree) - multiple tiers
			folderTiers := []string{
				fmt.Sprintf("dc%d.vm", dcIdx+1),
				fmt.Sprintf("dc%d.vm.workloads", dcIdx+1),
				fmt.Sprintf("dc%d.vm.workloads.tier1", dcIdx+1),
				fmt.Sprintf("dc%d.vm.workloads.tier1.prod", dcIdx+1),
				fmt.Sprintf("dc%d.vm.workloads.tier1.staging", dcIdx+1),
				fmt.Sprintf("dc%d.vm.workloads.tier2", dcIdx+1),
				fmt.Sprintf("dc%d.vm.database", dcIdx+1),
				fmt.Sprintf("dc%d.vm.database.primary", dcIdx+1),
				fmt.Sprintf("dc%d.vm.infra", dcIdx+1),
			}

			var prevFolderID *uuid.UUID
			for fIdx, fPath := range folderTiers {
				fID := uuid.New()
				fMoRef := fmt.Sprintf("group-v%d-%d-%d", vcIdx+1, dcIdx+1, fIdx+1)
				fName := fPath[strings.LastIndex(fPath, ".")+1:]
				var actualFolderID uuid.UUID
				err := tx.QueryRow(ctx, `
					INSERT INTO inventory_folders (id, vcenter_id, moref, name, folder_type, parent_id, path, created_at, updated_at)
					VALUES ($1, $2, $3, $4, 'vm', $5, $6::ltree, NOW(), NOW())
					ON CONFLICT (vcenter_id, moref) DO UPDATE SET name = EXCLUDED.name, path = EXCLUDED.path, updated_at = NOW()
					RETURNING id
				`, fID, actualVCID, fMoRef, fName, prevFolderID, fPath).Scan(&actualFolderID)
				if err != nil {
					return nil, fmt.Errorf("failed inserting folder %s: %w", fPath, err)
				}
				meta.FolderIDs = append(meta.FolderIDs, actualFolderID)
				meta.FolderPaths = append(meta.FolderPaths, fPath)
				folderCopy := actualFolderID
				prevFolderID = &folderCopy
			}

			// Distributed switch & portgroups
			dvsID := uuid.New()
			dvsMoRef := fmt.Sprintf("dvs-%d-%d", vcIdx+1, dcIdx+1)
			var actualDVSID uuid.UUID
			err = tx.QueryRow(ctx, `
				INSERT INTO distributed_switches (id, vcenter_id, datacenter_id, moref, name, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
				ON CONFLICT (vcenter_id, moref) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()
				RETURNING id
			`, dvsID, actualVCID, actualDCID, dvsMoRef, fmt.Sprintf("DVS-Core-%d-%d", vcIdx+1, dcIdx+1)).Scan(&actualDVSID)
			if err != nil {
				return nil, fmt.Errorf("failed inserting dvs: %w", err)
			}

			// Portgroups with different VLAN ranges
			pgVLANSpecs := []string{
				"100",
				"10-20, 100-200",
				"1-4094",
				"500-550",
			}
			for pgIdx, spec := range pgVLANSpecs {
				pgID := uuid.New()
				norm, _ := vlan.NormalizeSpec(spec)
				var actualPGID uuid.UUID
				err = tx.QueryRow(ctx, `
					INSERT INTO portgroups (id, vcenter_id, switch_id, moref, name, vlan_ids, created_at, updated_at)
					VALUES ($1, $2, $3, $4, $5, $6::int4multirange, NOW(), NOW())
					ON CONFLICT (vcenter_id, moref) DO UPDATE SET name = EXCLUDED.name, vlan_ids = EXCLUDED.vlan_ids, updated_at = NOW()
					RETURNING id
				`, pgID, actualVCID, actualDVSID, fmt.Sprintf("dvportgroup-%d-%d-%d", vcIdx+1, dcIdx+1, pgIdx+1), fmt.Sprintf("PG-VLAN-%s", spec), norm).Scan(&actualPGID)
				if err != nil {
					return nil, fmt.Errorf("failed inserting portgroup: %w", err)
				}
				meta.PortgroupIDs = append(meta.PortgroupIDs, actualPGID)
			}

			// Networks
			for netIdx := 0; netIdx < 2; netIdx++ {
				netID := uuid.New()
				norm, _ := vlan.NormalizeSpec("100-200")
				var actualNetID uuid.UUID
				err = tx.QueryRow(ctx, `
					INSERT INTO networks (id, vcenter_id, moref, name, vlan_ids, created_at, updated_at)
					VALUES ($1, $2, $3, $4, $5::int4multirange, NOW(), NOW())
					ON CONFLICT (vcenter_id, moref) DO UPDATE SET name = EXCLUDED.name, vlan_ids = EXCLUDED.vlan_ids, updated_at = NOW()
					RETURNING id
				`, netID, actualVCID, fmt.Sprintf("network-%d-%d-%d", vcIdx+1, dcIdx+1, netIdx+1), fmt.Sprintf("VM-Network-%d-%d", dcIdx+1, netIdx+1), norm).Scan(&actualNetID)
				if err != nil {
					return nil, fmt.Errorf("failed inserting network: %w", err)
				}
				meta.NetworkIDs = append(meta.NetworkIDs, actualNetID)
			}

			// Datastores (fast NVMe, SAN, slow spinning)
			dsTypes := []string{"VMFS", "vSAN", "NFS"}
			for dsIdx, dsType := range dsTypes {
				dsID := uuid.New()
				dsMoRef := fmt.Sprintf("datastore-%d-%d-%d", vcIdx+1, dcIdx+1, dsIdx+1)
				var actualDSID uuid.UUID
				err = tx.QueryRow(ctx, `
					INSERT INTO datastores (id, vcenter_id, datacenter_id, moref, name, type, capacity_bytes, free_bytes, accessible, created_at, updated_at)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true, NOW(), NOW())
					ON CONFLICT (vcenter_id, moref) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()
					RETURNING id
				`, dsID, actualVCID, actualDCID, dsMoRef, fmt.Sprintf("DS-%s-%d-%d", dsType, dcIdx+1, dsIdx+1), dsType, int64(10995116277760), int64(5497558138880)).Scan(&actualDSID)
				if err != nil {
					return nil, fmt.Errorf("failed inserting datastore: %w", err)
				}
				meta.DatastoreIDs = append(meta.DatastoreIDs, actualDSID)
			}

			// Clusters: 1 Large cluster + 2 Small clusters
			clusterConfigs := []struct {
				name    string
				isLarge bool
			}{
				{name: fmt.Sprintf("Cluster-Large-%d-%d", vcIdx+1, dcIdx+1), isLarge: true},
				{name: fmt.Sprintf("Cluster-Small-A-%d-%d", vcIdx+1, dcIdx+1), isLarge: false},
				{name: fmt.Sprintf("Cluster-Small-B-%d-%d", vcIdx+1, dcIdx+1), isLarge: false},
			}

			for cIdx, cCfg := range clusterConfigs {
				clusterID := uuid.New()
				cMoRef := fmt.Sprintf("domain-c%d-%d-%d", vcIdx+1, dcIdx+1, cIdx+1)
				var actualClusterID uuid.UUID
				err = tx.QueryRow(ctx, `
					INSERT INTO clusters (id, vcenter_id, datacenter_id, moref, name, created_at, updated_at)
					VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
					ON CONFLICT (vcenter_id, moref) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()
					RETURNING id
				`, clusterID, actualVCID, actualDCID, cMoRef, cCfg.name).Scan(&actualClusterID)
				if err != nil {
					return nil, fmt.Errorf("failed inserting cluster: %w", err)
				}
				meta.ClusterIDs = append(meta.ClusterIDs, actualClusterID)
				meta.ClusterDatacenter[actualClusterID] = actualDCID

				// Hosts per cluster: 16 for large, 4 for small
				hostCount := 4
				if cCfg.isLarge {
					hostCount = 16
				}
				for hIdx := 0; hIdx < hostCount; hIdx++ {
					hostID := uuid.New()
					hMoRef := fmt.Sprintf("host-%d-%d-%d-%d", vcIdx+1, dcIdx+1, cIdx+1, hIdx+1)
					hName := fmt.Sprintf("esxi-%02d-%02d-%02d.corp.local", dcIdx+1, cIdx+1, hIdx+1)
					var actualHostID uuid.UUID
					err = tx.QueryRow(ctx, `
						INSERT INTO hosts (id, vcenter_id, cluster_id, datacenter_id, moref, name, connection_state, power_state, created_at, updated_at)
						VALUES ($1, $2, $3, $4, $5, $6, 'connected', 'poweredOn', NOW(), NOW())
						ON CONFLICT (vcenter_id, moref) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()
						RETURNING id
					`, hostID, actualVCID, actualClusterID, actualDCID, hMoRef, hName).Scan(&actualHostID)
					if err != nil {
						return nil, fmt.Errorf("failed inserting host: %w", err)
					}
					meta.HostIDs = append(meta.HostIDs, actualHostID)
					meta.HostCluster[actualHostID] = actualClusterID
					meta.HostDatacenter[actualHostID] = actualDCID
				}
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed committing topology: %w", err)
	}

	return meta, nil
}

// Generate executes the full synthetic dataset generation pipeline via PostgreSQL COPY.
func (g *Generator) Generate(ctx context.Context) (*DatasetStats, error) {
	startTime := time.Now()

	if g.cfg.CleanFirst {
		if err := g.CleanDatabase(ctx); err != nil {
			return nil, err
		}
	}

	// 1. Seed topology metadata
	meta, err := g.SeedTopology(ctx)
	if err != nil {
		return nil, fmt.Errorf("topology seeding failed: %w", err)
	}

	stats := &DatasetStats{
		VMCount:               g.cfg.VMCount,
		VCenterIDs:            meta.VCenterIDs,
		DatacenterIDs:         meta.DatacenterIDs,
		ClusterIDs:            meta.ClusterIDs,
		HostIDs:               meta.HostIDs,
		DatastoreIDs:          meta.DatastoreIDs,
		FolderPaths:           meta.FolderPaths,
		SampleCIDRs:           []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "2001:db8::/32"},
		SampleVLANMultiranges: []string{"{[100,100]}", "{[10,20],[100,200]}", "{[1,4094]}"},
	}

	// Common prefix pools to stress trigram indexing with repeated and substring patterns
	namePrefixes := []string{
		"prod-web-frontend-",
		"staging-web-frontend-",
		"prod-api-gateway-",
		"db-postgres-cluster-node-",
		"cache-redis-shard-",
		"k8s-worker-node-pool-",
		"infra-monitoring-prometheus-",
		"analytics-spark-worker-",
	}

	powerStates := []string{"poweredOn", "poweredOn", "poweredOn", "poweredOff", "suspended"}
	guestOSList := []string{
		"rhel9_64Guest",
		"ubuntu64Guest",
		"windows2022srv_64Guest",
		"debian12_64Guest",
		"otherLinux64Guest",
	}
	cpuCoreOptions := []int{1, 2, 4, 8, 16, 32}
	memoryOptions := []int64{2048, 4096, 8192, 16384, 32768, 65536}

	tagCategories := []string{"Environment", "Tier", "Owner", "Compliance", "BackupPolicy"}
	tagValues := map[string][]string{
		"Environment":  {"production", "staging", "development", "disaster-recovery"},
		"Tier":         {"frontend", "backend", "database", "cache", "message-queue"},
		"Owner":        {"platform-team", "sre-team", "data-eng", "security-ops"},
		"Compliance":   {"pci-dss", "hipaa", "soc2-type2", "gdpr"},
		"BackupPolicy": {"hourly-snapshot", "daily-snapshot", "weekly-archive", "gold-sla"},
	}

	customAttrNames := []string{"CostCenter", "OwnerEmail", "ApplicationID", "PatchWindow", "TicketID"}

	totalBatches := (g.cfg.VMCount + g.cfg.BatchSize - 1) / g.cfg.BatchSize
	vmCounter := 0

	for batchIdx := 0; batchIdx < totalBatches; batchIdx++ {
		currentBatchCount := g.cfg.BatchSize
		if vmCounter+currentBatchCount > g.cfg.VMCount {
			currentBatchCount = g.cfg.VMCount - vmCounter
		}
		if currentBatchCount <= 0 {
			break
		}

		// Struct buffers for batch
		type vmRow struct {
			id           uuid.UUID
			vcID         uuid.UUID
			moref        string
			instUUID     string
			biosUUID     string
			name         string
			powerState   string
			connState    string
			cpuCores     int
			memMB        int64
			folderID     uuid.UUID
			folderPath   string
			hostID       uuid.UUID
			clusterID    uuid.UUID
			datacenterID uuid.UUID
			guestOS      string
			guestHost    string
			guestIP      any
			annotation   string
		}

		type dsAttach struct {
			vmID uuid.UUID
			dsID uuid.UUID
		}

		type nicAttach struct {
			id        uuid.UUID
			vmID      uuid.UUID
			devKey    int32
			mac       string
			ip        any
			netID     any
			pgID      any
			vlanRange any
		}

		type tagAttach struct {
			vmID     uuid.UUID
			tagID    string
			category string
			tag      string
		}

		type attrAttach struct {
			vmID  uuid.UUID
			name  string
			value string
		}

		vmRows := make([]vmRow, currentBatchCount)
		var dsList []dsAttach
		var nicList []nicAttach
		var tagList []tagAttach
		var attrList []attrAttach

		for i := 0; i < currentBatchCount; i++ {
			globalIdx := vmCounter + i + 1
			vmID := uuid.New()
			vcID := meta.VCenterIDs[g.rnd.Intn(len(meta.VCenterIDs))]
			hostID := meta.HostIDs[g.rnd.Intn(len(meta.HostIDs))]
			clusterID := meta.HostCluster[hostID]
			dcID := meta.HostDatacenter[hostID]
			folderIdx := g.rnd.Intn(len(meta.FolderIDs))
			folderID := meta.FolderIDs[folderIdx]
			folderPath := meta.FolderPaths[folderIdx]

			moref := fmt.Sprintf("vm-%d", globalIdx)
			instUUID := fmt.Sprintf("50%02x%04x-%04x-%04x-%04x-%012x",
				g.rnd.Intn(256), g.rnd.Intn(65536), g.rnd.Intn(65536), g.rnd.Intn(65536), g.rnd.Intn(65536), globalIdx)
			biosUUID := fmt.Sprintf("42%02x%04x-%04x-%04x-%04x-%012x",
				g.rnd.Intn(256), g.rnd.Intn(65536), g.rnd.Intn(65536), g.rnd.Intn(65536), g.rnd.Intn(65536), globalIdx)

			prefix := namePrefixes[g.rnd.Intn(len(namePrefixes))]
			vmName := fmt.Sprintf("%s%05d", prefix, globalIdx)
			guestHost := fmt.Sprintf("%s.corp.local", vmName)

			// 80% IPv4, 20% IPv6
			var parsedIP any
			var ipStr string
			if g.rnd.Float32() < 0.8 {
				// Pick from subnets: 10.x.y.z, 172.16.x.y, 192.168.x.y
				ipOctet2 := (globalIdx >> 16) & 0xFF
				ipOctet3 := (globalIdx >> 8) & 0xFF
				ipOctet4 := globalIdx & 0xFF
				ipStr = fmt.Sprintf("10.%d.%d.%d", ipOctet2, ipOctet3, ipOctet4)
			} else {
				ipStr = fmt.Sprintf("2001:db8::%x", globalIdx)
			}
			if addr, err := netip.ParseAddr(ipStr); err == nil {
				parsedIP = addr
			}

			powerState := powerStates[g.rnd.Intn(len(powerStates))]
			guestOS := guestOSList[g.rnd.Intn(len(guestOSList))]
			cpuCores := cpuCoreOptions[g.rnd.Intn(len(cpuCoreOptions))]
			memoryMB := memoryOptions[g.rnd.Intn(len(memoryOptions))]

			vmRows[i] = vmRow{
				id:           vmID,
				vcID:         vcID,
				moref:        moref,
				instUUID:     instUUID,
				biosUUID:     biosUUID,
				name:         vmName,
				powerState:   powerState,
				connState:    "connected",
				cpuCores:     cpuCores,
				memMB:        memoryMB,
				folderID:     folderID,
				folderPath:   folderPath,
				hostID:       hostID,
				clusterID:    clusterID,
				datacenterID: dcID,
				guestOS:      guestOS,
				guestHost:    guestHost,
				guestIP:      parsedIP,
				annotation:   fmt.Sprintf("Synthetic benchmark workload VM %d", globalIdx),
			}

			// Capture sample records for test validation
			if len(stats.SampleVMIDs) < 50 {
				stats.SampleVMIDs = append(stats.SampleVMIDs, vmID)
				stats.SampleVMNames = append(stats.SampleVMNames, vmName)
				stats.SampleInstanceUUIDs = append(stats.SampleInstanceUUIDs, instUUID)
				stats.SampleMoRefs = append(stats.SampleMoRefs, moref)
				if ipStr != "" {
					stats.SampleIPs = append(stats.SampleIPs, ipStr)
				}
			}

			// 1 to 3 distinct datastores per VM
			dsCount := 1 + g.rnd.Intn(2)
			if dsCount > len(meta.DatastoreIDs) {
				dsCount = len(meta.DatastoreIDs)
			}
			dsPerm := g.rnd.Perm(len(meta.DatastoreIDs))
			for d := 0; d < dsCount; d++ {
				dsID := meta.DatastoreIDs[dsPerm[d]]
				dsList = append(dsList, dsAttach{vmID: vmID, dsID: dsID})
			}

			// 1 to 4 NICs per VM
			nicCount := 1 + g.rnd.Intn(3)
			for n := 0; n < nicCount; n++ {
				nicID := uuid.New()
				mac := fmt.Sprintf("00:50:56:%02x:%02x:%02x", (globalIdx>>16)&0xFF, (globalIdx>>8)&0xFF, (n+1)&0xFF)
				var pgID any
				var vlanLit any
				if len(meta.PortgroupIDs) > 0 {
					pgID = meta.PortgroupIDs[g.rnd.Intn(len(meta.PortgroupIDs))]
					vlanLit = stats.SampleVLANMultiranges[g.rnd.Intn(len(stats.SampleVLANMultiranges))]
				}
				nicList = append(nicList, nicAttach{
					id:        nicID,
					vmID:      vmID,
					devKey:    int32(4000 + n),
					mac:       mac,
					ip:        parsedIP,
					pgID:      pgID,
					vlanRange: vlanLit,
				})
				if len(stats.SampleMACs) < 20 {
					stats.SampleMACs = append(stats.SampleMACs, mac)
				}
			}

			// 1 to 8 distinct tags per VM across multiple categories
			seenTagIDs := make(map[string]bool)
			numTags := 1 + g.rnd.Intn(5)
			for t := 0; t < numTags; t++ {
				cat := tagCategories[g.rnd.Intn(len(tagCategories))]
				possibleVals := tagValues[cat]
				val := possibleVals[g.rnd.Intn(len(possibleVals))]
				tagID := fmt.Sprintf("urn:vmomi:InventoryServiceTag:%s-%s", cat, val)
				if seenTagIDs[tagID] {
					continue
				}
				seenTagIDs[tagID] = true
				tagList = append(tagList, tagAttach{
					vmID:     vmID,
					tagID:    tagID,
					category: cat,
					tag:      val,
				})
				if len(stats.SampleTags) < 20 {
					stats.SampleTags = append(stats.SampleTags, [2]string{cat, val})
				}
			}

			// 1 to 4 custom attributes per VM (high cardinality)
			numAttrs := 1 + g.rnd.Intn(4)
			for a := 0; a < numAttrs && a < len(customAttrNames); a++ {
				attrName := customAttrNames[a]
				var attrVal string
				switch attrName {
				case "CostCenter":
					attrVal = fmt.Sprintf("CC-%04d", 1000+g.rnd.Intn(9000))
				case "OwnerEmail":
					attrVal = fmt.Sprintf("owner-%03d@corp.internal", g.rnd.Intn(500))
				case "ApplicationID":
					attrVal = fmt.Sprintf("APP-%04d", 100+g.rnd.Intn(900))
				case "PatchWindow":
					attrVal = fmt.Sprintf("SUN-%02d00-UTC", g.rnd.Intn(24))
				case "TicketID":
					attrVal = fmt.Sprintf("INC-%06d", 100000+g.rnd.Intn(900000))
				}
				attrList = append(attrList, attrAttach{
					vmID:  vmID,
					name:  attrName,
					value: attrVal,
				})
				if len(stats.SampleCustomAttrs) < 20 {
					stats.SampleCustomAttrs = append(stats.SampleCustomAttrs, [2]string{attrName, attrVal})
				}
			}
		}

		// Perform COPY streaming within a single transaction
		tx, err := g.pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed beginning batch transaction: %w", err)
		}

		// 1. Unlogged staging table for virtual_machines
		_, err = tx.Exec(ctx, `
			CREATE TEMP TABLE staging_vms (
				id UUID NOT NULL,
				vcenter_id UUID NOT NULL,
				moref TEXT NOT NULL,
				instance_uuid TEXT NOT NULL,
				bios_uuid TEXT,
				name TEXT NOT NULL,
				power_state TEXT NOT NULL,
				connection_state TEXT NOT NULL,
				cpu_cores INT NOT NULL,
				memory_mb BIGINT NOT NULL,
				folder_id UUID,
				folder_path TEXT,
				host_id UUID,
				cluster_id UUID,
				datacenter_id UUID,
				guest_os TEXT NOT NULL,
				guest_host_name TEXT NOT NULL,
				guest_ip_address inet,
				annotation TEXT NOT NULL
			) ON COMMIT DROP;
		`)
		if err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("failed creating staging table: %w", err)
		}

		vmCols := []string{
			"id", "vcenter_id", "moref", "instance_uuid", "bios_uuid", "name",
			"power_state", "connection_state", "cpu_cores", "memory_mb", "folder_id",
			"folder_path", "host_id", "cluster_id", "datacenter_id", "guest_os",
			"guest_host_name", "guest_ip_address", "annotation",
		}

		_, err = tx.CopyFrom(
			ctx,
			pgx.Identifier{"staging_vms"},
			vmCols,
			pgx.CopyFromSlice(len(vmRows), func(i int) ([]any, error) {
				r := vmRows[i]
				return []any{
					r.id, r.vcID, r.moref, r.instUUID, r.biosUUID, r.name,
					r.powerState, r.connState, r.cpuCores, r.memMB, r.folderID,
					r.folderPath, r.hostID, r.clusterID, r.datacenterID, r.guestOS,
					r.guestHost, r.guestIP, r.annotation,
				}, nil
			}),
		)
		if err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("CopyFrom staging_vms failed: %w", err)
		}

		// Insert into virtual_machines
		_, err = tx.Exec(ctx, `
			INSERT INTO virtual_machines (
				id, vcenter_id, moref, instance_uuid, bios_uuid, name,
				power_state, connection_state, cpu_cores, memory_mb,
				folder_id, folder_path, host_id, cluster_id, datacenter_id,
				guest_os, guest_host_name, guest_ip_address, annotation,
				created_at, updated_at, first_seen_at, last_seen_at
			)
			SELECT
				id, vcenter_id, moref, instance_uuid, bios_uuid, name,
				power_state, connection_state, cpu_cores, memory_mb,
				folder_id,
				CASE WHEN folder_path IS NOT NULL AND folder_path != '' THEN folder_path::ltree ELSE NULL END,
				host_id, cluster_id, datacenter_id,
				guest_os, guest_host_name, guest_ip_address, annotation,
				NOW(), NOW(), NOW(), NOW()
			FROM staging_vms
			ON CONFLICT (vcenter_id, moref) WHERE deleted_at IS NULL DO NOTHING;
		`)
		if err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("insert into virtual_machines failed: %w", err)
		}

		// Copy into vm_datastores
		if len(dsList) > 0 {
			_, err = tx.CopyFrom(
				ctx,
				pgx.Identifier{"vm_datastores"},
				[]string{"vm_id", "datastore_id"},
				pgx.CopyFromSlice(len(dsList), func(i int) ([]any, error) {
					return []any{dsList[i].vmID, dsList[i].dsID}, nil
				}),
			)
			if err != nil {
				_ = tx.Rollback(ctx)
				return nil, fmt.Errorf("CopyFrom vm_datastores failed: %w", err)
			}
		}

		// Copy into vm_network_interfaces via staging table to support int4multirange cast
		if len(nicList) > 0 {
			_, err = tx.Exec(ctx, `
				CREATE TEMP TABLE staging_nics (
					id UUID NOT NULL,
					vm_id UUID NOT NULL,
					device_key INT NOT NULL,
					mac_address TEXT NOT NULL,
					ip_address inet,
					portgroup_id UUID,
					vlan_ids TEXT
				) ON COMMIT DROP;
			`)
			if err != nil {
				_ = tx.Rollback(ctx)
				return nil, fmt.Errorf("failed creating staging_nics: %w", err)
			}

			_, err = tx.CopyFrom(
				ctx,
				pgx.Identifier{"staging_nics"},
				[]string{"id", "vm_id", "device_key", "mac_address", "ip_address", "portgroup_id", "vlan_ids"},
				pgx.CopyFromSlice(len(nicList), func(i int) ([]any, error) {
					n := nicList[i]
					return []any{n.id, n.vmID, n.devKey, n.mac, n.ip, n.pgID, n.vlanRange}, nil
				}),
			)
			if err != nil {
				_ = tx.Rollback(ctx)
				return nil, fmt.Errorf("CopyFrom staging_nics failed: %w", err)
			}

			_, err = tx.Exec(ctx, `
				INSERT INTO vm_network_interfaces (id, vm_id, device_key, mac_address, ip_address, portgroup_id, vlan_ids, created_at, updated_at)
				SELECT id, vm_id, device_key, mac_address, ip_address, portgroup_id,
					CASE WHEN vlan_ids IS NOT NULL AND vlan_ids != '' THEN vlan_ids::int4multirange ELSE NULL END,
					NOW(), NOW()
				FROM staging_nics;
			`)
			if err != nil {
				_ = tx.Rollback(ctx)
				return nil, fmt.Errorf("insert into vm_network_interfaces failed: %w", err)
			}
		}

		// Copy into vm_tags
		if len(tagList) > 0 {
			_, err = tx.CopyFrom(
				ctx,
				pgx.Identifier{"vm_tags"},
				[]string{"vm_id", "tag_id", "category_name", "tag_name"},
				pgx.CopyFromSlice(len(tagList), func(i int) ([]any, error) {
					t := tagList[i]
					return []any{t.vmID, t.tagID, t.category, t.tag}, nil
				}),
			)
			if err != nil {
				_ = tx.Rollback(ctx)
				return nil, fmt.Errorf("CopyFrom vm_tags failed: %w", err)
			}
		}

		// Copy into vm_custom_attributes
		if len(attrList) > 0 {
			_, err = tx.CopyFrom(
				ctx,
				pgx.Identifier{"vm_custom_attributes"},
				[]string{"vm_id", "name", "value"},
				pgx.CopyFromSlice(len(attrList), func(i int) ([]any, error) {
					a := attrList[i]
					return []any{a.vmID, a.name, a.value}, nil
				}),
			)
			if err != nil {
				_ = tx.Rollback(ctx)
				return nil, fmt.Errorf("CopyFrom vm_custom_attributes failed: %w", err)
			}
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("failed committing batch %d: %w", batchIdx, err)
		}

		vmCounter += currentBatchCount
		stats.NICCount += len(nicList)
		stats.DatastoreAttachCount += len(dsList)
		stats.TagCount += len(tagList)
		stats.CustomAttrCount += len(attrList)

		if g.cfg.OnProgress != nil {
			g.cfg.OnProgress(vmCounter, time.Since(startTime))
		}
	}

	stats.Duration = time.Since(startTime)
	if stats.Duration > 0 {
		stats.ThroughputVMsPerSec = float64(stats.VMCount) / stats.Duration.Seconds()
	}

	return stats, nil
}
