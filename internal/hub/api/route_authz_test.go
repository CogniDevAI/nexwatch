package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/CogniDevAI/nexwatch/internal/hub/agenttoken"
	"github.com/CogniDevAI/nexwatch/internal/hub/checks"
	"github.com/CogniDevAI/nexwatch/internal/hub/commands"
	"github.com/CogniDevAI/nexwatch/internal/hub/metrics"
	"github.com/CogniDevAI/nexwatch/internal/hub/notify"

	// Registers the app's migrations so tests.NewTestApp() creates the same
	// schema (agents, users.role, metrics, ...) that production runs
	// against, and so the "users" collection already has the "role" field
	// without hand-rolling it per test.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// fakeCommandSender is a no-op CommandSender: none of these scenarios need
// a real connected agent, only route-level authorization.
type fakeCommandSender struct{}

func (fakeCommandSender) SendCommand(agentID string, payload *protocol.CommandPayload) error {
	return nil
}

// fakeCheckRunner is a CheckRunner that never actually probes anything: it
// builds an (unsaved) synthetic check_results record so route-level tests
// can assert on the "run now" endpoint's response shape without depending
// on internal/hub/checks' own scheduling/execution behavior (covered by
// that package's own tests).
type fakeCheckRunner struct{ app core.App }

func (f fakeCheckRunner) Snapshot() map[string]checks.Snapshot {
	return map[string]checks.Snapshot{}
}

func (f fakeCheckRunner) RunOnce(checkID string) (*core.Record, error) {
	col, err := f.app.FindCollectionByNameOrId("check_results")
	if err != nil {
		return nil, err
	}
	rec := core.NewRecord(col)
	rec.Set("check_id", checkID)
	rec.Set("status", "up")
	rec.Set("latency_ms", 12.5)
	rec.Set("checked_at", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))
	return rec, nil
}

// fakeLatestReleaseFetcher is a LatestReleaseFetcher test double: none of
// these scenarios need a real call to api.github.com, only route-level
// authorization/behavior.
type fakeLatestReleaseFetcher struct {
	version     string
	publishedAt string
	err         error
}

func (f fakeLatestReleaseFetcher) FetchLatest(ctx context.Context) (string, string, error) {
	if f.err != nil {
		return "", "", f.err
	}
	return f.version, f.publishedAt, nil
}

// registerRoutesForTest wires the exact same "/api/custom" group and
// RequireAuth() middleware that cmd/hub/main.go registers in production, so
// these scenarios exercise real route-level authorization rather than a
// reimplementation of it.
func registerRoutesForTest(app core.App, e *core.ServeEvent) {
	metricsSvc := metrics.NewService(app)
	apiGroup := e.Router.Group("/api/custom")
	apiGroup.Bind(apis.RequireAuth())
	RegisterRoutes(e, apiGroup, metricsSvc, fakeCommandSender{})
	RegisterAlertRoutes(apiGroup, notify.NewService(app))
	RegisterChecksRoutes(apiGroup, fakeCheckRunner{app: app})
	RegisterDockerRoutes(apiGroup, commands.NewBroker(), fakeCommandSender{})
	RegisterUpdateRoutes(apiGroup, commands.NewBroker(), fakeCommandSender{}, fakeLatestReleaseFetcher{})
	RegisterLogsRoutes(apiGroup)
	RegisterPrometheusTokenRoute(apiGroup)
	RegisterReportRoutes(apiGroup)
	RegisterPushRoutes(apiGroup)
}

func mustSaveCheck(t testing.TB, app core.App, name string) *core.Record {
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

func mustNewApp(t testing.TB) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	return app
}

func mustSaveSuperuser(t testing.TB, app core.App, email string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatalf("find superusers collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.SetEmail(email)
	rec.SetPassword("password123456")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save superuser: %v", err)
	}
	return rec
}

func mustSaveUserWithRole(t testing.TB, app core.App, email, role string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.SetEmail(email)
	rec.SetPassword("password123456")
	rec.Set("role", role)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save user %s: %v", email, err)
	}
	return rec
}

func mustAuthToken(t testing.TB, rec *core.Record) string {
	t.Helper()
	token, err := rec.NewAuthToken()
	if err != nil {
		t.Fatalf("NewAuthToken() for %s: %v", rec.Id, err)
	}
	return token
}

func mustSaveAgent(t testing.TB, app core.App, hostname string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("hostname", hostname)
	rec.Set("status", "online")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	return rec
}

func TestRoutes_UnauthenticatedDashboardIsRejected(t *testing.T) {
	app := mustNewApp(t)

	scenario := tests.ApiScenario{
		Name:            "unauthenticated GET /api/custom/dashboard",
		Method:          http.MethodGet,
		URL:             "/api/custom/dashboard",
		ExpectedStatus:  http.StatusUnauthorized,
		ExpectedContent: []string{"requires valid record authorization"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestRoutes_SuperuserCanAccessDashboard(t *testing.T) {
	app := mustNewApp(t)
	superuser := mustSaveSuperuser(t, app, "root@example.com")
	token := mustAuthToken(t, superuser)

	scenario := tests.ApiScenario{
		Name:            "superuser GET /api/custom/dashboard",
		Method:          http.MethodGet,
		URL:             "/api/custom/dashboard",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"agents"`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestRoutes_ViewerCannotGenerateAgentToken(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-viewer-test")
	viewer := mustSaveUserWithRole(t, app, "viewer@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	scenario := tests.ApiScenario{
		Name:            "viewer POST /api/custom/agents/{id}/token",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/token",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"requires a higher role"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestRoutes_AdminCanGenerateAgentTokenAndItHashesCorrectly(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-admin-test")
	admin := mustSaveUserWithRole(t, app, "admin@example.com", "admin")
	token := mustAuthToken(t, admin)

	scenario := tests.ApiScenario{
		Name:            "admin POST /api/custom/agents/{id}/token",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/token",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"token":`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			var body struct {
				Token string `json:"token"`
			}
			defer func() { _ = res.Body.Close() }()
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatalf("decode response body: %v", err)
			}
			if body.Token == "" {
				t.Fatal("response did not include a plaintext token")
			}

			fresh, err := app.FindRecordById("agents", agent.Id)
			if err != nil {
				t.Fatalf("find agent after token generation: %v", err)
			}
			wantHash := agenttoken.Hash(body.Token)
			if got := fresh.GetString("token_hash"); got != wantHash {
				t.Errorf("stored token_hash = %q, want sha256(returned plaintext) = %q", got, wantHash)
			}
		},
	}
	scenario.Test(t)
}

func TestRoutes_ThreadDumpWithNonJVMPIDReturns400(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-threaddump-test")
	admin := mustSaveUserWithRole(t, app, "admin-td@example.com", "admin")
	token := mustAuthToken(t, admin)

	// Seed a "processes" metric snapshot containing a non-JVM process at a
	// known PID, so isDumpablePID() has something to look the PID up in.
	metricsCol, err := app.FindCollectionByNameOrId("metrics")
	if err != nil {
		t.Fatalf("find metrics collection: %v", err)
	}
	snapshot := core.NewRecord(metricsCol)
	snapshot.Set("agent_id", agent.Id)
	snapshot.Set("type", "processes")
	snapshot.Set("data", map[string]any{
		"processes": []map[string]any{
			{"pid": 4242, "name": "bash", "cmdline": "/bin/bash"},
		},
		"total_count": 1,
	})
	snapshot.Set("timestamp", "2026-01-01 00:00:00.000Z")
	snapshot.Set("resolution", "raw")
	if err := app.Save(snapshot); err != nil {
		t.Fatalf("save processes snapshot: %v", err)
	}

	scenario := tests.ApiScenario{
		Name:            "thread-dump request for a non-JVM pid",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/thread-dump",
		Body:            strings.NewReader(`{"pid": 4242}`),
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusBadRequest,
		ExpectedContent: []string{"does not look like a JVM"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

// mustSaveAlertRule saves a minimal valid "alert_rules" record.
func mustSaveAlertRule(t testing.TB, app core.App) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("alert_rules")
	if err != nil {
		t.Fatalf("find alert_rules collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "authz-test-rule")
	rec.Set("metric_type", "cpu")
	rec.Set("condition", "gt")
	rec.Set("threshold", 80)
	rec.Set("duration", 10)
	rec.Set("severity", "critical")
	rec.Set("enabled", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save alert rule: %v", err)
	}
	return rec
}

// mustSaveFiringAlert saves a minimal "alerts" record with status=firing.
func mustSaveFiringAlert(t testing.TB, app core.App, ruleID, agentID string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("alerts")
	if err != nil {
		t.Fatalf("find alerts collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("rule_id", ruleID)
	rec.Set("agent_id", agentID)
	rec.Set("status", "firing")
	rec.Set("value", 95)
	rec.Set("message", "cpu too high")
	rec.Set("fired_at", "2026-01-01 00:00:00.000Z")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save alert: %v", err)
	}
	return rec
}

func TestRoutes_ViewerCannotAckAlert(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-ack-viewer")
	rule := mustSaveAlertRule(t, app)
	alert := mustSaveFiringAlert(t, app, rule.Id, agent.Id)
	viewer := mustSaveUserWithRole(t, app, "viewer-ack@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	scenario := tests.ApiScenario{
		Name:            "viewer POST /api/custom/alerts/{id}/ack",
		Method:          http.MethodPost,
		URL:             "/api/custom/alerts/" + alert.Id + "/ack",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"requires a higher role"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestRoutes_OperatorCanAckAlert(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-ack-operator")
	rule := mustSaveAlertRule(t, app)
	alert := mustSaveFiringAlert(t, app, rule.Id, agent.Id)
	operator := mustSaveUserWithRole(t, app, "operator-ack@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST /api/custom/alerts/{id}/ack",
		Method:          http.MethodPost,
		URL:             "/api/custom/alerts/" + alert.Id + "/ack",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"status":"ok"`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			fresh, err := app.FindRecordById("alerts", alert.Id)
			if err != nil {
				t.Fatalf("find alert after ack: %v", err)
			}
			if fresh.GetString("acknowledged_at") == "" {
				t.Error("acknowledged_at is empty, want it set")
			}
			if fresh.GetString("acknowledged_by") == "" {
				t.Error("acknowledged_by is empty, want it set")
			}
		},
	}
	scenario.Test(t)
}

func TestRoutes_AckUnknownAlertReturns404(t *testing.T) {
	app := mustNewApp(t)
	operator := mustSaveUserWithRole(t, app, "operator-ack-404@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST /api/custom/alerts/{id}/ack for an unknown id",
		Method:          http.MethodPost,
		URL:             "/api/custom/alerts/does-not-exist/ack",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusNotFound,
		ExpectedContent: []string{"alert not found"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestRoutes_OperatorCanUnackAlert(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-unack-operator")
	rule := mustSaveAlertRule(t, app)
	alert := mustSaveFiringAlert(t, app, rule.Id, agent.Id)
	alert.Set("acknowledged_at", "2026-01-01 00:05:00.000Z")
	alert.Set("acknowledged_by", "someone")
	if err := app.Save(alert); err != nil {
		t.Fatalf("save pre-acknowledged alert: %v", err)
	}
	operator := mustSaveUserWithRole(t, app, "operator-unack@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST /api/custom/alerts/{id}/unack",
		Method:          http.MethodPost,
		URL:             "/api/custom/alerts/" + alert.Id + "/unack",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"status":"ok"`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			fresh, err := app.FindRecordById("alerts", alert.Id)
			if err != nil {
				t.Fatalf("find alert after unack: %v", err)
			}
			if fresh.GetString("acknowledged_at") != "" {
				t.Error("acknowledged_at still set, want cleared")
			}
			if fresh.GetString("acknowledged_by") != "" {
				t.Error("acknowledged_by still set, want cleared")
			}
		},
	}
	scenario.Test(t)
}

func TestRoutes_ViewerCanListActiveSilences(t *testing.T) {
	app := mustNewApp(t)
	viewer := mustSaveUserWithRole(t, app, "viewer-silences@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	scenario := tests.ApiScenario{
		Name:            "viewer GET /api/custom/silences/active",
		Method:          http.MethodGet,
		URL:             "/api/custom/silences/active",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"silences"`, `"total"`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestRoutes_ViewerCannotRunCheckNow(t *testing.T) {
	app := mustNewApp(t)
	check := mustSaveCheck(t, app, "viewer-cannot-run")
	viewer := mustSaveUserWithRole(t, app, "viewer-checks@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	scenario := tests.ApiScenario{
		Name:            "viewer POST /api/custom/checks/{id}/run",
		Method:          http.MethodPost,
		URL:             "/api/custom/checks/" + check.Id + "/run",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"requires a higher role"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestRoutes_OperatorCanRunCheckNow(t *testing.T) {
	app := mustNewApp(t)
	check := mustSaveCheck(t, app, "operator-can-run")
	operator := mustSaveUserWithRole(t, app, "operator-checks@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST /api/custom/checks/{id}/run",
		Method:          http.MethodPost,
		URL:             "/api/custom/checks/" + check.Id + "/run",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"status":"up"`, `"check_id":"` + check.Id + `"`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestRoutes_ViewerCanReadChecksSummary(t *testing.T) {
	app := mustNewApp(t)
	mustSaveCheck(t, app, "summary-check")
	viewer := mustSaveUserWithRole(t, app, "viewer-summary@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	scenario := tests.ApiScenario{
		Name:            "viewer GET /api/custom/checks/summary",
		Method:          http.MethodGet,
		URL:             "/api/custom/checks/summary",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"checks"`, `"summary-check"`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}
