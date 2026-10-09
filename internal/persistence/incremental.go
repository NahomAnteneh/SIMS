package persistence

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/vlan"
	"github.com/jackc/pgx/v5"
)

var (
	// ErrVMNotFound is returned when attempting an incremental update on a non-existent or soft-deleted VM.
	ErrVMNotFound = errors.New("vm_not_found")
)

// ApplyVMIncrementalUpdate applies a targeted field-mask update to an active VM record.
// Untouched columns, tags, custom attributes, and network interfaces remain intact.
func (p *BulkPersister) ApplyVMIncrementalUpdate(ctx context.Context, vcenterID, moref string, mask collector.VMFieldMask) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Locate active VM ID
	var vmID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT id FROM virtual_machines
		WHERE vcenter_id = $1 AND moref = $2 AND deleted_at IS NULL
	`, vcenterID, moref).Scan(&vmID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVMNotFound
		}
		return fmt.Errorf("failed to locate VM for incremental update: %w", err)
	}

	// 2. Build dynamic UPDATE for virtual_machines if any direct columns are modified
	setClauses := []string{"updated_at = NOW()"}
	args := []any{vmID}
	argIdx := 2

	if mask.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *mask.Name)
		argIdx++
	}
	if mask.PowerState != nil {
		setClauses = append(setClauses, fmt.Sprintf("power_state = $%d", argIdx))
		args = append(args, *mask.PowerState)
		argIdx++
	}
	if mask.ConnectionState != nil {
		setClauses = append(setClauses, fmt.Sprintf("connection_state = $%d", argIdx))
		args = append(args, *mask.ConnectionState)
		argIdx++
	}
	if mask.CPUCores != nil {
		setClauses = append(setClauses, fmt.Sprintf("cpu_cores = $%d", argIdx))
		args = append(args, *mask.CPUCores)
		argIdx++
	}
	if mask.MemoryMB != nil {
		setClauses = append(setClauses, fmt.Sprintf("memory_mb = $%d", argIdx))
		args = append(args, *mask.MemoryMB)
		argIdx++
	}
	if mask.GuestOS != nil {
		setClauses = append(setClauses, fmt.Sprintf("guest_os = $%d", argIdx))
		args = append(args, *mask.GuestOS)
		argIdx++
	}
	if mask.GuestHostName != nil {
		setClauses = append(setClauses, fmt.Sprintf("guest_host_name = $%d", argIdx))
		args = append(args, *mask.GuestHostName)
		argIdx++
	}
	if mask.GuestIPAddress != nil {
		ip := strings.TrimSpace(*mask.GuestIPAddress)
		if ip != "" {
			if _, parseErr := netip.ParseAddr(ip); parseErr == nil {
				setClauses = append(setClauses, fmt.Sprintf("guest_ip_address = $%d", argIdx))
				args = append(args, ip)
				argIdx++
			}
		} else {
			setClauses = append(setClauses, fmt.Sprintf("guest_ip_address = $%d", argIdx))
			args = append(args, nil)
			argIdx++
		}
	}
	if mask.Annotation != nil {
		setClauses = append(setClauses, fmt.Sprintf("annotation = $%d", argIdx))
		args = append(args, *mask.Annotation)
		argIdx++
	}
	if mask.FolderID != nil {
		setClauses = append(setClauses, fmt.Sprintf("folder_id = $%d::uuid", argIdx))
		args = append(args, *mask.FolderID)
		argIdx++
	}
	if mask.FolderPath != nil {
		setClauses = append(setClauses, fmt.Sprintf("folder_path = $%d::ltree", argIdx))
		args = append(args, *mask.FolderPath)
		argIdx++
	}
	if mask.HostID != nil {
		setClauses = append(setClauses, fmt.Sprintf("host_id = $%d::uuid", argIdx))
		args = append(args, *mask.HostID)
		argIdx++
	}
	if mask.ClusterID != nil {
		setClauses = append(setClauses, fmt.Sprintf("cluster_id = $%d::uuid", argIdx))
		args = append(args, *mask.ClusterID)
		argIdx++
	}
	if mask.DatacenterID != nil {
		setClauses = append(setClauses, fmt.Sprintf("datacenter_id = $%d::uuid", argIdx))
		args = append(args, *mask.DatacenterID)
		argIdx++
	}
	if mask.CommittedStorageBytes != nil {
		setClauses = append(setClauses, fmt.Sprintf("committed_storage_bytes = $%d", argIdx))
		args = append(args, *mask.CommittedStorageBytes)
		argIdx++
	}
	if mask.ProvisionedStorageBytes != nil {
		setClauses = append(setClauses, fmt.Sprintf("provisioned_storage_bytes = $%d", argIdx))
		args = append(args, *mask.ProvisionedStorageBytes)
		argIdx++
	}
	if mask.LastSeenAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_seen_at = $%d", argIdx))
		args = append(args, *mask.LastSeenAt)
	}

	if len(setClauses) > 1 {
		updateQuery := fmt.Sprintf("UPDATE virtual_machines SET %s WHERE id = $1", strings.Join(setClauses, ", "))
		if _, err := tx.Exec(ctx, updateQuery, args...); err != nil {
			return fmt.Errorf("failed to execute incremental VM update: %w", err)
		}
	}

	// 3. Update Custom Attributes if specified (without touching other fields; replace-set per VM)
	if mask.CustomAttributes != nil {
		if _, err := tx.Exec(ctx, "DELETE FROM vm_custom_attributes WHERE vm_id = $1", vmID); err != nil {
			return fmt.Errorf("failed to delete previous custom attributes: %w", err)
		}
		for _, ca := range mask.CustomAttributes {
			_, err := tx.Exec(ctx, `
				INSERT INTO vm_custom_attributes (vm_id, name, value, created_at)
				VALUES ($1, $2, $3, NOW())
			`, vmID, ca.Name, ca.Value)
			if err != nil {
				return fmt.Errorf("failed to insert custom attribute %q: %w", ca.Name, err)
			}
		}
	}

	// 4. Update NICs atomically if specified (S2.11)
	if mask.NetworkInterfaces != nil {
		// Delete existing NIC rows
		if _, err := tx.Exec(ctx, "DELETE FROM vm_network_interfaces WHERE vm_id = $1", vmID); err != nil {
			return fmt.Errorf("failed to delete previous NIC rows: %w", err)
		}
		// Insert new NIC rows
		for _, nic := range mask.NetworkInterfaces {
			var ipParam any = nil
			if nic.IPAddress != "" {
				if _, parseErr := netip.ParseAddr(nic.IPAddress); parseErr == nil {
					ipParam = nic.IPAddress
				}
			}
			var netID any = nil
			if nic.NetworkID != "" {
				netID = nic.NetworkID
			}
			var pgID any = nil
			if nic.PortgroupID != "" {
				pgID = nic.PortgroupID
			}
			var vlanParam any = nil
			if nic.VLANSpec != "" {
				if norm, normErr := vlan.NormalizeSpec(nic.VLANSpec); normErr == nil {
					vlanParam = norm
				}
			}

			_, err := tx.Exec(ctx, `
				INSERT INTO vm_network_interfaces (
					vm_id, device_key, mac_address, ip_address, network_id, portgroup_id, vlan_ids, created_at, updated_at
				) VALUES (
					$1, $2, $3, $4, $5::uuid, $6::uuid, $7::int4multirange, NOW(), NOW()
				)
			`, vmID, nic.DeviceKey, nic.MacAddress, ipParam, netID, pgID, vlanParam)
			if err != nil {
				return fmt.Errorf("failed to insert NIC row (key=%d, mac=%s): %w", nic.DeviceKey, nic.MacAddress, err)
			}
		}
	}

	return tx.Commit(ctx)
}

// ApplyVMLeave marks a VM as deleted by setting deleted_at = NOW().
func (p *BulkPersister) ApplyVMLeave(ctx context.Context, vcenterID, moref string) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE virtual_machines
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE vcenter_id = $1 AND moref = $2 AND deleted_at IS NULL
	`, vcenterID, moref)
	if err != nil {
		return fmt.Errorf("failed to mark VM leave: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrVMNotFound
	}
	return nil
}
