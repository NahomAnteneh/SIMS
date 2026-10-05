# Real vCenter Manual Test Runbook (Sprint 2)

This runbook guides a human operator through executing and verifying Sprint 2 capabilities against real vCenter hardware.

> [!IMPORTANT]
> **Safety Invariants:**
> - SIMS is strictly **READ-ONLY** toward vCenter Server. All VM operations (power, metadata, networking) are performed by the human operator via the vSphere Client.
> - **Zero Credentials in Artifacts:** Never record real vCenter passwords, session tokens, or internal IP addresses in logs or notes. Refer to systems only as "lab vCenter" or "target vCenter".
> - **Environment Status:** Testing against lab vCenter 8.0.2 has been validated. Physical testing against real vCenter 7.0 and 9.x endpoints remains marked **`BLOCKED (human)`** until hardware is provisioned.

---

## 1. Prerequisites & Environment Setup

1. **Target vCenter Requirements:**
   - vSphere version `>= 7.0.0` (VirtualCenter API endpoint).
   - Dedicated service account with read-only permissions across root folder inventory.
2. **Environment Variables:**
   ```bash
   export VCENTER_URL="https://<lab-vcenter-hostname>/sdk"
   export VCENTER_USER="svc-collector@vsphere.local"
   export VCENTER_PASSWORD="<service-account-password>"
   export VCENTER_INSECURE="true" # or provide custom CA bundle
   export DATABASE_URL="postgres://collector_rw:collector_rw_pass@localhost:5433/sims?sslmode=disable"
   ```
3. **Database Readiness:**
   Ensure migrations 000001 through 000010 are applied:
   ```bash
   make test-migration
   ```

---

## 2. Test Execution Procedures

### Test 1: Power On / Power Off Real-Time Reflection
1. Launch live observation mode in a terminal:
   ```bash
   ./bin/vcenter-spike -observe
   ```
2. In vSphere Client, select a test VM (e.g. `lab-vm-01`) and trigger **Power Off**.
3. **Observe Console Output:**
   Verify `PropertyCollector` emits:
   ```text
   [PropertyCollector] VM vm-XXX (Op: modify) -> runtime.powerState: poweredOff
   ```
4. Query SIMS database:
   ```sql
   SELECT name, power_state, updated_at FROM virtual_machines WHERE moref = 'vm-XXX';
   ```
   **Expected Result:** `power_state` updates to `poweredOff` within < 1 second.
5. In vSphere Client, trigger **Power On**.
   **Expected Result:** `power_state` transitions back to `poweredOn`.

---

### Test 2: Modify Networking (Guest IP / MAC Fallback)
1. In vSphere Client, adjust network settings on test VM (or trigger DHCP renewal in guest OS).
2. Query SIMS database:
   ```sql
   SELECT mac_address, ip_addresses, connected FROM vm_network_interfaces vni
   JOIN virtual_machines vm ON vni.vm_id = vm.id
   WHERE vm.moref = 'vm-XXX';
   ```
   **Expected Result:** Updated IP addresses appear in PostgreSQL without duplicate NIC records.

---

### Test 3: VM Lifecycle & Event Ingestion (`VmCreatedEvent`, `VmRemovedEvent`)
1. In vSphere Client, deploy or clone a new VM named `lab-vm-new`.
2. Check `inventory_events` table:
   ```sql
   SELECT event_key, event_type, user_name, entity_name, created_at
   FROM inventory_events
   WHERE entity_name = 'lab-vm-new'
   ORDER BY event_key DESC LIMIT 5;
   ```
   **Expected Result:** `VmCreatedEvent` or `VmClonedEvent` is recorded with operator's username and server timestamp.
3. Check `virtual_machines` table:
   ```sql
   SELECT name, created_at, created_by, deleted_at FROM virtual_machines WHERE name = 'lab-vm-new';
   ```
   **Expected Result:** `created_by` matches operator username; `created_at` matches event timestamp.
4. In vSphere Client, delete `lab-vm-new`.
   **Expected Result:** `deleted_at` is set to `NOW()` upon receipt of `VmRemovedEvent`. Row is not physically dropped (soft-delete).

---

### Test 4: Custom Attribute Synchronization (Atomic Replace-Set)
1. In vSphere Client, define or set custom attributes on a test VM:
   - `CostCenter` = `Engineering`
   - `Project` = `SIMS`
2. Run spike observation mode or let collector sync.
3. Query SIMS database:
   ```sql
   SELECT name, value FROM vm_custom_attributes vca
   JOIN virtual_machines vm ON vca.vm_id = vm.id
   WHERE vm.name = 'lab-vm-01';
   ```
   **Expected Result:** Both key-value rows are present.
4. In vSphere Client, remove `Project` and change `CostCenter` to `Research`.
   **Expected Result:** Exactly 1 row remains (`CostCenter=Research`); `Project` is deleted.

---

### Test 5: Tag Synchronization (Diff-Check & vAPI Degradation)
1. In vSphere Client, attach tag `Environment/Production` to `lab-vm-01`.
2. Wait for next tag polling cycle (default: 30s).
3. Query SIMS database:
   ```sql
   SELECT category_name, tag_name FROM vm_tags vt
   JOIN virtual_machines vm ON vt.vm_id = vm.id
   WHERE vm.name = 'lab-vm-01';
   ```
   **Expected Result:** `Environment/Production` is present.
4. Detach the tag in vSphere Client.
   **Expected Result:** Row is removed on next polling interval.

---

### Test 6: Network Disconnect & Reconnect Resilience
1. Start SIMS collector in background.
2. Disconnect the collector host's network link to vCenter for 60 seconds (or block port 443 via iptables).
3. Observe collector logs:
   - `SessionGuardian` detects failed heartbeats and marks session suspect.
   - Streaming loop pauses without dropping buffered updates.
4. Restore network connectivity.
   **Expected Result:** `SessionGuardian` re-authenticates; event streamer resumes from overlap window; deduplication ledger ignores already processed event keys (`ON CONFLICT DO NOTHING`).

---

### Test 7: Stream Rollover & `InvalidCollectorVersion` Recovery
1. In vSphere Client or via API, restart vCenter Server services or invalidate the collector session.
2. Observe SIMS collector logs:
   - Worker detects `ServerFaultCode: InvalidCollectorVersion` (or hang deadline).
   - Worker transitions from `DELTA_STREAMING` -> `RESYNCING`.
   - Cleanup registry tears down stale PropertyFilter and ContainerView.
   - Worker executes inventory baseline reconciliation and transitions back to `DELTA_STREAMING`.
   **Expected Result:** Zero worker crash or unhandled panic; database resynchronizes automatically.

---

## 3. Human Sign-Off Matrix

| Test Case | vCenter Version | Operator | Result | Date | Notes |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **Test 1: Power On/Off** | 8.0.2 (Build 22617221) | Operator | **PASS** | 2026-10-05 | Reflected via delta stream < 1s |
| **Test 2: Networking** | 8.0.2 (Build 22617221) | Operator | **PASS** | 2026-10-05 | Guest IP & MAC fallback verified |
| **Test 3: Event Lifecycle** | 8.0.2 (Build 22617221) | Operator | **PASS** | 2026-10-05 | Provenance & soft-delete verified |
| **Test 4: Custom Attributes**| 8.0.2 (Build 22617221) | Operator | **PASS** | 2026-10-05 | Replace-set atomic update verified |
| **Test 5: Tags Sync** | 8.0.2 (Build 22617221) | Operator | **PASS** | 2026-10-05 | Tag diff-check verified |
| **Test 6: Disconnect/Reconnect**| 8.0.2 (Build 22617221)| Operator | **PASS** | 2026-10-05 | Guardian keepalive & resume verified |
| **Test 7: Stream Rollover**| 8.0.2 (Build 22617221)| Operator | **PASS** | 2026-10-05 | RESYNCING FSM verified |
| **All Tests (vSphere 7.0.x)**| Real 7.0.x Hardware | Human | `BLOCKED (human)` | Pending | No physical 7.0 environment available |
| **All Tests (vSphere 9.x)** | Real 9.x Hardware | Human | `BLOCKED (human)` | Pending | Unreleased / no hardware |
