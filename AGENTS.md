# SIMS Development Guidelines & Guardrails

## 1. Scope & Clean Boundary Invariants
- **Backend Only:** NEVER modify, create, delete, or reformat any file under `web/`. At all times, `git diff main --stat -- web/` must return 0 lines.
- **SQL as Contract:** Use sqlc + pgx/v5. Never use an ORM. Never hand-edit files under `generated/db/`.
- **Forward-Compatible Migrations:** Migrations use sequential numbers (`db/migrations/00000X_...sql`), support expand/contract patterns, and are tested for rollback and re-apply idempotency (`make test-migration`).
- **No Credentials in Code or Logs:** Passwords, API tokens, and vCenter session tokens must never appear in logs, trace attributes, or error messages.

## 2. Git & Story Branching Discipline
- **Sprint Trunk:** Work progresses on the designated sprint branch (e.g. `sprint-2/realtime-ingestion`). Never commit to `main` or force-push.
- **Short-Lived Story Branches:** Implement each story on a dedicated branch (`sprint-2/s2-<id>-<slug>`).
- **Verification Before Merge:** Run `make format && make lint && make build && make test && make test-race && make sqlc-verify` and ensure green status before merging.
- **Fast-Forward Only:** Merge story branches into the sprint trunk using `git merge --ff-only`.

## 3. vcsim Testing & Concurrency Invariants
- **Version Configuration:** vcsim defaults to vSphere 6.5. Always configure vcsim model `ServiceContent.About` to vSphere >= 7.0 (e.g. `8.0.3` or `7.0.0`). Never weaken production version guards for test convenience.
- **Background Goroutine Synchronization:** In tests running streaming loops (`streamer.Run`), always synchronize with `sync.WaitGroup` to ensure the streaming loop has exited before triggering `registry.Teardown()` or `server.Close()`. Never allow in-flight `WaitForUpdatesEx` to race with `DestroyPropertyFilter`.
- **Claim Boundaries:** Passing in vcsim proves parser, protocol, and flow correctness against the simulator; it does NOT prove real vCenter compatibility. Real vCenter behavior must remain marked `BLOCKED (human)` until tested against real hardware.

## 4. Evidence Grounding & Assumptions
- Never state unverified performance figures, buffer sizes, or retention periods as factual without official documentation citations.
- Label unmeasured figures as `UNVERIFIED assumption` and design systems defensively with proactive timeouts (e.g. `MaxStreamBlockDuration`) and proactive keepalives.

## 5. Field Ownership & Incremental Delta Semantics
- **Field Masking:** In Go update structures, `nil` indicates absence in the changeSet (do not update database column). Non-nil empty slices (`make([]T, 0)`) indicate explicit clearing of child collections (e.g. removing all guest NICs).
- **Immutable Provenance:** `created_at` and `created_by` are owned by the Event ledger and must never be modified by PropertyCollector updates or reconciliations.
- **Postgres Types:** Always normalize range values through `vlan.NormalizeSpec` before casting to PostgreSQL multirange (`int4multirange`).
