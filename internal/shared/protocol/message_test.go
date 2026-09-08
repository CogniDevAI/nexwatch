package protocol

import (
	"testing"
	"time"
)

func TestMessageType_String(t *testing.T) {
	tests := []struct {
		name string
		mt   MessageType
		want string
	}{
		{name: "register", mt: MessageTypeRegister, want: "REGISTER"},
		{name: "metrics", mt: MessageTypeMetrics, want: "METRICS"},
		{name: "heartbeat", mt: MessageTypeHeartbeat, want: "HEARTBEAT"},
		{name: "command", mt: MessageTypeCommand, want: "COMMAND"},
		{name: "ack", mt: MessageTypeAck, want: "ACK"},
		{name: "command response", mt: MessageTypeCommandResponse, want: "COMMAND_RESPONSE"},
		{name: "logs", mt: MessageTypeLogs, want: "LOGS"},
		{name: "unknown zero value", mt: MessageType(0), want: "UNKNOWN"},
		{name: "unknown out of range", mt: MessageType(255), want: "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mt.String(); got != tt.want {
				t.Fatalf("MessageType(%d).String() = %q, want %q", tt.mt, got, tt.want)
			}
		})
	}
}

func TestNewMessage_EncodeDecodeRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		msgType MessageType
		payload any
	}{
		{
			name:    "register payload",
			msgType: MessageTypeRegister,
			payload: RegisterPayload{AgentID: "agent-1", Hostname: "host-a", OS: "linux", IP: "10.0.0.1", Version: "1.2.3"},
		},
		{
			name:    "metrics payload",
			msgType: MessageTypeMetrics,
			payload: MetricsPayload{
				AgentID: "agent-1",
				Metrics: []MetricData{
					{Type: "cpu", Data: map[string]any{"usage": 42.5}, Timestamp: 1000},
					{Type: "memory", Data: map[string]any{"used": int64(1024)}, Timestamp: 2000},
				},
			},
		},
		{
			name:    "heartbeat payload",
			msgType: MessageTypeHeartbeat,
			payload: HeartbeatPayload{AgentID: "agent-1", Uptime: 3600},
		},
		{
			name:    "command payload",
			msgType: MessageTypeCommand,
			payload: CommandPayload{Command: "thread_dump", Args: map[string]any{"pid": int64(1234)}},
		},
		{
			name:    "ack payload",
			msgType: MessageTypeAck,
			payload: AckPayload{MessageTimestamp: 5000, Status: "ok"},
		},
		{
			name:    "command response payload",
			msgType: MessageTypeCommandResponse,
			payload: CommandResponsePayload{
				Command:   "thread_dump",
				RequestID: "req-1",
				AgentID:   "agent-1",
				PID:       1234,
				Output:    "thread dump output",
				Error:     "",
			},
		},
		{
			name:    "logs payload",
			msgType: MessageTypeLogs,
			payload: LogsPayload{
				AgentID: "agent-1",
				Entries: []LogEntry{
					{Ts: 1000, Source: "journald", Unit: "nginx.service", Level: "error", Message: "boom", Fields: map[string]string{"syslog_identifier": "nginx"}},
					{Ts: 2000, Source: "file:/var/log/app.log", Level: "info", Message: "started"},
				},
				Dropped: 3,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := time.Now().UnixMilli()
			msg, err := NewMessage(tt.msgType, tt.payload)
			if err != nil {
				t.Fatalf("NewMessage() unexpected error: %v", err)
			}
			after := time.Now().UnixMilli()

			if msg.Type != tt.msgType {
				t.Fatalf("NewMessage() Type = %v, want %v", msg.Type, tt.msgType)
			}
			if msg.Timestamp < before || msg.Timestamp > after {
				t.Fatalf("NewMessage() Timestamp = %d, want between %d and %d", msg.Timestamp, before, after)
			}

			encoded, err := msg.Encode()
			if err != nil {
				t.Fatalf("Encode() unexpected error: %v", err)
			}

			decoded, err := Decode(encoded)
			if err != nil {
				t.Fatalf("Decode() unexpected error: %v", err)
			}

			if decoded.Type != tt.msgType {
				t.Fatalf("Decode() Type = %v, want %v", decoded.Type, tt.msgType)
			}
			if decoded.Timestamp != msg.Timestamp {
				t.Fatalf("Decode() Timestamp = %d, want %d (propagation lost)", decoded.Timestamp, msg.Timestamp)
			}

			switch tt.msgType {
			case MessageTypeRegister:
				var got RegisterPayload
				if err := decoded.DecodePayload(&got); err != nil {
					t.Fatalf("DecodePayload() unexpected error: %v", err)
				}
				want := tt.payload.(RegisterPayload)
				if got != want {
					t.Fatalf("DecodePayload() = %+v, want %+v", got, want)
				}
			case MessageTypeMetrics:
				var got MetricsPayload
				if err := decoded.DecodePayload(&got); err != nil {
					t.Fatalf("DecodePayload() unexpected error: %v", err)
				}
				want := tt.payload.(MetricsPayload)
				if got.AgentID != want.AgentID {
					t.Fatalf("DecodePayload() AgentID = %q, want %q", got.AgentID, want.AgentID)
				}
				if len(got.Metrics) != len(want.Metrics) {
					t.Fatalf("DecodePayload() Metrics len = %d, want %d", len(got.Metrics), len(want.Metrics))
				}
				for i := range want.Metrics {
					if got.Metrics[i].Type != want.Metrics[i].Type {
						t.Fatalf("DecodePayload() Metrics[%d].Type = %q, want %q", i, got.Metrics[i].Type, want.Metrics[i].Type)
					}
					if got.Metrics[i].Timestamp != want.Metrics[i].Timestamp {
						t.Fatalf("DecodePayload() Metrics[%d].Timestamp = %d, want %d", i, got.Metrics[i].Timestamp, want.Metrics[i].Timestamp)
					}
				}
			case MessageTypeHeartbeat:
				var got HeartbeatPayload
				if err := decoded.DecodePayload(&got); err != nil {
					t.Fatalf("DecodePayload() unexpected error: %v", err)
				}
				want := tt.payload.(HeartbeatPayload)
				if got != want {
					t.Fatalf("DecodePayload() = %+v, want %+v", got, want)
				}
			case MessageTypeCommand:
				var got CommandPayload
				if err := decoded.DecodePayload(&got); err != nil {
					t.Fatalf("DecodePayload() unexpected error: %v", err)
				}
				want := tt.payload.(CommandPayload)
				if got.Command != want.Command {
					t.Fatalf("DecodePayload() Command = %q, want %q", got.Command, want.Command)
				}
			case MessageTypeAck:
				var got AckPayload
				if err := decoded.DecodePayload(&got); err != nil {
					t.Fatalf("DecodePayload() unexpected error: %v", err)
				}
				want := tt.payload.(AckPayload)
				if got != want {
					t.Fatalf("DecodePayload() = %+v, want %+v", got, want)
				}
			case MessageTypeCommandResponse:
				var got CommandResponsePayload
				if err := decoded.DecodePayload(&got); err != nil {
					t.Fatalf("DecodePayload() unexpected error: %v", err)
				}
				want := tt.payload.(CommandResponsePayload)
				if got != want {
					t.Fatalf("DecodePayload() = %+v, want %+v", got, want)
				}
			case MessageTypeLogs:
				var got LogsPayload
				if err := decoded.DecodePayload(&got); err != nil {
					t.Fatalf("DecodePayload() unexpected error: %v", err)
				}
				want := tt.payload.(LogsPayload)
				if got.AgentID != want.AgentID || got.Dropped != want.Dropped {
					t.Fatalf("DecodePayload() = %+v, want %+v", got, want)
				}
				if len(got.Entries) != len(want.Entries) {
					t.Fatalf("DecodePayload() Entries len = %d, want %d", len(got.Entries), len(want.Entries))
				}
				for i := range want.Entries {
					if got.Entries[i].Message != want.Entries[i].Message || got.Entries[i].Level != want.Entries[i].Level {
						t.Fatalf("DecodePayload() Entries[%d] = %+v, want %+v", i, got.Entries[i], want.Entries[i])
					}
				}
			}
		})
	}
}

func TestNewMessage_UnmarshalableValueReturnsError(t *testing.T) {
	// A Go channel is a type msgpack cannot marshal, so NewMessage should
	// surface the underlying encoding error rather than paper over it.
	_, err := NewMessage(MessageTypeMetrics, make(chan int))
	if err == nil {
		t.Fatal("NewMessage() with unmarshalable payload expected error, got nil")
	}
}

func TestDecode_InvalidBytesReturnsError(t *testing.T) {
	_, err := Decode([]byte{0xff, 0xff, 0xff})
	if err == nil {
		t.Fatal("Decode() with invalid msgpack bytes expected error, got nil")
	}
}

func TestDecode_EmptyBytesReturnsError(t *testing.T) {
	_, err := Decode(nil)
	if err == nil {
		t.Fatal("Decode() with empty bytes expected error, got nil")
	}
}

func TestMessage_DecodePayload_MismatchedTargetReturnsError(t *testing.T) {
	msg, err := NewMessage(MessageTypeHeartbeat, HeartbeatPayload{AgentID: "a1", Uptime: 10})
	if err != nil {
		t.Fatalf("NewMessage() unexpected error: %v", err)
	}

	// Decoding a heartbeat payload's map/array shape into an incompatible
	// scalar target must fail loudly instead of silently zero-valuing it.
	var target int
	if err := msg.DecodePayload(&target); err == nil {
		t.Fatal("DecodePayload() into incompatible type expected error, got nil")
	}
}

func TestUnknownMessageType_RoundTripsWithoutPayloadInterpretation(t *testing.T) {
	// A message type the receiver does not recognize must still survive an
	// encode/decode round trip so the caller can log-and-ignore it.
	msg, err := NewMessage(MessageType(99), map[string]any{"raw": "data"})
	if err != nil {
		t.Fatalf("NewMessage() unexpected error: %v", err)
	}

	encoded, err := msg.Encode()
	if err != nil {
		t.Fatalf("Encode() unexpected error: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() unexpected error: %v", err)
	}

	if decoded.Type != MessageType(99) {
		t.Fatalf("Decode() Type = %v, want %v", decoded.Type, MessageType(99))
	}
	if decoded.Type.String() != "UNKNOWN" {
		t.Fatalf("Decode() Type.String() = %q, want UNKNOWN", decoded.Type.String())
	}
}
