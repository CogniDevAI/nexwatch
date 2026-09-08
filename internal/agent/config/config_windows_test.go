//go:build windows

package config

import (
	"os"
	"strings"
	"testing"
)

func TestDefaultCollectorsEnabled_Windows(t *testing.T) {
	got := defaultCollectorsEnabled()
	for _, excluded := range []string{"oracle", "vulnerabilities"} {
		for _, name := range got {
			if name == excluded {
				t.Errorf("defaultCollectorsEnabled() includes %q, which has no Windows implementation", excluded)
			}
		}
	}
	for _, required := range []string{"cpu", "memory", "disk", "network", "sysinfo", "docker", "ports", "processes", "hardening", "diskio", "connections", "services", "cve_scan"} {
		found := false
		for _, name := range got {
			if name == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("defaultCollectorsEnabled() missing %q", required)
		}
	}
}

func TestDefaultDockerSocket_Windows(t *testing.T) {
	if got := defaultDockerSocket(); got != "npipe:////./pipe/docker_engine" {
		t.Errorf("defaultDockerSocket() = %q, want npipe:////./pipe/docker_engine", got)
	}
}

func TestProgramDataDir(t *testing.T) {
	t.Run("uses ProgramData env var when set", func(t *testing.T) {
		t.Setenv("ProgramData", `D:\CustomProgramData`)
		if got := programDataDir(); got != `D:\CustomProgramData` {
			t.Errorf("programDataDir() = %q, want D:\\CustomProgramData", got)
		}
	})

	t.Run("falls back to C:\\ProgramData when unset", func(t *testing.T) {
		_ = os.Unsetenv("ProgramData")
		if got := programDataDir(); got != `C:\ProgramData` {
			t.Errorf("programDataDir() = %q, want C:\\ProgramData", got)
		}
	})
}

func TestDefaultCveScanCacheDir_Windows(t *testing.T) {
	t.Setenv("ProgramData", `C:\ProgramData`)
	got := defaultCveScanCacheDir()
	if !strings.HasSuffix(got, `NexWatch\scanner-cache`) {
		t.Errorf("defaultCveScanCacheDir() = %q, want suffix NexWatch\\scanner-cache", got)
	}
}

func TestDefaultSystemConfigPath_Windows(t *testing.T) {
	t.Setenv("ProgramData", `C:\ProgramData`)
	got := defaultSystemConfigPath()
	if got != `C:\ProgramData\NexWatch\agent.yaml` {
		t.Errorf("defaultSystemConfigPath() = %q, want C:\\ProgramData\\NexWatch\\agent.yaml", got)
	}
}
