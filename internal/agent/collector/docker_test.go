package collector

import (
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestDockerHostURL(t *testing.T) {
	tests := []struct {
		name       string
		socketPath string
		want       string
	}{
		{"bare unix socket path gets unix scheme", "/var/run/docker.sock", "unix:///var/run/docker.sock"},
		{"windows named pipe URL passed through unchanged", "npipe:////./pipe/docker_engine", "npipe:////./pipe/docker_engine"},
		{"explicit tcp URL passed through unchanged", "tcp://127.0.0.1:2375", "tcp://127.0.0.1:2375"},
		{"explicit unix URL passed through unchanged", "unix:///custom/docker.sock", "unix:///custom/docker.sock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DockerHostURL(tt.socketPath); got != tt.want {
				t.Errorf("DockerHostURL(%q) = %q, want %q", tt.socketPath, got, tt.want)
			}
		})
	}
}

func TestCalculateCPUPercent(t *testing.T) {
	tests := []struct {
		name  string
		stats *container.StatsResponse
		want  float64
	}{
		{
			name: "typical usage across 4 cpus",
			stats: &container.StatsResponse{
				CPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 2_000_000_000},
					SystemUsage: 100_000_000_000,
					OnlineCPUs:  4,
				},
				PreCPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000_000},
					SystemUsage: 90_000_000_000,
				},
			},
			// cpuDelta=1e9, systemDelta=1e10 -> (1e9/1e10)*4*100 = 40
			want: 40.0,
		},
		{
			name: "zero system delta yields zero percent",
			stats: &container.StatsResponse{
				CPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 2_000_000_000},
					SystemUsage: 100_000_000_000,
					OnlineCPUs:  4,
				},
				PreCPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000_000},
					SystemUsage: 100_000_000_000,
				},
			},
			want: 0.0,
		},
		{
			name: "zero cpu delta yields zero percent",
			stats: &container.StatsResponse{
				CPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000_000},
					SystemUsage: 100_000_000_000,
					OnlineCPUs:  4,
				},
				PreCPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000_000},
					SystemUsage: 90_000_000_000,
				},
			},
			want: 0.0,
		},
		{
			name: "falls back to percpu usage length when online cpus is zero",
			stats: &container.StatsResponse{
				CPUStats: container.CPUStats{
					CPUUsage: container.CPUUsage{
						TotalUsage:  2_000_000_000,
						PercpuUsage: []uint64{1, 2}, // 2 cpus
					},
					SystemUsage: 100_000_000_000,
					OnlineCPUs:  0,
				},
				PreCPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000_000},
					SystemUsage: 90_000_000_000,
				},
			},
			// (1e9/1e10)*2*100 = 20
			want: 20.0,
		},
		{
			name: "falls back to a single cpu when neither online cpus nor percpu usage is known",
			stats: &container.StatsResponse{
				CPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 2_000_000_000},
					SystemUsage: 100_000_000_000,
					OnlineCPUs:  0,
				},
				PreCPUStats: container.CPUStats{
					CPUUsage:    container.CPUUsage{TotalUsage: 1_000_000_000},
					SystemUsage: 90_000_000_000,
				},
			},
			// (1e9/1e10)*1*100 = 10
			want: 10.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := calculateCPUPercent(tt.stats); got != tt.want {
				t.Errorf("calculateCPUPercent() = %v, want %v", got, tt.want)
			}
		})
	}
}
