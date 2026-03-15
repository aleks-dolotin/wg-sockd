#!/usr/bin/env bash
# wg-sockd uninstall script
# Usage: sudo bash deploy/uninstall.sh [--purge]
set -euo pipefail

BINARY_NAME="wg-sockd"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/wg-sockd"
DATA_DIR="/var/lib/wg-sockd"
RUN_DIR="/var/run/wg-sockd"
SERVICE_NAME="wg-sockd"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
GROUP_NAME="wg-sockd"
USER_NAME="wg-sockd"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
fatal() { echo -e "${RED}[ERROR]${NC} $*" >&2; exit 1; }

PURGE=false
if [ "${1:-}" = "--purge" ]; then
    PURGE=true
fi

if [ "$(id -u)" -ne 0 ]; then
    fatal "This script must be run as root (use: sudo bash uninstall.sh)"
fi

# --- Stop and disable service ---
info "Stopping service..."
systemctl stop "$SERVICE_NAME" 2>/dev/null || true
systemctl disable "$SERVICE_NAME" 2>/dev/null || true

# --- Remove service file ---
if [ -f "$SERVICE_FILE" ]; then
    rm -f "$SERVICE_FILE"
    systemctl daemon-reload
    info "Removed systemd unit"
else
    info "Service file not found — skipping"
fi

# --- Remove binary ---
if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
    rm -f "${INSTALL_DIR}/${BINARY_NAME}"
    info "Removed binary"
else
    info "Binary not found — skipping"
fi

# --- Remove socket ---
rm -f "${RUN_DIR}/${BINARY_NAME}.sock" 2>/dev/null || true
rmdir "$RUN_DIR" 2>/dev/null || true

# --- Optionally purge data ---
if [ "$PURGE" = true ]; then
    warn "Purging all data and configuration..."
    rm -rf "$CONFIG_DIR"
    rm -rf "$DATA_DIR"
    rm -rf "$RUN_DIR"

    # Remove user and group
    if id "$USER_NAME" >/dev/null 2>&1; then
        userdel "$USER_NAME" 2>/dev/null || true
        info "Removed user $USER_NAME"
    fi
    if getent group "$GROUP_NAME" >/dev/null 2>&1; then
        groupdel "$GROUP_NAME" 2>/dev/null || true
        info "Removed group $GROUP_NAME"
    fi

    # Remove K8s node label
    if command -v kubectl >/dev/null 2>&1; then
        kubectl label node "$(hostname)" wg-sockd- 2>/dev/null || true
        info "Removed Kubernetes node label"
    fi

    info "All data purged"
else
    info "Config preserved at $CONFIG_DIR"
    info "Data preserved at $DATA_DIR"
    echo ""
    echo "  To remove all data: sudo bash $0 --purge"
fi

echo ""
info "wg-sockd uninstalled"

