#!/usr/bin/env bash
# wg-sockd install script
# Usage: curl -sSL https://raw.githubusercontent.com/aleks-dolotin/wg-sockd/main/deploy/install.sh | sudo bash
# Modes:
#   Default:      Installs full binary (with UI) + CTL
#   --agent-only: Installs lean binary (no UI) + CTL — for K8s sidecar / headless
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
BOLD='\033[1m'
NC='\033[0m' # No Color

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
fatal() { error "$@"; exit 1; }

# --- Task 5.1: Parse flags ---
AGENT_ONLY=false
while [[ $# -gt 0 ]]; do
    case "$1" in
        --agent-only)
            AGENT_ONLY=true
            shift
            ;;
        *)
            fatal "Unknown option: $1 (supported: --agent-only)"
            ;;
    esac
done

if [ "$AGENT_ONLY" = true ]; then
    info "Mode: agent-only (lean binary, no UI)"
else
    info "Mode: default (full binary with UI)"
fi

# --- Task 5.2: Read environment variables with validation ---
# Bool validation aligned with Go's strconv.ParseBool: true/false/1/0/t/f (case-insensitive) (F14).
validate_bool() {
    local name="$1" value="$2"
    case "${value,,}" in
        true|false|1|0|t|f) return 0 ;;
        *) fatal "Invalid boolean value for ${name}: '${value}' (accepted: true/false/1/0/t/f)" ;;
    esac
}

# Normalise bool to "true" or "false" for internal use.
normalise_bool() {
    case "${1,,}" in
        true|1|t) echo "true" ;;
        false|0|f) echo "false" ;;
    esac
}

if [ -n "${WG_SOCKD_SERVE_UI:-}" ]; then
    validate_bool "WG_SOCKD_SERVE_UI" "$WG_SOCKD_SERVE_UI"
    ENV_SERVE_UI="$(normalise_bool "$WG_SOCKD_SERVE_UI")"
    info "WG_SOCKD_SERVE_UI=${ENV_SERVE_UI} (from environment)"
fi

if [ -n "${WG_SOCKD_UI_LISTEN:-}" ]; then
    # Basic format check — must contain a colon (host:port).
    if ! echo "$WG_SOCKD_UI_LISTEN" | grep -q ':'; then
        fatal "Invalid WG_SOCKD_UI_LISTEN: '${WG_SOCKD_UI_LISTEN}' (expected host:port format)"
    fi
    ENV_UI_LISTEN="$WG_SOCKD_UI_LISTEN"
    info "WG_SOCKD_UI_LISTEN=${ENV_UI_LISTEN} (from environment)"
fi

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

# --- Task 5.4: WireGuard check (6 distros + generic fallback) (F9) ---
check_wireguard() {
    if command -v wg >/dev/null 2>&1; then
        info "WireGuard tools found: $(command -v wg)"
        return 0
    fi

    warn "WireGuard tools (wg) not found"

    # Detect distro and suggest install command.
    local distro=""
    if [ -f /etc/os-release ]; then
        distro="$(. /etc/os-release && echo "${ID:-}")"
    fi

    case "$distro" in
        ubuntu|debian)
            warn "  Install with: apt install wireguard-tools" ;;
        fedora)
            warn "  Install with: dnf install wireguard-tools" ;;
        centos|rhel|rocky|almalinux)
            warn "  Install with: yum install wireguard-tools" ;;
        arch|manjaro)
            warn "  Install with: pacman -S wireguard-tools" ;;
        opensuse*|sles)
            warn "  Install with: zypper install wireguard-tools" ;;
        alpine)
            warn "  Install with: apk add wireguard-tools" ;;
        *)
            warn "  Install wireguard-tools for your distribution" ;;
    esac

    return 1
}

WG_MISSING=false
if ! check_wireguard; then
    WG_MISSING=true
    warn "Continuing without WireGuard — agent will start in degraded mode"
fi

# --- Task 5.3: Interactive/non-interactive UI binding detection ---
determine_ui_listen() {
    # If agent-only, no UI needed.
    if [ "$AGENT_ONLY" = true ]; then
        UI_LISTEN=""
        SERVE_UI="false"
        return
    fi

    SERVE_UI="true"

    # Environment variable override takes precedence.
    if [ -n "${ENV_UI_LISTEN:-}" ]; then
        UI_LISTEN="$ENV_UI_LISTEN"
        return
    fi
    if [ -n "${ENV_SERVE_UI:-}" ]; then
        SERVE_UI="$ENV_SERVE_UI"
    fi

    # Default binding.
    UI_LISTEN="127.0.0.1:8080"

    # Check if running interactively (terminal attached).
    if [ -t 0 ]; then
        # Interactive mode — prompt for binding.
        echo ""
        echo -e "  ${BOLD}UI Binding Configuration${NC}"
        echo "  The web UI will be available at http://<address>:8080"
        echo ""
        echo "  1) 127.0.0.1:8080  — localhost only (secure, requires SSH tunnel)"
        echo "  2) 0.0.0.0:8080    — all interfaces (⚠️  accessible from network)"
        echo "  3) Custom address"
        echo ""
        read -rp "  Select [1]: " choice
        case "${choice:-1}" in
            1|"") UI_LISTEN="127.0.0.1:8080" ;;
            2)
                UI_LISTEN="0.0.0.0:8080"
                warn "Binding to 0.0.0.0 — UI will be accessible from all network interfaces"
                ;;
            3)
                read -rp "  Enter address (host:port): " custom
                UI_LISTEN="${custom:-127.0.0.1:8080}"
                ;;
            *)
                UI_LISTEN="127.0.0.1:8080"
                info "Invalid choice, using default: 127.0.0.1:8080"
                ;;
        esac
    else
        # Non-interactive (piped) — use 0.0.0.0 with bold warning.
        UI_LISTEN="0.0.0.0:8080"
        echo ""
        echo -e "  ${BOLD}${YELLOW}⚠️  Non-interactive install: UI bound to 0.0.0.0:8080${NC}"
        echo -e "  ${BOLD}${YELLOW}    The UI will be accessible from ALL network interfaces.${NC}"
        echo -e "  ${BOLD}${YELLOW}    Edit ${CONFIG_DIR}/config.yaml to change ui_listen after install.${NC}"
        echo ""
    fi
}

determine_ui_listen

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

# --- Task 5.5: Download or copy binary (two modes) ---
install_binary() {
    local name="$1"
    local suffix="${2:-}" # e.g. "-full" for full binary
    local local_bin="./bin/${name}${suffix}"

    # Check if binary exists locally (e.g., from make build)
    if [ -f "$local_bin" ]; then
        info "Installing local binary: $local_bin"
        install -m 0755 "$local_bin" "${INSTALL_DIR}/${name}"
        return 0
    fi

    # Also check without suffix locally.
    if [ -n "$suffix" ] && [ -f "./bin/${name}" ]; then
        info "Installing local binary: ./bin/${name}"
        install -m 0755 "./bin/${name}" "${INSTALL_DIR}/${name}"
        return 0
    fi

    # Try GitHub releases
    local latest_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    local download_url=""
    local asset_name="${name}${suffix}-linux-${ARCH}"

    if command -v curl >/dev/null 2>&1; then
        local release_info
        release_info="$(curl -sSL "$latest_url" 2>/dev/null || true)"
        if [ -n "$release_info" ] && echo "$release_info" | grep -q "browser_download_url"; then
            download_url="$(echo "$release_info" | grep "browser_download_url.*${asset_name}" | head -1 | cut -d'"' -f4)"
        fi
    fi

    if [ -n "${download_url:-}" ]; then
        info "Downloading ${name} from: $download_url"
        local tmp_bin
        tmp_bin="$(mktemp)"
        TMP_FILES+=("$tmp_bin")
        curl -sSL --fail -o "$tmp_bin" "$download_url"

        # Task 5.6: SHA256 checksum verification.
        local checksum_url="${download_url}.sha256"
        if command -v sha256sum >/dev/null 2>&1; then
            local tmp_checksum
            tmp_checksum="$(mktemp)"
            TMP_FILES+=("$tmp_checksum")
            if curl -sSL --fail -o "$tmp_checksum" "$checksum_url" 2>/dev/null; then
                local expected actual
                expected="$(awk '{print $1}' "$tmp_checksum")"
                actual="$(sha256sum "$tmp_bin" | awk '{print $1}')"
                if [ "$expected" != "$actual" ]; then
                    fatal "SHA256 mismatch for ${name}: expected=${expected}, got=${actual}"
                fi
                info "SHA256 checksum verified for ${name}"
                rm -f "$tmp_checksum"
            else
                info "No SHA256 checksum published for this release — skipping verification"
            fi
        else
            info "sha256sum not found — skipping checksum verification"
        fi

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
# Default mode: try "-full" suffix first (embedded UI). Agent-only: lean binary.
if [ "$AGENT_ONLY" = true ]; then
    if ! install_binary "$BINARY_NAME" ""; then
        fatal "Cannot find ${BINARY_NAME} binary. Build with 'make build' first, or ensure GitHub releases are available."
    fi
else
    if ! install_binary "$BINARY_NAME" "-full"; then
        if ! install_binary "$BINARY_NAME" ""; then
            fatal "Cannot find ${BINARY_NAME} binary. Build with 'make build-full' or 'make build' first."
        fi
    fi
fi

# Install CTL binary (optional — warn if missing).
if ! install_binary "$CTL_BINARY_NAME" ""; then
    warn "${CTL_BINARY_NAME} not found — skipping (build with 'make build-ctl')"
fi

# --- Task 5.7: Binary version verification ---
if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
    INSTALLED_VERSION="$("${INSTALL_DIR}/${BINARY_NAME}" --version 2>&1 || true)"
    if [ -n "$INSTALLED_VERSION" ]; then
        info "Installed: ${INSTALLED_VERSION}"
        # AC-28: Full mode + --version lacks +ui → warning.
        if [ "$AGENT_ONLY" = false ] && ! echo "$INSTALLED_VERSION" | grep -q '+ui'; then
            warn "Full mode selected but binary does not contain embedded UI (+ui tag missing)"
            warn "  You may need to build with: make build-full"
        fi
    fi
fi

# --- Create directories ---
info "Creating directories..."
mkdir -p "$CONFIG_DIR"
mkdir -p "$DATA_DIR"
mkdir -p "$RUN_DIR"

# Set ownership
chown "${USER_NAME}:${GROUP_NAME}" "$DATA_DIR"
chmod 0750 "$DATA_DIR"
chown "${USER_NAME}:${GROUP_NAME}" "$RUN_DIR"
chmod 0750 "$RUN_DIR"

# --- Task 5.8 + 5.9: Config — upgrade path or fresh install ---
if [ -f "${CONFIG_DIR}/config.yaml" ]; then
    # Upgrade path: check if serve_ui already present.
    if grep -q "^serve_ui:" "${CONFIG_DIR}/config.yaml"; then
        info "Config already has serve_ui — not modifying"
    else
        info "Upgrading config — appending serve_ui settings..."
        echo "" >> "${CONFIG_DIR}/config.yaml"
        echo "# Added by installer upgrade — UI settings" >> "${CONFIG_DIR}/config.yaml"
        if [ "$AGENT_ONLY" = true ]; then
            # F16: agent-only → only append serve_ui=false, no ui_listen.
            echo "serve_ui: false" >> "${CONFIG_DIR}/config.yaml"
        else
            echo "serve_ui: ${SERVE_UI}" >> "${CONFIG_DIR}/config.yaml"
            echo "ui_listen: \"${UI_LISTEN}\"" >> "${CONFIG_DIR}/config.yaml"
        fi
        info "Config updated with serve_ui settings"
    fi
else
    # Fresh install — generate default config.
    info "Installing default config..."
    cat > "${CONFIG_DIR}/config.yaml" <<CFGEOF
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

# UI settings
serve_ui: ${SERVE_UI}
CFGEOF
    if [ "$AGENT_ONLY" = false ] && [ -n "${UI_LISTEN:-}" ]; then
        echo "ui_listen: \"${UI_LISTEN}\"" >> "${CONFIG_DIR}/config.yaml"
    fi

    chown root:"${GROUP_NAME}" "${CONFIG_DIR}/config.yaml"
    chmod 0640 "${CONFIG_DIR}/config.yaml"
    info "Default config installed to ${CONFIG_DIR}/config.yaml"
fi

# Task 5.10: Security warning for 0.0.0.0
if grep -q 'ui_listen:.*0\.0\.0\.0' "${CONFIG_DIR}/config.yaml" 2>/dev/null; then
    echo ""
    echo -e "  ${BOLD}${YELLOW}⚠️  SECURITY WARNING${NC}"
    echo -e "  ${YELLOW}  ui_listen is bound to 0.0.0.0 — the UI is accessible from all interfaces.${NC}"
    echo -e "  ${YELLOW}  Consider restricting to 127.0.0.1 or a specific IP, or adding a reverse proxy with auth.${NC}"
    echo ""
fi

# Run --dry-run to validate config (if available).
if [ -f "${INSTALL_DIR}/${BINARY_NAME}" ]; then
    info "Validating config with --dry-run..."
    if ! "${INSTALL_DIR}/${BINARY_NAME}" --config "${CONFIG_DIR}/config.yaml" --dry-run >/dev/null 2>&1; then
        warn "--dry-run failed — check ${CONFIG_DIR}/config.yaml for errors"
    else
        info "Config validation passed"
    fi
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

# --- Task 5.11: kubectl label block removed (AC-31) ---

# --- Task 5.12: Restructured final summary ---
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
echo "    sudo curl --unix-socket ${RUN_DIR}/${BINARY_NAME}.sock http://localhost/api/health"
echo ""

# Show UI URL only in default mode (AC-22: no URL for agent-only).
if [ "$AGENT_ONLY" = false ] && [ "${SERVE_UI}" = "true" ] && [ -n "${UI_LISTEN:-}" ]; then
    echo -e "  ${GREEN}UI available at: http://${UI_LISTEN}${NC}"
    echo ""
fi

# Show WireGuard reminder if missing.
if [ "$WG_MISSING" = true ]; then
    echo -e "  ${YELLOW}⚠️  WireGuard tools not found — install wireguard-tools and restart the agent.${NC}"
    echo ""
fi

echo "  Verify installation:"
echo "    ${BINARY_NAME} --version"
echo "    ${BINARY_NAME} --config ${CONFIG_DIR}/config.yaml --dry-run"
if [ -f "${INSTALL_DIR}/${CTL_BINARY_NAME}" ]; then
    echo "    ${CTL_BINARY_NAME} --version"
fi
echo ""
echo "  Socket access requires root or membership in the wg-sockd group."
echo "  To allow your user to access the socket without sudo:"
echo "    sudo usermod -aG wg-sockd \$USER"
echo "  Then re-login for the group change to take effect."
echo ""

