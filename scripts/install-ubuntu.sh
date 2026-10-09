#!/usr/bin/env bash
set -euo pipefail

# SIMS Ubuntu Host Automated Installation Script
# Deploys SIMS as native systemd services on Ubuntu (no Docker, no vcsim).

if [ "$EUID" -ne 0 ]; then
  echo "ERROR: This script must be run as root (or via sudo)." >&2
  exit 1
fi

VERSION="${1:-1.0.0}"
RELEASE_TARBALL="${2:-}"

echo "================================================================================"
echo "          SIMS v${VERSION} Production Host Installer for Ubuntu Linux           "
echo "================================================================================"

# 1. Install System Dependencies
echo "--> [1/8] Installing host packages..."
apt-get update -y
apt-get install -y --no-install-recommends \
  curl \
  tar \
  ca-certificates \
  postgresql \
  postgresql-contrib \
  nginx

# Install Node.js runtime if not present
if ! command -v node >/dev/null 2>&1; then
  echo "--> Installing Node.js LTS runtime..."
  mkdir -p /etc/apt/keyrings
  curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg
  echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_20.x nodistro main" > /etc/apt/sources.list.d/nodesource.list
  apt-get update -y
  apt-get install -y nodejs
fi

# 2. Create System User and Directories
echo "--> [2/8] Creating system user and directory structure..."
if ! id -u sims >/dev/null 2>&1; then
  useradd -r -m -d /opt/sims -s /usr/sbin/nologin -c "SIMS Service Account" sims
fi

mkdir -p /opt/sims/releases
mkdir -p /opt/sims/shared
mkdir -p /etc/sims
mkdir -p /var/log/sims

# Load existing configuration if present
if [ -f "/etc/sims/database.env" ]; then
  # shellcheck disable=SC1091
  source /etc/sims/database.env
fi
if [ -f "/etc/sims/sims.env" ]; then
  # shellcheck disable=SC1091
  source /etc/sims/sims.env
fi

# Detect Database Configuration (Single unified user for all operations)
DB_USER="${DB_USER:-${DATABASE_USER:-${SIMS_DB_USER:-sims}}}"
DB_NAME="${DB_NAME:-${DATABASE_NAME:-${SIMS_DB_NAME:-sims}}}"
DB_HOST="${DB_HOST:-${DATABASE_HOST:-127.0.0.1}}"
DB_PORT="${DB_PORT:-${DATABASE_PORT:-5432}}"

DB_PASSWORD="${DB_PASSWORD:-${DATABASE_PASSWORD:-${SIMS_DB_PASSWORD:-}}}"
if [ -z "${DB_PASSWORD}" ]; then
  DB_PASSWORD=$(openssl rand -hex 16)
fi

# Detect Application Administrator Credentials
ADMIN_USERNAME="${ADMIN_USERNAME:-${ADMIN_INITIAL_USERNAME:-${SIMS_ADMIN_USERNAME:-admin}}}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-${ADMIN_INITIAL_PASSWORD:-${SIMS_ADMIN_PASSWORD:-}}}"
if [ -z "${ADMIN_PASSWORD}" ]; then
  ADMIN_PASSWORD=$(openssl rand -hex 12)
fi

GEN_JWT_SECRET="${JWT_SECRET:-$(openssl rand -hex 32)}"

# 3. Configure PostgreSQL Engine & Single Unified Role
echo "--> [3/8] Configuring PostgreSQL database and single user (${DB_USER})..."
systemctl start postgresql
systemctl enable postgresql

sudo -u postgres psql -v ON_ERROR_STOP=1 <<EOF
-- Create Database
SELECT 'CREATE DATABASE ${DB_NAME}' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '${DB_NAME}')\gexec

-- Connect to database and configure extensions
\c ${DB_NAME}
CREATE EXTENSION IF NOT EXISTS ltree;
CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Create ONLY ONE user for all operations
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '${DB_USER}') THEN
    CREATE ROLE "${DB_USER}" WITH LOGIN PASSWORD '${DB_PASSWORD}' CREATEDB;
  ELSE
    ALTER ROLE "${DB_USER}" WITH PASSWORD '${DB_PASSWORD}';
  END IF;
END
\$\$;

GRANT ALL PRIVILEGES ON DATABASE ${DB_NAME} TO "${DB_USER}";
GRANT ALL ON SCHEMA public TO "${DB_USER}";
ALTER SCHEMA public OWNER TO "${DB_USER}";
ALTER DATABASE ${DB_NAME} OWNER TO "${DB_USER}";
EOF

# 4. Generate Production Configuration Files
echo "--> [4/8] Generating secure service environment files in /etc/sims/..."

cat > /etc/sims/migration.env <<EOF
MIGRATION_DATABASE_URL=postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable
EOF

cat > /etc/sims/collector.env <<EOF
PORT=8081
LOG_LEVEL=info
DATABASE_URL=postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable
VCENTER_URL=
VCENTER_USER=
VCENTER_PASSWORD=
VCENTER_INSECURE=false
SYNC_INTERVAL=30s
SIMS_COLLECTOR_KEEPALIVE_INTERVAL=60s
SIMS_COLLECTOR_KEEPALIVE_TIMEOUT=10s
SIMS_COLLECTOR_KEEPALIVE_MAX_FAILURES=2
SIMS_COLLECTOR_MAX_STREAM_BLOCK_DURATION=5m
EOF

cat > /etc/sims/query-api.env <<EOF
PORT=8080
LOG_LEVEL=info
DATABASE_URL=postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable
AUTH_ENABLED=true
JWT_SECRET=${GEN_JWT_SECRET}
ADMIN_USERNAME=${ADMIN_USERNAME}
ADMIN_PASSWORD=${ADMIN_PASSWORD}
CORS_ALLOWED_ORIGINS=
EOF

cat > /etc/sims/web.env <<EOF
PORT=8082
HOSTNAME=127.0.0.1
NODE_ENV=production
INTERNAL_QUERY_API_URL=http://127.0.0.1:8080
QUERY_API_URL=http://127.0.0.1:8080
EOF

chmod 0600 /etc/sims/*.env
chown -R sims:sims /etc/sims

# 5. Extract Release Artifacts
echo "--> [5/8] Staging release artifacts to /opt/sims/releases/v${VERSION}..."
TARGET_DIR="/opt/sims/releases/v${VERSION}"
mkdir -p "${TARGET_DIR}"

if [ -n "${RELEASE_TARBALL}" ] && [ -f "${RELEASE_TARBALL}" ]; then
  tar -xzf "${RELEASE_TARBALL}" -C "/opt/sims/releases/"
  if [ -d "/opt/sims/releases/sims-v${VERSION}" ]; then
    rm -rf "${TARGET_DIR}"
    mv "/opt/sims/releases/sims-v${VERSION}" "${TARGET_DIR}"
  fi
else
  # Running from local cloned directory
  SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
  if [ -d "${REPO_ROOT}/dist/sims-v${VERSION}" ]; then
    cp -r "${REPO_ROOT}/dist/sims-v${VERSION}"/* "${TARGET_DIR}/"
  else
    echo "ERROR: Release artifacts not found. Please provide release tarball or run ./scripts/package-release.sh first." >&2
    exit 1
  fi
fi

# Ensure executable permissions
chmod +x "${TARGET_DIR}/bin/"*

# 6. Apply Schema Migrations
echo "--> [6/8] Executing database schema migrations via Goose..."
"${TARGET_DIR}/bin/goose" -dir "${TARGET_DIR}/migrations" postgres "postgres://${DB_USER}:${DB_PASSWORD}@${DB_HOST}:${DB_PORT}/${DB_NAME}?sslmode=disable" up

# Atomic symlink
ln -sfn "${TARGET_DIR}" /opt/sims/current
chown -R sims:sims /opt/sims

# 7. Configure and Enable Systemd Services & Nginx
echo "--> [7/8] Installing systemd services and Nginx configuration..."
cp "${TARGET_DIR}/deploy/systemd/sims-collector.service" /etc/systemd/system/
cp "${TARGET_DIR}/deploy/systemd/sims-query-api.service" /etc/systemd/system/
cp "${TARGET_DIR}/deploy/systemd/sims-web.service" /etc/systemd/system/

systemctl daemon-reload
systemctl enable sims-collector sims-query-api sims-web
systemctl restart sims-collector sims-query-api sims-web

cp "${TARGET_DIR}/deploy/nginx/sims.conf" /etc/nginx/sites-available/sims
ln -sfn /etc/nginx/sites-available/sims /etc/nginx/sites-enabled/sims
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl restart nginx

# 8. Verify Deployment Health
echo "--> [8/8] Verifying health and readiness probes..."
sleep 3

MAX_RETRIES=10
RETRY_COUNT=0
HEALTHY=false

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
  if curl -sf http://127.0.0.1:8080/readyz >/dev/null 2>&1 && \
     curl -sf http://127.0.0.1:8081/readyz >/dev/null 2>&1; then
    HEALTHY=true
    break
  fi
  echo "    Waiting for services to report ready (attempt $((RETRY_COUNT + 1))/${MAX_RETRIES})..."
  sleep 2
  RETRY_COUNT=$((RETRY_COUNT + 1))
done

if [ "$HEALTHY" = true ]; then
  echo "================================================================================"
  echo "               SIMS v${VERSION} Installation Completed Successfully!            "
  echo "================================================================================"
  echo ""
  echo "Web Console URL:        http://$(hostname -I | awk '{print $1}')/"
  echo "Query API Endpoint:     http://$(hostname -I | awk '{print $1}')/v1/"
  echo ""
  echo "INITIAL BOOTSTRAP ADMINISTRATOR CREDENTIALS:"
  echo "  Username: ${ADMIN_USERNAME}"
  echo "  Password: ${ADMIN_PASSWORD}"
  echo ""
  echo "DATABASE CREDENTIALS (SINGLE UNIFIED USER):"
  echo "  Username: ${DB_USER}"
  echo "  Database: ${DB_NAME}"
  echo ""
  echo "IMPORTANT: Save these credentials securely. They will not be displayed again."
  echo "Configurations saved in /etc/sims/*.env"
  echo "================================================================================"
else
  echo "WARNING: Services did not report ready within the expected time window."
  echo "Check systemd status with: systemctl status sims-query-api sims-collector sims-web"
  exit 1
fi
