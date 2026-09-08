package logs

import (
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// SettingRetentionDays is the "settings" collection key controlling how
// long "logs" rows are kept before being purged, matching the key/
// value-JSON convention used elsewhere in the hub (see
// internal/hub/checks.SettingRetentionDays, internal/hub/audit).
const SettingRetentionDays = "logs_retention_days"

// SettingMaxRows is the "settings" collection key for the hard cap on
// total "logs" rows — enforced independently of age-based retention, so a
// burst of very recent, very high-volume logging can't blow past the
// hub's storage budget even before SettingRetentionDays would otherwise
// purge anything.
const SettingMaxRows = "logs_max_rows"

const (
	defaultRetentionDays = 3
	defaultMaxRows       = 2_000_000
	purgeBatchSize       = 500
)

// resolveIntSetting reads key from the "settings" collection, accepting
// either a raw JSON number or a JSON string (the UI settings form
// convention encodes numeric settings as a quoted string — see
// ui/src/pages/Settings.tsx), and falls back to fallback when the setting
// is missing, unreadable, or not a positive number.
func resolveIntSetting(app core.App, key string, fallback int) int {
	record, err := app.FindFirstRecordByFilter(
		"settings",
		"key = {:key}",
		map[string]any{"key": key},
	)
	if err != nil {
		return fallback
	}

	var v any
	if err := json.Unmarshal([]byte(record.GetString("value")), &v); err != nil {
		return fallback
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
	return fallback
}

func resolveRetentionDays(app core.App) int {
	return resolveIntSetting(app, SettingRetentionDays, defaultRetentionDays)
}

func resolveMaxRows(app core.App) int {
	return resolveIntSetting(app, SettingMaxRows, defaultMaxRows)
}

// PurgeOld deletes "logs" rows older than the configured retention period
// (default defaultRetentionDays), in batches so a large backlog doesn't
// attempt to load everything into memory at once. Intended to be called
// from the metrics downsampler's existing hourly retention loop (see
// internal/hub/metrics/downsampler.go's purgeOldData, which already calls
// audit.PurgeOld the same way) rather than running its own ticker.
func PurgeOld(app core.App) {
	days := resolveRetentionDays(app)
	cutoff := time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(dateFormat)

	for {
		records, err := app.FindRecordsByFilter(
			"logs",
			"ts < {:cutoff}",
			"",
			purgeBatchSize,
			0,
			map[string]any{"cutoff": cutoff},
		)
		if err != nil {
			slog.Error("logs retention query failed", "error", err)
			return
		}
		if len(records) == 0 {
			return
		}

		for _, r := range records {
			if err := app.Delete(r); err != nil {
				slog.Error("failed to delete old log entry", "id", r.Id, "error", err)
			}
		}

		if len(records) < purgeBatchSize {
			return
		}
	}
}

// EnforceMaxRows deletes the oldest "logs" rows once the total row count
// exceeds the configured hard cap (default defaultMaxRows), trimming back
// down to exactly the cap. A no-op when the collection is at or under the
// cap.
func EnforceMaxRows(app core.App) {
	maxRows := resolveMaxRows(app)

	total, err := app.CountRecords("logs")
	if err != nil {
		slog.Error("logs row count failed", "error", err)
		return
	}
	if total <= int64(maxRows) {
		return
	}

	excess := int(total - int64(maxRows))
	deleted := 0
	for deleted < excess {
		limit := purgeBatchSize
		if remaining := excess - deleted; remaining < limit {
			limit = remaining
		}

		// Oldest first (ascending "ts"), so trimming always removes the
		// least useful rows first.
		records, err := app.FindRecordsByFilter("logs", "id != ''", "ts", limit, 0)
		if err != nil {
			slog.Error("logs max-rows query failed", "error", err)
			return
		}
		if len(records) == 0 {
			return
		}

		for _, r := range records {
			if err := app.Delete(r); err != nil {
				slog.Error("failed to delete log entry over max rows", "id", r.Id, "error", err)
			}
			deleted++
		}
	}

	slog.Info("logs hard cap enforced", "max_rows", maxRows, "deleted", deleted)
}
