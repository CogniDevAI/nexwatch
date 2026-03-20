package metrics

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// Service handles metric ingestion, storage, and queries.
type Service struct {
	app core.App
}

// NewService creates a new metrics service.
func NewService(app core.App) *Service {
	return &Service{app: app}
}

// IngestMetrics processes a MetricsPayload from an agent, inserting records
// into the metrics collection and updating docker_containers if applicable.
func (s *Service) IngestMetrics(app core.App, agentID string, payload *protocol.MetricsPayload) {
	for _, m := range payload.Metrics {
		// Convert metric timestamp (millis) to time.
		var ts time.Time
		if m.Timestamp > 0 {
			ts = time.UnixMilli(m.Timestamp)
		} else {
			ts = time.Now()
		}

		// Marshal the data map to JSON for storage.
		dataJSON, err := json.Marshal(m.Data)
		if err != nil {
			log.Printf("[metrics] marshal data error for agent %s: %v", agentID, err)
			continue
		}

		// Insert into metrics collection.
		collection, err := app.FindCollectionByNameOrId("metrics")
		if err != nil {
			log.Printf("[metrics] find collection error: %v", err)
			return
		}

		record := core.NewRecord(collection)
		record.Set("agent_id", agentID)
		record.Set("type", m.Type)
		record.Set("data", string(dataJSON))
		record.Set("timestamp", ts.UTC().Format("2006-01-02 15:04:05.000Z"))
		record.Set("resolution", "raw")

		if err := app.Save(record); err != nil {
			log.Printf("[metrics] save error for agent %s type %s: %v", agentID, m.Type, err)
			continue
		}

		// If docker type, also update docker_containers collection.
		if m.Type == "docker" {
			s.upsertDockerContainer(app, agentID, m.Data)
		}
	}
}

// upsertDockerContainer creates or updates a docker container record.
func (s *Service) upsertDockerContainer(app core.App, agentID string, data map[string]any) {
	containerID, _ := data["container_id"].(string)
	if containerID == "" {
		return
	}

	// Try to find existing record.
	record, err := app.FindFirstRecordByFilter(
		"docker_containers",
		"agent_id = {:agentId} && container_id = {:containerId}",
		map[string]any{
			"agentId":     agentID,
			"containerId": containerID,
		},
	)

	if err != nil {
		// Create new record.
		collection, err := app.FindCollectionByNameOrId("docker_containers")
		if err != nil {
			log.Printf("[metrics] docker collection not found: %v", err)
			return
		}
		record = core.NewRecord(collection)
		record.Set("agent_id", agentID)
		record.Set("container_id", containerID)
	}

	// Update fields from data map.
	if name, ok := data["name"].(string); ok {
		record.Set("name", name)
	}
	if image, ok := data["image"].(string); ok {
		record.Set("image", image)
	}
	if status, ok := data["status"].(string); ok {
		record.Set("status", status)
	}
	if cpuPct, ok := toFloat64(data["cpu_percent"]); ok {
		record.Set("cpu_percent", cpuPct)
	}
	if memUsage, ok := toFloat64(data["memory_usage"]); ok {
		record.Set("memory_usage", memUsage)
	}
	if memLimit, ok := toFloat64(data["memory_limit"]); ok {
		record.Set("memory_limit", memLimit)
	}
	if netRx, ok := toFloat64(data["network_rx"]); ok {
		record.Set("network_rx", netRx)
	}
	if netTx, ok := toFloat64(data["network_tx"]); ok {
		record.Set("network_tx", netTx)
	}

	record.Set("updated_at", time.Now().UTC().Format("2006-01-02 15:04:05.000Z"))

	if err := app.Save(record); err != nil {
		log.Printf("[metrics] docker container save error: %v", err)
	}
}

// QueryMetrics returns metrics matching the given filters.
func (s *Service) QueryMetrics(agentID, metricType string, start, end time.Time, resolution string) ([]*core.Record, error) {
	filter := "timestamp >= {:start} && timestamp <= {:end}"
	params := map[string]any{
		"start": start.UTC().Format("2006-01-02 15:04:05.000Z"),
		"end":   end.UTC().Format("2006-01-02 15:04:05.000Z"),
	}

	if agentID != "" {
		filter += " && agent_id = {:agentId}"
		params["agentId"] = agentID
	}
	if metricType != "" {
		filter += " && type = {:type}"
		params["type"] = metricType
	}
	if resolution != "" {
		filter += " && resolution = {:resolution}"
		params["resolution"] = resolution
	}

	records, err := s.app.FindRecordsByFilter(
		"metrics",
		filter,
		"-timestamp",
		1000, // max records
		0,
		params,
	)
	if err != nil {
		return nil, fmt.Errorf("query metrics: %w", err)
	}

	return records, nil
}

// GetLatestMetricsByAgent returns the most recent metric of each type for a given agent.
func (s *Service) GetLatestMetricsByAgent(agentID string) (map[string]*core.Record, error) {
	types := []string{"cpu", "memory", "disk", "network"}
	result := make(map[string]*core.Record)

	for _, t := range types {
		records, err := s.app.FindRecordsByFilter(
			"metrics",
			"agent_id = {:agentId} && type = {:type}",
			"-timestamp",
			1,
			0,
			map[string]any{
				"agentId": agentID,
				"type":    t,
			},
		)
		if err != nil || len(records) == 0 {
			continue
		}
		result[t] = records[0]
	}

	return result, nil
}

// toFloat64 safely converts various numeric types to float64.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
