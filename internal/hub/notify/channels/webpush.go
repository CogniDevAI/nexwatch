package channels

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
	"github.com/CogniDevAI/nexwatch/internal/hub/push"
)

// WebPushNotifier delivers alert notifications as browser Web Push
// messages (F11 — PWA + Web Push notifications) to every subscribed device
// matching the channel's configured audience.
type WebPushNotifier struct {
	app    core.App
	sender *push.Sender
}

// NewWebPushNotifier creates a new Web Push notifier.
func NewWebPushNotifier(app core.App) *WebPushNotifier {
	return &WebPushNotifier{app: app, sender: push.NewSender(app)}
}

// Type returns the channel type identifier.
func (n *WebPushNotifier) Type() string {
	return "webpush"
}

// Send delivers an alert notification via Web Push to every subscription
// matching the channel's configured audience ("all"/"admins"/"operators",
// default "all"). Unlike every other Notifier, this fans out to a
// potentially large number of independent recipients: a delivery failure
// for one subscription (expired, rate limited, network error) is logged
// and does not stop delivery to the rest, so Send only returns an error for
// a setup-level problem (VAPID keys unavailable, subscriptions unreadable)
// rather than aggregating every per-subscription outcome — an empty
// audience is not itself a failure.
func (n *WebPushNotifier) Send(ctx context.Context, alert *core.Record, channel *core.Record, alertCtx notify.AlertContext) error {
	config, err := notify.ParseChannelConfig(channel)
	if err != nil {
		return err
	}

	audience := notify.GetConfigString(config, "audience")
	if audience == "" {
		audience = push.AudienceAll
	}

	subs, err := push.SubscriptionsForAudience(n.app, audience)
	if err != nil {
		return fmt.Errorf("failed to load push subscriptions: %w", err)
	}
	if len(subs) == 0 {
		return nil
	}

	payload := buildAlertPayload(alert, alertCtx)

	for _, sub := range subs {
		if sendErr := n.sender.SendToSubscription(ctx, sub, payload); sendErr != nil {
			slog.Warn("web push delivery failed", "endpoint", sub.GetString("endpoint"), "error", sendErr)
		}
	}

	return nil
}

// buildAlertPayload maps an alert record onto the push.Payload JSON shape
// the service worker (ui/src/sw.ts) renders as a browser notification.
// "url" points at the affected host's own page (or the checks/alert
// history page for a check-based/unresolvable alert — see
// AlertContext.DashboardPath) rather than always the generic alert history
// list, so tapping the notification takes the operator straight to the
// host that fired it. It uses alertCtx.DashboardURL (absolute, prefixed
// with "public_base_url") when that setting is configured, falling back to
// the bare relative path — resolved against the current origin by the
// browser — otherwise.
func buildAlertPayload(alert *core.Record, alertCtx notify.AlertContext) push.Payload {
	status := alert.GetString("status")

	url := alertCtx.DashboardURL
	if url == "" {
		url = alertCtx.DashboardPath
	}

	return push.Payload{
		Title:     fmt.Sprintf("NexWatch — %s", strings.ToUpper(status)),
		Body:      notify.RenderMessage(alert, alertCtx.Severity),
		URL:       url,
		Severity:  alertCtx.Severity,
		Tag:       "nexwatch-alert-" + alert.GetString("rule_id"),
		Timestamp: time.Now().UTC().UnixMilli(),
	}
}
