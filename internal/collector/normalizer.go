package collector

import (
	"net/netip"
	"strings"

	"github.com/google/uuid"
	"github.com/infrastructure/sims/internal/domain"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// cleanIP sanitizes IP strings by stripping IPv6 zone indexes (%eth0, etc.)
// and validating address syntax so PostgreSQL inet does not reject it.
func cleanIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := netip.ParseAddr(raw); err == nil {
		return parsed.WithZone("").String()
	}
	if idx := strings.Index(raw, "%"); idx != -1 {
		raw = raw[:idx]
	}
	if parsed, err := netip.ParseAddr(raw); err == nil {
		return parsed.String()
	}
	return ""
}

// NormalizeVM maps a vSphere mo.VirtualMachine DTO into a normalized domain.VMRecord,
// resolving topology references against the provided immutable TopologySnapshot.
func NormalizeVM(vcenterID string, vm mo.VirtualMachine, snap *TopologySnapshot) domain.VMRecord {
	moref := vm.Reference().Value

	instanceUUID := ""
	biosUUID := ""
	name := vm.Name
	powerState := "poweredOff"
	connectionState := "connected"
	cpuCores := int32(0)
	memoryMB := int64(0)
	guestOS := ""
	guestHostName := ""
	guestIP := ""
	annotation := ""

	if vm.Summary.Config.InstanceUuid != "" {
		instanceUUID = vm.Summary.Config.InstanceUuid
	} else if vm.Config != nil && vm.Config.InstanceUuid != "" {
		instanceUUID = vm.Config.InstanceUuid
	}
	if strings.TrimSpace(instanceUUID) == "" {
		// Real vCenters may have VMs/templates with empty instance UUID.
		// Generate deterministic fallback UUID from (vcenterID, moref) to satisfy
		// the active unique constraint and ensure idempotent re-sync.
		instanceUUID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(vcenterID+":"+moref)).String()
	}

	if vm.Summary.Config.Uuid != "" {
		biosUUID = vm.Summary.Config.Uuid
	} else if vm.Config != nil && vm.Config.Uuid != "" {
		biosUUID = vm.Config.Uuid
	}

	if vm.Summary.Config.Name != "" {
		name = vm.Summary.Config.Name
	}

	if vm.Summary.Runtime.PowerState != "" {
		powerState = string(vm.Summary.Runtime.PowerState)
	} else if vm.Runtime.PowerState != "" {
		powerState = string(vm.Runtime.PowerState)
	}

	if vm.Summary.Runtime.ConnectionState != "" {
		connectionState = string(vm.Summary.Runtime.ConnectionState)
	}

	if vm.Summary.Config.NumCpu > 0 {
		cpuCores = vm.Summary.Config.NumCpu
	} else if vm.Config != nil {
		cpuCores = vm.Config.Hardware.NumCPU
	}

	if vm.Summary.Config.MemorySizeMB > 0 {
		memoryMB = int64(vm.Summary.Config.MemorySizeMB)
	} else if vm.Config != nil {
		memoryMB = int64(vm.Config.Hardware.MemoryMB)
	}

	if vm.Guest != nil {
		if vm.Guest.GuestFullName != "" {
			guestOS = vm.Guest.GuestFullName
		}
		if vm.Guest.HostName != "" {
			guestHostName = vm.Guest.HostName
		}
		if vm.Guest.IpAddress != "" {
			guestIP = cleanIP(vm.Guest.IpAddress)
		}
	}
	if guestOS == "" && vm.Summary.Config.GuestFullName != "" {
		guestOS = vm.Summary.Config.GuestFullName
	}

	if vm.Summary.Config.Annotation != "" {
		annotation = vm.Summary.Config.Annotation
	}

	// Topology resolution
	var folderID, folderPath string
	if vm.Parent != nil && snap != nil {
		folderID = snap.FolderIDByMoRef[vm.Parent.Value]
		folderPath = snap.FolderPathByMoRef[vm.Parent.Value]
	}

	var hostID, clusterID, datacenterID string
	if vm.Runtime.Host != nil && snap != nil {
		hostID = snap.HostIDByMoRef[vm.Runtime.Host.Value]
		clusterID = snap.ClusterIDByMoRef[vm.Runtime.Host.Value]
		datacenterID = snap.DatacenterIDByMoRef[vm.Runtime.Host.Value]
	}
	if datacenterID == "" && vm.Parent != nil && snap != nil {
		datacenterID = snap.DatacenterIDByMoRef[vm.Parent.Value]
	}

	// Datastores
	var dsMoRefs []string
	var dsIDs []string
	for _, dsRef := range vm.Datastore {
		dsMoRefs = append(dsMoRefs, dsRef.Value)
		if snap != nil {
			if id, ok := snap.DatastoreIDByMoRef[dsRef.Value]; ok {
				dsIDs = append(dsIDs, id)
			}
		}
	}

	// Network interfaces extraction with hardware fallback
	nics := extractNetworkInterfaces(vm, snap)

	// Custom attributes extraction
	var customAttrs []domain.CustomAttributeRecord
	if len(vm.CustomValue) > 0 && snap != nil && len(snap.CustomFieldsByKey) > 0 {
		for _, val := range vm.CustomValue {
			if strVal, ok := val.(*types.CustomFieldStringValue); ok {
				if attrName, exists := snap.CustomFieldsByKey[strVal.Key]; exists && attrName != "" && strVal.Value != "" {
					customAttrs = append(customAttrs, domain.CustomAttributeRecord{
						Name:  attrName,
						Value: strVal.Value,
					})
				}
			}
		}
	}

	return domain.VMRecord{
		VCenterID:         vcenterID,
		MoRef:             moref,
		InstanceUUID:      instanceUUID,
		BiosUUID:          biosUUID,
		Name:              name,
		PowerState:        powerState,
		ConnectionState:   connectionState,
		CPUCores:          cpuCores,
		MemoryMB:          memoryMB,
		FolderID:          folderID,
		FolderPath:        folderPath,
		HostID:            hostID,
		ClusterID:         clusterID,
		DatacenterID:      datacenterID,
		GuestOS:           guestOS,
		GuestHostName:     guestHostName,
		GuestIPAddress:    guestIP,
		Annotation:        annotation,
		DatastoreMoRefs:   dsMoRefs,
		DatastoreIDs:      dsIDs,
		NetworkInterfaces: nics,
		CustomAttributes:  customAttrs,
	}
}

// extractNetworkInterfaces extracts virtual NIC records.
// It prioritizes guest.net information. If guest.net is empty or missing
// (e.g. VM powered off or VMware Tools not running), it falls back to
// hardware devices in vm.Config.Hardware.Device.
func extractNetworkInterfaces(vm mo.VirtualMachine, snap *TopologySnapshot) []domain.NetworkInterfaceRecord {
	var nics []domain.NetworkInterfaceRecord

	// Strategy 1: guest.net populated
	if vm.Guest != nil && len(vm.Guest.Net) > 0 {
		for _, gNet := range vm.Guest.Net {
			mac := strings.ToLower(strings.TrimSpace(gNet.MacAddress))
			netMoRef := gNet.Network

			var netID, pgID string
			if snap != nil {
				netID = snap.NetworkIDByMoRef[netMoRef]
			}

			// If interface has multiple IPs, create an entry for each IP, or if 0 IPs, one with empty IP
			if len(gNet.IpAddress) > 0 {
				for _, ip := range gNet.IpAddress {
					nics = append(nics, domain.NetworkInterfaceRecord{
						DeviceKey:    gNet.DeviceConfigId,
						MacAddress:   mac,
						IPAddress:    cleanIP(ip),
						NetworkID:    netID,
						PortgroupID:  pgID,
						NetworkMoRef: netMoRef,
					})
				}
			} else if mac != "" {
				nics = append(nics, domain.NetworkInterfaceRecord{
					DeviceKey:    gNet.DeviceConfigId,
					MacAddress:   mac,
					IPAddress:    "",
					NetworkID:    netID,
					PortgroupID:  pgID,
					NetworkMoRef: netMoRef,
				})
			}
		}
	}

	if len(nics) > 0 {
		return nics
	}

	// Strategy 2: Fallback to Hardware devices (VirtualEthernetCard)
	if vm.Config != nil && vm.Config.Hardware.Device != nil {
		for _, dev := range vm.Config.Hardware.Device {
			if eth, ok := dev.(types.BaseVirtualEthernetCard); ok {
				card := eth.GetVirtualEthernetCard()
				if card == nil {
					continue
				}

				mac := strings.ToLower(strings.TrimSpace(card.MacAddress))
				var netMoRef, pgMoRef, netID, pgID string

				if card.Backing != nil {
					switch b := card.Backing.(type) {
					case *types.VirtualEthernetCardNetworkBackingInfo:
						if b.Network != nil {
							netMoRef = b.Network.Value
						}
					case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
						pgMoRef = b.Port.PortgroupKey
					}
				}

				if snap != nil {
					if netMoRef != "" {
						netID = snap.NetworkIDByMoRef[netMoRef]
					}
					if pgMoRef != "" {
						pgID = snap.PortgroupIDByMoRef[pgMoRef]
					}
				}

				nics = append(nics, domain.NetworkInterfaceRecord{
					DeviceKey:      card.Key,
					MacAddress:     mac,
					IPAddress:      "", // Hardware config does not know guest runtime IP
					NetworkID:      netID,
					PortgroupID:    pgID,
					NetworkMoRef:   netMoRef,
					PortgroupMoRef: pgMoRef,
				})
			}
		}
	}

	return nics
}
