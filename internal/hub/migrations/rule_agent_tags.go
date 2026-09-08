package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration adds agents.tags and alert_rules.target_tags, both JSON
// arrays of plain strings (e.g. ["prod", "db"]), used together by the alert
// engine (internal/hub/alerts) to target rules at a set of tagged agents
// instead of one specific agent or every agent. A field left unset behaves
// as an empty array when read via record.GetStringSlice(), so no explicit
// default value is configured here.
//
// Targeting semantics (see alerts.matchesTargets): a rule whose agent_id is
// set applies to that agent only; otherwise, a rule whose target_tags is
// non-empty applies to agents having ANY of those tags; otherwise the rule
// applies to every agent.
//
// It must run after the "agents" and "alert_rules" collections have been
// created (collections.go) — the filename is deliberately sorted after
// "collections.go" ("c" < "r" byte-wise) so a fresh install applies them in
// the correct order.
func init() {
	m.Register(func(app core.App) error {
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}
		if agents.Fields.GetByName("tags") == nil {
			agents.Fields.Add(&core.JSONField{Name: "tags", MaxSize: 2000})
			if err := app.Save(agents); err != nil {
				return err
			}
		}

		alertRules, err := app.FindCollectionByNameOrId("alert_rules")
		if err != nil {
			return err
		}
		if alertRules.Fields.GetByName("target_tags") == nil {
			alertRules.Fields.Add(&core.JSONField{Name: "target_tags", MaxSize: 2000})
			if err := app.Save(alertRules); err != nil {
				return err
			}
		}

		return nil
	}, func(app core.App) error {
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}
		agents.Fields.RemoveByName("tags")
		if err := app.Save(agents); err != nil {
			return err
		}

		alertRules, err := app.FindCollectionByNameOrId("alert_rules")
		if err != nil {
			return err
		}
		alertRules.Fields.RemoveByName("target_tags")
		return app.Save(alertRules)
	})
}
