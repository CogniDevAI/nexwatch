#!/bin/sh
# Regenerate deploy/nginx/cloudflare-real-ip.conf from Cloudflare's published
# IP range lists.
#
# Usage: scripts/update-cloudflare-ips.sh

set -eu

# shellcheck disable=SC1007  # intentional: clear CDPATH so `cd` can't print an unexpected path
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
OUT_FILE="${SCRIPT_DIR}/../deploy/nginx/cloudflare-real-ip.conf"
V4_URL="https://www.cloudflare.com/ips-v4"
V6_URL="https://www.cloudflare.com/ips-v6"

TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

echo "Fetching ${V4_URL} ..."
curl -fsSL "$V4_URL" -o "${TMP_DIR}/v4"

echo "Fetching ${V6_URL} ..."
curl -fsSL "$V6_URL" -o "${TMP_DIR}/v6"

if [ ! -s "${TMP_DIR}/v4" ] || [ ! -s "${TMP_DIR}/v6" ]; then
    echo "ERROR: downloaded range list was empty — aborting without writing ${OUT_FILE}" >&2
    exit 1
fi

TODAY=$(date -u +%Y-%m-%d)

{
    echo "# Cloudflare IP ranges for nginx's real_ip module."
    echo "#"
    echo "# Source: ${V4_URL} and ${V6_URL}"
    echo "# Last updated: ${TODAY}"
    echo "#"
    echo "# Regenerate with scripts/update-cloudflare-ips.sh whenever Cloudflare"
    echo "# publishes a new range list."
    echo "#"
    echo "# IMPORTANT: only \`include\` this file when nginx sits directly behind"
    echo "# Cloudflare. If nginx is NOT behind Cloudflare (direct internet exposure,"
    echo "# a different CDN, or another reverse proxy in front of it), remove the"
    echo "# \`include deploy/nginx/cloudflare-real-ip.conf;\` line from nginx.conf —"
    echo "# otherwise any client could set the CF-Connecting-IP header itself and"
    echo "# have it trusted as \$remote_addr."
    echo ""
    echo "# IPv4"
    while IFS= read -r range; do
        [ -n "$range" ] && echo "set_real_ip_from ${range};"
    done < "${TMP_DIR}/v4"
    echo ""
    echo "# IPv6"
    while IFS= read -r range; do
        [ -n "$range" ] && echo "set_real_ip_from ${range};"
    done < "${TMP_DIR}/v6"
    echo ""
    echo "real_ip_header CF-Connecting-IP;"
    echo "real_ip_recursive on;"
} > "$OUT_FILE"

echo "Wrote $(grep -c set_real_ip_from "$OUT_FILE") ranges to ${OUT_FILE}"
