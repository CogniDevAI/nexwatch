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

	content := string(data)
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

	mode := info.Mode().Perm()
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

	mode := info.Mode().Perm()
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

	rootUsers := []string{}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
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
