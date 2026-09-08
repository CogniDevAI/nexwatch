package command

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/docker/docker/api/types/container"
)

// DockerClient is the subset of the Docker SDK client needed to perform a
// container lifecycle action and read back its resulting state. Declared
// here (rather than importing *client.Client directly into DockerAction)
// so tests can inject a fake instead of depending on a real Docker daemon.
type DockerClient interface {
	ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerInspect(ctx context.Context, containerID string) (container.InspectResponse, error)
}

// allowedDockerActions is the strict allowlist of lifecycle actions the
// agent will perform. Anything else is rejected before it ever reaches the
// Docker client, regardless of what the hub sends.
var allowedDockerActions = map[string]bool{
	"start":   true,
	"stop":    true,
	"restart": true,
}

// containerIDPattern matches a Docker container id: 12 (short) to 64
// (full) hex characters. Rejecting anything else prevents a malformed or
// unexpected value from the hub reaching the Docker daemon at all.
var containerIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{12,64}$`)

// dockerActionTimeout bounds how long a single start/stop/restart (plus
// its follow-up inspect) is allowed to take.
const dockerActionTimeout = 30 * time.Second

// DockerActionResult is the outcome of a DockerAction call.
type DockerActionResult struct {
	OK    bool
	State string
	Error string
}

// DockerAction performs an allowlisted lifecycle action (start/stop/
// restart) against containerID using cli, returning the container's state
// afterward. Validation (action allowlist, container id shape) happens
// before any call to cli, so an invalid request from the hub never touches
// the Docker daemon.
func DockerAction(ctx context.Context, cli DockerClient, containerID, action string) DockerActionResult {
	if !allowedDockerActions[action] {
		return DockerActionResult{Error: fmt.Sprintf("unsupported docker action: %q", action)}
	}
	if !containerIDPattern.MatchString(containerID) {
		return DockerActionResult{Error: "invalid container id"}
	}

	actionCtx, cancel := context.WithTimeout(ctx, dockerActionTimeout)
	defer cancel()

	var err error
	switch action {
	case "start":
		err = cli.ContainerStart(actionCtx, containerID, container.StartOptions{})
	case "stop":
		err = cli.ContainerStop(actionCtx, containerID, container.StopOptions{})
	case "restart":
		err = cli.ContainerRestart(actionCtx, containerID, container.StopOptions{})
	}
	if err != nil {
		return DockerActionResult{Error: fmt.Sprintf("%s failed: %s", action, err.Error())}
	}

	info, err := cli.ContainerInspect(actionCtx, containerID)
	if err != nil || info.ContainerJSONBase == nil || info.State == nil {
		// The action itself succeeded even though the follow-up inspect
		// failed (or returned an incomplete response) — report success
		// with an unknown state rather than masking it as a failure.
		return DockerActionResult{OK: true, State: "unknown"}
	}

	return DockerActionResult{OK: true, State: string(info.State.Status)}
}
