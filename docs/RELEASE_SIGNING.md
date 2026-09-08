# Release Signing

NexWatch releases are checksummed on every build. When a project GPG key is
configured, the checksum file is also signed, and both published container
images are signed keylessly via Sigstore/cosign. This document explains how
to set up signing for maintainers, and how to verify a release as a user.

## What gets signed

For every tag push (`v*`), the `release.yml` workflow:

1. Builds `nexwatch-hub` and `nexwatch-agent` for `linux/darwin` × `amd64/arm64`,
   packages each as a `.tar.gz`, and writes a per-file `.tar.gz.sha256`.
2. Aggregates every tarball's checksum into one `SHA256SUMS` file.
3. If a signing key is configured (see below), produces a detached, ASCII-armored
   signature `SHA256SUMS.asc` over `SHA256SUMS` and attaches both to the GitHub
   Release. If no key is configured, the release is still published — unsigned —
   with a visible warning in the workflow log.
4. Builds and pushes multi-arch `nexwatch-hub` and `nexwatch-agent` container
   images to `ghcr.io/cognidevai`, then signs each image **by digest** with
   `cosign sign --yes`, using GitHub's OIDC identity (no key material or
   secrets involved — "keyless" signing).

## Maintainer setup: the release GPG key

Signing `SHA256SUMS` requires a GPG key added as two repository secrets.
Image signing needs no setup — it uses GitHub OIDC automatically for every
release build.

### 1. Generate a dedicated release key

Do this once, on a maintainer's machine, and keep the private key and its
passphrase somewhere safe (a password manager or hardware token). Do not
reuse a personal GPG key.

```bash
gpg --full-generate-key
# Choose: (9) ECC (sign only), curve Curve 25519, key does not expire (or a
# long expiry you're prepared to rotate), name "NexWatch Release Signing",
# email a maintained address (e.g. a team alias), and a strong passphrase.
```

Find the new key's ID:

```bash
gpg --list-secret-keys --keyid-format=long
```

### 2. Export the private key for GitHub Actions

```bash
gpg --armor --export-secret-keys <KEY_ID> > private-signing-key.asc
```

Add its contents as the `GPG_PRIVATE_KEY` repository secret (Settings →
Secrets and variables → Actions → New repository secret), and add the key's
passphrase as `GPG_PASSPHRASE`. Then delete `private-signing-key.asc` from
disk — it must never be committed.

### 3. Publish the public key

Export the public key and commit it to the repository so the installer and
users can fetch it without trusting GitHub's release assets themselves:

```bash
gpg --armor --export <KEY_ID> > scripts/release-signing-key.asc
git add scripts/release-signing-key.asc
git commit -m "chore: add release signing public key"
```

Until this file exists, `scripts/install-agent.sh` and manual verification
fall back to a warning (or, with `--require-signature`, they abort) instead
of failing — the installer never assumes the key file is present.

Once both secrets are set and the public key is committed, the next tagged
release will be signed automatically.

## Verifying a release manually

### Checksums and signature

```bash
curl -fsSLO https://github.com/CogniDevAI/nexwatch/releases/download/vX.Y.Z/SHA256SUMS
curl -fsSLO https://github.com/CogniDevAI/nexwatch/releases/download/vX.Y.Z/SHA256SUMS.asc
curl -fsSLO https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/release-signing-key.asc

gpg --import release-signing-key.asc
gpg --verify SHA256SUMS.asc SHA256SUMS   # must print "Good signature"

# Then verify your downloaded tarball against the signed checksums:
sha256sum --ignore-missing -c SHA256SUMS
```

### Container images (cosign, keyless)

Requires [cosign](https://docs.sigstore.dev/system_config/installation/) v2+.

```bash
cosign verify \
  --certificate-identity-regexp "^https://github.com/CogniDevAI/nexwatch/.github/workflows/release.yml@refs/tags/v.*$" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/cognidevai/nexwatch-hub:X.Y.Z

cosign verify \
  --certificate-identity-regexp "^https://github.com/CogniDevAI/nexwatch/.github/workflows/release.yml@refs/tags/v.*$" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/cognidevai/nexwatch-agent:X.Y.Z
```

A successful verification prints the signing certificate's identity (the
`release.yml` workflow, running from a `v*` tag) and its transparency log
entry in the public [Rekor](https://docs.sigstore.dev/logging/overview/) log —
there is nothing to install or trust beyond the GitHub Actions OIDC issuer
itself.

## Threat model notes

- The per-file `.tar.gz.sha256` checksums are generated and served from the
  same GitHub Release as the binaries, so they only protect against
  transport corruption, not a compromised release — anyone who could tamper
  with the release could tamper with both files identically.
- `SHA256SUMS.asc` protects against that case too, as long as the private
  key stays out of the repository and CI logs, and the public key
  (`scripts/release-signing-key.asc`) is fetched from a channel the attacker
  doesn't control (the repository's `main` branch, not the release itself).
- Cosign's keyless signing binds the signature to the exact GitHub Actions
  workflow run (repository, workflow file, and ref) via a short-lived
  OIDC-issued certificate — there's no long-lived private key to steal, but
  verification does depend on trusting GitHub's OIDC issuer.
