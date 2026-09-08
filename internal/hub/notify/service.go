package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Service dispatches alert notifications to configured channels.
type Service struct {
	app       core.App
	notifiers map[string]Notifier
	mu        sync.RWMutex
}

// NewService creates a new notification service.
func NewService(app core.App) *Service {
	return &Service{
		app:       app,
		notifiers: make(map[string]Notifier),
	}
}

// RegisterNotifier adds a notifier implementation for a given channel type.
func (s *Service) RegisterNotifier(n Notifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notifiers[n.Type()] = n
	slog.Info("notifier registered", "channel_type", n.Type())
}

// Dispatch sends notifications for an alert to all channels linked to the rule.
func (s *Service) Dispatch(app core.App, alert *core.Record, rule *core.Record) {
	channelIDs := rule.GetStringSlice("notification_channels")
	if len(channelIDs) == 0 {
		slog.Warn("rule has no notification channels configured", "rule_id", rule.Id)
		return
	}
	s.dispatchToChannels(app, alert, channelIDs, rule)
}

// DispatchEscalation sends a one-time escalation notification for alert to
// the rule's escalation_channels — a separate list from
// notification_channels, so escalations reach a distinct (typically wider
// or higher-urgency) audience. The rendered message is prefixed with
// "Escalated: " so it is unmistakable from the original firing/re-fire
// notification even in a channel that only surfaces the raw text (e.g. a
// webhook payload). The prefix is applied only to the in-memory alert
// passed to notifiers here — it is never persisted.
func (s *Service) DispatchEscalation(app core.App, alert *core.Record, rule *core.Record) {
	channelIDs := rule.GetStringSlice("escalation_channels")
	if len(channelIDs) == 0 {
		slog.Warn("rule has no escalation channels configured", "rule_id", rule.Id)
		return
	}

	original := alert.GetString("message")
	alert.Set("message", "Escalated: "+original)
	defer alert.Set("message", original)

	s.dispatchToChannels(app, alert, channelIDs, rule)
}

// dispatchToChannels is the shared delivery loop behind Dispatch and
// DispatchEscalation: it resolves each channel ID, skips disabled channels
// or ones with no registered Notifier, and sends through the rest,
// continuing past any individual channel's failure. The enrichment context
// (hostname, tags, rule/check details, severity, dashboard link) is
// resolved exactly once here — not per channel — since it is the same for
// every channel notified about this alert.
func (s *Service) dispatchToChannels(app core.App, alert *core.Record, channelIDs []string, rule *core.Record) {
	alertCtx := BuildAlertContext(app, alert, rule)

	for _, channelID := range channelIDs {
		channel, err := app.FindRecordById("notification_channels", channelID)
		if err != nil {
			slog.Warn("notification channel not found", "channel_id", channelID, "error", err)
			continue
		}

		if !channel.GetBool("enabled") {
			slog.Info("notification channel disabled, skipping", "channel_id", channelID, "channel_name", channel.GetString("name"))
			continue
		}

		channelType := channel.GetString("type")

		s.mu.RLock()
		notifier, exists := s.notifiers[channelType]
		s.mu.RUnlock()

		if !exists {
			slog.Warn("no notifier registered for channel type", "channel_type", channelType)
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err = notifier.Send(ctx, alert, channel, alertCtx)
		cancel()

		if err != nil {
			slog.Error("failed to send notification", "channel_type", channelType, "channel_name", channel.GetString("name"), "alert_id", alert.Id, "error", err)
		} else {
			slog.Info("notification sent", "channel_type", channelType, "channel_name", channel.GetString("name"), "alert_id", alert.Id)
		}
	}
}

// SendTestNotification sends a test message to a specific channel.
func (s *Service) SendTestNotification(channel *core.Record) error {
	channelType := channel.GetString("type")

	s.mu.RLock()
	notifier, exists := s.notifiers[channelType]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("no notifier registered for type: %s", channelType)
	}

	// Create a fake alert record for testing.
	alertCollection, err := s.app.FindCollectionByNameOrId("alerts")
	if err != nil {
		return fmt.Errorf("alerts collection not found: %w", err)
	}

	fakeAlert := core.NewRecord(alertCollection)
	fakeAlert.Set("status", "firing")
	fakeAlert.Set("value", 95.5)
	fakeAlert.Set("message", "[TEST] This is a test notification from NexWatch. If you see this, your notification channel is working correctly!")
	fakeAlert.Set("fired_at", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))

	// No rule backs a synthetic test alert, so BuildAlertContext falls back
	// to parsing severity from the "[TEST]" message tag, and hostname/rule/
	// check fields stay empty (there is no real agent or check to resolve).
	alertCtx := BuildAlertContext(s.app, fakeAlert, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return notifier.Send(ctx, fakeAlert, channel, alertCtx)
}

// AlertContext carries the agent/check/rule details resolved once per
// dispatch (see Service.dispatchToChannels and Service.SendTestNotification)
// so every channel notified about the same alert can render a consistent,
// detailed view — hostname, tags, rule/check identity, severity, and a
// deep link into the dashboard — without each independently re-querying
// collections.
type AlertContext struct {
	// Hostname and AgentTags describe the agent an agent-based alert fired
	// against; both are empty for a check-based alert (check_down/
	// cert_expiry — see internal/hub/alerts.isCheckMetricType).
	Hostname  string
	AgentTags []string

	// RuleName and RuleTarget describe the alert_rules record that fired
	// this alert. RuleTarget mirrors alert_rules.target (a process/service
	// name, a log pattern, etc.) and is often empty for threshold-only
	// rules (cpu/memory/disk/...).
	RuleName   string
	RuleTarget string

	// CheckName and CheckTarget describe the checks record a check-based
	// alert fired against; both are empty for an agent-based alert.
	CheckName   string
	CheckTarget string

	// Severity is resolved from the rule's own severity field, falling
	// back to SeverityFromMessage when no rule is available (e.g. a
	// synthetic test notification) or its severity is unset.
	Severity string

	Status         string
	Value          float64
	FiredAt        string
	ResolvedAt     string
	AcknowledgedBy string

	// DashboardPath is a relative deep link into the UI for this incident
	// ("/servers/{agent_id}", "/checks", or "/alerts/history" as a
	// fallback) — always set, so a channel that can resolve a relative URL
	// against its own known origin (e.g. the web push service worker) can
	// use it even when public_base_url is unset.
	DashboardPath string

	// DashboardURL is DashboardPath prefixed with the "public_base_url"
	// setting, and is only set when that setting is non-empty — a channel
	// that needs an absolute, clickable URL (Slack/Teams/Discord buttons,
	// PagerDuty links, ntfy's Click header, Gotify's click extra) should
	// skip that enrichment entirely when this is empty rather than send a
	// broken relative link to an external service.
	DashboardURL string
}

// BuildAlertContext resolves an AlertContext for alert, using rule (when
// available — a synthetic test notification has none) to fill in the rule's
// name/target/severity. It is exported so each channel's own tests can
// build a realistic context the same way the dispatcher does, instead of
// duplicating this resolution logic.
func BuildAlertContext(app core.App, alert *core.Record, rule *core.Record) AlertContext {
	ctx := AlertContext{
		Status:         alert.GetString("status"),
		Value:          alert.GetFloat("value"),
		FiredAt:        alert.GetString("fired_at"),
		ResolvedAt:     alert.GetString("resolved_at"),
		AcknowledgedBy: alert.GetString("acknowledged_by"),
		DashboardPath:  "/alerts/history",
	}

	if rule != nil {
		ctx.RuleName = rule.GetString("name")
		ctx.RuleTarget = rule.GetString("target")
		ctx.Severity = rule.GetString("severity")
	}
	if ctx.Severity == "" {
		ctx.Severity = SeverityFromMessage(alert.GetString("message"))
	}

	if agentID := alert.GetString("agent_id"); agentID != "" {
		if agent, err := app.FindRecordById("agents", agentID); err == nil {
			ctx.Hostname = agent.GetString("hostname")
			ctx.AgentTags = agent.GetStringSlice("tags")
		}
		ctx.DashboardPath = "/servers/" + agentID
	} else if checkID := alert.GetString("check_id"); checkID != "" {
		if check, err := app.FindRecordById("checks", checkID); err == nil {
			ctx.CheckName = check.GetString("name")
			ctx.CheckTarget = check.GetString("target")
		}
		ctx.DashboardPath = "/checks"
	}

	if base := settingString(app, "public_base_url", ""); base != "" {
		ctx.DashboardURL = strings.TrimRight(base, "/") + ctx.DashboardPath
	}

	return ctx
}

// settingString reads a string "settings" value, falling back to def when
// the key is missing, unreadable, or not a JSON string. This duplicates the
// tiny "settings" key/value helper already duplicated in
// internal/hub/backup, internal/hub/report, internal/hub/api/settings.go,
// and internal/hub/push (see that package's SettingString for why it is
// duplicated per-package rather than shared).
func settingString(app core.App, key, def string) string {
	record, err := app.FindFirstRecordByFilter("settings", "key = {:key}", map[string]any{"key": key})
	if err != nil {
		return def
	}
	var v any
	if err := json.Unmarshal([]byte(record.GetString("value")), &v); err != nil {
		return def
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return def
	}
	return s
}

// AlertData holds template-friendly data for rendering notification messages.
type AlertData struct {
	AgentName  string
	MetricType string
	Value      float64
	Threshold  float64
	Condition  string
	Severity   string
	Status     string
	Message    string
	FiredAt    string
	ResolvedAt string

	// AcknowledgedAt/AcknowledgedBy/EscalatedAt surface the alert's
	// lifecycle context (see internal/hub/alerts.Engine) so an operator
	// reading any channel's rendered text — not just the dashboard — can
	// tell an alert has already been acknowledged or escalated. Message
	// itself already carries the agent's tags and the rule's target (when
	// set), baked in by Engine.buildMessage at fire/refresh time.
	AcknowledgedAt string
	AcknowledgedBy string
	EscalatedAt    string
}

// defaultTemplate is the Go template used to format notification messages.
const defaultTemplate = `🚨 NexWatch Alert
Status: {{.Status}} | Severity: {{.Severity}}
{{.Message}}
Time: {{.FiredAt}}{{if .ResolvedAt}}
Resolved: {{.ResolvedAt}}{{end}}{{if .AcknowledgedAt}}
Acknowledged by {{.AcknowledgedBy}} at {{.AcknowledgedAt}}{{end}}{{if .EscalatedAt}}
Escalated at {{.EscalatedAt}}{{end}}`

// RenderMessage formats an alert notification using the default template.
// severity is rendered verbatim into the "Severity: ..." line — callers pass
// AlertContext.Severity (resolved from the rule, falling back to
// SeverityFromMessage) so the line is never blank the way it was before
// AlertData.Severity was wired up at all.
func RenderMessage(alert *core.Record, severity string) string {
	data := AlertData{
		Severity:       severity,
		Status:         alert.GetString("status"),
		Value:          alert.GetFloat("value"),
		Message:        alert.GetString("message"),
		FiredAt:        alert.GetString("fired_at"),
		ResolvedAt:     alert.GetString("resolved_at"),
		AcknowledgedAt: alert.GetString("acknowledged_at"),
		AcknowledgedBy: alert.GetString("acknowledged_by"),
		EscalatedAt:    alert.GetString("escalated_at"),
	}

	tmpl, err := template.New("alert").Parse(defaultTemplate)
	if err != nil {
		return data.Message
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return data.Message
	}

	return buf.String()
}

// ParseChannelConfig extracts the config JSON from a notification channel record.
func ParseChannelConfig(channel *core.Record) (map[string]any, error) {
	configStr := channel.GetString("config")
	if configStr == "" {
		return nil, fmt.Errorf("channel %s has empty config", channel.Id)
	}

	var config map[string]any
	if err := json.Unmarshal([]byte(configStr), &config); err != nil {
		return nil, fmt.Errorf("invalid config JSON for channel %s: %w", channel.Id, err)
	}

	return config, nil
}

// GetConfigString extracts a string value from channel config.
func GetConfigString(config map[string]any, key string) string {
	if v, ok := config[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// GetConfigInt extracts an integer value from channel config.
func GetConfigInt(config map[string]any, key string) int {
	if v, ok := config[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case json.Number:
			i, _ := n.Int64()
			return int(i)
		}
	}
	return 0
}

// ConfigError signals that a channel's configuration is invalid or
// incomplete (e.g. a required field is missing), as opposed to a transient
// delivery failure (network error, non-2xx response, timeout). The
// notification test endpoint (see api.handleTestNotification) uses
// errors.As against this type to return an HTTP 400 with a clear message
// instead of a 500, so a misconfigured channel reads as "fix your config"
// rather than "something went wrong."
type ConfigError struct {
	msg string
}

func (e *ConfigError) Error() string { return e.msg }

// NewConfigError builds a ConfigError, following fmt.Errorf's
// format/args convention.
func NewConfigError(format string, args ...any) error {
	return &ConfigError{msg: fmt.Sprintf(format, args...)}
}

// SeverityFromMessage recovers the "[severity]" tag that
// alerts.Engine.buildMessage prefixes onto every rendered alert message
// (e.g. "[critical] cpu on web-01: cpu > 80.0 (current: 95.0)"). Alert
// records carry no persisted severity column of their own — the rule's
// severity is only known at fire time, and the Notifier interface receives
// just the alert and channel records — so parsing the message is the only
// way a channel can recover it after the fact. Returns "warning" or
// "critical"; any message without a recognizable bracket tag (e.g. the
// synthetic "[TEST]" message SendTestNotification sends) defaults to
// "critical" so a channel that pages on severity fails toward paging
// rather than silently downgrading an unrecognized alert.
func SeverityFromMessage(message string) string {
	if strings.HasPrefix(message, "[") {
		if end := strings.Index(message, "]"); end > 0 {
			switch strings.ToLower(message[1:end]) {
			case "warning":
				return "warning"
			case "critical":
				return "critical"
			}
		}
	}
	return "critical"
}
