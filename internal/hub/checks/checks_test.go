package checks

import "testing"

func TestNextDebouncedState(t *testing.T) {
	tests := []struct {
		name               string
		prevStatus         string
		prevFails          int
		success            bool
		failuresBeforeDown int
		wantStatus         string
		wantFails          int
	}{
		{
			name:               "first ever attempt succeeds",
			prevStatus:         "",
			prevFails:          0,
			success:            true,
			failuresBeforeDown: 2,
			wantStatus:         "up",
			wantFails:          0,
		},
		{
			name:               "first ever attempt fails, stays up during grace period",
			prevStatus:         "",
			prevFails:          0,
			success:            false,
			failuresBeforeDown: 2,
			wantStatus:         "up",
			wantFails:          1,
		},
		{
			name:               "second consecutive failure flips to down",
			prevStatus:         "up",
			prevFails:          1,
			success:            false,
			failuresBeforeDown: 2,
			wantStatus:         "down",
			wantFails:          2,
		},
		{
			name:               "already down stays down on continued failure",
			prevStatus:         "down",
			prevFails:          2,
			success:            false,
			failuresBeforeDown: 2,
			wantStatus:         "down",
			wantFails:          3,
		},
		{
			name:               "success while down immediately recovers",
			prevStatus:         "down",
			prevFails:          5,
			success:            true,
			failuresBeforeDown: 2,
			wantStatus:         "up",
			wantFails:          0,
		},
		{
			name:               "failuresBeforeDown of 1 flips down on first failure",
			prevStatus:         "",
			prevFails:          0,
			success:            false,
			failuresBeforeDown: 1,
			wantStatus:         "down",
			wantFails:          1,
		},
		{
			name:               "failuresBeforeDown below 1 is clamped to 1",
			prevStatus:         "",
			prevFails:          0,
			success:            false,
			failuresBeforeDown: 0,
			wantStatus:         "down",
			wantFails:          1,
		},
		{
			name:               "still within grace period keeps previous down status",
			prevStatus:         "down",
			prevFails:          1,
			success:            false,
			failuresBeforeDown: 3,
			wantStatus:         "down",
			wantFails:          2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, fails := nextDebouncedState(tt.prevStatus, tt.prevFails, tt.success, tt.failuresBeforeDown)
			if status != tt.wantStatus {
				t.Errorf("status = %q, want %q", status, tt.wantStatus)
			}
			if fails != tt.wantFails {
				t.Errorf("consecutiveFails = %d, want %d", fails, tt.wantFails)
			}
		})
	}
}
