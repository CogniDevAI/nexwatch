//go:build !windows

package main

import (
	"fmt"
	"runtime"

	"github.com/CogniDevAI/nexwatch/internal/agent/config"
)

// isWindowsService always reports false outside Windows: there is no
// Service Control Manager to run under, so main() always takes the
// standalone signal-driven path.
func isWindowsService() bool { return false }

// runWindowsService is never called on this platform (isWindowsService
// always returns false first), but is defined here so main.go compiles
// unconditionally rather than needing its own build tags.
func runWindowsService(cfg *config.Config) {
	panic("runWindowsService called on a non-Windows platform")
}

// handleServiceCommand reports that --service management is Windows-only.
// On Linux, the equivalent lifecycle is a systemd unit — see
// scripts/install-agent.sh and deploy/nexwatch-agent.service.
func handleServiceCommand(action string, extraArgs []string) error {
	return fmt.Errorf("--service %s is not supported on %s (this is a Windows-only feature; see scripts/install-agent.sh for the Linux/systemd install path)", action, runtime.GOOS)
}
