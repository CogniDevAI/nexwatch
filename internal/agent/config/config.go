package config

import (
	"flag"
	"log"
	"os"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	kflag "github.com/knadh/koanf/providers/basicflag"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config holds all agent configuration.
type Config struct {
	HubURL            string        `koanf:"hub_url"`
	Token             string        `koanf:"token"`
	Interval          time.Duration `koanf:"interval"`
	CollectorsEnabled []string      `koanf:"collectors_enabled"`
	DockerSocket      string        `koanf:"docker_socket"`
	OracleHome        string        `koanf:"oracle_home"`
	OracleSID         string        `koanf:"oracle_sid"`
	ConfigFile        string        `koanf:"-"` // not loaded from config file itself
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		HubURL:   "ws://localhost:8090/ws/agent",
		Token:    "",
		Interval: 10 * time.Second,
		CollectorsEnabled: []string{
			"cpu", "memory", "disk", "network", "sysinfo", "docker",
			"ports", "processes", "hardening", "vulnerabilities",
			"diskio", "connections", "services",
		},
		DockerSocket: "/var/run/docker.sock",
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

	// Parse os.Args (skip program name).
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Printf("[config] flag parse error: %v", err)
	}

	// Determine config file path.
	configFile := fs.Lookup("config").Value.String()
	if configFile == "" {
		// Try default paths.
		for _, p := range []string{"./agent.yaml", "/etc/nexwatch/agent.yaml"} {
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
		default:
			return ""
		}
	}), nil); err != nil {
		log.Printf("[config] env load error: %v", err)
	}

	// 3. Load from CLI flags (highest priority).
	// Map flag names to koanf keys.
	flagMap := map[string]string{
		"hub":           "hub_url",
		"token":         "token",
		"interval":      "interval",
		"docker-socket": "docker_socket",
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
