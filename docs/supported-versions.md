# Supported vSphere Versions & Capability Policy

This document defines the official version compatibility, support lifecycle, and behavioral capability policy for the Distributed vSphere Inventory & Search Platform (SIMS).

---

## 1. Version Support Policy

SIMS supports VMware vSphere / vCenter Server **7.0 and higher** (including 7.x, 8.x, 9.x, and future releases), with **no hard-coded ceiling check**.

| Version Range | Registration / Connection Action | Compatibility Status | Description |
| :--- | :--- | :--- | :--- |
| **< 7.0** (e.g. 6.0, 6.5, 6.7) | **Rejected** | `unsupported` | Rejected immediately with typed error code `unsupported_version`. Pre-7.0 APIs lack modern tag, identity, and security features required by SIMS. |
| **$\ge$ 7.0** (Verified in Matrix) | **Accepted** | `certified` | Fully supported and certified. Real vCenter behavioral validation has been performed and recorded in `docs/vsphere-capability-matrix.md`. |
| **$\ge$ 7.0** (Unverified in Matrix) | **Accepted** | `untested` | Accepted with an operational warning. Functions using standard vSphere API contracts, but real-appliance quirks have not yet been recorded in the capability matrix. |

### Guarantees
1. **No Ceiling Guard:** A 9.x (or future 10.x) vCenter will never be rejected due to version number.
2. **Deterministic Certification:** A version is promoted to `certified` as soon as its real-vCenter verification column is completed in `docs/vsphere-capability-matrix.md`.
3. **Read-Only Safety:** SIMS operates exclusively in read-only mode across all supported versions; it never mutates vCenter configuration or inventory state.

---

## 2. Release Lines in Scope

### VMware vCenter Server 7.0.x
- **Status:** Supported & in scope
- **Releases:** 7.0 GA, 7.0 Update 1, 7.0 Update 2, 7.0 Update 3
- **API Versions:** 7.0, 7.0.1.0, 7.0.2.0, 7.0.3.0
- **Architectural Notes:** vSphere 7.0 Update 2 announced deprecation of legacy `/rest` endpoints in favor of `/api`. SIMS supports both styles via capability-driven fallback.

### VMware vCenter Server 8.0.x
- **Status:** Supported & in scope
- **Releases:** 8.0 GA, 8.0 Update 1, 8.0 Update 2, 8.0 Update 3
- **API Versions:** 8.0, 8.0.1.0, 8.0.2.0, 8.0.3.0
- **Architectural Notes:** Includes unified authentication session support between REST and SOAP APIs (8.0 U3).

### VMware vCenter Server 9.0.x
- **Status:** Supported & in scope (GA: June 17, 2025; build 24755229)
- **Releases:** 9.0 GA
- **API Version:** 9.0.0.0
- **Architectural Notes:** Part of VMware Cloud Foundation (VCF) 9.0 / VMware vSphere Foundation (VVF) 9.0. Full SOAP and REST backward compatibility maintained.

### VMware vCenter Server 9.1.x
- **Status:** Supported & in scope (GA: May 12, 2026; 9.1.1: September 2026)
- **Releases:** 9.1.0, 9.1.1
- **API Versions:** 9.1.0.0, 9.1.1.0
- **Architectural Notes:** Broadcom vSphere Web Services API latest reference line. `govmomi` v0.56.0 includes generated bindings derived directly from vSphere 9.1.0.0.

### Future Major Versions (e.g. 10.0+)
- **Status:** Accepted with `compatibility = untested`
- **Architectural Notes:** Forward compatibility guaranteed by dynamic API negotiation and capability detection.

---

## 3. Connect-Time Detection & Telemetry

When establishing a connection, SIMS reads `ServiceInstance.content.about` (`types.AboutInfo`):
- `Version`: Full product version string (e.g., `"8.0.3"`, `"9.1.0"`).
- `ApiVersion`: Upstream API version string (e.g., `"8.0.3.0"`, `"9.1.0.0"`).
- `Build`: Official build number (e.g., `"24022515"`).
- `ApiType`: Must be `"VirtualCenter"`.
- `ProductLineId`: Typically `"vpx"`.

### Metrics & Logging
- **Metric:** `sims_vcenter_info{vcenter_id="...", major_minor="8.0", api_version="8.0.3.0", compatibility="certified"}`
- **Cardinality:** Restricted to `major.minor` to prevent metric cardinality explosion across patch releases.
- **Collector State:** Preserves exact `version`, `api_version`, and `build` in memory and in the database `vcenters` record (added in Sprint 3).

---

## 4. Capability Flags Architecture

Rather than branching on version numbers in application code, SIMS queries and probes feature capabilities:

| Capability Flag | Mechanism | Supported Versions | Failure Handling |
| :--- | :--- | :--- | :--- |
| **`tagging_rest_endpoint`** | Probes `/rest/com/vmware/cis/tagging` with fallback to `/api/vcenter/tagging` | 7.x, 8.x, 9.x | Degrades tags via `ErrTaggingUnavailable`; core inventory collection continues unaffected (ADR-016). |
| **`property_collector_delta`** | `WaitForUpdatesEx` change streaming | 7.x, 8.x, 9.x | Catches `InvalidCollectorVersion` fault and timeout hangs; triggers clean full reconciliation. |
| **`custom_attributes_soap`** | `CustomFieldsManager` definitions & VM `customValue` | 7.x, 8.x, 9.x | Fallback to empty attribute map if service is absent or unprivileged. |
| **`dvs_topology`** | `DistributedVirtualSwitch` and DVPortgroup traversal | 7.x, 8.x, 9.x | Standard switch (VSS) fallback when DVS is not configured. |

---

## 5. Client Library Compatibility

SIMS pins `github.com/vmware/govmomi` to version **v0.56.0**:
- **Official Upstream Statement:** "vSphere 7.0 and higher" ([govmomi README](https://github.com/vmware/govmomi#compatibility)).
- **Generated Bindings:** vSphere 9.1.0.0 (`vim25.Version = "9.1.0.0"` in `vim25/client.go`).
- **Negotiation:** Uses dynamic SOAP action dispatch and optional `/sdk/vimServiceVersions.xml` discovery. Does not pin an obsolete version that modern 9.x servers reject.

---

## 6. Testing & Certification Matrix

1. **Every Pull Request (CI):**
   - In-process `simulator.VPX()` and containerized `vcsim`.
   - Unit and mock tests validating version detection for:
     - 7.0, 8.0, 9.0, 9.1 (accepted, compatibility evaluated)
     - Pre-7.0 (e.g. 6.5, 6.7; rejected with `unsupported_version`)
     - Future 10.0 (accepted, reported as `untested`)
2. **Nightly & Pre-Release:**
   - Real vCenter environments across all available major versions: 7.0.x, 8.0.x (U3), 9.0.x, 9.1.x.
   - Results recorded per version column in `docs/vsphere-capability-matrix.md`.
3. **Sprint 4 Mixed Fleet Scale Testing:**
   - Validates heterogeneous fleets containing 7.x, 8.x, and 9.x vCenters simultaneously managed by a single SIMS cluster.
   - Cross-vCenter vMotion (xvMotion) tested in both directions across differing major versions.
