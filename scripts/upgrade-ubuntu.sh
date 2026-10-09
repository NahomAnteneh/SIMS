#!/usr/bin/env bash
set -euo pipefail

# SIMS Automated Upgrade Script for Ubuntu
# Performs atomic release upgrades with database migrations and automated rollback on failure.

if [ "$EUID" -ne 0 ]; then
  echo "ERROR: This script must be run as root (or via sudo)." >&2
  exit 1
fi

if [ $# -lt 1 ]; then
  echo "Usage: $0 <path-to-sims-release-tarball> [version]" >&2
  echo "Example: $0 sims-v1.1.0-linux-amd64.tar.gz 1.1.0" >&2
  exit 1
fi

RELEASE_TARBALL="$1"
if [ ! -f "${RELEASE_TARBALL}" ]; then
  echo "ERROR: Release tarball not found at ${RELEASE_TARBALL}" >&2
  exit 1
fi

# Determine target version
if [ $# -ge 2 ]; then
  NEW_VERSION="$2"
else
  # Extract version from filename pattern sims-vX.Y.Z-...
  NEW_VERSION="$(basename "${RELEASE_TARBALL}" | sed -n 's/^sims-v\([0-9.]*\).*/\1/p')"
  if [ -z "${NEW_VERSION}" ]; then
    NEW_VERSION="$(date +%Y%m%d%H%M%S)"
  fi
fi

CURRENT_LINK="/opt/sims/current"
PREV_DIR="$(readlink -f "${CURRENT_LINK}" || true)"
TARGET_DIR="/opt/sims/releases/v${NEW_VERSION}"

echo "================================================================================"
echo "                   SIMS Release Upgrade to v${NEW_VERSION}                      "
echo "================================================================================"
echo "Active Release:      ${PREV_DIR}"
echo "Target Release:      ${TARGET_DIR}"
echo "Archive:             ${RELEASE_TARBALL}"
echo "================================================================================"

# 1. Verify Configuration & Environment
if [ ! -f "/etc/sims/migration.env" ]; then
  echo "ERROR: /etc/sims/migration.env not found. Ensure SIMS host is installed." >&2
  exit 1
fi

# Load migration connection string
# shellcheck disable=SC1091
source /etc/sims/migration.env

if [ -z "${MIGRATION_DATABASE_URL:-}" ]; then
  echo "ERROR: MIGRATION_DATABASE_URL is not configured in /etc/sims/migration.env." >&2
  exit 1
fi

# 2. Extract New Release
echo "--> [1/5] Extracting release archive..."
mkdir -p "${TARGET_DIR}"
tar -xzf "${RELEASE_TARBALL}" -C "/opt/sims/releases/"
if [ -d "/opt/sims/releases/sims-v${NEW_VERSION}" ] && [ "/opt/sims/releases/sims-v${NEW_VERSION}" != "${TARGET_DIR}" ]; then
  rm -rf "${TARGET_DIR}"
  mv "/opt/sims/releases/sims-v${NEW_VERSION}" "${TARGET_DIR}"
fi

chmod +x "${TARGET_DIR}/bin/"*
chown -R sims:sims "${TARGET_DIR}"

# 3. Apply Schema Migrations
echo "--> [2/5] Running forward database migrations..."
if ! "${TARGET_DIR}/bin/goose" -dir "${TARGET_DIR}/migrations" postgres "${MIGRATION_DATABASE_URL}" up; then
  echo "================================================================================" >&2
  echo "ABORT: Database migration failed. Current release ${PREV_DIR} is untouched." >&2
  echo "================================================================================" >&2
  rm -rf "${TARGET_DIR}"
  exit 1
fi

# 4. Atomic Symlink Flip & Service Reload
echo "--> [3/5] Performing atomic symlink switch to v${NEW_VERSION}..."
ln -sfn "${TARGET_DIR}" "${CURRENT_LINK}"

# Update any new systemd unit files if present
if [ -d "${TARGET_DIR}/deploy/systemd" ]; then
  cp "${TARGET_DIR}/deploy/systemd/"*.service /etc/systemd/system/
  systemctl daemon-reload
fi

echo "--> [4/5] Restarting SIMS services..."
systemctl restart sims-collector sims-query-api sims-web

# 5. Health Verification & Automated Rollback
echo "--> [5/5] Performing post-upgrade health probe verification..."
sleep 3

MAX_RETRIES=15
RETRY_COUNT=0
UPGRADE_HEALTHY=false

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
  if curl -sf http://127.0.0.1:8080/readyz >/dev/null 2>&1 && \
     curl -sf http://127.0.0.1:8081/readyz >/dev/null 2>&1; then
    UPGRADE_HEALTHY=true
    break
  fi
  echo "    Waiting for services to become healthy (attempt $((RETRY_COUNT + 1))/${MAX_RETRIES})..."
  sleep 2
  RETRY_COUNT=$((RETRY_COUNT + 1))
done

if [ "${UPGRADE_HEALTHY}" = true ]; then
  echo "================================================================================"
  echo "            UPGRADE SUCCESSFUL: SIMS is now running v${NEW_VERSION}             "
  echo "================================================================================"
  echo "All health and readiness checks passed."
  echo "Active symlink: ${CURRENT_LINK} -> ${TARGET_DIR}"
  exit 0
else
  echo "================================================================================" >&2
  echo "CRITICAL: Upgrade health check failed! Initiating automatic rollback..." >&2
  echo "================================================================================" >&2
  
  if [ -n "${PREV_DIR}" ] && [ -d "${PREV_DIR}" ]; then
    echo "--> Rolling back symlink to previous release: ${PREV_DIR}"
    ln -sfn "${PREV_DIR}" "${CURRENT_LINK}"
    systemctl restart sims-collector sims-query-api sims-web
    sleep 3
    if curl -sf http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
      echo "--> Rollback verified: Previous version is running and healthy."
    else
      echo "--> Warning: Health checks failed after rollback. Inspect systemd journals."
    fi
  else
    echo "ERROR: Previous release directory not found. Cannot automatically rollback." >&2
  fi
  
  exit 1
fi
