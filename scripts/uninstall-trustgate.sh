#!/bin/bash
# TrustGate macOS Uninstaller
# Usage: bash /Library/Application\ Support/TrustGate/uninstall-trustgate.sh

set -e

echo "=== TrustGate Uninstaller ==="
echo ""

# Confirm
read -p "This will completely remove TrustGate from your Mac. Continue? [y/N] " confirm
if [[ "$confirm" != [yY] ]]; then
    echo "Cancelled."
    exit 0
fi

echo ""
echo "Stopping services..."

# Stop LaunchAgents (user level)
launchctl unload /Library/LaunchAgents/com.trustgate.agent.plist 2>/dev/null || true
launchctl unload /Library/LaunchAgents/com.trustgate.tray.plist 2>/dev/null || true

# Stop LaunchDaemons (legacy, if present)
sudo launchctl unload /Library/LaunchDaemons/com.trustgate.agent.plist 2>/dev/null || true

# Kill processes
killall aigw 2>/dev/null || true
killall aigw-tray 2>/dev/null || true

echo "Removing files..."

# Remove binaries
sudo rm -f /usr/local/bin/aigw

# Remove app bundle
sudo rm -rf /Applications/TrustGate.app

# Remove LaunchAgents plists
sudo rm -f /Library/LaunchAgents/com.trustgate.agent.plist
sudo rm -f /Library/LaunchAgents/com.trustgate.tray.plist

# Remove LaunchDaemons plist (legacy)
sudo rm -f /Library/LaunchDaemons/com.trustgate.agent.plist

# Remove config (ask first)
if [ -d "/Library/Application Support/TrustGate" ]; then
    read -p "Remove configuration files? [y/N] " remove_config
    if [[ "$remove_config" == [yY] ]]; then
        sudo rm -rf "/Library/Application Support/TrustGate"
    else
        echo "  Configuration files kept at: /Library/Application Support/TrustGate/"
    fi
fi

# Remove logs
rm -rf ~/Library/Logs/TrustGate

# Forget pkg receipt
sudo pkgutil --forget com.trustgate.agent 2>/dev/null || true

echo ""
echo "=== TrustGate has been uninstalled ==="
