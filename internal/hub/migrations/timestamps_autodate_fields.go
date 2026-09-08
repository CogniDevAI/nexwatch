package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// autodateCollections lists every custom (non-auth) collection created by
// this application's earlier migrations. None of them received "created" or
// "updated" autodate fields at creation time, so any PocketBase list query
// requesting "sort=-created" (the UI's default list order) fails with a 400
// "Something went wrong" for these collections. Auth collections (e.g.
// "users") already have these fields from PocketBase's own scaffolding and
// are intentionally excluded here.
//
// This migration's filename is deliberately sorted after "collections.go"
// (which creates agents/metrics/docker_containers/alert_rules/alerts/
// notification_channels/settings) and after "thread_dumps.go" (which
// creates thread_dumps) — "t" < "u" and "thread_dumps(...)" < "timestamps_..."
// byte-wise — so this always runs once those collections already exist.
var autodateCollections = []string{
	"agents",
	"metrics",
	"docker_containers",
	"alert_rules",
	"alerts",
	"notification_channels",
	"settings",
	"thread_dumps",
}

func init() {
	m.Register(func(app core.App) error {
		for _, name := range autodateCollections {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return err
			}

			changed := false
			if col.Fields.GetByName("created") == nil {
				col.Fields.Add(&core.AutodateField{
					Name:     "created",
					OnCreate: true,
				})
				changed = true
			}
			if col.Fields.GetByName("updated") == nil {
				col.Fields.Add(&core.AutodateField{
					Name:     "updated",
					OnCreate: true,
					OnUpdate: true,
				})
				changed = true
			}
			if !changed {
				continue
			}

			col.AddIndex("idx_"+name+"_created", false, "created", "")

			if err := app.Save(col); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		for _, name := range autodateCollections {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue // already gone
			}
			col.RemoveIndex("idx_" + name + "_created")
			col.Fields.RemoveByName("created")
			col.Fields.RemoveByName("updated")
			if err := app.Save(col); err != nil {
				return err
			}
		}
		return nil
	})
}
