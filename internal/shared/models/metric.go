package models

import "time"

// MetricType identifies the kind of metric being collected.
type MetricType string

const (
	MetricTypeCPU     MetricType = "cpu"
	MetricTypeMemory  MetricType = "memory"
	MetricTypeDisk    MetricType = "disk"
	MetricTypeNetwork MetricType = "network"
	MetricTypeDocker  MetricType = "docker"
)

// Metric represents a single metric data point collected by an agent.
type Metric struct {
	ID        string         `json:"id" msgpack:"id"`
	AgentID   string         `json:"agentId" msgpack:"a"`
	Type      MetricType     `json:"type" msgpack:"t"`
	Data      map[string]any `json:"data" msgpack:"d"`
	Timestamp time.Time      `json:"timestamp" msgpack:"ts"`
}
