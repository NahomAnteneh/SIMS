# VMware Tags & Custom Attributes Architecture (v1.0.0)

## 1. Overview & Dual Protocol Architecture

VMware vSphere provides two distinct metadata classification systems:
1. **VMware Tags (vSphere Automation REST API / CIS Tags):** Modern category-based tagging accessed via `/api/cis/tagging`. Tags consist of a Category Name (e.g. `Environment`, `Compliance`) and a Tag Name (e.g. `Production`, `PCI-DSS`).
2. **Custom Attributes (SOAP API / `vim25`):** Legacy key-value pairs assigned directly on managed entity objects.

SIMS provides unified querying across both metadata systems via dedicated normalized relational tables and index structures.

---

## 2. Invariants & Guardrails (AGENTS.md §5)

### 2.1 The Non-Destructive Preservation Rule
- **Rule:** A network failure, HTTP 500 error, or timeout from the vSphere Automation REST service (`vapi/rest`) must **NEVER** wipe existing tags from the SIMS database.
- **Implementation:** In Go update structures:
  - `nil` slice indicates absence in the changeSet $\to$ DO NOT DELETE TAGS.
  - Non-nil empty slice (`make([]TagRecord, 0)`) indicates explicit user clearing of tags $\to$ delete tags for this VM.
  - Non-empty slice indicates atomic replacement $\to$ delete existing and insert new.

### 2.2 Event vs Polling Synchronization
- Custom attributes are delivered in real time via PropertyCollector `customValue` property updates.
- VMware tags do not emit standard PropertyCollector events; they are synchronized periodically via `TagPoller` and `TagFetcher` with differential hashing.

---

## 3. Database Schema & Indexing

```sql
-- vm_tags
CREATE TABLE vm_tags (
    vm_id UUID NOT NULL REFERENCES virtual_machines(id) ON DELETE CASCADE,
    tag_id TEXT NOT NULL,
    category_name TEXT NOT NULL,
    tag_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (vm_id, tag_id)
);
CREATE INDEX idx_vm_tags_category_tag ON vm_tags (category_name, tag_name);

-- vm_custom_attributes
CREATE TABLE vm_custom_attributes (
    vm_id UUID NOT NULL REFERENCES virtual_machines(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (vm_id, name)
);
CREATE INDEX idx_vm_custom_attrs_name_value ON vm_custom_attributes (name, value);
```
