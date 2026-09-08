package checks

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// PingRunner abstracts invoking the system "ping" command so tests can
// inject a fake instead of shelling out to the real binary.
type PingRunner interface {
	// Ping reports nil when target responded within timeout, or a
	// descriptive error otherwise (an invalid target, a missing "ping"
	// binary, or the process itself reporting failure/no reply).
	Ping(target string, timeout time.Duration) error
}

// execPingRunner is the production PingRunner: it shells out to the
// platform's "ping" binary via exec.Command (argv-based, never a shell, so
// the target is never shell-interpreted regardless).
type execPingRunner struct{}

// validPingTarget matches a bare hostname or IPv4/IPv6 address: letters,
// digits, dots, colons, hyphens, and underscores only. exec.Command never
// invokes a shell, so this isn't strictly needed to prevent injection, but
// it rejects shell metacharacters and other malformed input defensively
// and produces a clearer error than an arbitrary "ping" failure would.
var validPingTarget = regexp.MustCompile(`^[A-Za-z0-9.:_-]+$`)

func validatePingTarget(target string) error {
	if target == "" || !validPingTarget.MatchString(target) {
		return fmt.Errorf("invalid ping target %q", target)
	}
	return nil
}

// Ping runs "ping -c 1" against target with a per-platform timeout flag
// (-t on macOS/BSD, -W on Linux — detected via runtime.GOOS), returning nil
// when the target replied within timeout.
func (execPingRunner) Ping(target string, timeout time.Duration) error {
	if err := validatePingTarget(target); err != nil {
		return err
	}

	timeoutSec := int(timeout.Round(time.Second) / time.Second)
	if timeoutSec < 1 {
		timeoutSec = 1
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("ping", "-c", "1", "-t", strconv.Itoa(timeoutSec), target)
	default: // linux and other unix-likes
		cmd = exec.Command("ping", "-c", "1", "-W", strconv.Itoa(timeoutSec), target)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return fmt.Errorf("ping command not found on this host")
		}
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("ping failed: %s", msg)
	}
	return nil
}

// execICMP performs one ICMP probe attempt via the scheduler's configured
// PingRunner (execPingRunner in production, a fake in tests).
func (s *Scheduler) execICMP(check *core.Record) probeResult {
	timeout := timeoutDuration(check)
	target := check.GetString("target")

	start := time.Now()
	err := s.pingRunner.Ping(target, timeout)
	latency := time.Since(start)
	if err != nil {
		return probeResult{Success: false, LatencyMs: msFloat(latency), Error: err.Error()}
	}
	return probeResult{Success: true, LatencyMs: msFloat(latency)}
}
