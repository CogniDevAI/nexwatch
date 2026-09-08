package api

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func mustSaveLog(t testing.TB, app core.App, agentID, level, unit, message string, ts time.Time) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("logs")
	if err != nil {
		t.Fatalf("find logs collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("agent_id", agentID)
	rec.Set("ts", ts.UTC().Format(logsDateFormat))
	rec.Set("source", "journald")
	rec.Set("unit", unit)
	rec.Set("level", level)
	rec.Set("message", message)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save log: %v", err)
	}
	return rec
}

func TestLogsQuery_UnauthenticatedIsRejected(t *testing.T) {
	app := mustNewApp(t)

	scenario := tests.ApiScenario{
		Name:            "unauthenticated GET /api/custom/logs",
		Method:          http.MethodGet,
		URL:             "/api/custom/logs",
		ExpectedStatus:  http.StatusUnauthorized,
		ExpectedContent: []string{"requires valid record authorization"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestLogsQuery_ReturnsNewestFirstAndFiltersByLevel(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-logs-query")
	viewer := mustSaveUserWithRole(t, app, "viewer-logs@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	now := time.Now().UTC()
	mustSaveLog(t, app, agent.Id, "info", "nginx.service", "server started", now.Add(-2*time.Minute))
	mustSaveLog(t, app, agent.Id, "error", "nginx.service", "connection refused", now.Add(-1*time.Minute))

	scenario := tests.ApiScenario{
		Name:               "GET /api/custom/logs?level=error",
		Method:             http.MethodGet,
		URL:                "/api/custom/logs?agent_id=" + agent.Id + "&level=error",
		Headers:            map[string]string{"Authorization": token},
		ExpectedStatus:     http.StatusOK,
		ExpectedContent:    []string{"connection refused"},
		NotExpectedContent: []string{"server started"},
		TestAppFactory:     func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestLogsQuery_SubstringSearchIsCaseInsensitive(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-logs-search")
	viewer := mustSaveUserWithRole(t, app, "viewer-logs-search@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	mustSaveLog(t, app, agent.Id, "error", "app.service", "OutOfMemoryError raised", time.Now().UTC())
	mustSaveLog(t, app, agent.Id, "info", "app.service", "healthy heartbeat", time.Now().UTC())

	scenario := tests.ApiScenario{
		Name:               "GET /api/custom/logs?q=outofmemory",
		Method:             http.MethodGet,
		URL:                "/api/custom/logs?agent_id=" + agent.Id + "&q=outofmemory",
		Headers:            map[string]string{"Authorization": token},
		ExpectedStatus:     http.StatusOK,
		ExpectedContent:    []string{"OutOfMemoryError raised"},
		NotExpectedContent: []string{"healthy heartbeat"},
		TestAppFactory:     func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestLogsQuery_CursorPaginationLoadsOlderEntries(t *testing.T) {
	app := mustNewApp(t)
	t.Cleanup(app.Cleanup) // reused across two scenario.Test() calls below, so disable each one's own auto-cleanup and do it once here
	agent := mustSaveAgent(t, app, "host-logs-cursor")
	viewer := mustSaveUserWithRole(t, app, "viewer-logs-cursor@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	now := time.Now().UTC()
	mustSaveLog(t, app, agent.Id, "info", "", "oldest entry", now.Add(-3*time.Minute))
	middle := mustSaveLog(t, app, agent.Id, "info", "", "middle entry", now.Add(-2*time.Minute))
	mustSaveLog(t, app, agent.Id, "info", "", "newest entry", now.Add(-1*time.Minute))

	// First page: limit 2, should return newest + middle, with a next_before cursor.
	scenario := tests.ApiScenario{
		Name:           "GET /api/custom/logs?limit=2 (first page)",
		Method:         http.MethodGet,
		URL:            "/api/custom/logs?agent_id=" + agent.Id + "&limit=2",
		Headers:        map[string]string{"Authorization": token},
		ExpectedStatus: http.StatusOK,
		ExpectedContent: []string{
			"newest entry", "middle entry", `"next_before"`,
		},
		NotExpectedContent:    []string{"oldest entry"},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)

	// Second page: pass a cursor built from the middle record (the last
	// row of page 1) — should return only the older, third entry.
	cursor := strconv.FormatInt(middle.GetDateTime("ts").Time().UnixMilli(), 10) + "_" + middle.Id
	scenario2 := tests.ApiScenario{
		Name:                  "GET /api/custom/logs?before=<cursor> (second page)",
		Method:                http.MethodGet,
		URL:                   "/api/custom/logs?agent_id=" + agent.Id + "&before=" + cursor,
		Headers:               map[string]string{"Authorization": token},
		ExpectedStatus:        http.StatusOK,
		ExpectedContent:       []string{"oldest entry"},
		NotExpectedContent:    []string{"middle entry", "newest entry"},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario2.Test(t)
}

func TestLogUnits_ReturnsDistinctUnitsSortedAlphabetically(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-logs-units")
	viewer := mustSaveUserWithRole(t, app, "viewer-logs-units@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	now := time.Now().UTC()
	mustSaveLog(t, app, agent.Id, "info", "zeta.service", "z", now)
	mustSaveLog(t, app, agent.Id, "info", "alpha.service", "a", now)
	mustSaveLog(t, app, agent.Id, "info", "alpha.service", "a2", now)
	// Outside the 24h lookback window — must not appear.
	mustSaveLog(t, app, agent.Id, "info", "old.service", "old", now.Add(-48*time.Hour))

	scenario := tests.ApiScenario{
		Name:            "GET /api/custom/logs/units",
		Method:          http.MethodGet,
		URL:             "/api/custom/logs/units?agent_id=" + agent.Id,
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"units":["alpha.service","zeta.service"]`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}
