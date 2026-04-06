package collector

import (
	"context"
	"sort"
	"strings"

	psnet "github.com/shirou/gopsutil/v4/net"
)

// ConnectionsCollector gathers TCP connection state summary and per-port counts.
type ConnectionsCollector struct{}

// NewConnectionsCollector creates a new connections collector.
func NewConnectionsCollector() *ConnectionsCollector {
	return &ConnectionsCollector{}
}

// Name returns the collector identifier.
func (c *ConnectionsCollector) Name() string { return "connections" }

// Collect gathers TCP connection counts by state and top listening ports.
func (c *ConnectionsCollector) Collect(ctx context.Context) (map[string]any, error) {
	conns, err := psnet.ConnectionsWithContext(ctx, "tcp")
	if err != nil {
		return nil, err
	}

	// Normalise state strings to lowercase for consistent matching.
	stateCount := make(map[string]int)
	// Track established connections per local port for listening ports.
	portEstablished := make(map[uint32]int)
	// Track which ports are in LISTEN state.
	listeningPorts := make(map[uint32]bool)

	for _, conn := range conns {
		state := strings.ToLower(conn.Status)

		switch state {
		case "established":
			stateCount["established"]++
			// Count established connections destined to local listening ports.
			if conn.Laddr.Port > 0 {
				portEstablished[conn.Laddr.Port]++
			}
		case "time_wait":
			stateCount["time_wait"]++
		case "close_wait":
			stateCount["close_wait"]++
		case "listen":
			stateCount["listen"]++
			listeningPorts[conn.Laddr.Port] = true
		case "syn_sent":
			stateCount["syn_sent"]++
		case "syn_recv":
			stateCount["syn_recv"]++
		case "fin_wait1":
			stateCount["fin_wait1"]++
		case "fin_wait2":
			stateCount["fin_wait2"]++
		case "last_ack":
			stateCount["last_ack"]++
		case "closing":
			stateCount["closing"]++
		default:
			stateCount["other"]++
		}
	}

	summary := map[string]int{
		"established": stateCount["established"],
		"time_wait":   stateCount["time_wait"],
		"close_wait":  stateCount["close_wait"],
		"listen":      stateCount["listen"],
		"syn_sent":    stateCount["syn_sent"],
		"syn_recv":    stateCount["syn_recv"],
		"fin_wait1":   stateCount["fin_wait1"],
		"fin_wait2":   stateCount["fin_wait2"],
		"last_ack":    stateCount["last_ack"],
		"closing":     stateCount["closing"],
		"other":       stateCount["other"],
	}

	// Build top-10 listening ports by established connection count.
	type portEntry struct {
		Port             uint32 `json:"port"`
		EstablishedCount int    `json:"established_count"`
	}

	portSlice := make([]portEntry, 0, len(listeningPorts))
	for port := range listeningPorts {
		portSlice = append(portSlice, portEntry{
			Port:             port,
			EstablishedCount: portEstablished[port],
		})
	}

	sort.Slice(portSlice, func(i, j int) bool {
		return portSlice[i].EstablishedCount > portSlice[j].EstablishedCount
	})

	if len(portSlice) > 10 {
		portSlice = portSlice[:10]
	}

	byPort := make([]map[string]any, 0, len(portSlice))
	for _, p := range portSlice {
		byPort = append(byPort, map[string]any{
			"port":              p.Port,
			"established_count": p.EstablishedCount,
		})
	}

	return map[string]any{
		"summary":  summary,
		"total":    len(conns),
		"by_port":  byPort,
	}, nil
}
