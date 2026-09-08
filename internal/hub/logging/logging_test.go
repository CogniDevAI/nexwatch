package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

func TestResolveFormat(t *testing.T) {
	tests := []struct {
		name string
		flag string
		env  string
		want string
	}{
		{"flag wins over env", "json", "text", "json"},
		{"env used when flag is empty", "", "json", "json"},
		{"defaults to text when neither is set", "", "", "text"},
		{"unrecognized env value passed through (NewHandler decides fallback)", "", "yaml", "yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvLogFormat, tt.env)
			if got := ResolveFormat(tt.flag); got != tt.want {
				t.Errorf("ResolveFormat(%q) with env=%q = %q, want %q", tt.flag, tt.env, got, tt.want)
			}
		})
	}
}

func TestNewHandler_SelectsFormatByOutputShape(t *testing.T) {
	t.Run("json format produces parseable JSON lines", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(NewHandler("json", &buf))
		logger.Info("hello", "agent_id", "a1")

		var decoded map[string]any
		if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
			t.Fatalf("json handler output did not parse as JSON: %v\noutput: %s", err, buf.String())
		}
		if decoded["msg"] != "hello" || decoded["agent_id"] != "a1" {
			t.Errorf("decoded JSON = %+v, want msg=hello and agent_id=a1", decoded)
		}
	})

	t.Run("text format (default) produces key=value text, not JSON", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(NewHandler("text", &buf))
		logger.Info("hello", "agent_id", "a1")

		out := buf.String()
		if strings.HasPrefix(strings.TrimSpace(out), "{") {
			t.Errorf("text handler output looked like JSON: %s", out)
		}
		if !strings.Contains(out, "agent_id=a1") {
			t.Errorf("text handler output = %q, want it to contain agent_id=a1", out)
		}
	})

	t.Run("unrecognized format falls back to text", func(t *testing.T) {
		var buf bytes.Buffer
		logger := slog.New(NewHandler("bogus", &buf))
		logger.Info("hello")

		if strings.HasPrefix(strings.TrimSpace(buf.String()), "{") {
			t.Errorf("unrecognized format output looked like JSON: %s", buf.String())
		}
	})
}

func TestGenerateRequestID_ReturnsDistinctNonEmptyValues(t *testing.T) {
	a := GenerateRequestID()
	b := GenerateRequestID()

	if a == "" || b == "" {
		t.Fatal("GenerateRequestID() returned an empty value")
	}
	if a == b {
		t.Fatalf("GenerateRequestID() returned the same value twice: %q", a)
	}
}

func TestRequestIDMiddleware_GeneratesAndEchoesRequestID(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)

	var buf bytes.Buffer
	logger := slog.New(NewHandler("json", &buf))

	scenario := tests.ApiScenario{
		Name:            "GET /ping without an incoming X-Request-ID",
		Method:          http.MethodGet,
		URL:             "/ping",
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"pong":"ok"`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			e.Router.BindFunc(RequestIDMiddleware(logger))
			e.Router.GET("/ping", func(e *core.RequestEvent) error {
				return e.JSON(http.StatusOK, map[string]string{"pong": "ok"})
			})
		},
	}
	scenario.Test(t)

	if strings.TrimSpace(buf.String()) == "" {
		t.Fatal("RequestIDMiddleware did not log an access line")
	}
	var logLine map[string]any
	// Only one line should have been written for this single request.
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &logLine); err != nil {
		t.Fatalf("access log line did not parse as JSON: %v\nline: %s", err, buf.String())
	}
	if logLine["method"] != http.MethodGet || logLine["path"] != "/ping" {
		t.Errorf("access log line = %+v, want method=GET path=/ping", logLine)
	}
	if _, ok := logLine["request_id"].(string); !ok || logLine["request_id"] == "" {
		t.Errorf("access log line request_id = %+v, want a non-empty generated value", logLine["request_id"])
	}
}

func TestRequestIDMiddleware_PropagatesIncomingRequestID(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)

	var buf bytes.Buffer
	logger := slog.New(NewHandler("json", &buf))

	const incomingID = "caller-supplied-id-123"

	var capturedHeader string
	scenario := tests.ApiScenario{
		Name:            "GET /ping with an incoming X-Request-ID",
		Method:          http.MethodGet,
		URL:             "/ping",
		Headers:         map[string]string{"X-Request-ID": incomingID},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"pong":"ok"`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			e.Router.BindFunc(RequestIDMiddleware(logger))
			e.Router.GET("/ping", func(e *core.RequestEvent) error {
				capturedHeader = e.Response.Header().Get("X-Request-ID")
				return e.JSON(http.StatusOK, map[string]string{"pong": "ok"})
			})
		},
	}
	scenario.Test(t)

	if capturedHeader != incomingID {
		t.Errorf("response X-Request-ID = %q, want the propagated caller value %q", capturedHeader, incomingID)
	}

	var logLine map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &logLine); err != nil {
		t.Fatalf("access log line did not parse as JSON: %v\nline: %s", err, buf.String())
	}
	if logLine["request_id"] != incomingID {
		t.Errorf("access log line request_id = %v, want %q", logLine["request_id"], incomingID)
	}
}
