package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// DockerCollector gathers metrics from running Docker containers.
type DockerCollector struct {
	socketPath string
}

// NewDockerCollector creates a new Docker collector.
// socketPath defaults to the standard Docker socket if empty.
func NewDockerCollector(socketPath string) *DockerCollector {
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}
	return &DockerCollector{socketPath: socketPath}
}

// Name returns the collector identifier.
func (c *DockerCollector) Name() string { return "docker" }

// Collect gathers container list and per-container resource usage.
// Returns empty data (no error) when Docker is not available.
func (c *DockerCollector) Collect(ctx context.Context) (map[string]any, error) {
	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+c.socketPath),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		// Docker not available — return empty, don't error.
		log.Printf("[docker] client creation failed (docker may not be installed): %v", err)
		return map[string]any{
			"available":  false,
			"containers": []map[string]any{},
		}, nil
	}
	defer cli.Close()

	// Verify connectivity with a ping.
	_, err = cli.Ping(ctx)
	if err != nil {
		log.Printf("[docker] daemon not reachable: %v", err)
		return map[string]any{
			"available":  false,
			"containers": []map[string]any{},
		}, nil
	}

	// List all containers (including stopped).
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		log.Printf("[docker] container list error: %v", err)
		return map[string]any{
			"available":  false,
			"containers": []map[string]any{},
		}, nil
	}

	results := make([]map[string]any, 0, len(containers))
	for _, ctr := range containers {
		name := ""
		if len(ctr.Names) > 0 {
			name = strings.TrimPrefix(ctr.Names[0], "/")
		}

		entry := map[string]any{
			"id":      ctr.ID[:12],
			"name":    name,
			"image":   ctr.Image,
			"status":  ctr.Status,
			"state":   ctr.State,
			"created": ctr.Created,
		}

		// Only fetch stats for running containers.
		if ctr.State == "running" {
			stats, err := getContainerStats(ctx, cli, ctr.ID)
			if err == nil {
				for k, v := range stats {
					entry[k] = v
				}
			}
		}

		results = append(results, entry)
	}

	return map[string]any{
		"available":       true,
		"container_count": len(containers),
		"containers":      results,
	}, nil
}

// getContainerStats fetches CPU, memory, and network stats for a single container.
func getContainerStats(ctx context.Context, cli *client.Client, containerID string) (map[string]any, error) {
	resp, err := cli.ContainerStats(ctx, containerID, false) // one-shot, not streaming
	if err != nil {
		return nil, fmt.Errorf("container stats: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read stats body: %w", err)
	}

	var stats container.StatsResponse
	if err := json.Unmarshal(body, &stats); err != nil {
		return nil, fmt.Errorf("unmarshal stats: %w", err)
	}

	// Calculate CPU percentage.
	cpuPercent := calculateCPUPercent(&stats)

	// Memory usage.
	memUsage := stats.MemoryStats.Usage
	memLimit := stats.MemoryStats.Limit
	memPercent := 0.0
	if memLimit > 0 {
		memPercent = float64(memUsage) / float64(memLimit) * 100.0
	}

	// Network I/O aggregated across all interfaces.
	var netRx, netTx uint64
	for _, v := range stats.Networks {
		netRx += v.RxBytes
		netTx += v.TxBytes
	}

	return map[string]any{
		"cpu_percent": cpuPercent,
		"mem_usage":   memUsage,
		"mem_limit":   memLimit,
		"mem_percent": memPercent,
		"net_rx":      netRx,
		"net_tx":      netTx,
	}, nil
}

// calculateCPUPercent computes the CPU usage percentage from Docker stats.
func calculateCPUPercent(stats *container.StatsResponse) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage)

	if systemDelta > 0 && cpuDelta > 0 {
		cpuCount := float64(stats.CPUStats.OnlineCPUs)
		if cpuCount == 0 {
			cpuCount = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
		}
		if cpuCount == 0 {
			cpuCount = 1
		}
		return (cpuDelta / systemDelta) * cpuCount * 100.0
	}
	return 0.0
}
