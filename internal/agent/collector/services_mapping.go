package collector

// This file holds the Windows services collector's state-mapping logic as
// pure functions with no dependency on golang.org/x/sys/windows, so it
// compiles and is unit-tested on every platform (including this repo's own
// Linux/macOS dev and CI machines) even though the collector itself only
// registers on Windows. services_windows.go calls mapWindowsService with
// values read from the real Service Control Manager via
// golang.org/x/sys/windows/svc/mgr.

// windowsServiceState mirrors the subset of Windows SERVICE_STATUS
// "CurrentState" values this collector distinguishes.
type windowsServiceState int

const (
	windowsServiceUnknown windowsServiceState = iota
	windowsServiceRunning
	windowsServiceStopped
	windowsServicePaused
	windowsServiceStartPending
	windowsServiceStopPending
	windowsServiceContinuePending
	windowsServicePausePending
)

// windowsStartType mirrors the subset of Windows service "start type"
// (QUERY_SERVICE_CONFIG.dwStartType) values this collector distinguishes.
type windowsStartType int

const (
	windowsStartUnknown windowsStartType = iota
	windowsStartAutomatic
	windowsStartAutomaticDelayed
	windowsStartManual
	windowsStartDisabled
)

// mapWindowsService converts one Windows service's name, current state,
// configured start type, and display description into the same
// {name, load, active, sub, description} payload shape the Linux
// systemctl-based collector (services_linux.go) produces, so hub alert
// rules — specifically service_failed, which reads only "active"/"sub" via
// internal/hub/alerts.Engine.isFailedServiceState's "failed"/"inactive"/
// "dead" vocabulary — evaluate identically regardless of the agent's OS.
//
// A service that is Stopped while configured to start automatically is
// mapped to sub="failed": on Windows, unlike a manually-stopped or
// disabled service, an automatic service being stopped means it crashed,
// was killed, or failed to start after boot — the same "this should be
// running and isn't" signal a Linux "failed" systemd unit carries. A
// Stopped service with any other start type maps to sub="stopped" (not
// "failed"/"inactive"/"dead") so it does not spuriously breach a
// service_failed rule an operator points at it, matching the expectation
// that a manually-managed or disabled service being off is normal, not an
// incident.
func mapWindowsService(name string, state windowsServiceState, startType windowsStartType, description string) map[string]any {
	var active, sub string

	switch state {
	case windowsServiceRunning:
		active = "active"
		sub = "running"
	case windowsServicePaused:
		active = "active"
		sub = "paused"
	case windowsServiceStartPending, windowsServiceContinuePending:
		active = "activating"
		sub = "start-pending"
	case windowsServiceStopPending, windowsServicePausePending:
		active = "deactivating"
		sub = "stop-pending"
	case windowsServiceStopped:
		active = "inactive"
		if startType == windowsStartAutomatic || startType == windowsStartAutomaticDelayed {
			sub = "failed"
		} else {
			sub = "stopped"
		}
	default:
		active = "unknown"
		sub = "unknown"
	}

	return map[string]any{
		"name":        name,
		"load":        "loaded",
		"active":      active,
		"sub":         sub,
		"start_type":  windowsStartTypeString(startType),
		"description": description,
	}
}

// windowsStartTypeString renders a windowsStartType for the payload's
// informational "start_type" field.
func windowsStartTypeString(t windowsStartType) string {
	switch t {
	case windowsStartAutomatic:
		return "automatic"
	case windowsStartAutomaticDelayed:
		return "automatic (delayed)"
	case windowsStartManual:
		return "manual"
	case windowsStartDisabled:
		return "disabled"
	default:
		return "unknown"
	}
}

// summarizeWindowsServices aggregates a slice of already-mapped Windows
// service payloads (see mapWindowsService) into the same
// {services, total, running, failed, other} envelope the Linux collector
// produces, capped at the same 100-service limit.
func summarizeWindowsServices(services []map[string]any) map[string]any {
	if len(services) > 100 {
		services = services[:100]
	}

	running, failed := 0, 0
	for _, s := range services {
		switch s["sub"] {
		case "running":
			running++
		case "failed":
			failed++
		}
	}

	total := len(services)
	other := total - running - failed

	return map[string]any{
		"services": services,
		"total":    total,
		"running":  running,
		"failed":   failed,
		"other":    other,
	}
}
