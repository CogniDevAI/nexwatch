package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

// DiscordNotifier sends alert notifications via Discord webhooks.
type DiscordNotifier struct {
	client *http.Client
}

// NewDiscordNotifier creates a new Discord notifier.
func NewDiscordNotifier() *DiscordNotifier {
	return &DiscordNotifier{
		client: &http.Client{},
	}
}

// Type returns the channel type identifier.
func (n *DiscordNotifier) Type() string {
	return "discord"
}

// Send delivers an alert notification via Discord webhook.
// Channel config expects: webhook_url
func (n *DiscordNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record) error {
	config, err := notify.ParseChannelConfig(channel)
	if err != nil {
		return err
	}

	webhookURL := notify.GetConfigString(config, "webhook_url")
	if webhookURL == "" {
		return fmt.Errorf("discord config missing required field: webhook_url")
	}

	// Render the message text.
	text := notify.RenderMessage(alert)

	// Determine embed color based on alert status.
	color := 15158332 // red for firing
	status := alert.GetString("status")
	if status == "resolved" {
		color = 3066993 // green for resolved
	}

	// Build Discord webhook payload with embed.
	payload := map[string]any{
		"username":   "NexWatch",
		"avatar_url": "",
		"content":    "",
		"embeds": []map[string]any{
			{
				"title":       fmt.Sprintf("Alert: %s", status),
				"description": text,
				"color":       color,
				"timestamp":   alert.GetString("fired_at"),
				"footer": map[string]string{
					"text": "NexWatch Monitoring",
				},
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal discord payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create discord request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NexWatch/0.1.0")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("discord request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// Discord returns 204 No Content on success.
	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord webhook returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
