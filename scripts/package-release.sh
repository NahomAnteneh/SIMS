#!/usr/bin/env bash
set -euo pipefail

# SIMS Release Packaging Script
# Builds Go binaries, Next.js standalone web frontend, and bundles migrations into a tarball.

VERSION="${1:-1.0.0}"
ARCH="${ARCH:-amd64}"
OS="${OS:-linux}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
DIST_DIR="${REPO_ROOT}/dist"
STAGE_DIR="${DIST_DIR}/sims-v${VERSION}"
TARBALL="${DIST_DIR}/sims-v${VERSION}-${OS}-${ARCH}.tar.gz"

echo "=== Packaging SIMS v${VERSION} for ${OS}/${ARCH} ==="

mkdir -p "${DIST_DIR}"
rm -rf "${STAGE_DIR}" "${TARBALL}"
mkdir -p "${STAGE_DIR}/bin" "${STAGE_DIR}/migrations" "${STAGE_DIR}/web" "${STAGE_DIR}/deploy" "${STAGE_DIR}/scripts"

# 1. Ensure Goose is present
echo "--> Ensuring Goose migration CLI is built..."
make -C "${REPO_ROOT}" tools
cp "${REPO_ROOT}/bin/goose" "${STAGE_DIR}/bin/goose"

# 2. Build Go Daemons (collector and query-api)
echo "--> Compiling Go binaries for ${OS}/${ARCH}..."
CGO_ENABLED=0 GOOS="${OS}" GOARCH="${ARCH}" go build -ldflags="-s -w" -o "${STAGE_DIR}/bin/collector" "${REPO_ROOT}/cmd/collector"
CGO_ENABLED=0 GOOS="${OS}" GOARCH="${ARCH}" go build -ldflags="-s -w" -o "${STAGE_DIR}/bin/query-api" "${REPO_ROOT}/cmd/query-api"

# 3. Build Next.js Web Frontend (Standalone mode)
echo "--> Building Next.js Web Console (standalone)..."
(
  cd "${REPO_ROOT}/web"
  npm run build
)

echo "--> Assembling standalone web distribution..."
cp -r "${REPO_ROOT}/web/.next/standalone/." "${STAGE_DIR}/web/"
mkdir -p "${STAGE_DIR}/web/.next"
cp -r "${REPO_ROOT}/web/.next/static" "${STAGE_DIR}/web/.next/static"
if [ -d "${REPO_ROOT}/web/public" ]; then
  cp -r "${REPO_ROOT}/web/public" "${STAGE_DIR}/web/public"
fi

# 4. Copy Database Migrations
echo "--> Copying SQL schema migrations..."
cp -r "${REPO_ROOT}/db/migrations"/* "${STAGE_DIR}/migrations/"

# 5. Copy Deploy configurations and scripts
echo "--> Copying deployment assets..."
cp -r "${REPO_ROOT}/deploy/systemd" "${STAGE_DIR}/deploy/"
cp -r "${REPO_ROOT}/deploy/nginx" "${STAGE_DIR}/deploy/"
cp -r "${REPO_ROOT}/deploy/env" "${STAGE_DIR}/deploy/"
cp "${REPO_ROOT}/scripts/install-ubuntu.sh" "${STAGE_DIR}/scripts/"
cp "${REPO_ROOT}/scripts/upgrade-ubuntu.sh" "${STAGE_DIR}/scripts/"
chmod +x "${STAGE_DIR}/scripts/"*.sh

# 6. Create Tarball
echo "--> Creating release archive: ${TARBALL}..."
(
  cd "${DIST_DIR}"
  tar -czf "${TARBALL}" -C "${DIST_DIR}" "sims-v${VERSION}"
)

echo "=== Release package created successfully: ${TARBALL} ==="
ls -lh "${TARBALL}"
