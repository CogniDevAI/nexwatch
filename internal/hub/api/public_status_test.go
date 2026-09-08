package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// mustSetSetting upserts a "settings" record with a JSON-encoded value,
// bypassing HTTP/API rules the same way setSettingValue does in
// production code — used here to seed settings before a scenario runs.
func mustSetSetting(t testing.TB, app core.App, key string, value any) {
	t.Helper()
	if err := setSettingValue(app, key, value); err != nil {
		t.Fatalf("setSettingValue(%q): %v", key, err)
	}
}

func TestPublicStatus_DisabledReturns404(t *testing.T) {
	app := mustNewApp(t)

	scenario := tests.ApiScenario{
		Name:            "GET /api/public/status while disabled",
		Method:          http.MethodGet,
		URL:             "/api/public/status",
		ExpectedStatus:  http.StatusNotFound,
		ExpectedContent: []string{"not enabled"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			RegisterPublicStatusRoute(e, fakeCheckRunner{app: app})
		},
	}
	scenario.Test(t)
}

func TestPublicStatus_EnabledReturnsOnlyAllowlistedItemsNoLeakedFields(t *testing.T) {
	app := mustNewApp(t)

	check := mustSaveCheck(t, app, "internal-billing-api")
	// The check's real target must never appear in the public response.
	check.Set("target", "10.0.0.55:5432")
	if err := app.Save(check); err != nil {
		t.Fatalf("save check target: %v", err)
	}
	agent := mustSaveAgent(t, app, "internal-hostname-web-01")

	mustSetSetting(t, app, statusPageEnabledKey, true)
	mustSetSetting(t, app, statusPageTitleKey, "Acme status")
	mustSetSetting(t, app, statusPageItemsKey, []statusPageItemConfig{
		{Type: "check", ID: check.Id, Label: "API"},
		{Type: "agent", ID: agent.Id, Label: "Web tier"},
	})

	scenario := tests.ApiScenario{
		Name:           "GET /api/public/status while enabled",
		Method:         http.MethodGet,
		URL:            "/api/public/status",
		ExpectedStatus: http.StatusOK,
		ExpectedContent: []string{
			`"title":"Acme status"`,
			`"API"`,
			`"Web tier"`,
		},
		NotExpectedContent: []string{
			// Never leak the raw check target, agent id/hostname, or check
			// id/name — only the admin-typed label may appear.
			"10.0.0.55",
			"internal-billing-api",
			"internal-hostname-web-01",
			check.Id,
			agent.Id,
		},
		TestAppFactory: func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			RegisterPublicStatusRoute(e, fakeCheckRunner{app: app})
		},
	}
	scenario.Test(t)
}

func TestPublicStatus_RateLimitReturns429(t *testing.T) {
	app := mustNewApp(t)
	mustSetSetting(t, app, statusPageEnabledKey, true)

	// httptest.NewRequest (used internally by tests.ApiScenario) always
	// sets RemoteAddr to "192.0.2.1:1234", so pre-seeding that IP's hit
	// history to the limit deterministically forces the very next request
	// to be rejected.
	limiter := newIPRateLimiter()
	now := time.Now()
	for i := 0; i < statusPageRateLimitPerMin; i++ {
		limiter.hits["192.0.2.1"] = append(limiter.hits["192.0.2.1"], now)
	}

	scenario := tests.ApiScenario{
		Name:            "GET /api/public/status over the rate limit",
		Method:          http.MethodGet,
		URL:             "/api/public/status",
		ExpectedStatus:  http.StatusTooManyRequests,
		ExpectedContent: []string{"too many requests"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerPublicStatusRoute(e, fakeCheckRunner{app: app}, limiter)
		},
	}
	scenario.Test(t)
}

func TestIPRateLimiter_AllowsUpToLimitThenBlocks(t *testing.T) {
	limiter := newIPRateLimiter()

	for i := 0; i < 5; i++ {
		if !limiter.Allow("1.2.3.4", 5, time.Minute) {
			t.Fatalf("request %d should have been allowed", i+1)
		}
	}
	if limiter.Allow("1.2.3.4", 5, time.Minute) {
		t.Fatal("6th request within the window should have been blocked")
	}

	// A distinct IP has its own independent budget.
	if !limiter.Allow("5.6.7.8", 5, time.Minute) {
		t.Fatal("a different IP should not share the exhausted budget")
	}
}

func TestIPRateLimiter_WindowExpiryAllowsMoreRequests(t *testing.T) {
	limiter := newIPRateLimiter()
	past := time.Now().Add(-2 * time.Minute)
	limiter.hits["9.9.9.9"] = []time.Time{past, past, past}

	if !limiter.Allow("9.9.9.9", 3, time.Minute) {
		t.Fatal("expired hits outside the window should not count against the limit")
	}
}
