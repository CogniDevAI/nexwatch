package collector

import "testing"

// TestRate covers the pure per-second delta computation used by
// NetworkCollector.Collect to derive bytes_recv_per_sec/bytes_sent_per_sec
// from two cumulative counter samples, including the counter-reset case
// (e.g. an interface counter wrapping, or the agent restarting) which must
// clamp to zero instead of reporting a bogus negative or huge rate.
func TestRate(t *testing.T) {
	tests := []struct {
		name           string
		prev           uint64
		cur            uint64
		elapsedSeconds float64
		want           float64
	}{
		{"steady increase over one second", 1000, 2000, 1, 1000},
		{"steady increase over ten seconds", 1000, 11000, 10, 1000},
		{"no growth", 5000, 5000, 1, 0},
		{"counter reset (cur < prev) clamps to zero", 5000, 100, 1, 0},
		{"zero elapsed time clamps to zero", 1000, 2000, 0, 0},
		{"negative elapsed time clamps to zero", 1000, 2000, -1, 0},
		{"fractional interval", 0, 500, 0.5, 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rate(tt.prev, tt.cur, tt.elapsedSeconds); got != tt.want {
				t.Errorf("rate(%d, %d, %v) = %v, want %v", tt.prev, tt.cur, tt.elapsedSeconds, got, tt.want)
			}
		})
	}
}

// TestNetworkCollector_Name is a trivial sanity check that the collector
// reports its expected identifier, matching the "network" metric type the
// hub's alert engine and API routes key off of.
func TestNetworkCollector_Name(t *testing.T) {
	c := NewNetworkCollector()
	if got := c.Name(); got != "network" {
		t.Errorf("Name() = %q, want %q", got, "network")
	}
}
