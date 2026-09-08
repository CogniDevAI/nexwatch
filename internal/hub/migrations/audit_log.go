package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration creates the "audit_log" collection: an append-only record
// of who did what, used by internal/hub/audit (writes) and a
// GET /api/custom/audit endpoint (reads, or direct SDK list — see
// internal/hub/api/audit_routes.go).
//
// Design notes:
//   - actor_id/target_id/agent_id are plain text fields, not relations.
//     An audit trail must survive the thing it describes being deleted
//     later (a deleted user, agent, or alert rule) — a RelationField with
//     CascadeDelete would erase exactly the history someone would want to
//     look up after a deletion, and a non-cascading relation would still
//     dangle once the referenced record is gone. Plain text avoids both.
//   - details is a JSON blob for action-specific context. It must never
//     hold secret values (channel config, tokens) — only field names or
//     other non-sensitive metadata. See audit.redactedFieldNames.
//   - No Create/Update/DeleteRule: every audit_log write goes through
//     app.Save() from internal/hub/audit.Record (server-only), the same
//     convention as "metrics"/"docker_containers" in collections.go.
//     list/view are operator+ (both operator and admin can read; there is
//     no viewer-readable audit trail).
func init() {
	operatorOrAbove := "@request.auth.id != '' && @request.auth.role != 'viewer'"

	m.Register(func(app core.App) error {
		auditLog := core.NewBaseCollection("audit_log")
		auditLog.ListRule = &operatorOrAbove
		auditLog.ViewRule = &operatorOrAbove
		// CreateRule/UpdateRule/DeleteRule stay nil: server-only writes.
		auditLog.Fields.Add(
			&core.TextField{Name: "actor_id", Max: 255},
			&core.TextField{Name: "actor_email", Max: 255},
			&core.TextField{Name: "actor_role", Max: 50},
			&core.TextField{Name: "action", Required: true, Max: 255},
			&core.TextField{Name: "target_type", Max: 100},
			&core.TextField{Name: "target_id", Max: 255},
			&core.TextField{Name: "agent_id", Max: 255},
			&core.JSONField{Name: "details", MaxSize: 20000},
			&core.SelectField{
				Name:      "result",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"success", "failure"},
			},
			&core.TextField{Name: "ip", Max: 64},
			&core.TextField{Name: "request_id", Max: 64},
			&core.AutodateField{Name: "created", OnCreate: true},
		)
		auditLog.Indexes = []string{
			"CREATE INDEX idx_audit_log_created ON audit_log (created)",
			"CREATE INDEX idx_audit_log_action ON audit_log (action)",
			"CREATE INDEX idx_audit_log_actor ON audit_log (actor_id)",
			"CREATE INDEX idx_audit_log_agent ON audit_log (agent_id)",
		}
		return app.Save(auditLog)
	}, func(app core.App) error {
		col, err := app.FindCollectionByNameOrId("audit_log")
		if err != nil {
			return nil // already gone
		}
		return app.Delete(col)
	})
}
