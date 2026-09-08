package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

// TeamsNotifier sends alert notifications via a Microsoft Teams Incoming
// Webhook, using an Adaptive Card payload.
type TeamsNotifier struct {
	client *http.Client
}

// NewTeamsNotifier creates a new Microsoft Teams notifier.
func NewTeamsNotifier() *TeamsNotifier {
	return &TeamsNotifier{client: &http.Client{}}
}

// Type returns the channel type identifier.
func (n *TeamsNotifier) Type() string {
	return "teams"
}

// adaptiveCardColor maps status/severity to an Adaptive Card TextBlock
// color token: "good" (green) once resolved, "attention" (red) for a
// critical firing alert, "warning" (amber) otherwise.
func adaptiveCardColor(status, severity string) string {
	if status == "resolved" {
		return "good"
	}
	if severity == "critical" {
		return "attention"
	}
	return "warning"
}

// Send delivers an alert notification as a Microsoft Teams Adaptive Card.
// Channel config expects: webhook_url
func (n *TeamsNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record, alertCtx notify.AlertContext) error {
	config, err := notify.ParseChannelConfig(channel)
	if err != nil {
		return err
	}

	webhookURL := notify.GetConfigString(config, "webhook_url")
	if webhookURL == "" {
		return notify.NewConfigError("teams config missing required field: webhook_url")
	}

	status := alert.GetString("status")
	severity := alertCtx.Severity

	facts := []map[string]any{
		{"title": "Status", "value": status},
		{"title": "Severity", "value": severity},
		{"title": "Value", "value": fmt.Sprintf("%.2f", alert.GetFloat("value"))},
		{"title": "Alert ID", "value": alert.Id},
		{"title": "Fired at", "value": alert.GetString("fired_at")},
	}
	if resolvedAt := alert.GetString("resolved_at"); resolvedAt != "" {
		facts = append(facts, map[string]any{"title": "Resolved at", "value": resolvedAt})
	}
	if alertCtx.Hostname != "" {
		facts = append(facts, map[string]any{"title": "Host", "value": alertCtx.Hostname})
	}
	if alertCtx.RuleName != "" {
		facts = append(facts, map[string]any{"title": "Rule", "value": alertCtx.RuleName})
	}
	if alertCtx.CheckName != "" {
		facts = append(facts, map[string]any{"title": "Check", "value": alertCtx.CheckName})
	}
	if len(alertCtx.AgentTags) > 0 {
		facts = append(facts, map[string]any{"title": "Tags", "value": strings.Join(alertCtx.AgentTags, ", ")})
	}

	cardBody := []map[string]any{
		{
			"type":   "TextBlock",
			"text":   fmt.Sprintf("NexWatch Alert: %s", status),
			"weight": "bolder",
			"size":   "medium",
			"color":  adaptiveCardColor(status, severity),
			"wrap":   true,
		},
		{
			"type":  "FactSet",
			"facts": facts,
		},
		{
			"type": "TextBlock",
			"text": alert.GetString("message"),
			"wrap": true,
		},
	}

	card := map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":    "AdaptiveCard",
		"version": "1.4",
		"body":    cardBody,
	}
	if alertCtx.DashboardURL != "" {
		card["actions"] = []map[string]any{
			{"type": "Action.OpenUrl", "title": "View in NexWatch", "url": alertCtx.DashboardURL},
		}
	}

	payload := map[string]any{
		"type": "message",
		"attachments": []map[string]any{
			{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content":     card,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal teams payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create teams request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NexWatch/0.1.0")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("teams request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("teams webhook returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
