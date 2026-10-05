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

		case "summary.config":
			if sc, ok := c.Val.(types.VirtualMachineConfigSummary); ok {
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
			mask.NetworkInterfaces = a.parseGuestNet(c.Val, snap)

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
		if snap != nil && gnic.Network != "" {
			if net, ok := snap.Networks[gnic.Network]; ok {
				netID = net.ID
				vlanSpec = net.VLANSpec
			} else if pg, ok := snap.Portgroups[gnic.Network]; ok {
				pgID = pg.ID
				vlanSpec = pg.VLANSpec
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
