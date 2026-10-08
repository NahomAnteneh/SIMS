package collector

import (
	"sync/atomic"
)

// TopologyNode represents a resolved topology entity in the memory cache.
type TopologyNode struct {
	ID          string
	VCenterID   string
	MoRef       string
	Name        string
	ParentID    string
	Path        string
	VLANSpec    string
	SwitchMoRef string
}

// DatastoreInfo contains runtime and sizing metadata for a datastore.
type DatastoreInfo struct {
	Capacity   int64
	FreeSpace  int64
	Type       string
	Accessible bool
}

// TopologySnapshot is an immutable cache of vSphere topology objects and ID mappings.
type TopologySnapshot struct {
	VCenterID string

	// General lookups
	ByMoRef map[string]TopologyNode
	ByID    map[string]TopologyNode

	// Specialized lookups
	Datacenters map[string]TopologyNode
	Datastores  map[string]TopologyNode
	Networks    map[string]TopologyNode
	Portgroups  map[string]TopologyNode
	Folders     map[string]TopologyNode
	Hosts       map[string]TopologyNode
	Clusters    map[string]TopologyNode

	// Datastore runtime metadata
	DatastoreInfoByMoRef map[string]DatastoreInfo

	// Fast ID resolutions for VM normalization
	DatastoreIDByMoRef  map[string]string
	NetworkIDByMoRef    map[string]string
	PortgroupIDByMoRef  map[string]string
	NetworkIDByName     map[string]string
	PortgroupIDByName   map[string]string
	NetworkByName       map[string]TopologyNode
	PortgroupByName     map[string]TopologyNode
	FolderIDByMoRef     map[string]string
	FolderPathByMoRef   map[string]string
	HostIDByMoRef       map[string]string
	ClusterIDByMoRef    map[string]string
	DatacenterIDByMoRef map[string]string

	// Custom fields definitions (key -> field name)
	CustomFieldsByKey map[int32]string
}

// NewEmptySnapshot returns an empty, initialized TopologySnapshot.
func NewEmptySnapshot(vcenterID string) *TopologySnapshot {
	return &TopologySnapshot{
		VCenterID:            vcenterID,
		ByMoRef:              make(map[string]TopologyNode),
		ByID:                 make(map[string]TopologyNode),
		Datacenters:          make(map[string]TopologyNode),
		Datastores:           make(map[string]TopologyNode),
		Networks:             make(map[string]TopologyNode),
		Portgroups:           make(map[string]TopologyNode),
		Folders:              make(map[string]TopologyNode),
		Hosts:                make(map[string]TopologyNode),
		Clusters:             make(map[string]TopologyNode),
		DatastoreInfoByMoRef: make(map[string]DatastoreInfo),
		DatastoreIDByMoRef:   make(map[string]string),
		NetworkIDByMoRef:     make(map[string]string),
		PortgroupIDByMoRef:   make(map[string]string),
		NetworkIDByName:      make(map[string]string),
		PortgroupIDByName:    make(map[string]string),
		NetworkByName:        make(map[string]TopologyNode),
		PortgroupByName:      make(map[string]TopologyNode),
		FolderIDByMoRef:      make(map[string]string),
		FolderPathByMoRef:    make(map[string]string),
		HostIDByMoRef:        make(map[string]string),
		ClusterIDByMoRef:     make(map[string]string),
		DatacenterIDByMoRef:  make(map[string]string),
		CustomFieldsByKey:    make(map[int32]string),
	}
}

// TopologyCache provides lock-free, atomic access to the current immutable TopologySnapshot.
type TopologyCache struct {
	snapshot atomic.Pointer[TopologySnapshot]
}

// NewTopologyCache creates a new cache with an initial empty snapshot.
func NewTopologyCache(vcenterID string) *TopologyCache {
	c := &TopologyCache{}
	c.snapshot.Store(NewEmptySnapshot(vcenterID))
	return c
}

// Current returns the current immutable snapshot pointer. Readers never block writers.
func (tc *TopologyCache) Current() *TopologySnapshot {
	return tc.snapshot.Load()
}

// Swap atomically swaps the current snapshot pointer with a freshly constructed one.
func (tc *TopologyCache) Swap(newSnapshot *TopologySnapshot) {
	tc.snapshot.Store(newSnapshot)
}

// ResolveDatacenterMoRef traverses up the topology tree from moref until it encounters a Datacenter.
func (snap *TopologySnapshot) ResolveDatacenterMoRef(moref string) string {
	curr := moref
	visited := make(map[string]bool)
	for curr != "" && !visited[curr] {
		visited[curr] = true
		if _, ok := snap.Datacenters[curr]; ok {
			return curr
		}
		node, ok := snap.ByMoRef[curr]
		if !ok {
			break
		}
		curr = node.ParentID
	}
	return ""
}
