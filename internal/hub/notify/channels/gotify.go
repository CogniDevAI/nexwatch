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

// GotifyNotifier sends alert notifications to a self-hosted Gotify server.
type GotifyNotifier struct {
	client *http.Client
}

// NewGotifyNotifier creates a new Gotify notifier.
func NewGotifyNotifier() *GotifyNotifier {
	return &GotifyNotifier{client: &http.Client{}}
}

// Type returns the channel type identifier.
func (n *GotifyNotifier) Type() string {
	return "gotify"
}

// gotifyPriority returns the configured priority if set, otherwise a
// default derived from status/severity on Gotify's conventional 0-10
// scale: resolved is low (2), critical is high (8), warning is medium (5).
func gotifyPriority(configured int, status, severity string) int {
	if configured > 0 {
		return configured
	}
	if status == "resolved" {
		return 2
	}
	if severity == "critical" {
		return 8
	}
	return 5
}

// Send delivers an alert notification to a Gotify server.
// Channel config expects: server_url, app_token (required); priority
// (optional, defaults by status/severity when unset).
func (n *GotifyNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record, alertCtx notify.AlertContext) error {
	config, err := notify.ParseChannelConfig(channel)
	if err != nil {
		return err
	}

	serverURL := notify.GetConfigString(config, "server_url")
	appToken := notify.GetConfigString(config, "app_token")
	if serverURL == "" || appToken == "" {
		return notify.NewConfigError("gotify config missing required fields (server_url, app_token)")
	}

	status := alert.GetString("status")
	severity := alertCtx.Severity

	extras := map[string]any{
		"client::display": map[string]any{
			"contentType": "text/markdown",
		},
	}
	// client::notification.click.url makes the Gotify app open the
	// dashboard when the notification is tapped — only set when a public
	// base URL is configured, since Gotify needs an absolute URL here.
	if alertCtx.DashboardURL != "" {
		extras["client::notification"] = map[string]any{
			"click": map[string]any{"url": alertCtx.DashboardURL},
		}
	}

	payload := map[string]any{
		"title":    fmt.Sprintf("NexWatch Alert — %s", strings.ToUpper(status)),
		"message":  notify.RenderMessage(alert, severity),
		"priority": gotifyPriority(notify.GetConfigInt(config, "priority"), status, severity),
		"extras":   extras,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal gotify payload: %w", err)
	}

	url := strings.TrimRight(serverURL, "/") + "/message"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create gotify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", appToken)

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("gotify request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("gotify returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
