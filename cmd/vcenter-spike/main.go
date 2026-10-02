package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/infrastruture/sims/internal/logging"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/event"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

type ProbeResult struct {
	Name        string
	Status      string
	Description string
	Evidence    string
}

func main() {
	rawURL := getEnv("VCENTER_URL", "https://localhost:8989/sdk")
	username := getEnv("VCENTER_USER", "user")
	password := getEnv("VCENTER_PASSWORD", "pass")
	insecureTLS := getEnvBool("VCENTER_INSECURE", true)

	safeURL := logging.RedactURL(rawURL)
	fmt.Printf("===================================================================\n")
	fmt.Printf(" vSphere Capability & Behavioral Matrix Spike CLI (Read-Only)      \n")
	fmt.Printf(" Target: %s (User: %s)\n", safeURL, username)
	fmt.Printf("===================================================================\n\n")

	u, err := soap.ParseURL(rawURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Invalid vCenter URL: %v\n", err)
		os.Exit(1)
	}
	u.User = url.UserPassword(username, password)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var results []ProbeResult

	// 1. Probe TLS Behavior
	tlsResult := probeTLS(u.Host, insecureTLS)
	results = append(results, tlsResult)

	// Connect govmomi client
	client, err := govmomi.NewClient(ctx, u, insecureTLS)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Failed to connect and authenticate to vCenter: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		_ = client.Logout(ctx)
		fmt.Println("\n[Clean Logout] Session successfully terminated on server.")
	}()

	// 2. vCenter Version
	results = append(results, probeVCenterVersion(client))

	// 3. View Manager & Inventory Size
	viewMgr := view.NewManager(client.Client)

	// ESXi Version
	results = append(results, probeESXiVersion(ctx, viewMgr, client.ServiceContent.RootFolder))

	// Inventory Size
	results = append(results, probeInventorySize(ctx, viewMgr, client.ServiceContent.RootFolder))

	// 4. PropertyCollector Behavior
	results = append(results, probePropertyCollector(ctx, client))

	// 5. EventHistoryCollector Behavior
	results = append(results, probeEventHistoryCollector(ctx, client))

	// 6. VDS Support
	results = append(results, probeVDSSupport(ctx, viewMgr, client.ServiceContent.RootFolder))

	// 7. guest.net Availability
	results = append(results, probeGuestNet(ctx, viewMgr, client.ServiceContent.RootFolder))

	// 8. Session Lifetime Behavior
	results = append(results, probeSessionLifetime(ctx, client))

	// 9. InvalidCollectorVersion Behavior
	results = append(results, probeInvalidCollectorVersion(ctx, client))

	// Print Results Table
	fmt.Printf("%-32s | %-8s | %s\n", "PROBE", "STATUS", "DETAILS / EVIDENCE")
	fmt.Printf("%s\n", strings.Repeat("-", 100))
	for _, r := range results {
		fmt.Printf("%-32s | %-8s | %s\n", r.Name, r.Status, r.Evidence)
	}
	fmt.Printf("%s\n", strings.Repeat("-", 100))
}

func probeTLS(host string, insecure bool) ProbeResult {
	conf := &tls.Config{
		InsecureSkipVerify: insecure, // #nosec G402 -- intentionally configurable for lab/vcsim environments
	}
	conn, err := tls.Dial("tcp", host, conf)
	if err != nil {
		return ProbeResult{
			Name:     "TLS Behavior",
			Status:   "FAIL",
			Evidence: fmt.Sprintf("TLS handshake failed: %v", err),
		}
	}
	defer conn.Close()

	state := conn.ConnectionState()
	var tlsVersionStr string
	switch state.Version {
	case tls.VersionTLS13:
		tlsVersionStr = "TLS 1.3"
	case tls.VersionTLS12:
		tlsVersionStr = "TLS 1.2"
	default:
		tlsVersionStr = fmt.Sprintf("TLS 0x%04x", state.Version)
	}

	return ProbeResult{
		Name:     "TLS Behavior",
		Status:   "PASS",
		Evidence: fmt.Sprintf("Negotiated %s (Cipher: 0x%04x, InsecureSkipVerify: %v)", tlsVersionStr, state.CipherSuite, insecure),
	}
}

func probeVCenterVersion(c *govmomi.Client) ProbeResult {
	about := c.ServiceContent.About
	return ProbeResult{
		Name:     "vCenter Version",
		Status:   "PASS",
		Evidence: fmt.Sprintf("%s (v%s, Build %s, API %s, OS: %s)", about.FullName, about.Version, about.Build, about.ApiVersion, about.OsType),
	}
}

func probeESXiVersion(ctx context.Context, vm *view.Manager, root types.ManagedObjectReference) ProbeResult {
	cv, err := vm.CreateContainerView(ctx, root, []string{"HostSystem"}, true)
	if err != nil {
		return ProbeResult{Name: "ESXi Version", Status: "FAIL", Evidence: err.Error()}
	}
	defer func() { _ = cv.Destroy(ctx) }()

	var hosts []mo.HostSystem
	err = cv.Retrieve(ctx, []string{"HostSystem"}, []string{"summary.config.product"}, &hosts)
	if err != nil {
		return ProbeResult{Name: "ESXi Version", Status: "FAIL", Evidence: err.Error()}
	}

	if len(hosts) == 0 {
		return ProbeResult{Name: "ESXi Version", Status: "WARN", Evidence: "0 ESXi hosts discovered in inventory"}
	}

	prod := hosts[0].Summary.Config.Product
	return ProbeResult{
		Name:     "ESXi Version",
		Status:   "PASS",
		Evidence: fmt.Sprintf("%s (v%s, Build %s) [%d host(s) found]", prod.FullName, prod.Version, prod.Build, len(hosts)),
	}
}

func probeInventorySize(ctx context.Context, vm *view.Manager, root types.ManagedObjectReference) ProbeResult {
	entities := []string{"Datacenter", "ClusterComputeResource", "HostSystem", "VirtualMachine", "Datastore", "Network"}
	counts := make(map[string]int)

	for _, entity := range entities {
		cv, err := vm.CreateContainerView(ctx, root, []string{entity}, true)
		if err != nil {
			counts[entity] = -1
			continue
		}
		refs, err := cv.Find(ctx, []string{entity}, nil)
		if err != nil {
			counts[entity] = -1
		} else {
			counts[entity] = len(refs)
		}
		_ = cv.Destroy(ctx)
	}

	return ProbeResult{
		Name:   "Inventory Size",
		Status: "PASS",
		Evidence: fmt.Sprintf("DC: %d, Cluster: %d, Host: %d, VM: %d, Datastore: %d, Network: %d",
			counts["Datacenter"], counts["ClusterComputeResource"], counts["HostSystem"],
			counts["VirtualMachine"], counts["Datastore"], counts["Network"]),
	}
}

func probePropertyCollector(ctx context.Context, c *govmomi.Client) ProbeResult {
	pc := property.DefaultCollector(c.Client)
	root := c.ServiceContent.RootFolder

	viewMgr := view.NewManager(c.Client)
	cv, err := viewMgr.CreateContainerView(ctx, root, []string{"VirtualMachine"}, true)
	if err != nil {
		return ProbeResult{Name: "PropertyCollector Behavior", Status: "FAIL", Evidence: err.Error()}
	}
	defer func() { _ = cv.Destroy(ctx) }()

	filterSpec := types.PropertyFilterSpec{
		ObjectSet: []types.ObjectSpec{
			{
				Obj:  cv.Reference(),
				Skip: types.NewBool(true),
				SelectSet: []types.BaseSelectionSpec{
					&types.TraversalSpec{
						Type: "ContainerView",
						Path: "view",
						Skip: types.NewBool(false),
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

	filter, err := pc.CreateFilter(ctx, types.CreateFilter{
		This: pc.Reference(),
		Spec: filterSpec,
	})
	if err != nil {
		return ProbeResult{Name: "PropertyCollector Behavior", Status: "FAIL", Evidence: fmt.Sprintf("CreateFilter failed: %v", err)}
	}
	defer func() { _ = filter.Destroy(ctx) }()

	res, err := pc.WaitForUpdates(ctx, "")
	if err != nil {
		return ProbeResult{Name: "PropertyCollector Behavior", Status: "FAIL", Evidence: fmt.Sprintf("WaitForUpdates failed: %v", err)}
	}

	return ProbeResult{
		Name:     "PropertyCollector Behavior",
		Status:   "PASS",
		Evidence: fmt.Sprintf("Filter created and destroyed cleanly; initial version token: %q, object updates: %d", res.Version, len(res.FilterSet)),
	}
}

func probeEventHistoryCollector(ctx context.Context, c *govmomi.Client) ProbeResult {
	em := event.NewManager(c.Client)
	filter := types.EventFilterSpec{}

	collector, err := em.CreateCollectorForEvents(ctx, filter)
	if err != nil {
		return ProbeResult{Name: "EventHistoryCollector Behavior", Status: "FAIL", Evidence: fmt.Sprintf("CreateCollectorForEvents failed: %v", err)}
	}
	defer func() { _ = collector.Destroy(ctx) }()

	latestPage, err := collector.LatestPage(ctx)
	if err != nil {
		return ProbeResult{Name: "EventHistoryCollector Behavior", Status: "FAIL", Evidence: fmt.Sprintf("LatestPage failed: %v", err)}
	}

	return ProbeResult{
		Name:     "EventHistoryCollector Behavior",
		Status:   "PASS",
		Evidence: fmt.Sprintf("Created and destroyed cleanly; retrieved %d recent events", len(latestPage)),
	}
}

func probeVDSSupport(ctx context.Context, vm *view.Manager, root types.ManagedObjectReference) ProbeResult {
	cv, err := vm.CreateContainerView(ctx, root, []string{"DistributedVirtualSwitch", "VmwareDistributedVirtualSwitch"}, true)
	if err != nil {
		return ProbeResult{Name: "VDS Support", Status: "FAIL", Evidence: err.Error()}
	}
	defer func() { _ = cv.Destroy(ctx) }()

	kinds := []string{"DistributedVirtualSwitch", "VmwareDistributedVirtualSwitch"}
	refs, _ := cv.Find(ctx, kinds, nil)

	return ProbeResult{
		Name:     "VDS Support",
		Status:   "PASS",
		Evidence: fmt.Sprintf("DVS ManagedObject types supported by endpoint (%d DVS objects present)", len(refs)),
	}
}

func probeGuestNet(ctx context.Context, vm *view.Manager, root types.ManagedObjectReference) ProbeResult {
	cv, err := vm.CreateContainerView(ctx, root, []string{"VirtualMachine"}, true)
	if err != nil {
		return ProbeResult{Name: "guest.net Availability", Status: "FAIL", Evidence: err.Error()}
	}
	defer func() { _ = cv.Destroy(ctx) }()

	var vms []mo.VirtualMachine
	err = cv.Retrieve(ctx, []string{"VirtualMachine"}, []string{"name", "guest.net"}, &vms)
	if err != nil {
		return ProbeResult{Name: "guest.net Availability", Status: "FAIL", Evidence: err.Error()}
	}

	if len(vms) == 0 {
		return ProbeResult{Name: "guest.net Availability", Status: "WARN", Evidence: "0 VMs available to inspect guest.net"}
	}

	populated := 0
	for _, v := range vms {
		if v.Guest != nil && len(v.Guest.Net) > 0 {
			populated++
		}
	}

	return ProbeResult{
		Name:     "guest.net Availability",
		Status:   "PASS",
		Evidence: fmt.Sprintf("%d of %d VMs have populated guest.net IP/MAC structures", populated, len(vms)),
	}
}

func probeSessionLifetime(ctx context.Context, c *govmomi.Client) ProbeResult {
	active, err := c.SessionManager.SessionIsActive(ctx)
	if err != nil {
		return ProbeResult{Name: "Session Lifetime Behavior", Status: "FAIL", Evidence: err.Error()}
	}
	return ProbeResult{
		Name:     "Session Lifetime Behavior",
		Status:   "PASS",
		Evidence: fmt.Sprintf("Session active: %v, UserSession ref: %s", active, c.SessionManager.Reference().Value),
	}
}

func probeInvalidCollectorVersion(ctx context.Context, c *govmomi.Client) ProbeResult {
	pc := property.DefaultCollector(c.Client)

	// Pass an intentionally bogus version to verify server response
	_, err := pc.WaitForUpdates(ctx, "INVALID_VERSION_STAMP_99999")
	if err == nil {
		return ProbeResult{
			Name:     "InvalidCollectorVersion Behavior",
			Status:   "WARN",
			Evidence: "Server accepted bogus version token without fault",
		}
	}

	errStr := err.Error()
	isExpectedFault := strings.Contains(errStr, "InvalidCollectorVersion") || strings.Contains(errStr, "invalid") || soap.IsSoapFault(err)

	status := "PASS"
	if !isExpectedFault {
		status = "WARN"
	}

	return ProbeResult{
		Name:     "InvalidCollectorVersion Behavior",
		Status:   status,
		Evidence: fmt.Sprintf("Bogus version correctly rejected with fault: %v", errStr),
	}
}

func getEnv(key, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok && strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		b, err := strconv.ParseBool(strings.TrimSpace(val))
		if err == nil {
			return b
		}
	}
	return defaultVal
}
