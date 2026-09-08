#!/usr/bin/env bash
# Test harness for scripts/install-agent.sh's release signature verification.
#
# Sources the installer (which only defines functions when sourced — see its
# entrypoint guard) and exercises verify_release_signature() directly against
# a throwaway GPG key and a fake release, with curl replaced by a local-file
# mock so no network access is required.
#
# Usage: scripts/test-install-agent.sh

set -u

# shellcheck disable=SC1007  # intentional: clear CDPATH so `cd` can't print an unexpected path
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
INSTALLER="${SCRIPT_DIR}/install-agent.sh"

PASS_COUNT=0
FAIL_COUNT=0

pass() { PASS_COUNT=$((PASS_COUNT + 1)); printf '\033[0;32m[PASS]\033[0m %s\n' "$1"; }
fail() { FAIL_COUNT=$((FAIL_COUNT + 1)); printf '\033[0;31m[FAIL]\033[0m %s\n' "$1"; }
note() { printf '\033[0;36m[..]\033[0m %s\n' "$1"; }

if ! command -v gpg > /dev/null 2>&1; then
    echo "SKIP: gpg is not installed — cannot test release signature verification."
    exit 0
fi

# Use a short, explicit template under /tmp rather than plain `mktemp -d`:
# some platforms (notably macOS) default TMPDIR to a deeply nested path, and
# gpg-agent's UNIX socket (created under a further-nested $TMP_DIR/gnupg/)
# can exceed the OS's AF_UNIX path length limit there. /tmp is short and
# writable everywhere this script is expected to run.
WORKDIR=$(mktemp -d /tmp/nexwatch-install-test.XXXXXX)
# shellcheck disable=SC2329  # invoked indirectly via the trap below
cleanup() { rm -rf "$WORKDIR"; }
trap cleanup EXIT INT TERM

GNUPGHOME_SIGNER="${WORKDIR}/signer-gnupg"
mkdir -p "$GNUPGHOME_SIGNER"
chmod 700 "$GNUPGHOME_SIGNER"

note "Generating throwaway GPG signing key (this can take a few seconds)..."
cat > "${WORKDIR}/keygen.batch" <<'EOF'
%echo Generating ephemeral test key
Key-Type: RSA
Key-Length: 2048
Subkey-Type: RSA
Subkey-Length: 2048
Name-Real: NexWatch Test Signing Key
Name-Email: test@nexwatch.invalid
Expire-Date: 0
%no-protection
%commit
%echo done
EOF

if ! GNUPGHOME="$GNUPGHOME_SIGNER" gpg --batch --gen-key "${WORKDIR}/keygen.batch" > "${WORKDIR}/keygen.log" 2>&1; then
    echo "SKIP: failed to generate a throwaway GPG key in this environment."
    cat "${WORKDIR}/keygen.log"
    exit 0
fi

GNUPGHOME="$GNUPGHOME_SIGNER" gpg --batch --armor --export test@nexwatch.invalid > "${WORKDIR}/pubkey.asc"

# --- Fake release artifacts ---
ARCHIVE_NAME="nexwatch-agent_1.0.0_linux_amd64.tar.gz"
echo "fake tarball content for testing" > "${WORKDIR}/${ARCHIVE_NAME}"

SHA256_CMD="sha256sum"
command -v sha256sum > /dev/null 2>&1 || SHA256_CMD="shasum -a 256"
TARBALL_HASH=$(${SHA256_CMD} "${WORKDIR}/${ARCHIVE_NAME}" | awk '{print $1}')
printf '%s  %s\n' "$TARBALL_HASH" "$ARCHIVE_NAME" > "${WORKDIR}/SHA256SUMS"

GNUPGHOME="$GNUPGHOME_SIGNER" gpg --batch --yes --armor --detach-sign \
    --output "${WORKDIR}/SHA256SUMS.asc" "${WORKDIR}/SHA256SUMS"

# A tampered SUMS file: same signature, different (attacker-modified) content.
# The signature was produced over the original bytes, so gpg must reject this.
printf '%s  %s\n' "dead0000dead0000dead0000dead0000dead0000dead0000dead0000dead0000" "$ARCHIVE_NAME" > "${WORKDIR}/SHA256SUMS.tampered"

# --- Source the installer (defines functions only — entrypoint guard prevents main() from running) ---
# shellcheck source=install-agent.sh
source "$INSTALLER"

# --- curl mock: serves files from $MOCK_DIR based on the requested URL's suffix instead of hitting the network ---
curl() {
    local out="" url="" arg
    for arg in "$@"; do
        case "$arg" in
            -o) expect_out=1 ;;
            http*://*) url="$arg" ;;
            *)
                if [ "${expect_out:-0}" = "1" ]; then
                    out="$arg"
                    expect_out=0
                fi
                ;;
        esac
    done

    local src=""
    case "$url" in
        */SHA256SUMS.asc) src="${MOCK_DIR:-}/sig" ;;
        */SHA256SUMS) src="${MOCK_DIR:-}/sums" ;;
        */release-signing-key.asc) src="${MOCK_DIR:-}/key" ;;
    esac

    if [ -z "$src" ] || [ ! -f "$src" ]; then
        return 22
    fi
    cp "$src" "$out"
}

TEST_REQUIRE_SIGNATURE=0

run_verify() {
    # Runs verify_release_signature in a subshell so its internal error()/exit
    # calls only terminate the subshell, not this test driver. The subshell
    # is a fork of this process, so it inherits TEST_REQUIRE_SIGNATURE (and
    # every other variable/function already set here) regardless of export.
    (
        TMP_DIR="${WORKDIR}/run-$$-${RANDOM}"
        mkdir -p "$TMP_DIR"
        VERSION="v1.0.0"
        GITHUB_BASE="https://example.invalid/CogniDevAI/nexwatch"
        SIGNING_KEY_URL="https://example.invalid/release-signing-key.asc"
        SIGNING_KEY_FILE=""
        REQUIRE_SIGNATURE="$TEST_REQUIRE_SIGNATURE"
        verify_release_signature "${WORKDIR}/${ARCHIVE_NAME}" "$ARCHIVE_NAME" > "${WORKDIR}/last-run.log" 2>&1
    )
}

echo ""
echo "=== Test 1: valid signature + matching hash passes ==="
MOCK_DIR="${WORKDIR}/scenario-valid"
mkdir -p "$MOCK_DIR"
cp "${WORKDIR}/SHA256SUMS" "$MOCK_DIR/sums"
cp "${WORKDIR}/SHA256SUMS.asc" "$MOCK_DIR/sig"
cp "${WORKDIR}/pubkey.asc" "$MOCK_DIR/key"
TEST_REQUIRE_SIGNATURE=0
rc=0
run_verify || rc=$?
cat "${WORKDIR}/last-run.log"
if [ "$rc" -eq 0 ]; then
    pass "valid signature + matching hash verified successfully (exit 0)"
else
    fail "expected exit 0 for valid signature, got ${rc}"
fi

echo ""
echo "=== Test 2: tampered SHA256SUMS is rejected ==="
MOCK_DIR="${WORKDIR}/scenario-tampered"
mkdir -p "$MOCK_DIR"
cp "${WORKDIR}/SHA256SUMS.tampered" "$MOCK_DIR/sums"
cp "${WORKDIR}/SHA256SUMS.asc" "$MOCK_DIR/sig"   # signature is over the ORIGINAL content
cp "${WORKDIR}/pubkey.asc" "$MOCK_DIR/key"
TEST_REQUIRE_SIGNATURE=0
rc=0
run_verify || rc=$?
cat "${WORKDIR}/last-run.log"
if [ "$rc" -ne 0 ]; then
    pass "tampered SHA256SUMS correctly rejected (non-zero exit)"
else
    fail "expected a non-zero exit for a tampered SHA256SUMS, got 0"
fi

echo ""
echo "=== Test 3a: missing signature warns and continues (no --require-signature) ==="
MOCK_DIR="${WORKDIR}/scenario-missing"
mkdir -p "$MOCK_DIR"
# No sums/sig/key files present at all — curl mock will 404 on every request.
TEST_REQUIRE_SIGNATURE=0
rc=0
run_verify || rc=$?
cat "${WORKDIR}/last-run.log"
if [ "$rc" -eq 1 ] && grep -q "skipping signature verification" "${WORKDIR}/last-run.log"; then
    pass "missing signature warns and returns 1 (fallback to checksum) when not required"
else
    fail "expected a warning and exit 1 when signature is missing and not required, got ${rc}"
fi

echo ""
echo "=== Test 3b: missing signature aborts with --require-signature ==="
TEST_REQUIRE_SIGNATURE=1
rc=0
run_verify || rc=$?
cat "${WORKDIR}/last-run.log"
if [ "$rc" -ne 0 ] && grep -q "require-signature" "${WORKDIR}/last-run.log"; then
    pass "missing signature aborts when --require-signature (NEXWATCH_REQUIRE_SIGNATURE) is set"
else
    fail "expected an abort mentioning --require-signature, got exit ${rc}"
fi

echo ""
echo "=== Summary ==="
echo "Passed: ${PASS_COUNT}"
echo "Failed: ${FAIL_COUNT}"

if [ "$FAIL_COUNT" -gt 0 ]; then
    exit 1
fi
exit 0
