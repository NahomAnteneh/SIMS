# vSphere Capability & Behavioral Matrix

> **Status:** BLOCKED ON HUMAN  
> **Reason:** Real vCenter validation requires production credentials and network access that cannot be automated in simulated CI. The `vcsim` column below was generated empirically using `cmd/vcenter-spike`. The `Real vCenter Result` column is strictly marked `PENDING (BLOCKED ON HUMAN)`.

---

## 1. Overview

This document records the architectural and operational capabilities of VMware vSphere endpoints. Simulator environments like `vcsim` do not fully reproduce real vCenter behavioral quirks, SOAP fault handling, session eviction timing, or cross-vCenter vMotion (xvMotion).

The spike CLI `cmd/vcenter-spike` provides a safe, read-only validation tool that probes all key endpoints, verifies object lifecycle teardown (destroying ContainerViews, PropertyFilters, and EventHistoryCollectors), and logs out cleanly.

---

## 2. Capability Matrix

| Probe / Behavior | vcsim Result (Empirical) | Real vCenter Result | Architectural Assumption & Risk |
| :--- | :--- | :--- | :--- |
| **vCenter Version** | VMware vCenter Server 6.5.0 build-5973321 (API 6.5, Linux) | `PENDING (BLOCKED ON HUMAN)` | Real vCenters in scope will be vSphere 7.0u3 or 8.0+. Older API versions may omit modern properties. |
| **ESXi Version** | VMware ESXi 8.0.2 build-21997540 (4 hosts discovered) | `PENDING (BLOCKED ON HUMAN)` | ESXi build numbers reflect patch levels; hardware status sensors differ by OEM build. |
| **TLS Handshake & Ciphers** | PASS: Negotiated TLS 1.3 (Cipher: 0x1301) | `PENDING (BLOCKED ON HUMAN)` | Real vCenters may enforce TLS 1.2 only or require custom enterprise CA certificate validation. |
| **PropertyCollector** | PASS: PropertyFilter created and destroyed; initial version token `"-"`; 1 update received | `PENDING (BLOCKED ON HUMAN)` | Real vCenter version strings are sequential integers or opaque alphanumeric tokens. Must handle version truncation. |
| **EventHistoryCollector** | PASS: Created and destroyed cleanly; retrieved 10 recent events via `LatestPage` | `PENDING (BLOCKED ON HUMAN)` | Real vCenter retains events for 30–90 days. Collectors must page with deterministic keys and handle large event spikes. |
| **VDS Support** | PASS: `DistributedVirtualSwitch` ManagedObject type supported (1 DVS object present) | `PENDING (BLOCKED ON HUMAN)` | Standard switches (VSS) vs distributed switches (VDS) have different port group key representations. |
| **guest.net Availability**| PASS: 4 of 4 VMs had populated IP/MAC addresses in `guest.net` | `PENDING (BLOCKED ON HUMAN)` | VMs without VMware Tools or in powered-off state have empty `guest.net`. Topology parser must fallback to NIC MAC addresses. |
| **Custom Attributes** | PASS: `CustomFieldsManager` supported (`cfm.Field`, `cfm.Add`, `cfm.Set`); VM `customValue` retrieved and resolved key->name | `PENDING (BLOCKED ON HUMAN)` | Keys are integer IDs scoped per vCenter. Topology snapshot must hydrate field definitions to resolve names; updates require cache invalidation. |
| **vSphere Tags (REST)** | PASS: Tested with `vapi/simulator` (`/rest/com/vmware/cis/*`). Unregistered standalone `vcsim` returns 404, handled via graceful degradation (`ErrTaggingUnavailable`) | `PENDING (BLOCKED ON HUMAN)` | Tags live exclusively in vSphere Automation REST API, not SOAP PropertyCollector. Batch tag lookup must be bounded and resilient to REST timeouts. |
| **Inventory Scale** | PASS: 1 DC, 1 Cluster, 4 Hosts, 4 VMs, 1 Datastore, 3 Networks | `PENDING (BLOCKED ON HUMAN)` | Target scale is 10,000+ VMs per vCenter. Initial snapshot hydration must stream using COPY to prevent memory exhaustion. |
| **Session Lifetime** | PASS: `SessionIsActive` returned `true`, session reference confirmed | `PENDING (BLOCKED ON HUMAN)` | vCenter idle timeout defaults to 30 minutes. Collector heartbeat loop must refresh sessions or re-authenticate on 401. |
| **InvalidCollectorVersion** | WARN: vcsim blocked on unknown token until timeout rather than returning fault | `PENDING (BLOCKED ON HUMAN)` | Real vCenter returns `types.InvalidCollectorVersion` SOAP fault when token expires. Collector must catch this and trigger full resync. |
| **xvMotion Identity Change**| *Documented manual procedure* (see Section 3) | `PENDING (BLOCKED ON HUMAN)` | When a VM moves across vCenters, `moref` changes while `instanceUuid` (usually) persists, but MAC/BIOS UUID can conflict. |

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

# 3. Copy the output table and update the "Real vCenter Result" column in this document.
```
