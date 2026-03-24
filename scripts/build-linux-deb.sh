#!/bin/bash
# Build a Debian .deb package for TrustGate
# Usage: bash scripts/build-linux-deb.sh <version> <arch> [binary-dir]
#   version:    e.g., 0.1.0
#   arch:       amd64 or arm64
#   binary-dir: directory containing aigw binary (default: dist/)
#
# Output: trustgate_<version>_<arch>.deb
#
# Requirements: dpkg-deb (available on Ubuntu/Debian runners)

set -euo pipefail

VERSION="${1:?Usage: $0 <version> <arch> [binary-dir]}"
ARCH="${2:?Usage: $0 <version> <arch> [binary-dir]}"
BINARY_DIR="${3:-dist}"

# Map Go arch names to Debian arch names
DEB_ARCH="$ARCH"
if [ "$ARCH" = "amd64" ]; then
    DEB_ARCH="amd64"
elif [ "$ARCH" = "arm64" ]; then
    DEB_ARCH="arm64"
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
WORK_DIR=$(mktemp -d)
PKG_DIR="${WORK_DIR}/trustgate_${VERSION}_${DEB_ARCH}"
OUTPUT_DEB="${REPO_DIR}/trustgate_${VERSION}_${DEB_ARCH}.deb"

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

echo "=== Building TrustGate .deb package ==="
echo "  Version: ${VERSION}"
echo "  Arch:    ${DEB_ARCH}"
echo "  Binaries: ${BINARY_DIR}"
echo ""

# --- Create directory structure ---
mkdir -p "${PKG_DIR}/DEBIAN"
mkdir -p "${PKG_DIR}/usr/local/bin"
mkdir -p "${PKG_DIR}/etc/trustgate"
mkdir -p "${PKG_DIR}/lib/systemd/system"
mkdir -p "${PKG_DIR}/var/log/trustgate"
mkdir -p "${PKG_DIR}/var/lib/trustgate"

# --- Copy binary ---
AGENT_BINARY="${BINARY_DIR}/aigw-linux-${ARCH}"
if [ ! -f "$AGENT_BINARY" ]; then
    echo "ERROR: Agent binary not found: ${AGENT_BINARY}"
    exit 1
fi
cp "$AGENT_BINARY" "${PKG_DIR}/usr/local/bin/aigw"
chmod 755 "${PKG_DIR}/usr/local/bin/aigw"

# --- Copy config files ---
cp "${SCRIPT_DIR}/default-agent.yaml" "${PKG_DIR}/etc/trustgate/agent.yaml"
cp "${SCRIPT_DIR}/default-policies.yaml" "${PKG_DIR}/etc/trustgate/policies.yaml"

# --- Copy systemd unit ---
cp "${SCRIPT_DIR}/trustgate.service" "${PKG_DIR}/lib/systemd/system/trustgate.service"

# --- Create DEBIAN/control ---
INSTALLED_SIZE=$(du -sk "${PKG_DIR}" | cut -f1)
cat > "${PKG_DIR}/DEBIAN/control" <<CTRL
Package: trustgate
Version: ${VERSION}
Section: net
Priority: optional
Architecture: ${DEB_ARCH}
Installed-Size: ${INSTALLED_SIZE}
Maintainer: Sator Inc <support@sator-inc.com>
Homepage: https://github.com/sator-inc-com/trustgate-core
Description: TrustGate AI Zero Trust Gateway
 Inspect and control AI input/output in real time.
 Provides text inspection, policy enforcement, and LLM proxy
 capabilities as a sidecar or desktop agent.
CTRL

# --- Create DEBIAN/postinst ---
cat > "${PKG_DIR}/DEBIAN/postinst" <<'POSTINST'
#!/bin/bash
set -e

# Reload systemd and enable service
systemctl daemon-reload
systemctl enable trustgate.service

# Start service
systemctl start trustgate.service || true

echo ""
echo "TrustGate Agent installed successfully."
echo ""
echo "  Service: systemctl status trustgate"
echo "  Config:  /etc/trustgate/agent.yaml"
echo "  Logs:    journalctl -u trustgate -f"
echo "  Verify:  curl -s http://localhost:8787/v1/health | jq ."
echo ""
POSTINST
chmod 755 "${PKG_DIR}/DEBIAN/postinst"

# --- Create DEBIAN/prerm ---
cat > "${PKG_DIR}/DEBIAN/prerm" <<'PRERM'
#!/bin/bash
set -e

# Stop and disable service before removal
systemctl stop trustgate.service 2>/dev/null || true
systemctl disable trustgate.service 2>/dev/null || true
PRERM
chmod 755 "${PKG_DIR}/DEBIAN/prerm"

# --- Create DEBIAN/postrm ---
cat > "${PKG_DIR}/DEBIAN/postrm" <<'POSTRM'
#!/bin/bash
set -e

# Reload systemd after unit file removal
systemctl daemon-reload

# On purge, remove config and data
if [ "$1" = "purge" ]; then
    rm -rf /etc/trustgate
    rm -rf /var/log/trustgate
    rm -rf /var/lib/trustgate
fi
POSTRM
chmod 755 "${PKG_DIR}/DEBIAN/postrm"

# --- Create DEBIAN/conffiles ---
cat > "${PKG_DIR}/DEBIAN/conffiles" <<CONF
/etc/trustgate/agent.yaml
/etc/trustgate/policies.yaml
CONF

# --- Build .deb ---
echo "Building .deb package..."
dpkg-deb --build --root-owner-group "${PKG_DIR}" "${OUTPUT_DEB}"

echo ""
echo "=== Done ==="
echo "  Output: ${OUTPUT_DEB}"
echo "  Size:   $(du -h "${OUTPUT_DEB}" | cut -f1)"
echo ""
echo "Install with:"
echo "  sudo dpkg -i ${OUTPUT_DEB}"
