package api

import "testing"

// TestExtractNetworkBytesAndRates covers the primary-interface selection
// (skip loopback, pick the interface with the most cumulative traffic) and
// the two derived views of a "network" metric's data: the cumulative
// counters in MB (extractNetworkBytes, kept for backwards compatibility)
// and the current bytes-per-second rate (extractNetworkRates, the fix for
// the network metric being mislabeled as a rate when it was actually a
// cumulative counter since agent start).
func TestExtractNetworkBytesAndRates(t *testing.T) {
	tests := []struct {
		name       string
		data       map[string]any
		wantRxMB   float64
		wantTxMB   float64
		wantRxRate float64
		wantTxRate float64
	}{
		{
			name: "single non-loopback interface",
			data: map[string]any{
				"interfaces": []any{
					map[string]any{
						"name":               "eth0",
						"bytes_recv":         float64(10 * 1024 * 1024),
						"bytes_sent":         float64(5 * 1024 * 1024),
						"bytes_recv_per_sec": 1500.0,
						"bytes_sent_per_sec": 250.0,
					},
				},
			},
			wantRxMB: 10, wantTxMB: 5,
			wantRxRate: 1500, wantTxRate: 250,
		},
		{
			name: "loopback is skipped in favor of the real interface",
			data: map[string]any{
				"interfaces": []any{
					map[string]any{
						"name":               "lo0",
						"bytes_recv":         float64(999 * 1024 * 1024),
						"bytes_sent":         float64(999 * 1024 * 1024),
						"bytes_recv_per_sec": 99999.0,
						"bytes_sent_per_sec": 99999.0,
					},
					map[string]any{
						"name":               "eth0",
						"bytes_recv":         float64(2 * 1024 * 1024),
						"bytes_sent":         float64(1 * 1024 * 1024),
						"bytes_recv_per_sec": 100.0,
						"bytes_sent_per_sec": 50.0,
					},
				},
			},
			wantRxMB: 2, wantTxMB: 1,
			wantRxRate: 100, wantTxRate: 50,
		},
		{
			name: "picks the interface with the most cumulative traffic",
			data: map[string]any{
				"interfaces": []any{
					map[string]any{
						"name":               "eth0",
						"bytes_recv":         float64(1 * 1024 * 1024),
						"bytes_sent":         float64(1 * 1024 * 1024),
						"bytes_recv_per_sec": 10.0,
						"bytes_sent_per_sec": 10.0,
					},
					map[string]any{
						"name":               "eth1",
						"bytes_recv":         float64(50 * 1024 * 1024),
						"bytes_sent":         float64(20 * 1024 * 1024),
						"bytes_recv_per_sec": 5000.0,
						"bytes_sent_per_sec": 2000.0,
					},
				},
			},
			wantRxMB: 50, wantTxMB: 20,
			wantRxRate: 5000, wantTxRate: 2000,
		},
		{
			name:     "missing interfaces field",
			data:     map[string]any{},
			wantRxMB: 0, wantTxMB: 0,
			wantRxRate: 0, wantTxRate: 0,
		},
		{
			name: "older agent without *_per_sec fields falls back to zero rate",
			data: map[string]any{
				"interfaces": []any{
					map[string]any{
						"name":       "eth0",
						"bytes_recv": float64(3 * 1024 * 1024),
						"bytes_sent": float64(1 * 1024 * 1024),
					},
				},
			},
			wantRxMB: 3, wantTxMB: 1,
			wantRxRate: 0, wantTxRate: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rxMB, txMB := extractNetworkBytes(tt.data)
			if rxMB != tt.wantRxMB || txMB != tt.wantTxMB {
				t.Errorf("extractNetworkBytes() = (%v, %v), want (%v, %v)", rxMB, txMB, tt.wantRxMB, tt.wantTxMB)
			}

			rxRate, txRate := extractNetworkRates(tt.data)
			if rxRate != tt.wantRxRate || txRate != tt.wantTxRate {
				t.Errorf("extractNetworkRates() = (%v, %v), want (%v, %v)", rxRate, txRate, tt.wantRxRate, tt.wantTxRate)
			}
		})
	}
}
