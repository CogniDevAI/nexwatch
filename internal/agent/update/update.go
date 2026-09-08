// Package update implements the agent's self-update flow: given a target
// version, it downloads the matching release tarball, verifies its
// checksum (mandatory) and GPG signature (when published), extracts and
// sanity-checks the new binary, then atomically replaces the currently
// running executable. It mirrors — and is verified independently of —
// scripts/install-agent.sh's own download/verify logic, since the two
// paths (fresh install vs. hub-initiated self-update) must both refuse a
// tampered or corrupt release.
package update

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Update stage names, echoed back to the hub as protocol.CommandResponsePayload.Stage.
const (
	StageStarted     = "started"
	StageDownloading = "downloading"
	StageVerifying   = "verifying"
	StageInstalling  = "installing"
	StageRestarting  = "restarting"
	StageFailed      = "failed"
	StageDone        = "done"
	// StageRestartRequired is reported instead of StageRestarting when
	// Result.RestartRequired is true (Windows only — see that field's doc
	// comment): the download/verify/install succeeded, but the binary swap
	// itself is deferred to the next host restart rather than taking
	// effect via an immediate re-exec.
	StageRestartRequired = "restart_required"
)

// Defaults for an Updater constructed via New.
const (
	DefaultHTTPTimeout     = 30 * time.Second
	DefaultMaxDownloadSize = 200 * 1024 * 1024 // 200 MiB
	// DefaultSigningKeyURL matches scripts/install-agent.sh's own default,
	// so a self-update and a fresh install trust the same key by default.
	DefaultSigningKeyURL = "https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/release-signing-key.asc"
	// binaryEntryName is the exact (and only) archive entry extraction
	// will ever accept for a non-Windows target, matching the flat
	// single-file layout .github/workflows/release.yml packages
	// ("tar -czf ... nexwatch-agent"). binaryEntryNameWindows is the same
	// for a Windows target's .zip asset ("nexwatch-agent.exe").
	binaryEntryName        = "nexwatch-agent"
	binaryEntryNameWindows = "nexwatch-agent.exe"
)

// Request describes the update to apply — the same shape the hub sends as
// a protocol.UpdatePayload inside an "update" COMMAND's Args.
type Request struct {
	// Version is the target agent version without a leading "v" (e.g. "0.9.1").
	Version string
	// BaseURL is the release download base (the "agent_release_base_url"
	// hub setting). The full asset URL is "<BaseURL>/v<Version>/<asset>".
	BaseURL string
	// OS is the target runtime.GOOS (e.g. "linux", "darwin").
	OS string
	// Arch is the target runtime.GOARCH (e.g. "amd64", "arm64").
	Arch string
}

// Result is the structured outcome of an Apply call.
type Result struct {
	Stage       string
	OK          bool
	Error       string
	FromVersion string
	ToVersion   string
	// RestartRequired is set on an OK Windows result when the new binary
	// could not be swapped into place immediately (the running .exe was
	// locked without FILE_SHARE_DELETE — see installBinary's Windows
	// fallback) and was instead scheduled via MoveFileEx with
	// MOVEFILE_DELAY_UNTIL_REBOOT: the update completes only after the
	// host is rebooted or the service is otherwise stopped and restarted,
	// not on the immediate re-exec/exit(0) the non-Windows path relies on.
	RestartRequired bool
}

// HTTPDoer performs HTTP requests. Satisfied by *http.Client; a test
// substitutes a client pointed at an httptest.Server.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// GPGRunner runs the subset of gpg operations needed to verify a detached
// signature against an imported public key. Available lets Apply tell
// "gpg is missing" (skip, unless signatures are required) apart from
// "the signature itself failed to verify" (always a hard failure).
type GPGRunner interface {
	Available() bool
	Verify(gnupgHome string, keyData, sigData []byte, dataFile string) error
}

// CommandRunner runs an external command and returns its combined output.
// Used only to sanity-run the freshly extracted binary as
// "<path> --version" before it is installed.
type CommandRunner interface {
	Output(ctx context.Context, path string, args ...string) ([]byte, error)
}

// Updater applies self-updates. Zero-value fields fall back to sensible
// defaults (see httpTimeout/maxDownloadSize/signingKeyURL) except the three
// seams (HTTPClient/GPG/Exec/ExecutablePath), which New wires to real
// implementations — construct one directly (as tests do) to inject fakes.
type Updater struct {
	// CurrentVersion is the running agent's own version (main.version),
	// reported back as Result.FromVersion.
	CurrentVersion string

	// RequireSignature, when true, fails the update unless a valid GPG
	// signature over SHA256SUMS was verified — mirrors
	// scripts/install-agent.sh's --require-signature / NEXWATCH_REQUIRE_SIGNATURE.
	RequireSignature bool
	// SigningKeyURL overrides DefaultSigningKeyURL. Ignored when SigningKeyFile is set.
	SigningKeyURL string
	// SigningKeyFile, when set, reads the signing public key from a local
	// file instead of fetching SigningKeyURL.
	SigningKeyFile string

	// HTTPTimeout bounds each individual HTTP request (defaults to DefaultHTTPTimeout).
	HTTPTimeout time.Duration
	// MaxDownloadSize caps the release tarball's size (defaults to DefaultMaxDownloadSize).
	MaxDownloadSize int64

	// OnStage, if set, is called before each stage begins (StageDownloading,
	// StageVerifying, StageInstalling) so the caller can stream progress
	// (e.g. cmd/agent/main.go sending COMMAND_RESPONSE messages).
	OnStage func(stage string)

	// Seams — New wires these to real implementations.
	HTTPClient     HTTPDoer
	GPG            GPGRunner
	Exec           CommandRunner
	ExecutablePath func() (string, error)
}

// New returns an Updater wired to real HTTP, gpg, and exec implementations
// for currentVersion (the running agent's own main.version).
func New(currentVersion string) *Updater {
	return &Updater{
		CurrentVersion: currentVersion,
		HTTPClient:     &http.Client{Timeout: DefaultHTTPTimeout},
		GPG:            defaultGPGRunner{},
		Exec:           defaultCommandRunner{},
		ExecutablePath: defaultExecutablePath,
	}
}

// Apply downloads, verifies, and installs the release described by req,
// replacing the currently running executable on success. It never panics
// on a malformed or hostile response — every failure mode returns a
// Result with Stage=StageFailed and a human-readable Error instead.
func (u *Updater) Apply(ctx context.Context, req Request) Result {
	result := Result{FromVersion: u.CurrentVersion, ToVersion: req.Version}

	fail := func(format string, args ...any) Result {
		result.Stage = StageFailed
		result.OK = false
		result.Error = fmt.Sprintf(format, args...)
		return result
	}

	if req.Version == "" {
		return fail("update: version is required")
	}
	if req.OS == "" || req.Arch == "" {
		return fail("update: target os/arch is required")
	}
	if req.BaseURL == "" {
		return fail("update: base_url is required")
	}

	tmpDir, err := os.MkdirTemp("", "nexwatch-agent-update-*")
	if err != nil {
		return fail("create temp dir: %s", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// A Windows target ships as a .zip (the platform's native archive
	// format, and the one scripts/install-agent.ps1 and Get-FileHash work
	// with natively) containing "nexwatch-agent.exe"; every other target
	// keeps the existing .tar.gz containing the extension-less
	// "nexwatch-agent" — see extractArchive.
	assetExt := "tar.gz"
	entryName := binaryEntryName
	if req.OS == "windows" {
		assetExt = "zip"
		entryName = binaryEntryNameWindows
	}
	assetName := fmt.Sprintf("nexwatch-agent_%s_%s_%s.%s", req.Version, req.OS, req.Arch, assetExt)
	releaseBase := strings.TrimRight(req.BaseURL, "/") + "/v" + req.Version
	tarballPath := filepath.Join(tmpDir, assetName)

	u.report(StageDownloading)
	if _, err := u.fetchToFile(ctx, releaseBase+"/"+assetName, tarballPath); err != nil {
		return fail("download %s: %s", assetName, err)
	}

	u.report(StageVerifying)
	if err := u.verify(ctx, tmpDir, releaseBase, assetName, tarballPath); err != nil {
		return fail("%s", err)
	}

	u.report(StageInstalling)
	extractedPath, err := extractArchive(tarballPath, tmpDir, entryName)
	if err != nil {
		return fail("extract: %s", err)
	}

	reportedVersion, err := u.sanityCheckVersion(ctx, extractedPath)
	if err != nil {
		return fail("sanity check of extracted binary: %s", err)
	}
	if !versionMatches(reportedVersion, req.Version) {
		return fail("version mismatch: extracted binary reports %q, wanted %q", reportedVersion, req.Version)
	}

	restartRequired, err := u.installBinary(extractedPath)
	if err != nil {
		return fail("install: %s", err)
	}

	result.Stage = StageDone
	result.OK = true
	result.RestartRequired = restartRequired
	return result
}

func (u *Updater) report(stage string) {
	if u.OnStage != nil {
		u.OnStage(stage)
	}
}

func (u *Updater) httpTimeout() time.Duration {
	if u.HTTPTimeout > 0 {
		return u.HTTPTimeout
	}
	return DefaultHTTPTimeout
}

func (u *Updater) maxDownloadSize() int64 {
	if u.MaxDownloadSize > 0 {
		return u.MaxDownloadSize
	}
	return DefaultMaxDownloadSize
}

func (u *Updater) signingKeyURL() string {
	if u.SigningKeyURL != "" {
		return u.SigningKeyURL
	}
	return DefaultSigningKeyURL
}

// smallFileLimit caps how large a checksums/signature/key file fetch is
// allowed to be — plenty for any of the three, and small enough that a
// malicious or misconfigured server can't use them to exhaust memory.
const smallFileLimit = 1 << 20 // 1 MiB

// fetchToFile GETs url and streams the response body to destPath, enforcing
// u.maxDownloadSize(). Returns the HTTP status code even on error, so
// callers can distinguish "not found" from a transport failure.
func (u *Updater) fetchToFile(ctx context.Context, url, destPath string) (int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, u.httpTimeout())
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := u.HTTPClient.Do(httpReq)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, url)
	}

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return resp.StatusCode, err
	}
	defer func() { _ = out.Close() }()

	limit := u.maxDownloadSize()
	n, err := io.Copy(out, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return resp.StatusCode, err
	}
	if n > limit {
		return resp.StatusCode, fmt.Errorf("response exceeds max download size of %d bytes", limit)
	}
	return resp.StatusCode, nil
}

// fetchBytes GETs url and returns its body, capped at smallFileLimit — used
// for SHA256SUMS, SHA256SUMS.asc, and the signing public key, none of which
// are ever expected to approach that size.
func (u *Updater) fetchBytes(ctx context.Context, url string) ([]byte, int, error) {
	reqCtx, cancel := context.WithTimeout(ctx, u.httpTimeout())
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := u.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, smallFileLimit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if len(data) > smallFileLimit {
		return nil, resp.StatusCode, fmt.Errorf("response exceeds %d bytes", smallFileLimit)
	}
	return data, resp.StatusCode, nil
}

// verify checks tarballPath's SHA-256 against SHA256SUMS (falling back to
// the per-file "<asset>.sha256" when SHA256SUMS itself isn't published),
// then attempts GPG signature verification when a SHA256SUMS.asc is
// published. A published signature that fails to verify is always a hard
// error — mirroring scripts/install-agent.sh's verify_release_signature,
// which never treats a bad signature as "skip".
func (u *Updater) verify(ctx context.Context, tmpDir, releaseBase, assetName, tarballPath string) error {
	sumsData, sumsStatus, sumsErr := u.fetchBytes(ctx, releaseBase+"/SHA256SUMS")
	haveSums := sumsErr == nil && sumsStatus == http.StatusOK

	var expectedHash string
	if haveSums {
		expected, ok := parseSHA256Sums(sumsData, assetName)
		if !ok {
			return fmt.Errorf("no checksum entry for %s in SHA256SUMS", assetName)
		}
		expectedHash = expected
	} else {
		perFile, status, err := u.fetchBytes(ctx, releaseBase+"/"+assetName+".sha256")
		if err != nil || status != http.StatusOK {
			return fmt.Errorf("checksum unavailable: no SHA256SUMS and no %s.sha256 published", assetName)
		}
		expected, ok := parsePerFileSHA256(perFile)
		if !ok {
			return fmt.Errorf("malformed checksum file %s.sha256", assetName)
		}
		expectedHash = expected
	}

	actualHash, err := sha256File(tarballPath)
	if err != nil {
		return fmt.Errorf("hash tarball: %w", err)
	}
	if !strings.EqualFold(actualHash, expectedHash) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	sigVerified := false
	if haveSums {
		verified, err := u.attemptSignatureVerification(ctx, tmpDir, sumsData, releaseBase)
		if err != nil {
			return err // a published signature that failed to verify — never a skip.
		}
		sigVerified = verified
	}

	if u.RequireSignature && !sigVerified {
		return fmt.Errorf("update_require_signature is set but no valid release signature could be verified")
	}

	return nil
}

// attemptSignatureVerification returns (true, nil) when SHA256SUMS.asc was
// verified successfully, (false, nil) when verification was skipped (no
// .asc published, gpg unavailable, or the signing key couldn't be
// obtained), and (false, err) only when a signature was actually attempted
// and failed — a distinct, always-fatal outcome regardless of
// RequireSignature.
func (u *Updater) attemptSignatureVerification(ctx context.Context, tmpDir string, sumsData []byte, releaseBase string) (bool, error) {
	sigData, sigStatus, sigErr := u.fetchBytes(ctx, releaseBase+"/SHA256SUMS.asc")
	if sigErr != nil || sigStatus != http.StatusOK {
		return false, nil // no signature published.
	}
	if u.GPG == nil || !u.GPG.Available() {
		return false, nil // can't verify — gpg missing.
	}

	var keyData []byte
	var err error
	if u.SigningKeyFile != "" {
		keyData, err = os.ReadFile(u.SigningKeyFile)
		if err != nil {
			return false, nil // local key unreadable — treat as "can't verify", not a failure.
		}
	} else {
		keyData, _, err = u.fetchBytes(ctx, u.signingKeyURL())
		if err != nil {
			return false, nil // key fetch failed — same skip treatment as install-agent.sh.
		}
	}

	dataFile := filepath.Join(tmpDir, "SHA256SUMS")
	if err := os.WriteFile(dataFile, sumsData, 0o600); err != nil {
		return false, fmt.Errorf("write SHA256SUMS for verification: %w", err)
	}
	gnupgHome := filepath.Join(tmpDir, "gnupg")
	if err := os.MkdirAll(gnupgHome, 0o700); err != nil {
		return false, fmt.Errorf("create GNUPGHOME: %w", err)
	}

	if err := u.GPG.Verify(gnupgHome, keyData, sigData, dataFile); err != nil {
		return false, fmt.Errorf("signature verification failed: %w", err)
	}
	return true, nil
}

// parseSHA256Sums parses a "SHA256SUMS" aggregate file (lines shaped
// "<hash>  <filename>" or "<hash> *<filename>", as produced by `sha256sum`)
// and returns the hash recorded for filename.
func parseSHA256Sums(data []byte, filename string) (string, bool) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == filename {
			return fields[0], true
		}
	}
	return "", false
}

// parsePerFileSHA256 parses a "<asset>.sha256" file, which contains either
// just the hash or "<hash>  <filename>" — see scripts/install-agent.sh's
// own verify_checksum for the same shape.
func parsePerFileSHA256(data []byte) (string, bool) {
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", false
	}
	return fields[0], true
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractArchive dispatches to extractBinary (.tar.gz) or extractBinaryZip
// (.zip, used for a Windows target — see Apply) based on archivePath's
// extension, extracting entryName into destDir.
func extractArchive(archivePath, destDir, entryName string) (string, error) {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractBinaryZip(archivePath, destDir, entryName)
	}
	return extractBinary(archivePath, destDir, entryName)
}

// extractBinary extracts exactly one entry named entryName from the
// tarball at tarballPath into destDir, and rejects everything else —
// including a maliciously named entry (e.g. "../../etc/passwd") — by
// skipping any entry whose cleaned name doesn't exactly match, and by
// always writing to a fixed destination path (filepath.Join(destDir,
// entryName)) that is never derived from the entry's own name, so a
// hostile name can never escape destDir even if this allowlist check were
// somehow bypassed. Only a regular file is accepted (never a symlink or
// directory), and the extracted file is made executable (0o700) before it
// is handed to the version sanity check.
func extractBinary(tarballPath, destDir, entryName string) (string, error) {
	f, err := os.Open(tarballPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	destPath := filepath.Join(destDir, entryName)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return "", fmt.Errorf("no %q entry found in archive", entryName)
		}
		if err != nil {
			return "", err
		}

		// tar entry names always use "/" regardless of host OS.
		cleanName := path.Clean(hdr.Name)
		if cleanName != entryName && cleanName != "./"+entryName {
			continue // not the entry we want — including any traversal attempt.
		}
		if hdr.Typeflag != tar.TypeReg {
			return "", fmt.Errorf("entry %q is not a regular file", hdr.Name)
		}

		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700) //nolint:gosec // must be executable: it is run for the "--version" sanity check below and then installed as the running agent binary.
		if err != nil {
			return "", err
		}
		if _, err := io.CopyN(out, tr, hdr.Size); err != nil && err != io.EOF {
			_ = out.Close()
			return "", fmt.Errorf("write extracted binary: %w", err)
		}
		if err := out.Close(); err != nil {
			return "", err
		}
		if err := os.Chmod(destPath, 0o700); err != nil { //nolint:gosec // see the OpenFile call above — must stay executable.
			return "", err
		}
		return destPath, nil
	}
}

// extractBinaryZip is extractBinary's .zip equivalent (used for a Windows
// target's nexwatch-agent_<ver>_windows_<arch>.zip asset), with the same
// single-entry allowlist and fixed-destination-path traversal guard: only
// an entry whose cleaned name exactly matches entryName is extracted, and
// it is always written to filepath.Join(destDir, entryName) regardless of
// what the zip entry's own name claims.
func extractBinaryZip(zipPath, destDir, entryName string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("zip: %w", err)
	}
	defer func() { _ = zr.Close() }()

	destPath := filepath.Join(destDir, entryName)

	for _, entry := range zr.File {
		cleanName := path.Clean(filepath.ToSlash(entry.Name))
		if cleanName != entryName && cleanName != "./"+entryName {
			continue // not the entry we want — including any traversal attempt.
		}
		if entry.FileInfo().IsDir() {
			return "", fmt.Errorf("entry %q is a directory, not a regular file", entry.Name)
		}

		rc, err := entry.Open()
		if err != nil {
			return "", fmt.Errorf("open zip entry %q: %w", entry.Name, err)
		}

		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700) //nolint:gosec // must be executable — see extractBinary's identical comment.
		if err != nil {
			_ = rc.Close()
			return "", err
		}
		_, copyErr := io.Copy(out, rc) //nolint:gosec // bounded by the zip entry's own declared size, itself bounded by fetchToFile's maxDownloadSize on the archive.
		closeErr := out.Close()
		_ = rc.Close()
		if copyErr != nil {
			return "", fmt.Errorf("write extracted binary: %w", copyErr)
		}
		if closeErr != nil {
			return "", closeErr
		}
		if err := os.Chmod(destPath, 0o700); err != nil { //nolint:gosec // see the OpenFile call above — must stay executable.
			return "", err
		}
		return destPath, nil
	}

	return "", fmt.Errorf("no %q entry found in archive", entryName)
}

// sanityCheckVersion runs "<path> --version" via u.Exec and returns its
// trimmed output.
func (u *Updater) sanityCheckVersion(ctx context.Context, path string) (string, error) {
	out, err := u.Exec.Output(ctx, path, "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// versionMatches reports whether reported (the extracted binary's
// "--version" output) matches want (the requested version), tolerant of a
// "v" prefix on either side and of the flag printing more than just the
// bare version (e.g. "nexwatch-agent 0.9.1").
func versionMatches(reported, want string) bool {
	r := strings.TrimPrefix(strings.TrimSpace(reported), "v")
	w := strings.TrimPrefix(strings.TrimSpace(want), "v")
	if w == "" {
		return false
	}
	return r == w || strings.Contains(r, w)
}

// installBinary atomically replaces the currently running executable
// (resolved via u.ExecutablePath, defaulting to os.Executable() through
// symlinks) with newPath: rename current -> "<exe>.previous" (preserving a
// rollback copy), then rename newPath -> "<exe>", preserving the original
// file's mode. If the second rename fails, it restores the previous binary
// from the backup before returning the error, so a failed install never
// leaves the agent without any executable at all — except on the one
// documented Windows fallback below, where a locked target is instead
// scheduled for a delayed replacement and restartRequired is reported true
// with a nil error.
//
// On Windows, the second rename can fail with a sharing violation even
// though the first (renaming the running executable itself away) succeeds
// — Go's runtime opens the running image with FILE_SHARE_DELETE, but
// nothing guarantees a security product, an open handle from the SCM, or
// antivirus scanning hasn't transiently locked the destination name.  When
// that happens and req.OS is windows, installBinary falls back to
// scheduleDelayedReplace (update_windows.go's real
// windows.MoveFileEx(..., MOVEFILE_DELAY_UNTIL_REBOOT), a no-op stub
// everywhere else), which asks Windows to complete the rename the next
// time the system boots, and reports restartRequired instead of failing
// the update outright.
func (u *Updater) installBinary(newPath string) (restartRequired bool, err error) {
	execPath, err := u.resolveExecutablePath()
	if err != nil {
		return false, fmt.Errorf("resolve running executable: %w", err)
	}

	mode := os.FileMode(0o755)
	if info, statErr := os.Stat(execPath); statErr == nil {
		mode = info.Mode()
	}
	if err := os.Chmod(newPath, mode); err != nil {
		return false, fmt.Errorf("preserve executable mode: %w", err)
	}

	previousPath := execPath + ".previous"
	_ = os.Remove(previousPath) // drop any stale backup from an earlier update.

	if err := os.Rename(execPath, previousPath); err != nil {
		return false, fmt.Errorf("back up current binary: %w", err)
	}

	if err := os.Rename(newPath, execPath); err != nil {
		if runningOnWindows {
			if scheduleErr := scheduleDelayedReplace(newPath, execPath); scheduleErr == nil {
				// The rename itself never happened, so execPath still
				// holds nothing (it was moved to previousPath above) and
				// newPath is scheduled to take its place on next boot.
				// Restore the running binary's own name so the agent
				// (and any process supervisor) keeps finding it there
				// until then.
				if restoreErr := os.Rename(previousPath, execPath); restoreErr != nil {
					return false, fmt.Errorf("install failed (%s), delayed replacement scheduled, but restoring the running binary also failed (%s) — %s may be missing, restore manually from %s", err, restoreErr, execPath, previousPath)
				}
				return true, nil
			}
		}
		if restoreErr := os.Rename(previousPath, execPath); restoreErr != nil {
			return false, fmt.Errorf("install failed (%s) AND rollback failed (%s) — %s may be missing, restore manually from %s", err, restoreErr, execPath, previousPath)
		}
		return false, fmt.Errorf("install failed, rolled back to the previous binary: %w", err)
	}

	return false, nil
}

// runningOnWindows gates installBinary's delayed-replacement fallback. A
// var (rather than a direct runtime.GOOS comparison) so update_test.go can
// force the Windows-only branch on this repo's own non-Windows dev/CI
// machines and exercise it with a fake scheduleDelayedReplace.
var runningOnWindows = runtime.GOOS == "windows"

// scheduleDelayedReplace asks the OS to move newPath over execPath the
// next time the system starts, for platforms where a running executable's
// file name can be locked against an in-place rename (see installBinary's
// Windows fallback). The default implementation always fails — there is
// no cross-platform equivalent, and every non-Windows installBinary path
// is expected to succeed outright or roll back, never reach this seam.
// update_windows.go overrides it with the real
// windows.MoveFileEx(..., MOVEFILE_DELAY_UNTIL_REBOOT) call.
var scheduleDelayedReplace = func(newPath, execPath string) error {
	return fmt.Errorf("delayed replacement on next start is not supported on %s", runtime.GOOS)
}

func (u *Updater) resolveExecutablePath() (string, error) {
	if u.ExecutablePath != nil {
		return u.ExecutablePath()
	}
	return defaultExecutablePath()
}

func defaultExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

// defaultGPGRunner shells out to the system "gpg" binary, the same tool
// scripts/install-agent.sh's own verify_release_signature uses.
type defaultGPGRunner struct{}

func (defaultGPGRunner) Available() bool {
	_, err := exec.LookPath("gpg")
	return err == nil
}

func (defaultGPGRunner) Verify(gnupgHome string, keyData, sigData []byte, dataFile string) error {
	// gnupgHome is always a subdirectory of a temp dir this package itself
	// created (Updater.attemptSignatureVerification), never derived from
	// network or request input, so keyFile/sigFile below can't escape it —
	// gosec's taint tracker flags the write anyway since gnupgHome is a
	// function parameter, the same false-positive shape already accepted
	// for G304 in .golangci.yml.
	keyFile := filepath.Join(gnupgHome, "key.asc")
	if err := os.WriteFile(keyFile, keyData, 0o600); err != nil { //nolint:gosec // see comment above.
		return fmt.Errorf("write signing key: %w", err)
	}
	sigFile := filepath.Join(gnupgHome, "SHA256SUMS.asc")
	if err := os.WriteFile(sigFile, sigData, 0o600); err != nil { //nolint:gosec // see comment above.
		return fmt.Errorf("write signature: %w", err)
	}

	env := append(os.Environ(), "GNUPGHOME="+gnupgHome)

	importCmd := exec.Command("gpg", "--batch", "--quiet", "--import", keyFile)
	importCmd.Env = env
	if out, err := importCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("import signing key: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	verifyCmd := exec.Command("gpg", "--batch", "--verify", sigFile, dataFile)
	verifyCmd.Env = env
	if out, err := verifyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("gpg verify: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// defaultCommandRunner runs the extracted binary for real via os/exec.
type defaultCommandRunner struct{}

func (defaultCommandRunner) Output(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	return cmd.CombinedOutput()
}
