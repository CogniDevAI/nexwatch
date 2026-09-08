package collector

import "testing"

func TestRoundTo2(t *testing.T) {
	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{"already 2 decimals", 12.34, 12.34},
		{"truncates extra decimals", 12.3456, 12.34},
		{"truncates rather than rounds up", 12.999, 12.99},
		{"whole number stays whole", 5.0, 5.0},
		{"zero", 0.0, 0.0},
		{"negative value", -3.456, -3.45},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := roundTo2(tt.in); got != tt.want {
				t.Errorf("roundTo2(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"shorter than max is unchanged", "hello", 10, "hello"},
		{"exactly at max is unchanged", "hello", 5, "hello"},
		{"longer than max is truncated with ellipsis", "hello world", 5, "hello..."},
		{"leading/trailing whitespace trimmed first", "  hello  ", 10, "hello"},
		{"empty string stays empty", "", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateStr(tt.in, tt.max); got != tt.want {
				t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
		})
	}
}
