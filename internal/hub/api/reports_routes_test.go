package api

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func registerReportRoutesForTest(app core.App, e *core.ServeEvent) {
	apiGroup := e.Router.Group("/api/custom")
	apiGroup.Bind(apis.RequireAuth())
	RegisterReportRoutes(apiGroup)
}

func TestReportPreview_OperatorCannotAccess(t *testing.T) {
	app := mustNewApp(t)
	operator := mustSaveUserWithRole(t, app, "operator-report@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator GET /api/custom/reports/preview",
		Method:          http.MethodGet,
		URL:             "/api/custom/reports/preview",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"requires a higher role"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerReportRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestReportPreview_AdminGetsSubjectHTMLAndText(t *testing.T) {
	app := mustNewApp(t)
	mustSaveAgent(t, app, "web-01")
	admin := mustSaveUserWithRole(t, app, "admin-report@example.com", "admin")
	token := mustAuthToken(t, admin)

	scenario := tests.ApiScenario{
		Name:            "admin GET /api/custom/reports/preview",
		Method:          http.MethodGet,
		URL:             "/api/custom/reports/preview?period_days=7",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"subject":`, `"html":`, `"text":`, "NexWatch weekly report"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerReportRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestReportPreview_InvalidPeriodDaysReturns400(t *testing.T) {
	app := mustNewApp(t)
	admin := mustSaveUserWithRole(t, app, "admin-report-bad@example.com", "admin")
	token := mustAuthToken(t, admin)

	scenario := tests.ApiScenario{
		Name:            "admin GET /api/custom/reports/preview with a bad period_days",
		Method:          http.MethodGet,
		URL:             "/api/custom/reports/preview?period_days=not-a-number",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusBadRequest,
		ExpectedContent: []string{"period_days must be a positive integer"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerReportRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestReportSend_OperatorCannotAccess(t *testing.T) {
	app := mustNewApp(t)
	operator := mustSaveUserWithRole(t, app, "operator-report-send@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST /api/custom/reports/send",
		Method:          http.MethodPost,
		URL:             "/api/custom/reports/send",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"requires a higher role"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerReportRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}

func TestReportSend_AdminWithNoChannelsConfiguredReturnsEmptyResults(t *testing.T) {
	app := mustNewApp(t)
	admin := mustSaveUserWithRole(t, app, "admin-report-send@example.com", "admin")
	token := mustAuthToken(t, admin)

	scenario := tests.ApiScenario{
		Name:            "admin POST /api/custom/reports/send with no channels configured",
		Method:          http.MethodPost,
		URL:             "/api/custom/reports/send",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"subject":`, `"results":[]`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerReportRoutesForTest(app, e)
		},
	}
	scenario.Test(t)
}
