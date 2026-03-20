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

// WebhookNotifier sends alert notifications via HTTP webhooks.
type WebhookNotifier struct {
	client *http.Client
}

// NewWebhookNotifier creates a new webhook notifier.
func NewWebhookNotifier() *WebhookNotifier {
	return &WebhookNotifier{
		client: &http.Client{},
	}
}

// Type returns the channel type identifier.
func (n *WebhookNotifier) Type() string {
	return "webhook"
}

// Send delivers an alert notification via HTTP POST (or configured method).
// Channel config expects: url, method (optional, default POST), headers (optional JSON object)
func (n *WebhookNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record) error {
	config, err := notify.ParseChannelConfig(channel)
	if err != nil {
		return err
	}

	url := notify.GetConfigString(config, "url")
	if url == "" {
		return fmt.Errorf("webhook config missing required field: url")
	}

	method := notify.GetConfigString(config, "method")
	if method == "" {
		method = "POST"
	}

	// Build JSON payload.
	payload := map[string]any{
		"source":  "nexwatch",
		"alert_id": alert.Id,
		"status":  alert.GetString("status"),
		"value":   alert.GetFloat("value"),
		"message": alert.GetString("message"),
		"fired_at": alert.GetString("fired_at"),
		"resolved_at": alert.GetString("resolved_at"),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "NexWatch/0.1.0")

	// Apply custom headers from config.
	if headersRaw, ok := config["headers"]; ok {
		if headersMap, ok := headersRaw.(map[string]any); ok {
			for k, v := range headersMap {
				if s, ok := v.(string); ok {
					req.Header.Set(k, s)
				}
			}
		}
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response body for error diagnostics.
	_, _ = io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}
