#!/usr/bin/env bash
# wg-sockd install script
# Usage: curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash
set -euo pipefail

# --- Configuration ---
GITHUB_REPO="aleks-dolotin/wg-sockd"
BINARY_NAME="wg-sockd"
CTL_BINARY_NAME="wg-sockd-ctl"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/wg-sockd"
DATA_DIR="/var/lib/wg-sockd"
RUN_DIR="/run/wg-sockd"
SERVICE_NAME="wg-sockd"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"
GID=5000
GROUP_NAME="wg-sockd"
USER_NAME="wg-sockd"

# Track temp files for cleanup on failure (set -e).
TMP_FILES=()
cleanup() { rm -f "${TMP_FILES[@]}" 2>/dev/null || true; }
trap cleanup EXIT

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
    local name="$1"
    local local_bin="./bin/${name}"

    # Check if binary exists locally (e.g., from make build)
    if [ -f "$local_bin" ]; then
        info "Installing local binary: $local_bin"
        install -m 0755 "$local_bin" "${INSTALL_DIR}/${name}"
        return 0
    fi

    # Try GitHub releases
    local latest_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    local download_url=""

    if command -v curl >/dev/null 2>&1; then
        local release_info
        release_info="$(curl -sSL "$latest_url" 2>/dev/null || true)"
        if [ -n "$release_info" ] && echo "$release_info" | grep -q "browser_download_url"; then
            download_url="$(echo "$release_info" | grep "browser_download_url.*${name}-linux-${ARCH}" | head -1 | cut -d'"' -f4)"
        fi
    fi

    if [ -n "${download_url:-}" ]; then
        info "Downloading ${name} from: $download_url"
        local tmp_bin
        tmp_bin="$(mktemp)"
        TMP_FILES+=("$tmp_bin")
        curl -sSL --fail -o "$tmp_bin" "$download_url"

        # Verify downloaded binary is not empty / truncated.
        local size
        size="$(stat -c%s "$tmp_bin" 2>/dev/null || stat -f%z "$tmp_bin" 2>/dev/null || echo 0)"
        if [ "$size" -lt 1024 ]; then
            fatal "Downloaded ${name} is suspiciously small (${size} bytes) — aborting"
        fi

        install -m 0755 "$tmp_bin" "${INSTALL_DIR}/${name}"
        rm -f "$tmp_bin"
        info "${name} installed to ${INSTALL_DIR}/${name}"
        return 0
    fi

    # Fallback: check if binary is in current directory or PATH
    if [ -f "./${name}" ]; then
        info "Installing ${name} from current directory"
        install -m 0755 "./${name}" "${INSTALL_DIR}/${name}"
        return 0
    elif command -v "$name" >/dev/null 2>&1; then
        warn "No release found for ${name}, using existing binary in PATH"
        return 0
    fi

    # Return failure — let caller decide if fatal.
    return 1
}

# Install main binary (required).
if ! install_binary "$BINARY_NAME"; then
    fatal "Cannot find ${BINARY_NAME} binary. Build with 'make build' first, or ensure GitHub releases are available."
fi

# Install CTL binary (optional — warn if missing).
if ! install_binary "$CTL_BINARY_NAME"; then
    warn "${CTL_BINARY_NAME} not found — skipping (build with 'make build-ctl')"
fi

# --- Create directories ---
# Note: /run/wg-sockd is managed by systemd RuntimeDirectory=wg-sockd —
# we only need to pre-create CONFIG_DIR and DATA_DIR.
info "Creating directories..."
mkdir -p "$CONFIG_DIR"
mkdir -p "$DATA_DIR"

# Set ownership
chown "${USER_NAME}:${GROUP_NAME}" "$DATA_DIR"
chmod 0750 "$DATA_DIR"

# --- Install default config (if not exists) ---
if [ ! -f "${CONFIG_DIR}/config.yaml" ]; then
    info "Installing default config..."
    cat > "${CONFIG_DIR}/config.yaml" <<'EOF'
# wg-sockd agent configuration
interface: wg0
socket_path: /run/wg-sockd/wg-sockd.sock
db_path: /var/lib/wg-sockd/wg-sockd.db
conf_path: /etc/wireguard/wg0.conf
auto_approve_unknown: false
peer_limit: 250
reconcile_interval: 30s
rate_limit: 10  # max requests/second per connection (0 = disabled)
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
ReadWritePaths=/etc/wireguard /var/lib/wg-sockd /run/wg-sockd
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
if [ -f "${INSTALL_DIR}/${CTL_BINARY_NAME}" ]; then
    echo "  CTL:     ${INSTALL_DIR}/${CTL_BINARY_NAME}"
fi
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

