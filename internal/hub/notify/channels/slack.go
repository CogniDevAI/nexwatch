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

// SlackNotifier sends alert notifications via a Slack Incoming Webhook.
type SlackNotifier struct {
	client *http.Client
}

// NewSlackNotifier creates a new Slack notifier.
func NewSlackNotifier() *SlackNotifier {
	return &SlackNotifier{client: &http.Client{}}
}

// Type returns the channel type identifier.
func (n *SlackNotifier) Type() string {
	return "slack"
}

// slackColor picks the Slack attachment sidebar color: resolved alerts are
// green regardless of severity (the incident is over), otherwise red for
// critical and amber for warning, matching the same red/amber/green scheme
// used by the other severity-aware channels (Teams, ntfy, Gotify).
func slackColor(status, severity string) string {
	if status == "resolved" {
		return "#2eb67d"
	}
	if severity == "warning" {
		return "#ecb22e"
	}
	return "#e01e5a"
}

// Send delivers an alert notification via a Slack Incoming Webhook.
// Channel config expects: webhook_url
func (n *SlackNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record, alertCtx notify.AlertContext) error {
	config, err := notify.ParseChannelConfig(channel)
	if err != nil {
		return err
	}

	webhookURL := notify.GetConfigString(config, "webhook_url")
	if webhookURL == "" {
		return notify.NewConfigError("slack config missing required field: webhook_url")
	}

	status := alert.GetString("status")
	severity := alertCtx.Severity
	text := notify.RenderMessage(alert, severity)

	headerText := fmt.Sprintf("%s — %s", strings.ToUpper(severity), strings.ToUpper(status))

	fields := []map[string]any{
		{"type": "mrkdwn", "text": fmt.Sprintf("*Status:*\n%s", status)},
		{"type": "mrkdwn", "text": fmt.Sprintf("*Severity:*\n%s", severity)},
		{"type": "mrkdwn", "text": fmt.Sprintf("*Value:*\n%.2f", alert.GetFloat("value"))},
		{"type": "mrkdwn", "text": fmt.Sprintf("*Alert ID:*\n%s", alert.Id)},
	}
	if alertCtx.Hostname != "" {
		fields = append(fields, map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Host:*\n%s", alertCtx.Hostname)})
	}
	if alertCtx.RuleName != "" {
		fields = append(fields, map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Rule:*\n%s", alertCtx.RuleName)})
	}
	if alertCtx.CheckName != "" {
		fields = append(fields, map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Check:*\n%s", alertCtx.CheckName)})
	}
	if len(alertCtx.AgentTags) > 0 {
		fields = append(fields, map[string]any{"type": "mrkdwn", "text": fmt.Sprintf("*Tags:*\n%s", strings.Join(alertCtx.AgentTags, ", "))})
	}

	blocks := []map[string]any{
		{
			"type": "header",
			"text": map[string]any{"type": "plain_text", "text": headerText, "emoji": true},
		},
		{
			"type":   "section",
			"fields": fields,
		},
		{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": alert.GetString("message")},
		},
	}
	if alertCtx.DashboardURL != "" {
		blocks = append(blocks, map[string]any{
			"type": "actions",
			"elements": []map[string]any{
				{
					"type": "button",
					"text": map[string]any{"type": "plain_text", "text": "View in NexWatch", "emoji": true},
					"url":  alertCtx.DashboardURL,
				},
			},
		})
	}

	payload := map[string]any{
		"text": text,
		"attachments": []map[string]any{
			{
				"color":  slackColor(status, severity),
				"blocks": blocks,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NexWatch/0.1.0")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("slack webhook returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
