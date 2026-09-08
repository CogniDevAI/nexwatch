package checks

import (
	"context"
	"encoding/json"
	"log/slog"
	"strconv"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// SettingRetentionDays is the "settings" collection key controlling how
// long "check_results" rows are kept before being purged (see
// internal/hub/backup for the same key/value-JSON convention used by other
// hub settings).
const SettingRetentionDays = "check_results_retention_days"

const defaultRetentionDays = 7

// resolveRetentionDays reads SettingRetentionDays from the "settings"
// collection, accepting either a raw JSON number or a JSON string (the UI
// settings form convention seen elsewhere in this codebase encodes numeric
// settings as a quoted string — see ui/src/pages/Settings.tsx), and falls
// back to defaultRetentionDays when the setting is missing, unreadable, or
// not a positive number.
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

// runRetentionLoop periodically purges check_results rows older than the
// configured retention period, mirroring metrics.Downsampler's retention
// loop shape (an initial delay, then an hourly ticker).
func (s *Scheduler) runRetentionLoop(ctx context.Context) {
	timer := time.NewTimer(2 * time.Minute)
	select {
	case <-timer.C:
	case <-ctx.Done():
		timer.Stop()
		return
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.purgeOldResults()
		case <-ctx.Done():
			return
		}
	}
}

// purgeOldResults deletes check_results rows older than the configured
// retention period, in batches, so a large backlog doesn't attempt to load
// everything into memory at once.
func (s *Scheduler) purgeOldResults() {
	days := resolveRetentionDays(s.app)
	cutoff := s.clock().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	cutoffStr := cutoff.Format("2006-01-02 15:04:05.000Z")

	const batchSize = 200
	for {
		records, err := s.app.FindRecordsByFilter(
			"check_results",
			"checked_at < {:cutoff}",
			"",
			batchSize,
			0,
			map[string]any{"cutoff": cutoffStr},
		)
		if err != nil || len(records) == 0 {
			return
		}

		for _, r := range records {
			if err := s.app.Delete(r); err != nil {
				slog.Error("failed to delete old check result", "id", r.Id, "error", err)
			}
		}

		if len(records) < batchSize {
			return
		}
	}
}
