// Package report builds and delivers the weekly (or however configured)
// fleet email report: a fleet summary, alert activity, per-host resource
// peaks, black-box check health, CVE totals, and log volume, rendered as
// both HTML and plain text and sent through the hub's email notification
// channels. See ui/DESIGN.md § Weekly report and the README's "Weekly
// report" section for the full contract.
package report

import (
	"encoding/json"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/cron"

	"github.com/CogniDevAI/nexwatch/internal/hub/uptime"
)

// Settings keys stored in the "settings" collection (see
// internal/hub/backup for the same key/value-JSON convention).
const (
	EnabledKey    = "report_enabled"
	CronKey       = "report_cron"
	ChannelIDsKey = "report_channel_ids"
	PeriodDaysKey = "report_period_days"

	// PublicBaseURLKey is the hub's externally-reachable base URL, shown in
	// the report footer (and reused by other features later — see the
	// mission brief). Empty means "unknown"; the footer link is simply
	// omitted rather than guessed from a request Host header, since a
	// scheduled cron run has no request to read one from.
	PublicBaseURLKey = "public_base_url"

	// DefaultCron runs the report every Monday at 08:00.
	DefaultCron       = "0 8 * * 1"
	DefaultPeriodDays = 7

	// jobID identifies the cron job registered with app.Cron(), so a
	// subsequent Register call (e.g. after a settings change) safely
	// replaces it instead of accumulating duplicate jobs.
	jobID = "nexwatch_report"
)

// Settings is the resolved weekly-report configuration.
type Settings struct {
	Enabled    bool
	Cron       string
	ChannelIDs []string
	PeriodDays int
}

// LoadSettings reads report configuration from the "settings" collection,
// falling back to defaults for any key that is missing, unreadable, or the
// wrong type.
func LoadSettings(app core.App) Settings {
	s := Settings{Enabled: false, Cron: DefaultCron, PeriodDays: DefaultPeriodDays}

	if v, ok := getSetting(app, EnabledKey); ok {
		if b, ok := v.(bool); ok {
			s.Enabled = b
		}
	}
	if v, ok := getSetting(app, CronKey); ok {
		if str, ok := v.(string); ok && str != "" {
			s.Cron = str
		}
	}
	if v, ok := getSetting(app, PeriodDaysKey); ok {
		if f, ok := toFloat64(v); ok && f > 0 {
			s.PeriodDays = int(f)
		}
	}
	if v, ok := getSetting(app, ChannelIDsKey); ok {
		if raw, ok := v.([]any); ok {
			for _, id := range raw {
				if str, ok := id.(string); ok && str != "" {
					s.ChannelIDs = append(s.ChannelIDs, str)
				}
			}
		}
	}

	return s
}

func getSetting(app core.App, key string) (any, bool) {
	record, err := app.FindFirstRecordByFilter(
		"settings",
		"key = {:key}",
		map[string]any{"key": key},
	)
	if err != nil {
		return nil, false
	}

	raw := record.GetString("value")
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, false
	}
	return v, true
}

func getSettingString(app core.App, key, def string) string {
	v, ok := getSetting(app, key)
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return def
	}
	return s
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// Register loads Settings and, if enabled, schedules the "nexwatch_report"
// cron job that builds and sends the report on schedule. It also binds
// hooks on the "settings" collection so a create/update to any of this
// feature's keys re-registers the cron job immediately — unlike
// internal/hub/backup's Register (called once at startup only), the weekly
// report's schedule must take effect the moment an admin changes it in
// Settings, without a hub restart.
func Register(app core.App) {
	registerCron(app)

	app.OnRecordAfterCreateSuccess("settings").BindFunc(func(e *core.RecordEvent) error {
		if isReportSettingKey(e.Record.GetString("key")) {
			registerCron(app)
		}
		return e.Next()
	})
	app.OnRecordAfterUpdateSuccess("settings").BindFunc(func(e *core.RecordEvent) error {
		if isReportSettingKey(e.Record.GetString("key")) {
			registerCron(app)
		}
		return e.Next()
	})
}

func isReportSettingKey(key string) bool {
	switch key {
	case EnabledKey, CronKey, ChannelIDsKey, PeriodDaysKey:
		return true
	default:
		return false
	}
}

func registerCron(app core.App) {
	settings := LoadSettings(app)

	app.Cron().Remove(jobID)

	if !settings.Enabled {
		slog.Info("weekly report disabled via settings")
		return
	}

	cronExpr := settings.Cron
	if _, err := cron.NewSchedule(cronExpr); err != nil {
		slog.Error("invalid report_cron setting, falling back to default",
			"configured", cronExpr, "default", DefaultCron, "error", err)
		cronExpr = DefaultCron
	}

	app.Cron().MustAdd(jobID, cronExpr, func() {
		runScheduled(app)
	})
	slog.Info("weekly report scheduled", "cron", cronExpr, "period_days", settings.PeriodDays, "channels", len(settings.ChannelIDs))
}

// runScheduled builds and sends the report for the currently-configured
// period/channels. Settings are re-read here (rather than closed over at
// registerCron time) so a channel list or period-days change between
// registration and the next tick still takes effect.
func runScheduled(app core.App) {
	settings := LoadSettings(app)

	rpt, err := Build(app, settings.PeriodDays)
	if err != nil {
		slog.Error("failed to build scheduled weekly report", "error", err)
		return
	}

	results := Send(app, rpt)
	for _, r := range results {
		if r.Err != nil {
			slog.Error("failed to send scheduled weekly report", "channel_id", r.ChannelID, "error", r.Err)
		} else {
			slog.Info("scheduled weekly report sent", "channel_id", r.ChannelID)
		}
	}
}

// Report is the fully-resolved data behind one rendering of the weekly
// report (see Build).
type Report struct {
	PeriodDays  int
	PeriodStart time.Time
	PeriodEnd   time.Time
	GeneratedAt time.Time
	HubURL      string

	Fleet  FleetSummary
	Alerts AlertsSummary
	Peaks  []ResourcePeak
	Checks ChecksSummary
	CVE    []HostCVE
	Logs   []HostLogVolume
}

// FleetSummary covers agent counts and average uptime over the period.
type FleetSummary struct {
	AgentsTotal  int
	AgentsOnline int
	// AverageUptimePercent is the mean, across every agent, of that
	// agent's uptime over the period — computed the same way the public
	// status page does (see internal/hub/uptime): each agent's alert
	// history is treated as downtime intervals, merged, then measured
	// against the period window. An agent with no alert history at all
	// reads as 100% for the period, the same known limitation documented
	// there.
	AverageUptimePercent float64
}

// AlertsSummary covers alert activity fired during the period.
type AlertsSummary struct {
	CountBySeverity   map[string]int // "warning" | "critical" -> count
	TopRules          []RuleFiringCount
	MeanTimeToResolve time.Duration
}

// RuleFiringCount is one row of AlertsSummary.TopRules.
type RuleFiringCount struct {
	RuleName string
	Firings  int
}

// ResourcePeak is one host's peak resource usage over the period.
type ResourcePeak struct {
	Hostname  string
	MaxCPU    float64
	MaxMemory float64
	MaxDisk   float64
}

// ChecksSummary covers black-box check health over the period.
type ChecksSummary struct {
	UptimePercent      float64
	WorstLatencyName   string
	WorstLatencyMillis float64
}

// HostCVE is one host's latest CVE severity totals (a live snapshot, not
// period-scoped — there is no historical CVE time series to average over).
type HostCVE struct {
	Hostname string
	Critical int
	High     int
	Medium   int
	Low      int
}

// HostLogVolume is the number of log rows shipped by one host during the
// period.
type HostLogVolume struct {
	Hostname string
	Rows     int
}

// Build gathers every section of the report for the last periodDays, as of
// now. It never returns a partial Report on error for one section — a
// query failure for one section degrades that section to its zero value
// (documented per-field above) rather than failing the whole report,
// since a mostly-complete weekly digest is more useful than none at all
// when, say, the checks collection query hiccups.
func Build(app core.App, periodDays int) (Report, error) {
	if periodDays <= 0 {
		periodDays = DefaultPeriodDays
	}
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -periodDays)

	rpt := Report{
		PeriodDays:  periodDays,
		PeriodStart: start,
		PeriodEnd:   now,
		GeneratedAt: now,
		HubURL:      getSettingString(app, PublicBaseURLKey, ""),
	}

	agents, err := app.FindRecordsByFilter("agents", "", "hostname", 10000, 0)
	if err != nil {
		agents = nil
	}

	rpt.Fleet = buildFleetSummary(app, agents, start, now)
	rpt.Alerts = buildAlertsSummary(app, start, now)
	rpt.Peaks = buildResourcePeaks(app, agents, start, now)
	rpt.Checks = buildChecksSummary(app, start, now)
	rpt.CVE = buildCVETotals(app, agents)
	rpt.Logs = buildLogVolume(app, agents, start, now)

	return rpt, nil
}

func buildFleetSummary(app core.App, agents []*core.Record, start, end time.Time) FleetSummary {
	summary := FleetSummary{AgentsTotal: len(agents)}
	if len(agents) == 0 {
		summary.AverageUptimePercent = 100
		return summary
	}

	var totalUptime float64
	for _, agent := range agents {
		if agent.GetString("status") == "online" {
			summary.AgentsOnline++
		}
		totalUptime += agentUptimePercent(app, agent.Id, start, end)
	}
	summary.AverageUptimePercent = round2(totalUptime / float64(len(agents)))

	return summary
}

// agentUptimePercent mirrors internal/hub/api's computeAgentMetrics window
// calculation (same alert-history-as-downtime-proxy technique, sharing the
// interval math via internal/hub/uptime), scoped to [start, end] only —
// the report has no need for a day-by-day breakdown or last-incident
// timestamp, just the one window percentage.
func agentUptimePercent(app core.App, agentID string, start, end time.Time) float64 {
	alerts, err := app.FindRecordsByFilter(
		"alerts",
		"agent_id = {:id} && fired_at <= {:end}",
		"fired_at",
		5000,
		0,
		map[string]any{"id": agentID, "end": fmtTimestamp(end)},
	)
	if err != nil {
		alerts = nil
	}

	var intervals []uptime.Interval
	for _, a := range alerts {
		fired := a.GetDateTime("fired_at").Time()
		iEnd := end
		if resolvedStr := a.GetString("resolved_at"); resolvedStr != "" {
			if resolved := a.GetDateTime("resolved_at").Time(); !resolved.IsZero() {
				iEnd = resolved
			}
		}
		if iEnd.Before(fired) {
			iEnd = fired
		}
		if iEnd.Before(start) {
			continue
		}
		intervals = append(intervals, uptime.Interval{Start: fired, End: iEnd})
	}

	return round2(uptime.UptimePercent(uptime.Merge(intervals), start, end))
}

func buildAlertsSummary(app core.App, start, end time.Time) AlertsSummary {
	summary := AlertsSummary{CountBySeverity: map[string]int{"warning": 0, "critical": 0}}

	rules, err := app.FindRecordsByFilter("alert_rules", "", "", 10000, 0)
	if err != nil {
		rules = nil
	}
	ruleName := make(map[string]string, len(rules))
	ruleSeverity := make(map[string]string, len(rules))
	for _, r := range rules {
		ruleName[r.Id] = r.GetString("name")
		ruleSeverity[r.Id] = r.GetString("severity")
	}

	alerts, err := app.FindRecordsByFilter(
		"alerts",
		"fired_at >= {:start} && fired_at <= {:end}",
		"",
		20000,
		0,
		map[string]any{"start": fmtTimestamp(start), "end": fmtTimestamp(end)},
	)
	if err != nil {
		alerts = nil
	}

	firingsByRule := make(map[string]int)
	var resolvedCount int
	var totalResolveTime time.Duration
	for _, a := range alerts {
		ruleID := a.GetString("rule_id")
		firingsByRule[ruleID]++
		if sev := ruleSeverity[ruleID]; sev != "" {
			summary.CountBySeverity[sev]++
		}

		if resolvedStr := a.GetString("resolved_at"); resolvedStr != "" {
			fired := a.GetDateTime("fired_at").Time()
			resolved := a.GetDateTime("resolved_at").Time()
			if resolved.After(fired) {
				totalResolveTime += resolved.Sub(fired)
				resolvedCount++
			}
		}
	}

	if resolvedCount > 0 {
		summary.MeanTimeToResolve = totalResolveTime / time.Duration(resolvedCount)
	}

	type ruleCount struct {
		id    string
		count int
	}
	counts := make([]ruleCount, 0, len(firingsByRule))
	for id, c := range firingsByRule {
		counts = append(counts, ruleCount{id: id, count: c})
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].count != counts[j].count {
			return counts[i].count > counts[j].count
		}
		return counts[i].id < counts[j].id // stable tiebreaker
	})
	limit := 5
	if len(counts) < limit {
		limit = len(counts)
	}
	for _, c := range counts[:limit] {
		name := ruleName[c.id]
		if name == "" {
			name = "(deleted rule)"
		}
		summary.TopRules = append(summary.TopRules, RuleFiringCount{RuleName: name, Firings: c.count})
	}

	return summary
}

func buildResourcePeaks(app core.App, agents []*core.Record, start, end time.Time) []ResourcePeak {
	peaks := make([]ResourcePeak, 0, len(agents))
	for _, agent := range agents {
		peaks = append(peaks, ResourcePeak{
			Hostname:  agent.GetString("hostname"),
			MaxCPU:    round2(maxMetricPercent(app, agent.Id, "cpu", "total_percent", start, end)),
			MaxMemory: round2(maxMetricPercent(app, agent.Id, "memory", "used_percent", start, end)),
			MaxDisk:   round2(maxDiskPercent(app, agent.Id, start, end)),
		})
	}
	return peaks
}

// maxMetricPercent returns the maximum value of data[dataKey] across every
// "1h" downsampled metric of type metricType for agentID within [start,
// end], falling back to "raw" resolution when no downsampled rows exist
// yet (a freshly-added agent, or a period shorter than the downsampler's
// aggregation window).
func maxMetricPercent(app core.App, agentID, metricType, dataKey string, start, end time.Time) float64 {
	records := queryMetricsWithFallback(app, agentID, metricType, start, end)
	var max float64
	for _, r := range records {
		data, ok := decodeData(r)
		if !ok {
			continue
		}
		if v, ok := toFloat(data[dataKey]); ok && v > max {
			max = v
		}
	}
	return max
}

func maxDiskPercent(app core.App, agentID string, start, end time.Time) float64 {
	records := queryMetricsWithFallback(app, agentID, "disk", start, end)
	var max float64
	for _, r := range records {
		data, ok := decodeData(r)
		if !ok {
			continue
		}
		if v := diskPercentFromData(data); v > max {
			max = v
		}
	}
	return max
}

func queryMetricsWithFallback(app core.App, agentID, metricType string, start, end time.Time) []*core.Record {
	params := map[string]any{
		"agentId": agentID,
		"type":    metricType,
		"start":   fmtTimestamp(start),
		"end":     fmtTimestamp(end),
	}
	records, err := app.FindRecordsByFilter(
		"metrics",
		"agent_id = {:agentId} && type = {:type} && resolution = '1h' && timestamp >= {:start} && timestamp <= {:end}",
		"",
		5000, 0, params,
	)
	if err == nil && len(records) > 0 {
		return records
	}

	records, err = app.FindRecordsByFilter(
		"metrics",
		"agent_id = {:agentId} && type = {:type} && resolution = 'raw' && timestamp >= {:start} && timestamp <= {:end}",
		"",
		5000, 0, params,
	)
	if err != nil {
		return nil
	}
	return records
}

// diskPercentFromData returns the used_percent for the root "/" mount, or
// the first mount's used_percent if root is not present — mirroring
// internal/hub/api's extractDiskPercent, duplicated here rather than
// exported/shared since it is a small, stable piece of unmarshalled-JSON
// navigation, the same "repeat the small helper" convention this codebase
// already uses for settings getters (see internal/hub/backup,
// internal/hub/checks/retention.go).
func diskPercentFromData(data map[string]any) float64 {
	mountsRaw, ok := data["mounts"]
	if !ok {
		return 0
	}
	mounts, ok := mountsRaw.([]any)
	if !ok || len(mounts) == 0 {
		return 0
	}

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

	if first, ok := mounts[0].(map[string]any); ok {
		if v, ok := toFloat(first["used_percent"]); ok {
			return v
		}
	}
	return 0
}

func buildChecksSummary(app core.App, start, end time.Time) ChecksSummary {
	checkRecords, err := app.FindRecordsByFilter("checks", "", "", 10000, 0)
	if err != nil {
		checkRecords = nil
	}
	checkName := make(map[string]string, len(checkRecords))
	for _, c := range checkRecords {
		checkName[c.Id] = c.GetString("name")
	}

	results, err := app.FindRecordsByFilter(
		"check_results",
		"checked_at >= {:start} && checked_at <= {:end}",
		"",
		20000, 0,
		map[string]any{"start": fmtTimestamp(start), "end": fmtTimestamp(end)},
	)
	if err != nil {
		results = nil
	}

	var up, total int
	var worstLatency float64
	var worstCheckID string
	for _, r := range results {
		total++
		if r.GetString("status") == "up" {
			up++
		}
		if latency := r.GetFloat("latency_ms"); latency > worstLatency {
			worstLatency = latency
			worstCheckID = r.GetString("check_id")
		}
	}

	summary := ChecksSummary{UptimePercent: 100}
	if total > 0 {
		summary.UptimePercent = round2(100 * float64(up) / float64(total))
	}
	if worstCheckID != "" {
		name := checkName[worstCheckID]
		if name == "" {
			name = "(deleted check)"
		}
		summary.WorstLatencyName = name
		summary.WorstLatencyMillis = round2(worstLatency)
	}

	return summary
}

func buildCVETotals(app core.App, agents []*core.Record) []HostCVE {
	totals := make([]HostCVE, 0, len(agents))
	for _, agent := range agents {
		records, err := app.FindRecordsByFilter(
			"metrics",
			"agent_id = {:id} && type = 'cve_scan'",
			"-timestamp",
			1, 0,
			map[string]any{"id": agent.Id},
		)
		if err != nil || len(records) == 0 {
			continue
		}
		data, ok := decodeData(records[0])
		if !ok {
			continue
		}
		available, _ := data["available"].(bool)
		if !available {
			continue
		}
		rawTotals, ok := data["totals"].(map[string]any)
		if !ok {
			continue
		}
		host := HostCVE{Hostname: agent.GetString("hostname")}
		if v, ok := toFloat(rawTotals["critical"]); ok {
			host.Critical = int(v)
		}
		if v, ok := toFloat(rawTotals["high"]); ok {
			host.High = int(v)
		}
		if v, ok := toFloat(rawTotals["medium"]); ok {
			host.Medium = int(v)
		}
		if v, ok := toFloat(rawTotals["low"]); ok {
			host.Low = int(v)
		}
		totals = append(totals, host)
	}
	return totals
}

func buildLogVolume(app core.App, agents []*core.Record, start, end time.Time) []HostLogVolume {
	volumes := make([]HostLogVolume, 0, len(agents))
	for _, agent := range agents {
		count, err := app.CountRecords("logs", dbx.NewExp(
			"agent_id = {:id} AND ts >= {:start} AND ts <= {:end}",
			dbx.Params{"id": agent.Id, "start": fmtTimestamp(start), "end": fmtTimestamp(end)},
		))
		if err != nil {
			continue
		}
		if count == 0 {
			continue // omit hosts with no shipped logs this period, rather than a row of zeroes
		}
		volumes = append(volumes, HostLogVolume{Hostname: agent.GetString("hostname"), Rows: int(count)})
	}
	return volumes
}

func decodeData(record *core.Record) (map[string]any, bool) {
	var data map[string]any
	if err := json.Unmarshal([]byte(record.GetString("data")), &data); err != nil {
		return nil, false
	}
	return data, true
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func fmtTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.000Z")
}
