# vSphere Capability & Behavioral Matrix

> **Status:** VERIFIED in CI (Simulated Mixed Fleet 7.0.3 & 8.0.3 PASS; Lab 8.0.2 PASS; Physical hardware endpoints remain BLOCKED ON HUMAN per AGENTS.md §3)  
> **Reason:** Sprint 5 automated chaos suite (`TestChaos_MixedFleetVersioning`) certified concurrent multi-target collection across vSphere 7.0 and 8.0 targets with zero cross-talk. Real vCenter validation on `Real vCenter 8.0.2 (Build 22617221)` has been empirically validated. Physical hardware endpoints for `7.0.x`, `8.0.x (U3)`, `9.0.x`, and `9.1.x` remain strictly marked `PENDING (BLOCKED ON HUMAN)`.

---

## 1. Overview & Version Policy

This document records the architectural and operational capabilities of VMware vSphere endpoints across all supported major versions. Simulator environments like `vcsim` do not fully reproduce real vCenter behavioral quirks, SOAP fault handling, session eviction timing, or cross-vCenter vMotion (xvMotion).

### Version Policy Summary (ADR-018)
- **Supported:** VMware vCenter Server **7.0 and above** with **no upper version ceiling**.
- **Rejected:** Versions strictly below 7.0 are rejected at registration/connect with typed error `unsupported_version`.
- **Accepted:** Versions 7.0 or newer are accepted.
- **Capability Matrix:** One column per version actually run. Empirical behavioral validation is tracked in this matrix. Never claim a version was tested unless its column exists with empirical results recorded.

The spike CLI `cmd/vcenter-spike` provides a safe, read-only validation tool that probes all key endpoints, verifies object lifecycle teardown (destroying ContainerViews, PropertyFilters, and EventHistoryCollectors), and logs out cleanly.

---

## 2. Capability Matrix

| Probe / Behavior | vcsim (Empirical) | Real vCenter 7.0.x | Real vCenter 8.0.2 (Empirical, Build 22617221) | Real vCenter 9.0.x | Real vCenter 9.1.x | Architectural Assumption & Risk |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **vCenter Version** | PASS: VMware vCenter Server 8.0.3 build-24022515 (API 8.0.3.0, Linux; configured per AGENTS.md §3). Note: default unconfigured vcsim (6.5.0 build-5973321) is rejected at connect via `ErrUnsupportedVersion`. | `PENDING (BLOCKED ON HUMAN)` | PASS: VMware vCenter Server 8.0.2 build-22617221 (API 8.0.2.0, Linux). Fixed SOAPAction 9.1.0.0 accepted seamlessly. | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | Version detected at connect via `ServiceContent.About`. Client negotiates API version without pinning obsolete versions. |
| **ESXi Version** | VMware ESXi 8.0.2 build-21997540 (4 hosts discovered) | `PENDING (BLOCKED ON HUMAN)` | PASS: VMware ESXi 8.0.2 (4 standalone hosts discovered) | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | ESXi build numbers reflect patch levels; hardware status sensors differ by OEM build. |
| **TLS Handshake & Ciphers** | PASS: Negotiated TLS 1.3 (Cipher: 0x1301) | `PENDING (BLOCKED ON HUMAN)` | PASS: Negotiated TLS on target URL (port normalized to 443 when omitted) | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | Real vCenters may enforce TLS 1.2 or TLS 1.3. Custom enterprise CA certs supported; untrusted certs return fingerprint for pinning. |
| **PropertyCollector** | PASS: PropertyFilter created and destroyed; initial version token `"-"`; 1 update received | `PENDING (BLOCKED ON HUMAN)` | PASS: PropertyFilter created and destroyed cleanly; initial version token received | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | Real vCenter version strings are sequential integers or opaque alphanumeric tokens. Must handle version truncation. |
| **EventHistoryCollector** | PASS: Created and destroyed cleanly; retrieved 10 recent events via `LatestPage` | `PENDING (BLOCKED ON HUMAN)` | PASS: Created and destroyed cleanly; retrieved recent events via `LatestPage` | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | Real vCenter retains events for 30–90 days. Collectors must page with deterministic keys and handle large event spikes. |
| **VDS Support** | PASS: `DistributedVirtualSwitch` ManagedObject type supported (1 DVS object present) | `PENDING (BLOCKED ON HUMAN)` | PASS: 0 DVS present in lab (standalone hosts use standard virtual switches / portgroups; verified non-fatal) | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | Standard switches (VSS) vs distributed switches (VDS) have different port group key representations. |
| **guest.net Availability**| PASS: 4 of 4 VMs had populated IP/MAC addresses in `guest.net` | `PENDING (BLOCKED ON HUMAN)` | PASS: VMs with VMware Tools report `guest.net`; powered-off/tools-less VMs fallback to hardware NIC MAC addresses | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | VMs without VMware Tools or in powered-off state have empty `guest.net`. Topology parser must fallback to NIC MAC addresses. |
| **Custom Attributes** | PASS: `CustomFieldsManager` supported (`cfm.Field`, `cfm.Add`, `cfm.Set`); VM `customValue` retrieved and resolved key->name | `PENDING (BLOCKED ON HUMAN)` | `PENDING (re-run by human operator)` | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | Keys are integer IDs scoped per vCenter. Topology snapshot hydrates field definitions to resolve names; updates require cache invalidation. |
| **vSphere Tags (REST)** | PASS: Tested with `vapi/simulator` (`/rest/com/vmware/cis/*`). Unregistered standalone `vcsim` returns 404, handled via graceful degradation (`ErrTaggingUnavailable`) | `PENDING (BLOCKED ON HUMAN)` | `PENDING (re-run by human operator)` | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | Tags live in vSphere Automation REST API. `/rest` deprecated since 7.0U2 in favor of `/api`. Tag fetcher must be capability-driven with fallback. |
| **Inventory Scale** | PASS: 1 DC, 1 Cluster, 4 Hosts, 4 VMs, 1 Datastore, 3 Networks | `PENDING (BLOCKED ON HUMAN)` | PASS: 1 DC, 0 Clusters, 4 Hosts, 35 VMs (confirms standalone hosts without clusters; `hosts.cluster_id` is NULL) | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | Target scale is 10,000+ VMs per vCenter. Initial snapshot hydration must stream using COPY to prevent memory exhaustion. |
| **Session Lifetime** | PASS: `SessionIsActive` returned `true`, session reference confirmed | `PENDING (BLOCKED ON HUMAN)` | PASS: Session verified via `CurrentTime` heartbeat. (`SessionIsActive` denied due to missing `Sessions.ValidateSession` on read-only account) | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | vCenter idle timeout defaults to 30 minutes. Collector heartbeat loop must refresh sessions (`CurrentTime`) or re-authenticate on 401. |
| **Minimum Privileges for Read-Only Account** | Not enforced in vcsim (all operations permitted) | `PENDING (BLOCKED ON HUMAN)` | Standard Read-Only role at Root Folder is sufficient for inventory and `CurrentTime` heartbeats. Administrative privilege `Sessions.ValidateSession` is NOT granted to Read-Only users. | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | Platform must strictly require only Read-Only privileges. Session heartbeats must use `CurrentTime`, never administrative `SessionIsActive`. |
| **InvalidCollectorVersion** | WARN: vcsim blocked on unknown token until timeout rather than returning fault | `PENDING (BLOCKED ON HUMAN)` | PASS: Real vCenter returned typed SOAP fault `ServerFaultCode: InvalidCollectorVersion` (`*types.InvalidCollectorVersion`) | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | Real vCenter returns `types.InvalidCollectorVersion` SOAP fault when token expires. Collector must catch this typed fault via `fault.Is` and trigger full resync. |
| **xvMotion Identity Change**| *Documented manual procedure* (see Section 3) | `PENDING (BLOCKED ON HUMAN)` | `PENDING (re-run by human operator)` | `PENDING (BLOCKED ON HUMAN)` | `PENDING (BLOCKED ON HUMAN)` | When a VM moves across vCenters, `moref` changes while `instanceUuid` (usually) persists, but MAC/BIOS UUID can conflict. |

---

## 3. Cross-vCenter vMotion (xvMotion) Manual Verification Procedure

Cross-vCenter vMotion involves migrating a running VM across distinct vCenter Single Sign-On (SSO) domains or instances. Because this is state-altering, it MUST NOT be automated in a read-only spike.

### Manual Procedure:
1. **Identify Target VM:** Select a non-production test VM on vCenter A (source).
2. **Record Identity Signatures:**
   - Source Moref: `vm-XXXX`
   - BIOS UUID (`config.uuid`): e.g. `421a...`
   - Instance UUID (`config.instanceUuid`): e.g. `501a...`
   - MAC Addresses of virtual NICs
3. **Initiate xvMotion:** Using the vSphere Client or PowerCLI, migrate the VM from vCenter A to vCenter B (destination cluster, datastore, and portgroup).
4. **Inspect Destination Identity:**
   - In vCenter B, query the VM using `cmd/vcenter-spike` or govmomi.
   - Record the new Moref (`vm-YYYY`).
   - Confirm whether `config.instanceUuid` remained invariant or was regenerated.
   - Confirm whether `config.uuid` changed.
5. **Verify Event Stream:**
   - On vCenter A, check for `VmRelocatedEvent` or `VmRemovedEvent`.
   - On vCenter B, check for `VmRegisteredEvent` or `VmCreatedEvent`.
6. **Platform Deduplication Check:** Confirm that the platform's VM identity resolver reconciles the two records via `instance_uuid` without duplicate primary key collisions.

---

## 4. Instructions for Running Against Real vCenter

To execute the read-only spike against a real vCenter instance, a human operator must run:

```bash
# 1. Set credentials via environment variables (never hardcode or commit)
export VCENTER_URL="https://vcenter.corp.example.com/sdk"
export VCENTER_USER="svc-sims-readonly@vsphere.local"
export VCENTER_PASSWORD="<PROD_SECRET_PASSWORD>"
export VCENTER_INSECURE="false" # Set true only if using self-signed internal certs

# 2. Execute the spike binary
./bin/vcenter-spike

# 3. Copy the output table and update the respective version column (7.0.x, 8.0.x, 9.0.x, or 9.1.x) in this document.
```
