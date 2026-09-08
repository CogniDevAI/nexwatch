package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration adds requested_by to the thread_dumps collection. It must
// run after the collection has been created (thread_dumps.go) — the
// filename is deliberately sorted after "thread_dumps.go" ("." < "_" in
// ASCII) so a fresh install applies them in the correct order.
func init() {
	m.Register(func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("thread_dumps")
		if err != nil {
			return err
		}

		if col.Fields.GetByName("requested_by") != nil {
			return nil // already applied
		}

		col.Fields.Add(&core.TextField{Name: "requested_by", Max: 255})
		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("thread_dumps")
		if err != nil {
			return nil
		}
		col.Fields.RemoveByName("requested_by")
		return app.Save(col)
	})
}
