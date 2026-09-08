package platform

import (
	"runtime"
	"testing"
)

func TestSupported(t *testing.T) {
	tests := []struct {
		name          string
		collector     string
		wantSupported bool
	}{
		{"cpu is always supported", "cpu", true},
		{"unknown collector defaults to supported", "some_future_collector", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := Supported(tt.collector)
			if ok != tt.wantSupported {
				t.Errorf("Supported(%q) = (%v, %q), want ok=%v", tt.collector, ok, reason, tt.wantSupported)
			}
			if ok && reason != "" {
				t.Errorf("Supported(%q) returned a reason %q while reporting supported", tt.collector, reason)
			}
		})
	}
}

// TestSupported_WindowsUnsupportedCollectors documents the collectors this
// build knows have no Windows implementation. It only asserts the negative
// case when actually running on windows (the map is keyed by GOOS), but
// always exercises Supported() so the table stays part of coverage on
// every platform.
func TestSupported_WindowsUnsupportedCollectors(t *testing.T) {
	for _, name := range []string{"vulnerabilities", "oracle"} {
		ok, reason := Supported(name)
		if runtime.GOOS == "windows" {
			if ok {
				t.Errorf("Supported(%q) = true on windows, want false", name)
			}
			if reason == "" {
				t.Errorf("Supported(%q) returned no reason on windows", name)
			}
		} else if !ok {
			t.Errorf("Supported(%q) = false on %s, want true (only excluded on windows)", name, runtime.GOOS)
		}
	}
}

// TestSupported_AlwaysHasReasonWhenUnsupported guards against a future
// entry in the unsupported map that forgets to set a reason string, which
// would produce a useless log line at registration time.
func TestSupported_AlwaysHasReasonWhenUnsupported(t *testing.T) {
	for collector, byGOOS := range unsupported {
		for goos, reason := range byGOOS {
			if reason == "" {
				t.Errorf("unsupported[%q][%q] has an empty reason", collector, goos)
			}
		}
	}
}
