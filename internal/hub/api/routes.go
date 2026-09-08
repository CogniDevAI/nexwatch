package api

import (
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
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/CogniDevAI/nexwatch/docs"
	"github.com/CogniDevAI/nexwatch/internal/hub/agenttoken"
	"github.com/CogniDevAI/nexwatch/internal/hub/audit"
	"github.com/CogniDevAI/nexwatch/internal/hub/metrics"
	"github.com/CogniDevAI/nexwatch/internal/hub/threaddump"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// CommandSender can send a command to a connected agent.
type CommandSender interface {
	SendCommand(agentID string, payload *protocol.CommandPayload) error
}

// RegisteredRoute describes one HTTP route registered under "/api/custom".
// openapi_test.go derives the expected route list from these (rather than
// a hand-maintained slice) so it fails when a new route is added to
// RegisterRoutes/RegisterAlertRoutes without a matching entry in
// docs/openapi.yaml.
type RegisteredRoute struct {
	Method string
	Path   string // full path including the "/api/custom" prefix
}

// routeRecorder wraps a router.RouterGroup, recording every route
// registered through it (with its full prefixed path) alongside actually
// registering it.
type routeRecorder struct {
	group      *router.RouterGroup[*core.RequestEvent]
	prefix     string
	Registered []RegisteredRoute
}

func newRouteRecorder(group *router.RouterGroup[*core.RequestEvent], prefix string) *routeRecorder {
	return &routeRecorder{group: group, prefix: prefix}
}

func (r *routeRecorder) GET(path string, action func(e *core.RequestEvent) error) *router.Route[*core.RequestEvent] {
	r.Registered = append(r.Registered, RegisteredRoute{Method: http.MethodGet, Path: r.prefix + path})
	return r.group.GET(path, action)
}

func (r *routeRecorder) POST(path string, action func(e *core.RequestEvent) error) *router.Route[*core.RequestEvent] {
	r.Registered = append(r.Registered, RegisteredRoute{Method: http.MethodPost, Path: r.prefix + path})
	return r.group.POST(path, action)
}

// RegisterRoutes registers all custom API routes on apiGroup, which the
// caller must already have bound with the desired auth middleware (e.g.
// apis.RequireAuth()) and mounted at the "/api/custom" prefix. It returns
// every route it registered, for openapi_test.go to cross-check against
// docs/openapi.yaml.
func RegisterRoutes(se *core.ServeEvent, apiGroup *router.RouterGroup[*core.RequestEvent], metricsSvc *metrics.Service, cmdSender CommandSender) []RegisteredRoute {
	tdSvc := threaddump.NewService(se.App)
	rec := newRouteRecorder(apiGroup, "/api/custom")

	// GET /api/custom/dashboard — agent summaries with latest metrics
	rec.GET("/dashboard", func(e *core.RequestEvent) error {
		return handleDashboard(e, metricsSvc)
	})

	// GET /api/custom/metrics — time-range metric queries
	rec.GET("/metrics", func(e *core.RequestEvent) error {
		return handleMetricsQuery(e, metricsSvc)
	})

	// GET /api/custom/openapi.yaml — this hub's OpenAPI specification
	rec.GET("/openapi.yaml", handleOpenAPISpec)

	// POST /api/custom/agents/{id}/token — generate/rotate a hashed agent token
	rec.POST("/agents/{id}/token", handleGenerateAgentToken).Bind(RequireRole(RoleOperator))

	// GET /api/custom/agents/{id}/processes/history — aggregate process stats over a time range
	rec.GET("/agents/{id}/processes/history", handleProcessHistory)

	// GET /api/custom/agents/{id}/processes/timeline — time-series data for a specific process name
	rec.GET("/agents/{id}/processes/timeline", handleProcessTimeline)

	// GET /api/custom/agents/{id}/ports — latest open ports for an agent
	rec.GET("/agents/{id}/ports", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "ports")
	})

	// GET /api/custom/agents/{id}/processes — latest process list for an agent
	rec.GET("/agents/{id}/processes", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "processes")
	})

	// GET /api/custom/agents/{id}/hardening — latest hardening report for an agent
	rec.GET("/agents/{id}/hardening", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "hardening")
	})

	// GET /api/custom/agents/{id}/vulnerabilities — latest vulnerability scan for an agent
	rec.GET("/agents/{id}/vulnerabilities", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "vulnerabilities")
	})

	// GET /api/custom/agents/{id}/diskio — latest disk I/O stats for an agent
	rec.GET("/agents/{id}/diskio", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "diskio")
	})

	// GET /api/custom/agents/{id}/connections — latest TCP connection summary for an agent
	rec.GET("/agents/{id}/connections", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "connections")
	})

	// GET /api/custom/agents/{id}/services — latest systemd service list for an agent
	rec.GET("/agents/{id}/services", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "services")
	})

	// GET /api/custom/agents/{id}/hardware — system hardware info from sysinfo, cpu, memory metrics
	rec.GET("/agents/{id}/hardware", handleHardware)

	// GET /api/custom/agents/{id}/oracle — latest Oracle metrics for an agent
	rec.GET("/agents/{id}/oracle", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "oracle")
	})

	// GET /api/custom/agents/{id}/cve — latest CVE scan report for an agent
	rec.GET("/agents/{id}/cve", func(e *core.RequestEvent) error {
		return handleLatestMetricByType(e, "cve_scan")
	})

	// POST /api/custom/agents/{id}/thread-dump — request a thread dump for a PID
	rec.POST("/agents/{id}/thread-dump", func(e *core.RequestEvent) error {
		return handleRequestThreadDump(e, tdSvc, cmdSender)
	}).Bind(RequireRole(RoleOperator))

	// GET /api/custom/agents/{id}/thread-dumps — list historical thread dumps
	rec.GET("/agents/{id}/thread-dumps", handleListThreadDumps)

	// GET /api/custom/thread-dumps/{dumpId} — get a single thread dump by ID
	rec.GET("/thread-dumps/{dumpId}", handleGetThreadDump)

	return rec.Registered
}

// handleOpenAPISpec serves the embedded OpenAPI 3.1 document describing
// every /api/custom/* route (see docs/openapi.yaml).
func handleOpenAPISpec(e *core.RequestEvent) error {
	e.Response.Header().Set("Content-Type", "application/yaml")
	_, err := e.Response.Write(docs.OpenAPISpec)
	return err
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
		// CveTotals is the agent's latest cve_scan severity totals
		// (critical/high/medium/low/unknown/fixable), omitted when no scan
		// has been reported yet — cheap to include since GetLatestMetricsByAgent
		// already fetched every metric type for this agent.
		CveTotals map[string]int `json:"cve_totals,omitempty"`
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
				case "cve_scan":
					if available, _ := data["available"].(bool); available {
						if totals, ok := data["totals"].(map[string]any); ok {
							ct := make(map[string]int, len(totals))
							for k, v := range totals {
								if f, ok := toFloat(v); ok {
									ct[k] = int(f)
								}
							}
							if len(ct) > 0 {
								summary.CveTotals = ct
							}
						}
					}
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
// Response format: { cpu: {timestamps, values}, memory: {timestamps, values}, disk: {timestamps, values},
// network_rx: {timestamps, values}, network_tx: {timestamps, values},
// network_rx_rate: {timestamps, values}, network_tx_rate: {timestamps, values} }
//
// network_rx/network_tx are the agent's cumulative byte counters since it
// started (in MB, for backwards compatibility with existing UI callers);
// network_rx_rate/network_tx_rate are the current throughput in bytes per
// second, sourced from the agent's bytes_recv_per_sec/bytes_sent_per_sec
// fields (see internal/agent/collector/network.go). Prefer the *_rate
// series for anything labeled as a rate — the cumulative fields are not one.
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
	networkRxRate := timeSeries{Timestamps: []float64{}, Values: []float64{}}
	networkTxRate := timeSeries{Timestamps: []float64{}, Values: []float64{}}

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

			rxRate, txRate := extractNetworkRates(data)
			networkRxRate.Timestamps = append(networkRxRate.Timestamps, ts)
			networkRxRate.Values = append(networkRxRate.Values, rxRate)
			networkTxRate.Timestamps = append(networkTxRate.Timestamps, ts)
			networkTxRate.Values = append(networkTxRate.Values, txRate)
		}
	}

	// Sort all series by timestamp (records come sorted by -timestamp, we need ascending).
	sortTimeSeries(&cpu)
	sortTimeSeries(&memory)
	sortTimeSeries(&disk)
	sortTimeSeries(&networkRx)
	sortTimeSeries(&networkTx)
	sortTimeSeries(&networkRxRate)
	sortTimeSeries(&networkTxRate)

	return e.JSON(http.StatusOK, map[string]any{
		"cpu":             cpu,
		"memory":          memory,
		"disk":            disk,
		"network_rx":      networkRx,
		"network_tx":      networkTx,
		"network_rx_rate": networkRxRate,
		"network_tx_rate": networkTxRate,
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

// bestNetworkInterface returns the data map of the interface with the most
// cumulative traffic (skipping loopback), or nil if none is found. Both
// extractNetworkBytes and extractNetworkRates key off the same interface so
// the cumulative totals and the current rate always describe the same NIC.
func bestNetworkInterface(data map[string]any) map[string]any {
	interfacesRaw, ok := data["interfaces"]
	if !ok {
		return nil
	}
	interfaces, ok := interfacesRaw.([]any)
	if !ok || len(interfaces) == 0 {
		return nil
	}

	var best map[string]any
	var bestTotal float64
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
		if best == nil || rx+tx > bestTotal {
			best = ifMap
			bestTotal = rx + tx
		}
	}
	return best
}

// extractNetworkBytes returns total bytes_recv and bytes_sent from the
// primary interface, converted to MB. This is a cumulative counter since
// the agent started — not a rate — kept only for backwards compatibility;
// see extractNetworkRates for current throughput.
func extractNetworkBytes(data map[string]any) (float64, float64) {
	best := bestNetworkInterface(data)
	if best == nil {
		return 0, 0
	}
	rx, _ := toFloat(best["bytes_recv"])
	tx, _ := toFloat(best["bytes_sent"])
	// Convert to MB for display.
	return math.Round(rx/1024/1024*100) / 100, math.Round(tx/1024/1024*100) / 100
}

// extractNetworkRates returns the current bytes-per-second receive/send
// rate from the primary interface's bytes_recv_per_sec/bytes_sent_per_sec
// fields (see internal/agent/collector/network.go). Older agent versions
// that have not yet reported these fields fall back to 0 rather than
// misrepresenting the cumulative counters as a rate.
func extractNetworkRates(data map[string]any) (float64, float64) {
	best := bestNetworkInterface(data)
	if best == nil {
		return 0, 0
	}
	rxRate, _ := toFloat(best["bytes_recv_per_sec"])
	txRate, _ := toFloat(best["bytes_sent_per_sec"])
	return math.Round(rxRate*100) / 100, math.Round(txRate*100) / 100
}

// handleGenerateAgentToken issues a new random authentication token for an
// agent. Only the SHA-256 hash of the token is persisted (agents.token_hash);
// the plaintext value is returned in the response exactly once and is not
// recoverable afterwards. Calling this again for the same agent rotates the
// token, invalidating the previous one.
func handleGenerateAgentToken(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")
	if agentID == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{
			"error": "agent ID is required",
		})
	}

	agent, err := e.App.FindRecordById("agents", agentID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{
			"error": "agent not found",
		})
	}

	plaintext, hash, err := agenttoken.Generate()
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to generate token",
		})
	}

	agent.Set("token_hash", hash)
	if agent.GetString("status") == "" {
		agent.Set("status", "pending")
	}
	if err := e.App.Save(agent); err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to save token",
		})
	}

	audit.Record(e.App, e, audit.Entry{
		Action:     "agent.token.regenerate",
		TargetType: "agents",
		TargetID:   agent.Id,
		AgentID:    agent.Id,
		Details:    map[string]any{"hostname": agent.GetString("hostname")},
		Result:     "success",
	})

	return e.JSON(http.StatusOK, map[string]string{
		"token": plaintext,
	})
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
		// Wildfly/JBoss: identified by jboss-modules.jar or jboss.server.base.dir.
		// Extract the instance name from -Djboss.server.base.dir=/path/to/wildfly/INSTANCE.
		if strings.Contains(cmdline, "jboss-modules.jar") || strings.Contains(cmdline, "jboss.server.base.dir") {
			const baseDirFlag = "-Djboss.server.base.dir="
			if idx := strings.Index(cmdline, baseDirFlag); idx >= 0 {
				rest := strings.TrimSpace(cmdline[idx+len(baseDirFlag):])
				// The value ends at the next space (next JVM flag) or end of string.
				fields := strings.Fields(rest)
				if len(fields) > 0 {
					instance := filepath.Base(fields[0])
					return name + " (wildfly/" + instance + ")"
				}
			}
			return name + " (wildfly)"
		}
		// ActiveMQ: identified by activemq.jar.
		if strings.Contains(cmdline, "activemq.jar") {
			return name + " (activemq)"
		}
		// Look for -jar /path/to/something.jar
		if idx := strings.Index(cmdline, "-jar "); idx >= 0 {
			rest := strings.TrimSpace(cmdline[idx+5:])
			fields := strings.Fields(rest)
			if len(fields) > 0 {
				return name + " (" + filepath.Base(fields[0]) + ")"
			}
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
			"diskio":          `{"devices":[]}`,
			"connections":     `{"summary":{},"total":0,"by_port":[]}`,
			"services":        `{"services":[],"total":0,"running":0,"failed":0,"other":0}`,
			"cve_scan":        `{"scanner":"","scanner_version":"","db_updated_at":"","scanned_at":"","duration_ms":0,"stale":false,"available":false,"error":"no scan has been reported yet","targets":[],"totals":{"critical":0,"high":0,"medium":0,"low":0,"unknown":0,"fixable":0}}`,
			"oracle":          `{"instance":{},"sessions":{},"blocked_sessions":[],"top_sql":[],"tablespaces":[],"sga":{},"pga":{},"waits":[],"locks":[]}`,
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

// ─── Thread Dump handlers ──────────────────────────────────────────────────────

// handleRequestThreadDump triggers a thread dump for a given PID on an agent.
// Before dispatching the command it verifies the PID is present in the
// agent's latest process snapshot and looks like a JVM process, to prevent
// requesting a jstack dump against an arbitrary/unrelated PID.
func handleRequestThreadDump(e *core.RequestEvent, tdSvc *threaddump.Service, sender CommandSender) error {
	agentID := e.Request.PathValue("id")

	var body struct {
		PID         int    `json:"pid"`
		ProcessName string `json:"process_name"`
	}
	if err := json.NewDecoder(e.Request.Body).Decode(&body); err != nil {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
	}
	if body.PID == 0 {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "pid is required"})
	}

	procs, err := latestProcessSnapshot(e.App, agentID)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to load process snapshot"})
	}
	if ok, reason := isDumpablePID(procs, body.PID); !ok {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": reason})
	}

	requestedBy := ""
	if e.Auth != nil {
		requestedBy = e.Auth.Id
		if email := e.Auth.GetString("email"); email != "" {
			requestedBy = fmt.Sprintf("%s (%s)", e.Auth.Id, email)
		}
	}

	requestID, err := tdSvc.RequestDump(agentID, body.PID, body.ProcessName, requestedBy, sender.SendCommand)
	if err != nil {
		audit.Record(e.App, e, audit.Entry{
			Action:     "threaddump.request",
			TargetType: "agents",
			TargetID:   agentID,
			AgentID:    agentID,
			Details:    map[string]any{"pid": body.PID, "process_name": body.ProcessName},
			Result:     "failure",
		})
		return e.JSON(http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
	}

	audit.Record(e.App, e, audit.Entry{
		Action:     "threaddump.request",
		TargetType: "agents",
		TargetID:   agentID,
		AgentID:    agentID,
		Details:    map[string]any{"pid": body.PID, "process_name": body.ProcessName, "request_id": requestID},
		Result:     "success",
	})

	return e.JSON(http.StatusAccepted, map[string]string{
		"request_id": requestID,
		"status":     "pending",
	})
}

// latestProcessSnapshot loads and decodes the most recently recorded
// "processes" metric snapshot for an agent. It returns a nil slice (not an
// error) when no snapshot has been recorded yet.
func latestProcessSnapshot(app core.App, agentID string) ([]processEntry, error) {
	records, err := app.FindRecordsByFilter(
		"metrics",
		"agent_id = {:agentId} && type = 'processes'",
		"-timestamp",
		1, 0,
		map[string]any{"agentId": agentID},
	)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}

	var snap processSnapshot
	if err := json.Unmarshal([]byte(records[0].GetString("data")), &snap); err != nil {
		return nil, err
	}
	return snap.Processes, nil
}

// handleListThreadDumps returns the dump history for an agent.
func handleListThreadDumps(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")

	records, err := e.App.FindRecordsByFilter(
		"thread_dumps",
		"agent_id = {:agentId}",
		"-taken_at", 50, 0,
		map[string]any{"agentId": agentID},
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch thread dumps"})
	}

	type dumpSummary struct {
		ID          string `json:"id"`
		PID         int    `json:"pid"`
		ProcessName string `json:"process_name"`
		RequestID   string `json:"request_id"`
		Status      string `json:"status"`
		Error       string `json:"error,omitempty"`
		TakenAt     string `json:"taken_at"`
	}

	items := make([]dumpSummary, 0, len(records))
	for _, r := range records {
		items = append(items, dumpSummary{
			ID:          r.Id,
			PID:         int(r.GetFloat("pid")),
			ProcessName: r.GetString("process_name"),
			RequestID:   r.GetString("request_id"),
			Status:      r.GetString("status"),
			Error:       r.GetString("error"),
			TakenAt:     r.GetString("taken_at"),
		})
	}

	return e.JSON(http.StatusOK, map[string]any{
		"dumps": items,
		"total": len(items),
	})
}

// handleGetThreadDump returns the full output of a single dump.
func handleGetThreadDump(e *core.RequestEvent) error {
	dumpID := e.Request.PathValue("dumpId")

	record, err := e.App.FindRecordById("thread_dumps", dumpID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{"error": "dump not found"})
	}

	return e.JSON(http.StatusOK, map[string]any{
		"id":           record.Id,
		"pid":          int(record.GetFloat("pid")),
		"process_name": record.GetString("process_name"),
		"request_id":   record.GetString("request_id"),
		"status":       record.GetString("status"),
		"output":       record.GetString("output"),
		"error":        record.GetString("error"),
		"taken_at":     record.GetString("taken_at"),
	})
}
