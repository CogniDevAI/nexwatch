// Package audit records who did what against the hub: every custom
// mutating route calls Record directly, and a handful of PocketBase
// request-scoped hooks (see hooks.go) call it for collections mutated
// straight through the SDK's default REST CRUD (alert_rules,
// notification_channels, silences, checks, users, agents).
package audit

import (
	"encoding/json"
	"log/slog"
	"sort"

	"github.com/pocketbase/pocketbase/core"
)

// Entry describes one audit_log row to record. Action is a short
// dotted-namespace string (e.g. "docker.restart", "alert_rule.update"),
// consistent with the catalogue documented on the "audit_log" migration.
type Entry struct {
	Action     string
	TargetType string
	TargetID   string
	AgentID    string
	Details    map[string]any
	Result     string // "success" | "failure"
}

// Record writes one audit_log entry. When e is non-nil, actor identity
// (id/email/role), the client IP, and the request id are derived from it;
// when e is nil (there is no HTTP request in flight — an engine-internal
// write), those fields are simply left empty rather than guessed.
//
// Record never returns an error: a failure to write an audit entry must
// never fail or roll back the action it is describing. Failures are
// logged instead.
func Record(app core.App, e *core.RequestEvent, entry Entry) {
	col, err := app.FindCollectionByNameOrId("audit_log")
	if err != nil {
		slog.Error("audit_log collection not found", "error", err)
		return
	}

	rec := core.NewRecord(col)
	rec.Set("action", entry.Action)
	rec.Set("target_type", entry.TargetType)
	rec.Set("target_id", entry.TargetID)
	rec.Set("agent_id", entry.AgentID)
	rec.Set("result", entry.Result)

	if entry.Details != nil {
		if detailsJSON, err := json.Marshal(entry.Details); err == nil {
			rec.Set("details", string(detailsJSON))
		} else {
			slog.Error("failed to marshal audit log details", "action", entry.Action, "error", err)
		}
	}

	if e != nil {
		if e.Auth != nil {
			rec.Set("actor_id", e.Auth.Id)
			rec.Set("actor_email", e.Auth.GetString("email"))
			rec.Set("actor_role", actorRole(e.Auth))
		}
		rec.Set("ip", e.RealIP())
		rec.Set("request_id", e.Response.Header().Get("X-Request-ID"))
	}

	if err := app.Save(rec); err != nil {
		slog.Error("failed to save audit log entry", "action", entry.Action, "error", err)
	}
}

// actorRole resolves an auth record's role the same way
// internal/hub/api.RoleOfRecord does, duplicated here (as a plain string
// rather than that package's Role type) specifically to avoid an import
// cycle: internal/hub/api registers routes that call audit.Record, so
// internal/hub/audit must not import internal/hub/api.
func actorRole(auth *core.Record) string {
	if auth == nil {
		return ""
	}
	if auth.IsSuperuser() {
		return "admin"
	}
	if auth.Collection().Name == "users" {
		if role := auth.GetString("role"); role != "" {
			return role
		}
		return "viewer"
	}
	return "viewer"
}

// redactedFieldNames returns the sorted top-level keys of a JSON-encoded
// object (e.g. notification_channels.config), so an audit entry can note
// which fields were set/changed without ever storing the values
// themselves — config values routinely hold webhook URLs, bot tokens, or
// other secrets that must never land in the audit trail. Returns nil for
// an empty or unparsable value.
func redactedFieldNames(jsonValue string) []string {
	if jsonValue == "" {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(jsonValue), &m); err != nil {
		return nil
	}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
