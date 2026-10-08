package collector

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/vmware/govmomi/vim25/types"
)

// VMFieldMask defines optional property updates for incremental modification.
// Only non-nil fields are updated in the database, guaranteeing that single-field
// modifications never overwrite or zero out untouched columns.
type VMFieldMask struct {
	InstanceUUID    *string
	BiosUUID        *string
	Name            *string
	PowerState      *string
	ConnectionState *string
	CPUCores        *int32
	MemoryMB        *int64
	GuestOS         *string
	GuestHostName   *string
	GuestIPAddress  *string
	Annotation      *string
	FolderID        *string
	FolderPath      *string
	HostID          *string
	ClusterID       *string
	DatacenterID    *string
	LastSeenAt      *time.Time

	// Storage metrics
	CommittedStorageBytes   *int64
	ProvisionedStorageBytes *int64

	// Sub-resources
	CustomAttributes  []domain.CustomAttributeRecord
	NetworkInterfaces []domain.NetworkInterfaceRecord
}

// VMPersister defines the database contract required by IncrementalApplier.
type VMPersister interface {
	PersistVMBatch(ctx context.Context, batch []domain.VMRecord) (int, error)
	ApplyVMIncrementalUpdate(ctx context.Context, vcenterID, moref string, mask VMFieldMask) error
	ApplyVMLeave(ctx context.Context, vcenterID, moref string) error
}

// IncrementalApplier maps PropertyCollector ObjectUpdates (Enter, Modify, Leave)
// to targeted, transactional database persistence operations.
type IncrementalApplier struct {
	persister VMPersister
	logger    *slog.Logger

	mu   sync.RWMutex
	snap *TopologySnapshot
}

// NewIncrementalApplier constructs an IncrementalApplier instance.
func NewIncrementalApplier(persister VMPersister, snap *TopologySnapshot, logger *slog.Logger) *IncrementalApplier {
	if logger == nil {
		logger = slog.Default()
	}
	return &IncrementalApplier{
		persister: persister,
		snap:      snap,
		logger:    logger,
	}
}

// SetTopologySnapshot updates the active immutable TopologySnapshot used for entity resolution.
func (a *IncrementalApplier) SetTopologySnapshot(snap *TopologySnapshot) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.snap = snap
}

// TopologySnapshot returns the active TopologySnapshot.
func (a *IncrementalApplier) TopologySnapshot() *TopologySnapshot {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.snap
}

// ApplyBatch applies a slice of ObjectUpdates transactionally.
func (a *IncrementalApplier) ApplyBatch(ctx context.Context, vcenterID string, updates []types.ObjectUpdate) error {
	for _, u := range updates {
		if err := a.ApplyObjectUpdate(ctx, vcenterID, u); err != nil {
			return fmt.Errorf("failed to apply object update (%s:%s): %w", u.Obj.Type, u.Obj.Value, err)
		}
	}
	return nil
}

// ApplyObjectUpdate applies an individual ObjectUpdate based on its Kind (Enter, Modify, Leave).
func (a *IncrementalApplier) ApplyObjectUpdate(ctx context.Context, vcenterID string, update types.ObjectUpdate) error {
	if update.Obj.Type != "VirtualMachine" {
		// Currently only VirtualMachine objects are streamed
		return nil
	}

	moref := update.Obj.Value

	switch update.Kind {
	case types.ObjectUpdateKindEnter:
		return a.handleEnter(ctx, vcenterID, moref, update.ChangeSet)

	case types.ObjectUpdateKindModify:
		return a.handleModify(ctx, vcenterID, moref, update.ChangeSet)

	case types.ObjectUpdateKindLeave:
		return a.handleLeave(ctx, vcenterID, moref)

	default:
		a.logger.Debug("ignoring unknown object update kind", slog.String("kind", string(update.Kind)), slog.String("moref", moref))
		return nil
	}
}

// handleEnter parses all properties from ChangeSet and persists a complete VMRecord.
func (a *IncrementalApplier) handleEnter(ctx context.Context, vcenterID, moref string, changes []types.PropertyChange) error {
	mask := a.parseChangeSetToMask(vcenterID, moref, changes)

	// Try incremental update first in case the record already exists (e.g. re-register or un-delete)
	err := a.persister.ApplyVMIncrementalUpdate(ctx, vcenterID, moref, mask)
	if err == nil {
		return nil
	}

	// If VM record does not exist in DB yet, construct a full domain.VMRecord and persist
	name := moref
	if mask.Name != nil && *mask.Name != "" {
		name = *mask.Name
	}
	powerState := "poweredOff"
	if mask.PowerState != nil && *mask.PowerState != "" {
		powerState = *mask.PowerState
	}
	connectionState := "connected"
	if mask.ConnectionState != nil && *mask.ConnectionState != "" {
		connectionState = *mask.ConnectionState
	}

	instanceUUID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(vcenterID+":"+moref)).String()
	if mask.InstanceUUID != nil && *mask.InstanceUUID != "" {
		instanceUUID = *mask.InstanceUUID
	}

	vm := domain.VMRecord{
		VCenterID:         vcenterID,
		MoRef:             moref,
		InstanceUUID:      instanceUUID,
		Name:              name,
		PowerState:        powerState,
		ConnectionState:   connectionState,
		DatastoreIDs:      []string{},
		DatastoreMoRefs:   []string{},
		NetworkInterfaces: mask.NetworkInterfaces,
		CustomAttributes:  mask.CustomAttributes,
	}
	if mask.BiosUUID != nil && *mask.BiosUUID != "" {
		vm.BiosUUID = *mask.BiosUUID
	}
	if mask.FolderID != nil {
		vm.FolderID = *mask.FolderID
	}
	if mask.FolderPath != nil {
		vm.FolderPath = *mask.FolderPath
	}
	if mask.HostID != nil {
		vm.HostID = *mask.HostID
	}
	if mask.ClusterID != nil {
		vm.ClusterID = *mask.ClusterID
	}
	if mask.DatacenterID != nil {
		vm.DatacenterID = *mask.DatacenterID
	}
	if mask.CPUCores != nil {
		vm.CPUCores = *mask.CPUCores
	}
	if mask.MemoryMB != nil {
		vm.MemoryMB = *mask.MemoryMB
	}
	if mask.GuestOS != nil {
		vm.GuestOS = *mask.GuestOS
	}
	if mask.GuestHostName != nil {
		vm.GuestHostName = *mask.GuestHostName
	}
	if mask.GuestIPAddress != nil {
		vm.GuestIPAddress = *mask.GuestIPAddress
	}
	if mask.Annotation != nil {
		vm.Annotation = *mask.Annotation
	}
	if mask.CommittedStorageBytes != nil {
		vm.CommittedStorageBytes = *mask.CommittedStorageBytes
	}
	if mask.ProvisionedStorageBytes != nil {
		vm.ProvisionedStorageBytes = *mask.ProvisionedStorageBytes
	}

	_, err = a.persister.PersistVMBatch(ctx, []domain.VMRecord{vm})
	return err
}

// handleModify parses only changed property paths and executes a targeted field-mask update.
func (a *IncrementalApplier) handleModify(ctx context.Context, vcenterID, moref string, changes []types.PropertyChange) error {
	mask := a.parseChangeSetToMask(vcenterID, moref, changes)
	now := time.Now()
	mask.LastSeenAt = &now

	return a.persister.ApplyVMIncrementalUpdate(ctx, vcenterID, moref, mask)
}

// handleLeave marks a VM as soft-deleted.
func (a *IncrementalApplier) handleLeave(ctx context.Context, vcenterID, moref string) error {
	err := a.persister.ApplyVMLeave(ctx, vcenterID, moref)
	if err != nil && (strings.Contains(err.Error(), "vm_not_found") || strings.Contains(err.Error(), "not found")) {
		// If already deleted or missing, treat leave as idempotent success
		return nil
	}
	return err
}

// parseChangeSetToMask extracts individual property modifications into a VMFieldMask.
func (a *IncrementalApplier) parseChangeSetToMask(vcenterID, moref string, changes []types.PropertyChange) VMFieldMask {
	a.mu.RLock()
	snap := a.snap
	a.mu.RUnlock()

	var mask VMFieldMask
	var rawGuestNet any
	var rawHardwareDevices any

	for _, c := range changes {
		switch c.Name {
		case "name":
			if s, ok := c.Val.(string); ok {
				mask.Name = &s
			}

		case "runtime.powerState":
			if ps, ok := c.Val.(types.VirtualMachinePowerState); ok {
				s := string(ps)
				mask.PowerState = &s
			} else if s, ok := c.Val.(string); ok {
				mask.PowerState = &s
			}

		case "runtime.connectionState":
			if cs, ok := c.Val.(types.VirtualMachineConnectionState); ok {
				s := string(cs)
				mask.ConnectionState = &s
			} else if s, ok := c.Val.(string); ok {
				mask.ConnectionState = &s
			}

		case "summary.runtime":
			if rt, ok := c.Val.(types.VirtualMachineRuntimeInfo); ok {
				if rt.PowerState != "" {
					s := string(rt.PowerState)
					mask.PowerState = &s
				}
				if rt.ConnectionState != "" {
					s := string(rt.ConnectionState)
					mask.ConnectionState = &s
				}
				if rt.Host != nil && snap != nil {
					a.resolveHost(rt.Host.Value, snap, &mask)
				}
			}

		case "config.instanceUuid":
			if s, ok := c.Val.(string); ok && s != "" {
				mask.InstanceUUID = &s
			}

		case "summary.config.instanceUuid":
			if s, ok := c.Val.(string); ok && s != "" {
				mask.InstanceUUID = &s
			}

		case "config.uuid":
			if s, ok := c.Val.(string); ok && s != "" {
				mask.BiosUUID = &s
			}

		case "summary.config.uuid":
			if s, ok := c.Val.(string); ok && s != "" {
				mask.BiosUUID = &s
			}

		case "summary.config":
			if sc, ok := c.Val.(types.VirtualMachineConfigSummary); ok {
				if sc.InstanceUuid != "" {
					mask.InstanceUUID = &sc.InstanceUuid
				}
				if sc.Uuid != "" {
					mask.BiosUUID = &sc.Uuid
				}
				if sc.Name != "" {
					mask.Name = &sc.Name
				}
				if sc.NumCpu > 0 {
					mask.CPUCores = &sc.NumCpu
				}
				if sc.MemorySizeMB > 0 {
					mb := int64(sc.MemorySizeMB)
					mask.MemoryMB = &mb
				}
				if sc.GuestId != "" {
					mask.GuestOS = &sc.GuestId
				}
				if sc.Annotation != "" {
					mask.Annotation = &sc.Annotation
				}
			}

		case "summary.guest":
			if g, ok := c.Val.(types.VirtualMachineGuestSummary); ok {
				if g.GuestFullName != "" {
					mask.GuestOS = &g.GuestFullName
				}
				if g.HostName != "" {
					mask.GuestHostName = &g.HostName
				}
				if g.IpAddress != "" {
					ip := cleanIP(g.IpAddress)
					mask.GuestIPAddress = &ip
				}
			}

		case "summary.customValue", "customValue":
			attrs := a.parseCustomValues(c.Val, snap)
			if len(attrs) > 0 {
				mask.CustomAttributes = attrs
			}

		case "guest.net":
			rawGuestNet = c.Val

		case "config.hardware.device", "config.hardware":
			rawHardwareDevices = c.Val
			if mask.ProvisionedStorageBytes == nil {
				if capBytes := a.calculateDiskCapacity(c.Val); capBytes > 0 {
					mask.ProvisionedStorageBytes = &capBytes
				}
			}

		case "summary.storage":
			if st, ok := c.Val.(types.VirtualMachineStorageSummary); ok {
				committed := st.Committed
				provisioned := st.Committed + st.Uncommitted
				mask.CommittedStorageBytes = &committed
				mask.ProvisionedStorageBytes = &provisioned
			}

		case "parent":
			if ref, ok := c.Val.(types.ManagedObjectReference); ok && snap != nil {
				if id, ok := snap.FolderIDByMoRef[ref.Value]; ok {
					mask.FolderID = &id
				}
				if path, ok := snap.FolderPathByMoRef[ref.Value]; ok {
					mask.FolderPath = &path
				}
			}

		case "runtime.host":
			if ref, ok := c.Val.(types.ManagedObjectReference); ok && snap != nil {
				a.resolveHost(ref.Value, snap, &mask)
			}
		}
	}

	if rawHardwareDevices != nil && rawGuestNet != nil {
		devNICs := a.parseHardwareDevices(rawHardwareDevices, snap)
		gNICs := a.parseGuestNet(rawGuestNet, snap)
		if len(devNICs) > 0 {
			guestIPByMAC := make(map[string][]string)
			for _, g := range gNICs {
				if g.IPAddress != "" {
					guestIPByMAC[g.MacAddress] = append(guestIPByMAC[g.MacAddress], g.IPAddress)
				}
			}
			var enriched []domain.NetworkInterfaceRecord
			for _, d := range devNICs {
				if ips, ok := guestIPByMAC[d.MacAddress]; ok && len(ips) > 0 {
					for _, ip := range ips {
						rec := d
						rec.IPAddress = ip
						enriched = append(enriched, rec)
					}
				} else {
					enriched = append(enriched, d)
				}
			}
			mask.NetworkInterfaces = enriched
		} else if len(gNICs) > 0 {
			mask.NetworkInterfaces = gNICs
		}
	} else if rawHardwareDevices != nil {
		if devNICs := a.parseHardwareDevices(rawHardwareDevices, snap); len(devNICs) > 0 {
			mask.NetworkInterfaces = devNICs
		}
	} else if rawGuestNet != nil {
		if gNICs := a.parseGuestNet(rawGuestNet, snap); len(gNICs) > 0 {
			mask.NetworkInterfaces = gNICs
		}
	}

	return mask
}

// resolveHost populates host, cluster, and datacenter references from a host MoRef.
func (a *IncrementalApplier) resolveHost(hostMoRef string, snap *TopologySnapshot, mask *VMFieldMask) {
	if snap == nil {
		return
	}
	if hostID, ok := snap.HostIDByMoRef[hostMoRef]; ok {
		mask.HostID = &hostID
	}
	if clusterID, ok := snap.ClusterIDByMoRef[hostMoRef]; ok {
		mask.ClusterID = &clusterID
	}
	if dcID, ok := snap.DatacenterIDByMoRef[hostMoRef]; ok {
		mask.DatacenterID = &dcID
	}
}

// parseCustomValues decodes custom field values into name-value pairs using TopologySnapshot definitions.
func (a *IncrementalApplier) parseCustomValues(val any, snap *TopologySnapshot) []domain.CustomAttributeRecord {
	var res []domain.CustomAttributeRecord
	var cfList []types.BaseCustomFieldValue
	switch v := val.(type) {
	case []types.BaseCustomFieldValue:
		cfList = v
	case *types.ArrayOfCustomFieldValue:
		if v != nil {
			cfList = v.CustomFieldValue
		}
	}

	for _, item := range cfList {
		if strVal, ok := item.(*types.CustomFieldStringValue); ok {
			attrName := fmt.Sprintf("custom_field_%d", strVal.Key)
			if snap != nil && snap.CustomFieldsByKey != nil {
				if name, ok := snap.CustomFieldsByKey[strVal.Key]; ok && name != "" {
					attrName = name
				}
			}
			res = append(res, domain.CustomAttributeRecord{
				Name:  attrName,
				Value: strVal.Value,
			})
		}
	}
	return res
}

// parseGuestNet decodes GuestNicInfo items into domain.NetworkInterfaceRecords.
func (a *IncrementalApplier) parseGuestNet(val any, snap *TopologySnapshot) []domain.NetworkInterfaceRecord {
	nics := make([]domain.NetworkInterfaceRecord, 0)
	var nicList []types.GuestNicInfo

	switch v := val.(type) {
	case []types.GuestNicInfo:
		nicList = v
	case *types.ArrayOfGuestNicInfo:
		if v != nil {
			nicList = v.GuestNicInfo
		}
	}

	for _, gnic := range nicList {
		mac := strings.ToLower(strings.TrimSpace(gnic.MacAddress))
		if mac == "" {
			continue
		}

		var netID, pgID, vlanSpec string
		netName := gnic.Network
		if snap != nil && gnic.Network != "" {
			if net, ok := snap.Networks[gnic.Network]; ok {
				netID = net.ID
				if netID == "" {
					netID = snap.NetworkIDByMoRef[gnic.Network]
				}
				vlanSpec = net.VLANSpec
			} else if pg, ok := snap.Portgroups[gnic.Network]; ok {
				pgID = pg.ID
				if pgID == "" {
					pgID = snap.PortgroupIDByMoRef[gnic.Network]
				}
				vlanSpec = pg.VLANSpec
			} else if id, ok := snap.PortgroupIDByMoRef[gnic.Network]; ok {
				pgID = id
				if pg, ok2 := snap.Portgroups[gnic.Network]; ok2 {
					vlanSpec = pg.VLANSpec
				}
			} else if id, ok := snap.NetworkIDByMoRef[gnic.Network]; ok {
				netID = id
				if net, ok2 := snap.Networks[gnic.Network]; ok2 {
					vlanSpec = net.VLANSpec
				}
			}

			if netID == "" && pgID == "" {
				if pg, ok := snap.PortgroupByName[netName]; ok {
					pgID = pg.ID
					if pgID == "" {
						pgID = snap.PortgroupIDByMoRef[pg.MoRef]
					}
					vlanSpec = pg.VLANSpec
				} else if net, ok := snap.NetworkByName[netName]; ok {
					netID = net.ID
					if netID == "" {
						netID = snap.NetworkIDByMoRef[net.MoRef]
					}
					vlanSpec = net.VLANSpec
				}
			}
		}

		if len(gnic.IpAddress) > 0 {
			for _, rawIP := range gnic.IpAddress {
				nics = append(nics, domain.NetworkInterfaceRecord{
					DeviceKey:    gnic.DeviceConfigId,
					MacAddress:   mac,
					IPAddress:    cleanIP(rawIP),
					NetworkID:    netID,
					PortgroupID:  pgID,
					NetworkMoRef: gnic.Network,
					VLANSpec:     vlanSpec,
				})
			}
		} else {
			nics = append(nics, domain.NetworkInterfaceRecord{
				DeviceKey:    gnic.DeviceConfigId,
				MacAddress:   mac,
				IPAddress:    "",
				NetworkID:    netID,
				PortgroupID:  pgID,
				NetworkMoRef: gnic.Network,
				VLANSpec:     vlanSpec,
			})
		}
	}

	return nics
}

// parseHardwareDevices extracts network interfaces from virtual hardware device changes.
func (a *IncrementalApplier) parseHardwareDevices(val any, snap *TopologySnapshot) []domain.NetworkInterfaceRecord {
	var devices []types.BaseVirtualDevice
	switch v := val.(type) {
	case []types.BaseVirtualDevice:
		devices = v
	case *types.ArrayOfVirtualDevice:
		if v != nil {
			devices = v.VirtualDevice
		}
	case types.VirtualHardware:
		devices = v.Device
	case *types.VirtualHardware:
		if v != nil {
			devices = v.Device
		}
	}

	if len(devices) == 0 {
		return nil
	}

	var nics []domain.NetworkInterfaceRecord
	for _, dev := range devices {
		if eth, ok := dev.(types.BaseVirtualEthernetCard); ok {
			card := eth.GetVirtualEthernetCard()
			if card == nil {
				continue
			}

			mac := strings.ToLower(strings.TrimSpace(card.MacAddress))
			var netMoRef, pgMoRef, netID, pgID, vlanSpec, netName string

			if card.Backing != nil {
				switch b := card.Backing.(type) {
				case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
					pgMoRef = b.Port.PortgroupKey
				case *types.VirtualEthernetCardNetworkBackingInfo:
					if b.Network != nil {
						netMoRef = b.Network.Value
					}
					netName = b.DeviceName
				}
			}

			if snap != nil {
				if pgMoRef != "" {
					if pg, ok := snap.Portgroups[pgMoRef]; ok {
						pgID = pg.ID
						if pgID == "" {
							pgID = snap.PortgroupIDByMoRef[pgMoRef]
						}
						vlanSpec = pg.VLANSpec
						if pg.Name != "" {
							netName = pg.Name
						}
					} else if id, ok := snap.PortgroupIDByMoRef[pgMoRef]; ok {
						pgID = id
					}
				}
				if netMoRef != "" {
					if net, ok := snap.Networks[netMoRef]; ok {
						netID = net.ID
						if netID == "" {
							netID = snap.NetworkIDByMoRef[netMoRef]
						}
						if vlanSpec == "" {
							vlanSpec = net.VLANSpec
						}
						if net.Name != "" {
							netName = net.Name
						}
					} else if id, ok := snap.NetworkIDByMoRef[netMoRef]; ok {
						netID = id
					}
				}
				if netName != "" {
					if pg, ok := snap.PortgroupByName[netName]; ok {
						if pgID == "" {
							pgID = pg.ID
							if pgID == "" {
								pgID = snap.PortgroupIDByMoRef[pg.MoRef]
							}
						}
						if vlanSpec == "" {
							vlanSpec = pg.VLANSpec
						}
					} else if net, ok := snap.NetworkByName[netName]; ok {
						if netID == "" {
							netID = net.ID
							if netID == "" {
								netID = snap.NetworkIDByMoRef[net.MoRef]
							}
						}
						if vlanSpec == "" {
							vlanSpec = net.VLANSpec
						}
					}
				}
			}

			var matchedMoRef string
			if pgMoRef != "" {
				matchedMoRef = pgMoRef
			} else if netMoRef != "" {
				matchedMoRef = netMoRef
			}

			nics = append(nics, domain.NetworkInterfaceRecord{
				DeviceKey:    card.Key,
				MacAddress:   mac,
				IPAddress:    "",
				NetworkID:    netID,
				PortgroupID:  pgID,
				NetworkMoRef: matchedMoRef,
				VLANSpec:     vlanSpec,
			})
		}
	}
	return nics
}

// calculateDiskCapacity totals the disk capacity from virtual hardware devices.
func (a *IncrementalApplier) calculateDiskCapacity(val any) int64 {
	var devices []types.BaseVirtualDevice
	switch v := val.(type) {
	case []types.BaseVirtualDevice:
		devices = v
	case *types.ArrayOfVirtualDevice:
		if v != nil {
			devices = v.VirtualDevice
		}
	case types.VirtualHardware:
		devices = v.Device
	case *types.VirtualHardware:
		if v != nil {
			devices = v.Device
		}
	}

	var total int64
	for _, dev := range devices {
		if disk, ok := dev.(*types.VirtualDisk); ok {
			if disk.CapacityInBytes > 0 {
				total += disk.CapacityInBytes
			} else if disk.CapacityInKB > 0 {
				total += disk.CapacityInKB * 1024
			}
		}
	}
	return total
}
