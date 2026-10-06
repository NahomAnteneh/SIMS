# SIMS PostgreSQL High Availability (HA) Runbook

## 1. Architectural Overview & Design Goals

The SIMS database architecture requires continuous availability for both:
- **Collector ingestion (`collector_rw`):** High-throughput bulk ingest, lease coordination, and differential reconciliation.
- **Query API (`query_ro`):** Read-only search, topology navigation, and inventory queries.

To guarantee Zero Data Loss ($RPO = 0$) and minimal recovery time ($RTO < 30\text{s}$), SIMS supports two high-availability topologies based on target enterprise infrastructure:
1. **Kubernetes Deployments:** CloudNativePG (CNPG) Operator (v1.30.x with PostgreSQL 18).
2. **VM / Bare-Metal Deployments:** Patroni with etcd DCS (Distributed Configuration Store) and HAProxy VIP.

---

## 2. CloudNativePG (Kubernetes) Topology

### 2.1 Cluster Architecture
```text
               +----------------------------------------+
               |        Kubernetes Ingress / Service    |
               +----------------------------------------+
                             |              |
              Primary Service (Port 5432)   Replica Service (Port 5432)
                             |              |
                  +----------+              +------------+
                  |                                      |
                  v                                      v
          +---------------+             +----------------+  +----------------+
          | Pod: sims-db-1 |             | Pod: sims-db-2 |  | Pod: sims-db-3 |
          |   (Primary)    | === WAL ==> |   (Standby)    |  |   (Standby)    |
          +---------------+             +----------------+  +----------------+
                 |                              |                   |
          Persistent Volume              Persistent Volume   Persistent Volume
            Zone A                         Zone B              Zone C
```

- **Instances:** 3 instances deployed across failure domains / availability zones using pod anti-affinity.
- **Replication Mode:** Streaming physical replication. Quorum-based synchronous commit (`synchronous_commit: on`, `num_sync: 1`) to guarantee zero loss on primary failure.
- **Traffic Routing:**
  - `sims-db-rw` Service: Routes to current Primary for `collector_rw` role.
  - `sims-db-ro` Service: Load balances across healthy standby replicas for `query_ro` role.

### 2.2 CNPG Cluster Specification
```yaml
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: sims-postgres
  namespace: sims-system
spec:
  instances: 3
  imageName: ghcr.io/cloudnative-pg/postgresql:18.0
  postgresql:
    parameters:
      shared_buffers: "4GB"
      work_mem: "64MB"
      max_connections: "300"
      synchronous_commit: "on"
      archive_mode: "on"
      archive_timeout: "5min"
  storage:
    size: 250Gi
    storageClass: enterprise-fast-nvme
  affinity:
    podAntiAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchExpressions:
            - key: cnpg.io/cluster
              operator: In
              values:
              - sims-postgres
          topologyKey: topology.kubernetes.io/zone
```

---

## 3. Patroni + etcd (Virtual Machines / Bare-Metal)

For non-Kubernetes enterprise environments:
- **Distributed Consensus:** etcd v3 cluster (3 or 5 nodes).
- **Process Supervisor:** Patroni 4.x managing PostgreSQL 18 instances.
- **Failover Consensus:** Leader key lease in etcd (10s TTL, 2s heartbeat loop).
- **VIP & Routing:** HAProxy with health checks polling Patroni REST API (`/primary` for write, `/replica` for read) combined with Keepalived virtual IP.

---

## 4. Operational Procedures

### 4.1 Controlled / Planned Maintenance Switchover
A planned switchover promotes a standby with zero data loss and minimal pause (< 5 seconds).

**CloudNativePG:**
```bash
# 1. Inspect cluster status
kubectl cnpg status sims-postgres -n sims-system

# 2. Trigger switchover to a specific target standby (e.g. sims-postgres-2)
kubectl cnpg promote sims-postgres-2 -n sims-system

# 3. Observe promotion and old primary demotion/re-attach
kubectl cnpg status sims-postgres -n sims-system --watch
```

**Patroni:**
```bash
# 1. Check patronictl cluster topology
patronictl -c /etc/patroni/patroni.yml list

# 2. Initiate controlled switchover
patronictl -c /etc/patroni/patroni.yml switchover --master sims-db-1 --candidate sims-db-2 --scheduled now

# 3. Verify health
patronictl -c /etc/patroni/patroni.yml topology
```

### 4.2 Unplanned Primary Crash & Automated Failover
When the primary host crashes or network partitions:
1. **DCS Leases Expire:** After TTL expires without heartbeat (10 seconds), DCS marks master key free.
2. **Standby Election:** Standby with the highest WAL LSN position acquires leader lease and runs `pg_ctl promote`.
3. **Fencing / STONITH:** The crashed node is fenced to prevent split-brain.
4. **Service Endpoint Update:** Kubernetes endpoints / HAProxy health checks immediately redirect write traffic to the new primary.
5. **Client Auto-Reconnect:** SIMS `pgxpool` detects broken connections, backs off, re-resolves the primary IP, and reconnects within 2-5 seconds.
6. **Worker Lease Fencing:** SIMS worker lease coordinator (`internal/collector/lease.go`) uses database fencing versions to ensure no split-brain collector worker continues writing.

---

## 5. Failover Verification Drill & SLAs

### SLA Targets:
- **Maximum Failover Duration ($RTO$):** $\le 30\text{ seconds}$
- **Data Loss Tolerance ($RPO$):** $0\text{ transactions}$ (synchronous standby)

### Drill Verification Steps:
1. Run background scale test inserting synthetic telemetry batches (`go test -v ./tests/integration -run TestScaleS4`).
2. Simulate sudden primary failure:
   ```bash
   kubectl delete pod sims-postgres-1 --grace-period=0 --force -n sims-system
   ```
3. Record duration until write operations resume successfully on promoted primary.
4. Verify table row count and lease consistency:
   ```sql
   SELECT count(*) FROM virtual_machines;
   SELECT target_id, holder_id, fencing_token FROM collector_leases;
   ```
5. Confirm no duplicate leases exist and old primary cleanly joins as standby using `pg_rewind`.
