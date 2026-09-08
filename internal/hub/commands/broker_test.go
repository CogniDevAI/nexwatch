package commands

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// fakeSender is a Sender test double. When respond is set, it simulates
// the agent replying asynchronously (as ws.Hub.readPump -> HandleResponse
// would in production) shortly after SendCommand is called.
type fakeSender struct {
	mu      sync.Mutex
	sendErr error
	sent    []string // agent ids SendCommand was called with
	respond func(broker *Broker, requestID string)
	broker  *Broker
}

func (f *fakeSender) SendCommand(agentID string, payload *protocol.CommandPayload) error {
	f.mu.Lock()
	f.sent = append(f.sent, agentID)
	f.mu.Unlock()

	if f.sendErr != nil {
		return f.sendErr
	}
	if f.respond != nil {
		requestID, _ := payload.Args["request_id"].(string)
		go f.respond(f.broker, requestID)
	}
	return nil
}

func TestBroker_SendReturnsMatchingResponse(t *testing.T) {
	broker := NewBroker()
	sender := &fakeSender{broker: broker}
	sender.respond = func(b *Broker, requestID string) {
		b.HandleResponse(&protocol.CommandResponsePayload{
			RequestID: requestID,
			OK:        true,
			State:     "running",
		})
	}

	payload := &protocol.CommandPayload{Command: "docker_action", Args: map[string]any{"request_id": "req-1"}}
	resp, err := broker.Send(context.Background(), sender, "agent-1", "req-1", payload, time.Second)

	if err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}
	if resp == nil || !resp.OK || resp.State != "running" {
		t.Errorf("Send() response = %+v, want OK=true State=running", resp)
	}
	if len(sender.sent) != 1 || sender.sent[0] != "agent-1" {
		t.Errorf("SendCommand called with %v, want [agent-1]", sender.sent)
	}
}

func TestBroker_SendPropagatesSenderError(t *testing.T) {
	broker := NewBroker()
	sender := &fakeSender{sendErr: errors.New("agent not connected")}

	payload := &protocol.CommandPayload{Command: "docker_action", Args: map[string]any{"request_id": "req-2"}}
	resp, err := broker.Send(context.Background(), sender, "agent-2", "req-2", payload, time.Second)

	if err == nil {
		t.Fatal("Send() error = nil, want an error when the sender fails")
	}
	if resp != nil {
		t.Errorf("Send() response = %+v, want nil on sender error", resp)
	}
}

func TestBroker_SendTimesOutWithoutAResponse(t *testing.T) {
	broker := NewBroker()
	sender := &fakeSender{} // never calls HandleResponse

	payload := &protocol.CommandPayload{Command: "docker_action", Args: map[string]any{"request_id": "req-3"}}
	_, err := broker.Send(context.Background(), sender, "agent-3", "req-3", payload, 20*time.Millisecond)

	if !errors.Is(err, ErrTimeout) {
		t.Errorf("Send() error = %v, want ErrTimeout", err)
	}
}

func TestBroker_SendRespectsContextCancellation(t *testing.T) {
	broker := NewBroker()
	sender := &fakeSender{} // never responds

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	payload := &protocol.CommandPayload{Command: "docker_action", Args: map[string]any{"request_id": "req-4"}}
	_, err := broker.Send(ctx, sender, "agent-4", "req-4", payload, time.Second)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Send() error = %v, want context.Canceled", err)
	}
}

func TestBroker_UnmatchedResponseIsIgnored(t *testing.T) {
	broker := NewBroker()

	// A response for a request_id nobody is waiting on (e.g. a thread_dump
	// response, which uses its own poll flow) must not panic or block.
	broker.HandleResponse(&protocol.CommandResponsePayload{RequestID: "unknown-request"})
}

func TestBroker_PendingEntryIsCleanedUpAfterSend(t *testing.T) {
	broker := NewBroker()
	sender := &fakeSender{broker: broker}
	sender.respond = func(b *Broker, requestID string) {
		b.HandleResponse(&protocol.CommandResponsePayload{RequestID: requestID, OK: true})
	}

	payload := &protocol.CommandPayload{Command: "docker_action", Args: map[string]any{"request_id": "req-5"}}
	if _, err := broker.Send(context.Background(), sender, "agent-5", "req-5", payload, time.Second); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	broker.mu.Lock()
	_, stillPending := broker.pending["req-5"]
	broker.mu.Unlock()
	if stillPending {
		t.Error("Send() left a pending entry behind after completing")
	}
}
