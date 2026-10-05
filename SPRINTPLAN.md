# Implementation Sprint Plan

## Distributed vSphere Inventory & Search Platform

**Delivery model:** Scrum / trunk-based development
**Schedule:** Sprint 0 + 5 × 2-week delivery sprints
**Total:** approximately 10.5 weeks
**Target:** production-ready v1.0
**Revision:** 2 — October 2026 (updated after Sprint 0, Sprint 1 and the tags/custom-attributes feature were delivered)
**Frontend:** out of scope — built by a separate team; this plan delivers the backend and a complete, versioned API contract

---

## Revision 2 summary

This revision keeps the original sprint structure and updates it with what was delivered and decided.

| Change | Effect on the plan |
| ------ | ------------------ |
| Sprint 0 and Sprint 1 delivered | Marked as delivered; measured results and findings recorded |
| vSphere tags and custom attributes added | New data model and REST tag fetcher (delivered); synchronization added to Sprints 2 and 4; filters added to Sprint 3 |
| Frontend moved to another team | All UI stories removed; replaced by "complete API" stories and an API contract process |
| vCenter registration API added | Admin endpoints, connection validation, credential handling and audit log (Sprint 3), wired to leases and assignment (Sprint 4) |
| vSphere version policy (7.0+) | Support policy for vCenter Server 7.0 and above (no ceiling), capability flags, and capability matrix |
| Verification discipline | Every claim backed by a command that was actually run; evidence kept in sprint reports |

Markers used in this document:

```text
[Delivered]  built and reported in a sprint report
[Added]      new story or section in this revision
[Changed]    existing story changed in this revision
[Proposed]   needs a decision (usually an ADR) before implementation
```

Schedule: unchanged at about 10.5 weeks. Sprint 3 was rebalanced (UI stories removed, API completeness and vCenter management added); see the scope note in Sprint 3.

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
vSphere tags (REST) and custom attributes (SOAP)
API-first delivery (frontend built by another team)
vCenter registration API with write-only, encrypted credentials
vCenter Server 7.0 and above support with capability detection
```

For that reason, the recommended delivery sequence is:

```text
Sprint 0   Foundations and architecture validation                [Delivered]
Sprint 1   Data model + snapshot ingestion + first end-to-end API slice   [Delivered, plus tags/custom attributes]
Sprint 2   Real-time ingestion + event recovery + resilience + tag/attribute sync
Sprint 3   Search engine + complete API + auth + vCenter management API
Sprint 4   Multi-vCenter scale + reconciliation + HA + vCenter lifecycle
Sprint 5   Performance + security + chaos + production release
```

The goal is **not** to make every component production-complete before anything runs. The goal is to have a working vertical slice at the end of every sprint. Because the frontend is built by another team, that slice is demonstrated through the API (curl, contract tests and the published OpenAPI document), not through a UI.

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
| goose            | 3.28.0 (migrations; ADR-003) |
| golangci-lint    | 2.14.0 |
| govulncheck      | 1.8.0 |
| vSphere (managed) | vCenter Server 7.0 and above (see 2.1) |
| Frontend (Next.js / TypeScript) | Owned by the frontend team; not pinned or maintained by this plan |
| OpenTelemetry Go | current stable                                             |
| Prometheus       | current stable                                             |
| Grafana          | current supported release                                  |
| CloudNativePG    | 1.30.x where Kubernetes is used                            |

Go 1.27.1 is the current Go 1.27 patch release and includes fixes across the compiler, runtime, networking, and standard library.

`govmomi` 0.56.0 is the current release and includes current vSphere bindings and `vcsim` fixes.

PostgreSQL 18.6 is the current supported minor release for PostgreSQL 18, and PostgreSQL recommends staying on the current minor release for a supported major version.

`pgx` 5.11.0 is the current 5.x release, while `sqlc` 1.31.1 is the current sqlc release I verified. sqlc explicitly supports generating type-safe Go code against `pgx/v5`.

Versions in this table were pinned and checked against upstream registries during Sprint 0 (see the Sprint 0 report). The frontend stack is chosen and maintained by the frontend team.

---

## 2.1 vSphere version support policy [Added]

The platform supports VMware vCenter Server **7.0 and above**, with **no upper version ceiling**. It is designed to operate seamlessly across mixed fleets spanning multiple major versions.

| Detected version | Policy |
| ---------------- | ------ |
| Below 7.0 | Rejected at registration / connect with a typed error (`unsupported_version`) |
| 7.0 and above | Accepted. Real-vCenter behavioral validation is tracked in `docs/vsphere-capability-matrix.md`. No upper version ceiling. |

Rules:

```text
version is DETECTED at connect time from ServiceContent.About, never typed in by hand:
    product version, API version, build, API type, product line ID
record major.minor (low cardinality) in logs, a metric label (sims_vcenter_info), and collector_state
version details are stored on the vcenters row and exposed by the API (S3.13)
behavior differences are handled by capability flags, not by scattered version checks or ceiling guards
    e.g. tagging REST endpoint style (/rest vs /api), PropertyCollector behavior, event types
the SOAP (vim25) client negotiates the API version; the client library does not pin a version a modern vCenter would reject
the tagging REST endpoints must be verified across available supported vCenter versions before relying on them
failed or unknown capability detection degrades that feature gracefully (e.g. ErrTaggingUnavailable), not the whole vCenter
the platform only reads from vCenter; it never changes vCenter state
```

Test matrix:

```text
vcsim                    every pull request (fast, in-process / container, not version-faithful)
real vCenter runs        nightly and before each release (using whichever real vCenters the team has; record which)
mixed fleet              Sprint 4 scale test and cross-vCenter migration test (an older and a newer supported vCenter)
```

The exact patch levels covered and behavior differences are recorded in `docs/vsphere-capability-matrix.md` (one column per tested version) and documented in `docs/supported-versions.md`.

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

## 3.1 External team: Frontend [Changed]

The web UI is built by a separate frontend team. It is **not** part of this plan's capacity.

```text
Backend team owns                          Frontend team owns
-----------------                          ------------------
REST API and OpenAPI contract              everything under web/
API behavior, errors, pagination           UI design, state, routing, components
API documentation and examples             UI tests
CORS and authentication configuration      the web container and its deployment
contract changelog and deprecations        consuming the contract
```

Working agreement:

```text
backend work does not modify the web/ directory (checked in review and, once added, in CI)
the OpenAPI document is the integration contract; changes follow section 4.8
the frontend team reviews API changes at the start of each sprint, before the contract is frozen for that sprint
contract owner: DB/Search Engineer, with Tech Lead sign-off
```

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

**Observed deviation [Delivered]:** Sprint 1 and the tags/custom-attributes feature were each built on a single longer-lived branch (small commits, CI green at each step). From Sprint 2 onward, split work into per-story branches and pull requests so the two-day rule holds and reviews stay small.

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

### Credentials registered through the API [Proposed — ADR-017]

vCenters are registered at runtime through the API (S3.13), so the platform must store vCenter credentials. The rules:

```text
credentials are WRITE-ONLY through the API: accepted on create/update, never returned
stored encrypted at rest (envelope encryption; the key comes from environment, Kubernetes Secret or a secret manager)
    or stored as a reference to an external secret where the deployment has a secret manager
decrypted only inside the collector process that owns the target, and only while connecting
never written to logs, traces, metrics labels, audit events, error messages or API responses
rotation works without restarting the collector (an update triggers a controlled reconnect)
the platform's vCenter account is READ-ONLY; the platform never changes vCenter state
```

The storage mechanism is an open decision (ADR-017). Until it is accepted, vCenter credentials continue to come from the existing configuration sources.

---

## 4.6 Security scanning begins in Sprint 0

`govulncheck` should be part of CI. Go's official security guidance recommends it for identifying known vulnerabilities that affect reachable code.

---

## 4.7 Observability is implemented with the feature

OpenTelemetry is used as the application telemetry layer. Current OpenTelemetry Go status lists traces and metrics as stable; application-specific instrumentation should supplement framework/library instrumentation.

No story is considered complete without its operational signals.

---

## 4.8 API-first contract rules [Added]

The frontend is built by another team, so the API is the product. These rules apply from Sprint 1 onward.

```text
api/openapi.yaml (OpenAPI 3.1) is the single source of truth for the contract
every endpoint, parameter, field, enum and error is in the contract before it ships
every response is validated against the contract in tests (contract tests run in CI)
the contract and the implementation change in the same pull request
```

Compatibility within /v1:

```text
additive changes only: new endpoints, new optional fields, new optional parameters
enums are documented as extensible (clients must tolerate new values)
no removing or renaming fields, no changing types, no changing meaning
a breaking change needs a new version (/v2) or an announced deprecation window
deprecations are marked in the contract and returned in a response header
every contract change is recorded in docs/api-changelog.md
```

Consistent behavior across all endpoints:

```text
errors:        one machine-readable problem format (RFC 9457 problem details) with stable error codes
pagination:    cursor/keyset everywhere, opaque cursors, documented maximum page size
identifiers:   stable internal IDs in every resource; MoRef and instance UUID exposed as attributes
timestamps:    RFC 3339, UTC
field control: list endpoints return table fields by default; documented fields/include parameters opt into more (bounded); detail endpoints return full resources
request ID:    returned on every response and present in logs and traces
auth:          the same bearer-token scheme and the same 401/403 semantics everywhere
CORS:          configured by environment, restrictive by default, documented
```

For the frontend team:

```text
a published OpenAPI document per release
runnable examples for every endpoint in docs/api.md
a changelog and deprecation notices
a mock server built from the contract (for example with Prism), so UI work does not wait for backend sprints
```

---

## 4.9 Repository boundaries [Added]

```text
web/ belongs to the frontend team
backend pull requests and automation do not modify web/
[Proposed] CODEOWNERS entry for web/ and a CI guard that fails a backend PR touching web/
the frontend scaffold delivered in Sprint 0 stays as it is until the frontend team takes it over
```

---

## 4.10 Verification discipline [Added]

```text
"done" is backed by a command that was run and its recorded output
versions, library APIs and SQL features are checked against official documentation or source before use
no test is skipped, loosened or disabled to reach green
anything that cannot be verified in the available environment is marked BLOCKED with the reason, never assumed
each sprint ends with a report: what was done, measured numbers, deviations, what could not be verified, risks
```

---

# 5. Sprint 0 — Foundations and Architecture Validation

**Duration:** 3–4 working days

**Sprint goal:**

> The team can build, test, run, observe, and safely change the system, and every high-risk architectural assumption has a recorded decision.

**Status [Delivered]:** see the Sprint 0 report. Two items need a person and remain open (listed under "Sprint 0 exit criteria").

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
* the frontend scaffold builds. After Sprint 0 the scaffold belongs to the frontend team and backend work does not modify it (section 3.1).
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

[Changed] Node, Next.js and the package manager were pinned in Sprint 0 for the scaffold. From here on they are the frontend team's to maintain (section 3.1).

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
```

The web container is built and owned by the frontend team and is not part of the backend's definition of a working stack. [Proposed] Make it an optional compose profile so backend work never depends on it. (Sprint 0 delivered a `sims-web` service in the default stack.)

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
tagging REST API availability and path style
tag attach/detach visibility (events, PropertyCollector)
custom attributes (customValue, CustomFieldsManager changes)
minimum privileges for a read-only account
```

Output:

```text
docs/vsphere-capability-matrix.md
```

[Changed] Run the spike once per available supported vCenter version (an older and a newer supported vCenter). The matrix has a vcsim column plus one column per tested real version; differences between versions become capability flags (section 2.1).

[Delivered] The spike CLI (`cmd/vcenter-spike`) and the vcsim column exist. The real-vCenter columns are still pending (blocked on access). Finding: vcsim hangs on an unknown PropertyCollector version token instead of returning `InvalidCollectorVersion`, so only a real vCenter can show the true behavior.

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
ADR-016 Tags (REST) and custom attributes (SOAP) synchronization    [drafted, Proposed]
ADR-017 vCenter credential storage and rotation                      [Added, to draft]
ADR-018 vSphere version support policy and capability flags          [Added, to draft]
ADR-019 API contract, versioning and deprecation policy              [Added, to draft]
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

[Proposed] Add a guard that fails a backend pull request which touches `web/` (section 4.9).

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

**Status [Delivered]:** all criteria are met (CI is green on GitHub Actions) except two that need a person:

```text
real-vCenter capability run across available supported versions (the spike CLI exists; only the run is missing)
approval of the ADRs (all are Proposed)
```

Decisions made during delivery:

```text
migrations:        goose (single-file up/down, parsed by sqlc)               ADR-003
CI:                GitHub Actions; steps call Makefile targets
Go module:         one root module (github.com/infrastructure/sims); web/ is separate
local PostgreSQL:  host port 5433 (configurable) to avoid clashing with a host PostgreSQL
PostgreSQL 18:     the data volume mounts at /var/lib/postgresql, not .../data
```

---

# 14. Sprint 1 — Data Model, Hydration, Persistence, First Vertical Slice

**Duration:** 2 weeks

**Sprint goal:**

> Connect to one vCenter, retrieve the complete VM inventory, normalize it, persist it efficiently into PostgreSQL, and expose it through the first working API.

**Status [Delivered]:** see the Sprint 1 report. [Changed] The original goal ended with "display it through the first working UI"; the UI belongs to the frontend team.

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

[Delivered] Migrations 000002–000005 (collector tables, topology, virtual machines and attachments, default privileges). Tags and custom attributes were added afterwards by migration 000006 (S1.16).

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

[Delivered] Keyset pagination ordered by (name, id); opaque cursors; page size clamped to 1–100 (default 20); the API connects as `query_ro` only; CORS configurable by environment; OpenAPI 3.1 contract test against real responses.

---

# 28. S1.14 Frontend removed; API consumer enablement [Changed]

The web UI is built by another team (section 3.1). The original story (`/dashboard`, `/vms`, `/vms/[id]`) is removed, and backend work does not build or modify anything under `web/`.

It is replaced by a backend story:

```text
api/openapi.yaml    accurate request/response schemas, errors and examples
docs/api.md         curl examples for every endpoint
CORS                configurable by environment, restrictive by default
contract tests      real responses validated against the contract
```

Acceptance:

```text
the frontend team can build against the contract without asking the backend team how an endpoint behaves
```

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
responses validate against api/openapi.yaml
```

## Sprint 1 measured results [Delivered]

| Test | Result |
| ---- | ------ |
| 5,000-VM vcsim hydration and persistence | 4.76 s (about 1,050 VMs/s); heap 108 MB → 120 MB; replaying every batch left exactly 5,000 rows |
| 50,000-row synthetic persistence | 2.24 s (22,307 records/s) |
| ContainerView lifecycle | zero views left after success, fault and cancellation |
| Race detector, lint, sqlc-verify | clean |

Findings carried forward:

```text
pgx binary COPY cannot write ltree directly: the staging column is TEXT and the merge casts with ::ltree
vcsim with 5,000 VMs initializes slowly unless Autostart is disabled (over 7 minutes → under 2 minutes)
vcsim hangs on an unknown PropertyCollector version token: every SOAP call needs a deadline
vcsim always fills guest.net; real vCenter may return it empty (no VMware Tools, powered off):
    MAC extraction from VirtualEthernetCard devices was implemented as a fallback; confirm on real vCenter
```

Follow-up before Sprint 3:

```text
The (vcenter_id, moref) lookup used idx_vms_vcenter_name_id_active with a filter instead of the unique
partial index uq_vms_vcenter_moref_active. Run ANALYZE and re-check on a realistic dataset; if the planner
still avoids it, fix the query shape or the index. MoRef lookup is a hot path for the Sprint 3 search classifier.
```

---

# 30. S1.16 Tags and custom attributes [Added — Delivered]

Delivered after Sprint 1 on branch `feature/vm-tags-custom-attributes` (see the tags/custom-attributes report).

The two are different kinds of data and come from different APIs:

| Data | Source | Delivered by the PropertyCollector stream? |
| ---- | ------ | ------------------------------------------ |
| Custom attributes (key/value) | VM `customValue` property; names from `CustomFieldsManager.field` | Yes, it is a VM property |
| Tags (category + tag) | vSphere Automation REST API (`vapi/rest`, `vapi/tags`) | No — separate API and session |

## Schema

```text
vm_tags                 vm_id, tag_id, category_name, tag_name   UNIQUE(vm_id, tag_id)
vm_custom_attributes    vm_id, name, value                       UNIQUE(vm_id, name)
```

Indexes, each tied to a query pattern:

```text
(category_name, tag_name, vm_id)    "find VMs by category + tag"
(name, value, vm_id)                "find VMs by attribute name + value"
```

Privileges follow the existing rule: collector_rw DML only, query_ro SELECT only.

## Behavior

```text
custom attributes: key → name resolved at normalization time using the topology snapshot
tags: bulk lookup in bounded batches (500 objects), every call under a context deadline
the tag REST service failing NEVER fails inventory hydration:
    typed ErrTaggingUnavailable → warning → metric → tags marked degraded → hydration continues
the REST session is logged out on every exit path
persistence: replace-set per VM inside the same transaction as the VM merge, via COPY + staging
compaction: the final record of a batch carries the final tag and attribute sets
```

## API

```text
GET /v1/vms/{id}    returns tags[] and custom_attributes[]
GET /v1/vms         unchanged (the list stays light)
```

## Metrics

```text
sims_tag_fetch_duration_seconds
sims_tag_fetch_failures_total
sims_tags_persisted_total
sims_custom_attributes_persisted_total
```

## Known limits (carried into later sprints)

```text
tags are NOT streamed: a tag attach/detach does not arrive through PropertyCollector
    → S2.14 adds tag synchronization; S4.12 adds tag reconciliation
filtering by tag or attribute is not in the API yet → S3.3
tagging on real vCenter across supported versions is unverified (vcsim only) → capability matrix
ADR-016 (Proposed) records the REST/SOAP split and field ownership
```

---

# 31. Sprint 1 demo

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
documented API calls (curl)
```

---

# 32. Sprint 2 — Real-Time Ingestion, Event Ledger, Recovery and Backpressure

**Duration:** 2 weeks

**Sprint goal:**

> The collector remains synchronized with a live vCenter through normal changes, reconnects, rollover, event replay, and database pressure without intentionally dropping updates.

This is the riskiest engineering sprint.

---

# 33. S2.1 PropertyCollector stream

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

# 34. S2.2 Incremental property application

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

Tests must prove a single-field modification cannot zero out unrelated fields. This includes `customValue` changes, which must not touch tag rows, NIC rows or any other field.

---

# 35. S2.3 Truncated update handling

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

# 36. S2.4 InvalidCollectorVersion recovery

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

[Changed] The recovery path must handle two signals: an explicit `InvalidCollectorVersion` fault, and a call that simply never returns (the behavior observed in vcsim). Both are tested, and the real-vCenter behavior is recorded for supported versions.

---

# 37. S2.5 Session guardian

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

# 38. S2.6 Backoff

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

# 39. S2.7 Resource cleanup registry

Every server-side object must register itself:

```text
ContainerView
PropertyFilter
EventHistoryCollector
vAPI/REST session (tagging)
```

in a worker-scoped cleanup registry.

Shutdown order:

```text
stop producer
stop streaming
destroy event collector
destroy property filter
destroy ContainerView
logout REST (vAPI) session
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

# 40. S2.8 EventHistoryCollector

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

# 41. S2.9 Event restart/replay semantics

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

# 42. S2.10 Field ownership rules

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
| custom_attributes      | PropertyCollector (definitions: refreshed on a schedule) |
| tags                   | Tag synchronizer and reconciler (REST) |
| vCenter version, compatibility, capabilities | Session layer (detected at connect) |

Every repository method must respect these ownership boundaries.

---

# 43. S2.11 Network-interface updates

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

# 44. S2.12 Bounded queue

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

# 45. S2.13 Database outage behavior

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

# 46. S2.14 Tag and custom attribute synchronization [Added]

Custom attributes and tags change through different paths, so they are synchronized differently.

## Custom attributes (PropertyCollector)

```text
add customValue to the VM property filter used for streaming
Modify updates only the attribute rows of that VM (replace-set per VM)
changes to attribute DEFINITIONS (CustomFieldsManager: add, rename, remove a field) are not VM changes:
    watch CustomFieldsManager.field or refresh definitions on a schedule, then re-resolve names
    verify on real vCenter which of these is observable
a renamed definition must not rewrite every VM row more than once
```

## Tags (REST, not streamed)

```text
tags are NOT delivered by WaitForUpdatesEx
first VERIFY on real vCenter across available supported versions whether a tag attach/detach produces an event or any other signal
    (record the result in the capability matrix)
if a signal exists:   use it as a hint to re-fetch tags for the affected object only
if no signal exists:  poll
polling design:
    one tag poller per vCenter, running inside the worker that owns the target
    bounded batches (same limits as hydration), inside the vCenter concurrency budget (S4.5)
    interval configurable per vCenter, with a default taken from measurement, not a guess
    only rewrite the tag rows of a VM whose tag set actually changed
    on tag service failure: keep serving the last known tags, mark tags degraded, expose staleness
```

## Staleness must be visible

```text
vCenter status exposes tags_last_synced_at and tags_state (ok | degraded | unavailable)
VM detail never claims freshness it cannot prove
```

Acceptance:

```text
a custom attribute change reaches the database through the stream
a tag attach/detach converges within the configured poll interval
a tag service outage does not affect VM synchronization
the tag poller holds no REST session after shutdown
```

---

# 47. Sprint 2 test gate

Mandatory:

```text
fault-injection matrix
event replay tests
InvalidCollectorVersion tests
network disconnect tests
database outage tests
resource leak tests
tag/attribute synchronization tests (including tag service outage)
1-hour churn
race detector
```

---

# 48. Sprint 2 demo

On real vCenter:

```text
power on VM
power off VM
modify networking
create VM
clone VM
delete VM
set and change a custom attribute
attach and detach a tag
disconnect collector
reconnect
force stream rollover
restart collector
```

The database must converge automatically. [Changed] Run the demo against available supported vCenters (an older and a newer supported vCenter).

---

# 49. Sprint 3 — Search Engine, Complete API, Authentication and vCenter Management

**Duration:** 2 weeks

**Sprint goal:**

> API clients (the frontend team's UI, scripts, integrations) can rapidly find VMs through exact identifiers, text, network, topology, tags, custom attributes and compound filters with deterministic pagination and authorization; they can browse the whole inventory; and administrators can register and manage vCenters.

**Scope note [Changed]:** the UI stories were removed (frontend is another team's work). Their capacity goes to API completeness (S3.11) and the vCenter management API (S3.13). If capacity runs short, S3.13 moves to the start of Sprint 4; the rest of the sprint does not depend on it.

---

# 50. S3.1 Query schema and API contract

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

[Changed] The complete endpoint catalog is in S3.11 and the vCenter endpoints in S3.13. The contract is reviewed with the frontend team on the first day of the sprint (section 4.8). `GET /v1/vms` and `GET /v1/vms/{id}` already exist from Sprint 1.

---

# 51. S3.2 Search classifier

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

# 52. S3.3 Filter AST

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
HAS_TAG          (category, tag; category alone is allowed)
HAS_ATTRIBUTE    (name, value; name alone is allowed)
```

Tag and attribute predicates use the indexes delivered in S1.16. Tags can be stale by up to the sync interval (S2.14), and search results never claim more freshness than the data has.

AST must be typed.

Unknown attributes return:

```text
HTTP 400
```

The compiler must reject invalid combinations before SQL generation.

---

# 53. S3.4 SQL compiler

Compiler output:

```text
SQL template
+
ordered parameters
```

Never concatenate values.

The generated query must have deterministic structure so logically equivalent filters produce predictable query shapes.

---

# 54. S3.5 Index implementation

Create and test:

```text
B-tree
    instance_uuid
    moref
    hostname
    exact names
    foreign keys
    tag (category, tag) and attribute (name, value) lookups   [delivered, S1.16]

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

# 55. S3.6 Query plan regression tests

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

# 56. S3.7 Keyset pagination

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

# 57. S3.8 OIDC authentication

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

# 58. S3.9 Authorization and scope injection

Map user identity to:

```text
vCenter allow-list
folder scope
role
```

[Changed] Roles: `viewer` (search and read, within scope) and `inventory-admin` (vCenter management, S3.13). Mutating admin endpoints are never reachable without authentication, even in development profiles, unless a dev-only flag explicitly disables auth and the process refuses to start with it in a production configuration.

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

# 59. S3.10 Search API performance path

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

# 60. S3.11 Complete API surface for the frontend team [Changed]

Replaces "Production UI". The UI is built by another team; this story makes sure it never has to ask for a missing endpoint.

## Process

```text
sprint day 1   contract review with the frontend team: walk through every screen they plan and the calls each needs
               gaps become stories in this sprint, or a dated backlog item
mid-sprint     contract frozen for the sprint; additive changes only afterwards (section 4.8)
sprint end     published OpenAPI document + changelog + mock server built from the contract
```

## Endpoint catalog

Search and VMs:

```text
GET  /v1/search/vms?q=             quick search (classifier)
POST /v1/vms/search                structured filters (AST), sort, cursor
GET  /v1/vms                       simple list (delivered in Sprint 1)
GET  /v1/vms/{id}                  full detail: hardware, host, cluster, datastores, NICs/IPs/VLANs, tags, custom attributes, timestamps
```

Facets and filter metadata (so a UI can build filter controls without hardcoding):

```text
GET  /v1/vms/facets?fields=...     counts grouped by power state, cluster, host, datastore, vCenter, tag category, ...
GET  /v1/meta/vm-fields            filterable and sortable fields, their types and allowed operators
GET  /v1/tags                      tag categories and tags (with counts)
GET  /v1/custom-attributes         attribute names (with counts) and bounded distinct values
```

Inventory topology (read-only):

```text
GET  /v1/vcenters                  see S3.13
GET  /v1/datacenters
GET  /v1/clusters
GET  /v1/hosts
GET  /v1/datastores
GET  /v1/networks                  includes portgroups and VLAN information
GET  /v1/folders                   tree browsing: children of a folder, path, VM counts
each topology type also has GET /v1/<type>/{id} and GET /v1/<type>/{id}/vms
```

Identity and operations:

```text
GET  /v1/me                        who am I: role, vCenter and folder scope
GET  /healthz /readyz /metrics     operations (not part of the UI contract)
```

Optional — confirm with the frontend team before building:

```text
bounded export of a search (CSV/NDJSON, streamed, capped)
server-side saved searches
```

## Cross-cutting requirements

```text
every list: cursor pagination, documented maximum page size, deterministic ordering
every filter and sort field is allow-listed and documented in /v1/meta/vm-fields
unknown field or operator → 400 with a machine-readable error code
authorization scope applies to facets, counts, tags and topology endpoints, not only to VM search
counts and facets respect the caller's scope (no totals leaking from vCenters the caller cannot see)
responses carry data timestamps where staleness matters (for example tags)
```

Acceptance:

```text
the frontend team confirms the contract covers their planned screens
every endpoint has contract tests, scope tests and a documented example
no endpoint returns unbounded results
```

---

# 61. S3.12 API result design

Search response:

```json
{
  "items": [],
  "nextCursor": "...",
  "hasMore": true
}
```

Each list item should contain only fields required for the table.

Detailed VM data is fetched separately. Detail responses include `tags` and `custom_attributes` (delivered in S1.16). Errors use the common problem format (section 4.8).

---

# 62. S3.13 vCenter management API [Added]

Administrators register and manage vCenters through the API instead of editing the database.

## Endpoints

```text
POST   /v1/vcenters/test-connection   validate without saving
POST   /v1/vcenters                   register (validates first)
GET    /v1/vcenters                   list with status
GET    /v1/vcenters/{id}              detail with status
PATCH  /v1/vcenters/{id}              rename, enable/disable, rotate credentials, TLS settings, limits, priority
DELETE /v1/vcenters/{id}              decommission (see below)
POST   /v1/vcenters/{id}/resync       request a full resync / reconciliation
```

GET endpoints are readable by `viewer` within their vCenter scope. Everything else requires `inventory-admin`.

Registration request (secrets are write-only):

```json
{
  "name": "vc-prod-01",
  "host": "vc01.example.com",
  "port": 443,
  "username": "svc-inventory@vsphere.local",
  "password": "<write-only>",
  "tls": { "verify": true, "ca_pem": "<optional>", "pinned_fingerprint": null },
  "enabled": true,
  "priority": 100
}
```

## Validation before anything is saved

```text
1. input validation and address policy (see SSRF below)
2. TCP + TLS handshake with the configured trust settings
3. login with the supplied credentials
4. read ServiceContent.About: product version, API version, build
5. version policy: vCenter Server 7.0 and above (section 2.1)
6. read-only sanity check: list a small sample of inventory
7. probe the tagging REST service: tags capability = available | unavailable
8. log out; destroy anything that was created
```

Result:

```text
201 Created with the vCenter resource (no secrets):
    id, name, host, version {product, api, build}, compatibility, capabilities {tags}, status
failure → problem details with a stable code:
    unreachable | tls_untrusted | auth_failed | unsupported_version | insufficient_privileges | timeout | address_not_allowed
tls_untrusted returns the presented certificate fingerprint so an administrator can pin it deliberately
```

Rules:

```text
credentials are write-only (section 4.5): never returned, logged, traced, audited or used as metric labels
the whole validation runs under a hard overall deadline, and every step has its own deadline
POST accepts an Idempotency-Key header; a repeated request returns the original result
registering the same host twice returns 409
test-connection and create are rate limited per caller
```

## SSRF protection

The API opens outbound connections to addresses supplied by a caller.

```text
only vCenter-style HTTPS endpoints; the allowed port range is configuration
loopback, link-local (including cloud metadata addresses) and other configured ranges are denied by default
optional per-deployment allow-list of host patterns or networks
DNS is resolved once and the resolved address is the one connected to (no rebinding)
redirects are never followed
```

## Decommission (DELETE)

```text
step 1: disable the target; the owning collector releases the lease and tears down vSphere resources (S4.1)
step 2: mark the vCenter's inventory deleted (tombstones) so it disappears from search immediately
step 3: purge after a configurable retention period, in the bounded batches of S4.9
step 4: delete the stored credentials
DELETE is idempotent; purging immediately requires an explicit confirmation field
```

## Audit log

```text
audit_events: who, when, action, target, outcome, request ID — never secrets
written for every administrative action, including failures
readable through an admin-only endpoint
```

## Schema changes (expand/contract, section 4.3)

```text
vcenters: product version, API version, build, compatibility, capabilities, TLS settings, credential reference
audit_events (new table)
privileges follow the existing rule: collector_rw DML only, query_ro SELECT only
the API process may read credential material only through the secret store interface, never with query_ro
```

## Sequencing

```text
needs: OIDC and roles (S3.8, S3.9) and version detection (section 2.1)
before Sprint 4: a restarted collector loads enabled targets from collector_targets;
    pickup without a restart, leases and multi-worker assignment arrive with S4.1–S4.4
a newly registered vCenter shows status PENDING_ASSIGNMENT until a worker owns it
```

Acceptance:

```text
vCenters across supported versions can be registered through the API, and their versions are detected correctly
bad credentials, untrusted certificate, unreachable host and unsupported version each return the documented error code
no secret appears in any response, log, trace, metric or audit record (tests scan the outputs)
SSRF tests: loopback, link-local/metadata address, redirect and DNS-rebinding attempts are rejected
non-admin callers cannot reach any mutating endpoint
a decommissioned vCenter disappears from search immediately
```

---

# 63. Sprint 3 test gate

Required:

```text
AST fuzzing
cursor fuzzing
SQL injection tests
scope bypass tests
OpenAPI contract tests
API integration tests
black-box HTTP end-to-end tests
vCenter API tests (validation errors, SSRF, secret-leak scans, idempotency)
tag and attribute filter tests
query plan regression tests
```

---

# 64. Sprint 3 demo

An API client should be able to search for:

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
tag
custom attribute
```

The search must never call vCenter.

[Added] Then an administrator registers vCenters spanning supported versions (an older and a newer supported vCenter) through `POST /v1/vcenters` (after `test-connection`), sees the detected versions and status, and the inventory appears in search. Only registration and test-connection call vCenter; the search path never does.

---

# 65. Sprint 4 — Multi-vCenter Scale, Leases, Reconciliation and HA

**Duration:** 2 weeks

**Sprint goal:**

> The platform can operate across multiple vCenters and collector instances while remaining self-healing and preventing duplicate ownership.

---

# 66. S4.1 Collector target registry

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

[Changed] The admin path is the vCenter management API (S3.13). This story makes its effects real, without restarting any collector:

```text
POST /v1/vcenters            → vcenters + collector_targets rows, then NOTIFY; a worker acquires the lease and starts
PATCH enabled=false          → the owning worker releases the lease and tears down vSphere resources
PATCH credentials or TLS     → the owning worker reconnects with the new settings and keeps the inventory
DELETE                       → decommission flow (S3.13)
POST /resync                 → reconciliation request (S4.6)
```

The registry also holds the detected version, compatibility and capabilities reported by the session layer (section 2.1). Bootstrap from configuration remains supported for the first install.

---

# 67. S4.2 PostgreSQL worker leases

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

# 68. S4.3 Target assignment

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

[Added] These states, the owning worker and the last successful sync are exposed on `GET /v1/vcenters/{id}`, so administrators and the frontend can see what is happening.

---

# 69. S4.4 LISTEN/NOTIFY configuration updates

Use PostgreSQL `LISTEN/NOTIFY` for fast configuration propagation.

But:

```text
NOTIFY = wake-up signal
database = source of truth
```

If a notification is missed, periodic reconciliation still discovers the change.

---

# 70. S4.5 Concurrency control

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

[Added] The tag poller (S2.14) and every REST call count against the per-vCenter API budget.

---

# 71. S4.6 Differential reconciliation

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

# 72. S4.7 Reconciliation triggers

Run reconciliation:

```text
every 24h
after serious stream recovery
after prolonged reconnect
after collector replacement
```

The 24-hour sweep from the original implementation plan remains the baseline.

---

# 73. S4.8 Cross-vCenter migration

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

[Added] Test the move in both directions between different vCenter versions across supported versions (an older and a newer supported vCenter). Tags and custom attributes belong to the VM: they are re-read from the destination vCenter after a move. Tag identifiers are not guaranteed to be the same across vCenters (verify), so `(category name, tag name)` is the user-visible identity.

---

# 74. S4.9 Tombstone management

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

# 75. S4.10 PostgreSQL HA

If Kubernetes is already the corporate platform:

```text
CloudNativePG
```

is the preferred operational approach.

CloudNativePG 1.30.x currently supports PostgreSQL 18 and Kubernetes 1.34–1.36.

Do not introduce Kubernetes solely to run this system.

For a VM-based corporate environment, use the organization's existing PostgreSQL HA standard.

---

# 76. S4.11 Backup and restore

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

# 77. S4.12 Tag and custom attribute reconciliation [Added]

Tags are not streamed (S2.14), so the platform needs a sweep that proves they converge.

```text
per vCenter, on a configurable schedule and after the tag service recovers:
    fetch the full tag assignment set for the vCenter's VMs (bounded batches, inside the REST budget)
    compare with vm_tags
    fix drift: add what is missing, remove what is stale
    measure: VMs checked, rows changed, duration, REST calls
custom attributes: definitions are refreshed first; values are verified by the normal reconciliation (S4.6)
```

Rules:

```text
a failed tag fetch NEVER deletes tags (missing data is not the same as missing tags)
only a successful, complete fetch may remove a tag row
drift above a threshold raises an alert (the poller or the signal is not working)
```

Acceptance:

```text
a tag attached or detached directly in vCenter converges within one interval
a tag service outage followed by recovery converges without manual repair
a mixed fleet spanning supported versions converges across all targets
```

---

# 78. Sprint 4 scale test

Run:

```text
3 vCenters
5 vCenters
10 vCenters
```

with realistic inventory sizes.

[Changed] The fleet is a mix of vCenter versions across available supported versions (real vCenter for an older and a newer supported vCenter where available, vcsim for the rest), with tags and custom attributes at realistic densities.

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
tag sync duration and REST call rate
```

---

# 79. Sprint 4 test gate

Required:

```text
worker takeover test
multi-vCenter test
xvMotion test
reconciliation convergence test
tag and attribute convergence test
vCenter lifecycle test (register → collect → disable → resume → decommission)
mixed-fleet test spanning available supported versions
PostgreSQL failover test
backup restore test
10-target scale test
```

---

# 80. Sprint 4 demo

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
register a new vCenter through the API while the fleet is running
attach a tag directly in vCenter
```

The inventory must recover without manual repair.

---

# 81. Sprint 5 — Performance, Security, Chaos and Production Release

**Duration:** 2 weeks

**Sprint goal:**

> Prove the system against its SLOs, remove production blockers, complete operational documentation, and release v1.0.

The original plan reserves the final sprint for telemetry, performance, tuning, chaos, packaging, security and documentation.

The difference here is that much of that work has been started earlier; Sprint 5 is verification and hardening rather than "build observability from scratch."

---

# 82. S5.1 Performance dataset generator

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
tags per VM (none to dozens), several tags in one category
custom attributes per VM, including high-cardinality values
```

---

# 83. S5.2 Query workload model

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
tag (category + tag)
custom attribute (name + value)
facet counts
topology browsing (folders, clusters, hosts)
vCenter list and status
compound filters
pagination
```

Workload should be mixed, not run as isolated microbenchmarks only.

---

# 84. S5.3 Query performance gate

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

# 85. S5.4 Ingestion performance gate

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

# 86. S5.5 PostgreSQL tuning

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

# 87. S5.6 Connection-pool tuning

Separate pool budgets for:

```text
query API
collector
reconciliation
administration
```

Prevent a reconciliation storm from consuming every database connection.

---

# 88. S5.7 OpenTelemetry instrumentation

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
tag fetch (REST)
vCenter registration and test-connection
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

# 89. S5.8 Security review

Verify:

```text
TLS configuration
OIDC validation
JWT key rotation
RBAC
DB roles
secret handling
vCenter credential storage and rotation
SSRF on vCenter registration
secret-leak scans (logs, traces, API, audit)
admin endpoint authorization
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

# 90. S5.9 Chaos tests

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
tag service unavailable
REST session expiry
mixed fleet (an older and a newer supported vCenter)
vCenter registered while the fleet is under load
```

Each test has:

```text
expected behavior
maximum recovery time
data-loss expectation
alert expectation
```

---

# 91. S5.10 72-hour soak

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

# 92. S5.11 Deployment packaging

Build:

```text
collector container
query-api container
migration job
```

The web container is built, versioned and deployed by the frontend team. This plan provides the integration contract only: API base URL, CORS origins and authentication settings (section 4.8).

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

# 93. S5.12 Rolling deployment behavior

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

# 94. S5.13 Documentation

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
api-changelog.md
frontend-integration-guide.md
supported-versions.md
tags-and-custom-attributes.md
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

# 95. S5.14 Release candidate

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

# 96. S5.15 API contract freeze and version certification [Added]

## API contract freeze

```text
openapi.yaml tagged v1.0.0 together with the release
contract tests green against the release candidate
changelog complete; deprecations (if any) listed
mock server image published from the frozen contract
the frontend team confirms their screens work against the release candidate
```

## Version certification

```text
the full real-vCenter suite on 7.0.x, 8.0.x (U3), 9.0.x, 9.1.x and on a mixed fleet (recording which real environments are available)
results recorded per version in docs/vsphere-capability-matrix.md
every behavior difference is a documented capability flag, not an undocumented special case
```

Anything that passes only on vcsim is NOT certified.

---

# 97. Sprint 5 final gate

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
[✓] API contract frozen as v1 and accepted by the frontend team
[✓] vCenter supported versions verified on real vCenter (recording available patch levels in capability matrix)
[✓] vCenter registration security review passed (credentials, SSRF, audit)
```

---

# 98. Cross-sprint dependency map

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
 └── first API
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
 ├── complete API      ├── multi-vCenter
 ├── auth              ├── reconciliation
 └── vCenter API       └── HA
      │                    │
      └──────────┬─────────┘
                 ▼
                S5
                 │
        performance/security
        chaos/soak/release
```

---

# 99. Parallel work by sprint

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
    events + NIC sync + tag/attribute sync

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
    vCenter management API (validation, version detection, credentials)

DB Engineer
    AST + indexes + pagination

SRE
    OIDC + deployment

QA
    fuzz + API contract + black-box API tests + SSRF and secret-leak tests

Frontend team (external)
    contract review on sprint day 1; consumes the frozen contract
```

## Sprint 4

```text
Collector A/B
    leases + multi-vCenter + vCenter lifecycle + tag reconciliation

DB Engineer
    reconciliation + HA

SRE
    database failover + backups

QA
    multi-target chaos

Frontend team (external)
    consumes topology and status endpoints
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

# 100. Definition of Ready

A story is Ready only when:

```text
business/technical outcome is explicit
dependencies are known
API/schema changes are known (API changes checked against the contract rules, section 4.8)
test strategy exists
acceptance criteria are measurable
observability requirements are identified
security implications are understood
rollback behavior is understood
```

---

# 101. Definition of Done

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
API contract (openapi.yaml) and changelog updated for any API change
generated code regenerated
migration tested
deployed to shared environment
demonstrated
verification evidence recorded in the sprint report (commands run, output)
```

This follows the original plan's principle that stories must include failure-path tests, telemetry and documentation rather than only happy-path functionality.

---

# 102. Pull-request standards

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

For API changes:

```text
openapi.yaml diff
compatibility statement (additive or breaking)
contract tests added or updated
example added to docs/api.md
changelog entry
web/ not modified
```

For anything that talks to vCenter:

```text
behavior across supported vCenter versions
read-only guarantee (no write calls to vCenter)
```

---

# 103. Database-change checklist

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

# 104. Performance engineering rules

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

# 105. Reliability engineering rules

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
tag poller
vCenter management API
database
```

---

# 106. Operational SLOs

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
| Custom attribute change → DB P95   | ≤ 250 ms (stream) |
| Tag change → DB                    | ≤ configured poll interval |
| vCenter registration validation    | ≤ 15 s hard deadline (proposed) |
| Credential or secret exposure      | 0 |
| Breaking API changes within /v1    | 0 |

The original specification's core SLA values remain the authoritative starting point for the database/write path and ingestion behavior.

---

# 107. Alert strategy

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
tag sync stale beyond threshold
tag service unavailable (sustained)
authentication failure on an enabled vCenter
unsupported vCenter version (< 7.0) detected
restore failure
```

Every production alert must link to a runbook.

---

# 108. Release gates

There are four formal release gates.

## Gate 1 — Functional

```text
all critical features work
API contract complete and accepted by the frontend team
vCenter registration works for supported versions (vCenter Server 7.0 and above)
```

## Gate 2 — Correctness

```text
replay
reconciliation
event ordering
identity invariants
scope enforcement
tag and attribute convergence
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
vCenter credential handling reviewed
```

A release that passes functionality but fails correctness or operational gates does not ship.

---

# 109. Risk-driven testing matrix

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
| tag changes are not streamed | tag sync and reconciliation tests |
| vCenter version behavior differences | nightly real-vCenter runs across available supported versions |
| credential leakage through the registration API | secret-leak scan tests and review |
| SSRF through vCenter registration | SSRF test suite |
| API contract drift breaks the frontend | contract tests, changelog, mock server |

The original risk register already identifies simulator fidelity, server-side object leaks, boot storms, query latency, schema changes, and topology ambiguity as major risks.

---

# 110. What should NOT be scheduled as "nice to have" work during v1

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
frontend implementation (owned by another team)
writing to vCenter (editing tags, attributes or VMs): the platform is read-only
server-side features no consumer has asked for
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

# 111. Final project milestone sequence

```text
DAY 1–4
Sprint 0 [Delivered]
Foundation
        ↓
WEEK 1–2
Sprint 1 [Delivered]
Snapshot inventory + API + tags/attributes
        ↓
WEEK 3–4
Sprint 2
Real-time synchronization + tag/attribute sync
        ↓
WEEK 5–6
Sprint 3
Search + complete API + auth + vCenter management API
        ↓
WEEK 7–8
Sprint 4
Multi-vCenter + HA + vCenter lifecycle
        ↓
WEEK 9–10
Sprint 5
Performance + security + chaos
        ↓
~DAY 53
v1.0
```

---

# 112. v1.0 release definition

The product is ready for production only when a new environment can perform this sequence successfully:

```text
1. An administrator registers a vCenter through the API (test-connection first)
2. The version is detected (vCenter Server 7.0 and above) and accepted
3. Collector acquires the target
4. Authenticate
5. Discover topology
6. Hydrate inventory (including custom attributes)
7. Fetch tags (or report tags as degraded)
8. Persist inventory
9. Start delta stream
10. Start event stream
11. Expose searchable inventory
12. An API client authenticates
13. Search VM (including by tag and by custom attribute)
14. Open VM details
15. Modify a VM, a custom attribute and a tag in vCenter
16. Observe updated state (tags within the poll interval)
17. Restart collector
18. Verify recovery
19. Drop an event
20. Run reconciliation
21. Verify convergence
22. Disable and re-enable the vCenter through the API
23. Fail database primary
24. Verify service recovery
25. Upgrade application
26. Verify no inventory loss
27. Repeat the core path against vCenters across available supported versions (an older and a newer supported vCenter)
```

That entire path becomes the final release smoke test.

---

# 113. Recommended implementation order inside the repository

The team should implement the repository in this order:

```text
1. Database migrations
2. Domain types
3. sqlc generation
4. PostgreSQL repositories
5. vSphere session layer
6. topology provider
7. hydration
8. normalization (including custom attributes) and tag fetcher
9. staging/COPY writer
10. worker state machine
11. PropertyCollector
12. event ledger
13. recovery/reconciliation
14. search compiler
15. REST API
16. auth/scope
17. vCenter management API and audit log (frontend: external team)
18. HA/deployment
19. performance
20. chaos/release
```

This minimizes circular dependencies.

---

# 114. The most important engineering invariant

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

That is the fundamental correctness property of the system. For tags, convergence is bounded by the poll and reconciliation interval rather than by the event stream.

---

# 115. Final delivery outcome

At the end of the project, the team should have:

```text
Go collector fleet
        +
govmomi integration
        +
normalized PostgreSQL inventory
        +
tags and custom attributes
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
complete versioned REST API + OpenAPI contract
        +
vCenter management API (vCenter Server 7.0 and above)
        +
OpenTelemetry
        +
HA deployment
        +
load/chaos/soak evidence
        +
production runbooks
```

The result is a **single operational inventory platform**, not merely a vSphere synchronization script. The user interface is delivered separately by the frontend team against the published API contract.

The implementation should remain intentionally conservative about infrastructure: PostgreSQL is the durable system of record, in-process Go buffering handles the low-latency write path, and additional distributed infrastructure is introduced only when measured requirements justify it.
