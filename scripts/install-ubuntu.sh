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

# 3. Configure PostgreSQL Engine
echo "--> [3/8] Configuring PostgreSQL database and isolated service roles..."
systemctl start postgresql
systemctl enable postgresql

# Generate cryptographically secure passwords
GEN_MIGRATION_PASS=$(openssl rand -hex 16)
GEN_COLLECTOR_PASS=$(openssl rand -hex 16)
GEN_QUERY_PASS=$(openssl rand -hex 16)
GEN_JWT_SECRET=$(openssl rand -hex 32)
GEN_ADMIN_PASS=$(openssl rand -hex 12)

sudo -u postgres psql -v ON_ERROR_STOP=1 <<EOF
-- Create Database
SELECT 'CREATE DATABASE sims' WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'sims')\gexec

-- Connect to database and configure extensions
\c sims
CREATE EXTENSION IF NOT EXISTS ltree;
CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Create Roles with strong random passwords
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'migration_owner') THEN
    CREATE ROLE migration_owner WITH LOGIN PASSWORD '${GEN_MIGRATION_PASS}' CREATEDB;
  ELSE
    ALTER ROLE migration_owner WITH PASSWORD '${GEN_MIGRATION_PASS}';
  END IF;

  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'collector_rw') THEN
    CREATE ROLE collector_rw WITH LOGIN PASSWORD '${GEN_COLLECTOR_PASS}';
  ELSE
    ALTER ROLE collector_rw WITH PASSWORD '${GEN_COLLECTOR_PASS}';
  END IF;

  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'query_ro') THEN
    CREATE ROLE query_ro WITH LOGIN PASSWORD '${GEN_QUERY_PASS}';
  ELSE
    ALTER ROLE query_ro WITH PASSWORD '${GEN_QUERY_PASS}';
  END IF;
END
\$\$;

GRANT ALL PRIVILEGES ON DATABASE sims TO migration_owner;
GRANT ALL ON SCHEMA public TO migration_owner;
EOF

# 4. Generate Production Configuration Files
echo "--> [4/8] Generating secure service environment files in /etc/sims/..."

cat > /etc/sims/migration.env <<EOF
MIGRATION_DATABASE_URL=postgres://migration_owner:${GEN_MIGRATION_PASS}@127.0.0.1:5432/sims?sslmode=disable
EOF

cat > /etc/sims/collector.env <<EOF
PORT=8081
LOG_LEVEL=info
DATABASE_URL=postgres://collector_rw:${GEN_COLLECTOR_PASS}@127.0.0.1:5432/sims?sslmode=disable
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
DATABASE_URL=postgres://query_ro:${GEN_QUERY_PASS}@127.0.0.1:5432/sims?sslmode=disable
AUTH_ENABLED=true
JWT_SECRET=${GEN_JWT_SECRET}
ADMIN_INITIAL_PASSWORD=${GEN_ADMIN_PASS}
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
"${TARGET_DIR}/bin/goose" -dir "${TARGET_DIR}/migrations" postgres "postgres://migration_owner:${GEN_MIGRATION_PASS}@127.0.0.1:5432/sims?sslmode=disable" up

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
  echo "  Username: admin"
  echo "  Password: ${GEN_ADMIN_PASS}"
  echo ""
  echo "IMPORTANT: Save these credentials securely. They will not be displayed again."
  echo "Configurations saved in /etc/sims/*.env"
  echo "================================================================================"
else
  echo "WARNING: Services did not report ready within the expected time window."
  echo "Check systemd status with: systemctl status sims-query-api sims-collector sims-web"
  exit 1
fi
