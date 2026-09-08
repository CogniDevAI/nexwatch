package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/CogniDevAI/nexwatch/internal/hub/agenttoken"
)

func registerPrometheusTokenRouteForTest(app core.App, e *core.ServeEvent) {
	apiGroup := e.Router.Group("/api/custom")
	apiGroup.Bind(apis.RequireAuth())
	RegisterPrometheusTokenRoute(apiGroup)
}

func TestPrometheusToken_ViewerCannotGenerate(t *testing.T) {
	app := mustNewApp(t)
	viewer := mustSaveUserWithRole(t, app, "viewer-prom@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	scenario := tests.ApiScenario{
		Name:            "viewer POST /api/custom/prometheus/token",
		Method:          http.MethodPost,
		URL:             "/api/custom/prometheus/token",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"requires a higher role"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerPrometheusTokenRouteForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestPrometheusToken_OperatorCannotGenerate(t *testing.T) {
	app := mustNewApp(t)
	operator := mustSaveUserWithRole(t, app, "operator-prom@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST /api/custom/prometheus/token",
		Method:          http.MethodPost,
		URL:             "/api/custom/prometheus/token",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"requires a higher role"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerPrometheusTokenRouteForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestPrometheusToken_AdminGeneratesAndItHashesCorrectly(t *testing.T) {
	app := mustNewApp(t)
	admin := mustSaveUserWithRole(t, app, "admin-prom@example.com", "admin")
	token := mustAuthToken(t, admin)

	scenario := tests.ApiScenario{
		Name:            "admin POST /api/custom/prometheus/token",
		Method:          http.MethodPost,
		URL:             "/api/custom/prometheus/token",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"token":`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerPrometheusTokenRouteForTest(app, e)
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

			stored, ok := getSettingValue(app, prometheusTokenSettingKey)
			if !ok {
				t.Fatal("prometheus_token setting was not stored")
			}
			storedHash, _ := stored.(string)
			wantHash := agenttoken.Hash(body.Token)
			if storedHash != wantHash {
				t.Errorf("stored prometheus_token = %q, want sha256(returned plaintext) = %q", storedHash, wantHash)
			}
			if storedHash == body.Token {
				t.Error("prometheus_token setting stored the plaintext token instead of its hash")
			}
		},
	}
	scenario.Test(t)
}
