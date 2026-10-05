//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/infrastructure/sims/internal/vsphere"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/view"
)

func TestVCSim(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Initialize in-process VPX simulator model
	model := simulator.VPX()
	vsphere.ConfigureTestVPX(model)
	defer model.Remove()

	err := model.Create()
	if err != nil {
		t.Fatalf("failed to create in-process vcsim model: %v", err)
	}

	// 2. Start mock HTTP/SOAP TLS server
	s := model.Service.NewServer()
	defer s.Close()

	// 3. Connect client and authenticate
	client, err := govmomi.NewClient(ctx, s.URL, true)
	if err != nil {
		t.Fatalf("failed to connect to in-process vcsim: %v", err)
	}
	defer func() {
		_ = client.Logout(ctx)
	}()

	// 4. Create ContainerView for VirtualMachines
	viewMgr := view.NewManager(client.Client)
	cv, err := viewMgr.CreateContainerView(ctx, client.ServiceContent.RootFolder, []string{"VirtualMachine"}, true)
	if err != nil {
		t.Fatalf("failed to create ContainerView: %v", err)
	}
	defer func() {
		_ = cv.Destroy(ctx)
	}()

	// 5. Query and count VMs
	vms, err := cv.Find(ctx, []string{"VirtualMachine"}, nil)
	if err != nil {
		t.Fatalf("failed to query VMs from ContainerView: %v", err)
	}

	t.Logf("In-process vcsim initialized successfully; discovered %d VirtualMachine(s)", len(vms))
	if len(vms) == 0 {
		t.Errorf("expected >0 VMs in default VPX model, got 0")
	}
}
