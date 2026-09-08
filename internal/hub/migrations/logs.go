package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration creates the "logs" collection for agent-shipped log
// entries (internal/hub/logs): both journald and file-tailed lines
// shipped by internal/agent/logs over the new LOGS WebSocket message.
// Retention/hard-cap enforcement (internal/hub/logs.PurgeOld,
// EnforceMaxRows) reuses the metrics downsampler's existing hourly
// retention loop (see internal/hub/metrics/downsampler.go), the same
// pattern audit_log's retention already follows.
//
// It also adds an optional "logs_lines_total" gauge field to "agents",
// incremented by internal/hub/logs.Service.IngestLogs on every ingested
// batch so cumulative shipped-log-line volume is visible per agent
// without a separate aggregate query.
//
//   - logs: one shipped log line. Writes are server-only (no Create/
//     Update/DeleteRule) via the WebSocket LOGS message handler, matching
//     the "metrics"/"check_results" convention — ListRule still allows
//     authenticated users to list AND to subscribe over realtime for live
//     tail.
//
// Must run after "agents" (collections.go) since logs.agent_id references
// it — filename "logs.go" ("l" > "c" byte-wise) sorts after
// "collections.go", so a fresh install applies them in order.
func init() {
	authOnly := "@request.auth.id != ''"

	m.Register(func(app core.App) error {
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}

		logsCollection := core.NewBaseCollection("logs")
		logsCollection.ListRule = &authOnly
		logsCollection.ViewRule = &authOnly
		logsCollection.Fields.Add(
			&core.RelationField{
				Name:          "agent_id",
				Required:      true,
				CollectionId:  agents.Id,
				MaxSelect:     1,
				CascadeDelete: true,
			},
			&core.DateField{Name: "ts", Required: true},
			&core.TextField{Name: "source", Max: 255},
			&core.TextField{Name: "unit", Max: 255},
			&core.SelectField{
				Name:      "level",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"error", "warning", "info", "debug"},
			},
			&core.TextField{Name: "message", Required: true, Max: 8192},
			&core.JSONField{Name: "fields", MaxSize: 4000},
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		logsCollection.Indexes = []string{
			"CREATE INDEX idx_logs_agent_ts ON logs (agent_id, ts)",
			"CREATE INDEX idx_logs_level_ts ON logs (level, ts)",
			"CREATE INDEX idx_logs_unit ON logs (unit)",
		}
		if err := app.Save(logsCollection); err != nil {
			return err
		}

		if agents.Fields.GetByName("logs_lines_total") == nil {
			agents.Fields.Add(&core.NumberField{Name: "logs_lines_total"})
			if err := app.Save(agents); err != nil {
				return err
			}
		}

		return nil
	}, func(app core.App) error {
		if agents, err := app.FindCollectionByNameOrId("agents"); err == nil {
			if agents.Fields.GetByName("logs_lines_total") != nil {
				agents.Fields.RemoveByName("logs_lines_total")
				_ = app.Save(agents)
			}
		}

		col, err := app.FindCollectionByNameOrId("logs")
		if err != nil {
			return nil // already gone
		}
		return app.Delete(col)
	})
}
