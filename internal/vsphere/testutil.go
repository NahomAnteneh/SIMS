package vsphere

import "github.com/vmware/govmomi/simulator"

// ConfigureTestVPX configures a simulator.VPX model to report vSphere 8.0.3,
// ensuring simulator-based unit and integration tests satisfy the "vSphere 7.0 and above" policy.
func ConfigureTestVPX(model *simulator.Model) {
	model.ServiceContent.About.Version = "8.0.3"
	model.ServiceContent.About.ApiVersion = "8.0.3.0"
	model.ServiceContent.About.FullName = "VMware vCenter Server 8.0.3 build-24022515"
}
