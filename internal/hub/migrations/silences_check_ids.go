package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration adds silences.check_ids (relation, multiple) so a
// maintenance-window silence can also target black-box "checks" records
// (internal/hub/checks), not just agents. Checks have no equivalent of an
// agent's single agent_id — a silence can cover several specific checks at
// once ("One check" in the UI still only ever picks one, but the field
// itself is multi-select the same way alert_rules.target_tags is a list),
// which is why this is a relation with MaxSelect > 1 rather than the
// single-select agent_id pattern.
//
// See alerts.SilenceMatchesCheck (internal/hub/alerts/engine.go) for the
// matching semantics this field feeds: a silence covers a check when its
// check_ids includes that check, when its tags overlap the check's own
// tags, or when the silence is global (no agent_id, no tags, and now no
// check_ids either — see that function's doc comment for why check_ids
// had to join the "is this global" test rather than only affect check
// matching).
//
// This must run after "checks" (checks.go, for the relation's
// CollectionId) and "silences" (silences.go) — the filename is
// deliberately sorted after "silences.go" ("silences.go" <
// "silences_check_ids.go" byte-wise, since '.' sorts before '_') so a
// fresh install applies them in the correct order.
func init() {
	m.Register(func(app core.App) error {
		checks, err := app.FindCollectionByNameOrId("checks")
		if err != nil {
			return err
		}

		silences, err := app.FindCollectionByNameOrId("silences")
		if err != nil {
			return err
		}

		if silences.Fields.GetByName("check_ids") == nil {
			silences.Fields.Add(&core.RelationField{
				Name:         "check_ids",
				CollectionId: checks.Id,
				MaxSelect:    999,
				// Not required — empty participates in the "is this
				// silence global" test alongside agent_id/tags (see
				// alerts.SilenceMatchesAgent/SilenceMatchesCheck).
			})
		}
		return app.Save(silences)
	}, func(app core.App) error {
		silences, err := app.FindCollectionByNameOrId("silences")
		if err != nil {
			return nil // already gone
		}
		silences.Fields.RemoveByName("check_ids")
		return app.Save(silences)
	})
}
