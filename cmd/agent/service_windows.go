//go:build windows

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/CogniDevAI/nexwatch/internal/agent/config"
)

// serviceName/serviceDisplayName identify the Windows service
// scripts/install-agent.ps1 and the "--service install" subcommand below
// both register. serviceName is also used as the Event Log source name.
const (
	serviceName        = "NexWatchAgent"
	serviceDisplayName = "NexWatch Agent"
	serviceDescription = "Collects and reports host metrics, security posture, and logs to a NexWatch hub."
)

// maxServiceLogBytes bounds %ProgramData%\NexWatch\agent.log before
// logrotate.go's rotatingWriter rotates it to agent.log.1.
const maxServiceLogBytes = 10 * 1024 * 1024 // 10 MiB

// isWindowsService reports whether this process was started by the
// Service Control Manager (as opposed to an interactive console session).
// A query failure is treated as "no" — the safer default, since it falls
// back to the normal signal-driven console path rather than silently
// trying (and failing) to call svc.Run outside an actual service context.
func isWindowsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// runWindowsService redirects log output to a rotating
// %ProgramData%\NexWatch\agent.log (there is no console to write to under
// the SCM, and stdout/stderr are simply discarded), then runs the agent
// under svc.Run, whose handler (agentServiceHandler) drives runAgent's
// lifecycle from SCM start/stop/shutdown requests.
func runWindowsService(cfg *config.Config) {
	logDir := filepath.Join(config.ProgramDataDir(), "NexWatch")
	if err := os.MkdirAll(logDir, 0o755); err != nil { //nolint:gosec // ProgramData is not a sensitive location; matches this directory's existing agent.yaml/scanner-cache permissions.
		// Fall back to whatever default output log already has (typically
		// discarded by the SCM) rather than aborting the service.
		log.Printf("[agent] could not create log directory %s: %v (logging may be lost)", logDir, err)
	} else {
		logPath := filepath.Join(logDir, "agent.log")
		if w, err := newRotatingWriter(logPath, maxServiceLogBytes); err != nil {
			log.Printf("[agent] could not open service log %s: %v (logging may be lost)", logPath, err)
		} else {
			defer func() { _ = w.Close() }()
			log.SetOutput(w)
		}
	}

	log.Printf("NexWatch Agent %s starting as a Windows service", version)
	log.Printf("Hub: %s | Interval: %s", cfg.HubURL, cfg.Interval)

	if err := svc.Run(serviceName, &agentServiceHandler{cfg: cfg}); err != nil {
		log.Printf("[agent] service run failed: %v", err)
	}
}

// agentServiceHandler adapts runAgent's context-driven lifecycle to
// svc.Handler's Execute contract: report StartPending while runAgent spins
// up, Running once it does, translate a Stop/Shutdown control request into
// canceling runAgent's context, and report Stopped once it has returned.
type agentServiceHandler struct {
	cfg *config.Config
}

func (h *agentServiceHandler) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (svcSpecificEC bool, exitCode uint32) {
	changes <- svc.Status{State: svc.StartPending}

	elog, elogErr := eventlog.Open(serviceName)
	logEvent := func(info bool, msg string) {
		if elogErr != nil || elog == nil {
			return // best-effort only — see runWindowsService's own doc comment on log output.
		}
		if info {
			_ = elog.Info(1, msg)
		} else {
			_ = elog.Error(1, msg)
		}
	}
	if elogErr == nil {
		defer func() { _ = elog.Close() }()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // always released: the loop below also calls it explicitly on a Stop/Shutdown request, which is a safe, idiomatic no-op the second time.
	done := make(chan struct{})
	go func() {
		defer close(done)
		runAgent(ctx, h.cfg)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	logEvent(true, fmt.Sprintf("NexWatch Agent %s started", version))

loop:
	for {
		select {
		case <-done:
			// runAgent returned on its own (should not normally happen —
			// it only returns when its ctx is canceled, and nothing but
			// this handler holds cancel — but exit cleanly either way
			// rather than hanging the service in Running forever).
			break loop
		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				changes <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				break loop
			}
		}
	}

	logEvent(true, fmt.Sprintf("NexWatch Agent %s stopped", version))
	changes <- svc.Status{State: svc.Stopped}
	return false, 0
}

// handleServiceCommand implements "--service <action> [--config <path>]".
func handleServiceCommand(action string, extraArgs []string) error {
	switch action {
	case "install":
		return installService(extraArgs)
	case "uninstall":
		return uninstallService()
	case "start":
		return startService()
	case "stop":
		return stopService()
	case "status":
		return statusService()
	default:
		return fmt.Errorf("unknown --service action %q (want install, uninstall, start, stop, or status)", action)
	}
}

// serviceConfigPath extracts an optional "--config <path>" from
// installService's extraArgs, defaulting to
// config.ProgramDataDir()\NexWatch\agent.yaml (the same path
// config.Load() itself falls back to — see config_windows.go's
// defaultSystemConfigPath) when not given.
func serviceConfigPath(extraArgs []string) string {
	for i, arg := range extraArgs {
		if arg == "--config" && i+1 < len(extraArgs) {
			return extraArgs[i+1]
		}
		if v, ok := strings.CutPrefix(arg, "--config="); ok {
			return v
		}
	}
	return filepath.Join(config.ProgramDataDir(), "NexWatch", "agent.yaml")
}

// installService registers the NexWatchAgent Windows service, pointing it
// at this process's own executable with "--config <path>" (see
// serviceConfigPath), set to start automatically and to restart itself 2
// seconds after an unexpected failure — the Windows equivalent of
// deploy/nexwatch-agent.service's Restart=always/RestartSec=2 for Linux.
// It also best-effort registers an Event Log source for Start/Stop
// messages (agentServiceHandler); a failure there does not fail the
// install, since the service is fully functional without it.
func installService(extraArgs []string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve own executable path: %w", err)
	}
	configPath := serviceConfigPath(extraArgs)

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	if existing, err := m.OpenService(serviceName); err == nil {
		_ = existing.Close()
		return fmt.Errorf("service %q is already installed (run \"--service uninstall\" first to reinstall)", serviceName)
	}

	s, err := m.CreateService(serviceName, exePath, mgr.Config{
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		StartType:   mgr.StartAutomatic,
	}, "--config", configPath)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 2 * time.Second},
	}, 86400); err != nil { // reset the failure count after a day with no further failures.
		log.Printf("[agent] service installed, but setting recovery actions failed (the service will not auto-restart on crash): %v", err)
	}

	if err := eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info); err != nil {
		log.Printf("[agent] service installed, but registering the Event Log source failed (Start/Stop events will not appear in Event Viewer): %v", err)
	}

	fmt.Printf("Service %q installed (config: %s)\n", serviceName, configPath)
	return nil
}

// uninstallService removes the NexWatchAgent service registration (and its
// best-effort Event Log source), stopping it first if it is running.
func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed: %w", serviceName, err)
	}
	defer func() { _ = s.Close() }()

	if status, err := s.Query(); err == nil && status.State != svc.Stopped {
		if _, err := s.Control(svc.Stop); err != nil {
			log.Printf("[agent] could not stop service before uninstalling (continuing anyway): %v", err)
		}
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}

	if err := eventlog.Remove(serviceName); err != nil {
		log.Printf("[agent] service uninstalled, but removing the Event Log source failed (harmless): %v", err)
	}

	fmt.Printf("Service %q uninstalled\n", serviceName)
	return nil
}

// startService starts the already-installed NexWatchAgent service.
func startService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed: %w", serviceName, err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	fmt.Printf("Service %q started\n", serviceName)
	return nil
}

// stopService sends a stop control request to the NexWatchAgent service.
func stopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed: %w", serviceName, err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.Control(svc.Stop); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	fmt.Printf("Service %q stop requested\n", serviceName)
	return nil
}

// statusService prints the NexWatchAgent service's current SCM state.
func statusService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %q is not installed: %w", serviceName, err)
	}
	defer func() { _ = s.Close() }()

	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service status: %w", err)
	}
	fmt.Printf("Service %q: %s\n", serviceName, serviceStateString(status.State))
	return nil
}

// serviceStateString renders an svc.State for statusService's output.
func serviceStateString(s svc.State) string {
	switch s {
	case svc.Running:
		return "running"
	case svc.Stopped:
		return "stopped"
	case svc.Paused:
		return "paused"
	case svc.StartPending:
		return "start pending"
	case svc.StopPending:
		return "stop pending"
	case svc.ContinuePending:
		return "continue pending"
	case svc.PausePending:
		return "pause pending"
	default:
		return fmt.Sprintf("unknown (%d)", s)
	}
}
