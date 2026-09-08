package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// newNotificationChannelTypes are the notification_channels.type values
// added by this migration on top of the ones collections.go originally
// defined (email, webhook, telegram, discord). Each has a matching
// notify.Notifier implementation registered in cmd/hub/main.go
// (internal/hub/notify/channels/{slack,teams,pagerduty,ntfy,gotify}.go).
var newNotificationChannelTypes = []string{"slack", "teams", "pagerduty", "ntfy", "gotify"}

// originalNotificationChannelTypes is the type Values list exactly as
// collections.go first defined it, used to restore it on downgrade.
var originalNotificationChannelTypes = []string{"email", "webhook", "telegram", "discord"}

// This migration extends notification_channels.type with five new channel
// types. It only depends on the "notification_channels" collection already
// existing (created in collections.go, "c" sorts before "n" byte-wise), so
// it can run independently of every other migration filed after
// collections.go.
func init() {
	m.Register(func(app core.App) error {
		channels, err := app.FindCollectionByNameOrId("notification_channels")
		if err != nil {
			return err
		}

		field := channels.Fields.GetByName("type")
		selectField, ok := field.(*core.SelectField)
		if !ok {
			return nil // schema drifted unexpectedly; leave it alone rather than guess
		}
		for _, v := range newNotificationChannelTypes {
			if !slices.Contains(selectField.Values, v) {
				selectField.Values = append(selectField.Values, v)
			}
		}

		return app.Save(channels)
	}, func(app core.App) error {
		channels, err := app.FindCollectionByNameOrId("notification_channels")
		if err != nil {
			return err
		}

		if field := channels.Fields.GetByName("type"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				selectField.Values = slices.Clone(originalNotificationChannelTypes)
			}
		}

		return app.Save(channels)
	})
}
