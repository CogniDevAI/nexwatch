package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// newCheckAlertRuleTypes are the metric_type values this migration adds on
// top of the ones collections.go/rule_types_extension.go already defined.
// Each drives a dedicated evaluation path in the alert engine
// (internal/hub/alerts) that targets "checks" records instead of "agents"
// records:
//
//   - check_down: breaches when the check's latest debounced state (the
//     most recent check_results.status) is "down". Applies to one check
//     when alert_rules.check_id is set, or to every check when it is empty
//     (one alert per breaching check either way).
//   - cert_expiry: breaches when the check's latest recorded
//     tls_expires_at is within alert_rules.threshold days of now. Only
//     meaningful for http checks that recorded a certificate; other checks
//     simply never breach this rule type. threshold is reused as "warn
//     within N days" rather than a metric comparison value.
//
// State-key generalization: the alert engine's ruleAgentKey/updateState
// machinery is keyed by (rule id, subject id) and was written assuming the
// subject is always an agent. Rather than fork a parallel state machine,
// check_down/cert_expiry reuse the exact same keying with the check's
// record ID standing in for the agent ID — see the doc comment on
// ruleAgentKey in internal/hub/alerts/engine.go. Consequently an "alerts"
// row fired by one of these two rule types carries check_id instead of
// agent_id, so alerts.agent_id must become optional (a check-triggered
// alert has no agent at all) and alerts.check_id is added so history can
// link back to the check.
//
// This must run after "checks" (checks.go) and "alert_rules"/"alerts"
// (collections.go) — the filename is deliberately sorted after both
// ("checks.go" and "collections.go" both start with "c", this file starts
// with "r", which is byte-wise greater) so a fresh install applies them in
// the correct order.
func init() {
	newCheckAlertRuleTypes := []string{"check_down", "cert_expiry"}

	m.Register(func(app core.App) error {
		checks, err := app.FindCollectionByNameOrId("checks")
		if err != nil {
			return err
		}

		alertRules, err := app.FindCollectionByNameOrId("alert_rules")
		if err != nil {
			return err
		}

		if field := alertRules.Fields.GetByName("metric_type"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				for _, v := range newCheckAlertRuleTypes {
					if !slices.Contains(selectField.Values, v) {
						selectField.Values = append(selectField.Values, v)
					}
				}
			}
		}

		if alertRules.Fields.GetByName("check_id") == nil {
			alertRules.Fields.Add(&core.RelationField{
				Name:         "check_id",
				CollectionId: checks.Id,
				MaxSelect:    1,
				// Not required — empty means "apply to every check" for
				// check_down/cert_expiry rules; unused by other rule types.
			})
		}
		if err := app.Save(alertRules); err != nil {
			return err
		}

		alerts, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}

		// A check-triggered alert has no agent at all, so agent_id can no
		// longer be required.
		if field := alerts.Fields.GetByName("agent_id"); field != nil {
			if rf, ok := field.(*core.RelationField); ok {
				rf.Required = false
			}
		}
		if alerts.Fields.GetByName("check_id") == nil {
			alerts.Fields.Add(&core.RelationField{
				Name:          "check_id",
				CollectionId:  checks.Id,
				MaxSelect:     1,
				CascadeDelete: true,
			})
		}
		return app.Save(alerts)
	}, func(app core.App) error {
		alertRules, err := app.FindCollectionByNameOrId("alert_rules")
		if err != nil {
			return err
		}
		if field := alertRules.Fields.GetByName("metric_type"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				kept := make([]string, 0, len(selectField.Values))
				for _, v := range selectField.Values {
					if !slices.Contains(newCheckAlertRuleTypes, v) {
						kept = append(kept, v)
					}
				}
				selectField.Values = kept
			}
		}
		alertRules.Fields.RemoveByName("check_id")
		if err := app.Save(alertRules); err != nil {
			return err
		}

		alerts, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		if field := alerts.Fields.GetByName("agent_id"); field != nil {
			if rf, ok := field.(*core.RelationField); ok {
				rf.Required = true
			}
		}
		alerts.Fields.RemoveByName("check_id")
		return app.Save(alerts)
	})
}
