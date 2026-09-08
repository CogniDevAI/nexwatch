package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// This migration creates the "checks" and "check_results" collections for
// black-box monitoring (internal/hub/checks): hub-side HTTP/TCP/ICMP probes
// against arbitrary targets (not necessarily a registered "agents" record),
// with TLS certificate expiry tracking for HTTP checks.
//
// It has no dependency on any other collection (checks are standalone,
// targeting a URL/host:port/hostname rather than an "agents" record), so it
// can run independently of collections.go — but the filename is
// deliberately kept alphabetically self-contained ("checks.go") so it reads
// naturally alongside the later "rule_check_types.go" migration, which DOES
// depend on both this collection and "alert_rules"/"alerts" from
// collections.go and therefore must (and does, "r" > "c" byte-wise) run
// after both.
//
//   - checks: one monitored target. "type" selects which prober runs
//     (internal/hub/checks/{http,tcp,icmp}.go); most fields are
//     type-specific and simply ignored by the other prober types.
//   - check_results: one row per probe attempt. "status" is the
//     *debounced* up/down state (see checks.Scheduler) — it only flips to
//     "down" after failures_before_down consecutive failed attempts, and
//     back to "up" on the first success — while latency_ms/status_code/
//     error/tls_expires_at always reflect that specific attempt. Writes are
//     server-only (no Create/Update/DeleteRule, same convention as the
//     "metrics" collection in collections.go); reads are open to any
//     authenticated user.
func init() {
	authOnly := "@request.auth.id != ''"
	operatorOrAbove := "@request.auth.id != '' && @request.auth.role != 'viewer'"

	m.Register(func(app core.App) error {
		checks := core.NewBaseCollection("checks")
		checks.ListRule = &authOnly
		checks.ViewRule = &authOnly
		checks.CreateRule = &operatorOrAbove
		checks.UpdateRule = &operatorOrAbove
		checks.DeleteRule = &operatorOrAbove
		checks.Fields.Add(
			&core.TextField{Name: "name", Required: true, Max: 255},
			&core.SelectField{
				Name:      "type",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"http", "tcp", "icmp"},
			},
			// URL for http, "host:port" for tcp, a bare hostname/IP for icmp.
			&core.TextField{Name: "target", Required: true, Max: 2048},
			&core.NumberField{Name: "interval_seconds"}, // app-level default 60, min 10
			&core.NumberField{Name: "timeout_seconds"},  // app-level default 5
			&core.SelectField{
				Name:      "method",
				MaxSelect: 1,
				Values:    []string{"GET", "HEAD"},
			}, // http only; app-level default GET
			&core.NumberField{Name: "expected_status"},                 // http only; app-level default 200
			&core.TextField{Name: "expected_body_contains", Max: 1000}, // http only, optional
			&core.BoolField{Name: "verify_tls"},                        // http only; UI defaults the create form to true
			&core.NumberField{Name: "tls_expiry_warn_days"},            // http only; app-level default 14
			&core.NumberField{Name: "failures_before_down"},            // app-level default 2
			&core.BoolField{Name: "enabled"},
			&core.JSONField{Name: "tags", MaxSize: 2000},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)
		checks.Indexes = []string{
			"CREATE INDEX idx_checks_enabled ON checks (enabled)",
			"CREATE INDEX idx_checks_type ON checks (type)",
		}
		if err := app.Save(checks); err != nil {
			return err
		}

		checkResults := core.NewBaseCollection("check_results")
		checkResults.ListRule = &authOnly
		checkResults.ViewRule = &authOnly
		// No Create/Update/DeleteRule: server-only writes via the checks
		// scheduler (app.Save bypasses API rules entirely), matching the
		// "metrics" collection's convention in collections.go.
		checkResults.Fields.Add(
			&core.RelationField{
				Name:          "check_id",
				Required:      true,
				CollectionId:  checks.Id,
				MaxSelect:     1,
				CascadeDelete: true,
			},
			&core.SelectField{
				Name:      "status",
				Required:  true,
				MaxSelect: 1,
				Values:    []string{"up", "down"},
			},
			&core.NumberField{Name: "latency_ms"},
			&core.NumberField{Name: "status_code"},
			&core.TextField{Name: "error", Max: 1000},
			&core.DateField{Name: "tls_expires_at"},
			&core.DateField{Name: "checked_at", Required: true},
		)
		checkResults.Indexes = []string{
			"CREATE INDEX idx_check_results_check_checked ON check_results (check_id, checked_at)",
		}
		return app.Save(checkResults)
	}, func(app core.App) error {
		for _, name := range []string{"check_results", "checks"} {
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
