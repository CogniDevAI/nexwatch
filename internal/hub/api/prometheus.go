package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/CogniDevAI/nexwatch/internal/hub/agenttoken"
	"github.com/CogniDevAI/nexwatch/internal/hub/audit"
	"github.com/CogniDevAI/nexwatch/internal/hub/metrics"
)

// prometheusTokenSettingKey stores the SHA-256 hash of the bearer token
// required to scrape GET /metrics — never the plaintext. This matters more
// here than for most other settings: the "settings" collection's ListRule/
// ViewRule allow ANY authenticated user (see users_roles_rbac.go), so a
// plaintext secret stored under this key would be readable by a viewer
// role account. Hashing it (the same convention agents.token_hash already
// uses for agent auth — see internal/hub/agenttoken) means the stored
// value is safe to expose through the ordinary settings read path; only
// the one-time generation response ever carries the plaintext.
const prometheusTokenSettingKey = "prometheus_token"

// RegisterPrometheusRoute registers the unauthenticated (bearer-token
// gated) GET /metrics endpoint directly on the top-level router, mirroring
// RegisterHealthRoute/RegisterPublicStatusRoute — Prometheus itself has no
// way to carry a PocketBase auth record, so this can't live behind
// apis.RequireAuth() the way /api/custom/* does. It carries its own
// authorization instead (a bearer token compared against the hashed
// prometheus_token setting).
func RegisterPrometheusRoute(se *core.ServeEvent, checker publicStatusChecker, metricsSvc *metrics.Service) {
	se.Router.GET("/metrics", func(e *core.RequestEvent) error {
		return handlePrometheusMetrics(e, checker, metricsSvc)
	})
}

// RegisterPrometheusTokenRoute registers the admin-only route that
// generates (or rotates) the /metrics bearer token, on apiGroup ("/api/
// custom"). It returns every route it registered, for openapi_test.go to
// cross-check against docs/openapi.yaml.
func RegisterPrometheusTokenRoute(apiGroup *router.RouterGroup[*core.RequestEvent]) []RegisteredRoute {
	rec := newRouteRecorder(apiGroup, "/api/custom")

	// POST /api/custom/prometheus/token — generate/rotate the /metrics
	// bearer token. Returns the plaintext exactly once; only its SHA-256
	// hash is persisted (prometheus_token setting).
	rec.POST("/prometheus/token", handleGeneratePrometheusToken).Bind(RequireRole(RoleAdmin))

	return rec.Registered
}

func handleGeneratePrometheusToken(e *core.RequestEvent) error {
	plaintext, hash, err := agenttoken.Generate()
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to generate token",
		})
	}

	if err := setSettingValue(e.App, prometheusTokenSettingKey, hash); err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to store token",
		})
	}

	audit.Record(e.App, e, audit.Entry{
		Action:     "prometheus.token.generate",
		TargetType: "settings",
		TargetID:   prometheusTokenSettingKey,
		Result:     "success",
	})

	return e.JSON(http.StatusOK, map[string]string{"token": plaintext})
}

func handlePrometheusMetrics(e *core.RequestEvent, checker publicStatusChecker, metricsSvc *metrics.Service) error {
	storedHash := settingString(e.App, prometheusTokenSettingKey, "")
	if storedHash == "" {
		return e.JSON(http.StatusNotFound, map[string]string{"error": "prometheus exposition is not enabled"})
	}

	provided := strings.TrimPrefix(e.Request.Header.Get("Authorization"), "Bearer ")
	if provided == "" || !constantTimeHashEqual(agenttoken.Hash(provided), storedHash) {
		return e.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid or missing bearer token"})
	}

	body := buildPrometheusExposition(e.App, checker, metricsSvc)
	e.Response.Header().Set("Content-Type", "text/plain; version=0.0.4")
	e.Response.WriteHeader(http.StatusOK)
	_, err := e.Response.Write(body)
	return err
}

func constantTimeHashEqual(a, b string) bool {
	// Both sides are always 64-char hex SHA-256 digests here, so a
	// length-safe byte comparison is enough; subtle.ConstantTimeCompare
	// additionally guards against any future change to that shape.
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

type promMetricFamily struct {
	name string
	help string
	typ  string // "gauge" | "counter"
	buf  strings.Builder
}

func newPromFamily(name, help, typ string) *promMetricFamily {
	return &promMetricFamily{name: name, help: help, typ: typ}
}

func (f *promMetricFamily) add(value float64, labelPairs ...string) {
	if len(labelPairs) == 0 {
		f.buf.WriteString(f.name)
		f.buf.WriteByte(' ')
		f.buf.WriteString(formatPromFloat(value))
		f.buf.WriteByte('\n')
		return
	}

	f.buf.WriteString(f.name)
	f.buf.WriteByte('{')
	for i := 0; i+1 < len(labelPairs); i += 2 {
		if i > 0 {
			f.buf.WriteByte(',')
		}
		f.buf.WriteString(labelPairs[i])
		f.buf.WriteString(`="`)
		f.buf.WriteString(escapePromLabelValue(labelPairs[i+1]))
		f.buf.WriteByte('"')
	}
	f.buf.WriteString("} ")
	f.buf.WriteString(formatPromFloat(value))
	f.buf.WriteByte('\n')
}

// render writes this family's HELP/TYPE header followed by its samples, but
// only if at least one sample was added — an empty family (e.g. no docker
// containers reported yet) omits itself entirely rather than printing a
// header with zero series under it.
func (f *promMetricFamily) render(out *strings.Builder) {
	if f.buf.Len() == 0 {
		return
	}
	out.WriteString("# HELP ")
	out.WriteString(f.name)
	out.WriteByte(' ')
	out.WriteString(f.help)
	out.WriteByte('\n')
	out.WriteString("# TYPE ")
	out.WriteString(f.name)
	out.WriteByte(' ')
	out.WriteString(f.typ)
	out.WriteByte('\n')
	out.WriteString(f.buf.String())
	out.WriteByte('\n')
}

func formatPromFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// escapePromLabelValue escapes a label value per the text exposition
// format: backslash and double-quote are backslash-escaped, and a literal
// newline becomes "\n".
func escapePromLabelValue(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	v = strings.ReplaceAll(v, "\n", `\n`)
	return v
}

// buildPrometheusExposition renders the full text-exposition-format body
// for GET /metrics. It queries collections directly (agents, metrics,
// docker_containers, checks, alerts, alert_rules) rather than reusing the
// dashboard/checks-summary handlers' response shapes, since Prometheus
// wants one flat sample per series rather than a nested JSON document.
// Iteration order (agents/checks/containers sorted by id) is deterministic
// so scrapes are stable and tests can assert on exact output.
func buildPrometheusExposition(app core.App, checker publicStatusChecker, metricsSvc *metrics.Service) []byte {
	var out strings.Builder

	hubUptime := newPromFamily("nexwatch_hub_uptime_seconds", "Seconds since the hub process started.", "counter")
	hubUptime.add(time.Since(startTime).Seconds())
	hubUptime.render(&out)

	agents, err := app.FindRecordsByFilter("agents", "", "id", 10000, 0)
	if err != nil {
		agents = nil
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].Id < agents[j].Id })

	online := 0
	for _, a := range agents {
		if a.GetString("status") == "online" {
			online++
		}
	}
	agentsOnline := newPromFamily("nexwatch_agents_online", "Number of agents currently online.", "gauge")
	agentsOnline.add(float64(online))
	agentsOnline.render(&out)

	agentsTotal := newPromFamily("nexwatch_agents_total", "Total number of registered agents.", "gauge")
	agentsTotal.add(float64(len(agents)))
	agentsTotal.render(&out)

	agentUp := newPromFamily("nexwatch_agent_up", "Whether the agent is currently online (1) or not (0).", "gauge")
	cpuPct := newPromFamily("nexwatch_cpu_percent", "Latest total CPU usage percent.", "gauge")
	memPct := newPromFamily("nexwatch_memory_percent", "Latest memory used percent.", "gauge")
	diskPct := newPromFamily("nexwatch_disk_percent", "Latest root filesystem used percent.", "gauge")
	netRx := newPromFamily("nexwatch_network_rx_bytes_per_second", "Latest inbound network throughput, summed across non-loopback interfaces.", "gauge")
	netTx := newPromFamily("nexwatch_network_tx_bytes_per_second", "Latest outbound network throughput, summed across non-loopback interfaces.", "gauge")
	load1 := newPromFamily("nexwatch_load1", "1-minute load average.", "gauge")
	dropped := newPromFamily("nexwatch_agent_dropped_messages_total", "Cumulative count of messages the agent's transport has discarded due to a full outgoing queue.", "counter")
	cveFindings := newPromFamily("nexwatch_cve_findings", "Latest CVE scan finding count by severity.", "gauge")

	for _, agent := range agents {
		hostname := agent.GetString("hostname")
		up := 0.0
		if agent.GetString("status") == "online" {
			up = 1
		}
		agentUp.add(up, "agent_id", agent.Id, "hostname", hostname)
		dropped.add(agent.GetFloat("dropped_messages"), "agent_id", agent.Id, "hostname", hostname)

		latest, _ := metricsSvc.GetLatestMetricsByAgent(agent.Id)
		if rec, ok := latest["cpu"]; ok {
			if v, ok := jsonNumber(rec, "total_percent"); ok {
				cpuPct.add(v, "agent_id", agent.Id, "hostname", hostname)
			}
		}
		if rec, ok := latest["memory"]; ok {
			if v, ok := jsonNumber(rec, "used_percent"); ok {
				memPct.add(v, "agent_id", agent.Id, "hostname", hostname)
			}
		}
		if rec, ok := latest["disk"]; ok {
			if data, ok := decodeMetricData(rec); ok {
				diskPct.add(extractDiskPercent(data), "agent_id", agent.Id, "hostname", hostname)
			}
		}
		if rec, ok := latest["network"]; ok {
			if data, ok := decodeMetricData(rec); ok {
				rx, tx := sumNetworkRates(data)
				netRx.add(rx, "agent_id", agent.Id, "hostname", hostname)
				netTx.add(tx, "agent_id", agent.Id, "hostname", hostname)
			}
		}

		if data, ok := latestMetricDataByType(app, agent.Id, "sysinfo"); ok {
			if v, ok := toFloat(data["load1"]); ok {
				load1.add(v, "agent_id", agent.Id, "hostname", hostname)
			}
		}

		if data, ok := latestMetricDataByType(app, agent.Id, "cve_scan"); ok {
			if available, _ := data["available"].(bool); available {
				if totals, ok := data["totals"].(map[string]any); ok {
					severities := make([]string, 0, len(totals))
					for sev := range totals {
						severities = append(severities, sev)
					}
					sort.Strings(severities)
					for _, sev := range severities {
						if v, ok := toFloat(totals[sev]); ok {
							cveFindings.add(v, "agent_id", agent.Id, "hostname", hostname, "severity", sev)
						}
					}
				}
			}
		}
	}
	agentUp.render(&out)
	cpuPct.render(&out)
	memPct.render(&out)
	diskPct.render(&out)
	netRx.render(&out)
	netTx.render(&out)
	load1.render(&out)
	dropped.render(&out)
	cveFindings.render(&out)

	// --- Docker containers ---
	containers, err := app.FindRecordsByFilter("docker_containers", "", "id", 20000, 0)
	if err != nil {
		containers = nil
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].Id < containers[j].Id })

	containerCPU := newPromFamily("nexwatch_container_cpu_percent", "Latest per-container CPU usage percent.", "gauge")
	containerMem := newPromFamily("nexwatch_container_memory_bytes", "Latest per-container memory usage in bytes.", "gauge")
	containerUp := newPromFamily("nexwatch_container_up", "Whether the container is currently running (1) or not (0).", "gauge")
	for _, c := range containers {
		labels := []string{"agent_id", c.GetString("agent_id"), "container", c.GetString("name"), "image", c.GetString("image")}
		containerCPU.add(c.GetFloat("cpu_percent"), labels...)
		containerMem.add(c.GetFloat("memory_usage"), labels...)
		up := 0.0
		if c.GetString("status") == "running" {
			up = 1
		}
		containerUp.add(up, labels...)
	}
	containerCPU.render(&out)
	containerMem.render(&out)
	containerUp.render(&out)

	// --- Checks ---
	checkRecords, err := app.FindRecordsByFilter("checks", "", "id", 10000, 0)
	if err != nil {
		checkRecords = nil
	}
	sort.Slice(checkRecords, func(i, j int) bool { return checkRecords[i].Id < checkRecords[j].Id })
	snapshot := checker.Snapshot()

	checkUp := newPromFamily("nexwatch_check_up", "Whether the check's most recent probe succeeded (1) or not (0).", "gauge")
	checkLatency := newPromFamily("nexwatch_check_latency_milliseconds", "Latency of the check's most recent probe, in milliseconds.", "gauge")
	checkTLSExpiry := newPromFamily("nexwatch_check_tls_expiry_seconds", "Seconds until the check's TLS certificate expires.", "gauge")
	for _, c := range checkRecords {
		snap, known := snapshot[c.Id]
		if !known {
			continue // never probed yet — no meaningful sample to emit
		}
		labels := []string{"check_id", c.Id, "name", c.GetString("name"), "type", c.GetString("type")}
		up := 0.0
		if snap.Status == "up" {
			up = 1
		}
		checkUp.add(up, labels...)
		checkLatency.add(snap.LatencyMs, labels...)
		if !snap.TLSExpiresAt.IsZero() {
			checkTLSExpiry.add(time.Until(snap.TLSExpiresAt).Seconds(), labels...)
		}
	}
	checkUp.render(&out)
	checkLatency.render(&out)
	checkTLSExpiry.render(&out)

	// --- Alerts ---
	ruleSeverity := make(map[string]string)
	rules, err := app.FindRecordsByFilter("alert_rules", "", "", 10000, 0)
	if err == nil {
		for _, r := range rules {
			ruleSeverity[r.Id] = r.GetString("severity")
		}
	}
	firing, err := app.FindRecordsByFilter("alerts", "status = 'firing'", "", 20000, 0)
	if err != nil {
		firing = nil
	}
	firingBySeverity := map[string]int{"warning": 0, "critical": 0}
	for _, a := range firing {
		sev := ruleSeverity[a.GetString("rule_id")]
		if sev == "" {
			continue
		}
		firingBySeverity[sev]++
	}
	alertsFiring := newPromFamily("nexwatch_alerts_firing", "Number of currently-firing alerts by severity.", "gauge")
	for _, sev := range []string{"warning", "critical"} {
		alertsFiring.add(float64(firingBySeverity[sev]), "severity", sev)
	}
	alertsFiring.render(&out)

	return []byte(out.String())
}

// latestMetricDataByType returns the decoded "data" JSON of the most recent
// "metrics" record of type for agentID. Unlike metrics.Service's
// GetLatestMetricsByAgent (hardcoded to cpu/memory/disk/network), this
// queries a single arbitrary type — used here for "sysinfo" (load1) and
// "cve_scan", neither of which that method fetches.
func latestMetricDataByType(app core.App, agentID, metricType string) (map[string]any, bool) {
	records, err := app.FindRecordsByFilter(
		"metrics",
		"agent_id = {:id} && type = {:type}",
		"-timestamp",
		1,
		0,
		map[string]any{"id": agentID, "type": metricType},
	)
	if err != nil || len(records) == 0 {
		return nil, false
	}
	return decodeMetricData(records[0])
}

func decodeMetricData(record *core.Record) (map[string]any, bool) {
	var data map[string]any
	if err := json.Unmarshal([]byte(record.GetString("data")), &data); err != nil {
		return nil, false
	}
	return data, true
}

func jsonNumber(record *core.Record, key string) (float64, bool) {
	data, ok := decodeMetricData(record)
	if !ok {
		return 0, false
	}
	return toFloat(data[key])
}

// sumNetworkRates sums bytes_recv_per_sec/bytes_sent_per_sec across every
// non-loopback interface in a "network" metric's decoded data (see
// internal/agent/collector/network.go for the shape). Loopback is excluded
// since it never reflects real external throughput and would otherwise
// double-count local traffic between processes on the same host.
func sumNetworkRates(data map[string]any) (rx, tx float64) {
	ifaces, ok := data["interfaces"].([]any)
	if !ok {
		return 0, 0
	}
	for _, raw := range ifaces {
		iface, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := iface["name"].(string); name == "lo" || strings.HasPrefix(name, "lo0") {
			continue
		}
		if v, ok := toFloat(iface["bytes_recv_per_sec"]); ok {
			rx += v
		}
		if v, ok := toFloat(iface["bytes_sent_per_sec"]); ok {
			tx += v
		}
	}
	return rx, tx
}
