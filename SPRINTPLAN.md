# Implementation Sprint Plan

## Distributed vSphere Inventory & Search Platform

**Delivery model:** Scrum / trunk-based development
**Schedule:** Sprint 0 + 5 × 2-week delivery sprints
**Total:** approximately 10.5 weeks
**Target:** production-ready v1.0

---

# 1. Delivery strategy

The original implementation plan proposed Sprint 0 plus four two-week sprints.

The finalized architecture is now broader than that original implementation plan because it adds several deliberate changes:

```text
Normalized topology
COPY + staging persistence
sqlc + pgx instead of ORM
PostgreSQL worker leases
PropertyCollector / EventHistoryCollector separation
event overlap + deduplication
ltree folder hierarchy
int4multirange VLAN representation
typed search compiler
OIDC/RBAC
distributed collector fleet
1M+ scale testing
```

For that reason, the recommended delivery sequence is:

```text
Sprint 0   Foundations and architecture validation
Sprint 1   Data model + snapshot ingestion + first end-to-end slice
Sprint 2   Real-time ingestion + event recovery + resilience
Sprint 3   Search engine + API + production UI
Sprint 4   Multi-vCenter scale + reconciliation + HA
Sprint 5   Performance + security + chaos + production release
```

The goal is **not** to make every component production-complete before anything runs. The goal is to have a working vertical slice at the end of every sprint.

---

# 2. Final technology baseline

The implementation should pin versions rather than floating on `latest`.

| Component        | Baseline                                                   |
| ---------------- | ---------------------------------------------------------- |
| Go               | 1.27.1                                                     |
| govmomi          | 0.56.0                                                     |
| PostgreSQL       | 18.6                                                       |
| pgx              | 5.11.x                                                     |
| sqlc             | 1.31.1                                                     |
| Next.js          | 16.3.8                                                     |
| TypeScript       | current supported release compatible with Next.js baseline |
| OpenTelemetry Go | current stable                                             |
| Prometheus       | current stable                                             |
| Grafana          | current supported release                                  |
| CloudNativePG    | 1.30.x where Kubernetes is used                            |

Go 1.27.1 is the current Go 1.27 patch release and includes fixes across the compiler, runtime, networking, and standard library.

`govmomi` 0.56.0 is the current release and includes current vSphere bindings and `vcsim` fixes.

PostgreSQL 18.6 is the current supported minor release for PostgreSQL 18, and PostgreSQL recommends staying on the current minor release for a supported major version.

`pgx` 5.11.0 is the current 5.x release, while `sqlc` 1.31.1 is the current sqlc release I verified. sqlc explicitly supports generating type-safe Go code against `pgx/v5`.

Next.js 16.3.8 is the current Active LTS security release as of September 30, 2026.

---

# 3. Team structure

The implementation uses the five-person engineering shape from the original plan.

| Role                      | Primary ownership                                               |
| ------------------------- | --------------------------------------------------------------- |
| Collector Engineer A      | Worker lifecycle, sessions, PropertyCollector, streaming        |
| Collector Engineer B      | Topology, hydration, events, reconciliation                     |
| DB/Search Engineer        | Schema, migrations, indexes, sqlc, persistence, compiler        |
| DevOps/SRE                | CI/CD, environments, deployments, observability, security       |
| QA/SDET                   | Test harness, integration, replay, performance, fault injection |
| Product Owner / Tech Lead | Prioritization, architecture decisions, acceptance              |

The fifth engineer should not become a permanent "test-only" gatekeeper. QA works alongside developers from the first sprint.

---

# 4. Engineering rules for the entire project

These rules apply to every sprint.

## 4.1 Trunk-based development

```text
main
 │
 ├── short-lived feature branch
 │
 └── PR
      │
      ├── CI
      ├── review
      └── merge
```

Branches should normally live for less than two working days.

Feature flags are preferred to long-lived branches.

---

## 4.2 SQL is the database contract

No ORM is introduced.

Use:

```text
SQL migrations
      +
sqlc
      +
pgx
```

The SQL files are the source of truth and generated code is never edited manually. sqlc's model is explicitly SQL → generated type-safe Go code.

---

## 4.3 Migrations are forward-compatible

For production:

```text
expand
  ↓
deploy application compatible with old + new
  ↓
backfill
  ↓
switch reads/writes
  ↓
contract
```

Do not make application deployment depend on an irreversible destructive migration.

---

## 4.4 Every change gets tests at the lowest useful level

```text
pure business rule
    → unit test

DB query
    → PostgreSQL integration test

vSphere behavior
    → vcsim test

protocol/recovery behavior
    → fake/mocked vSphere layer

production-like interaction
    → real-vCenter test

performance
    → benchmark/load test

failure behavior
    → fault injection / chaos test
```

---

## 4.5 No credentials in source control

Secrets must only come from:

```text
environment
Kubernetes Secret
enterprise secret manager
local developer secret file excluded by git
```

Credentials never appear in:

```text
logs
traces
panic messages
test snapshots
API responses
metrics labels
```

---

## 4.6 Security scanning begins in Sprint 0

`govulncheck` should be part of CI. Go's official security guidance recommends it for identifying known vulnerabilities that affect reachable code.

---

## 4.7 Observability is implemented with the feature

OpenTelemetry is used as the application telemetry layer. Current OpenTelemetry Go status lists traces and metrics as stable; application-specific instrumentation should supplement framework/library instrumentation.

No story is considered complete without its operational signals.

---

# 5. Sprint 0 — Foundations and Architecture Validation

**Duration:** 3–4 working days

**Sprint goal:**

> The team can build, test, run, observe, and safely change the system, and every high-risk architectural assumption has a recorded decision.

---

## 5.1 S0.1 Repository bootstrap

### Implementation

Create:

```text
repo/
├── cmd/
│   ├── collector/
│   └── query-api/
├── internal/
├── db/
│   ├── migrations/
│   └── queries/
├── generated/
├── api/
├── web/
├── deploy/
├── tests/
└── docs/
```

Initialize:

```text
go.mod
go.work or separate modules according to final repo strategy
package boundaries
Makefile / task runner
.editorconfig
.gitignore
.golangci.yml
sqlc.yaml
OpenAPI definition
```

### Acceptance

* `go test ./...` works on an empty repository.
* `go vet ./...` passes.
* linting passes.
* `sqlc generate` is deterministic.
* frontend builds.
* generated code is reproducible.

---

# 6. S0.2 Toolchain pinning

Pin:

```text
Go version
PostgreSQL major/minor
govmomi
pgx
sqlc
Node
Next.js
package manager
container base images
```

Use:

```text
go.mod
go.sum
package-lock.json / pnpm-lock.yaml
Docker image digests where required
```

CI must fail if generated files differ from the committed versions.

---

# 7. S0.3 Local development environment

Provide one command:

```bash
make dev-up
```

which starts:

```text
PostgreSQL 18.6
vcsim
Prometheus
Grafana
collector
query-api
Next.js
```

A developer should be able to go from a clean machine to a working environment using the repository documentation.

---

# 8. S0.4 Database bootstrap

Create:

```text
000001_extensions.sql
```

Enable only extensions actually required:

```text
pg_trgm
ltree
```

Create roles:

```text
migration_owner
collector_rw
query_ro
```

Verify explicitly:

```sql
query_ro cannot INSERT
query_ro cannot UPDATE
query_ro cannot DELETE
collector_rw cannot modify schema
```

---

# 9. S0.5 sqlc bootstrap

Create:

```yaml
version: "2"

sql:
  - engine: postgresql
    schema: db/migrations
    queries: db/queries
    gen:
      go:
        package: db
        out: generated/db
        sql_package: pgx/v5
```

The project should use generated code rather than handwritten repetitive query wrappers.

---

# 10. S0.6 Real-vCenter capability matrix

Before building against assumptions, execute a short real-vCenter spike.

Record:

```text
vCenter version
ESXi version
TLS behavior
PropertyCollector behavior
EventHistoryCollector behavior
VDS support
guest.net availability
xvMotion behavior
large inventory size
session lifetime behavior
InvalidCollectorVersion behavior
```

Output:

```text
docs/vsphere-capability-matrix.md
```

This is critical because `vcsim` is not a perfect behavioral replacement for real vCenter. The original plan already identifies simulator fidelity as a major risk.

---

# 11. S0.7 Architecture Decision Records

Create ADRs:

```text
ADR-001 PostgreSQL as operational search store
ADR-002 No ORM
ADR-003 sqlc + pgx
ADR-004 COPY + staging persistence
ADR-005 No Redis in v1
ADR-006 No Kafka/NATS in v1
ADR-007 No mandatory gRPC
ADR-008 Collector lease model
ADR-009 PropertyCollector ownership
ADR-010 Event replay semantics
ADR-011 VM identity model
ADR-012 Topology normalization
ADR-013 Pagination model
ADR-014 Search indexing model
ADR-015 Authentication and scope model
```

---

# 12. S0.8 CI pipeline

Initial CI stages:

```text
format
↓
lint
↓
unit tests
↓
race tests
↓
sqlc verify/generate
↓
migration test
↓
PostgreSQL integration tests
↓
vcsim integration tests
↓
govulncheck
↓
container build
```

Security and generated-code checks must run on every pull request.

---

# 13. Sprint 0 exit criteria

Sprint 0 is complete when:

```text
[✓] repository builds
[✓] local stack starts
[✓] PostgreSQL migrations execute
[✓] sqlc generation is deterministic
[✓] CI is green
[✓] vcsim integration skeleton works
[✓] real vCenter access exists
[✓] capability matrix is documented
[✓] ADRs are approved
[✓] test environments are defined
```

---

# 14. Sprint 1 — Data Model, Hydration, Persistence, First Vertical Slice

**Duration:** 2 weeks

**Sprint goal:**

> Connect to one vCenter, retrieve the complete VM inventory, normalize it, persist it efficiently into PostgreSQL, and display it through the first working UI.

This sprint deliberately builds the **write path and database before sophisticated streaming**.

---

# 15. S1.1 Production database schema

Implement:

```text
vcenters
collector_targets
collector_leases
collector_state

datacenters
inventory_folders
clusters
hosts
datastores
networks
distributed_switches
portgroups

virtual_machines
vm_datastores
vm_network_interfaces

inventory_events
```

Do not add every conceivable vSphere object.

Only implement entities required for the v1 search experience.

---

# 16. S1.2 VM identity and constraints

Implement:

```text
internal id
instance_uuid
vcenter_id
moref
```

Constraints:

```text
UNIQUE(instance_uuid) WHERE active
UNIQUE(vcenter_id, moref) WHERE active
```

Add tests for:

```text
duplicate instance UUID
duplicate MoRef
soft-deleted identity reuse
xvMotion identity change
```

---

# 17. S1.3 Topology model

Implement normalized topology.

For folders:

```text
path ltree
```

For topology objects:

```text
ID
vCenter ID
MoRef
name
parent ID
metadata
```

For datastores and networks, use stable internal IDs.

The objective is to prevent a rename from triggering thousands of VM-row updates.

---

# 18. S1.4 Network model

Implement:

```text
vm_network_interfaces
```

with:

```text
MAC
IP inet
network
portgroup
VLAN information
device key
```

Indexes:

```text
MAC B-tree
IP GiST/SP-GiST
network_id B-tree
```

Test:

```text
IPv4
IPv6
multiple NICs
missing IP
multiple IPs per interface
VM with no guest networking
```

---

# 19. S1.5 VLAN model

Implement `int4multirange`.

Examples:

```text
100
100-110
100-110,200,300-305
```

Must normalize into equivalent range representation.

Tests must verify:

```text
overlap
contains
adjacent ranges
single VLAN
trunk range
multiple ranges
```

Do not expand ranges into thousands of individual integers.

---

# 20. S1.6 Session manager

Implement:

```text
Connect()
Login()
CurrentTime()
Logout()
Reconnect()
```

Implement:

```text
TLS validation
custom CA
certificate failure
timeout
context cancellation
credential redaction
```

The implementation should support:

```text
TLS 1.2 minimum
TLS 1.3 preferred
```

and make the policy configurable per deployment.

---

# 21. S1.7 Worker FSM

Implement the worker states:

```text
INIT
CONNECTING
SESSION_ACTIVE
TOPOLOGY_PREFLIGHT
FULL_INVENTORY_SYNC
DELTA_STREAMING
RESYNCING
DEGRADED
STOPPING
```

The original plan defined seven initial states; the final architecture adds explicit `RESYNCING` and lifecycle states because recovery is now a first-class operation.

Implement a table-driven transition model.

Illegal transitions return typed errors.

---

# 22. S1.8 Topology cache

Implement an immutable snapshot model:

```text
TopologySnapshot
```

containing lookup maps:

```text
MoRef → object
object ID → object
network ID → network
datastore ID → datastore
folder ID → folder
```

Readers should not block while a new topology snapshot is constructed.

Preferred implementation:

```text
build new snapshot
       ↓
atomic pointer swap
       ↓
old snapshot becomes GC eligible
```

This is preferable to a heavily contended global mutex for read-heavy access.

---

# 23. S1.9 Inventory hydration

Use:

```text
ContainerView
PropertyFilterSpec
TraversalSpec
PropertySpec
RetrievePropertiesEx
ContinueRetrievePropertiesEx
```

The retrieval continuation token and the PropertyCollector update version must remain separate concepts.

Use bounded page sizes.

Start with:

```text
maxObjects = 500
```

and make it configurable.

---

# 24. S1.10 Change normalization

Create an internal canonical model:

```go
type VMRecord struct {
    ID
    VCenterID
    InstanceUUID
    MoRef
    Name
    Hostname
    PowerState
    FolderID
    DatacenterID
    ClusterID
    HostID
    CPUCores
    MemoryMB
    ...
}
```

The vSphere object model must not leak directly into the database layer.

This creates an important isolation boundary:

```text
govmomi
   ↓
vSphere DTO
   ↓
normalized domain model
   ↓
persistence model
```

---

# 25. S1.11 Bulk persistence

Implement:

```text
batch
 ↓
COPY FROM STDIN
 ↓
staging table
 ↓
single transaction
 ↓
set-based merge/upsert
```

PostgreSQL explicitly documents `COPY` as the high-performance loading mechanism and exposes progress through `pg_stat_progress_copy`.

Initial flush defaults:

```text
500 records
or
250 ms
```

but both must be configurable.

---

# 26. S1.12 Batch compaction

Before writing:

```text
same VM
same batch
multiple updates
```

must collapse to:

```text
one final record
```

Use:

```text
instance_uuid → pending change
```

with deterministic field merge rules.

Tests:

```text
1 update
2 updates
100 updates
mixed property updates
delete + update
update + delete
```

---

# 27. S1.13 Minimal API

Implement:

```http
GET /healthz
GET /readyz
GET /metrics

GET /v1/vms
GET /v1/vms/{id}
```

The first VM endpoint should support:

```text
vcenter_id
name
limit
cursor
```

No generic AST yet.

---

# 28. S1.14 Minimal frontend

Create:

```text
/dashboard
/vms
/vms/[id]
```

The `/vms` page only needs:

```text
search field
results table
pagination
basic filters
```

It is intentionally not the finished UX.

---

# 29. S1.15 Sprint 1 test gate

Required:

```text
5,000-VM vcsim hydration
50,000-row synthetic DB
duplicate-batch tests
migration tests
query tests
container lifecycle tests
race detector
```

Acceptance:

```text
all VMs persisted
no duplicate identities
no leaked ContainerViews
query_ro cannot write
API returns hydrated inventory
frontend displays inventory
```

---

# 30. Sprint 1 demo

Demo:

```text
docker compose / local environment
       ↓
vcsim
       ↓
5,000 VMs
       ↓
hydration
       ↓
PostgreSQL
       ↓
API
       ↓
Next.js inventory screen
```

---

# 31. Sprint 2 — Real-Time Ingestion, Event Ledger, Recovery and Backpressure

**Duration:** 2 weeks

**Sprint goal:**

> The collector remains synchronized with a live vCenter through normal changes, reconnects, rollover, event replay, and database pressure without intentionally dropping updates.

This is the riskiest engineering sprint.

---

# 32. S2.1 PropertyCollector stream

Implement:

```text
CreateFilter
WaitForUpdatesEx
change application
version tracking
filter cleanup
```

Exactly one goroutine owns the streaming loop for a worker.

Broadcom's PropertyCollector documentation warns against concurrent `WaitForUpdates`/`WaitForUpdatesEx` operations in the same session.

The implementation must preserve this invariant.

---

# 33. S2.2 Incremental property application

For:

```text
Enter
Modify
Leave
```

implement:

### Enter

Load complete required state and persist.

### Modify

Update only changed property paths.

### Leave

Write:

```text
is_deleted = true
deleted_at = now()
```

Tests must prove a single-field modification cannot zero out unrelated fields.

---

# 34. S2.3 Truncated update handling

When:

```text
UpdateSet.truncated == true
```

process returned changes and continue according to the current PropertyCollector version semantics.

Do not treat truncation itself as an outage.

Track:

```text
collector_property_updates_truncated_total
```

---

# 35. S2.4 InvalidCollectorVersion recovery

Implement:

```text
STREAMING
 ↓
InvalidCollectorVersion
 ↓
RESYNCING
 ↓
destroy stale filter
 ↓
create new filter
 ↓
full/required rehydration
 ↓
reconcile
 ↓
new stream
```

Acceptance:

```text
inventory stays queryable
no duplicate identities
worker does not restart process-wide
recovery duration is measured
```

---

# 36. S2.5 Session guardian

Implement:

```text
CurrentTime heartbeat
```

with:

```text
60-second interval
2 consecutive failures → dead session
```

The thresholds must be configurable.

The worker then transitions into reconnect logic.

---

# 37. S2.6 Backoff

Use exponential backoff with jitter.

For example:

```text
base = 2s
cap = 60s
jitter = bounded
```

Use a deterministic injectable clock/random source in unit tests.

Test at least 1,000 generated delays per backoff stage.

---

# 38. S2.7 Resource cleanup registry

Every server-side object must register itself:

```text
ContainerView
PropertyFilter
EventHistoryCollector
```

in a worker-scoped cleanup registry.

Shutdown order:

```text
stop producer
stop streaming
destroy event collector
destroy property filter
destroy ContainerView
logout
close client
```

Test:

```text
normal shutdown
context cancellation
network failure
panic
authentication failure
InvalidCollectorVersion
```

---

# 39. S2.8 EventHistoryCollector

Implement event capture for the v1 event set.

Store:

```text
event_key
event_type
created_time
entity
entity_moref
payload
```

Events must be idempotent.

Unique key:

```text
(vcenter_id, event_key)
```

---

# 40. S2.9 Event restart/replay semantics

Do not assume an EventHistoryCollector survives client restart.

Persist:

```text
last_event_key
last_event_time
```

After restart:

```text
new collector
 ↓
retrieve from overlap window
 ↓
deduplicate by event identity
 ↓
process
```

This provides:

```text
at-least-once retrieval
+
deduplication
+
reconciliation
```

rather than pretending vSphere gives a durable broker offset.

---

# 41. S2.10 Field ownership rules

Explicitly define:

| Field                  | Owner                        |
| ---------------------- | ---------------------------- |
| power_state            | PropertyCollector            |
| hostname               | PropertyCollector            |
| host                   | PropertyCollector            |
| datastore relationship | PropertyCollector            |
| NIC/IP                 | PropertyCollector            |
| folder                 | PropertyCollector            |
| created_at             | Event layer                  |
| created_by             | Event layer                  |
| first_seen_at          | persistence layer            |
| last_seen_at           | reconciler                   |
| deleted_at             | PropertyCollector/reconciler |

Every repository method must respect these ownership boundaries.

---

# 42. S2.11 Network-interface updates

Only rewrite NIC rows when network state actually changes.

Transaction:

```text
VM update
+
delete previous NIC rows
+
insert new NIC rows
+
commit
```

Must be atomic.

Test:

```text
IP change
NIC addition
NIC removal
multiple NICs
IPv6
VM deletion
```

---

# 43. S2.12 Bounded queue

Implement:

```text
producer
    ↓
bounded queue
    ↓
batcher
```

Queue state:

```text
depth
capacity
fill_ratio
blocked_producers
```

When full:

```text
producer blocks
```

Never:

```text
drop oldest
drop newest
silent overwrite
```

The original plan explicitly requires bounded blocking rather than dropped events.

---

# 44. S2.13 Database outage behavior

Test:

```text
PostgreSQL unavailable for 10s
PostgreSQL unavailable for 60s
PostgreSQL slow
PostgreSQL connection exhausted
```

Verify:

```text
no intentional event drop
queue grows predictably
vSphere stream stays healthy within configured limits
recovery drains queue
memory remains bounded
```

---

# 45. Sprint 2 test gate

Mandatory:

```text
fault-injection matrix
event replay tests
InvalidCollectorVersion tests
network disconnect tests
database outage tests
resource leak tests
1-hour churn
race detector
```

---

# 46. Sprint 2 demo

On real vCenter:

```text
power on VM
power off VM
modify networking
create VM
clone VM
delete VM
disconnect collector
reconnect
force stream rollover
restart collector
```

The database must converge automatically.

---

# 47. Sprint 3 — Search Engine, Query API, Authentication and Production UX

**Duration:** 2 weeks

**Sprint goal:**

> Users can rapidly find VMs through exact identifiers, text, network, topology, and compound filters with deterministic pagination and authorization.

---

# 48. S3.1 Query schema and API contract

Define OpenAPI 3.1.

Endpoints:

```text
GET  /healthz
GET  /readyz
GET  /metrics

GET  /v1/search/vms
POST /v1/vms/search
GET  /v1/vms/{id}
```

Use schema validation before the search compiler.

---

# 49. S3.2 Search classifier

For:

```text
GET /v1/search/vms?q=...
```

classify:

```text
UUID
MoRef
IPv4
IPv6
MAC
exact name
hostname
substring
```

Avoid sending every search through one expensive generic SQL path.

---

# 50. S3.3 Filter AST

Implement:

```text
AND
OR
NOT
EQ
NE
IN
CONTAINS
PREFIX
CIDR_CONTAINS
CIDR_OVERLAP
VLAN_OVERLAP
DESCENDANTS_OF_FOLDER
```

AST must be typed.

Unknown attributes return:

```text
HTTP 400
```

The compiler must reject invalid combinations before SQL generation.

---

# 51. S3.4 SQL compiler

Compiler output:

```text
SQL template
+
ordered parameters
```

Never concatenate values.

The generated query must have deterministic structure so logically equivalent filters produce predictable query shapes.

---

# 52. S3.5 Index implementation

Create and test:

```text
B-tree
    instance_uuid
    moref
    hostname
    exact names
    foreign keys

GIN
    pg_trgm name/hostname

GiST
    folder path
    multiranges
    relevant hierarchy/network structures

SP-GiST/GiST
    IP network
```

Do not create indexes because they "might be useful."

Every index must correspond to a documented query pattern.

---

# 53. S3.6 Query plan regression tests

For every hot query:

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
```

Record:

```text
execution time
planning time
rows
buffers hit
buffers read
scan type
```

PostgreSQL's documentation explicitly recommends `EXPLAIN`/`EXPLAIN ANALYZE` for understanding query performance and using planner statistics appropriately.

Create baseline plan snapshots for release-critical queries.

---

# 54. S3.7 Keyset pagination

Support:

```text
(name, id)
(updated_at, id)
(first_seen_at, id)
```

Cursor contains:

```text
version
query hash
sort
direction
last value
last id
signature
```

Test:

```text
insert during pagination
delete during pagination
update during pagination
cursor tampering
query mismatch
expired cursor
```

---

# 55. S3.8 OIDC authentication

Implement:

```text
OIDC discovery
JWT verification
issuer validation
audience validation
expiry validation
key rotation
```

Do not call the identity provider for every request.

Cache verified key material according to the provider's key rotation behavior.

---

# 56. S3.9 Authorization and scope injection

Map user identity to:

```text
vCenter allow-list
folder scope
role
```

Compiler automatically injects scope predicates.

Negative tests must prove:

```text
user cannot remove scope using:
AND
OR
NOT
nested expressions
ID lookup
cursor
search shortcut
```

---

# 57. S3.10 Search API performance path

Hot query execution should look like:

```text
HTTP
 ↓
decode
 ↓
validate
 ↓
compile
 ↓
pgx/sqlc
 ↓
PostgreSQL
 ↓
encode
```

Avoid:

```text
reflection-heavy object mapping
N+1 queries
unbounded result sets
ORM query generation
```

---

# 58. S3.11 Production UI

Implement:

```text
global VM search
filter drawer
saved URL state
sort
pagination
details panel
VM detail page
loading/error/empty states
```

The result list must be virtualized.

Search input should use:

```text
debounce
AbortController
request cancellation
```

No stale result should overwrite a newer search.

---

# 59. S3.12 API result design

Search response:

```json
{
  "items": [],
  "nextCursor": "...",
  "hasMore": true
}
```

Each list item should contain only fields required for the table.

Detailed VM data is fetched separately.

---

# 60. Sprint 3 test gate

Required:

```text
AST fuzzing
cursor fuzzing
SQL injection tests
scope bypass tests
OpenAPI contract tests
API integration tests
frontend E2E tests
query plan regression tests
```

---

# 61. Sprint 3 demo

A user should be able to type:

```text
db-prod
10.20.10.15
00:50:56:...
prod-cluster
```

and then refine results with:

```text
power state
cluster
host
datastore
VLAN
subnet
folder
vCenter
```

The search must never call vCenter.

---

# 62. Sprint 4 — Multi-vCenter Scale, Leases, Reconciliation and HA

**Duration:** 2 weeks

**Sprint goal:**

> The platform can operate across multiple vCenters and collector instances while remaining self-healing and preventing duplicate ownership.

---

# 63. S4.1 Collector target registry

Implement:

```text
collector_targets
```

fields:

```text
vcenter_id
enabled
priority
limits
concurrency policy
```

API/admin path can be added later; initial configuration may be database/bootstrap driven.

---

# 64. S4.2 PostgreSQL worker leases

Implement lease acquisition transactionally.

Conceptually:

```text
attempt acquire
 ↓
if expired → takeover
if active → another worker owns it
```

Lease renewal:

```text
heartbeat
```

Lease TTL:

```text
short enough for failover
long enough to avoid false takeover
```

Test:

```text
worker crash
network partition
slow heartbeat
database reconnect
two workers racing for same target
```

---

# 65. S4.3 Target assignment

A worker should manage only targets it currently owns.

State:

```text
AVAILABLE
ASSIGNED
RUNNING
DEGRADED
REASSIGNING
```

The system should eventually converge to one active worker per target.

---

# 66. S4.4 LISTEN/NOTIFY configuration updates

Use PostgreSQL `LISTEN/NOTIFY` for fast configuration propagation.

But:

```text
NOTIFY = wake-up signal
database = source of truth
```

If a notification is missed, periodic reconciliation still discovers the change.

---

# 67. S4.5 Concurrency control

Protect vCenters from excessive hydration.

Global configuration:

```text
max concurrent hydrations
```

Per-vCenter:

```text
max concurrent API-heavy operations
```

Backpressure must exist at:

```text
target scheduling
hydration
ingestion
database
```

not only in the queue.

---

# 68. S4.6 Differential reconciliation

Implement:

```text
S_db \ S_vcenter
S_vcenter \ S_db
S_db ∩ S_vcenter
```

Actions:

```text
missing in vCenter
    → tombstone

missing in DB
    → enqueue hydration

present in both
    → touch last_seen_at
```

Do not load entire VM records when IDs are sufficient.

---

# 69. S4.7 Reconciliation triggers

Run reconciliation:

```text
every 24h
after serious stream recovery
after prolonged reconnect
after collector replacement
```

The 24-hour sweep from the original implementation plan remains the baseline.

---

# 70. S4.8 Cross-vCenter migration

Implement identity handling for:

```text
source Leave
target Enter
```

Test both event orders:

```text
Leave → Enter
Enter → Leave
```

Verify:

```text
one logical VM
updated vcenter_id
updated moref
stable internal ID where policy permits
```

No failed batch should result from a stale `(vcenter_id, moref)` unique constraint.

---

# 71. S4.9 Tombstone management

Create:

```text
partial index on deleted_at
```

Purge in bounded batches:

```text
DELETE ... LIMIT-equivalent batching strategy
```

Never perform an unbounded giant delete in production.

Measure:

```text
rows purged
duration
locks
dead tuples
vacuum effects
```

---

# 72. S4.10 PostgreSQL HA

If Kubernetes is already the corporate platform:

```text
CloudNativePG
```

is the preferred operational approach.

CloudNativePG 1.30.x currently supports PostgreSQL 18 and Kubernetes 1.34–1.36.

Do not introduce Kubernetes solely to run this system.

For a VM-based corporate environment, use the organization's existing PostgreSQL HA standard.

---

# 73. S4.11 Backup and restore

Implement:

```text
base backup
WAL archival
restore procedure
restore validation
RPO measurement
RTO measurement
```

At least one restore test must be executed.

---

# 74. Sprint 4 scale test

Run:

```text
3 vCenters
5 vCenters
10 vCenters
```

with realistic inventory sizes.

Measure:

```text
startup time
hydration concurrency
queue depth
DB write rate
query rate
memory
CPU
collector reconnect rate
```

---

# 75. Sprint 4 test gate

Required:

```text
worker takeover test
multi-vCenter test
xvMotion test
reconciliation convergence test
PostgreSQL failover test
backup restore test
10-target scale test
```

---

# 76. Sprint 4 demo

Demonstrate:

```text
vCenter A
vCenter B
vCenter C
       ↓
collector fleet
       ↓
PostgreSQL
       ↓
single global search
```

Then:

```text
kill collector
move VM
drop event
restart vCenter
fail PostgreSQL primary
```

The inventory must recover without manual repair.

---

# 77. Sprint 5 — Performance, Security, Chaos and Production Release

**Duration:** 2 weeks

**Sprint goal:**

> Prove the system against its SLOs, remove production blockers, complete operational documentation, and release v1.0.

The original plan reserves the final sprint for telemetry, performance, tuning, chaos, packaging, security and documentation.

The difference here is that much of that work has been started earlier; Sprint 5 is verification and hardening rather than "build observability from scratch."

---

# 78. S5.1 Performance dataset generator

Generate:

```text
100k active VMs
500k active VMs
1M active VMs
5M rows
10M rows
```

Distributions must include:

```text
large clusters
small clusters
repeated VM names
long folder trees
large datastores
large VLAN ranges
large IP subnets
IPv4
IPv6
large NIC counts
many vCenters
```

---

# 79. S5.2 Query workload model

Benchmark:

```text
exact UUID
MoRef
exact name
name prefix
name substring
hostname
IP
CIDR
MAC
VLAN overlap
datastore
cluster
folder subtree
compound filters
pagination
```

Workload should be mixed, not run as isolated microbenchmarks only.

---

# 80. S5.3 Query performance gate

Target:

```text
PostgreSQL P99 ≤ 5 ms
```

for the agreed indexed hot-path workload.

Also record:

```text
API P50
API P95
API P99
DB P50
DB P95
DB P99
```

A PostgreSQL query that is 4 ms while the HTTP endpoint is 80 ms is not considered "4 ms end-to-end."

---

# 81. S5.4 Ingestion performance gate

Measure:

```text
change
 ↓
worker receive
 ↓
queue
 ↓
COPY
 ↓
merge
 ↓
commit
```

Target:

```text
P95 change → durable DB ≤ 250 ms
batch write P95 ≤ 15 ms
batch write P99 ≤ 50 ms
```

The original specification established these core ingestion and query targets.

---

# 82. S5.5 PostgreSQL tuning

Tune only from measurement.

Investigate:

```text
shared buffers
work_mem
maintenance_work_mem
autovacuum
GIN pending lists
connection pool size
checkpoint settings
WAL
statistics targets
extended statistics
```

Evaluate indexes using:

```text
pg_stat_user_indexes
pg_stat_statements
EXPLAIN (ANALYZE, BUFFERS)
```

Do not blindly increase configuration values.

---

# 83. S5.6 Connection-pool tuning

Separate pool budgets for:

```text
query API
collector
reconciliation
administration
```

Prevent a reconciliation storm from consuming every database connection.

---

# 84. S5.7 OpenTelemetry instrumentation

Instrument:

```text
HTTP requests
search compilation
database queries
vCenter login
RetrievePropertiesEx
WaitForUpdatesEx
event retrieval
batch flush
COPY
merge transaction
reconciliation
worker lifecycle
```

Recommended traces:

```text
search request
 ├── auth
 ├── compile
 └── db query

collector update
 ├── receive
 ├── normalize
 ├── queue
 └── persist
```

OpenTelemetry recommends combining library instrumentation with application-specific instrumentation for deeper visibility.

---

# 85. S5.8 Security review

Verify:

```text
TLS configuration
OIDC validation
JWT key rotation
RBAC
DB roles
secret handling
SQL injection
cursor forgery
scope bypass
container security
dependency vulnerabilities
```

Run:

```text
govulncheck
SAST
container scanner
dependency audit
API fuzzing
```

No unresolved critical vulnerabilities.

---

# 86. S5.9 Chaos tests

Mandatory scenarios:

```text
vCenter unavailable
vCenter restarted
network partition
high latency
packet loss
PropertyCollector rollover
database unavailable
database slow
database failover
collector crash
collector split-brain attempt
clock skew
large event burst
large VM churn
```

Each test has:

```text
expected behavior
maximum recovery time
data-loss expectation
alert expectation
```

---

# 87. S5.10 72-hour soak

Run production-like workloads for 72 hours.

Monitor:

```text
RSS / heap
goroutine count
DB connections
queue depth
event rate
reconnections
collector state
index size
dead tuples
autovacuum
latency
error rate
```

The original plan already establishes a 72-hour soak as a release requirement.

Release is blocked if there is:

```text
memory growth trend
goroutine growth
server-side vSphere object leak
event loss
duplicate identity creation
persistent queue growth
query latency regression
```

---

# 88. S5.11 Deployment packaging

Build:

```text
collector container
query-api container
Next.js container
migration job
```

Containers must:

```text
run non-root
use minimal images
have read-only filesystem where practical
drop unnecessary Linux capabilities
set resource requests/limits
define liveness/readiness probes
handle SIGTERM
```

---

# 89. S5.12 Rolling deployment behavior

Verify:

```text
Query API rolling update
Collector rolling update
DB migration
```

Collectors must shut down gracefully:

```text
stop accepting target work
finish bounded in-flight persistence
release lease
destroy vSphere resources
logout
exit
```

No manual cleanup should be needed.

---

# 90. S5.13 Documentation

Deliver:

```text
architecture.md
deployment.md
local-development.md
vcenter-onboarding.md
database.md
api.md
security.md
troubleshooting.md
reconciliation.md
backup-restore.md
upgrade.md
on-call.md
```

Runbooks must include:

```text
symptom
diagnosis
commands
dashboards
decision tree
recovery
escalation
```

---

# 91. S5.14 Release candidate

Create:

```text
v1.0.0-rc1
```

Deploy to staging.

Run:

```text
full smoke
full integration
performance gate
chaos subset
restore test
security scan
upgrade test
rollback test
```

Only then tag:

```text
v1.0.0
```

---

# 92. Sprint 5 final gate

Production release requires:

```text
[✓] query SLO verified
[✓] ingestion SLO verified
[✓] no event loss
[✓] no duplicate identity
[✓] 72h soak passed
[✓] DB failover passed
[✓] restore test passed
[✓] security review passed
[✓] runbooks complete
[✓] dashboards/alerts validated
[✓] rolling upgrade passed
[✓] rollback tested
```

---

# 93. Cross-sprint dependency map

```text
S0
 │
 ├── toolchain
 ├── CI
 ├── DB foundation
 ├── vSphere capability matrix
 └── ADRs
      │
      ▼
S1
 │
 ├── schema
 ├── topology
 ├── session
 ├── hydration
 ├── persistence
 └── first API/UI
      │
      ▼
S2
 │
 ├── PropertyCollector
 ├── EventHistoryCollector
 ├── recovery
 ├── backpressure
 └── replay
      │
      ├───────────────┐
      ▼               ▼
S3                   S4
 │                    │
 ├── search            ├── leases
 ├── API               ├── multi-vCenter
 ├── auth              ├── reconciliation
 └── UI                └── HA
      │                    │
      └──────────┬─────────┘
                 ▼
                S5
                 │
        performance/security
        chaos/soak/release
```

---

# 94. Parallel work by sprint

## Sprint 1

```text
Collector A
    session + FSM

Collector B
    topology + hydration

DB Engineer
    schema + sqlc + COPY

SRE
    CI + local stack

QA
    vcsim + PostgreSQL integration
```

## Sprint 2

```text
Collector A
    PropertyCollector + recovery

Collector B
    events + NIC sync

DB Engineer
    staging + event ledger

SRE
    telemetry + fault environment

QA
    replay + failure injection
```

## Sprint 3

```text
Collector engineers
    streaming stabilization

DB Engineer
    AST + indexes + pagination

SRE
    OIDC + deployment

QA
    fuzz + API contract + E2E

Frontend work
    search UX
```

## Sprint 4

```text
Collector A/B
    leases + multi-vCenter

DB Engineer
    reconciliation + HA

SRE
    database failover + backups

QA
    multi-target chaos

Frontend
    topology/detail UX
```

## Sprint 5

All engineers shift toward:

```text
benchmark
profiling
failure testing
documentation
production fixes
```

No large new features should enter Sprint 5.

---

# 95. Definition of Ready

A story is Ready only when:

```text
business/technical outcome is explicit
dependencies are known
API/schema changes are known
test strategy exists
acceptance criteria are measurable
observability requirements are identified
security implications are understood
rollback behavior is understood
```

---

# 96. Definition of Done

A story is Done only when:

```text
code reviewed
tests pass
race detector passes where applicable
integration test exists for I/O
failure behavior tested
metrics/traces/logging exist
security implications addressed
documentation updated
generated code regenerated
migration tested
deployed to shared environment
demonstrated
```

This follows the original plan's principle that stories must include failure-path tests, telemetry and documentation rather than only happy-path functionality.

---

# 97. Pull-request standards

Every PR should contain:

```text
What changed
Why
How it was tested
Performance impact
DB migration impact
Operational impact
Rollback considerations
```

For SQL changes:

```text
EXPLAIN output
index justification
expected cardinality
lock considerations
migration cost
```

For collector changes:

```text
state transition impact
vSphere resource lifecycle
reconnect behavior
event ordering implications
```

---

# 98. Database-change checklist

Every schema PR must answer:

```text
Does this require a migration?
Does it lock a large table?
Can it be deployed online?
Does it require backfill?
Does it need an index?
Can the index be built safely?
Does it affect replication?
Does sqlc generation change?
Does the query plan change?
```

For large indexes, evaluate online creation and maintenance behavior rather than blindly creating them during peak traffic.

---

# 99. Performance engineering rules

The team should never optimize based purely on intuition.

The workflow is:

```text
measure
 ↓
profile
 ↓
form hypothesis
 ↓
change one variable
 ↓
benchmark
 ↓
compare
 ↓
keep/revert
```

Benchmark reports must record:

```text
commit SHA
dataset size
hardware
PostgreSQL configuration
query mix
concurrency
result
```

---

# 100. Reliability engineering rules

Every asynchronous component must answer:

```text
What happens if it stops?
What happens if it restarts?
What happens if the dependency is slow?
What happens if the dependency disappears?
How does it recover?
How do we know it recovered?
```

This applies to:

```text
collector
worker
queue
batcher
event tailer
reconciler
query API
database
frontend
```

---

# 101. Operational SLOs

The final v1 targets are:

| Metric                             |             Target |
| ---------------------------------- | -----------------: |
| Indexed PostgreSQL query P99       |             ≤ 5 ms |
| Query API P99                      |            ≤ 15 ms |
| Typical search response            | ≤ 100 ms perceived |
| Batch write P95                    |            ≤ 15 ms |
| Batch write P99                    |            ≤ 50 ms |
| Change → DB P95                    |           ≤ 250 ms |
| Target rebalance                   |            ≤ 5 sec |
| Dropped events                     |                  0 |
| Unauthorized data access           |                  0 |
| Unplanned collector restart        |                  0 |
| Server-side vSphere resource leaks |                  0 |

The original specification's core SLA values remain the authoritative starting point for the database/write path and ingestion behavior.

---

# 102. Alert strategy

Critical alerts:

```text
dropped event > 0
collector target stale
repeated InvalidCollectorVersion
queue > 80%
queue continuously increasing
database unavailable
database replication unhealthy
query P99 above threshold
write P99 above threshold
unexpected inventory count drop
event stream silent on active target
lease conflict
restore failure
```

Every production alert must link to a runbook.

---

# 103. Release gates

There are four formal release gates.

## Gate 1 — Functional

```text
all critical features work
```

## Gate 2 — Correctness

```text
replay
reconciliation
event ordering
identity invariants
scope enforcement
```

## Gate 3 — Performance

```text
query
ingestion
startup
failover
```

## Gate 4 — Operational

```text
alerts
backup
restore
upgrade
rollback
runbooks
security
```

A release that passes functionality but fails correctness or operational gates does not ship.

---

# 104. Risk-driven testing matrix

| Risk                       | Test                      |
| -------------------------- | ------------------------- |
| `vcsim` fidelity           | nightly real-vCenter      |
| PropertyCollector rollover | fault injection           |
| event collector restart    | replay + overlap          |
| VM identity collision      | database integration      |
| xvMotion ordering          | event permutations        |
| queue saturation           | burst test                |
| DB outage                  | integration + chaos       |
| query regression           | EXPLAIN baseline          |
| scope bypass               | negative API tests        |
| credential leakage         | log/security tests        |
| vSphere resource leaks     | repeated worker lifecycle |
| memory growth              | 72-hour soak              |

The original risk register already identifies simulator fidelity, server-side object leaks, boot storms, query latency, schema changes, and topology ambiguity as major risks.

---

# 105. What should NOT be scheduled as "nice to have" work during v1

Do not allow these to consume sprint capacity:

```text
Redis integration
Kafka/NATS integration
gRPC
advanced analytics
historical reporting
multi-region
mobile UI
complex customization
generic CMDB integrations
```

They are future architecture extensions.

The v1 objective is:

```text
FAST
CORRECT
SEARCHABLE
SELF-HEALING
OPERABLE
```

---

# 106. Final project milestone sequence

```text
DAY 1–4
Sprint 0
Foundation
        ↓
WEEK 1–2
Sprint 1
Snapshot inventory
        ↓
WEEK 3–4
Sprint 2
Real-time synchronization
        ↓
WEEK 5–6
Sprint 3
Search + API + UI
        ↓
WEEK 7–8
Sprint 4
Multi-vCenter + HA
        ↓
WEEK 9–10
Sprint 5
Performance + security + chaos
        ↓
~DAY 53
v1.0
```

---

# 107. v1.0 release definition

The product is ready for production only when a new environment can perform this sequence successfully:

```text
1. Configure vCenter
2. Collector acquires target
3. Authenticate
4. Discover topology
5. Hydrate inventory
6. Persist inventory
7. Start delta stream
8. Start event stream
9. Expose searchable inventory
10. User authenticates
11. Search VM
12. Open VM details
13. Modify VM in vCenter
14. Observe updated state
15. Restart collector
16. Verify recovery
17. Drop an event
18. Run reconciliation
19. Verify convergence
20. Fail database primary
21. Verify service recovery
22. Upgrade application
23. Verify no inventory loss
```

That entire path becomes the final release smoke test.

---

# 108. Recommended implementation order inside the repository

The team should implement the repository in this order:

```text
1. Database migrations
2. Domain types
3. sqlc generation
4. PostgreSQL repositories
5. vSphere session layer
6. topology provider
7. hydration
8. normalization
9. staging/COPY writer
10. worker state machine
11. PropertyCollector
12. event ledger
13. recovery/reconciliation
14. search compiler
15. REST API
16. auth/scope
17. frontend
18. HA/deployment
19. performance
20. chaos/release
```

This minimizes circular dependencies.

---

# 109. The most important engineering invariant

Throughout development, maintain this invariant:

```text
vCenter
    ↓
may temporarily disagree with
    ↓
PostgreSQL
    ↓
but the system must converge automatically
```

The platform is therefore not judged solely by:

> "Did we receive the event?"

It is judged by:

> "After any supported sequence of failures and recoveries, does the catalog converge to the actual vSphere inventory?"

That is the fundamental correctness property of the system.

---

# 110. Final delivery outcome

At the end of the project, the team should have:

```text
Go collector fleet
        +
govmomi integration
        +
normalized PostgreSQL inventory
        +
sqlc/pgx persistence
        +
PropertyCollector streaming
        +
EventHistoryCollector event ledger
        +
reconciliation
        +
distributed target leases
        +
high-performance SQL search
        +
OIDC/RBAC
        +
Next.js inventory UI
        +
OpenTelemetry
        +
HA deployment
        +
load/chaos/soak evidence
        +
production runbooks
```

The result is a **single operational inventory platform**, not merely a vSphere synchronization script.

The implementation should remain intentionally conservative about infrastructure: PostgreSQL is the durable system of record, in-process Go buffering handles the low-latency write path, and additional distributed infrastructure is introduced only when measured requirements justify it.
