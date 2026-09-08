package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/CogniDevAI/nexwatch/internal/hub/commands"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

func registerUpdateRoutesForTest(app core.App, e *core.ServeEvent, broker *commands.Broker, sender CommandSender, fetcher LatestReleaseFetcher) {
	apiGroup := e.Router.Group("/api/custom")
	apiGroup.Bind(apis.RequireAuth())
	RegisterUpdateRoutes(apiGroup, broker, sender, fetcher)
}

// mustSaveAgentWithPlatform saves an agent with platform/arch/version set,
// the shape internal/hub/api/update_routes.go requires to dispatch an
// update.
func mustSaveAgentWithPlatform(t testing.TB, app core.App, hostname, platform, arch, version string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("hostname", hostname)
	rec.Set("status", "online")
	rec.Set("platform", platform)
	rec.Set("arch", arch)
	rec.Set("version", version)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	return rec
}

func TestAgentUpdate_ViewerForbidden(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgentWithPlatform(t, app, "host-update-viewer", "linux", "amd64", "0.9.0")
	viewer := mustSaveUserWithRole(t, app, "viewer-update@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	scenario := tests.ApiScenario{
		Name:            "viewer POST agent update",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/update",
		Headers:         map[string]string{"Authorization": token},
		Body:            strings.NewReader(`{"version": "0.9.1"}`),
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"requires a higher role"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerUpdateRoutesForTest(app, e, commands.NewBroker(), neverRespondingCommandSender{}, fakeLatestReleaseFetcher{})
		},
	}
	scenario.Test(t)
}

func TestAgentUpdate_InvalidVersionReturns400(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgentWithPlatform(t, app, "host-update-badversion", "linux", "amd64", "0.9.0")
	operator := mustSaveUserWithRole(t, app, "operator-update-badversion@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST agent update with a malformed version",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/update",
		Headers:         map[string]string{"Authorization": token},
		Body:            strings.NewReader(`{"version": "not-a-version"}`),
		ExpectedStatus:  http.StatusBadRequest,
		ExpectedContent: []string{"version must look like"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerUpdateRoutesForTest(app, e, commands.NewBroker(), neverRespondingCommandSender{}, fakeLatestReleaseFetcher{})
		},
	}
	scenario.Test(t)
}

func TestAgentUpdate_UnknownAgentReturns404(t *testing.T) {
	app := mustNewApp(t)
	operator := mustSaveUserWithRole(t, app, "operator-update-404@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST agent update for an unknown agent id",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/does-not-exist/update",
		Headers:         map[string]string{"Authorization": token},
		Body:            strings.NewReader(`{"version": "0.9.1"}`),
		ExpectedStatus:  http.StatusNotFound,
		ExpectedContent: []string{"agent not found"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerUpdateRoutesForTest(app, e, commands.NewBroker(), neverRespondingCommandSender{}, fakeLatestReleaseFetcher{})
		},
	}
	scenario.Test(t)
}

func TestAgentUpdate_UnknownOSArchReturns400(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-update-no-arch") // no platform/arch set
	operator := mustSaveUserWithRole(t, app, "operator-update-noarch@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST agent update when the agent's os/arch is unknown",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/update",
		Headers:         map[string]string{"Authorization": token},
		Body:            strings.NewReader(`{"version": "0.9.1"}`),
		ExpectedStatus:  http.StatusBadRequest,
		ExpectedContent: []string{"os/arch is not known"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerUpdateRoutesForTest(app, e, commands.NewBroker(), neverRespondingCommandSender{}, fakeLatestReleaseFetcher{})
		},
	}
	scenario.Test(t)
}

func TestAgentUpdate_OperatorSuccessSetsAgentFieldsAndAudits(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgentWithPlatform(t, app, "host-update-success", "linux", "amd64", "0.9.0")
	operator := mustSaveUserWithRole(t, app, "operator-update-success@example.com", "operator")
	token := mustAuthToken(t, operator)

	broker := commands.NewBroker()
	sender := respondingCommandSender{broker: broker, response: newUpdateStartedResponse()}

	scenario := tests.ApiScenario{
		Name:            "operator POST agent update succeeds",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/update",
		Headers:         map[string]string{"Authorization": token},
		Body:            strings.NewReader(`{"version": "v0.9.1"}`),
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"ok":true`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerUpdateRoutesForTest(app, e, broker, sender, fakeLatestReleaseFetcher{})
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			reloaded, err := app.FindRecordById("agents", agent.Id)
			if err != nil {
				t.Fatalf("reload agent: %v", err)
			}
			if got := reloaded.GetString("update_status"); got != "started" {
				t.Errorf("update_status = %q, want started", got)
			}
			// The leading "v" must be stripped before it's stored, matching
			// the release asset filename convention.
			if got := reloaded.GetString("update_target_version"); got != "0.9.1" {
				t.Errorf("update_target_version = %q, want 0.9.1", got)
			}
			if reloaded.GetString("update_requested_at") == "" {
				t.Error("update_requested_at was not set")
			}
			if reloaded.GetString("update_requested_by") == "" {
				t.Error("update_requested_by was not set")
			}

			entries, err := app.FindRecordsByFilter(
				"audit_log",
				"action = 'agent.update.request' && target_id = '"+agent.Id+"'",
				"", 1, 0,
			)
			if err != nil || len(entries) == 0 {
				t.Fatalf("expected an audit_log entry for agent.update.request, err=%v entries=%d", err, len(entries))
			}
			if entries[0].GetString("result") != "success" {
				t.Errorf("audit_log result = %q, want success", entries[0].GetString("result"))
			}
		},
	}
	scenario.Test(t)
}

func TestAgentUpdate_AgentNotConnectedReturns502AndMarksFailed(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgentWithPlatform(t, app, "host-update-not-connected", "linux", "amd64", "0.9.0")
	operator := mustSaveUserWithRole(t, app, "operator-update-notconn@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST agent update when the agent send fails",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/update",
		Headers:         map[string]string{"Authorization": token},
		Body:            strings.NewReader(`{"version": "0.9.1"}`),
		ExpectedStatus:  http.StatusBadGateway,
		ExpectedContent: []string{`"ok":false`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			broker := commands.NewBroker()
			sender := respondingCommandSender{broker: broker, sendErr: http.ErrHandlerTimeout}
			registerUpdateRoutesForTest(app, e, broker, sender, fakeLatestReleaseFetcher{})
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			reloaded, err := app.FindRecordById("agents", agent.Id)
			if err != nil {
				t.Fatalf("reload agent: %v", err)
			}
			if got := reloaded.GetString("update_status"); got != "failed" {
				t.Errorf("update_status = %q, want failed", got)
			}
			if reloaded.GetString("update_error") == "" {
				t.Error("update_error was not set")
			}
		},
	}
	scenario.Test(t)
}

func TestAgentUpdateAll_OperatorForbidden(t *testing.T) {
	app := mustNewApp(t)
	operator := mustSaveUserWithRole(t, app, "operator-update-all-forbidden@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST update-all is forbidden (admin only)",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/update-all",
		Headers:         map[string]string{"Authorization": token},
		Body:            strings.NewReader(`{"version": "0.9.1"}`),
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"requires a higher role"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerUpdateRoutesForTest(app, e, commands.NewBroker(), neverRespondingCommandSender{}, fakeLatestReleaseFetcher{})
		},
	}
	scenario.Test(t)
}

func TestAgentUpdateAll_AdminFanOutSkipsUpToDateWhenOnlyOutdated(t *testing.T) {
	app := mustNewApp(t)
	outdated := mustSaveAgentWithPlatform(t, app, "host-outdated", "linux", "amd64", "0.9.0")
	upToDate := mustSaveAgentWithPlatform(t, app, "host-up-to-date", "linux", "amd64", "0.9.1")
	admin := mustSaveUserWithRole(t, app, "admin-update-all@example.com", "admin")
	token := mustAuthToken(t, admin)

	broker := commands.NewBroker()
	sender := respondingCommandSender{broker: broker, response: newUpdateStartedResponse()}

	scenario := tests.ApiScenario{
		Name:            "admin POST update-all with only_outdated skips the up-to-date agent",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/update-all",
		Headers:         map[string]string{"Authorization": token},
		Body:            strings.NewReader(`{"version": "0.9.1", "only_outdated": true}`),
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{outdated.Id},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerUpdateRoutesForTest(app, e, broker, sender, fakeLatestReleaseFetcher{})
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			outdatedReloaded, err := app.FindRecordById("agents", outdated.Id)
			if err != nil {
				t.Fatalf("reload outdated agent: %v", err)
			}
			if got := outdatedReloaded.GetString("update_status"); got != "started" {
				t.Errorf("outdated agent update_status = %q, want started", got)
			}

			upToDateReloaded, err := app.FindRecordById("agents", upToDate.Id)
			if err != nil {
				t.Fatalf("reload up-to-date agent: %v", err)
			}
			if got := upToDateReloaded.GetString("update_status"); got != "" {
				t.Errorf("up-to-date agent update_status = %q, want untouched (empty)", got)
			}
		},
	}
	scenario.Test(t)
}

func TestLatestVersion_ReturnsVersionAndCachesSecondCall(t *testing.T) {
	app := mustNewApp(t)
	viewer := mustSaveUserWithRole(t, app, "viewer-latest-version@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	fetcher := &countingFetcher{version: "0.9.5", publishedAt: "2026-01-01T00:00:00Z"}

	scenario := tests.ApiScenario{
		Name:            "viewer GET latest-version",
		Method:          http.MethodGet,
		URL:             "/api/custom/agents/latest-version",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"version":"0.9.5"`, `"cached":false`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerUpdateRoutesForTest(app, e, commands.NewBroker(), neverRespondingCommandSender{}, fetcher)
		},
	}
	scenario.Test(t)

	if fetcher.calls != 1 {
		t.Fatalf("fetcher was called %d times after first request, want 1", fetcher.calls)
	}
}

func TestLatestVersion_FetcherErrorReturns200WithError(t *testing.T) {
	app := mustNewApp(t)
	viewer := mustSaveUserWithRole(t, app, "viewer-latest-version-error@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	scenario := tests.ApiScenario{
		Name:            "viewer GET latest-version when GitHub is unreachable",
		Method:          http.MethodGet,
		URL:             "/api/custom/agents/latest-version",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"error"`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerUpdateRoutesForTest(app, e, commands.NewBroker(), neverRespondingCommandSender{}, fakeLatestReleaseFetcher{err: errors.New("network unreachable")})
		},
	}
	scenario.Test(t)
}

// countingFetcher is a LatestReleaseFetcher that counts how many times
// FetchLatest was actually invoked, to prove handleLatestVersion's cache
// avoids a second upstream call within the TTL.
type countingFetcher struct {
	version     string
	publishedAt string
	calls       int
}

func (f *countingFetcher) FetchLatest(ctx context.Context) (string, string, error) {
	f.calls++
	return f.version, f.publishedAt, nil
}

// newUpdateStartedResponse returns the canned COMMAND_RESPONSE
// respondingCommandSender (docker_routes_test.go) sends back for a
// dispatched "update" command — a successful immediate "started" ack.
func newUpdateStartedResponse() *protocol.CommandResponsePayload {
	return &protocol.CommandResponsePayload{OK: true, Stage: "started"}
}
