package config

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	kflag "github.com/knadh/koanf/providers/basicflag"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config holds all agent configuration.
type Config struct {
	HubURL             string        `koanf:"hub_url"`
	Token              string        `koanf:"token"`
	Interval           time.Duration `koanf:"interval"`
	CollectorsEnabled  []string      `koanf:"collectors_enabled"`
	DockerSocket       string        `koanf:"docker_socket"`
	DockerUpdateChecks bool          `koanf:"docker_update_checks"`
	OracleHome         string        `koanf:"oracle_home"`
	OracleSID          string        `koanf:"oracle_sid"`
	// CveScanEnabled toggles the cve_scan collector's actual scanning
	// (detecting/running trivy or grype) — the collector itself is
	// registered whenever "cve_scan" is in CollectorsEnabled; this flag
	// separately lets an operator keep the collector present (so the
	// hub/UI see a clear "disabled" state) without it ever shelling out.
	CveScanEnabled bool `koanf:"cve_scan_enabled"`
	// CveScanInterval is how often the background scan re-runs. Enforced
	// to be at least MinCveScanInterval (1h) — see collector.NewCveScanCollector.
	CveScanInterval time.Duration `koanf:"cve_scan_interval"`
	// CveScanMaxImages caps how many unique running-container images are
	// scanned per run, on top of the host filesystem scan.
	CveScanMaxImages int `koanf:"cve_scan_max_images"`
	// CveScanCacheDir is exported to the scanner subprocess as
	// TRIVY_CACHE_DIR/GRYPE_DB_CACHE_DIR so its vulnerability database
	// persists across scans instead of re-downloading every run.
	CveScanCacheDir string `koanf:"cve_scan_cache_dir"`

	// LogsEnabled toggles log shipping (internal/agent/logs) entirely.
	LogsEnabled bool `koanf:"logs_enabled"`
	// LogsMaxLinesPerSec caps how many log lines per second are forwarded
	// to the hub; anything past this is dropped (and counted) rather than
	// queued, so a noisy source can never overwhelm the send queue.
	LogsMaxLinesPerSec int `koanf:"logs_max_lines_per_sec"`
	// LogSources lists where to read logs from. When empty (the default),
	// Load resolves it to one journald source covering every unit up to
	// "warning" priority if the journalctl binary is present on PATH, or
	// no sources at all otherwise (see resolveDefaultLogSources).
	LogSources []LogSource `koanf:"log_sources"`

	// AutoUpdateEnabled gates whether this agent accepts a hub-initiated
	// "update" COMMAND at all (internal/agent/update). When false, the
	// agent immediately refuses the command with a clear error instead of
	// downloading anything — see cmd/agent/main.go's handleUpdateCommand.
	AutoUpdateEnabled bool `koanf:"auto_update_enabled"`
	// UpdateRequireSignature, when true, fails a self-update unless the
	// release's SHA256SUMS carries a GPG signature that verifies —
	// mirrors scripts/install-agent.sh's --require-signature.
	UpdateRequireSignature bool `koanf:"update_require_signature"`
	// UpdateSigningKeyURL overrides the URL the release signing public key
	// is fetched from. Defaults to the same raw GitHub URL
	// scripts/install-agent.sh uses (see update.DefaultSigningKeyURL).
	UpdateSigningKeyURL string `koanf:"update_signing_key_url"`
	// UpdateSigningKeyFile, when set, reads the signing public key from a
	// local file instead of fetching UpdateSigningKeyURL — useful for an
	// air-gapped host mirroring releases internally.
	UpdateSigningKeyFile string `koanf:"update_signing_key_file"`

	ConfigFile string `koanf:"-"` // not loaded from config file itself
}

// LogSource configures one place internal/agent/logs reads log lines from.
type LogSource struct {
	// Type is "journald" or "file".
	Type string `koanf:"type"`
	// Path is the file to tail. Only used when Type is "file".
	Path string `koanf:"path"`
	// Units restricts a journald source to these systemd unit names.
	// Empty means every unit.
	Units []string `koanf:"units"`
	// PriorityMax caps a journald source to this syslog priority and more
	// severe ones (e.g. "warning" includes emerg/alert/crit/err/warning
	// but not notice/info/debug). Empty means every priority. Only used
	// when Type is "journald".
	PriorityMax string `koanf:"priority_max"`
}

// DefaultConfig returns a Config with sensible defaults. Several fields —
// CollectorsEnabled, DockerSocket, and CveScanCacheDir — differ by GOOS
// (see defaultCollectorsEnabled/defaultDockerSocket/defaultCveScanCacheDir
// in config_unix.go/config_windows.go), since a Linux path or the
// Linux-only oracle/vulnerabilities collectors make no sense as a Windows
// default. UpdateSigningKeyFile is deliberately left unset on every
// platform: it opts an agent into trusting a local key file instead of
// fetching UpdateSigningKeyURL, which only makes sense for an operator who
// explicitly configures an air-gapped mirror.
func DefaultConfig() *Config {
	return &Config{
		HubURL:             "ws://localhost:8090/ws/agent",
		Token:              "",
		Interval:           10 * time.Second,
		CollectorsEnabled:  defaultCollectorsEnabled(),
		DockerSocket:       defaultDockerSocket(),
		DockerUpdateChecks: true,
		CveScanEnabled:     true,
		CveScanInterval:    12 * time.Hour,
		CveScanMaxImages:   10,
		CveScanCacheDir:    defaultCveScanCacheDir(),
		LogsEnabled:        true,
		LogsMaxLinesPerSec: 200,
		AutoUpdateEnabled:  true,
		// Matches update.DefaultSigningKeyURL and
		// scripts/install-agent.sh's own DEFAULT_SIGNING_KEY_URL — kept as
		// a literal here (rather than importing internal/agent/update just
		// for this constant) the same way install-agent.sh keeps its own
		// copy.
		UpdateSigningKeyURL: "https://raw.githubusercontent.com/CogniDevAI/nexwatch/main/scripts/release-signing-key.asc",
	}
}

// Load reads configuration with the following priority (highest wins):
//
//	CLI flags > environment variables > config file > defaults
func Load() *Config {
	cfg := DefaultConfig()

	// Parse CLI flags first to get --config path.
	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	fs.String("hub", cfg.HubURL, "Hub WebSocket URL")
	fs.String("token", cfg.Token, "Agent authentication token")
	fs.Int("interval", int(cfg.Interval.Seconds()), "Collection interval in seconds")
	fs.String("config", "", "Path to config file (YAML)")
	fs.String("docker-socket", cfg.DockerSocket, "Docker socket path")
	fs.Bool("docker-update-checks", cfg.DockerUpdateChecks, "Check container images for available registry updates (default true)")
	fs.Bool("auto-update-enabled", cfg.AutoUpdateEnabled, "Accept hub-initiated self-update commands (default true)")
	fs.Bool("update-require-signature", cfg.UpdateRequireSignature, "Refuse a self-update unless its release signature verifies")
	fs.String("update-signing-key-url", cfg.UpdateSigningKeyURL, "URL to fetch the release signing public key from")
	fs.String("update-signing-key-file", cfg.UpdateSigningKeyFile, "Local file with the release signing public key")
	fs.String("oracle-home", cfg.OracleHome, "ORACLE_HOME path for the oracle collector (Linux/Unix only)")
	fs.String("oracle-sid", cfg.OracleSID, "Oracle SID for the oracle collector (Linux/Unix only)")

	// Parse os.Args (skip program name).
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Printf("[config] flag parse error: %v", err)
	}

	// Determine config file path.
	configFile := fs.Lookup("config").Value.String()
	if configFile == "" {
		// Try default paths: "./agent.yaml" everywhere, then the
		// OS-appropriate system config path (see
		// defaultSystemConfigPath in config_unix.go/config_windows.go).
		for _, p := range []string{"./agent.yaml", defaultSystemConfigPath()} {
			if _, err := os.Stat(p); err == nil {
				configFile = p
				break
			}
		}
	}

	k := koanf.New(".")

	// 1. Load from YAML config file (lowest priority after defaults).
	if configFile != "" {
		if err := k.Load(file.Provider(configFile), yaml.Parser()); err != nil {
			log.Printf("[config] config file %s: %v (skipping)", configFile, err)
		} else {
			log.Printf("[config] loaded config from %s", configFile)
		}
	}

	// 2. Load from environment variables (NEXWATCH_ prefix).
	if err := k.Load(env.Provider("NEXWATCH_", ".", func(s string) string {
		// NEXWATCH_HUB_URL -> hub_url, NEXWATCH_TOKEN -> token, etc.
		switch s {
		case "NEXWATCH_HUB_URL":
			return "hub_url"
		case "NEXWATCH_TOKEN":
			return "token"
		case "NEXWATCH_INTERVAL":
			return "interval"
		case "NEXWATCH_DOCKER_SOCKET":
			return "docker_socket"
		case "NEXWATCH_DOCKER_UPDATE_CHECKS":
			return "docker_update_checks"
		case "NEXWATCH_CVE_SCAN_ENABLED":
			return "cve_scan_enabled"
		case "NEXWATCH_CVE_SCAN_INTERVAL":
			return "cve_scan_interval"
		case "NEXWATCH_CVE_SCAN_MAX_IMAGES":
			return "cve_scan_max_images"
		case "NEXWATCH_CVE_SCAN_CACHE_DIR":
			return "cve_scan_cache_dir"
		case "NEXWATCH_LOGS_ENABLED":
			return "logs_enabled"
		case "NEXWATCH_LOGS_MAX_LINES_PER_SEC":
			return "logs_max_lines_per_sec"
		case "NEXWATCH_AUTO_UPDATE_ENABLED":
			return "auto_update_enabled"
		case "NEXWATCH_UPDATE_REQUIRE_SIGNATURE":
			return "update_require_signature"
		case "NEXWATCH_UPDATE_SIGNING_KEY_URL":
			return "update_signing_key_url"
		case "NEXWATCH_UPDATE_SIGNING_KEY_FILE":
			return "update_signing_key_file"
		case "NEXWATCH_ORACLE_HOME":
			return "oracle_home"
		case "NEXWATCH_ORACLE_SID":
			return "oracle_sid"
		default:
			return ""
		}
	}), nil); err != nil {
		log.Printf("[config] env load error: %v", err)
	}

	// 3. Load from CLI flags (highest priority).
	// Map flag names to koanf keys.
	flagMap := map[string]string{
		"hub":                      "hub_url",
		"token":                    "token",
		"interval":                 "interval",
		"docker-socket":            "docker_socket",
		"docker-update-checks":     "docker_update_checks",
		"auto-update-enabled":      "auto_update_enabled",
		"update-require-signature": "update_require_signature",
		"update-signing-key-url":   "update_signing_key_url",
		"update-signing-key-file":  "update_signing_key_file",
		"oracle-home":              "oracle_home",
		"oracle-sid":               "oracle_sid",
	}

	// Only apply flags that were explicitly set.
	setFlags := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		setFlags[f.Name] = true
	})

	if len(setFlags) > 0 {
		if err := k.Load(kflag.ProviderWithValue(fs, ".", func(key string, value string) (string, any) {
			mappedKey, ok := flagMap[key]
			if !ok || !setFlags[key] {
				return "", nil
			}
			return mappedKey, value
		}), nil); err != nil {
			log.Printf("[config] flag load error: %v", err)
		}
	}

	// Unmarshal merged config.
	if err := k.Unmarshal("", cfg); err != nil {
		log.Printf("[config] unmarshal error: %v", err)
	}

	// Debug oracle config.
	if cfg.OracleHome != "" || cfg.OracleSID != "" {
		log.Printf("[config] oracle_home=%s oracle_sid=%s", cfg.OracleHome, cfg.OracleSID)
	} else {
		// Try reading directly from koanf in case struct tags didn't map.
		if v := k.String("oracle_home"); v != "" {
			cfg.OracleHome = v
		}
		if v := k.String("oracle_sid"); v != "" {
			cfg.OracleSID = v
		}
		if cfg.OracleHome != "" {
			log.Printf("[config] oracle_home=%s oracle_sid=%s (recovered from koanf)", cfg.OracleHome, cfg.OracleSID)
		}
	}

	// Handle interval: koanf may load it as an int (seconds) from flags/env.
	if raw := k.Get("interval"); raw != nil {
		switch v := raw.(type) {
		case string:
			if d, err := time.ParseDuration(v); err == nil {
				cfg.Interval = d
			} else {
				// Try parsing as seconds.
				var secs int
				if _, err := parseIntFromStr(v, &secs); err == nil {
					cfg.Interval = time.Duration(secs) * time.Second
				}
			}
		case int:
			cfg.Interval = time.Duration(v) * time.Second
		case int64:
			cfg.Interval = time.Duration(v) * time.Second
		case float64:
			cfg.Interval = time.Duration(int(v)) * time.Second
		}
	}

	// Ensure interval is at least 1 second.
	if cfg.Interval < time.Second {
		cfg.Interval = time.Second
	}

	// Handle docker_update_checks: koanf may load it as a string ("true"/
	// "false") from env or CLI flags rather than a native bool, the same
	// way "interval" needs manual handling above.
	if raw := k.Get("docker_update_checks"); raw != nil {
		switch v := raw.(type) {
		case bool:
			cfg.DockerUpdateChecks = v
		case string:
			if b, err := strconv.ParseBool(v); err == nil {
				cfg.DockerUpdateChecks = b
			}
		}
	}

	// Handle auto_update_enabled the same way as docker_update_checks above.
	if raw := k.Get("auto_update_enabled"); raw != nil {
		switch v := raw.(type) {
		case bool:
			cfg.AutoUpdateEnabled = v
		case string:
			if b, err := strconv.ParseBool(v); err == nil {
				cfg.AutoUpdateEnabled = b
			}
		}
	}

	// Handle update_require_signature the same way as docker_update_checks above.
	if raw := k.Get("update_require_signature"); raw != nil {
		switch v := raw.(type) {
		case bool:
			cfg.UpdateRequireSignature = v
		case string:
			if b, err := strconv.ParseBool(v); err == nil {
				cfg.UpdateRequireSignature = b
			}
		}
	}

	// Handle cve_scan_enabled the same way as docker_update_checks above.
	if raw := k.Get("cve_scan_enabled"); raw != nil {
		switch v := raw.(type) {
		case bool:
			cfg.CveScanEnabled = v
		case string:
			if b, err := strconv.ParseBool(v); err == nil {
				cfg.CveScanEnabled = b
			}
		}
	}

	// Handle cve_scan_interval the same way as "interval" above — koanf may
	// load it as a duration string, a bare number of seconds (string or
	// numeric), from a YAML file, env var, or flag.
	if raw := k.Get("cve_scan_interval"); raw != nil {
		switch v := raw.(type) {
		case string:
			if d, err := time.ParseDuration(v); err == nil {
				cfg.CveScanInterval = d
			} else {
				var secs int
				if _, err := parseIntFromStr(v, &secs); err == nil {
					cfg.CveScanInterval = time.Duration(secs) * time.Second
				}
			}
		case int:
			cfg.CveScanInterval = time.Duration(v) * time.Second
		case int64:
			cfg.CveScanInterval = time.Duration(v) * time.Second
		case float64:
			cfg.CveScanInterval = time.Duration(int(v)) * time.Second
		}
	}
	// Enforce a floor so a misconfigured value can't hammer the scanner
	// (and, by extension, the registries/DB it hits) every cycle.
	if cfg.CveScanInterval < time.Hour {
		cfg.CveScanInterval = time.Hour
	}

	if raw := k.Get("cve_scan_max_images"); raw != nil {
		switch v := raw.(type) {
		case string:
			var n int
			if _, err := parseIntFromStr(v, &n); err == nil {
				cfg.CveScanMaxImages = n
			}
		case int:
			cfg.CveScanMaxImages = v
		case int64:
			cfg.CveScanMaxImages = int(v)
		case float64:
			cfg.CveScanMaxImages = int(v)
		}
	}
	if cfg.CveScanMaxImages <= 0 {
		cfg.CveScanMaxImages = 10
	}

	// Handle logs_enabled the same way as docker_update_checks above.
	if raw := k.Get("logs_enabled"); raw != nil {
		switch v := raw.(type) {
		case bool:
			cfg.LogsEnabled = v
		case string:
			if b, err := strconv.ParseBool(v); err == nil {
				cfg.LogsEnabled = b
			}
		}
	}

	// Handle logs_max_lines_per_sec the same way as cve_scan_max_images above.
	if raw := k.Get("logs_max_lines_per_sec"); raw != nil {
		switch v := raw.(type) {
		case string:
			var n int
			if _, err := parseIntFromStr(v, &n); err == nil {
				cfg.LogsMaxLinesPerSec = n
			}
		case int:
			cfg.LogsMaxLinesPerSec = v
		case int64:
			cfg.LogsMaxLinesPerSec = int(v)
		case float64:
			cfg.LogsMaxLinesPerSec = int(v)
		}
	}
	if cfg.LogsMaxLinesPerSec <= 0 {
		cfg.LogsMaxLinesPerSec = 200
	}

	// An empty log_sources list (the common case — nothing set in the
	// config file) resolves to one journald source covering every unit up
	// to "warning" priority, but only when journalctl is actually present:
	// shipping nothing is a much better default than an agent that spams
	// its own log with "journalctl: command not found" every cycle on a
	// host with no systemd journal.
	if len(cfg.LogSources) == 0 {
		if _, err := exec.LookPath("journalctl"); err == nil {
			cfg.LogSources = []LogSource{{Type: "journald", PriorityMax: "warning"}}
		}
	}

	cfg.ConfigFile = configFile

	return cfg
}

// parseIntFromStr is a simple int parser from string.
func parseIntFromStr(s string, target *int) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		n = n*10 + int(c-'0')
	}
	*target = n
	return n, nil
}
