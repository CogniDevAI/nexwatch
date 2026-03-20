package collector

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

// ProcessesCollector gathers information about running processes.
type ProcessesCollector struct{}

// NewProcessesCollector creates a new processes collector.
func NewProcessesCollector() *ProcessesCollector {
	return &ProcessesCollector{}
}

// Name returns the collector identifier.
func (c *ProcessesCollector) Name() string { return "processes" }

// Collect gathers the top 50 processes sorted by CPU usage descending.
func (c *ProcessesCollector) Collect(ctx context.Context) (map[string]any, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("process list: %w", err)
	}

	type procInfo struct {
		PID       int32   `json:"pid"`
		Name      string  `json:"name"`
		CPU       float64 `json:"cpu_percent"`
		Memory    float64 `json:"mem_percent"`
		RSS       uint64  `json:"rss"`
		Status    string  `json:"status"`
		User      string  `json:"user"`
		CmdLine   string  `json:"cmdline"`
	}

	infos := make([]procInfo, 0, len(procs))

	for _, p := range procs {
		// Skip processes we can't access (permission errors are common).
		name, err := p.NameWithContext(ctx)
		if err != nil {
			continue
		}

		cpuPct, _ := p.CPUPercentWithContext(ctx)
		memPct, _ := p.MemoryPercentWithContext(ctx)

		var rss uint64
		if memInfo, err := p.MemoryInfoWithContext(ctx); err == nil && memInfo != nil {
			rss = memInfo.RSS
		}

		statusSlice, _ := p.StatusWithContext(ctx)
		status := ""
		if len(statusSlice) > 0 {
			status = statusSlice[0]
		}

		user, _ := p.UsernameWithContext(ctx)

		cmdline, _ := p.CmdlineWithContext(ctx)
		// Truncate long command lines.
		if len(cmdline) > 200 {
			cmdline = cmdline[:200] + "..."
		}

		infos = append(infos, procInfo{
			PID:     p.Pid,
			Name:    name,
			CPU:     cpuPct,
			Memory:  float64(memPct),
			RSS:     rss,
			Status:  status,
			User:    user,
			CmdLine: cmdline,
		})
	}

	// Sort by CPU percent descending.
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].CPU > infos[j].CPU
	})

	// Limit to top 50.
	if len(infos) > 50 {
		infos = infos[:50]
	}

	// Convert to []map[string]any for JSON serialization.
	processes := make([]map[string]any, 0, len(infos))
	for _, info := range infos {
		processes = append(processes, map[string]any{
			"pid":         info.PID,
			"name":        info.Name,
			"cpu_percent": roundTo2(info.CPU),
			"mem_percent": roundTo2(info.Memory),
			"rss":         info.RSS,
			"status":      info.Status,
			"user":        info.User,
			"cmdline":     info.CmdLine,
		})
	}

	log.Printf("[processes] collected %d processes (from %d total)", len(processes), len(procs))

	return map[string]any{
		"processes":   processes,
		"total_count": len(procs),
	}, nil
}

// roundTo2 rounds a float64 to 2 decimal places.
func roundTo2(v float64) float64 {
	return float64(int(v*100)) / 100
}

// truncateStr truncates a string to max length, appending "..." if truncated.
func truncateStr(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
