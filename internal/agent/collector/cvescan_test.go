package collector

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// ─── Fixtures ────────────────────────────────────────────────────────────

// trivyFixture is a realistic Trivy 0.5x `--format json` rootfs/image
// report, hand-written from the documented schema: top-level
// SchemaVersion/ArtifactName/ArtifactType/Metadata/Results, each Result
// carrying a Target string and a Vulnerabilities array.
const trivyFixture = `{
  "SchemaVersion": 2,
  "ArtifactName": "/",
  "ArtifactType": "rootfs",
  "Metadata": {
    "OS": { "Family": "ubuntu", "Name": "22.04" }
  },
  "Results": [
    {
      "Target": "Ubuntu 22.04 (ubuntu)",
      "Class": "os-pkgs",
      "Type": "ubuntu",
      "Vulnerabilities": [
        {
          "VulnerabilityID": "CVE-2023-0001",
          "PkgName": "openssl",
          "InstalledVersion": "3.0.2-0ubuntu1.10",
          "FixedVersion": "3.0.2-0ubuntu1.12",
          "Severity": "CRITICAL",
          "Title": "openssl: heap buffer overflow"
        },
        {
          "VulnerabilityID": "CVE-2023-0002",
          "PkgName": "curl",
          "InstalledVersion": "7.81.0-1ubuntu1.15",
          "FixedVersion": "",
          "Severity": "MEDIUM",
          "Title": "curl: information disclosure"
        }
      ]
    },
    {
      "Target": "usr/share/app/package-lock.json",
      "Class": "lang-pkgs",
      "Type": "npm",
      "Vulnerabilities": [
        {
          "VulnerabilityID": "CVE-2023-0003",
          "PkgName": "lodash",
          "InstalledVersion": "4.17.15",
          "FixedVersion": "4.17.21",
          "Severity": "HIGH",
          "Title": "lodash: prototype pollution"
        }
      ]
    }
  ]
}`

// trivyFixtureEmpty is a clean Trivy report with no findings at all.
const trivyFixtureEmpty = `{
  "SchemaVersion": 2,
  "ArtifactName": "/",
  "ArtifactType": "rootfs",
  "Results": [
    { "Target": "Ubuntu 22.04 (ubuntu)", "Class": "os-pkgs", "Type": "ubuntu" }
  ]
}`

// trivyVersionFixture mirrors `trivy --version --format json`.
const trivyVersionFixture = `{
  "Version": "0.50.1",
  "VulnerabilityDB": { "Version": 2, "UpdatedAt": "2024-05-01T00:12:00Z" }
}`

// grypeFixture is a realistic Grype 0.8x `-o json` report, hand-written
// from the documented schema: matches[].vulnerability/artifact plus a
// top-level descriptor.version/descriptor.db.built.
const grypeFixture = `{
  "matches": [
    {
      "vulnerability": {
        "id": "CVE-2023-0001",
        "severity": "Critical",
        "fix": { "versions": ["3.0.2-0ubuntu1.12"], "state": "fixed" }
      },
      "artifact": {
        "name": "openssl",
        "version": "3.0.2-0ubuntu1.10",
        "type": "deb",
        "locations": [ { "path": "/var/lib/dpkg/status" } ]
      }
    },
    {
      "vulnerability": {
        "id": "CVE-2023-0009",
        "severity": "Negligible",
        "fix": { "state": "not-fixed" }
      },
      "artifact": {
        "name": "libc6",
        "version": "2.35-0ubuntu3",
        "type": "deb",
        "locations": [ { "path": "/var/lib/dpkg/status" } ]
      }
    }
  ],
  "source": { "type": "directory", "target": "/" },
  "descriptor": {
    "name": "grype",
    "version": "0.80.2",
    "db": { "built": "2024-05-02T03:00:00Z", "schemaVersion": 5 }
  }
}`

const grypeFixtureEmpty = `{
  "matches": [],
  "source": { "type": "directory", "target": "/" },
  "descriptor": { "name": "grype", "version": "0.80.2", "db": { "built": "2024-05-02T03:00:00Z" } }
}`

// ─── Parser tests ────────────────────────────────────────────────────────

func TestParseTrivyReport(t *testing.T) {
	findings, err := parseTrivyReport([]byte(trivyFixture))
	if err != nil {
		t.Fatalf("parseTrivyReport() error = %v", err)
	}
	if len(findings) != 3 {
		t.Fatalf("len(findings) = %d, want 3", len(findings))
	}

	byID := make(map[string]cveFinding, len(findings))
	for _, f := range findings {
		byID[f.ID] = f
	}

	openssl, ok := byID["CVE-2023-0001"]
	if !ok {
		t.Fatalf("missing CVE-2023-0001")
	}
	if openssl.Severity != "critical" {
		t.Errorf("openssl severity = %q, want critical", openssl.Severity)
	}
	if openssl.Package != "openssl" || openssl.Installed != "3.0.2-0ubuntu1.10" || openssl.Fixed != "3.0.2-0ubuntu1.12" {
		t.Errorf("openssl finding = %+v", openssl)
	}
	if openssl.Target != "Ubuntu 22.04 (ubuntu)" {
		t.Errorf("openssl target = %q", openssl.Target)
	}

	curl, ok := byID["CVE-2023-0002"]
	if !ok {
		t.Fatalf("missing CVE-2023-0002")
	}
	if curl.Severity != "medium" || curl.Fixed != "" {
		t.Errorf("curl finding = %+v", curl)
	}

	lodash, ok := byID["CVE-2023-0003"]
	if !ok {
		t.Fatalf("missing CVE-2023-0003")
	}
	if lodash.Target != "usr/share/app/package-lock.json" {
		t.Errorf("lodash target = %q", lodash.Target)
	}
}

func TestParseTrivyReport_Empty(t *testing.T) {
	findings, err := parseTrivyReport([]byte(trivyFixtureEmpty))
	if err != nil {
		t.Fatalf("parseTrivyReport() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("len(findings) = %d, want 0", len(findings))
	}
}

func TestParseTrivyReport_InvalidJSON(t *testing.T) {
	if _, err := parseTrivyReport([]byte("not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

func TestParseGrypeReport(t *testing.T) {
	findings, dbBuilt, version, err := parseGrypeReport([]byte(grypeFixture))
	if err != nil {
		t.Fatalf("parseGrypeReport() error = %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("len(findings) = %d, want 2", len(findings))
	}
	if dbBuilt != "2024-05-02T03:00:00Z" {
		t.Errorf("dbBuilt = %q", dbBuilt)
	}
	if version != "0.80.2" {
		t.Errorf("version = %q", version)
	}

	byID := make(map[string]cveFinding, len(findings))
	for _, f := range findings {
		byID[f.ID] = f
	}

	openssl := byID["CVE-2023-0001"]
	if openssl.Severity != "critical" || openssl.Fixed != "3.0.2-0ubuntu1.12" {
		t.Errorf("openssl finding = %+v", openssl)
	}
	if openssl.Target != "/var/lib/dpkg/status" {
		t.Errorf("openssl target = %q", openssl.Target)
	}

	libc := byID["CVE-2023-0009"]
	if libc.Severity != "low" {
		t.Errorf("negligible severity should normalize to low, got %q", libc.Severity)
	}
	if libc.Fixed != "" {
		t.Errorf("not-fixed state should leave Fixed empty, got %q", libc.Fixed)
	}
}

func TestParseGrypeReport_Empty(t *testing.T) {
	findings, dbBuilt, version, err := parseGrypeReport([]byte(grypeFixtureEmpty))
	if err != nil {
		t.Fatalf("parseGrypeReport() error = %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("len(findings) = %d, want 0", len(findings))
	}
	if dbBuilt == "" || version == "" {
		t.Errorf("descriptor metadata should still be present on an empty report: dbBuilt=%q version=%q", dbBuilt, version)
	}
}

func TestParseGrypeReport_InvalidJSON(t *testing.T) {
	if _, _, _, err := parseGrypeReport([]byte("not json")); err == nil {
		t.Fatal("expected an error for invalid JSON")
	}
}

// ─── Severity normalization ──────────────────────────────────────────────

func TestNormalizeCveSeverity(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"CRITICAL", "critical"},
		{"High", "high"},
		{"medium", "medium"},
		{"LOW", "low"},
		{"Negligible", "low"},
		{"Unknown", "unknown"},
		{"", "unknown"},
		{"something-else", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			if got := normalizeCveSeverity(tt.raw); got != tt.want {
				t.Errorf("normalizeCveSeverity(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// ─── Dedupe / build target ───────────────────────────────────────────────

func TestDedupeCveFindings(t *testing.T) {
	findings := []cveFinding{
		{ID: "CVE-1", Package: "openssl", Target: "a"},
		{ID: "CVE-1", Package: "openssl", Target: "a"}, // exact duplicate
		{ID: "CVE-1", Package: "openssl", Target: "b"}, // different target, kept
		{ID: "CVE-2", Package: "curl", Target: "a"},
	}
	got := dedupeCveFindings(findings)
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3: %+v", len(got), got)
	}
}

func TestBuildCveTarget(t *testing.T) {
	findings := []cveFinding{
		{ID: "CVE-1", Severity: "low", Package: "a", Fixed: "1.1"},
		{ID: "CVE-2", Severity: "critical", Package: "b"},
		{ID: "CVE-3", Severity: "high", Package: "c", Fixed: "2.0"},
		{ID: "CVE-4", Severity: "medium", Package: "d"},
	}
	target := buildCveTarget("host", "/", findings)

	if target["kind"] != "host" || target["ref"] != "/" {
		t.Errorf("target kind/ref = %v/%v", target["kind"], target["ref"])
	}
	counts, ok := target["counts"].(map[string]int)
	if !ok {
		t.Fatalf("counts is not map[string]int: %T", target["counts"])
	}
	if counts["critical"] != 1 || counts["high"] != 1 || counts["medium"] != 1 || counts["low"] != 1 {
		t.Errorf("counts = %+v", counts)
	}
	if target["fixable"] != 2 {
		t.Errorf("fixable = %v, want 2", target["fixable"])
	}

	findingsData, ok := target["findings"].([]map[string]any)
	if !ok || len(findingsData) != 4 {
		t.Fatalf("findings = %+v", target["findings"])
	}
	// Most severe first.
	if findingsData[0]["id"] != "CVE-2" || findingsData[0]["severity"] != "critical" {
		t.Errorf("first finding = %+v, want CVE-2/critical first", findingsData[0])
	}
	if findingsData[len(findingsData)-1]["severity"] != "low" {
		t.Errorf("last finding severity = %v, want low", findingsData[len(findingsData)-1]["severity"])
	}
}

func TestBuildCveTarget_TruncatesTo200(t *testing.T) {
	findings := make([]cveFinding, 0, 250)
	for i := 0; i < 250; i++ {
		findings = append(findings, cveFinding{
			ID:       fmt.Sprintf("CVE-%d", i),
			Severity: "low",
			Package:  fmt.Sprintf("pkg-%d", i),
			Target:   fmt.Sprintf("t-%d", i),
		})
	}
	target := buildCveTarget("host", "/", findings)
	findingsData, _ := target["findings"].([]map[string]any)
	if len(findingsData) != cveScanFindingsLimit {
		t.Fatalf("len(findingsData) = %d, want %d", len(findingsData), cveScanFindingsLimit)
	}
	// Counts/fixable are computed over ALL findings, not just the
	// truncated slice.
	counts, _ := target["counts"].(map[string]int)
	if counts["low"] != 250 {
		t.Errorf("counts[low] = %d, want 250 (counts must not be truncated)", counts["low"])
	}
}

func TestAggregateCveTotals(t *testing.T) {
	targets := []map[string]any{
		buildCveTarget("host", "/", []cveFinding{
			{ID: "CVE-1", Severity: "critical", Package: "a"},
			{ID: "CVE-2", Severity: "high", Package: "b", Fixed: "1.0"},
		}),
		buildCveTarget("image", "nginx:latest", []cveFinding{
			{ID: "CVE-3", Severity: "critical", Package: "c"},
			{ID: "CVE-4", Severity: "low", Package: "d"},
		}),
	}
	totals := aggregateCveTotals(targets)
	if totals["critical"] != 2 || totals["high"] != 1 || totals["low"] != 1 || totals["fixable"] != 1 {
		t.Errorf("totals = %+v", totals)
	}
}

// ─── fakeCveRunner ───────────────────────────────────────────────────────

// fakeCveRunner is an in-memory cveRunner for testing scan()/Collect()
// without touching a real PATH or spawning real processes.
type fakeCveRunner struct {
	mu             sync.Mutex
	lookPathErr    map[string]error // binary name -> error (nil entries mean "found")
	outputs        map[string][]byte
	versionOutputs map[string][]byte // bin -> output for a "--version"/"version" call
	callCount      int
}

func newFakeCveRunner() *fakeCveRunner {
	return &fakeCveRunner{
		lookPathErr:    make(map[string]error),
		outputs:        make(map[string][]byte),
		versionOutputs: make(map[string][]byte),
	}
}

// setVersionOutput registers the output returned when Run is called with a
// "--version" argument (trivyVersion's call shape) for the given binary.
func (f *fakeCveRunner) setVersionOutput(bin string, out []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.versionOutputs[bin] = out
}

func (f *fakeCveRunner) LookPath(file string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err, ok := f.lookPathErr[file]; ok {
		if err != nil {
			return "", err
		}
		return "/usr/local/bin/" + file, nil
	}
	return "", errors.New("not found")
}

// setOutput registers the fixed output returned for a command whose first
// argument (or, for grype's positional target, the binary name) matches
// key. To keep the fake simple, every call to a given binary returns the
// same registered output regardless of arguments — sufficient for testing
// the collector's assembly logic, since parser correctness is already
// covered by the parser tests above.
func (f *fakeCveRunner) setOutput(bin string, out []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outputs[bin] = out
}

func (f *fakeCveRunner) Run(_ context.Context, name string, args []string, _ []string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++

	isVersionCall := false
	for _, a := range args {
		if a == "--version" {
			isVersionCall = true
			break
		}
	}

	// name is the resolved path ("/usr/local/bin/trivy"); match by suffix.
	matchesBin := func(bin string) bool {
		return len(name) >= len(bin) && name[len(name)-len(bin):] == bin
	}

	if isVersionCall {
		for bin, out := range f.versionOutputs {
			if matchesBin(bin) {
				return out, nil
			}
		}
		return nil, fmt.Errorf("fakeCveRunner: no version output registered for %q", name)
	}

	for bin, out := range f.outputs {
		if matchesBin(bin) {
			return out, nil
		}
	}
	return nil, fmt.Errorf("fakeCveRunner: no output registered for %q", name)
}

// ─── Collector behavior ──────────────────────────────────────────────────

func TestCveScanCollector_NoScannerAvailable(t *testing.T) {
	runner := newFakeCveRunner() // no binaries registered as found
	c := newCveScanCollector(true, time.Hour, 10, t.TempDir(), "/nonexistent/docker.sock", runner)

	data, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if data["available"] != false {
		t.Errorf("available = %v, want false", data["available"])
	}
	if data["error"] == "" {
		t.Error("expected a non-empty error message naming install options")
	}
}

func TestCveScanCollector_Disabled(t *testing.T) {
	runner := newFakeCveRunner()
	c := newCveScanCollector(false, time.Hour, 10, t.TempDir(), "/nonexistent/docker.sock", runner)

	data, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if data["available"] != false {
		t.Errorf("available = %v, want false", data["available"])
	}
	if runner.callCount != 0 {
		t.Errorf("disabled collector must never invoke the runner, callCount = %d", runner.callCount)
	}
}

func TestCveScanCollector_PendingBeforeFirstRun(t *testing.T) {
	runner := newFakeCveRunner()
	runner.lookPathErr["trivy"] = nil
	runner.setOutput("trivy", []byte(trivyFixture))
	c := newCveScanCollector(true, time.Hour, 10, t.TempDir(), "/nonexistent/docker.sock", runner)

	data, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if data["available"] != false {
		t.Errorf("before the first run, available should be false (scan pending), got %v", data["available"])
	}
	if data["stale"] != true {
		t.Errorf("before the first run, stale should be true, got %v", data["stale"])
	}
}

func TestCveScanCollector_RunOnce_TrivyPopulatesPayload(t *testing.T) {
	runner := newFakeCveRunner()
	runner.lookPathErr["trivy"] = nil
	runner.setOutput("trivy", []byte(trivyFixture))
	runner.setVersionOutput("trivy", []byte(trivyVersionFixture))
	c := newCveScanCollector(true, time.Hour, 10, t.TempDir(), "/nonexistent/docker.sock", runner)

	c.runOnce()

	data, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if data["available"] != true {
		t.Fatalf("available = %v, want true", data["available"])
	}
	if data["scanner"] != "trivy" {
		t.Errorf("scanner = %v, want trivy", data["scanner"])
	}
	if data["scanner_version"] != "0.50.1" {
		t.Errorf("scanner_version = %v, want 0.50.1", data["scanner_version"])
	}
	if data["stale"] != false {
		t.Errorf("stale = %v, want false immediately after a run", data["stale"])
	}
	targets, ok := data["targets"].([]map[string]any)
	if !ok || len(targets) != 1 {
		t.Fatalf("targets = %+v", data["targets"])
	}
	if targets[0]["kind"] != "host" || targets[0]["ref"] != "/" {
		t.Errorf("host target = %+v", targets[0])
	}
	totals, ok := data["totals"].(map[string]any)
	if !ok {
		t.Fatalf("totals is not map[string]any: %T", data["totals"])
	}
	if totals["critical"] != 1 || totals["high"] != 1 || totals["medium"] != 1 {
		t.Errorf("totals = %+v", totals)
	}
}

func TestCveScanCollector_RunOnce_GrypeFallback(t *testing.T) {
	runner := newFakeCveRunner()
	// trivy not found; grype is.
	runner.lookPathErr["grype"] = nil
	runner.setOutput("grype", []byte(grypeFixture))
	c := newCveScanCollector(true, time.Hour, 10, t.TempDir(), "/nonexistent/docker.sock", runner)

	c.runOnce()

	data, _ := c.Collect(context.Background())
	if data["scanner"] != "grype" {
		t.Errorf("scanner = %v, want grype", data["scanner"])
	}
	if data["db_updated_at"] != "2024-05-02T03:00:00Z" {
		t.Errorf("db_updated_at = %v", data["db_updated_at"])
	}
	totals, _ := data["totals"].(map[string]any)
	if totals["critical"] != 1 || totals["low"] != 1 {
		t.Errorf("totals = %+v", totals)
	}
}

func TestCveScanCollector_StaleAfterTwiceInterval(t *testing.T) {
	runner := newFakeCveRunner()
	runner.lookPathErr["trivy"] = nil
	runner.setOutput("trivy", []byte(trivyFixtureEmpty))
	c := newCveScanCollector(true, time.Hour, 10, t.TempDir(), "/nonexistent/docker.sock", runner)
	// Bypass the constructor's MinCveScanInterval floor (a production
	// safeguard, not something this staleness test needs) so the 2x
	// threshold trips in milliseconds instead of hours.
	c.interval = 10 * time.Millisecond

	c.runOnce()
	data, _ := c.Collect(context.Background())
	if data["stale"] != false {
		t.Fatalf("stale immediately after a run = %v, want false", data["stale"])
	}

	time.Sleep(30 * time.Millisecond) // > 2x the 10ms interval
	data, _ = c.Collect(context.Background())
	if data["stale"] != true {
		t.Errorf("stale after 2x interval = %v, want true", data["stale"])
	}
}

func TestCveScanCollector_MinIntervalEnforced(t *testing.T) {
	c := newCveScanCollector(true, time.Minute, 10, t.TempDir(), "", newFakeCveRunner())
	if c.interval != MinCveScanInterval {
		t.Errorf("interval = %v, want the enforced floor %v", c.interval, MinCveScanInterval)
	}
}

func TestCveScanCollector_Name(t *testing.T) {
	c := newCveScanCollector(true, time.Hour, 10, t.TempDir(), "", newFakeCveRunner())
	if c.Name() != "cve_scan" {
		t.Errorf("Name() = %q, want cve_scan", c.Name())
	}
}

func TestCveScanCollector_DockerUnreachableIsNotAnError(t *testing.T) {
	runner := newFakeCveRunner()
	runner.lookPathErr["trivy"] = nil
	runner.setOutput("trivy", []byte(trivyFixtureEmpty))
	c := newCveScanCollector(true, time.Hour, 10, t.TempDir(), t.TempDir()+"/no-such-docker.sock", runner)

	c.runOnce()
	data, _ := c.Collect(context.Background())
	if data["available"] != true {
		t.Fatalf("available = %v, want true (host scan still succeeds)", data["available"])
	}
	if data["error"] != "" {
		t.Errorf("error = %q, want empty (an unreachable Docker daemon is best-effort, not an error)", data["error"])
	}
	targets, _ := data["targets"].([]map[string]any)
	if len(targets) != 1 {
		t.Errorf("targets = %+v, want exactly the host target (no images)", targets)
	}
}

// ─── execCveRunner smoke test ────────────────────────────────────────────

func TestExecCveRunner_LookPath_NotFound(t *testing.T) {
	r := execCveRunner{}
	if _, err := r.LookPath("nexwatch-definitely-not-a-real-binary"); err == nil {
		t.Error("expected an error for a nonexistent binary")
	}
}
