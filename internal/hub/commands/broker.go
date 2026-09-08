// Package commands provides a small request/response broker for hub->agent
// COMMAND messages that an HTTP handler needs to wait on synchronously,
// generalizing the pending-request-map pattern originally embedded ad hoc
// in internal/hub/threaddump.Service (which stays on its own
// request/poll flow — see docker_routes.go for why the two are not
// merged).
package commands

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// ErrTimeout is returned by Send when no matching COMMAND_RESPONSE arrives
// before the timeout elapses.
var ErrTimeout = errors.New("timed out waiting for agent response")

// Sender can deliver a COMMAND payload to a connected agent. Implemented
// by ws.Hub in production; tests supply a fake.
type Sender interface {
	SendCommand(agentID string, payload *protocol.CommandPayload) error
}

// Broker correlates a COMMAND sent to an agent with its COMMAND_RESPONSE,
// keyed by request id, so an HTTP handler can await a synchronous-feeling
// result instead of returning immediately and making the caller poll.
type Broker struct {
	mu      sync.Mutex
	pending map[string]chan *protocol.CommandResponsePayload
}

// NewBroker creates an empty Broker.
func NewBroker() *Broker {
	return &Broker{pending: make(map[string]chan *protocol.CommandResponsePayload)}
}

// Send delivers payload to agentID via sender and blocks until a
// COMMAND_RESPONSE carrying the same requestID arrives (via HandleResponse),
// the timeout elapses, or ctx is canceled — whichever happens first.
//
// requestID must already be present in payload (e.g. in payload.Args, in
// whatever shape that command type expects) so the agent echoes it back on
// its response; Broker itself never inspects or sets payload's contents,
// it only uses requestID as the correlation key.
func (b *Broker) Send(ctx context.Context, sender Sender, agentID, requestID string, payload *protocol.CommandPayload, timeout time.Duration) (*protocol.CommandResponsePayload, error) {
	ch := make(chan *protocol.CommandResponsePayload, 1)

	b.mu.Lock()
	b.pending[requestID] = ch
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.pending, requestID)
		b.mu.Unlock()
	}()

	if err := sender.SendCommand(agentID, payload); err != nil {
		return nil, fmt.Errorf("agent not reachable: %w", err)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
		return nil, ErrTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// HandleResponse resolves a pending Send call for payload.RequestID, if one
// is waiting. It is safe to call for every COMMAND_RESPONSE the hub
// receives, including ones the broker knows nothing about (e.g. a
// thread_dump response, which uses its own request/poll flow via
// threaddump.Service) — those are silently ignored.
func (b *Broker) HandleResponse(payload *protocol.CommandResponsePayload) {
	b.mu.Lock()
	ch, ok := b.pending[payload.RequestID]
	if ok {
		delete(b.pending, payload.RequestID)
	}
	b.mu.Unlock()

	if ok {
		select {
		case ch <- payload:
		default:
			// The waiter already gave up (ctx canceled/timed out) and
			// stopped reading; drop the response rather than block.
		}
	}
}
