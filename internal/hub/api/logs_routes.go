package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
)

// logsDateFormat matches the format every other collection in this hub
// stores PocketBase DateField values as.
const logsDateFormat = "2006-01-02 15:04:05.000Z"

// logsQueryDefaultLimit and logsQueryMaxLimit bound GET /api/custom/logs's
// "limit" query parameter.
const (
	logsQueryDefaultLimit = 100
	logsQueryMaxLimit     = 500
)

// logsUnitsLookback bounds how far back GET /api/custom/logs/units scans
// for distinct unit names.
const logsUnitsLookback = 24 * time.Hour

// logsUnitsScanCap bounds how many candidate rows GET /api/custom/logs/units
// inspects, so a very high-volume agent can't make the units dropdown's
// backing request unboundedly expensive.
const logsUnitsScanCap = 5000

// RegisterLogsRoutes registers log search/live-tail-support API routes on
// apiGroup, which the caller must already have bound with the desired auth
// middleware (e.g. apis.RequireAuth()) and mounted at the "/api/custom"
// prefix. It returns every route it registered, for openapi_test.go to
// cross-check against docs/openapi.yaml. Live tail itself uses PocketBase
// realtime directly against the "logs" collection (see its ListRule in
// internal/hub/migrations/logs.go) rather than a custom route.
func RegisterLogsRoutes(apiGroup *router.RouterGroup[*core.RequestEvent]) []RegisteredRoute {
	rec := newRouteRecorder(apiGroup, "/api/custom")

	// GET /api/custom/logs — filtered, paginated log search, newest first.
	rec.GET("/logs", handleLogsQuery)

	// GET /api/custom/logs/units — distinct units seen in the last 24h,
	// for populating the Logs page's unit filter.
	rec.GET("/logs/units", handleLogUnits)

	return rec.Registered
}

// logEntryResponse is one row of GET /api/custom/logs.
type logEntryResponse struct {
	ID      string            `json:"id"`
	AgentID string            `json:"agent_id"`
	Ts      int64             `json:"ts"`
	Source  string            `json:"source"`
	Unit    string            `json:"unit,omitempty"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

// handleLogsQuery implements GET /api/custom/logs?agent_id=&level=&unit=&q=&since=&until=&limit=&before=.
// Results are newest first; "before" is an opaque "<unixMillis>_<id>"
// cursor (the value of the previous response's next_before) for
// "load older".
func handleLogsQuery(e *core.RequestEvent) error {
	q := e.Request.URL.Query()

	limit := logsQueryDefaultLimit
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > logsQueryMaxLimit {
		limit = logsQueryMaxLimit
	}

	filter := "id != ''"
	params := map[string]any{}

	if agentID := q.Get("agent_id"); agentID != "" {
		filter += " && agent_id = {:agentId}"
		params["agentId"] = agentID
	}
	if level := q.Get("level"); level != "" {
		filter += " && level = {:level}"
		params["level"] = level
	}
	if unit := q.Get("unit"); unit != "" {
		filter += " && unit = {:unit}"
		params["unit"] = unit
	}
	if search := q.Get("q"); search != "" {
		// PocketBase's "~" operator is a case-insensitive LIKE/contains
		// match, exactly the "case-insensitive substring match" the API
		// contract promises.
		filter += " && message ~ {:q}"
		params["q"] = search
	}
	if since := q.Get("since"); since != "" {
		if ts, ok := parseUnixMillisParam(since); ok {
			filter += " && ts >= {:since}"
			params["since"] = ts.Format(logsDateFormat)
		}
	}
	if until := q.Get("until"); until != "" {
		if ts, ok := parseUnixMillisParam(until); ok {
			filter += " && ts <= {:until}"
			params["until"] = ts.Format(logsDateFormat)
		}
	}
	if before := q.Get("before"); before != "" {
		if cursorTs, cursorID, ok := parseLogsCursor(before); ok {
			filter += " && (ts < {:beforeTs} || (ts = {:beforeTs} && id < {:beforeId}))"
			params["beforeTs"] = cursorTs.Format(logsDateFormat)
			params["beforeId"] = cursorID
		}
	}

	records, err := e.App.FindRecordsByFilter("logs", filter, "-ts,-id", limit, 0, params)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query logs"})
	}

	entries := make([]logEntryResponse, 0, len(records))
	for _, r := range records {
		entries = append(entries, logEntryResponse{
			ID:      r.Id,
			AgentID: r.GetString("agent_id"),
			Ts:      r.GetDateTime("ts").Time().UnixMilli(),
			Source:  r.GetString("source"),
			Unit:    r.GetString("unit"),
			Level:   r.GetString("level"),
			Message: r.GetString("message"),
			Fields:  decodeLogFields(r.GetString("fields")),
		})
	}

	resp := map[string]any{"entries": entries}
	// Only offer a next page when this page was full — a short page means
	// there is nothing older left to fetch.
	if len(records) == limit {
		last := records[len(records)-1]
		resp["next_before"] = formatLogsCursor(last.GetDateTime("ts").Time(), last.Id)
	}

	return e.JSON(http.StatusOK, resp)
}

// handleLogUnits implements GET /api/custom/logs/units?agent_id=,
// returning the distinct "unit" values seen in the last 24h (optionally
// scoped to one agent), sorted alphabetically.
func handleLogUnits(e *core.RequestEvent) error {
	since := time.Now().UTC().Add(-logsUnitsLookback).Format(logsDateFormat)
	filter := "ts >= {:since}"
	params := map[string]any{"since": since}

	if agentID := e.Request.URL.Query().Get("agent_id"); agentID != "" {
		filter += " && agent_id = {:agentId}"
		params["agentId"] = agentID
	}

	records, err := e.App.FindRecordsByFilter("logs", filter, "-ts", logsUnitsScanCap, 0, params)
	if err != nil {
		return e.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to query log units"})
	}

	seen := make(map[string]bool)
	units := make([]string, 0)
	for _, r := range records {
		unit := r.GetString("unit")
		if unit == "" || seen[unit] {
			continue
		}
		seen[unit] = true
		units = append(units, unit)
	}
	sort.Strings(units)

	return e.JSON(http.StatusOK, map[string]any{"units": units})
}

// parseUnixMillisParam parses a "since"/"until" query value as unix
// milliseconds.
func parseUnixMillisParam(raw string) (time.Time, bool) {
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.UnixMilli(ms).UTC(), true
}

// formatLogsCursor builds the opaque "before" cursor for one row.
func formatLogsCursor(ts time.Time, id string) string {
	return strconv.FormatInt(ts.UnixMilli(), 10) + "_" + id
}

// parseLogsCursor parses a "before" cursor built by formatLogsCursor.
func parseLogsCursor(cursor string) (ts time.Time, id string, ok bool) {
	idx := strings.LastIndex(cursor, "_")
	if idx < 0 || idx == len(cursor)-1 {
		return time.Time{}, "", false
	}
	ms, err := strconv.ParseInt(cursor[:idx], 10, 64)
	if err != nil {
		return time.Time{}, "", false
	}
	return time.UnixMilli(ms).UTC(), cursor[idx+1:], true
}

// decodeLogFields best-effort decodes the JSON-encoded "fields" column,
// returning nil (rather than an error) for an empty or unparsable value —
// fields is supplementary metadata, never required for a usable response.
func decodeLogFields(raw string) map[string]string {
	if raw == "" || raw == "null" {
		return nil
	}
	var fields map[string]string
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil
	}
	return fields
}
