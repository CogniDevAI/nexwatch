#!/bin/sh
# NexWatch Agent Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/install-agent.sh | bash -s -- --hub ws://hub:8090/ws/agent --token TOKEN
#
# Environment variables (alternative to flags):
#   HUB_URL   - Hub WebSocket URL (e.g. ws://hub:8090/ws/agent)
#   TOKEN     - Agent authentication token
#   VERSION   - Agent version to install (default: latest)

set -eu

# --- Defaults ---
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="nexwatch-agent"
CONFIG_DIR="/etc/nexwatch"
SERVICE_NAME="nexwatch-agent"
REPO="CogniDevAI/nexwatch"
GITHUB_BASE="https://github.com/${REPO}"

# Read from environment or leave empty for flag parsing.
HUB_URL="${HUB_URL:-}"
TOKEN="${TOKEN:-}"
VERSION="${VERSION:-latest}"
INTERVAL="${INTERVAL:-10}"

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

info()    { printf "${CYAN}[INFO]${NC} %s\n" "$1"; }
success() { printf "${GREEN}[OK]${NC} %s\n" "$1"; }
warn()    { printf "${YELLOW}[WARN]${NC} %s\n" "$1"; }
error()   { printf "${RED}[ERROR]${NC} %s\n" "$1"; exit 1; }

# --- Parse arguments ---
while [ $# -gt 0 ]; do
    case "$1" in
        --hub)
            HUB_URL="$2"
            shift 2
            ;;
        --token)
            TOKEN="$2"
            shift 2
            ;;
        --version)
            VERSION="$2"
            shift 2
            ;;
        --interval)
            INTERVAL="$2"
            shift 2
            ;;
        --help|-h)
            echo "NexWatch Agent Installer"
            echo ""
            echo "Usage:"
            echo "  install-agent.sh [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --hub URL        Hub WebSocket URL (required)"
            echo "  --token TOKEN    Agent authentication token (required)"
            echo "  --version VER    Agent version (default: latest)"
            echo "  --interval SECS  Collection interval in seconds (default: 10)"
            echo "  --help           Show this help"
            echo ""
            echo "Environment variables:"
            echo "  HUB_URL, TOKEN, VERSION, INTERVAL"
            exit 0
            ;;
        *)
            warn "Unknown option: $1"
            shift
            ;;
    esac
done

# --- Validate required params ---
if [ -z "$HUB_URL" ]; then
    error "HUB_URL is required. Use --hub or set HUB_URL environment variable."
fi

if [ -z "$TOKEN" ]; then
    error "TOKEN is required. Use --token or set TOKEN environment variable."
fi

# --- Detect OS and architecture ---
detect_platform() {
    OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
    ARCH="$(uname -m)"

    case "$OS" in
        linux)
            ;;
        *)
            error "Unsupported OS: $OS. NexWatch agent only supports Linux."
            ;;
    esac

    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            error "Unsupported architecture: $ARCH. Supported: amd64, arm64."
            ;;
    esac

    info "Detected platform: ${OS}/${ARCH}"
}

# --- Check for root ---
check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        error "This script must be run as root (or with sudo)."
    fi
}

# --- Resolve latest version ---
resolve_version() {
    if [ "$VERSION" = "latest" ]; then
        info "Resolving latest version..."
        VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | \
            grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
        if [ -z "$VERSION" ]; then
            error "Failed to resolve latest version from GitHub."
        fi
        info "Latest version: ${VERSION}"
    fi
}

# --- Download binary ---
download_binary() {
    DOWNLOAD_URL="${GITHUB_BASE}/releases/download/${VERSION}/${BINARY_NAME}_${VERSION#v}_${OS}_${ARCH}.tar.gz"
    TMP_DIR=$(mktemp -d)
    TMP_FILE="${TMP_DIR}/${BINARY_NAME}.tar.gz"

    info "Downloading ${BINARY_NAME} ${VERSION} from ${DOWNLOAD_URL}..."

    if ! curl -fsSL -o "$TMP_FILE" "$DOWNLOAD_URL"; then
        rm -rf "$TMP_DIR"
        error "Download failed. Check that version ${VERSION} exists at ${DOWNLOAD_URL}"
    fi

    info "Extracting..."
    tar -xzf "$TMP_FILE" -C "$TMP_DIR"

    # Find the binary in the extracted contents.
    EXTRACTED_BINARY=""
    for f in "${TMP_DIR}/${BINARY_NAME}" "${TMP_DIR}/agent" "${TMP_DIR}/nexwatch-agent"; do
        if [ -f "$f" ]; then
            EXTRACTED_BINARY="$f"
            break
        fi
    done

    if [ -z "$EXTRACTED_BINARY" ]; then
        rm -rf "$TMP_DIR"
        error "Binary not found in archive."
    fi

    # Install to target directory.
    install -m 755 "$EXTRACTED_BINARY" "${INSTALL_DIR}/${BINARY_NAME}"
    rm -rf "$TMP_DIR"

    success "Binary installed to ${INSTALL_DIR}/${BINARY_NAME}"
}

# --- Create config ---
create_config() {
    mkdir -p "$CONFIG_DIR"

    cat > "${CONFIG_DIR}/agent.yaml" <<YAML
# NexWatch Agent Configuration
# Generated by install-agent.sh

hub_url: "${HUB_URL}"
token: "${TOKEN}"
interval: ${INTERVAL}s
docker_socket: /var/run/docker.sock
collectors_enabled:
  - cpu
  - memory
  - disk
  - network
  - sysinfo
  - docker
  - ports
  - processes
  - hardening
  - vulnerabilities
YAML

    chmod 600 "${CONFIG_DIR}/agent.yaml"
    success "Config written to ${CONFIG_DIR}/agent.yaml"
}

# --- Create systemd service ---
create_service() {
    cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=NexWatch Monitoring Agent
Documentation=https://github.com/${REPO}
After=network-online.target docker.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/${BINARY_NAME} --config ${CONFIG_DIR}/agent.yaml
Restart=always
RestartSec=5
LimitNOFILE=65536

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${CONFIG_DIR}
PrivateTmp=true

# Allow access to Docker socket
SupplementaryGroups=docker

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

[Install]
WantedBy=multi-user.target
UNIT

    success "Systemd service created at /etc/systemd/system/${SERVICE_NAME}.service"
}

# --- Enable and start service ---
enable_and_start() {
    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME"
    systemctl start "$SERVICE_NAME"

    # Brief pause then check status.
    sleep 2
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        success "Service ${SERVICE_NAME} is running"
    else
        warn "Service may not have started correctly. Check: journalctl -u ${SERVICE_NAME} -f"
    fi
}

# --- Main ---
main() {
    echo ""
    echo "  _   _          __        __    _       _     "
    echo " | \ | | _____  _\ \      / /_ _| |_ ___| |__  "
    echo " |  \| |/ _ \ \/ /\ \ /\ / / _\` | __/ __| '_ \ "
    echo " | |\  |  __/>  <  \ V  V / (_| | || (__| | | |"
    echo " |_| \_|\___/_/\_\  \_/\_/ \__,_|\__\___|_| |_|"
    echo ""
    echo " Agent Installer"
    echo ""

    check_root
    detect_platform
    resolve_version
    download_binary
    create_config
    create_service
    enable_and_start

    echo ""
    success "NexWatch Agent ${VERSION} installed and running!"
    echo ""
    info "Useful commands:"
    echo "  Status:   systemctl status ${SERVICE_NAME}"
    echo "  Logs:     journalctl -u ${SERVICE_NAME} -f"
    echo "  Restart:  systemctl restart ${SERVICE_NAME}"
    echo "  Stop:     systemctl stop ${SERVICE_NAME}"
    echo "  Config:   ${CONFIG_DIR}/agent.yaml"
    echo ""
}

main
