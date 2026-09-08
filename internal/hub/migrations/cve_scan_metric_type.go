package migrations

import (
	"slices"

	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration adds the "cve_scan" metrics.type value — the agent-side
// CVE scanner payload produced by internal/agent/collector/cvescan.go —
// and the "cve_count" alert_rules.metric_type value, which breaches when
// the latest cve_scan totals for the agent satisfy condition/threshold
// against critical+high severity findings (or critical only, when
// alert_rules.target == "critical" — see internal/hub/alerts/engine.go's
// evalCveCount). It must run after "collections.go" (which creates both
// "metrics" and "alert_rules") — the filename is deliberately sorted after
// it byte-wise ("collections.go" vs "cve_scan_metric_type.go": both start
// with "c", the second byte 'o' < 'v') so a fresh install applies them in
// order.
func init() {
	m.Register(func(app core.App) error {
		metrics, err := app.FindCollectionByNameOrId("metrics")
		if err != nil {
			return err
		}
		if field := metrics.Fields.GetByName("type"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				if !slices.Contains(selectField.Values, "cve_scan") {
					selectField.Values = append(selectField.Values, "cve_scan")
				}
			}
		}
		if err := app.Save(metrics); err != nil {
			return err
		}

		alertRules, err := app.FindCollectionByNameOrId("alert_rules")
		if err != nil {
			return err
		}
		if field := alertRules.Fields.GetByName("metric_type"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				if !slices.Contains(selectField.Values, "cve_count") {
					selectField.Values = append(selectField.Values, "cve_count")
				}
			}
		}
		return app.Save(alertRules)
	}, func(app core.App) error {
		metrics, err := app.FindCollectionByNameOrId("metrics")
		if err != nil {
			return err
		}
		if field := metrics.Fields.GetByName("type"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				kept := make([]string, 0, len(selectField.Values))
				for _, v := range selectField.Values {
					if v != "cve_scan" {
						kept = append(kept, v)
					}
				}
				selectField.Values = kept
			}
		}
		if err := app.Save(metrics); err != nil {
			return err
		}

		alertRules, err := app.FindCollectionByNameOrId("alert_rules")
		if err != nil {
			return err
		}
		if field := alertRules.Fields.GetByName("metric_type"); field != nil {
			if selectField, ok := field.(*core.SelectField); ok {
				kept := make([]string, 0, len(selectField.Values))
				for _, v := range selectField.Values {
					if v != "cve_count" {
						kept = append(kept, v)
					}
				}
				selectField.Values = kept
			}
		}
		return app.Save(alertRules)
	})
}
