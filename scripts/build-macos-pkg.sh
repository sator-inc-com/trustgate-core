#!/bin/bash
# Build a macOS .pkg installer for TrustGate
# Usage: bash scripts/build-macos-pkg.sh <version> <arch> [binary-dir]
#   version:    e.g., 0.1.0
#   arch:       arm64 or amd64
#   binary-dir: directory containing aigw binaries (default: dist/)
#
# Output: TrustGate-macOS-<version>-<arch>.pkg

set -euo pipefail

VERSION="${1:?Usage: $0 <version> <arch> [binary-dir]}"
VERSION="${VERSION#v}"  # Strip leading 'v' for installer compatibility
ARCH="${2:?Usage: $0 <version> <arch> [binary-dir]}"
BINARY_DIR="${3:-dist}"
SIGN_IDENTITY="${MACOS_SIGN_IDENTITY:-}"  # "Developer ID Installer: ..." (empty = unsigned)
APP_SIGN_IDENTITY="${MACOS_APP_SIGN_IDENTITY:-}"  # "Developer ID Application: ..." (empty = skip)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
WORK_DIR=$(mktemp -d)
PKG_ROOT="${WORK_DIR}/root"
PKG_SCRIPTS="${WORK_DIR}/scripts"
PKG_RESOURCES="${WORK_DIR}/resources"
OUTPUT_PKG="${REPO_DIR}/TrustGate-macOS-${VERSION}-${ARCH}.pkg"

cleanup() {
    rm -rf "$WORK_DIR"
}
trap cleanup EXIT

echo "=== Building TrustGate macOS .pkg ==="
echo "  Version: ${VERSION}"
echo "  Arch:    ${ARCH}"
echo "  Binaries: ${BINARY_DIR}"
echo ""

# --- Create staging layout ---
mkdir -p "${PKG_ROOT}/usr/local/bin"
mkdir -p "${PKG_ROOT}/Library/Application Support/TrustGate"
mkdir -p "${PKG_ROOT}/Library/LaunchAgents"
mkdir -p "${PKG_SCRIPTS}"
mkdir -p "${PKG_RESOURCES}"

# --- Copy binaries ---
AGENT_BINARY="${BINARY_DIR}/aigw-darwin-${ARCH}"
if [ ! -f "$AGENT_BINARY" ]; then
    echo "ERROR: Agent binary not found: ${AGENT_BINARY}"
    exit 1
fi
cp "$AGENT_BINARY" "${PKG_ROOT}/usr/local/bin/aigw"
chmod 755 "${PKG_ROOT}/usr/local/bin/aigw"
# Sign agent binary (required for notarization)
if [ -n "$APP_SIGN_IDENTITY" ]; then
    echo "  Signing aigw with: ${APP_SIGN_IDENTITY}"
    codesign --force --options runtime \
        --sign "$APP_SIGN_IDENTITY" \
        "${PKG_ROOT}/usr/local/bin/aigw"
fi

TRAY_BINARY="${BINARY_DIR}/aigw-tray-darwin-${ARCH}"
if [ -f "$TRAY_BINARY" ]; then
    # Create .app bundle for Tray Manager
    APP_DIR="${PKG_ROOT}/Applications/TrustGate.app/Contents"
    mkdir -p "${APP_DIR}/MacOS"
    mkdir -p "${APP_DIR}/Resources"
    cp "$TRAY_BINARY" "${APP_DIR}/MacOS/aigw-tray"
    chmod 755 "${APP_DIR}/MacOS/aigw-tray"
    # Copy Info.plist with version substitution
    cp "${SCRIPT_DIR}/Info.plist" "${APP_DIR}/Info.plist"
    sed -i '' "s|__VERSION__|${VERSION}|g" "${APP_DIR}/Info.plist"
    # Copy app icon
    if [ -f "${SCRIPT_DIR}/AppIcon.icns" ]; then
        cp "${SCRIPT_DIR}/AppIcon.icns" "${APP_DIR}/Resources/AppIcon.icns"
    fi
    # Sign the .app bundle (required for notarization)
    if [ -n "$APP_SIGN_IDENTITY" ]; then
        echo "  Signing .app bundle with: ${APP_SIGN_IDENTITY}"
        codesign --force --options runtime --deep \
            --sign "$APP_SIGN_IDENTITY" \
            "${PKG_ROOT}/Applications/TrustGate.app"
    fi
    echo "  Tray manager: included (.app bundle)"
else
    echo "  Tray manager: not available for ${ARCH}, skipping"
fi

# --- Copy config files ---
cp "${SCRIPT_DIR}/default-agent.yaml" "${PKG_ROOT}/Library/Application Support/TrustGate/agent.yaml"
cp "${SCRIPT_DIR}/default-policies.yaml" "${PKG_ROOT}/Library/Application Support/TrustGate/policies.yaml"

# --- Copy uninstall script ---
cp "${SCRIPT_DIR}/uninstall-trustgate.sh" "${PKG_ROOT}/Library/Application Support/TrustGate/uninstall-trustgate.sh"
chmod 755 "${PKG_ROOT}/Library/Application Support/TrustGate/uninstall-trustgate.sh"

# --- Copy launchd plists (both as LaunchAgents for user-level control) ---
cp "${SCRIPT_DIR}/com.trustgate.agent.plist" "${PKG_ROOT}/Library/LaunchAgents/"

if [ -d "${PKG_ROOT}/Applications/TrustGate.app" ]; then
    cp "${SCRIPT_DIR}/com.trustgate.tray.plist" "${PKG_ROOT}/Library/LaunchAgents/"
fi

# --- Copy install scripts ---
cp "${SCRIPT_DIR}/preinstall" "${PKG_SCRIPTS}/preinstall"
cp "${SCRIPT_DIR}/postinstall" "${PKG_SCRIPTS}/postinstall"
chmod 755 "${PKG_SCRIPTS}/preinstall"
chmod 755 "${PKG_SCRIPTS}/postinstall"

# --- Copy resources ---
cp "${SCRIPT_DIR}/welcome.html" "${PKG_RESOURCES}/welcome.html"

# --- Prepare distribution.xml ---
cp "${SCRIPT_DIR}/distribution.xml" "${WORK_DIR}/distribution.xml"
sed -i '' "s|__VERSION__|${VERSION}|g" "${WORK_DIR}/distribution.xml"

# --- Build component package ---
echo "Building component package..."
pkgbuild \
    --root "${PKG_ROOT}" \
    --scripts "${PKG_SCRIPTS}" \
    --identifier "com.trustgate.agent" \
    --version "${VERSION}" \
    --install-location "/" \
    "${WORK_DIR}/trustgate-component.pkg"

# --- Build product package ---
echo "Building product package..."
if [ -n "$SIGN_IDENTITY" ]; then
    echo "  Signing with: ${SIGN_IDENTITY}"
    productbuild \
        --distribution "${WORK_DIR}/distribution.xml" \
        --resources "${PKG_RESOURCES}" \
        --package-path "${WORK_DIR}" \
        --sign "${SIGN_IDENTITY}" \
        "${OUTPUT_PKG}"
else
    echo "  WARNING: No signing identity set (unsigned package)"
    productbuild \
        --distribution "${WORK_DIR}/distribution.xml" \
        --resources "${PKG_RESOURCES}" \
        --package-path "${WORK_DIR}" \
        "${OUTPUT_PKG}"
fi

echo ""
echo "=== Done ==="
echo "  Output: ${OUTPUT_PKG}"
echo "  Size:   $(du -h "${OUTPUT_PKG}" | cut -f1)"
echo ""
echo "Install with:"
echo "  sudo installer -pkg ${OUTPUT_PKG} -target /"
