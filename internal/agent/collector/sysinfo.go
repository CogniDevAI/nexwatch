package collector

import (
	"context"
	"fmt"
	"runtime"

	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
)

// SysInfoCollector gathers system information: hostname, OS, uptime, load averages.
type SysInfoCollector struct{}

// NewSysInfoCollector creates a new system info collector.
func NewSysInfoCollector() *SysInfoCollector {
	return &SysInfoCollector{}
}

// Name returns the collector identifier.
func (c *SysInfoCollector) Name() string { return "sysinfo" }

// Collect gathers system-level information.
func (c *SysInfoCollector) Collect(ctx context.Context) (map[string]any, error) {
	info, err := host.InfoWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("host info: %w", err)
	}

	result := map[string]any{
		"hostname":         info.Hostname,
		"os":               info.OS,
		"platform":         info.Platform,
		"platform_version": info.PlatformVersion,
		"kernel_version":   info.KernelVersion,
		"arch":             runtime.GOARCH,
		"uptime":           info.Uptime,
		"boot_time":        info.BootTime,
		"procs":            info.Procs,
	}

	// Load averages (not available on all platforms).
	avg, err := load.AvgWithContext(ctx)
	if err == nil && avg != nil {
		result["load1"] = avg.Load1
		result["load5"] = avg.Load5
		result["load15"] = avg.Load15
	}

	return result, nil
}
