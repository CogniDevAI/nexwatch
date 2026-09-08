package collector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/net"
)

// networkIOState holds the previous per-interface counters snapshot for
// rate calculations, mirroring the pattern used by DiskIOCollector.
type networkIOState struct {
	counters map[string]net.IOCountersStat
	ts       time.Time
}

var (
	networkIOMu   sync.Mutex
	networkIOPrev *networkIOState
)

// NetworkCollector gathers per-interface network I/O metrics.
type NetworkCollector struct{}

// NewNetworkCollector creates a new network collector.
func NewNetworkCollector() *NetworkCollector {
	return &NetworkCollector{}
}

// Name returns the collector identifier.
func (c *NetworkCollector) Name() string { return "network" }

// Collect gathers network I/O counters per interface, plus the
// bytes-per-second send/receive rate computed from the delta against the
// previous collection. bytes_sent/bytes_recv (and the other cumulative
// counters) are kept for backwards compatibility, but they are a running
// total since the agent started — not a rate — so callers that want
// current throughput should use bytes_sent_per_sec/bytes_recv_per_sec
// instead.
func (c *NetworkCollector) Collect(ctx context.Context) (map[string]any, error) {
	counters, err := net.IOCountersWithContext(ctx, true) // per-interface
	if err != nil {
		return nil, fmt.Errorf("net io counters: %w", err)
	}

	now := time.Now()

	networkIOMu.Lock()
	prev := networkIOPrev
	curByName := make(map[string]net.IOCountersStat, len(counters))
	for _, iface := range counters {
		curByName[iface.Name] = iface
	}
	networkIOPrev = &networkIOState{counters: curByName, ts: now}
	networkIOMu.Unlock()

	interfaces := make([]map[string]any, 0, len(counters))
	for _, iface := range counters {
		var rxPerSec, txPerSec float64

		if prev != nil {
			elapsed := now.Sub(prev.ts).Seconds()
			if elapsed > 0 {
				if p, ok := prev.counters[iface.Name]; ok {
					rxPerSec = rate(p.BytesRecv, iface.BytesRecv, elapsed)
					txPerSec = rate(p.BytesSent, iface.BytesSent, elapsed)
				}
			}
		}

		interfaces = append(interfaces, map[string]any{
			"name":               iface.Name,
			"bytes_sent":         iface.BytesSent,
			"bytes_recv":         iface.BytesRecv,
			"bytes_sent_per_sec": txPerSec,
			"bytes_recv_per_sec": rxPerSec,
			"packets_sent":       iface.PacketsSent,
			"packets_recv":       iface.PacketsRecv,
			"errin":              iface.Errin,
			"errout":             iface.Errout,
			"dropin":             iface.Dropin,
			"dropout":            iface.Dropout,
		})
	}

	return map[string]any{
		"interfaces": interfaces,
	}, nil
}

// rate computes the per-second delta between two cumulative counter
// readings, clamping to zero when the counter went backwards (e.g. the
// interface counters were reset, or the agent restarted).
func rate(prev, cur uint64, elapsedSeconds float64) float64 {
	if cur <= prev || elapsedSeconds <= 0 {
		return 0
	}
	return float64(cur-prev) / elapsedSeconds
}
