package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration adds acknowledgement fields to "alerts" and escalation
// fields to "alert_rules"/"alerts", so the alert engine (internal/hub/alerts)
// can track whether a firing alert has been acknowledged by an operator and
// whether it has been escalated to a secondary set of notification
// channels. It must run after the "alerts" and "alert_rules" collections
// have been created (collections.go) — the filename is deliberately sorted
// after "collections.go" ("c" < "l" byte-wise) so a fresh install applies
// them in the correct order.
//
//   - alerts.acknowledged_at / alerts.acknowledged_by: set together by
//     POST /api/custom/alerts/{id}/ack (cleared by .../unack). An
//     acknowledged firing alert is not re-notified after cooldown and is
//     not escalated (see engine.updateState). Resolution keeps both fields
//     for history.
//   - alert_rules.escalation_channels / alert_rules.escalation_after: an
//     unacknowledged, unsilenced alert that has been firing for at least
//     escalation_after seconds (0 = disabled) is dispatched once to
//     escalation_channels with an "Escalated:" message prefix.
//   - alerts.escalated_at: set the first time an alert escalates, so a
//     given firing episode escalates at most once.
func init() {
	m.Register(func(app core.App) error {
		alerts, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		if alerts.Fields.GetByName("acknowledged_at") == nil {
			alerts.Fields.Add(&core.DateField{Name: "acknowledged_at"})
		}
		if alerts.Fields.GetByName("acknowledged_by") == nil {
			alerts.Fields.Add(&core.TextField{Name: "acknowledged_by", Max: 255})
		}
		if alerts.Fields.GetByName("escalated_at") == nil {
			alerts.Fields.Add(&core.DateField{Name: "escalated_at"})
		}
		if err := app.Save(alerts); err != nil {
			return err
		}

		notifChannels, err := app.FindCollectionByNameOrId("notification_channels")
		if err != nil {
			return err
		}

		alertRules, err := app.FindCollectionByNameOrId("alert_rules")
		if err != nil {
			return err
		}
		if alertRules.Fields.GetByName("escalation_channels") == nil {
			alertRules.Fields.Add(&core.RelationField{
				Name:         "escalation_channels",
				CollectionId: notifChannels.Id,
				MaxSelect:    10,
			})
		}
		if alertRules.Fields.GetByName("escalation_after") == nil {
			// Seconds a rule must be firing, unacknowledged and unsilenced
			// before it escalates. 0 (the zero value for an optional
			// NumberField) disables escalation for the rule.
			alertRules.Fields.Add(&core.NumberField{Name: "escalation_after"})
		}
		return app.Save(alertRules)
	}, func(app core.App) error {
		alerts, err := app.FindCollectionByNameOrId("alerts")
		if err != nil {
			return err
		}
		alerts.Fields.RemoveByName("acknowledged_at")
		alerts.Fields.RemoveByName("acknowledged_by")
		alerts.Fields.RemoveByName("escalated_at")
		if err := app.Save(alerts); err != nil {
			return err
		}

		alertRules, err := app.FindCollectionByNameOrId("alert_rules")
		if err != nil {
			return err
		}
		alertRules.Fields.RemoveByName("escalation_channels")
		alertRules.Fields.RemoveByName("escalation_after")
		return app.Save(alertRules)
	})
}
