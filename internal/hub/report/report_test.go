package report

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/cron"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// agents/metrics/alert_rules/alerts/checks/check_results/logs/
	// notification_channels collections this package reads from.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
)

func newTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

func mustSetSetting(t *testing.T, app core.App, key string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal setting %q: %v", key, err)
	}
	col, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		t.Fatalf("find settings collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("key", key)
	rec.Set("value", string(encoded))
	if err := app.Save(rec); err != nil {
		t.Fatalf("save setting %q: %v", key, err)
	}
}

func mustCreateAgent(t *testing.T, app core.App, hostname, status string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("hostname", hostname)
	rec.Set("status", status)
	rec.Set("token", "token-"+hostname)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save agent %s: %v", hostname, err)
	}
	return rec
}

func mustCreateAlertRule(t *testing.T, app core.App, name, severity string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("alert_rules")
	if err != nil {
		t.Fatalf("find alert_rules collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("metric_type", "cpu")
	rec.Set("condition", "gt")
	rec.Set("threshold", 80)
	rec.Set("duration", 60)
	rec.Set("severity", severity)
	rec.Set("enabled", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save alert rule %s: %v", name, err)
	}
	return rec
}

func mustCreateAlert(t *testing.T, app core.App, ruleID, agentID, firedAt, resolvedAt string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("alerts")
	if err != nil {
		t.Fatalf("find alerts collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("rule_id", ruleID)
	rec.Set("agent_id", agentID)
	status := "firing"
	if resolvedAt != "" {
		status = "resolved"
	}
	rec.Set("status", status)
	rec.Set("value", 95)
	rec.Set("message", "test alert")
	rec.Set("fired_at", firedAt)
	if resolvedAt != "" {
		rec.Set("resolved_at", resolvedAt)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save alert: %v", err)
	}
	return rec
}

func mustCreateMetric(t *testing.T, app core.App, agentID, metricType, resolution, timestamp string, data map[string]any) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("metrics")
	if err != nil {
		t.Fatalf("find metrics collection: %v", err)
	}
	dataJSON, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal metric data: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("agent_id", agentID)
	rec.Set("type", metricType)
	rec.Set("data", string(dataJSON))
	rec.Set("timestamp", timestamp)
	rec.Set("resolution", resolution)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save metric: %v", err)
	}
}

func mustCreateCheck(t *testing.T, app core.App, name string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("checks")
	if err != nil {
		t.Fatalf("find checks collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("type", "tcp")
	rec.Set("target", "127.0.0.1:1")
	rec.Set("enabled", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save check: %v", err)
	}
	return rec
}

func mustCreateCheckResult(t *testing.T, app core.App, checkID, status string, latencyMs float64, checkedAt string) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("check_results")
	if err != nil {
		t.Fatalf("find check_results collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("check_id", checkID)
	rec.Set("status", status)
	rec.Set("latency_ms", latencyMs)
	rec.Set("checked_at", checkedAt)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save check result: %v", err)
	}
}

func mustCreateLog(t *testing.T, app core.App, agentID, ts string) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("logs")
	if err != nil {
		t.Fatalf("find logs collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("agent_id", agentID)
	rec.Set("ts", ts)
	rec.Set("level", "info")
	rec.Set("message", "test log line")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save log: %v", err)
	}
}

func mustCreateEmailChannel(t *testing.T, app core.App, name string, enabled bool) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("notification_channels")
	if err != nil {
		t.Fatalf("find notification_channels collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("type", "email")
	rec.Set("enabled", enabled)
	rec.Set("config", `{"host":"smtp.example.com","port":2525,"from":"reports@nexwatch.local","to":"ops@example.com"}`)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save email channel: %v", err)
	}
	return rec
}

func mustCreateWebhookChannel(t *testing.T, app core.App, name string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("notification_channels")
	if err != nil {
		t.Fatalf("find notification_channels collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("type", "webhook")
	rec.Set("enabled", true)
	rec.Set("config", `{"url":"https://example.com/hook"}`)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save webhook channel: %v", err)
	}
	return rec
}

const ts = "2006-01-02 15:04:05.000Z"

func TestBuild_FleetSummary(t *testing.T) {
	app := newTestApp(t)
	mustCreateAgent(t, app, "web-01", "online")
	mustCreateAgent(t, app, "db-01", "offline")

	rpt, err := Build(app, 7)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if rpt.Fleet.AgentsTotal != 2 {
		t.Errorf("AgentsTotal = %d, want 2", rpt.Fleet.AgentsTotal)
	}
	if rpt.Fleet.AgentsOnline != 1 {
		t.Errorf("AgentsOnline = %d, want 1", rpt.Fleet.AgentsOnline)
	}
	// Neither agent has any alert history, so both read as 100% uptime.
	if rpt.Fleet.AverageUptimePercent != 100 {
		t.Errorf("AverageUptimePercent = %v, want 100", rpt.Fleet.AverageUptimePercent)
	}
}

func TestBuild_AlertsSummary_CountsTopRulesAndMTTR(t *testing.T) {
	app := newTestApp(t)
	agent := mustCreateAgent(t, app, "web-01", "online")
	criticalRule := mustCreateAlertRule(t, app, "High CPU", "critical")
	warningRule := mustCreateAlertRule(t, app, "Disk warning", "warning")

	now := time.Now().UTC()
	fired := now.Add(-2 * time.Hour).Format(ts)
	resolved := now.Add(-1 * time.Hour).Format(ts) // 1h to resolve

	mustCreateAlert(t, app, criticalRule.Id, agent.Id, fired, resolved)
	mustCreateAlert(t, app, criticalRule.Id, agent.Id, fired, "")
	mustCreateAlert(t, app, warningRule.Id, agent.Id, fired, "")

	rpt, err := Build(app, 7)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	if rpt.Alerts.CountBySeverity["critical"] != 2 {
		t.Errorf("critical count = %d, want 2", rpt.Alerts.CountBySeverity["critical"])
	}
	if rpt.Alerts.CountBySeverity["warning"] != 1 {
		t.Errorf("warning count = %d, want 1", rpt.Alerts.CountBySeverity["warning"])
	}
	if len(rpt.Alerts.TopRules) == 0 || rpt.Alerts.TopRules[0].RuleName != "High CPU" || rpt.Alerts.TopRules[0].Firings != 2 {
		t.Errorf("TopRules[0] = %+v, want {High CPU, 2}", rpt.Alerts.TopRules)
	}
	if rpt.Alerts.MeanTimeToResolve != time.Hour {
		t.Errorf("MeanTimeToResolve = %v, want 1h", rpt.Alerts.MeanTimeToResolve)
	}
}

func TestBuild_ResourcePeaks(t *testing.T) {
	app := newTestApp(t)
	agent := mustCreateAgent(t, app, "web-01", "online")

	now := time.Now().UTC()
	mustCreateMetric(t, app, agent.Id, "cpu", "1h", now.Add(-3*time.Hour).Format(ts), map[string]any{"total_percent": 40.0})
	mustCreateMetric(t, app, agent.Id, "cpu", "1h", now.Add(-1*time.Hour).Format(ts), map[string]any{"total_percent": 91.5})
	mustCreateMetric(t, app, agent.Id, "memory", "1h", now.Add(-1*time.Hour).Format(ts), map[string]any{"used_percent": 72.0})
	mustCreateMetric(t, app, agent.Id, "disk", "1h", now.Add(-1*time.Hour).Format(ts), map[string]any{
		"mounts": []map[string]any{{"path": "/", "used_percent": 55.0}},
	})

	rpt, err := Build(app, 7)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(rpt.Peaks) != 1 {
		t.Fatalf("len(Peaks) = %d, want 1", len(rpt.Peaks))
	}
	peak := rpt.Peaks[0]
	if peak.Hostname != "web-01" {
		t.Errorf("Hostname = %q, want web-01", peak.Hostname)
	}
	if peak.MaxCPU != 91.5 {
		t.Errorf("MaxCPU = %v, want 91.5", peak.MaxCPU)
	}
	if peak.MaxMemory != 72 {
		t.Errorf("MaxMemory = %v, want 72", peak.MaxMemory)
	}
	if peak.MaxDisk != 55 {
		t.Errorf("MaxDisk = %v, want 55", peak.MaxDisk)
	}
}

func TestBuild_ResourcePeaks_FallsBackToRawWhenNoDownsampledRows(t *testing.T) {
	app := newTestApp(t)
	agent := mustCreateAgent(t, app, "web-02", "online")
	now := time.Now().UTC()
	mustCreateMetric(t, app, agent.Id, "cpu", "raw", now.Add(-30*time.Minute).Format(ts), map[string]any{"total_percent": 12.5})

	rpt, err := Build(app, 7)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(rpt.Peaks) != 1 || rpt.Peaks[0].MaxCPU != 12.5 {
		t.Errorf("Peaks = %+v, want a single raw-resolution fallback peak of 12.5", rpt.Peaks)
	}
}

func TestBuild_ChecksSummary(t *testing.T) {
	app := newTestApp(t)
	check := mustCreateCheck(t, app, "billing-api")
	now := time.Now().UTC()
	mustCreateCheckResult(t, app, check.Id, "up", 42, now.Add(-3*time.Hour).Format(ts))
	mustCreateCheckResult(t, app, check.Id, "up", 300, now.Add(-2*time.Hour).Format(ts))
	mustCreateCheckResult(t, app, check.Id, "down", 0, now.Add(-1*time.Hour).Format(ts))

	rpt, err := Build(app, 7)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	wantUptime := round2(100 * 2.0 / 3.0)
	if rpt.Checks.UptimePercent != wantUptime {
		t.Errorf("UptimePercent = %v, want %v", rpt.Checks.UptimePercent, wantUptime)
	}
	if rpt.Checks.WorstLatencyName != "billing-api" || rpt.Checks.WorstLatencyMillis != 300 {
		t.Errorf("worst latency = %q/%v, want billing-api/300", rpt.Checks.WorstLatencyName, rpt.Checks.WorstLatencyMillis)
	}
}

func TestBuild_CVETotals(t *testing.T) {
	app := newTestApp(t)
	agent := mustCreateAgent(t, app, "db-01", "online")
	now := time.Now().UTC()
	mustCreateMetric(t, app, agent.Id, "cve_scan", "raw", now.Format(ts), map[string]any{
		"available": true,
		"totals":    map[string]any{"critical": 2.0, "high": 5.0, "medium": 1.0, "low": 0.0},
	})

	rpt, err := Build(app, 7)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(rpt.CVE) != 1 {
		t.Fatalf("len(CVE) = %d, want 1", len(rpt.CVE))
	}
	got := rpt.CVE[0]
	if got.Hostname != "db-01" || got.Critical != 2 || got.High != 5 || got.Medium != 1 || got.Low != 0 {
		t.Errorf("CVE[0] = %+v, want {db-01 2 5 1 0}", got)
	}
}

func TestBuild_CVETotals_UnavailableScanIsOmitted(t *testing.T) {
	app := newTestApp(t)
	agent := mustCreateAgent(t, app, "db-02", "online")
	now := time.Now().UTC()
	mustCreateMetric(t, app, agent.Id, "cve_scan", "raw", now.Format(ts), map[string]any{"available": false})

	rpt, err := Build(app, 7)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(rpt.CVE) != 0 {
		t.Errorf("CVE = %+v, want empty (scan unavailable)", rpt.CVE)
	}
}

func TestBuild_LogVolume(t *testing.T) {
	app := newTestApp(t)
	agent := mustCreateAgent(t, app, "web-03", "online")
	other := mustCreateAgent(t, app, "web-04", "online")
	now := time.Now().UTC()
	mustCreateLog(t, app, agent.Id, now.Add(-1*time.Hour).Format(ts))
	mustCreateLog(t, app, agent.Id, now.Add(-2*time.Hour).Format(ts))
	_ = other // no logs for this one — should be omitted, not a zero row

	rpt, err := Build(app, 7)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}
	if len(rpt.Logs) != 1 || rpt.Logs[0].Hostname != "web-03" || rpt.Logs[0].Rows != 2 {
		t.Errorf("Logs = %+v, want [{web-03 2}]", rpt.Logs)
	}
}

func TestRenderHTML_ContainsExpectedSections(t *testing.T) {
	app := newTestApp(t)
	agent := mustCreateAgent(t, app, "web-01", "online")
	rule := mustCreateAlertRule(t, app, "High CPU", "critical")
	now := time.Now().UTC()
	mustCreateAlert(t, app, rule.Id, agent.Id, now.Add(-time.Hour).Format(ts), "")
	mustCreateMetric(t, app, agent.Id, "cpu", "1h", now.Add(-time.Hour).Format(ts), map[string]any{"total_percent": 50.0})
	check := mustCreateCheck(t, app, "billing-api")
	mustCreateCheckResult(t, app, check.Id, "up", 20, now.Add(-time.Hour).Format(ts))

	rpt, err := Build(app, 7)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	html, err := RenderHTML(rpt)
	if err != nil {
		t.Fatalf("RenderHTML() error: %v", err)
	}
	for _, want := range []string{"NexWatch weekly report", "Fleet", "Alerts", "Resource peaks", "Checks", "High CPU", "web-01", "billing-api"} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML report missing %q", want)
		}
	}
	if !strings.Contains(html, "prefers-color-scheme: dark") {
		t.Error("HTML report missing a dark-mode-safe style block")
	}
}

func TestRenderText_ContainsExpectedSectionsAndNoHTMLTags(t *testing.T) {
	app := newTestApp(t)
	agent := mustCreateAgent(t, app, "web-01", "online")
	rule := mustCreateAlertRule(t, app, "High CPU", "critical")
	now := time.Now().UTC()
	mustCreateAlert(t, app, rule.Id, agent.Id, now.Add(-time.Hour).Format(ts), "")

	rpt, err := Build(app, 7)
	if err != nil {
		t.Fatalf("Build() error: %v", err)
	}

	text, err := RenderText(rpt)
	if err != nil {
		t.Fatalf("RenderText() error: %v", err)
	}
	for _, want := range []string{"NexWatch weekly report", "FLEET", "ALERTS", "RESOURCE PEAKS", "CHECKS", "High CPU"} {
		if !strings.Contains(text, want) {
			t.Errorf("text report missing %q\n---\n%s", want, text)
		}
	}
	if strings.Contains(text, "<html") || strings.Contains(text, "<table") {
		t.Error("text report should not contain HTML markup")
	}
}

func TestSubject_IncludesPeriodDates(t *testing.T) {
	rpt := Report{
		PeriodStart: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		PeriodEnd:   time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC),
	}
	subject := Subject(rpt)
	if !strings.Contains(subject, "Jan 1") || !strings.Contains(subject, "Jan 8, 2026") {
		t.Errorf("Subject() = %q, want it to mention the period dates", subject)
	}
}

// --- Cron schedule parsing / hot-reload ------------------------------------

// findReportJob locates the "nexwatch_report" job among app.Cron().Jobs().
// tests.NewTestApp() boots a real PocketBase app, which registers its own
// built-in system cron jobs (log cleanup, DB optimize, etc.) — so
// asserting on Jobs() length or Total() directly would be coupled to
// PocketBase's own internal job count. Only this package's own job matters
// here.
func findReportJob(app core.App) *cron.Job {
	for _, j := range app.Cron().Jobs() {
		if j.Id() == jobID {
			return j
		}
	}
	return nil
}

func TestRegisterCron_DefaultCronParses(t *testing.T) {
	app := newTestApp(t)
	mustSetSetting(t, app, EnabledKey, true)

	Register(app)

	job := findReportJob(app)
	if job == nil {
		t.Fatalf("no %q job registered; jobs = %+v", jobID, app.Cron().Jobs())
	}
	if job.Expression() != DefaultCron {
		t.Errorf("job expression = %q, want default %q", job.Expression(), DefaultCron)
	}
}

func TestRegisterCron_InvalidCronFallsBackToDefault(t *testing.T) {
	app := newTestApp(t)
	mustSetSetting(t, app, EnabledKey, true)
	mustSetSetting(t, app, CronKey, "not a cron expression")

	Register(app)

	job := findReportJob(app)
	if job == nil || job.Expression() != DefaultCron {
		t.Fatalf("job = %+v, want one job with the default expression", job)
	}
}

func TestRegisterCron_DisabledRemovesJob(t *testing.T) {
	app := newTestApp(t)
	mustSetSetting(t, app, EnabledKey, false)

	Register(app)

	if job := findReportJob(app); job != nil {
		t.Errorf("found a %q job while disabled: %+v", jobID, job)
	}
}

func TestRegisterCron_SettingsChangeHotReloadsSchedule(t *testing.T) {
	app := newTestApp(t)
	mustSetSetting(t, app, EnabledKey, true)
	Register(app)

	job := findReportJob(app)
	if job == nil || job.Expression() != DefaultCron {
		t.Fatalf("initial job = %+v, want default expression %q", job, DefaultCron)
	}

	// Changing report_cron after Register must re-register the job with
	// the new schedule, without a process restart.
	col, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		t.Fatalf("find settings collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("key", CronKey)
	rec.Set("value", `"0 9 * * 2"`)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save report_cron setting: %v", err)
	}

	job = findReportJob(app)
	if job == nil || job.Expression() != "0 9 * * 2" {
		t.Fatalf("job = %+v, want the newly-saved cron expression", job)
	}
}

// --- Send: email-only fan-out ----------------------------------------------

// fakeRawSender is a rawSender that never dials real SMTP — used so
// Send's channel-selection logic (email-only, enabled-only) can be tested
// without network access or a slow/hanging connection attempt against the
// fixture channels' fake smtp.example.com host.
type fakeRawSender struct {
	sent []string // channel ids actually asked to deliver
}

func (f *fakeRawSender) SendRaw(_ context.Context, _, _, _ string, channel *core.Record) error {
	f.sent = append(f.sent, channel.Id)
	return nil
}

func TestSend_SkipsNonEmailAndDisabledChannels(t *testing.T) {
	app := newTestApp(t)
	email := mustCreateEmailChannel(t, app, "ops-email", true)
	disabledEmail := mustCreateEmailChannel(t, app, "ops-email-disabled", false)
	webhook := mustCreateWebhookChannel(t, app, "ops-webhook")

	rpt := Report{PeriodStart: time.Now().Add(-7 * 24 * time.Hour), PeriodEnd: time.Now()}
	fake := &fakeRawSender{}
	results := send(app, rpt, []string{email.Id, disabledEmail.Id, webhook.Id, "does-not-exist"}, fake)

	if len(results) != 4 {
		t.Fatalf("len(results) = %d, want 4", len(results))
	}

	byID := make(map[string]SendResult, len(results))
	for _, r := range results {
		byID[r.ChannelID] = r
	}

	if err := byID[email.Id].Err; err != nil {
		t.Errorf("enabled email channel should have succeeded, got error: %v", err)
	}
	if err := byID[disabledEmail.Id].Err; err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Errorf("disabled channel error = %v, want a 'disabled' error", err)
	}
	if err := byID[webhook.Id].Err; err == nil || !strings.Contains(err.Error(), "not email") {
		t.Errorf("webhook channel error = %v, want a 'not email' error", err)
	}
	if err := byID["does-not-exist"].Err; err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("missing channel error = %v, want a 'not found' error", err)
	}

	if len(fake.sent) != 1 || fake.sent[0] != email.Id {
		t.Errorf("SendRaw was invoked for %v, want only the enabled email channel %q", fake.sent, email.Id)
	}
}

func TestSend_UsesConfiguredChannelIDsFromSettings(t *testing.T) {
	app := newTestApp(t)
	email := mustCreateEmailChannel(t, app, "ops-email", true)
	mustSetSetting(t, app, ChannelIDsKey, []string{email.Id})

	rpt := Report{PeriodStart: time.Now().Add(-7 * 24 * time.Hour), PeriodEnd: time.Now()}
	fake := &fakeRawSender{}
	// Exercise Send's own settings lookup by swapping in the fake sender
	// through the unexported worker with settings-derived channel IDs,
	// mirroring exactly what the exported Send does internally.
	results := send(app, rpt, LoadSettings(app).ChannelIDs, fake)

	if len(results) != 1 || results[0].ChannelID != email.Id || results[0].Err != nil {
		t.Fatalf("results = %+v, want one successful delivery to %q", results, email.Id)
	}
}
