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
	// MessageTypeLogs carries a batch of log entries from agent to hub
	// (internal/agent/logs, internal/hub/logs).
	MessageTypeLogs
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
	case MessageTypeLogs:
		return "LOGS"
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
	// Arch is the agent's runtime.GOARCH (e.g. "amd64", "arm64") — added
	// for F9 (agent self-update) so the hub knows which release asset
	// architecture to request. Older agents omit it and decode as "".
	Arch string `msgpack:"arch,omitempty"`
	// Platform is the agent's runtime.GOOS (e.g. "linux", "darwin").
	// Deliberately distinct from OS above: OS is a human-readable display
	// string (e.g. "ubuntu 22.04" via gopsutil host.Info(), see
	// cmd/agent/main.go buildRegisterMessage) that never matches the
	// release asset naming scheme's GOOS component
	// (nexwatch-agent_<version>_<goos>_<goarch>.tar.gz — see
	// .github/workflows/release.yml), so a self-update download URL built
	// from OS would be wrong for every non-macOS host. Platform carries
	// the exact runtime.GOOS string the release assets are actually built
	// for. Older agents omit it and decode as "".
	Platform string `msgpack:"platform,omitempty"`
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

// LogEntry is one shipped log line within a LogsPayload.
type LogEntry struct {
	// Ts is the entry's timestamp in unix milliseconds.
	Ts int64 `msgpack:"ts"`
	// Source identifies where the entry came from: "journald" for a
	// systemd-journal source, or "file:<path>" for a tailed file source.
	Source string `msgpack:"src"`
	// Unit is the systemd unit name (journald sources) or empty (file
	// sources — a file has no inherent "unit").
	Unit string `msgpack:"unit,omitempty"`
	// Level is one of "error", "warning", "info", "debug".
	Level string `msgpack:"level"`
	// Message is the log line text, capped hub-side to the "logs.message"
	// field's 8 KiB limit (internal/hub/migrations/logs.go).
	Message string `msgpack:"msg"`
	// Fields carries a small set of extra structured attributes (e.g.
	// journald's SYSLOG_IDENTIFIER). Deliberately kept small — this rides
	// in every batched WebSocket message.
	Fields map[string]string `msgpack:"fields,omitempty"`
}

// LogsPayload carries a batch of log entries from agent to hub.
type LogsPayload struct {
	AgentID string     `msgpack:"a"`
	Entries []LogEntry `msgpack:"entries"`
	// Dropped is the number of lines the agent's log pipeline discarded
	// since the previous batch because of rate limiting or a full internal
	// queue (internal/agent/logs.Manager) — never because a line failed to
	// parse. Reported so an operator can see the pipeline is shedding load
	// instead of silently losing lines.
	Dropped uint64 `msgpack:"dropped,omitempty"`
}

// HeartbeatPayload is a lightweight keep-alive message.
type HeartbeatPayload struct {
	AgentID string `msgpack:"id"`
	Uptime  int64  `msgpack:"uptime"`

	// DroppedMessages is the agent transport's cumulative count of messages
	// discarded because its outgoing send queue was full (see
	// internal/agent/transport.WSTransport.Send). It is added with a new
	// field tag rather than reusing an existing one so older agents (which
	// omit it) decode fine against a newer hub, and it simply defaults to 0.
	DroppedMessages uint64 `msgpack:"dropped,omitempty"`
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
// Fields below Error are optional and only populated by specific command
// types (PID/Output by "thread_dump"; OK/ContainerID/Action/State by
// "docker_action") — a generic COMMAND_RESPONSE decoder (ws.Hub.
// handleCommandResponse) never needs to know which command produced it.
type CommandResponsePayload struct {
	Command   string `msgpack:"cmd"`
	RequestID string `msgpack:"req_id"`
	AgentID   string `msgpack:"agent_id"`
	PID       int    `msgpack:"pid,omitempty"`
	Output    string `msgpack:"output"`
	Error     string `msgpack:"error,omitempty"`

	// OK reports whether a "docker_action" command succeeded.
	OK bool `msgpack:"ok,omitempty"`
	// ContainerID is the target container of a "docker_action" response.
	ContainerID string `msgpack:"container_id,omitempty"`
	// Action is the docker lifecycle action ("start"/"stop"/"restart") a
	// "docker_action" response was for.
	Action string `msgpack:"action,omitempty"`
	// State is the container's state (e.g. "running", "exited") after a
	// "docker_action" command completed.
	State string `msgpack:"state,omitempty"`

	// Stage reports progress for an "update" command: one of "started",
	// "downloading", "verifying", "installing", "restarting", "failed",
	// "done". The agent sends one COMMAND_RESPONSE per stage, all carrying
	// the same RequestID — internal/hub/update.Service reduces the stream
	// into the agent record's update_status field (see
	// internal/hub/ws/handler.go's commandResponseHandler wiring in
	// cmd/hub/main.go). The first ("started") response is also the one
	// internal/hub/commands.Broker.Send blocks on, so the HTTP handler
	// returns as soon as the agent has accepted the command, without
	// waiting for the whole download/verify/install to finish.
	Stage string `msgpack:"stage,omitempty"`
	// FromVersion/ToVersion report the update's source and target agent
	// version, populated on the "done"/"failed" terminal stages.
	FromVersion string `msgpack:"from_version,omitempty"`
	ToVersion   string `msgpack:"to_version,omitempty"`
}

// DockerActionPayload is the argument shape for a "docker_action" COMMAND,
// carried inside CommandPayload.Args. Args is a loosely-typed map (rather
// than a nested msgpack struct) for the same reason ThreadDump's pid/
// request_id/process_name args are — the agent-side decoder must tolerate
// whatever concrete type msgpack chooses for each value.
type DockerActionPayload struct {
	RequestID   string
	ContainerID string
	Action      string
}

// ToArgs converts p into the map[string]any expected by CommandPayload.Args.
func (p DockerActionPayload) ToArgs() map[string]any {
	return map[string]any{
		"request_id":   p.RequestID,
		"container_id": p.ContainerID,
		"action":       p.Action,
	}
}

// ParseDockerActionArgs extracts a DockerActionPayload from a decoded
// CommandPayload's Args map, defaulting every field to "" when absent or
// of an unexpected type.
func ParseDockerActionArgs(args map[string]any) DockerActionPayload {
	p := DockerActionPayload{}
	if v, ok := args["request_id"].(string); ok {
		p.RequestID = v
	}
	if v, ok := args["container_id"].(string); ok {
		p.ContainerID = v
	}
	if v, ok := args["action"].(string); ok {
		p.Action = v
	}
	return p
}

// UpdatePayload is the argument shape for an "update" COMMAND, carried
// inside CommandPayload.Args, mirroring DockerActionPayload's shape/reason
// for existing as a loosely-typed map rather than a nested msgpack struct.
type UpdatePayload struct {
	RequestID string
	// Version is the target agent version, without a leading "v" (e.g.
	// "0.9.1"), matching the release asset filename convention in
	// .github/workflows/release.yml.
	Version string
	// BaseURL is the release download base (the "agent_release_base_url"
	// setting, e.g. "https://github.com/CogniDevAI/nexwatch/releases/download"),
	// with the full asset URL built as "<BaseURL>/v<Version>/<asset>".
	BaseURL string
	// OS is the target runtime.GOOS (e.g. "linux", "darwin") — the
	// agent's own registered Platform (protocol.RegisterPayload.Platform),
	// resolved hub-side.
	OS string
	// Arch is the target runtime.GOARCH (e.g. "amd64", "arm64") — the
	// agent's own registered Arch (protocol.RegisterPayload.Arch).
	Arch string
}

// ToArgs converts p into the map[string]any expected by CommandPayload.Args.
func (p UpdatePayload) ToArgs() map[string]any {
	return map[string]any{
		"request_id": p.RequestID,
		"version":    p.Version,
		"base_url":   p.BaseURL,
		"os":         p.OS,
		"arch":       p.Arch,
	}
}

// ParseUpdateArgs extracts an UpdatePayload from a decoded CommandPayload's
// Args map, defaulting every field to "" when absent or of an unexpected
// type — the same tolerance ParseDockerActionArgs applies, since msgpack
// decodes an "any"-typed map's values into whatever concrete Go type it
// chooses.
func ParseUpdateArgs(args map[string]any) UpdatePayload {
	p := UpdatePayload{}
	if v, ok := args["request_id"].(string); ok {
		p.RequestID = v
	}
	if v, ok := args["version"].(string); ok {
		p.Version = v
	}
	if v, ok := args["base_url"].(string); ok {
		p.BaseURL = v
	}
	if v, ok := args["os"].(string); ok {
		p.OS = v
	}
	if v, ok := args["arch"].(string); ok {
		p.Arch = v
	}
	return p
}
