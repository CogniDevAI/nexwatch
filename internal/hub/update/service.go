// Package update reduces an agent's "update" COMMAND_RESPONSE progress
// stream into the agents record's update_status/update_error fields — the
// hub-side half of F9 (agent self-update). It is deliberately separate
// from internal/hub/commands.Broker: Broker only resolves the single
// synchronous "started" response an HTTP handler blocks on (see
// internal/hub/api/update_routes.go), while every subsequent
// downloading/verifying/installing/restarting/failed/done response for the
// same request id arrives after Broker has already stopped waiting on it —
// this Service is what keeps consuming those.
package update

import (
	"log/slog"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// Service is stateless — HandleResponse reads and writes the agents
// collection directly through the *core.App it's given, the same way
// internal/hub/threaddump.Service.HandleResponse does.
type Service struct{}

// NewService returns a ready-to-use Service.
func NewService() *Service {
	return &Service{}
}

// HandleResponse updates the target agent's update_status/update_error
// fields from one "update" COMMAND_RESPONSE. It ignores every response for
// any other command (docker_action, thread_dump), and silently ignores a
// response for an agent id it can't find — the agent may have been deleted
// mid-update, which is not this Service's concern to report.
func (s *Service) HandleResponse(app core.App, payload *protocol.CommandResponsePayload) {
	if payload.Command != "update" || payload.AgentID == "" {
		return
	}

	record, err := app.FindRecordById("agents", payload.AgentID)
	if err != nil {
		slog.Warn("update response for unknown agent", "agent_id", payload.AgentID, "request_id", payload.RequestID)
		return
	}

	stage := payload.Stage
	if stage == "" {
		// Defensive default for a response that somehow omits Stage —
		// every agent build wired for F9 always sets it.
		if payload.OK {
			stage = "done"
		} else {
			stage = "failed"
		}
	}

	record.Set("update_status", stage)
	switch stage {
	case "failed":
		record.Set("update_error", payload.Error)
	case "done", "restart_required":
		// restart_required means the download/verify/install itself
		// succeeded — only the binary swap is deferred to the next host
		// restart (Windows-only, see update_status_restart_required.go) —
		// so it clears any prior error the same way a normal "done" does.
		record.Set("update_error", "")
	}

	if err := app.Save(record); err != nil {
		slog.Error("failed to save agent update status", "agent_id", payload.AgentID, "request_id", payload.RequestID, "error", err)
	}
}
