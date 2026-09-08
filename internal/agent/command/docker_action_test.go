package command

import (
	"context"
	"errors"
	"testing"

	"github.com/docker/docker/api/types/container"
)

// fakeDockerClient is a DockerClient test double: each method records that
// it was called and returns whatever the test configured, so DockerAction
// can be exercised without a real Docker daemon.
type fakeDockerClient struct {
	startErr    error
	stopErr     error
	restartErr  error
	inspectResp container.InspectResponse
	inspectErr  error

	startCalled   bool
	stopCalled    bool
	restartCalled bool
}

func (f *fakeDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	f.startCalled = true
	return f.startErr
}

func (f *fakeDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	f.stopCalled = true
	return f.stopErr
}

func (f *fakeDockerClient) ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error {
	f.restartCalled = true
	return f.restartErr
}

func (f *fakeDockerClient) ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error) {
	return f.inspectResp, f.inspectErr
}

func inspectWithState(status container.ContainerState) container.InspectResponse {
	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{
			State: &container.State{Status: status},
		},
	}
}

const validContainerID = "0123456789ab" // 12 hex chars

func TestDockerAction_RejectsUnknownAction(t *testing.T) {
	cli := &fakeDockerClient{}
	result := DockerAction(context.Background(), cli, validContainerID, "delete")

	if result.OK {
		t.Fatal("DockerAction() with an unlisted action returned OK=true, want false")
	}
	if result.Error == "" {
		t.Error("DockerAction() with an unlisted action returned an empty Error")
	}
	if cli.startCalled || cli.stopCalled || cli.restartCalled {
		t.Error("DockerAction() with an unlisted action must never call the Docker client")
	}
}

func TestDockerAction_RejectsInvalidContainerID(t *testing.T) {
	tests := []struct {
		name        string
		containerID string
	}{
		{"too short", "abc123"},
		{"non-hex characters", "zzzzzzzzzzzz"},
		{"empty", ""},
		{"shell metacharacters", "abc123; rm -rf /"},
		{"too long", "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cli := &fakeDockerClient{}
			result := DockerAction(context.Background(), cli, tt.containerID, "start")

			if result.OK {
				t.Fatalf("DockerAction(%q) returned OK=true, want false", tt.containerID)
			}
			if result.Error == "" {
				t.Errorf("DockerAction(%q) returned an empty Error", tt.containerID)
			}
			if cli.startCalled {
				t.Errorf("DockerAction(%q) must never reach the Docker client", tt.containerID)
			}
		})
	}
}

func TestDockerAction_AcceptsFullLengthContainerID(t *testing.T) {
	fullID := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd" // 64 hex chars
	cli := &fakeDockerClient{inspectResp: inspectWithState("running")}

	result := DockerAction(context.Background(), cli, fullID, "start")

	if !result.OK {
		t.Fatalf("DockerAction() with a full-length id = %+v, want OK=true", result)
	}
	if !cli.startCalled {
		t.Error("DockerAction() with a valid full-length id did not call ContainerStart")
	}
}

func TestDockerAction_StartSuccess(t *testing.T) {
	cli := &fakeDockerClient{inspectResp: inspectWithState("running")}

	result := DockerAction(context.Background(), cli, validContainerID, "start")

	if !result.OK {
		t.Fatalf("DockerAction(start) = %+v, want OK=true", result)
	}
	if result.State != "running" {
		t.Errorf("DockerAction(start).State = %q, want %q", result.State, "running")
	}
	if !cli.startCalled {
		t.Error("DockerAction(start) did not call ContainerStart")
	}
}

func TestDockerAction_StopSuccess(t *testing.T) {
	cli := &fakeDockerClient{inspectResp: inspectWithState("exited")}

	result := DockerAction(context.Background(), cli, validContainerID, "stop")

	if !result.OK {
		t.Fatalf("DockerAction(stop) = %+v, want OK=true", result)
	}
	if result.State != "exited" {
		t.Errorf("DockerAction(stop).State = %q, want %q", result.State, "exited")
	}
	if !cli.stopCalled {
		t.Error("DockerAction(stop) did not call ContainerStop")
	}
}

func TestDockerAction_RestartSuccess(t *testing.T) {
	cli := &fakeDockerClient{inspectResp: inspectWithState("running")}

	result := DockerAction(context.Background(), cli, validContainerID, "restart")

	if !result.OK {
		t.Fatalf("DockerAction(restart) = %+v, want OK=true", result)
	}
	if !cli.restartCalled {
		t.Error("DockerAction(restart) did not call ContainerRestart")
	}
}

func TestDockerAction_ClientErrorSurfacesAsFailure(t *testing.T) {
	cli := &fakeDockerClient{startErr: errors.New("no such container")}

	result := DockerAction(context.Background(), cli, validContainerID, "start")

	if result.OK {
		t.Fatal("DockerAction() with a client error returned OK=true, want false")
	}
	if result.Error == "" {
		t.Error("DockerAction() with a client error returned an empty Error")
	}
}

func TestDockerAction_InspectFailureAfterSuccessfulActionStillReportsOK(t *testing.T) {
	cli := &fakeDockerClient{inspectErr: errors.New("inspect unavailable")}

	result := DockerAction(context.Background(), cli, validContainerID, "start")

	if !result.OK {
		t.Fatalf("DockerAction() = %+v, want OK=true even when the follow-up inspect fails", result)
	}
	if result.State != "unknown" {
		t.Errorf("DockerAction().State = %q, want %q when inspect fails", result.State, "unknown")
	}
}

func TestDockerAction_IncompleteInspectResponseStillReportsOK(t *testing.T) {
	cli := &fakeDockerClient{inspectResp: container.InspectResponse{}} // no ContainerJSONBase/State

	result := DockerAction(context.Background(), cli, validContainerID, "start")

	if !result.OK {
		t.Fatalf("DockerAction() = %+v, want OK=true even when inspect returns an incomplete response", result)
	}
	if result.State != "unknown" {
		t.Errorf("DockerAction().State = %q, want %q for an incomplete inspect response", result.State, "unknown")
	}
}
