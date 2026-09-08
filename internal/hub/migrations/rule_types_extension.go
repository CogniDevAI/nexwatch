package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// newAlertRuleTypes are the metric_type values added by this migration on
// top of the ones collections.go originally defined. Each drives a
// dedicated evaluation path in the alert engine (internal/hub/alerts) that
// ignores the ordinary getLatestMetricValue/condition/threshold flow:
//
//   - process_down: breaches when no process in the agent's latest
//     "processes" metric snapshot has a name or cmdline containing
//     alert_rules.target (case-insensitive substring).
//   - service_failed: breaches when the agent's latest "services" metric
//     snapshot lists alert_rules.target in a failed/inactive/dead state,
//     or does not list it at all (the services collector already drops
//     services it considers "inactive").
//   - agent_offline: breaches when agents.status = "offline" or last_seen
//     is older than the heartbeat timeout; ignores condition/threshold.
var newAlertRuleTypes = []string{"process_down", "service_failed", "agent_offline"}

// originalAlertRuleMetricTypes is the metric_type Values list exactly as
// collections.go first defined it, used to restore it on downgrade.
var originalAlertRuleMetricTypes = []string{
	"cpu", "memory", "disk", "network", "docker", "sysinfo", "ports",
	"processes", "hardening", "vulnerabilities",
}

// This migration extends alert_rules.metric_type with three new rule types
// and adds alert_rules.target (the process name for process_down, the
// service name for service_failed; unused by other rule types). It must run
// after the "alert_rules" collection has been created (collections.go) —
// the filename is deliberately sorted after "collections.go" ("c" < "r"
// byte-wise) so a fresh install applies them in the correct order.
func init() {
	m.Register(func(app core.App) error {
		alertRules, err := app.FindCollectionByNameOrId("alert_rules")
		if err != nil {
			return err
		}

		field := alertRules.Fields.GetByName("metric_type")
		selectField, ok := field.(*core.SelectField)
		if !ok {
			return nil // schema drifted unexpectedly; leave it alone rather than guess
		}
		for _, v := range newAlertRuleTypes {
			if !slices.Contains(selectField.Values, v) {
				selectField.Values = append(selectField.Values, v)
			}
		}

		if alertRules.Fields.GetByName("target") == nil {
			alertRules.Fields.Add(&core.TextField{Name: "target", Max: 255})
		}

		return app.Save(alertRules)
	}, func(app core.App) error {
		alertRules, err := app.FindCollectionByNameOrId("alert_rules")
		if err != nil {
			return err
		}

		if field := alertRules.Fields.GetByName("metric_type"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				selectField.Values = slices.Clone(originalAlertRuleMetricTypes)
			}
		}
		alertRules.Fields.RemoveByName("target")

		return app.Save(alertRules)
	})
}
