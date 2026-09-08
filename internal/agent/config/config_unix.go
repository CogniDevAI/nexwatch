//go:build !windows

package config

// defaultCollectorsEnabled returns the default collector list for
// Linux/macOS, including the Linux-only oracle/vulnerabilities collectors
// (oracle is opt-in via oracle_home/oracle_sid regardless; vulnerabilities
// runs unconditionally once enabled). cmd/agent's registerCollectors still
// consults internal/agent/platform.Supported at registration time, so this
// list is also what a Windows host running with a config carried over
// from a Linux one would fall back to skipping — see
// defaultCollectorsEnabled in config_windows.go for that platform's own,
// narrower default.
func defaultCollectorsEnabled() []string {
	return []string{
		"cpu", "memory", "disk", "network", "sysinfo", "docker",
		"ports", "processes", "hardening", "vulnerabilities",
		"diskio", "connections", "services", "cve_scan",
	}
}

// defaultDockerSocket returns the standard Unix Docker socket path.
func defaultDockerSocket() string { return "/var/run/docker.sock" }

// defaultCveScanCacheDir returns the standard Linux path for the cve_scan
// collector's Trivy/Grype database cache.
func defaultCveScanCacheDir() string { return "/var/lib/nexwatch/scanner-cache" }

// defaultSystemConfigPath returns the system-wide agent.yaml location
// probed by Load() after "./agent.yaml", matching
// scripts/install-agent.sh's CONFIG_DIR and deploy/nexwatch-agent.service.
func defaultSystemConfigPath() string { return "/etc/nexwatch/agent.yaml" }
