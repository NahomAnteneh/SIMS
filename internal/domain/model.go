package domain

import "time"

// VMRecord represents a normalized virtual machine entity in the core domain.
// It is strictly decoupled from govmomi vSphere DTOs and database-specific models.
type VMRecord struct {
	ID                string
	VCenterID         string
	MoRef             string
	InstanceUUID      string
	BiosUUID          string
	Name              string
	PowerState        string
	ConnectionState   string
	CPUCores          int32
	MemoryMB          int64
	FolderID          string
	FolderPath        string
	HostID            string
	ClusterID         string
	DatacenterID      string
	GuestOS           string
	GuestHostName     string
	GuestIPAddress    string
	Annotation        string
	DatastoreIDs      []string
	DatastoreMoRefs   []string
	NetworkInterfaces []NetworkInterfaceRecord
	DeletedAt         *time.Time
	UpdatedAt         time.Time
}

// NetworkInterfaceRecord represents a normalized virtual NIC attached to a VM.
type NetworkInterfaceRecord struct {
	DeviceKey      int32
	MacAddress     string
	IPAddress      string
	NetworkID      string
	PortgroupID    string
	NetworkMoRef   string
	PortgroupMoRef string
	VLANSpec       string
}
