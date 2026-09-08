package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tarEntry describes one file to write into a test tarball.
type tarEntry struct {
	name     string
	content  []byte
	typeflag byte // defaults to tar.TypeReg when zero
}

func buildTarGz(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     0o755,
			Size:     int64(len(e.content)),
			Typeflag: typeflag,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write(e.content); err != nil {
			t.Fatalf("write tar content: %v", err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

// zipEntry describes one file to write into a test zip archive.
type zipEntry struct {
	name    string
	content []byte
}

func buildZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	for _, e := range entries {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", e.name, err)
		}
		if _, err := w.Write(e.content); err != nil {
			t.Fatalf("write zip entry %q: %v", e.name, err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// testRoute is one path's canned response for newTestServer.
type testRoute struct {
	status int
	body   []byte
}

func newTestServer(t *testing.T, routes map[string]testRoute) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, ok := routes[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if route.status != 0 && route.status != http.StatusOK {
			w.WriteHeader(route.status)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(route.body)
	}))
	t.Cleanup(server.Close)
	return server
}

// fakeGPG is a GPGRunner test double.
type fakeGPG struct {
	available    bool
	verifyErr    error
	verifyCalled bool
}

func (f *fakeGPG) Available() bool { return f.available }

func (f *fakeGPG) Verify(gnupgHome string, keyData, sigData []byte, dataFile string) error {
	f.verifyCalled = true
	return f.verifyErr
}

// fakeExec is a CommandRunner test double standing in for actually running
// the extracted binary — the go-testing skill calls for a small mock at
// exactly this system-execution boundary rather than spawning a real
// subprocess in every table case.
type fakeExec struct {
	version string
	err     error
}

func (f *fakeExec) Output(ctx context.Context, path string, args ...string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []byte(f.version), nil
}

// baseUpdater returns an Updater with every seam faked, ready for a test to
// override individual fields.
func baseUpdater(server *httptest.Server, version string) *Updater {
	return &Updater{
		CurrentVersion: "0.9.0",
		HTTPClient:     server.Client(),
		GPG:            &fakeGPG{available: false},
		Exec:           &fakeExec{version: version},
		ExecutablePath: func() (string, error) { return "", errors.New("installBinary should not be reached in this test") },
	}
}

func TestApply_Success(t *testing.T) {
	tarball := buildTarGz(t, []tarEntry{{name: "nexwatch-agent", content: []byte("fake binary v0.9.1")}})
	asset := "nexwatch-agent_0.9.1_linux_amd64.tar.gz"
	sums := fmt.Sprintf("%s  %s\n", sha256Hex(tarball), asset)

	server := newTestServer(t, map[string]testRoute{
		"/v0.9.1/" + asset:             {body: tarball},
		"/v0.9.1/SHA256SUMS":           {body: []byte(sums)},
		"/v0.9.1/SHA256SUMS.asc":       {status: http.StatusNotFound},
		"/v0.9.1/" + asset + ".sha256": {status: http.StatusNotFound},
	})

	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "nexwatch-agent")
	if err := os.WriteFile(execPath, []byte("old binary"), 0o755); err != nil {
		t.Fatalf("seed exec path: %v", err)
	}

	u := baseUpdater(server, "0.9.1")
	u.ExecutablePath = func() (string, error) { return execPath, nil }

	result := u.Apply(context.Background(), Request{Version: "0.9.1", BaseURL: server.URL, OS: "linux", Arch: "amd64"})

	if !result.OK {
		t.Fatalf("Apply() OK = false, error = %q, want success", result.Error)
	}
	if result.Stage != StageDone {
		t.Errorf("Apply() Stage = %q, want %q", result.Stage, StageDone)
	}
	if result.FromVersion != "0.9.0" || result.ToVersion != "0.9.1" {
		t.Errorf("Apply() From/To = %s/%s, want 0.9.0/0.9.1", result.FromVersion, result.ToVersion)
	}

	installed, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(installed) != "fake binary v0.9.1" {
		t.Errorf("installed binary content = %q, want the extracted content", installed)
	}
	if _, err := os.Stat(execPath + ".previous"); err != nil {
		t.Errorf("expected a .previous backup of the old binary, stat error: %v", err)
	}
}

func TestApply_FallsBackToPerFileChecksumWhenSHA256SUMSMissing(t *testing.T) {
	tarball := buildTarGz(t, []tarEntry{{name: "nexwatch-agent", content: []byte("fake binary v0.9.1")}})
	asset := "nexwatch-agent_0.9.1_linux_amd64.tar.gz"

	server := newTestServer(t, map[string]testRoute{
		"/v0.9.1/" + asset:             {body: tarball},
		"/v0.9.1/SHA256SUMS":           {status: http.StatusNotFound},
		"/v0.9.1/" + asset + ".sha256": {body: []byte(sha256Hex(tarball) + "  " + asset + "\n")},
	})

	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "nexwatch-agent")
	if err := os.WriteFile(execPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed exec path: %v", err)
	}

	u := baseUpdater(server, "0.9.1")
	u.ExecutablePath = func() (string, error) { return execPath, nil }

	result := u.Apply(context.Background(), Request{Version: "0.9.1", BaseURL: server.URL, OS: "linux", Arch: "amd64"})

	if !result.OK {
		t.Fatalf("Apply() OK = false, error = %q, want success via per-file checksum fallback", result.Error)
	}
}

func TestApply_HashMismatch(t *testing.T) {
	tarball := buildTarGz(t, []tarEntry{{name: "nexwatch-agent", content: []byte("fake binary v0.9.1")}})
	asset := "nexwatch-agent_0.9.1_linux_amd64.tar.gz"
	// Deliberately wrong hash.
	sums := fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), asset)

	server := newTestServer(t, map[string]testRoute{
		"/v0.9.1/" + asset:             {body: tarball},
		"/v0.9.1/SHA256SUMS":           {body: []byte(sums)},
		"/v0.9.1/SHA256SUMS.asc":       {status: http.StatusNotFound},
		"/v0.9.1/" + asset + ".sha256": {status: http.StatusNotFound},
	})

	u := baseUpdater(server, "0.9.1")
	result := u.Apply(context.Background(), Request{Version: "0.9.1", BaseURL: server.URL, OS: "linux", Arch: "amd64"})

	if result.OK {
		t.Fatal("Apply() OK = true, want failure on checksum mismatch")
	}
	if result.Stage != StageFailed {
		t.Errorf("Apply() Stage = %q, want %q", result.Stage, StageFailed)
	}
	if !strings.Contains(result.Error, "checksum mismatch") {
		t.Errorf("Apply() Error = %q, want it to mention checksum mismatch", result.Error)
	}
}

func TestApply_BadSignatureIsAlwaysFatal(t *testing.T) {
	tarball := buildTarGz(t, []tarEntry{{name: "nexwatch-agent", content: []byte("fake binary v0.9.1")}})
	asset := "nexwatch-agent_0.9.1_linux_amd64.tar.gz"
	sums := fmt.Sprintf("%s  %s\n", sha256Hex(tarball), asset)

	server := newTestServer(t, map[string]testRoute{
		"/v0.9.1/" + asset:       {body: tarball},
		"/v0.9.1/SHA256SUMS":     {body: []byte(sums)},
		"/v0.9.1/SHA256SUMS.asc": {body: []byte("-----BEGIN PGP SIGNATURE-----\nfake\n-----END PGP SIGNATURE-----\n")},
		"/signing-key.asc":       {body: []byte("fake public key")},
	})

	gpg := &fakeGPG{available: true, verifyErr: errors.New("bad signature")}
	u := baseUpdater(server, "0.9.1")
	u.GPG = gpg
	u.SigningKeyURL = server.URL + "/signing-key.asc"
	// RequireSignature is deliberately false: a bad signature must fail
	// regardless, unlike a merely-missing one.
	u.RequireSignature = false

	result := u.Apply(context.Background(), Request{Version: "0.9.1", BaseURL: server.URL, OS: "linux", Arch: "amd64"})

	if result.OK {
		t.Fatal("Apply() OK = true, want failure on a bad signature even with require_signature=false")
	}
	if !gpg.verifyCalled {
		t.Error("expected GPG.Verify to be called")
	}
	if !strings.Contains(result.Error, "signature verification failed") {
		t.Errorf("Apply() Error = %q, want it to mention signature verification failure", result.Error)
	}
}

func TestApply_MissingSignatureWithRequireFlagFails(t *testing.T) {
	tarball := buildTarGz(t, []tarEntry{{name: "nexwatch-agent", content: []byte("fake binary v0.9.1")}})
	asset := "nexwatch-agent_0.9.1_linux_amd64.tar.gz"
	sums := fmt.Sprintf("%s  %s\n", sha256Hex(tarball), asset)

	server := newTestServer(t, map[string]testRoute{
		"/v0.9.1/" + asset:       {body: tarball},
		"/v0.9.1/SHA256SUMS":     {body: []byte(sums)},
		"/v0.9.1/SHA256SUMS.asc": {status: http.StatusNotFound},
	})

	u := baseUpdater(server, "0.9.1")
	u.RequireSignature = true

	result := u.Apply(context.Background(), Request{Version: "0.9.1", BaseURL: server.URL, OS: "linux", Arch: "amd64"})

	if result.OK {
		t.Fatal("Apply() OK = true, want failure when require_signature is set and no signature is published")
	}
	if !strings.Contains(result.Error, "update_require_signature") {
		t.Errorf("Apply() Error = %q, want it to mention update_require_signature", result.Error)
	}
}

func TestApply_MissingSignatureWithoutRequireFlagSucceeds(t *testing.T) {
	tarball := buildTarGz(t, []tarEntry{{name: "nexwatch-agent", content: []byte("fake binary v0.9.1")}})
	asset := "nexwatch-agent_0.9.1_linux_amd64.tar.gz"
	sums := fmt.Sprintf("%s  %s\n", sha256Hex(tarball), asset)

	server := newTestServer(t, map[string]testRoute{
		"/v0.9.1/" + asset:       {body: tarball},
		"/v0.9.1/SHA256SUMS":     {body: []byte(sums)},
		"/v0.9.1/SHA256SUMS.asc": {status: http.StatusNotFound},
	})

	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "nexwatch-agent")
	if err := os.WriteFile(execPath, []byte("old"), 0o755); err != nil {
		t.Fatalf("seed exec path: %v", err)
	}

	u := baseUpdater(server, "0.9.1")
	u.ExecutablePath = func() (string, error) { return execPath, nil }
	u.RequireSignature = false

	result := u.Apply(context.Background(), Request{Version: "0.9.1", BaseURL: server.URL, OS: "linux", Arch: "amd64"})

	if !result.OK {
		t.Fatalf("Apply() OK = false, error = %q, want success (signature optional, none published)", result.Error)
	}
}

func TestApply_VersionSanityMismatch(t *testing.T) {
	tarball := buildTarGz(t, []tarEntry{{name: "nexwatch-agent", content: []byte("fake binary")}})
	asset := "nexwatch-agent_0.9.1_linux_amd64.tar.gz"
	sums := fmt.Sprintf("%s  %s\n", sha256Hex(tarball), asset)

	server := newTestServer(t, map[string]testRoute{
		"/v0.9.1/" + asset:       {body: tarball},
		"/v0.9.1/SHA256SUMS":     {body: []byte(sums)},
		"/v0.9.1/SHA256SUMS.asc": {status: http.StatusNotFound},
	})

	// The extracted binary reports the WRONG version.
	u := baseUpdater(server, "0.9.0")
	result := u.Apply(context.Background(), Request{Version: "0.9.1", BaseURL: server.URL, OS: "linux", Arch: "amd64"})

	if result.OK {
		t.Fatal("Apply() OK = true, want failure on a version sanity mismatch")
	}
	if !strings.Contains(result.Error, "version mismatch") {
		t.Errorf("Apply() Error = %q, want it to mention a version mismatch", result.Error)
	}
}

func TestApply_TraversalEntryRejected(t *testing.T) {
	tarball := buildTarGz(t, []tarEntry{{name: "../../../../tmp/nexwatch-update-traversal-canary", content: []byte("evil payload")}})
	asset := "nexwatch-agent_0.9.1_linux_amd64.tar.gz"
	sums := fmt.Sprintf("%s  %s\n", sha256Hex(tarball), asset)

	server := newTestServer(t, map[string]testRoute{
		"/v0.9.1/" + asset:       {body: tarball},
		"/v0.9.1/SHA256SUMS":     {body: []byte(sums)},
		"/v0.9.1/SHA256SUMS.asc": {status: http.StatusNotFound},
	})

	u := baseUpdater(server, "0.9.1")
	result := u.Apply(context.Background(), Request{Version: "0.9.1", BaseURL: server.URL, OS: "linux", Arch: "amd64"})

	if result.OK {
		t.Fatal("Apply() OK = true, want failure when the archive has no legitimate nexwatch-agent entry")
	}
	if !strings.Contains(result.Error, "no \"nexwatch-agent\" entry found") {
		t.Errorf("Apply() Error = %q, want it to report no valid entry was found", result.Error)
	}
	if _, err := os.Stat("/tmp/nexwatch-update-traversal-canary"); err == nil {
		_ = os.Remove("/tmp/nexwatch-update-traversal-canary")
		t.Fatal("traversal entry escaped the extraction directory and was written to /tmp")
	}
}

func TestExtractBinary_RejectsNonRegularEntry(t *testing.T) {
	tarball := buildTarGz(t, []tarEntry{{name: "nexwatch-agent", content: nil, typeflag: tar.TypeSymlink}})
	tmpDir := t.TempDir()
	tarballPath := filepath.Join(tmpDir, "a.tar.gz")
	if err := os.WriteFile(tarballPath, tarball, 0o644); err != nil {
		t.Fatalf("write tarball: %v", err)
	}

	destDir := t.TempDir()
	if _, err := extractBinary(tarballPath, destDir, binaryEntryName); err == nil {
		t.Fatal("extractBinary() expected an error for a symlink entry, got nil")
	}
}

func TestApply_WindowsTargetUsesZipAsset(t *testing.T) {
	zipData := buildZip(t, []zipEntry{{name: "nexwatch-agent.exe", content: []byte("fake windows binary v0.9.1")}})
	asset := "nexwatch-agent_0.9.1_windows_amd64.zip"
	sums := fmt.Sprintf("%s  %s\n", sha256Hex(zipData), asset)

	server := newTestServer(t, map[string]testRoute{
		"/v0.9.1/" + asset:       {body: zipData},
		"/v0.9.1/SHA256SUMS":     {body: []byte(sums)},
		"/v0.9.1/SHA256SUMS.asc": {status: http.StatusNotFound},
	})

	tmpDir := t.TempDir()
	execPath := filepath.Join(tmpDir, "nexwatch-agent.exe")
	if err := os.WriteFile(execPath, []byte("old windows binary"), 0o755); err != nil {
		t.Fatalf("seed exec path: %v", err)
	}

	u := baseUpdater(server, "0.9.1")
	u.ExecutablePath = func() (string, error) { return execPath, nil }

	result := u.Apply(context.Background(), Request{Version: "0.9.1", BaseURL: server.URL, OS: "windows", Arch: "amd64"})

	if !result.OK {
		t.Fatalf("Apply() OK = false, error = %q, want success", result.Error)
	}
	if result.RestartRequired {
		t.Error("Apply() RestartRequired = true, want false on a normal (non-locked) install")
	}

	installed, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(installed) != "fake windows binary v0.9.1" {
		t.Errorf("installed binary content = %q, want the extracted content", installed)
	}
}

func TestExtractBinaryZip_RejectsTraversalEntry(t *testing.T) {
	zipData := buildZip(t, []zipEntry{{name: "../../../../tmp/nexwatch-update-zip-traversal-canary", content: []byte("evil payload")}})
	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "a.zip")
	if err := os.WriteFile(zipPath, zipData, 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	destDir := t.TempDir()
	if _, err := extractBinaryZip(zipPath, destDir, binaryEntryNameWindows); err == nil {
		t.Fatal("extractBinaryZip() expected an error when no legitimate entry is present, got nil")
	}
	if _, err := os.Stat("/tmp/nexwatch-update-zip-traversal-canary"); err == nil {
		_ = os.Remove("/tmp/nexwatch-update-zip-traversal-canary")
		t.Fatal("traversal entry escaped the extraction directory and was written to /tmp")
	}
}

func TestExtractBinaryZip_RejectsDirectoryEntry(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	header := &zip.FileHeader{Name: "nexwatch-agent.exe/", Method: zip.Store}
	header.SetMode(fs.ModeDir | 0o755)
	if _, err := zw.CreateHeader(header); err != nil {
		t.Fatalf("create directory entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}

	tmpDir := t.TempDir()
	zipPath := filepath.Join(tmpDir, "a.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}

	destDir := t.TempDir()
	if _, err := extractBinaryZip(zipPath, destDir, binaryEntryNameWindows); err == nil {
		t.Fatal("extractBinaryZip() expected an error for a directory entry, got nil")
	}
}

func TestExtractArchive_DispatchesOnExtension(t *testing.T) {
	tmpDir := t.TempDir()

	tarball := buildTarGz(t, []tarEntry{{name: "nexwatch-agent", content: []byte("tar content")}})
	tarPath := filepath.Join(tmpDir, "a.tar.gz")
	if err := os.WriteFile(tarPath, tarball, 0o644); err != nil {
		t.Fatalf("write tarball: %v", err)
	}
	if got, err := extractArchive(tarPath, t.TempDir(), binaryEntryName); err != nil {
		t.Fatalf("extractArchive(.tar.gz) error = %v", err)
	} else if content, _ := os.ReadFile(got); string(content) != "tar content" {
		t.Errorf("extractArchive(.tar.gz) content = %q, want %q", content, "tar content")
	}

	zipData := buildZip(t, []zipEntry{{name: "nexwatch-agent.exe", content: []byte("zip content")}})
	zipPath := filepath.Join(tmpDir, "a.zip")
	if err := os.WriteFile(zipPath, zipData, 0o644); err != nil {
		t.Fatalf("write zip: %v", err)
	}
	if got, err := extractArchive(zipPath, t.TempDir(), binaryEntryNameWindows); err != nil {
		t.Fatalf("extractArchive(.zip) error = %v", err)
	} else if content, _ := os.ReadFile(got); string(content) != "zip content" {
		t.Errorf("extractArchive(.zip) content = %q, want %q", content, "zip content")
	}
}

func TestInstallBinary_RollbackOnRenameFailure(t *testing.T) {
	execDir := t.TempDir()
	execPath := filepath.Join(execDir, "nexwatch-agent")
	original := []byte("original binary content")
	if err := os.WriteFile(execPath, original, 0o755); err != nil {
		t.Fatalf("seed exec path: %v", err)
	}

	// newPath lives in its own directory, made read-only after the file is
	// written. Renaming a file OUT of a directory requires write
	// permission on that directory (it must remove the source entry), so
	// the second rename (newPath -> execPath) fails with a permission
	// error even though the first rename (execPath -> execPath+".previous",
	// entirely within execDir) already succeeded — exercising
	// installBinary's rollback path.
	newDir := t.TempDir()
	newPath := filepath.Join(newDir, "nexwatch-agent-new")
	if err := os.WriteFile(newPath, []byte("new binary content"), 0o755); err != nil {
		t.Fatalf("seed new path: %v", err)
	}
	if err := os.Chmod(newDir, 0o555); err != nil {
		t.Fatalf("make newDir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(newDir, 0o755) }) // let t.TempDir() clean up newDir afterward.

	u := &Updater{ExecutablePath: func() (string, error) { return execPath, nil }}

	_, err := u.installBinary(newPath)
	if err == nil {
		t.Fatal("installBinary() expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("installBinary() error = %q, want it to mention a rollback", err.Error())
	}

	restored, readErr := os.ReadFile(execPath)
	if readErr != nil {
		t.Fatalf("read execPath after rollback: %v", readErr)
	}
	if !bytes.Equal(restored, original) {
		t.Errorf("execPath content after rollback = %q, want original content %q", restored, original)
	}
}

// withWindowsInstallTarget temporarily forces installBinary's
// runningOnWindows-gated fallback branch on regardless of the actual host
// OS, restoring the original value on test cleanup — the only way to
// exercise that branch on this repo's own non-Windows dev/CI machines.
func withWindowsInstallTarget(t *testing.T) {
	t.Helper()
	original := runningOnWindows
	runningOnWindows = true
	t.Cleanup(func() { runningOnWindows = original })
}

func TestInstallBinary_WindowsDelayedReplaceOnRenameFailure(t *testing.T) {
	withWindowsInstallTarget(t)

	execDir := t.TempDir()
	execPath := filepath.Join(execDir, "nexwatch-agent.exe")
	original := []byte("original windows binary")
	if err := os.WriteFile(execPath, original, 0o755); err != nil {
		t.Fatalf("seed exec path: %v", err)
	}

	newDir := t.TempDir()
	newPath := filepath.Join(newDir, "nexwatch-agent-new.exe")
	if err := os.WriteFile(newPath, []byte("new windows binary"), 0o755); err != nil {
		t.Fatalf("seed new path: %v", err)
	}
	if err := os.Chmod(newDir, 0o555); err != nil {
		t.Fatalf("make newDir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(newDir, 0o755) })

	var scheduledFrom, scheduledTo string
	originalSchedule := scheduleDelayedReplace
	scheduleDelayedReplace = func(from, to string) error {
		scheduledFrom, scheduledTo = from, to
		return nil // simulate a successful MoveFileEx(..., MOVEFILE_DELAY_UNTIL_REBOOT).
	}
	t.Cleanup(func() { scheduleDelayedReplace = originalSchedule })

	u := &Updater{ExecutablePath: func() (string, error) { return execPath, nil }}

	restartRequired, err := u.installBinary(newPath)
	if err != nil {
		t.Fatalf("installBinary() error = %v, want nil (delayed replace should succeed)", err)
	}
	if !restartRequired {
		t.Error("installBinary() restartRequired = false, want true")
	}
	if scheduledFrom != newPath || scheduledTo != execPath {
		t.Errorf("scheduleDelayedReplace called with (%q, %q), want (%q, %q)", scheduledFrom, scheduledTo, newPath, execPath)
	}

	// The old binary must still be reachable under its original name until
	// the scheduled replacement actually happens at next boot.
	restored, readErr := os.ReadFile(execPath)
	if readErr != nil {
		t.Fatalf("read execPath after scheduling: %v", readErr)
	}
	if !bytes.Equal(restored, original) {
		t.Errorf("execPath content after scheduling = %q, want the original (still-running) binary %q", restored, original)
	}
}

func TestInstallBinary_WindowsDelayedReplaceAlsoFailsFallsBackToRollbackError(t *testing.T) {
	withWindowsInstallTarget(t)

	execDir := t.TempDir()
	execPath := filepath.Join(execDir, "nexwatch-agent.exe")
	if err := os.WriteFile(execPath, []byte("original"), 0o755); err != nil {
		t.Fatalf("seed exec path: %v", err)
	}

	newDir := t.TempDir()
	newPath := filepath.Join(newDir, "nexwatch-agent-new.exe")
	if err := os.WriteFile(newPath, []byte("new"), 0o755); err != nil {
		t.Fatalf("seed new path: %v", err)
	}
	if err := os.Chmod(newDir, 0o555); err != nil {
		t.Fatalf("make newDir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(newDir, 0o755) })

	originalSchedule := scheduleDelayedReplace
	scheduleDelayedReplace = func(from, to string) error {
		return errors.New("MoveFileEx failed: access denied")
	}
	t.Cleanup(func() { scheduleDelayedReplace = originalSchedule })

	u := &Updater{ExecutablePath: func() (string, error) { return execPath, nil }}

	restartRequired, err := u.installBinary(newPath)
	if err == nil {
		t.Fatal("installBinary() expected an error when delayed replace also fails, got nil")
	}
	if restartRequired {
		t.Error("installBinary() restartRequired = true on a failure path, want false")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("installBinary() error = %q, want it to mention a rollback (delayed replace itself failed too)", err.Error())
	}
	if _, err := os.Stat(execPath + ".previous"); !os.IsNotExist(err) {
		t.Errorf("expected .previous backup to be consumed by rollback, stat error = %v", err)
	}
}

func TestVersionMatches(t *testing.T) {
	tests := []struct {
		name     string
		reported string
		want     string
		matches  bool
	}{
		{"exact match", "0.9.1", "0.9.1", true},
		{"v prefix on reported", "v0.9.1", "0.9.1", true},
		{"v prefix on want", "0.9.1", "v0.9.1", true},
		{"embedded in longer output", "nexwatch-agent 0.9.1", "0.9.1", true},
		{"mismatch", "0.9.0", "0.9.1", false},
		{"empty want never matches", "0.9.1", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionMatches(tt.reported, tt.want); got != tt.matches {
				t.Errorf("versionMatches(%q, %q) = %v, want %v", tt.reported, tt.want, got, tt.matches)
			}
		})
	}
}

func TestParseSHA256Sums(t *testing.T) {
	data := []byte("aaaa  file-a.tar.gz\nbbbb *file-b.tar.gz\n\nzzzz  unrelated.txt\n")

	tests := []struct {
		name     string
		filename string
		wantHash string
		wantOK   bool
	}{
		{"two-space separator", "file-a.tar.gz", "aaaa", true},
		{"binary-mode asterisk prefix", "file-b.tar.gz", "bbbb", true},
		{"not present", "missing.tar.gz", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, ok := parseSHA256Sums(data, tt.filename)
			if ok != tt.wantOK || hash != tt.wantHash {
				t.Errorf("parseSHA256Sums() = (%q, %v), want (%q, %v)", hash, ok, tt.wantHash, tt.wantOK)
			}
		})
	}
}

func TestParsePerFileSHA256(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		wantHash string
		wantOK   bool
	}{
		{"hash only", "abcd1234\n", "abcd1234", true},
		{"hash and filename", "abcd1234  some-file.tar.gz\n", "abcd1234", true},
		{"empty", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, ok := parsePerFileSHA256([]byte(tt.data))
			if ok != tt.wantOK || hash != tt.wantHash {
				t.Errorf("parsePerFileSHA256(%q) = (%q, %v), want (%q, %v)", tt.data, hash, ok, tt.wantHash, tt.wantOK)
			}
		})
	}
}
