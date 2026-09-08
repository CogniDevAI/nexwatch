package collector

import (
	"os"
	"testing"
)

func TestEvaluateSSHRootLoginConfig(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantStatus string
	}{
		{
			name:       "explicitly disabled",
			content:    "Port 22\nPermitRootLogin no\n",
			wantStatus: "pass",
		},
		{
			name:       "prohibit-password is restricted",
			content:    "PermitRootLogin prohibit-password\n",
			wantStatus: "pass",
		},
		{
			name:       "forced-commands-only is restricted",
			content:    "permitrootlogin forced-commands-only\n",
			wantStatus: "pass",
		},
		{
			name:       "explicitly yes fails",
			content:    "PermitRootLogin yes\n",
			wantStatus: "fail",
		},
		{
			name:       "commented out line is ignored, falls through to warn",
			content:    "#PermitRootLogin no\n",
			wantStatus: "warn",
		},
		{
			name:       "not present at all warns",
			content:    "Port 22\nProtocol 2\n",
			wantStatus: "warn",
		},
		{
			name:       "empty file warns",
			content:    "",
			wantStatus: "warn",
		},
		{
			name:       "case insensitive directive name",
			content:    "PERMITROOTLOGIN NO\n",
			wantStatus: "pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateSSHRootLoginConfig(tt.content)
			if got.Status != tt.wantStatus {
				t.Errorf("evaluateSSHRootLoginConfig(%q) status = %q, want %q (description: %s)", tt.content, got.Status, tt.wantStatus, got.Description)
			}
			if got.Name != "ssh_root_login" {
				t.Errorf("Name = %q, want ssh_root_login", got.Name)
			}
		})
	}
}

func TestEvaluatePasswdPermissions(t *testing.T) {
	tests := []struct {
		name       string
		mode       uint32
		wantStatus string
	}{
		{"0644 no group/other write", 0o644, "pass"},
		{"0600 owner only", 0o600, "pass"},
		{"0664 group writable fails", 0o664, "fail"},
		{"0666 world writable fails", 0o666, "fail"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluatePasswdPermissions(os.FileMode(tt.mode))
			if got.Status != tt.wantStatus {
				t.Errorf("evaluatePasswdPermissions(0o%o) status = %q, want %q", tt.mode, got.Status, tt.wantStatus)
			}
		})
	}
}

func TestEvaluateShadowPermissions(t *testing.T) {
	tests := []struct {
		name       string
		mode       uint32
		wantStatus string
	}{
		{"0600 root only", 0o600, "pass"},
		{"0640 group read only", 0o640, "pass"},
		{"0644 world readable fails", 0o644, "fail"},
		{"0664 group writable fails", 0o664, "fail"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateShadowPermissions(os.FileMode(tt.mode))
			if got.Status != tt.wantStatus {
				t.Errorf("evaluateShadowPermissions(0o%o) status = %q, want %q", tt.mode, got.Status, tt.wantStatus)
			}
		})
	}
}

func TestEvaluateExtraRootUsers(t *testing.T) {
	tests := []struct {
		name       string
		passwd     string
		wantStatus string
	}{
		{
			name:       "only root has uid 0",
			passwd:     "root:x:0:0:root:/root:/bin/bash\ndaemon:x:1:1:daemon:/usr/sbin:/usr/sbin/nologin\n",
			wantStatus: "pass",
		},
		{
			name:       "extra uid 0 account fails",
			passwd:     "root:x:0:0:root:/root:/bin/bash\nbackdoor:x:0:0:sneaky:/root:/bin/bash\n",
			wantStatus: "fail",
		},
		{
			name:       "empty passwd content passes trivially",
			passwd:     "",
			wantStatus: "pass",
		},
		{
			name:       "malformed lines without enough fields are ignored",
			passwd:     "root:x:0:0:root:/root:/bin/bash\nmalformed-line\n",
			wantStatus: "pass",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateExtraRootUsers(tt.passwd)
			if got.Status != tt.wantStatus {
				t.Errorf("evaluateExtraRootUsers(...) status = %q, want %q (description: %s)", got.Status, tt.wantStatus, got.Description)
			}
		})
	}
}

func TestEvaluateDangerousPorts(t *testing.T) {
	dangerousPorts := map[uint32]string{
		21: "FTP",
		23: "Telnet",
	}

	t.Run("no ports listening, all pass", func(t *testing.T) {
		results := evaluateDangerousPorts(dangerousPorts, map[uint32]bool{})
		if len(results) != 2 {
			t.Fatalf("len(results) = %d, want 2", len(results))
		}
		for _, r := range results {
			if r.Status != "pass" {
				t.Errorf("result %s status = %q, want pass", r.Name, r.Status)
			}
		}
	})

	t.Run("one dangerous port listening fails only that one", func(t *testing.T) {
		results := evaluateDangerousPorts(dangerousPorts, map[uint32]bool{23: true})
		statuses := map[string]string{}
		for _, r := range results {
			statuses[r.Name] = r.Status
		}
		if statuses["dangerous_port_21"] != "pass" {
			t.Errorf("dangerous_port_21 status = %q, want pass", statuses["dangerous_port_21"])
		}
		if statuses["dangerous_port_23"] != "fail" {
			t.Errorf("dangerous_port_23 status = %q, want fail", statuses["dangerous_port_23"])
		}
	})
}

func TestEvaluateWindowsFirewallOutput(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantStatus string
	}{
		{
			name: "all profiles on",
			output: "Domain Profile Settings:\n" +
				"----------------------------------------------------------------------\n" +
				"State                                 ON\n\n" +
				"Private Profile Settings:\n" +
				"----------------------------------------------------------------------\n" +
				"State                                 ON\n\n" +
				"Public Profile Settings:\n" +
				"----------------------------------------------------------------------\n" +
				"State                                 ON\n\n" +
				"Ok.\n",
			wantStatus: "pass",
		},
		{
			name: "one profile off",
			output: "Domain Profile Settings:\n" +
				"State                                 ON\n" +
				"Private Profile Settings:\n" +
				"State                                 ON\n" +
				"Public Profile Settings:\n" +
				"State                                 OFF\n",
			wantStatus: "fail",
		},
		{
			name:       "unparseable output",
			output:     "some unexpected netsh error text",
			wantStatus: "skip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateWindowsFirewallOutput(tt.output)
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q (description: %s)", got.Status, tt.wantStatus, got.Description)
			}
		})
	}
}

func TestEvaluateWindowsDefenderOutput(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantStatus string
	}{
		{"enabled", "True\r\n", "pass"},
		{"disabled", "False\n", "fail"},
		{"case insensitive", "TRUE", "pass"},
		{"unexpected output", "Get-MpComputerStatus is not recognized", "skip"},
		{"empty output", "", "skip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateWindowsDefenderOutput(tt.output)
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tt.wantStatus)
			}
		})
	}
}

func TestEvaluateRDPDenyFlag(t *testing.T) {
	tests := []struct {
		name       string
		denyTS     uint64
		ok         bool
		wantStatus string
	}{
		{"read failed", 0, false, "skip"},
		{"rdp enabled (denyTS=0)", 0, true, "warn"},
		{"rdp disabled (denyTS=1)", 1, true, "pass"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateRDPDenyFlag(tt.denyTS, tt.ok)
			if got.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Name != "rdp_exposure" {
				t.Errorf("name = %q, want rdp_exposure", got.Name)
			}
		})
	}
}

func TestCheckRDPStatus_SkipsOnNonWindows(t *testing.T) {
	// hardening_other.go's implementation (compiled on this test's own
	// platform, since it's never windows in this repo's CI) always
	// reports "could not read" — verifying the no-op stub behaves exactly
	// like a real read failure rather than a distinct "not applicable"
	// status the hub/UI would need to special-case.
	c := NewHardeningCollector()
	got := c.checkRDPStatus()
	if got.Status != "skip" {
		t.Errorf("status = %q, want skip", got.Status)
	}
}

func TestCheckWindowsDefenderStatus_SkipsOnNonWindows(t *testing.T) {
	c := NewHardeningCollector()
	got := c.checkWindowsDefenderStatus()
	if got.Status != "skip" {
		t.Errorf("status = %q, want skip", got.Status)
	}
}
