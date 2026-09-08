package audit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

// newTestRequestEvent builds a minimal *core.RequestEvent carrying an
// authenticated actor, the same shape a real HTTP request handled through
// PocketBase's router would produce — enough for audit.Record (called from
// inside a RegisterHooks binding) to read e.Auth, e.RealIP(), and the
// response header it writes the request id to.
func newTestRequestEvent(app core.App, auth *core.Record) *core.RequestEvent {
	req := httptest.NewRequest(http.MethodPost, "/api/collections/x/records", nil)
	req.RemoteAddr = "203.0.113.10:12345"
	rec := httptest.NewRecorder()

	e := &core.RequestEvent{App: app, Auth: auth}
	e.Response = rec
	e.Request = req
	return e
}

func mustSaveAdmin(t testing.TB, app core.App, email string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("find users collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.SetEmail(email)
	rec.SetPassword("password123456")
	rec.Set("role", "admin")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save admin user: %v", err)
	}
	return rec
}

// triggerDeleteRequest simulates what apis.recordDelete does for one
// collection/record: build a RecordRequestEvent and run it through the
// app's OnRecordDeleteRequest chain (which RegisterHooks binds into),
// with a final handler that actually deletes the record — the same
// position form.Submit()/e.App.Delete() occupies in the real handler.
func triggerDeleteRequest(t testing.TB, app core.App, e *core.RequestEvent, collectionName string, record *core.Record) error {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		t.Fatalf("find %s collection: %v", collectionName, err)
	}

	event := &core.RecordRequestEvent{}
	event.RequestEvent = e
	event.Collection = collection
	event.Record = record

	return app.OnRecordDeleteRequest(collectionName).Trigger(event, func(e *core.RecordRequestEvent) error {
		return e.App.Delete(e.Record)
	})
}

// triggerCreateRequest mirrors triggerDeleteRequest for the create path.
func triggerCreateRequest(t testing.TB, app core.App, e *core.RequestEvent, collectionName string, record *core.Record) error {
	t.Helper()
	collection, err := app.FindCollectionByNameOrId(collectionName)
	if err != nil {
		t.Fatalf("find %s collection: %v", collectionName, err)
	}

	event := &core.RecordRequestEvent{}
	event.RequestEvent = e
	event.Collection = collection
	event.Record = record

	return app.OnRecordCreateRequest(collectionName).Trigger(event, func(e *core.RecordRequestEvent) error {
		return e.App.Save(e.Record)
	})
}

func TestRegisterHooks_AgentDeleteWritesAuditEntry(t *testing.T) {
	app := mustNewTestApp(t)
	RegisterHooks(app)

	agent := mustSaveAuditAgent(t, app, "hook-test-agent")
	admin := mustSaveAdmin(t, app, "hook-admin@example.com")
	e := newTestRequestEvent(app, admin)

	if err := triggerDeleteRequest(t, app, e, "agents", agent); err != nil {
		t.Fatalf("triggerDeleteRequest(agents) error = %v", err)
	}

	entries, err := app.FindRecordsByFilter(
		"audit_log",
		"action = 'agent.delete' && target_id = {:id}",
		"", 1, 0,
		map[string]any{"id": agent.Id},
	)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one agent.delete audit_log entry, err=%v count=%d", err, len(entries))
	}
	entry := entries[0]

	if entry.GetString("actor_id") != admin.Id {
		t.Errorf("audit_log.actor_id = %q, want %q", entry.GetString("actor_id"), admin.Id)
	}
	if entry.GetString("actor_role") != "admin" {
		t.Errorf("audit_log.actor_role = %q, want admin", entry.GetString("actor_role"))
	}
	if entry.GetString("result") != "success" {
		t.Errorf("audit_log.result = %q, want success", entry.GetString("result"))
	}

	var details map[string]any
	if err := json.Unmarshal([]byte(entry.GetString("details")), &details); err != nil {
		t.Fatalf("audit_log.details did not parse as JSON: %v", err)
	}
	if details["hostname"] != "hook-test-agent" {
		t.Errorf("audit_log.details[hostname] = %v, want hook-test-agent", details["hostname"])
	}
	if _, hasToken := details["token_hash"]; hasToken {
		t.Error("audit_log.details must never include the agent's token_hash")
	}

	// The agent record itself must actually be gone — the hook wraps the
	// real deletion, it doesn't replace it.
	if _, err := app.FindRecordById("agents", agent.Id); err == nil {
		t.Error("agent record still exists after triggerDeleteRequest, want it deleted")
	}
}

func TestRegisterHooks_NotificationChannelCreateRedactsConfigInAuditDetails(t *testing.T) {
	app := mustNewTestApp(t)
	RegisterHooks(app)

	col, err := app.FindCollectionByNameOrId("notification_channels")
	if err != nil {
		t.Fatalf("find notification_channels collection: %v", err)
	}
	channel := core.NewRecord(col)
	channel.Set("name", "ops-webhook")
	channel.Set("type", "webhook")
	channel.Set("enabled", true)
	const secretURL = "https://hooks.example.com/T00000/B00000/super-secret-token"
	channel.Set("config", map[string]any{"url": secretURL})

	admin := mustSaveAdmin(t, app, "hook-admin-channel@example.com")
	e := newTestRequestEvent(app, admin)

	if err := triggerCreateRequest(t, app, e, "notification_channels", channel); err != nil {
		t.Fatalf("triggerCreateRequest(notification_channels) error = %v", err)
	}

	entries, err := app.FindRecordsByFilter(
		"audit_log",
		"action = 'notification_channel.create' && target_id = {:id}",
		"", 1, 0,
		map[string]any{"id": channel.Id},
	)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one notification_channel.create audit_log entry, err=%v count=%d", err, len(entries))
	}

	rawDetails := entries[0].GetString("details")
	if strings.Contains(rawDetails, secretURL) {
		t.Fatalf("audit_log.details leaked the channel's secret config value: %s", rawDetails)
	}

	var details map[string]any
	if err := json.Unmarshal([]byte(rawDetails), &details); err != nil {
		t.Fatalf("audit_log.details did not parse as JSON: %v", err)
	}
	fields, _ := details["config_fields"].([]any)
	if len(fields) != 1 || fields[0] != "url" {
		t.Errorf("audit_log.details[config_fields] = %v, want [url] (names only, no values)", fields)
	}
}
