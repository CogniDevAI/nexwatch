package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

const (
	// DefaultCveScanInterval is how often the background scan re-runs when
	// the caller does not override it.
	DefaultCveScanInterval = 12 * time.Hour
	// MinCveScanInterval is the floor NewCveScanCollector enforces,
	// regardless of the interval requested.
	MinCveScanInterval = 1 * time.Hour
	// DefaultCveScanMaxImages caps how many unique running-container images
	// are scanned per run when the caller does not override it.
	DefaultCveScanMaxImages = 10
	// DefaultCveScanCacheDir is where the scanner's vulnerability database
	// is cached across runs when the caller does not override it.
	DefaultCveScanCacheDir = "/var/lib/nexwatch/scanner-cache"

	// cveScanHardTimeout bounds one full scan run (host + every image),
	// so a hung scanner process can never block future runs indefinitely.
	cveScanHardTimeout = 15 * time.Minute
	// cveScanFirstRunDelay staggers the very first scan after agent start,
	// so it doesn't compete with the agent's own startup/registration.
	cveScanFirstRunDelay = 2 * time.Minute
	// cveScanFindingsLimit is the maximum number of findings kept per
	// target, sorted by severity (most severe first).
	cveScanFindingsLimit = 200
)

// cveRunner is the seam over exec.LookPath/exec.Command used to detect and
// invoke trivy/grype. Tests supply a fake implementation instead of
// touching the real PATH or spawning real processes.
type cveRunner interface {
	LookPath(file string) (string, error)
	Run(ctx context.Context, name string, args []string, env []string) ([]byte, error)
}

// execCveRunner is the real cveRunner backed by os/exec.
type execCveRunner struct{}

func (execCveRunner) LookPath(file string) (string, error) { return exec.LookPath(file) }

func (execCveRunner) Run(ctx context.Context, name string, args []string, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		// Trivy/Grype normally exit 0 even when vulnerabilities are found
		// (we never pass --exit-code), but be defensive: a non-zero exit
		// with usable stdout is still worth trying to parse rather than
		// discarding, since the JSON parser is the final judge of validity.
		if errors.As(err, &exitErr) && len(out) > 0 {
			return out, nil
		}
		return nil, err
	}
	return out, nil
}

// cveFinding is one normalized vulnerability finding, independent of which
// scanner produced it.
type cveFinding struct {
	ID        string
	Severity  string // critical, high, medium, low, unknown (lowercase)
	Package   string
	Installed string
	Fixed     string
	Title     string
	Target    string // the scanner's sub-target (e.g. "Ubuntu 22.04 (ubuntu)", a lockfile path)
}

// cveSeverityRank orders findings most-severe-first; anything unrecognized
// sorts after "unknown".
var cveSeverityRank = map[string]int{
	"critical": 0,
	"high":     1,
	"medium":   2,
	"low":      3,
	"unknown":  4,
}

// CveScanCollector detects an installed trivy or grype binary and, on its
// own schedule (independent of the agent's collection interval), scans the
// host filesystem and — when reachable — the images of running Docker
// containers for known CVEs. Collect never blocks on a scan: it always
// returns the most recently completed result immediately, flagging it
// "stale" once it is older than twice the scan interval.
type CveScanCollector struct {
	enabled      bool
	interval     time.Duration
	maxImages    int
	cacheDir     string
	dockerSocket string
	runner       cveRunner

	mu         sync.Mutex
	lastResult map[string]any
	lastRunAt  time.Time
	stopOnce   sync.Once
	stopCh     chan struct{}
}

// NewCveScanCollector creates a CveScanCollector and, when enabled, starts
// its background scan loop: a first run cveScanFirstRunDelay after
// creation, then every interval (clamped to at least MinCveScanInterval)
// thereafter. maxImages <= 0 and cacheDir == "" fall back to their
// package defaults.
func NewCveScanCollector(enabled bool, interval time.Duration, maxImages int, cacheDir, dockerSocket string) *CveScanCollector {
	c := newCveScanCollector(enabled, interval, maxImages, cacheDir, dockerSocket, execCveRunner{})
	if enabled {
		c.start()
	}
	return c
}

// newCveScanCollector builds the collector without starting its background
// goroutine, so tests can drive scan()/runOnce() synchronously with a fake
// runner.
func newCveScanCollector(enabled bool, interval time.Duration, maxImages int, cacheDir, dockerSocket string, runner cveRunner) *CveScanCollector {
	if interval < MinCveScanInterval {
		interval = MinCveScanInterval
	}
	if maxImages <= 0 {
		maxImages = DefaultCveScanMaxImages
	}
	if cacheDir == "" {
		cacheDir = DefaultCveScanCacheDir
	}

	c := &CveScanCollector{
		enabled:      enabled,
		interval:     interval,
		maxImages:    maxImages,
		cacheDir:     cacheDir,
		dockerSocket: dockerSocket,
		runner:       runner,
		stopCh:       make(chan struct{}),
	}
	if enabled {
		c.lastResult = notAvailableCvePayload("scan pending: the first run has not completed yet")
	} else {
		c.lastResult = notAvailableCvePayload("cve_scan_enabled is false")
	}
	return c
}

// Name returns the collector identifier / metrics.type value.
func (c *CveScanCollector) Name() string { return "cve_scan" }

// Collect returns the cached result of the most recently completed
// background scan, marking it "stale" when it is older than twice the
// configured interval (or when scanning is disabled but was never
// available in the first place). It never runs a scan itself and never
// blocks.
func (c *CveScanCollector) Collect(_ context.Context) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make(map[string]any, len(c.lastResult))
	for k, v := range c.lastResult {
		out[k] = v
	}
	if c.enabled {
		out["stale"] = c.lastRunAt.IsZero() || time.Since(c.lastRunAt) > 2*c.interval
	}
	return out, nil
}

// Stop halts the background scan loop. Safe to call multiple times or
// never (the goroutine exits with the process either way).
func (c *CveScanCollector) Stop() {
	c.stopOnce.Do(func() { close(c.stopCh) })
}

func (c *CveScanCollector) start() {
	go func() {
		select {
		case <-time.After(cveScanFirstRunDelay):
		case <-c.stopCh:
			return
		}
		c.runOnce()

		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.runOnce()
			case <-c.stopCh:
				return
			}
		}
	}()
}

// runOnce performs one full scan (bounded by cveScanHardTimeout) and
// atomically swaps it in as the cached result.
func (c *CveScanCollector) runOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), cveScanHardTimeout)
	defer cancel()

	result := c.scan(ctx)

	c.mu.Lock()
	c.lastResult = result
	c.lastRunAt = time.Now()
	c.mu.Unlock()
}

// scan detects a scanner and runs it against the host filesystem plus (best
// effort) the images of currently running Docker containers, up to
// maxImages unique refs. It never panics or blocks indefinitely (bounded by
// the caller's ctx); any per-target failure is recorded in the payload's
// "error" field rather than aborting the whole run.
func (c *CveScanCollector) scan(ctx context.Context) map[string]any {
	start := time.Now()

	scanner, bin, found := detectCveScanner(c.runner)
	if !found {
		return notAvailableCvePayload("no supported scanner found (install trivy or grype)")
	}

	env := []string{
		"TRIVY_CACHE_DIR=" + c.cacheDir,
		"GRYPE_DB_CACHE_DIR=" + c.cacheDir,
	}

	meta := &cveScanMeta{}
	var targets []map[string]any
	var firstErr string
	recordErr := func(err error) {
		if firstErr == "" && err != nil {
			firstErr = err.Error()
		}
	}

	if t, err := c.scanTarget(ctx, scanner, bin, env, "host", "/", meta); err != nil {
		recordErr(err)
	} else if t != nil {
		targets = append(targets, t)
	}

	images := c.dockerImages(ctx)
	if len(images) > c.maxImages {
		images = images[:c.maxImages]
	}
	for _, ref := range images {
		if t, err := c.scanTarget(ctx, scanner, bin, env, "image", ref, meta); err != nil {
			recordErr(err)
		} else if t != nil {
			targets = append(targets, t)
		}
	}

	if scanner == "trivy" {
		meta.scannerVersion = trivyVersion(ctx, c.runner, bin)
	}

	return map[string]any{
		"scanner":         scanner,
		"scanner_version": meta.scannerVersion,
		"db_updated_at":   meta.dbUpdatedAt,
		"scanned_at":      time.Now().UTC().Format(time.RFC3339),
		"duration_ms":     time.Since(start).Milliseconds(),
		"stale":           false,
		"available":       true,
		"error":           firstErr,
		"targets":         targets,
		"totals":          aggregateCveTotals(targets),
	}
}

// cveScanMeta accumulates scanner/DB metadata discovered while parsing
// individual scan targets (grype embeds it in every scan's JSON; trivy
// requires a separate version call — see scan()).
type cveScanMeta struct {
	scannerVersion string
	dbUpdatedAt    string
}

// scanTarget runs the scanner against one target (the host filesystem or
// one image reference) and parses its output into the payload shape
// documented on CveScanCollector. Returns (nil, nil) only when the
// scanner produced no usable output and no error (treated as "nothing to
// report" rather than a failure).
func (c *CveScanCollector) scanTarget(ctx context.Context, scanner, bin string, env []string, kind, ref string, meta *cveScanMeta) (map[string]any, error) {
	args, err := cveScanArgs(scanner, kind, ref)
	if err != nil {
		return nil, err
	}

	out, err := c.runner.Run(ctx, bin, args, env)
	if err != nil {
		return nil, fmt.Errorf("%s scan of %s: %w", scanner, ref, err)
	}

	var findings []cveFinding
	switch scanner {
	case "trivy":
		findings, err = parseTrivyReport(out)
	case "grype":
		var dbBuilt, version string
		findings, dbBuilt, version, err = parseGrypeReport(out)
		if meta.dbUpdatedAt == "" {
			meta.dbUpdatedAt = dbBuilt
		}
		if meta.scannerVersion == "" {
			meta.scannerVersion = version
		}
	default:
		return nil, fmt.Errorf("unsupported scanner %q", scanner)
	}
	if err != nil {
		return nil, fmt.Errorf("parse %s output for %s: %w", scanner, ref, err)
	}

	return buildCveTarget(kind, ref, findings), nil
}

// cveScanArgs returns the CLI arguments for scanning kind ("host" or
// "image") ref with the given scanner.
func cveScanArgs(scanner, kind, ref string) ([]string, error) {
	switch scanner {
	case "trivy":
		if kind == "host" {
			return []string{"rootfs", "--scanners", "vuln", "--format", "json", "--quiet", "--timeout", "10m", ref}, nil
		}
		return []string{"image", "--format", "json", "--quiet", ref}, nil
	case "grype":
		if kind == "host" {
			return []string{"dir:" + ref, "-o", "json"}, nil
		}
		return []string{ref, "-o", "json"}, nil
	default:
		return nil, fmt.Errorf("unsupported scanner %q", scanner)
	}
}

// detectCveScanner reports the first of trivy/grype found on PATH (trivy
// preferred), along with its resolved path.
func detectCveScanner(r cveRunner) (scanner, path string, found bool) {
	if p, err := r.LookPath("trivy"); err == nil {
		return "trivy", p, true
	}
	if p, err := r.LookPath("grype"); err == nil {
		return "grype", p, true
	}
	return "", "", false
}

// trivyVersion best-effort resolves the installed trivy's own version via
// `trivy --version --format json`. Any failure (unsupported flag on an
// older trivy, unparsable output) leaves it blank rather than failing the
// scan — this is enrichment, not required data.
func trivyVersion(ctx context.Context, r cveRunner, bin string) string {
	out, err := r.Run(ctx, bin, []string{"--version", "--format", "json"}, nil)
	if err != nil {
		return ""
	}
	var v struct {
		Version string `json:"Version"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return ""
	}
	return v.Version
}

// dockerImages returns the deduplicated image references of currently
// running containers, or nil when Docker is unreachable — mirroring
// DockerCollector's "not available is not an error" behavior. Order is
// deterministic (sorted) so a run capped by maxImages is reproducible.
func (c *CveScanCollector) dockerImages(ctx context.Context) []string {
	socketPath := c.dockerSocket
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}

	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+socketPath),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil
	}
	defer func() { _ = cli.Close() }()

	if _, err := cli.Ping(ctx); err != nil {
		return nil
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		log.Printf("[cve_scan] container list error: %v", err)
		return nil
	}

	seen := make(map[string]bool, len(containers))
	images := make([]string, 0, len(containers))
	for _, ctr := range containers {
		if ctr.Image == "" || seen[ctr.Image] {
			continue
		}
		seen[ctr.Image] = true
		images = append(images, ctr.Image)
	}
	sort.Strings(images)
	return images
}

// ─── Trivy parsing ───────────────────────────────────────────────────────

// trivyReport mirrors the subset of Trivy's `--format json` schema (Trivy
// 0.5x) this collector needs.
type trivyReport struct {
	Results []struct {
		Target          string `json:"Target"`
		Vulnerabilities []struct {
			VulnerabilityID  string `json:"VulnerabilityID"`
			PkgName          string `json:"PkgName"`
			InstalledVersion string `json:"InstalledVersion"`
			FixedVersion     string `json:"FixedVersion"`
			Severity         string `json:"Severity"`
			Title            string `json:"Title"`
		} `json:"Vulnerabilities"`
	} `json:"Results"`
}

// parseTrivyReport decodes a Trivy JSON report into normalized findings.
func parseTrivyReport(data []byte) ([]cveFinding, error) {
	var report trivyReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}

	var findings []cveFinding
	for _, result := range report.Results {
		for _, v := range result.Vulnerabilities {
			findings = append(findings, cveFinding{
				ID:        v.VulnerabilityID,
				Severity:  normalizeCveSeverity(v.Severity),
				Package:   v.PkgName,
				Installed: v.InstalledVersion,
				Fixed:     v.FixedVersion,
				Title:     v.Title,
				Target:    result.Target,
			})
		}
	}
	return findings, nil
}

// ─── Grype parsing ───────────────────────────────────────────────────────

// grypeReport mirrors the subset of Grype's `-o json` schema (Grype 0.8x)
// this collector needs.
type grypeReport struct {
	Matches []struct {
		Vulnerability struct {
			ID       string `json:"id"`
			Severity string `json:"severity"`
			Fix      struct {
				Versions []string `json:"versions"`
				State    string   `json:"state"`
			} `json:"fix"`
		} `json:"vulnerability"`
		Artifact struct {
			Name      string `json:"name"`
			Version   string `json:"version"`
			Locations []struct {
				Path string `json:"path"`
			} `json:"locations"`
		} `json:"artifact"`
	} `json:"matches"`
	Descriptor struct {
		Version string `json:"version"`
		DB      struct {
			Built string `json:"built"`
		} `json:"db"`
	} `json:"descriptor"`
}

// parseGrypeReport decodes a Grype JSON report into normalized findings,
// plus the scanner's own version and vulnerability DB build timestamp
// (both embedded once per report under "descriptor").
func parseGrypeReport(data []byte) (findings []cveFinding, dbBuilt string, scannerVersion string, err error) {
	var report grypeReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, "", "", err
	}

	for _, m := range report.Matches {
		fixed := ""
		if strings.EqualFold(m.Vulnerability.Fix.State, "fixed") && len(m.Vulnerability.Fix.Versions) > 0 {
			fixed = strings.Join(m.Vulnerability.Fix.Versions, ", ")
		}
		target := ""
		if len(m.Artifact.Locations) > 0 {
			target = m.Artifact.Locations[0].Path
		}
		findings = append(findings, cveFinding{
			ID:        m.Vulnerability.ID,
			Severity:  normalizeCveSeverity(m.Vulnerability.Severity),
			Package:   m.Artifact.Name,
			Installed: m.Artifact.Version,
			Fixed:     fixed,
			Target:    target,
		})
	}

	return findings, report.Descriptor.DB.Built, report.Descriptor.Version, nil
}

// ─── Shared normalization/aggregation ──────────────────────────────────────

// normalizeCveSeverity maps a scanner-reported severity string (Trivy:
// UPPERCASE; Grype: Titlecase, plus a "Negligible" bucket Trivy has no
// equivalent for) onto the shared lowercase taxonomy used by the payload
// and the UI's SeverityBadge.
func normalizeCveSeverity(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "critical":
		return "critical"
	case "high":
		return "high"
	case "medium":
		return "medium"
	case "low":
		return "low"
	case "negligible":
		return "low"
	default:
		return "unknown"
	}
}

// dedupeCveFindings collapses findings that share (id, package, target) —
// the same underlying advisory reported more than once for the same
// package/sub-target (e.g. a Trivy result listing both an OS and a
// re-detected library match) — keeping the first occurrence.
func dedupeCveFindings(findings []cveFinding) []cveFinding {
	seen := make(map[string]bool, len(findings))
	out := make([]cveFinding, 0, len(findings))
	for _, f := range findings {
		key := f.ID + "|" + f.Package + "|" + f.Target
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

// buildCveTarget dedupes, sorts (most severe first), truncates to
// cveScanFindingsLimit, and summarizes findings into the payload's
// per-target shape.
func buildCveTarget(kind, ref string, findings []cveFinding) map[string]any {
	findings = dedupeCveFindings(findings)
	sort.SliceStable(findings, func(i, j int) bool {
		return cveSeverityRank[findings[i].Severity] < cveSeverityRank[findings[j].Severity]
	})

	counts := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0, "unknown": 0}
	fixable := 0
	for _, f := range findings {
		counts[f.Severity]++
		if f.Fixed != "" {
			fixable++
		}
	}

	limited := findings
	if len(limited) > cveScanFindingsLimit {
		limited = limited[:cveScanFindingsLimit]
	}
	findingsData := make([]map[string]any, 0, len(limited))
	for _, f := range limited {
		findingsData = append(findingsData, map[string]any{
			"id":        f.ID,
			"severity":  f.Severity,
			"package":   f.Package,
			"installed": f.Installed,
			"fixed":     f.Fixed,
			"title":     f.Title,
			"target":    f.Target,
		})
	}

	return map[string]any{
		"kind":     kind,
		"ref":      ref,
		"counts":   counts,
		"fixable":  fixable,
		"findings": findingsData,
	}
}

// aggregateCveTotals sums every target's counts/fixable into one summary.
func aggregateCveTotals(targets []map[string]any) map[string]any {
	totals := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0, "unknown": 0, "fixable": 0}
	for _, t := range targets {
		counts, _ := t["counts"].(map[string]int)
		for k := range totals {
			if k == "fixable" {
				continue
			}
			totals[k] += counts[k]
		}
		if fixable, ok := t["fixable"].(int); ok {
			totals["fixable"] += fixable
		}
	}
	out := make(map[string]any, len(totals))
	for k, v := range totals {
		out[k] = v
	}
	return out
}

// notAvailableCvePayload builds the payload shape used whenever no scan
// data is available at all (no scanner installed, or scanning disabled).
func notAvailableCvePayload(errMsg string) map[string]any {
	return map[string]any{
		"scanner":         "",
		"scanner_version": "",
		"db_updated_at":   "",
		"scanned_at":      "",
		"duration_ms":     int64(0),
		"stale":           false,
		"available":       false,
		"error":           errMsg,
		"targets":         []map[string]any{},
		"totals": map[string]any{
			"critical": 0, "high": 0, "medium": 0, "low": 0, "unknown": 0, "fixable": 0,
		},
	}
}
