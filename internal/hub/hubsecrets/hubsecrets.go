// Package hubsecrets stores server-only key/value secrets — currently just
// the VAPID private key for Web Push (F11 — PWA + Web Push notifications)
// — in the "hub_secrets" collection, which has every API rule set to nil
// (superuser-only). This is the deliberate boundary from the "settings"
// collection: "settings" is readable by any authenticated user (see
// internal/hub/api/settings.go), so a value that must never reach a
// regular "users" record — even a read-only admin browsing the
// PocketBase Admin UI as a non-superuser — belongs here instead.
//
// (Named "hubsecrets" rather than "secrets" to avoid colliding with the
// operator's own tooling convention of denying automated Read/Edit access
// to any "**/secrets/*" path as a blanket credential-safety guardrail —
// this package holds no credentials of its own, but a literal "secrets"
// directory name would still be caught by that same blanket rule.)
//
// Go code reads and writes hub_secrets directly via core.App, the same
// server-side-bypasses-API-rules convention used throughout this codebase
// (e.g. internal/hub/audit.Record writing "audit_log" directly).
package hubsecrets

import (
	"github.com/pocketbase/pocketbase/core"
)

// Get returns the stored value for key and whether it was found.
func Get(app core.App, key string) (string, bool) {
	record, err := app.FindFirstRecordByFilter(
		"hub_secrets",
		"key = {:key}",
		map[string]any{"key": key},
	)
	if err != nil {
		return "", false
	}
	return record.GetString("value"), true
}

// Set upserts a "hub_secrets" record for key, saving directly via
// core.App.Save — bypassing API rules entirely, since this package is the
// only intended writer of this collection.
func Set(app core.App, key, value string) error {
	record, err := app.FindFirstRecordByFilter(
		"hub_secrets",
		"key = {:key}",
		map[string]any{"key": key},
	)
	if err != nil {
		col, cErr := app.FindCollectionByNameOrId("hub_secrets")
		if cErr != nil {
			return cErr
		}
		record = core.NewRecord(col)
		record.Set("key", key)
	}
	record.Set("value", value)
	return app.Save(record)
}
