package alerts

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func createCheck(t *testing.T, app core.App, name, checkType, target string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("checks")
	if err != nil {
		t.Fatalf("find checks collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("type", checkType)
	rec.Set("target", target)
	rec.Set("enabled", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save check %s: %v", name, err)
	}
	return rec
}

// createCheckResult inserts a check_results row with an explicit
// checkedAt/status, mimicking what checks.Scheduler.RunOnce would persist
// (status here is already the debounced value, not a raw attempt outcome).
func createCheckResult(t *testing.T, app core.App, checkID, status string, tlsExpiresAt time.Time, checkedAt time.Time) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("check_results")
	if err != nil {
		t.Fatalf("find check_results collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("check_id", checkID)
	rec.Set("status", status)
	if !tlsExpiresAt.IsZero() {
		rec.Set("tls_expires_at", tlsExpiresAt.UTC().Format("2006-01-02 15:04:05.000Z"))
	}
	rec.Set("checked_at", checkedAt.UTC().Format("2006-01-02 15:04:05.000Z"))
	if err := app.Save(rec); err != nil {
		t.Fatalf("save check_result for %s: %v", checkID, err)
	}
	return rec
}

func countCheckAlerts(t *testing.T, app core.App, ruleID, checkID string) []*core.Record {
	t.Helper()
	recs, err := app.FindRecordsByFilter(
		"alerts",
		"rule_id = {:r} && check_id = {:c}",
		"-fired_at",
		0,
		0,
		map[string]any{"r": ruleID, "c": checkID},
	)
	if err != nil {
		t.Fatalf("find check alerts: %v", err)
	}
	return recs
}

func TestEvalCheckDown_NoResultsYetIsNotOK(t *testing.T) {
	app := newTestApp(t)
	check := createCheck(t, app, "no-results", "http", "https://example.com")

	eng := NewEngine(app)
	_, _, ok := eng.evalCheckDown(check.Id)
	if ok {
		t.Error("expected ok=false when the check has never run")
	}
}

func TestEvalCheckDown_ReadsLatestDebouncedStatus(t *testing.T) {
	app := newTestApp(t)
	check := createCheck(t, app, "flappy", "tcp", "10.0.0.1:5432")

	now := time.Now()
	createCheckResult(t, app, check.Id, "up", time.Time{}, now.Add(-2*time.Minute))
	createCheckResult(t, app, check.Id, "down", time.Time{}, now.Add(-1*time.Minute))

	eng := NewEngine(app)
	value, breaching, ok := eng.evalCheckDown(check.Id)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !breaching || value != 1 {
		t.Errorf("value=%v breaching=%v, want value=1 breaching=true (latest result is down)", value, breaching)
	}
}

func TestEvalCheckDown_UpIsNotBreaching(t *testing.T) {
	app := newTestApp(t)
	check := createCheck(t, app, "healthy", "tcp", "10.0.0.1:5432")
	createCheckResult(t, app, check.Id, "up", time.Time{}, time.Now())

	eng := NewEngine(app)
	value, breaching, ok := eng.evalCheckDown(check.Id)
	if !ok || breaching || value != 0 {
		t.Errorf("value=%v breaching=%v ok=%v, want value=0 breaching=false ok=true", value, breaching, ok)
	}
}

func TestEvalCertExpiry_NoTLSRecordedIsNotOK(t *testing.T) {
	app := newTestApp(t)
	check := createCheck(t, app, "tcp-check", "tcp", "10.0.0.1:5432")
	createCheckResult(t, app, check.Id, "up", time.Time{}, time.Now())

	eng := NewEngine(app)
	_, _, ok := eng.evalCertExpiry(check.Id, 14)
	if ok {
		t.Error("expected ok=false when the check never recorded a certificate")
	}
}

func TestEvalCertExpiry_WithinWarnDaysBreaches(t *testing.T) {
	app := newTestApp(t)
	check := createCheck(t, app, "https-check", "http", "https://example.com")
	expiresIn5Days := time.Now().Add(5 * 24 * time.Hour)
	createCheckResult(t, app, check.Id, "up", expiresIn5Days, time.Now())

	eng := NewEngine(app)
	value, breaching, ok := eng.evalCertExpiry(check.Id, 14)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !breaching {
		t.Error("expected breaching=true (5 days left <= 14 day warn threshold)")
	}
	if value < 4.9 || value > 5.1 {
		t.Errorf("value = %v, want ~5 (days left)", value)
	}
}

func TestEvalCertExpiry_FarFromExpiryDoesNotBreach(t *testing.T) {
	app := newTestApp(t)
	check := createCheck(t, app, "https-check-2", "http", "https://example.com")
	expiresIn90Days := time.Now().Add(90 * 24 * time.Hour)
	createCheckResult(t, app, check.Id, "up", expiresIn90Days, time.Now())

	eng := NewEngine(app)
	_, breaching, ok := eng.evalCertExpiry(check.Id, 14)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if breaching {
		t.Error("expected breaching=false (90 days left > 14 day warn threshold)")
	}
}

func TestEvaluateCheckRule_ScopedToOneCheck(t *testing.T) {
	app := newTestApp(t)
	target := createCheck(t, app, "target-check", "tcp", "10.0.0.1:1")
	other := createCheck(t, app, "other-check", "tcp", "10.0.0.2:1")
	createCheckResult(t, app, target.Id, "down", time.Time{}, time.Now())
	createCheckResult(t, app, other.Id, "down", time.Time{}, time.Now())

	rule := createRule(t, app, ruleOpts{
		name: "check-down-scoped", metricType: "check_down", condition: "gt", threshold: 1,
		severity: "critical", durationSeconds: 10, enabled: true, checkID: target.Id,
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now
	// The first evaluation only starts breach-tracking; advancing past the
	// rule's duration makes the second evaluation fire.
	eng.evaluateCheckRule(rule, nil)
	clock.Advance(11 * time.Second)
	eng.evaluateCheckRule(rule, nil)

	if got := countCheckAlerts(t, app, rule.Id, target.Id); len(got) != 1 {
		t.Errorf("target check alerts = %d, want 1", len(got))
	}
	if got := countCheckAlerts(t, app, rule.Id, other.Id); len(got) != 0 {
		t.Errorf("other check alerts = %d, want 0 (rule is scoped to target only)", len(got))
	}
}

func TestEvaluateCheckRule_EmptyCheckIDAppliesToEveryCheck(t *testing.T) {
	app := newTestApp(t)
	down1 := createCheck(t, app, "down-1", "tcp", "10.0.0.1:1")
	down2 := createCheck(t, app, "down-2", "tcp", "10.0.0.2:1")
	up1 := createCheck(t, app, "up-1", "tcp", "10.0.0.3:1")
	createCheckResult(t, app, down1.Id, "down", time.Time{}, time.Now())
	createCheckResult(t, app, down2.Id, "down", time.Time{}, time.Now())
	createCheckResult(t, app, up1.Id, "up", time.Time{}, time.Now())

	rule := createRule(t, app, ruleOpts{
		name: "check-down-global", metricType: "check_down", condition: "gt", threshold: 1,
		severity: "critical", durationSeconds: 10, enabled: true, // checkID left empty: applies to every check
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now
	eng.evaluateCheckRule(rule, nil)
	clock.Advance(11 * time.Second)
	eng.evaluateCheckRule(rule, nil)

	if got := countCheckAlerts(t, app, rule.Id, down1.Id); len(got) != 1 {
		t.Errorf("down-1 alerts = %d, want 1", len(got))
	}
	if got := countCheckAlerts(t, app, rule.Id, down2.Id); len(got) != 1 {
		t.Errorf("down-2 alerts = %d, want 1", len(got))
	}
	if got := countCheckAlerts(t, app, rule.Id, up1.Id); len(got) != 0 {
		t.Errorf("up-1 alerts = %d, want 0", len(got))
	}
}

func TestFireAlert_CheckBased_SetsCheckIDNotAgentID(t *testing.T) {
	app := newTestApp(t)
	check := createCheck(t, app, "billing-api", "http", "https://billing.internal/health")
	createCheckResult(t, app, check.Id, "down", time.Time{}, time.Now())

	rule := createRule(t, app, ruleOpts{
		name: "check-down", metricType: "check_down", condition: "gt", threshold: 1,
		severity: "critical", durationSeconds: 10, enabled: true,
	})

	eng := NewEngine(app)
	clock := &fakeClock{now: time.Now()}
	eng.clock = clock.Now
	eng.evaluateCheckRule(rule, nil)
	clock.Advance(11 * time.Second)
	eng.evaluateCheckRule(rule, nil)

	alerts := countCheckAlerts(t, app, rule.Id, check.Id)
	if len(alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(alerts))
	}
	alert := alerts[0]
	if alert.GetString("agent_id") != "" {
		t.Errorf("agent_id = %q, want empty for a check-based alert", alert.GetString("agent_id"))
	}
	if alert.GetString("check_id") != check.Id {
		t.Errorf("check_id = %q, want %q", alert.GetString("check_id"), check.Id)
	}
	if msg := alert.GetString("message"); msg == "" {
		t.Error("expected a non-empty message")
	} else {
		t.Logf("check_down message: %s", msg)
	}
}

func TestSeedState_RestoresCheckBasedFiringAlerts(t *testing.T) {
	app := newTestApp(t)
	check := createCheck(t, app, "restore-me", "tcp", "10.0.0.9:1")
	rule := createRule(t, app, ruleOpts{
		name: "check-down-restore", metricType: "check_down", condition: "gt", threshold: 1,
		severity: "critical", durationSeconds: 10, enabled: true,
	})

	col, err := app.FindCollectionByNameOrId("alerts")
	if err != nil {
		t.Fatalf("find alerts collection: %v", err)
	}
	alert := core.NewRecord(col)
	alert.Set("rule_id", rule.Id)
	alert.Set("check_id", check.Id)
	alert.Set("status", "firing")
	alert.Set("value", 1)
	alert.Set("message", "pre-existing check_down incident")
	alert.Set("fired_at", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))
	if err := app.Save(alert); err != nil {
		t.Fatalf("save pre-existing alert: %v", err)
	}

	eng := NewEngine(app)
	eng.seedState()

	eng.mu.Lock()
	state, exists := eng.states[ruleAgentKey{RuleID: rule.Id, AgentID: check.Id}]
	eng.mu.Unlock()
	if !exists {
		t.Fatal("expected seedState to restore a state entry keyed by check id")
	}
	if !state.Fired || state.ActiveAlertID != alert.Id {
		t.Errorf("restored state = %+v, want Fired=true ActiveAlertID=%q", state, alert.Id)
	}
}
