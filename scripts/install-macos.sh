#!/bin/bash
# TrustGate macOS Installation Script
# Usage:
#   ./install-macos.sh              Install TrustGate
#   ./install-macos.sh --uninstall  Uninstall TrustGate
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="$HOME/Library/Application Support/TrustGate"
LOG_DIR="$HOME/Library/Logs/TrustGate"
LAUNCH_AGENTS="$HOME/Library/LaunchAgents"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo -e "${BLUE}"
echo "  ___________                 __  ________        __          "
echo "  \__    ___/______ __ ______/  |_/  _____/_____ _/  |_  ____  "
echo "    |    |  \_  __ \  |  \  __/   \  ___\\__  \   __\/ __ \ "
echo "    |    |   |  | \/  |  /|  |  \    \_\  \/ __ \|  | \  ___/ "
echo "    |____|   |__|  |____/ |__|   \______  (____  /__|  \___  >"
echo "                                        \/     \/          \/ "
echo -e "${NC}"
echo "  AI Zero Trust Gateway — macOS Installer"
echo ""

# --- Uninstall ---
if [ "$1" = "--uninstall" ]; then
    echo -e "${YELLOW}Uninstalling TrustGate...${NC}"

    # Stop services
    if [ -f "$LAUNCH_AGENTS/com.trustgate.tray.plist" ]; then
        launchctl unload "$LAUNCH_AGENTS/com.trustgate.tray.plist" 2>/dev/null || true
        rm -f "$LAUNCH_AGENTS/com.trustgate.tray.plist"
        echo "  Tray agent removed"
    fi
    if [ -f "$LAUNCH_AGENTS/com.trustgate.agent.plist" ]; then
        launchctl unload "$LAUNCH_AGENTS/com.trustgate.agent.plist" 2>/dev/null || true
        rm -f "$LAUNCH_AGENTS/com.trustgate.agent.plist"
        echo "  Agent service removed"
    fi

    # Kill processes
    pkill -f "aigw serve" 2>/dev/null || true
    pkill -f "aigw-tray" 2>/dev/null || true

    # Remove binaries
    rm -f "$INSTALL_DIR/aigw" "$INSTALL_DIR/aigw-tray"
    echo "  Binaries removed"

    # Restore Chrome original binary
    CHROME_APP="/Applications/Google Chrome.app"
    CHROME_BIN="$CHROME_APP/Contents/MacOS/Google Chrome"
    CHROME_REAL="$CHROME_APP/Contents/MacOS/Google Chrome.real"
    if [ -f "$CHROME_REAL" ]; then
        sudo mv "$CHROME_REAL" "$CHROME_BIN"
        echo "  Chrome original binary restored"
    fi

    echo ""
    echo -e "${YELLOW}Note: Config and data preserved at:${NC}"
    echo "  $CONFIG_DIR"
    echo "  To fully remove: rm -rf \"$CONFIG_DIR\""
    echo ""
    echo -e "${GREEN}Uninstall complete.${NC}"
    exit 0
fi

# --- Install ---
echo "Installation paths:"
echo "  Binaries:  $INSTALL_DIR"
echo "  Config:    $CONFIG_DIR"
echo "  Logs:      $LOG_DIR"
echo ""

# Check binaries
AIGW_BIN=""
TRAY_BIN=""

# Look for binaries in script directory or current directory
for dir in "$SCRIPT_DIR" "$(pwd)" "$SCRIPT_DIR/.."; do
    if [ -f "$dir/aigw" ]; then
        AIGW_BIN="$dir/aigw"
    fi
    if [ -f "$dir/aigw-tray" ]; then
        TRAY_BIN="$dir/aigw-tray"
    fi
done

if [ -z "$AIGW_BIN" ]; then
    echo -e "${RED}Error: aigw binary not found.${NC}"
    echo "  Build first: go build -o aigw ./cmd/aigw"
    exit 1
fi

echo -e "${GREEN}Found aigw:${NC} $AIGW_BIN"
if [ -n "$TRAY_BIN" ]; then
    echo -e "${GREEN}Found aigw-tray:${NC} $TRAY_BIN"
fi

# Check port
if lsof -Pi :8787 -sTCP:LISTEN -t >/dev/null 2>&1; then
    echo ""
    echo -e "${YELLOW}Warning: Port 8787 is already in use.${NC}"
    echo "  Existing TrustGate instance or another service may be running."
    read -p "  Continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

# Create directories
echo ""
echo "Creating directories..."
mkdir -p "$CONFIG_DIR"
mkdir -p "$LOG_DIR"
mkdir -p "$LAUNCH_AGENTS"

# Copy binaries
echo "Installing binaries..."
cp "$AIGW_BIN" "$INSTALL_DIR/aigw"
chmod +x "$INSTALL_DIR/aigw"
if [ -n "$TRAY_BIN" ]; then
    cp "$TRAY_BIN" "$INSTALL_DIR/aigw-tray"
    chmod +x "$INSTALL_DIR/aigw-tray"
fi

# Create default config if not exists
if [ ! -f "$CONFIG_DIR/agent.yaml" ]; then
    echo "Creating default configuration..."
    cat > "$CONFIG_DIR/agent.yaml" << 'YAML'
version: "1"
mode: standalone

listen:
  host: 127.0.0.1
  port: 8787
  include_trustgate_body: true
  allow_debug: true

logging:
  level: info
  format: text

detectors:
  pii:
    enabled: true
  injection:
    enabled: true
    language: [en, ja]
  confidential:
    enabled: true
    keywords:
      critical: ["極秘", "社外秘", "CONFIDENTIAL", "TOP SECRET"]
      high: ["機密", "内部限定", "INTERNAL ONLY"]

backend:
  provider: mock
  mock:
    delay_ms: 50

policy:
  source: local
  file: policies.yaml

audit:
  mode: local
  path: audit.db
  retention_days: 90
YAML
fi

# Copy default policies if not exists
if [ ! -f "$CONFIG_DIR/policies.yaml" ]; then
    if [ -f "$SCRIPT_DIR/default-policies.yaml" ]; then
        cp "$SCRIPT_DIR/default-policies.yaml" "$CONFIG_DIR/policies.yaml"
    elif [ -f "$SCRIPT_DIR/../policies.yaml" ]; then
        cp "$SCRIPT_DIR/../policies.yaml" "$CONFIG_DIR/policies.yaml"
    fi
    echo "  Default policies installed"
fi

# Install launchd agent (TrustGate Agent service)
echo "Setting up launchd service..."
PLIST="$LAUNCH_AGENTS/com.trustgate.agent.plist"
sed -e "s|__CONFIG_PATH__|$CONFIG_DIR/agent.yaml|g" \
    -e "s|__LOG_DIR__|$LOG_DIR|g" \
    -e "s|__CONFIG_DIR__|$CONFIG_DIR|g" \
    "$SCRIPT_DIR/com.trustgate.agent.plist" > "$PLIST"
launchctl load "$PLIST" 2>/dev/null || true
echo "  Agent service registered (com.trustgate.agent)"

# Install tray auto-launch
if [ -n "$TRAY_BIN" ]; then
    echo "Setting up tray auto-launch..."
    cp "$SCRIPT_DIR/com.trustgate.tray.plist" "$LAUNCH_AGENTS/com.trustgate.tray.plist"
    launchctl load "$LAUNCH_AGENTS/com.trustgate.tray.plist" 2>/dev/null || true
    echo "  Tray auto-launch registered (com.trustgate.tray)"
fi

# Configure Chrome to suppress debugger banner
CHROME_APP="/Applications/Google Chrome.app"
CHROME_BIN="$CHROME_APP/Contents/MacOS/Google Chrome"
CHROME_REAL="$CHROME_APP/Contents/MacOS/Google Chrome.real"

if [ -d "$CHROME_APP" ]; then
    if [ -f "$CHROME_REAL" ]; then
        echo -e "${GREEN}Chrome debugger banner suppression already configured.${NC}"
    elif [ -f "$CHROME_BIN" ] && ! head -c 4 "$CHROME_BIN" | grep -q '#!'; then
        # Binary exists and is not already a wrapper script
        echo "Configuring Chrome to suppress debugger banner..."
        sudo mv "$CHROME_BIN" "$CHROME_REAL"
        sudo tee "$CHROME_BIN" > /dev/null << 'WRAPPER'
#!/bin/bash
exec "$(dirname "$0")/Google Chrome.real" --silent-debugger-extension-api "$@"
WRAPPER
        sudo chmod +x "$CHROME_BIN"
        echo -e "${GREEN}  Chrome debugger banner will be suppressed.${NC}"
    fi
else
    echo -e "${YELLOW}Chrome not found at $CHROME_APP — skipping banner config.${NC}"
fi

# Start service
echo ""
echo "Starting TrustGate Agent..."
launchctl start com.trustgate.agent 2>/dev/null || true
sleep 2

# Verify
if curl -s http://localhost:8787/v1/health | grep -q "healthy" 2>/dev/null; then
    echo -e "${GREEN}TrustGate is running!${NC}"
else
    echo -e "${YELLOW}Agent may still be starting. Check:${NC}"
    echo "  curl http://localhost:8787/v1/health"
fi

echo ""
echo -e "${GREEN}Installation complete!${NC}"
echo ""
echo "  Agent:    http://localhost:8787"
echo "  Config:   $CONFIG_DIR/agent.yaml"
echo "  Logs:     $LOG_DIR/trustgate.log"
echo "  Audit:    $CONFIG_DIR/audit.db"
echo ""
echo "Commands:"
echo "  aigw serve --mock-backend     Start manually"
echo "  aigw logs --last 10           View recent logs"
echo "  aigw doctor                   Diagnose environment"
echo "  aigw model download prompt-guard-2-86m   Download LLM model"
echo ""
echo "  Uninstall: $0 --uninstall"
