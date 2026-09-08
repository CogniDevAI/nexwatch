package api

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/CogniDevAI/nexwatch/internal/hub/audit"
	"github.com/CogniDevAI/nexwatch/internal/hub/commands"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// allowedDockerActions is the strict allowlist of lifecycle actions this
// endpoint will forward to an agent. It mirrors (and is independently
// enforced from) the agent-side allowlist in
// internal/agent/command/docker_action.go — defense in depth: a malformed
// or unexpected action is rejected at the hub before a COMMAND is even
// sent, not just at the agent.
var allowedDockerActions = map[string]bool{
	"start":   true,
	"stop":    true,
	"restart": true,
}

// dockerContainerIDPattern matches a Docker container id: 12 (short) to 64
// (full) hex characters.
var dockerContainerIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{12,64}$`)

// dockerActionTimeout bounds how long the hub waits for the agent's
// COMMAND_RESPONSE before giving up and returning 504 to the caller.
const dockerActionTimeout = 35 * time.Second

// RegisterDockerRoutes registers the Docker container action endpoint on
// apiGroup, which the caller must already have bound with the desired auth
// middleware and mounted at the "/api/custom" prefix. It returns every
// route it registered, for openapi_test.go to cross-check against
// docs/openapi.yaml.
//
// This is intentionally a separate route file/broker (internal/hub/commands)
// rather than folded into threaddump.Service: a thread dump is a
// fire-and-forget request the UI polls for a result (its own history
// table, potentially large output) while a docker action is a small,
// synchronous "did it work" round trip better suited to a generic
// request/response broker. Migrating thread-dump's own pending-map to the
// same broker was considered but is not a small change — thread-dump
// intentionally returns 202 immediately and persists a polling record,
// which the broker's block-until-response model does not fit — so it was
// left as-is per the task's own guidance.
func RegisterDockerRoutes(apiGroup *router.RouterGroup[*core.RequestEvent], broker *commands.Broker, cmdSender CommandSender) []RegisteredRoute {
	rec := newRouteRecorder(apiGroup, "/api/custom")

	// POST /api/custom/agents/{id}/docker/{containerId}/{action} — start,
	// stop, or restart a container on the given agent, waiting
	// synchronously for the agent's result.
	rec.POST("/agents/{id}/docker/{containerId}/{action}", func(e *core.RequestEvent) error {
		return handleDockerAction(e, broker, cmdSender)
	}).Bind(RequireRole(RoleOperator))

	return rec.Registered
}

// handleDockerAction validates the requested action and container id,
// dispatches a docker_action COMMAND to the agent via broker, and returns
// its result. Every outcome (success or failure) is recorded to the audit
// log, since a Docker lifecycle action is exactly the kind of
// operator-triggered state change the audit trail exists for.
func handleDockerAction(e *core.RequestEvent, broker *commands.Broker, sender CommandSender) error {
	agentID := e.Request.PathValue("id")
	containerID := e.Request.PathValue("containerId")
	action := e.Request.PathValue("action")

	if !allowedDockerActions[action] {
		return e.JSON(http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("unsupported docker action: %q", action),
		})
	}
	if !dockerContainerIDPattern.MatchString(containerID) {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "invalid container id"})
	}

	// Best-effort hostname lookup for the audit trail only — a bogus
	// agentID simply leaves this empty and falls through to the broker,
	// which reports "agent not reachable" the same way an unconnected but
	// otherwise valid agent id would.
	var hostname string
	if agent, err := e.App.FindRecordById("agents", agentID); err == nil {
		hostname = agent.GetString("hostname")
	}

	requestID := fmt.Sprintf("da-%d-%s", time.Now().UnixMilli(), containerID[:min(12, len(containerID))])
	args := protocol.DockerActionPayload{RequestID: requestID, ContainerID: containerID, Action: action}
	payload := &protocol.CommandPayload{Command: "docker_action", Args: args.ToArgs()}

	resp, sendErr := broker.Send(e.Request.Context(), sender, agentID, requestID, payload, dockerActionTimeout)

	var (
		status     int
		body       = map[string]any{}
		result     = "success"
		auditError string
	)

	switch {
	case errors.Is(sendErr, commands.ErrTimeout):
		status = http.StatusGatewayTimeout
		result = "failure"
		auditError = "timed out waiting for the agent to respond"
		body["ok"] = false
		body["error"] = auditError

	case sendErr != nil:
		status = http.StatusBadGateway
		result = "failure"
		auditError = sendErr.Error()
		body["ok"] = false
		body["error"] = auditError

	case !resp.OK:
		status = http.StatusBadGateway
		result = "failure"
		auditError = resp.Error
		body["ok"] = false
		body["error"] = resp.Error
		if resp.State != "" {
			body["state"] = resp.State
		}

	default:
		status = http.StatusOK
		body["ok"] = true
		body["state"] = resp.State
	}

	audit.Record(e.App, e, audit.Entry{
		Action:     "docker." + action,
		TargetType: "docker_container",
		TargetID:   containerID,
		AgentID:    agentID,
		Details:    map[string]any{"hostname": hostname, "error": auditError},
		Result:     result,
	})

	return e.JSON(status, body)
}
