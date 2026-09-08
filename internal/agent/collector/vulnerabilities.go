package collector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

// VulnerabilitiesCollector performs basic vulnerability detection checks.
type VulnerabilitiesCollector struct{}

// NewVulnerabilitiesCollector creates a new vulnerabilities collector.
func NewVulnerabilitiesCollector() *VulnerabilitiesCollector {
	return &VulnerabilitiesCollector{}
}

// Name returns the collector identifier.
func (c *VulnerabilitiesCollector) Name() string { return "vulnerabilities" }

// vulnItem represents a single vulnerability finding.
type vulnItem struct {
	Name           string `json:"name"`
	Severity       string `json:"severity"` // critical, high, medium, low
	Description    string `json:"description"`
	Recommendation string `json:"recommendation"`
}

// Collect runs all vulnerability checks and returns aggregate results.
func (c *VulnerabilitiesCollector) Collect(ctx context.Context) (map[string]any, error) {
	items := []vulnItem{}

	items = append(items, c.checkWorldWritableFiles()...)
	items = append(items, c.checkSUIDBinaries()...)
	items = append(items, c.checkRootServices(ctx)...)
	items = append(items, c.checkWeakPermissions()...)

	// Count by severity.
	summary := map[string]int{
		"critical": 0,
		"high":     0,
		"medium":   0,
		"low":      0,
	}
	for _, item := range items {
		summary[item.Severity]++
	}

	// Convert items to []map[string]any.
	itemsData := make([]map[string]any, 0, len(items))
	for _, item := range items {
		itemsData = append(itemsData, map[string]any{
			"name":           item.Name,
			"severity":       item.Severity,
			"description":    item.Description,
			"recommendation": item.Recommendation,
		})
	}

	return map[string]any{
		"items":   itemsData,
		"summary": summary,
		"total":   len(items),
	}, nil
}

// checkWorldWritableFiles looks for world-writable files in sensitive directories.
func (c *VulnerabilitiesCollector) checkWorldWritableFiles() []vulnItem {
	if runtime.GOOS == "windows" {
		return []vulnItem{{
			Name:           "world_writable_files",
			Severity:       "low",
			Description:    "World-writable file check not applicable on Windows",
			Recommendation: "N/A — skipped",
		}}
	}

	items := []vulnItem{}
	dirsToCheck := []string{"/etc"}

	for _, dir := range dirsToCheck {
		// Use find command with a depth limit to avoid long scans.
		out, err := exec.Command("find", dir, "-maxdepth", "2", "-perm", "-0002", "-type", "f").CombinedOutput()
		if err != nil {
			continue
		}

		if item := evaluateWorldWritableFindOutput(dir, string(out)); item != nil {
			items = append(items, *item)
		}
	}

	// Check /tmp separately (less severe — it's expected to be world-writable).
	if info, err := os.Stat("/tmp"); err == nil {
		if item := evaluateTmpStickyBit(info.Mode()); item != nil {
			items = append(items, *item)
		}
	}

	return items
}

// evaluateWorldWritableFindOutput parses the newline-delimited output of the
// `find <dir> -perm -0002 -type f` command used to locate world-writable
// files, and builds the corresponding finding (or nil if none were found).
// It is a pure function over the command output so it can be tested with
// fixture strings instead of a real filesystem scan.
func evaluateWorldWritableFindOutput(dir, findOutput string) *vulnItem {
	files := strings.Split(strings.TrimSpace(findOutput), "\n")
	wwFiles := []string{}
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		wwFiles = append(wwFiles, f)
	}

	if len(wwFiles) == 0 {
		return nil
	}

	// Limit reported files to avoid huge payloads.
	reported := wwFiles
	if len(reported) > 20 {
		reported = reported[:20]
	}
	return &vulnItem{
		Name:           fmt.Sprintf("world_writable_in_%s", strings.ReplaceAll(dir, "/", "_")),
		Severity:       "high",
		Description:    fmt.Sprintf("Found %d world-writable files in %s: %s", len(wwFiles), dir, strings.Join(reported, ", ")),
		Recommendation: fmt.Sprintf("Remove world-writable permission: chmod o-w <file> for files in %s", dir),
	}
}

// evaluateTmpStickyBit reports a finding when /tmp's mode is missing the
// sticky bit. It is pure over the mode value so it can be tested without
// touching the filesystem.
func evaluateTmpStickyBit(mode os.FileMode) *vulnItem {
	if mode&os.ModeSticky != 0 {
		return nil
	}
	return &vulnItem{
		Name:           "tmp_no_sticky_bit",
		Severity:       "medium",
		Description:    "/tmp does not have the sticky bit set",
		Recommendation: "Set sticky bit: chmod +t /tmp",
	}
}

// checkSUIDBinaries looks for SUID binaries in unusual locations.
func (c *VulnerabilitiesCollector) checkSUIDBinaries() []vulnItem {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return nil
	}

	items := []vulnItem{}

	// Known safe SUID directories.
	safeDirs := map[string]bool{
		"/usr/bin":   true,
		"/usr/sbin":  true,
		"/usr/lib":   true,
		"/bin":       true,
		"/sbin":      true,
		"/usr/lib64": true,
		"/snap":      true,
	}

	// Find SUID files.
	out, err := exec.Command("find", "/", "-maxdepth", "4", "-perm", "-4000", "-type", "f").CombinedOutput()
	if err != nil {
		// Permission errors are common; process what we got.
		if len(out) == 0 {
			return nil
		}
	}

	if item := evaluateSUIDFindOutput(string(out), safeDirs); item != nil {
		items = append(items, *item)
	}

	return items
}

// evaluateSUIDFindOutput parses the newline-delimited output of the
// `find / -perm -4000 -type f` command used to locate SUID binaries and
// flags any outside the known-safe directories. It is a pure function over
// the command output so it can be tested with fixture strings instead of a
// real filesystem scan.
func evaluateSUIDFindOutput(findOutput string, safeDirs map[string]bool) *vulnItem {
	unusualSUID := []string{}
	files := strings.Split(strings.TrimSpace(findOutput), "\n")
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}

		dir := filepath.Dir(f)
		isSafe := false
		for safeDir := range safeDirs {
			if strings.HasPrefix(dir, safeDir) {
				isSafe = true
				break
			}
		}

		if !isSafe {
			unusualSUID = append(unusualSUID, f)
		}
	}

	if len(unusualSUID) == 0 {
		return nil
	}

	reported := unusualSUID
	if len(reported) > 20 {
		reported = reported[:20]
	}
	return &vulnItem{
		Name:           "suid_unusual_locations",
		Severity:       "high",
		Description:    fmt.Sprintf("Found %d SUID binaries in unusual locations: %s", len(unusualSUID), strings.Join(reported, ", ")),
		Recommendation: "Review SUID binaries and remove unnecessary ones: chmod u-s <file>",
	}
}

// checkRootServices identifies services/processes running as root.
func (c *VulnerabilitiesCollector) checkRootServices(ctx context.Context) []vulnItem {
	if runtime.GOOS == "windows" {
		return nil
	}

	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil
	}

	// Services that ideally should NOT run as root.
	riskyAsRoot := map[string]bool{
		"nginx":        true,
		"apache2":      true,
		"httpd":        true,
		"mysqld":       true,
		"postgres":     true,
		"mongod":       true,
		"redis-server": true,
		"node":         true,
		"java":         true,
		"python":       true,
		"python3":      true,
	}

	rootServices := []string{}
	for _, p := range procs {
		name, err := p.NameWithContext(ctx)
		if err != nil {
			continue
		}

		if !riskyAsRoot[name] {
			continue
		}

		uids, err := p.UidsWithContext(ctx)
		if err != nil {
			continue
		}

		if isRiskyRootProcess(name, uids, riskyAsRoot) {
			rootServices = append(rootServices, fmt.Sprintf("%s (PID %d)", name, p.Pid))
		}
	}

	items := []vulnItem{}
	if len(rootServices) > 0 {
		reported := rootServices
		if len(reported) > 10 {
			reported = reported[:10]
		}
		items = append(items, vulnItem{
			Name:           "services_running_as_root",
			Severity:       "medium",
			Description:    fmt.Sprintf("Found %d services running as root: %s", len(rootServices), strings.Join(reported, ", ")),
			Recommendation: "Configure services to run as dedicated non-root users",
		})
	}

	return items
}

// isRiskyRootProcess reports whether a process name is on the risky-as-root
// list and its effective UID (uids[1] if available, otherwise uids[0]) is
// 0. It is a pure function over already-fetched process attributes so it
// can be tested with fixture values instead of live processes.
func isRiskyRootProcess(name string, uids []uint32, riskyAsRoot map[string]bool) bool {
	if !riskyAsRoot[name] {
		return false
	}

	// Check effective UID (index 1 if available, otherwise 0).
	if len(uids) > 1 {
		return uids[1] == 0
	} else if len(uids) > 0 {
		return uids[0] == 0
	}
	return false
}

// checkWeakPermissions checks for files with overly permissive permissions.
func (c *VulnerabilitiesCollector) checkWeakPermissions() []vulnItem {
	if runtime.GOOS == "windows" {
		return nil
	}

	items := []vulnItem{}

	// Check common sensitive files.
	sensitiveFiles := map[string]os.FileMode{
		"/etc/crontab":         0o644,
		"/etc/ssh/sshd_config": 0o644,
		"/etc/sudoers":         0o440,
	}

	for path, maxPerm := range sensitiveFiles {
		info, err := os.Stat(path)
		if err != nil {
			continue // File doesn't exist — skip.
		}

		if item := evaluateFilePermission(path, info.Mode().Perm(), maxPerm); item != nil {
			items = append(items, *item)
		}
	}

	return items
}

// evaluateFilePermission reports a finding when a sensitive file's
// permission bits exceed the maximum allowed. It is pure over the mode
// values so it can be tested without touching the filesystem.
func evaluateFilePermission(path string, perm, maxPerm os.FileMode) *vulnItem {
	if perm <= maxPerm {
		return nil
	}
	return &vulnItem{
		Name:           fmt.Sprintf("weak_permissions_%s", strings.ReplaceAll(filepath.Base(path), ".", "_")),
		Severity:       "medium",
		Description:    fmt.Sprintf("%s has permissions %s (should be %s or more restrictive)", path, perm, maxPerm),
		Recommendation: fmt.Sprintf("Fix permissions: chmod %o %s", maxPerm, path),
	}
}
