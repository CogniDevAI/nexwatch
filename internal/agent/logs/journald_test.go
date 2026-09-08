package logs

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/CogniDevAI/nexwatch/internal/agent/config"
)

// fakeJournaldRunner returns a fixed byte stream instead of shelling out to
// a real journalctl binary — this dev machine (and CI) may have no
// systemd journal at all.
type fakeJournaldRunner struct {
	output    string
	gotArgs   []string
	returnErr error
}

func (f *fakeJournaldRunner) Run(ctx context.Context, args []string) (io.ReadCloser, error) {
	f.gotArgs = args
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	return io.NopCloser(strings.NewReader(f.output)), nil
}

func TestJournaldReader_ParsesEntries(t *testing.T) {
	fixture := `{"MESSAGE":"connection refused","_SYSTEMD_UNIT":"nginx.service","PRIORITY":"3","SYSLOG_IDENTIFIER":"nginx","__REALTIME_TIMESTAMP":"1700000000000000"}
{"MESSAGE":"server started","_SYSTEMD_UNIT":"","PRIORITY":"6","SYSLOG_IDENTIFIER":"myapp","__REALTIME_TIMESTAMP":"1700000001000000"}
`
	runner := &fakeJournaldRunner{output: fixture}
	reader := NewJournaldReader(runner)

	var got []Entry
	err := reader.Run(context.Background(), config.LogSource{Type: "journald"}, func(e Entry) {
		got = append(got, e)
	})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}

	if got[0].Message != "connection refused" {
		t.Errorf("entries[0].Message = %q, want %q", got[0].Message, "connection refused")
	}
	if got[0].Unit != "nginx.service" {
		t.Errorf("entries[0].Unit = %q, want %q", got[0].Unit, "nginx.service")
	}
	if got[0].Level != "error" {
		t.Errorf("entries[0].Level = %q, want %q (priority 3)", got[0].Level, "error")
	}
	if got[0].Ts != 1700000000000 {
		t.Errorf("entries[0].Ts = %d, want %d", got[0].Ts, 1700000000000)
	}
	if got[0].Source != "journald" {
		t.Errorf("entries[0].Source = %q, want %q", got[0].Source, "journald")
	}

	// Second line has no _SYSTEMD_UNIT — falls back to SYSLOG_IDENTIFIER.
	if got[1].Unit != "myapp" {
		t.Errorf("entries[1].Unit = %q, want %q (fallback to syslog identifier)", got[1].Unit, "myapp")
	}
	if got[1].Level != "info" {
		t.Errorf("entries[1].Level = %q, want %q (priority 6)", got[1].Level, "info")
	}
}

func TestJournaldReader_SkipsUnparsableLines(t *testing.T) {
	fixture := "not json at all\n{\"MESSAGE\":\"ok line\",\"PRIORITY\":\"6\",\"__REALTIME_TIMESTAMP\":\"1700000000000000\"}\n"
	runner := &fakeJournaldRunner{output: fixture}
	reader := NewJournaldReader(runner)

	var got []Entry
	err := reader.Run(context.Background(), config.LogSource{Type: "journald"}, func(e Entry) {
		got = append(got, e)
	})
	if err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1 (bad line skipped)", len(got))
	}
	if got[0].Message != "ok line" {
		t.Errorf("entries[0].Message = %q, want %q", got[0].Message, "ok line")
	}
}

func TestJournaldArgs(t *testing.T) {
	tests := []struct {
		name   string
		source config.LogSource
		want   []string
	}{
		{
			name:   "no units, no priority",
			source: config.LogSource{Type: "journald"},
			want:   []string{"-f", "-o", "json", "--since", "now"},
		},
		{
			name:   "with units and priority",
			source: config.LogSource{Type: "journald", Units: []string{"nginx.service", "app.service"}, PriorityMax: "warning"},
			want:   []string{"-f", "-o", "json", "--since", "now", "-u", "nginx.service", "-u", "app.service", "-p", "warning"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := journaldArgs(tt.source)
			if len(got) != len(tt.want) {
				t.Fatalf("journaldArgs() = %v, want %v", got, tt.want)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("journaldArgs()[%d] = %q, want %q (full: %v)", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestJournaldReader_PropagatesRunnerError(t *testing.T) {
	runner := &fakeJournaldRunner{returnErr: io.ErrUnexpectedEOF}
	reader := NewJournaldReader(runner)

	err := reader.Run(context.Background(), config.LogSource{Type: "journald"}, func(Entry) {})
	if err == nil {
		t.Fatal("Run() expected error from runner, got nil")
	}
}
