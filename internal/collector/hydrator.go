package collector

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/infrastructure/sims/internal/domain"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/methods"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
)

// HydratorConfig configures batch sizes and traversal options for inventory collection.
type HydratorConfig struct {
	MaxObjects int // Default: 500
}

// Hydrator executes full snapshot hydration of vSphere topology and virtual machine inventory.
type Hydrator struct {
	session *vsphere.SessionManager
	cfg     HydratorConfig
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

	// Resolve folder ltree paths: traverse up to root
	for moref, folder := range snap.Folders {
		path := h.buildFolderPath(moref, snap.Folders)
		snap.FolderPathByMoRef[moref] = path
		folder.Path = path
		snap.Folders[moref] = folder
		snap.ByMoRef[moref] = folder
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
			"config",
			"guest",
			"runtime",
			"parent",
			"datastore",
			"network",
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

	res, err := methods.RetrievePropertiesEx(ctx, client.Client, retrieveReq)
	if err != nil {
		return fmt.Errorf("initial RetrievePropertiesEx failed: %w", err)
	}

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

		continueRes, err := methods.ContinueRetrievePropertiesEx(ctx, client.Client, continueReq)
		if err != nil {
			return fmt.Errorf("ContinueRetrievePropertiesEx failed with token %s: %w", retrievalContinuationToken, err)
		}
		res = &types.RetrievePropertiesExResponse{Returnval: &continueRes.Returnval}
	}

	return nil
}
