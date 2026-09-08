package update

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// "agents" collection with the update_status/update_error fields this
	// package depends on.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

func mustNewTestApp(t testing.TB) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

func mustSaveAgent(t testing.TB, app core.App, hostname string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("agents")
	if err != nil {
		t.Fatalf("find agents collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("hostname", hostname)
	rec.Set("status", "online")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save agent: %v", err)
	}
	return rec
}

func TestHandleResponse_IgnoresNonUpdateCommands(t *testing.T) {
	app := mustNewTestApp(t)
	agent := mustSaveAgent(t, app, "host-ignore")

	svc := NewService()
	svc.HandleResponse(app, &protocol.CommandResponsePayload{
		Command: "docker_action",
		AgentID: agent.Id,
		Stage:   "done",
	})

	reloaded, err := app.FindRecordById("agents", agent.Id)
	if err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	if reloaded.GetString("update_status") != "" {
		t.Errorf("update_status = %q, want empty (response for a different command must be ignored)", reloaded.GetString("update_status"))
	}
}

func TestHandleResponse_ProgressStagesUpdateStatus(t *testing.T) {
	app := mustNewTestApp(t)
	agent := mustSaveAgent(t, app, "host-progress")

	svc := NewService()
	for _, stage := range []string{"started", "downloading", "verifying", "installing", "restarting"} {
		svc.HandleResponse(app, &protocol.CommandResponsePayload{
			Command:   "update",
			AgentID:   agent.Id,
			RequestID: "req-1",
			OK:        true,
			Stage:     stage,
		})

		reloaded, err := app.FindRecordById("agents", agent.Id)
		if err != nil {
			t.Fatalf("reload agent: %v", err)
		}
		if got := reloaded.GetString("update_status"); got != stage {
			t.Fatalf("after stage %q: update_status = %q, want %q", stage, got, stage)
		}
	}
}

func TestHandleResponse_FailedStageSetsError(t *testing.T) {
	app := mustNewTestApp(t)
	agent := mustSaveAgent(t, app, "host-failed")

	svc := NewService()
	svc.HandleResponse(app, &protocol.CommandResponsePayload{
		Command:   "update",
		AgentID:   agent.Id,
		RequestID: "req-2",
		OK:        false,
		Stage:     "failed",
		Error:     "checksum mismatch",
	})

	reloaded, err := app.FindRecordById("agents", agent.Id)
	if err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	if got := reloaded.GetString("update_status"); got != "failed" {
		t.Errorf("update_status = %q, want failed", got)
	}
	if got := reloaded.GetString("update_error"); got != "checksum mismatch" {
		t.Errorf("update_error = %q, want %q", got, "checksum mismatch")
	}
}

func TestHandleResponse_DoneStageClearsPriorError(t *testing.T) {
	app := mustNewTestApp(t)
	agent := mustSaveAgent(t, app, "host-done")
	agent.Set("update_error", "a previous failure")
	if err := app.Save(agent); err != nil {
		t.Fatalf("seed prior error: %v", err)
	}

	svc := NewService()
	svc.HandleResponse(app, &protocol.CommandResponsePayload{
		Command:   "update",
		AgentID:   agent.Id,
		RequestID: "req-3",
		OK:        true,
		Stage:     "done",
	})

	reloaded, err := app.FindRecordById("agents", agent.Id)
	if err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	if got := reloaded.GetString("update_status"); got != "done" {
		t.Errorf("update_status = %q, want done", got)
	}
	if got := reloaded.GetString("update_error"); got != "" {
		t.Errorf("update_error = %q, want cleared on success", got)
	}
}

func TestHandleResponse_RestartRequiredStageClearsPriorErrorLikeDone(t *testing.T) {
	app := mustNewTestApp(t)
	agent := mustSaveAgent(t, app, "host-restart-required")
	agent.Set("update_error", "a previous failure")
	if err := app.Save(agent); err != nil {
		t.Fatalf("seed prior error: %v", err)
	}

	svc := NewService()
	svc.HandleResponse(app, &protocol.CommandResponsePayload{
		Command:   "update",
		AgentID:   agent.Id,
		RequestID: "req-restart-required",
		OK:        true,
		Stage:     "restart_required",
	})

	reloaded, err := app.FindRecordById("agents", agent.Id)
	if err != nil {
		t.Fatalf("reload agent: %v", err)
	}
	if got := reloaded.GetString("update_status"); got != "restart_required" {
		t.Errorf("update_status = %q, want restart_required", got)
	}
	if got := reloaded.GetString("update_error"); got != "" {
		t.Errorf("update_error = %q, want cleared (the install itself succeeded)", got)
	}
}

func TestHandleResponse_UnknownAgentIsIgnored(t *testing.T) {
	app := mustNewTestApp(t)
	svc := NewService()
	// Must not panic or error out — just silently ignored.
	svc.HandleResponse(app, &protocol.CommandResponsePayload{
		Command: "update",
		AgentID: "does-not-exist",
		Stage:   "done",
	})
}

func TestHandleResponse_DefaultsStageWhenMissing(t *testing.T) {
	app := mustNewTestApp(t)

	t.Run("ok defaults to done", func(t *testing.T) {
		agent := mustSaveAgent(t, app, "host-default-ok")
		NewService().HandleResponse(app, &protocol.CommandResponsePayload{
			Command: "update",
			AgentID: agent.Id,
			OK:      true,
		})
		reloaded, err := app.FindRecordById("agents", agent.Id)
		if err != nil {
			t.Fatalf("reload agent: %v", err)
		}
		if got := reloaded.GetString("update_status"); got != "done" {
			t.Errorf("update_status = %q, want done", got)
		}
	})

	t.Run("not ok defaults to failed", func(t *testing.T) {
		agent := mustSaveAgent(t, app, "host-default-failed")
		NewService().HandleResponse(app, &protocol.CommandResponsePayload{
			Command: "update",
			AgentID: agent.Id,
			OK:      false,
			Error:   "boom",
		})
		reloaded, err := app.FindRecordById("agents", agent.Id)
		if err != nil {
			t.Fatalf("reload agent: %v", err)
		}
		if got := reloaded.GetString("update_status"); got != "failed" {
			t.Errorf("update_status = %q, want failed", got)
		}
		if got := reloaded.GetString("update_error"); got != "boom" {
			t.Errorf("update_error = %q, want boom", got)
		}
	})
}
