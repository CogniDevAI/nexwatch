package api

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// TestHealthz_ReturnsOKWithAgentCounts asserts the happy path: an
// unauthenticated GET /healthz returns 200 with the process version,
// uptime, a healthy "db" status, and a count of only the online agents.
func TestHealthz_ReturnsOKWithAgentCounts(t *testing.T) {
	app := mustNewApp(t)
	mustSaveAgentWithStatus(t, app, "online-agent", "online")
	mustSaveAgentWithStatus(t, app, "offline-agent", "offline")

	scenario := tests.ApiScenario{
		Name:           "unauthenticated GET /healthz",
		Method:         http.MethodGet,
		URL:            "/healthz",
		ExpectedStatus: http.StatusOK,
		ExpectedContent: []string{
			`"status":"ok"`,
			`"db":"ok"`,
			`"agents_online":1`,
		},
		TestAppFactory: func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			RegisterHealthRoute(e, "test-version")
		},
	}
	scenario.Test(t)
}

// TestHealthz_Returns503WhenDBCheckFails asserts that a failing DB check
// (simulated here by removing the "status" column the check queries)
// flips status/db to "error" and the HTTP status to 503, rather than
// reporting healthy while the database is actually unusable.
func TestHealthz_Returns503WhenDBCheckFails(t *testing.T) {
	app := mustNewApp(t)

	agents, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	agents.RemoveIndex("idx_agents_status")
	agents.Fields.RemoveByName("status")
	if err := app.Save(agents); err != nil {
		t.Fatalf("remove agents.status field: %v", err)
	}

	scenario := tests.ApiScenario{
		Name:           "GET /healthz with a broken DB query",
		Method:         http.MethodGet,
		URL:            "/healthz",
		ExpectedStatus: http.StatusServiceUnavailable,
		ExpectedContent: []string{
			`"status":"error"`,
			`"db":"error"`,
		},
		TestAppFactory: func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			RegisterHealthRoute(e, "test-version")
		},
	}
	scenario.Test(t)
}

// mustSaveAgentWithStatus is like mustSaveAgent but lets the test control
// the agent's status, which /healthz's agents_online count depends on.
func mustSaveAgentWithStatus(t testing.TB, app core.App, hostname, status string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("hostname", hostname)
	rec.Set("status", status)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	return rec
}
