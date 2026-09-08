package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Adds the "log_match" alert rule metric_type (internal/hub/alerts.Engine.
// evalLogMatch): breaches when the count of an agent's log entries
// (internal/hub/logs, the "logs" collection added by logs.go) matching
// alert_rules.target in the last alert_rules.duration seconds is >=
// alert_rules.threshold. target is either a case-insensitive substring or
// a /regex/ (delimited by leading and trailing slashes). condition is
// intentionally ignored — a log_match rule always breaches on "count >=
// threshold" (semantically fixed to "gte"); the UI hides the Condition
// select for this rule type (see AlertRuleForm.tsx's LOGS_METRIC_TYPES
// group). This reuses the existing "target" field added by
// rule_types_extension.go, so no new field is needed beyond the
// metric_type enum value itself.
//
// Must run after "alert_rules" (collections.go) — filename starts with
// "r", sorting after "collections.go" byte-wise.
func init() {
	const newType = "log_match"

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
		if !slices.Contains(selectField.Values, newType) {
			selectField.Values = append(selectField.Values, newType)
		}
		return app.Save(alertRules)
	}, func(app core.App) error {
		alertRules, err := app.FindCollectionByNameOrId("alert_rules")
		if err != nil {
			return err
		}
		if field := alertRules.Fields.GetByName("metric_type"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				kept := make([]string, 0, len(selectField.Values))
				for _, v := range selectField.Values {
					if v != newType {
						kept = append(kept, v)
					}
				}
				selectField.Values = kept
			}
		}
		return app.Save(alertRules)
	})
}
