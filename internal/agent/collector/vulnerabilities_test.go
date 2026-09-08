package collector

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestEvaluateWorldWritableFindOutput(t *testing.T) {
	tests := []struct {
		name       string
		findOutput string
		wantNil    bool
		wantCount  int
	}{
		{"empty output yields no finding", "", true, 0},
		{"whitespace-only output yields no finding", "\n\n  \n", true, 0},
		{"single file found", "/etc/foo.conf\n", false, 1},
		{"multiple files found", "/etc/foo.conf\n/etc/bar.conf\n/etc/baz.conf\n", false, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateWorldWritableFindOutput("/etc", tt.findOutput)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("evaluateWorldWritableFindOutput() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("evaluateWorldWritableFindOutput() = nil, want a finding")
			}
			wantPhrase := "Found " + strconv.Itoa(tt.wantCount) + " world-writable files"
			if !strings.Contains(got.Description, wantPhrase) {
				t.Errorf("Description = %q, want it to contain %q", got.Description, wantPhrase)
			}
			if got.Name != "world_writable_in__etc" {
				t.Errorf("Name = %q, want world_writable_in__etc", got.Name)
			}
		})
	}
}

func TestEvaluateWorldWritableFindOutput_TruncatesReportedListTo20(t *testing.T) {
	lines := make([]string, 25)
	for i := range lines {
		lines[i] = "/etc/file" + string(rune('a'+i))
	}
	findOutput := strings.Join(lines, "\n")

	got := evaluateWorldWritableFindOutput("/etc", findOutput)
	if got == nil {
		t.Fatal("expected a finding for 25 world-writable files")
	}
	if !strings.Contains(got.Description, "Found 25 world-writable files") {
		t.Errorf("Description = %q, want it to report the full count of 25", got.Description)
	}
}

func TestEvaluateTmpStickyBit(t *testing.T) {
	tests := []struct {
		name    string
		mode    os.FileMode
		wantNil bool
	}{
		{"sticky bit set yields no finding", os.ModeSticky | 0o777, true},
		{"sticky bit missing yields a finding", 0o777, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateTmpStickyBit(tt.mode)
			if tt.wantNil && got != nil {
				t.Errorf("evaluateTmpStickyBit(%v) = %+v, want nil", tt.mode, got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("evaluateTmpStickyBit(%v) = nil, want a finding", tt.mode)
			}
		})
	}
}

func TestEvaluateSUIDFindOutput(t *testing.T) {
	safeDirs := map[string]bool{"/usr/bin": true, "/bin": true}

	tests := []struct {
		name       string
		findOutput string
		wantNil    bool
	}{
		{"empty output yields no finding", "", true},
		{"only safe-dir binaries yields no finding", "/usr/bin/sudo\n/bin/su\n", true},
		{"unusual location binary yields a finding", "/opt/weird/binary\n", false},
		{"mixed safe and unusual only flags the unusual one", "/usr/bin/sudo\n/tmp/backdoor\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateSUIDFindOutput(tt.findOutput, safeDirs)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("evaluateSUIDFindOutput() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("evaluateSUIDFindOutput() = nil, want a finding")
			}
		})
	}
}

func TestEvaluateSUIDFindOutput_DoesNotFlagSafeDirSubpaths(t *testing.T) {
	safeDirs := map[string]bool{"/usr/bin": true}
	got := evaluateSUIDFindOutput("/usr/bin/sudo\n", safeDirs)
	if got != nil {
		t.Errorf("evaluateSUIDFindOutput() = %+v, want nil for a binary directly under a safe dir", got)
	}
}

func TestIsRiskyRootProcess(t *testing.T) {
	riskyAsRoot := map[string]bool{"nginx": true, "mysqld": true}

	tests := []struct {
		name string
		proc string
		uids []uint32
		want bool
	}{
		{"risky process running as root (effective uid index 1)", "nginx", []uint32{0, 0, 0, 0}, true},
		{"risky process not running as root", "nginx", []uint32{0, 1000, 1000, 1000}, false},
		{"non-risky process ignored even if root", "bash", []uint32{0, 0, 0, 0}, false},
		{"single-uid slice falls back to index 0", "mysqld", []uint32{0}, true},
		{"empty uid slice is never risky", "nginx", []uint32{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRiskyRootProcess(tt.proc, tt.uids, riskyAsRoot); got != tt.want {
				t.Errorf("isRiskyRootProcess(%q, %v) = %v, want %v", tt.proc, tt.uids, got, tt.want)
			}
		})
	}
}

func TestEvaluateFilePermission(t *testing.T) {
	tests := []struct {
		name    string
		perm    os.FileMode
		maxPerm os.FileMode
		wantNil bool
	}{
		{"exactly at max is fine", 0o644, 0o644, true},
		{"more restrictive than max is fine", 0o600, 0o644, true},
		{"looser than max is a finding", 0o666, 0o644, false},
		{"sudoers world readable is a finding", 0o444, 0o440, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := evaluateFilePermission("/etc/sudoers", tt.perm, tt.maxPerm)
			if tt.wantNil && got != nil {
				t.Errorf("evaluateFilePermission() = %+v, want nil", got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("evaluateFilePermission() = nil, want a finding")
			}
		})
	}
}
