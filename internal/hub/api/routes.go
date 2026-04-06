package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

	// GET /api/custom/agents/{id}/processes/history — aggregate process stats over a time range
	router.GET("/api/custom/agents/{id}/processes/history", func(e *core.RequestEvent) error {
		return handleProcessHistory(e)
	})

	// GET /api/custom/agents/{id}/processes/timeline — time-series data for a specific process name
	router.GET("/api/custom/agents/{id}/processes/timeline", func(e *core.RequestEvent) error {
		return handleProcessTimeline(e)
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

	// GET /api/custom/agents/{id}/hardware — system hardware info from sysinfo, cpu, memory metrics
	router.GET("/api/custom/agents/{id}/hardware", func(e *core.RequestEvent) error {
		return handleHardware(e)
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

// parseRangeParam converts a range string ("1h", "6h", "24h") to milliseconds.
// Returns the duration in milliseconds and the canonical range label.
func parseRangeParam(r string) (int64, string) {
	switch r {
	case "6h":
		return 6 * 60 * 60 * 1000, "6h"
	case "24h":
		return 24 * 60 * 60 * 1000, "24h"
	default:
		return 1 * 60 * 60 * 1000, "1h"
	}
}

// extractCmdKey returns a unique display key for a process.
// For java processes it extracts the -jar filename so each JVM is tracked separately.
// Examples: "java (bancacore-api.jar)", "java (bancacore-ms.jar)", "nginx"
func extractCmdKey(name, cmdline string) string {
	if name == "java" || strings.HasSuffix(name, "/java") {
		// Look for -jar /path/to/something.jar
		if idx := strings.Index(cmdline, "-jar "); idx >= 0 {
			rest := strings.TrimSpace(cmdline[idx+5:])
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				return name + " (" + filepath.Base(fields[0]) + ")"
			}
		}
		// Wildfly/JBoss standalone: look for jboss-modules.jar or standalone.xml
		if strings.Contains(cmdline, "jboss-modules") || strings.Contains(cmdline, "wildfly") {
			return name + " (wildfly)"
		}
	}
	return name
}

// extractCmdFragment returns just the distinguishing part of a cmd key (e.g. "bancacore-api.jar").
func extractCmdFragment(cmdKey string) string {
	if start := strings.Index(cmdKey, "("); start >= 0 {
		end := strings.Index(cmdKey, ")")
		if end > start {
			return cmdKey[start+1 : end]
		}
	}
	return ""
}

// processSnapshot holds parsed process data from a single metrics record.
type processEntry struct {
	PID        int     `json:"pid"`
	Name       string  `json:"name"`
	CPUPercent float64 `json:"cpu_percent"`
	MemPercent float64 `json:"mem_percent"`
	RSS        int64   `json:"rss"`
	Status     string  `json:"status"`
	User       string  `json:"user"`
	Cmdline    string  `json:"cmdline"`
}

type processSnapshot struct {
	Processes  []processEntry `json:"processes"`
	TotalCount int            `json:"total_count"`
}

// handleProcessHistory aggregates process stats across historical snapshots
// to identify top resource consumers.
func handleProcessHistory(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")
	if agentID == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "agent ID is required"})
	}

	// Verify agent exists.
	_, err := e.App.FindRecordById("agents", agentID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{"error": "agent not found"})
	}

	rangeStr := e.Request.URL.Query().Get("range")
	rangeMs, rangeLabel := parseRangeParam(rangeStr)
	since := time.Now().UnixMilli() - rangeMs

	records, err := e.App.FindRecordsByFilter(
		"metrics",
		"agent_id = {:agentId} && type = 'processes' && timestamp >= {:since}",
		"-timestamp",
		500,
		0,
		map[string]any{
			"agentId": agentID,
			"since":   since,
		},
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query metrics"})
	}

	type processStats struct {
		name        string
		user        string
		sampleCount int
		cpuSum      float64
		maxCPU      float64
		memSum      float64
		maxMem      float64
		maxRSS      int64
	}

	statsMap := make(map[string]*processStats)

	for _, record := range records {
		dataStr := record.GetString("data")
		var snap processSnapshot
		if err := json.Unmarshal([]byte(dataStr), &snap); err != nil {
			continue
		}
		for _, p := range snap.Processes {
			if p.Name == "" {
				continue
			}
			key := extractCmdKey(p.Name, p.Cmdline)
			s, ok := statsMap[key]
			if !ok {
				s = &processStats{name: key}
				statsMap[key] = s
			}
			s.sampleCount++
			s.cpuSum += p.CPUPercent
			if p.CPUPercent > s.maxCPU {
				s.maxCPU = p.CPUPercent
			}
			s.memSum += p.MemPercent
			if p.MemPercent > s.maxMem {
				s.maxMem = p.MemPercent
			}
			if p.RSS > s.maxRSS {
				s.maxRSS = p.RSS
			}
			s.user = p.User
		}
	}

	type resultEntry struct {
		Name        string  `json:"name"`
		CmdFragment string  `json:"cmd_fragment,omitempty"`
		User        string  `json:"user"`
		SampleCount int     `json:"sample_count"`
		AvgCPU      float64 `json:"avg_cpu"`
		MaxCPU      float64 `json:"max_cpu"`
		AvgMem      float64 `json:"avg_mem"`
		MaxMem      float64 `json:"max_mem"`
		MaxRSS      int64   `json:"max_rss"`
	}

	results := make([]resultEntry, 0, len(statsMap))
	for key, s := range statsMap {
		var avgCPU, avgMem float64
		if s.sampleCount > 0 {
			avgCPU = math.Round(s.cpuSum/float64(s.sampleCount)*100) / 100
			avgMem = math.Round(s.memSum/float64(s.sampleCount)*100) / 100
		}
		results = append(results, resultEntry{
			Name:        key,
			CmdFragment: extractCmdFragment(key),
			User:        s.user,
			SampleCount: s.sampleCount,
			AvgCPU:      avgCPU,
			MaxCPU:      math.Round(s.maxCPU*100) / 100,
			AvgMem:      avgMem,
			MaxMem:      math.Round(s.maxMem*100) / 100,
			MaxRSS:      s.maxRSS,
		})
	}

	// Sort by avg_cpu descending, take top 20.
	sort.Slice(results, func(i, j int) bool {
		return results[i].AvgCPU > results[j].AvgCPU
	})
	if len(results) > 20 {
		results = results[:20]
	}

	return e.JSON(http.StatusOK, map[string]any{
		"range":          rangeLabel,
		"snapshot_count": len(records),
		"top_by_cpu":     results,
	})
}

// handleProcessTimeline returns time-series CPU/memory data for a specific process name.
func handleProcessTimeline(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")
	if agentID == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "agent ID is required"})
	}

	// "name" param accepts either a plain name ("nginx") or a cmd_key ("java (bancacore-api.jar)").
	processName := e.Request.URL.Query().Get("name")
	if processName == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "name param is required"})
	}
	// Extract the cmd_fragment if present (e.g. "bancacore-api.jar" from "java (bancacore-api.jar)").
	cmdFragment := extractCmdFragment(processName)
	// Plain process name without the fragment suffix for matching against p.Name.
	baseName := processName
	if idx := strings.Index(processName, " ("); idx >= 0 {
		baseName = processName[:idx]
	}

	// Verify agent exists.
	_, err := e.App.FindRecordById("agents", agentID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{"error": "agent not found"})
	}

	rangeStr := e.Request.URL.Query().Get("range")
	rangeMs, rangeLabel := parseRangeParam(rangeStr)
	since := time.Now().UnixMilli() - rangeMs

	records, err := e.App.FindRecordsByFilter(
		"metrics",
		"agent_id = {:agentId} && type = 'processes' && timestamp >= {:since}",
		"+timestamp",
		720,
		0,
		map[string]any{
			"agentId": agentID,
			"since":   since,
		},
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query metrics"})
	}

	type timelinePoint struct {
		Timestamp  int64   `json:"timestamp"`
		CPUPercent float64 `json:"cpu_percent"`
		MemPercent float64 `json:"mem_percent"`
		RSS        int64   `json:"rss"`
		PID        int     `json:"pid"`
	}

	points := make([]timelinePoint, 0, len(records))

	for _, record := range records {
		ts := record.GetInt("timestamp")
		dataStr := record.GetString("data")
		var snap processSnapshot
		if err := json.Unmarshal([]byte(dataStr), &snap); err != nil {
			continue
		}

		// Find the process instance with the highest cpu_percent.
		// If cmdFragment is set, match by cmdline content (e.g. "bancacore-api.jar").
		var best *processEntry
		for i := range snap.Processes {
			p := &snap.Processes[i]
			if p.Name != baseName {
				continue
			}
			if cmdFragment != "" && !strings.Contains(p.Cmdline, cmdFragment) {
				continue
			}
			if best == nil || p.CPUPercent > best.CPUPercent {
				best = p
			}
		}
		if best == nil {
			// Process not present in this snapshot — skip.
			continue
		}

		points = append(points, timelinePoint{
			Timestamp:  int64(ts),
			CPUPercent: math.Round(best.CPUPercent*100) / 100,
			MemPercent: math.Round(best.MemPercent*100) / 100,
			RSS:        best.RSS,
			PID:        best.PID,
		})
	}

	// Downsample to ~120 points if necessary.
	if len(points) > 120 {
		step := len(points) / 120
		sampled := make([]timelinePoint, 0, 120)
		for i := 0; i < len(points); i += step {
			sampled = append(sampled, points[i])
		}
		points = sampled
	}

	return e.JSON(http.StatusOK, map[string]any{
		"name":   processName,
		"range":  rangeLabel,
		"points": points,
	})
}

// handleHardware returns combined hardware information for an agent by reading the latest
// sysinfo, cpu, and memory metric records. Missing metrics are silently omitted.
func handleHardware(e *core.RequestEvent) error {
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

	type hardwareResponse struct {
		CPULogical      int     `json:"cpu_logical,omitempty"`
		CPUPhysical     int     `json:"cpu_physical,omitempty"`
		TotalRAM        int64   `json:"total_ram,omitempty"`
		Kernel          string  `json:"kernel,omitempty"`
		Arch            string  `json:"arch,omitempty"`
		Uptime          int64   `json:"uptime,omitempty"`
		Load1           float64 `json:"load1,omitempty"`
		Load5           float64 `json:"load5,omitempty"`
		Load15          float64 `json:"load15,omitempty"`
		Procs           int     `json:"procs,omitempty"`
		Platform        string  `json:"platform,omitempty"`
		PlatformVersion string  `json:"platform_version,omitempty"`
	}

	resp := hardwareResponse{}

	// Helper to fetch latest metric data of a given type.
	fetchLatest := func(metricType string) map[string]any {
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
			return nil
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(records[0].GetString("data")), &data); err != nil {
			return nil
		}
		return data
	}

	// Extract sysinfo fields.
	if sysinfo := fetchLatest("sysinfo"); sysinfo != nil {
		if v, ok := sysinfo["kernel_version"].(string); ok {
			resp.Kernel = v
		}
		if v, ok := sysinfo["arch"].(string); ok {
			resp.Arch = v
		}
		if v, ok := toFloat(sysinfo["uptime"]); ok {
			resp.Uptime = int64(v)
		}
		if v, ok := toFloat(sysinfo["load1"]); ok {
			resp.Load1 = v
		}
		if v, ok := toFloat(sysinfo["load5"]); ok {
			resp.Load5 = v
		}
		if v, ok := toFloat(sysinfo["load15"]); ok {
			resp.Load15 = v
		}
		if v, ok := toFloat(sysinfo["procs"]); ok {
			resp.Procs = int(v)
		}
		if v, ok := sysinfo["platform"].(string); ok {
			resp.Platform = v
		}
		if v, ok := sysinfo["platform_version"].(string); ok {
			resp.PlatformVersion = v
		}
	}

	// Extract cpu fields.
	if cpu := fetchLatest("cpu"); cpu != nil {
		if v, ok := toFloat(cpu["logical_count"]); ok {
			resp.CPULogical = int(v)
		}
		if v, ok := toFloat(cpu["physical_count"]); ok {
			resp.CPUPhysical = int(v)
		}
	}

	// Extract memory fields.
	if memory := fetchLatest("memory"); memory != nil {
		if v, ok := toFloat(memory["total"]); ok {
			resp.TotalRAM = int64(v)
		}
	}

	return e.JSON(http.StatusOK, resp)
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
