package persistence

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vlan"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// WriterConfig defines tuning parameters for the bulk persistence pipeline.
type WriterConfig struct {
	FlushInterval time.Duration // Default: 250ms
	FlushSize     int           // Default: 500 records
}

// BulkPersister executes high-throughput bulk persistence using COPY + staging table + set-based merge.
type BulkPersister struct {
	pool    *pgxpool.Pool
	cfg     WriterConfig
	metrics *telemetry.Metrics
}

// NewBulkPersister creates a new BulkPersister instance.
func NewBulkPersister(pool *pgxpool.Pool, cfg WriterConfig, metrics *telemetry.Metrics) *BulkPersister {
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 250 * time.Millisecond
	}
	if cfg.FlushSize <= 0 {
		cfg.FlushSize = 500
	}
	return &BulkPersister{
		pool:    pool,
		cfg:     cfg,
		metrics: metrics,
	}
}

// PersistTopology persists or updates all topology entities in a single transaction,
// populating internal database UUIDs back into the TopologySnapshot.
func (p *BulkPersister) PersistTopology(ctx context.Context, snap *collector.TopologySnapshot) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin topology transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	vcenterID := snap.VCenterID

	// 0. Datacenters
	for moref, dc := range snap.Datacenters {
		var id string
		err := tx.QueryRow(ctx, `
			INSERT INTO datacenters (vcenter_id, moref, name, updated_at)
			VALUES ($1::uuid, $2, $3, NOW())
			ON CONFLICT (vcenter_id, moref) DO UPDATE
			SET name = EXCLUDED.name, updated_at = NOW()
			RETURNING id::text
		`, vcenterID, moref, dc.Name).Scan(&id)
		if err != nil {
			return fmt.Errorf("failed to upsert datacenter %s: %w", moref, err)
		}
		snap.DatacenterIDByMoRef[moref] = id
	}

	// Resolve Datacenter IDs for all nodes in the topology snapshot
	for nodeMoref := range snap.ByMoRef {
		dcMoref := snap.ResolveDatacenterMoRef(nodeMoref)
		if dcMoref != "" {
			if dcID, ok := snap.DatacenterIDByMoRef[dcMoref]; ok && dcID != "" {
				snap.DatacenterIDByMoRef[nodeMoref] = dcID
			}
		}
	}

	// 1. Folders: insert parent-less first, then children
	for moref, f := range snap.Folders {
		var id string
		path := f.Path
		if path == "" {
			path = "root"
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO inventory_folders (vcenter_id, moref, name, path, updated_at)
			VALUES ($1::uuid, $2, $3, $4::ltree, NOW())
			ON CONFLICT (vcenter_id, moref) DO UPDATE
			SET name = EXCLUDED.name, path = EXCLUDED.path, updated_at = NOW()
			RETURNING id::text
		`, vcenterID, moref, f.Name, path).Scan(&id)
		if err != nil {
			return fmt.Errorf("failed to upsert folder %s: %w", moref, err)
		}
		snap.FolderIDByMoRef[moref] = id
		snap.FolderPathByMoRef[moref] = path
	}

	// 2. Datastores
	for moref, ds := range snap.Datastores {
		var id string
		err := tx.QueryRow(ctx, `
			INSERT INTO datastores (vcenter_id, moref, name, updated_at)
			VALUES ($1::uuid, $2, $3, NOW())
			ON CONFLICT (vcenter_id, moref) DO UPDATE
			SET name = EXCLUDED.name, updated_at = NOW()
			RETURNING id::text
		`, vcenterID, moref, ds.Name).Scan(&id)
		if err != nil {
			return fmt.Errorf("failed to upsert datastore %s: %w", moref, err)
		}
		snap.DatastoreIDByMoRef[moref] = id
	}

	// 3. Distributed Switches
	for moref, node := range snap.ByMoRef {
		if strings.HasPrefix(moref, "dvs-") {
			var id string
			err := tx.QueryRow(ctx, `
				INSERT INTO distributed_switches (vcenter_id, moref, name, updated_at)
				VALUES ($1::uuid, $2, $3, NOW())
				ON CONFLICT (vcenter_id, moref) DO UPDATE
				SET name = EXCLUDED.name, updated_at = NOW()
				RETURNING id::text
			`, vcenterID, moref, node.Name).Scan(&id)
			if err != nil {
				return fmt.Errorf("failed to upsert switch %s: %w", moref, err)
			}
		}
	}

	// 4. Portgroups
	for moref, pg := range snap.Portgroups {
		var id string
		vlanLit := "{}"
		if pg.VLANSpec != "" {
			if norm, err := vlan.NormalizeSpec(pg.VLANSpec); err == nil {
				vlanLit = norm
			}
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO portgroups (vcenter_id, moref, name, vlan_ids, updated_at)
			VALUES ($1::uuid, $2, $3, $4::int4multirange, NOW())
			ON CONFLICT (vcenter_id, moref) DO UPDATE
			SET name = EXCLUDED.name, vlan_ids = EXCLUDED.vlan_ids, updated_at = NOW()
			RETURNING id::text
		`, vcenterID, moref, pg.Name, vlanLit).Scan(&id)
		if err != nil {
			return fmt.Errorf("failed to upsert portgroup %s: %w", moref, err)
		}
		snap.PortgroupIDByMoRef[moref] = id
	}

	// 5. Networks
	for moref, netNode := range snap.Networks {
		var id string
		vlanLit := "{}"
		if netNode.VLANSpec != "" {
			if norm, err := vlan.NormalizeSpec(netNode.VLANSpec); err == nil {
				vlanLit = norm
			}
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO networks (vcenter_id, moref, name, vlan_ids, updated_at)
			VALUES ($1::uuid, $2, $3, $4::int4multirange, NOW())
			ON CONFLICT (vcenter_id, moref) DO UPDATE
			SET name = EXCLUDED.name, vlan_ids = EXCLUDED.vlan_ids, updated_at = NOW()
			RETURNING id::text
		`, vcenterID, moref, netNode.Name, vlanLit).Scan(&id)
		if err != nil {
			return fmt.Errorf("failed to upsert network %s: %w", moref, err)
		}
		snap.NetworkIDByMoRef[moref] = id
	}

	// 6. Clusters
	for moref, cl := range snap.Clusters {
		var id string
		var dcUUID *string
		if dcID, ok := snap.DatacenterIDByMoRef[moref]; ok && dcID != "" {
			dcUUID = &dcID
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO clusters (vcenter_id, datacenter_id, moref, name, updated_at)
			VALUES ($1::uuid, $2::uuid, $3, $4, NOW())
			ON CONFLICT (vcenter_id, moref) DO UPDATE
			SET datacenter_id = EXCLUDED.datacenter_id, name = EXCLUDED.name, updated_at = NOW()
			RETURNING id::text
		`, vcenterID, dcUUID, moref, cl.Name).Scan(&id)
		if err != nil {
			return fmt.Errorf("failed to upsert cluster %s: %w", moref, err)
		}
		snap.ClusterIDByMoRef[moref] = id
	}

	// 7. Hosts
	for moref, h := range snap.Hosts {
		var id string
		var clusterUUID *string
		if clusterID, ok := snap.ClusterIDByMoRef[h.ParentID]; ok && clusterID != "" {
			clusterUUID = &clusterID
		}
		var dcUUID *string
		if dcID, ok := snap.DatacenterIDByMoRef[moref]; ok && dcID != "" {
			dcUUID = &dcID
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO hosts (vcenter_id, cluster_id, datacenter_id, moref, name, updated_at)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, NOW())
			ON CONFLICT (vcenter_id, moref) DO UPDATE
			SET cluster_id = EXCLUDED.cluster_id, datacenter_id = EXCLUDED.datacenter_id, name = EXCLUDED.name, updated_at = NOW()
			RETURNING id::text
		`, vcenterID, clusterUUID, dcUUID, moref, h.Name).Scan(&id)
		if err != nil {
			return fmt.Errorf("failed to upsert host %s: %w", moref, err)
		}
		snap.HostIDByMoRef[moref] = id
		if clusterUUID != nil {
			snap.ClusterIDByMoRef[moref] = *clusterUUID
		}
	}

	return tx.Commit(ctx)
}

// PersistVMBatch performs batch compaction, staging COPY, and set-based merge in a single transaction.
func (p *BulkPersister) PersistVMBatch(ctx context.Context, records []domain.VMRecord) (int, error) {
	if len(records) == 0 {
		return 0, nil
	}

	flushStart := time.Now()
	rawCount := len(records)

	// 1. In-batch compaction
	compacted := collector.CompactBatch(records)
	compactedCount := len(compacted)

	vcenterID := records[0].VCenterID
	if p.metrics != nil && rawCount > 0 {
		ratio := float64(rawCount-compactedCount) / float64(rawCount)
		p.metrics.CompactionRatio.WithLabelValues(vcenterID).Observe(ratio)
		p.metrics.BatchSize.WithLabelValues(vcenterID).Observe(float64(compactedCount))
	}

	// 2. Begin single database transaction
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to begin persistence transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 3. Create unlogged/temporary staging table without foreign keys or indexes.
	// folder_path is typed as TEXT to allow native string streaming via CopyFrom without ltree binary framing.
	_, err = tx.Exec(ctx, `
		CREATE TEMP TABLE staging_vms (
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
			annotation TEXT NOT NULL,
			deleted_at TIMESTAMPTZ
		) ON COMMIT DROP;
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to create staging table: %w", err)
	}

	columns := []string{
		"vcenter_id",
		"moref",
		"instance_uuid",
		"bios_uuid",
		"name",
		"power_state",
		"connection_state",
		"cpu_cores",
		"memory_mb",
		"folder_id",
		"folder_path",
		"host_id",
		"cluster_id",
		"datacenter_id",
		"guest_os",
		"guest_host_name",
		"guest_ip_address",
		"annotation",
		"deleted_at",
	}

	// 4. COPY FROM STDIN into staging table
	copyStart := time.Now()
	_, err = tx.CopyFrom(
		ctx,
		pgx.Identifier{"staging_vms"},
		columns,
		pgx.CopyFromSlice(len(compacted), func(i int) ([]any, error) {
			r := compacted[i]

			var biosUUID, folderID, hostID, clusterID, datacenterID any
			if r.BiosUUID != "" {
				biosUUID = r.BiosUUID
			}
			if r.FolderID != "" {
				folderID = r.FolderID
			}
			if r.HostID != "" {
				hostID = r.HostID
			}
			if r.ClusterID != "" {
				clusterID = r.ClusterID
			}
			if r.DatacenterID != "" {
				datacenterID = r.DatacenterID
			}

			var guestIP any
			if r.GuestIPAddress != "" {
				addrStr := strings.TrimSpace(r.GuestIPAddress)
				if idx := strings.Index(addrStr, "%"); idx != -1 {
					addrStr = addrStr[:idx]
				}
				if parsed, err := netip.ParseAddr(addrStr); err == nil {
					guestIP = parsed.WithZone("")
				}
			}

			var deletedAt any
			if r.DeletedAt != nil {
				deletedAt = *r.DeletedAt
			}

			var folderPath any
			if r.FolderPath != "" {
				folderPath = r.FolderPath
			}

			instUUID := strings.TrimSpace(r.InstanceUUID)
			if instUUID == "" {
				instUUID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(r.VCenterID+":"+r.MoRef)).String()
			}

			return []any{
				r.VCenterID,
				r.MoRef,
				instUUID,
				biosUUID,
				r.Name,
				r.PowerState,
				r.ConnectionState,
				r.CPUCores,
				r.MemoryMB,
				folderID,
				folderPath,
				hostID,
				clusterID,
				datacenterID,
				r.GuestOS,
				r.GuestHostName,
				guestIP,
				r.Annotation,
				deletedAt,
			}, nil
		}),
	)
	if err != nil {
		return 0, fmt.Errorf("binary CopyFrom to staging failed: %w", err)
	}

	copyDuration := time.Since(copyStart)
	if p.metrics != nil {
		p.metrics.CopyDuration.WithLabelValues(vcenterID).Observe(copyDuration.Seconds())
	}

	// 5. Set-based upsert merge into virtual_machines
	mergeStart := time.Now()

	// 5a. Soft-delete any existing active VMs marked deleted in staging
	_, err = tx.Exec(ctx, `
		UPDATE virtual_machines v
		SET deleted_at = s.deleted_at, updated_at = NOW()
		FROM staging_vms s
		WHERE v.vcenter_id = s.vcenter_id
		  AND v.moref = s.moref
		  AND v.deleted_at IS NULL
		  AND s.deleted_at IS NOT NULL;
	`)
	if err != nil {
		return 0, fmt.Errorf("soft-delete merge failed: %w", err)
	}

	// 5b. Upsert active VMs and retrieve their synthetic UUIDs
	rows, err := tx.Query(ctx, `
		INSERT INTO virtual_machines (
			vcenter_id, moref, instance_uuid, bios_uuid, name, power_state,
			connection_state, cpu_cores, memory_mb, folder_id, folder_path,
			host_id, cluster_id, datacenter_id, guest_os, guest_host_name,
			guest_ip_address, annotation, deleted_at, updated_at
		)
		SELECT DISTINCT ON (vcenter_id, moref)
			vcenter_id, moref, instance_uuid, bios_uuid, name, power_state,
			connection_state, cpu_cores, memory_mb, folder_id,
			CASE WHEN folder_path IS NOT NULL AND folder_path != '' THEN folder_path::ltree ELSE NULL END,
			host_id, cluster_id, datacenter_id, guest_os, guest_host_name,
			guest_ip_address, annotation, deleted_at, NOW()
		FROM staging_vms
		WHERE deleted_at IS NULL
		ORDER BY vcenter_id, moref
		ON CONFLICT (vcenter_id, moref) WHERE deleted_at IS NULL
		DO UPDATE SET
			instance_uuid = EXCLUDED.instance_uuid,
			bios_uuid = EXCLUDED.bios_uuid,
			name = EXCLUDED.name,
			power_state = EXCLUDED.power_state,
			connection_state = EXCLUDED.connection_state,
			cpu_cores = EXCLUDED.cpu_cores,
			memory_mb = EXCLUDED.memory_mb,
			folder_id = EXCLUDED.folder_id,
			folder_path = EXCLUDED.folder_path,
			host_id = EXCLUDED.host_id,
			cluster_id = EXCLUDED.cluster_id,
			datacenter_id = EXCLUDED.datacenter_id,
			guest_os = EXCLUDED.guest_os,
			guest_host_name = EXCLUDED.guest_host_name,
			guest_ip_address = EXCLUDED.guest_ip_address,
			annotation = EXCLUDED.annotation,
			deleted_at = EXCLUDED.deleted_at,
			updated_at = NOW()
		RETURNING id::text, moref, instance_uuid;
	`)
	if err != nil {
		return 0, fmt.Errorf("set-based upsert merge failed: %w", err)
	}

	morefToVMID := make(map[string]string)
	var affectedVMIDs []string
	for rows.Next() {
		var id, moref, instUUID string
		if err := rows.Scan(&id, &moref, &instUUID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("failed to scan merged VM id: %w", err)
		}
		morefToVMID[moref] = id
		affectedVMIDs = append(affectedVMIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("set-based upsert merge iteration failed: %w", err)
	}
	rows.Close()

	// 6. Bulk attach Datastores & Network Interfaces for affected VMs
	if len(affectedVMIDs) > 0 {
		// Clean stale child associations for the affected VMs
		_, err = tx.Exec(ctx, `DELETE FROM vm_datastores WHERE vm_id = ANY($1::uuid[])`, affectedVMIDs)
		if err != nil {
			return 0, fmt.Errorf("failed to clear stale datastore attachments: %w", err)
		}

		_, err = tx.Exec(ctx, `DELETE FROM vm_network_interfaces WHERE vm_id = ANY($1::uuid[])`, affectedVMIDs)
		if err != nil {
			return 0, fmt.Errorf("failed to clear stale NICs: %w", err)
		}

		_, err = tx.Exec(ctx, `DELETE FROM vm_tags WHERE vm_id = ANY($1::uuid[])`, affectedVMIDs)
		if err != nil {
			return 0, fmt.Errorf("failed to clear stale tags: %w", err)
		}

		_, err = tx.Exec(ctx, `DELETE FROM vm_custom_attributes WHERE vm_id = ANY($1::uuid[])`, affectedVMIDs)
		if err != nil {
			return 0, fmt.Errorf("failed to clear stale custom attributes: %w", err)
		}

		// Prepare bulk datastore attachments
		type dsAttach struct {
			vmID string
			dsID string
		}
		var dsList []dsAttach

		type nicAttach struct {
			vmID      string
			devKey    int32
			mac       string
			ip        any
			netID     any
			pgID      any
			vlanRange any
		}
		var nicList []nicAttach

		type tagAttach struct {
			vmID         string
			tagID        string
			categoryName string
			tagName      string
		}
		var tagList []tagAttach

		type customAttrAttach struct {
			vmID  string
			name  string
			value string
		}
		var attrList []customAttrAttach

		for _, r := range compacted {
			vmID, ok := morefToVMID[r.MoRef]
			if !ok {
				continue
			}

			for _, dsID := range r.DatastoreIDs {
				if dsID != "" {
					dsList = append(dsList, dsAttach{vmID: vmID, dsID: dsID})
				}
			}

			for _, nic := range r.NetworkInterfaces {
				var parsedIP any
				if nic.IPAddress != "" {
					addrStr := strings.TrimSpace(nic.IPAddress)
					if idx := strings.Index(addrStr, "%"); idx != -1 {
						addrStr = addrStr[:idx]
					}
					if ip, err := netip.ParseAddr(addrStr); err == nil {
						parsedIP = ip.WithZone("")
					}
				}
				var netID, pgID any
				if nic.NetworkID != "" {
					netID = nic.NetworkID
				}
				if nic.PortgroupID != "" {
					pgID = nic.PortgroupID
				}
				var vlanLit any
				if nic.VLANSpec != "" {
					if norm, err := vlan.NormalizeSpec(nic.VLANSpec); err == nil {
						vlanLit = norm
					}
				}

				nicList = append(nicList, nicAttach{
					vmID:      vmID,
					devKey:    nic.DeviceKey,
					mac:       nic.MacAddress,
					ip:        parsedIP,
					netID:     netID,
					pgID:      pgID,
					vlanRange: vlanLit,
				})
			}

			for _, tag := range r.Tags {
				if tag.TagID != "" && tag.TagName != "" {
					tagList = append(tagList, tagAttach{
						vmID:         vmID,
						tagID:        tag.TagID,
						categoryName: tag.CategoryName,
						tagName:      tag.TagName,
					})
				}
			}

			for _, attr := range r.CustomAttributes {
				if attr.Name != "" {
					attrList = append(attrList, customAttrAttach{
						vmID:  vmID,
						name:  attr.Name,
						value: attr.Value,
					})
				}
			}
		}

		// COPY datastores
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
				return 0, fmt.Errorf("bulk copy vm_datastores failed: %w", err)
			}
		}

		// COPY network interfaces
		if len(nicList) > 0 {
			_, err = tx.CopyFrom(
				ctx,
				pgx.Identifier{"vm_network_interfaces"},
				[]string{"vm_id", "device_key", "mac_address", "ip_address", "network_id", "portgroup_id", "vlan_ids"},
				pgx.CopyFromSlice(len(nicList), func(i int) ([]any, error) {
					n := nicList[i]
					return []any{n.vmID, n.devKey, n.mac, n.ip, n.netID, n.pgID, n.vlanRange}, nil
				}),
			)
			if err != nil {
				return 0, fmt.Errorf("bulk copy vm_network_interfaces failed: %w", err)
			}
		}

		// COPY tags
		if len(tagList) > 0 {
			_, err = tx.CopyFrom(
				ctx,
				pgx.Identifier{"vm_tags"},
				[]string{"vm_id", "tag_id", "category_name", "tag_name"},
				pgx.CopyFromSlice(len(tagList), func(i int) ([]any, error) {
					t := tagList[i]
					return []any{t.vmID, t.tagID, t.categoryName, t.tagName}, nil
				}),
			)
			if err != nil {
				return 0, fmt.Errorf("bulk copy vm_tags failed: %w", err)
			}
			if p.metrics != nil {
				p.metrics.TagsPersisted.WithLabelValues(vcenterID).Add(float64(len(tagList)))
			}
		}

		// COPY custom attributes
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
				return 0, fmt.Errorf("bulk copy vm_custom_attributes failed: %w", err)
			}
			if p.metrics != nil {
				p.metrics.CustomAttributesPersisted.WithLabelValues(vcenterID).Add(float64(len(attrList)))
			}
		}
	}

	mergeDuration := time.Since(mergeStart)
	if p.metrics != nil {
		p.metrics.MergeDuration.WithLabelValues(vcenterID).Observe(mergeDuration.Seconds())
	}

	// 7. Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("transaction commit failed: %w", err)
	}

	flushDuration := time.Since(flushStart)
	if p.metrics != nil {
		p.metrics.FlushDuration.WithLabelValues(vcenterID).Observe(flushDuration.Seconds())
	}

	return compactedCount, nil
}

// StreamFlusher buffers incoming VMRecords and flushes them when either FlushSize
// or FlushInterval is reached.
type StreamFlusher struct {
	persister *BulkPersister
	buffer    []domain.VMRecord
	mu        sync.Mutex
	stopCh    chan struct{}
	doneCh    chan struct{}
}

// NewStreamFlusher constructs a timed buffer flusher.
func NewStreamFlusher(persister *BulkPersister) *StreamFlusher {
	sf := &StreamFlusher{
		persister: persister,
		buffer:    make([]domain.VMRecord, 0, persister.cfg.FlushSize),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
	go sf.runTicker()
	return sf
}

func (sf *StreamFlusher) runTicker() {
	ticker := time.NewTicker(sf.persister.cfg.FlushInterval)
	defer ticker.Stop()
	defer close(sf.doneCh)

	for {
		select {
		case <-sf.stopCh:
			_ = sf.Flush(context.Background())
			return
		case <-ticker.C:
			_ = sf.Flush(context.Background())
		}
	}
}

// Push adds records to the buffer, triggering a flush if FlushSize is met.
func (sf *StreamFlusher) Push(ctx context.Context, records []domain.VMRecord) error {
	sf.mu.Lock()
	sf.buffer = append(sf.buffer, records...)
	shouldFlush := len(sf.buffer) >= sf.persister.cfg.FlushSize
	sf.mu.Unlock()

	if shouldFlush {
		return sf.Flush(ctx)
	}
	return nil
}

// Flush flushes any currently buffered records to PostgreSQL.
func (sf *StreamFlusher) Flush(ctx context.Context) error {
	sf.mu.Lock()
	if len(sf.buffer) == 0 {
		sf.mu.Unlock()
		return nil
	}
	toFlush := sf.buffer
	sf.buffer = make([]domain.VMRecord, 0, sf.persister.cfg.FlushSize)
	sf.mu.Unlock()

	_, err := sf.persister.PersistVMBatch(ctx, toFlush)
	return err
}

// Close stops the background ticker and performs a final flush.
func (sf *StreamFlusher) Close() error {
	close(sf.stopCh)
	<-sf.doneCh
	return nil
}
