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

	// Storage sizing calculation
	var committedStorageBytes, provisionedStorageBytes int64
	if vm.Summary.Storage != nil {
		committedStorageBytes = vm.Summary.Storage.Committed
		provisionedStorageBytes = vm.Summary.Storage.Committed + vm.Summary.Storage.Uncommitted
	}
	var diskCapBytes int64
	if vm.Config != nil && vm.Config.Hardware.Device != nil {
		for _, dev := range vm.Config.Hardware.Device {
			if disk, ok := dev.(*types.VirtualDisk); ok {
				if disk.CapacityInBytes > 0 {
					diskCapBytes += disk.CapacityInBytes
				} else if disk.CapacityInKB > 0 {
					diskCapBytes += disk.CapacityInKB * 1024
				}
			}
		}
	}
	if provisionedStorageBytes <= 0 && diskCapBytes > 0 {
		provisionedStorageBytes = diskCapBytes
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
		VCenterID:               vcenterID,
		MoRef:                   moref,
		InstanceUUID:            instanceUUID,
		BiosUUID:                biosUUID,
		Name:                    name,
		PowerState:              powerState,
		ConnectionState:         connectionState,
		CPUCores:                cpuCores,
		MemoryMB:                memoryMB,
		FolderID:                folderID,
		FolderPath:              folderPath,
		HostID:                  hostID,
		ClusterID:               clusterID,
		DatacenterID:            datacenterID,
		GuestOS:                 guestOS,
		GuestHostName:           guestHostName,
		GuestIPAddress:          guestIP,
		Annotation:              annotation,
		CommittedStorageBytes:   committedStorageBytes,
		ProvisionedStorageBytes: provisionedStorageBytes,
		DatastoreMoRefs:         dsMoRefs,
		DatastoreIDs:            dsIDs,
		NetworkInterfaces:       nics,
		CustomAttributes:        customAttrs,
	}
}

// extractNetworkInterfaces extracts virtual NIC records.
// It prioritizes authoritative hardware devices in vm.Config.Hardware.Device,
// correlating with guest.net for active IP address enrichment and resolving
// VLAN specs from the topology snapshot.
func extractNetworkInterfaces(vm mo.VirtualMachine, snap *TopologySnapshot) []domain.NetworkInterfaceRecord {
	var nics []domain.NetworkInterfaceRecord

	// Index guest.net by DeviceConfigId and by MAC address for runtime IP enrichment
	type guestNetInfo struct {
		ips         []string
		networkName string
	}
	guestByDevKey := make(map[int32]guestNetInfo)
	guestByMAC := make(map[string]guestNetInfo)

	if vm.Guest != nil && len(vm.Guest.Net) > 0 {
		for _, gNet := range vm.Guest.Net {
			mac := strings.ToLower(strings.TrimSpace(gNet.MacAddress))
			var cleanIPs []string
			for _, ip := range gNet.IpAddress {
				if c := cleanIP(ip); c != "" {
					cleanIPs = append(cleanIPs, c)
				}
			}
			info := guestNetInfo{
				ips:         cleanIPs,
				networkName: gNet.Network,
			}
			if gNet.DeviceConfigId > 0 {
				guestByDevKey[gNet.DeviceConfigId] = info
			}
			if mac != "" {
				guestByMAC[mac] = info
			}
		}
	}

	// Strategy 1: Iterate hardware virtual ethernet devices (authoritative source of interfaces & backing)
	if vm.Config != nil && vm.Config.Hardware.Device != nil {
		for _, dev := range vm.Config.Hardware.Device {
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
					// If netName is known, attempt resolution against name lookups
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

				// Find runtime IP enrichment from guest.net
				var ips []string
				if gInfo, ok := guestByDevKey[card.Key]; ok {
					ips = gInfo.ips
					if netName == "" && gInfo.networkName != "" {
						netName = gInfo.networkName
					}
				} else if gInfo, ok := guestByMAC[mac]; ok {
					ips = gInfo.ips
					if netName == "" && gInfo.networkName != "" {
						netName = gInfo.networkName
					}
				}

				// Secondary name lookup if guest network name was found
				if snap != nil && (netID == "" && pgID == "") && netName != "" {
					if pg, ok := snap.PortgroupByName[netName]; ok {
						pgID = pg.ID
						if pgID == "" {
							pgID = snap.PortgroupIDByMoRef[pg.MoRef]
						}
						if vlanSpec == "" {
							vlanSpec = pg.VLANSpec
						}
					} else if net, ok := snap.NetworkByName[netName]; ok {
						netID = net.ID
						if netID == "" {
							netID = snap.NetworkIDByMoRef[net.MoRef]
						}
						if vlanSpec == "" {
							vlanSpec = net.VLANSpec
						}
					}
				}

				if len(ips) > 0 {
					for _, ip := range ips {
						nics = append(nics, domain.NetworkInterfaceRecord{
							DeviceKey:      card.Key,
							MacAddress:     mac,
							IPAddress:      ip,
							NetworkID:      netID,
							PortgroupID:    pgID,
							NetworkMoRef:   netMoRef,
							PortgroupMoRef: pgMoRef,
							VLANSpec:       vlanSpec,
							NetworkName:    netName,
						})
					}
				} else {
					nics = append(nics, domain.NetworkInterfaceRecord{
						DeviceKey:      card.Key,
						MacAddress:     mac,
						IPAddress:      "",
						NetworkID:      netID,
						PortgroupID:    pgID,
						NetworkMoRef:   netMoRef,
						PortgroupMoRef: pgMoRef,
						VLANSpec:       vlanSpec,
						NetworkName:    netName,
					})
				}
			}
		}
	}

	if len(nics) > 0 {
		return nics
	}

	// Strategy 2: Fallback when vm.Config.Hardware.Device is absent (e.g. lightweight DTOs)
	if vm.Guest != nil && len(vm.Guest.Net) > 0 {
		for _, gNet := range vm.Guest.Net {
			mac := strings.ToLower(strings.TrimSpace(gNet.MacAddress))
			var netID, pgID, vlanSpec, netName string
			netName = gNet.Network

			if snap != nil && netName != "" {
				if id, ok := snap.NetworkIDByMoRef[netName]; ok {
					netID = id
					if net, ok := snap.Networks[netName]; ok {
						vlanSpec = net.VLANSpec
					}
				} else if id, ok := snap.PortgroupIDByMoRef[netName]; ok {
					pgID = id
					if pg, ok := snap.Portgroups[netName]; ok {
						vlanSpec = pg.VLANSpec
					}
				} else if pg, ok := snap.PortgroupByName[netName]; ok {
					pgID = snap.PortgroupIDByMoRef[pg.MoRef]
					vlanSpec = pg.VLANSpec
				} else if net, ok := snap.NetworkByName[netName]; ok {
					netID = snap.NetworkIDByMoRef[net.MoRef]
					vlanSpec = net.VLANSpec
				} else if net, ok := snap.Networks[netName]; ok {
					netID = snap.NetworkIDByMoRef[net.MoRef]
					vlanSpec = net.VLANSpec
				} else if pg, ok := snap.Portgroups[netName]; ok {
					pgID = snap.PortgroupIDByMoRef[pg.MoRef]
					vlanSpec = pg.VLANSpec
				}
			}

			if len(gNet.IpAddress) > 0 {
				for _, ip := range gNet.IpAddress {
					cIP := cleanIP(ip)
					nics = append(nics, domain.NetworkInterfaceRecord{
						DeviceKey:    gNet.DeviceConfigId,
						MacAddress:   mac,
						IPAddress:    cIP,
						NetworkID:    netID,
						PortgroupID:  pgID,
						NetworkMoRef: gNet.Network,
						VLANSpec:     vlanSpec,
						NetworkName:  netName,
					})
				}
			} else if mac != "" {
				nics = append(nics, domain.NetworkInterfaceRecord{
					DeviceKey:    gNet.DeviceConfigId,
					MacAddress:   mac,
					IPAddress:    "",
					NetworkID:    netID,
					PortgroupID:  pgID,
					NetworkMoRef: gNet.Network,
					VLANSpec:     vlanSpec,
					NetworkName:  netName,
				})
			}
		}
	}

	return nics
}
