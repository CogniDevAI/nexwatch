package collector

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/net"
)

// NetworkCollector gathers per-interface network I/O metrics.
type NetworkCollector struct{}

// NewNetworkCollector creates a new network collector.
func NewNetworkCollector() *NetworkCollector {
	return &NetworkCollector{}
}

// Name returns the collector identifier.
func (c *NetworkCollector) Name() string { return "network" }

// Collect gathers network I/O counters per interface.
func (c *NetworkCollector) Collect(ctx context.Context) (map[string]any, error) {
	counters, err := net.IOCountersWithContext(ctx, true) // per-interface
	if err != nil {
		return nil, fmt.Errorf("net io counters: %w", err)
	}

	interfaces := make([]map[string]any, 0, len(counters))
	for _, iface := range counters {
		interfaces = append(interfaces, map[string]any{
			"name":        iface.Name,
			"bytes_sent":  iface.BytesSent,
			"bytes_recv":  iface.BytesRecv,
			"packets_sent": iface.PacketsSent,
			"packets_recv": iface.PacketsRecv,
			"errin":       iface.Errin,
			"errout":      iface.Errout,
			"dropin":      iface.Dropin,
			"dropout":     iface.Dropout,
		})
	}

	return map[string]any{
		"interfaces": interfaces,
	}, nil
}
