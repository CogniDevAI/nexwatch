package alerts

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
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

// agentOfflineHeartbeatTimeout mirrors the frontend's OFFLINE_THRESHOLD_MS
// (ui/src/lib/agent.ts): agents.status is only flipped to "offline" by the
// WebSocket handler on an explicit disconnect (internal/hub/ws/handler.go),
// so it can lag behind reality across SSH tunnel reconnects or races. The
// agent_offline rule type treats an agent as offline when EITHER signal
// says so, matching the "agents.status == offline (or last_seen older than
// the heartbeat timeout)" semantics.
const agentOfflineHeartbeatTimeout = 90 * time.Second

// ruleAgentKey uniquely identifies a rule+subject combination for state
// tracking. For every metric_type except check_down/cert_expiry, "subject"
// means an agent and AgentID holds agents.id. For check_down/cert_expiry
// (internal/hub/checks), there is no agent at all — AgentID instead holds
// the check's record id (checks.id). The rest of the state machine
// (updateState, fireAlert, refreshFiringAlert, buildMessage, seedState) is
// reused as-is for both: it was already generic over "some subject id
// this rule+state pertains to," so check-based rule types were added by
// widening what that id can mean rather than forking a parallel state
// machine. See internal/hub/migrations/rule_check_types.go for the
// corresponding schema change (alerts.check_id, alerts.agent_id now
// optional).
type ruleAgentKey struct {
	RuleID  string
	AgentID string
}

// isCheckMetricType reports whether metricType is one of the check-based
// rule types (targeting internal/hub/checks records) rather than an
// agent-based one.
func isCheckMetricType(metricType string) bool {
	return metricType == "check_down" || metricType == "cert_expiry"
}

// ruleAgentState holds the evaluation state for a rule+agent pair.
//
// Fired is the piece that keeps the engine idempotent: State alone cannot
// distinguish "just started breaching, still inside the grace period" from
// "already firing" when a warning-severity rule keeps the same State value
// (StateWarning) across both — that ambiguity was the root cause of the
// alert storm (a warning-severity rule re-entered the "fire" branch on every
// evaluation cycle once its duration had elapsed, because nothing recorded
// that it had already fired). Fired makes that distinction explicit.
type ruleAgentState struct {
	State          AlertState
	Fired          bool      // true once an alert record exists for the current breach period
	FirstBreachAt  time.Time // when the threshold was first exceeded in the current breach period
	FiredAt        time.Time // when this breach period's alert record was created (escalation timer)
	LastNotifiedAt time.Time // when the last notification was dispatched; zero means "never notified"
	ActiveAlertID  string    // PocketBase record ID of the currently firing alert
	Silenced       bool      // true while an active silence covered the last evaluation
}

// NotifyFunc is called when an alert state changes and a notification should be sent.
// It receives the alert record and the rule record.
type NotifyFunc func(app core.App, alert *core.Record, rule *core.Record)

// Engine evaluates alert rules against metrics and manages alert state transitions.
type Engine struct {
	app            core.App
	notifyFunc     NotifyFunc
	escalateFunc   NotifyFunc
	states         map[ruleAgentKey]*ruleAgentState
	mu             sync.Mutex
	stopCh         chan struct{}
	evalInterval   time.Duration
	cooldownPeriod time.Duration

	// clock returns the current time and defaults to time.Now. It exists as
	// a seam so tests can drive the ok/warning/critical/resolved state
	// machine deterministically instead of sleeping across real durations.
	clock func() time.Time
}

// NewEngine creates a new alert evaluation engine.
func NewEngine(app core.App) *Engine {
	return &Engine{
		app:            app,
		states:         make(map[ruleAgentKey]*ruleAgentState),
		stopCh:         make(chan struct{}),
		evalInterval:   30 * time.Second,
		cooldownPeriod: 5 * time.Minute,
		clock:          time.Now,
	}
}

// SetNotifyFunc registers the callback invoked on alert state transitions.
func (e *Engine) SetNotifyFunc(fn NotifyFunc) {
	e.notifyFunc = fn
}

// SetEscalateFunc registers the callback invoked when an unacknowledged,
// unsilenced alert crosses its rule's escalation_after threshold. It
// receives the same alert/rule pair as NotifyFunc but is expected to
// dispatch to the rule's escalation_channels instead of
// notification_channels (see notify.Service.DispatchEscalation).
func (e *Engine) SetEscalateFunc(fn NotifyFunc) {
	e.escalateFunc = fn
}

// Start seeds in-memory state from any alerts still marked "firing" from a
// previous run (see seedState) and launches the background evaluation
// goroutine. Seeding runs synchronously so the very first evaluation cycle
// already knows about incidents that were active before a restart, instead
// of re-firing a duplicate alert or losing track of the incident entirely.
func (e *Engine) Start() {
	e.seedState()
	go e.evalLoop()
	slog.Info("alerts engine started", "interval", e.evalInterval, "cooldown", e.cooldownPeriod)
}

// Stop signals the evaluation goroutine to stop.
func (e *Engine) Stop() {
	close(e.stopCh)
}

// seedState rehydrates the in-memory rule+agent state map from any "alerts"
// records still marked status="firing", so a hub restart neither re-fires a
// duplicate alert for an incident that is still breaching nor silently
// forgets about it. Restored state is marked Fired=true with
// FirstBreachAt/FiredAt/LastNotifiedAt set to the alert's fired_at, matching
// where the state machine would already be had the process never
// restarted — except when the alert was silenced, in which case
// LastNotifiedAt is left zero (never notified) so the very next
// non-silenced evaluation notifies immediately rather than waiting out a
// cooldown window that never actually started.
func (e *Engine) seedState() {
	firing, err := e.app.FindRecordsByFilter(
		"alerts",
		"status = 'firing'",
		"",
		1000,
		0,
	)
	if err != nil {
		slog.Error("failed to load firing alerts for state seeding", "error", err)
		return
	}
	if len(firing) == 0 {
		return
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	restored := 0
	for _, alert := range firing {
		ruleID := alert.GetString("rule_id")
		// A check-triggered alert (check_down/cert_expiry) has no agent_id
		// at all — fall back to check_id, which plays the same "subject
		// id" role in ruleAgentKey. See ruleAgentKey's doc comment.
		subjectID := alert.GetString("agent_id")
		if subjectID == "" {
			subjectID = alert.GetString("check_id")
		}
		if ruleID == "" || subjectID == "" {
			continue
		}

		firedAt := alert.GetDateTime("fired_at").Time()
		silenced := alert.GetBool("silenced")

		state := StateCritical
		if rule, err := e.app.FindRecordById("alert_rules", ruleID); err == nil && rule.GetString("severity") == "warning" {
			state = StateWarning
		}

		lastNotifiedAt := firedAt
		if silenced {
			lastNotifiedAt = time.Time{}
		}

		e.states[ruleAgentKey{RuleID: ruleID, AgentID: subjectID}] = &ruleAgentState{
			State:          state,
			Fired:          true,
			FirstBreachAt:  firedAt,
			FiredAt:        firedAt,
			LastNotifiedAt: lastNotifiedAt,
			ActiveAlertID:  alert.Id,
			Silenced:       silenced,
		}
		restored++
	}

	if restored > 0 {
		slog.Info("restored firing alerts from a previous run", "count", restored)
	}
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
		slog.Error("failed to fetch alert rules", "error", err)
		return
	}

	silences, err := e.activeSilences()
	if err != nil {
		// Degrade gracefully: an unreadable silences table should not stop
		// alert evaluation altogether, only its silence-suppression.
		slog.Error("failed to fetch active silences", "error", err)
		silences = nil
	}

	for _, rule := range rules {
		if isCheckMetricType(rule.GetString("metric_type")) {
			e.evaluateCheckRule(rule, silences)
		} else {
			e.evaluateRule(rule, silences)
		}
	}
}

// evaluateCheckRule evaluates a check_down or cert_expiry rule against
// every "checks" record it targets: exactly the one named by
// alert_rules.check_id when set, or every check when it is empty (in which
// case a separately-breaching check each gets its own alert — see
// ruleAgentKey's doc comment). silences (the same already-fetched active
// silences evaluateRule uses) is checked per-check via isCheckSilenced/
// SilenceMatchesCheck — a check_ids-scoped silence, a tag-overlap with the
// check's own tags, or a global silence all suppress notification the same
// way an agent-based silence does for evaluateRule.
func (e *Engine) evaluateCheckRule(rule *core.Record, silences []*core.Record) {
	var targets []*core.Record
	if checkID := rule.GetString("check_id"); checkID != "" {
		check, err := e.app.FindRecordById("checks", checkID)
		if err != nil {
			return
		}
		targets = []*core.Record{check}
	} else {
		checks, err := e.app.FindRecordsByFilter("checks", "enabled = true", "", 500, 0)
		if err != nil {
			return
		}
		targets = checks
	}

	metricType := rule.GetString("metric_type")
	severity := rule.GetString("severity")
	duration := time.Duration(rule.GetFloat("duration")) * time.Second
	warnDays := rule.GetFloat("threshold")
	now := e.clock()

	for _, check := range targets {
		var value float64
		var breaching, ok bool

		switch metricType {
		case "check_down":
			value, breaching, ok = e.evalCheckDown(check.Id)
		case "cert_expiry":
			value, breaching, ok = e.evalCertExpiry(check.Id, warnDays)
		}
		if !ok {
			continue
		}

		silenced := isCheckSilenced(check, silences, now)
		e.updateState(rule, check.Id, breaching, value, severity, duration, silenced)
	}
}

// evalCheckDown reports whether check's latest recorded debounced state
// (the most recent check_results.status — already debounced by
// checks.Scheduler per failures_before_down, not the raw single-attempt
// result) is "down". ok is false when the check has never run yet. value
// is 1 while down (the breaching state) and 0 while up.
func (e *Engine) evalCheckDown(checkID string) (value float64, breaching bool, ok bool) {
	result, ok := e.latestCheckResult(checkID)
	if !ok {
		return 0, false, false
	}
	if result.GetString("status") == "down" {
		return 1, true, true
	}
	return 0, false, true
}

// evalCertExpiry reports whether check's latest recorded TLS certificate
// expires within warnDays days of now. ok is false when the check has
// never run yet, or its latest result recorded no certificate at all (a
// tcp/icmp check, or an http check that hasn't completed a TLS handshake).
// value is the number of days left (can be negative for an already-expired
// certificate).
func (e *Engine) evalCertExpiry(checkID string, warnDays float64) (value float64, breaching bool, ok bool) {
	result, ok := e.latestCheckResult(checkID)
	if !ok {
		return 0, false, false
	}
	expiresAt := result.GetDateTime("tls_expires_at").Time()
	if expiresAt.IsZero() {
		return 0, false, false
	}

	daysLeft := e.clock().Sub(expiresAt).Hours() / -24
	return daysLeft, daysLeft <= warnDays, true
}

// latestCheckResult loads the most recent check_results row for checkID.
func (e *Engine) latestCheckResult(checkID string) (*core.Record, bool) {
	records, err := e.app.FindRecordsByFilter(
		"check_results",
		"check_id = {:checkId}",
		"-checked_at",
		1,
		0,
		map[string]any{"checkId": checkID},
	)
	if err != nil || len(records) == 0 {
		return nil, false
	}
	return records[0], true
}

// getCheckNameTarget looks up a check's name and target for message
// building, falling back to the raw id (and an empty target) when the
// check can no longer be found (e.g. deleted between evaluation and
// message rendering).
func (e *Engine) getCheckNameTarget(checkID string) (name, target string) {
	record, err := e.app.FindRecordById("checks", checkID)
	if err != nil {
		return checkID, ""
	}
	name = record.GetString("name")
	if name == "" {
		name = checkID
	}
	return name, record.GetString("target")
}

// activeSilences fetches every silence whose window could still be active
// (ends_at in the future). isSilenced then applies the exact
// starts_at/now comparison in Go, since comparing parsed times is more
// precise than composing a single filter expression for "starts_at <= now
// < ends_at" against PocketBase's date string representation.
func (e *Engine) activeSilences() ([]*core.Record, error) {
	now := e.clock().UTC().Format("2006-01-02 15:04:05.000Z")
	return e.app.FindRecordsByFilter(
		"silences",
		"ends_at > {:now}",
		"",
		500,
		0,
		map[string]any{"now": now},
	)
}

// matchesTargets reports whether rule applies to agent, per the alert
// targeting rules: a rule with agent_id set applies to that agent only;
// otherwise a rule with a non-empty target_tags applies to agents having
// ANY of those tags; otherwise the rule applies to every agent.
func matchesTargets(agent *core.Record, rule *core.Record) bool {
	if ruleAgentID := rule.GetString("agent_id"); ruleAgentID != "" {
		return agent.Id == ruleAgentID
	}

	targetTags := rule.GetStringSlice("target_tags")
	if len(targetTags) == 0 {
		return true
	}

	agentTags := agent.GetStringSlice("tags")
	return stringSlicesIntersect(targetTags, agentTags)
}

// isSilenced reports whether an active silence in silences currently covers
// agent at the instant now. A silence is active when
// starts_at <= now < ends_at.
func isSilenced(agent *core.Record, silences []*core.Record, now time.Time) bool {
	for _, s := range silences {
		startsAt := s.GetDateTime("starts_at").Time()
		endsAt := s.GetDateTime("ends_at").Time()
		if now.Before(startsAt) || !now.Before(endsAt) {
			continue // not active at this instant
		}
		if SilenceMatchesAgent(agent, s) {
			return true
		}
	}
	return false
}

// SilenceMatchesAgent reports whether silence covers agent: its agent_id
// equals the agent, ANY of its tags match one of the agent's tags, or
// agent_id, tags, AND check_ids are all empty (a global silence covering
// every agent, and every check — see SilenceMatchesCheck). check_ids
// participates in the "is this global" test alongside agent_id/tags so a
// silence scoped only to specific checks (check_ids set, agent_id/tags
// empty) is not mistaken for a global silence that would also cover every
// agent. It is exported so internal/hub/api can reuse the same matching
// rule to list which agents an active silence covers (GET
// /api/custom/silences/active) without duplicating the logic.
func SilenceMatchesAgent(agent *core.Record, silence *core.Record) bool {
	silenceAgentID := silence.GetString("agent_id")
	silenceTags := silence.GetStringSlice("tags")
	silenceCheckIDs := silence.GetStringSlice("check_ids")

	if silenceAgentID == "" && len(silenceTags) == 0 && len(silenceCheckIDs) == 0 {
		return true // global silence
	}
	if silenceAgentID != "" && silenceAgentID == agent.Id {
		return true
	}
	if len(silenceTags) > 0 && stringSlicesIntersect(silenceTags, agent.GetStringSlice("tags")) {
		return true
	}
	return false
}

// isCheckSilenced reports whether an active silence in silences currently
// covers check at the instant now — the check-based counterpart of
// isSilenced. A silence is active when starts_at <= now < ends_at.
func isCheckSilenced(check *core.Record, silences []*core.Record, now time.Time) bool {
	for _, s := range silences {
		startsAt := s.GetDateTime("starts_at").Time()
		endsAt := s.GetDateTime("ends_at").Time()
		if now.Before(startsAt) || !now.Before(endsAt) {
			continue // not active at this instant
		}
		if SilenceMatchesCheck(check, s) {
			return true
		}
	}
	return false
}

// SilenceMatchesCheck reports whether silence covers check: its check_ids
// includes the check, ANY of its tags match one of the check's own tags, or
// agent_id, tags, AND check_ids are all empty (a global silence — see
// SilenceMatchesAgent's doc comment for why check_ids joined that test). A
// silence scoped only by agent_id (no tags, no check_ids) never covers a
// check — an agent_id has no meaning for a check-based alert. It is
// exported for the same reason SilenceMatchesAgent is: internal/hub/api
// reuses it to list which checks an active silence covers (GET
// /api/custom/silences/active's covered_check_ids).
func SilenceMatchesCheck(check *core.Record, silence *core.Record) bool {
	silenceAgentID := silence.GetString("agent_id")
	silenceTags := silence.GetStringSlice("tags")
	silenceCheckIDs := silence.GetStringSlice("check_ids")

	if silenceAgentID == "" && len(silenceTags) == 0 && len(silenceCheckIDs) == 0 {
		return true // global silence
	}
	for _, id := range silenceCheckIDs {
		if id == check.Id {
			return true
		}
	}
	if len(silenceTags) > 0 && stringSlicesIntersect(silenceTags, check.GetStringSlice("tags")) {
		return true
	}
	return false
}

// stringSlicesIntersect reports whether a and b share at least one element.
func stringSlicesIntersect(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// evaluateRule evaluates a single alert rule against every agent it targets
// (see matchesTargets), against the given already-fetched active silences.
func (e *Engine) evaluateRule(rule *core.Record, silences []*core.Record) {
	ruleAgentID := rule.GetString("agent_id")
	metricType := rule.GetString("metric_type")

	// Determine the candidate agent pool. A single-agent-scoped rule always
	// evaluates that one agent regardless of status (existing behavior,
	// unchanged). A tag-scoped or global rule evaluates online agents only,
	// EXCEPT agent_offline rules, which must also see offline agents —
	// otherwise an agent going offline would immediately drop out of its
	// own "is it offline" check.
	var candidates []*core.Record
	if ruleAgentID != "" {
		agent, err := e.app.FindRecordById("agents", ruleAgentID)
		if err != nil {
			return
		}
		candidates = []*core.Record{agent}
	} else {
		filter := "status = 'online'"
		if metricType == "agent_offline" {
			filter = "id != ''"
		}
		agents, err := e.app.FindRecordsByFilter("agents", filter, "", 200, 0)
		if err != nil {
			return
		}
		candidates = agents
	}

	condition := rule.GetString("condition")
	threshold := rule.GetFloat("threshold")
	duration := time.Duration(rule.GetFloat("duration")) * time.Second
	severity := rule.GetString("severity")
	target := rule.GetString("target")
	now := e.clock()

	for _, agent := range candidates {
		if !matchesTargets(agent, rule) {
			continue
		}

		var value float64
		var breaching, ok bool

		switch metricType {
		case "agent_offline":
			value, breaching, ok = e.evalAgentOffline(agent, now)
		case "process_down":
			value, breaching, ok = e.evalProcessDown(agent.Id, target)
		case "service_failed":
			value, breaching, ok = e.evalServiceFailed(agent.Id, target)
		case "cve_count":
			value, breaching, ok = e.evalCveCount(agent.Id, target, condition, threshold)
		case "log_match":
			value, breaching, ok = e.evalLogMatch(agent.Id, target, threshold, duration)
		default:
			value, ok = e.getLatestMetricValue(agent.Id, metricType)
			if ok {
				breaching = e.checkCondition(value, condition, threshold)
			}
		}
		if !ok {
			continue
		}

		silenced := isSilenced(agent, silences, now)
		e.updateState(rule, agent.Id, breaching, value, severity, duration, silenced)
	}
}

// evalAgentOffline reports whether agent is currently offline, ignoring the
// rule's condition/threshold entirely: breaching when agents.status is
// "offline", or when last_seen is older than agentOfflineHeartbeatTimeout
// (status alone can lag — see the constant's doc comment). value is 1 while
// offline (the breaching state) and 0 while online.
func (e *Engine) evalAgentOffline(agent *core.Record, now time.Time) (value float64, breaching bool, ok bool) {
	if agent.GetString("status") == "offline" {
		return 1, true, true
	}
	lastSeen := agent.GetDateTime("last_seen").Time()
	if lastSeen.IsZero() || now.Sub(lastSeen) >= agentOfflineHeartbeatTimeout {
		return 1, true, true
	}
	return 0, false, true
}

// evalProcessDown reports whether no process in the agent's latest
// "processes" metric snapshot has a name or cmdline containing target
// (case-insensitive substring match). ok is false when no snapshot has been
// recorded yet, matching getLatestMetricValue's "skip this cycle" behavior.
// value is 0 when target is absent (breaching) and 1 when present.
func (e *Engine) evalProcessDown(agentID, target string) (value float64, breaching bool, ok bool) {
	data, ok := e.latestMetricData(agentID, "processes")
	if !ok {
		return 0, false, false
	}

	lowerTarget := strings.ToLower(target)
	procs, _ := data["processes"].([]any)
	for _, p := range procs {
		proc, ok := p.(map[string]any)
		if !ok {
			continue
		}
		name, _ := proc["name"].(string)
		cmdline, _ := proc["cmdline"].(string)
		if strings.Contains(strings.ToLower(name), lowerTarget) || strings.Contains(strings.ToLower(cmdline), lowerTarget) {
			return 1, false, true
		}
	}
	return 0, true, true
}

// evalServiceFailed reports whether the agent's latest "services" metric
// snapshot lists target (exact, case-insensitive match on the service name)
// in a failed/inactive/dead state, or does not list it at all — the
// services collector (internal/agent/collector/services.go) already drops
// units it considers "inactive", so a target that has stopped normally is
// indistinguishable from one that was never listed, and both count as a
// breach. ok is false when no snapshot has been recorded yet. value is 1
// when found and healthy, 0 when failed or absent (breaching).
func (e *Engine) evalServiceFailed(agentID, target string) (value float64, breaching bool, ok bool) {
	data, ok := e.latestMetricData(agentID, "services")
	if !ok {
		return 0, false, false
	}

	services, _ := data["services"].([]any)
	for _, s := range services {
		svc, ok := s.(map[string]any)
		if !ok {
			continue
		}
		name, _ := svc["name"].(string)
		if !strings.EqualFold(name, target) {
			continue
		}
		active, _ := svc["active"].(string)
		sub, _ := svc["sub"].(string)
		if isFailedServiceState(active) || isFailedServiceState(sub) {
			return 0, true, true
		}
		return 1, false, true
	}
	return 0, true, true // not listed at all -> treated as stopped/failed
}

// isFailedServiceState reports whether a systemd "active" or "sub" state
// string indicates the service is not running.
func isFailedServiceState(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "failed", "inactive", "dead":
		return true
	default:
		return false
	}
}

// evalCveCount reports the count of CVE findings from the agent's latest
// "cve_scan" metric snapshot — critical+high severity by default, or
// critical only when target == "critical" (case-insensitive) — and
// whether it breaches condition/threshold, unlike agent_offline/
// process_down/service_failed which ignore condition/threshold entirely.
// ok is false when no scan has been recorded yet or the latest one
// reported available=false (no scanner installed, or scanning disabled) —
// matching getLatestMetricValue's "skip this cycle" behavior for missing
// data, since there is nothing meaningful to compare against yet.
func (e *Engine) evalCveCount(agentID, target, condition string, threshold float64) (value float64, breaching bool, ok bool) {
	data, ok := e.latestMetricData(agentID, "cve_scan")
	if !ok {
		return 0, false, false
	}
	if available, _ := data["available"].(bool); !available {
		return 0, false, false
	}
	totals, _ := data["totals"].(map[string]any)
	if totals == nil {
		return 0, false, false
	}

	critical, _ := toFloat64(totals["critical"])
	if strings.EqualFold(strings.TrimSpace(target), "critical") {
		value = critical
	} else {
		high, _ := toFloat64(totals["high"])
		value = critical + high
	}

	return value, e.checkCondition(value, condition, threshold), true
}

// logMatchScanCap bounds how many candidate "logs" rows evalLogMatch
// inspects per evaluation, so a very high-volume agent (or an overly
// broad pattern) cannot make a single alert-evaluation cycle scan
// unboundedly many rows. evalLogMatch only needs to know whether count >=
// threshold, not the exact count once it is already far past it, so
// reporting exactly logMatchScanCap when the true count is higher is an
// acceptable tradeoff — it is still comfortably above any sane threshold.
const logMatchScanCap = 5000

// evalLogMatch reports how many of the agent's log entries
// (internal/hub/logs, the "logs" collection) in the last window seconds
// match target, and whether that count breaches threshold. target is
// either a case-insensitive substring — matched via the "logs"
// collection's indexed "~" LIKE filter, so the database itself narrows
// the candidate set — or a /regex/ pattern (delimited by leading and
// trailing slashes), evaluated in Go against a candidate set fetched by
// agent+time range alone and capped at logMatchScanCap rows. condition is
// intentionally ignored: a log_match rule always breaches on "count >=
// threshold" (see internal/hub/migrations/rule_log_match.go). ok is false
// when target is empty (nothing meaningful to match) or the target is a
// malformed /regex/ (never breaches rather than silently falling back to
// a literal match on the delimited text, which would surprise whoever
// configured it).
func (e *Engine) evalLogMatch(agentID, target string, threshold float64, window time.Duration) (value float64, breaching bool, ok bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return 0, false, false
	}
	since := e.clock().UTC().Add(-window).Format("2006-01-02 15:04:05.000Z")

	if pattern, isRegex := parseLogMatchRegex(target); isRegex {
		if pattern == nil {
			return 0, false, false
		}
		records, err := e.app.FindRecordsByFilter(
			"logs",
			"agent_id = {:agentId} && ts >= {:since}",
			"",
			logMatchScanCap,
			0,
			map[string]any{"agentId": agentID, "since": since},
		)
		if err != nil {
			return 0, false, false
		}
		count := 0
		for _, r := range records {
			if pattern.MatchString(r.GetString("message")) {
				count++
			}
		}
		value = float64(count)
		return value, value >= threshold, true
	}

	records, err := e.app.FindRecordsByFilter(
		"logs",
		"agent_id = {:agentId} && ts >= {:since} && message ~ {:pattern}",
		"",
		logMatchScanCap,
		0,
		map[string]any{"agentId": agentID, "since": since, "pattern": target},
	)
	if err != nil {
		return 0, false, false
	}
	value = float64(len(records))
	return value, value >= threshold, true
}

// parseLogMatchRegex reports whether target is a /regex/-delimited
// pattern and, if so, compiles it. A malformed regex returns
// isRegex=true with a nil pattern, distinguishing "not a regex at all"
// from "was meant as one but doesn't compile" — evalLogMatch treats the
// latter as never-breaching.
func parseLogMatchRegex(target string) (pattern *regexp.Regexp, isRegex bool) {
	if len(target) < 2 || !strings.HasPrefix(target, "/") || !strings.HasSuffix(target, "/") {
		return nil, false
	}
	body := target[1 : len(target)-1]
	re, err := regexp.Compile(body)
	if err != nil {
		return nil, true
	}
	return re, true
}

// latestMetricData loads and JSON-decodes the most recent metrics record's
// data field for agentID/metricType, returning ok=false when no such
// snapshot exists or it fails to decode.
func (e *Engine) latestMetricData(agentID, metricType string) (map[string]any, bool) {
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
		return nil, false
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(records[0].GetString("data")), &data); err != nil {
		return nil, false
	}
	return data, true
}

// getLatestMetricValue retrieves the latest numeric value for a metric type.
// It extracts the primary value: total_percent for cpu, used_percent for memory/disk,
// bytes_recv for network.
func (e *Engine) getLatestMetricValue(agentID, metricType string) (float64, bool) {
	data, ok := e.latestMetricData(agentID, metricType)
	if !ok {
		return 0, false
	}

	// Extract the primary metric value based on type.
	var val float64
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

// updateState manages the state machine for a rule+subject pair, where
// "subject" is an agent id for every metric_type except
// check_down/cert_expiry (a check id there instead — see ruleAgentKey's
// doc comment; the parameter is still named agentID here since that is
// still the overwhelmingly common case). It is idempotent by construction:
// at most one "firing" alerts record ever exists per (rule_id, subject id)
// pair at a time.
//
//   - First breach: starts the duration timer, does not fire yet.
//   - Breach persists past duration, not yet fired: creates exactly one
//     alert record. If silenced is true, no notification is dispatched and
//     the alert's silenced field is set instead.
//   - Breach persists, already fired: refreshes the *same* record's
//     value/message/severity/silenced instead of inserting a new row. A
//     silenced or acknowledged alert is never re-notified or escalated; an
//     unsilenced, unacknowledged one re-dispatches a notification once the
//     cooldown has elapsed since the last notification (or immediately if
//     it has never been notified, including right after a silence ends).
//   - Breach stops while firing: resolves the active record (status +
//     resolved_at) and resets to OK so the next breach starts a fresh
//     incident (a new record).
func (e *Engine) updateState(rule *core.Record, agentID string, breaching bool, value float64, severity string, duration time.Duration, silenced bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	key := ruleAgentKey{RuleID: rule.Id, AgentID: agentID}
	state, exists := e.states[key]
	if !exists {
		state = &ruleAgentState{State: StateOK}
		e.states[key] = state
	}

	now := e.clock()

	if !breaching {
		if state.State == StateWarning || state.State == StateCritical {
			if state.ActiveAlertID != "" {
				e.resolveAlert(state.ActiveAlertID, rule)
			}
			*state = ruleAgentState{State: StateOK}
		}
		return
	}

	if state.State == StateOK || state.State == StateResolved {
		// Start tracking a new breach period. Nothing fires yet — the
		// duration must elapse first.
		state.FirstBreachAt = now
		state.State = StateWarning
		state.Fired = false
		state.Silenced = false
		return
	}

	if !state.Fired {
		// Already tracking a breach but haven't fired for it yet.
		if now.Sub(state.FirstBreachAt) < duration {
			return
		}

		newState := StateCritical
		if severity == "warning" {
			newState = StateWarning
		}
		state.State = newState

		alertID := e.fireAlert(rule, agentID, value, severity, silenced)
		if alertID != "" {
			state.ActiveAlertID = alertID
			state.Fired = true
			state.FiredAt = now
			state.Silenced = silenced
			if !silenced {
				state.LastNotifiedAt = now
			}
			// Left zero when silenced, so the first non-silenced
			// evaluation notifies immediately instead of waiting out a
			// cooldown that never actually started.
		}
		return
	}

	// Already firing: keep the existing record current rather than
	// inserting a duplicate.
	if state.ActiveAlertID == "" {
		return
	}
	record := e.refreshFiringAlert(state.ActiveAlertID, rule, agentID, value, severity, silenced)
	if record == nil {
		return
	}

	if silenced {
		state.Silenced = true
		return // no notification, no escalation, while silenced
	}

	wasSilenced := state.Silenced
	state.Silenced = false

	acked := record.GetString("acknowledged_at") != ""
	if acked {
		return // acknowledged alerts are never re-notified or escalated
	}

	if wasSilenced || state.LastNotifiedAt.IsZero() || now.Sub(state.LastNotifiedAt) >= e.cooldownPeriod {
		e.renotify(record, rule)
		state.LastNotifiedAt = now
	}

	e.maybeEscalate(rule, record, state, now)
}

// maybeEscalate dispatches a one-time escalation notification once an
// unacknowledged, unsilenced alert has been firing for at least the rule's
// escalation_after seconds, then records escalated_at so it never
// re-escalates within the same firing episode. escalation_after <= 0
// disables escalation for the rule.
func (e *Engine) maybeEscalate(rule *core.Record, record *core.Record, state *ruleAgentState, now time.Time) {
	escalationAfter := rule.GetFloat("escalation_after")
	if escalationAfter <= 0 {
		return
	}
	if record.GetString("escalated_at") != "" {
		return
	}
	if now.Sub(state.FiredAt) < time.Duration(escalationAfter)*time.Second {
		return
	}

	record.Set("escalated_at", now.UTC().Format("2006-01-02 15:04:05.000Z"))
	if err := e.app.Save(record); err != nil {
		slog.Error("failed to mark alert escalated", "alert_id", record.Id, "error", err)
		return
	}

	slog.Info("alert escalated", "alert_id", record.Id, "rule_id", rule.Id)
	if e.escalateFunc != nil {
		e.escalateFunc(e.app, record, rule)
	}
}

// buildMessage renders the human-readable alert message shared by
// fireAlert and refreshFiringAlert, so an updated record reads the same way
// a freshly-fired one would. subjectID is an agent id for every metric_type
// except check_down/cert_expiry, where it is a check id instead (see
// ruleAgentKey's doc comment). It also appends the agent's tags (when any
// are set) so a channel that only surfaces the raw message text (e.g. a
// webhook payload) still shows which tagged group of agents is affected —
// checks have no such tags dimension, so that suffix is skipped for them.
func (e *Engine) buildMessage(rule *core.Record, subjectID string, value float64, severity string) string {
	metricType := rule.GetString("metric_type")

	if isCheckMetricType(metricType) {
		name, target := e.getCheckNameTarget(subjectID)
		switch metricType {
		case "check_down":
			return fmt.Sprintf("[%s] check_down: %s (%s) is down", severity, name, target)
		case "cert_expiry":
			return fmt.Sprintf("[%s] cert_expiry: %s (%s) certificate expires in %.0f day(s)", severity, name, target, value)
		}
	}

	agentName := e.getAgentHostname(subjectID)
	target := rule.GetString("target")

	var base string
	switch metricType {
	case "agent_offline":
		base = fmt.Sprintf("[%s] agent offline: %s has not been seen since %s", severity, agentName, e.agentLastSeen(subjectID))
	case "process_down":
		base = fmt.Sprintf("[%s] process_down on %s: process %q not found in the latest snapshot", severity, agentName, target)
	case "service_failed":
		base = fmt.Sprintf("[%s] service_failed on %s: service %q is failed/inactive", severity, agentName, target)
	case "cve_count":
		scope := "critical+high"
		if strings.EqualFold(strings.TrimSpace(target), "critical") {
			scope = "critical"
		}
		base = fmt.Sprintf("[%s] cve_count on %s: %s CVE count is %.0f", severity, agentName, scope, value)
	case "log_match":
		base = fmt.Sprintf("[%s] log_match on %s: %.0f log line(s) matched %q (threshold %.0f)",
			severity, agentName, value, target, rule.GetFloat("threshold"))
	default:
		condition := rule.GetString("condition")
		threshold := rule.GetFloat("threshold")
		condStr := ">"
		switch condition {
		case "lt":
			condStr = "<"
		case "eq":
			condStr = "="
		}
		base = fmt.Sprintf("[%s] %s on %s: %s %s %.1f (current: %.1f)",
			severity, metricType, agentName, metricType, condStr, threshold, value)
	}

	if tags := e.getAgentTags(subjectID); len(tags) > 0 {
		base += fmt.Sprintf(" [tags: %s]", strings.Join(tags, ", "))
	}
	return base
}

// fireAlert creates a new alert record for a newly-detected incident.
// subjectID is an agent id for every metric_type except
// check_down/cert_expiry, where it is a check id instead — set on
// alerts.check_id rather than alerts.agent_id, which is left empty (see
// ruleAgentKey's doc comment and migrations/rule_check_types.go). It
// always persists the record (with silenced set accordingly) so the
// incident is tracked even while silenced, but only dispatches a
// notification when silenced is false. Returns the new record's ID, or ""
// on failure.
func (e *Engine) fireAlert(rule *core.Record, subjectID string, value float64, severity string, silenced bool) string {
	collection, err := e.app.FindCollectionByNameOrId("alerts")
	if err != nil {
		slog.Error("alerts collection not found", "error", err)
		return ""
	}

	metricType := rule.GetString("metric_type")
	message := e.buildMessage(rule, subjectID, value, severity)
	now := e.clock().UTC().Format("2006-01-02 15:04:05.000Z")

	record := core.NewRecord(collection)
	record.Set("rule_id", rule.Id)
	if isCheckMetricType(metricType) {
		record.Set("check_id", subjectID)
	} else {
		record.Set("agent_id", subjectID)
	}
	record.Set("status", "firing")
	record.Set("value", value)
	record.Set("message", message)
	record.Set("fired_at", now)
	record.Set("silenced", silenced)

	if err := e.app.Save(record); err != nil {
		slog.Error("failed to save alert", "rule_id", rule.Id, "subject_id", subjectID, "error", err)
		return ""
	}

	slog.Info("alert fired", "rule_id", rule.Id, "subject_id", subjectID, "alert_id", record.Id, "severity", severity, "value", value, "silenced", silenced, "message", message)

	if silenced {
		slog.Info("alert fired while silenced, notification suppressed", "alert_id", record.Id)
	} else {
		e.dispatch(record, rule)
	}

	return record.Id
}

// refreshFiringAlert updates the value/message/silenced state of an alert
// that is still actively breaching. This is what keeps a
// persistently-breaching rule+agent pair to exactly one open "alerts" row
// instead of inserting a new one on every evaluation cycle — the root cause
// of the alert storm. It returns the updated record (or nil on failure) so
// the caller can pass it straight to a notifier without a second lookup.
func (e *Engine) refreshFiringAlert(alertID string, rule *core.Record, subjectID string, value float64, severity string, silenced bool) *core.Record {
	record, err := e.app.FindRecordById("alerts", alertID)
	if err != nil {
		slog.Error("failed to find firing alert to refresh", "alert_id", alertID, "error", err)
		return nil
	}

	record.Set("value", value)
	record.Set("message", e.buildMessage(rule, subjectID, value, severity))
	record.Set("silenced", silenced)

	if err := e.app.Save(record); err != nil {
		slog.Error("failed to refresh alert", "alert_id", alertID, "error", err)
		return nil
	}
	return record
}

// renotify re-dispatches a notification for an alert that is still
// breaching after the cooldown period has elapsed. It never creates or
// duplicates a database record — only fireAlert does that.
func (e *Engine) renotify(record *core.Record, rule *core.Record) {
	if record == nil {
		return
	}
	slog.Info("alert re-notified (still breaching)", "alert_id", record.Id, "rule_id", rule.Id)
	e.dispatch(record, rule)
}

// dispatch invokes the registered NotifyFunc, if any.
func (e *Engine) dispatch(alert *core.Record, rule *core.Record) {
	if e.notifyFunc != nil {
		e.notifyFunc(e.app, alert, rule)
	}
}

// resolveAlert marks an alert as resolved and dispatches a resolution
// notification through the same NotifyFunc used for firing/re-firing (see
// notify.Service.Dispatch, which renders a distinct "resolved" message via
// RenderMessage and lets channels like PagerDuty send an event_action:
// "resolve" using the alert's ID as the dedup key). Acknowledgement and
// escalation fields are intentionally left untouched so they remain visible
// in the alert's history after resolution. rule is passed through only for
// the notifier callback — resolution itself never re-reads rule fields.
func (e *Engine) resolveAlert(alertID string, rule *core.Record) {
	record, err := e.app.FindRecordById("alerts", alertID)
	if err != nil {
		slog.Error("failed to find alert for resolution", "alert_id", alertID, "error", err)
		return
	}

	now := e.clock().UTC().Format("2006-01-02 15:04:05.000Z")
	record.Set("status", "resolved")
	record.Set("resolved_at", now)

	if err := e.app.Save(record); err != nil {
		slog.Error("failed to resolve alert", "alert_id", alertID, "error", err)
		return
	}

	slog.Info("alert resolved", "alert_id", alertID)
	e.dispatch(record, rule)
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

// getAgentTags looks up the tags for an agent ID, returning nil (rather
// than erroring) when the agent cannot be found or has no tags set.
func (e *Engine) getAgentTags(agentID string) []string {
	record, err := e.app.FindRecordById("agents", agentID)
	if err != nil {
		return nil
	}
	return record.GetStringSlice("tags")
}

// agentLastSeen returns a human-readable last_seen timestamp for an agent,
// falling back to a neutral phrase when unknown.
func (e *Engine) agentLastSeen(agentID string) string {
	record, err := e.app.FindRecordById("agents", agentID)
	if err != nil {
		return "an unknown time"
	}
	lastSeen := record.GetString("last_seen")
	if lastSeen == "" {
		return "an unknown time"
	}
	return lastSeen
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
