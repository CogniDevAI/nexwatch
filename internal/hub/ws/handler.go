package ws

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pocketbase/pocketbase/core"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/CogniDevAI/nexwatch/internal/hub/agenttoken"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// MetricHandler is called when a METRICS message is received from an agent.
type MetricHandler func(app core.App, agentID string, payload *protocol.MetricsPayload)

// CommandResponseHandler is called when a COMMAND_RESPONSE is received from an agent.
type CommandResponseHandler func(app core.App, payload *protocol.CommandResponsePayload)

// LogsHandler is called when a LOGS message is received from an agent.
type LogsHandler func(app core.App, agentID string, payload *protocol.LogsPayload)

// ConnectedAgent represents a connected WebSocket agent.
type ConnectedAgent struct {
	ID       string
	Conn     *websocket.Conn
	LastSeen time.Time
	mu       sync.Mutex
}

// Send writes a msgpack-encoded message to the agent's WebSocket connection.
func (ca *ConnectedAgent) Send(msg *protocol.Message) error {
	data, err := msg.Encode()
	if err != nil {
		return err
	}
	ca.mu.Lock()
	defer ca.mu.Unlock()
	return ca.Conn.WriteMessage(websocket.BinaryMessage, data)
}

// Hub manages all connected agents and routes messages.
type Hub struct {
	app                    core.App
	agents                 sync.Map // map[string]*ConnectedAgent (agentID → conn)
	metricHandler          MetricHandler
	commandResponseHandler CommandResponseHandler
	logsHandler            LogsHandler
	stopCh                 chan struct{}
}

// NewHub creates a new WebSocket hub.
func NewHub(app core.App) *Hub {
	return &Hub{
		app:    app,
		stopCh: make(chan struct{}),
	}
}

// SetMetricHandler registers the callback for metric messages.
func (h *Hub) SetMetricHandler(fn MetricHandler) {
	h.metricHandler = fn
}

// SetCommandResponseHandler registers the callback for command response messages.
func (h *Hub) SetCommandResponseHandler(fn CommandResponseHandler) {
	h.commandResponseHandler = fn
}

// SetLogsHandler registers the callback for LOGS messages.
func (h *Hub) SetLogsHandler(fn LogsHandler) {
	h.logsHandler = fn
}

// SendCommand sends a COMMAND message to the agent identified by agentID.
func (h *Hub) SendCommand(agentID string, payload *protocol.CommandPayload) error {
	v, ok := h.agents.Load(agentID)
	if !ok {
		return fmt.Errorf("agent %s not connected", agentID)
	}
	ca := v.(*ConnectedAgent)
	msg, err := protocol.NewMessage(protocol.MessageTypeCommand, payload)
	if err != nil {
		return err
	}
	return ca.Send(msg)
}

// ConnectedAgentCount returns the number of currently connected agents.
func (h *Hub) ConnectedAgentCount() int {
	count := 0
	h.agents.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// StartHeartbeatChecker launches a background goroutine that marks agents
// as offline if they haven't sent any message within the timeout.
func (h *Hub) StartHeartbeatChecker(timeout time.Duration) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.agents.Range(func(key, value any) bool {
					agent := value.(*ConnectedAgent)
					if time.Since(agent.LastSeen) > timeout {
						slog.Warn("agent heartbeat timeout, disconnecting", "agent_id", agent.ID)
						_ = agent.Conn.Close()
						h.removeAgent(agent.ID)
					}
					return true
				})
			case <-h.stopCh:
				return
			}
		}
	}()
}

// Stop signals all background goroutines to stop.
func (h *Hub) Stop() {
	close(h.stopCh)
}

// HandleWebSocket is the HTTP handler for the /ws/agent endpoint.
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Authenticate: token from query param or Authorization header.
	token := r.URL.Query().Get("token")
	if token == "" {
		token = r.Header.Get("Authorization")
	}
	if token == "" {
		http.Error(w, "missing authentication token", http.StatusUnauthorized)
		return
	}

	// Validate token against agents collection.
	agentRecord, err := h.findAgentByToken(token)
	if err != nil {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	// Upgrade to WebSocket.
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "error", err)
		return
	}

	agentID := agentRecord.Id
	ca := &ConnectedAgent{
		ID:       agentID,
		Conn:     conn,
		LastSeen: time.Now(),
	}

	// Register in connected agents map.
	h.agents.Store(agentID, ca)
	slog.Info("agent connected", "agent_id", agentID, "token_prefix", token[:min(8, len(token))])

	// Update agent status to online.
	agentRecord.Set("status", "online")
	agentRecord.Set("last_seen", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))
	if err := h.app.Save(agentRecord); err != nil {
		slog.Error("failed to update agent status", "agent_id", agentID, "error", err)
	}

	// Start read pump in a goroutine.
	go h.readPump(ca)
}

// findAgentByToken looks up an agent record by the SHA-256 hash of the
// presented plaintext token, then re-verifies the match with a
// constant-time comparison as defense-in-depth against timing side-channels.
func (h *Hub) findAgentByToken(token string) (*core.Record, error) {
	hash := agenttoken.Hash(token)

	record, err := h.app.FindFirstRecordByFilter(
		"agents",
		"token_hash = {:hash}",
		map[string]any{"hash": hash},
	)
	if err != nil {
		return nil, err
	}

	if subtle.ConstantTimeCompare([]byte(record.GetString("token_hash")), []byte(hash)) != 1 {
		return nil, fmt.Errorf("token mismatch")
	}

	return record, nil
}

// readPump reads messages from the agent's WebSocket connection and routes them.
func (h *Hub) readPump(ca *ConnectedAgent) {
	defer func() {
		_ = ca.Conn.Close()
		h.removeAgent(ca.ID)
	}()

	// Set read deadline and pong handler for keep-alive.
	ca.Conn.SetReadLimit(512 * 1024) // 512KB max message size
	_ = ca.Conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	ca.Conn.SetPongHandler(func(string) error {
		_ = ca.Conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})

	for {
		_, data, err := ca.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Warn("agent read error", "agent_id", ca.ID, "error", err)
			}
			return
		}

		// Reset read deadline on any message.
		_ = ca.Conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		ca.LastSeen = time.Now()

		// Decode the envelope.
		msg, err := protocol.Decode(data)
		if err != nil {
			slog.Warn("agent message decode error", "agent_id", ca.ID, "error", err)
			continue
		}

		h.routeMessage(ca, msg)
	}
}

// routeMessage dispatches a decoded message based on its type.
func (h *Hub) routeMessage(ca *ConnectedAgent, msg *protocol.Message) {
	switch msg.Type {
	case protocol.MessageTypeRegister:
		h.handleRegister(ca, msg)

	case protocol.MessageTypeMetrics:
		h.handleMetrics(ca, msg)

	case protocol.MessageTypeHeartbeat:
		h.handleHeartbeat(ca, msg)

	case protocol.MessageTypeCommandResponse:
		h.handleCommandResponse(ca, msg)

	case protocol.MessageTypeLogs:
		h.handleLogs(ca, msg)

	default:
		slog.Warn("agent sent unknown message type", "agent_id", ca.ID, "message_type", msg.Type)
	}
}

// handleRegister processes REGISTER messages from agents.
func (h *Hub) handleRegister(ca *ConnectedAgent, msg *protocol.Message) {
	var payload protocol.RegisterPayload
	if err := msg.DecodePayload(&payload); err != nil {
		slog.Warn("agent register decode error", "agent_id", ca.ID, "error", err)
		return
	}

	slog.Info("agent registering",
		"agent_id", ca.ID,
		"hostname", payload.Hostname,
		"os", payload.OS,
		"ip", payload.IP,
		"version", payload.Version,
	)

	// Update agent record in PocketBase.
	record, err := h.app.FindRecordById("agents", ca.ID)
	if err != nil {
		slog.Warn("agent not found for registration update", "agent_id", ca.ID, "error", err)
		return
	}

	record.Set("hostname", payload.Hostname)
	record.Set("os", payload.OS)
	record.Set("ip", payload.IP)
	record.Set("version", payload.Version)
	record.Set("status", "online")
	record.Set("last_seen", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))
	// Arch/Platform are only sent by an agent build wired for F9 — an
	// older agent's REGISTER simply omits them and these Set calls write
	// "", leaving whatever was already recorded from a prior REGISTER
	// untouched only if the field already held a non-empty value... which
	// it wouldn't after an explicit Set(""). In practice every agent from
	// this codebase always sends both, so this is a non-issue in
	// production; a genuinely blank value here just means the fields were
	// never known for this agent.
	if payload.Arch != "" {
		record.Set("arch", payload.Arch)
	}
	if payload.Platform != "" {
		record.Set("platform", payload.Platform)
	}

	// If this REGISTER's version matches the version a self-update
	// (internal/hub/api/update_routes.go) most recently targeted, the
	// agent has successfully restarted on the new binary — mark the
	// update done. This is the reconciliation path for the case where the
	// agent's own "done" COMMAND_RESPONSE (internal/hub/update.Service)
	// never arrives because the re-exec/restart happens too fast for it
	// to be flushed, or the WebSocket connection was already torn down by
	// the time it would have been sent.
	target := record.GetString("update_target_version")
	if target != "" && target == payload.Version && record.GetString("update_status") != "done" {
		record.Set("update_status", "done")
		record.Set("update_error", "")
	}

	if err := h.app.Save(record); err != nil {
		slog.Error("agent registration save error", "agent_id", ca.ID, "error", err)
		h.sendAck(ca, msg.Timestamp, "error")
		return
	}

	h.sendAck(ca, msg.Timestamp, "ok")
}

// handleMetrics processes METRICS messages from agents.
func (h *Hub) handleMetrics(ca *ConnectedAgent, msg *protocol.Message) {
	var payload protocol.MetricsPayload
	if err := msg.DecodePayload(&payload); err != nil {
		slog.Warn("agent metrics decode error", "agent_id", ca.ID, "error", err)
		return
	}

	// Ensure the agent ID in the payload matches the connection.
	payload.AgentID = ca.ID

	if h.metricHandler != nil {
		h.metricHandler(h.app, ca.ID, &payload)
	}

	// Update last_seen.
	h.updateLastSeen(ca.ID)
}

// handleCommandResponse processes COMMAND_RESPONSE messages from agents.
func (h *Hub) handleCommandResponse(ca *ConnectedAgent, msg *protocol.Message) {
	var payload protocol.CommandResponsePayload
	if err := msg.DecodePayload(&payload); err != nil {
		slog.Warn("agent command response decode error", "agent_id", ca.ID, "error", err)
		return
	}
	payload.AgentID = ca.ID
	slog.Info("agent command response",
		"agent_id", ca.ID,
		"command", payload.Command,
		"request_id", payload.RequestID,
		"error", payload.Error,
	)

	if h.commandResponseHandler != nil {
		h.commandResponseHandler(h.app, &payload)
	}
}

// handleLogs processes LOGS messages from agents.
func (h *Hub) handleLogs(ca *ConnectedAgent, msg *protocol.Message) {
	var payload protocol.LogsPayload
	if err := msg.DecodePayload(&payload); err != nil {
		slog.Warn("agent logs decode error", "agent_id", ca.ID, "error", err)
		return
	}

	// Ensure the agent ID in the payload matches the connection, the same
	// way handleMetrics does.
	payload.AgentID = ca.ID

	if h.logsHandler != nil {
		h.logsHandler(h.app, ca.ID, &payload)
	}

	h.updateLastSeen(ca.ID)
}

// handleHeartbeat processes HEARTBEAT messages from agents.
func (h *Hub) handleHeartbeat(ca *ConnectedAgent, msg *protocol.Message) {
	var payload protocol.HeartbeatPayload
	if err := msg.DecodePayload(&payload); err != nil {
		slog.Warn("agent heartbeat decode error", "agent_id", ca.ID, "error", err)
		return
	}

	ca.LastSeen = time.Now()
	h.recordHeartbeat(ca.ID, payload.DroppedMessages)
}

// updateLastSeen updates the agent's last_seen field and ensures status is online.
func (h *Hub) updateLastSeen(agentID string) {
	record, err := h.app.FindRecordById("agents", agentID)
	if err != nil {
		return
	}
	record.Set("last_seen", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))
	record.Set("status", "online")
	_ = h.app.Save(record)
}

// recordHeartbeat is like updateLastSeen but also persists the agent
// transport's latest cumulative dropped-message count (only HEARTBEAT
// messages carry it — METRICS messages go through updateLastSeen instead,
// which must not overwrite dropped_messages with a stale/zero value). It
// logs a warning whenever the count increases since the last heartbeat, so
// a persistently full agent send queue is visible instead of silently
// losing metrics/heartbeats.
func (h *Hub) recordHeartbeat(agentID string, droppedMessages uint64) {
	record, err := h.app.FindRecordById("agents", agentID)
	if err != nil {
		return
	}
	record.Set("last_seen", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))
	record.Set("status", "online")

	if previous := uint64(record.GetFloat("dropped_messages")); droppedMessages > previous {
		slog.Warn("agent dropped_messages increased (send queue overflowing)",
			"agent_id", agentID,
			"previous", previous,
			"current", droppedMessages,
		)
	}
	record.Set("dropped_messages", droppedMessages)

	_ = h.app.Save(record)
}

// removeAgent cleans up a disconnected agent.
func (h *Hub) removeAgent(agentID string) {
	h.agents.Delete(agentID)
	slog.Info("agent disconnected", "agent_id", agentID)

	// Mark agent as offline in the database.
	record, err := h.app.FindRecordById("agents", agentID)
	if err != nil {
		return
	}
	record.Set("status", "offline")
	_ = h.app.Save(record)
}

// sendAck sends an ACK message back to the agent.
func (h *Hub) sendAck(ca *ConnectedAgent, refTimestamp int64, status string) {
	ack := protocol.AckPayload{
		MessageTimestamp: refTimestamp,
		Status:           status,
	}
	ackData, err := msgpack.Marshal(ack)
	if err != nil {
		return
	}
	msg := &protocol.Message{
		Type:      protocol.MessageTypeAck,
		Payload:   ackData,
		Timestamp: time.Now().UnixMilli(),
	}
	if err := ca.Send(msg); err != nil {
		slog.Warn("agent ack send error", "agent_id", ca.ID, "error", err)
	}
}

// GetAgentsSummary returns a JSON-serializable summary of all connected agents.
func (h *Hub) GetAgentsSummary() []map[string]any {
	var agents []map[string]any
	h.agents.Range(func(key, value any) bool {
		ca := value.(*ConnectedAgent)
		agents = append(agents, map[string]any{
			"id":        ca.ID,
			"connected": true,
			"last_seen": ca.LastSeen,
		})
		return true
	})
	return agents
}
