package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/metrics"
)

// RegisterRoutes registers all custom API routes on the PocketBase router.
func RegisterRoutes(se *core.ServeEvent, metricsSvc *metrics.Service) {
	router := se.Router

	// GET /api/custom/dashboard — agent summaries with latest metrics
	router.GET("/api/custom/dashboard", func(e *core.RequestEvent) error {
		return handleDashboard(e, metricsSvc)
	})

	// GET /api/custom/metrics — time-range metric queries
	router.GET("/api/custom/metrics", func(e *core.RequestEvent) error {
		return handleMetricsQuery(e, metricsSvc)
	})

	// GET /api/custom/agents/{id}/install-command — generate agent install command
	router.GET("/api/custom/agents/{id}/install-command", func(e *core.RequestEvent) error {
		return handleInstallCommand(e)
	})

	// GET /api/custom/agents/{id}/ports — latest open ports for an agent
	router.GET("/api/custom/agents/{id}/ports", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "ports")
	})

	// GET /api/custom/agents/{id}/processes — latest process list for an agent
	router.GET("/api/custom/agents/{id}/processes", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "processes")
	})

	// GET /api/custom/agents/{id}/hardening — latest hardening report for an agent
	router.GET("/api/custom/agents/{id}/hardening", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "hardening")
	})

	// GET /api/custom/agents/{id}/vulnerabilities — latest vulnerability scan for an agent
	router.GET("/api/custom/agents/{id}/vulnerabilities", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "vulnerabilities")
	})
}

// handleDashboard returns a summary of all agents with their latest metrics.
// The response format is { agents: { [agentId]: { cpu, memory, disk } }, total: N }
// where cpu/memory/disk are percentage values (0-100) ready for display.
func handleDashboard(e *core.RequestEvent, metricsSvc *metrics.Service) error {
	// Fetch all agents.
	agents, err := e.App.FindRecordsByFilter(
		"agents",
		"id != ''", // match all
		"-last_seen",
		100,
		0,
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to fetch agents",
		})
	}

	type metricsSummary struct {
		CPU    float64 `json:"cpu"`
		Memory float64 `json:"memory"`
		Disk   float64 `json:"disk"`
	}

	// Build a map keyed by agent ID with extracted percentage values.
	summaries := make(map[string]metricsSummary, len(agents))
	for _, agent := range agents {
		summary := metricsSummary{}

		// Fetch latest metrics for this agent.
		latestMetrics, err := metricsSvc.GetLatestMetricsByAgent(agent.Id)
		if err == nil {
			for mtype, record := range latestMetrics {
				dataStr := record.GetString("data")
				var data map[string]any
				if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
					continue
				}
				switch mtype {
				case "cpu":
					if v, ok := toFloat(data["total_percent"]); ok {
						summary.CPU = v
					}
				case "memory":
					if v, ok := toFloat(data["used_percent"]); ok {
						summary.Memory = v
					}
				case "disk":
					// Use the root mount ("/") or the first mount with the highest usage.
					summary.Disk = extractDiskPercent(data)
				}
			}
		}

		summaries[agent.Id] = summary
	}

	return e.JSON(http.StatusOK, map[string]any{
		"agents": summaries,
		"total":  len(agents),
	})
}

// extractDiskPercent returns the used_percent for the root "/" mount,
// or the first mount's used_percent if root is not found.
func extractDiskPercent(data map[string]any) float64 {
	mountsRaw, ok := data["mounts"]
	if !ok {
		return 0
	}

	mounts, ok := mountsRaw.([]any)
	if !ok || len(mounts) == 0 {
		return 0
	}

	// Try to find root mount first.
	for _, m := range mounts {
		mount, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if path, _ := mount["path"].(string); path == "/" {
			if v, ok := toFloat(mount["used_percent"]); ok {
				return v
			}
		}
	}

	// Fallback to first mount.
	if mount, ok := mounts[0].(map[string]any); ok {
		if v, ok := toFloat(mount["used_percent"]); ok {
			return v
		}
	}

	return 0
}

// toFloat safely converts various numeric types to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// parseTimeParam parses a time parameter that can be either a Unix timestamp
// (seconds since epoch) or an RFC3339 string. Returns fallback on failure.
func parseTimeParam(s string, fallback time.Time) time.Time {
	if s == "" {
		return fallback
	}
	// Try Unix timestamp first (integer seconds).
	if secs, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(secs, 0).UTC()
	}
	// Try RFC3339.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return fallback
}

// parseTimestamp converts a PocketBase timestamp string to Unix seconds.
func parseTimestamp(ts string) float64 {
	layouts := []string{
		"2006-01-02 15:04:05.000Z",
		"2006-01-02 15:04:05.000",
		"2006-01-02 15:04:05Z",
		"2006-01-02 15:04:05",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, ts); err == nil {
			return float64(t.Unix())
		}
	}
	return 0
}

// timeSeries holds parallel arrays of timestamps and values for chart rendering.
type timeSeries struct {
	Timestamps []float64 `json:"timestamps"`
	Values     []float64 `json:"values"`
}

// handleMetricsQuery returns metrics as structured time series for chart rendering.
// Query params: agent_id, start (unix seconds or RFC3339), end (unix seconds or RFC3339), resolution
// Response format: { cpu: {timestamps, values}, memory: {timestamps, values}, disk: {timestamps, values}, network_rx: {timestamps, values}, network_tx: {timestamps, values} }
func handleMetricsQuery(e *core.RequestEvent, metricsSvc *metrics.Service) error {
	agentID := e.Request.URL.Query().Get("agent_id")
	resolution := e.Request.URL.Query().Get("resolution")

	now := time.Now().UTC()
	start := parseTimeParam(e.Request.URL.Query().Get("start"), now.Add(-1*time.Hour))
	end := parseTimeParam(e.Request.URL.Query().Get("end"), now)

	// Auto-select resolution based on time range if not specified.
	if resolution == "" {
		duration := end.Sub(start)
		switch {
		case duration <= 2*time.Hour:
			resolution = "raw"
		case duration <= 24*time.Hour:
			resolution = "1m"
		case duration <= 7*24*time.Hour:
			resolution = "5m"
		default:
			resolution = "1h"
		}
	}

	// Query all metric types for this agent in the time range.
	records, err := metricsSvc.QueryMetrics(agentID, "" /* all types */, start, end, resolution)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("query failed: %v", err),
		})
	}

	// Build time series for each metric type.
	cpu := timeSeries{Timestamps: []float64{}, Values: []float64{}}
	memory := timeSeries{Timestamps: []float64{}, Values: []float64{}}
	disk := timeSeries{Timestamps: []float64{}, Values: []float64{}}
	networkRx := timeSeries{Timestamps: []float64{}, Values: []float64{}}
	networkTx := timeSeries{Timestamps: []float64{}, Values: []float64{}}

	for _, r := range records {
		mtype := r.GetString("type")
		ts := parseTimestamp(r.GetString("timestamp"))
		if ts == 0 {
			continue
		}

		dataStr := r.GetString("data")
		var data map[string]any
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}

		switch mtype {
		case "cpu":
			if v, ok := toFloat(data["total_percent"]); ok {
				cpu.Timestamps = append(cpu.Timestamps, ts)
				cpu.Values = append(cpu.Values, math.Round(v*100)/100)
			}
		case "memory":
			if v, ok := toFloat(data["used_percent"]); ok {
				memory.Timestamps = append(memory.Timestamps, ts)
				memory.Values = append(memory.Values, math.Round(v*100)/100)
			}
		case "disk":
			v := extractDiskPercent(data)
			if v > 0 {
				disk.Timestamps = append(disk.Timestamps, ts)
				disk.Values = append(disk.Values, math.Round(v*100)/100)
			}
		case "network":
			rx, tx := extractNetworkBytes(data)
			networkRx.Timestamps = append(networkRx.Timestamps, ts)
			networkRx.Values = append(networkRx.Values, rx)
			networkTx.Timestamps = append(networkTx.Timestamps, ts)
			networkTx.Values = append(networkTx.Values, tx)
		}
	}

	// Sort all series by timestamp (records come sorted by -timestamp, we need ascending).
	sortTimeSeries(&cpu)
	sortTimeSeries(&memory)
	sortTimeSeries(&disk)
	sortTimeSeries(&networkRx)
	sortTimeSeries(&networkTx)

	return e.JSON(http.StatusOK, map[string]any{
		"cpu":        cpu,
		"memory":     memory,
		"disk":       disk,
		"network_rx": networkRx,
		"network_tx": networkTx,
	})
}

// sortTimeSeries sorts timestamps and values in ascending order.
func sortTimeSeries(ts *timeSeries) {
	if len(ts.Timestamps) <= 1 {
		return
	}
	// Build index pairs, sort by timestamp.
	type pair struct {
		ts  float64
		val float64
	}
	pairs := make([]pair, len(ts.Timestamps))
	for i := range ts.Timestamps {
		pairs[i] = pair{ts: ts.Timestamps[i], val: ts.Values[i]}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].ts < pairs[j].ts })
	for i, p := range pairs {
		ts.Timestamps[i] = p.ts
		ts.Values[i] = p.val
	}
}

// extractNetworkBytes returns total bytes_recv and bytes_sent from the primary interface.
func extractNetworkBytes(data map[string]any) (float64, float64) {
	interfacesRaw, ok := data["interfaces"]
	if !ok {
		return 0, 0
	}
	interfaces, ok := interfacesRaw.([]any)
	if !ok || len(interfaces) == 0 {
		return 0, 0
	}

	// Find the interface with the most traffic (skip loopback).
	var bestRx, bestTx float64
	for _, iface := range interfaces {
		ifMap, ok := iface.(map[string]any)
		if !ok {
			continue
		}
		name, _ := ifMap["name"].(string)
		if name == "lo0" || name == "lo" {
			continue
		}
		rx, _ := toFloat(ifMap["bytes_recv"])
		tx, _ := toFloat(ifMap["bytes_sent"])
		if rx+tx > bestRx+bestTx {
			bestRx = rx
			bestTx = tx
		}
	}
	// Convert to MB for display.
	return math.Round(bestRx/1024/1024*100) / 100, math.Round(bestTx/1024/1024*100) / 100
}

// handleInstallCommand generates a curl install command for an agent.
func handleInstallCommand(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")
	if agentID == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{
			"error": "agent ID is required",
		})
	}

	// Lookup the agent record.
	agent, err := e.App.FindRecordById("agents", agentID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{
			"error": "agent not found",
		})
	}

	token := agent.GetString("token")
	if token == "" {
		// Generate a token if not present.
		token, err = generateToken()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{
				"error": "failed to generate token",
			})
		}
		agent.Set("token", token)
		if err := e.App.Save(agent); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{
				"error": "failed to save token",
			})
		}
	}

	// Determine the hub URL from the request.
	scheme := "https"
	if e.Request.TLS == nil {
		scheme = "http"
	}
	hubURL := fmt.Sprintf("%s://%s", scheme, e.Request.Host)

	// Generate the install command.
	installCmd := fmt.Sprintf(
		`curl -sSL %s/install.sh | sudo bash -s -- --hub="%s" --token="%s"`,
		hubURL, hubURL, token,
	)

	return e.JSON(http.StatusOK, map[string]any{
		"agentId":        agentID,
		"token":          token,
		"installCommand": installCmd,
		"hubUrl":         hubURL,
	})
}

// generateToken creates a cryptographically secure random token.
func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// handleLatestMetricByType returns the latest metric data for a given agent and metric type.
// It queries the metrics collection for the most recent record matching the agent_id and type,
// then returns just the JSON data field.
func handleLatestMetricByType(e *core.RequestEvent, metricType string) error {
	agentID := e.Request.PathValue("id")
	if agentID == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{
			"error": "agent ID is required",
		})
	}

	// Verify agent exists.
	_, err := e.App.FindRecordById("agents", agentID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{
			"error": "agent not found",
		})
	}

	// Query latest metric record of the given type for this agent.
	records, err := e.App.FindRecordsByFilter(
		"metrics",
		"agent_id = {:agentId} && type = {:type}",
		"-timestamp",
		1,
		0,
		map[string]any{
			"agentId": agentID,
			"type":    metricType,
		},
	)
	if err != nil || len(records) == 0 {
		// Return an empty but valid structure matching each collector's data shape.
		// This lets the frontend handle "no data" gracefully without special-casing.
		emptyResponses := map[string]string{
			"ports":           `{"listeners":[],"count":0}`,
			"processes":       `{"processes":[],"total_count":0}`,
			"hardening":       `{"checks":[],"score":0,"total":0,"passed":0,"failed":0,"warnings":0}`,
			"vulnerabilities": `{"items":[],"summary":{"critical":0,"high":0,"medium":0,"low":0},"total":0}`,
		}
		empty, ok := emptyResponses[metricType]
		if !ok {
			empty = "{}"
		}
		e.Response.Header().Set("Content-Type", "application/json")
		_, writeErr := e.Response.Write([]byte(empty))
		return writeErr
	}

	record := records[0]
	dataStr := record.GetString("data")

	// Return ONLY the data field content directly — no wrapper.
	// The frontend expects the raw data shape from each collector
	// (e.g. {listeners: [...], count: N} for ports).
	var data json.RawMessage
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to parse metric data",
		})
	}

	// Write raw JSON directly so we don't double-encode.
	e.Response.Header().Set("Content-Type", "application/json")
	_, writeErr := e.Response.Write(data)
	return writeErr
}
