# Supported vSphere Versions & Capability Policy

This document defines the official version compatibility, support lifecycle, and behavioral capability policy for the Distributed vSphere Inventory & Search Platform (SIMS).

---

## 1. Version Support Policy

SIMS supports **vCenter Server 7.0 and above**, with **no upper version ceiling**.

| Version Range | Registration / Connection Action | Policy Description |
| :--- | :--- | :--- |
| **Below 7.0** (e.g. 6.0, 6.5, 6.7) | **Rejected** | Rejected immediately with typed error code `unsupported_version`. Pre-7.0 APIs lack modern tag, identity, and security features required by SIMS. |
| **7.0 and above** | **Accepted** | Accepted with no upper ceiling. Operational validation is tracked empirically in `docs/vsphere-capability-matrix.md`. |

### Guarantees
1. **No Version Ceiling:** Any vCenter Server 7.0 and above is accepted; no upper version ceiling is imposed.
2. **Capability Flags Over Version Branching:** Behavioral differences between releases are handled via capability flags and runtime probing, not hardcoded version checks.
3. **Read-Only Safety:** SIMS operates exclusively in read-only mode across all supported versions; it never mutates vCenter configuration or inventory state.

---

## 2. Release History (Informational)

> [!NOTE]
> The following overview of vSphere releases is provided as informational context only and does NOT define or restrict the support boundary. Any vCenter Server 7.0 and above is accepted.

### VMware vCenter Server 7.0
- **Context:** GA released April 2020. Update releases include 7.0 U1, 7.0 U2, 7.0 U3.
- **API Versions:** 7.0, 7.0.1.0, 7.0.2.0, 7.0.3.0.
- **Architectural Notes:** vSphere 7.0 Update 2 announced deprecation of legacy `/rest` endpoints in favor of `/api`. SIMS supports both styles via capability-driven fallback.

### VMware vCenter Server 8.0
- **Context:** GA released October 2022. Update releases include 8.0 U1, 8.0 U2, 8.0 U3.
- **API Versions:** 8.0, 8.0.1.0, 8.0.2.0, 8.0.3.0.
- **Architectural Notes:** Includes unified authentication session support between REST and SOAP APIs (8.0 U3).

### VMware vCenter Server 9.0
- **Context:** GA released June 17, 2025 (build 24755229).
- **API Version:** 9.0.0.0.
- **Architectural Notes:** Part of VMware Cloud Foundation (VCF) 9.0 / VMware vSphere Foundation (VVF) 9.0. Full SOAP and REST backward compatibility maintained.

### VMware vCenter Server 9.1
- **Context:** GA released May 12, 2026; 9.1.1 released September 2026.
- **API Versions:** 9.1.0.0, 9.1.1.0.
- **Architectural Notes:** Pinned client `govmomi` v0.56.0 includes generated bindings derived from vSphere 9.1.0.0. Part of the ongoing 9.x line; SIMS imposes no upper version ceiling.

---

## 3. Connect-Time Detection & Telemetry

When establishing a connection, SIMS reads `ServiceInstance.content.about` (`types.AboutInfo`):
- `Version`: Full product version string (e.g., `"8.0.3"`, `"9.1.0"`).
- `ApiVersion`: Upstream API version string (e.g., `"8.0.3.0"`, `"9.1.0.0"`).
- `Build`: Official build number (e.g., `"24022515"`).
- `ApiType`: Must be `"VirtualCenter"`.
- `ProductLineId`: Typically `"vpx"`.

### Metrics & Logging
- **Metric:** `sims_vcenter_info{vcenter_id="...", major_minor="8.0", api_version="8.0.3.0"}`
- **Cardinality:** Restricted to `major.minor` to prevent metric cardinality explosion across patch releases.
- **Collector State:** Preserves exact `version`, `api_version`, and `build` in memory and in the database `vcenters` record (added in Sprint 3).

---

## 4. Capability Flags Architecture

Rather than branching on version numbers in application code, SIMS queries and probes feature capabilities:

| Capability Flag | Mechanism | Supported Versions | Failure Handling |
| :--- | :--- | :--- | :--- |
| **`tagging_rest_endpoint`** | Probes `/rest/com/vmware/cis/tagging` with fallback to `/api/vcenter/tagging` | 7.0 and above | Degrades tags via `ErrTaggingUnavailable`; core inventory collection continues unaffected (ADR-016). |
| **`property_collector_delta`** | `WaitForUpdatesEx` change streaming | 7.0 and above | Catches `InvalidCollectorVersion` fault and timeout hangs; triggers clean full reconciliation. |
| **`custom_attributes_soap`** | `CustomFieldsManager` definitions & VM `customValue` | 7.0 and above | Fallback to empty attribute map if service is absent or unprivileged. |
| **`dvs_topology`** | `DistributedVirtualSwitch` and DVPortgroup traversal | 7.0 and above | Standard switch (VSS) fallback when DVS is not configured. |

---

## 5. Client Library Compatibility

SIMS pins `github.com/vmware/govmomi` to version **v0.56.0**:
- **Official Upstream Statement:** "vSphere 7.0 and higher." ([govmomi README](https://github.com/vmware/govmomi#compatibility)).
- **Generated Bindings:** vSphere 9.1.0.0 (`vim25.Version = "9.1.0.0"` in `vim25/client.go`).
- **Negotiation:** Uses dynamic SOAP action dispatch and optional `/sdk/vimServiceVersions.xml` discovery. Does not pin an obsolete version that modern servers reject.

---

## 6. Testing & Validation Matrix

1. **Every Pull Request (CI):**
   - In-process `simulator.VPX()` and containerized `vcsim`.
   - Unit and mock tests validating version detection for:
     - 7.0, an older supported version, and newer versions (all accepted)
     - Pre-7.0 (e.g. 6.5, 6.7; rejected with `unsupported_version`)
2. **Nightly & Pre-Release:**
   - Real vCenter environments across available supported versions (e.g. an older and a newer supported vCenter).
   - Results recorded per version column in `docs/vsphere-capability-matrix.md`.
3. **Sprint 4 Mixed Fleet Scale Testing:**
   - Validates heterogeneous fleets across supported vCenter versions simultaneously managed by a single SIMS cluster.
   - Cross-vCenter vMotion (xvMotion) tested in both directions across differing versions.
