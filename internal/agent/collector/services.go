package collector

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// ServicesCollector gathers systemd service statuses.
type ServicesCollector struct{}

// NewServicesCollector creates a new services collector.
func NewServicesCollector() *ServicesCollector {
	return &ServicesCollector{}
}

// Name returns the collector identifier.
func (c *ServicesCollector) Name() string { return "services" }

// Collect runs systemctl and parses service states.
func (c *ServicesCollector) Collect(ctx context.Context) (map[string]any, error) {
	cmd := exec.CommandContext(ctx,
		"systemctl", "list-units",
		"--type=service",
		"--all",
		"--no-pager",
		"--no-legend",
		"--plain",
	)

	out, err := cmd.Output()
	if err != nil {
		// systemctl may not be present (e.g. macOS). Return empty rather than error.
		return map[string]any{
			"services": []map[string]any{},
			"total":    0,
			"running":  0,
			"failed":   0,
			"other":    0,
		}, nil
	}

	type serviceEntry struct {
		Name        string `json:"name"`
		Load        string `json:"load"`
		Active      string `json:"active"`
		Sub         string `json:"sub"`
		Description string `json:"description"`
	}

	services := make([]map[string]any, 0, 64)
	running := 0
	failed := 0

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Fields: UNIT LOAD ACTIVE SUB DESCRIPTION...
		// Fields are whitespace-separated; description may contain spaces.
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		unit := fields[0]
		load := fields[1]
		active := fields[2]
		sub := fields[3]
		description := ""
		if len(fields) > 4 {
			description = strings.Join(fields[4:], " ")
		}

		// Skip inactive services.
		if active == "inactive" {
			continue
		}

		// Strip .service suffix.
		name := strings.TrimSuffix(unit, ".service")

		switch sub {
		case "running":
			running++
		case "failed":
			failed++
		}

		services = append(services, map[string]any{
			"name":        name,
			"load":        load,
			"active":      active,
			"sub":         sub,
			"description": description,
		})

		if len(services) >= 100 {
			break
		}
	}

	total := len(services)
	other := total - running - failed

	return map[string]any{
		"services": services,
		"total":    total,
		"running":  running,
		"failed":   failed,
		"other":    other,
	}, nil
}
