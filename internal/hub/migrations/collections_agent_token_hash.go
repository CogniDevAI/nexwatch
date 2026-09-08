package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"

	"github.com/CogniDevAI/nexwatch/internal/hub/agenttoken"
)

// This migration replaces the plaintext agents.token field with a hashed
// agents.token_hash field. It must run after the "agents" collection has
// been created (collections.go) — the filename is deliberately sorted after
// "collections.go" ("." < "_" in ASCII) so a fresh install applies them in
// the correct order.
func init() {
	m.Register(func(app core.App) error {
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}

		// Add token_hash if not already present (idempotent re-run safety).
		if agents.Fields.GetByName("token_hash") == nil {
			agents.Fields.Add(&core.TextField{Name: "token_hash", Max: 128})
			if err := app.Save(agents); err != nil {
				return err
			}
		}

		// Backfill token_hash for every agent that still has a plaintext token.
		records, err := app.FindAllRecords("agents")
		if err != nil {
			return err
		}
		for _, record := range records {
			token := record.GetString("token")
			if token == "" {
				continue
			}
			record.Set("token_hash", agenttoken.Hash(token))
			if err := app.Save(record); err != nil {
				return err
			}
		}

		// Drop the plaintext field and its unique index, then add a partial
		// unique index on token_hash — partial so agents that have not yet
		// been issued a token (empty token_hash) don't collide with each
		// other under the unique constraint.
		agents.Fields.RemoveByName("token")
		agents.RemoveIndex("idx_agents_token")
		agents.AddIndex("idx_agents_token_hash", true, "token_hash", "token_hash != ''")

		return app.Save(agents)
	}, func(app core.App) error {
		// Note: this restores the schema shape only. The plaintext token
		// values were never persisted after the up migration ran (only
		// their hashes were), so downgrading does not recover them —
		// every agent will need a new token generated after a rollback.
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}

		if agents.Fields.GetByName("token") == nil {
			agents.Fields.Add(&core.TextField{Name: "token", Max: 255})
		}
		agents.RemoveIndex("idx_agents_token_hash")
		agents.AddIndex("idx_agents_token", true, "token", "")

		return app.Save(agents)
	})
}
