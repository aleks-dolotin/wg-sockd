#!/usr/bin/env bash
# wg-sockd install script
# Usage: curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash
set -euo pipefail

# --- Configuration ---
GITHUB_REPO="aleks-dolotin/wg-sockd"
BINARY_NAME="wg-sockd"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/wg-sockd"
DATA_DIR="/var/lib/wg-sockd"
RUN_DIR="/var/run/wg-sockd"
SERVICE_NAME="wg-sockd"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
GID=5000
GROUP_NAME="wg-sockd"
USER_NAME="wg-sockd"

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
fatal() { error "$@"; exit 1; }

# --- Pre-flight checks ---
if [ "$(id -u)" -ne 0 ]; then
    fatal "This script must be run as root (use: sudo bash install.sh)"
fi

if [ "$(uname -s)" != "Linux" ]; then
    fatal "wg-sockd requires Linux (detected: $(uname -s))"
fi

# --- Detect architecture ---
detect_arch() {
    local arch
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)  echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        *)             fatal "Unsupported architecture: $arch" ;;
    esac
}

ARCH="$(detect_arch)"
info "Detected architecture: $ARCH"

# --- Create group and user (idempotent) ---
info "Creating group $GROUP_NAME (GID $GID)..."
if getent group "$GROUP_NAME" >/dev/null 2>&1; then
    info "Group $GROUP_NAME already exists"
else
    groupadd -g "$GID" "$GROUP_NAME"
    info "Created group $GROUP_NAME with GID $GID"
fi

info "Creating user $USER_NAME..."
if id "$USER_NAME" >/dev/null 2>&1; then
    info "User $USER_NAME already exists"
else
    useradd -r -g "$GROUP_NAME" -s /usr/sbin/nologin -d /nonexistent "$USER_NAME"
    info "Created system user $USER_NAME"
fi

# Ensure user is in the group
usermod -aG "$GROUP_NAME" "$USER_NAME" 2>/dev/null || true

# --- Download or copy binary ---
install_binary() {
    # Check if binary exists locally (e.g., from make build)
    local local_bin="./bin/${BINARY_NAME}"
    if [ -f "$local_bin" ]; then
        info "Installing local binary: $local_bin"
        install -m 0755 "$local_bin" "${INSTALL_DIR}/${BINARY_NAME}"
        return
    fi

    # Try GitHub releases
    local latest_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    local download_url

    if command -v curl >/dev/null 2>&1; then
        local release_info
        release_info="$(curl -sSL "$latest_url" 2>/dev/null || true)"
        if [ -n "$release_info" ] && echo "$release_info" | grep -q "browser_download_url"; then
            download_url="$(echo "$release_info" | grep "browser_download_url.*${BINARY_NAME}-linux-${ARCH}" | head -1 | cut -d'"' -f4)"
        fi
    fi

    if [ -n "${download_url:-}" ]; then
        info "Downloading from: $download_url"
        TMP_BIN="$(mktemp)"
        curl -sSL -o "$TMP_BIN" "$download_url"
        install -m 0755 "$TMP_BIN" "${INSTALL_DIR}/${BINARY_NAME}"
        rm -f "$TMP_BIN"
    else
        # Fallback: check if binary is in current directory or PATH
        if [ -f "./${BINARY_NAME}" ]; then
            info "Installing binary from current directory"
            install -m 0755 "./${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
        elif command -v "$BINARY_NAME" >/dev/null 2>&1; then
            warn "No release found, using existing binary in PATH"
        else
            fatal "Cannot find ${BINARY_NAME} binary. Build with 'make build' first, or ensure GitHub releases are available."
        fi
    fi

    info "Binary installed to ${INSTALL_DIR}/${BINARY_NAME}"
}

install_binary

# --- Create directories ---
info "Creating directories..."
mkdir -p "$CONFIG_DIR"
mkdir -p "$DATA_DIR"
mkdir -p "$RUN_DIR"

# Set ownership
chown "${USER_NAME}:${GROUP_NAME}" "$DATA_DIR"
chown "${USER_NAME}:${GROUP_NAME}" "$RUN_DIR"
chmod 0750 "$DATA_DIR"
chmod 0750 "$RUN_DIR"

# --- Install default config (if not exists) ---
if [ ! -f "${CONFIG_DIR}/config.yaml" ]; then
    info "Installing default config..."
    cat > "${CONFIG_DIR}/config.yaml" <<'EOF'
# wg-sockd agent configuration
interface: wg0
socket_path: /var/run/wg-sockd/wg-sockd.sock
db_path: /var/lib/wg-sockd/wg-sockd.db
conf_path: /etc/wireguard/wg0.conf
auto_approve_unknown: false
peer_limit: 250
reconcile_interval: 30s
# external_endpoint: "vpn.example.com:51820"  # used in client .conf and QR codes
EOF
    chown root:"${GROUP_NAME}" "${CONFIG_DIR}/config.yaml"
    chmod 0640 "${CONFIG_DIR}/config.yaml"
    info "Default config installed to ${CONFIG_DIR}/config.yaml"
else
    info "Config already exists at ${CONFIG_DIR}/config.yaml — skipping"
fi

# --- Install systemd unit ---
info "Installing systemd service..."
cat > "$SERVICE_FILE" <<'EOF'
[Unit]
Description=wg-sockd WireGuard Management Agent
After=network.target
Documentation=https://github.com/aleks-dolotin/wg-sockd

[Service]
Type=notify
User=wg-sockd
Group=wg-sockd
AmbientCapabilities=CAP_NET_ADMIN
ExecStart=/usr/local/bin/wg-sockd --config /etc/wg-sockd/config.yaml
Restart=on-failure
RestartSec=2
WatchdogSec=30
RuntimeDirectory=wg-sockd
StateDirectory=wg-sockd
ConfigurationDirectory=wg-sockd
ProtectSystem=strict
ProtectHome=yes
ReadWritePaths=/etc/wireguard /var/lib/wg-sockd /var/run/wg-sockd
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
info "Service enabled: $SERVICE_NAME"

# --- Start or restart service ---
if systemctl is-active --quiet "$SERVICE_NAME"; then
    info "Service already running — restarting..."
    systemctl restart "$SERVICE_NAME"
else
    info "Starting service..."
    systemctl start "$SERVICE_NAME"
fi

# --- Label Kubernetes node (if kubectl available) ---
if command -v kubectl >/dev/null 2>&1; then
    local_node="$(hostname)"
    info "kubectl found — labeling node '$local_node' with wg-sockd=active..."
    if kubectl label node "$local_node" wg-sockd=active --overwrite 2>/dev/null; then
        info "Node labeled: wg-sockd=active"
    else
        warn "Failed to label node (not in a K8s cluster or insufficient permissions)"
    fi
fi

# --- Done ---
echo ""
info "============================================"
info "  wg-sockd installed successfully!"
info "============================================"
echo ""
echo "  Binary:  ${INSTALL_DIR}/${BINARY_NAME}"
echo "  Config:  ${CONFIG_DIR}/config.yaml"
echo "  Data:    ${DATA_DIR}/"
echo "  Socket:  ${RUN_DIR}/${BINARY_NAME}.sock"
echo "  Service: systemctl status ${SERVICE_NAME}"
echo ""
echo "  Quick test:"
echo "    curl --unix-socket ${RUN_DIR}/${BINARY_NAME}.sock http://localhost/api/health"
echo ""

# Output Helm values for K8s users
echo "  # For Kubernetes users — add to your Helm values.yaml:"
echo "  # nodeSelector:"
echo "  #   wg-sockd: active"
echo "  # securityContext:"
echo "  #   runAsGroup: ${GID}"
echo "  # podSecurityContext:"
echo "  #   supplementalGroups:"
echo "  #     - ${GID}"
echo ""

