# SIMS Backup and Disaster Recovery Runbook

## 1. Overview & Operational SLAs

This runbook defines backup scheduling, WAL archiving, and Point-In-Time Recovery (PITR) procedures for the SIMS PostgreSQL database cluster.

### Target Service Level Agreements (SLAs)
- **Recovery Point Objective (RPO):** $\le 5\text{ minutes}$ (guaranteed by continuous WAL streaming and 5-minute archive timeout).
- **Recovery Time Objective (RTO):** $\le 30\text{ minutes}$ for a full restoration of a 250GB database volume.

---

## 2. Backup Architecture & Tooling

SIMS utilizes **pgBackRest** (or **Barman Object Store** with CloudNativePG) for enterprise backup orchestration:
- **Continuous WAL Archiving:** WAL segments (16MB chunks) are compressed and shipped immediately to durable object storage upon completion or after `archive_timeout = 300s`.
- **Scheduled Base Backups:**
  - **Full Backup:** Executed weekly (Sundays at 01:00 UTC).
  - **Differential Backup:** Executed daily (Monday–Saturday at 01:00 UTC).
- **Storage Location:** Multi-zone cloud object storage (AWS S3 / Google Cloud Storage / Azure Blob) with versioning and immutable object lock enabled for ransomware protection.

---

## 3. Configuration & Parameter Settings

### PostgreSQL Configuration (`postgresql.conf` / CNPG Spec):
```ini
# Continuous Archiving
wal_level = replica
archive_mode = on
archive_timeout = 300s
archive_command = 'pgbackrest --stanza=sims archive-push %p'
max_wal_senders = 10
wal_keep_size = 4096MB
```

### pgBackRest Stanza Configuration (`/etc/pgbackrest/pgbackrest.conf`):
```ini
[global]
repo1-type=s3
repo1-s3-bucket=sims-db-backups-prod
repo1-s3-endpoint=s3.us-west-2.amazonaws.com
repo1-s3-region=us-west-2
repo1-retention-full=2
repo1-retention-diff=7
process-max=4
compress-type=zst
compress-level=3

[sims]
pg1-path=/var/lib/postgresql/18/main
pg1-user=postgres
```

---

## 4. Backup Operations

### 4.1 On-Demand Backup Execution
```bash
# Execute manual full base backup
pgbackrest --stanza=sims --type=full backup

# Execute manual differential backup
pgbackrest --stanza=sims --type=diff backup

# Inspect backup repository integrity and list points in time
pgbackrest --stanza=sims info
```

### 4.2 Automated Verification & Health Checks
A daily cron job runs repository verification:
```bash
pgbackrest --stanza=sims check
```

---

## 5. Restoration & Point-In-Time Recovery (PITR) Procedure

### 5.1 Restoring to the Latest Point in Time
Use this procedure after catastrophic hardware loss:
```bash
# 1. Stop PostgreSQL service
systemctl stop postgresql

# 2. Clear existing corrupted data directory
rm -rf /var/lib/postgresql/18/main/*

# 3. Restore latest base backup + replay all WAL archives
pgbackrest --stanza=sims --type=immediate restore

# 4. Start PostgreSQL service
systemctl start postgresql

# 5. Monitor replay in log
tail -f /var/log/postgresql/postgresql-18-main.log
```

### 5.2 Point-In-Time Recovery (PITR) to a Specific Timestamp
Use this procedure to recover from an accidental administrative deletion (e.g. before a flawed migration or manual script):
```bash
# Target recovery timestamp: 2026-10-06 14:30:00 UTC
TARGET_TIME="2026-10-06 14:30:00+00"

# 1. Stop PostgreSQL service
systemctl stop postgresql
rm -rf /var/lib/postgresql/18/main/*

# 2. Restore specifying recovery target
pgbackrest --stanza=sims \
    --type=time \
    --target="${TARGET_TIME}" \
    --target-action=promote \
    restore

# 3. Start PostgreSQL service
systemctl start postgresql
```

---

## 6. Restore Validation & Integrity Verification Drill

Every restore drill must execute the following validation steps:

1. **Relation & Page Checksums:**
   ```bash
   /usr/lib/postgresql/18/bin/pg_checksums --check -D /var/lib/postgresql/18/main
   ```
2. **Schema & Migration Verification:**
   Verify Goose migrations report exact expected version:
   ```bash
   goose -dir db/migrations postgres "$DB_URL" status
   ```
3. **Foreign Key and Table Row Integrity:**
   ```sql
   -- Check that critical parent-child relations are intact
   SELECT count(*) FROM virtual_machines;
   SELECT count(*) FROM guest_nics;
   SELECT count(*) FROM vm_disks;
   SELECT count(*) FROM vm_tags;
   SELECT count(*) FROM event_ledger;
   ```
4. **Read & Write Sanity Test:**
   Insert a test heartbeat row and confirm query API can read without errors.

---

## 7. RPO & RTO Measurement Protocol

### RPO Measurement (Lag in Bytes / Time):
```sql
SELECT
    pg_wal_lsn_diff(pg_current_wal_lsn(), pg_last_wal_replay_lsn()) AS replication_lag_bytes,
    NOW() - pg_last_xact_replay_timestamp() AS replication_time_lag;
```
- In CloudNativePG: monitored by metric `cnpg_collector_wal_archive_status`.
- Alert if WAL archiving lag exceeds 5 minutes.

### RTO Measurement Drill:
1. Log start timestamp: $T_{\text{start}} = \text{NOW()}$.
2. Issue restore command and wait for promotion: $T_{\text{ready}} = \text{NOW()}$.
3. Calculate elapsed time:
   $$\text{RTO} = T_{\text{ready}} - T_{\text{start}}$$
4. Record results in `docs/backup-restore-runbook.md` audit logs. Target is strictly $< 30\text{ minutes}$.
