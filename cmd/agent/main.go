package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/host"

	"github.com/CogniDevAI/nexwatch/internal/agent/collector"
	"github.com/CogniDevAI/nexwatch/internal/agent/config"
	"github.com/CogniDevAI/nexwatch/internal/agent/transport"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// version is set at build time via ldflags.
var version = "dev"

func main() {
	// Load configuration (YAML file < env vars < CLI flags).
	cfg := config.Load()

	if cfg.Token == "" {
		fmt.Fprintln(os.Stderr, "Error: --token is required (or set NEXWATCH_TOKEN)")
		os.Exit(1)
	}

	log.Printf("NexWatch Agent %s starting", version)
	log.Printf("Hub: %s | Interval: %s", cfg.HubURL, cfg.Interval)

	// Create a context that cancels on SIGINT/SIGTERM.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

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
			}
		default:
			log.Printf("[agent] received message type=%s", msg.Type)
		}
	}

	// Start transport in background.
	go ws.Start(ctx)

	// Main collection loop.
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	log.Printf("Agent running (collecting every %s). Press Ctrl+C to stop.", cfg.Interval)

	for {
		select {
		case <-ticker.C:
			collectAndSend(ctx, registry, ws, agentID)

		case sig := <-sigCh:
			log.Printf("Received signal %s, shutting down...", sig)
			cancel()
			ws.Stop()
			ws.Wait()
			log.Println("Agent stopped.")
			return

		case <-ctx.Done():
			ws.Stop()
			ws.Wait()
			log.Println("Agent stopped.")
			return
		}
	}
}

// registerCollectors adds all enabled collectors to the registry.
func registerCollectors(registry *collector.Registry, cfg *config.Config) {
	enabled := make(map[string]bool)
	for _, name := range cfg.CollectorsEnabled {
		enabled[name] = true
	}

	if enabled["cpu"] {
		registry.Register(collector.NewCPUCollector())
	}
	if enabled["memory"] {
		registry.Register(collector.NewMemoryCollector())
	}
	if enabled["disk"] {
		registry.Register(collector.NewDiskCollector())
	}
	if enabled["network"] {
		registry.Register(collector.NewNetworkCollector())
	}
	if enabled["sysinfo"] {
		registry.Register(collector.NewSysInfoCollector())
	}
	if enabled["docker"] {
		registry.Register(collector.NewDockerCollector(cfg.DockerSocket))
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

// collectAndSend runs all collectors and sends the combined metrics.
func collectAndSend(ctx context.Context, registry *collector.Registry, ws *transport.WSTransport, agentID string) {
	now := time.Now().UnixMilli()
	metrics := make([]protocol.MetricData, 0, registry.Count())

	for _, c := range registry.All() {
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
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
