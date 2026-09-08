package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/CogniDevAI/nexwatch/internal/hub/agenttoken"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// agents collection with the token_hash field this package relies on.
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

// createAgent creates an "agents" record with the given plaintext token
// hashed into token_hash (empty plaintextToken leaves token_hash empty,
// simulating an agent that has never been issued a token).
func createAgent(t *testing.T, app core.App, hostname, status, plaintextToken string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("hostname", hostname)
	rec.Set("status", status)
	if plaintextToken != "" {
		rec.Set("token_hash", agenttoken.Hash(plaintextToken))
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save agent %s: %v", hostname, err)
	}
	return rec
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

// --- findAgentByToken ---------------------------------------------------

func TestFindAgentByToken_Found(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-a", "offline", "secret-token")
	hub := NewHub(app)

	found, err := hub.findAgentByToken("secret-token")
	if err != nil {
		t.Fatalf("findAgentByToken() unexpected error: %v", err)
	}
	if found.Id != agent.Id {
		t.Errorf("findAgentByToken() Id = %q, want %q", found.Id, agent.Id)
	}
}

func TestFindAgentByToken_NotFound(t *testing.T) {
	app := newTestApp(t)
	createAgent(t, app, "host-a", "offline", "secret-token")
	hub := NewHub(app)

	_, err := hub.findAgentByToken("wrong-token")
	if err == nil {
		t.Fatal("findAgentByToken() with an unknown token expected an error, got nil")
	}
}

func TestFindAgentByToken_UnissuedAgentNeverMatches(t *testing.T) {
	// An agent with an empty token_hash (never issued a token) must not be
	// matched by any presented token, including an empty one.
	app := newTestApp(t)
	createAgent(t, app, "host-a", "pending", "")
	hub := NewHub(app)

	if _, err := hub.findAgentByToken(""); err == nil {
		t.Fatal("findAgentByToken(\"\") against an unissued agent expected an error, got nil")
	}
	if _, err := hub.findAgentByToken("anything"); err == nil {
		t.Fatal("findAgentByToken() against an unissued agent expected an error, got nil")
	}
}

// --- routeMessage dispatch ------------------------------------------------

func TestRouteMessage_MetricsDispatchesToHandler(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-metrics", "offline", "tok")
	hub := NewHub(app)

	var gotAgentID string
	var gotPayload *protocol.MetricsPayload
	hub.SetMetricHandler(func(app core.App, agentID string, payload *protocol.MetricsPayload) {
		gotAgentID = agentID
		gotPayload = payload
	})

	ca := &ConnectedAgent{ID: agent.Id}
	msg, err := protocol.NewMessage(protocol.MessageTypeMetrics, &protocol.MetricsPayload{
		AgentID: "should-be-overwritten",
		Metrics: []protocol.MetricData{{Type: "cpu", Data: map[string]any{"total_percent": 50.0}, Timestamp: 123}},
	})
	if err != nil {
		t.Fatalf("NewMessage() unexpected error: %v", err)
	}

	hub.routeMessage(ca, msg)

	if gotPayload == nil {
		t.Fatal("metric handler was not invoked")
	}
	if gotAgentID != agent.Id {
		t.Errorf("metric handler agentID = %q, want %q", gotAgentID, agent.Id)
	}
	if gotPayload.AgentID != agent.Id {
		t.Errorf("metric handler payload.AgentID = %q, want %q (must be overwritten to the connection's agent)", gotPayload.AgentID, agent.Id)
	}
	if len(gotPayload.Metrics) != 1 || gotPayload.Metrics[0].Type != "cpu" {
		t.Errorf("metric handler payload.Metrics = %+v, want one cpu metric", gotPayload.Metrics)
	}

	fresh, err := app.FindRecordById("agents", agent.Id)
	if err != nil {
		t.Fatalf("find agent after metrics: %v", err)
	}
	if fresh.GetString("status") != "online" {
		t.Errorf("agent status after METRICS = %q, want online", fresh.GetString("status"))
	}
	if fresh.GetString("last_seen") == "" {
		t.Error("agent last_seen was not updated after METRICS")
	}
}

func TestRouteMessage_LogsDispatchesToHandler(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-logs", "offline", "tok")
	hub := NewHub(app)

	var gotAgentID string
	var gotPayload *protocol.LogsPayload
	hub.SetLogsHandler(func(app core.App, agentID string, payload *protocol.LogsPayload) {
		gotAgentID = agentID
		gotPayload = payload
	})

	ca := &ConnectedAgent{ID: agent.Id}
	msg, err := protocol.NewMessage(protocol.MessageTypeLogs, &protocol.LogsPayload{
		AgentID: "should-be-overwritten",
		Entries: []protocol.LogEntry{{Ts: 123, Source: "journald", Level: "error", Message: "boom"}},
		Dropped: 5,
	})
	if err != nil {
		t.Fatalf("NewMessage() unexpected error: %v", err)
	}

	hub.routeMessage(ca, msg)

	if gotPayload == nil {
		t.Fatal("logs handler was not invoked")
	}
	if gotAgentID != agent.Id {
		t.Errorf("logs handler agentID = %q, want %q", gotAgentID, agent.Id)
	}
	if gotPayload.AgentID != agent.Id {
		t.Errorf("logs handler payload.AgentID = %q, want %q (must be overwritten to the connection's agent)", gotPayload.AgentID, agent.Id)
	}
	if len(gotPayload.Entries) != 1 || gotPayload.Entries[0].Message != "boom" {
		t.Errorf("logs handler payload.Entries = %+v, want one entry with message %q", gotPayload.Entries, "boom")
	}
	if gotPayload.Dropped != 5 {
		t.Errorf("logs handler payload.Dropped = %d, want 5", gotPayload.Dropped)
	}

	fresh, err := app.FindRecordById("agents", agent.Id)
	if err != nil {
		t.Fatalf("find agent after logs: %v", err)
	}
	if fresh.GetString("status") != "online" {
		t.Errorf("agent status after LOGS = %q, want online", fresh.GetString("status"))
	}
}

func TestRouteMessage_HeartbeatUpdatesLastSeen(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-hb", "offline", "tok")
	hub := NewHub(app)

	ca := &ConnectedAgent{ID: agent.Id}
	msg, err := protocol.NewMessage(protocol.MessageTypeHeartbeat, &protocol.HeartbeatPayload{AgentID: agent.Id, Uptime: 100})
	if err != nil {
		t.Fatalf("NewMessage() unexpected error: %v", err)
	}

	before := time.Now()
	hub.routeMessage(ca, msg)

	if ca.LastSeen.Before(before) {
		t.Errorf("ca.LastSeen = %v, want updated to at or after %v", ca.LastSeen, before)
	}

	fresh, err := app.FindRecordById("agents", agent.Id)
	if err != nil {
		t.Fatalf("find agent after heartbeat: %v", err)
	}
	if fresh.GetString("status") != "online" {
		t.Errorf("agent status after HEARTBEAT = %q, want online", fresh.GetString("status"))
	}
}

// TestRouteMessage_HeartbeatPersistsDroppedMessages asserts that the
// HEARTBEAT payload's cumulative dropped-message count (set by the agent's
// WSTransport when its send queue is full — see
// internal/agent/transport/ws.go) is persisted onto the agent record, and
// that it is updated again on a later heartbeat that reports a higher
// count, so an operator can see a persistently overflowing queue.
func TestRouteMessage_HeartbeatPersistsDroppedMessages(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-dropped", "offline", "tok")
	hub := NewHub(app)
	ca := &ConnectedAgent{ID: agent.Id}

	send := func(dropped uint64) {
		msg, err := protocol.NewMessage(protocol.MessageTypeHeartbeat, &protocol.HeartbeatPayload{
			AgentID:         agent.Id,
			DroppedMessages: dropped,
		})
		if err != nil {
			t.Fatalf("NewMessage() unexpected error: %v", err)
		}
		hub.routeMessage(ca, msg)
	}

	send(5)
	fresh, err := app.FindRecordById("agents", agent.Id)
	if err != nil {
		t.Fatalf("find agent after first heartbeat: %v", err)
	}
	if got := fresh.GetFloat("dropped_messages"); got != 5 {
		t.Fatalf("dropped_messages after first heartbeat = %v, want 5", got)
	}

	send(12)
	fresh, err = app.FindRecordById("agents", agent.Id)
	if err != nil {
		t.Fatalf("find agent after second heartbeat: %v", err)
	}
	if got := fresh.GetFloat("dropped_messages"); got != 12 {
		t.Fatalf("dropped_messages after second heartbeat = %v, want 12 (updated to the latest reported count)", got)
	}
}

func TestRouteMessage_CommandResponseDispatchesToHandler(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-cmd", "online", "tok")
	hub := NewHub(app)

	var got *protocol.CommandResponsePayload
	hub.SetCommandResponseHandler(func(app core.App, payload *protocol.CommandResponsePayload) {
		got = payload
	})

	ca := &ConnectedAgent{ID: agent.Id}
	msg, err := protocol.NewMessage(protocol.MessageTypeCommandResponse, &protocol.CommandResponsePayload{
		Command:   "thread_dump",
		RequestID: "req-42",
		AgentID:   "should-be-overwritten",
		PID:       1234,
		Output:    "dump output",
	})
	if err != nil {
		t.Fatalf("NewMessage() unexpected error: %v", err)
	}

	hub.routeMessage(ca, msg)

	if got == nil {
		t.Fatal("command response handler was not invoked")
	}
	if got.AgentID != agent.Id {
		t.Errorf("command response handler payload.AgentID = %q, want %q", got.AgentID, agent.Id)
	}
	if got.Command != "thread_dump" || got.RequestID != "req-42" || got.PID != 1234 {
		t.Errorf("command response handler payload = %+v, unexpected content", got)
	}
}

func TestRouteMessage_UnknownTypeIsIgnored(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-unknown", "offline", "tok")
	hub := NewHub(app)

	metricCalled := false
	cmdRespCalled := false
	hub.SetMetricHandler(func(core.App, string, *protocol.MetricsPayload) { metricCalled = true })
	hub.SetCommandResponseHandler(func(core.App, *protocol.CommandResponsePayload) { cmdRespCalled = true })

	ca := &ConnectedAgent{ID: agent.Id}
	msg, err := protocol.NewMessage(protocol.MessageType(200), map[string]any{"raw": "data"})
	if err != nil {
		t.Fatalf("NewMessage() unexpected error: %v", err)
	}

	// Must not panic and must not invoke any registered handler.
	hub.routeMessage(ca, msg)

	if metricCalled || cmdRespCalled {
		t.Error("routeMessage invoked a handler for an unknown message type")
	}
}

// --- HandleWebSocket ------------------------------------------------------

func TestHandleWebSocket_MissingTokenReturns401(t *testing.T) {
	app := newTestApp(t)
	hub := NewHub(app)
	server := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("GET without token: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandleWebSocket_WrongTokenReturns401(t *testing.T) {
	app := newTestApp(t)
	createAgent(t, app, "host-wrong", "offline", "correct-token")
	hub := NewHub(app)
	server := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "?token=wrong-token")
	if err != nil {
		t.Fatalf("GET with wrong token: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHandleWebSocket_ValidTokenUpgrades(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "host-valid", "offline", "correct-token")
	hub := NewHub(app)
	server := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	t.Cleanup(server.Close)

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(server.URL)+"?token=correct-token", nil)
	if err != nil {
		t.Fatalf("Dial() unexpected error: %v", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}

	// Give the handler's goroutine a moment to register the agent and
	// persist the online status before asserting on shared state.
	deadline := time.Now().Add(2 * time.Second)
	for hub.ConnectedAgentCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if got := hub.ConnectedAgentCount(); got != 1 {
		t.Fatalf("ConnectedAgentCount() = %d, want 1", got)
	}

	// The connected-agents map is populated before the status is persisted,
	// so poll for the DB write rather than reading it exactly once.
	var fresh *core.Record
	for time.Now().Before(deadline) {
		var err error
		fresh, err = app.FindRecordById("agents", agent.Id)
		if err != nil {
			t.Fatalf("find agent after upgrade: %v", err)
		}
		if fresh.GetString("status") == "online" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fresh.GetString("status") != "online" {
		t.Errorf("agent status after successful upgrade = %q, want online", fresh.GetString("status"))
	}

	// Close and wait for the server's readPump goroutine to finish (it
	// touches h.app on disconnect via removeAgent), so it cannot still be
	// running against this test's app when app.Cleanup() runs and the next
	// test's app is created.
	_ = conn.Close()
	waitForDisconnect(t, hub)
}

func TestHandleWebSocket_AuthorizationHeaderFallback(t *testing.T) {
	app := newTestApp(t)
	createAgent(t, app, "host-header", "offline", "header-token")
	hub := NewHub(app)
	server := httptest.NewServer(http.HandlerFunc(hub.HandleWebSocket))
	t.Cleanup(server.Close)

	header := http.Header{}
	header.Set("Authorization", "header-token")
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL(server.URL), header)
	if err != nil {
		t.Fatalf("Dial() unexpected error: %v", err)
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusSwitchingProtocols)
	}

	deadline := time.Now().Add(2 * time.Second)
	for hub.ConnectedAgentCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// See TestHandleWebSocket_ValidTokenUpgrades: drain the server-side
	// goroutine before this test's app is cleaned up.
	_ = conn.Close()
	waitForDisconnect(t, hub)
}

// waitForDisconnect blocks until the hub reports zero connected agents (or a
// short timeout elapses), so a test can be sure the server-side readPump
// goroutine spawned by HandleWebSocket has finished touching the app before
// the test's app.Cleanup() runs.
func waitForDisconnect(t *testing.T, hub *Hub) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for hub.ConnectedAgentCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := hub.ConnectedAgentCount(); got != 0 {
		t.Fatalf("ConnectedAgentCount() after close = %d, want 0", got)
	}
}

// --- SendCommand ------------------------------------------------------

func TestSendCommand_AgentNotConnectedReturnsError(t *testing.T) {
	app := newTestApp(t)
	hub := NewHub(app)

	err := hub.SendCommand("nonexistent-agent", &protocol.CommandPayload{Command: "thread_dump"})
	if err == nil {
		t.Fatal("SendCommand() to a disconnected agent expected an error, got nil")
	}
}
