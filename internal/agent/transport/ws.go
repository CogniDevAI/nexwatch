package transport

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

const (
	// sendQueueSize is the buffer size for the outgoing message channel.
	sendQueueSize = 256

	// heartbeatInterval is how often we send heartbeat messages.
	heartbeatInterval = 30 * time.Second

	// writeTimeout is the deadline for writing a single message.
	writeTimeout = 10 * time.Second

	// initialBackoff is the starting delay for reconnection attempts.
	initialBackoff = 1 * time.Second

	// maxBackoff is the maximum delay between reconnection attempts.
	maxBackoff = 60 * time.Second

	// backoffMultiplier doubles the delay on each failed attempt.
	backoffMultiplier = 2
)

// WSTransport manages a persistent WebSocket connection to the hub
// with auto-reconnect, heartbeat, and a buffered send queue.
type WSTransport struct {
	hubURL  string
	token   string
	agentID string

	conn           *websocket.Conn
	connMu         sync.Mutex
	sendCh         chan []byte
	stopCh         chan struct{}
	done           chan struct{}
	currentBackoff time.Duration

	// droppedMessages is a cumulative count of messages discarded because
	// sendCh was full (see Send). It is read via DroppedMessages() and
	// reported in the agent's HEARTBEAT payload so the hub — and an
	// operator — can see that the queue has been overflowing instead of
	// silently losing metrics/heartbeats.
	droppedMessages atomic.Uint64

	// heartbeatInterval controls how often the write pump emits a
	// HEARTBEAT message. It defaults to heartbeatInterval (the package
	// constant) and exists as a seam so tests can drive a fast heartbeat
	// against a local WebSocket server instead of waiting 30s.
	heartbeatEvery time.Duration

	// OnConnect is called each time a connection is established.
	// Use this to send REGISTER messages after reconnection.
	OnConnect func()

	// OnMessage is called when a message is received from the hub.
	OnMessage func(msg *protocol.Message)
}

// NewWSTransport creates a new WebSocket transport.
func NewWSTransport(hubURL, token, agentID string) *WSTransport {
	return &WSTransport{
		hubURL:         hubURL,
		token:          token,
		agentID:        agentID,
		sendCh:         make(chan []byte, sendQueueSize),
		stopCh:         make(chan struct{}),
		done:           make(chan struct{}),
		currentBackoff: initialBackoff,
		heartbeatEvery: heartbeatInterval,
	}
}

// Start begins the transport: connect, read pump, write pump, heartbeat.
// It blocks until ctx is cancelled or Stop is called.
func (t *WSTransport) Start(ctx context.Context) {
	defer close(t.done)

	// Derive a context that also cancels when Stop() closes stopCh. Without
	// this, Stop() only unblocks the reconnect-loop's own top-of-loop check
	// below — it has no effect at all while runPumps is actively running
	// against a healthy connection (its pumpCtx, in turn derived from this
	// same ctx, would never see stopCh close), so Stop()+Wait() could block
	// forever whenever the agent asked to shut down mid-connection — e.g.
	// the self-update flow's reexecOrExit (cmd/agent/main.go).
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-t.stopCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	for {
		select {
		case <-ctx.Done():
			t.closeConn()
			return
		case <-t.stopCh:
			t.closeConn()
			return
		default:
		}

		if err := t.connect(ctx); err != nil {
			log.Printf("[transport] connection failed: %v", err)
			if !t.waitBackoff(ctx) {
				return
			}
			continue
		}

		log.Printf("[transport] connected to %s", t.hubURL)
		t.resetBackoff()

		if t.OnConnect != nil {
			t.OnConnect()
		}

		// Run read/write pumps until disconnect.
		t.runPumps(ctx)

		log.Println("[transport] disconnected, will reconnect...")
	}
}

// Stop signals the transport to shut down. It also immediately closes any
// active connection so a read pump blocked in conn.ReadMessage() (which
// does not itself observe context cancellation — only the code around it
// does) wakes up right away instead of waiting on the peer to notice a
// close frame or on the next heartbeat tick.
func (t *WSTransport) Stop() {
	select {
	case <-t.stopCh:
	default:
		close(t.stopCh)
	}
	t.closeConn()
}

// Wait blocks until the transport has fully stopped.
func (t *WSTransport) Wait() {
	<-t.done
}

// Send encodes a protocol message and queues it for sending.
// It is safe to call from multiple goroutines.
func (t *WSTransport) Send(msg *protocol.Message) error {
	data, err := msg.Encode()
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}

	select {
	case t.sendCh <- data:
		return nil
	default:
		total := t.droppedMessages.Add(1)
		log.Printf("[transport] send queue full, dropping message type=%s (dropped_messages total: %d)", msg.Type, total)
		return fmt.Errorf("send queue full, dropping message type=%s", msg.Type)
	}
}

// DroppedMessages returns the cumulative count of messages discarded so far
// because the send queue was full.
func (t *WSTransport) DroppedMessages() uint64 {
	return t.droppedMessages.Load()
}

// connect establishes a WebSocket connection to the hub.
func (t *WSTransport) connect(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", t.token)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, t.hubURL, header)
	if err != nil {
		return fmt.Errorf("dial %s: %w", t.hubURL, err)
	}

	t.connMu.Lock()
	t.conn = conn
	t.connMu.Unlock()

	return nil
}

// closeConn safely closes the current connection.
func (t *WSTransport) closeConn() {
	t.connMu.Lock()
	defer t.connMu.Unlock()
	if t.conn != nil {
		_ = t.conn.Close()
		t.conn = nil
	}
}

// runPumps runs the read and write pumps concurrently.
// Returns when either pump exits (connection lost).
func (t *WSTransport) runPumps(ctx context.Context) {
	pumpCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer cancel()
		t.readPump(pumpCtx)
	}()

	go func() {
		defer wg.Done()
		defer cancel()
		t.writePump(pumpCtx)
	}()

	wg.Wait()
	t.closeConn()
}

// readPump continuously reads messages from the WebSocket.
func (t *WSTransport) readPump(ctx context.Context) {
	t.connMu.Lock()
	conn := t.conn
	t.connMu.Unlock()
	if conn == nil {
		return
	}

	conn.SetReadLimit(512 * 1024) // 512KB

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[transport] read error: %v", err)
			}
			return
		}

		if t.OnMessage != nil {
			msg, err := protocol.Decode(data)
			if err != nil {
				log.Printf("[transport] decode error: %v", err)
				continue
			}
			t.OnMessage(msg)
		}
	}
}

// writePump sends queued messages and heartbeats.
func (t *WSTransport) writePump(ctx context.Context) {
	heartbeatTicker := time.NewTicker(t.heartbeatEvery)
	defer heartbeatTicker.Stop()

	t.connMu.Lock()
	conn := t.conn
	t.connMu.Unlock()
	if conn == nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			// Send close message before exiting.
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(writeTimeout),
			)
			return

		case data := <-t.sendCh:
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
				log.Printf("[transport] write error: %v", err)
				return
			}

		case <-heartbeatTicker.C:
			hb, err := protocol.NewMessage(protocol.MessageTypeHeartbeat, &protocol.HeartbeatPayload{
				AgentID:         t.agentID,
				DroppedMessages: t.droppedMessages.Load(),
			})
			if err != nil {
				log.Printf("[transport] heartbeat encode error: %v", err)
				continue
			}
			data, err := hb.Encode()
			if err != nil {
				continue
			}
			_ = conn.SetWriteDeadline(time.Now().Add(writeTimeout))
			if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
				log.Printf("[transport] heartbeat write error: %v", err)
				return
			}

		case <-t.stopCh:
			return
		}
	}
}

// resetBackoff resets the backoff timer after a successful connection.
func (t *WSTransport) resetBackoff() {
	t.currentBackoff = initialBackoff
}

// waitBackoff waits for the current backoff duration, then doubles it.
// Returns false if the context was cancelled during the wait.
func (t *WSTransport) waitBackoff(ctx context.Context) bool {
	log.Printf("[transport] reconnecting in %s...", t.currentBackoff)

	timer := time.NewTimer(t.currentBackoff)
	defer timer.Stop()

	select {
	case <-timer.C:
		t.currentBackoff = nextBackoff(t.currentBackoff)
		return true
	case <-ctx.Done():
		return false
	case <-t.stopCh:
		return false
	}
}

// nextBackoff computes the next reconnect delay given the current one,
// doubling it (backoffMultiplier) and capping it at maxBackoff. It is a
// pure function so the growth/cap behavior can be tested without timers.
func nextBackoff(current time.Duration) time.Duration {
	next := current * backoffMultiplier
	if next > maxBackoff {
		return maxBackoff
	}
	return next
}
