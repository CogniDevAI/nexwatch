package api

import (
	"math"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/CogniDevAI/nexwatch/internal/hub/audit"
	"github.com/CogniDevAI/nexwatch/internal/hub/checks"
)

// CheckRunner is the subset of checks.Scheduler the API needs: running an
// on-demand probe and reading the in-memory latest-result snapshot.
// Declared as an interface here (rather than importing *checks.Scheduler
// directly into handler signatures) so route tests can inject a fake.
type CheckRunner interface {
	RunOnce(checkID string) (*core.Record, error)
	Snapshot() map[string]checks.Snapshot
}

// RegisterChecksRoutes registers black-box monitoring API routes on
// apiGroup, which the caller must already have bound with the desired auth
// middleware (e.g. apis.RequireAuth()) and mounted at the "/api/custom"
// prefix. It returns every route it registered, for openapi_test.go to
// cross-check against docs/openapi.yaml.
func RegisterChecksRoutes(apiGroup *router.RouterGroup[*core.RequestEvent], runner CheckRunner) []RegisteredRoute {
	rec := newRouteRecorder(apiGroup, "/api/custom")

	// GET /api/custom/checks/summary — every check with its current status,
	// latency, TLS expiry, and 24h/7d uptime.
	rec.GET("/checks/summary", func(e *core.RequestEvent) error {
		return handleChecksSummary(e, runner)
	})

	// POST /api/custom/checks/{id}/run — run a check now, on demand.
	rec.POST("/checks/{id}/run", func(e *core.RequestEvent) error {
		return handleRunCheck(e, runner)
	}).Bind(RequireRole(RoleOperator))

	// GET /api/custom/checks/{id}/results — latency/status time series.
	rec.GET("/checks/{id}/results", handleCheckResults)

	return rec.Registered
}

func roundTo2(v float64) float64 {
	return math.Round(v*100) / 100
}

// checkSummaryEntry is one row of GET /api/custom/checks/summary.
type checkSummaryEntry struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Type          string  `json:"type"`
	Target        string  `json:"target"`
	Status        string  `json:"status,omitempty"`
	LatencyMs     float64 `json:"latency_ms"`
	LastCheckedAt string  `json:"last_checked_at,omitempty"`
	TLSExpiresAt  string  `json:"tls_expires_at,omitempty"`
	// CertExpiringSoon is true when TLSExpiresAt is within the check's own
	// tls_expiry_warn_days of now — computed server-side so the UI doesn't
	// need to separately fetch each check's raw config just to render the
	// warning badge.
	CertExpiringSoon bool    `json:"cert_expiring_soon"`
	Uptime24h        float64 `json:"uptime_24h"`
	Uptime7d         float64 `json:"uptime_7d"`
	Enabled          bool    `json:"enabled"`
}

// handleChecksSummary lists every "checks" record with its current status.
// Current-state fields (status/latency/last_checked_at/tls_expires_at)
// prefer the scheduler's in-memory Snapshot (no DB round trip); a check
// with no snapshot entry yet — e.g. right after a hub restart, before it
// has run again — falls back to its most recently persisted
// "check_results" row so the summary is never blank for an existing check.
// Uptime percentages always come from a "check_results" query since they
// aggregate over a time window the in-memory snapshot doesn't retain.
func handleChecksSummary(e *core.RequestEvent, runner CheckRunner) error {
	checkRecords, err := e.App.FindRecordsByFilter("checks", "id != ''", "name", 1000, 0)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to fetch checks",
		})
	}

	snapshot := runner.Snapshot()
	entries := make([]checkSummaryEntry, 0, len(checkRecords))

	for _, c := range checkRecords {
		entry := checkSummaryEntry{
			ID:      c.Id,
			Name:    c.GetString("name"),
			Type:    c.GetString("type"),
			Target:  c.GetString("target"),
			Enabled: c.GetBool("enabled"),
		}

		var tlsExpiresAt time.Time
		if snap, ok := snapshot[c.Id]; ok {
			entry.Status = snap.Status
			entry.LatencyMs = roundTo2(snap.LatencyMs)
			if !snap.LastCheckedAt.IsZero() {
				entry.LastCheckedAt = snap.LastCheckedAt.Format(time.RFC3339)
			}
			tlsExpiresAt = snap.TLSExpiresAt
		} else if latest, ok := latestCheckResultRecord(e.App, c.Id); ok {
			entry.Status = latest.GetString("status")
			entry.LatencyMs = roundTo2(latest.GetFloat("latency_ms"))
			entry.LastCheckedAt = latest.GetString("checked_at")
			tlsExpiresAt = latest.GetDateTime("tls_expires_at").Time()
		}
		if !tlsExpiresAt.IsZero() {
			entry.TLSExpiresAt = tlsExpiresAt.Format(time.RFC3339)
			daysLeft := time.Until(tlsExpiresAt).Hours() / 24
			entry.CertExpiringSoon = daysLeft <= float64(checks.TLSExpiryWarnDays(c))
		}

		entry.Uptime24h = computeUptime(e.App, c.Id, 24*time.Hour)
		entry.Uptime7d = computeUptime(e.App, c.Id, 7*24*time.Hour)

		entries = append(entries, entry)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"checks": entries,
		"total":  len(entries),
	})
}

// latestCheckResultRecord loads the most recently persisted check_results
// row for checkID.
func latestCheckResultRecord(app core.App, checkID string) (*core.Record, bool) {
	records, err := app.FindRecordsByFilter(
		"check_results",
		"check_id = {:id}",
		"-checked_at",
		1,
		0,
		map[string]any{"id": checkID},
	)
	if err != nil || len(records) == 0 {
		return nil, false
	}
	return records[0], true
}

// computeUptime returns the percentage of check_results rows recorded for
// checkID within the last window that have status="up", or 0 when there is
// no data in that window at all.
func computeUptime(app core.App, checkID string, window time.Duration) float64 {
	since := time.Now().UTC().Add(-window).Format("2006-01-02 15:04:05.000Z")
	records, err := app.FindRecordsByFilter(
		"check_results",
		"check_id = {:id} && checked_at >= {:since}",
		"",
		20000,
		0,
		map[string]any{"id": checkID, "since": since},
	)
	if err != nil || len(records) == 0 {
		return 0
	}

	up := 0
	for _, r := range records {
		if r.GetString("status") == "up" {
			up++
		}
	}
	return roundTo2(float64(up) / float64(len(records)) * 100)
}

// handleRunCheck runs a check immediately (on demand) and returns the
// resulting check_results row.
func handleRunCheck(e *core.RequestEvent, runner CheckRunner) error {
	checkID := e.Request.PathValue("id")
	if checkID == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "check ID is required"})
	}
	if _, err := e.App.FindRecordById("checks", checkID); err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{"error": "check not found"})
	}

	record, err := runner.RunOnce(checkID)
	if err != nil {
		audit.Record(e.App, e, audit.Entry{
			Action:     "check.run",
			TargetType: "checks",
			TargetID:   checkID,
			Details:    map[string]any{"error": err.Error()},
			Result:     "failure",
		})
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	audit.Record(e.App, e, audit.Entry{
		Action:     "check.run",
		TargetType: "checks",
		TargetID:   checkID,
		Details:    map[string]any{"status": record.GetString("status")},
		Result:     "success",
	})

	return e.JSON(http.StatusOK, map[string]any{
		"id":             record.Id,
		"check_id":       record.GetString("check_id"),
		"status":         record.GetString("status"),
		"latency_ms":     roundTo2(record.GetFloat("latency_ms")),
		"status_code":    record.GetInt("status_code"),
		"error":          record.GetString("error"),
		"tls_expires_at": record.GetString("tls_expires_at"),
		"checked_at":     record.GetString("checked_at"),
	})
}

// checkResultPoint is one point of GET /api/custom/checks/{id}/results.
type checkResultPoint struct {
	Timestamp float64 `json:"timestamp"`
	LatencyMs float64 `json:"latency_ms"`
	Status    string  `json:"status"`
}

// handleCheckResults returns a latency/status time series for one check
// over the last 24h or 7d, downsampled to at most 500 points.
func handleCheckResults(e *core.RequestEvent) error {
	checkID := e.Request.PathValue("id")
	if checkID == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{"error": "check ID is required"})
	}
	if _, err := e.App.FindRecordById("checks", checkID); err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{"error": "check not found"})
	}

	var window time.Duration
	var label string
	switch e.Request.URL.Query().Get("range") {
	case "7d":
		window, label = 7*24*time.Hour, "7d"
	default:
		window, label = 24*time.Hour, "24h"
	}

	since := time.Now().UTC().Add(-window).Format("2006-01-02 15:04:05.000Z")
	records, err := e.App.FindRecordsByFilter(
		"check_results",
		"check_id = {:id} && checked_at >= {:since}",
		"+checked_at",
		20000,
		0,
		map[string]any{"id": checkID, "since": since},
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to query check results",
		})
	}

	points := make([]checkResultPoint, 0, len(records))
	for _, r := range records {
		points = append(points, checkResultPoint{
			Timestamp: parseTimestamp(r.GetString("checked_at")),
			LatencyMs: roundTo2(r.GetFloat("latency_ms")),
			Status:    r.GetString("status"),
		})
	}

	const maxPoints = 500
	if len(points) > maxPoints {
		step := len(points) / maxPoints
		if step < 1 {
			step = 1
		}
		sampled := make([]checkResultPoint, 0, maxPoints)
		for i := 0; i < len(points); i += step {
			sampled = append(sampled, points[i])
		}
		points = sampled
	}

	return e.JSON(http.StatusOK, map[string]any{
		"check_id": checkID,
		"range":    label,
		"points":   points,
	})
}
