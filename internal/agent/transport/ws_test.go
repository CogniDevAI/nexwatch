package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

func TestNextBackoff_GrowsAndCaps(t *testing.T) {
	tests := []struct {
		name    string
		current time.Duration
		want    time.Duration
	}{
		{name: "doubles from initial", current: initialBackoff, want: 2 * time.Second},
		{name: "doubles again", current: 2 * time.Second, want: 4 * time.Second},
		{name: "doubles again from 4s", current: 4 * time.Second, want: 8 * time.Second},
		{name: "stays capped once at max", current: maxBackoff, want: maxBackoff},
		{name: "clamps when doubling would exceed max", current: 40 * time.Second, want: maxBackoff},
		{name: "clamps value just under max", current: 59 * time.Second, want: maxBackoff},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextBackoff(tt.current); got != tt.want {
				t.Errorf("nextBackoff(%v) = %v, want %v", tt.current, got, tt.want)
			}
		})
	}
}

func TestNextBackoff_NeverExceedsMax(t *testing.T) {
	backoff := initialBackoff
	for i := 0; i < 20; i++ {
		backoff = nextBackoff(backoff)
		if backoff > maxBackoff {
			t.Fatalf("nextBackoff exceeded maxBackoff after %d iterations: %v > %v", i, backoff, maxBackoff)
		}
	}
	if backoff != maxBackoff {
		t.Errorf("nextBackoff after repeated growth = %v, want to have settled at maxBackoff %v", backoff, maxBackoff)
	}
}

func TestNewWSTransport_InitializesBackoffAndHeartbeatDefaults(t *testing.T) {
	tr := NewWSTransport("ws://example.com/ws", "tok", "agent-1")

	if tr.currentBackoff != initialBackoff {
		t.Errorf("NewWSTransport() currentBackoff = %v, want %v", tr.currentBackoff, initialBackoff)
	}
	if tr.heartbeatEvery != heartbeatInterval {
		t.Errorf("NewWSTransport() heartbeatEvery = %v, want %v", tr.heartbeatEvery, heartbeatInterval)
	}
}

func TestWSTransport_ResetBackoff(t *testing.T) {
	tr := NewWSTransport("ws://example.com/ws", "tok", "agent-1")
	tr.currentBackoff = maxBackoff

	tr.resetBackoff()

	if tr.currentBackoff != initialBackoff {
		t.Errorf("resetBackoff() currentBackoff = %v, want %v", tr.currentBackoff, initialBackoff)
	}
}

func TestSend_QueuesEncodedMessage(t *testing.T) {
	tr := NewWSTransport("ws://example.com/ws", "tok", "agent-1")

	msg, err := protocol.NewMessage(protocol.MessageTypeHeartbeat, &protocol.HeartbeatPayload{AgentID: "agent-1"})
	if err != nil {
		t.Fatalf("NewMessage() unexpected error: %v", err)
	}

	if err := tr.Send(msg); err != nil {
		t.Fatalf("Send() unexpected error: %v", err)
	}

	select {
	case data := <-tr.sendCh:
		decoded, err := protocol.Decode(data)
		if err != nil {
			t.Fatalf("Decode() unexpected error: %v", err)
		}
		if decoded.Type != protocol.MessageTypeHeartbeat {
			t.Errorf("queued message Type = %v, want %v", decoded.Type, protocol.MessageTypeHeartbeat)
		}
	default:
		t.Fatal("Send() did not queue the message onto sendCh")
	}
}

func TestSend_DropsWithoutBlockingWhenQueueFull(t *testing.T) {
	tr := NewWSTransport("ws://example.com/ws", "tok", "agent-1")

	msg, err := protocol.NewMessage(protocol.MessageTypeHeartbeat, &protocol.HeartbeatPayload{AgentID: "agent-1"})
	if err != nil {
		t.Fatalf("NewMessage() unexpected error: %v", err)
	}

	// Fill the queue to capacity.
	for i := 0; i < sendQueueSize; i++ {
		if err := tr.Send(msg); err != nil {
			t.Fatalf("Send() #%d unexpected error while filling queue: %v", i, err)
		}
	}

	// The queue is now full; Send must return an error immediately rather
	// than blocking the caller.
	done := make(chan error, 1)
	go func() { done <- tr.Send(msg) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Send() on a full queue expected an error, got nil")
		}
		if !strings.Contains(err.Error(), "send queue full") {
			t.Errorf("Send() error = %q, want it to mention the full queue", err.Error())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Send() blocked instead of dropping the message when the queue was full")
	}

	if got := tr.DroppedMessages(); got != 1 {
		t.Fatalf("DroppedMessages() = %d, want 1 after exactly one dropped send", got)
	}
}

// TestSend_DroppedMessagesAccumulatesAcrossMultipleDrops asserts the
// dropped-message counter keeps incrementing (rather than saturating at 1
// or resetting) across repeated drops, since it is meant to be reported
// as a cumulative total in the agent's HEARTBEAT payload.
func TestSend_DroppedMessagesAccumulatesAcrossMultipleDrops(t *testing.T) {
	tr := NewWSTransport("ws://example.com/ws", "tok", "agent-1")

	msg, err := protocol.NewMessage(protocol.MessageTypeHeartbeat, &protocol.HeartbeatPayload{AgentID: "agent-1"})
	if err != nil {
		t.Fatalf("NewMessage() unexpected error: %v", err)
	}

	for i := 0; i < sendQueueSize; i++ {
		if err := tr.Send(msg); err != nil {
			t.Fatalf("Send() #%d unexpected error while filling queue: %v", i, err)
		}
	}
	if got := tr.DroppedMessages(); got != 0 {
		t.Fatalf("DroppedMessages() = %d, want 0 before the queue overflows", got)
	}

	const extraDrops = 3
	for i := 0; i < extraDrops; i++ {
		if err := tr.Send(msg); err == nil {
			t.Fatalf("Send() #%d on a full queue expected an error, got nil", i)
		}
	}

	if got := tr.DroppedMessages(); got != extraDrops {
		t.Fatalf("DroppedMessages() = %d, want %d", got, extraDrops)
	}
}

// newTestWSServer starts an httptest server that upgrades every request to
// a WebSocket, requires a non-empty Authorization header, and records the
// first N raw frames it receives onto the returned channel.
func newTestWSServer(t *testing.T, received chan<- []byte) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			select {
			case received <- data:
			default:
			}
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func TestWSTransport_ConnectRegisterHeartbeatHappyPath(t *testing.T) {
	var mu sync.Mutex
	var frames [][]byte
	received := make(chan []byte, 16)

	server := newTestWSServer(t, received)

	tr := NewWSTransport(wsURL(server.URL), "test-token", "agent-1")
	tr.heartbeatEvery = 20 * time.Millisecond

	connected := make(chan struct{}, 1)
	tr.OnConnect = func() {
		regMsg, err := protocol.NewMessage(protocol.MessageTypeRegister, &protocol.RegisterPayload{AgentID: "agent-1"})
		if err != nil {
			t.Errorf("NewMessage(register) unexpected error: %v", err)
			return
		}
		if err := tr.Send(regMsg); err != nil {
			t.Errorf("Send(register) unexpected error: %v", err)
		}
		select {
		case connected <- struct{}{}:
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go tr.Start(ctx)
	defer tr.Wait()
	defer tr.Stop()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("OnConnect was never called")
	}

	// Collect frames until we have seen at least a REGISTER and one HEARTBEAT,
	// or time out.
	deadline := time.After(2 * time.Second)
	sawRegister, sawHeartbeat := false, false
	for !sawRegister || !sawHeartbeat {
		select {
		case data := <-received:
			mu.Lock()
			frames = append(frames, data)
			mu.Unlock()
			msg, err := protocol.Decode(data)
			if err != nil {
				t.Fatalf("server received undecodable frame: %v", err)
			}
			switch msg.Type {
			case protocol.MessageTypeRegister:
				sawRegister = true
			case protocol.MessageTypeHeartbeat:
				sawHeartbeat = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for REGISTER+HEARTBEAT frames; got %d frames, sawRegister=%v sawHeartbeat=%v", len(frames), sawRegister, sawHeartbeat)
		}
	}

	cancel()
}

func TestWSTransport_ConnectFailureTriggersBackoffWait(t *testing.T) {
	// Point at a URL with nothing listening; connect() must fail and Start
	// must not panic, instead going through the backoff/wait path until the
	// context is cancelled.
	tr := NewWSTransport("ws://127.0.0.1:1/ws", "tok", "agent-1")

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		tr.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start() did not return after context cancellation on connect failure")
	}
}

func TestWSTransport_StopIsIdempotent(t *testing.T) {
	tr := NewWSTransport("ws://127.0.0.1:1/ws", "tok", "agent-1")

	// Calling Stop multiple times must not panic (close of closed channel).
	tr.Stop()
	tr.Stop()
}

// TestWSTransport_StopWhileConnectedUnblocksWait is a regression test for a
// bug where Stop() had no effect on an actively-connected transport: the
// read/write pumps only ever watched the ctx passed into Start() (tied to
// the process's own SIGINT/SIGTERM), never t.stopCh, so calling Stop() —
// and then Wait(), as the self-update restart flow in cmd/agent/main.go
// does — blocked forever as long as the connection stayed healthy. This
// asserts Stop() reliably unblocks Wait() well within a healthy
// connection's heartbeat interval, without needing the context passed to
// Start() to ever be cancelled.
func TestWSTransport_StopWhileConnectedUnblocksWait(t *testing.T) {
	received := make(chan []byte, 16)
	server := newTestWSServer(t, received)

	tr := NewWSTransport(wsURL(server.URL), "test-token", "agent-1")
	tr.heartbeatEvery = time.Hour // never fire — Stop() must not depend on it.

	connected := make(chan struct{}, 1)
	tr.OnConnect = func() {
		select {
		case connected <- struct{}{}:
		default:
		}
	}

	// A ctx that is never cancelled by this test — only Stop() should end Start().
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		tr.Start(ctx)
		close(done)
	}()

	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("OnConnect was never called")
	}

	stopReturned := make(chan struct{})
	go func() {
		tr.Stop()
		close(stopReturned)
	}()

	select {
	case <-stopReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() itself did not return")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Start() did not return after Stop() while connected — Wait() would block forever")
	}
}
