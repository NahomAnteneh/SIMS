package collector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// HydratorConfig configures batch sizes and traversal options for inventory collection.
type HydratorConfig struct {
	MaxObjects int // Default: 500
}

// Hydrator executes full snapshot hydration of vSphere topology and virtual machine inventory.
type Hydrator struct {
	session    *vsphere.SessionManager
	cfg        HydratorConfig
	tagFetcher TagFetcher
}

// NewHydrator constructs an inventory hydrator with verified default configuration.
func NewHydrator(sm *vsphere.SessionManager, cfg HydratorConfig) *Hydrator {
	if cfg.MaxObjects <= 0 {
		cfg.MaxObjects = 500
	}
	return &Hydrator{
		session: sm,
		cfg:     cfg,
	}
}

// SetTagFetcher assigns an optional TagFetcher for enriching VMs with vSphere tags.
func (h *Hydrator) SetTagFetcher(tf TagFetcher) {
	h.tagFetcher = tf
}

// TagFetcher returns the configured TagFetcher, if any.
func (h *Hydrator) TagFetcher() TagFetcher {
	return h.tagFetcher
}

// HydrateTopology retrieves all structural topology objects and builds an immutable TopologySnapshot.
func (h *Hydrator) HydrateTopology(ctx context.Context, vcenterID string) (*TopologySnapshot, error) {
	client := h.session.Client()
	if client == nil {
		return nil, vsphere.ErrNotConnected
	}

	snap := NewEmptySnapshot(vcenterID)

	viewMgr := view.NewManager(client.Client)
	rootFolder := client.ServiceContent.RootFolder

	// Entity types needed for full topology tree
	kinds := []string{
		"Datacenter",
		"Folder",
		"ComputeResource",
		"ClusterComputeResource",
		"HostSystem",
		"Datastore",
		"Network",
		"DistributedVirtualPortgroup",
		"VmwareDistributedVirtualSwitch",
	}

	cv, err := viewMgr.CreateContainerView(ctx, rootFolder, kinds, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create topology ContainerView: %w", err)
	}

	// CRITICAL: Guarantee ContainerView destruction on EVERY exit path
	defer func() {
		destroyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = cv.Destroy(destroyCtx)
	}()

	var entities []mo.ManagedEntity
	err = cv.Retrieve(ctx, kinds, []string{"name", "parent"}, &entities)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve topology entities: %w", err)
	}

	// Build initial MoRef -> Node map
	for _, e := range entities {
		ref := e.Reference()
		node := TopologyNode{
			VCenterID: vcenterID,
			MoRef:     ref.Value,
			Name:      e.Name,
		}
		if e.Parent != nil {
			node.ParentID = e.Parent.Value
		}

		snap.ByMoRef[ref.Value] = node

		switch ref.Type {
		case "Datacenter":
			snap.Datacenters[ref.Value] = node
		case "Datastore":
			snap.Datastores[ref.Value] = node
		case "Network":
			snap.Networks[ref.Value] = node
		case "DistributedVirtualPortgroup":
			snap.Portgroups[ref.Value] = node
		case "Folder":
			snap.Folders[ref.Value] = node
		case "HostSystem":
			snap.Hosts[ref.Value] = node
		case "ClusterComputeResource":
			snap.Clusters[ref.Value] = node
		}
	}

	// Retrieve Datacenter entities to capture directly attached datastore and network lists
	var datacenters []mo.Datacenter
	if err := cv.Retrieve(ctx, []string{"Datacenter"}, []string{"name", "datastore", "network"}, &datacenters); err == nil {
		for _, dc := range datacenters {
			dcRef := dc.Reference().Value
			for _, dsRef := range dc.Datastore {
				snap.DatacenterIDByMoRef[dsRef.Value] = dcRef
			}
			for _, netRef := range dc.Network {
				snap.DatacenterIDByMoRef[netRef.Value] = dcRef
			}
		}
	}

	// Retrieve Datastores with summary sizing information and true names
	var datastores []mo.Datastore
	if err := cv.Retrieve(ctx, []string{"Datastore"}, []string{"name", "parent", "summary"}, &datastores); err == nil {
		for _, ds := range datastores {
			mRef := ds.Reference().Value
			node := snap.Datastores[mRef]
			if ds.Name != "" {
				node.Name = ds.Name
				snap.DatastoreByName[ds.Name] = node
			}
			snap.Datastores[mRef] = node
			snap.ByMoRef[mRef] = node
			snap.DatastoreInfoByMoRef[mRef] = DatastoreInfo{
				Capacity:   ds.Summary.Capacity,
				FreeSpace:  ds.Summary.FreeSpace,
				Type:       ds.Summary.Type,
				Accessible: ds.Summary.Accessible,
			}
		}
	}

	// Extract VLAN specifications, true names, and switch references from DistributedVirtualPortgroup
	var dvpgs []mo.DistributedVirtualPortgroup
	if err := cv.Retrieve(ctx, []string{"DistributedVirtualPortgroup"}, []string{"name", "config.defaultPortConfig", "config.distributedVirtualSwitch"}, &dvpgs); err == nil {
		for _, dvpg := range dvpgs {
			mRef := dvpg.Reference().Value
			pgNode := snap.Portgroups[mRef]
			if dvpg.Name != "" {
				pgNode.Name = dvpg.Name
			}
			if dvpg.Config.DistributedVirtualSwitch != nil {
				pgNode.SwitchMoRef = dvpg.Config.DistributedVirtualSwitch.Value
			}
			var vlanSpec string
			if setting, ok := dvpg.Config.DefaultPortConfig.(*types.VMwareDVSPortSetting); ok && setting != nil && setting.Vlan != nil {
				switch vs := setting.Vlan.(type) {
				case *types.VmwareDistributedVirtualSwitchVlanIdSpec:
					if vs.VlanId > 0 && vs.VlanId <= 4094 {
						vlanSpec = fmt.Sprintf("%d", vs.VlanId)
					}
				case *types.VmwareDistributedVirtualSwitchPvlanSpec:
					if vs.PvlanId > 0 && vs.PvlanId <= 4094 {
						vlanSpec = fmt.Sprintf("%d", vs.PvlanId)
					}
				case *types.VmwareDistributedVirtualSwitchTrunkVlanSpec:
					var parts []string
					for _, r := range vs.VlanId {
						if r.Start == r.End {
							parts = append(parts, fmt.Sprintf("%d", r.Start))
						} else {
							parts = append(parts, fmt.Sprintf("%d-%d", r.Start, r.End))
						}
					}
					vlanSpec = strings.Join(parts, ", ")
				}
			}
			if vlanSpec != "" {
				pgNode.VLANSpec = vlanSpec
			}
			snap.Portgroups[mRef] = pgNode
			snap.ByMoRef[mRef] = pgNode
			if pgNode.Name != "" {
				snap.PortgroupByName[pgNode.Name] = pgNode
			}
		}
	}

	// Retrieve Distributed Virtual Switches with true names
	var switches []mo.VmwareDistributedVirtualSwitch
	if err := cv.Retrieve(ctx, []string{"VmwareDistributedVirtualSwitch", "DistributedVirtualSwitch"}, []string{"name", "parent"}, &switches); err == nil {
		for _, sw := range switches {
			mRef := sw.Reference().Value
			node := snap.ByMoRef[mRef]
			node.VCenterID = vcenterID
			node.MoRef = mRef
			if sw.Name != "" {
				node.Name = sw.Name
			}
			if sw.Parent != nil {
				node.ParentID = sw.Parent.Value
			}
			snap.ByMoRef[mRef] = node
		}
	}

	// Retrieve Standard Networks with true names
	var networks []mo.Network
	if err := cv.Retrieve(ctx, []string{"Network"}, []string{"name"}, &networks); err == nil {
		for _, net := range networks {
			mRef := net.Reference().Value
			netNode := snap.Networks[mRef]
			if net.Name != "" {
				netNode.Name = net.Name
			}
			snap.Networks[mRef] = netNode
			snap.ByMoRef[mRef] = netNode
			if netNode.Name != "" {
				snap.NetworkByName[netNode.Name] = netNode
			}
		}
	}

	// Extract standard switch portgroup VLANs from HostSystem network configs
	var hostSystems []mo.HostSystem
	if err := cv.Retrieve(ctx, []string{"HostSystem"}, []string{"config.network"}, &hostSystems); err == nil {
		for _, hs := range hostSystems {
			if hs.Config != nil && hs.Config.Network != nil {
				for _, pg := range hs.Config.Network.Portgroup {
					vlanSpec := ""
					if pg.Spec.VlanId == 4095 {
						vlanSpec = "1-4094"
					} else if pg.Spec.VlanId > 0 && pg.Spec.VlanId <= 4094 {
						vlanSpec = fmt.Sprintf("%d", pg.Spec.VlanId)
					}
					if vlanSpec != "" {
						for mRef, netNode := range snap.Networks {
							if netNode.Name == pg.Spec.Name && netNode.VLANSpec == "" {
								netNode.VLANSpec = vlanSpec
								snap.Networks[mRef] = netNode
								snap.ByMoRef[mRef] = netNode
								snap.NetworkByName[netNode.Name] = netNode
							}
						}
						for mRef, pgNode := range snap.Portgroups {
							if pgNode.Name == pg.Spec.Name && pgNode.VLANSpec == "" {
								pgNode.VLANSpec = vlanSpec
								snap.Portgroups[mRef] = pgNode
								snap.ByMoRef[mRef] = pgNode
								snap.PortgroupByName[pgNode.Name] = pgNode
							}
						}
					}
				}
			}
		}
	}

	// Fallback Datacenter resolution: if vCenter has only 1 datacenter, ensure all entities link to it
	if len(snap.Datacenters) == 1 {
		var singleDCMoRef string
		for dcMoRef := range snap.Datacenters {
			singleDCMoRef = dcMoRef
			break
		}
		for mRef := range snap.Datastores {
			if snap.DatacenterIDByMoRef[mRef] == "" {
				snap.DatacenterIDByMoRef[mRef] = singleDCMoRef
			}
		}
		for mRef := range snap.Networks {
			if snap.DatacenterIDByMoRef[mRef] == "" {
				snap.DatacenterIDByMoRef[mRef] = singleDCMoRef
			}
		}
		for mRef := range snap.Portgroups {
			if snap.DatacenterIDByMoRef[mRef] == "" {
				snap.DatacenterIDByMoRef[mRef] = singleDCMoRef
			}
		}
	}

	// Resolve folder ltree paths: traverse up to root
	for moref, folder := range snap.Folders {
		path := h.buildFolderPath(moref, snap.Folders)
		snap.FolderPathByMoRef[moref] = path
		folder.Path = path
		snap.Folders[moref] = folder
		snap.ByMoRef[moref] = folder
	}

	// Populate CustomFieldsManager definitions if available
	if client.ServiceContent.CustomFieldsManager != nil {
		cfm, err := object.GetCustomFieldsManager(client.Client)
		if err == nil && cfm != nil {
			fieldDefs, err := cfm.Field(ctx)
			if err == nil {
				for _, f := range fieldDefs {
					snap.CustomFieldsByKey[f.Key] = f.Name
				}
			}
		}
	}

	return snap, nil
}

// buildFolderPath constructs a dot-delimited ltree path by traversing folder parents.
func (h *Hydrator) buildFolderPath(moref string, folders map[string]TopologyNode) string {
	var segments []string
	curr := moref
	visited := make(map[string]bool)

	for curr != "" && !visited[curr] {
		visited[curr] = true
		f, ok := folders[curr]
		if !ok {
			break
		}
		// Sanitize name for ltree label (alphanumeric and underscore only)
		clean := sanitizeLtreeLabel(f.Name)
		if clean != "" {
			segments = append([]string{clean}, segments...)
		}
		curr = f.ParentID
	}

	if len(segments) == 0 {
		return "root"
	}
	return strings.Join(segments, ".")
}

// sanitizeLtreeLabel formats arbitrary folder names into valid PostgreSQL ltree labels.
func sanitizeLtreeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	res := strings.Trim(b.String(), "_")
	if res == "" {
		return "folder"
	}
	if len(res) > 250 {
		res = res[:250]
	}
	return res
}

// HydrateInventory retrieves all virtual machines in bounded pages using ContainerView,
// PropertyFilterSpec, TraversalSpec, and RetrievePropertiesEx/ContinueRetrievePropertiesEx.
func (h *Hydrator) HydrateInventory(
	ctx context.Context,
	vcenterID string,
	snap *TopologySnapshot,
	onBatch func([]domain.VMRecord) error,
) error {
	client := h.session.Client()
	if client == nil {
		return vsphere.ErrNotConnected
	}

	viewMgr := view.NewManager(client.Client)
	rootFolder := client.ServiceContent.RootFolder

	cv, err := viewMgr.CreateContainerView(ctx, rootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		return fmt.Errorf("failed to create inventory ContainerView: %w", err)
	}

	// CRITICAL: Guarantee ContainerView destruction on EVERY exit path (success, error, panic, context cancel)
	defer func() {
		destroyCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = cv.Destroy(destroyCtx)
	}()

	// Build PropertyFilterSpec with TraversalSpec
	ts := &types.TraversalSpec{
		SelectionSpec: types.SelectionSpec{Name: "traverseContainerView"},
		Type:          "ContainerView",
		Path:          "view",
		Skip:          types.NewBool(false),
	}

	ps := types.PropertySpec{
		Type: "VirtualMachine",
		PathSet: []string{
			"name",
			"summary.config",
			"summary.runtime",
			"summary.storage",
			"config",
			"config.hardware.device",
			"guest",
			"runtime",
			"parent",
			"datastore",
			"network",
			"customValue",
		},
	}

	pfs := types.PropertyFilterSpec{
		ObjectSet: []types.ObjectSpec{
			{
				Obj:       cv.Reference(),
				SelectSet: []types.BaseSelectionSpec{ts},
				Skip:      types.NewBool(false),
			},
		},
		PropSet: []types.PropertySpec{ps},
	}

	collectorRef := property.DefaultCollector(client.Client).Reference()

	retrieveReq := &types.RetrievePropertiesEx{
		This:    collectorRef,
		SpecSet: []types.PropertyFilterSpec{pfs},
		Options: types.RetrieveOptions{
			MaxObjects: int32(h.cfg.MaxObjects),
		},
	}

	_, initSpan := telemetry.StartSpan(ctx, "vCenter.RetrievePropertiesEx",
		trace.WithAttributes(
			attribute.String("vcenter.id", vcenterID),
			attribute.Int("vcenter.max_objects", h.cfg.MaxObjects),
		),
	)
	res, err := methods.RetrievePropertiesEx(ctx, client.Client, retrieveReq)
	if err != nil {
		telemetry.RecordSafeError(initSpan, err)
		initSpan.End()
		return fmt.Errorf("initial RetrievePropertiesEx failed: %w", err)
	}
	initSpan.End()

	for res != nil && res.Returnval != nil {
		objects := res.Returnval.Objects
		if len(objects) > 0 {
			var vms []mo.VirtualMachine
			err := mo.LoadObjectContent(objects, &vms)
			if err != nil {
				return fmt.Errorf("failed to load ObjectContent to VirtualMachine DTOs: %w", err)
			}

			records := make([]domain.VMRecord, len(vms))
			for i, v := range vms {
				records[i] = NormalizeVM(vcenterID, v, snap)
			}

			// Enrich with tags via TagFetcher if configured
			if h.tagFetcher != nil {
				refs := make([]types.ManagedObjectReference, len(vms))
				for i, v := range vms {
					refs[i] = v.Reference()
				}
				tagMap, tErr := h.tagFetcher.FetchTagsForObjects(ctx, refs)
				if tErr != nil {
					// Degradation: tagging REST failure must NOT fail inventory hydration
				} else if tagMap != nil {
					for i, v := range vms {
						if vmTags, ok := tagMap[v.Reference().Value]; ok {
							records[i].Tags = vmTags
						}
					}
				}
			}

			if err := onBatch(records); err != nil {
				return fmt.Errorf("onBatch callback failed: %w", err)
			}
		}

		// Continuation token is separate from PropertyCollector update version
		retrievalContinuationToken := res.Returnval.Token
		if retrievalContinuationToken == "" {
			break
		}

		// Check context before next page
		if err := ctx.Err(); err != nil {
			// Best-effort cancel continuation token
			cancelReq := &types.CancelRetrievePropertiesEx{
				This:  collectorRef,
				Token: retrievalContinuationToken,
			}
			_, _ = methods.CancelRetrievePropertiesEx(context.Background(), client.Client, cancelReq)
			return err
		}

		continueReq := &types.ContinueRetrievePropertiesEx{
			This:  collectorRef,
			Token: retrievalContinuationToken,
		}

		_, contSpan := telemetry.StartSpan(ctx, "vCenter.ContinueRetrievePropertiesEx",
			trace.WithAttributes(
				attribute.String("vcenter.id", vcenterID),
			),
		)
		continueRes, err := methods.ContinueRetrievePropertiesEx(ctx, client.Client, continueReq)
		if err != nil {
			telemetry.RecordSafeError(contSpan, err)
			contSpan.End()
			return fmt.Errorf("ContinueRetrievePropertiesEx failed with token %s: %w", retrievalContinuationToken, err)
		}
		contSpan.End()
		res = &types.RetrievePropertiesExResponse{Returnval: &continueRes.Returnval}
	}

	return nil
}
