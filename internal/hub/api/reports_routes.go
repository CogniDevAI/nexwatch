package api

import (
	"net/http"
	"strconv"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/CogniDevAI/nexwatch/internal/hub/audit"
	"github.com/CogniDevAI/nexwatch/internal/hub/report"
)

// RegisterReportRoutes registers the weekly-report preview/send API routes
// on apiGroup ("/api/custom"), admin-only — generating and delivering a
// report is a data-export action in the same class as the Prometheus token
// and settings changes, all of which this codebase restricts to
// RoleAdmin. It returns every route it registered, for openapi_test.go to
// cross-check against docs/openapi.yaml.
func RegisterReportRoutes(apiGroup *router.RouterGroup[*core.RequestEvent]) []RegisteredRoute {
	rec := newRouteRecorder(apiGroup, "/api/custom")

	// GET /api/custom/reports/preview?period_days= — render the report
	// without sending it.
	rec.GET("/reports/preview", handleReportPreview).Bind(RequireRole(RoleAdmin))

	// POST /api/custom/reports/send — build and send the report now,
	// through the configured report_channel_ids.
	rec.POST("/reports/send", handleReportSend).Bind(RequireRole(RoleAdmin))

	return rec.Registered
}

func handleReportPreview(e *core.RequestEvent) error {
	periodDays := report.DefaultPeriodDays
	if raw := e.Request.URL.Query().Get("period_days"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return e.JSON(http.StatusBadRequest, map[string]string{"error": "period_days must be a positive integer"})
		}
		periodDays = parsed
	}

	rpt, err := report.Build(e.App, periodDays)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to build report"})
	}

	html, err := report.RenderHTML(rpt)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to render report"})
	}
	text, err := report.RenderText(rpt)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to render report"})
	}

	return e.JSON(http.StatusOK, map[string]string{
		"subject": report.Subject(rpt),
		"html":    html,
		"text":    text,
	})
}

func handleReportSend(e *core.RequestEvent) error {
	settings := report.LoadSettings(e.App)

	rpt, err := report.Build(e.App, settings.PeriodDays)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to build report"})
	}

	results := report.Send(e.App, rpt)

	type channelResult struct {
		ChannelID   string `json:"channel_id"`
		ChannelName string `json:"channel_name"`
		Success     bool   `json:"success"`
		Error       string `json:"error,omitempty"`
	}

	out := make([]channelResult, 0, len(results))
	failures := 0
	for _, r := range results {
		cr := channelResult{ChannelID: r.ChannelID, ChannelName: r.ChannelName, Success: r.Err == nil}
		if r.Err != nil {
			cr.Error = r.Err.Error()
			failures++
		}
		out = append(out, cr)
	}

	audit.Record(e.App, e, audit.Entry{
		Action:     "report.send",
		TargetType: "settings",
		TargetID:   report.ChannelIDsKey,
		Details:    map[string]any{"channels": len(results), "failures": failures},
		Result:     resultLabel(failures == 0),
	})

	return e.JSON(http.StatusOK, map[string]any{
		"subject": report.Subject(rpt),
		"results": out,
	})
}

func resultLabel(success bool) string {
	if success {
		return "success"
	}
	return "failure"
}
