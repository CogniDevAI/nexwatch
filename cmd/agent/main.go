package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/docker/docker/client"
	"github.com/shirou/gopsutil/v4/host"

	"github.com/CogniDevAI/nexwatch/internal/agent/collector"
	"github.com/CogniDevAI/nexwatch/internal/agent/command"
	"github.com/CogniDevAI/nexwatch/internal/agent/config"
	"github.com/CogniDevAI/nexwatch/internal/agent/logs"
	"github.com/CogniDevAI/nexwatch/internal/agent/platform"
	"github.com/CogniDevAI/nexwatch/internal/agent/transport"
	"github.com/CogniDevAI/nexwatch/internal/agent/update"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// collectorThrottle tracks last collection times to throttle expensive collectors.
var (
	lastCollectTime   = make(map[string]time.Time)
	lastCollectTimeMu sync.Mutex
)

// minIntervals defines the minimum interval between collections for specific collectors.
// Collectors not listed here run every cycle.
var minIntervals = map[string]time.Duration{
	"hardening":       5 * time.Minute,
	"vulnerabilities": 5 * time.Minute,
}

// version is set at build time via ldflags.
var version = "dev"

func main() {
	// Handle "--version"/"-version" before config.Load() parses its own
	// flag set (which does not declare this flag, and would otherwise log
	// a parse error and fall through to a normal — and pointless, since no
	// --token was given — agent startup). This is also what
	// internal/agent/update.Apply's sanity check runs after extracting a
	// new binary ("<new> --version"), so it must be cheap and side-effect
	// free: no config loading, no network, no logging.
	for _, arg := range os.Args[1:] {
		if arg == "--version" || arg == "-version" {
			fmt.Println(version)
			return
		}
	}

	// Handle "--service install|uninstall|start|stop|status" (Windows
	// only — see service_windows.go/service_other.go) before
	// config.Load(), the same way "--version" above is: these subcommands
	// manage the Windows Service Control Manager registration itself and
	// never start the agent's collection loop, so they need neither a hub
	// URL nor a token.
	if len(os.Args) >= 2 && os.Args[1] == "--service" {
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: nexwatch-agent --service install|uninstall|start|stop|status [--config <path>]")
			os.Exit(1)
		}
		if err := handleServiceCommand(os.Args[2], os.Args[3:]); err != nil { //nolint:staticcheck // SA4023: service_other.go's non-Windows build always errors here, but service_windows.go's real implementation can succeed.
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(1)
		}
		return
	}

	// Load configuration (YAML file < env vars < CLI flags).
	cfg := config.Load()

	if cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "Error: --token is required (or set NEXWATCH_TOKEN)")
		os.Exit(1)
	}

	// Running under the Windows Service Control Manager (started via "sc
	// start"/net start/the SCM's own boot-time auto-start, as opposed to a
	// normal interactive/console invocation) takes over log output
	// (service_windows.go's rotating %ProgramData%\NexWatch\agent.log —
	// there is no console to write to) and drives runAgent's lifecycle
	// from SCM start/stop control requests instead of OS signals.
	if isWindowsService() {
		runWindowsService(cfg)
		return
	}

	log.Printf("NexWatch Agent %s starting", version)
	log.Printf("Hub: %s | Interval: %s", cfg.HubURL, cfg.Interval)

	// Create a context that cancels on SIGINT/SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-sigCh:
			log.Printf("Received signal %s, shutting down...", sig)
			cancel()
		case <-ctx.Done():
		}
	}()

	runAgent(ctx, cfg)
}

// runAgent wires up the collector registry, WebSocket transport, log
// shipping, and the main collection loop, running until ctx is canceled —
// by the standalone signal-handling goroutine in main() above, or by
// service_windows.go's service handler reacting to an SCM stop/shutdown
// request. It is the one piece of agent lifecycle shared between a normal
// console run and running under the Windows Service Control Manager.
func runAgent(ctx context.Context, cfg *config.Config) {
	// Initialize collector registry.
	registry := collector.NewRegistry()
	registerCollectors(registry, cfg)
	log.Printf("Registered %d collectors: %s", registry.Count(), collectorNames(registry))

	// Derive agent identity.
	agentID := deriveAgentID()

	// Initialize WebSocket transport.
	ws := transport.NewWSTransport(cfg.HubURL, cfg.Token, agentID)

	// On connect (and reconnect), send REGISTER message.
	ws.OnConnect = func() {
		registerMsg := buildRegisterMessage(agentID)
		if err := ws.Send(registerMsg); err != nil {
			log.Printf("[agent] failed to send REGISTER: %v", err)
		} else {
			log.Println("[agent] REGISTER sent")
		}
	}

	// Handle messages from hub (ACK, COMMAND).
	ws.OnMessage = func(msg *protocol.Message) {
		switch msg.Type {
		case protocol.MessageTypeAck:
			var ack protocol.AckPayload
			if err := msg.DecodePayload(&ack); err == nil {
				log.Printf("[agent] ACK received: ref=%d status=%s", ack.MessageTimestamp, ack.Status)
			}
		case protocol.MessageTypeCommand:
			var cmd protocol.CommandPayload
			if err := msg.DecodePayload(&cmd); err == nil {
				log.Printf("[agent] COMMAND received: %s", cmd.Command)
				go handleCommand(ctx, ws, cmd, cfg)
			}
		default:
			log.Printf("[agent] received message type=%s", msg.Type)
		}
	}

	// Start transport in background.
	go ws.Start(ctx)

	// Start log shipping in the background, if enabled and at least one
	// source resolved (config.Load defaults to one journald source when
	// journalctl is present, otherwise none — see config.Load).
	if cfg.LogsEnabled && len(cfg.LogSources) > 0 {
		logsMgr := logs.NewManager(cfg.LogSources, cfg.LogsMaxLinesPerSec)
		log.Printf("[agent] log shipping enabled: %d source(s), max %d lines/sec", len(cfg.LogSources), cfg.LogsMaxLinesPerSec)
		go logsMgr.Run(ctx, func(batch logs.Batch) {
			sendLogsBatch(ws, agentID, batch)
		})
	} else {
		log.Println("[agent] log shipping disabled (logs_enabled=false or no log sources configured)")
	}

	// Main collection loop.
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	log.Printf("Agent running (collecting every %s). Press Ctrl+C to stop.", cfg.Interval)

	for {
		select {
		case <-ticker.C:
			collectAndSend(ctx, registry, ws, agentID)

		case <-ctx.Done():
			ws.Stop()
			ws.Wait()
			log.Println("Agent stopped.")
			return
		}
	}
}

// registerCollectors adds all enabled collectors to the registry, skipping
// any collector that platform.Supported reports has no working
// implementation on this runtime.GOOS (e.g. an agent.yaml copied verbatim
// from a Linux host onto a Windows one) and logging one clear line listing
// what was disabled and why, instead of registering a collector that would
// silently return empty or broken data.
func registerCollectors(registry *collector.Registry, cfg *config.Config) {
	enabled := make(map[string]bool)
	for _, name := range cfg.CollectorsEnabled {
		enabled[name] = true
	}

	var skipped []string
	register := func(name string, factory func()) {
		if !enabled[name] {
			return
		}
		if ok, reason := platform.Supported(name); !ok {
			skipped = append(skipped, fmt.Sprintf("%s (%s)", name, reason))
			return
		}
		factory()
	}

	register("cpu", func() { registry.Register(collector.NewCPUCollector()) })
	register("memory", func() { registry.Register(collector.NewMemoryCollector()) })
	register("disk", func() { registry.Register(collector.NewDiskCollector()) })
	register("network", func() { registry.Register(collector.NewNetworkCollector()) })
	register("sysinfo", func() { registry.Register(collector.NewSysInfoCollector()) })
	register("docker", func() {
		registry.Register(collector.NewDockerCollector(cfg.DockerSocket, cfg.DockerUpdateChecks))
	})
	register("ports", func() { registry.Register(collector.NewPortsCollector()) })
	register("processes", func() { registry.Register(collector.NewProcessesCollector()) })
	register("hardening", func() { registry.Register(collector.NewHardeningCollector()) })
	register("vulnerabilities", func() { registry.Register(collector.NewVulnerabilitiesCollector()) })
	register("oracle", func() {
		registry.Register(collector.NewOracleCollector(cfg.OracleHome, cfg.OracleSID))
	})
	register("diskio", func() { registry.Register(collector.NewDiskIOCollector()) })
	register("connections", func() { registry.Register(collector.NewConnectionsCollector()) })
	register("services", func() { registry.Register(collector.NewServicesCollector()) })
	register("cve_scan", func() {
		registry.Register(collector.NewCveScanCollector(
			cfg.CveScanEnabled, cfg.CveScanInterval, cfg.CveScanMaxImages,
			cfg.CveScanCacheDir, cfg.DockerSocket,
		))
	})

	if len(skipped) > 0 {
		log.Printf("[agent] %d collector(s) not supported on %s, disabled: %s", len(skipped), runtime.GOOS, strings.Join(skipped, "; "))
	}
}

// collectorNames returns a comma-separated list of registered collector names.
func collectorNames(registry *collector.Registry) string {
	names := ""
	for i, c := range registry.All() {
		if i > 0 {
			names += ", "
		}
		names += c.Name()
	}
	return names
}

// shouldCollect checks if a collector should run based on its minimum interval.
func shouldCollect(name string) bool {
	minInterval, hasThrottle := minIntervals[name]
	if !hasThrottle {
		return true // No throttle — always collect.
	}

	lastCollectTimeMu.Lock()
	defer lastCollectTimeMu.Unlock()

	last, exists := lastCollectTime[name]
	if !exists || time.Since(last) >= minInterval {
		lastCollectTime[name] = time.Now()
		return true
	}
	return false
}

// collectAndSend runs all collectors and sends the combined metrics.
func collectAndSend(ctx context.Context, registry *collector.Registry, ws *transport.WSTransport, agentID string) {
	now := time.Now().UnixMilli()
	metrics := make([]protocol.MetricData, 0, registry.Count())

	for _, c := range registry.All() {
		if !shouldCollect(c.Name()) {
			continue
		}

		data, err := c.Collect(ctx)
		if err != nil {
			log.Printf("[collect] %s error: %v", c.Name(), err)
			continue
		}

		metrics = append(metrics, protocol.MetricData{
			Type:      c.Name(),
			Data:      data,
			Timestamp: now,
		})
	}

	if len(metrics) == 0 {
		return
	}

	payload := &protocol.MetricsPayload{
		AgentID: agentID,
		Metrics: metrics,
	}

	msg, err := protocol.NewMessage(protocol.MessageTypeMetrics, payload)
	if err != nil {
		log.Printf("[agent] failed to build metrics message: %v", err)
		return
	}

	if err := ws.Send(msg); err != nil {
		log.Printf("[agent] failed to send metrics: %v", err)
	}
}

// sendLogsBatch converts a logs.Batch into a protocol.LogsPayload and
// sends it over the WebSocket transport queue, matching collectAndSend's
// shape for METRICS. Sending is fire-and-forget: a queue-full drop is
// already logged by transport.WSTransport.Send and reflected in the
// agent's next HEARTBEAT (dropped_messages), so this only needs to log
// the send-level failure, not retry.
func sendLogsBatch(ws *transport.WSTransport, agentID string, batch logs.Batch) {
	if len(batch.Entries) == 0 && batch.Dropped == 0 {
		return
	}

	entries := make([]protocol.LogEntry, 0, len(batch.Entries))
	for _, e := range batch.Entries {
		entries = append(entries, protocol.LogEntry{
			Ts:      e.Ts,
			Source:  e.Source,
			Unit:    e.Unit,
			Level:   e.Level,
			Message: e.Message,
			Fields:  e.Fields,
		})
	}

	payload := &protocol.LogsPayload{
		AgentID: agentID,
		Entries: entries,
		Dropped: batch.Dropped,
	}

	msg, err := protocol.NewMessage(protocol.MessageTypeLogs, payload)
	if err != nil {
		log.Printf("[agent] failed to build logs message: %v", err)
		return
	}

	if err := ws.Send(msg); err != nil {
		log.Printf("[agent] failed to send logs batch: %v", err)
	}
}

// buildRegisterMessage creates a REGISTER protocol message with system info.
func buildRegisterMessage(agentID string) *protocol.Message {
	hostname, _ := os.Hostname()
	osName := runtime.GOOS

	// Try to get more detailed OS info.
	if info, err := host.Info(); err == nil {
		osName = fmt.Sprintf("%s %s", info.Platform, info.PlatformVersion)
	}

	ip := getOutboundIP()

	payload := &protocol.RegisterPayload{
		AgentID:  agentID,
		Hostname: hostname,
		OS:       osName,
		IP:       ip,
		Version:  version,
		// Arch/Platform are the raw runtime.GOARCH/GOOS strings (e.g.
		// "amd64"/"linux"), deliberately distinct from OS above — see
		// protocol.RegisterPayload's doc comment — needed by the hub to
		// build a self-update release asset URL (F9).
		Arch:     runtime.GOARCH,
		Platform: runtime.GOOS,
	}

	msg, err := protocol.NewMessage(protocol.MessageTypeRegister, payload)
	if err != nil {
		log.Printf("[agent] failed to build REGISTER message: %v", err)
		return &protocol.Message{
			Type:      protocol.MessageTypeRegister,
			Timestamp: time.Now().UnixMilli(),
		}
	}

	return msg
}

// deriveAgentID generates a stable agent identifier from the hostname.
func deriveAgentID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return hostname
}

// getOutboundIP returns the preferred outbound IP of this machine.
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer func() { _ = conn.Close() }()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// handleCommand dispatches a COMMAND message from the hub to the appropriate handler.
func handleCommand(ctx context.Context, ws *transport.WSTransport, cmd protocol.CommandPayload, cfg *config.Config) {
	switch cmd.Command {
	case "thread_dump":
		handleThreadDump(ctx, ws, cmd)
	case "docker_action":
		handleDockerActionCommand(ctx, ws, cmd, cfg)
	case "update":
		handleUpdateCommand(ctx, ws, cmd, cfg)
	default:
		log.Printf("[agent] unknown command: %s", cmd.Command)
	}
}

// sendUpdateResponse builds and sends one COMMAND_RESPONSE for an "update"
// command's progress. It never blocks the caller on a send failure (the
// same fire-and-forget tolerance sendLogsBatch and every other outbound
// send in this file already applies) — a dropped progress update just
// means the hub's update_status lags until the next one, or until REGISTER
// reconciles it after a successful restart.
func sendUpdateResponse(ws *transport.WSTransport, requestID string, result update.Result) {
	resp := &protocol.CommandResponsePayload{
		Command:     "update",
		RequestID:   requestID,
		OK:          result.OK,
		Error:       result.Error,
		Stage:       result.Stage,
		FromVersion: result.FromVersion,
		ToVersion:   result.ToVersion,
	}
	msg, err := protocol.NewMessage(protocol.MessageTypeCommandResponse, resp)
	if err != nil {
		log.Printf("[agent] failed to encode update response: %v", err)
		return
	}
	if err := ws.Send(msg); err != nil {
		log.Printf("[agent] failed to send update response (stage=%s): %v", result.Stage, err)
	}
}

// handleUpdateCommand handles a hub-initiated "update" COMMAND. It replies
// immediately with a "started" (or, if self-update is disabled, "failed")
// COMMAND_RESPONSE so the hub's synchronous broker wait
// (internal/hub/commands.Broker.Send) returns right away, then — when
// accepted — runs the download/verify/install flow in a goroutine,
// streaming one COMMAND_RESPONSE per stage back to the hub. On success it
// re-execs the new binary in place (syscall.Exec, the primary path — see
// reexecOrExit) so the update takes effect without depending on a
// supervisor restarting the process; only on a platform/failure where
// re-exec itself can't run does it fall back to exiting 0 and relying on
// systemd's Restart=always (deploy/nexwatch-agent.service,
// scripts/install-agent.sh) to bring the new binary up.
func handleUpdateCommand(ctx context.Context, ws *transport.WSTransport, cmd protocol.CommandPayload, cfg *config.Config) {
	args := protocol.ParseUpdateArgs(cmd.Args)
	log.Printf("[agent] update requested: version=%s os=%s arch=%s req=%s", args.Version, args.OS, args.Arch, args.RequestID)

	if !cfg.AutoUpdateEnabled {
		sendUpdateResponse(ws, args.RequestID, update.Result{
			Stage:       update.StageFailed,
			OK:          false,
			Error:       "auto-update is disabled on this agent (auto_update_enabled=false)",
			FromVersion: version,
			ToVersion:   args.Version,
		})
		return
	}

	// Immediate ack — the hub's broker blocks on exactly this response.
	sendUpdateResponse(ws, args.RequestID, update.Result{
		Stage:       update.StageStarted,
		OK:          true,
		FromVersion: version,
		ToVersion:   args.Version,
	})

	go func() {
		updater := update.New(version)
		updater.RequireSignature = cfg.UpdateRequireSignature
		updater.SigningKeyURL = cfg.UpdateSigningKeyURL
		updater.SigningKeyFile = cfg.UpdateSigningKeyFile
		updater.OnStage = func(stage string) {
			sendUpdateResponse(ws, args.RequestID, update.Result{
				Stage:       stage,
				OK:          true,
				FromVersion: version,
				ToVersion:   args.Version,
			})
		}

		result := updater.Apply(ctx, update.Request{
			Version: args.Version,
			BaseURL: args.BaseURL,
			OS:      args.OS,
			Arch:    args.Arch,
		})

		if !result.OK {
			sendUpdateResponse(ws, args.RequestID, result)
			return
		}

		if result.RestartRequired {
			// Windows only (see Updater.installBinary's delayed-replace
			// fallback): the new binary could not be swapped into place
			// immediately because the running .exe's name was locked, so
			// Windows was asked to complete the rename at the next boot
			// instead. This process is still running its OLD binary
			// (installBinary restored it under its original name before
			// returning), and re-exec/exit would just reconnect into that
			// same old binary — pointless churn — so report
			// restart_required and stop here instead of falling through to
			// the normal restarting/re-exec path below. The update only
			// actually takes effect once the host is rebooted (or the
			// service otherwise restarted) and Windows completes the
			// deferred rename; the hub's REGISTER handler (internal/hub/ws
			// /handler.go) promotes update_status straight to "done" once
			// this agent re-registers running the new version.
			log.Printf("[agent] update: install scheduled for the next host restart (new binary locked); still running %s until then", version)
			sendUpdateResponse(ws, args.RequestID, update.Result{
				Stage:       update.StageRestartRequired,
				OK:          true,
				FromVersion: result.FromVersion,
				ToVersion:   result.ToVersion,
			})
			return
		}

		sendUpdateResponse(ws, args.RequestID, update.Result{
			Stage:       update.StageRestarting,
			OK:          true,
			FromVersion: result.FromVersion,
			ToVersion:   result.ToVersion,
		})

		// Give the "restarting" COMMAND_RESPONSE a brief moment to actually
		// reach the hub over the WebSocket before this process replaces or
		// exits itself.
		time.Sleep(300 * time.Millisecond)
		reexecOrExit(ws)
	}()
}

// reexecOrExit re-execs the just-installed binary in place via
// syscall.Exec (the primary path, so the update takes effect immediately
// without depending on any process supervisor), falling back to a clean
// os.Exit(0) if re-exec itself fails or isn't supported on this platform.
// systemd's Restart=always (see deploy/nexwatch-agent.service and
// scripts/install-agent.sh, both changed for F9 — Restart=on-failure does
// NOT restart on a clean exit 0) makes the fallback path work too, but
// re-exec is preferred since it needs no supervisor at all.
func reexecOrExit(ws *transport.WSTransport) {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("[agent] update: could not resolve own executable for re-exec, exiting: %v", err)
		ws.Stop()
		ws.Wait()
		os.Exit(0)
	}

	log.Printf("[agent] update: re-executing %s", exe)
	ws.Stop()
	ws.Wait()

	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil { //nolint:gosec // exe is os.Executable()'s own resolved path, not attacker input.
		log.Printf("[agent] update: re-exec failed, falling back to exit(0): %v", err)
		os.Exit(0)
	}
}

// handleDockerActionCommand performs a start/stop/restart lifecycle action
// against a Docker container and sends the result back as a
// COMMAND_RESPONSE. It builds its own short-lived Docker client per call
// (mirroring collector.DockerCollector) rather than keeping one open for
// the agent's whole lifetime, since docker_action commands are rare
// compared to the collection loop.
func handleDockerActionCommand(ctx context.Context, ws *transport.WSTransport, cmd protocol.CommandPayload, cfg *config.Config) {
	args := protocol.ParseDockerActionArgs(cmd.Args)
	log.Printf("[agent] docker action requested: container=%s action=%s req=%s", args.ContainerID, args.Action, args.RequestID)

	resp := &protocol.CommandResponsePayload{
		Command:     "docker_action",
		RequestID:   args.RequestID,
		ContainerID: args.ContainerID,
		Action:      args.Action,
	}

	socketPath := cfg.DockerSocket
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}
	cli, err := client.NewClientWithOpts(
		client.WithHost(collector.DockerHostURL(socketPath)),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		resp.Error = fmt.Sprintf("docker client unavailable: %s", err.Error())
		log.Printf("[agent] docker action failed: %v", err)
	} else {
		defer func() { _ = cli.Close() }()
		result := command.DockerAction(ctx, cli, args.ContainerID, args.Action)
		resp.OK = result.OK
		resp.State = result.State
		resp.Error = result.Error
		if result.OK {
			log.Printf("[agent] docker action success: container=%s action=%s state=%s", args.ContainerID, args.Action, result.State)
		} else {
			log.Printf("[agent] docker action failed: container=%s action=%s err=%s", args.ContainerID, args.Action, result.Error)
		}
	}

	msg, err := protocol.NewMessage(protocol.MessageTypeCommandResponse, resp)
	if err != nil {
		log.Printf("[agent] failed to encode docker action response: %v", err)
		return
	}
	if err := ws.Send(msg); err != nil {
		log.Printf("[agent] failed to send docker action response: %v", err)
	}
}

// handleThreadDump executes jstack for the requested PID and sends the result back.
func handleThreadDump(ctx context.Context, ws *transport.WSTransport, cmd protocol.CommandPayload) {
	requestID, _ := cmd.Args["request_id"].(string)

	// Extract PID — msgpack decodes numbers as int64 or float64.
	var pid int
	switch v := cmd.Args["pid"].(type) {
	case int:
		pid = v
	case int64:
		pid = int(v)
	case float64:
		pid = int(v)
	}

	processName, _ := cmd.Args["process_name"].(string)
	log.Printf("[agent] thread dump requested: pid=%d process=%s req=%s", pid, processName, requestID)

	resp := &protocol.CommandResponsePayload{
		Command:   "thread_dump",
		RequestID: requestID,
		PID:       pid,
	}

	output, err := command.ThreadDump(ctx, pid)
	if err != nil {
		resp.Error = err.Error()
		log.Printf("[agent] thread dump failed: pid=%d err=%v", pid, err)
	} else {
		resp.Output = output
		log.Printf("[agent] thread dump success: pid=%d bytes=%d", pid, len(output))
	}

	msg, err := protocol.NewMessage(protocol.MessageTypeCommandResponse, resp)
	if err != nil {
		log.Printf("[agent] failed to encode thread dump response: %v", err)
		return
	}

	if err := ws.Send(msg); err != nil {
		log.Printf("[agent] failed to send thread dump response: %v", err)
	}
}
