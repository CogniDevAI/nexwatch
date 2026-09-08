package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func mustSavePushSubscription(t testing.TB, app core.App, userID, endpoint string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("push_subscriptions")
	if err != nil {
		t.Fatalf("find push_subscriptions collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("user_id", userID)
	rec.Set("endpoint", endpoint)
	rec.Set("p256dh", "BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM")
	rec.Set("auth", "tBHItJI5svbpez7KI4CCXg")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save push subscription: %v", err)
	}
	return rec
}

func TestPushRoutes_VAPIDPublicKey_GeneratesAndIsStable(t *testing.T) {
	app := mustNewApp(t)
	viewer := mustSaveUserWithRole(t, app, "push-viewer@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	var firstKey string
	scenario := tests.ApiScenario{
		Name:            "GET /api/custom/push/vapid-public-key (first call generates)",
		Method:          http.MethodGet,
		URL:             "/api/custom/push/vapid-public-key",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"public_key":`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		// This test issues a second request against the same app below to
		// assert key stability, so the builtin per-scenario cleanup must be
		// deferred to the second (and last) scenario.Test() call.
		DisableTestAppCleanup: true,
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			apiGroup := e.Router.Group("/api/custom")
			apiGroup.Bind(apis.RequireAuth())
			RegisterPushRoutes(apiGroup)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			var body struct {
				PublicKey string `json:"public_key"`
			}
			defer func() { _ = res.Body.Close() }()
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.PublicKey == "" {
				t.Fatal("public_key was empty")
			}
			firstKey = body.PublicKey
		},
	}
	scenario.Test(t)

	scenario2 := tests.ApiScenario{
		Name:            "GET /api/custom/push/vapid-public-key (second call reuses)",
		Method:          http.MethodGet,
		URL:             "/api/custom/push/vapid-public-key",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"public_key":`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			apiGroup := e.Router.Group("/api/custom")
			apiGroup.Bind(apis.RequireAuth())
			RegisterPushRoutes(apiGroup)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			var body struct {
				PublicKey string `json:"public_key"`
			}
			defer func() { _ = res.Body.Close() }()
			if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.PublicKey != firstKey {
				t.Errorf("public_key changed between calls: %q != %q, want a stable generated-once pair", body.PublicKey, firstKey)
			}
		},
	}
	scenario2.Test(t)
}

func TestPushRoutes_TestPush_NoSubscriptionsReturnsEmptyResults(t *testing.T) {
	app := mustNewApp(t)
	viewer := mustSaveUserWithRole(t, app, "push-test-empty@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	scenario := tests.ApiScenario{
		Name:            "POST /api/custom/push/test with no subscriptions",
		Method:          http.MethodPost,
		URL:             "/api/custom/push/test",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"results":[]`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			apiGroup := e.Router.Group("/api/custom")
			apiGroup.Bind(apis.RequireAuth())
			RegisterPushRoutes(apiGroup)
		},
	}
	scenario.Test(t)
}

func TestPushRoutes_TestPush_SendsToOwnSubscriptionOnly(t *testing.T) {
	app := mustNewApp(t)
	viewer := mustSaveUserWithRole(t, app, "push-test-owner@example.com", "viewer")
	other := mustSaveUserWithRole(t, app, "push-test-other@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	mustSavePushSubscription(t, app, viewer.Id, server.URL+"/push/mine")
	mustSavePushSubscription(t, app, other.Id, server.URL+"/push/not-mine")

	// handleSendTestPush builds its own push.NewSender(e.App) with a real
	// *http.Client — httptest.Server accepts plain HTTP from any client,
	// so no client-injection seam is needed for this route-level test; it
	// exercises the real code path exactly as production would.
	scenario := tests.ApiScenario{
		Name:            "POST /api/custom/push/test only reaches the caller's own subscription",
		Method:          http.MethodPost,
		URL:             "/api/custom/push/test",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"success":true`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			apiGroup := e.Router.Group("/api/custom")
			apiGroup.Bind(apis.RequireAuth())
			RegisterPushRoutes(apiGroup)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if requestCount != 1 {
				t.Errorf("push service received %d requests, want exactly 1 (only the caller's own subscription)", requestCount)
			}
		},
	}
	scenario.Test(t)
}
