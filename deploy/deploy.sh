#!/bin/bash
# NexWatch Production Deploy Script
# Tested on Ubuntu 22.04 / Debian 12
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/deploy/deploy.sh | bash
#
# Or clone the repo and run:
#   cd deploy && bash deploy.sh

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

# ── Install dependencies ───────────────────────────────────────────────────────
info "Installing dependencies..."
apt-get update -qq
apt-get install -y -qq curl git docker.io docker-compose-plugin gettext-base 2>/dev/null || \
    apt-get install -y -qq curl git docker.io docker-compose gettext-base 2>/dev/null
systemctl enable docker --now
success "Dependencies installed"

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
    read -p "  Enter your domain (e.g. nexwatch.tudominio.com): " DOMAIN
    read -p "  Enter your email for SSL certificates: " EMAIL
    read -p "  Enter your timezone (e.g. America/Guayaquil): " TZ_INPUT

    sed -i "s|DOMAIN=.*|DOMAIN=${DOMAIN}|" .env
    sed -i "s|CERTBOT_EMAIL=.*|CERTBOT_EMAIL=${EMAIL}|" .env
    sed -i "s|TZ=.*|TZ=${TZ_INPUT}|" .env
    success ".env configured"
else
    info "Using existing .env"
fi

# Load env vars
set -a; source .env; set +a

# ── Render nginx config with domain ───────────────────────────────────────────
info "Generating nginx config for domain: ${DOMAIN}..."
mkdir -p nginx/conf.d
envsubst '${DOMAIN}' < nginx/conf.d/nexwatch.conf > /tmp/nexwatch.conf.rendered
mv /tmp/nexwatch.conf.rendered nginx/conf.d/nexwatch.conf
success "Nginx config generated"

# ── Initial SSL certificate (HTTP-01 challenge) ────────────────────────────────
info "Checking SSL certificates..."
if [ ! -d "/etc/letsencrypt/live/${DOMAIN}" ]; then
    info "Obtaining SSL certificate for ${DOMAIN}..."

    # Start nginx with HTTP only for the challenge (temp config)
    cat > /tmp/nginx-init.conf <<NGINXEOF
events { worker_connections 64; }
http {
    server {
        listen 80;
        location /.well-known/acme-challenge/ { root /var/www/certbot; }
        location / { return 200 'ok'; }
    }
}
NGINXEOF

    # Mount certbot www and run challenge
    docker run --rm \
        -v certbot-www:/var/www/certbot \
        -p 80:80 \
        --name nginx-init \
        nginx:alpine sh -c "mkdir -p /var/www/certbot && nginx -c /etc/nginx/nginx.conf" &
    sleep 3

    docker run --rm \
        -v certbot-certs:/etc/letsencrypt \
        -v certbot-www:/var/www/certbot \
        certbot/certbot certonly \
        --webroot -w /var/www/certbot \
        --email "${CERTBOT_EMAIL}" \
        --agree-tos --no-eff-email \
        -d "${DOMAIN}" \
        --non-interactive

    docker stop nginx-init 2>/dev/null || true
    success "SSL certificate obtained"
else
    info "SSL certificate already exists"
fi

# ── Pull images and start ──────────────────────────────────────────────────────
info "Pulling latest images..."
docker compose -f docker-compose.prod.yml pull

info "Starting NexWatch..."
docker compose -f docker-compose.prod.yml up -d

# ── Wait for hub to be ready ───────────────────────────────────────────────────
info "Waiting for hub to start..."
for i in $(seq 1 30); do
    if docker compose -f docker-compose.prod.yml exec -T hub wget -qO- http://localhost:8090/api/health > /dev/null 2>&1; then
        break
    fi
    sleep 2
done

success "NexWatch is running!"
echo ""
echo "  ┌─────────────────────────────────────────────────┐"
echo "  │                                                 │"
echo "  │   NexWatch UI:   https://${DOMAIN}              "
echo "  │   PocketBase:    https://${DOMAIN}/_/           "
echo "  │                                                 │"
echo "  │   Agent install command:                        │"
echo "  │   See: https://${DOMAIN} → Agents → New Agent  "
echo "  │                                                 │"
echo "  └─────────────────────────────────────────────────┘"
echo ""
warn "First time? Set up your admin account at: https://${DOMAIN}/_/"
echo ""
info "Useful commands:"
echo "  Logs:    docker compose -f ${DEPLOY_DIR}/deploy/docker-compose.prod.yml logs -f"
echo "  Restart: docker compose -f ${DEPLOY_DIR}/deploy/docker-compose.prod.yml restart"
echo "  Update:  cd ${DEPLOY_DIR} && git pull && cd deploy && docker compose -f docker-compose.prod.yml pull && docker compose -f docker-compose.prod.yml up -d"
echo ""
