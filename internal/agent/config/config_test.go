package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withArgs sets os.Args for the duration of the test and restores it after.
func withArgs(t *testing.T, args []string) {
	t.Helper()
	old := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = old })
}

// nonexistentConfigPath returns a --config path guaranteed not to exist, so
// Load() falls through to defaults/env/flags without picking up a real file.
func nonexistentConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "does-not-exist.yaml")
}

func writeYAML(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("failed to write test yaml: %v", err)
	}
	return path
}

func TestDefaultConfig(t *testing.T) {
	def := DefaultConfig()

	if def.HubURL != "ws://localhost:8090/ws/agent" {
		t.Errorf("DefaultConfig().HubURL = %q, want ws://localhost:8090/ws/agent", def.HubURL)
	}
	if def.Token != "" {
		t.Errorf("DefaultConfig().Token = %q, want empty", def.Token)
	}
	if def.Interval != 10*time.Second {
		t.Errorf("DefaultConfig().Interval = %v, want 10s", def.Interval)
	}
	if def.DockerSocket != "/var/run/docker.sock" {
		t.Errorf("DefaultConfig().DockerSocket = %q, want /var/run/docker.sock", def.DockerSocket)
	}
	if !def.DockerUpdateChecks {
		t.Error("DefaultConfig().DockerUpdateChecks = false, want true")
	}
	wantCollectors := []string{
		"cpu", "memory", "disk", "network", "sysinfo", "docker",
		"ports", "processes", "hardening", "vulnerabilities",
		"diskio", "connections", "services", "cve_scan",
	}
	if len(def.CollectorsEnabled) != len(wantCollectors) {
		t.Fatalf("DefaultConfig().CollectorsEnabled = %v, want %v", def.CollectorsEnabled, wantCollectors)
	}
	for i, c := range wantCollectors {
		if def.CollectorsEnabled[i] != c {
			t.Errorf("DefaultConfig().CollectorsEnabled[%d] = %q, want %q", i, def.CollectorsEnabled[i], c)
		}
	}
	if !def.CveScanEnabled {
		t.Error("DefaultConfig().CveScanEnabled = false, want true")
	}
	if def.CveScanInterval != 12*time.Hour {
		t.Errorf("DefaultConfig().CveScanInterval = %v, want 12h", def.CveScanInterval)
	}
	if def.CveScanMaxImages != 10 {
		t.Errorf("DefaultConfig().CveScanMaxImages = %d, want 10", def.CveScanMaxImages)
	}
	if def.CveScanCacheDir != "/var/lib/nexwatch/scanner-cache" {
		t.Errorf("DefaultConfig().CveScanCacheDir = %q, want /var/lib/nexwatch/scanner-cache", def.CveScanCacheDir)
	}
}

func TestLoad_NoSourcesReturnsDefaults(t *testing.T) {
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t)})

	cfg := Load()
	def := DefaultConfig()

	if cfg.HubURL != def.HubURL {
		t.Errorf("Load().HubURL = %q, want default %q", cfg.HubURL, def.HubURL)
	}
	if cfg.Token != def.Token {
		t.Errorf("Load().Token = %q, want default %q", cfg.Token, def.Token)
	}
	if cfg.Interval != def.Interval {
		t.Errorf("Load().Interval = %v, want default %v", cfg.Interval, def.Interval)
	}
	if cfg.DockerSocket != def.DockerSocket {
		t.Errorf("Load().DockerSocket = %q, want default %q", cfg.DockerSocket, def.DockerSocket)
	}
	if cfg.DockerUpdateChecks != def.DockerUpdateChecks {
		t.Errorf("Load().DockerUpdateChecks = %v, want default %v", cfg.DockerUpdateChecks, def.DockerUpdateChecks)
	}
	if cfg.OracleHome != "" || cfg.OracleSID != "" {
		t.Errorf("Load().OracleHome/SID = %q/%q, want empty when unset", cfg.OracleHome, cfg.OracleSID)
	}
}

func TestLoad_YAMLFileOverridesDefaults(t *testing.T) {
	path := writeYAML(t, `
hub_url: "ws://yaml-host:9999/ws/agent"
token: "yaml-token"
interval: 30s
docker_socket: "/custom/docker.sock"
oracle_home: "/oracle/home"
oracle_sid: "ORCL"
collectors_enabled:
  - cpu
  - memory
`)
	withArgs(t, []string{"agent", "--config", path})

	cfg := Load()

	if cfg.HubURL != "ws://yaml-host:9999/ws/agent" {
		t.Errorf("Load().HubURL = %q, want yaml value", cfg.HubURL)
	}
	if cfg.Token != "yaml-token" {
		t.Errorf("Load().Token = %q, want yaml value", cfg.Token)
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("Load().Interval = %v, want 30s", cfg.Interval)
	}
	if cfg.DockerSocket != "/custom/docker.sock" {
		t.Errorf("Load().DockerSocket = %q, want yaml value", cfg.DockerSocket)
	}
	if cfg.OracleHome != "/oracle/home" {
		t.Errorf("Load().OracleHome = %q, want /oracle/home", cfg.OracleHome)
	}
	if cfg.OracleSID != "ORCL" {
		t.Errorf("Load().OracleSID = %q, want ORCL", cfg.OracleSID)
	}
	if len(cfg.CollectorsEnabled) != 2 || cfg.CollectorsEnabled[0] != "cpu" || cfg.CollectorsEnabled[1] != "memory" {
		t.Errorf("Load().CollectorsEnabled = %v, want [cpu memory]", cfg.CollectorsEnabled)
	}
	if cfg.ConfigFile != path {
		t.Errorf("Load().ConfigFile = %q, want %q", cfg.ConfigFile, path)
	}
}

func TestLoad_EnvOverridesYAML(t *testing.T) {
	path := writeYAML(t, `
hub_url: "ws://yaml-host/ws/agent"
token: "yaml-token"
docker_socket: "/yaml/docker.sock"
`)
	withArgs(t, []string{"agent", "--config", path})
	t.Setenv("NEXWATCH_HUB_URL", "ws://env-host/ws/agent")
	t.Setenv("NEXWATCH_DOCKER_SOCKET", "/env/docker.sock")

	cfg := Load()

	if cfg.HubURL != "ws://env-host/ws/agent" {
		t.Errorf("Load().HubURL = %q, want env value to win over yaml", cfg.HubURL)
	}
	if cfg.DockerSocket != "/env/docker.sock" {
		t.Errorf("Load().DockerSocket = %q, want env value to win over yaml", cfg.DockerSocket)
	}
	// Token was only set in YAML, not env, so it must survive untouched.
	if cfg.Token != "yaml-token" {
		t.Errorf("Load().Token = %q, want yaml value preserved when env does not set it", cfg.Token)
	}
}

func TestLoad_DockerUpdateChecksFromYAML(t *testing.T) {
	path := writeYAML(t, "docker_update_checks: false\n")
	withArgs(t, []string{"agent", "--config", path})

	cfg := Load()

	if cfg.DockerUpdateChecks {
		t.Error("Load().DockerUpdateChecks = true, want false (yaml value)")
	}
}

func TestLoad_DockerUpdateChecksFromEnv(t *testing.T) {
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t)})
	t.Setenv("NEXWATCH_DOCKER_UPDATE_CHECKS", "false")

	cfg := Load()

	if cfg.DockerUpdateChecks {
		t.Error("Load().DockerUpdateChecks = true, want false (env value)")
	}
}

func TestLoad_DockerUpdateChecksFromFlagOverridesEnv(t *testing.T) {
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t), "--docker-update-checks=false"})
	t.Setenv("NEXWATCH_DOCKER_UPDATE_CHECKS", "true")

	cfg := Load()

	if cfg.DockerUpdateChecks {
		t.Error("Load().DockerUpdateChecks = true, want false (flag value must win over env)")
	}
}

func TestLoad_DockerUpdateChecksUnsetKeepsDefaultTrue(t *testing.T) {
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t)})

	cfg := Load()

	if !cfg.DockerUpdateChecks {
		t.Error("Load().DockerUpdateChecks = false, want true (default) when nothing overrides it")
	}
}

func TestLoad_CveScanEnabledFromYAML(t *testing.T) {
	path := writeYAML(t, "cve_scan_enabled: false\n")
	withArgs(t, []string{"agent", "--config", path})

	cfg := Load()

	if cfg.CveScanEnabled {
		t.Error("Load().CveScanEnabled = true, want false (yaml value)")
	}
}

func TestLoad_CveScanEnabledFromEnv(t *testing.T) {
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t)})
	t.Setenv("NEXWATCH_CVE_SCAN_ENABLED", "false")

	cfg := Load()

	if cfg.CveScanEnabled {
		t.Error("Load().CveScanEnabled = true, want false (env value)")
	}
}

func TestLoad_CveScanEnabledUnsetKeepsDefaultTrue(t *testing.T) {
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t)})

	cfg := Load()

	if !cfg.CveScanEnabled {
		t.Error("Load().CveScanEnabled = false, want true (default) when nothing overrides it")
	}
}

func TestLoad_CveScanIntervalFromYAML(t *testing.T) {
	path := writeYAML(t, "cve_scan_interval: 6h\n")
	withArgs(t, []string{"agent", "--config", path})

	cfg := Load()

	if cfg.CveScanInterval != 6*time.Hour {
		t.Errorf("Load().CveScanInterval = %v, want 6h", cfg.CveScanInterval)
	}
}

func TestLoad_CveScanIntervalFromEnvBareSeconds(t *testing.T) {
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t)})
	t.Setenv("NEXWATCH_CVE_SCAN_INTERVAL", "7200")

	cfg := Load()

	if cfg.CveScanInterval != 2*time.Hour {
		t.Errorf("Load().CveScanInterval = %v, want 2h", cfg.CveScanInterval)
	}
}

func TestLoad_CveScanIntervalBelowFloorClampsToMinimum(t *testing.T) {
	path := writeYAML(t, "cve_scan_interval: 5m\n")
	withArgs(t, []string{"agent", "--config", path})

	cfg := Load()

	if cfg.CveScanInterval != time.Hour {
		t.Errorf("Load().CveScanInterval = %v, want the 1h floor", cfg.CveScanInterval)
	}
}

func TestLoad_CveScanMaxImagesFromYAML(t *testing.T) {
	path := writeYAML(t, "cve_scan_max_images: 25\n")
	withArgs(t, []string{"agent", "--config", path})

	cfg := Load()

	if cfg.CveScanMaxImages != 25 {
		t.Errorf("Load().CveScanMaxImages = %d, want 25", cfg.CveScanMaxImages)
	}
}

func TestLoad_CveScanCacheDirFromEnv(t *testing.T) {
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t)})
	t.Setenv("NEXWATCH_CVE_SCAN_CACHE_DIR", "/opt/nexwatch/cache")

	cfg := Load()

	if cfg.CveScanCacheDir != "/opt/nexwatch/cache" {
		t.Errorf("Load().CveScanCacheDir = %q, want /opt/nexwatch/cache", cfg.CveScanCacheDir)
	}
}

func TestLoad_EnvToken(t *testing.T) {
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t)})
	t.Setenv("NEXWATCH_TOKEN", "env-token")

	cfg := Load()

	if cfg.Token != "env-token" {
		t.Errorf("Load().Token = %q, want env-token", cfg.Token)
	}
}

func TestLoad_EnvOracleVarsAreMapped(t *testing.T) {
	// NEXWATCH_ORACLE_HOME / NEXWATCH_ORACLE_SID are in the env-to-koanf key
	// switch in Load(), matching every other Config field that has both a
	// YAML key and an env var — Oracle fields used to be YAML-only, which
	// this test now pins as fixed.
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t)})
	t.Setenv("NEXWATCH_ORACLE_HOME", "/env/oracle")
	t.Setenv("NEXWATCH_ORACLE_SID", "ENVSID")

	cfg := Load()

	if cfg.OracleHome != "/env/oracle" {
		t.Errorf("Load().OracleHome = %q, want /env/oracle", cfg.OracleHome)
	}
	if cfg.OracleSID != "ENVSID" {
		t.Errorf("Load().OracleSID = %q, want ENVSID", cfg.OracleSID)
	}
}

func TestLoad_OracleFlagsOverrideEnvAndYAML(t *testing.T) {
	path := writeYAML(t, "oracle_home: /yaml/oracle\noracle_sid: YAMLSID\n")
	withArgs(t, []string{
		"agent", "--config", path,
		"--oracle-home", "/flag/oracle",
		"--oracle-sid", "FLAGSID",
	})
	t.Setenv("NEXWATCH_ORACLE_HOME", "/env/oracle")
	t.Setenv("NEXWATCH_ORACLE_SID", "ENVSID")

	cfg := Load()

	if cfg.OracleHome != "/flag/oracle" {
		t.Errorf("Load().OracleHome = %q, want /flag/oracle (flag beats env and YAML)", cfg.OracleHome)
	}
	if cfg.OracleSID != "FLAGSID" {
		t.Errorf("Load().OracleSID = %q, want FLAGSID (flag beats env and YAML)", cfg.OracleSID)
	}
}

func TestLoad_CLIFlagsOverrideEnvAndYAML(t *testing.T) {
	path := writeYAML(t, `
hub_url: "ws://yaml-host/ws/agent"
token: "yaml-token"
interval: 5s
docker_socket: "/yaml/docker.sock"
`)
	withArgs(t, []string{
		"agent",
		"--config", path,
		"--hub", "ws://flag-host/ws/agent",
		"--token", "flag-token",
		"--interval", "77",
		"--docker-socket", "/flag/docker.sock",
	})
	t.Setenv("NEXWATCH_HUB_URL", "ws://env-host/ws/agent")
	t.Setenv("NEXWATCH_TOKEN", "env-token")
	t.Setenv("NEXWATCH_INTERVAL", "55")
	t.Setenv("NEXWATCH_DOCKER_SOCKET", "/env/docker.sock")

	cfg := Load()

	if cfg.HubURL != "ws://flag-host/ws/agent" {
		t.Errorf("Load().HubURL = %q, want flag value to win", cfg.HubURL)
	}
	if cfg.Token != "flag-token" {
		t.Errorf("Load().Token = %q, want flag value to win", cfg.Token)
	}
	if cfg.Interval != 77*time.Second {
		t.Errorf("Load().Interval = %v, want 77s (flag value to win)", cfg.Interval)
	}
	if cfg.DockerSocket != "/flag/docker.sock" {
		t.Errorf("Load().DockerSocket = %q, want flag value to win", cfg.DockerSocket)
	}
}

func TestLoad_UnsetFlagsDoNotOverrideEnv(t *testing.T) {
	// Only --token is passed on the CLI; --hub is left at its flag default
	// but must NOT clobber the env-provided hub_url, because Load() only
	// applies flags that were explicitly visited via fs.Visit.
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t), "--token", "flag-token"})
	t.Setenv("NEXWATCH_HUB_URL", "ws://env-host/ws/agent")

	cfg := Load()

	if cfg.Token != "flag-token" {
		t.Errorf("Load().Token = %q, want flag-token", cfg.Token)
	}
	if cfg.HubURL != "ws://env-host/ws/agent" {
		t.Errorf("Load().HubURL = %q, want env value preserved when flag not explicitly set", cfg.HubURL)
	}
}

func TestLoad_IntervalParsing(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		args     []string
		envKey   string
		envValue string
		want     time.Duration
	}{
		{
			name: "yaml duration string",
			yaml: "interval: 30s\n",
			want: 30 * time.Second,
		},
		{
			name: "yaml bare integer seconds",
			yaml: "interval: 45\n",
			want: 45 * time.Second,
		},
		{
			name:     "env plain integer seconds",
			envKey:   "NEXWATCH_INTERVAL",
			envValue: "55",
			want:     55 * time.Second,
		},
		{
			name:     "env duration string",
			envKey:   "NEXWATCH_INTERVAL",
			envValue: "2m",
			want:     2 * time.Minute,
		},
		{
			name: "flag integer seconds",
			args: []string{"--interval", "77"},
			want: 77 * time.Second,
		},
		{
			name: "sub-second value clamps to 1s minimum",
			args: []string{"--interval", "0"},
			want: 1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configPath := nonexistentConfigPath(t)
			if tt.yaml != "" {
				configPath = writeYAML(t, tt.yaml)
			}
			args := append([]string{"agent", "--config", configPath}, tt.args...)
			withArgs(t, args)
			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envValue)
			}

			cfg := Load()

			if cfg.Interval != tt.want {
				t.Errorf("Load().Interval = %v, want %v", cfg.Interval, tt.want)
			}
		})
	}
}

func TestLoad_CollectorsEnabledFromYAML(t *testing.T) {
	path := writeYAML(t, "collectors_enabled:\n  - docker\n  - ports\n  - processes\n")
	withArgs(t, []string{"agent", "--config", path})

	cfg := Load()

	want := []string{"docker", "ports", "processes"}
	if len(cfg.CollectorsEnabled) != len(want) {
		t.Fatalf("Load().CollectorsEnabled = %v, want %v", cfg.CollectorsEnabled, want)
	}
	for i, c := range want {
		if cfg.CollectorsEnabled[i] != c {
			t.Errorf("Load().CollectorsEnabled[%d] = %q, want %q", i, cfg.CollectorsEnabled[i], c)
		}
	}
}

func TestLoad_MissingConfigFileFallsBackToDefaults(t *testing.T) {
	withArgs(t, []string{"agent", "--config", nonexistentConfigPath(t)})

	cfg := Load()

	if cfg.HubURL != DefaultConfig().HubURL {
		t.Errorf("Load() with missing config file HubURL = %q, want default", cfg.HubURL)
	}
}

func TestLoad_OracleFieldsFromYAML(t *testing.T) {
	path := writeYAML(t, "oracle_home: /u01/app/oracle\noracle_sid: PROD\n")
	withArgs(t, []string{"agent", "--config", path})

	cfg := Load()

	if cfg.OracleHome != "/u01/app/oracle" {
		t.Errorf("Load().OracleHome = %q, want /u01/app/oracle", cfg.OracleHome)
	}
	if cfg.OracleSID != "PROD" {
		t.Errorf("Load().OracleSID = %q, want PROD", cfg.OracleSID)
	}
}
