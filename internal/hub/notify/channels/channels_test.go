package channels

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
	// Registers the app's migrations so tests.NewTestApp() creates the
	// notification_channels/alerts collections these notifiers read from.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
)

func newTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

func createChannel(t *testing.T, app core.App, chType string, config map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("notification_channels")
	if err != nil {
		t.Fatalf("find notification_channels collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "test-"+chType)
	rec.Set("type", chType)
	rec.Set("enabled", true)
	if config != nil {
		rec.Set("config", config)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save channel: %v", err)
	}
	return rec
}

// createAlert saves a rule ("r", severity critical), an agent ("host-x",
// no tags), and a firing/resolved alert linking the two, then returns the
// alert plus the AlertContext notify.BuildAlertContext would resolve for
// it — the same context Service.dispatchToChannels builds in production —
// so every notifier test exercises Send with a realistic, non-empty
// context by default.
func createAlert(t *testing.T, app core.App, status string, value float64, message string) (*core.Record, notify.AlertContext) {
	t.Helper()
	rulesCol, err := app.FindCollectionByNameOrId("alert_rules")
	if err != nil {
		t.Fatalf("find alert_rules collection: %v", err)
	}
	rule := core.NewRecord(rulesCol)
	rule.Set("name", "r")
	rule.Set("metric_type", "cpu")
	rule.Set("condition", "gt")
	rule.Set("threshold", 80)
	rule.Set("duration", 1)
	rule.Set("severity", "critical")
	rule.Set("enabled", true)
	if err := app.Save(rule); err != nil {
		t.Fatalf("save rule: %v", err)
	}

	agentsCol, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	agent := core.NewRecord(agentsCol)
	agent.Set("hostname", "host-x")
	agent.Set("status", "online")
	if err := app.Save(agent); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	alertsCol, err := app.FindCollectionByNameOrId("alerts")
	if err != nil {
		t.Fatalf("find alerts collection: %v", err)
	}
	alert := core.NewRecord(alertsCol)
	alert.Set("rule_id", rule.Id)
	alert.Set("agent_id", agent.Id)
	alert.Set("status", status)
	alert.Set("value", value)
	alert.Set("message", message)
	alert.Set("fired_at", "2026-01-01 00:00:00.000Z")
	if err := app.Save(alert); err != nil {
		t.Fatalf("save alert: %v", err)
	}
	return alert, notify.BuildAlertContext(app, alert, rule)
}

// createAlertWithTags is createAlert plus tags on the agent, so tests can
// assert a channel renders AlertContext.AgentTags.
func createAlertWithTags(t *testing.T, app core.App, status string, value float64, message string, tags []string) (*core.Record, notify.AlertContext) {
	t.Helper()
	alert, _ := createAlert(t, app, status, value, message)

	agent, err := app.FindRecordById("agents", alert.GetString("agent_id"))
	if err != nil {
		t.Fatalf("find agent: %v", err)
	}
	agent.Set("tags", tags)
	if err := app.Save(agent); err != nil {
		t.Fatalf("save agent tags: %v", err)
	}

	rule, err := app.FindRecordById("alert_rules", alert.GetString("rule_id"))
	if err != nil {
		t.Fatalf("find rule: %v", err)
	}
	return alert, notify.BuildAlertContext(app, alert, rule)
}

// createCheckAlert saves a check_down rule, a "checks" record, and a
// check-based alert (check_id set, agent_id empty), returning the alert and
// its resolved AlertContext — used by tests asserting a channel shows
// AlertContext.CheckName/CheckTarget instead of an agent hostname.
func createCheckAlert(t *testing.T, app core.App, status, message string) (*core.Record, notify.AlertContext) {
	t.Helper()

	checksCol, err := app.FindCollectionByNameOrId("checks")
	if err != nil {
		t.Fatalf("find checks collection: %v", err)
	}
	check := core.NewRecord(checksCol)
	check.Set("name", "billing-api")
	check.Set("type", "http")
	check.Set("target", "https://billing.internal/health")
	if err := app.Save(check); err != nil {
		t.Fatalf("save check: %v", err)
	}

	rulesCol, err := app.FindCollectionByNameOrId("alert_rules")
	if err != nil {
		t.Fatalf("find alert_rules collection: %v", err)
	}
	rule := core.NewRecord(rulesCol)
	rule.Set("name", "check-down-rule")
	rule.Set("metric_type", "check_down")
	rule.Set("check_id", check.Id)
	rule.Set("condition", "gt")
	rule.Set("threshold", 1)
	rule.Set("duration", 1)
	rule.Set("severity", "critical")
	rule.Set("enabled", true)
	if err := app.Save(rule); err != nil {
		t.Fatalf("save rule: %v", err)
	}

	alertsCol, err := app.FindCollectionByNameOrId("alerts")
	if err != nil {
		t.Fatalf("find alerts collection: %v", err)
	}
	alert := core.NewRecord(alertsCol)
	alert.Set("rule_id", rule.Id)
	alert.Set("check_id", check.Id)
	alert.Set("status", status)
	alert.Set("value", 1)
	alert.Set("message", message)
	alert.Set("fired_at", "2026-01-01 00:00:00.000Z")
	if err := app.Save(alert); err != nil {
		t.Fatalf("save alert: %v", err)
	}
	return alert, notify.BuildAlertContext(app, alert, rule)
}

// withRuleSeverity updates alert's rule to severity and returns the
// AlertContext re-resolved from that change — used where a test needs a
// severity other than createAlert's default "critical" (e.g. to exercise
// the warning-severity path) without duplicating the whole fixture.
func withRuleSeverity(t *testing.T, app core.App, alert *core.Record, severity string) notify.AlertContext {
	t.Helper()
	rule, err := app.FindRecordById("alert_rules", alert.GetString("rule_id"))
	if err != nil {
		t.Fatalf("find rule: %v", err)
	}
	rule.Set("severity", severity)
	if err := app.Save(rule); err != nil {
		t.Fatalf("save rule severity: %v", err)
	}
	return notify.BuildAlertContext(app, alert, rule)
}

// mustSetSetting upserts a "settings" record, following the same tiny
// test-only helper already duplicated in internal/hub/report and
// internal/hub/api (see those packages' own test files) — used here to set
// "public_base_url" so tests can assert on AlertContext.DashboardURL.
func mustSetSetting(t *testing.T, app core.App, key string, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal setting %q: %v", key, err)
	}
	col, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		t.Fatalf("find settings collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("key", key)
	rec.Set("value", string(encoded))
	if err := app.Save(rec); err != nil {
		t.Fatalf("save setting %q: %v", key, err)
	}
}

type capturedRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Body    map[string]any
	// RawBody holds the exact request body bytes, for channels (ntfy) that
	// POST a plain-text body rather than JSON.
	RawBody []byte
}

// newCapturingServer starts an httptest server that records the last
// request it received (method, path, headers, JSON body) and responds with
// the given status and body.
func newCapturingServer(t *testing.T, status int, respBody string) (*httptest.Server, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.Method = r.Method
		captured.Path = r.URL.Path
		captured.Headers = r.Header.Clone()
		data, _ := io.ReadAll(r.Body)
		captured.RawBody = data
		if len(data) > 0 {
			_ = json.Unmarshal(data, &captured.Body)
		}
		w.WriteHeader(status)
		if respBody != "" {
			_, _ = w.Write([]byte(respBody))
		}
	}))
	t.Cleanup(server.Close)
	return server, captured
}

// --- WebhookNotifier -------------------------------------------------

func TestWebhookNotifier_Send_DefaultMethodAndBody(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, "")
	channel := createChannel(t, app, "webhook", map[string]any{"url": server.URL + "/hook"})
	alert, alertCtx := createAlert(t, app, "firing", 95.5, "cpu too high")

	n := NewWebhookNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if captured.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST (default)", captured.Method)
	}
	if captured.Path != "/hook" {
		t.Errorf("Path = %q, want /hook", captured.Path)
	}
	if got := captured.Headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := captured.Headers.Get("User-Agent"); got != "NexWatch/0.1.0" {
		t.Errorf("User-Agent = %q, want NexWatch/0.1.0", got)
	}
	if captured.Body["status"] != "firing" {
		t.Errorf("body.status = %v, want firing", captured.Body["status"])
	}
	if captured.Body["value"] != 95.5 {
		t.Errorf("body.value = %v, want 95.5", captured.Body["value"])
	}
	if captured.Body["alert_id"] != alert.Id {
		t.Errorf("body.alert_id = %v, want %v", captured.Body["alert_id"], alert.Id)
	}
}

func TestWebhookNotifier_Send_CustomMethodAndHeaders(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, "")
	channel := createChannel(t, app, "webhook", map[string]any{
		"url":    server.URL,
		"method": "PUT",
		"headers": map[string]any{
			"X-Custom-Header": "custom-value",
		},
	})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewWebhookNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if captured.Method != http.MethodPut {
		t.Errorf("Method = %q, want PUT", captured.Method)
	}
	if got := captured.Headers.Get("X-Custom-Header"); got != "custom-value" {
		t.Errorf("X-Custom-Header = %q, want custom-value", got)
	}
}

func TestWebhookNotifier_Send_MissingURLReturnsError(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "webhook", map[string]any{})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewWebhookNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
		t.Fatal("Send() with no url configured expected an error, got nil")
	}
}

func TestWebhookNotifier_Send_ServerErrorStatusReturnsError(t *testing.T) {
	app := newTestApp(t)
	server, _ := newCapturingServer(t, http.StatusInternalServerError, "")
	channel := createChannel(t, app, "webhook", map[string]any{"url": server.URL})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewWebhookNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
		t.Fatal("Send() expected an error for a 500 response, got nil")
	}
}

// TestWebhookNotifier_Send_BodyIncludesEnrichedContext covers both an
// agent-based and a check-based alert in one test (sharing a single test
// app/server/channel) rather than two, since spinning up a full PocketBase
// test app is the dominant cost of this package's test suite under
// `go test -race`.
func TestWebhookNotifier_Send_BodyIncludesEnrichedContext(t *testing.T) {
	app := newTestApp(t)
	mustSetSetting(t, app, "public_base_url", "https://nexwatch.example.com")
	server, captured := newCapturingServer(t, http.StatusOK, "")
	channel := createChannel(t, app, "webhook", map[string]any{"url": server.URL})
	n := NewWebhookNotifier()

	t.Run("agent-based alert", func(t *testing.T) {
		alert, alertCtx := createAlertWithTags(t, app, "firing", 1.0, "m", []string{"web", "prod"})

		if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
			t.Fatalf("Send() unexpected error: %v", err)
		}

		ctx, ok := captured.Body["context"].(map[string]any)
		if !ok {
			t.Fatalf("body.context = %v, want an object", captured.Body["context"])
		}
		if ctx["hostname"] != "host-x" {
			t.Errorf("context.hostname = %v, want host-x", ctx["hostname"])
		}
		if ctx["rule_name"] != "r" {
			t.Errorf("context.rule_name = %v, want r", ctx["rule_name"])
		}
		if ctx["severity"] != "critical" {
			t.Errorf("context.severity = %v, want critical", ctx["severity"])
		}
		tags, ok := ctx["agent_tags"].([]any)
		if !ok || len(tags) != 2 {
			t.Fatalf("context.agent_tags = %v, want [web prod]", ctx["agent_tags"])
		}
		if ctx["dashboard_url"] != "https://nexwatch.example.com/servers/"+alert.GetString("agent_id") {
			t.Errorf("context.dashboard_url = %v, want the host page under public_base_url", ctx["dashboard_url"])
		}
	})

	t.Run("check-based alert", func(t *testing.T) {
		alert, alertCtx := createCheckAlert(t, app, "firing", "[critical] check_down: billing-api is down")

		if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
			t.Fatalf("Send() unexpected error: %v", err)
		}

		ctx := captured.Body["context"].(map[string]any)
		if ctx["check_name"] != "billing-api" {
			t.Errorf("context.check_name = %v, want billing-api", ctx["check_name"])
		}
		if ctx["check_target"] != "https://billing.internal/health" {
			t.Errorf("context.check_target = %v, want the check's target", ctx["check_target"])
		}
		if ctx["hostname"] != "" {
			t.Errorf("context.hostname = %v, want empty for a check-based alert", ctx["hostname"])
		}
	})
}

// --- DiscordNotifier ---------------------------------------------------

func TestDiscordNotifier_Send_FiringUsesRedEmbed(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusNoContent, "")
	channel := createChannel(t, app, "discord", map[string]any{"webhook_url": server.URL})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "cpu too high")

	n := NewDiscordNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if captured.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", captured.Method)
	}
	embeds, ok := captured.Body["embeds"].([]any)
	if !ok || len(embeds) != 1 {
		t.Fatalf("body.embeds = %v, want a single-element array", captured.Body["embeds"])
	}
	embed := embeds[0].(map[string]any)
	if embed["color"] != 15158332.0 {
		t.Errorf("embed.color = %v, want 15158332 (red) for firing", embed["color"])
	}
	if !strings.Contains(embed["description"].(string), "cpu too high") {
		t.Errorf("embed.description = %v, want it to contain the alert message", embed["description"])
	}
}

func TestDiscordNotifier_Send_ResolvedUsesGreenEmbed(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusNoContent, "")
	channel := createChannel(t, app, "discord", map[string]any{"webhook_url": server.URL})
	alert, alertCtx := createAlert(t, app, "resolved", 1.0, "back to normal")

	n := NewDiscordNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	embed := captured.Body["embeds"].([]any)[0].(map[string]any)
	if embed["color"] != 3066993.0 {
		t.Errorf("embed.color = %v, want 3066993 (green) for resolved", embed["color"])
	}
}

func TestDiscordNotifier_Send_EmbedIncludesHostRuleTagsAndLink(t *testing.T) {
	app := newTestApp(t)
	mustSetSetting(t, app, "public_base_url", "https://nexwatch.example.com")
	server, captured := newCapturingServer(t, http.StatusNoContent, "")
	channel := createChannel(t, app, "discord", map[string]any{"webhook_url": server.URL})
	alert, alertCtx := createAlertWithTags(t, app, "firing", 1.0, "cpu too high", []string{"web"})

	n := NewDiscordNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	embed := captured.Body["embeds"].([]any)[0].(map[string]any)
	if embed["url"] != "https://nexwatch.example.com/servers/"+alert.GetString("agent_id") {
		t.Errorf("embed.url = %v, want the dashboard host page", embed["url"])
	}
	fields, ok := embed["fields"].([]any)
	if !ok {
		t.Fatalf("embed.fields = %v, want an array", embed["fields"])
	}
	names := map[string]string{}
	for _, f := range fields {
		field := f.(map[string]any)
		names[field["name"].(string)] = field["value"].(string)
	}
	if names["Host"] != "host-x" {
		t.Errorf("Host field = %q, want host-x", names["Host"])
	}
	if names["Rule"] != "r" {
		t.Errorf("Rule field = %q, want r", names["Rule"])
	}
	if names["Tags"] != "web" {
		t.Errorf("Tags field = %q, want web", names["Tags"])
	}
}

func TestDiscordNotifier_Send_MissingWebhookURLReturnsError(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "discord", map[string]any{})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewDiscordNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
		t.Fatal("Send() with no webhook_url configured expected an error, got nil")
	}
}

func TestDiscordNotifier_Send_ErrorStatusReturnsError(t *testing.T) {
	app := newTestApp(t)
	server, _ := newCapturingServer(t, http.StatusBadRequest, `{"message":"bad request"}`)
	channel := createChannel(t, app, "discord", map[string]any{"webhook_url": server.URL})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewDiscordNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
		t.Fatal("Send() expected an error for a 400 response, got nil")
	}
}

// --- TelegramNotifier ----------------------------------------------------

func TestTelegramNotifier_Send_BuildsBotURLAndBody(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, `{"ok":true}`)
	channel := createChannel(t, app, "telegram", map[string]any{
		"bot_token": "123:ABC",
		"chat_id":   "chat-1",
	})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "cpu too high")

	n := NewTelegramNotifier()
	n.client = server.Client()
	n.baseURL = server.URL

	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if captured.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", captured.Method)
	}
	if captured.Path != "/bot123:ABC/sendMessage" {
		t.Errorf("Path = %q, want /bot123:ABC/sendMessage", captured.Path)
	}
	if captured.Body["chat_id"] != "chat-1" {
		t.Errorf("body.chat_id = %v, want chat-1", captured.Body["chat_id"])
	}
	if captured.Body["parse_mode"] != "HTML" {
		t.Errorf("body.parse_mode = %v, want HTML", captured.Body["parse_mode"])
	}
	if !strings.Contains(captured.Body["text"].(string), "cpu too high") {
		t.Errorf("body.text = %v, want it to contain the alert message", captured.Body["text"])
	}
}

func TestTelegramNotifier_Send_NonOKStatusReturnsError(t *testing.T) {
	app := newTestApp(t)
	server, _ := newCapturingServer(t, http.StatusInternalServerError, "")
	channel := createChannel(t, app, "telegram", map[string]any{"bot_token": "123:ABC", "chat_id": "chat-1"})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewTelegramNotifier()
	n.client = server.Client()
	n.baseURL = server.URL

	if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
		t.Fatal("Send() expected an error for a 500 response, got nil")
	}
}

func TestTelegramNotifier_Send_APIOKFalseReturnsError(t *testing.T) {
	app := newTestApp(t)
	server, _ := newCapturingServer(t, http.StatusOK, `{"ok":false,"description":"chat not found"}`)
	channel := createChannel(t, app, "telegram", map[string]any{"bot_token": "123:ABC", "chat_id": "chat-1"})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewTelegramNotifier()
	n.client = server.Client()
	n.baseURL = server.URL

	err := n.Send(context.Background(), alert, channel, alertCtx)
	if err == nil {
		t.Fatal("Send() expected an error when Telegram reports ok=false, got nil")
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Errorf("Send() error = %v, want it to include Telegram's description", err)
	}
}

func TestTelegramNotifier_Send_MissingCredentialsReturnsError(t *testing.T) {
	app := newTestApp(t)

	tests := []struct {
		name   string
		config map[string]any
	}{
		{"missing both", map[string]any{}},
		{"missing chat_id", map[string]any{"bot_token": "123:ABC"}},
		{"missing bot_token", map[string]any{"chat_id": "chat-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := createChannel(t, app, "telegram", tt.config)
			alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

			n := NewTelegramNotifier()
			if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
				t.Fatal("Send() with missing credentials expected an error, got nil")
			}
		})
	}
}

// --- EmailNotifier -------------------------------------------------------

func TestEmailNotifier_Send_AssemblesMessageAndInvokesSendMail(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "email", map[string]any{
		"host": "smtp.example.com",
		"port": 2525.0,
		"from": "alerts@nexwatch.local",
		"to":   "ops@example.com",
	})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "cpu too high")

	var gotAddr, gotFrom string
	var gotTo []string
	var gotMsg []byte
	n := NewEmailNotifier()
	n.sendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotFrom, gotTo, gotMsg = addr, from, to, msg
		return nil
	}

	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if gotAddr != "smtp.example.com:2525" {
		t.Errorf("addr = %q, want smtp.example.com:2525", gotAddr)
	}
	if gotFrom != "alerts@nexwatch.local" {
		t.Errorf("from = %q, want alerts@nexwatch.local", gotFrom)
	}
	if len(gotTo) != 1 || gotTo[0] != "ops@example.com" {
		t.Errorf("to = %v, want [ops@example.com]", gotTo)
	}
	msg := string(gotMsg)
	if !strings.Contains(msg, "From: alerts@nexwatch.local") {
		t.Errorf("message = %q, want a From header", msg)
	}
	if !strings.Contains(msg, "To: ops@example.com") {
		t.Errorf("message = %q, want a To header", msg)
	}
	if !strings.Contains(msg, "Subject: [NexWatch][critical] r on host-x") {
		t.Errorf("message = %q, want the severity/rule/host subject format", msg)
	}
	if !strings.Contains(msg, "cpu too high") {
		t.Errorf("message = %q, want the alert message in the body", msg)
	}
}

func TestEmailNotifier_Send_DefaultsPortTo587WhenZero(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "email", map[string]any{
		"host": "smtp.example.com",
		"from": "a@b.com",
		"to":   "c@d.com",
		// no port
	})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	var gotAddr string
	n := NewEmailNotifier()
	n.sendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr = addr
		return nil
	}

	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}
	if gotAddr != "smtp.example.com:587" {
		t.Errorf("addr = %q, want smtp.example.com:587 (default port)", gotAddr)
	}
}

func TestEmailNotifier_Send_AuthOnlyWhenCredentialsProvided(t *testing.T) {
	app := newTestApp(t)

	t.Run("no credentials means nil auth", func(t *testing.T) {
		channel := createChannel(t, app, "email", map[string]any{
			"host": "smtp.example.com", "from": "a@b.com", "to": "c@d.com",
		})
		alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

		var gotAuth smtp.Auth
		authCaptured := false
		n := NewEmailNotifier()
		n.sendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
			gotAuth = auth
			authCaptured = true
			return nil
		}
		if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
			t.Fatalf("Send() unexpected error: %v", err)
		}
		if !authCaptured {
			t.Fatal("sendMail was not called")
		}
		if gotAuth != nil {
			t.Errorf("auth = %v, want nil when no credentials are configured", gotAuth)
		}
	})

	t.Run("credentials provided means non-nil auth", func(t *testing.T) {
		channel := createChannel(t, app, "email", map[string]any{
			"host": "smtp.example.com", "from": "a@b.com", "to": "c@d.com",
			"username": "user", "password": "pass",
		})
		alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

		var gotAuth smtp.Auth
		n := NewEmailNotifier()
		n.sendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
			gotAuth = auth
			return nil
		}
		if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
			t.Fatalf("Send() unexpected error: %v", err)
		}
		if gotAuth == nil {
			t.Error("auth = nil, want a PlainAuth value when credentials are configured")
		}
	})
}

func TestEmailNotifier_Send_MissingRequiredFieldsReturnsError(t *testing.T) {
	app := newTestApp(t)

	tests := []struct {
		name   string
		config map[string]any
	}{
		{"missing host", map[string]any{"from": "a@b.com", "to": "c@d.com"}},
		{"missing from", map[string]any{"host": "smtp.example.com", "to": "c@d.com"}},
		{"missing to", map[string]any{"host": "smtp.example.com", "from": "a@b.com"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := createChannel(t, app, "email", tt.config)
			alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

			n := NewEmailNotifier()
			n.sendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
				t.Fatal("sendMail must not be called when required config is missing")
				return nil
			}
			if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
				t.Fatal("Send() with missing required config expected an error, got nil")
			}
		})
	}
}

func TestEmailNotifier_Send_ContextCancellationTimesOut(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "email", map[string]any{
		"host": "smtp.example.com", "from": "a@b.com", "to": "c@d.com",
	})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewEmailNotifier()
	block := make(chan struct{})
	n.sendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		<-block // never completes within the test's timeout
		return nil
	}
	defer close(block)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := n.Send(ctx, alert, channel, alertCtx)
	if err == nil {
		t.Fatal("Send() expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("Send() error = %v, want it to mention the timeout", err)
	}
}

// --- SlackNotifier ---------------------------------------------------------

func TestSlackNotifier_Send_FiringUsesRedAttachmentAndHeader(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, "ok")
	channel := createChannel(t, app, "slack", map[string]any{"webhook_url": server.URL})
	alert, alertCtx := createAlert(t, app, "firing", 95.5, "[critical] cpu too high")

	n := NewSlackNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if captured.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", captured.Method)
	}
	if got := captured.Headers.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if !strings.Contains(captured.Body["text"].(string), "cpu too high") {
		t.Errorf("body.text = %v, want it to contain the alert message", captured.Body["text"])
	}

	attachments, ok := captured.Body["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("body.attachments = %v, want a single-element array", captured.Body["attachments"])
	}
	attachment := attachments[0].(map[string]any)
	if attachment["color"] != "#e01e5a" {
		t.Errorf("attachment.color = %v, want red (#e01e5a) for a critical firing alert", attachment["color"])
	}

	blocks, ok := attachment["blocks"].([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("attachment.blocks = %v, want a non-empty array", attachment["blocks"])
	}
	header := blocks[0].(map[string]any)
	if header["type"] != "header" {
		t.Fatalf("blocks[0].type = %v, want header", header["type"])
	}
	headerText := header["text"].(map[string]any)["text"].(string)
	if !strings.Contains(headerText, "CRITICAL") || !strings.Contains(headerText, "FIRING") {
		t.Errorf("header text = %q, want it to mention CRITICAL and FIRING", headerText)
	}
}

func TestSlackNotifier_Send_ResolvedUsesGreenAttachment(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, "ok")
	channel := createChannel(t, app, "slack", map[string]any{"webhook_url": server.URL})
	alert, alertCtx := createAlert(t, app, "resolved", 10, "[critical] back to normal")

	n := NewSlackNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	attachment := captured.Body["attachments"].([]any)[0].(map[string]any)
	if attachment["color"] != "#2eb67d" {
		t.Errorf("attachment.color = %v, want green (#2eb67d) once resolved", attachment["color"])
	}
}

func TestSlackNotifier_Send_FieldsIncludeHostRuleTagsAndLinkButton(t *testing.T) {
	app := newTestApp(t)
	mustSetSetting(t, app, "public_base_url", "https://nexwatch.example.com")
	server, captured := newCapturingServer(t, http.StatusOK, "ok")
	channel := createChannel(t, app, "slack", map[string]any{"webhook_url": server.URL})
	alert, alertCtx := createAlertWithTags(t, app, "firing", 1.0, "[critical] cpu too high", []string{"web", "prod"})

	n := NewSlackNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	attachment := captured.Body["attachments"].([]any)[0].(map[string]any)
	blocks := attachment["blocks"].([]any)

	section := blocks[1].(map[string]any)
	fields := section["fields"].([]any)
	var texts []string
	for _, f := range fields {
		texts = append(texts, f.(map[string]any)["text"].(string))
	}
	joined := strings.Join(texts, "\n")
	if !strings.Contains(joined, "*Host:*\nhost-x") {
		t.Errorf("fields = %v, want a Host field for host-x", texts)
	}
	if !strings.Contains(joined, "*Rule:*\nr") {
		t.Errorf("fields = %v, want a Rule field for r", texts)
	}
	if !strings.Contains(joined, "*Tags:*\nweb, prod") {
		t.Errorf("fields = %v, want a Tags field listing web, prod", texts)
	}

	actionsBlock := blocks[len(blocks)-1].(map[string]any)
	if actionsBlock["type"] != "actions" {
		t.Fatalf("last block type = %v, want actions (the dashboard link button)", actionsBlock["type"])
	}
	elements := actionsBlock["elements"].([]any)
	button := elements[0].(map[string]any)
	if button["url"] != "https://nexwatch.example.com/servers/"+alert.GetString("agent_id") {
		t.Errorf("button.url = %v, want the dashboard host page", button["url"])
	}
}

func TestSlackNotifier_Send_MissingWebhookURLReturnsConfigError(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "slack", map[string]any{})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewSlackNotifier()
	err := n.Send(context.Background(), alert, channel, alertCtx)
	var cfgErr *notify.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Send() error = %v, want a *notify.ConfigError", err)
	}
}

func TestSlackNotifier_Send_ErrorStatusReturnsError(t *testing.T) {
	app := newTestApp(t)
	server, _ := newCapturingServer(t, http.StatusBadRequest, "invalid_payload")
	channel := createChannel(t, app, "slack", map[string]any{"webhook_url": server.URL})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewSlackNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
		t.Fatal("Send() expected an error for a 400 response, got nil")
	}
}

// --- TeamsNotifier -----------------------------------------------------------

func TestTeamsNotifier_Send_BuildsAdaptiveCardWithFactSet(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, "1")
	channel := createChannel(t, app, "teams", map[string]any{"webhook_url": server.URL})
	alert, _ := createAlert(t, app, "firing", 95.5, "[warning] memory elevated")
	alertCtx := withRuleSeverity(t, app, alert, "warning")

	n := NewTeamsNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if captured.Body["type"] != "message" {
		t.Errorf("body.type = %v, want message", captured.Body["type"])
	}
	attachments, ok := captured.Body["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("body.attachments = %v, want a single-element array", captured.Body["attachments"])
	}
	attachment := attachments[0].(map[string]any)
	if attachment["contentType"] != "application/vnd.microsoft.card.adaptive" {
		t.Errorf("attachment.contentType = %v, want application/vnd.microsoft.card.adaptive", attachment["contentType"])
	}

	content := attachment["content"].(map[string]any)
	if content["type"] != "AdaptiveCard" {
		t.Errorf("content.type = %v, want AdaptiveCard", content["type"])
	}
	body := content["body"].([]any)

	var factSet map[string]any
	for _, b := range body {
		block := b.(map[string]any)
		if block["type"] == "FactSet" {
			factSet = block
			break
		}
	}
	if factSet == nil {
		t.Fatal("content.body has no FactSet block")
	}
	facts := factSet["facts"].([]any)
	foundSeverity := false
	for _, f := range facts {
		fact := f.(map[string]any)
		if fact["title"] == "Severity" {
			foundSeverity = true
			if fact["value"] != "warning" {
				t.Errorf("Severity fact value = %v, want warning", fact["value"])
			}
		}
	}
	if !foundSeverity {
		t.Error("FactSet has no Severity fact")
	}
}

func TestTeamsNotifier_Send_ResolvedUsesGoodColor(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, "1")
	channel := createChannel(t, app, "teams", map[string]any{"webhook_url": server.URL})
	alert, alertCtx := createAlert(t, app, "resolved", 1.0, "[critical] back to normal")

	n := NewTeamsNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	content := captured.Body["attachments"].([]any)[0].(map[string]any)["content"].(map[string]any)
	titleBlock := content["body"].([]any)[0].(map[string]any)
	if titleBlock["color"] != "good" {
		t.Errorf("title block color = %v, want good once resolved", titleBlock["color"])
	}
}

func TestTeamsNotifier_Send_FactsIncludeHostRuleTagsAndOpenUrlAction(t *testing.T) {
	app := newTestApp(t)
	mustSetSetting(t, app, "public_base_url", "https://nexwatch.example.com")
	server, captured := newCapturingServer(t, http.StatusOK, "1")
	channel := createChannel(t, app, "teams", map[string]any{"webhook_url": server.URL})
	alert, alertCtx := createAlertWithTags(t, app, "firing", 1.0, "[warning] memory elevated", []string{"db"})

	n := NewTeamsNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	content := captured.Body["attachments"].([]any)[0].(map[string]any)["content"].(map[string]any)

	var factSet map[string]any
	for _, b := range content["body"].([]any) {
		block := b.(map[string]any)
		if block["type"] == "FactSet" {
			factSet = block
			break
		}
	}
	if factSet == nil {
		t.Fatal("content.body has no FactSet block")
	}
	values := map[string]any{}
	for _, f := range factSet["facts"].([]any) {
		fact := f.(map[string]any)
		values[fact["title"].(string)] = fact["value"]
	}
	if values["Host"] != "host-x" {
		t.Errorf("Host fact = %v, want host-x", values["Host"])
	}
	if values["Rule"] != "r" {
		t.Errorf("Rule fact = %v, want r", values["Rule"])
	}
	if values["Tags"] != "db" {
		t.Errorf("Tags fact = %v, want db", values["Tags"])
	}

	actions, ok := content["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("content.actions = %v, want a single Action.OpenUrl action", content["actions"])
	}
	action := actions[0].(map[string]any)
	if action["type"] != "Action.OpenUrl" {
		t.Errorf("action.type = %v, want Action.OpenUrl", action["type"])
	}
	if action["url"] != "https://nexwatch.example.com/servers/"+alert.GetString("agent_id") {
		t.Errorf("action.url = %v, want the dashboard host page", action["url"])
	}
}

func TestTeamsNotifier_Send_MissingWebhookURLReturnsConfigError(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "teams", map[string]any{})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewTeamsNotifier()
	err := n.Send(context.Background(), alert, channel, alertCtx)
	var cfgErr *notify.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Send() error = %v, want a *notify.ConfigError", err)
	}
}

func TestTeamsNotifier_Send_ErrorStatusReturnsError(t *testing.T) {
	app := newTestApp(t)
	server, _ := newCapturingServer(t, http.StatusInternalServerError, "")
	channel := createChannel(t, app, "teams", map[string]any{"webhook_url": server.URL})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewTeamsNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
		t.Fatal("Send() expected an error for a 500 response, got nil")
	}
}

// --- PagerDutyNotifier -------------------------------------------------------

func TestPagerDutyNotifier_Send_FiringSendsTriggerWithDedupKey(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusAccepted, `{"status":"success","message":"Event processed"}`)
	channel := createChannel(t, app, "pagerduty", map[string]any{"routing_key": "rk-123"})
	alert, alertCtx := createAlert(t, app, "firing", 95.5, "[critical] cpu too high")

	n := NewPagerDutyNotifier()
	n.client = server.Client()
	n.baseURL = server.URL

	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if captured.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", captured.Method)
	}
	if captured.Body["routing_key"] != "rk-123" {
		t.Errorf("body.routing_key = %v, want rk-123", captured.Body["routing_key"])
	}
	if captured.Body["event_action"] != "trigger" {
		t.Errorf("body.event_action = %v, want trigger for a firing alert", captured.Body["event_action"])
	}
	if captured.Body["dedup_key"] != alert.Id {
		t.Errorf("body.dedup_key = %v, want the alert id %v", captured.Body["dedup_key"], alert.Id)
	}
	payload := captured.Body["payload"].(map[string]any)
	if payload["severity"] != "critical" {
		t.Errorf("payload.severity = %v, want critical", payload["severity"])
	}
	if !strings.Contains(payload["summary"].(string), "cpu too high") {
		t.Errorf("payload.summary = %v, want it to contain the alert message", payload["summary"])
	}
	customDetails := payload["custom_details"].(map[string]any)
	if customDetails["alert_id"] != alert.Id {
		t.Errorf("custom_details.alert_id = %v, want %v", customDetails["alert_id"], alert.Id)
	}
}

func TestPagerDutyNotifier_Send_ResolvedSendsResolveWithSameDedupKey(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusAccepted, `{"status":"success"}`)
	channel := createChannel(t, app, "pagerduty", map[string]any{"routing_key": "rk-123"})
	alert, alertCtx := createAlert(t, app, "resolved", 10, "[critical] back to normal")

	n := NewPagerDutyNotifier()
	n.client = server.Client()
	n.baseURL = server.URL

	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if captured.Body["event_action"] != "resolve" {
		t.Errorf("body.event_action = %v, want resolve for a resolved alert", captured.Body["event_action"])
	}
	if captured.Body["dedup_key"] != alert.Id {
		t.Errorf("body.dedup_key = %v, want the same alert id %v used when it fired", captured.Body["dedup_key"], alert.Id)
	}
}

func TestPagerDutyNotifier_Send_CustomDetailsAndLinksIncludeContext(t *testing.T) {
	app := newTestApp(t)
	mustSetSetting(t, app, "public_base_url", "https://nexwatch.example.com")
	server, captured := newCapturingServer(t, http.StatusAccepted, `{"status":"success"}`)
	channel := createChannel(t, app, "pagerduty", map[string]any{"routing_key": "rk-123"})
	alert, alertCtx := createAlertWithTags(t, app, "firing", 95.5, "[critical] cpu too high", []string{"web"})

	n := NewPagerDutyNotifier()
	n.client = server.Client()
	n.baseURL = server.URL

	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	payload := captured.Body["payload"].(map[string]any)
	customDetails := payload["custom_details"].(map[string]any)
	if customDetails["hostname"] != "host-x" {
		t.Errorf("custom_details.hostname = %v, want host-x", customDetails["hostname"])
	}
	if customDetails["rule_name"] != "r" {
		t.Errorf("custom_details.rule_name = %v, want r", customDetails["rule_name"])
	}

	dashboardURL := "https://nexwatch.example.com/servers/" + alert.GetString("agent_id")
	if captured.Body["client_url"] != dashboardURL {
		t.Errorf("body.client_url = %v, want %v", captured.Body["client_url"], dashboardURL)
	}
	links, ok := captured.Body["links"].([]any)
	if !ok || len(links) != 1 {
		t.Fatalf("body.links = %v, want a single-element array", captured.Body["links"])
	}
	if links[0].(map[string]any)["href"] != dashboardURL {
		t.Errorf("links[0].href = %v, want %v", links[0].(map[string]any)["href"], dashboardURL)
	}
}

func TestPagerDutyNotifier_Send_WarningSeverityMapsToWarning(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusAccepted, `{"status":"success"}`)
	channel := createChannel(t, app, "pagerduty", map[string]any{"routing_key": "rk-123"})
	alert, _ := createAlert(t, app, "firing", 71, "[warning] memory elevated")
	alertCtx := withRuleSeverity(t, app, alert, "warning")

	n := NewPagerDutyNotifier()
	n.client = server.Client()
	n.baseURL = server.URL

	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	payload := captured.Body["payload"].(map[string]any)
	if payload["severity"] != "warning" {
		t.Errorf("payload.severity = %v, want warning", payload["severity"])
	}
}

func TestPagerDutyNotifier_Send_MissingRoutingKeyReturnsConfigError(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "pagerduty", map[string]any{})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewPagerDutyNotifier()
	err := n.Send(context.Background(), alert, channel, alertCtx)
	var cfgErr *notify.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Send() error = %v, want a *notify.ConfigError", err)
	}
	if !strings.Contains(cfgErr.Error(), "routing_key") {
		t.Errorf("error = %v, want it to mention routing_key", cfgErr.Error())
	}
}

func TestPagerDutyNotifier_Send_ErrorStatusReturnsError(t *testing.T) {
	app := newTestApp(t)
	server, _ := newCapturingServer(t, http.StatusBadRequest, `{"status":"invalid event"}`)
	channel := createChannel(t, app, "pagerduty", map[string]any{"routing_key": "rk-123"})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewPagerDutyNotifier()
	n.client = server.Client()
	n.baseURL = server.URL

	if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
		t.Fatal("Send() expected an error for a 400 response, got nil")
	}
}

// --- NtfyNotifier ------------------------------------------------------------

func TestNtfyNotifier_Send_FiringCriticalUsesUrgentPriority(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, `{"id":"1"}`)
	channel := createChannel(t, app, "ntfy", map[string]any{
		"server_url": server.URL,
		"topic":      "nexwatch-alerts",
	})
	alert, alertCtx := createAlert(t, app, "firing", 95.5, "[critical] cpu too high")

	n := NewNtfyNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if captured.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", captured.Method)
	}
	if captured.Path != "/nexwatch-alerts" {
		t.Errorf("Path = %q, want /nexwatch-alerts", captured.Path)
	}
	if got := captured.Headers.Get("Priority"); got != "5" {
		t.Errorf("Priority header = %q, want 5 (urgent) for a critical firing alert", got)
	}
	if got := captured.Headers.Get("Title"); !strings.Contains(got, "FIRING") {
		t.Errorf("Title header = %q, want it to mention FIRING", got)
	}
	if !strings.Contains(string(captured.RawBody), "cpu too high") {
		t.Errorf("body = %q, want it to contain the alert message", string(captured.RawBody))
	}
}

func TestNtfyNotifier_Send_ResolvedUsesLowPriority(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, `{"id":"1"}`)
	channel := createChannel(t, app, "ntfy", map[string]any{
		"server_url": server.URL,
		"topic":      "nexwatch-alerts",
	})
	alert, alertCtx := createAlert(t, app, "resolved", 10, "[critical] back to normal")

	n := NewNtfyNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if got := captured.Headers.Get("Priority"); got != "2" {
		t.Errorf("Priority header = %q, want 2 (low) once resolved", got)
	}
}

func TestNtfyNotifier_Send_ExplicitPriorityOverridesDefault(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, `{"id":"1"}`)
	channel := createChannel(t, app, "ntfy", map[string]any{
		"server_url": server.URL,
		"topic":      "nexwatch-alerts",
		"priority":   1.0,
	})
	alert, alertCtx := createAlert(t, app, "firing", 95.5, "[critical] cpu too high")

	n := NewNtfyNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if got := captured.Headers.Get("Priority"); got != "1" {
		t.Errorf("Priority header = %q, want the explicitly configured 1", got)
	}
}

func TestNtfyNotifier_Send_TokenSentAsBearerAuth(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, `{"id":"1"}`)
	channel := createChannel(t, app, "ntfy", map[string]any{
		"server_url": server.URL,
		"topic":      "nexwatch-alerts",
		"token":      "tk_secret",
	})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewNtfyNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if got := captured.Headers.Get("Authorization"); got != "Bearer tk_secret" {
		t.Errorf("Authorization header = %q, want Bearer tk_secret", got)
	}
}

// TestNtfyNotifier_Send_ClickHeaderAndAgentTags covers both the
// without-public_base_url and with-public_base_url cases in one test
// (sharing a single test app/server) rather than two, since spinning up a
// full PocketBase test app is the dominant cost of this package's test
// suite under `go test -race`.
func TestNtfyNotifier_Send_ClickHeaderAndAgentTags(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, `{"id":"1"}`)
	channel := createChannel(t, app, "ntfy", map[string]any{
		"server_url": server.URL,
		"topic":      "nexwatch-alerts",
	})
	n := NewNtfyNotifier()

	t.Run("no Click header without public_base_url", func(t *testing.T) {
		alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")
		if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
			t.Fatalf("Send() unexpected error: %v", err)
		}
		if got := captured.Headers.Get("Click"); got != "" {
			t.Errorf("Click header = %q, want empty when public_base_url is unset", got)
		}
	})

	t.Run("Click header and agent tags once public_base_url is set", func(t *testing.T) {
		mustSetSetting(t, app, "public_base_url", "https://nexwatch.example.com")
		alert, alertCtx := createAlertWithTags(t, app, "firing", 95.5, "[critical] cpu too high", []string{"web", "prod"})

		if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
			t.Fatalf("Send() unexpected error: %v", err)
		}

		wantClick := "https://nexwatch.example.com/servers/" + alert.GetString("agent_id")
		if got := captured.Headers.Get("Click"); got != wantClick {
			t.Errorf("Click header = %q, want %q", got, wantClick)
		}
		if got := captured.Headers.Get("Tags"); !strings.Contains(got, "web") || !strings.Contains(got, "prod") {
			t.Errorf("Tags header = %q, want it to include the agent's web and prod tags", got)
		}
	})
}

func TestNtfyNotifier_Send_MissingTopicReturnsConfigError(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "ntfy", map[string]any{"server_url": "https://ntfy.example.com"})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewNtfyNotifier()
	err := n.Send(context.Background(), alert, channel, alertCtx)
	var cfgErr *notify.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("Send() error = %v, want a *notify.ConfigError", err)
	}
}

func TestNtfyNotifier_Send_ErrorStatusReturnsError(t *testing.T) {
	app := newTestApp(t)
	server, _ := newCapturingServer(t, http.StatusForbidden, "")
	channel := createChannel(t, app, "ntfy", map[string]any{"server_url": server.URL, "topic": "t"})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewNtfyNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
		t.Fatal("Send() expected an error for a 403 response, got nil")
	}
}

// --- GotifyNotifier ----------------------------------------------------------

func TestGotifyNotifier_Send_FiringCriticalUsesHighPriorityAndAuthHeader(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, `{"id":1}`)
	channel := createChannel(t, app, "gotify", map[string]any{
		"server_url": server.URL,
		"app_token":  "gotify-token",
	})
	alert, alertCtx := createAlert(t, app, "firing", 95.5, "[critical] cpu too high")

	n := NewGotifyNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if captured.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", captured.Method)
	}
	if captured.Path != "/message" {
		t.Errorf("Path = %q, want /message", captured.Path)
	}
	if got := captured.Headers.Get("X-Gotify-Key"); got != "gotify-token" {
		t.Errorf("X-Gotify-Key header = %q, want gotify-token", got)
	}
	if captured.Body["priority"] != 8.0 {
		t.Errorf("body.priority = %v, want 8 for a critical firing alert", captured.Body["priority"])
	}
	if !strings.Contains(captured.Body["message"].(string), "cpu too high") {
		t.Errorf("body.message = %v, want it to contain the alert message", captured.Body["message"])
	}
	extras := captured.Body["extras"].(map[string]any)
	display := extras["client::display"].(map[string]any)
	if display["contentType"] != "text/markdown" {
		t.Errorf("extras.client::display.contentType = %v, want text/markdown", display["contentType"])
	}
}

func TestGotifyNotifier_Send_ResolvedUsesLowPriority(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, `{"id":1}`)
	channel := createChannel(t, app, "gotify", map[string]any{
		"server_url": server.URL,
		"app_token":  "gotify-token",
	})
	alert, alertCtx := createAlert(t, app, "resolved", 10, "[critical] back to normal")

	n := NewGotifyNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	if captured.Body["priority"] != 2.0 {
		t.Errorf("body.priority = %v, want 2 once resolved", captured.Body["priority"])
	}
}

// TestGotifyNotifier_Send_ClickURLConfiguration covers both the
// with-public_base_url and without-public_base_url cases in one test
// (sharing a single test app/server), matching the same rationale as
// TestNtfyNotifier_Send_ClickHeaderAndAgentTags above.
func TestGotifyNotifier_Send_ClickURLConfiguration(t *testing.T) {
	app := newTestApp(t)
	server, captured := newCapturingServer(t, http.StatusOK, `{"id":1}`)
	channel := createChannel(t, app, "gotify", map[string]any{
		"server_url": server.URL,
		"app_token":  "gotify-token",
	})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")
	n := NewGotifyNotifier()

	t.Run("no click extra without public_base_url", func(t *testing.T) {
		if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
			t.Fatalf("Send() unexpected error: %v", err)
		}
		extras := captured.Body["extras"].(map[string]any)
		if _, ok := extras["client::notification"]; ok {
			t.Errorf("extras[client::notification] = %v, want it absent when public_base_url is unset", extras["client::notification"])
		}
	})

	t.Run("click url set once public_base_url is configured", func(t *testing.T) {
		mustSetSetting(t, app, "public_base_url", "https://nexwatch.example.com")
		alertCtx = notify.BuildAlertContext(app, alert, nil)

		if err := n.Send(context.Background(), alert, channel, alertCtx); err != nil {
			t.Fatalf("Send() unexpected error: %v", err)
		}
		extras := captured.Body["extras"].(map[string]any)
		notification, ok := extras["client::notification"].(map[string]any)
		if !ok {
			t.Fatalf("extras[client::notification] = %v, want an object", extras["client::notification"])
		}
		click := notification["click"].(map[string]any)
		want := "https://nexwatch.example.com/servers/" + alert.GetString("agent_id")
		if click["url"] != want {
			t.Errorf("click.url = %v, want %v", click["url"], want)
		}
	})
}

func TestGotifyNotifier_Send_MissingConfigReturnsConfigError(t *testing.T) {
	app := newTestApp(t)
	tests := []struct {
		name   string
		config map[string]any
	}{
		{"missing both", map[string]any{}},
		{"missing app_token", map[string]any{"server_url": "https://gotify.example.com"}},
		{"missing server_url", map[string]any{"app_token": "tok"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := createChannel(t, app, "gotify", tt.config)
			alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

			n := NewGotifyNotifier()
			err := n.Send(context.Background(), alert, channel, alertCtx)
			var cfgErr *notify.ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("Send() error = %v, want a *notify.ConfigError", err)
			}
		})
	}
}

func TestGotifyNotifier_Send_ErrorStatusReturnsError(t *testing.T) {
	app := newTestApp(t)
	server, _ := newCapturingServer(t, http.StatusUnauthorized, "")
	channel := createChannel(t, app, "gotify", map[string]any{"server_url": server.URL, "app_token": "bad"})
	alert, alertCtx := createAlert(t, app, "firing", 1.0, "m")

	n := NewGotifyNotifier()
	if err := n.Send(context.Background(), alert, channel, alertCtx); err == nil {
		t.Fatal("Send() expected an error for a 401 response, got nil")
	}
}

// --- EmailNotifier.SendRaw (weekly report) --------------------------------

func TestEmailNotifier_SendRaw_AssemblesMultipartMessageAndInvokesSendMail(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "email", map[string]any{
		"host": "smtp.example.com",
		"port": 2525.0,
		"from": "reports@nexwatch.local",
		"to":   "ops@example.com",
	})

	var gotAddr, gotFrom string
	var gotTo []string
	var gotMsg []byte
	n := NewEmailNotifier()
	n.sendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotFrom, gotTo, gotMsg = addr, from, to, msg
		return nil
	}

	err := n.SendRaw(context.Background(), "Weekly report", "<h1>Fleet</h1><p>all good</p>", "Fleet\nall good", channel)
	if err != nil {
		t.Fatalf("SendRaw() unexpected error: %v", err)
	}

	if gotAddr != "smtp.example.com:2525" {
		t.Errorf("addr = %q, want smtp.example.com:2525", gotAddr)
	}
	if gotFrom != "reports@nexwatch.local" {
		t.Errorf("from = %q, want reports@nexwatch.local", gotFrom)
	}
	if len(gotTo) != 1 || gotTo[0] != "ops@example.com" {
		t.Errorf("to = %v, want [ops@example.com]", gotTo)
	}

	msg := string(gotMsg)
	if !strings.Contains(msg, "Subject: Weekly report") {
		t.Errorf("message = %q, want the subject header", msg)
	}
	if !strings.Contains(msg, "multipart/alternative") {
		t.Errorf("message = %q, want a multipart/alternative content type", msg)
	}
	if !strings.Contains(msg, "Content-Type: text/plain") || !strings.Contains(msg, "Fleet\nall good") {
		t.Errorf("message = %q, want a text/plain part with the plain-text body", msg)
	}
	if !strings.Contains(msg, "Content-Type: text/html") || !strings.Contains(msg, "<h1>Fleet</h1><p>all good</p>") {
		t.Errorf("message = %q, want a text/html part with the HTML body", msg)
	}
}

func TestEmailNotifier_SendRaw_MissingRequiredFieldsReturnsConfigError(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "email", map[string]any{"from": "reports@nexwatch.local", "to": "ops@example.com"}) // missing host

	n := NewEmailNotifier()
	n.sendMail = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		t.Fatal("sendMail must not be called when required config is missing")
		return nil
	}

	err := n.SendRaw(context.Background(), "Weekly report", "<p>x</p>", "x", channel)
	var cfgErr *notify.ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("SendRaw() error = %v, want a *notify.ConfigError", err)
	}
}
