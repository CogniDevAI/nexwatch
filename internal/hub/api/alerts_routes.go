package api

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"

	"github.com/CogniDevAI/nexwatch/internal/hub/alerts"
	"github.com/CogniDevAI/nexwatch/internal/hub/audit"
	"github.com/CogniDevAI/nexwatch/internal/hub/notify"
)

// RegisterAlertRoutes registers alert and notification API routes on
// apiGroup, which the caller must already have bound with the desired auth
// middleware (e.g. apis.RequireAuth()) and mounted at the "/api/custom"
// prefix. It returns every route it registered, for openapi_test.go to
// cross-check against docs/openapi.yaml.
func RegisterAlertRoutes(apiGroup *router.RouterGroup[*core.RequestEvent], notifySvc *notify.Service) []RegisteredRoute {
	rec := newRouteRecorder(apiGroup, "/api/custom")

	// POST /api/custom/notifications/:id/test — send a test notification to a channel.
	rec.POST("/notifications/{id}/test", func(e *core.RequestEvent) error {
		return handleTestNotification(e, notifySvc)
	}).Bind(RequireRole(RoleOperator))

	// POST /api/custom/alerts/{id}/ack — acknowledge a firing alert.
	rec.POST("/alerts/{id}/ack", handleAckAlert).Bind(RequireRole(RoleOperator))

	// POST /api/custom/alerts/{id}/unack — clear a previous acknowledgement.
	rec.POST("/alerts/{id}/unack", handleUnackAlert).Bind(RequireRole(RoleOperator))

	// GET /api/custom/silences/active — active silences with the agents they cover.
	rec.GET("/silences/active", handleActiveSilences)

	return rec.Registered
}

// handleAckAlert acknowledges a firing alert, recording who acknowledged it
// (auth record id, plus email when available — the same convention as
// handleRequestThreadDump's requestedBy) and when. An acknowledged alert is
// not re-notified after cooldown and is not escalated (see
// alerts.Engine.updateState); resolution keeps both fields for history.
func handleAckAlert(e *core.RequestEvent) error {
	alert, err := findAlertOrNotFound(e)
	if err != nil {
		return err
	}

	ackBy := ""
	if e.Auth != nil {
		ackBy = e.Auth.Id
		if email := e.Auth.GetString("email"); email != "" {
			ackBy = fmt.Sprintf("%s (%s)", e.Auth.Id, email)
		}
	}

	alert.Set("acknowledged_at", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))
	alert.Set("acknowledged_by", ackBy)
	if err := e.App.Save(alert); err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to acknowledge alert",
		})
	}

	audit.Record(e.App, e, audit.Entry{
		Action:     "alert.ack",
		TargetType: "alerts",
		TargetID:   alert.Id,
		AgentID:    alert.GetString("agent_id"),
		Details:    map[string]any{"message": alert.GetString("message")},
		Result:     "success",
	})

	return e.JSON(http.StatusOK, map[string]string{
		"status":             "ok",
		"acknowledged_at":    alert.GetString("acknowledged_at"),
		"acknowledged_by":    alert.GetString("acknowledged_by"),
		"acknowledged_alert": alert.Id,
	})
}

// handleUnackAlert clears a previous acknowledgement, letting the alert
// resume re-notification (after cooldown) and escalation.
func handleUnackAlert(e *core.RequestEvent) error {
	alert, err := findAlertOrNotFound(e)
	if err != nil {
		return err
	}

	alert.Set("acknowledged_at", "")
	alert.Set("acknowledged_by", "")
	if err := e.App.Save(alert); err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to clear acknowledgement",
		})
	}

	audit.Record(e.App, e, audit.Entry{
		Action:     "alert.unack",
		TargetType: "alerts",
		TargetID:   alert.Id,
		AgentID:    alert.GetString("agent_id"),
		Result:     "success",
	})

	return e.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

// findAlertOrNotFound resolves the "id" path value against the "alerts"
// collection, returning a ready-to-write 404 JSON error when it does not
// exist.
func findAlertOrNotFound(e *core.RequestEvent) (*core.Record, error) {
	alertID := e.Request.PathValue("id")
	if alertID == "" {
		return nil, e.JSON(http.StatusBadRequest, map[string]string{"error": "alert ID is required"})
	}
	alert, err := e.App.FindRecordById("alerts", alertID)
	if err != nil {
		return nil, e.JSON(http.StatusNotFound, map[string]string{"error": "alert not found"})
	}
	return alert, nil
}

// activeSilenceResponse describes one currently-active silence and the
// agents/checks it covers, for GET /api/custom/silences/active.
type activeSilenceResponse struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Reason          string   `json:"reason,omitempty"`
	AgentID         string   `json:"agent_id,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	CheckIDs        []string `json:"check_ids,omitempty"`
	StartsAt        string   `json:"starts_at"`
	EndsAt          string   `json:"ends_at"`
	CoveredAgentIDs []string `json:"covered_agent_ids"`
	CoveredCheckIDs []string `json:"covered_check_ids"`
}

// handleActiveSilences returns every silence whose window currently covers
// "now" (starts_at <= now < ends_at), each annotated with the ids of every
// agent and check it matches (see alerts.SilenceMatchesAgent/
// SilenceMatchesCheck), so operators can see at a glance which agents and
// checks have notifications suppressed right now.
func handleActiveSilences(e *core.RequestEvent) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05.000Z")

	silences, err := e.App.FindRecordsByFilter(
		"silences",
		"starts_at <= {:now} && ends_at > {:now}",
		"-starts_at",
		200,
		0,
		map[string]any{"now": now},
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch silences"})
	}

	agents, err := e.App.FindRecordsByFilter("agents", "id != ''", "", 500, 0)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch agents"})
	}

	checks, err := e.App.FindRecordsByFilter("checks", "id != ''", "", 500, 0)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to fetch checks"})
	}

	result := make([]activeSilenceResponse, 0, len(silences))
	for _, s := range silences {
		coveredAgents := make([]string, 0)
		for _, a := range agents {
			if alerts.SilenceMatchesAgent(a, s) {
				coveredAgents = append(coveredAgents, a.Id)
			}
		}
		coveredChecks := make([]string, 0)
		for _, c := range checks {
			if alerts.SilenceMatchesCheck(c, s) {
				coveredChecks = append(coveredChecks, c.Id)
			}
		}
		result = append(result, activeSilenceResponse{
			ID:              s.Id,
			Name:            s.GetString("name"),
			Reason:          s.GetString("reason"),
			AgentID:         s.GetString("agent_id"),
			Tags:            s.GetStringSlice("tags"),
			CheckIDs:        s.GetStringSlice("check_ids"),
			StartsAt:        s.GetString("starts_at"),
			EndsAt:          s.GetString("ends_at"),
			CoveredAgentIDs: coveredAgents,
			CoveredCheckIDs: coveredChecks,
		})
	}

	return e.JSON(http.StatusOK, map[string]any{
		"silences": result,
		"total":    len(result),
	})
}

// handleTestNotification sends a test notification to the specified channel.
func handleTestNotification(e *core.RequestEvent, notifySvc *notify.Service) error {
	channelID := e.Request.PathValue("id")
	if channelID == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{
			"error": "channel ID is required",
		})
	}

	channel, err := e.App.FindRecordById("notification_channels", channelID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{
			"error": "notification channel not found",
		})
	}

	if err := notifySvc.SendTestNotification(channel); err != nil {
		// A ConfigError means the channel's config is invalid or incomplete
		// (e.g. PagerDuty missing routing_key) rather than a delivery
		// failure — that's a client mistake to fix, not a server error.
		var cfgErr *notify.ConfigError
		if errors.As(err, &cfgErr) {
			return e.JSON(http.StatusBadRequest, map[string]string{
				"error": err.Error(),
			})
		}
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "test notification failed: " + err.Error(),
		})
	}

	return e.JSON(http.StatusOK, map[string]string{
		"status":  "ok",
		"message": "Test notification sent successfully",
	})
}
