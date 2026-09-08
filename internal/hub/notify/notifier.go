package notify

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// Notifier is the interface that all notification channel types must implement.
type Notifier interface {
	// Type returns the channel type identifier (e.g., "email", "webhook", "telegram", "discord").
	Type() string

	// Send delivers a notification for the given alert through the channel
	// configuration. alertCtx carries the hostname/tags/rule/check/severity/
	// dashboard-link details resolved once by the caller (see
	// Service.dispatchToChannels and Service.SendTestNotification) so every
	// channel notified about the same alert renders a consistent, detailed
	// view without each independently re-querying collections.
	Send(ctx context.Context, alert *core.Record, channel *core.Record, alertCtx AlertContext) error
}
