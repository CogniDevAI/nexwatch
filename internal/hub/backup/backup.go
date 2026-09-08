// Package backup schedules automatic daily backups of the hub's pb_data
// directory and prunes old backups beyond a configured retention count.
//
// Configuration lives in the hub's own "settings" collection (key/value
// pairs, see internal/hub/migrations/collections.go) rather than
// PocketBase's built-in Settings.Backups — this keeps backup configuration
// alongside the rest of NexWatch's user-editable settings and gives the UI
// a single place to manage it, instead of PocketBase's separate superuser
// dashboard settings.
package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/cron"
)

// Settings keys stored in the "settings" collection.
const (
	KeyEnabled = "backups_enabled"
	KeyCron    = "backup_cron"
	KeyKeep    = "backup_keep"

	// DefaultCron runs the backup daily at 03:00.
	DefaultCron = "0 3 * * *"
	// DefaultKeep retains the 7 most recent backups.
	DefaultKeep = 7

	// jobID identifies the cron job registered with app.Cron(), so a
	// subsequent call to Register (e.g. after a settings change) can safely
	// replace it rather than accumulating duplicate jobs.
	jobID = "nexwatch_backup"
)

// Settings is the resolved backup configuration.
type Settings struct {
	Enabled bool
	Cron    string
	Keep    int
}

// LoadSettings reads backup configuration from the "settings" collection,
// falling back to defaults for any key that is missing, unreadable, or the
// wrong type.
func LoadSettings(app core.App) Settings {
	s := Settings{Enabled: true, Cron: DefaultCron, Keep: DefaultKeep}

	if v, ok := getSetting(app, KeyEnabled); ok {
		if b, ok := v.(bool); ok {
			s.Enabled = b
		}
	}
	if v, ok := getSetting(app, KeyCron); ok {
		if str, ok := v.(string); ok && str != "" {
			s.Cron = str
		}
	}
	if v, ok := getSetting(app, KeyKeep); ok {
		if f, ok := toFloat64(v); ok {
			s.Keep = int(f)
		}
	}

	return s
}

func getSetting(app core.App, key string) (any, bool) {
	record, err := app.FindFirstRecordByFilter(
		"settings",
		"key = {:key}",
		map[string]any{"key": key},
	)
	if err != nil {
		return nil, false
	}

	raw := record.GetString("value")
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return nil, false
	}
	return v, true
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// Register loads Settings and, if enabled, schedules the "nexwatch_backup"
// cron job that performs a daily pb_data backup and retention pruning. It
// is a no-op (after removing any previously-registered job) when backups
// are disabled via settings.
func Register(app core.App) {
	settings := LoadSettings(app)

	app.Cron().Remove(jobID)

	if !settings.Enabled {
		slog.Info("scheduled backups disabled via settings")
		return
	}

	cronExpr := settings.Cron
	if _, err := cron.NewSchedule(cronExpr); err != nil {
		slog.Error("invalid backup_cron setting, falling back to default",
			"configured", cronExpr, "default", DefaultCron, "error", err)
		cronExpr = DefaultCron
	}

	app.Cron().MustAdd(jobID, cronExpr, func() {
		runBackup(app, settings.Keep)
	})
	slog.Info("scheduled backups enabled", "cron", cronExpr, "keep", settings.Keep)
}

// runBackup performs one backup and then prunes old backups beyond keep.
func runBackup(app core.App, keep int) {
	ctx := context.Background()
	name := fmt.Sprintf("nexwatch_%s.zip", time.Now().UTC().Format("20060102_150405"))

	if err := app.CreateBackup(ctx, name); err != nil {
		slog.Error("scheduled backup failed", "name", name, "error", err)
		return
	}
	slog.Info("scheduled backup created", "name", name)

	if err := pruneBackups(app, keep); err != nil {
		slog.Error("failed to prune old backups", "error", err)
	}
}

// backupInfo pairs a backup file's name with its modification time,
// decoupled from filesystem.System/blob.ListObject so the retention
// decision (selectBackupsForPruning) is a pure, easily-testable function.
type backupInfo struct {
	Name    string
	ModTime time.Time
}

// selectBackupsForPruning returns the names of backups to delete so that at
// most `keep` of the most recent backups remain. keep <= 0 means "keep
// everything" (pruning disabled).
func selectBackupsForPruning(backups []backupInfo, keep int) []string {
	if keep <= 0 || len(backups) <= keep {
		return nil
	}

	sorted := make([]backupInfo, len(backups))
	copy(sorted, backups)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ModTime.After(sorted[j].ModTime) })

	toRemove := make([]string, 0, len(sorted)-keep)
	for _, b := range sorted[keep:] {
		toRemove = append(toRemove, b.Name)
	}
	return toRemove
}

// pruneBackups lists all backups via the app's backup filesystem and
// deletes every one beyond the keep most recent, using
// selectBackupsForPruning for the actual retention decision.
func pruneBackups(app core.App, keep int) error {
	fsys, err := app.NewBackupsFilesystem()
	if err != nil {
		return fmt.Errorf("open backups filesystem: %w", err)
	}
	defer func() { _ = fsys.Close() }()

	objects, err := fsys.List("")
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}

	backups := make([]backupInfo, 0, len(objects))
	for _, o := range objects {
		backups = append(backups, backupInfo{Name: o.Key, ModTime: o.ModTime})
	}

	for _, name := range selectBackupsForPruning(backups, keep) {
		if err := fsys.Delete(name); err != nil {
			slog.Error("failed to delete old backup", "name", name, "error", err)
			continue
		}
		slog.Info("deleted old backup", "name", name)
	}

	return nil
}
