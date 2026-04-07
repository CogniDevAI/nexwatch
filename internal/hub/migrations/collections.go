package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

func init() {
	// authOnly is a PocketBase API rule that allows any authenticated user.
	authOnly := "@request.auth.id != ''"

	m.Register(func(app core.App) error {
		// ---------------------------------------------------------------
		// Collection: notification_channels (created first — referenced by alert_rules)
		// ---------------------------------------------------------------
		notifChannels := core.NewBaseCollection("notification_channels")
		notifChannels.ListRule = &authOnly
		notifChannels.ViewRule = &authOnly
		notifChannels.CreateRule = &authOnly
		notifChannels.UpdateRule = &authOnly
		notifChannels.DeleteRule = &authOnly
		notifChannels.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 255},
			&core.SelectField{
				Name:      "type",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"email", "webhook", "telegram", "discord"},
			},
			&core.JSONField{Name: "config", MaxSize: 10000},
			&core.BoolField{Name: "enabled"},
		)
		notifChannels.Indexes = []string{
			"CREATE INDEX idx_notification_channels_type ON notification_channels (type)",
		}
		if err := app.Save(notifChannels); err != nil {
			return err
		}

		// ---------------------------------------------------------------
		// Collection: agents
		// ---------------------------------------------------------------
		agents := core.NewBaseCollection("agents")
		agents.ListRule = &authOnly
		agents.ViewRule = &authOnly
		agents.CreateRule = &authOnly
		agents.UpdateRule = &authOnly
		agents.DeleteRule = &authOnly
		agents.Fields.Add(
			&core.TextField{Name: "hostname", Required: true, Max: 255},
			&core.TextField{Name: "os", Max: 100},
			&core.TextField{Name: "ip", Max: 45},
			&core.TextField{Name: "version", Max: 50},
			&core.SelectField{
				Name:      "status",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"online", "offline", "pending"},
			},
			&core.DateField{Name: "last_seen"},
			&core.TextField{Name: "token", Required: true, Max: 255},
		)
		agents.Indexes = []string{
			"CREATE UNIQUE INDEX idx_agents_token ON agents (token)",
			"CREATE INDEX idx_agents_status ON agents (status)",
			"CREATE INDEX idx_agents_hostname ON agents (hostname)",
		}
		if err := app.Save(agents); err != nil {
			return err
		}

		// ---------------------------------------------------------------
		// Collection: metrics
		// ---------------------------------------------------------------
		metrics := core.NewBaseCollection("metrics")
		metrics.ListRule = &authOnly
		metrics.ViewRule = &authOnly
		metrics.Fields.Add(
			&core.RelationField{
				Name:          "agent_id",
				Required:      true,
				CollectionId:  agents.Id,
				MaxSelect:     1,
				CascadeDelete: true,
			},
			&core.SelectField{
				Name:      "type",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"cpu", "memory", "disk", "network", "docker", "sysinfo", "ports", "processes", "hardening", "vulnerabilities", "diskio", "connections", "services", "oracle"},
			},
			&core.JSONField{Name: "data", MaxSize: 200000},
			&core.DateField{Name: "timestamp", Required: true},
			&core.SelectField{
				Name:      "resolution",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"raw", "1m", "5m", "1h"},
			},
		)
		metrics.Indexes = []string{
			"CREATE INDEX idx_metrics_agent_type_ts ON metrics (agent_id, type, timestamp)",
			"CREATE INDEX idx_metrics_resolution_ts ON metrics (resolution, timestamp)",
			"CREATE INDEX idx_metrics_ts ON metrics (timestamp)",
		}
		if err := app.Save(metrics); err != nil {
			return err
		}

		// ---------------------------------------------------------------
		// Collection: docker_containers
		// ---------------------------------------------------------------
		dockerContainers := core.NewBaseCollection("docker_containers")
		dockerContainers.ListRule = &authOnly
		dockerContainers.ViewRule = &authOnly
		dockerContainers.Fields.Add(
			&core.RelationField{
				Name:          "agent_id",
				Required:      true,
				CollectionId:  agents.Id,
				MaxSelect:     1,
				CascadeDelete: true,
			},
			&core.TextField{Name: "container_id", Required: true, Max: 64},
			&core.TextField{Name: "name", Required: true, Max: 255},
			&core.TextField{Name: "image", Max: 512},
			&core.TextField{Name: "status", Max: 100},
			&core.NumberField{Name: "cpu_percent"},
			&core.NumberField{Name: "memory_usage"},
			&core.NumberField{Name: "memory_limit"},
			&core.NumberField{Name: "network_rx"},
			&core.NumberField{Name: "network_tx"},
			&core.DateField{Name: "updated_at"},
		)
		dockerContainers.Indexes = []string{
			"CREATE INDEX idx_docker_agent ON docker_containers (agent_id)",
			"CREATE UNIQUE INDEX idx_docker_agent_container ON docker_containers (agent_id, container_id)",
		}
		if err := app.Save(dockerContainers); err != nil {
			return err
		}

		// ---------------------------------------------------------------
		// Collection: alert_rules
		// ---------------------------------------------------------------
		alertRules := core.NewBaseCollection("alert_rules")
		alertRules.ListRule = &authOnly
		alertRules.ViewRule = &authOnly
		alertRules.CreateRule = &authOnly
		alertRules.UpdateRule = &authOnly
		alertRules.DeleteRule = &authOnly
		alertRules.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 255},
			&core.SelectField{
				Name:      "metric_type",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"cpu", "memory", "disk", "network", "docker", "sysinfo", "ports", "processes", "hardening", "vulnerabilities"},
			},
			&core.SelectField{
				Name:      "condition",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"gt", "lt", "eq"},
			},
			&core.NumberField{Name: "threshold", Required: true},
			&core.NumberField{Name: "duration", Required: true}, // seconds
			&core.SelectField{
				Name:      "severity",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"warning", "critical"},
			},
			&core.RelationField{
				Name:         "agent_id",
				CollectionId: agents.Id,
				MaxSelect:    1,
				// Not required — empty means "apply to all agents"
			},
			&core.BoolField{Name: "enabled"},
			&core.RelationField{
				Name:         "notification_channels",
				CollectionId: notifChannels.Id,
				MaxSelect:    10,
			},
		)
		alertRules.Indexes = []string{
			"CREATE INDEX idx_alert_rules_enabled ON alert_rules (enabled)",
			"CREATE INDEX idx_alert_rules_metric ON alert_rules (metric_type)",
		}
		if err := app.Save(alertRules); err != nil {
			return err
		}

		// ---------------------------------------------------------------
		// Collection: alerts
		// ---------------------------------------------------------------
		alerts := core.NewBaseCollection("alerts")
		alerts.ListRule = &authOnly
		alerts.ViewRule = &authOnly
		alerts.Fields.Add(
			&core.RelationField{
				Name:          "rule_id",
				Required:      true,
				CollectionId:  alertRules.Id,
				MaxSelect:     1,
				CascadeDelete: true,
			},
			&core.RelationField{
				Name:          "agent_id",
				Required:      true,
				CollectionId:  agents.Id,
				MaxSelect:     1,
				CascadeDelete: true,
			},
			&core.SelectField{
				Name:      "status",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"firing", "resolved"},
			},
			&core.NumberField{Name: "value"},
			&core.TextField{Name: "message", Max: 1000},
			&core.DateField{Name: "fired_at", Required: true},
			&core.DateField{Name: "resolved_at"},
		)
		alerts.Indexes = []string{
			"CREATE INDEX idx_alerts_rule ON alerts (rule_id)",
			"CREATE INDEX idx_alerts_agent ON alerts (agent_id)",
			"CREATE INDEX idx_alerts_status ON alerts (status)",
			"CREATE INDEX idx_alerts_fired ON alerts (fired_at)",
		}
		if err := app.Save(alerts); err != nil {
			return err
		}

		// ---------------------------------------------------------------
		// Collection: settings
		// ---------------------------------------------------------------
		settings := core.NewBaseCollection("settings")
		settings.ListRule = &authOnly
		settings.ViewRule = &authOnly
		settings.CreateRule = &authOnly
		settings.UpdateRule = &authOnly
		settings.DeleteRule = &authOnly
		settings.Fields.Add(
			&core.TextField{Name: "key", Required: true, Max: 255},
			&core.JSONField{Name: "value", MaxSize: 50000},
		)
		settings.Indexes = []string{
			"CREATE UNIQUE INDEX idx_settings_key ON settings (key)",
		}
		if err := app.Save(settings); err != nil {
			return err
		}

		return nil
	}, func(app core.App) error {
		// Down migration: drop collections in reverse order
		collections := []string{
			"settings",
			"alerts",
			"alert_rules",
			"docker_containers",
			"metrics",
			"agents",
			"notification_channels",
		}
		for _, name := range collections {
			col, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue // already gone
			}
			if err := app.Delete(col); err != nil {
				return err
			}
		}
		return nil
	})
}
