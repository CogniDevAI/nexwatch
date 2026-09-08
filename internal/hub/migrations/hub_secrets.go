package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration creates the "hub_secrets" collection: server-only secret
// storage (e.g. the VAPID private key for Web Push — F11, see
// internal/hub/hubsecrets) that must never be readable by a regular
// authenticated user. Unlike "settings" (readable by any authenticated
// user, see collections.go), every API rule here is nil, which PocketBase
// resolves to "superusers only". Go code reads/writes it directly via
// app.Save/FindFirstRecordByFilter (bypassing API rules entirely, the same
// convention as "metrics"/"audit_log"), so the nil rules only matter for
// anyone hitting the collection through the REST API or the PocketBase
// Admin UI as a regular "users" record.
func init() {
	m.Register(func(app core.App) error {
		hubSecrets := core.NewBaseCollection("hub_secrets")
		// ListRule/ViewRule/CreateRule/UpdateRule/DeleteRule all stay nil:
		// superuser-only, never exposed to a "users" record.
		hubSecrets.Fields.Add(
			&core.TextField{Name: "key", Required: true, Max: 255},
			&core.TextField{Name: "value", Required: true, Max: 50000},
		)
		hubSecrets.Indexes = []string{
			"CREATE UNIQUE INDEX idx_hub_secrets_key ON hub_secrets (key)",
		}
		return app.Save(hubSecrets)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("hub_secrets")
		if err != nil {
			return nil // already gone
		}
		return app.Delete(col)
	})
}
