package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration creates the "push_subscriptions" collection: one row per
// browser/device Web Push subscription (F11 — PWA + Web Push
// notifications). It depends on the "users" auth collection, which
// PocketBase's own system migrations create before any migration in this
// package runs (see users_roles.go's comment on the same dependency), so
// it has no filename-ordering dependency on any other file here.
//
// Design notes:
//   - endpoint is unique: a browser's push subscription endpoint uniquely
//     identifies one device/browser installation, and the "webpush"
//     notifier (internal/hub/notify/channels/webpush.go) deletes a
//     subscription outright on a 404/410 response rather than updating it,
//     so there is never a legitimate reason for two rows to share one
//     endpoint.
//   - No UpdateRule: a subscription is deleted and re-created (by the UI's
//     "Enable on this device" flow) rather than edited in place, so only
//     list/view/create/delete are needed for the owning user; superusers
//     bypass every rule regardless.
func init() {
	ownRow := "@request.auth.id != '' && user_id = @request.auth.id"
	ownRowCreate := "@request.auth.id != '' && @request.auth.id = user_id"

	m.Register(func(app core.App) error {
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}

		pushSubs := core.NewBaseCollection("push_subscriptions")
		pushSubs.ListRule = &ownRow
		pushSubs.ViewRule = &ownRow
		pushSubs.CreateRule = &ownRowCreate
		pushSubs.DeleteRule = &ownRow
		pushSubs.Fields.Add(
			&core.RelationField{
				Name:          "user_id",
				Required:      true,
				CollectionId:  users.Id,
				MaxSelect:     1,
				CascadeDelete: true,
			},
			&core.TextField{Name: "endpoint", Required: true, Max: 2000},
			&core.TextField{Name: "p256dh", Required: true, Max: 255},
			&core.TextField{Name: "auth", Required: true, Max: 255},
			&core.TextField{Name: "user_agent", Max: 500},
			&core.DateField{Name: "last_seen"},
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		pushSubs.Indexes = []string{
			"CREATE UNIQUE INDEX idx_push_subscriptions_endpoint ON push_subscriptions (endpoint)",
			"CREATE INDEX idx_push_subscriptions_user ON push_subscriptions (user_id)",
		}
		return app.Save(pushSubs)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("push_subscriptions")
		if err != nil {
			return nil // already gone
		}
		return app.Delete(col)
	})
}
