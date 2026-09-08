package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

func registerAlertRoutesForTest(app core.App, e *core.ServeEvent) {
	apiGroup := e.Router.Group("/api/custom")
	apiGroup.Bind(apis.RequireAuth())
	RegisterAlertRoutes(apiGroup, notify.NewService(app))
}

// mustSaveSilence inserts a "silences" record covering [startsAt, endsAt),
// optionally scoped to checkIDs — the check-based counterpart of an
// agent_id/tags scope, exercised here for GET /api/custom/silences/active's
// covered_check_ids field.
func mustSaveSilence(t testing.TB, app core.App, checkIDs []string, startsAt, endsAt time.Time) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("silences")
	if err != nil {
		t.Fatalf("find silences collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "test-silence")
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

// TestHandleActiveSilences_ReturnsCoveredCheckIDs pins the F1-P1 fix adding
// check-based silence coverage: a silence scoped to one check via
// check_ids must report that check's id in covered_check_ids, and must NOT
// report every agent as covered (a check_ids-only silence is not global —
// see alerts.SilenceMatchesAgent's doc comment).
func TestHandleActiveSilences_ReturnsCoveredCheckIDs(t *testing.T) {
	app := mustNewApp(t)
	check := mustSaveCheck(t, app, "billing-api")
	agent := mustSaveAgent(t, app, "host-silence-active")
	now := time.Now()
	mustSaveSilence(t, app, []string{check.Id}, now.Add(-time.Hour), now.Add(time.Hour))

	viewer := mustSaveUserWithRole(t, app, "viewer-silences@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	scenario := tests.ApiScenario{
		Name:            "viewer GET /api/custom/silences/active with a check-scoped silence",
		Method:          http.MethodGet,
		URL:             "/api/custom/silences/active",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"covered_check_ids"`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerAlertRoutesForTest(app, e)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			defer func() { _ = res.Body.Close() }()
			var body struct {
				Silences []struct {
					CoveredAgentIDs []string `json:"covered_agent_ids"`
					CoveredCheckIDs []string `json:"covered_check_ids"`
				} `json:"silences"`
			}
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatalf("decode response body: %v", err)
			}
			if len(body.Silences) != 1 {
				t.Fatalf("silences count = %d, want 1", len(body.Silences))
			}
			s := body.Silences[0]
			if len(s.CoveredCheckIDs) != 1 || s.CoveredCheckIDs[0] != check.Id {
				t.Errorf("covered_check_ids = %v, want [%s]", s.CoveredCheckIDs, check.Id)
			}
			if len(s.CoveredAgentIDs) != 0 {
				t.Errorf("covered_agent_ids = %v, want empty (a check_ids-only silence must not cover agent %s)", s.CoveredAgentIDs, agent.Id)
			}
		},
	}
	scenario.Test(t)
}
