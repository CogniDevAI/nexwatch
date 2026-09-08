package threaddump

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// Service handles thread dump requests and storage.
type Service struct {
	app core.App
}

// NewService creates a new thread dump service.
func NewService(app core.App) *Service {
	return &Service{app: app}
}

// RequestDump sends a thread_dump command to the agent and creates a pending record.
// requestedBy identifies the authenticated caller (record id, optionally with
// email) and is stored alongside the dump for auditing. Returns the
// request_id so the caller can poll for the result.
func (s *Service) RequestDump(agentID string, pid int, processName string, requestedBy string, sender func(agentID string, payload *protocol.CommandPayload) error) (string, error) {
	requestID := fmt.Sprintf("td-%d-%d", time.Now().UnixMilli(), pid)

	// Create a pending record in the DB.
	col, err := s.app.FindCollectionByNameOrId("thread_dumps")
	if err != nil {
		return "", fmt.Errorf("thread_dumps collection not found: %w", err)
	}

	record := core.NewRecord(col)
	record.Set("agent_id", agentID)
	record.Set("pid", pid)
	record.Set("process_name", processName)
	record.Set("request_id", requestID)
	record.Set("requested_by", requestedBy)
	record.Set("status", "pending")
	record.Set("taken_at", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))

	if err := s.app.Save(record); err != nil {
		return "", fmt.Errorf("failed to create thread dump record: %w", err)
	}

	// Send command to agent.
	payload := &protocol.CommandPayload{
		Command: "thread_dump",
		Args: map[string]any{
			"pid":          pid,
			"request_id":   requestID,
			"process_name": processName,
		},
	}

	if err := sender(agentID, payload); err != nil {
		// Mark as error immediately if agent not connected.
		record.Set("status", "error")
		record.Set("error", err.Error())
		_ = s.app.Save(record)
		return "", fmt.Errorf("agent not reachable: %w", err)
	}

	return requestID, nil
}

// HandleResponse processes a COMMAND_RESPONSE for a thread_dump and updates the DB record.
func (s *Service) HandleResponse(app core.App, payload *protocol.CommandResponsePayload) {
	if payload.Command != "thread_dump" {
		return
	}

	records, err := app.FindRecordsByFilter(
		"thread_dumps",
		"request_id = {:req}",
		"-taken_at", 1, 0,
		map[string]any{"req": payload.RequestID},
	)
	if err != nil || len(records) == 0 {
		slog.Warn("thread dump response for unknown request", "request_id", payload.RequestID)
		return
	}

	record := records[0]
	if payload.Error != "" {
		record.Set("status", "error")
		record.Set("error", payload.Error)
	} else {
		record.Set("status", "success")
		record.Set("output", payload.Output)
	}

	if err := app.Save(record); err != nil {
		slog.Error("failed to save thread dump response", "request_id", payload.RequestID, "error", err)
	}
}
