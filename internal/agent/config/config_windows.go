//go:build windows

package config

import "os"

// ProgramDataDir exports programDataDir for cmd/agent's Windows service
// integration (service_windows.go), which needs the same
// %ProgramData%\NexWatch directory this package's own defaults
// (defaultCveScanCacheDir, defaultSystemConfigPath) resolve into, for its
// rotating agent.log.
func ProgramDataDir() string { return programDataDir() }

// programDataDir resolves %ProgramData% (normally "C:\ProgramData"),
// falling back to that literal default in the rare case the environment
// variable itself is unset — Windows sets it for every process by
// default, but a minimal or misconfigured launch environment (e.g. some
// container base images, or a service started with a stripped
// environment block) might not.
func programDataDir() string {
	if v := os.Getenv("ProgramData"); v != "" {
		return v
	}
	return `C:\ProgramData`
}

// defaultCollectorsEnabled returns the default collector list for Windows:
// the same cross-platform set as Linux/macOS, minus oracle and
// vulnerabilities, which have no Windows implementation (see
// internal/agent/platform.Supported) — omitting them from the default
// avoids a "not supported on windows" log line on every normal Windows
// install, while an operator who explicitly lists either one in their own
// agent.yaml still gets that same graceful skip rather than a broken
// collector silently registering.
func defaultCollectorsEnabled() []string {
	return []string{
		"cpu", "memory", "disk", "network", "sysinfo", "docker",
		"ports", "processes", "hardening",
		"diskio", "connections", "services", "cve_scan",
	}
}

// defaultDockerSocket returns Docker Desktop/Docker Engine's default named
// pipe on Windows. See collector.DockerHostURL, which passes a value
// already containing "://" straight through to the Docker client instead
// of prefixing it with "unix://".
func defaultDockerSocket() string { return "npipe:////./pipe/docker_engine" }

// defaultCveScanCacheDir returns the Windows path for the cve_scan
// collector's Trivy/Grype database cache, alongside the agent's config and
// log files under %ProgramData%\NexWatch.
func defaultCveScanCacheDir() string { return programDataDir() + `\NexWatch\scanner-cache` }

// defaultSystemConfigPath returns the system-wide agent.yaml location
// probed by Load() after ".\agent.yaml", matching
// scripts/install-agent.ps1 and cmd/agent's Windows service integration.
func defaultSystemConfigPath() string { return programDataDir() + `\NexWatch\agent.yaml` }
