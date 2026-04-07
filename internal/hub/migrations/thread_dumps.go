package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	authOnly := "@request.auth.id != ''"

	m.Register(func(app core.App) error {
		// Skip if already exists (idempotent).
		if _, err := app.FindCollectionByNameOrId("thread_dumps"); err == nil {
			return nil
		}

		// Find agents collection for the relation field.
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}

		col := core.NewBaseCollection("thread_dumps")
		col.ListRule = &authOnly
		col.ViewRule = &authOnly
		col.CreateRule = &authOnly
		col.UpdateRule = nil
		col.DeleteRule = &authOnly
		col.Fields.Add(
			&core.RelationField{
				Name:          "agent_id",
				Required:      true,
				CollectionId:  agents.Id,
				MaxSelect:     1,
				CascadeDelete: true,
			},
			&core.NumberField{Name: "pid", Required: true},
			&core.TextField{Name: "process_name", Max: 255},
			&core.TextField{Name: "request_id", Required: true, Max: 64},
			&core.TextField{Name: "output", Max: 5000000}, // up to 5 MB
			&core.TextField{Name: "error", Max: 2000},
			&core.SelectField{
				Name:      "status",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"pending", "success", "error"},
			},
			&core.DateField{Name: "taken_at", Required: true},
		)
		col.Indexes = []string{
			"CREATE INDEX idx_thread_dumps_agent ON thread_dumps (agent_id, taken_at)",
		}

		return app.Save(col)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("thread_dumps")
		if err != nil {
			return nil
		}
		return app.Delete(col)
	})
}
