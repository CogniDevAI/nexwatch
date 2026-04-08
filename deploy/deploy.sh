#!/bin/bash
# NexWatch Production Deploy Script
# Supports: Ubuntu/Debian, AlmaLinux/Rocky/RHEL/CentOS, Oracle Linux
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/deploy/deploy.sh | sudo bash

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()    { echo -e "${CYAN}[INFO]${NC} $1"; }
success() { echo -e "${GREEN}[OK]${NC} $1"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $1"; }
error()   { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

REPO="CogniDevAI/nexwatch"
DEPLOY_DIR="/opt/nexwatch"

echo ""
echo "  NexWatch Production Deploy"
echo "  ─────────────────────────"
echo ""

# ── Root check ────────────────────────────────────────────────────────────────
if [ "$(id -u)" -ne 0 ]; then
    error "Run as root: sudo bash deploy.sh"
fi

# ── Install Docker ─────────────────────────────────────────────────────────────
info "Checking Docker..."

if ! command -v docker > /dev/null 2>&1; then
    info "Installing Docker..."

    if command -v apt-get > /dev/null 2>&1; then
        apt-get update -qq
        apt-get install -y -qq curl git
        curl -fsSL https://get.docker.com | sh

    elif command -v dnf > /dev/null 2>&1; then
        dnf install -y curl git
        curl -fsSL https://get.docker.com | sh

    elif command -v yum > /dev/null 2>&1; then
        yum install -y curl git
        curl -fsSL https://get.docker.com | sh

    else
        error "Unsupported OS. Install Docker manually: https://docs.docker.com/engine/install/"
    fi

    systemctl enable docker --now
    success "Docker installed"
else
    info "Docker already installed: $(docker --version)"
fi

# ── Install git if missing ─────────────────────────────────────────────────────
if ! command -v git > /dev/null 2>&1; then
    command -v dnf > /dev/null 2>&1 && dnf install -y git
    command -v apt-get > /dev/null 2>&1 && apt-get install -y -qq git
fi

# ── Clone or update repo ───────────────────────────────────────────────────────
if [ -d "$DEPLOY_DIR/.git" ]; then
    info "Updating existing installation..."
    git -C "$DEPLOY_DIR" pull --ff-only
else
    info "Cloning NexWatch repository..."
    git clone "https://github.com/${REPO}.git" "$DEPLOY_DIR"
fi
cd "$DEPLOY_DIR/deploy"
success "Repository ready at $DEPLOY_DIR"

# ── .env setup ────────────────────────────────────────────────────────────────
if [ ! -f ".env" ]; then
    cp .env.example .env
    echo ""
    warn "No .env file found — created from template."
    echo ""
    read -rp "  Hub port to expose (default 8090): " HUB_PORT_INPUT
    HUB_PORT_INPUT="${HUB_PORT_INPUT:-8090}"
    read -rp "  Timezone (e.g. America/Guayaquil): " TZ_INPUT
    TZ_INPUT="${TZ_INPUT:-America/Guayaquil}"

    sed -i "s|HUB_PORT=.*|HUB_PORT=${HUB_PORT_INPUT}|" .env
    sed -i "s|TZ=.*|TZ=${TZ_INPUT}|" .env
    success ".env configured"
else
    info "Using existing .env"
fi

# Load env — parse manually to avoid issues with comments on AlmaLinux/bash
while IFS='=' read -r key value; do
    # Skip empty lines and comments
    case "$key" in ''|\#*) continue ;; esac
    export "$key=$value"
done < .env

# ── Pull image and start ───────────────────────────────────────────────────────
info "Pulling latest NexWatch Hub image..."
docker compose -f docker-compose.prod.yml pull

info "Starting NexWatch Hub..."
docker compose -f docker-compose.prod.yml up -d

# ── Wait for hub ───────────────────────────────────────────────────────────────
info "Waiting for hub to start..."
for i in $(seq 1 15); do
    if curl -sf "http://localhost:${HUB_PORT:-8090}/api/health" > /dev/null 2>&1; then
        break
    fi
    sleep 2
done

success "NexWatch Hub is running on port ${HUB_PORT:-8090}!"
echo ""
echo "  ┌──────────────────────────────────────────────────────────┐"
echo "  │                                                          │"
echo "  │  Hub listening on:  http://localhost:${HUB_PORT:-8090}             │"
echo "  │                                                          │"
echo "  │  Configure your nginx to proxy this port.               │"
echo "  │  See nginx config example below.                        │"
echo "  │                                                          │"
echo "  └──────────────────────────────────────────────────────────┘"
echo ""
echo "  Nginx config snippet (add to your server block):"
echo ""
echo "    location / {"
echo "        proxy_pass         http://127.0.0.1:${HUB_PORT:-8090};"
echo "        proxy_http_version 1.1;"
echo "        proxy_set_header   Upgrade \$http_upgrade;"
echo "        proxy_set_header   Connection \$connection_upgrade;"
echo "        proxy_set_header   Host \$host;"
echo "        proxy_set_header   X-Real-IP \$remote_addr;"
echo "        proxy_set_header   X-Forwarded-For \$proxy_add_x_forwarded_for;"
echo "        proxy_set_header   X-Forwarded-Proto \$scheme;"
echo "        proxy_read_timeout 3600s;"
echo "    }"
echo ""
warn "First time? Go to https://tudominio.com/_/ to set up your admin account."
echo ""
info "Update command:  cd ${DEPLOY_DIR}/deploy && make update"
info "Logs:            cd ${DEPLOY_DIR}/deploy && make logs"
echo ""
