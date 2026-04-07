package collector

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
)

// diskIOState holds the previous snapshot for delta calculations.
type diskIOState struct {
	counters map[string]disk.IOCountersStat
	ts       time.Time
}

var (
	diskIOMu   sync.Mutex
	diskIOPrev *diskIOState
)

// DiskIOCollector gathers disk I/O throughput metrics per physical device.
type DiskIOCollector struct{}

// NewDiskIOCollector creates a new disk I/O collector.
func NewDiskIOCollector() *DiskIOCollector {
	return &DiskIOCollector{}
}

// Name returns the collector identifier.
func (c *DiskIOCollector) Name() string { return "diskio" }

// isVirtualDevice returns true for loop, ram, and dm- devices that should be skipped.
func isVirtualDevice(name string) bool {
	return strings.HasPrefix(name, "loop") ||
		strings.HasPrefix(name, "ram") ||
		strings.HasPrefix(name, "dm-")
}

// Collect gathers disk I/O deltas per physical device since the last call.
func (c *DiskIOCollector) Collect(ctx context.Context) (map[string]any, error) {
	counters, err := disk.IOCountersWithContext(ctx)
	if err != nil {
		log.Printf("[diskio] IOCounters error: %v", err)
		return nil, err
	}
	log.Printf("[diskio] collected %d devices", len(counters))

	now := time.Now()

	diskIOMu.Lock()
	prev := diskIOPrev
	diskIOPrev = &diskIOState{counters: counters, ts: now}
	diskIOMu.Unlock()

	devices := make([]map[string]any, 0, len(counters))

	for name, cur := range counters {
		if isVirtualDevice(name) {
			continue
		}

		entry := map[string]any{
			"name":                name,
			"reads_per_sec":       0.0,
			"writes_per_sec":      0.0,
			"read_bytes_per_sec":  0.0,
			"write_bytes_per_sec": 0.0,
			"io_time_ms":          cur.IoTime,
		}

		if prev != nil {
			elapsed := now.Sub(prev.ts).Seconds()
			if elapsed > 0 {
				if p, ok := prev.counters[name]; ok {
					entry["reads_per_sec"] = float64(cur.ReadCount-p.ReadCount) / elapsed
					entry["writes_per_sec"] = float64(cur.WriteCount-p.WriteCount) / elapsed
					entry["read_bytes_per_sec"] = float64(cur.ReadBytes-p.ReadBytes) / elapsed
					entry["write_bytes_per_sec"] = float64(cur.WriteBytes-p.WriteBytes) / elapsed
					// io_time_ms is delta since last call (cumulative ms spent doing I/O)
					if cur.IoTime >= p.IoTime {
						entry["io_time_ms"] = cur.IoTime - p.IoTime
					}
				}
			}
		}

		devices = append(devices, entry)
	}

	return map[string]any{
		"devices": devices,
	}, nil
}
