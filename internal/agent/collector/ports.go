package collector

import (
	"context"
	"fmt"
	"sort"

	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

// PortsCollector gathers information about listening network ports.
type PortsCollector struct{}

// NewPortsCollector creates a new ports collector.
func NewPortsCollector() *PortsCollector {
	return &PortsCollector{}
}

// Name returns the collector identifier.
func (c *PortsCollector) Name() string { return "ports" }

// Collect gathers all listening TCP/UDP ports with process information.
func (c *PortsCollector) Collect(ctx context.Context) (map[string]any, error) {
	conns, err := net.ConnectionsWithContext(ctx, "inet")
	if err != nil {
		return nil, fmt.Errorf("net connections: %w", err)
	}

	// Filter to LISTEN state only.
	type listener struct {
		Port     uint32 `json:"port"`
		Protocol string `json:"protocol"`
		Address  string `json:"address"`
		PID      int32  `json:"pid"`
		Process  string `json:"process"`
	}

	// Deduplicate by port+protocol (there can be multiple entries for the same listener).
	seen := make(map[string]bool)
	listeners := make([]map[string]any, 0)

	for _, conn := range conns {
		if conn.Status != "LISTEN" {
			continue
		}

		key := fmt.Sprintf("%d/%s/%s", conn.Laddr.Port, protocolName(conn.Type), conn.Laddr.IP)
		if seen[key] {
			continue
		}
		seen[key] = true

		procName := ""
		if conn.Pid > 0 {
			p, err := process.NewProcessWithContext(ctx, conn.Pid)
			if err == nil {
				if name, err := p.NameWithContext(ctx); err == nil {
					procName = name
				}
			}
		}

		listeners = append(listeners, map[string]any{
			"port":     conn.Laddr.Port,
			"protocol": protocolName(conn.Type),
			"address":  conn.Laddr.IP,
			"pid":      conn.Pid,
			"process":  procName,
		})
	}

	// Sort by port number for consistent output.
	sort.Slice(listeners, func(i, j int) bool {
		pi, _ := listeners[i]["port"].(uint32)
		pj, _ := listeners[j]["port"].(uint32)
		return pi < pj
	})

	return map[string]any{
		"listeners": listeners,
		"count":     len(listeners),
	}, nil
}

// protocolName converts a gopsutil connection type to a protocol name.
func protocolName(connType uint32) string {
	switch connType {
	case 1:
		return "tcp"
	case 2:
		return "udp"
	default:
		return fmt.Sprintf("unknown(%d)", connType)
	}
}
