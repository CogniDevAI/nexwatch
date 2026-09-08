package logs

import "testing"

func TestDetectLevel(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{name: "explicit ERROR uppercase", line: "2024-01-01 ERROR: connection refused", want: "error"},
		{name: "explicit error lowercase", line: "error opening file", want: "error"},
		{name: "fatal maps to error", line: "fatal: could not read config", want: "error"},
		{name: "panic maps to error", line: "panic: runtime error", want: "error"},
		{name: "warn token", line: "warn: disk usage high", want: "warning"},
		{name: "warning token", line: "WARNING: retrying request", want: "warning"},
		{name: "info token", line: "info: server started", want: "info"},
		{name: "notice maps to info", line: "notice: config reloaded", want: "info"},
		{name: "debug token", line: "debug: entering handler", want: "debug"},
		{name: "trace maps to debug", line: "trace: step 1 complete", want: "debug"},
		{name: "no token defaults to info", line: "server listening on :8080", want: "info"},
		{name: "empty line defaults to info", line: "", want: "info"},
		{name: "error takes precedence over info", line: "info: retrying after error from upstream", want: "error"},
		{name: "error takes precedence over warning", line: "warning escalated to error", want: "error"},
		{name: "does not match substring inside another word", line: "informational message about warnings module", want: "info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectLevel(tt.line); got != tt.want {
				t.Errorf("DetectLevel(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestMapSyslogPriority(t *testing.T) {
	tests := []struct {
		priority string
		want     string
	}{
		{"0", "error"},
		{"1", "error"},
		{"2", "error"},
		{"3", "error"},
		{"4", "warning"},
		{"5", "info"},
		{"6", "info"},
		{"7", "debug"},
		{"", "info"},
		{"garbage", "info"},
	}

	for _, tt := range tests {
		t.Run("priority_"+tt.priority, func(t *testing.T) {
			if got := mapSyslogPriority(tt.priority); got != tt.want {
				t.Errorf("mapSyslogPriority(%q) = %q, want %q", tt.priority, got, tt.want)
			}
		})
	}
}
