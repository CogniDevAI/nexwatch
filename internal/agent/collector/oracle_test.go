package collector

import (
	"reflect"
	"testing"
)

func TestSplitRow(t *testing.T) {
	tests := []struct {
		name     string
		row      string
		expected int
		want     []string
	}{
		{"exact field count", "a|b|c", 3, []string{"a", "b", "c"}},
		{"trims whitespace around fields", " a | b |c ", 3, []string{"a", "b", "c"}},
		{"pads short rows with empty strings", "a|b", 4, []string{"a", "b", "", ""}},
		{"empty row padded fully", "", 2, []string{"", ""}},
		{"extra fields are kept, not truncated", "a|b|c|d", 2, []string{"a", "b", "c", "d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitRow(tt.row, tt.expected)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitRow(%q, %d) = %v, want %v", tt.row, tt.expected, got, tt.want)
			}
		})
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int
	}{
		{"plain integer", "42", 42},
		{"leading/trailing whitespace", "  42  ", 42},
		{"negative number", "-7", -7},
		{"empty string defaults to zero", "", 0},
		{"non-numeric defaults to zero", "abc", 0},
		{"zero", "0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toInt(tt.in); got != tt.want {
				t.Errorf("toInt(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestToFloat(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want float64
	}{
		{"plain float", "42.5", 42.5},
		{"integer-looking value", "42", 42.0},
		{"leading/trailing whitespace", "  3.14  ", 3.14},
		{"negative float", "-1.5", -1.5},
		{"empty string defaults to zero", "", 0},
		{"non-numeric defaults to zero", "N/A", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toFloat(tt.in); got != tt.want {
				t.Errorf("toFloat(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeKey(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercases", "TOTAL PGA INUSE", "total_pga_inuse"},
		{"replaces spaces with underscores", "buffer cache", "buffer_cache"},
		{"trims whitespace before sanitizing", "  shared pool  ", "shared_pool"},
		{"already sanitized stays unchanged", "log_buffer", "log_buffer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeKey(tt.in); got != tt.want {
				t.Errorf("sanitizeKey(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseOutput_InstanceAndSessions(t *testing.T) {
	raw := `
SECTION:instance
PRODDB|OPEN|ACTIVE|dbhost01|2026-01-01 00:00:00|19.0.0.0.0

SECTION:sessions
42|10|32|5|1
`
	result := parseOutput(raw)

	instance, ok := result["instance"].(map[string]any)
	if !ok {
		t.Fatalf("result[instance] = %v (%T), want map[string]any", result["instance"], result["instance"])
	}
	if instance["name"] != "PRODDB" {
		t.Errorf("instance.name = %v, want PRODDB", instance["name"])
	}
	if instance["status"] != "OPEN" {
		t.Errorf("instance.status = %v, want OPEN", instance["status"])
	}
	if instance["host"] != "dbhost01" {
		t.Errorf("instance.host = %v, want dbhost01", instance["host"])
	}

	sessions, ok := result["sessions"].(map[string]any)
	if !ok {
		t.Fatalf("result[sessions] = %v (%T), want map[string]any", result["sessions"], result["sessions"])
	}
	if sessions["total"] != 42 {
		t.Errorf("sessions.total = %v, want 42", sessions["total"])
	}
	if sessions["active"] != 10 {
		t.Errorf("sessions.active = %v, want 10", sessions["active"])
	}
	if sessions["blocked"] != 1 {
		t.Errorf("sessions.blocked = %v, want 1", sessions["blocked"])
	}
}

func TestParseOutput_BlockedSessionsAndTopSQL(t *testing.T) {
	raw := `
SECTION:blocked_sessions
101|5001|appuser|ACTIVE|202|Application|enq: TX - row lock contention|30|SELECT * FROM orders WHERE id = :1

SECTION:top_sql
abc123xyz|1000|12.5|0.0125|8.2|500000|1200|SELECT * FROM big_table
`
	result := parseOutput(raw)

	blocked, ok := result["blocked_sessions"].([]map[string]any)
	if !ok || len(blocked) != 1 {
		t.Fatalf("result[blocked_sessions] = %v, want a single-element slice", result["blocked_sessions"])
	}
	if blocked[0]["sid"] != 101 {
		t.Errorf("blocked_sessions[0].sid = %v, want 101", blocked[0]["sid"])
	}
	if blocked[0]["blocking_sid"] != 202 {
		t.Errorf("blocked_sessions[0].blocking_sid = %v, want 202", blocked[0]["blocking_sid"])
	}
	if blocked[0]["username"] != "appuser" {
		t.Errorf("blocked_sessions[0].username = %v, want appuser", blocked[0]["username"])
	}

	topSQL, ok := result["top_sql"].([]map[string]any)
	if !ok || len(topSQL) != 1 {
		t.Fatalf("result[top_sql] = %v, want a single-element slice", result["top_sql"])
	}
	if topSQL[0]["sql_id"] != "abc123xyz" {
		t.Errorf("top_sql[0].sql_id = %v, want abc123xyz", topSQL[0]["sql_id"])
	}
	if topSQL[0]["executions"] != 1000 {
		t.Errorf("top_sql[0].executions = %v, want 1000", topSQL[0]["executions"])
	}
	if topSQL[0]["elapsed_secs"] != 12.5 {
		t.Errorf("top_sql[0].elapsed_secs = %v, want 12.5", topSQL[0]["elapsed_secs"])
	}
}

func TestParseOutput_TablespacesSGAPGAWaitsLocksRedo(t *testing.T) {
	raw := `
SECTION:tablespaces
USERS|1024.5|2048.0|50.02|ONLINE|PERMANENT

SECTION:sga
Shared Pool|512.0
Buffer Cache|2048.0

SECTION:pga
total PGA inuse|128.5
total PGA allocated|256.0

SECTION:waits
db file sequential read|50000|120.5|User I/O

SECTION:locks
55|appuser|TX|Exclusive|None|123456|orders_table

SECTION:redo
340.75
`
	result := parseOutput(raw)

	tablespaces, ok := result["tablespaces"].([]map[string]any)
	if !ok || len(tablespaces) != 1 {
		t.Fatalf("result[tablespaces] = %v, want a single-element slice", result["tablespaces"])
	}
	if tablespaces[0]["name"] != "USERS" {
		t.Errorf("tablespaces[0].name = %v, want USERS", tablespaces[0]["name"])
	}
	if tablespaces[0]["used_pct"] != 50.02 {
		t.Errorf("tablespaces[0].used_pct = %v, want 50.02", tablespaces[0]["used_pct"])
	}

	sga, ok := result["sga"].(map[string]any)
	if !ok {
		t.Fatalf("result[sga] = %v, want map[string]any", result["sga"])
	}
	if sga["shared_pool"] != 512.0 {
		t.Errorf("sga.shared_pool = %v, want 512.0", sga["shared_pool"])
	}
	if sga["buffer_cache"] != 2048.0 {
		t.Errorf("sga.buffer_cache = %v, want 2048.0", sga["buffer_cache"])
	}

	pga, ok := result["pga"].(map[string]any)
	if !ok {
		t.Fatalf("result[pga] = %v, want map[string]any", result["pga"])
	}
	if pga["total_pga_inuse"] != 128.5 {
		t.Errorf("pga.total_pga_inuse = %v, want 128.5", pga["total_pga_inuse"])
	}

	waits, ok := result["waits"].([]map[string]any)
	if !ok || len(waits) != 1 {
		t.Fatalf("result[waits] = %v, want a single-element slice", result["waits"])
	}
	if waits[0]["event"] != "db file sequential read" {
		t.Errorf("waits[0].event = %v, want 'db file sequential read'", waits[0]["event"])
	}

	locks, ok := result["locks"].([]map[string]any)
	if !ok || len(locks) != 1 {
		t.Fatalf("result[locks] = %v, want a single-element slice", result["locks"])
	}
	if locks[0]["object_name"] != "orders_table" {
		t.Errorf("locks[0].object_name = %v, want orders_table", locks[0]["object_name"])
	}

	if result["redo_mb_last_hour"] != 340.75 {
		t.Errorf("result[redo_mb_last_hour] = %v, want 340.75", result["redo_mb_last_hour"])
	}
}

func TestParseOutput_IgnoresErrorAndOraLines(t *testing.T) {
	raw := `
SECTION:instance
ERROR: ORA-00942 table or view does not exist
ORA-01017: invalid username/password
PRODDB|OPEN|ACTIVE|dbhost01|2026-01-01 00:00:00|19.0.0.0.0
`
	result := parseOutput(raw)

	instance, ok := result["instance"].(map[string]any)
	if !ok {
		t.Fatalf("result[instance] = %v, want map[string]any", result["instance"])
	}
	if instance["name"] != "PRODDB" {
		t.Errorf("instance.name = %v, want PRODDB (ERROR/ORA- lines must be filtered out)", instance["name"])
	}
}

func TestParseOutput_EmptySectionsProduceEmptySlicesNotNil(t *testing.T) {
	result := parseOutput("")

	for _, key := range []string{"blocked_sessions", "top_sql", "tablespaces", "waits", "locks"} {
		val, ok := result[key].([]map[string]any)
		if !ok {
			t.Fatalf("result[%s] = %v (%T), want []map[string]any", key, result[key], result[key])
		}
		if len(val) != 0 {
			t.Errorf("result[%s] = %v, want empty slice", key, val)
		}
	}

	if _, exists := result["instance"]; exists {
		t.Errorf("result[instance] should be absent when no instance section is present, got %v", result["instance"])
	}
	if _, exists := result["redo_mb_last_hour"]; exists {
		t.Errorf("result[redo_mb_last_hour] should be absent when no redo section is present, got %v", result["redo_mb_last_hour"])
	}
}
