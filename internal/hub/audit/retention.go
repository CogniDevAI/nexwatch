package audit

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// SettingRetentionDays is the "settings" collection key controlling how
// long "audit_log" rows are kept before being purged, matching the
// key/value-JSON convention used elsewhere in the hub (see
// internal/hub/checks.SettingRetentionDays, internal/hub/backup).
const SettingRetentionDays = "audit_retention_days"

const defaultRetentionDays = 180

// resolveRetentionDays reads SettingRetentionDays from the "settings"
// collection, accepting either a raw JSON number or a JSON string (the UI
// settings form convention encodes numeric settings as a quoted string),
// and falls back to defaultRetentionDays when the setting is missing,
// unreadable, or not a positive number.
func resolveRetentionDays(app core.App) int {
	record, err := app.FindFirstRecordByFilter(
		"settings",
		"key = {:key}",
		map[string]any{"key": SettingRetentionDays},
	)
	if err != nil {
		return defaultRetentionDays
	}

	var v any
	if err := json.Unmarshal([]byte(record.GetString("value")), &v); err != nil {
		return defaultRetentionDays
	}

	switch n := v.(type) {
	case float64:
		if n > 0 {
			return int(n)
		}
	case string:
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultRetentionDays
}

// PurgeOld deletes "audit_log" rows older than the configured retention
// period (default defaultRetentionDays). Intended to be called from an
// existing periodic retention loop (see metrics.Downsampler.purgeOldData)
// rather than running its own ticker.
func PurgeOld(app core.App) {
	retentionDays := resolveRetentionDays(app)
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour).Format("2006-01-02 15:04:05.000Z")

	records, err := app.FindRecordsByFilter(
		"audit_log",
		"created < {:cutoff}",
		"",
		5000,
		0,
		map[string]any{"cutoff": cutoff},
	)
	if err != nil {
		slog.Error("audit log retention query failed", "error", err)
		return
	}

	for _, r := range records {
		if err := app.Delete(r); err != nil {
			slog.Error("failed to delete old audit log entry", "id", r.Id, "error", err)
		}
	}
}
