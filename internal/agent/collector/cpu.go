package collector

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/cpu"
)

// CPUCollector gathers CPU usage metrics (per-core and total).
type CPUCollector struct{}

// NewCPUCollector creates a new CPU collector.
func NewCPUCollector() *CPUCollector {
	return &CPUCollector{}
}

// Name returns the collector identifier.
func (c *CPUCollector) Name() string { return "cpu" }

// Collect gathers CPU usage percentages per-core and total.
func (c *CPUCollector) Collect(ctx context.Context) (map[string]any, error) {
	// Total CPU usage (all cores combined).
	totalPercent, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return nil, fmt.Errorf("cpu total percent: %w", err)
	}

	// Per-core CPU usage.
	perCorePercent, err := cpu.PercentWithContext(ctx, 0, true)
	if err != nil {
		return nil, fmt.Errorf("cpu per-core percent: %w", err)
	}

	// CPU counts (logical and physical).
	logicalCount, _ := cpu.CountsWithContext(ctx, true)
	physicalCount, _ := cpu.CountsWithContext(ctx, false)

	cores := make([]map[string]any, 0, len(perCorePercent))
	for i, pct := range perCorePercent {
		cores = append(cores, map[string]any{
			"core":    i,
			"percent": pct,
		})
	}

	total := 0.0
	if len(totalPercent) > 0 {
		total = totalPercent[0]
	}

	return map[string]any{
		"total_percent":  total,
		"cores":          cores,
		"logical_count":  logicalCount,
		"physical_count": physicalCount,
	}, nil
}
