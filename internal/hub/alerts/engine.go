package alerts

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// AlertState represents the current state of an alert rule+agent pair.
type AlertState int

const (
	StateOK AlertState = iota
	StateWarning
	StateCritical
	StateResolved
)

func (s AlertState) String() string {
	switch s {
	case StateOK:
		return "ok"
	case StateWarning:
		return "warning"
	case StateCritical:
		return "critical"
	case StateResolved:
		return "resolved"
	default:
		return "unknown"
	}
}

// ruleAgentKey uniquely identifies a rule+agent combination for state tracking.
type ruleAgentKey struct {
	RuleID  string
	AgentID string
}

// ruleAgentState holds the evaluation state for a rule+agent pair.
type ruleAgentState struct {
	State           AlertState
	FirstBreachAt   time.Time // when the threshold was first exceeded
	LastNotifiedAt  time.Time // when the last notification was sent
	ActiveAlertID   string    // PocketBase record ID of the currently firing alert
}

// NotifyFunc is called when an alert state changes and a notification should be sent.
// It receives the alert record and the rule record.
type NotifyFunc func(app core.App, alert *core.Record, rule *core.Record)

// Engine evaluates alert rules against metrics and manages alert state transitions.
type Engine struct {
	app            core.App
	notifyFunc     NotifyFunc
	states         map[ruleAgentKey]*ruleAgentState
	mu             sync.Mutex
	stopCh         chan struct{}
	evalInterval   time.Duration
	cooldownPeriod time.Duration
}

// NewEngine creates a new alert evaluation engine.
func NewEngine(app core.App) *Engine {
	return &Engine{
		app:            app,
		states:         make(map[ruleAgentKey]*ruleAgentState),
		stopCh:         make(chan struct{}),
		evalInterval:   30 * time.Second,
		cooldownPeriod: 5 * time.Minute,
	}
}

// SetNotifyFunc registers the callback invoked on alert state transitions.
func (e *Engine) SetNotifyFunc(fn NotifyFunc) {
	e.notifyFunc = fn
}

// Start launches the background evaluation goroutine.
func (e *Engine) Start() {
	go e.evalLoop()
	log.Printf("[alerts] engine started (interval: %s, cooldown: %s)", e.evalInterval, e.cooldownPeriod)
}

// Stop signals the evaluation goroutine to stop.
func (e *Engine) Stop() {
	close(e.stopCh)
}

// evalLoop runs the periodic alert evaluation cycle.
func (e *Engine) evalLoop() {
	// Small initial delay to let the system stabilize after startup.
	timer := time.NewTimer(10 * time.Second)
	select {
	case <-timer.C:
	case <-e.stopCh:
		timer.Stop()
		return
	}

	ticker := time.NewTicker(e.evalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			e.evaluate()
		case <-e.stopCh:
			return
		}
	}
}

// evaluate runs one full evaluation cycle across all enabled rules and matching agents.
func (e *Engine) evaluate() {
	rules, err := e.app.FindRecordsByFilter(
		"alert_rules",
		"enabled = true",
		"",
		500,
		0,
	)
	if err != nil {
		log.Printf("[alerts] failed to fetch rules: %v", err)
		return
	}

	for _, rule := range rules {
		e.evaluateRule(rule)
	}
}

// evaluateRule evaluates a single alert rule against all matching agents.
func (e *Engine) evaluateRule(rule *core.Record) {
	ruleAgentID := rule.GetString("agent_id")
	metricType := rule.GetString("metric_type")

	// Determine which agents to evaluate.
	var agentIDs []string
	if ruleAgentID != "" {
		// Rule is scoped to a specific agent.
		agentIDs = []string{ruleAgentID}
	} else {
		// Global rule: evaluate against all online agents.
		agents, err := e.app.FindRecordsByFilter(
			"agents",
			"status = 'online'",
			"",
			200,
			0,
		)
		if err != nil {
			return
		}
		for _, a := range agents {
			agentIDs = append(agentIDs, a.Id)
		}
	}

	condition := rule.GetString("condition")
	threshold := rule.GetFloat("threshold")
	durationSec := rule.GetFloat("duration")
	severity := rule.GetString("severity")

	for _, agentID := range agentIDs {
		// Get the latest metric value for this agent and type.
		value, ok := e.getLatestMetricValue(agentID, metricType)
		if !ok {
			continue
		}

		breaching := e.checkCondition(value, condition, threshold)
		e.updateState(rule, agentID, breaching, value, severity, time.Duration(durationSec)*time.Second)
	}
}

// getLatestMetricValue retrieves the latest numeric value for a metric type.
// It extracts the primary value: total_percent for cpu, used_percent for memory/disk,
// bytes_recv for network.
func (e *Engine) getLatestMetricValue(agentID, metricType string) (float64, bool) {
	records, err := e.app.FindRecordsByFilter(
		"metrics",
		"agent_id = {:agentId} && type = {:type}",
		"-timestamp",
		1,
		0,
		map[string]any{
			"agentId": agentID,
			"type":    metricType,
		},
	)
	if err != nil || len(records) == 0 {
		return 0, false
	}

	dataStr := records[0].GetString("data")
	var data map[string]any
	if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
		return 0, false
	}

	// Extract the primary metric value based on type.
	var val float64
	var ok bool
	switch metricType {
	case "cpu":
		val, ok = toFloat64(data["total_percent"])
		if !ok {
			val, ok = toFloat64(data["percent"])
		}
	case "memory":
		val, ok = toFloat64(data["used_percent"])
		if !ok {
			val, ok = toFloat64(data["percent"])
		}
	case "disk":
		val, ok = toFloat64(data["used_percent"])
		if !ok {
			val, ok = toFloat64(data["percent"])
		}
	case "network":
		val, ok = toFloat64(data["bytes_recv"])
		if !ok {
			val, ok = toFloat64(data["bytes_sent"])
		}
	default:
		// Try generic "value" or "percent" field.
		val, ok = toFloat64(data["value"])
		if !ok {
			val, ok = toFloat64(data["percent"])
		}
	}

	return val, ok
}

// checkCondition evaluates whether a value breaches the threshold per the condition.
func (e *Engine) checkCondition(value float64, condition string, threshold float64) bool {
	switch condition {
	case "gt":
		return value > threshold
	case "lt":
		return value < threshold
	case "eq":
		return value == threshold
	default:
		return false
	}
}

// updateState manages the state machine for a rule+agent pair.
func (e *Engine) updateState(rule *core.Record, agentID string, breaching bool, value float64, severity string, duration time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	key := ruleAgentKey{RuleID: rule.Id, AgentID: agentID}
	state, exists := e.states[key]
	if !exists {
		state = &ruleAgentState{State: StateOK}
		e.states[key] = state
	}

	now := time.Now()

	if breaching {
		if state.State == StateOK || state.State == StateResolved {
			// Start tracking the breach.
			state.FirstBreachAt = now
			state.State = StateWarning
			return
		}

		// Already in warning or critical state — check duration.
		if now.Sub(state.FirstBreachAt) >= duration {
			if state.State == StateWarning {
				// Transition to critical (or warning severity).
				newState := StateCritical
				if severity == "warning" {
					newState = StateWarning
				}
				state.State = newState

				// Fire the alert.
				alertID := e.fireAlert(rule, agentID, value, severity)
				if alertID != "" {
					state.ActiveAlertID = alertID
					state.LastNotifiedAt = now
				}
			} else if state.State == StateCritical {
				// Already critical — check cooldown for re-notification.
				if !state.LastNotifiedAt.IsZero() && now.Sub(state.LastNotifiedAt) >= e.cooldownPeriod {
					// Re-notify after cooldown.
					e.fireAlert(rule, agentID, value, severity)
					state.LastNotifiedAt = now
				}
			}
		}
	} else {
		// Not breaching — resolve if currently alerting.
		if state.State == StateWarning || state.State == StateCritical {
			if state.ActiveAlertID != "" {
				e.resolveAlert(state.ActiveAlertID)
			}
			state.State = StateResolved
			state.ActiveAlertID = ""
			state.FirstBreachAt = time.Time{}

			// After resolution, reset to OK for next evaluation cycle.
			state.State = StateOK
		}
	}
}

// fireAlert creates an alert record and triggers notification.
func (e *Engine) fireAlert(rule *core.Record, agentID string, value float64, severity string) string {
	collection, err := e.app.FindCollectionByNameOrId("alerts")
	if err != nil {
		log.Printf("[alerts] alerts collection not found: %v", err)
		return ""
	}

	// Build a meaningful message.
	agentName := e.getAgentHostname(agentID)
	metricType := rule.GetString("metric_type")
	condition := rule.GetString("condition")
	threshold := rule.GetFloat("threshold")

	condStr := ">"
	switch condition {
	case "lt":
		condStr = "<"
	case "eq":
		condStr = "="
	}

	message := fmt.Sprintf("[%s] %s on %s: %s %s %.1f (current: %.1f)",
		severity, metricType, agentName, metricType, condStr, threshold, value)

	now := time.Now().UTC().Format("2006-01-02 15:04:05.000Z")

	record := core.NewRecord(collection)
	record.Set("rule_id", rule.Id)
	record.Set("agent_id", agentID)
	record.Set("status", "firing")
	record.Set("value", value)
	record.Set("message", message)
	record.Set("fired_at", now)

	if err := e.app.Save(record); err != nil {
		log.Printf("[alerts] failed to save alert: %v", err)
		return ""
	}

	log.Printf("[alerts] FIRED: %s", message)

	// Trigger notification.
	if e.notifyFunc != nil {
		e.notifyFunc(e.app, record, rule)
	}

	return record.Id
}

// resolveAlert marks an alert as resolved.
func (e *Engine) resolveAlert(alertID string) {
	record, err := e.app.FindRecordById("alerts", alertID)
	if err != nil {
		log.Printf("[alerts] failed to find alert %s for resolution: %v", alertID, err)
		return
	}

	now := time.Now().UTC().Format("2006-01-02 15:04:05.000Z")
	record.Set("status", "resolved")
	record.Set("resolved_at", now)

	if err := e.app.Save(record); err != nil {
		log.Printf("[alerts] failed to resolve alert %s: %v", alertID, err)
		return
	}

	log.Printf("[alerts] RESOLVED: alert %s", alertID)
}

// getAgentHostname looks up the hostname for an agent ID.
func (e *Engine) getAgentHostname(agentID string) string {
	record, err := e.app.FindRecordById("agents", agentID)
	if err != nil {
		return agentID
	}
	hostname := record.GetString("hostname")
	if hostname == "" {
		return agentID
	}
	return hostname
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
