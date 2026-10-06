# SIMS API Changelog & Contract Specification (v1.0.0)

## 1. Release History

### v1.0.0 (Production Release) — 2026-10-06
- **Frozen Contract:** Tagged `api/openapi.yaml` as `1.0.0` OpenAPI 3.1 compliant contract.
- **Performance Guarantees:** Hardened hot-path query response time to P99 $\le$ 5ms (PostgreSQL) and $\le$ 20ms (API HTTP).
- **Hardened Keyset Pagination:** Base64-encoded deterministic keyset cursor with strict UUID validation and direction bounds.
- **OpenTelemetry Tracing:** Full W3C `traceparent` propagation across all API endpoints with zero secret leakage.
- **SSRF Defenses:** Validated pre-registration endpoint (`POST /v1/vcenters/validate`) with port, scheme, and loopback restrictions.
- **Security Hardening:** Least privilege database user separation (`query_ro` SELECT-only; write-only credential storage).

### v0.2.0 (Sprint 4: Multi-Target Fleet & Tags) — 2026-10-06
- Added Multi-Target dynamic registration endpoints (`/v1/vcenters`).
- Added VMware Automation tag and category filtering.
- Added custom attribute querying.
- Added differential reconciliation and xvMotion deduplication.

### v0.1.0 (Sprint 1–3: Core Inventory & Search) — 2026-10-05
- Initial OpenAPI specification.
- Core VM listing and detail endpoints (`/v1/vms`, `/v1/vms/{id}`).
- Trigram fuzzy name search and CIDR/MAC filtering.
- Hierarchical folder subtree queries via `ltree`.

---

## 2. API Contract Stability Policy

1. **Semantic Versioning:** All breaking changes require a major version bump (`/v2/`).
2. **Backward-Compatible Extensions:** Adding new fields to responses or optional query parameters to existing endpoints is considered non-breaking.
3. **Cursor Format:** Cursors are opaque tokens. Clients must treat cursor strings as immutable opaque tokens without parsing their internals.
4. **Error Responses:** All errors follow standard RFC 7807 problem details with fields `code`, `message`, and optional `details`.
