# SIMS Frontend Integration Guide (v1.0.0)

## 1. Overview & Clean Boundary Invariant

This guide provides frontend engineers with instructions for integrating UI applications with the SIMS Query API.

> [!NOTE]
> Throughout Sprint 1–5 backend development, the `web/` directory was maintained strictly unchanged (`git diff main --stat -- web/` returned 0 lines). This document outlines the stable v1.0.0 contract ready for consumption.

---

## 2. API Endpoints Reference

### 2.1 Virtual Machine Search (`GET /v1/vms`)
Returns a paginated list of virtual machines with optional filtering, sorting, and full-text search.

#### Query Parameters

| Parameter | Type | Example | Description |
|:---|:---|:---|:---|
| `q` | string | `web-prod` | Full-text and trigram substring search over VM names and hostnames |
| `filter` | string | `power_state:poweredOn AND cpu>=4` | Structured filter expression |
| `tag` | string | `Environment:Production` | Filter by `category:name` |
| `custom_attribute`| string | `Owner:DevOps` | Filter by custom attribute `name:value` |
| `ip` | string | `10.0.1.0/24` | CIDR subnet or exact IP search |
| `vlan` | int / range | `100` or `100-105` | VLAN ID or range overlap |
| `folder` | string | `Datacenter1/vm/Production` | Folder path or subtree prefix match |
| `limit` | int | `50` | Page size (1 to 200, default: 50) |
| `cursor` | string | `ZXlKa...` | Opaque keyset pagination cursor |

#### Response Schema
```json
{
  "items": [
    {
      "id": "c138d38d-8a21-4fbe-9b37-29cbcfbd3ef8",
      "vcenter_id": "90e29b9e-64c4-4d89-9dbf-1c4b8b609919",
      "moref": "vm-101",
      "instance_uuid": "503923d8-5544-3322-1100-abcdef012345",
      "name": "web-prod-01",
      "power_state": "poweredOn",
      "connection_state": "connected",
      "cpu_cores": 4,
      "memory_mb": 8192,
      "ip_addresses": ["10.0.1.50"],
      "tags": [
        {"category": "Environment", "name": "Production"}
      ]
    }
  ],
  "pagination": {
    "has_next": true,
    "has_prev": false,
    "next_cursor": "ZXlKaWQ...",
    "prev_cursor": null
  }
}
```

---

## 3. Keyset Pagination Integration

SIMS uses high-performance keyset pagination:
1. Always pass `cursor=next_cursor` when advancing pages forward.
2. If `has_next` is `false`, hide or disable the "Next Page" button.
3. Treat cursor tokens as completely opaque strings; never parse or construct them on the client.

---

## 4. Error Handling

Errors return RFC 7807 compliant payloads:
```json
{
  "code": "invalid_cursor",
  "message": "The provided pagination cursor is invalid or malformed",
  "details": {}
}
```
HTTP Status Codes:
- `400 Bad Request`: Invalid filter syntax or corrupt cursor.
- `401 Unauthorized`: Missing or invalid Bearer JWT token.
- `404 Not Found`: Entity not found.
- `500 Internal Server Error`: Server-side failure.
