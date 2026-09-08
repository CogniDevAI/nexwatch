package push

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/pocketbase/pocketbase/core"
)

// Audience values accepted by a "webpush" notification channel's config.
const (
	AudienceAll       = "all"
	AudienceAdmins    = "admins"
	AudienceOperators = "operators"
)

// Payload is the JSON body pushed to the browser's service worker (see
// ui/src/sw.ts's "push" event handler for the matching shape).
type Payload struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	URL       string `json:"url,omitempty"`
	Severity  string `json:"severity,omitempty"`
	Tag       string `json:"tag,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// SendResult is the outcome of sending to one subscription — used by
// POST /api/custom/push/test to report a per-endpoint result.
type SendResult struct {
	Endpoint string `json:"endpoint"`
	Success  bool   `json:"success"`
	Error    string `json:"error,omitempty"`
}

// Sender delivers Web Push messages to individual subscriptions.
type Sender struct {
	App core.App
	// Client is an http.Client seam for tests (pointed at an httptest
	// server via the subscription's own endpoint URL, not a rewritten
	// transport). Nil uses http.DefaultClient.
	Client *http.Client
}

// NewSender creates a Sender with a real *http.Client.
func NewSender(app core.App) *Sender {
	return &Sender{App: app, Client: &http.Client{}}
}

// SubscriptionsForAudience returns every "push_subscriptions" record whose
// owning user matches audience: AudienceAll (every subscribed user,
// regardless of role), AudienceAdmins ("admin" role only), or
// AudienceOperators ("operator" role only — not "admin" too; this is an
// exact role-bucket match rather than the "operator or above" hierarchy
// used for permission checks elsewhere, e.g. RequireRole, since a channel
// audience is a targeting choice, not a permission floor). Superusers never
// appear here: a subscription's "user_id" relation only points at the
// "users" collection, not "_superusers".
func SubscriptionsForAudience(app core.App, audience string) ([]*core.Record, error) {
	filter := ""
	switch audience {
	case AudienceAdmins:
		filter = "user_id.role = 'admin'"
	case AudienceOperators:
		filter = "user_id.role = 'operator'"
	}
	return app.FindRecordsByFilter("push_subscriptions", filter, "", 0, 0)
}

// SendToSubscription sends payload as a Web Push message to sub (a
// "push_subscriptions" record). On a 404/410 response (the push service
// reports the subscription no longer exists) it deletes sub and returns an
// error describing the pruning. On 429 (rate limited) it logs and returns
// an error without deleting the subscription, since a rate limit is
// transient and says nothing about whether the subscription itself is
// still valid.
func (s *Sender) SendToSubscription(ctx context.Context, sub *core.Record, payload Payload) error {
	publicKey, privateKey, subject, err := EnsureVAPIDKeys(s.App)
	if err != nil {
		return err
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal push payload: %w", err)
	}

	webSub := &webpush.Subscription{
		Endpoint: sub.GetString("endpoint"),
		Keys: webpush.Keys{
			P256dh: sub.GetString("p256dh"),
			Auth:   sub.GetString("auth"),
		},
	}

	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := webpush.SendNotificationWithContext(ctx, body, webSub, &webpush.Options{
		HTTPClient: client,
		// webpush-go's own getVAPIDAuthorizationHeader prepends "mailto:"
		// to Subscriber unless it already starts with "https:" — it does
		// NOT recognize an already-present "mailto:" prefix, so passing
		// subject (which VAPIDSubjectSetting stores in full URI form, e.g.
		// "mailto:admin@localhost") straight through would double it into
		// "mailto:mailto:admin@localhost". Stripping a leading "mailto:"
		// here lets webpush-go re-add it exactly once, while an "https:"
		// subject (the VAPID spec's other allowed form) passes through
		// untouched either way.
		Subscriber:      strings.TrimPrefix(subject, "mailto:"),
		VAPIDPublicKey:  publicKey,
		VAPIDPrivateKey: privateKey,
		TTL:             60,
	})
	if err != nil {
		return fmt.Errorf("web push request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	endpoint := sub.GetString("endpoint")
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		if delErr := s.App.Delete(sub); delErr != nil {
			slog.Error("failed to prune stale push subscription", "endpoint", endpoint, "error", delErr)
		} else {
			slog.Info("pruned stale push subscription", "endpoint", endpoint, "status", resp.StatusCode)
		}
		return fmt.Errorf("push subscription no longer valid (status %d), removed", resp.StatusCode)
	case resp.StatusCode == http.StatusTooManyRequests:
		slog.Warn("push service rate limited, skipping", "endpoint", endpoint, "retry_after", resp.Header.Get("Retry-After"))
		return fmt.Errorf("push service rate limited (retry after %s)", resp.Header.Get("Retry-After"))
	case resp.StatusCode >= 300:
		return fmt.Errorf("push service returned status %d", resp.StatusCode)
	}

	return nil
}
