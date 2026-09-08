package alerts

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// agents/metrics/alert_rules/alerts collections the engine relies on.
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

func createAgent(t *testing.T, app core.App, hostname, status string) *core.Record {
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

func createRule(t *testing.T, app core.App, opts ruleOpts) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("alert_rules")
	if err != nil {
		t.Fatalf("find alert_rules collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", opts.name)
	rec.Set("metric_type", opts.metricType)
	rec.Set("condition", opts.condition)
	rec.Set("threshold", opts.threshold)
	rec.Set("duration", opts.durationSeconds)
	rec.Set("severity", opts.severity)
	rec.Set("enabled", opts.enabled)
	if opts.agentID != "" {
		rec.Set("agent_id", opts.agentID)
	}
	if len(opts.targetTags) > 0 {
		rec.Set("target_tags", opts.targetTags)
	}
	if opts.target != "" {
		rec.Set("target", opts.target)
	}
	if opts.escalationAfter > 0 {
		rec.Set("escalation_after", opts.escalationAfter)
	}
	if opts.checkID != "" {
		rec.Set("check_id", opts.checkID)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save rule %s: %v", opts.name, err)
	}
	return rec
}

type ruleOpts struct {
	name            string
	metricType      string
	condition       string
	threshold       float64
	durationSeconds float64
	severity        string
	enabled         bool
	agentID         string
	targetTags      []string
	target          string
	escalationAfter float64
	checkID         string
}

// createAgentWithTags is createAgent plus a "tags" JSON array, used by the
// targeting/silence tests below.
func createAgentWithTags(t *testing.T, app core.App, hostname, status string, tags []string) *core.Record {
	t.Helper()
	agent := createAgent(t, app, hostname, status)
	agent.Set("tags", tags)
	if err := app.Save(agent); err != nil {
		t.Fatalf("save agent tags for %s: %v", hostname, err)
	}
	return agent
}

// createSilence inserts a "silences" record covering [startsAt, endsAt).
func createSilence(t *testing.T, app core.App, agentID string, tags []string, startsAt, endsAt time.Time) *core.Record {
	t.Helper()
	return createSilenceWithChecks(t, app, agentID, tags, nil, startsAt, endsAt)
}

// createSilenceWithChecks is createSilence plus check_ids, used by the
// check-based silence-matching tests below.
func createSilenceWithChecks(t *testing.T, app core.App, agentID string, tags, checkIDs []string, startsAt, endsAt time.Time) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("silences")
	if err != nil {
		t.Fatalf("find silences collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "test-silence")
	if agentID != "" {
		rec.Set("agent_id", agentID)
	}
	if len(tags) > 0 {
		rec.Set("tags", tags)
	}
	if len(checkIDs) > 0 {
		rec.Set("check_ids", checkIDs)
	}
	rec.Set("starts_at", startsAt.UTC().Format("2006-01-02 15:04:05.000Z"))
	rec.Set("ends_at", endsAt.UTC().Format("2006-01-02 15:04:05.000Z"))
	if err := app.Save(rec); err != nil {
		t.Fatalf("save silence: %v", err)
	}
	return rec
}

func createMetric(t *testing.T, app core.App, agentID, metricType string, data map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("metrics")
	if err != nil {
		t.Fatalf("find metrics collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("agent_id", agentID)
	rec.Set("type", metricType)
	rec.Set("data", data)
	rec.Set("timestamp", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))
	rec.Set("resolution", "raw")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save metric for agent %s: %v", agentID, err)
	}
	return rec
}

func countAlerts(t *testing.T, app core.App, ruleID, agentID string) []*core.Record {
	t.Helper()
	recs, err := app.FindRecordsByFilter(
		"alerts",
		"rule_id = {:r} && agent_id = {:a}",
		"-fired_at",
		0,
		0,
		map[string]any{"r": ruleID, "a": agentID},
	)
	if err != nil {
		t.Fatalf("find alerts: %v", err)
	}
	return recs
}

func TestAlertState_String(t *testing.T) {
	tests := []struct {
		state AlertState
		want  string
	}{
		{StateOK, "ok"},
		{StateWarning, "warning"},
		{StateCritical, "critical"},
		{StateResolved, "resolved"},
		{AlertState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("AlertState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestCheckCondition(t *testing.T) {
	app := newTestApp(t)
	eng := NewEngine(app)

	tests := []struct {
		name      string
		value     float64
		condition string
		threshold float64
		want      bool
	}{
		{"gt breaches when greater", 90, "gt", 80, true},
		{"gt does not breach when equal", 80, "gt", 80, false},
		{"gt does not breach when less", 70, "gt", 80, false},
		{"lt breaches when less", 10, "lt", 20, true},
		{"lt does not breach when equal", 20, "lt", 20, false},
		{"lt does not breach when greater", 30, "lt", 20, false},
		{"eq breaches when equal", 50, "eq", 50, true},
		{"eq does not breach when different", 51, "eq", 50, false},
		{"unknown condition never breaches", 1000, "bogus", 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eng.checkCondition(tt.value, tt.condition, tt.threshold); got != tt.want {
				t.Errorf("checkCondition(%v, %q, %v) = %v, want %v", tt.value, tt.condition, tt.threshold, got, tt.want)
			}
		})
	}
}

func TestGetLatestMetricValue_FieldFallbacks(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-fallback", "online")
	eng := NewEngine(app)

	tests := []struct {
		name       string
		metricType string
		data       map[string]any
		wantValue  float64
		wantOK     bool
	}{
		{"cpu prefers total_percent", "cpu", map[string]any{"total_percent": 42.0, "percent": 1.0}, 42.0, true},
		{"cpu falls back to percent", "cpu", map[string]any{"percent": 33.0}, 33.0, true},
		{"memory prefers used_percent", "memory", map[string]any{"used_percent": 55.0}, 55.0, true},
		{"disk falls back to percent", "disk", map[string]any{"percent": 60.0}, 60.0, true},
		{"network prefers bytes_recv", "network", map[string]any{"bytes_recv": 1024.0, "bytes_sent": 2048.0}, 1024.0, true},
		{"network falls back to bytes_sent", "network", map[string]any{"bytes_sent": 2048.0}, 2048.0, true},
		{"unrecognized type has no matching field", "docker", map[string]any{"cpu_percent": 5.0}, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			createMetric(t, app, agent.Id, tt.metricType, tt.data)

			val, ok := eng.getLatestMetricValue(agent.Id, tt.metricType)
			if ok != tt.wantOK {
				t.Fatalf("getLatestMetricValue() ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && val != tt.wantValue {
				t.Errorf("getLatestMetricValue() = %v, want %v", val, tt.wantValue)
			}
		})
	}
}

func TestGetLatestMetricValue_NoMetricsReturnsNotOK(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-nodata", "online")
	eng := NewEngine(app)

	_, ok := eng.getLatestMetricValue(agent.Id, "cpu")
	if ok {
		t.Fatal("getLatestMetricValue() with no metric records expected ok=false")
	}
}

// fakeClock lets tests advance time deterministically.
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func TestUpdateState_OKToWarningToCriticalAfterDuration(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-1", "online")
	rule := createRule(t, app, ruleOpts{
		name: "cpu-high", metricType: "cpu", condition: "gt", threshold: 80,
		durationSeconds: 30, severity: "critical", enabled: true,
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now

	var fired int
	eng.SetNotifyFunc(func(app core.App, alert, rule *core.Record) { fired++ })

	key := ruleAgentKey{RuleID: rule.Id, AgentID: agent.Id}

	// First breach: transitions OK -> warning, does not fire yet.
	eng.updateState(rule, agent.Id, true, 95, "critical", 30*time.Second, false)
	if got := eng.states[key].State; got != StateWarning {
		t.Fatalf("after first breach, state = %v, want %v", got, StateWarning)
	}
	if fired != 0 {
		t.Fatalf("fired = %d, want 0 before duration elapses", fired)
	}

	// Still within duration: stays warning, still no alert.
	clock.Advance(10 * time.Second)
	eng.updateState(rule, agent.Id, true, 95, "critical", 30*time.Second, false)
	if got := eng.states[key].State; got != StateWarning {
		t.Fatalf("within duration, state = %v, want %v", got, StateWarning)
	}
	if fired != 0 {
		t.Fatalf("fired = %d, want 0 still within duration", fired)
	}

	// Duration elapsed: transitions warning -> critical and fires once.
	clock.Advance(21 * time.Second) // total 31s since first breach
	eng.updateState(rule, agent.Id, true, 95, "critical", 30*time.Second, false)
	if got := eng.states[key].State; got != StateCritical {
		t.Fatalf("after duration elapsed, state = %v, want %v", got, StateCritical)
	}
	if fired != 1 {
		t.Fatalf("fired = %d, want 1 after duration elapses", fired)
	}

	alerts := countAlerts(t, app, rule.Id, agent.Id)
	if len(alerts) != 1 {
		t.Fatalf("len(alerts) = %d, want 1", len(alerts))
	}
	if got := alerts[0].GetString("status"); got != "firing" {
		t.Fatalf("alert status = %q, want firing", got)
	}

	// Value drops back under threshold: resolves and returns to OK.
	eng.updateState(rule, agent.Id, false, 50, "critical", 30*time.Second, false)
	if got := eng.states[key].State; got != StateOK {
		t.Fatalf("after resolution, state = %v, want %v", got, StateOK)
	}
	if got := eng.states[key].ActiveAlertID; got != "" {
		t.Fatalf("after resolution, ActiveAlertID = %q, want empty", got)
	}

	resolved, err := app.FindRecordById("alerts", alerts[0].Id)
	if err != nil {
		t.Fatalf("find alert after resolution: %v", err)
	}
	if got := resolved.GetString("status"); got != "resolved" {
		t.Fatalf("alert status after resolution = %q, want resolved", got)
	}
	if resolved.GetString("resolved_at") == "" {
		t.Error("resolved alert has no resolved_at timestamp")
	}
}

// TestUpdateState_ResolutionDispatchesNotification guards the fix that
// makes resolveAlert call the registered NotifyFunc: some channels (e.g.
// PagerDuty's event_action="resolve") depend on receiving a dispatch for
// the resolved alert, not just the firing one.
func TestUpdateState_ResolutionDispatchesNotification(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-resolve-notify", "online")
	rule := createRule(t, app, ruleOpts{
		name: "cpu-resolve-notify", metricType: "cpu", condition: "gt", threshold: 80,
		durationSeconds: 10, severity: "critical", enabled: true,
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now

	var statuses []string
	eng.SetNotifyFunc(func(app core.App, alert, rule *core.Record) {
		statuses = append(statuses, alert.GetString("status"))
	})

	// Breach, then fire once the duration elapses: one "firing" dispatch.
	eng.updateState(rule, agent.Id, true, 95, "critical", 10*time.Second, false)
	clock.Advance(11 * time.Second)
	eng.updateState(rule, agent.Id, true, 95, "critical", 10*time.Second, false)
	if len(statuses) != 1 || statuses[0] != "firing" {
		t.Fatalf("statuses after firing = %v, want [firing]", statuses)
	}

	// Breach stops: resolveAlert must dispatch a second notification for
	// the now-resolved record.
	eng.updateState(rule, agent.Id, false, 50, "critical", 10*time.Second, false)
	if len(statuses) != 2 || statuses[1] != "resolved" {
		t.Fatalf("statuses after resolution = %v, want [firing resolved]", statuses)
	}
}

func TestUpdateState_WarningSeverityStaysWarningAfterFiring(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-2", "online")
	rule := createRule(t, app, ruleOpts{
		name: "mem-warn", metricType: "memory", condition: "gt", threshold: 70,
		durationSeconds: 10, severity: "warning", enabled: true,
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now

	key := ruleAgentKey{RuleID: rule.Id, AgentID: agent.Id}

	eng.updateState(rule, agent.Id, true, 85, "warning", 10*time.Second, false)
	clock.Advance(11 * time.Second)
	eng.updateState(rule, agent.Id, true, 85, "warning", 10*time.Second, false)

	if got := eng.states[key].State; got != StateWarning {
		t.Fatalf("severity=warning after duration elapsed, state = %v, want %v (must not escalate to critical)", got, StateWarning)
	}

	alerts := countAlerts(t, app, rule.Id, agent.Id)
	if len(alerts) != 1 {
		t.Fatalf("len(alerts) = %d, want 1 (warning severity still fires an alert)", len(alerts))
	}
}

// TestUpdateState_CooldownReNotifiesWithoutNewRecord is the regression test
// for the alert storm: a rule that keeps breaching past its cooldown period
// must re-dispatch a notification, but it must NEVER insert a second
// "alerts" row for the same (rule_id, agent_id) pair while the first one is
// still firing. The old implementation created a brand new record on every
// cooldown-elapsed cycle (and, for warning-severity rules, on every single
// evaluation cycle once duration had elapsed) — this asserts the fix.
func TestUpdateState_CooldownReNotifiesWithoutNewRecord(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-3", "online")
	rule := createRule(t, app, ruleOpts{
		name: "cpu-cooldown", metricType: "cpu", condition: "gt", threshold: 80,
		durationSeconds: 30, severity: "critical", enabled: true,
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now
	eng.cooldownPeriod = 60 * time.Second

	var notifications int
	eng.SetNotifyFunc(func(app core.App, alert, rule *core.Record) { notifications++ })

	// Breach, then reach critical after duration -> fires alert #1.
	eng.updateState(rule, agent.Id, true, 95, "critical", 30*time.Second, false)
	clock.Advance(31 * time.Second)
	eng.updateState(rule, agent.Id, true, 95, "critical", 30*time.Second, false)

	alerts := countAlerts(t, app, rule.Id, agent.Id)
	if len(alerts) != 1 {
		t.Fatalf("after first fire, len(alerts) = %d, want 1", len(alerts))
	}
	if notifications != 1 {
		t.Fatalf("notifications = %d, want 1 after first fire", notifications)
	}
	firstAlertID := alerts[0].Id

	// Still breaching, still critical, but within cooldown: no re-notify,
	// no new record.
	clock.Advance(20 * time.Second) // 20s since last notify, cooldown is 60s
	eng.updateState(rule, agent.Id, true, 97, "critical", 30*time.Second, false)

	if alerts := countAlerts(t, app, rule.Id, agent.Id); len(alerts) != 1 {
		t.Fatalf("within cooldown, len(alerts) = %d, want 1 (no duplicate)", len(alerts))
	}
	if notifications != 1 {
		t.Fatalf("notifications = %d, want 1 still within cooldown", notifications)
	}

	// Cooldown has now elapsed since the last notification: re-notifies,
	// but still updates the SAME record rather than inserting a new one.
	clock.Advance(61 * time.Second) // 81s since last notify, past 60s cooldown
	eng.updateState(rule, agent.Id, true, 99, "critical", 30*time.Second, false)

	alerts = countAlerts(t, app, rule.Id, agent.Id)
	if len(alerts) != 1 {
		t.Fatalf("after cooldown elapsed, len(alerts) = %d, want 1 (still no duplicate)", len(alerts))
	}
	if alerts[0].Id != firstAlertID {
		t.Fatalf("after cooldown elapsed, alert id changed from %q to %q, want the same record reused", firstAlertID, alerts[0].Id)
	}
	if got := alerts[0].GetFloat("value"); got != 99 {
		t.Fatalf("after cooldown elapsed, alert value = %v, want 99 (refreshed to latest reading)", got)
	}
	if notifications != 2 {
		t.Fatalf("notifications = %d, want 2 (re-notified once after cooldown)", notifications)
	}
}

// TestUpdateState_ContinuedBreachUpdatesSameRecord asserts that repeated
// evaluation cycles while a rule keeps breaching — well before any cooldown
// elapses — never insert additional "alerts" rows, and that the single
// existing row's value/message stay current.
func TestUpdateState_ContinuedBreachUpdatesSameRecord(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-continued", "online")
	rule := createRule(t, app, ruleOpts{
		name: "mem-continued", metricType: "memory", condition: "gt", threshold: 70,
		durationSeconds: 10, severity: "warning", enabled: true,
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now

	// First breach, then fire once duration elapses.
	eng.updateState(rule, agent.Id, true, 80, "warning", 10*time.Second, false)
	clock.Advance(11 * time.Second)
	eng.updateState(rule, agent.Id, true, 82, "warning", 10*time.Second, false)

	alerts := countAlerts(t, app, rule.Id, agent.Id)
	if len(alerts) != 1 {
		t.Fatalf("after first fire, len(alerts) = %d, want 1", len(alerts))
	}

	// Several more evaluation cycles, still breaching, well within cooldown:
	// this must keep updating the same record, never insert another.
	for i, v := range []float64{85, 90, 93} {
		clock.Advance(30 * time.Second)
		eng.updateState(rule, agent.Id, true, v, "warning", 10*time.Second, false)

		alerts = countAlerts(t, app, rule.Id, agent.Id)
		if len(alerts) != 1 {
			t.Fatalf("cycle %d: len(alerts) = %d, want 1 (no duplicate rows while still breaching)", i, len(alerts))
		}
	}

	if got := alerts[0].GetFloat("value"); got != 93 {
		t.Fatalf("final alert value = %v, want 93 (updated to the latest breach reading)", got)
	}
}

// TestUpdateState_RebreachAfterResolveCreatesNewRecord asserts that once an
// incident resolves, a fresh breach starts a brand new incident (a new
// "alerts" row) rather than reusing the resolved one.
func TestUpdateState_RebreachAfterResolveCreatesNewRecord(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-rebreach", "online")
	rule := createRule(t, app, ruleOpts{
		name: "cpu-rebreach", metricType: "cpu", condition: "gt", threshold: 80,
		durationSeconds: 10, severity: "critical", enabled: true,
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now

	// First incident: breach, fire, resolve.
	eng.updateState(rule, agent.Id, true, 90, "critical", 10*time.Second, false)
	clock.Advance(11 * time.Second)
	eng.updateState(rule, agent.Id, true, 90, "critical", 10*time.Second, false)
	eng.updateState(rule, agent.Id, false, 50, "critical", 10*time.Second, false)

	alerts := countAlerts(t, app, rule.Id, agent.Id)
	if len(alerts) != 1 {
		t.Fatalf("after first incident resolves, len(alerts) = %d, want 1", len(alerts))
	}
	if got := alerts[0].GetString("status"); got != "resolved" {
		t.Fatalf("first alert status = %q, want resolved", got)
	}

	// Second incident: breach again, fire again after the duration.
	clock.Advance(1 * time.Second)
	eng.updateState(rule, agent.Id, true, 91, "critical", 10*time.Second, false)
	clock.Advance(11 * time.Second)
	eng.updateState(rule, agent.Id, true, 91, "critical", 10*time.Second, false)

	alerts = countAlerts(t, app, rule.Id, agent.Id)
	if len(alerts) != 2 {
		t.Fatalf("after re-breach, len(alerts) = %d, want 2 (a new incident record)", len(alerts))
	}

	firing := 0
	for _, a := range alerts {
		if a.GetString("status") == "firing" {
			firing++
		}
	}
	if firing != 1 {
		t.Fatalf("firing alert count = %d, want exactly 1", firing)
	}
}

// TestEngine_SeedStateRestoresFiringAlertsAcrossRestart asserts that a fresh
// Engine rehydrates its in-memory state from alerts already marked
// "firing" in the database, so a hub restart neither re-fires a duplicate
// alert for a still-breaching incident nor loses track of it.
func TestEngine_SeedStateRestoresFiringAlertsAcrossRestart(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-restart", "online")
	rule := createRule(t, app, ruleOpts{
		name: "cpu-restart", metricType: "cpu", condition: "gt", threshold: 80,
		durationSeconds: 10, severity: "critical", enabled: true,
	})

	// Simulate an alert that was already firing before the "restart".
	alertsCol, err := app.FindCollectionByNameOrId("alerts")
	if err != nil {
		t.Fatalf("find alerts collection: %v", err)
	}
	firedAt := time.Now().Add(-2 * time.Minute).UTC()
	existing := core.NewRecord(alertsCol)
	existing.Set("rule_id", rule.Id)
	existing.Set("agent_id", agent.Id)
	existing.Set("status", "firing")
	existing.Set("value", 95)
	existing.Set("message", "[critical] cpu on host-restart: cpu > 80.0 (current: 95.0)")
	existing.Set("fired_at", firedAt.Format("2006-01-02 15:04:05.000Z"))
	if err := app.Save(existing); err != nil {
		t.Fatalf("save pre-existing firing alert: %v", err)
	}

	// A fresh Engine instance, as if the hub had just restarted.
	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now
	eng.seedState()

	key := ruleAgentKey{RuleID: rule.Id, AgentID: agent.Id}
	state, ok := eng.states[key]
	if !ok {
		t.Fatal("seedState() did not restore state for the pre-existing firing alert")
	}
	if !state.Fired {
		t.Error("restored state.Fired = false, want true")
	}
	if state.ActiveAlertID != existing.Id {
		t.Errorf("restored state.ActiveAlertID = %q, want %q", state.ActiveAlertID, existing.Id)
	}
	if state.State != StateCritical {
		t.Errorf("restored state.State = %v, want StateCritical (rule severity is critical)", state.State)
	}

	var notifications int
	eng.SetNotifyFunc(func(app core.App, alert, r *core.Record) { notifications++ })

	// Still breaching: must update the restored record, not insert a new one.
	eng.updateState(rule, agent.Id, true, 96, "critical", 10*time.Second, false)
	if alerts := countAlerts(t, app, rule.Id, agent.Id); len(alerts) != 1 {
		t.Fatalf("after restart + continued breach, len(alerts) = %d, want 1 (no duplicate)", len(alerts))
	}
	if notifications != 0 {
		t.Fatalf("notifications = %d, want 0 (still within cooldown of the restored LastNotifiedAt)", notifications)
	}

	// Breach stops: the restored record resolves normally.
	eng.updateState(rule, agent.Id, false, 40, "critical", 10*time.Second, false)
	resolved, err := app.FindRecordById("alerts", existing.Id)
	if err != nil {
		t.Fatalf("find alert after resolution: %v", err)
	}
	if got := resolved.GetString("status"); got != "resolved" {
		t.Fatalf("alert status after resolution = %q, want resolved", got)
	}
	if resolved.GetString("resolved_at") == "" {
		t.Error("resolved alert has no resolved_at timestamp")
	}
}

func TestEvaluateRule_ScopedToOneAgent(t *testing.T) {
	app := newTestApp(t)
	agent1 := createAgent(t, app, "scoped-1", "online")
	agent2 := createAgent(t, app, "scoped-2", "online")
	createMetric(t, app, agent1.Id, "cpu", map[string]any{"total_percent": 95.0})
	createMetric(t, app, agent2.Id, "cpu", map[string]any{"total_percent": 95.0})

	rule := createRule(t, app, ruleOpts{
		name: "scoped-rule", metricType: "cpu", condition: "gt", threshold: 80,
		durationSeconds: 1, severity: "critical", enabled: true, agentID: agent1.Id,
	})

	eng := NewEngine(app)
	eng.evaluateRule(rule, nil)

	if _, ok := eng.states[ruleAgentKey{RuleID: rule.Id, AgentID: agent1.Id}]; !ok {
		t.Error("scoped rule did not evaluate its target agent")
	}
	if _, ok := eng.states[ruleAgentKey{RuleID: rule.Id, AgentID: agent2.Id}]; ok {
		t.Error("scoped rule evaluated an agent it is not scoped to")
	}
	if len(eng.states) != 1 {
		t.Errorf("len(eng.states) = %d, want 1 for a single-agent-scoped rule", len(eng.states))
	}
}

func TestEvaluateRule_GlobalRuleAppliesToOnlineAgentsOnly(t *testing.T) {
	app := newTestApp(t)
	online := createAgent(t, app, "global-online", "online")
	offline := createAgent(t, app, "global-offline", "offline")
	createMetric(t, app, online.Id, "cpu", map[string]any{"total_percent": 95.0})
	createMetric(t, app, offline.Id, "cpu", map[string]any{"total_percent": 95.0})

	rule := createRule(t, app, ruleOpts{
		name: "global-rule", metricType: "cpu", condition: "gt", threshold: 80,
		durationSeconds: 1, severity: "critical", enabled: true, // agentID left empty: global
	})

	eng := NewEngine(app)
	eng.evaluateRule(rule, nil)

	if _, ok := eng.states[ruleAgentKey{RuleID: rule.Id, AgentID: online.Id}]; !ok {
		t.Error("global rule did not evaluate the online agent")
	}
	if _, ok := eng.states[ruleAgentKey{RuleID: rule.Id, AgentID: offline.Id}]; ok {
		t.Error("global rule evaluated an offline agent, want online agents only")
	}
}

func TestEvaluate_DisabledRuleIsIgnored(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "disabled-target", "online")
	createMetric(t, app, agent.Id, "cpu", map[string]any{"total_percent": 99.0})

	rule := createRule(t, app, ruleOpts{
		name: "disabled-rule", metricType: "cpu", condition: "gt", threshold: 10,
		durationSeconds: 1, severity: "critical", enabled: false,
	})

	eng := NewEngine(app)
	eng.evaluate()

	if _, ok := eng.states[ruleAgentKey{RuleID: rule.Id, AgentID: agent.Id}]; ok {
		t.Error("evaluate() processed a disabled rule, want it skipped entirely")
	}
	if len(eng.states) != 0 {
		t.Errorf("len(eng.states) = %d, want 0 when the only rule is disabled", len(eng.states))
	}
}

func TestEvaluate_EnabledRuleFiresAcrossOnlineAgents(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "enabled-target", "online")
	createMetric(t, app, agent.Id, "cpu", map[string]any{"total_percent": 99.0})

	rule := createRule(t, app, ruleOpts{
		name: "enabled-rule", metricType: "cpu", condition: "gt", threshold: 10,
		durationSeconds: 1, severity: "critical", enabled: true, agentID: agent.Id,
	})

	eng := NewEngine(app)
	eng.evaluate()

	if _, ok := eng.states[ruleAgentKey{RuleID: rule.Id, AgentID: agent.Id}]; !ok {
		t.Error("evaluate() did not process the enabled rule")
	}
}

// ─── A. Targeting (matchesTargets) ──────────────────────────────────────────

func TestMatchesTargets(t *testing.T) {
	app := newTestApp(t)

	untagged := createAgent(t, app, "matches-untagged", "online")
	prodDB := createAgentWithTags(t, app, "matches-prod-db", "online", []string{"prod", "db"})
	stagingWeb := createAgentWithTags(t, app, "matches-staging-web", "online", []string{"staging", "web"})

	tests := []struct {
		name string
		opts ruleOpts
		want map[string]bool // agent hostname -> want match
	}{
		{
			name: "agent_id set matches only that agent",
			opts: ruleOpts{agentID: prodDB.Id},
			want: map[string]bool{"matches-untagged": false, "matches-prod-db": true, "matches-staging-web": false},
		},
		{
			name: "target_tags matches agents with any overlapping tag",
			opts: ruleOpts{targetTags: []string{"prod"}},
			want: map[string]bool{"matches-untagged": false, "matches-prod-db": true, "matches-staging-web": false},
		},
		{
			name: "target_tags with multiple tags matches on any one of them",
			opts: ruleOpts{targetTags: []string{"web", "db"}},
			want: map[string]bool{"matches-untagged": false, "matches-prod-db": true, "matches-staging-web": true},
		},
		{
			name: "no agent_id and no target_tags matches every agent",
			opts: ruleOpts{},
			want: map[string]bool{"matches-untagged": true, "matches-prod-db": true, "matches-staging-web": true},
		},
	}

	agents := map[string]*core.Record{
		"matches-untagged":    untagged,
		"matches-prod-db":     prodDB,
		"matches-staging-web": stagingWeb,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.name = tt.name
			tt.opts.metricType = "cpu"
			tt.opts.condition = "gt"
			tt.opts.threshold = 80
			tt.opts.durationSeconds = 1
			tt.opts.severity = "critical"
			tt.opts.enabled = true
			rule := createRule(t, app, tt.opts)

			for hostname, want := range tt.want {
				if got := matchesTargets(agents[hostname], rule); got != want {
					t.Errorf("matchesTargets(%s, %s) = %v, want %v", hostname, tt.name, got, want)
				}
			}
		})
	}
}

// ─── B. Silences (isSilenced / silenceMatchesAgent) ─────────────────────────

func TestIsSilenced(t *testing.T) {
	app := newTestApp(t)
	now := time.Now()

	targeted := createAgentWithTags(t, app, "silence-targeted", "online", []string{"prod"})
	other := createAgentWithTags(t, app, "silence-other", "online", []string{"staging"})

	activeGlobal := createSilence(t, app, "", nil, now.Add(-time.Hour), now.Add(time.Hour))
	activeByAgent := createSilence(t, app, targeted.Id, nil, now.Add(-time.Hour), now.Add(time.Hour))
	activeByTag := createSilence(t, app, "", []string{"prod"}, now.Add(-time.Hour), now.Add(time.Hour))
	expired := createSilence(t, app, "", nil, now.Add(-2*time.Hour), now.Add(-time.Hour))
	future := createSilence(t, app, "", nil, now.Add(time.Hour), now.Add(2*time.Hour))

	tests := []struct {
		name     string
		agent    *core.Record
		silences []*core.Record
		want     bool
	}{
		{"no silences at all", targeted, nil, false},
		{"active global silence covers every agent", other, []*core.Record{activeGlobal}, true},
		{"active agent-scoped silence covers that agent", targeted, []*core.Record{activeByAgent}, true},
		{"active agent-scoped silence does not cover a different agent", other, []*core.Record{activeByAgent}, false},
		{"active tag silence covers a matching agent", targeted, []*core.Record{activeByTag}, true},
		{"active tag silence does not cover a non-matching agent", other, []*core.Record{activeByTag}, false},
		{"expired silence does not cover anyone", other, []*core.Record{expired}, false},
		{"future silence does not cover anyone yet", other, []*core.Record{future}, false},
		{"one matching silence among several is enough", other, []*core.Record{expired, future, activeGlobal}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSilenced(tt.agent, tt.silences, now); got != tt.want {
				t.Errorf("isSilenced() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSilenceMatchesAgent_CheckOnlyScopeDoesNotCoverAgents pins the fix
// that made check_ids join the "is this silence global" test: before
// check_ids existed, a silence with no agent_id and no tags always meant
// "global, covers every agent." Now that a silence can be scoped to
// specific checks only (check_ids set, agent_id/tags empty), that same
// silence must NOT also silence every agent as an accidental side effect.
func TestSilenceMatchesAgent_CheckOnlyScopeDoesNotCoverAgents(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "silence-check-scoped-agent", "online")
	check := createCheck(t, app, "some-check", "tcp", "10.0.0.1:1")

	checkScoped := createSilenceWithChecks(t, app, "", nil, []string{check.Id}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))

	if SilenceMatchesAgent(agent, checkScoped) {
		t.Error("a check_ids-scoped silence must not match an unrelated agent")
	}
}

func TestIsCheckSilenced(t *testing.T) {
	app := newTestApp(t)
	now := time.Now()

	targeted := createCheck(t, app, "silence-targeted-check", "http", "https://example.com")
	targeted.Set("tags", []string{"prod"})
	if err := app.Save(targeted); err != nil {
		t.Fatalf("save check tags: %v", err)
	}
	other := createCheck(t, app, "silence-other-check", "http", "https://other.example.com")
	other.Set("tags", []string{"staging"})
	if err := app.Save(other); err != nil {
		t.Fatalf("save check tags: %v", err)
	}

	unrelatedAgent := createAgent(t, app, "silence-unrelated-agent", "online")

	activeGlobal := createSilence(t, app, "", nil, now.Add(-time.Hour), now.Add(time.Hour))
	activeByCheckID := createSilenceWithChecks(t, app, "", nil, []string{targeted.Id}, now.Add(-time.Hour), now.Add(time.Hour))
	activeByTag := createSilence(t, app, "", []string{"prod"}, now.Add(-time.Hour), now.Add(time.Hour))
	activeByAgentOnly := createSilence(t, app, unrelatedAgent.Id, nil, now.Add(-time.Hour), now.Add(time.Hour))
	expired := createSilence(t, app, "", nil, now.Add(-2*time.Hour), now.Add(-time.Hour))
	future := createSilence(t, app, "", nil, now.Add(time.Hour), now.Add(2*time.Hour))

	tests := []struct {
		name     string
		check    *core.Record
		silences []*core.Record
		want     bool
	}{
		{"no silences at all", targeted, nil, false},
		{"active global silence covers every check", other, []*core.Record{activeGlobal}, true},
		{"active check_ids silence covers the named check", targeted, []*core.Record{activeByCheckID}, true},
		{"active check_ids silence does not cover a different check", other, []*core.Record{activeByCheckID}, false},
		{"active tag silence covers a matching check", targeted, []*core.Record{activeByTag}, true},
		{"active tag silence does not cover a non-matching check", other, []*core.Record{activeByTag}, false},
		{"an agent-only silence never covers a check", targeted, []*core.Record{activeByAgentOnly}, false},
		{"expired silence does not cover anyone", other, []*core.Record{expired}, false},
		{"future silence does not cover anyone yet", other, []*core.Record{future}, false},
		{"one matching silence among several is enough", other, []*core.Record{expired, future, activeGlobal}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCheckSilenced(tt.check, tt.silences, now); got != tt.want {
				t.Errorf("isCheckSilenced() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvaluateCheckRule_SilencedCheckFiresWithoutNotifying(t *testing.T) {
	app := newTestApp(t)
	check := createCheck(t, app, "silenced-check-eval", "tcp", "10.0.0.5:1")
	createCheckResult(t, app, check.Id, "down", time.Time{}, time.Now())

	rule := createRule(t, app, ruleOpts{
		name: "check-down-silenced", metricType: "check_down", condition: "gt", threshold: 1,
		severity: "critical", durationSeconds: 10, enabled: true,
	})

	now := time.Now()
	silence := createSilenceWithChecks(t, app, "", nil, []string{check.Id}, now.Add(-time.Hour), now.Add(time.Hour))

	eng := NewEngine(app)
	clock := &fakeClock{now: now}
	eng.clock = clock.Now

	var notifications int
	eng.SetNotifyFunc(func(app core.App, alert, rule *core.Record) { notifications++ })

	eng.evaluateCheckRule(rule, []*core.Record{silence})
	clock.Advance(11 * time.Second)
	eng.evaluateCheckRule(rule, []*core.Record{silence})

	alerts := countCheckAlerts(t, app, rule.Id, check.Id)
	if len(alerts) != 1 {
		t.Fatalf("len(alerts) = %d, want 1 (fires even while silenced)", len(alerts))
	}
	if !alerts[0].GetBool("silenced") {
		t.Error("alert.silenced = false, want true while an active check silence covers it")
	}
	if notifications != 0 {
		t.Errorf("notifications = %d, want 0 while the check is silenced", notifications)
	}
}

// ─── C. New rule types ──────────────────────────────────────────────────────

func TestEvalProcessDown(t *testing.T) {
	app := newTestApp(t)
	eng := NewEngine(app)

	tests := []struct {
		name          string
		target        string
		processes     []map[string]any
		withSnapshot  bool
		wantBreaching bool
		wantValue     float64
		wantOK        bool
	}{
		{
			name:         "no snapshot yet skips this cycle",
			target:       "nginx",
			withSnapshot: false,
			wantOK:       false,
		},
		{
			name:          "target present by name is not breaching",
			target:        "nginx",
			processes:     []map[string]any{{"pid": 1, "name": "nginx", "cmdline": "/usr/sbin/nginx"}},
			withSnapshot:  true,
			wantBreaching: false,
			wantValue:     1,
			wantOK:        true,
		},
		{
			name:          "target present by cmdline substring is not breaching",
			target:        "bancacore-api.jar",
			processes:     []map[string]any{{"pid": 2, "name": "java", "cmdline": "/usr/bin/java -jar bancacore-api.jar"}},
			withSnapshot:  true,
			wantBreaching: false,
			wantValue:     1,
			wantOK:        true,
		},
		{
			name:          "case-insensitive match",
			target:        "NGINX",
			processes:     []map[string]any{{"pid": 1, "name": "nginx", "cmdline": "/usr/sbin/nginx"}},
			withSnapshot:  true,
			wantBreaching: false,
			wantValue:     1,
			wantOK:        true,
		},
		{
			name:          "target absent is breaching",
			target:        "nginx",
			processes:     []map[string]any{{"pid": 3, "name": "sshd", "cmdline": "/usr/sbin/sshd -D"}},
			withSnapshot:  true,
			wantBreaching: true,
			wantValue:     0,
			wantOK:        true,
		},
		{
			name:          "empty process list is breaching",
			target:        "nginx",
			processes:     []map[string]any{},
			withSnapshot:  true,
			wantBreaching: true,
			wantValue:     0,
			wantOK:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := createAgent(t, app, "procdown-"+tt.name, "online")
			if tt.withSnapshot {
				createMetric(t, app, agent.Id, "processes", map[string]any{
					"processes":   tt.processes,
					"total_count": len(tt.processes),
				})
			}

			value, breaching, ok := eng.evalProcessDown(agent.Id, tt.target)
			if ok != tt.wantOK {
				t.Fatalf("evalProcessDown() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if breaching != tt.wantBreaching {
				t.Errorf("evalProcessDown() breaching = %v, want %v", breaching, tt.wantBreaching)
			}
			if value != tt.wantValue {
				t.Errorf("evalProcessDown() value = %v, want %v", value, tt.wantValue)
			}
		})
	}
}

func TestEvalServiceFailed(t *testing.T) {
	app := newTestApp(t)
	eng := NewEngine(app)

	tests := []struct {
		name          string
		target        string
		services      []map[string]any
		withSnapshot  bool
		wantBreaching bool
		wantValue     float64
		wantOK        bool
	}{
		{
			name:         "no snapshot yet skips this cycle",
			target:       "nginx",
			withSnapshot: false,
			wantOK:       false,
		},
		{
			name:          "target running is not breaching",
			target:        "nginx",
			services:      []map[string]any{{"name": "nginx", "load": "loaded", "active": "active", "sub": "running"}},
			withSnapshot:  true,
			wantBreaching: false,
			wantValue:     1,
			wantOK:        true,
		},
		{
			name:          "target failed is breaching",
			target:        "nginx",
			services:      []map[string]any{{"name": "nginx", "load": "loaded", "active": "failed", "sub": "failed"}},
			withSnapshot:  true,
			wantBreaching: true,
			wantValue:     0,
			wantOK:        true,
		},
		{
			name:          "target dead sub-state is breaching",
			target:        "nginx",
			services:      []map[string]any{{"name": "nginx", "load": "loaded", "active": "inactive", "sub": "dead"}},
			withSnapshot:  true,
			wantBreaching: true,
			wantValue:     0,
			wantOK:        true,
		},
		{
			name:          "target not listed at all is breaching",
			target:        "nginx",
			services:      []map[string]any{{"name": "sshd", "load": "loaded", "active": "active", "sub": "running"}},
			withSnapshot:  true,
			wantBreaching: true,
			wantValue:     0,
			wantOK:        true,
		},
		{
			name:          "exact case-insensitive name match required",
			target:        "NGINX",
			services:      []map[string]any{{"name": "nginx", "load": "loaded", "active": "active", "sub": "running"}},
			withSnapshot:  true,
			wantBreaching: false,
			wantValue:     1,
			wantOK:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := createAgent(t, app, "svcfail-"+tt.name, "online")
			if tt.withSnapshot {
				createMetric(t, app, agent.Id, "services", map[string]any{
					"services": tt.services,
					"total":    len(tt.services),
				})
			}

			value, breaching, ok := eng.evalServiceFailed(agent.Id, tt.target)
			if ok != tt.wantOK {
				t.Fatalf("evalServiceFailed() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if breaching != tt.wantBreaching {
				t.Errorf("evalServiceFailed() breaching = %v, want %v", breaching, tt.wantBreaching)
			}
			if value != tt.wantValue {
				t.Errorf("evalServiceFailed() value = %v, want %v", value, tt.wantValue)
			}
		})
	}
}

func TestEvalCveCount(t *testing.T) {
	app := newTestApp(t)
	eng := NewEngine(app)

	tests := []struct {
		name          string
		target        string
		condition     string
		threshold     float64
		withSnapshot  bool
		available     bool
		totals        map[string]any
		wantOK        bool
		wantValue     float64
		wantBreaching bool
	}{
		{
			name:         "no snapshot yet skips this cycle",
			condition:    "gt",
			threshold:    0,
			withSnapshot: false,
			wantOK:       false,
		},
		{
			name:         "scanner unavailable skips this cycle",
			condition:    "gt",
			threshold:    0,
			withSnapshot: true,
			available:    false,
			totals:       map[string]any{"critical": 5.0},
			wantOK:       false,
		},
		{
			name:          "default target sums critical+high",
			condition:     "gt",
			threshold:     3,
			withSnapshot:  true,
			available:     true,
			totals:        map[string]any{"critical": 2.0, "high": 3.0, "medium": 1.0},
			wantOK:        true,
			wantValue:     5,
			wantBreaching: true,
		},
		{
			name:          "critical-only target ignores high",
			target:        "critical",
			condition:     "gt",
			threshold:     3,
			withSnapshot:  true,
			available:     true,
			totals:        map[string]any{"critical": 2.0, "high": 10.0},
			wantOK:        true,
			wantValue:     2,
			wantBreaching: false,
		},
		{
			name:          "critical-only target case-insensitive",
			target:        "CRITICAL",
			condition:     "gte",
			threshold:     2,
			withSnapshot:  true,
			available:     true,
			totals:        map[string]any{"critical": 2.0, "high": 10.0},
			wantOK:        true,
			wantValue:     2,
			wantBreaching: false, // "gte" is not a recognized condition -> checkCondition() defaults to false
		},
		{
			name:          "not breaching when below threshold",
			condition:     "gt",
			threshold:     100,
			withSnapshot:  true,
			available:     true,
			totals:        map[string]any{"critical": 1.0, "high": 1.0},
			wantOK:        true,
			wantValue:     2,
			wantBreaching: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := createAgent(t, app, "cvecount-"+tt.name, "online")
			if tt.withSnapshot {
				data := map[string]any{"available": tt.available}
				if tt.totals != nil {
					data["totals"] = tt.totals
				}
				createMetric(t, app, agent.Id, "cve_scan", data)
			}

			value, breaching, ok := eng.evalCveCount(agent.Id, tt.target, tt.condition, tt.threshold)
			if ok != tt.wantOK {
				t.Fatalf("evalCveCount() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if value != tt.wantValue {
				t.Errorf("evalCveCount() value = %v, want %v", value, tt.wantValue)
			}
			if breaching != tt.wantBreaching {
				t.Errorf("evalCveCount() breaching = %v, want %v", breaching, tt.wantBreaching)
			}
		})
	}
}

func TestEvaluateRule_CveCountFiresAlert(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "cve-fire", "online")
	createMetric(t, app, agent.Id, "cve_scan", map[string]any{
		"available": true,
		"totals":    map[string]any{"critical": 4.0, "high": 2.0},
	})

	rule := createRule(t, app, ruleOpts{
		name: "cve-rule", metricType: "cve_count", condition: "gt", threshold: 3,
		durationSeconds: 1, severity: "critical", enabled: true, agentID: agent.Id,
	})

	eng := NewEngine(app)
	eng.evaluateRule(rule, nil)

	state, ok := eng.states[ruleAgentKey{RuleID: rule.Id, AgentID: agent.Id}]
	if !ok {
		t.Fatal("cve_count rule did not evaluate its target agent")
	}
	// A first breach only starts the duration timer (see updateState's doc
	// comment) — it does not fire immediately even though 6 > 3.
	if state.State != StateWarning {
		t.Errorf("state = %v, want StateWarning (first breach starts the duration timer)", state.State)
	}
	if state.FirstBreachAt.IsZero() {
		t.Error("FirstBreachAt should be set once a cve_count rule starts breaching")
	}
}

func TestEvalAgentOffline(t *testing.T) {
	app := newTestApp(t)
	eng := NewEngine(app)
	now := time.Now()

	tests := []struct {
		name          string
		status        string
		lastSeen      time.Time
		wantBreaching bool
		wantValue     float64
	}{
		{"online and recently seen", "online", now, false, 0},
		{"status offline overrides recent last_seen", "offline", now, true, 1},
		{"online but last_seen far in the past", "online", now.Add(-2 * time.Hour), true, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := createAgent(t, app, "offline-"+tt.name, tt.status)
			agent.Set("last_seen", tt.lastSeen.UTC().Format("2006-01-02 15:04:05.000Z"))
			if err := app.Save(agent); err != nil {
				t.Fatalf("save agent last_seen: %v", err)
			}

			value, breaching, ok := eng.evalAgentOffline(agent, now)
			if !ok {
				t.Fatal("evalAgentOffline() ok = false, want true")
			}
			if breaching != tt.wantBreaching {
				t.Errorf("evalAgentOffline() breaching = %v, want %v", breaching, tt.wantBreaching)
			}
			if value != tt.wantValue {
				t.Errorf("evalAgentOffline() value = %v, want %v", value, tt.wantValue)
			}
		})
	}
}

func TestEvaluateRule_AgentOfflineEvaluatesOfflineAgentsToo(t *testing.T) {
	app := newTestApp(t)
	online := createAgent(t, app, "ao-online", "online")
	offline := createAgent(t, app, "ao-offline", "offline")
	offline.Set("last_seen", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))
	if err := app.Save(offline); err != nil {
		t.Fatalf("save offline agent: %v", err)
	}

	rule := createRule(t, app, ruleOpts{
		// condition/threshold are required fields but are ignored by the
		// agent_offline evaluation path; any placeholder value works.
		name: "agent-offline-rule", metricType: "agent_offline", condition: "gt", threshold: 1,
		durationSeconds: 1, severity: "critical", enabled: true,
	})

	eng := NewEngine(app)
	eng.evaluateRule(rule, nil)

	if _, ok := eng.states[ruleAgentKey{RuleID: rule.Id, AgentID: offline.Id}]; !ok {
		t.Error("agent_offline rule did not evaluate the offline agent")
	}
	if _, ok := eng.states[ruleAgentKey{RuleID: rule.Id, AgentID: online.Id}]; !ok {
		t.Error("agent_offline rule did not evaluate the online agent")
	}
}

// ─── B. Silence suppresses notification, then notifies once it ends ────────

func TestUpdateState_SilencedSuppressesNotificationThenNotifiesAfter(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-silenced", "online")
	rule := createRule(t, app, ruleOpts{
		name: "cpu-silenced", metricType: "cpu", condition: "gt", threshold: 80,
		durationSeconds: 10, severity: "critical", enabled: true,
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now

	var notifications int
	eng.SetNotifyFunc(func(app core.App, alert, rule *core.Record) { notifications++ })

	// Breach persists past duration while silenced: fires (creates the
	// record) but must not notify.
	eng.updateState(rule, agent.Id, true, 95, "critical", 10*time.Second, true)
	clock.Advance(11 * time.Second)
	eng.updateState(rule, agent.Id, true, 95, "critical", 10*time.Second, true)

	alerts := countAlerts(t, app, rule.Id, agent.Id)
	if len(alerts) != 1 {
		t.Fatalf("len(alerts) = %d, want 1 (fires even while silenced)", len(alerts))
	}
	if !alerts[0].GetBool("silenced") {
		t.Error("alert.silenced = false, want true while an active silence covers it")
	}
	if notifications != 0 {
		t.Fatalf("notifications = %d, want 0 while silenced", notifications)
	}

	// Still breaching, still silenced on a later cycle: still no notification.
	clock.Advance(1 * time.Second)
	eng.updateState(rule, agent.Id, true, 96, "critical", 10*time.Second, true)
	if notifications != 0 {
		t.Fatalf("notifications = %d, want 0 while still silenced", notifications)
	}

	// Silence ends, still breaching: notifies immediately (it was never
	// notified before) and clears silenced.
	eng.updateState(rule, agent.Id, true, 97, "critical", 10*time.Second, false)
	if notifications != 1 {
		t.Fatalf("notifications = %d, want 1 once the silence ends", notifications)
	}

	fresh, err := app.FindRecordById("alerts", alerts[0].Id)
	if err != nil {
		t.Fatalf("find alert after silence ends: %v", err)
	}
	if fresh.GetBool("silenced") {
		t.Error("alert.silenced = true, want false once the silence has ended")
	}
}

// ─── D. Acknowledgement blocks re-notify and escalation ────────────────────

func TestUpdateState_AckedDoesNotRenotifyOrEscalate(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-acked", "online")
	rule := createRule(t, app, ruleOpts{
		name: "cpu-acked", metricType: "cpu", condition: "gt", threshold: 80,
		durationSeconds: 10, severity: "critical", enabled: true, escalationAfter: 5,
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now
	eng.cooldownPeriod = 1 * time.Second

	var notifications, escalations int
	eng.SetNotifyFunc(func(app core.App, alert, rule *core.Record) { notifications++ })
	eng.SetEscalateFunc(func(app core.App, alert, rule *core.Record) { escalations++ })

	// Fire the alert.
	eng.updateState(rule, agent.Id, true, 95, "critical", 10*time.Second, false)
	clock.Advance(11 * time.Second)
	eng.updateState(rule, agent.Id, true, 95, "critical", 10*time.Second, false)

	alerts := countAlerts(t, app, rule.Id, agent.Id)
	if len(alerts) != 1 {
		t.Fatalf("len(alerts) = %d, want 1", len(alerts))
	}
	if notifications != 1 {
		t.Fatalf("notifications = %d, want 1 after firing", notifications)
	}

	// Acknowledge it.
	alert := alerts[0]
	alert.Set("acknowledged_at", clock.now.UTC().Format("2006-01-02 15:04:05.000Z"))
	alert.Set("acknowledged_by", "operator@example.com")
	if err := app.Save(alert); err != nil {
		t.Fatalf("save acknowledged alert: %v", err)
	}

	// Still breaching, well past both cooldown and escalation_after: must
	// neither re-notify nor escalate now that it is acknowledged.
	clock.Advance(1 * time.Hour)
	eng.updateState(rule, agent.Id, true, 96, "critical", 10*time.Second, false)

	if notifications != 1 {
		t.Fatalf("notifications = %d, want still 1 (acknowledged alerts are not re-notified)", notifications)
	}
	if escalations != 0 {
		t.Fatalf("escalations = %d, want 0 (acknowledged alerts are not escalated)", escalations)
	}
}

// ─── E. Escalation fires exactly once per firing episode ───────────────────

func TestUpdateState_EscalatesOnceAfterThreshold(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-escalate", "online")
	rule := createRule(t, app, ruleOpts{
		name: "cpu-escalate", metricType: "cpu", condition: "gt", threshold: 80,
		durationSeconds: 10, severity: "critical", enabled: true, escalationAfter: 30,
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now
	eng.cooldownPeriod = 24 * time.Hour // keep re-notify out of the way

	var escalations int
	eng.SetEscalateFunc(func(app core.App, alert, rule *core.Record) { escalations++ })

	// Fire the alert.
	eng.updateState(rule, agent.Id, true, 95, "critical", 10*time.Second, false)
	clock.Advance(11 * time.Second)
	eng.updateState(rule, agent.Id, true, 95, "critical", 10*time.Second, false)

	if escalations != 0 {
		t.Fatalf("escalations = %d, want 0 before escalation_after has elapsed", escalations)
	}

	// Past escalation_after (30s) since firing: escalates exactly once.
	clock.Advance(31 * time.Second)
	eng.updateState(rule, agent.Id, true, 96, "critical", 10*time.Second, false)
	if escalations != 1 {
		t.Fatalf("escalations = %d, want 1 once escalation_after has elapsed", escalations)
	}

	alerts := countAlerts(t, app, rule.Id, agent.Id)
	if len(alerts) != 1 {
		t.Fatalf("len(alerts) = %d, want 1", len(alerts))
	}
	if alerts[0].GetString("escalated_at") == "" {
		t.Error("alert.escalated_at is empty, want it set after escalation")
	}

	// Further cycles, still breaching: must not escalate a second time for
	// the same firing episode.
	clock.Advance(1 * time.Minute)
	eng.updateState(rule, agent.Id, true, 97, "critical", 10*time.Second, false)
	if escalations != 1 {
		t.Fatalf("escalations = %d, want still 1 (escalates at most once per firing episode)", escalations)
	}
}

func TestMaybeEscalate_DisabledWhenEscalationAfterIsZero(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-no-escalate", "online")
	rule := createRule(t, app, ruleOpts{
		name: "cpu-no-escalate", metricType: "cpu", condition: "gt", threshold: 80,
		durationSeconds: 10, severity: "critical", enabled: true, // escalationAfter left at 0
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now
	eng.cooldownPeriod = 24 * time.Hour

	var escalations int
	eng.SetEscalateFunc(func(app core.App, alert, rule *core.Record) { escalations++ })

	eng.updateState(rule, agent.Id, true, 95, "critical", 10*time.Second, false)
	clock.Advance(11 * time.Second)
	eng.updateState(rule, agent.Id, true, 95, "critical", 10*time.Second, false)
	clock.Advance(1 * time.Hour)
	eng.updateState(rule, agent.Id, true, 96, "critical", 10*time.Second, false)

	if escalations != 0 {
		t.Fatalf("escalations = %d, want 0 when escalation_after is 0 (disabled)", escalations)
	}
}

// createLog inserts a "logs" record at ts for agentID with the given
// message, used by the log_match evaluator tests below.
func createLog(t *testing.T, app core.App, agentID, message string, ts time.Time) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("logs")
	if err != nil {
		t.Fatalf("find logs collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("agent_id", agentID)
	rec.Set("ts", ts.UTC().Format("2006-01-02 15:04:05.000Z"))
	rec.Set("source", "journald")
	rec.Set("level", "error")
	rec.Set("message", message)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save log record: %v", err)
	}
	return rec
}

func TestEvalLogMatch(t *testing.T) {
	app := newTestApp(t)
	eng := NewEngine(app)
	now := time.Now().UTC()

	tests := []struct {
		name          string
		target        string
		threshold     float64
		messages      []string
		wantOK        bool
		wantValue     float64
		wantBreaching bool
	}{
		{
			name:      "empty target skips this cycle",
			target:    "",
			threshold: 1,
			wantOK:    false,
		},
		{
			name:          "substring match below threshold does not breach",
			target:        "error",
			threshold:     3,
			messages:      []string{"an error occurred", "another ERROR here"},
			wantOK:        true,
			wantValue:     2,
			wantBreaching: false,
		},
		{
			name:          "substring match at threshold breaches",
			target:        "error",
			threshold:     2,
			messages:      []string{"an error occurred", "another ERROR here"},
			wantOK:        true,
			wantValue:     2,
			wantBreaching: true,
		},
		{
			name:          "substring is case-insensitive",
			target:        "OutOfMemory",
			threshold:     1,
			messages:      []string{"java.lang.outofmemoryerror: heap space"},
			wantOK:        true,
			wantValue:     1,
			wantBreaching: true,
		},
		{
			name:          "no matches is not breaching",
			target:        "disk full",
			threshold:     1,
			messages:      []string{"server started", "request handled"},
			wantOK:        true,
			wantValue:     0,
			wantBreaching: false,
		},
		{
			name:          "regex pattern matches",
			target:        "/error code [45]\\d\\d/",
			threshold:     1,
			messages:      []string{"request failed with error code 500", "request failed with error code 200"},
			wantOK:        true,
			wantValue:     1,
			wantBreaching: true,
		},
		{
			name:      "malformed regex never breaches",
			target:    "/[/",
			threshold: 0,
			messages:  []string{"anything"},
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agent := createAgent(t, app, "logmatch-"+tt.name, "online")
			for _, msg := range tt.messages {
				createLog(t, app, agent.Id, msg, now.Add(-10*time.Second))
			}

			value, breaching, ok := eng.evalLogMatch(agent.Id, tt.target, tt.threshold, time.Minute)
			if ok != tt.wantOK {
				t.Fatalf("evalLogMatch() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if value != tt.wantValue {
				t.Errorf("evalLogMatch() value = %v, want %v", value, tt.wantValue)
			}
			if breaching != tt.wantBreaching {
				t.Errorf("evalLogMatch() breaching = %v, want %v", breaching, tt.wantBreaching)
			}
		})
	}
}

func TestEvalLogMatch_IgnoresEntriesOutsideWindow(t *testing.T) {
	app := newTestApp(t)
	eng := NewEngine(app)
	agent := createAgent(t, app, "logmatch-window", "online")
	now := time.Now().UTC()

	createLog(t, app, agent.Id, "error inside window", now.Add(-30*time.Second))
	createLog(t, app, agent.Id, "error outside window", now.Add(-10*time.Minute))

	value, breaching, ok := eng.evalLogMatch(agent.Id, "error", 1, time.Minute)
	if !ok {
		t.Fatal("evalLogMatch() ok = false, want true")
	}
	if value != 1 {
		t.Errorf("evalLogMatch() value = %v, want 1 (only the in-window entry)", value)
	}
	if !breaching {
		t.Error("evalLogMatch() breaching = false, want true")
	}
}

func TestParseLogMatchRegex(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		wantIsRegex bool
		wantNil     bool
	}{
		{name: "plain substring", target: "connection refused", wantIsRegex: false},
		{name: "valid regex", target: "/error [0-9]+/", wantIsRegex: true, wantNil: false},
		{name: "malformed regex", target: "/[/", wantIsRegex: true, wantNil: true},
		{name: "single slash is not a regex", target: "/", wantIsRegex: false},
		{name: "empty string is not a regex", target: "", wantIsRegex: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern, isRegex := parseLogMatchRegex(tt.target)
			if isRegex != tt.wantIsRegex {
				t.Fatalf("parseLogMatchRegex(%q) isRegex = %v, want %v", tt.target, isRegex, tt.wantIsRegex)
			}
			if tt.wantNil && pattern != nil {
				t.Errorf("parseLogMatchRegex(%q) pattern = %v, want nil", tt.target, pattern)
			}
			if !tt.wantNil && tt.wantIsRegex && pattern == nil {
				t.Errorf("parseLogMatchRegex(%q) pattern = nil, want compiled pattern", tt.target)
			}
		})
	}
}

func TestEvaluateRule_LogMatchStartsBreachTrackingThenFires(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "logmatch-fire", "online")

	clock := &fakeClock{now: time.Now()}
	eng := NewEngine(app)
	eng.clock = clock.Now

	rule := createRule(t, app, ruleOpts{
		name:            "log match rule",
		metricType:      "log_match",
		condition:       "gt", // ignored by log_match
		threshold:       2,
		durationSeconds: 1,
		severity:        "critical",
		enabled:         true,
		agentID:         agent.Id,
		target:          "disk write error",
	})

	// Matching entries within the (1s) window as of the first evaluation.
	for i := 0; i < 3; i++ {
		createLog(t, app, agent.Id, "disk write error", clock.now)
	}

	eng.evaluateRule(rule, nil)

	state, ok := eng.states[ruleAgentKey{RuleID: rule.Id, AgentID: agent.Id}]
	if !ok {
		t.Fatal("log_match rule did not evaluate its target agent")
	}
	// A first breach only starts the duration timer (see updateState's doc
	// comment) — it does not fire immediately even though 3 >= 2.
	if state.State != StateWarning {
		t.Errorf("state = %v, want StateWarning (first breach starts the duration timer)", state.State)
	}

	// The condition keeps breaching (fresh matching entries land within the
	// window right up to the second evaluation) and the duration has now
	// elapsed, so this evaluation should fire.
	clock.Advance(2 * time.Second)
	createLog(t, app, agent.Id, "disk write error", clock.now)
	createLog(t, app, agent.Id, "disk write error", clock.now)
	eng.evaluateRule(rule, nil)

	alerts := countAlerts(t, app, rule.Id, agent.Id)
	if len(alerts) != 1 {
		t.Fatalf("got %d alerts, want 1", len(alerts))
	}
	if alerts[0].GetString("status") != "firing" {
		t.Errorf("alert status = %q, want %q", alerts[0].GetString("status"), "firing")
	}
}
