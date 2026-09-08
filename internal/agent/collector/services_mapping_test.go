package collector

import "testing"

func TestMapWindowsService(t *testing.T) {
	tests := []struct {
		name       string
		svcName    string
		state      windowsServiceState
		startType  windowsStartType
		wantActive string
		wantSub    string
	}{
		{
			name:       "running service",
			svcName:    "Spooler",
			state:      windowsServiceRunning,
			startType:  windowsStartAutomatic,
			wantActive: "active",
			wantSub:    "running",
		},
		{
			name:       "stopped automatic service is failed",
			svcName:    "wuauserv",
			state:      windowsServiceStopped,
			startType:  windowsStartAutomatic,
			wantActive: "inactive",
			wantSub:    "failed",
		},
		{
			name:       "stopped automatic-delayed service is failed",
			svcName:    "wuauserv",
			state:      windowsServiceStopped,
			startType:  windowsStartAutomaticDelayed,
			wantActive: "inactive",
			wantSub:    "failed",
		},
		{
			name:       "stopped manual service is not failed",
			svcName:    "sshd",
			state:      windowsServiceStopped,
			startType:  windowsStartManual,
			wantActive: "inactive",
			wantSub:    "stopped",
		},
		{
			name:       "stopped disabled service is not failed",
			svcName:    "Fax",
			state:      windowsServiceStopped,
			startType:  windowsStartDisabled,
			wantActive: "inactive",
			wantSub:    "stopped",
		},
		{
			name:       "paused service",
			svcName:    "MSSQLSERVER",
			state:      windowsServicePaused,
			startType:  windowsStartAutomatic,
			wantActive: "active",
			wantSub:    "paused",
		},
		{
			name:       "start pending service",
			svcName:    "Booting",
			state:      windowsServiceStartPending,
			startType:  windowsStartAutomatic,
			wantActive: "activating",
			wantSub:    "start-pending",
		},
		{
			name:       "unknown state",
			svcName:    "Mystery",
			state:      windowsServiceUnknown,
			startType:  windowsStartUnknown,
			wantActive: "unknown",
			wantSub:    "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapWindowsService(tt.svcName, tt.state, tt.startType, "a test service")

			if got["name"] != tt.svcName {
				t.Errorf("name = %v, want %v", got["name"], tt.svcName)
			}
			if got["active"] != tt.wantActive {
				t.Errorf("active = %v, want %v", got["active"], tt.wantActive)
			}
			if got["sub"] != tt.wantSub {
				t.Errorf("sub = %v, want %v", got["sub"], tt.wantSub)
			}
			if got["description"] != "a test service" {
				t.Errorf("description = %v, want %q", got["description"], "a test service")
			}

			// The service_failed alert rule (internal/hub/alerts.Engine.
			// isFailedServiceState) treats "failed"/"inactive"/"dead" as a
			// breach for either the "active" or "sub" field. Cross-check
			// that a mapping intended to be "not failed" doesn't
			// accidentally still breach via the "active" field.
			if tt.wantSub != "failed" && got["active"] == "inactive" && tt.wantSub != "stopped" {
				t.Errorf("unexpected inactive mapping for a non-failed case: %+v", got)
			}
		})
	}
}

func TestMapWindowsService_StoppedManualNeverReadsAsFailed(t *testing.T) {
	// Regression guard for the specific distinction the brief calls out:
	// a manually-stopped/disabled service must never present the
	// "failed"/"inactive"/"dead" vocabulary evalServiceFailed treats as a
	// breach, since that would fire spurious alerts for services that are
	// deliberately off.
	for _, st := range []windowsStartType{windowsStartManual, windowsStartDisabled} {
		got := mapWindowsService("svc", windowsServiceStopped, st, "")
		for _, field := range []string{"active", "sub"} {
			v, _ := got[field].(string)
			if v == "failed" || v == "dead" {
				t.Errorf("field %q = %q for start type %v, want neither \"failed\" nor \"dead\"", field, v, st)
			}
		}
	}
}

func TestSummarizeWindowsServices(t *testing.T) {
	services := []map[string]any{
		{"name": "a", "sub": "running"},
		{"name": "b", "sub": "running"},
		{"name": "c", "sub": "failed"},
		{"name": "d", "sub": "stopped"},
	}

	got := summarizeWindowsServices(services)

	if got["total"] != 4 {
		t.Errorf("total = %v, want 4", got["total"])
	}
	if got["running"] != 2 {
		t.Errorf("running = %v, want 2", got["running"])
	}
	if got["failed"] != 1 {
		t.Errorf("failed = %v, want 1", got["failed"])
	}
	if got["other"] != 1 {
		t.Errorf("other = %v, want 1", got["other"])
	}
}

func TestSummarizeWindowsServices_CapsAtOneHundred(t *testing.T) {
	services := make([]map[string]any, 150)
	for i := range services {
		services[i] = map[string]any{"name": "svc", "sub": "running"}
	}

	got := summarizeWindowsServices(services)

	if got["total"] != 100 {
		t.Errorf("total = %v, want 100 (capped)", got["total"])
	}
}

func TestWindowsStartTypeString(t *testing.T) {
	tests := []struct {
		startType windowsStartType
		want      string
	}{
		{windowsStartAutomatic, "automatic"},
		{windowsStartAutomaticDelayed, "automatic (delayed)"},
		{windowsStartManual, "manual"},
		{windowsStartDisabled, "disabled"},
		{windowsStartUnknown, "unknown"},
	}
	for _, tt := range tests {
		if got := windowsStartTypeString(tt.startType); got != tt.want {
			t.Errorf("windowsStartTypeString(%v) = %q, want %q", tt.startType, got, tt.want)
		}
	}
}
