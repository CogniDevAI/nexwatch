#!/usr/bin/env bash
# NexWatch Agent Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/install-agent.sh | bash -s -- --hub ws://hub:8090/ws/agent --token TOKEN
#
# Environment variables (alternative to flags):
#   HUB_URL                   - Hub WebSocket URL (e.g. ws://hub:8090/ws/agent)
#   TOKEN                     - Agent authentication token
#   VERSION                   - Agent version to install (default: latest)
#   INTERVAL                  - Collection interval in seconds (default: 10)
#   MODE                      - Agent mode: standard (default) or oracle
#   ORACLE_HOME               - Oracle Home path (oracle mode only)
#   ORACLE_SID                - Oracle SID (oracle mode only)
#   NEXWATCH_REQUIRE_SIGNATURE - If set (1/true), abort unless the release's
#                                signature can be verified (see --require-signature)
#   NEXWATCH_SIGNING_KEY_URL  - Override the URL the release signing public key
#                                is fetched from (see --signing-key-url)
#   NEXWATCH_SIGNING_KEY_FILE - Use a local signing public key file instead of
#                                fetching one (see --signing-key-file)
#
# This script is sourceable: when sourced (e.g. by scripts/test-install-agent.sh)
# it only defines the functions below and does not run main().

set -eu

# --- Defaults ---
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="nexwatch-agent"
CONFIG_DIR="/etc/nexwatch"
SERVICE_NAME="nexwatch-agent"
REPO="CogniDevAI/nexwatch"
GITHUB_BASE="https://github.com/${REPO}"
DEFAULT_SIGNING_KEY_URL="https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/release-signing-key.asc"

# Read from environment or leave empty for flag parsing.
HUB_URL="${HUB_URL:-}"
TOKEN="${TOKEN:-}"
VERSION="${VERSION:-latest}"
INTERVAL="${INTERVAL:-10}"
MODE="${MODE:-standard}"
ORACLE_HOME="${ORACLE_HOME:-}"
ORACLE_SID="${ORACLE_SID:-}"
REQUIRE_SIGNATURE="${NEXWATCH_REQUIRE_SIGNATURE:-0}"
SIGNING_KEY_URL="${NEXWATCH_SIGNING_KEY_URL:-$DEFAULT_SIGNING_KEY_URL}"
SIGNING_KEY_FILE="${NEXWATCH_SIGNING_KEY_FILE:-}"

# Service user/group — set by create_user based on MODE.
SERVICE_USER="nexwatch"
SERVICE_GROUP="nexwatch"

# Temp directory — set early so cleanup trap can reference it.
TMP_DIR=""

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

info()    { printf "${CYAN}[INFO]${NC} %s\n" "$1"; }
success() { printf "${GREEN}[OK]${NC} %s\n" "$1"; }
warn()    { printf "${YELLOW}[WARN]${NC} %s\n" "$1"; }
error()   { printf "${RED}[ERROR]${NC} %s\n" "$1" >&2; exit 1; }

# --- Cleanup trap ---
cleanup() {
    if [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ]; then
        rm -rf "$TMP_DIR"
    fi
}

# --- Normalize a boolean-ish flag value to "0" or "1" ---
normalize_bool() {
    case "$1" in
        1|true|TRUE|True|yes|YES|Yes) echo 1 ;;
        *) echo 0 ;;
    esac
}

print_help() {
    echo "NexWatch Agent Installer"
    echo ""
    echo "Usage:"
    echo "  install-agent.sh [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --hub URL             Hub WebSocket URL (required)"
    echo "  --token TOKEN         Agent authentication token (required)"
    echo "  --version VER         Agent version (default: latest)"
    echo "  --interval SECS       Collection interval in seconds (default: 10)"
    echo "  --mode MODE           Agent mode: standard (default) or oracle"
    echo "  --oracle-home PATH    Oracle Home path (oracle mode)"
    echo "  --oracle-sid SID      Oracle SID (oracle mode)"
    echo "  --require-signature   Abort if the release signature can't be verified"
    echo "  --signing-key-url URL URL to fetch the release signing public key from"
    echo "  --signing-key-file F  Use a local signing public key file instead"
    echo "  --help                Show this help"
    echo ""
    echo "Environment variables:"
    echo "  HUB_URL, TOKEN, VERSION, INTERVAL, MODE, ORACLE_HOME, ORACLE_SID,"
    echo "  NEXWATCH_REQUIRE_SIGNATURE, NEXWATCH_SIGNING_KEY_URL, NEXWATCH_SIGNING_KEY_FILE"
}

# --- Parse arguments ---
parse_args() {
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
            --mode)
                MODE="$2"
                shift 2
                ;;
            --oracle-home)
                ORACLE_HOME="$2"
                shift 2
                ;;
            --oracle-sid)
                ORACLE_SID="$2"
                shift 2
                ;;
            --require-signature)
                REQUIRE_SIGNATURE=1
                shift
                ;;
            --signing-key-url)
                SIGNING_KEY_URL="$2"
                shift 2
                ;;
            --signing-key-file)
                SIGNING_KEY_FILE="$2"
                shift 2
                ;;
            --help|-h)
                print_help
                exit 0
                ;;
            *)
                warn "Unknown option: $1"
                shift
                ;;
        esac
    done

    REQUIRE_SIGNATURE=$(normalize_bool "$REQUIRE_SIGNATURE")
}

# --- Validate required params ---
validate_args() {
    if [ -z "$HUB_URL" ]; then
        error "HUB_URL is required. Use --hub or set HUB_URL environment variable."
    fi

    if [ -z "$TOKEN" ]; then
        error "TOKEN is required. Use --token or set TOKEN environment variable."
    fi
}

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

# --- Verify checksum (per-file .sha256 fallback path) ---
verify_checksum() {
    TMP_FILE="$1"
    CHECKSUM_FILE="$2"

    # Determine which sha256 tool is available.
    SHA256_CMD=""
    if command -v sha256sum > /dev/null 2>&1; then
        SHA256_CMD="sha256sum"
    elif command -v shasum > /dev/null 2>&1; then
        SHA256_CMD="shasum -a 256"
    fi

    if [ -z "$SHA256_CMD" ]; then
        warn "No sha256sum or shasum found — skipping checksum verification."
        return
    fi

    # The .sha256 file contains: "<hash>  <filename>" or just "<hash>".
    EXPECTED=$(awk '{print $1}' "$CHECKSUM_FILE")
    if [ -z "$EXPECTED" ]; then
        warn "Checksum file is empty — skipping verification."
        return
    fi

    ACTUAL=$(${SHA256_CMD} "$TMP_FILE" | awk '{print $1}')

    if [ "$ACTUAL" != "$EXPECTED" ]; then
        error "Checksum verification failed. Expected: ${EXPECTED}  Got: ${ACTUAL}"
    fi

    success "Checksum verified"
}

# --- Verify release signature (SHA256SUMS + SHA256SUMS.asc), if published ---
# Returns 0 when the tarball was verified against a signed, trusted SHA256SUMS.
# Returns 1 when signature verification was skipped (no signature published,
# or gpg unavailable) — the caller should fall back to the per-file checksum.
# Aborts the script (via error()) on any verification FAILURE (bad signature,
# bad key, or hash mismatch) — that is never treated as "skip".
verify_release_signature() {
    TARBALL="$1"
    ARCHIVE="$2"

    if ! command -v gpg > /dev/null 2>&1; then
        if [ "$REQUIRE_SIGNATURE" = "1" ]; then
            error "gpg is required for signature verification (--require-signature set) but was not found."
        fi
        warn "gpg not found — skipping release signature verification."
        return 1
    fi

    SUMS_URL="${GITHUB_BASE}/releases/download/${VERSION}/SHA256SUMS"
    SIG_URL="${SUMS_URL}.asc"
    SUMS_FILE="${TMP_DIR}/SHA256SUMS"
    SIG_FILE="${TMP_DIR}/SHA256SUMS.asc"

    if ! curl -fsSL -o "$SUMS_FILE" "$SUMS_URL" 2>/dev/null || ! curl -fsSL -o "$SIG_FILE" "$SIG_URL" 2>/dev/null; then
        if [ "$REQUIRE_SIGNATURE" = "1" ]; then
            error "Release ${VERSION} has no signed SHA256SUMS at ${SUMS_URL} (--require-signature set)."
        fi
        warn "Release ${VERSION} has no signed checksums — skipping signature verification."
        return 1
    fi

    info "Verifying release signature..."

    GNUPGHOME_DIR="${TMP_DIR}/gnupg"
    mkdir -p "$GNUPGHOME_DIR"
    chmod 700 "$GNUPGHOME_DIR"

    if [ -n "$SIGNING_KEY_FILE" ]; then
        if [ ! -f "$SIGNING_KEY_FILE" ]; then
            error "Signing key file not found: ${SIGNING_KEY_FILE}"
        fi
        KEY_FILE="$SIGNING_KEY_FILE"
    else
        KEY_FILE="${TMP_DIR}/release-signing-key.asc"
        if ! curl -fsSL -o "$KEY_FILE" "$SIGNING_KEY_URL" 2>/dev/null; then
            if [ "$REQUIRE_SIGNATURE" = "1" ]; then
                error "Could not fetch signing public key from ${SIGNING_KEY_URL} (--require-signature set)."
            fi
            warn "Could not fetch signing public key from ${SIGNING_KEY_URL} — skipping signature verification."
            return 1
        fi
    fi

    if ! GNUPGHOME="$GNUPGHOME_DIR" gpg --batch --quiet --import "$KEY_FILE" 2>/dev/null; then
        error "Failed to import release signing public key from ${KEY_FILE}."
    fi

    if ! GNUPGHOME="$GNUPGHOME_DIR" gpg --batch --verify "$SIG_FILE" "$SUMS_FILE" 2>/dev/null; then
        error "Release signature verification FAILED for ${VERSION}. The release may be tampered with — aborting."
    fi

    success "Release signature verified"

    # Cross-check the tarball's own hash against the entry in the signed SHA256SUMS.
    EXPECTED=$(awk -v f="$ARCHIVE" '$2 == f {print $1}' "$SUMS_FILE")
    if [ -z "$EXPECTED" ]; then
        error "No checksum entry for ${ARCHIVE} in the signed SHA256SUMS. Aborting."
    fi

    SHA256_CMD=""
    if command -v sha256sum > /dev/null 2>&1; then
        SHA256_CMD="sha256sum"
    elif command -v shasum > /dev/null 2>&1; then
        SHA256_CMD="shasum -a 256"
    else
        error "No sha256sum or shasum found — cannot verify the signed checksum."
    fi

    ACTUAL=$(${SHA256_CMD} "$TARBALL" | awk '{print $1}')
    if [ "$ACTUAL" != "$EXPECTED" ]; then
        error "Checksum mismatch against signed SHA256SUMS. Expected: ${EXPECTED}  Got: ${ACTUAL}. Aborting."
    fi

    success "Tarball hash matches the signed SHA256SUMS"
    return 0
}

# --- Download binary ---
download_binary() {
    ARCHIVE_NAME="${BINARY_NAME}_${VERSION#v}_${OS}_${ARCH}.tar.gz"
    DOWNLOAD_URL="${GITHUB_BASE}/releases/download/${VERSION}/${ARCHIVE_NAME}"
    CHECKSUM_URL="${DOWNLOAD_URL}.sha256"

    TMP_DIR=$(mktemp -d)
    TMP_FILE="${TMP_DIR}/${ARCHIVE_NAME}"
    TMP_CHECKSUM="${TMP_DIR}/${ARCHIVE_NAME}.sha256"

    info "Downloading ${BINARY_NAME} ${VERSION}..."
    if ! curl -fsSL -o "$TMP_FILE" "$DOWNLOAD_URL"; then
        error "Download failed. Check that version ${VERSION} exists at ${DOWNLOAD_URL}"
    fi

    if ! verify_release_signature "$TMP_FILE" "$ARCHIVE_NAME"; then
        # No signature to verify against (or gpg missing) — fall back to the
        # per-file checksum, which only protects against transport corruption.
        info "Downloading checksum..."
        if curl -fsSL -o "$TMP_CHECKSUM" "$CHECKSUM_URL" 2>/dev/null; then
            verify_checksum "$TMP_FILE" "$TMP_CHECKSUM"
        else
            warn "Checksum file not found at ${CHECKSUM_URL} — skipping verification."
        fi
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
        error "Binary not found in archive."
    fi

    # Install to target directory.
    install -m 755 "$EXTRACTED_BINARY" "${INSTALL_DIR}/${BINARY_NAME}"

    success "Binary installed to ${INSTALL_DIR}/${BINARY_NAME}"
}

# --- Add a user to a supplementary group, if that group exists on this host ---
# Used for both docker (standard mode only) and the journald/log-reading
# groups (every mode, since log shipping — internal/agent/logs — applies
# regardless of standard vs. Oracle mode).
add_to_group_if_exists() {
    user="$1"
    group="$2"
    if getent group "$group" > /dev/null 2>&1; then
        if ! id -nG "$user" | grep -qw "$group"; then
            usermod -aG "$group" "$user"
            success "Added '${user}' to '${group}' group"
        else
            info "User '${user}' is already in '${group}' group"
        fi
    else
        info "Group '${group}' not found — skipping"
    fi
}

# --- Create system user ---
create_user() {
    if [ "$MODE" = "oracle" ]; then
        # Oracle mode: run as existing 'oracle' OS user (member of dba group).
        if id oracle > /dev/null 2>&1; then
            info "Oracle mode: using existing 'oracle' OS user"
            SERVICE_USER="oracle"
            SERVICE_GROUP="oinstall"
            # Ensure oracle is in dba group for sqlplus OS auth.
            if getent group dba > /dev/null 2>&1; then
                if ! id -nG oracle | grep -qw dba; then
                    usermod -aG dba oracle
                fi
            fi
            # Additive to the dba membership above: lets the oracle user
            # read journald (log_sources: journald) and /var/log
            # (log_sources: file) for the log-shipping feature, the same
            # as standard mode gets below.
            add_to_group_if_exists oracle systemd-journal
            add_to_group_if_exists oracle adm
        else
            error "Oracle mode requires an existing 'oracle' OS user. Is Oracle installed?"
        fi
        return
    fi

    # Standard mode: create dedicated nexwatch user.
    SERVICE_USER="nexwatch"
    SERVICE_GROUP="nexwatch"
    if id nexwatch > /dev/null 2>&1; then
        info "System user 'nexwatch' already exists"
    else
        info "Creating system user 'nexwatch'..."
        useradd --system --no-create-home --shell /usr/sbin/nologin nexwatch
        success "System user 'nexwatch' created"
    fi

    # Add to docker group if it exists.
    add_to_group_if_exists nexwatch docker

    # Lets the nexwatch user read journald (log_sources: journald) and
    # /var/log (log_sources: file) for the log-shipping feature, without
    # running as root. Both groups exist by default on any systemd-based
    # Linux distribution.
    add_to_group_if_exists nexwatch systemd-journal
    add_to_group_if_exists nexwatch adm
}

# --- Create config ---
create_config() {
    mkdir -p "$CONFIG_DIR"
    # Ensure the config directory is owned and accessible by the service user.
    chown "${SERVICE_USER}:${SERVICE_GROUP}" "$CONFIG_DIR" 2>/dev/null || true
    chmod 750 "$CONFIG_DIR" 2>/dev/null || true

    # Collector lists per mode.
    STANDARD_COLLECTORS="cpu memory disk network sysinfo docker ports processes hardening vulnerabilities diskio connections services"
    ORACLE_COLLECTORS="cpu memory disk network sysinfo processes diskio connections services oracle"

    if [ "$MODE" = "oracle" ]; then
        ALL_COLLECTORS="$ORACLE_COLLECTORS"
    else
        ALL_COLLECTORS="$STANDARD_COLLECTORS"
    fi

    if [ -f "${CONFIG_DIR}/agent.yaml" ]; then
        info "Config already exists, merging new collectors..."

        added=""
        for col in $ALL_COLLECTORS; do
            if ! grep -q "^\s*-\s*${col}\s*$" "${CONFIG_DIR}/agent.yaml"; then
                printf '  - %s\n' "$col" >> "${CONFIG_DIR}/agent.yaml"
                added="${added} ${col}"
            fi
        done

        # Also add oracle_home/oracle_sid if oracle mode and missing.
        if [ "$MODE" = "oracle" ]; then
            if ! grep -q "oracle_home" "${CONFIG_DIR}/agent.yaml"; then
                printf 'oracle_home: "%s"\n' "$ORACLE_HOME" >> "${CONFIG_DIR}/agent.yaml"
                printf 'oracle_sid: "%s"\n' "$ORACLE_SID" >> "${CONFIG_DIR}/agent.yaml"
                added="${added} oracle_home oracle_sid"
            fi
        fi

        if [ -n "$added" ]; then
            success "Added missing config:${added}"
        else
            info "Config already up to date"
        fi

        chown "${SERVICE_USER}:${SERVICE_GROUP}" "${CONFIG_DIR}/agent.yaml" 2>/dev/null || true
        chmod 600 "${CONFIG_DIR}/agent.yaml" 2>/dev/null || true
        return
    fi

    # Build new config file.
    if [ "$MODE" = "oracle" ]; then
        cat > "${CONFIG_DIR}/agent.yaml" <<YAML
# NexWatch Agent Configuration — Oracle Mode
# Generated by install-agent.sh

hub_url: "${HUB_URL}"
token: "${TOKEN}"
interval: ${INTERVAL}s
docker_socket: /var/run/docker.sock
oracle_home: "${ORACLE_HOME}"
oracle_sid: "${ORACLE_SID}"
collectors_enabled:
  - cpu
  - memory
  - disk
  - network
  - sysinfo
  - processes
  - diskio
  - connections
  - services
  - oracle
YAML
    else
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
  - diskio
  - connections
  - services
YAML
    fi

    chmod 600 "${CONFIG_DIR}/agent.yaml"
    chown "${SERVICE_USER}:${SERVICE_GROUP}" "${CONFIG_DIR}/agent.yaml"
    success "Config written to ${CONFIG_DIR}/agent.yaml"
}

# --- Create systemd service ---
create_service() {
    # Build optional docker supplementary group line (standard mode only).
    DOCKER_GROUP_LINE=""
    if [ "$MODE" != "oracle" ] && getent group docker > /dev/null 2>&1; then
        DOCKER_GROUP_LINE="SupplementaryGroups=docker"
    fi

    # Build the log-shipping supplementary group line — applies to both
    # standard and Oracle mode (additive to Oracle's own dba/docker
    # handling), so the agent can read journald and /var/log without
    # running as root. Only includes groups that actually exist on this
    # host, and omits the line entirely if neither does.
    LOG_GROUPS=""
    if getent group systemd-journal > /dev/null 2>&1; then
        LOG_GROUPS="systemd-journal"
    fi
    if getent group adm > /dev/null 2>&1; then
        LOG_GROUPS="${LOG_GROUPS:+${LOG_GROUPS} }adm"
    fi
    LOG_GROUPS_LINE=""
    if [ -n "$LOG_GROUPS" ]; then
        LOG_GROUPS_LINE="SupplementaryGroups=${LOG_GROUPS}"
    fi

    # Oracle mode: looser security (oracle user needs full /proc and oracle dirs).
    if [ "$MODE" = "oracle" ]; then
        cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=NexWatch Monitoring Agent (Oracle Mode)
Documentation=https://github.com/${REPO}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_GROUP}
Environment=ORACLE_HOME=${ORACLE_HOME}
Environment=ORACLE_SID=${ORACLE_SID}
Environment=LD_LIBRARY_PATH=${ORACLE_HOME}/lib
Environment=PATH=${ORACLE_HOME}/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
EnvironmentFile=-/etc/nexwatch/agent.env
ExecStart=${INSTALL_DIR}/${BINARY_NAME}
# Restart=always (not on-failure): a hub-initiated self-update
# (internal/agent/update) re-execs the new binary in place when possible,
# but falls back to a clean exit(0) when re-exec itself isn't available —
# Restart=on-failure does NOT restart systemd units on a clean exit 0.
Restart=always
RestartSec=2
LimitNOFILE=65536
${LOG_GROUPS_LINE}

# Writable /var/lib/nexwatch for the cve_scan collector's scanner cache
# (TRIVY_CACHE_DIR/GRYPE_DB_CACHE_DIR). systemd creates/owns this
# directory for ${SERVICE_USER}:${SERVICE_GROUP} regardless of the looser
# Oracle-mode sandboxing below.
StateDirectory=nexwatch

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

[Install]
WantedBy=multi-user.target
UNIT
    else
        cat > "/etc/systemd/system/${SERVICE_NAME}.service" <<UNIT
[Unit]
Description=NexWatch Monitoring Agent
Documentation=https://github.com/${REPO}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_GROUP}
EnvironmentFile=-/etc/nexwatch/agent.env
ExecStart=${INSTALL_DIR}/${BINARY_NAME}
# Restart=always (not on-failure): a hub-initiated self-update
# (internal/agent/update) re-execs the new binary in place when possible,
# but falls back to a clean exit(0) when re-exec itself isn't available —
# Restart=on-failure does NOT restart systemd units on a clean exit 0.
Restart=always
RestartSec=2
LimitNOFILE=65536

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${CONFIG_DIR}
PrivateTmp=true
${DOCKER_GROUP_LINE}
${LOG_GROUPS_LINE}

# Allow reading /proc and /sys for metric collectors (diskio, connections, processes)
ReadOnlyPaths=/proc /sys

# Writable /var/lib/nexwatch under ProtectSystem=strict, for the cve_scan
# collector's scanner cache (TRIVY_CACHE_DIR/GRYPE_DB_CACHE_DIR — see
# cve_scan_cache_dir in agent.yaml). systemd creates/owns this directory.
StateDirectory=nexwatch

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${SERVICE_NAME}

[Install]
WantedBy=multi-user.target
UNIT
    fi

    success "Systemd service created at /etc/systemd/system/${SERVICE_NAME}.service"
}

# --- Enable and start service ---
enable_and_start() {
    systemctl daemon-reload
    systemctl enable "$SERVICE_NAME"

    # Always restart so the new binary and unit take effect immediately.
    if systemctl is-active --quiet "$SERVICE_NAME"; then
        info "Restarting ${SERVICE_NAME} to apply new binary..."
        systemctl restart "$SERVICE_NAME"
    else
        systemctl start "$SERVICE_NAME"
    fi

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
    trap cleanup EXIT INT TERM

    echo ""
    echo "  _   _          __        __    _       _     "
    echo " | \ | | _____  _\ \      / /_ _| |_ ___| |__  "
    echo " |  \| |/ _ \ \/ /\ \ /\ / / _\` | __/ __| '_ \ "
    echo " | |\  |  __/>  <  \ V  V / (_| | || (__| | | |"
    echo " |_| \_|\___/_/\_\  \_/\_/ \__,_|\__\___|_| |_|"
    echo ""
    echo " Agent Installer"
    echo ""

    parse_args "$@"
    validate_args
    check_root
    detect_platform
    resolve_version
    create_user
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

# --- Entrypoint guard ---
# `return` only succeeds when this file is being sourced (e.g. by
# scripts/test-install-agent.sh). When executed directly — including via
# `curl | bash -s --`, where there is no on-disk file and BASH_SOURCE[0] is
# unset — `return` at the top level fails, so main() runs.
if ! (return 0 2>/dev/null); then
    main "$@"
fi
