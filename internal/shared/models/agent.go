package models

import "time"

// AgentStatus represents the connection state of an agent.
type AgentStatus string

const (
	AgentStatusOnline  AgentStatus = "online"
	AgentStatusOffline AgentStatus = "offline"
	AgentStatusPending AgentStatus = "pending"
)

// Agent represents a monitored server running the NexWatch agent.
type Agent struct {
	ID       string      `json:"id" msgpack:"id"`
	Name     string      `json:"name" msgpack:"name"`
	Hostname string      `json:"hostname" msgpack:"hostname"`
	OS       string      `json:"os" msgpack:"os"`
	IP       string      `json:"ip" msgpack:"ip"`
	Version  string      `json:"version" msgpack:"version"`
	Status   AgentStatus `json:"status" msgpack:"status"`
	LastSeen time.Time   `json:"lastSeen" msgpack:"lastSeen"`
}
