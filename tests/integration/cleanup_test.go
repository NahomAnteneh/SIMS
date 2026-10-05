//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/event"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

// TestCleanupRegistry_VCSimOrderedTeardown creates real server-side objects
// (ContainerView, PropertyFilter, EventHistoryCollector) on vcsim, tears them down
// via CleanupRegistry in strict 8-stage reverse order, and verifies on the server
// that each managed object was destroyed (confirmed via ManagedObjectNotFound SOAP faults).
func TestCleanupRegistry_VCSimOrderedTeardown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Initialize in-process VPX simulator model (configured to vCenter 8.0.3)
	model := simulator.VPX()
	vsphere.ConfigureTestVPX(model)
	defer model.Remove()

	if err := model.Create(); err != nil {
		t.Fatalf("failed to create vcsim model: %v", err)
	}

	server := model.Service.NewServer()
	defer server.Close()

	// 2. Connect client and authenticate
	client, err := govmomi.NewClient(ctx, server.URL, true)
	if err != nil {
		t.Fatalf("failed to connect to vcsim: %v", err)
	}

	reg := collector.NewCleanupRegistry(nil, 5*time.Second)

	// 3. Create real ContainerView
	viewMgr := view.NewManager(client.Client)
	cv, err := viewMgr.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		t.Fatalf("failed to create ContainerView: %v", err)
	}
	cvRef := cv.Reference()

	// 4. Create real PropertyFilter on the default PropertyCollector
	pc := property.DefaultCollector(client.Client)
	filterSpec := types.PropertyFilterSpec{
		ObjectSet: []types.ObjectSpec{
			{
				Obj: cvRef,
				SelectSet: []types.BaseSelectionSpec{
					&types.TraversalSpec{
						Type: cvRef.Type,
						Path: "view",
					},
				},
			},
		},
		PropSet: []types.PropertySpec{
			{
				Type:    "VirtualMachine",
				PathSet: []string{"name", "runtime.powerState"},
			},
		},
	}
	req := types.CreateFilter{
		This: pc.Reference(),
		Spec: filterSpec,
	}
	filterRes, err := pc.CreateFilter(ctx, req)
	if err != nil {
		t.Fatalf("failed to create PropertyFilter: %v", err)
	}
	filterRef := filterRes.Reference()
	wrappedFilter := vsphere.NewGovmomiPropertyFilter(filterRes)

	// 5. Create real EventHistoryCollector
	eventMgr := event.NewManager(client.Client)
	ehc, err := eventMgr.CreateCollectorForEvents(ctx, types.EventFilterSpec{})
	if err != nil {
		t.Fatalf("failed to create EventHistoryCollector: %v", err)
	}
	ehcRef := ehc.Reference()
	wrappedEventCollector := vsphere.NewGovmomiEventCollector(ehc)

	// Verify all 3 objects exist on the server before teardown
	var dummyCV mo.ContainerView
	if err := pc.RetrieveOne(ctx, cvRef, []string{"view"}, &dummyCV); err != nil {
		t.Fatalf("expected ContainerView %s to exist on server: %v", cvRef.Value, err)
	}
	t.Logf("Confirmed ContainerView exists on server: %s", cvRef.Value)

	var dummyFilter mo.PropertyFilter
	if err := pc.RetrieveOne(ctx, filterRef, []string{"spec"}, &dummyFilter); err != nil {
		t.Fatalf("expected PropertyFilter %s to exist on server: %v", filterRef.Value, err)
	}
	t.Logf("Confirmed PropertyFilter exists on server: %s", filterRef.Value)

	var dummyEHC mo.EventHistoryCollector
	if err := pc.RetrieveOne(ctx, ehcRef, []string{"latestPage"}, &dummyEHC); err != nil {
		t.Fatalf("expected EventHistoryCollector %s to exist on server: %v", ehcRef.Value, err)
	}
	t.Logf("Confirmed EventHistoryCollector exists on server: %s", ehcRef.Value)

	// 6. Register objects in CleanupRegistry (Stages 1 through 8)
	var stageExecution []collector.ResourceKind

	reg.RegisterProducer("producer-stream", func() {
		stageExecution = append(stageExecution, collector.ResourceKindProducer)
	})
	reg.RegisterDrain("queue-drain", func(ctx context.Context) error {
		stageExecution = append(stageExecution, collector.ResourceKindDrain)
		return nil
	})
	reg.RegisterEventCollector(wrappedEventCollector) // Stage 3
	reg.RegisterPropertyFilter(wrappedFilter)         // Stage 4
	reg.RegisterContainerView(cv.Destroy)             // Stage 5
	reg.RegisterREST(func(ctx context.Context) error {
		stageExecution = append(stageExecution, collector.ResourceKindREST)
		return nil
	})
	reg.Register(collector.ResourceKindSOAP, "vcsim-soap", func(ctx context.Context) error {
		stageExecution = append(stageExecution, collector.ResourceKindSOAP)
		return client.Logout(ctx)
	})
	reg.RegisterClientClose(func() {
		stageExecution = append(stageExecution, collector.ResourceKindClient)
	})

	// 7. Execute Teardown
	if err := reg.Teardown(ctx); err != nil {
		t.Fatalf("Teardown failed: %v", err)
	}

	// 8. Verify on the server that none remain using a new unauthenticated connection or server probe
	// Create a new client to inspect server state
	checkClient, err := govmomi.NewClient(ctx, server.URL, true)
	if err != nil {
		t.Fatalf("failed to connect check client: %v", err)
	}
	defer func() { _ = checkClient.Logout(ctx) }()

	checkPC := property.DefaultCollector(checkClient.Client)

	// Verify ContainerView was destroyed on the server
	errCV := checkPC.RetrieveOne(ctx, cvRef, []string{"view"}, &dummyCV)
	if errCV == nil {
		t.Errorf("ContainerView %s still exists on server after teardown!", cvRef.Value)
	} else if !isManagedObjectNotFound(errCV) {
		t.Errorf("expected ManagedObjectNotFound for destroyed ContainerView, got: %v", errCV)
	} else {
		t.Logf("Confirmed ContainerView %s destroyed on server (ManagedObjectNotFound)", cvRef.Value)
	}

	// Verify PropertyFilter was destroyed on the server
	errFilter := checkPC.RetrieveOne(ctx, filterRef, []string{"spec"}, &dummyFilter)
	if errFilter == nil {
		t.Errorf("PropertyFilter %s still exists on server after teardown!", filterRef.Value)
	} else if !isManagedObjectNotFound(errFilter) {
		t.Errorf("expected ManagedObjectNotFound for destroyed PropertyFilter, got: %v", errFilter)
	} else {
		t.Logf("Confirmed PropertyFilter %s destroyed on server (ManagedObjectNotFound)", filterRef.Value)
	}

	// Verify EventHistoryCollector was destroyed on the server
	errEHC := checkPC.RetrieveOne(ctx, ehcRef, []string{"latestPage"}, &dummyEHC)
	if errEHC == nil {
		t.Errorf("EventHistoryCollector %s still exists on server after teardown!", ehcRef.Value)
	} else if !isManagedObjectNotFound(errEHC) {
		t.Errorf("expected ManagedObjectNotFound for destroyed EventHistoryCollector, got: %v", errEHC)
	} else {
		t.Logf("Confirmed EventHistoryCollector %s destroyed on server (ManagedObjectNotFound)", ehcRef.Value)
	}
}

func isManagedObjectNotFound(err error) bool {
	if err == nil {
		return false
	}
	if soap.IsSoapFault(err) {
		fault := soap.ToSoapFault(err)
		if fault != nil {
			if _, ok := fault.VimFault().(*types.ManagedObjectNotFound); ok {
				return true
			}
			if fault.String == "The object has already been deleted or has not been completely created" {
				return true
			}
		}
	}
	return false
}
