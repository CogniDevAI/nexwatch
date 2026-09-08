package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/hub/checks"
	uptimepkg "github.com/CogniDevAI/nexwatch/internal/hub/uptime"
)

// Settings keys backing the public status page (see ui/DESIGN.md § Public
// status page). All are read via the settingXxx helpers in settings.go, so
// a missing/unset key always resolves to a safe default (page disabled,
// empty item list).
const (
	statusPageEnabledKey        = "status_page_enabled"
	statusPageTitleKey          = "status_page_title"
	statusPageDescriptionKey    = "status_page_description"
	statusPageItemsKey          = "status_page_items"
	statusPageShowUptimeDaysKey = "status_page_show_uptime_days"

	defaultStatusPageTitle    = "Service status"
	defaultShowUptimeDays     = 30
	minShowUptimeDays         = 1
	maxShowUptimeDays         = 90
	statusPageRateLimitPerMin = 60
)

// statusPageItemConfig is one entry of the status_page_items setting — an
// admin-curated allowlist of checks/agents to expose, each with its own
// public-facing label. Only Type/ID/Label are read from the stored JSON;
// any other key an older/future UI might write is ignored.
type statusPageItemConfig struct {
	Type  string `json:"type"` // "check" | "agent"
	ID    string `json:"id"`
	Label string `json:"label"`
}

// publicStatusChecker is the subset of *checks.Scheduler the public status
// endpoint needs — just the in-memory latest-result snapshot, not the
// ability to trigger a run. Declared narrowly here (rather than reusing
// CheckRunner from checks_routes.go) since RegisterPublicStatusRoute has no
// reason to require RunOnce; *checks.Scheduler satisfies both.
type publicStatusChecker interface {
	Snapshot() map[string]checks.Snapshot
}

type publicStatusDaily struct {
	Date      string  `json:"date"`
	Uptime    float64 `json:"uptime"`
	Incidents int     `json:"incidents"`
}

type publicStatusItem struct {
	Type           string              `json:"type"`
	Label          string              `json:"label"`
	Status         string              `json:"status"` // operational | degraded | down | unknown
	Uptime24h      float64             `json:"uptime_24h"`
	Uptime7d       float64             `json:"uptime_7d"`
	Uptime30d      float64             `json:"uptime_30d"`
	LatencyMs      *float64            `json:"latency_ms,omitempty"`
	Daily          []publicStatusDaily `json:"daily"`
	LastIncidentAt string              `json:"last_incident_at,omitempty"`
}

type publicStatusResponse struct {
	Title       string             `json:"title"`
	Description string             `json:"description,omitempty"`
	Overall     string             `json:"overall"`
	UpdatedAt   string             `json:"updated_at"`
	Items       []publicStatusItem `json:"items"`
}

// RegisterPublicStatusRoute registers the unauthenticated GET
// /api/public/status endpoint directly on the top-level router — outside
// both the "/api/custom" auth group and that group's rate-free assumptions,
// since this is the one route on the hub meant to be linked from a public
// status page and hit by anonymous visitors. It is intentionally NOT
// returned in a []RegisteredRoute slice and NOT wired into
// docs/openapi_test.go's coverage check: that check only guards
// authenticated /api/custom/* routes (see RegisterHealthRoute for the same
// convention with /healthz).
func RegisterPublicStatusRoute(se *core.ServeEvent, runner publicStatusChecker) {
	registerPublicStatusRoute(se, runner, newIPRateLimiter())
}

// registerPublicStatusRoute is the unexported worker behind
// RegisterPublicStatusRoute, taking an explicit limiter so tests can inject
// one with pre-seeded state (see public_status_test.go's rate-limit test).
func registerPublicStatusRoute(se *core.ServeEvent, runner publicStatusChecker, limiter *ipRateLimiter) {
	se.Router.GET("/api/public/status", func(e *core.RequestEvent) error {
		return handlePublicStatus(e, runner, limiter)
	})
}

func handlePublicStatus(e *core.RequestEvent, runner publicStatusChecker, limiter *ipRateLimiter) error {
	ip := clientIP(e.Request)
	if !limiter.Allow(ip, statusPageRateLimitPerMin, time.Minute) {
		return e.JSON(http.StatusTooManyRequests, map[string]string{
			"error": "too many requests",
		})
	}

	if !settingBool(e.App, statusPageEnabledKey, false) {
		return e.JSON(http.StatusNotFound, map[string]string{
			"error": "status page is not enabled",
		})
	}

	title := settingString(e.App, statusPageTitleKey, defaultStatusPageTitle)
	description := settingString(e.App, statusPageDescriptionKey, "")
	showDays := settingInt(e.App, statusPageShowUptimeDaysKey, defaultShowUptimeDays)
	if showDays < minShowUptimeDays {
		showDays = minShowUptimeDays
	}
	if showDays > maxShowUptimeDays {
		showDays = maxShowUptimeDays
	}

	var configs []statusPageItemConfig
	if raw, ok := settingRaw(e.App, statusPageItemsKey); ok {
		_ = json.Unmarshal([]byte(raw), &configs) // malformed config -> empty page, not an error
	}

	now := time.Now().UTC()
	snapshot := runner.Snapshot()

	items := make([]publicStatusItem, 0, len(configs))
	for i, cfg := range configs {
		var item publicStatusItem
		var ok bool
		switch cfg.Type {
		case "check":
			item, ok = buildCheckStatusItem(e.App, cfg, snapshot, showDays, now)
		case "agent":
			item, ok = buildAgentStatusItem(e.App, cfg, showDays, now)
		default:
			ok = false
		}
		if !ok {
			continue // unknown type or the referenced record no longer exists
		}
		if item.Label == "" {
			item.Label = fmt.Sprintf("%s %d", capitalize(cfg.Type), i+1)
		}
		items = append(items, item)
	}

	resp := publicStatusResponse{
		Title:       title,
		Description: description,
		Overall:     overallStatus(items),
		UpdatedAt:   now.Format("2006-01-02T15:04:05Z"),
		Items:       items,
	}

	e.Response.Header().Set("Cache-Control", "max-age=30")
	return e.JSON(http.StatusOK, resp)
}

// overallStatus reduces every item's status to the single worst one, so the
// page can show one banner without the visitor scanning every row. An empty
// item list (nothing configured yet) reads as "unknown" rather than
// falsely claiming everything is operational.
func overallStatus(items []publicStatusItem) string {
	if len(items) == 0 {
		return "unknown"
	}
	rank := map[string]int{"operational": 0, "unknown": 1, "degraded": 2, "down": 3}
	worst := "operational"
	for _, it := range items {
		if rank[it.Status] > rank[worst] {
			worst = it.Status
		}
	}
	return worst
}

// buildCheckStatusItem resolves one "check" item against the checks
// collection and the scheduler's live snapshot. ok is false when the
// referenced check no longer exists, so the caller can silently drop a
// stale item rather than surface a broken row on the public page.
func buildCheckStatusItem(app core.App, cfg statusPageItemConfig, snapshot map[string]checks.Snapshot, showDays int, now time.Time) (publicStatusItem, bool) {
	check, err := app.FindRecordById("checks", cfg.ID)
	if err != nil {
		return publicStatusItem{}, false
	}

	item := publicStatusItem{Type: "check", Label: cfg.Label}

	snap, known := snapshot[cfg.ID]
	switch {
	case !known:
		item.Status = "unknown"
	case snap.Status == "down":
		item.Status = "down"
	default:
		item.Status = "operational"
		if warnDays := check.GetInt("tls_expiry_warn_days"); warnDays > 0 && !snap.TLSExpiresAt.IsZero() {
			if time.Until(snap.TLSExpiresAt) <= time.Duration(warnDays)*24*time.Hour {
				item.Status = "degraded"
			}
		}
		latency := snap.LatencyMs
		item.LatencyMs = &latency
	}

	u24, u7, u30, daily, lastIncident := computeCheckMetrics(app, cfg.ID, showDays, now)
	item.Uptime24h = u24
	item.Uptime7d = u7
	item.Uptime30d = u30
	item.Daily = daily
	item.LastIncidentAt = lastIncident

	return item, true
}

// buildAgentStatusItem resolves one "agent" item against the agents
// collection and its alert history (as a proxy for downtime — see
// computeAgentMetrics). ok is false when the referenced agent no longer
// exists.
func buildAgentStatusItem(app core.App, cfg statusPageItemConfig, showDays int, now time.Time) (publicStatusItem, bool) {
	agent, err := app.FindRecordById("agents", cfg.ID)
	if err != nil {
		return publicStatusItem{}, false
	}

	item := publicStatusItem{Type: "agent", Label: cfg.Label}
	switch agent.GetString("status") {
	case "online":
		item.Status = "operational"
	case "offline":
		item.Status = "down"
	default:
		item.Status = "unknown"
	}

	u24, u7, u30, daily, lastIncident := computeAgentMetrics(app, cfg.ID, showDays, now)
	item.Uptime24h = u24
	item.Uptime7d = u7
	item.Uptime30d = u30
	item.Daily = daily
	item.LastIncidentAt = lastIncident

	return item, true
}

// checkDayBucket accumulates one day's check_results for uptime/incident
// computation.
type checkDayBucket struct {
	up, total, incidents int
}

// computeCheckMetrics derives a check's fixed-window uptime percentages, a
// day-by-day uptime/incident bar of length showDays, and the timestamp of
// its most recent "down" result, from a single ascending query over
// check_results covering max(showDays, 30) days. Uptime is a simple
// count-based ratio (up results / total results) rather than time-weighted
// by interval — every probe counts equally regardless of the check's
// configured interval, which is simpler and matches how the Checks page's
// own uptime_24h/7d already work (see ui/src/lib/checks.ts). A day with no
// probes at all reads as 100% uptime / 0 incidents: nothing was observed
// down, which is the more honest default for a check that didn't exist yet
// or was temporarily disabled, rather than implying an outage.
func computeCheckMetrics(app core.App, checkID string, showDays int, now time.Time) (uptime24h, uptime7d, uptime30d float64, daily []publicStatusDaily, lastIncidentAt string) {
	windowDays := showDays
	if windowDays < 30 {
		windowDays = 30
	}
	since := now.AddDate(0, 0, -windowDays)

	results, err := app.FindRecordsByFilter(
		"check_results",
		"check_id = {:id} && checked_at >= {:since}",
		"checked_at",
		20000,
		0,
		map[string]any{"id": checkID, "since": fmtTimestamp(since)},
	)
	if err != nil {
		results = nil
	}

	cutoff24 := now.Add(-24 * time.Hour)
	cutoff7 := now.AddDate(0, 0, -7)
	cutoff30 := now.AddDate(0, 0, -30)
	dailySince := now.AddDate(0, 0, -showDays)

	buckets := make(map[string]*checkDayBucket, showDays)
	order := make([]string, 0, showDays)
	for d := 0; d < showDays; d++ {
		day := dailySince.AddDate(0, 0, d).Format("2006-01-02")
		buckets[day] = &checkDayBucket{}
		order = append(order, day)
	}

	var up24, tot24, up7, tot7, up30, tot30 int
	lastStatus := ""
	for _, r := range results {
		ts := r.GetDateTime("checked_at").Time()
		up := r.GetString("status") == "up"

		if !ts.Before(cutoff24) {
			tot24++
			if up {
				up24++
			}
		}
		if !ts.Before(cutoff7) {
			tot7++
			if up {
				up7++
			}
		}
		if !ts.Before(cutoff30) {
			tot30++
			if up {
				up30++
			}
		}

		if !up {
			lastIncidentAt = r.GetString("checked_at")
		}

		if !ts.Before(dailySince) {
			day := ts.Format("2006-01-02")
			if acc, ok := buckets[day]; ok {
				acc.total++
				if up {
					acc.up++
				}
				if !up && lastStatus != "down" {
					acc.incidents++
				}
			}
		}
		lastStatus = r.GetString("status")
	}

	daily = make([]publicStatusDaily, 0, len(order))
	for _, day := range order {
		acc := buckets[day]
		uptime := 100.0
		if acc.total > 0 {
			uptime = round2(100 * float64(acc.up) / float64(acc.total))
		}
		daily = append(daily, publicStatusDaily{Date: day, Uptime: uptime, Incidents: acc.incidents})
	}

	uptime24h = pctOrFull(up24, tot24)
	uptime7d = pctOrFull(up7, tot7)
	uptime30d = pctOrFull(up30, tot30)
	return uptime24h, uptime7d, uptime30d, daily, lastIncidentAt
}

// computeAgentMetrics derives an agent's uptime/incident history from its
// alert history: every alert (any rule, any severity) fired against the
// agent is treated as a downtime interval from fired_at to resolved_at (or
// "now" if still firing). This is a pragmatic proxy, not a direct
// connectivity log — an agent that genuinely dropped offline without any
// alert rule configured to notice it will read as 100% uptime here. It is
// documented as a known limitation rather than hidden: see the "Status
// page" README section.
func computeAgentMetrics(app core.App, agentID string, showDays int, now time.Time) (uptime24h, uptime7d, uptime30d float64, daily []publicStatusDaily, lastIncidentAt string) {
	windowDays := showDays
	if windowDays < 30 {
		windowDays = 30
	}
	since := now.AddDate(0, 0, -windowDays)

	// fired_at <= now bounds the query without needing an OR clause for an
	// open-ended resolved_at; the in-memory pass below still needs to keep
	// every alert whose interval could overlap [since, now], so cap at a
	// generous limit rather than filtering resolved_at server-side.
	alerts, err := app.FindRecordsByFilter(
		"alerts",
		"agent_id = {:id} && fired_at <= {:now}",
		"fired_at",
		5000,
		0,
		map[string]any{"id": agentID, "now": fmtTimestamp(now)},
	)
	if err != nil {
		alerts = nil
	}

	var intervals []uptimepkg.Interval
	incidentsByDay := make(map[string]int)
	var lastIncidentTime time.Time
	for _, a := range alerts {
		fired := a.GetDateTime("fired_at").Time()
		end := now
		if resolvedStr := a.GetString("resolved_at"); resolvedStr != "" {
			if resolved := a.GetDateTime("resolved_at").Time(); !resolved.IsZero() {
				end = resolved
			}
		}
		if end.Before(fired) {
			end = fired
		}
		if end.Before(since) {
			continue // fully outside the window we care about
		}
		intervals = append(intervals, uptimepkg.Interval{Start: fired, End: end})

		if fired.After(lastIncidentTime) {
			lastIncidentTime = fired
			lastIncidentAt = a.GetString("fired_at")
		}
		incidentsByDay[fired.Format("2006-01-02")]++
	}

	merged := uptimepkg.Merge(intervals)

	uptime24h = round2(uptimepkg.UptimePercent(merged, now.Add(-24*time.Hour), now))
	uptime7d = round2(uptimepkg.UptimePercent(merged, now.AddDate(0, 0, -7), now))
	uptime30d = round2(uptimepkg.UptimePercent(merged, now.AddDate(0, 0, -30), now))

	dailySince := now.AddDate(0, 0, -showDays)
	daily = make([]publicStatusDaily, 0, showDays)
	for d := 0; d < showDays; d++ {
		dayStart := dailySince.AddDate(0, 0, d)
		dayEnd := dayStart.AddDate(0, 0, 1)
		if dayEnd.After(now) {
			dayEnd = now
		}
		uptime := 100.0
		if dayEnd.After(dayStart) {
			uptime = round2(uptimepkg.UptimePercent(merged, dayStart, dayEnd))
		}
		daily = append(daily, publicStatusDaily{
			Date:      dayStart.Format("2006-01-02"),
			Uptime:    uptime,
			Incidents: incidentsByDay[dayStart.Format("2006-01-02")],
		})
	}

	return uptime24h, uptime7d, uptime30d, daily, lastIncidentAt
}

func pctOrFull(up, total int) float64 {
	if total == 0 {
		return 100
	}
	return round2(100 * float64(up) / float64(total))
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}

func fmtTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05.000Z")
}

// clientIP extracts the request's remote IP for rate limiting, stripping
// the port from RemoteAddr. It intentionally ignores X-Forwarded-For:
// trusting a client-supplied header for rate-limit bucketing would let a
// visitor trivially bypass the limit by spoofing a new value per request,
// and a reverse-proxy deployment (see the README's nginx sample config)
// is expected to rate-limit at that layer too.
// capitalize upper-cases the first byte of s (ASCII-only; s is always one
// of the fixed literal strings "check"/"agent" here, never user input).
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ipRateLimiter is a simple in-memory sliding-window limiter keyed by
// client IP. It is intentionally not distributed/persisted — the public
// status endpoint is meant to be cheap and cacheable (Cache-Control:
// max-age=30), and a single hub process is the deployment model this
// codebase targets throughout (see README).
type ipRateLimiter struct {
	mu    sync.Mutex
	hits  map[string][]time.Time
	calls int
}

func newIPRateLimiter() *ipRateLimiter {
	return &ipRateLimiter{hits: make(map[string][]time.Time)}
}

// Allow reports whether ip may make another request under limit hits per
// window, recording this attempt regardless of the outcome. Every call
// prunes ip's own stale timestamps; every 1000th call across all IPs also
// sweeps the whole map of IPs with no recent activity, so the map doesn't
// grow unbounded across many distinct visitors over the process lifetime.
func (l *ipRateLimiter) Allow(ip string, limit int, window time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	recent := l.hits[ip][:0:0]
	for _, t := range l.hits[ip] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	allowed := len(recent) < limit
	l.hits[ip] = append(recent, now)

	l.calls++
	if l.calls%1000 == 0 {
		for k, ts := range l.hits {
			if len(ts) == 0 || ts[len(ts)-1].Before(cutoff) {
				delete(l.hits, k)
			}
		}
	}

	return allowed
}
