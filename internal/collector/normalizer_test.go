package collector_test

import (
	"testing"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

func TestNormalizeVMWithGuestNet(t *testing.T) {
	snap := collector.NewEmptySnapshot("vc-test-01")
	snap.NetworkIDByMoRef["network-10"] = "net-uuid-10"

	vm := mo.VirtualMachine{
		ManagedEntity: mo.ManagedEntity{
			ExtensibleManagedObject: mo.ExtensibleManagedObject{
				Self: types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-100"},
			},
			Name: "prod-web-01",
		},
		Summary: types.VirtualMachineSummary{
			Config: types.VirtualMachineConfigSummary{
				Name:         "prod-web-01",
				InstanceUuid: "50284e31-8e01-4474-9540-1e149363065a",
				Uuid:         "42284e31-8e01-4474-9540-1e149363065a",
				NumCpu:       4,
				MemorySizeMB: 8192,
			},
			Runtime: types.VirtualMachineRuntimeInfo{
				PowerState:      types.VirtualMachinePowerStatePoweredOn,
				ConnectionState: types.VirtualMachineConnectionStateConnected,
			},
		},
		Guest: &types.GuestInfo{
			HostName:      "web01.corp.local",
			GuestFullName: "Ubuntu Linux (64-bit)",
			IpAddress:     "192.168.1.50",
			Net: []types.GuestNicInfo{
				{
					DeviceConfigId: 4000,
					MacAddress:     "00:50:56:A1:00:01",
					Network:        "network-10",
					IpAddress:      []string{"192.168.1.50", "2001:db8::1"},
				},
			},
		},
	}

	record := collector.NormalizeVM("vc-test-01", vm, snap)

	if record.MoRef != "vm-100" {
		t.Errorf("expected MoRef vm-100, got %s", record.MoRef)
	}
	if record.InstanceUUID != "50284e31-8e01-4474-9540-1e149363065a" {
		t.Errorf("expected instance uuid, got %s", record.InstanceUUID)
	}
	if record.PowerState != "poweredOn" {
		t.Errorf("expected power state poweredOn, got %s", record.PowerState)
	}
	if record.CPUCores != 4 || record.MemoryMB != 8192 {
		t.Errorf("expected 4 cores and 8192MB, got %d cores and %d MB", record.CPUCores, record.MemoryMB)
	}
	if len(record.NetworkInterfaces) != 2 {
		t.Fatalf("expected 2 IP entries for dual-stack NIC, got %d", len(record.NetworkInterfaces))
	}
	if record.NetworkInterfaces[0].MacAddress != "00:50:56:a1:00:01" {
		t.Errorf("expected lowercase mac 00:50:56:a1:00:01, got %s", record.NetworkInterfaces[0].MacAddress)
	}
	if record.NetworkInterfaces[0].NetworkID != "net-uuid-10" {
		t.Errorf("expected resolved network ID net-uuid-10, got %s", record.NetworkInterfaces[0].NetworkID)
	}
}

func TestNormalizeVMFallbackToHardwareDevices(t *testing.T) {
	snap := collector.NewEmptySnapshot("vc-test-01")
	snap.NetworkIDByMoRef["network-20"] = "net-uuid-20"

	// VM without VMware Tools / powered off: Guest is empty or nil
	vm := mo.VirtualMachine{
		ManagedEntity: mo.ManagedEntity{
			ExtensibleManagedObject: mo.ExtensibleManagedObject{
				Self: types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-200"},
			},
			Name: "powered-off-vm",
		},
		Summary: types.VirtualMachineSummary{
			Config: types.VirtualMachineConfigSummary{
				Name:         "powered-off-vm",
				InstanceUuid: "60284e31-8e01-4474-9540-1e149363065b",
				NumCpu:       2,
				MemorySizeMB: 4096,
			},
			Runtime: types.VirtualMachineRuntimeInfo{
				PowerState: types.VirtualMachinePowerStatePoweredOff,
			},
		},
		Guest: &types.GuestInfo{
			// guest.net is empty!
			Net: []types.GuestNicInfo{},
		},
		Config: &types.VirtualMachineConfigInfo{
			Hardware: types.VirtualHardware{
				NumCPU:   2,
				MemoryMB: 4096,
				Device: []types.BaseVirtualDevice{
					&types.VirtualVmxnet3{
						VirtualEthernetCard: types.VirtualEthernetCard{
							VirtualDevice: types.VirtualDevice{
								Key: 4000,
								Backing: &types.VirtualEthernetCardNetworkBackingInfo{
									VirtualDeviceDeviceBackingInfo: types.VirtualDeviceDeviceBackingInfo{
										DeviceName: "VM Network",
									},
									Network: &types.ManagedObjectReference{Type: "Network", Value: "network-20"},
								},
							},
							MacAddress: "00:50:56:B2:00:99",
						},
					},
				},
			},
		},
	}

	record := collector.NormalizeVM("vc-test-01", vm, snap)

	if record.PowerState != "poweredOff" {
		t.Errorf("expected poweredOff, got %s", record.PowerState)
	}
	if len(record.NetworkInterfaces) != 1 {
		t.Fatalf("expected 1 NIC extracted from hardware devices fallback, got %d", len(record.NetworkInterfaces))
	}
	nic := record.NetworkInterfaces[0]
	if nic.MacAddress != "00:50:56:b2:00:99" {
		t.Errorf("expected fallback MAC 00:50:56:b2:00:99, got %s", nic.MacAddress)
	}
	if nic.NetworkID != "net-uuid-20" {
		t.Errorf("expected resolved network ID net-uuid-20, got %s", nic.NetworkID)
	}
	if nic.IPAddress != "" {
		t.Errorf("expected empty IP address in hardware fallback mode, got %s", nic.IPAddress)
	}
}

func TestNormalizeVMCustomAttributes(t *testing.T) {
	snap := collector.NewEmptySnapshot("vc-test-01")
	snap.CustomFieldsByKey[101] = "Environment"
	snap.CustomFieldsByKey[102] = "CostCenter"
	snap.CustomFieldsByKey[103] = "Owner"

	t.Run("multiple attributes with key-to-name resolution", func(t *testing.T) {
		vm := mo.VirtualMachine{
			ManagedEntity: mo.ManagedEntity{
				ExtensibleManagedObject: mo.ExtensibleManagedObject{
					Self: types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-301"},
				},
				CustomValue: []types.BaseCustomFieldValue{
					&types.CustomFieldStringValue{
						CustomFieldValue: types.CustomFieldValue{Key: 101},
						Value:            "Production",
					},
					&types.CustomFieldStringValue{
						CustomFieldValue: types.CustomFieldValue{Key: 102},
						Value:            "Engineering-90210",
					},
				},
				Name: "vm-with-attrs",
			},
		}

		rec := collector.NormalizeVM("vc-test-01", vm, snap)
		if len(rec.CustomAttributes) != 2 {
			t.Fatalf("expected 2 custom attributes, got %d", len(rec.CustomAttributes))
		}
		if rec.CustomAttributes[0].Name != "Environment" || rec.CustomAttributes[0].Value != "Production" {
			t.Errorf("attr 0 mismatch: %+v", rec.CustomAttributes[0])
		}
		if rec.CustomAttributes[1].Name != "CostCenter" || rec.CustomAttributes[1].Value != "Engineering-90210" {
			t.Errorf("attr 1 mismatch: %+v", rec.CustomAttributes[1])
		}
	})

	t.Run("unset empty values are skipped", func(t *testing.T) {
		vm := mo.VirtualMachine{
			ManagedEntity: mo.ManagedEntity{
				ExtensibleManagedObject: mo.ExtensibleManagedObject{
					Self: types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-302"},
				},
				CustomValue: []types.BaseCustomFieldValue{
					&types.CustomFieldStringValue{
						CustomFieldValue: types.CustomFieldValue{Key: 101},
						Value:            "Staging",
					},
					&types.CustomFieldStringValue{
						CustomFieldValue: types.CustomFieldValue{Key: 102},
						Value:            "", // Unset in vSphere
					},
				},
				Name: "vm-unset-attr",
			},
		}

		rec := collector.NormalizeVM("vc-test-01", vm, snap)
		if len(rec.CustomAttributes) != 1 {
			t.Fatalf("expected 1 custom attribute (empty value omitted), got %d", len(rec.CustomAttributes))
		}
		if rec.CustomAttributes[0].Name != "Environment" || rec.CustomAttributes[0].Value != "Staging" {
			t.Errorf("expected Environment=Staging, got %+v", rec.CustomAttributes[0])
		}
	})

	t.Run("unknown key not in CustomFieldsByKey is ignored", func(t *testing.T) {
		vm := mo.VirtualMachine{
			ManagedEntity: mo.ManagedEntity{
				ExtensibleManagedObject: mo.ExtensibleManagedObject{
					Self: types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-303"},
				},
				CustomValue: []types.BaseCustomFieldValue{
					&types.CustomFieldStringValue{
						CustomFieldValue: types.CustomFieldValue{Key: 99999}, // Unknown key
						Value:            "orphaned-attr-value",
					},
					&types.CustomFieldStringValue{
						CustomFieldValue: types.CustomFieldValue{Key: 103},
						Value:            "devops-team",
					},
				},
				Name: "vm-unknown-attr-key",
			},
		}

		rec := collector.NormalizeVM("vc-test-01", vm, snap)
		if len(rec.CustomAttributes) != 1 {
			t.Fatalf("expected 1 custom attribute, got %d", len(rec.CustomAttributes))
		}
		if rec.CustomAttributes[0].Name != "Owner" || rec.CustomAttributes[0].Value != "devops-team" {
			t.Errorf("expected Owner=devops-team, got %+v", rec.CustomAttributes[0])
		}
	})

	t.Run("vm with no custom values returns empty slice", func(t *testing.T) {
		vm := mo.VirtualMachine{
			ManagedEntity: mo.ManagedEntity{
				ExtensibleManagedObject: mo.ExtensibleManagedObject{
					Self: types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-304"},
				},
				CustomValue: nil,
				Name:        "vm-no-attrs",
			},
		}

		rec := collector.NormalizeVM("vc-test-01", vm, snap)
		if len(rec.CustomAttributes) != 0 {
			t.Errorf("expected 0 custom attributes, got %d", len(rec.CustomAttributes))
		}
	})
}
