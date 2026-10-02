# SIMS Query API Specification & Usage Guide

## Overview

The SIMS Query API provides read-only access to normalized vSphere inventory state stored in PostgreSQL. It is designed to be consumed by the frontend and downstream microservices.

- **Base URL:** `http://localhost:8080`
- **OpenAPI 3.1 Spec:** [`api/openapi.yaml`](../api/openapi.yaml)
- **Role & Security:** The API service connects exclusively with the `query_ro` database user. It possesses only `SELECT` privileges on application tables; any mutation attempt (`INSERT`, `UPDATE`, `DELETE`, `TRUNCATE`, DDL) is rejected by PostgreSQL.

---

## Configuration & Environment Variables

| Variable | Description | Default |
| :--- | :--- | :--- |
| `PORT` | Listening HTTP port | `8080` |
| `LOG_LEVEL` | Log severity (`debug`, `info`, `warn`, `error`) | `info` |
| `DATABASE_URL` | PostgreSQL connection string (must use `query_ro`) | `postgres://query_ro:query_ro_pass@localhost:5432/sims?sslmode=disable` |
| `CORS_ALLOWED_ORIGINS` | Comma-delimited list of allowed CORS origins or `*` | `""` (restrictive default: CORS rejected) |

### CORS Integration for Frontend Development
By default, cross-origin requests without a matching origin are blocked. To allow local frontend development without modifying `web/`, start the API with:

```bash
export CORS_ALLOWED_ORIGINS="http://localhost:3000,http://127.0.0.1:3000"
./bin/sims-query-api
```

---

## Endpoints

### 1. Liveness & Readiness Probes

#### `GET /healthz`
Returns process liveness.
```bash
curl -s http://localhost:8080/healthz | jq .
```
Response:
```json
{
  "status": "ok",
  "timestamp": "2026-10-02T12:00:00Z",
  "service": "sims-query-api",
  "version": "0.1.0"
}
```

#### `GET /readyz`
Returns readiness of downstream dependencies (verifies active PostgreSQL connection pool ping).
```bash
curl -s http://localhost:8080/readyz | jq .
```
Response:
```json
{
  "status": "ok",
  "timestamp": "2026-10-02T12:00:00Z",
  "service": "sims-query-api",
  "version": "0.1.0",
  "details": {
    "database_connection": "ok"
  }
}
```

#### `GET /metrics`
Prometheus exposition format metrics for service telemetry.
```bash
curl -s http://localhost:8080/metrics
```

---

### 2. Virtual Machines Inventory

#### `GET /v1/vms`
Lists active virtual machines using keyset pagination ordered deterministically by `(name ASC, id ASC)`.

##### Query Parameters:
- `vcenter_id` (optional, UUID): Filter by vCenter ID.
- `name` (optional, string): Case-insensitive substring match filter on VM name.
- `limit` (optional, integer): Page size between 1 and 100 (default: `20`).
- `cursor` (optional, string): Opaque base64-encoded cursor returned in previous page's `pagination.next_cursor`.

##### Keyset Pagination Mechanics:
Keyset pagination guarantees consistent ordering without offset drift during concurrent inventory insertions or deletions. The client treats the cursor as an opaque token.

Example:
```bash
curl -s "http://localhost:8080/v1/vms?limit=2" | jq .
```
Response:
```json
{
  "items": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440001",
      "vcenter_id": "a0000000-0000-0000-0000-000000000001",
      "moref": "vm-10",
      "instance_uuid": "5024b486-11f2-76bb-9cb9-43c242c1618a",
      "bios_uuid": "42245b73-0aa7-4bca-86b2-658a221f7e02",
      "name": "api-gateway-01",
      "power_state": "poweredOn",
      "connection_state": "connected",
      "cpu_cores": 4,
      "memory_mb": 8192,
      "folder_path": "Datacenters.DC1.vm.Production",
      "guest_os": "rhel9_64Guest",
      "guest_host_name": "api-gateway-01.corp.local",
      "guest_ip_address": "10.0.10.15",
      "created_at": "2026-10-01T12:00:00Z",
      "updated_at": "2026-10-02T08:30:00Z"
    },
    {
      "id": "550e8400-e29b-41d4-a716-446655440002",
      "vcenter_id": "a0000000-0000-0000-0000-000000000001",
      "moref": "vm-11",
      "instance_uuid": "5024b486-11f2-76bb-9cb9-43c242c1618b",
      "name": "api-gateway-02",
      "power_state": "poweredOn",
      "connection_state": "connected",
      "cpu_cores": 4,
      "memory_mb": 8192,
      "created_at": "2026-10-01T12:00:00Z",
      "updated_at": "2026-10-02T08:30:00Z"
    }
  ],
  "pagination": {
    "limit": 2,
    "next_cursor": "eyJuYW1lIjoiYXBpLWdhdGV3YXktMDIiLCJpZCI6IjU1MGU4NDAwLWUyOWItNDFkNC1hNzE2LTQ0NjY1NTQ0MDAwMiJ9",
    "has_more": true
  }
}
```

Fetching the next page:
```bash
curl -s "http://localhost:8080/v1/vms?limit=2&cursor=eyJuYW1lIjoiYXBpLWdhdGV3YXktMDIiLCJpZCI6IjU1MGU4NDAwLWUyOWItNDFkNC1hNzE2LTQ0NjY1NTQ0MDAwMiJ9" | jq .
```

---

#### `GET /v1/vms/{id}`
Retrieves complete details for a single virtual machine by internal database UUID, including its attached datastores and virtual network interfaces.

Example:
```bash
curl -s "http://localhost:8080/v1/vms/550e8400-e29b-41d4-a716-446655440001" | jq .
```
Response:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440001",
  "vcenter_id": "a0000000-0000-0000-0000-000000000001",
  "moref": "vm-10",
  "instance_uuid": "5024b486-11f2-76bb-9cb9-43c242c1618a",
  "bios_uuid": "42245b73-0aa7-4bca-86b2-658a221f7e02",
  "name": "api-gateway-01",
  "power_state": "poweredOn",
  "connection_state": "connected",
  "cpu_cores": 4,
  "memory_mb": 8192,
  "folder_path": "Datacenters.DC1.vm.Production",
  "guest_os": "rhel9_64Guest",
  "guest_host_name": "api-gateway-01.corp.local",
  "guest_ip_address": "10.0.10.15",
  "created_at": "2026-10-01T12:00:00Z",
  "updated_at": "2026-10-02T08:30:00Z",
  "datastores": [
    {
      "id": "f0000000-0000-0000-0000-000000000001",
      "moref": "datastore-10",
      "name": "vsanDatastore",
      "type": "vsan",
      "capacity_bytes": 10995116277760,
      "free_bytes": 5497558138880
    }
  ],
  "network_interfaces": [
    {
      "id": "01000000-0000-0000-0000-000000000001",
      "device_key": 4000,
      "mac_address": "00:50:56:a1:b2:c3",
      "ip_address": "10.0.10.15",
      "network_id": "02000000-0000-0000-0000-000000000001"
    }
  ]
}
```

---

## Error Handling

All error responses adhere to standard HTTP status codes and the `ErrorResponse` JSON shape:

```json
{
  "error": "Bad Request",
  "message": "malformed pagination cursor",
  "code": 400
}
```

| HTTP Status | Reason |
| :--- | :--- |
| `400 Bad Request` | Invalid query parameter (e.g. invalid `limit`, malformed `cursor`, or invalid UUID path parameter) |
| `404 Not Found` | Requested virtual machine ID does not exist or is inactive/soft-deleted |
| `500 Internal Server Error` | Database query failure or unexpected server error |
| `503 Service Unavailable` | Downstream database readiness check failed |
