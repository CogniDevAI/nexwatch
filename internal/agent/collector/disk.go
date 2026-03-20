package collector

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/disk"
)

// DiskCollector gathers disk usage metrics per mount point.
type DiskCollector struct{}

// NewDiskCollector creates a new disk collector.
func NewDiskCollector() *DiskCollector {
	return &DiskCollector{}
}

// Name returns the collector identifier.
func (c *DiskCollector) Name() string { return "disk" }

// Collect gathers disk usage for all partitions.
func (c *DiskCollector) Collect(ctx context.Context) (map[string]any, error) {
	partitions, err := disk.PartitionsWithContext(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("disk partitions: %w", err)
	}

	mounts := make([]map[string]any, 0, len(partitions))
	for _, p := range partitions {
		usage, err := disk.UsageWithContext(ctx, p.Mountpoint)
		if err != nil {
			continue // skip partitions we can't read
		}

		mounts = append(mounts, map[string]any{
			"path":         p.Mountpoint,
			"device":       p.Device,
			"fstype":       p.Fstype,
			"total":        usage.Total,
			"used":         usage.Used,
			"free":         usage.Free,
			"used_percent": usage.UsedPercent,
			"inodes_total": usage.InodesTotal,
			"inodes_used":  usage.InodesUsed,
			"inodes_free":  usage.InodesFree,
		})
	}

	return map[string]any{
		"mounts": mounts,
	}, nil
}
