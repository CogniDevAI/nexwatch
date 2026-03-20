package collector

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/mem"
)

// MemoryCollector gathers memory usage metrics.
type MemoryCollector struct{}

// NewMemoryCollector creates a new memory collector.
func NewMemoryCollector() *MemoryCollector {
	return &MemoryCollector{}
}

// Name returns the collector identifier.
func (c *MemoryCollector) Name() string { return "memory" }

// Collect gathers virtual memory usage statistics.
func (c *MemoryCollector) Collect(ctx context.Context) (map[string]any, error) {
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("virtual memory: %w", err)
	}

	return map[string]any{
		"total":        v.Total,
		"used":         v.Used,
		"available":    v.Available,
		"cached":       v.Cached,
		"free":         v.Free,
		"used_percent": v.UsedPercent,
		"buffers":      v.Buffers,
	}, nil
}
