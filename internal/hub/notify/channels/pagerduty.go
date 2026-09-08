package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

// pagerDutyEventsURL is the production PagerDuty Events API v2 endpoint.
const pagerDutyEventsURL = "https://events.pagerduty.com/v2/enqueue"

// pagerDutyTimestampLayout matches the format alerts.Engine stamps onto
// fired_at/resolved_at (e.g. "2026-01-01 00:00:00.000Z").
const pagerDutyTimestampLayout = "2006-01-02 15:04:05.000Z"

// PagerDutyNotifier sends alert notifications via the PagerDuty Events API
// v2 (Events API v2 "Alert Events" integration).
type PagerDutyNotifier struct {
	client *http.Client

	// baseURL defaults to pagerDutyEventsURL and exists as a seam so tests
	// can point the notifier at a local httptest server instead of the
	// real PagerDuty API.
	baseURL string
}

// NewPagerDutyNotifier creates a new PagerDuty notifier.
func NewPagerDutyNotifier() *PagerDutyNotifier {
	return &PagerDutyNotifier{
		client:  &http.Client{},
		baseURL: pagerDutyEventsURL,
	}
}

// Type returns the channel type identifier.
func (n *PagerDutyNotifier) Type() string {
	return "pagerduty"
}

// pagerDutyTimestamp reformats an alert's fired_at/resolved_at into
// RFC3339 for PagerDuty's payload.timestamp field. Returns "" (letting the
// field be omitted — PagerDuty treats it as optional) when the value
// doesn't parse, e.g. the empty string on a firing alert's resolved_at.
func pagerDutyTimestamp(value string) string {
	t, err := time.Parse(pagerDutyTimestampLayout, value)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// Send delivers an alert notification via the PagerDuty Events API v2.
// Channel config expects: routing_key
//
// A firing (or re-firing) alert sends event_action "trigger"; a resolved
// alert sends event_action "resolve" — both using the alert record's ID as
// the dedup_key, so PagerDuty ties the trigger and its later resolution to
// the same incident. This depends on the alerts engine dispatching a
// notification on resolution (see alerts.Engine.resolveAlert).
func (n *PagerDutyNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record, alertCtx notify.AlertContext) error {
	config, err := notify.ParseChannelConfig(channel)
	if err != nil {
		return err
	}

	routingKey := notify.GetConfigString(config, "routing_key")
	if routingKey == "" {
		return notify.NewConfigError("pagerduty config missing required field: routing_key")
	}

	status := alert.GetString("status")
	eventAction := "trigger"
	if status == "resolved" {
		eventAction = "resolve"
	}

	severity := alertCtx.Severity
	message := alert.GetString("message")

	customDetails := map[string]any{
		"alert_id": alert.Id,
		"status":   status,
		"value":    alert.GetFloat("value"),
		"message":  message,
		"agent_id": alert.GetString("agent_id"),
		"rule_id":  alert.GetString("rule_id"),
	}
	if alertCtx.Hostname != "" {
		customDetails["hostname"] = alertCtx.Hostname
	}
	if len(alertCtx.AgentTags) > 0 {
		customDetails["agent_tags"] = alertCtx.AgentTags
	}
	if alertCtx.RuleName != "" {
		customDetails["rule_name"] = alertCtx.RuleName
	}
	if alertCtx.CheckName != "" {
		customDetails["check_name"] = alertCtx.CheckName
		customDetails["check_target"] = alertCtx.CheckTarget
	}

	eventPayload := map[string]any{
		"summary":        message,
		"source":         "nexwatch",
		"severity":       severity,
		"custom_details": customDetails,
	}

	timestampSource := alert.GetString("fired_at")
	if status == "resolved" {
		timestampSource = alert.GetString("resolved_at")
	}
	if ts := pagerDutyTimestamp(timestampSource); ts != "" {
		eventPayload["timestamp"] = ts
	}

	payload := map[string]any{
		"routing_key":  routingKey,
		"event_action": eventAction,
		"dedup_key":    alert.Id,
		"client":       "NexWatch",
		"payload":      eventPayload,
	}
	if alertCtx.DashboardURL != "" {
		payload["client_url"] = alertCtx.DashboardURL
		payload["links"] = []map[string]any{
			{"href": alertCtx.DashboardURL, "text": "View in NexWatch"},
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal pagerduty payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create pagerduty request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("pagerduty request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("pagerduty API returned %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}
