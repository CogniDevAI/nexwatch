package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration adds "restart_required" to the agents.update_status select
// (originally idle/started/downloading/verifying/installing/restarting/
// failed/done — see collections_agent_update_fields.go). It is a distinct
// terminal-ish stage from "restarting": a Windows agent whose running .exe
// was locked schedules the new binary via MoveFileEx(...,
// MOVEFILE_DELAY_UNTIL_REBOOT) instead of swapping it in immediately (see
// internal/agent/update/update_windows.go and Result.RestartRequired in
// internal/agent/update/update.go) — the update only actually takes effect
// once the host is rebooted or the service is otherwise restarted, unlike
// every other platform's "restarting" stage, which re-execs into the new
// binary right away. cmd/agent/main.go's handleUpdateCommand reports this
// stage instead of restarting/re-exec when Result.RestartRequired is true,
// and internal/hub/update.Service.HandleResponse treats it like "done" for
// update_error (clearing it — the download/verify/install itself
// succeeded, only the swap is deferred).
//
// It must run after "collections_agent_update_fields.go" (which creates
// update_status in the first place) — the filename is deliberately sorted
// after it ("collections_..." < "update_status_...", 'c' < 'u') so a fresh
// install applies them in the correct order.
func init() {
	m.Register(func(app core.App) error {
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return err
		}

		if field := agents.Fields.GetByName("update_status"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				if !slices.Contains(selectField.Values, "restart_required") {
					selectField.Values = append(selectField.Values, "restart_required")
				}
			}
		}
		return app.Save(agents)
	}, func(app core.App) error {
		agents, err := app.FindCollectionByNameOrId("agents")
		if err != nil {
			return nil // already gone
		}
		if field := agents.Fields.GetByName("update_status"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				kept := make([]string, 0, len(selectField.Values))
				for _, v := range selectField.Values {
					if v != "restart_required" {
						kept = append(kept, v)
					}
				}
				selectField.Values = kept
			}
		}
		return app.Save(agents)
	})
}
