package channels

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

// ntfyDefaultServerURL is used when a channel's config omits server_url,
// pointing at the public ntfy.sh instance.
const ntfyDefaultServerURL = "https://ntfy.sh"

// NtfyNotifier sends alert notifications via an ntfy (https://ntfy.sh, or a
// self-hosted instance) topic.
type NtfyNotifier struct {
	client *http.Client
}

// NewNtfyNotifier creates a new ntfy notifier.
func NewNtfyNotifier() *NtfyNotifier {
	return &NtfyNotifier{client: &http.Client{}}
}

// Type returns the channel type identifier.
func (n *NtfyNotifier) Type() string {
	return "ntfy"
}

// ntfyPriority returns the configured priority (1-5) if valid, otherwise a
// default derived from status/severity: resolved alerts are low priority
// (2, "quiet"), critical alerts are urgent (5), everything else (warning)
// is the ntfy default (3).
func ntfyPriority(configured int, status, severity string) int {
	if configured >= 1 && configured <= 5 {
		return configured
	}
	if status == "resolved" {
		return 2
	}
	if severity == "critical" {
		return 5
	}
	return 3
}

// ntfyTags picks an ntfy emoji-shortcode tag (rendered as an emoji by ntfy
// clients) matching the alert's status/severity.
func ntfyTags(status, severity string) string {
	if status == "resolved" {
		return "white_check_mark"
	}
	if severity == "critical" {
		return "rotating_light"
	}
	return "warning"
}

// Send delivers an alert notification via an ntfy topic.
// Channel config expects: topic (required), server_url (optional, default
// https://ntfy.sh), token (optional, sent as a Bearer token), priority
// (optional, 1-5; defaults by status/severity when unset or out of range).
func (n *NtfyNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record, alertCtx notify.AlertContext) error {
	config, err := notify.ParseChannelConfig(channel)
	if err != nil {
		return err
	}

	topic := notify.GetConfigString(config, "topic")
	if topic == "" {
		return notify.NewConfigError("ntfy config missing required field: topic")
	}

	serverURL := notify.GetConfigString(config, "server_url")
	if serverURL == "" {
		serverURL = ntfyDefaultServerURL
	}
	serverURL = strings.TrimRight(serverURL, "/")

	status := alert.GetString("status")
	severity := alertCtx.Severity
	priority := ntfyPriority(notify.GetConfigInt(config, "priority"), status, severity)

	message := notify.RenderMessage(alert, severity)
	title := fmt.Sprintf("NexWatch Alert — %s", strings.ToUpper(status))

	url := serverURL + "/" + topic

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(message))
	if err != nil {
		return fmt.Errorf("failed to create ntfy request: %w", err)
	}
	req.Header.Set("Title", title)
	req.Header.Set("Priority", strconv.Itoa(priority))

	// Tags combines the status/severity emoji shortcode with the agent's
	// real tags (ntfy renders a recognized shortcode as an emoji and any
	// other tag as plain text), so an operator scanning notifications sees
	// which tagged group of agents is affected without opening the app.
	tags := ntfyTags(status, severity)
	if len(alertCtx.AgentTags) > 0 {
		tags = tags + "," + strings.Join(alertCtx.AgentTags, ",")
	}
	req.Header.Set("Tags", tags)

	// Click makes the notification open the dashboard when tapped, instead
	// of just ntfy's own app/web viewer — only set when a public base URL
	// is configured, since ntfy needs an absolute URL here.
	if alertCtx.DashboardURL != "" {
		req.Header.Set("Click", alertCtx.DashboardURL)
	}

	if token := notify.GetConfigString(config, "token"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ntfy returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
