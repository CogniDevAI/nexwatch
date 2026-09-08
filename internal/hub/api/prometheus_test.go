package api

import (
	"bufio"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/CogniDevAI/nexwatch/internal/hub/agenttoken"
	"github.com/CogniDevAI/nexwatch/internal/hub/checks"
	"github.com/CogniDevAI/nexwatch/internal/hub/metrics"
)

// snapshotCheckRunner is a publicStatusChecker with a fixed, pre-seeded
// Snapshot map — used where a test needs the exposition's per-check
// families (nexwatch_check_up/latency/tls_expiry) to actually emit a
// sample, unlike route_authz_test.go's fakeCheckRunner (always empty).
type snapshotCheckRunner map[string]checks.Snapshot

func (s snapshotCheckRunner) Snapshot() map[string]checks.Snapshot { return s }

func TestPrometheus_DisabledReturns404(t *testing.T) {
	app := mustNewApp(t)

	scenario := tests.ApiScenario{
		Name:            "GET /metrics with no token configured",
		Method:          http.MethodGet,
		URL:             "/metrics",
		ExpectedStatus:  http.StatusNotFound,
		ExpectedContent: []string{"not enabled"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			RegisterPrometheusRoute(e, fakeCheckRunner{app: app}, metrics.NewService(app))
		},
	}
	scenario.Test(t)
}

func TestPrometheus_WrongTokenReturns401(t *testing.T) {
	app := mustNewApp(t)
	_, hash, err := agenttoken.Generate()
	if err != nil {
		t.Fatalf("agenttoken.Generate(): %v", err)
	}
	mustSetSetting(t, app, prometheusTokenSettingKey, hash)

	scenario := tests.ApiScenario{
		Name:            "GET /metrics with an invalid bearer token",
		Method:          http.MethodGet,
		URL:             "/metrics",
		Headers:         map[string]string{"Authorization": "Bearer not-the-right-token"},
		ExpectedStatus:  http.StatusUnauthorized,
		ExpectedContent: []string{"invalid or missing bearer token"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			RegisterPrometheusRoute(e, fakeCheckRunner{app: app}, metrics.NewService(app))
		},
	}
	scenario.Test(t)
}

func TestPrometheus_MissingTokenReturns401(t *testing.T) {
	app := mustNewApp(t)
	_, hash, err := agenttoken.Generate()
	if err != nil {
		t.Fatalf("agenttoken.Generate(): %v", err)
	}
	mustSetSetting(t, app, prometheusTokenSettingKey, hash)

	scenario := tests.ApiScenario{
		Name:            "GET /metrics with no Authorization header",
		Method:          http.MethodGet,
		URL:             "/metrics",
		ExpectedStatus:  http.StatusUnauthorized,
		ExpectedContent: []string{"invalid or missing bearer token"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			RegisterPrometheusRoute(e, fakeCheckRunner{app: app}, metrics.NewService(app))
		},
	}
	scenario.Test(t)
}

// sampleLineRE matches one Prometheus text-exposition sample line: a metric
// name, an optional {label="value",...} block, a space, and a numeric
// value. Used to assert every non-comment, non-blank line of GET /metrics'
// body actually parses as a valid sample.
var sampleLineRE = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*(\{[a-zA-Z_][a-zA-Z0-9_]*="([^"\\]|\\.)*"(,[a-zA-Z_][a-zA-Z0-9_]*="([^"\\]|\\.)*")*\})? [+-]?[0-9]+(\.[0-9]+)?$`)

func TestPrometheus_ValidTokenReturnsParseableExposition(t *testing.T) {
	app := mustNewApp(t)
	plaintext, hash, err := agenttoken.Generate()
	if err != nil {
		t.Fatalf("agenttoken.Generate(): %v", err)
	}
	mustSetSetting(t, app, prometheusTokenSettingKey, hash)

	agent := mustSaveAgent(t, app, "prom-agent-01")
	check := mustSaveCheck(t, app, "prom-check-01")
	runner := snapshotCheckRunner{
		check.Id: checks.Snapshot{Status: "up", LatencyMs: 42.5},
	}

	var body string
	scenario := tests.ApiScenario{
		Name:            "GET /metrics with a valid bearer token",
		Method:          http.MethodGet,
		URL:             "/metrics",
		Headers:         map[string]string{"Authorization": "Bearer " + plaintext},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{"nexwatch_hub_uptime_seconds", "nexwatch_agent_up", agent.Id},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			RegisterPrometheusRoute(e, runner, metrics.NewService(app))
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			if ct := res.Header.Get("Content-Type"); ct != "text/plain; version=0.0.4" {
				t.Errorf("Content-Type = %q, want %q", ct, "text/plain; version=0.0.4")
			}
			raw, err := io.ReadAll(res.Body)
			if err != nil {
				t.Fatalf("read response body: %v", err)
			}
			body = string(raw)
		},
	}
	scenario.Test(t)

	if body == "" {
		t.Fatal("response body was empty")
	}

	seenHelp := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(body))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "# HELP "):
			name := strings.Fields(line)[2]
			seenHelp[name] = true
			continue
		case strings.HasPrefix(line, "# TYPE "):
			fields := strings.Fields(line)
			if len(fields) != 4 || (fields[3] != "gauge" && fields[3] != "counter") {
				t.Errorf("line %d: malformed TYPE line: %q", lineNum, line)
			}
			continue
		}
		if !sampleLineRE.MatchString(line) {
			t.Errorf("line %d does not look like a valid sample: %q", lineNum, line)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan response body: %v", err)
	}

	for _, name := range []string{
		"nexwatch_hub_uptime_seconds", "nexwatch_agents_online", "nexwatch_agents_total",
		"nexwatch_agent_up", "nexwatch_check_up",
	} {
		if !seenHelp[name] {
			t.Errorf("missing # HELP line for %s", name)
		}
	}

	if !strings.Contains(body, `check_id="`+check.Id+`"`) {
		t.Errorf("expected a nexwatch_check_up/latency sample for check %s", check.Id)
	}
	if !strings.Contains(body, "nexwatch_check_latency_milliseconds") {
		t.Error("missing nexwatch_check_latency_milliseconds series")
	}
}

func TestPrometheus_LabelValueEscaping(t *testing.T) {
	got := escapePromLabelValue(`back\slash "quoted" and` + "\nnewline")
	want := `back\\slash \"quoted\" and\nnewline`
	if got != want {
		t.Errorf("escapePromLabelValue() = %q, want %q", got, want)
	}
}
