package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/metrics"
)

// RegisterRoutes registers all custom API routes on the PocketBase router.
func RegisterRoutes(se *core.ServeEvent, metricsSvc *metrics.Service) {
	router := se.Router

	// GET /api/custom/dashboard — agent summaries with latest metrics
	router.GET("/api/custom/dashboard", func(e *core.RequestEvent) error {
		return handleDashboard(e, metricsSvc)
	})

	// GET /api/custom/metrics — time-range metric queries
	router.GET("/api/custom/metrics", func(e *core.RequestEvent) error {
		return handleMetricsQuery(e, metricsSvc)
	})

	// GET /api/custom/agents/{id}/install-command — generate agent install command
	router.GET("/api/custom/agents/{id}/install-command", func(e *core.RequestEvent) error {
		return handleInstallCommand(e)
	})
}

// handleDashboard returns a summary of all agents with their latest metrics.
func handleDashboard(e *core.RequestEvent, metricsSvc *metrics.Service) error {
	// Fetch all agents.
	agents, err := e.App.FindRecordsByFilter(
		"agents",
		"id != ''", // match all
		"-last_seen",
		100,
		0,
	)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": "failed to fetch agents",
		})
	}

	type agentSummary struct {
		ID       string         `json:"id"`
		Hostname string         `json:"hostname"`
		OS       string         `json:"os"`
		IP       string         `json:"ip"`
		Version  string         `json:"version"`
		Status   string         `json:"status"`
		LastSeen string         `json:"lastSeen"`
		CPU      map[string]any `json:"cpu,omitempty"`
		Memory   map[string]any `json:"memory,omitempty"`
		Disk     map[string]any `json:"disk,omitempty"`
	}

	var summaries []agentSummary
	for _, agent := range agents {
		summary := agentSummary{
			ID:       agent.Id,
			Hostname: agent.GetString("hostname"),
			OS:       agent.GetString("os"),
			IP:       agent.GetString("ip"),
			Version:  agent.GetString("version"),
			Status:   agent.GetString("status"),
			LastSeen: agent.GetString("last_seen"),
		}

		// Fetch latest metrics for this agent.
		latestMetrics, err := metricsSvc.GetLatestMetricsByAgent(agent.Id)
		if err == nil {
			for mtype, record := range latestMetrics {
				dataStr := record.GetString("data")
				var data map[string]any
				if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
					continue
				}
				switch mtype {
				case "cpu":
					summary.CPU = data
				case "memory":
					summary.Memory = data
				case "disk":
					summary.Disk = data
				}
			}
		}

		summaries = append(summaries, summary)
	}

	return e.JSON(http.StatusOK, map[string]any{
		"agents": summaries,
		"total":  len(summaries),
	})
}

// handleMetricsQuery returns metrics matching time-range and filters.
// Query params: agent_id, type, start, end, resolution
func handleMetricsQuery(e *core.RequestEvent, metricsSvc *metrics.Service) error {
	agentID := e.Request.URL.Query().Get("agent_id")
	metricType := e.Request.URL.Query().Get("type")
	resolution := e.Request.URL.Query().Get("resolution")

	startStr := e.Request.URL.Query().Get("start")
	endStr := e.Request.URL.Query().Get("end")

	// Default time range: last 1 hour.
	now := time.Now().UTC()
	start := now.Add(-1 * time.Hour)
	end := now

	if startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		}
	}
	if endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		}
	}

	// Auto-select resolution based on time range if not specified.
	if resolution == "" {
		duration := end.Sub(start)
		switch {
		case duration <= 2*time.Hour:
			resolution = "raw"
		case duration <= 24*time.Hour:
			resolution = "1m"
		case duration <= 7*24*time.Hour:
			resolution = "5m"
		default:
			resolution = "1h"
		}
	}

	records, err := metricsSvc.QueryMetrics(agentID, metricType, start, end, resolution)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{
			"error": fmt.Sprintf("query failed: %v", err),
		})
	}

	type metricResponse struct {
		ID         string         `json:"id"`
		AgentID    string         `json:"agentId"`
		Type       string         `json:"type"`
		Data       map[string]any `json:"data"`
		Timestamp  string         `json:"timestamp"`
		Resolution string         `json:"resolution"`
	}

	var results []metricResponse
	for _, r := range records {
		dataStr := r.GetString("data")
		var data map[string]any
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			data = map[string]any{}
		}

		results = append(results, metricResponse{
			ID:         r.Id,
			AgentID:    r.GetString("agent_id"),
			Type:       r.GetString("type"),
			Data:       data,
			Timestamp:  r.GetString("timestamp"),
			Resolution: r.GetString("resolution"),
		})
	}

	return e.JSON(http.StatusOK, map[string]any{
		"metrics":    results,
		"total":      len(results),
		"resolution": resolution,
		"start":      start.Format(time.RFC3339),
		"end":        end.Format(time.RFC3339),
	})
}

// handleInstallCommand generates a curl install command for an agent.
func handleInstallCommand(e *core.RequestEvent) error {
	agentID := e.Request.PathValue("id")
	if agentID == "" {
		return e.JSON(http.StatusBadRequest, map[string]string{
			"error": "agent ID is required",
		})
	}

	// Lookup the agent record.
	agent, err := e.App.FindRecordById("agents", agentID)
	if err != nil {
		return e.JSON(http.StatusNotFound, map[string]string{
			"error": "agent not found",
		})
	}

	token := agent.GetString("token")
	if token == "" {
		// Generate a token if not present.
		token, err = generateToken()
		if err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{
				"error": "failed to generate token",
			})
		}
		agent.Set("token", token)
		if err := e.App.Save(agent); err != nil {
			return e.JSON(http.StatusInternalServerError, map[string]string{
				"error": "failed to save token",
			})
		}
	}

	// Determine the hub URL from the request.
	scheme := "https"
	if e.Request.TLS == nil {
		scheme = "http"
	}
	hubURL := fmt.Sprintf("%s://%s", scheme, e.Request.Host)

	// Generate the install command.
	installCmd := fmt.Sprintf(
		`curl -sSL %s/install.sh | sudo bash -s -- --hub="%s" --token="%s"`,
		hubURL, hubURL, token,
	)

	return e.JSON(http.StatusOK, map[string]any{
		"agentId":        agentID,
		"token":          token,
		"installCommand": installCmd,
		"hubUrl":         hubURL,
	})
}

// generateToken creates a cryptographically secure random token.
func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
