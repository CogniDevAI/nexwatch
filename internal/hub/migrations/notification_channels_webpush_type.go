package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration adds "webpush" to notification_channels.type (F11 — PWA +
// Web Push notifications), following the same additive pattern as
// notification_channels_extra_types.go. The filename deliberately sorts
// after that one ("extra_types" < "webpush_type" byte-wise) so downgrades
// apply in the correct LIFO order: this migration's down-migration removes
// just "webpush" before notification_channels_extra_types.go's own
// down-migration resets the whole Values list to its original four.
var webpushNotificationChannelType = "webpush"

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
		if !slices.Contains(selectField.Values, webpushNotificationChannelType) {
			selectField.Values = append(selectField.Values, webpushNotificationChannelType)
		}

		return app.Save(channels)
	}, func(app core.App) error {
		channels, err := app.FindCollectionByNameOrId("notification_channels")
		if err != nil {
			return err
		}

		if field := channels.Fields.GetByName("type"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				selectField.Values = slices.DeleteFunc(slices.Clone(selectField.Values), func(v string) bool {
					return v == webpushNotificationChannelType
				})
			}
		}

		return app.Save(channels)
	})
}
