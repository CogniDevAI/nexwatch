//go:build windows

package collector

import (
	"context"
	"log"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Collect enumerates Windows services via the Service Control Manager and
// maps each one's status and start type through mapWindowsService
// (services_mapping.go), then aggregates them the same way
// services_linux.go's systemctl-based collector does. A service this
// process lacks SERVICE_QUERY_STATUS access to is silently omitted,
// matching mgr.ListServices's own documented behavior, rather than
// failing the whole collection.
func (c *ServicesCollector) Collect(_ context.Context) (map[string]any, error) {
	m, err := mgr.Connect()
	if err != nil {
		log.Printf("[services] connect to service control manager: %v", err)
		return summarizeWindowsServices(nil), nil
	}
	defer func() { _ = m.Disconnect() }()

	names, err := m.ListServices()
	if err != nil {
		log.Printf("[services] list services: %v", err)
		return summarizeWindowsServices(nil), nil
	}

	services := make([]map[string]any, 0, len(names))
	for _, name := range names {
		if len(services) >= 100 {
			break
		}
		if mapped, ok := queryWindowsService(m, name); ok {
			services = append(services, mapped)
		}
	}

	return summarizeWindowsServices(services), nil
}

// queryWindowsService opens, queries, and closes a single service by name,
// converting its status/config through mapWindowsService. ok is false when
// the service could not be opened or its status queried (e.g. it was
// deleted between ListServices and here, or this process lacks access) —
// that service is simply omitted rather than reported as an error. A
// config query failure (rarer than a status query failure, and not fatal
// to reporting the service at all) degrades to an unknown start type and
// empty description instead of omitting the service.
func queryWindowsService(m *mgr.Mgr, name string) (map[string]any, bool) {
	s, err := m.OpenService(name)
	if err != nil {
		return nil, false
	}
	defer func() { _ = s.Close() }()

	status, err := s.Query()
	if err != nil {
		return nil, false
	}

	description := ""
	startType := windowsStartUnknown
	if cfg, cfgErr := s.Config(); cfgErr == nil {
		description = cfg.Description
		startType = mapWindowsStartType(cfg.StartType, cfg.DelayedAutoStart)
	}

	return mapWindowsService(name, mapWindowsServiceState(status.State), startType, description), true
}

// mapWindowsServiceState converts a raw svc.State (from Service.Query) into
// the package's platform-independent windowsServiceState.
func mapWindowsServiceState(s svc.State) windowsServiceState {
	switch s {
	case svc.Running:
		return windowsServiceRunning
	case svc.Stopped:
		return windowsServiceStopped
	case svc.Paused:
		return windowsServicePaused
	case svc.StartPending:
		return windowsServiceStartPending
	case svc.StopPending:
		return windowsServiceStopPending
	case svc.ContinuePending:
		return windowsServiceContinuePending
	case svc.PausePending:
		return windowsServicePausePending
	default:
		return windowsServiceUnknown
	}
}

// mapWindowsStartType converts a raw mgr.Config.StartType (a
// windows.SERVICE_*_START constant) plus its DelayedAutoStart flag into
// the package's platform-independent windowsStartType.
func mapWindowsStartType(raw uint32, delayedAutoStart bool) windowsStartType {
	switch raw {
	case mgr.StartAutomatic:
		if delayedAutoStart {
			return windowsStartAutomaticDelayed
		}
		return windowsStartAutomatic
	case mgr.StartManual:
		return windowsStartManual
	case mgr.StartDisabled:
		return windowsStartDisabled
	default:
		return windowsStartUnknown
	}
}
