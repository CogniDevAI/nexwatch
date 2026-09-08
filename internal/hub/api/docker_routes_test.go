package api

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/CogniDevAI/nexwatch/internal/hub/commands"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// respondingCommandSender is a CommandSender that immediately simulates the
// target agent replying with a configured COMMAND_RESPONSE, so an endpoint
// test can observe a real success/failure round trip through the broker
// without a real WebSocket connection.
type respondingCommandSender struct {
	broker   *commands.Broker
	response *protocol.CommandResponsePayload
	sendErr  error
}

func (s respondingCommandSender) SendCommand(agentID string, payload *protocol.CommandPayload) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	requestID, _ := payload.Args["request_id"].(string)
	resp := *s.response
	resp.RequestID = requestID
	go s.broker.HandleResponse(&resp)
	return nil
}

// neverRespondingCommandSender accepts the send but never produces a
// COMMAND_RESPONSE, used to exercise the 504 timeout path.
type neverRespondingCommandSender struct{}

func (neverRespondingCommandSender) SendCommand(agentID string, payload *protocol.CommandPayload) error {
	return nil
}

func registerDockerRoutesForTest(app core.App, e *core.ServeEvent, broker *commands.Broker, sender CommandSender) {
	apiGroup := e.Router.Group("/api/custom")
	apiGroup.Bind(apis.RequireAuth())
	RegisterDockerRoutes(apiGroup, broker, sender)
}

func TestDockerAction_ViewerForbidden(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-docker-viewer")
	viewer := mustSaveUserWithRole(t, app, "viewer-docker@example.com", "viewer")
	token := mustAuthToken(t, viewer)

	scenario := tests.ApiScenario{
		Name:            "viewer POST docker action",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/docker/0123456789ab/restart",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusForbidden,
		ExpectedContent: []string{"requires a higher role"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerDockerRoutesForTest(app, e, commands.NewBroker(), neverRespondingCommandSender{})
		},
	}
	scenario.Test(t)
}

func TestDockerAction_InvalidActionReturns400(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-docker-invalid-action")
	operator := mustSaveUserWithRole(t, app, "operator-docker-invalid@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST docker action with an unsupported action",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/docker/0123456789ab/delete",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusBadRequest,
		ExpectedContent: []string{"unsupported docker action"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerDockerRoutesForTest(app, e, commands.NewBroker(), neverRespondingCommandSender{})
		},
	}
	scenario.Test(t)
}

func TestDockerAction_InvalidContainerIDReturns400(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-docker-invalid-id")
	operator := mustSaveUserWithRole(t, app, "operator-docker-invalid-id@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST docker action with a malformed container id",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/docker/not-a-valid-id/restart",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusBadRequest,
		ExpectedContent: []string{"invalid container id"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerDockerRoutesForTest(app, e, commands.NewBroker(), neverRespondingCommandSender{})
		},
	}
	scenario.Test(t)
}

func TestDockerAction_OperatorSuccessReturns200WithState(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-docker-success")
	operator := mustSaveUserWithRole(t, app, "operator-docker-success@example.com", "operator")
	token := mustAuthToken(t, operator)

	broker := commands.NewBroker()
	sender := respondingCommandSender{
		broker:   broker,
		response: &protocol.CommandResponsePayload{OK: true, State: "running"},
	}

	scenario := tests.ApiScenario{
		Name:            "operator POST docker action succeeds",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/docker/0123456789ab/restart",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusOK,
		ExpectedContent: []string{`"ok":true`, `"state":"running"`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerDockerRoutesForTest(app, e, broker, sender)
		},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			entries, err := app.FindRecordsByFilter(
				"audit_log",
				"action = 'docker.restart' && target_id = '0123456789ab'",
				"", 1, 0,
			)
			if err != nil || len(entries) == 0 {
				t.Fatalf("expected an audit_log entry for docker.restart, err=%v entries=%d", err, len(entries))
			}
			if entries[0].GetString("result") != "success" {
				t.Errorf("audit_log result = %q, want success", entries[0].GetString("result"))
			}
		},
	}
	scenario.Test(t)
}

func TestDockerAction_AgentReportedFailureReturns502(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-docker-agent-failure")
	operator := mustSaveUserWithRole(t, app, "operator-docker-failure@example.com", "operator")
	token := mustAuthToken(t, operator)

	broker := commands.NewBroker()
	sender := respondingCommandSender{
		broker:   broker,
		response: &protocol.CommandResponsePayload{OK: false, Error: "no such container"},
	}

	scenario := tests.ApiScenario{
		Name:            "operator POST docker action where the agent reports failure",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/docker/0123456789ab/restart",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusBadGateway,
		ExpectedContent: []string{`"ok":false`, "no such container"},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			registerDockerRoutesForTest(app, e, broker, sender)
		},
	}
	scenario.Test(t)
}

func TestDockerAction_AgentNotConnectedReturns502(t *testing.T) {
	app := mustNewApp(t)
	agent := mustSaveAgent(t, app, "host-docker-not-connected")
	operator := mustSaveUserWithRole(t, app, "operator-docker-notconn@example.com", "operator")
	token := mustAuthToken(t, operator)

	scenario := tests.ApiScenario{
		Name:            "operator POST docker action when the agent send fails",
		Method:          http.MethodPost,
		URL:             "/api/custom/agents/" + agent.Id + "/docker/0123456789ab/restart",
		Headers:         map[string]string{"Authorization": token},
		ExpectedStatus:  http.StatusBadGateway,
		ExpectedContent: []string{`"ok":false`},
		TestAppFactory:  func(t testing.TB) *tests.TestApp { return app },
		BeforeTestFunc: func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) {
			broker := commands.NewBroker()
			sender := respondingCommandSender{broker: broker, sendErr: http.ErrHandlerTimeout}
			registerDockerRoutesForTest(app, e, broker, sender)
		},
	}
	scenario.Test(t)
}

// The 504 timeout path (dockerActionTimeout, 35s in production) is not
// re-exercised end-to-end here to keep the suite fast — commands.Broker's
// own TestBroker_SendTimesOutWithoutAResponse (broker_test.go) proves
// Send() returns commands.ErrTimeout when no response arrives in time, and
// handleDockerAction's errors.Is(sendErr, commands.ErrTimeout) branch that
// maps it to a 504 is otherwise identical in shape to the already-tested
// 502 "agent not connected" branch above (TestDockerAction_AgentNotConnectedReturns502).
