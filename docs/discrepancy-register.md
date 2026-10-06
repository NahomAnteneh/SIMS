# Documentation Discrepancy Register

**Branch:** `sprint-2/s2-doc-reconciliation`
**Created:** 2026-10-05
**Method:** Code is the source of truth (AGENTS.md §4). Every fix verified by command.

---

## Group A — False Evidence Claims

| ID | File | Line(s) | Discrepancy | Fix Status | Commit |
|:---|:-----|:--------|:------------|:----------:|:-------|
| A1 | `docs/sprint2-report.md` | ~30 | Cites nonexistent test `TestSessionManager_OlderAPIVersionCompatibility` | [x] | `b777a55` |
| A2 | `docs/sprint2-report.md` | ~40 | Cites nonexistent test `TestFieldOwnership_IncrementalFieldMask` | [x] | `b777a55` |
| A3 | `docs/sprint2-report.md` | ~52 | Cites four undefined metrics: `sims_collector_session_healthy`, `sims_collector_heartbeat_duration_seconds`, `sims_collector_event_replay_count`, `sims_collector_event_replay_duration_seconds` | [x] | `b777a55` |
| A4 | `docs/adr/ADR-004.md` | ~36 | Claims `stage_vms` has `field_mask BIGINT NOT NULL`; column does not exist | [x] | `e9648f7` |
| A5a | `docs/sprint2-report.md` | ~38 | Claims `000001_extensions.sql` contains `uuid-ossp` | [x] | `b777a55` |
| A5b | `docs/sprint2-design-decisions.md` | ~38 | Claims `000001_extensions.sql` contains `uuid-ossp` | [x] | `b777a55` |

## Group B — Reference Docs Other Teams Build Against

| ID | File | Line(s) | Discrepancy | Fix Status | Commit |
|:---|:-----|:--------|:------------|:----------:|:-------|
| B1 | `docs/events-v1.md` | ~43-61 | DDL does not match `db/migrations/000004_virtual_machines.sql` | [x] | `39b7a2d` |
| B2a | `docs/api.md` | ~226 | Documents 503 for failed DB readiness — clarified table vs endpoint behavior | [x] | `39b7a2d` |
| B2b | `docs/api.md` | ~27 | Binary path `./bin/sims-query-api` should be `./bin/query-api` | [x] | `39b7a2d` |
| B3 | `docs/database.md` | ~108-113 | Pool config values do not match `cmd/query-api/main.go:41-46`; unverified performance claim | [x] | `39b7a2d` |
| B4 | `docs/local-development.md` | ~58 | PostgreSQL port `5432` should be `5433` | [x] | `39b7a2d` |
| B5 | `docs/real-vcenter-test-runbook.md` | ~61 | SQL references nonexistent `ip_addresses` and `connected` columns | [x] | `39b7a2d` |
| B6 | `docs/vsphere-capability-matrix.md` | ~4,26 | vcsim column reports `6.5.0` inconsistent with header and AGENTS.md; 8.0.2 empirical data not acknowledged in header | [x] | `39b7a2d` |
