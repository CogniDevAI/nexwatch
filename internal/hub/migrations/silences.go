package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration creates the "silences" collection (maintenance windows)
// and adds alerts.silenced, both consumed by the alert engine
// (internal/hub/alerts) to suppress notifications for agents undergoing
// planned maintenance without losing track of the underlying incident: a
// silenced alert still transitions through the normal firing/resolved state
// machine and keeps its "alerts" row current, it just does not dispatch a
// notification while an active silence covers it.
//
// Semantics (see alerts.isSilenced/silenceMatchesAgent):
//   - A silence is active when starts_at <= now < ends_at.
//   - It matches an agent when its agent_id equals the agent, ANY of its
//     tags match one of the agent's tags, or both agent_id and tags are
//     empty (a global silence covering every agent).
//
// It must run after the "agents" and "alerts" collections have been created
// (collections.go) — the filename is deliberately sorted after
// "collections.go" ("c" < "s" byte-wise) so a fresh install applies them in
// the correct order. It reuses the same operator/admin rule-expression
// style as users_roles_rbac.go (list/view any authenticated, create/update/
// delete requires role operator or admin) but has no code dependency on
// that migration, so its position relative to it does not matter.
func init() {
	authOnly := "@request.auth.id != ''"
	operatorOrAbove := "@request.auth.id != '' && @request.auth.role != 'viewer'"

	m.Register(func(app core.App) error {
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}

		silences := core.NewBaseCollection("silences")
		silences.ListRule = &authOnly
		silences.ViewRule = &authOnly
		silences.CreateRule = &operatorOrAbove
		silences.UpdateRule = &operatorOrAbove
		silences.DeleteRule = &operatorOrAbove
		silences.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 255},
			&core.TextField{Name: "reason", Max: 1000},
			&core.RelationField{
				Name:         "agent_id",
				CollectionId: agents.Id,
				MaxSelect:    1,
				// Not required — empty means "match by tags (or globally),
				// not one specific agent".
			},
			&core.JSONField{Name: "tags", MaxSize: 2000},
			&core.DateField{Name: "starts_at", Required: true},
			&core.DateField{Name: "ends_at", Required: true},
			&core.TextField{Name: "created_by", Max: 255},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		silences.Indexes = []string{
			"CREATE INDEX idx_silences_ends_at ON silences (ends_at)",
			"CREATE INDEX idx_silences_agent ON silences (agent_id)",
		}
		if err := app.Save(silences); err != nil {
			return err
		}

		alerts, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		if alerts.Fields.GetByName("silenced") == nil {
			alerts.Fields.Add(&core.BoolField{Name: "silenced"})
			if err := app.Save(alerts); err != nil {
				return err
			}
		}

		return nil
	}, func(app core.App) error {
		alerts, err := app.FindCollectionByNameOrId("alerts")
		if err == nil {
			alerts.Fields.RemoveByName("silenced")
			if err := app.Save(alerts); err != nil {
				return err
			}
		}

		silences, err := app.FindCollectionByNameOrId("silences")
		if err != nil {
			return nil // already gone
		}
		return app.Delete(silences)
	})
}
