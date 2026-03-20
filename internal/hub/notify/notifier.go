package notify

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// Notifier is the interface that all notification channel types must implement.
type Notifier interface {
	// Type returns the channel type identifier (e.g., "email", "webhook", "telegram", "discord").
	Type() string

	// Send delivers a notification for the given alert through the channel configuration.
	Send(ctx context.Context, alert *core.Record, channel *core.Record) error
}
