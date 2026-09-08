package logs

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/CogniDevAI/nexwatch/internal/agent/config"
)

// JournaldRunner starts the journald-reading command for a source and
// returns a ReadCloser over its output. It is a seam so tests can feed
// fixture journald JSON lines without a real journalctl binary or systemd
// journal present (this repo's own dev machines may have neither).
type JournaldRunner interface {
	Run(ctx context.Context, args []string) (io.ReadCloser, error)
}

// execJournaldRunner is the production JournaldRunner: it shells out to
// the real "journalctl" binary.
type execJournaldRunner struct{}

func (execJournaldRunner) Run(ctx context.Context, args []string) (io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &cmdReadCloser{ReadCloser: stdout, cmd: cmd}, nil
}

// cmdReadCloser closes the underlying pipe and waits for the process on
// Close, so a caller done reading also reaps the child process instead of
// leaking it as a zombie.
type cmdReadCloser struct {
	io.ReadCloser
	cmd *exec.Cmd
}

func (c *cmdReadCloser) Close() error {
	closeErr := c.ReadCloser.Close()
	_ = c.cmd.Wait() //nolint:errcheck // best-effort reap; ctx cancellation already killed the process
	return closeErr
}

// journaldArgs builds the "journalctl -f -o json --since now [-u unit
// ...] [-p priority]" argument list for source.
func journaldArgs(source config.LogSource) []string {
	args := []string{"-f", "-o", "json", "--since", "now"}
	for _, unit := range source.Units {
		if unit == "" {
			continue
		}
		args = append(args, "-u", unit)
	}
	if source.PriorityMax != "" {
		args = append(args, "-p", source.PriorityMax)
	}
	return args
}

// journaldLine is the subset of journalctl's per-line JSON export
// (`-o json`) this reader cares about. journald exports every field as a
// JSON string (even numeric-looking ones like PRIORITY and
// __REALTIME_TIMESTAMP), except MESSAGE, which can be a JSON array of
// byte values instead of a string when the message contains non-UTF8
// bytes — decodeMessage below handles both shapes.
type journaldLine struct {
	Message           json.RawMessage `json:"MESSAGE"`
	SystemdUnit       string          `json:"_SYSTEMD_UNIT"`
	Priority          string          `json:"PRIORITY"`
	SyslogIdentifier  string          `json:"SYSLOG_IDENTIFIER"`
	RealtimeTimestamp string          `json:"__REALTIME_TIMESTAMP"`
}

// decodeMessage extracts a plain string from journald's MESSAGE field,
// which is usually a JSON string but can be a JSON array of byte values
// for a message containing invalid UTF-8. Falls back to a placeholder
// rather than dropping the entry entirely.
func decodeMessage(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var bytesArr []byte
	if err := json.Unmarshal(raw, &bytesArr); err == nil {
		return string(bytesArr)
	}
	return "<unparseable message>"
}

// JournaldReader runs journalctl for one source and emits parsed Entry
// values as they arrive.
type JournaldReader struct {
	runner JournaldRunner
}

// NewJournaldReader creates a JournaldReader. A nil runner uses the real
// journalctl binary.
func NewJournaldReader(runner JournaldRunner) *JournaldReader {
	if runner == nil {
		runner = execJournaldRunner{}
	}
	return &JournaldReader{runner: runner}
}

// Run streams source's journal entries to emit until ctx is cancelled or
// the underlying command's output ends (e.g. journalctl exits because the
// journal was rotated out from under it). It returns nil on a clean
// ctx-cancellation stop and a non-nil error otherwise, so the caller
// (Manager) can decide whether to reconnect.
func (r *JournaldReader) Run(ctx context.Context, source config.LogSource, emit func(Entry)) error {
	stdout, err := r.runner.Run(ctx, journaldArgs(source))
	if err != nil {
		return err
	}
	defer stdout.Close() //nolint:errcheck // best-effort cleanup

	scanner := bufio.NewScanner(stdout)
	// journald JSON lines are usually short, but allow generous headroom
	// for a large MESSAGE field rather than silently truncating/erroring.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var jl journaldLine
		if err := json.Unmarshal(line, &jl); err != nil {
			log.Printf("[logs/journald] skipping unparsable line: %v", err)
			continue
		}

		unit := jl.SystemdUnit
		if unit == "" {
			unit = jl.SyslogIdentifier
		}

		entry := Entry{
			Ts:      parseJournaldTimestamp(jl.RealtimeTimestamp),
			Source:  "journald",
			Unit:    unit,
			Level:   mapSyslogPriority(jl.Priority),
			Message: decodeMessage(jl.Message),
		}
		if jl.SyslogIdentifier != "" {
			entry.Fields = map[string]string{"syslog_identifier": jl.SyslogIdentifier}
		}
		emit(entry)
	}

	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// parseJournaldTimestamp converts journald's __REALTIME_TIMESTAMP
// (microseconds since the Unix epoch, as a decimal string) into unix
// milliseconds, falling back to the current time when the field is
// missing or unparsable.
func parseJournaldTimestamp(raw string) int64 {
	micros, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return time.Now().UnixMilli()
	}
	return micros / 1000
}
