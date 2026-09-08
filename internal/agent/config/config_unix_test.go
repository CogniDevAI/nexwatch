//go:build !windows

package config

import "testing"

func TestDefaultCollectorsEnabled_Unix(t *testing.T) {
	got := defaultCollectorsEnabled()

	want := map[string]bool{
		"cpu": true, "memory": true, "disk": true, "network": true,
		"sysinfo": true, "docker": true, "ports": true, "processes": true,
		"hardening": true, "vulnerabilities": true, "diskio": true,
		"connections": true, "services": true, "cve_scan": true,
	}
	if len(got) != len(want) {
		t.Fatalf("defaultCollectorsEnabled() = %v (len %d), want len %d", got, len(got), len(want))
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("unexpected collector %q in unix default list", name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Errorf("missing collectors from unix default list: %v", want)
	}
}

func TestDefaultDockerSocket_Unix(t *testing.T) {
	if got := defaultDockerSocket(); got != "/var/run/docker.sock" {
		t.Errorf("defaultDockerSocket() = %q, want /var/run/docker.sock", got)
	}
}

func TestDefaultCveScanCacheDir_Unix(t *testing.T) {
	if got := defaultCveScanCacheDir(); got != "/var/lib/nexwatch/scanner-cache" {
		t.Errorf("defaultCveScanCacheDir() = %q, want /var/lib/nexwatch/scanner-cache", got)
	}
}

func TestDefaultSystemConfigPath_Unix(t *testing.T) {
	if got := defaultSystemConfigPath(); got != "/etc/nexwatch/agent.yaml" {
		t.Errorf("defaultSystemConfigPath() = %q, want /etc/nexwatch/agent.yaml", got)
	}
}
