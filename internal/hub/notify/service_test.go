package notify

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// notification_channels/alerts/alert_rules collections this package
	// dispatches against.
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

// fakeNotifier records every Send() call and returns a scripted error.
type fakeNotifier struct {
	channelType string
	err         error
	calls       []*core.Record // channel records passed to Send
}

func (f *fakeNotifier) Type() string { return f.channelType }

func (f *fakeNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record, alertCtx AlertContext) error {
	f.calls = append(f.calls, channel)
	return f.err
}

func createChannel(t *testing.T, app core.App, name, chType string, enabled bool, config map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("notification_channels")
	if err != nil {
		t.Fatalf("find notification_channels collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("type", chType)
	rec.Set("enabled", enabled)
	if config != nil {
		rec.Set("config", config)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save channel %s: %v", name, err)
	}
	return rec
}

func createAlertRule(t *testing.T, app core.App, channelIDs []string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("alert_rules")
	if err != nil {
		t.Fatalf("find alert_rules collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "test-rule")
	rec.Set("metric_type", "cpu")
	rec.Set("condition", "gt")
	rec.Set("threshold", 80)
	rec.Set("duration", 1)
	rec.Set("severity", "critical")
	rec.Set("enabled", true)
	if len(channelIDs) > 0 {
		rec.Set("notification_channels", channelIDs)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save alert rule: %v", err)
	}
	return rec
}

func createFiringAlert(t *testing.T, app core.App, rule *core.Record, agentID string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("alerts")
	if err != nil {
		t.Fatalf("find alerts collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("rule_id", rule.Id)
	rec.Set("agent_id", agentID)
	rec.Set("status", "firing")
	rec.Set("value", 95.0)
	rec.Set("message", "cpu too high")
	rec.Set("fired_at", "2026-01-01 00:00:00.000Z")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save alert: %v", err)
	}
	return rec
}

func createAgent(t *testing.T, app core.App, hostname string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("hostname", hostname)
	rec.Set("status", "online")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	return rec
}

func TestDispatch_SendsOnlyToEnabledChannels(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-dispatch")

	enabledCh := createChannel(t, app, "enabled-webhook", "webhook", true, map[string]any{"url": "http://example.com"})
	disabledCh := createChannel(t, app, "disabled-webhook", "webhook", false, map[string]any{"url": "http://example.com"})

	rule := createAlertRule(t, app, []string{enabledCh.Id, disabledCh.Id})
	alert := createFiringAlert(t, app, rule, agent.Id)

	svc := NewService(app)
	fake := &fakeNotifier{channelType: "webhook"}
	svc.RegisterNotifier(fake)

	svc.Dispatch(app, alert, rule)

	if len(fake.calls) != 1 {
		t.Fatalf("Send() call count = %d, want 1 (disabled channel must be skipped)", len(fake.calls))
	}
	if fake.calls[0].Id != enabledCh.Id {
		t.Errorf("Send() was called for channel %s, want the enabled channel %s", fake.calls[0].Id, enabledCh.Id)
	}
}

func TestDispatch_ContinuesPastFailingChannel(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-dispatch-fail")

	failingCh := createChannel(t, app, "failing-webhook", "webhook", true, map[string]any{"url": "http://example.com/fail"})
	workingCh := createChannel(t, app, "working-discord", "discord", true, map[string]any{"webhook_url": "http://example.com/discord"})

	rule := createAlertRule(t, app, []string{failingCh.Id, workingCh.Id})
	alert := createFiringAlert(t, app, rule, agent.Id)

	svc := NewService(app)
	failingNotifier := &fakeNotifier{channelType: "webhook", err: errors.New("boom")}
	workingNotifier := &fakeNotifier{channelType: "discord"}
	svc.RegisterNotifier(failingNotifier)
	svc.RegisterNotifier(workingNotifier)

	// Must not panic or stop early because the first channel's Send fails.
	svc.Dispatch(app, alert, rule)

	if len(failingNotifier.calls) != 1 {
		t.Errorf("failing notifier call count = %d, want 1", len(failingNotifier.calls))
	}
	if len(workingNotifier.calls) != 1 {
		t.Fatalf("working notifier call count = %d, want 1 (must still run after the earlier failure)", len(workingNotifier.calls))
	}
}

func TestDispatch_NoChannelsConfiguredIsANoop(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-no-channels")
	rule := createAlertRule(t, app, nil)
	alert := createFiringAlert(t, app, rule, agent.Id)

	svc := NewService(app)
	fake := &fakeNotifier{channelType: "webhook"}
	svc.RegisterNotifier(fake)

	svc.Dispatch(app, alert, rule) // must not panic

	if len(fake.calls) != 0 {
		t.Errorf("Send() call count = %d, want 0 when the rule has no channels", len(fake.calls))
	}
}

func TestDispatch_UnregisteredNotifierTypeIsSkipped(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-unregistered")
	ch := createChannel(t, app, "telegram-ch", "telegram", true, map[string]any{"bot_token": "x", "chat_id": "y"})
	rule := createAlertRule(t, app, []string{ch.Id})
	alert := createFiringAlert(t, app, rule, agent.Id)

	svc := NewService(app) // no notifiers registered at all

	svc.Dispatch(app, alert, rule) // must not panic
}

func TestRenderMessage_FiringWithoutResolvedAt(t *testing.T) {
	app := newTestApp(t)
	rule := createAlertRule(t, app, nil)
	agent := createAgent(t, app, "host-render")
	alert := createFiringAlert(t, app, rule, agent.Id)
	alert.Set("resolved_at", "")

	got := RenderMessage(alert, "critical")

	if !strings.Contains(got, "cpu too high") {
		t.Errorf("RenderMessage() = %q, want it to contain the alert message", got)
	}
	if !strings.Contains(got, "Status: firing") {
		t.Errorf("RenderMessage() = %q, want it to contain the status", got)
	}
	if strings.Contains(got, "Resolved:") {
		t.Errorf("RenderMessage() = %q, want no Resolved line when resolved_at is empty", got)
	}
}

func TestRenderMessage_ResolvedIncludesResolvedAt(t *testing.T) {
	app := newTestApp(t)
	rule := createAlertRule(t, app, nil)
	agent := createAgent(t, app, "host-render-resolved")
	alert := createFiringAlert(t, app, rule, agent.Id)
	alert.Set("status", "resolved")
	alert.Set("resolved_at", "2026-01-01 01:00:00.000Z")

	got := RenderMessage(alert, "critical")

	if !strings.Contains(got, "Resolved: 2026-01-01 01:00:00.000Z") {
		t.Errorf("RenderMessage() = %q, want it to contain the resolved_at line", got)
	}
}

func TestRenderMessage_SeverityIsRendered(t *testing.T) {
	// Pins the fix for the bug where AlertData.Severity was never
	// populated: the template's "Severity: {{.Severity}}" line rendered
	// blank on every channel regardless of the argument passed in.
	app := newTestApp(t)
	rule := createAlertRule(t, app, nil)
	agent := createAgent(t, app, "host-render-severity")
	alert := createFiringAlert(t, app, rule, agent.Id)

	got := RenderMessage(alert, "warning")

	if !strings.Contains(got, "Severity: warning") {
		t.Errorf("RenderMessage() = %q, want it to contain the rendered severity", got)
	}
}

// --- BuildAlertContext -------------------------------------------------

func TestBuildAlertContext_ResolvesSeverityFromRule(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-ctx-severity")
	rule := createAlertRule(t, app, nil)
	rule.Set("severity", "warning")
	if err := app.Save(rule); err != nil {
		t.Fatalf("save rule: %v", err)
	}
	alert := createFiringAlert(t, app, rule, agent.Id)
	alert.Set("message", "[critical] cpu too high") // stale/mismatched tag must lose to the rule

	ctx := BuildAlertContext(app, alert, rule)

	if ctx.Severity != "warning" {
		t.Errorf("Severity = %q, want the rule's severity (warning) to win over the message tag", ctx.Severity)
	}
}

func TestBuildAlertContext_FallsBackToMessageTagWithoutARule(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-ctx-no-rule")
	rule := createAlertRule(t, app, nil)
	alert := createFiringAlert(t, app, rule, agent.Id)
	alert.Set("message", "[warning] memory elevated")

	ctx := BuildAlertContext(app, alert, nil) // no rule available, e.g. a synthetic test notification

	if ctx.Severity != "warning" {
		t.Errorf("Severity = %q, want it parsed from the message tag when no rule is given", ctx.Severity)
	}
}

func TestBuildAlertContext_ResolvesHostnameAndTagsForAgentAlert(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-ctx-agent")
	agent.Set("tags", []string{"web", "prod"})
	if err := app.Save(agent); err != nil {
		t.Fatalf("save agent tags: %v", err)
	}
	rule := createAlertRule(t, app, nil)
	alert := createFiringAlert(t, app, rule, agent.Id)

	ctx := BuildAlertContext(app, alert, rule)

	if ctx.Hostname != "host-ctx-agent" {
		t.Errorf("Hostname = %q, want host-ctx-agent", ctx.Hostname)
	}
	if len(ctx.AgentTags) != 2 || ctx.AgentTags[0] != "web" || ctx.AgentTags[1] != "prod" {
		t.Errorf("AgentTags = %v, want [web prod]", ctx.AgentTags)
	}
	if ctx.CheckName != "" {
		t.Errorf("CheckName = %q, want empty for an agent-based alert", ctx.CheckName)
	}
}

func TestBuildAlertContext_ResolvesCheckNameAndTargetForCheckAlert(t *testing.T) {
	app := newTestApp(t)
	checksCol, err := app.FindCollectionByNameOrId("checks")
	if err != nil {
		t.Fatalf("find checks collection: %v", err)
	}
	check := core.NewRecord(checksCol)
	check.Set("name", "billing-api")
	check.Set("target", "https://billing.internal/health")
	check.Set("type", "http")
	if err := app.Save(check); err != nil {
		t.Fatalf("save check: %v", err)
	}

	rule := createAlertRule(t, app, nil)
	rule.Set("metric_type", "check_down")
	rule.Set("check_id", check.Id)
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
	alert.Set("status", "firing")
	alert.Set("message", "[critical] check_down: billing-api is down")
	alert.Set("fired_at", "2026-01-01 00:00:00.000Z")
	if err := app.Save(alert); err != nil {
		t.Fatalf("save alert: %v", err)
	}

	ctx := BuildAlertContext(app, alert, rule)

	if ctx.CheckName != "billing-api" {
		t.Errorf("CheckName = %q, want billing-api", ctx.CheckName)
	}
	if ctx.CheckTarget != "https://billing.internal/health" {
		t.Errorf("CheckTarget = %q, want the check's target", ctx.CheckTarget)
	}
	if ctx.Hostname != "" {
		t.Errorf("Hostname = %q, want empty for a check-based alert", ctx.Hostname)
	}
	if ctx.DashboardPath != "/checks" {
		t.Errorf("DashboardPath = %q, want /checks for a check-based alert", ctx.DashboardPath)
	}
}

func TestBuildAlertContext_DashboardURLOnlySetWithPublicBaseURL(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-ctx-dashboard")
	rule := createAlertRule(t, app, nil)
	alert := createFiringAlert(t, app, rule, agent.Id)

	withoutSetting := BuildAlertContext(app, alert, rule)
	if withoutSetting.DashboardURL != "" {
		t.Errorf("DashboardURL = %q, want empty when public_base_url is unset", withoutSetting.DashboardURL)
	}
	wantPath := "/servers/" + agent.Id
	if withoutSetting.DashboardPath != wantPath {
		t.Errorf("DashboardPath = %q, want %q regardless of public_base_url", withoutSetting.DashboardPath, wantPath)
	}

	col, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		t.Fatalf("find settings collection: %v", err)
	}
	setting := core.NewRecord(col)
	setting.Set("key", "public_base_url")
	setting.Set("value", `"https://nexwatch.example.com/"`)
	if err := app.Save(setting); err != nil {
		t.Fatalf("save public_base_url setting: %v", err)
	}

	withSetting := BuildAlertContext(app, alert, rule)
	want := "https://nexwatch.example.com" + wantPath
	if withSetting.DashboardURL != want {
		t.Errorf("DashboardURL = %q, want %q (trailing slash trimmed)", withSetting.DashboardURL, want)
	}
}

func TestParseChannelConfig_ValidJSON(t *testing.T) {
	app := newTestApp(t)
	ch := createChannel(t, app, "cfg-valid", "webhook", true, map[string]any{"url": "http://example.com", "timeout": 5.0})

	config, err := ParseChannelConfig(ch)
	if err != nil {
		t.Fatalf("ParseChannelConfig() unexpected error: %v", err)
	}
	if config["url"] != "http://example.com" {
		t.Errorf("ParseChannelConfig() url = %v, want http://example.com", config["url"])
	}
}

func TestParseChannelConfig_UnsetConfigYieldsNilMapWithoutError(t *testing.T) {
	// A channel whose config field was never set stores the JSON literal
	// "null" (confirmed empirically), not an empty string, so
	// ParseChannelConfig's `configStr == ""` guard never actually triggers
	// through normal record.Set() usage: json.Unmarshal("null", ...)
	// succeeds and yields a nil map. This pins that real behavior rather
	// than the "returns an error" behavior the guard's presence suggests.
	app := newTestApp(t)
	ch := createChannel(t, app, "cfg-unset", "webhook", true, nil)

	config, err := ParseChannelConfig(ch)
	if err != nil {
		t.Fatalf("ParseChannelConfig() with an unset config field unexpectedly errored: %v", err)
	}
	if config != nil {
		t.Errorf("ParseChannelConfig() = %v, want nil for an unset config field", config)
	}
}

func TestParseChannelConfig_NonObjectConfigReturnsError(t *testing.T) {
	app := newTestApp(t)
	ch := createChannel(t, app, "cfg-non-object", "webhook", true, nil)
	ch.Set("config", "not an object")
	if err := app.Save(ch); err != nil {
		t.Fatalf("save channel with non-object config: %v", err)
	}

	if _, err := ParseChannelConfig(ch); err == nil {
		t.Fatal("ParseChannelConfig() with a non-object JSON config expected an error, got nil")
	}
}

func TestGetConfigString(t *testing.T) {
	config := map[string]any{"url": "http://example.com", "port": 8080.0}

	if got := GetConfigString(config, "url"); got != "http://example.com" {
		t.Errorf("GetConfigString(url) = %q, want http://example.com", got)
	}
	if got := GetConfigString(config, "missing"); got != "" {
		t.Errorf("GetConfigString(missing) = %q, want empty", got)
	}
	if got := GetConfigString(config, "port"); got != "" {
		t.Errorf("GetConfigString(port) = %q, want empty (wrong type)", got)
	}
}

func TestGetConfigInt(t *testing.T) {
	config := map[string]any{
		"float_port": 587.0,
		"int_port":   25,
		"json_port":  json.Number("465"),
		"str_port":   "2525",
	}

	tests := []struct {
		key  string
		want int
	}{
		{"float_port", 587},
		{"int_port", 25},
		{"json_port", 465},
		{"str_port", 0}, // string values are not handled by GetConfigInt
		{"missing", 0},
	}

	for _, tt := range tests {
		if got := GetConfigInt(config, tt.key); got != tt.want {
			t.Errorf("GetConfigInt(%q) = %d, want %d", tt.key, got, tt.want)
		}
	}
}

// createEscalationRule is createAlertRule plus escalation_channels, used by
// the DispatchEscalation tests below.
func createEscalationRule(t *testing.T, app core.App, escalationChannelIDs []string) *core.Record {
	t.Helper()
	rule := createAlertRule(t, app, nil)
	if len(escalationChannelIDs) > 0 {
		rule.Set("escalation_channels", escalationChannelIDs)
	}
	rule.Set("escalation_after", 60)
	if err := app.Save(rule); err != nil {
		t.Fatalf("save escalation rule: %v", err)
	}
	return rule
}

func TestDispatchEscalation_SendsToEscalationChannelsWithPrefix(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-escalation-dispatch")

	escalationCh := createChannel(t, app, "escalation-webhook", "webhook", true, map[string]any{"url": "http://example.com"})
	normalCh := createChannel(t, app, "normal-webhook", "webhook", true, map[string]any{"url": "http://example.com"})

	rule := createEscalationRule(t, app, []string{escalationCh.Id})
	alert := createFiringAlert(t, app, rule, agent.Id)
	alert.Set("message", "cpu too high")

	svc := NewService(app)
	fake := &fakeNotifier{channelType: "webhook"}
	svc.RegisterNotifier(fake)

	svc.DispatchEscalation(app, alert, rule)

	if len(fake.calls) != 1 {
		t.Fatalf("Send() call count = %d, want 1 (only the escalation channel)", len(fake.calls))
	}
	if fake.calls[0].Id != escalationCh.Id {
		t.Errorf("Send() was called for channel %s, want the escalation channel %s (not %s)", fake.calls[0].Id, escalationCh.Id, normalCh.Id)
	}
}

func TestDispatchEscalation_PrefixesMessageWithoutPersistingIt(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-escalation-prefix")
	escalationCh := createChannel(t, app, "escalation-webhook-2", "webhook", true, map[string]any{"url": "http://example.com"})
	rule := createEscalationRule(t, app, []string{escalationCh.Id})
	alert := createFiringAlert(t, app, rule, agent.Id)
	alert.Set("message", "cpu too high")

	svc := NewService(app)

	var seenMessage string
	captor := &capturingNotifier{channelType: "webhook", onSend: func(alert *core.Record, alertCtx AlertContext) {
		seenMessage = RenderMessage(alert, alertCtx.Severity)
	}}
	svc.RegisterNotifier(captor)

	svc.DispatchEscalation(app, alert, rule)

	if !strings.Contains(seenMessage, "Escalated: cpu too high") {
		t.Errorf("rendered message = %q, want it to contain %q", seenMessage, "Escalated: cpu too high")
	}
	if got := alert.GetString("message"); got != "cpu too high" {
		t.Errorf("alert.message after DispatchEscalation = %q, want the original %q restored (never persisted)", got, "cpu too high")
	}
}

func TestDispatchEscalation_NoChannelsConfiguredIsANoop(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-escalation-noop")
	rule := createEscalationRule(t, app, nil) // no escalation_channels set
	alert := createFiringAlert(t, app, rule, agent.Id)

	svc := NewService(app)
	fake := &fakeNotifier{channelType: "webhook"}
	svc.RegisterNotifier(fake)

	svc.DispatchEscalation(app, alert, rule) // must not panic

	if len(fake.calls) != 0 {
		t.Errorf("Send() call count = %d, want 0 when the rule has no escalation channels", len(fake.calls))
	}
}

// capturingNotifier records the alert record and resolved AlertContext
// passed to Send by invoking onSend with them, letting a test observe what
// RenderMessage(alert, alertCtx.Severity) would produce at the moment Send
// is actually called.
type capturingNotifier struct {
	channelType string
	onSend      func(alert *core.Record, alertCtx AlertContext)
}

func (c *capturingNotifier) Type() string { return c.channelType }

func (c *capturingNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record, alertCtx AlertContext) error {
	if c.onSend != nil {
		c.onSend(alert, alertCtx)
	}
	return nil
}

func TestRenderMessage_IncludesAcknowledgementAndEscalationContext(t *testing.T) {
	app := newTestApp(t)
	rule := createAlertRule(t, app, nil)
	agent := createAgent(t, app, "host-render-lifecycle")
	alert := createFiringAlert(t, app, rule, agent.Id)
	alert.Set("acknowledged_at", "2026-01-01 00:05:00.000Z")
	alert.Set("acknowledged_by", "ops@example.com")
	alert.Set("escalated_at", "2026-01-01 00:10:00.000Z")

	got := RenderMessage(alert, "critical")

	if !strings.Contains(got, "Acknowledged by ops@example.com at 2026-01-01 00:05:00.000Z") {
		t.Errorf("RenderMessage() = %q, want it to contain the acknowledgement line", got)
	}
	if !strings.Contains(got, "Escalated at 2026-01-01 00:10:00.000Z") {
		t.Errorf("RenderMessage() = %q, want it to contain the escalation line", got)
	}
}

func TestRenderMessage_OmitsLifecycleLinesWhenUnset(t *testing.T) {
	app := newTestApp(t)
	rule := createAlertRule(t, app, nil)
	agent := createAgent(t, app, "host-render-no-lifecycle")
	alert := createFiringAlert(t, app, rule, agent.Id)

	got := RenderMessage(alert, "critical")

	if strings.Contains(got, "Acknowledged by") {
		t.Errorf("RenderMessage() = %q, want no acknowledgement line when unset", got)
	}
	if strings.Contains(got, "Escalated at") {
		t.Errorf("RenderMessage() = %q, want no escalation line when unset", got)
	}
}

// --- SendTestNotification error classification ---------------------------
//
// api.handleTestNotification uses errors.As against *ConfigError to decide
// between an HTTP 400 (fix your config) and a 500 (delivery failed). These
// tests guard the contract it depends on: a notifier's ConfigError must
// survive the round trip through SendTestNotification unwrapped, while a
// plain delivery error must not be misclassified as one.

func TestSendTestNotification_ConfigErrorIsDistinguishable(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "bad-pagerduty", "pagerduty", true, map[string]any{})

	svc := NewService(app)
	fake := &fakeNotifier{
		channelType: "pagerduty",
		err:         NewConfigError("pagerduty config missing required field: routing_key"),
	}
	svc.RegisterNotifier(fake)

	err := svc.SendTestNotification(channel)
	if err == nil {
		t.Fatal("SendTestNotification() error = nil, want the notifier's ConfigError")
	}
	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("SendTestNotification() error = %v, want it to unwrap to *ConfigError", err)
	}
	if !strings.Contains(cfgErr.Error(), "routing_key") {
		t.Errorf("ConfigError message = %q, want it to mention routing_key", cfgErr.Error())
	}
}

func TestSendTestNotification_DeliveryErrorIsNotAConfigError(t *testing.T) {
	app := newTestApp(t)
	channel := createChannel(t, app, "flaky-webhook", "webhook", true, map[string]any{"url": "http://example.com"})

	svc := NewService(app)
	fake := &fakeNotifier{channelType: "webhook", err: errors.New("connection refused")}
	svc.RegisterNotifier(fake)

	err := svc.SendTestNotification(channel)
	if err == nil {
		t.Fatal("SendTestNotification() error = nil, want the notifier's delivery error")
	}
	var cfgErr *ConfigError
	if errors.As(err, &cfgErr) {
		t.Error("a plain delivery error must not be classified as a *ConfigError")
	}
}

// --- SeverityFromMessage ---------------------------------------------------

func TestSeverityFromMessage(t *testing.T) {
	tests := []struct {
		name    string
		message string
		want    string
	}{
		{"critical tag", "[critical] cpu on web-01: cpu > 80.0 (current: 95.0)", "critical"},
		{"warning tag", "[warning] memory on db-01: memory > 70.0 (current: 75.0)", "warning"},
		{"tag is case-insensitive", "[CRITICAL] agent offline: web-01 has not been seen since ...", "critical"},
		{"unrecognized tag defaults to critical", "[TEST] This is a test notification from NexWatch.", "critical"},
		{"no bracket tag defaults to critical", "no tag here at all", "critical"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SeverityFromMessage(tt.message); got != tt.want {
				t.Errorf("SeverityFromMessage(%q) = %q, want %q", tt.message, got, tt.want)
			}
		})
	}
}
