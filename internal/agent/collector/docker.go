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
	socketPath   string
	updateChecks bool
	digestCache  *imageDigestCache
}

// NewDockerCollector creates a new Docker collector.
// socketPath defaults to the standard Docker socket if empty. updateChecks
// enables per-image registry digest comparison (see docker_update.go); it
// is best-effort and never fails the collection when a registry is
// unreachable.
func NewDockerCollector(socketPath string, updateChecks bool) *DockerCollector {
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}
	return &DockerCollector{
		socketPath:   socketPath,
		updateChecks: updateChecks,
		digestCache:  newImageDigestCache(),
	}
}

// Name returns the collector identifier.
func (c *DockerCollector) Name() string { return "docker" }

// DockerHostURL builds the URL passed to client.WithHost from a configured
// docker_socket value. A value that already carries a scheme (e.g. the
// Windows default "npipe:////./pipe/docker_engine", or an explicit
// "tcp://…") is passed through unchanged; a bare filesystem path (the
// Linux/macOS default "/var/run/docker.sock") is prefixed with "unix://",
// matching this collector's and cmd/agent/main.go's docker_action
// command's previous hardcoded behavior for that case.
func DockerHostURL(socketPath string) string {
	if strings.Contains(socketPath, "://") {
		return socketPath
	}
	return "unix://" + socketPath
}

// Collect gathers container list and per-container resource usage.
// Returns empty data (no error) when Docker is not available.
func (c *DockerCollector) Collect(ctx context.Context) (map[string]any, error) {
	cli, err := client.NewClientWithOpts(
		client.WithHost(DockerHostURL(c.socketPath)),
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
	defer func() { _ = cli.Close() }()

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
			// "container_id" (not "id") to match what
			// metrics.Service.upsertDockerContainer reads when populating
			// the "docker_containers" collection — this was previously a
			// silent mismatch ("id" here vs. "container_id" there) that
			// meant docker_containers was never actually populated in
			// production, discovered via a live agent+hub end-to-end run.
			"container_id": ctr.ID[:12],
			"name":         name,
			"image":        ctr.Image,
			"status":       ctr.Status,
			"state":        ctr.State,
			"created":      ctr.Created,
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

		if c.updateChecks {
			update := checkImageUpdate(ctx, cli, c.digestCache, ctr.ImageID, ctr.Image)
			if update.Checked {
				entry["image_digest"] = update.LocalDigest
				entry["remote_digest"] = update.RemoteDigest
				entry["update_available"] = update.UpdateAvailable
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
	defer func() { _ = resp.Body.Close() }()

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
		// "memory_usage"/"memory_limit"/"network_rx"/"network_tx" (not the
		// shorter "mem_usage"/"mem_limit"/"net_rx"/"net_tx") to match the
		// "docker_containers" collection's field names and what
		// metrics.Service.upsertDockerContainer reads — another
		// pre-existing key mismatch (alongside "container_id"/"id") found
		// via a live agent+hub end-to-end run: these stats reached the raw
		// "metrics" JSON blob fine but were silently dropped when
		// upserting docker_containers.
		"memory_usage": memUsage,
		"memory_limit": memLimit,
		"mem_percent":  memPercent,
		"network_rx":   netRx,
		"network_tx":   netTx,
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
