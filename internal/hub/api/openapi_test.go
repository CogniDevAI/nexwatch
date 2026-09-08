package api

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"gopkg.in/yaml.v3"

	"github.com/CogniDevAI/nexwatch/docs"
	"github.com/CogniDevAI/nexwatch/internal/hub/commands"
	"github.com/CogniDevAI/nexwatch/internal/hub/metrics"
	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

// openAPIDocument is the minimal shape needed to assert path coverage
// without pulling in a full OpenAPI validation library.
type openAPIDocument struct {
	OpenAPI string         `yaml:"openapi"`
	Paths   map[string]any `yaml:"paths"`
}

// TestOpenAPISpec_ParsesAsYAML asserts the embedded document is
// well-formed YAML with the expected top-level OpenAPI shape.
func TestOpenAPISpec_ParsesAsYAML(t *testing.T) {
	var doc openAPIDocument
	if err := yaml.Unmarshal(docs.OpenAPISpec, &doc); err != nil {
		t.Fatalf("docs/openapi.yaml did not parse as YAML: %v", err)
	}
	if doc.OpenAPI == "" {
		t.Error("docs/openapi.yaml has no top-level 'openapi' version field")
	}
	if len(doc.Paths) == 0 {
		t.Fatal("docs/openapi.yaml has no 'paths' entries")
	}
}

// TestOpenAPISpec_DocumentsEveryRegisteredRoute is the regression test for
// undocumented routes: it derives the actual set of registered
// "/api/custom/*" routes from RegisterRoutes/RegisterAlertRoutes — the same
// functions cmd/hub/main.go calls in production — plus the separately
// registered "/healthz", and fails if any of them is missing from
// docs/openapi.yaml's "paths" map. Adding a new route without documenting
// it now breaks this test instead of silently drifting out of sync.
func TestOpenAPISpec_DocumentsEveryRegisteredRoute(t *testing.T) {
	var doc openAPIDocument
	if err := yaml.Unmarshal(docs.OpenAPISpec, &doc); err != nil {
		t.Fatalf("docs/openapi.yaml did not parse as YAML: %v", err)
	}

	app := mustNewApp(t)
	admin := mustSaveUserWithRole(t, app, "openapi-check@example.com", "admin")
	token := mustAuthToken(t, admin)

	var registered []RegisteredRoute

	scenario := tests.ApiScenario{
		Name:            "GET /api/custom/openapi.yaml",
		Method:          http.MethodGet,
		URL:             "/api/custom/openapi.yaml",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{"openapi:"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			metricsSvc := metrics.NewService(app)
			apiGroup := e.Router.Group("/api/custom")
			apiGroup.Bind(apis.RequireAuth())
			registered = RegisterRoutes(e, apiGroup, metricsSvc, fakeCommandSender{})
			registered = append(registered, RegisterAlertRoutes(apiGroup, notify.NewService(app))...)
			registered = append(registered, RegisterChecksRoutes(apiGroup, fakeCheckRunner{app: app})...)
			registered = append(registered, RegisterDockerRoutes(apiGroup, commands.NewBroker(), fakeCommandSender{})...)
			registered = append(registered, RegisterUpdateRoutes(apiGroup, commands.NewBroker(), fakeCommandSender{}, fakeLatestReleaseFetcher{})...)
			registered = append(registered, RegisterLogsRoutes(apiGroup)...)
			registered = append(registered, RegisterPrometheusTokenRoute(apiGroup)...)
			registered = append(registered, RegisterReportRoutes(apiGroup)...)
		},
	}
	scenario.Test(t)

	// /healthz is registered directly on the top-level router (outside
	// "/api/custom") by RegisterHealthRoute, so it is not part of the
	// RegisterRoutes/RegisterAlertRoutes return value — asserted separately.
	registered = append(registered, RegisteredRoute{Method: http.MethodGet, Path: "/healthz"})

	if len(registered) == 0 {
		t.Fatal("no routes were registered — test setup is broken")
	}

	var missing []string
	for _, r := range registered {
		if _, ok := doc.Paths[r.Path]; !ok {
			missing = append(missing, r.Method+" "+r.Path)
		}
	}
	if len(missing) > 0 {
		t.Errorf("docs/openapi.yaml is missing %d registered route(s): %v", len(missing), missing)
	}
}
