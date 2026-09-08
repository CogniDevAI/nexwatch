package audit

import (
	"github.com/pocketbase/pocketbase/core"
)

// mutationSpec describes how one collection's create/update/delete
// requests map to audit_log entries.
type mutationSpec struct {
	collection   string
	actionPrefix string // e.g. "alert_rule" -> alert_rule.create/update/delete
	// ops lists which of "create"/"update"/"delete" get audited for this
	// collection. Every collection is audited for all three except
	// "agents", which the catalogue only wants for deletion (agent
	// creation/update happens routinely from the WebSocket registration
	// flow with no meaningful "actor" behind it).
	ops []string
	// details extracts a safe, non-secret summary of record for the audit
	// entry. Must never include secret-bearing fields (passwords, tokens,
	// channel config values) — see redactedFieldNames for the pattern used
	// to reference such fields by name only.
	details func(record *core.Record) map[string]any
}

var allOps = []string{"create", "update", "delete"}

var mutationSpecs = []mutationSpec{
	{
		collection:   "alert_rules",
		actionPrefix: "alert_rule",
		ops:          allOps,
		details: func(r *core.Record) map[string]any {
			return map[string]any{"name": r.GetString("name"), "metric_type": r.GetString("metric_type")}
		},
	},
	{
		collection:   "notification_channels",
		actionPrefix: "notification_channel",
		ops:          allOps,
		details: func(r *core.Record) map[string]any {
			// config values (webhook URLs, bot tokens, API keys, ...) are
			// never stored — only the names of the fields that were set.
			return map[string]any{
				"name":          r.GetString("name"),
				"type":          r.GetString("type"),
				"config_fields": redactedFieldNames(r.GetString("config")),
			}
		},
	},
	{
		collection:   "silences",
		actionPrefix: "silence",
		ops:          allOps,
		details: func(r *core.Record) map[string]any {
			return map[string]any{"name": r.GetString("name"), "agent_id": r.GetString("agent_id")}
		},
	},
	{
		collection:   "checks",
		actionPrefix: "check",
		ops:          allOps,
		details: func(r *core.Record) map[string]any {
			return map[string]any{"name": r.GetString("name"), "type": r.GetString("type")}
		},
	},
	{
		collection:   "users",
		actionPrefix: "user",
		ops:          allOps,
		details: func(r *core.Record) map[string]any {
			// Never the password/password hash — email and role are enough
			// to identify what changed.
			return map[string]any{"email": r.GetString("email"), "role": r.GetString("role")}
		},
	},
	{
		collection:   "agents",
		actionPrefix: "agent",
		ops:          []string{"delete"},
		details: func(r *core.Record) map[string]any {
			// Never token/token_hash.
			return map[string]any{"hostname": r.GetString("hostname"), "ip": r.GetString("ip")}
		},
	},
}

func containsOp(ops []string, op string) bool {
	for _, o := range ops {
		if o == op {
			return true
		}
	}
	return false
}

// RegisterHooks binds the request-scoped hooks that audit every mutation
// performed against the collections in mutationSpecs through PocketBase's
// default REST CRUD API — as opposed to a custom /api/custom/* route,
// which calls Record directly from its own handler instead.
//
// It deliberately binds the *Request hooks (OnRecordCreateRequest/
// OnRecordUpdateRequest/OnRecordDeleteRequest) rather than the
// model-level OnRecordAfterCreateSuccess/AfterUpdateSuccess/
// AfterDeleteSuccess hooks. The model-level hooks fire for every save
// regardless of where it came from, but carry no reference to the HTTP
// request at all — there is no way to recover the acting user, IP, or
// request id from inside one. The *Request hooks embed the full
// *core.RequestEvent (e.Auth, e.RealIP(), the request id header) and,
// just as importantly, only ever fire for a genuine incoming HTTP
// request — an internal engine write (the alert engine flipping
// alerts.status, the checks scheduler saving check_results, the ws hub
// updating an agent's last_seen) never reaches this code path at all, so
// "skip when there is no request" falls out of the hook choice itself
// rather than needing to be detected and special-cased.
func RegisterHooks(app core.App) {
	for _, spec := range mutationSpecs {
		if containsOp(spec.ops, "create") {
			bindCreate(app, spec)
		}
		if containsOp(spec.ops, "update") {
			bindUpdate(app, spec)
		}
		if containsOp(spec.ops, "delete") {
			bindDelete(app, spec)
		}
	}
}

func bindCreate(app core.App, spec mutationSpec) {
	app.OnRecordCreateRequest(spec.collection).BindFunc(func(e *core.RecordRequestEvent) error {
		err := e.Next()
		if err == nil {
			Record(e.App, e.RequestEvent, Entry{
				Action:     spec.actionPrefix + ".create",
				TargetType: spec.collection,
				TargetID:   e.Record.Id,
				Details:    spec.details(e.Record),
				Result:     "success",
			})
		}
		return err
	})
}

func bindUpdate(app core.App, spec mutationSpec) {
	app.OnRecordUpdateRequest(spec.collection).BindFunc(func(e *core.RecordRequestEvent) error {
		err := e.Next()
		if err == nil {
			Record(e.App, e.RequestEvent, Entry{
				Action:     spec.actionPrefix + ".update",
				TargetType: spec.collection,
				TargetID:   e.Record.Id,
				Details:    spec.details(e.Record),
				Result:     "success",
			})
		}
		return err
	})
}

func bindDelete(app core.App, spec mutationSpec) {
	app.OnRecordDeleteRequest(spec.collection).BindFunc(func(e *core.RecordRequestEvent) error {
		// Capture the pre-delete record's details before e.Next() removes
		// it, since a record deleted mid-request can no longer be read
		// back afterward.
		targetID := e.Record.Id
		details := spec.details(e.Record)

		err := e.Next()
		if err == nil {
			Record(e.App, e.RequestEvent, Entry{
				Action:     spec.actionPrefix + ".delete",
				TargetType: spec.collection,
				TargetID:   targetID,
				Details:    details,
				Result:     "success",
			})
		}
		return err
	})
}
