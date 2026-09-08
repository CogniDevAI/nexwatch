package logs

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// agents/logs collections this package relies on.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

func newTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

func createAgent(t *testing.T, app core.App, hostname string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("hostname", hostname)
	rec.Set("status", "online")
	rec.Set("token", "token-"+hostname)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save agent %s: %v", hostname, err)
	}
	return rec
}

func fetchLogsForAgent(t *testing.T, app core.App, agentID string) []*core.Record {
	t.Helper()
	records, err := app.FindRecordsByFilter(
		"logs",
		"agent_id = {:agentId}",
		"ts",
		1000,
		0,
		map[string]any{"agentId": agentID},
	)
	if err != nil {
		t.Fatalf("find logs: %v", err)
	}
	return records
}

func TestIngestLogs_SavesEntries(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "web-01")
	svc := NewService(app)

	payload := &protocol.LogsPayload{
		AgentID: agent.Id,
		Entries: []protocol.LogEntry{
			{Ts: time.Now().UnixMilli(), Source: "journald", Unit: "nginx.service", Level: "error", Message: "connection refused", Fields: map[string]string{"syslog_identifier": "nginx"}},
			{Ts: time.Now().UnixMilli(), Source: "file:/var/log/app.log", Level: "info", Message: "started"},
		},
		Dropped: 2,
	}

	svc.IngestLogs(app, agent.Id, payload)

	records := fetchLogsForAgent(t, app, agent.Id)
	if len(records) != 2 {
		t.Fatalf("got %d log records, want 2", len(records))
	}

	if records[0].GetString("level") != "error" {
		t.Errorf("records[0].level = %q, want %q", records[0].GetString("level"), "error")
	}
	if records[0].GetString("unit") != "nginx.service" {
		t.Errorf("records[0].unit = %q, want %q", records[0].GetString("unit"), "nginx.service")
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(records[0].GetString("fields")), &fields); err != nil {
		t.Fatalf("unmarshal fields: %v", err)
	}
	if fields["syslog_identifier"] != "nginx" {
		t.Errorf("fields[syslog_identifier] = %q, want %q", fields["syslog_identifier"], "nginx")
	}

	if records[1].GetString("source") != "file:/var/log/app.log" {
		t.Errorf("records[1].source = %q, want %q", records[1].GetString("source"), "file:/var/log/app.log")
	}

	// The gauge added by the migration should have been bumped by 2.
	updatedAgent, err := app.FindRecordById("agents", agent.Id)
	if err != nil {
		t.Fatalf("find agent: %v", err)
	}
	if got := updatedAgent.GetFloat("logs_lines_total"); got != 2 {
		t.Errorf("logs_lines_total = %v, want 2", got)
	}
}

func TestIngestLogs_DefaultsUnrecognizedLevelToInfo(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "web-02")
	svc := NewService(app)

	svc.IngestLogs(app, agent.Id, &protocol.LogsPayload{
		AgentID: agent.Id,
		Entries: []protocol.LogEntry{
			{Ts: time.Now().UnixMilli(), Source: "journald", Level: "", Message: "no level given"},
			{Ts: time.Now().UnixMilli(), Source: "journald", Level: "bogus", Message: "unrecognized level"},
		},
	})

	records := fetchLogsForAgent(t, app, agent.Id)
	if len(records) != 2 {
		t.Fatalf("got %d log records, want 2", len(records))
	}
	for _, r := range records {
		if r.GetString("level") != "info" {
			t.Errorf("record level = %q, want %q (default)", r.GetString("level"), "info")
		}
	}
}

func TestIngestLogs_TruncatesOverlongMessage(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "web-03")
	svc := NewService(app)

	longMessage := strings.Repeat("x", maxMessageBytes+500)
	svc.IngestLogs(app, agent.Id, &protocol.LogsPayload{
		AgentID: agent.Id,
		Entries: []protocol.LogEntry{
			{Ts: time.Now().UnixMilli(), Source: "journald", Level: "info", Message: longMessage},
		},
	})

	records := fetchLogsForAgent(t, app, agent.Id)
	if len(records) != 1 {
		t.Fatalf("got %d log records, want 1", len(records))
	}
	if len(records[0].GetString("message")) > maxMessageBytes {
		t.Errorf("stored message length = %d, want <= %d", len(records[0].GetString("message")), maxMessageBytes)
	}
}

func TestIngestLogs_EmptyPayloadIsNoOp(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "web-04")
	svc := NewService(app)

	svc.IngestLogs(app, agent.Id, &protocol.LogsPayload{AgentID: agent.Id})
	svc.IngestLogs(app, agent.Id, nil)

	records := fetchLogsForAgent(t, app, agent.Id)
	if len(records) != 0 {
		t.Fatalf("got %d log records, want 0", len(records))
	}
}

func TestTruncateUTF8_DoesNotSplitAMultiByteRune(t *testing.T) {
	// "é" is 2 bytes in UTF-8 (0xC3 0xA9). Truncating right after the
	// first byte would produce invalid UTF-8 if not handled carefully.
	s := "ab" + "é" // 2 + 2 = 4 bytes total
	got := truncateUTF8(s, 3)
	if !strings.HasPrefix(s, got) {
		t.Fatalf("truncateUTF8(%q, 3) = %q, not a prefix of the original", s, got)
	}
	if len(got) > 3 {
		t.Fatalf("truncateUTF8(%q, 3) = %q, len %d > 3", s, got, len(got))
	}
	for i, r := range got {
		_ = i
		if r == 0xFFFD {
			t.Fatalf("truncateUTF8(%q, 3) = %q produced invalid UTF-8 (replacement rune)", s, got)
		}
	}
}
