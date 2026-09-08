package collector

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/net"
)

// HardeningCollector performs basic server security hardening checks.
type HardeningCollector struct{}

// NewHardeningCollector creates a new hardening collector.
func NewHardeningCollector() *HardeningCollector {
	return &HardeningCollector{}
}

// Name returns the collector identifier.
func (c *HardeningCollector) Name() string { return "hardening" }

// checkResult represents the outcome of a single hardening check.
type checkResult struct {
	Name        string `json:"name"`
	Status      string `json:"status"` // pass, fail, warn, skip
	Description string `json:"description"`
	Severity    string `json:"severity"` // low, medium, high, critical
}

// Collect runs all hardening checks and returns aggregate results.
func (c *HardeningCollector) Collect(ctx context.Context) (map[string]any, error) {
	checks := []checkResult{}

	checks = append(checks, c.checkSSHRootLogin())
	checks = append(checks, c.checkFirewall())
	checks = append(checks, c.checkPasswdPermissions())
	checks = append(checks, c.checkShadowPermissions())
	checks = append(checks, c.checkExtraRootUsers())
	checks = append(checks, c.checkDangerousPorts(ctx)...)
	checks = append(checks, c.checkWindowsDefenderStatus())
	checks = append(checks, c.checkRDPStatus())

	// Calculate score.
	total := 0
	passed := 0
	failed := 0
	warnings := 0
	for _, check := range checks {
		switch check.Status {
		case "pass":
			total++
			passed++
		case "fail":
			total++
			failed++
		case "warn":
			total++
			warnings++
		case "skip":
			// Skip does not count toward total.
		}
	}

	score := 0.0
	if total > 0 {
		score = float64(passed) / float64(total) * 100
	}

	// Convert checks to []map[string]any.
	checksData := make([]map[string]any, 0, len(checks))
	for _, ch := range checks {
		checksData = append(checksData, map[string]any{
			"name":        ch.Name,
			"status":      ch.Status,
			"description": ch.Description,
			"severity":    ch.Severity,
		})
	}

	return map[string]any{
		"checks":   checksData,
		"score":    int(score),
		"total":    total,
		"passed":   passed,
		"failed":   failed,
		"warnings": warnings,
	}, nil
}

// checkSSHRootLogin verifies that SSH root login is disabled.
func (c *HardeningCollector) checkSSHRootLogin() checkResult {
	if runtime.GOOS == "windows" {
		return checkResult{
			Name:        "ssh_root_login",
			Status:      "skip",
			Description: "SSH root login check not applicable on Windows",
			Severity:    "high",
		}
	}

	data, err := os.ReadFile("/etc/ssh/sshd_config")
	if err != nil {
		return checkResult{
			Name:        "ssh_root_login",
			Status:      "skip",
			Description: fmt.Sprintf("Cannot read sshd_config: %v", err),
			Severity:    "high",
		}
	}

	return evaluateSSHRootLoginConfig(string(data))
}

// evaluateSSHRootLoginConfig inspects the content of an sshd_config file for
// the PermitRootLogin directive and reports whether root login is
// restricted. It is a pure function over the file content so the parsing
// logic can be tested with fixture strings instead of a real sshd_config.
func evaluateSSHRootLoginConfig(content string) checkResult {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "permitrootlogin") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				val := strings.ToLower(fields[1])
				if val == "no" || val == "prohibit-password" || val == "forced-commands-only" {
					return checkResult{
						Name:        "ssh_root_login",
						Status:      "pass",
						Description: fmt.Sprintf("SSH root login is restricted (%s)", fields[1]),
						Severity:    "high",
					}
				}
				return checkResult{
					Name:        "ssh_root_login",
					Status:      "fail",
					Description: fmt.Sprintf("SSH root login is set to '%s' — should be 'no' or 'prohibit-password'", fields[1]),
					Severity:    "high",
				}
			}
		}
	}

	// If PermitRootLogin is not found, default depends on OS but is often "yes".
	return checkResult{
		Name:        "ssh_root_login",
		Status:      "warn",
		Description: "PermitRootLogin not explicitly set in sshd_config (default may allow root login)",
		Severity:    "high",
	}
}

// checkFirewall checks if a firewall is active.
func (c *HardeningCollector) checkFirewall() checkResult {
	switch runtime.GOOS {
	case "linux":
		return c.checkFirewallLinux()
	case "darwin":
		return c.checkFirewallDarwin()
	case "windows":
		return c.checkFirewallWindows()
	default:
		return checkResult{
			Name:        "firewall_active",
			Status:      "skip",
			Description: fmt.Sprintf("Firewall check not implemented for %s", runtime.GOOS),
			Severity:    "high",
		}
	}
}

func (c *HardeningCollector) checkFirewallLinux() checkResult {
	// Check iptables.
	out, err := exec.Command("iptables", "-L", "-n").CombinedOutput()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		ruleCount := 0
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "Chain") || strings.HasPrefix(line, "target") {
				continue
			}
			ruleCount++
		}
		if ruleCount > 0 {
			return checkResult{
				Name:        "firewall_active",
				Status:      "pass",
				Description: fmt.Sprintf("iptables has %d rules configured", ruleCount),
				Severity:    "high",
			}
		}
	}

	// Check nftables.
	if out, err := exec.Command("nft", "list", "ruleset").CombinedOutput(); err == nil {
		if len(strings.TrimSpace(string(out))) > 10 {
			return checkResult{
				Name:        "firewall_active",
				Status:      "pass",
				Description: "nftables ruleset is active",
				Severity:    "high",
			}
		}
	}

	// Check ufw.
	if out, err := exec.Command("ufw", "status").CombinedOutput(); err == nil {
		if strings.Contains(string(out), "active") {
			return checkResult{
				Name:        "firewall_active",
				Status:      "pass",
				Description: "UFW firewall is active",
				Severity:    "high",
			}
		}
	}

	return checkResult{
		Name:        "firewall_active",
		Status:      "fail",
		Description: "No active firewall detected (iptables/nftables/ufw)",
		Severity:    "high",
	}
}

func (c *HardeningCollector) checkFirewallDarwin() checkResult {
	// Check macOS Application Firewall via socketfilterfw.
	out, err := exec.Command("/usr/libexec/ApplicationFirewall/socketfilterfw", "--getglobalstate").CombinedOutput()
	if err == nil {
		output := string(out)
		if strings.Contains(output, "enabled") {
			return checkResult{
				Name:        "firewall_active",
				Status:      "pass",
				Description: "macOS Application Firewall is enabled",
				Severity:    "high",
			}
		}
		return checkResult{
			Name:        "firewall_active",
			Status:      "fail",
			Description: "macOS Application Firewall is disabled",
			Severity:    "high",
		}
	}

	// Check pf.
	if out, err := exec.Command("pfctl", "-s", "info").CombinedOutput(); err == nil {
		if strings.Contains(string(out), "Enabled") {
			return checkResult{
				Name:        "firewall_active",
				Status:      "pass",
				Description: "PF firewall is enabled",
				Severity:    "high",
			}
		}
	}

	return checkResult{
		Name:        "firewall_active",
		Status:      "warn",
		Description: "Could not determine firewall status on macOS",
		Severity:    "high",
	}
}

// checkFirewallWindows checks Windows Firewall's per-profile enabled state
// via netsh, which ships with every supported Windows version — no
// PowerShell dependency needed for this one. Parsing lives in the pure
// evaluateWindowsFirewallOutput so it is testable with fixture output.
func (c *HardeningCollector) checkFirewallWindows() checkResult {
	out, err := exec.Command("netsh", "advfirewall", "show", "allprofiles", "state").CombinedOutput()
	if err != nil {
		return checkResult{
			Name:        "firewall_active",
			Status:      "skip",
			Description: fmt.Sprintf("could not run netsh advfirewall: %v", err),
			Severity:    "high",
		}
	}
	return evaluateWindowsFirewallOutput(string(out))
}

// evaluateWindowsFirewallOutput parses `netsh advfirewall show allprofiles
// state` output (one "State  ON"/"State  OFF" line per profile —
// Domain/Private/Public) into a checkResult. It is pure over the command's
// output text so it can be tested with fixture strings instead of a real
// netsh invocation (this repo's Linux/macOS dev and CI machines have no
// netsh at all).
func evaluateWindowsFirewallOutput(output string) checkResult {
	var states []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "State") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			states = append(states, strings.ToUpper(fields[len(fields)-1]))
		}
	}

	if len(states) == 0 {
		return checkResult{
			Name:        "firewall_active",
			Status:      "skip",
			Description: "could not parse netsh advfirewall output",
			Severity:    "high",
		}
	}

	for _, s := range states {
		if s != "ON" {
			return checkResult{
				Name:        "firewall_active",
				Status:      "fail",
				Description: "Windows Firewall is disabled on at least one profile",
				Severity:    "high",
			}
		}
	}

	return checkResult{
		Name:        "firewall_active",
		Status:      "pass",
		Description: fmt.Sprintf("Windows Firewall is enabled on all %d profile(s)", len(states)),
		Severity:    "high",
	}
}

// checkWindowsDefenderStatus reports Windows Defender's real-time
// protection state via PowerShell's Get-MpComputerStatus cmdlet. It skips
// (rather than fails) on any other OS, and skips just as gracefully when
// PowerShell or the Defender module itself isn't present — e.g. a Windows
// Server Core install without the module, or a host running a third-party
// antivirus that has disabled the Defender cmdlets — since that is a
// legitimate configuration, not a probe failure worth surfacing as
// warn/fail.
func (c *HardeningCollector) checkWindowsDefenderStatus() checkResult {
	if runtime.GOOS != "windows" {
		return checkResult{
			Name:        "defender_realtime_protection",
			Status:      "skip",
			Description: fmt.Sprintf("Windows Defender check not applicable on %s", runtime.GOOS),
			Severity:    "high",
		}
	}

	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command",
		"(Get-MpComputerStatus).RealTimeProtectionEnabled").CombinedOutput()
	if err != nil {
		return checkResult{
			Name:        "defender_realtime_protection",
			Status:      "skip",
			Description: fmt.Sprintf("could not query Windows Defender status: %v", err),
			Severity:    "high",
		}
	}
	return evaluateWindowsDefenderOutput(string(out))
}

// evaluateWindowsDefenderOutput parses Get-MpComputerStatus's
// RealTimeProtectionEnabled output ("True"/"False", plus a trailing
// newline) into a checkResult. Pure over the output text for the same
// testability reason as evaluateWindowsFirewallOutput above.
func evaluateWindowsDefenderOutput(output string) checkResult {
	switch strings.ToLower(strings.TrimSpace(output)) {
	case "true":
		return checkResult{
			Name:        "defender_realtime_protection",
			Status:      "pass",
			Description: "Windows Defender real-time protection is enabled",
			Severity:    "high",
		}
	case "false":
		return checkResult{
			Name:        "defender_realtime_protection",
			Status:      "fail",
			Description: "Windows Defender real-time protection is disabled",
			Severity:    "high",
		}
	default:
		return checkResult{
			Name:        "defender_realtime_protection",
			Status:      "skip",
			Description: "could not determine Windows Defender status (Get-MpComputerStatus unavailable)",
			Severity:    "high",
		}
	}
}

// evaluateRDPDenyFlag interprets the Windows "fDenyTSConnections" registry
// value (HKLM\System\CurrentControlSet\Control\Terminal Server — 0 means
// Remote Desktop is enabled, non-zero means disabled) into a checkResult.
// ok is false when the value could not be read at all (missing key/value,
// access denied, or simply not running on Windows — see
// hardening_windows.go/hardening_other.go), in which case the check is
// skipped rather than guessed. Kept pure (no registry access itself) so it
// is testable on every platform, including this repo's own Linux/macOS
// dev and CI machines, which cannot read a Windows registry at all.
func evaluateRDPDenyFlag(denyTSConnections uint64, ok bool) checkResult {
	if !ok {
		return checkResult{
			Name:        "rdp_exposure",
			Status:      "skip",
			Description: "could not read fDenyTSConnections from the registry",
			Severity:    "medium",
		}
	}
	if denyTSConnections == 0 {
		return checkResult{
			Name:        "rdp_exposure",
			Status:      "warn",
			Description: "Remote Desktop (RDP) is enabled — ensure it is intentional and properly secured",
			Severity:    "medium",
		}
	}
	return checkResult{
		Name:        "rdp_exposure",
		Status:      "pass",
		Description: "Remote Desktop (RDP) is disabled",
		Severity:    "medium",
	}
}

// checkPasswdPermissions verifies /etc/passwd has correct permissions.
func (c *HardeningCollector) checkPasswdPermissions() checkResult {
	if runtime.GOOS == "windows" {
		return checkResult{
			Name:        "passwd_permissions",
			Status:      "skip",
			Description: "File permission check not applicable on Windows",
			Severity:    "medium",
		}
	}

	info, err := os.Stat("/etc/passwd")
	if err != nil {
		return checkResult{
			Name:        "passwd_permissions",
			Status:      "skip",
			Description: fmt.Sprintf("Cannot stat /etc/passwd: %v", err),
			Severity:    "medium",
		}
	}

	return evaluatePasswdPermissions(info.Mode().Perm())
}

// evaluatePasswdPermissions checks a /etc/passwd file mode for group/other
// write bits. It is pure over the mode value so it can be tested without
// touching the filesystem.
func evaluatePasswdPermissions(mode os.FileMode) checkResult {
	if mode&0o022 == 0 { // No write by group or others.
		return checkResult{
			Name:        "passwd_permissions",
			Status:      "pass",
			Description: fmt.Sprintf("/etc/passwd permissions are %s (no group/other write)", mode),
			Severity:    "medium",
		}
	}

	return checkResult{
		Name:        "passwd_permissions",
		Status:      "fail",
		Description: fmt.Sprintf("/etc/passwd has overly permissive permissions: %s", mode),
		Severity:    "medium",
	}
}

// checkShadowPermissions verifies /etc/shadow has correct permissions.
func (c *HardeningCollector) checkShadowPermissions() checkResult {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return checkResult{
			Name:        "shadow_permissions",
			Status:      "skip",
			Description: fmt.Sprintf("Shadow file check not applicable on %s", runtime.GOOS),
			Severity:    "high",
		}
	}

	info, err := os.Stat("/etc/shadow")
	if err != nil {
		return checkResult{
			Name:        "shadow_permissions",
			Status:      "skip",
			Description: fmt.Sprintf("Cannot stat /etc/shadow: %v", err),
			Severity:    "high",
		}
	}

	return evaluateShadowPermissions(info.Mode().Perm())
}

// evaluateShadowPermissions checks a /etc/shadow file mode for
// read/write/exec-by-others or write-by-group bits. It is pure over the
// mode value so it can be tested without touching the filesystem.
func evaluateShadowPermissions(mode os.FileMode) checkResult {
	// Shadow should be readable only by root (0600 or 0640).
	if mode&0o037 == 0 { // No read/write/exec by others, no write by group.
		return checkResult{
			Name:        "shadow_permissions",
			Status:      "pass",
			Description: fmt.Sprintf("/etc/shadow permissions are %s", mode),
			Severity:    "high",
		}
	}

	return checkResult{
		Name:        "shadow_permissions",
		Status:      "fail",
		Description: fmt.Sprintf("/etc/shadow has overly permissive permissions: %s", mode),
		Severity:    "high",
	}
}

// checkExtraRootUsers verifies no extra accounts have UID 0.
func (c *HardeningCollector) checkExtraRootUsers() checkResult {
	if runtime.GOOS == "windows" {
		return checkResult{
			Name:        "extra_root_users",
			Status:      "skip",
			Description: "UID check not applicable on Windows",
			Severity:    "critical",
		}
	}

	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return checkResult{
			Name:        "extra_root_users",
			Status:      "skip",
			Description: fmt.Sprintf("Cannot read /etc/passwd: %v", err),
			Severity:    "critical",
		}
	}

	return evaluateExtraRootUsers(string(data))
}

// evaluateExtraRootUsers scans /etc/passwd-formatted content for accounts
// with UID 0. It is a pure function over the file content so the parsing
// logic can be tested with fixture strings instead of a real /etc/passwd.
func evaluateExtraRootUsers(passwdContent string) checkResult {
	rootUsers := []string{}
	scanner := bufio.NewScanner(strings.NewReader(passwdContent))
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Split(line, ":")
		if len(fields) >= 3 && fields[2] == "0" {
			rootUsers = append(rootUsers, fields[0])
		}
	}

	if len(rootUsers) <= 1 {
		return checkResult{
			Name:        "extra_root_users",
			Status:      "pass",
			Description: "Only 'root' has UID 0",
			Severity:    "critical",
		}
	}

	return checkResult{
		Name:        "extra_root_users",
		Status:      "fail",
		Description: fmt.Sprintf("Multiple accounts with UID 0: %s", strings.Join(rootUsers, ", ")),
		Severity:    "critical",
	}
}

// checkDangerousPorts checks if known dangerous ports are open (21/FTP, 23/Telnet, 3389/RDP).
func (c *HardeningCollector) checkDangerousPorts(ctx context.Context) []checkResult {
	dangerousPorts := map[uint32]string{
		21:   "FTP",
		23:   "Telnet",
		3389: "RDP",
	}

	conns, err := net.ConnectionsWithContext(ctx, "inet")
	if err != nil {
		results := make([]checkResult, 0, len(dangerousPorts))
		for port, svc := range dangerousPorts {
			results = append(results, checkResult{
				Name:        fmt.Sprintf("dangerous_port_%d", port),
				Status:      "skip",
				Description: fmt.Sprintf("Cannot check %s (port %d): %v", svc, port, err),
				Severity:    "high",
			})
		}
		return results
	}

	// Build set of listening ports.
	listeningPorts := make(map[uint32]bool)
	for _, conn := range conns {
		if conn.Status == "LISTEN" {
			listeningPorts[conn.Laddr.Port] = true
		}
	}

	return evaluateDangerousPorts(dangerousPorts, listeningPorts)
}

// evaluateDangerousPorts reports pass/fail for each dangerous port depending
// on whether it appears in the listening-ports set. It is a pure function
// over both maps so it can be tested without a real network connection
// snapshot.
func evaluateDangerousPorts(dangerousPorts map[uint32]string, listeningPorts map[uint32]bool) []checkResult {
	results := make([]checkResult, 0, len(dangerousPorts))
	for port, svc := range dangerousPorts {
		if listeningPorts[port] {
			results = append(results, checkResult{
				Name:        fmt.Sprintf("dangerous_port_%d", port),
				Status:      "fail",
				Description: fmt.Sprintf("%s service detected on port %d — consider disabling", svc, port),
				Severity:    "high",
			})
		} else {
			results = append(results, checkResult{
				Name:        fmt.Sprintf("dangerous_port_%d", port),
				Status:      "pass",
				Description: fmt.Sprintf("No %s service listening on port %d", svc, port),
				Severity:    "high",
			})
		}
	}

	return results
}
