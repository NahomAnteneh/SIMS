package collector_test

import (
	"context"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/collector"
	"github.com/infrastructure/sims/internal/telemetry"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vapi/rest"
	_ "github.com/vmware/govmomi/vapi/simulator"
	"github.com/vmware/govmomi/vapi/tags"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

func TestRestTagFetcher_LiveVAPISimulator(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	model := simulator.VPX()
	defer model.Remove()
	if err := model.Create(); err != nil {
		t.Fatalf("failed to create VPX model: %v", err)
	}

	model.Service.RegisterEndpoints = true
	server := model.Service.NewServer()
	defer server.Close()

	// 1. Connect and seed tags via govmomi tags.Manager
	sc := soap.NewClient(server.URL, true)
	vc, err := vim25.NewClient(ctx, sc)
	if err != nil {
		t.Fatalf("failed to create vim25 client: %v", err)
	}

	user := url.UserPassword("user", "pass")
	sm := session.NewManager(vc)
	if err := sm.Login(ctx, user); err != nil {
		t.Fatalf("failed to login to SOAP: %v", err)
	}
	defer func() { _ = sm.Logout(ctx) }()

	restClient := rest.NewClient(vc)
	if err := restClient.Login(ctx, user); err != nil {
		t.Fatalf("failed to login to REST client: %v", err)
	}
	defer func() { _ = restClient.Logout(ctx) }()

	tagMgr := tags.NewManager(restClient)

	// Create categories
	catEnvID, err := tagMgr.CreateCategory(ctx, &tags.Category{
		Name:        "Environment",
		Cardinality: "MULTIPLE",
	})
	if err != nil {
		t.Fatalf("failed to create Environment category: %v", err)
	}

	catCostID, err := tagMgr.CreateCategory(ctx, &tags.Category{
		Name:        "CostCenter",
		Cardinality: "SINGLE",
	})
	if err != nil {
		t.Fatalf("failed to create CostCenter category: %v", err)
	}

	// Create tags
	tagProdID, err := tagMgr.CreateTag(ctx, &tags.Tag{
		Name:       "Production",
		CategoryID: catEnvID,
	})
	if err != nil {
		t.Fatalf("failed to create Production tag: %v", err)
	}

	tagFinID, err := tagMgr.CreateTag(ctx, &tags.Tag{
		Name:       "Finance-90210",
		CategoryID: catCostID,
	})
	if err != nil {
		t.Fatalf("failed to create Finance tag: %v", err)
	}

	// Get all VMs to attach tags
	vManager := view.NewManager(vc)
	cView, err := vManager.CreateContainerView(ctx, vc.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		t.Fatalf("failed to create ContainerView: %v", err)
	}
	defer func() { _ = cView.Destroy(ctx) }()

	var vms []mo.VirtualMachine
	if err := cView.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name"}, &vms); err != nil {
		t.Fatalf("failed to retrieve VMs: %v", err)
	}
	if len(vms) == 0 {
		t.Fatalf("expected at least 1 VM in simulator")
	}

	// Attach tags to first VM
	targetVM := vms[0]
	if err := tagMgr.AttachTag(ctx, tagProdID, targetVM.Reference()); err != nil {
		t.Fatalf("failed to attach Production tag: %v", err)
	}
	if err := tagMgr.AttachTag(ctx, tagFinID, targetVM.Reference()); err != nil {
		t.Fatalf("failed to attach Finance tag: %v", err)
	}

	// 2. Test RestTagFetcher
	metrics := telemetry.NewMetrics("test", "0.1.0")
	fetcher := collector.NewRestTagFetcher(vc, "vc-sim-01", collector.TagFetcherConfig{
		Timeout:   5 * time.Second,
		BatchSize: 2, // Test batching across multiple VMs
	}, metrics)

	if err := fetcher.Init(ctx, user); err != nil {
		t.Fatalf("fetcher.Init failed: %v", err)
	}

	// Query all VM references
	refs := make([]types.ManagedObjectReference, len(vms))
	for i, v := range vms {
		refs[i] = v.Reference()
	}

	tagMap, err := fetcher.FetchTagsForObjects(ctx, refs)
	if err != nil {
		t.Fatalf("FetchTagsForObjects failed: %v", err)
	}

	// Assert target VM has both tags
	vmTags, ok := tagMap[targetVM.Reference().Value]
	if !ok {
		t.Fatalf("expected tags for VM %s, got none", targetVM.Reference().Value)
	}
	if len(vmTags) != 2 {
		t.Fatalf("expected 2 tags for VM, got %d", len(vmTags))
	}

	hasEnv := false
	hasCost := false
	for _, tRec := range vmTags {
		if tRec.CategoryName == "Environment" && tRec.TagName == "Production" && tRec.TagID == tagProdID {
			hasEnv = true
		}
		if tRec.CategoryName == "CostCenter" && tRec.TagName == "Finance-90210" && tRec.TagID == tagFinID {
			hasCost = true
		}
	}

	if !hasEnv || !hasCost {
		t.Errorf("expected Environment/Production and CostCenter/Finance-90210 tags, got %+v", vmTags)
	}

	// Clean close
	if err := fetcher.Close(ctx); err != nil {
		t.Errorf("fetcher.Close failed: %v", err)
	}
}

func TestRestTagFetcher_DegradationOnFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Invalid URL / unreachable server
	badURL, _ := url.Parse("https://127.0.0.1:59999/sdk")
	sc := soap.NewClient(badURL, true)
	c := &vim25.Client{Client: sc}
	metrics := telemetry.NewMetrics("test", "0.1.0")

	fetcher := collector.NewRestTagFetcher(c, "vc-broken-01", collector.TagFetcherConfig{
		Timeout:   500 * time.Millisecond,
		BatchSize: 10,
	}, metrics)

	err := fetcher.Init(ctx, url.UserPassword("user", "pass"))
	if err == nil {
		t.Fatalf("expected Init to fail on unreachable server")
	}
	if !errors.Is(err, collector.ErrTaggingUnavailable) {
		t.Errorf("expected ErrTaggingUnavailable, got: %v", err)
	}

	// Calling FetchTagsForObjects when uninitialized returns ErrTaggingUnavailable
	_, err = fetcher.FetchTagsForObjects(ctx, []types.ManagedObjectReference{{Type: "VirtualMachine", Value: "vm-1"}})
	if !errors.Is(err, collector.ErrTaggingUnavailable) {
		t.Errorf("expected ErrTaggingUnavailable, got %v", err)
	}
}
