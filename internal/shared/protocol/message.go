package protocol

import (
	"time"

	"github.com/vmihailenco/msgpack/v5"
)

// MessageType identifies the kind of WebSocket message.
type MessageType uint8

const (
	// MessageTypeRegister is sent by the agent on initial connection.
	MessageTypeRegister MessageType = iota + 1
	// MessageTypeMetrics carries metric data from agent to hub.
	MessageTypeMetrics
	// MessageTypeHeartbeat is a keep-alive ping from the agent.
	MessageTypeHeartbeat
	// MessageTypeCommand is sent from hub to agent for remote actions.
	MessageTypeCommand
	// MessageTypeAck acknowledges receipt of a message.
	MessageTypeAck
	// MessageTypeCommandResponse carries the result of a command back to the hub.
	MessageTypeCommandResponse
)

// String returns the human-readable name of a MessageType.
func (mt MessageType) String() string {
	switch mt {
	case MessageTypeRegister:
		return "REGISTER"
	case MessageTypeMetrics:
		return "METRICS"
	case MessageTypeHeartbeat:
		return "HEARTBEAT"
	case MessageTypeCommand:
		return "COMMAND"
	case MessageTypeAck:
		return "ACK"
	case MessageTypeCommandResponse:
		return "COMMAND_RESPONSE"
	default:
		return "UNKNOWN"
	}
}

// Message is the top-level envelope for all WebSocket communication.
type Message struct {
	Type      MessageType        `msgpack:"t"`
	Payload   msgpack.RawMessage `msgpack:"p"`
	Timestamp int64              `msgpack:"ts"`
}

// NewMessage creates a Message with the given type and marshaled payload.
func NewMessage(msgType MessageType, payload any) (*Message, error) {
	data, err := msgpack.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &Message{
		Type:      msgType,
		Payload:   data,
		Timestamp: time.Now().UnixMilli(),
	}, nil
}

// Encode serializes the Message to MessagePack bytes.
func (m *Message) Encode() ([]byte, error) {
	return msgpack.Marshal(m)
}

// Decode deserializes MessagePack bytes into a Message.
func Decode(data []byte) (*Message, error) {
	var msg Message
	if err := msgpack.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// DecodePayload unmarshals the message payload into the given target.
func (m *Message) DecodePayload(target any) error {
	return msgpack.Unmarshal(m.Payload, target)
}

// RegisterPayload is sent by the agent during initial registration.
type RegisterPayload struct {
	AgentID  string `msgpack:"id"`
	Hostname string `msgpack:"hostname"`
	OS       string `msgpack:"os"`
	IP       string `msgpack:"ip"`
	Version  string `msgpack:"version"`
}

// MetricsPayload carries a batch of metrics from agent to hub.
type MetricsPayload struct {
	AgentID string       `msgpack:"a"`
	Metrics []MetricData `msgpack:"m"`
}

// MetricData represents a single metric within a MetricsPayload.
type MetricData struct {
	Type      string         `msgpack:"t"`
	Data      map[string]any `msgpack:"d"`
	Timestamp int64          `msgpack:"ts"`
}

// HeartbeatPayload is a lightweight keep-alive message.
type HeartbeatPayload struct {
	AgentID string `msgpack:"id"`
	Uptime  int64  `msgpack:"uptime"`
}

// CommandPayload is sent from hub to agent for remote commands.
type CommandPayload struct {
	Command string         `msgpack:"cmd"`
	Args    map[string]any `msgpack:"args,omitempty"`
}

// AckPayload acknowledges a received message.
type AckPayload struct {
	MessageTimestamp int64  `msgpack:"ref"`
	Status           string `msgpack:"status"`
}

// CommandResponsePayload carries the result of a hub-initiated command.
type CommandResponsePayload struct {
	Command   string `msgpack:"cmd"`
	RequestID string `msgpack:"req_id"`
	AgentID   string `msgpack:"agent_id"`
	PID       int    `msgpack:"pid,omitempty"`
	Output    string `msgpack:"output"`
	Error     string `msgpack:"error,omitempty"`
}
