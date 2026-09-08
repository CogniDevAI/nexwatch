package audit

import (
	"encoding/json"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// same schema (audit_log, agents, notification_channels, ...) that
	// production runs against.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
)

func mustNewTestApp(t testing.TB) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

func TestRedactedFieldNames(t *testing.T) {
	tests := []struct {
		name string
		json string
		want []string
	}{
		{"empty string", "", nil},
		{"empty object", "{}", []string{}},
		{"single field", `{"url":"https://example.com/webhook"}`, []string{"url"}},
		{
			"multiple fields returned sorted regardless of input order",
			`{"token":"secret","url":"https://example.com","channel":"#alerts"}`,
			[]string{"channel", "token", "url"},
		},
		{"invalid json", "not json", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redactedFieldNames(tt.json)
			if len(got) != len(tt.want) {
				t.Fatalf("redactedFieldNames(%q) = %v, want %v", tt.json, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("redactedFieldNames(%q) = %v, want %v", tt.json, got, tt.want)
					break
				}
			}
		})
	}
}

func TestRedactedFieldNames_NeverReturnsValues(t *testing.T) {
	// The whole point of redactedFieldNames is that a secret value like a
	// webhook URL or bot token never appears anywhere in its output.
	secretValue := "https://hooks.example.com/T00000/B00000/super-secret-token"
	got := redactedFieldNames(`{"url":"` + secretValue + `"}`)

	for _, name := range got {
		if name == secretValue {
			t.Fatalf("redactedFieldNames leaked a secret value: %v", got)
		}
	}
	if len(got) != 1 || got[0] != "url" {
		t.Errorf("redactedFieldNames() = %v, want [url]", got)
	}
}

func mustSaveAuditAgent(t testing.TB, app core.App, hostname string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("hostname", hostname)
	rec.Set("status", "online")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	return rec
}

func TestRecord_WithNilRequestEventLeavesActorFieldsEmpty(t *testing.T) {
	app := mustNewTestApp(t)

	Record(app, nil, Entry{
		Action:     "check.run",
		TargetType: "checks",
		TargetID:   "some-check-id",
		Result:     "success",
	})

	entries, err := app.FindRecordsByFilter("audit_log", "action = 'check.run'", "", 1, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one audit_log entry, err=%v count=%d", err, len(entries))
	}
	entry := entries[0]
	if entry.GetString("actor_id") != "" || entry.GetString("actor_email") != "" || entry.GetString("ip") != "" {
		t.Errorf("Record(app, nil, ...) populated actor/ip fields: actor_id=%q actor_email=%q ip=%q",
			entry.GetString("actor_id"), entry.GetString("actor_email"), entry.GetString("ip"))
	}
	if entry.GetString("result") != "success" {
		t.Errorf("audit_log.result = %q, want success", entry.GetString("result"))
	}
}

func TestRecord_StoresDetailsAsJSON(t *testing.T) {
	app := mustNewTestApp(t)

	Record(app, nil, Entry{
		Action:     "docker.restart",
		TargetType: "docker_container",
		TargetID:   "abc123",
		AgentID:    "agent-1",
		Details:    map[string]any{"hostname": "web-01"},
		Result:     "success",
	})

	entries, err := app.FindRecordsByFilter("audit_log", "action = 'docker.restart'", "", 1, 0)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one audit_log entry, err=%v count=%d", err, len(entries))
	}

	var details map[string]any
	if err := json.Unmarshal([]byte(entries[0].GetString("details")), &details); err != nil {
		t.Fatalf("audit_log.details did not parse as JSON: %v", err)
	}
	if details["hostname"] != "web-01" {
		t.Errorf("audit_log.details[hostname] = %v, want web-01", details["hostname"])
	}
	if entries[0].GetString("agent_id") != "agent-1" {
		t.Errorf("audit_log.agent_id = %q, want agent-1", entries[0].GetString("agent_id"))
	}
}
