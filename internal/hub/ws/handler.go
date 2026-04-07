package ws

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pocketbase/pocketbase/core"
	"github.com/vmihailenco/msgpack/v5"

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
						log.Printf("[ws] agent %s heartbeat timeout, disconnecting", agent.ID)
						agent.Conn.Close()
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
		log.Printf("[ws] upgrade error: %v", err)
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
	log.Printf("[ws] agent %s connected (token: %s...)", agentID, token[:min(8, len(token))])

	// Update agent status to online.
	agentRecord.Set("status", "online")
	agentRecord.Set("last_seen", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))
	if err := h.app.Save(agentRecord); err != nil {
		log.Printf("[ws] failed to update agent status: %v", err)
	}

	// Start read pump in a goroutine.
	go h.readPump(ca)
}

// findAgentByToken looks up an agent record by its token field.
func (h *Hub) findAgentByToken(token string) (*core.Record, error) {
	record, err := h.app.FindFirstRecordByFilter(
		"agents",
		"token = {:token}",
		map[string]any{"token": token},
	)
	return record, err
}

// readPump reads messages from the agent's WebSocket connection and routes them.
func (h *Hub) readPump(ca *ConnectedAgent) {
	defer func() {
		ca.Conn.Close()
		h.removeAgent(ca.ID)
	}()

	// Set read deadline and pong handler for keep-alive.
	ca.Conn.SetReadLimit(512 * 1024) // 512KB max message size
	ca.Conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	ca.Conn.SetPongHandler(func(string) error {
		ca.Conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})

	for {
		_, data, err := ca.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[ws] agent %s read error: %v", ca.ID, err)
			}
			return
		}

		// Reset read deadline on any message.
		ca.Conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		ca.LastSeen = time.Now()

		// Decode the envelope.
		msg, err := protocol.Decode(data)
		if err != nil {
			log.Printf("[ws] agent %s decode error: %v", ca.ID, err)
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

	default:
		log.Printf("[ws] agent %s sent unknown message type: %d", ca.ID, msg.Type)
	}
}

// handleRegister processes REGISTER messages from agents.
func (h *Hub) handleRegister(ca *ConnectedAgent, msg *protocol.Message) {
	var payload protocol.RegisterPayload
	if err := msg.DecodePayload(&payload); err != nil {
		log.Printf("[ws] agent %s register decode error: %v", ca.ID, err)
		return
	}

	log.Printf("[ws] agent %s registering: hostname=%s os=%s ip=%s version=%s",
		ca.ID, payload.Hostname, payload.OS, payload.IP, payload.Version)

	// Update agent record in PocketBase.
	record, err := h.app.FindRecordById("agents", ca.ID)
	if err != nil {
		log.Printf("[ws] agent %s not found for registration update: %v", ca.ID, err)
		return
	}

	record.Set("hostname", payload.Hostname)
	record.Set("os", payload.OS)
	record.Set("ip", payload.IP)
	record.Set("version", payload.Version)
	record.Set("status", "online")
	record.Set("last_seen", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))

	if err := h.app.Save(record); err != nil {
		log.Printf("[ws] agent %s registration save error: %v", ca.ID, err)
		h.sendAck(ca, msg.Timestamp, "error")
		return
	}

	h.sendAck(ca, msg.Timestamp, "ok")
}

// handleMetrics processes METRICS messages from agents.
func (h *Hub) handleMetrics(ca *ConnectedAgent, msg *protocol.Message) {
	var payload protocol.MetricsPayload
	if err := msg.DecodePayload(&payload); err != nil {
		log.Printf("[ws] agent %s metrics decode error: %v", ca.ID, err)
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
		log.Printf("[ws] agent %s command response decode error: %v", ca.ID, err)
		return
	}
	payload.AgentID = ca.ID
	log.Printf("[ws] agent %s command response: cmd=%s req_id=%s err=%q", ca.ID, payload.Command, payload.RequestID, payload.Error)

	if h.commandResponseHandler != nil {
		h.commandResponseHandler(h.app, &payload)
	}
}

// handleHeartbeat processes HEARTBEAT messages from agents.
func (h *Hub) handleHeartbeat(ca *ConnectedAgent, msg *protocol.Message) {
	var payload protocol.HeartbeatPayload
	if err := msg.DecodePayload(&payload); err != nil {
		log.Printf("[ws] agent %s heartbeat decode error: %v", ca.ID, err)
		return
	}

	ca.LastSeen = time.Now()
	h.updateLastSeen(ca.ID)
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

// removeAgent cleans up a disconnected agent.
func (h *Hub) removeAgent(agentID string) {
	h.agents.Delete(agentID)
	log.Printf("[ws] agent %s disconnected", agentID)

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
		log.Printf("[ws] agent %s ack send error: %v", ca.ID, err)
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

// marshalJSON is a helper for JSON encoding.
func marshalJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}
