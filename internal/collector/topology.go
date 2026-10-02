package collector

import (
	"sync/atomic"
)

// TopologyNode represents a resolved topology entity in the memory cache.
type TopologyNode struct {
	ID        string
	VCenterID string
	MoRef     string
	Name      string
	ParentID  string
	Path      string
	VLANSpec  string
}

// TopologySnapshot is an immutable cache of vSphere topology objects and ID mappings.
type TopologySnapshot struct {
	VCenterID string

	// General lookups
	ByMoRef map[string]TopologyNode
	ByID    map[string]TopologyNode

	// Specialized lookups
	Datastores map[string]TopologyNode
	Networks   map[string]TopologyNode
	Portgroups map[string]TopologyNode
	Folders    map[string]TopologyNode
	Hosts      map[string]TopologyNode
	Clusters   map[string]TopologyNode

	// Fast ID resolutions for VM normalization
	DatastoreIDByMoRef  map[string]string
	NetworkIDByMoRef    map[string]string
	PortgroupIDByMoRef  map[string]string
	FolderIDByMoRef     map[string]string
	FolderPathByMoRef   map[string]string
	HostIDByMoRef       map[string]string
	ClusterIDByMoRef    map[string]string
	DatacenterIDByMoRef map[string]string
}

// NewEmptySnapshot returns an empty, initialized TopologySnapshot.
func NewEmptySnapshot(vcenterID string) *TopologySnapshot {
	return &TopologySnapshot{
		VCenterID:           vcenterID,
		ByMoRef:             make(map[string]TopologyNode),
		ByID:                make(map[string]TopologyNode),
		Datastores:          make(map[string]TopologyNode),
		Networks:            make(map[string]TopologyNode),
		Portgroups:          make(map[string]TopologyNode),
		Folders:             make(map[string]TopologyNode),
		Hosts:               make(map[string]TopologyNode),
		Clusters:            make(map[string]TopologyNode),
		DatastoreIDByMoRef:  make(map[string]string),
		NetworkIDByMoRef:    make(map[string]string),
		PortgroupIDByMoRef:  make(map[string]string),
		FolderIDByMoRef:     make(map[string]string),
		FolderPathByMoRef:   make(map[string]string),
		HostIDByMoRef:       make(map[string]string),
		ClusterIDByMoRef:    make(map[string]string),
		DatacenterIDByMoRef: make(map[string]string),
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
