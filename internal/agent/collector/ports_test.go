package collector

import "testing"

func TestProtocolName(t *testing.T) {
	tests := []struct {
		name     string
		connType uint32
		want     string
	}{
		{"tcp", 1, "tcp"},
		{"udp", 2, "udp"},
		{"unknown type", 99, "unknown(99)"},
		{"zero value is unknown", 0, "unknown(0)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := protocolName(tt.connType); got != tt.want {
				t.Errorf("protocolName(%d) = %q, want %q", tt.connType, got, tt.want)
			}
		})
	}
}
