package backup

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Registers the app's migrations so tests.NewTestApp() creates the
	// "settings" collection this package reads configuration from.
	_ "github.com/CogniDevAI/nexwatch/internal/hub/migrations"
)

func newTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

func setSetting(t *testing.T, app core.App, key string, value any) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		t.Fatalf("find settings collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("key", key)
	rec.Set("value", value)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save setting %s: %v", key, err)
	}
}

// TestLoadSettings_DefaultsWhenUnset asserts the documented defaults:
// backups_enabled=true, backup_cron="0 3 * * *", backup_keep=7.
func TestLoadSettings_DefaultsWhenUnset(t *testing.T) {
	app := newTestApp(t)

	got := LoadSettings(app)
	want := Settings{Enabled: true, Cron: DefaultCron, Keep: DefaultKeep}
	if got != want {
		t.Errorf("LoadSettings() = %+v, want %+v", got, want)
	}
}

// TestLoadSettings_ReadsConfiguredValues asserts each settings key
// overrides its corresponding default independently.
func TestLoadSettings_ReadsConfiguredValues(t *testing.T) {
	app := newTestApp(t)
	setSetting(t, app, KeyEnabled, false)
	setSetting(t, app, KeyCron, "0 4 * * *")
	setSetting(t, app, KeyKeep, 14)

	got := LoadSettings(app)
	want := Settings{Enabled: false, Cron: "0 4 * * *", Keep: 14}
	if got != want {
		t.Errorf("LoadSettings() = %+v, want %+v", got, want)
	}
}

// TestSelectBackupsForPruning covers the pure retention decision: which
// backup names to delete given a list of (name, mtime) pairs and a keep
// count, without touching any real filesystem.
func TestSelectBackupsForPruning(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	mk := func(daysAgo int) time.Time { return base.Add(-time.Duration(daysAgo) * 24 * time.Hour) }

	tests := []struct {
		name    string
		backups []backupInfo
		keep    int
		want    []string
	}{
		{
			name: "fewer backups than keep: nothing pruned",
			backups: []backupInfo{
				{Name: "b1", ModTime: mk(0)},
				{Name: "b2", ModTime: mk(1)},
			},
			keep: 7,
			want: nil,
		},
		{
			name: "exactly keep: nothing pruned",
			backups: []backupInfo{
				{Name: "b1", ModTime: mk(0)},
				{Name: "b2", ModTime: mk(1)},
			},
			keep: 2,
			want: nil,
		},
		{
			name: "more than keep: oldest are pruned",
			backups: []backupInfo{
				{Name: "newest", ModTime: mk(0)},
				{Name: "middle", ModTime: mk(1)},
				{Name: "oldest", ModTime: mk(2)},
			},
			keep: 2,
			want: []string{"oldest"},
		},
		{
			name: "unsorted input still prunes the oldest",
			backups: []backupInfo{
				{Name: "oldest", ModTime: mk(5)},
				{Name: "newest", ModTime: mk(0)},
				{Name: "middle", ModTime: mk(2)},
			},
			keep: 1,
			want: []string{"middle", "oldest"},
		},
		{
			name:    "keep <= 0 disables pruning entirely",
			backups: []backupInfo{{Name: "b1", ModTime: mk(0)}, {Name: "b2", ModTime: mk(100)}},
			keep:    0,
			want:    nil,
		},
		{
			name:    "no backups",
			backups: nil,
			keep:    7,
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectBackupsForPruning(tt.backups, tt.keep)
			if !equalStringSlices(got, tt.want) {
				t.Errorf("selectBackupsForPruning() = %v, want %v", got, tt.want)
			}
		})
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRegister_DisabledRemovesAnyExistingJob asserts that Register with
// backups_enabled=false does not schedule a cron job (and removes one that
// may have been left over from a previous, enabled Register call).
func TestRegister_DisabledRemovesAnyExistingJob(t *testing.T) {
	app := newTestApp(t)
	setSetting(t, app, KeyEnabled, false)

	Register(app)

	for _, j := range app.Cron().Jobs() {
		if j.Id() == jobID {
			t.Fatalf("Register() with backups disabled left a %q cron job registered", jobID)
		}
	}
}

// TestRegister_EnabledSchedulesJob asserts that Register with the default
// (enabled) settings schedules exactly the "nexwatch_backup" cron job.
func TestRegister_EnabledSchedulesJob(t *testing.T) {
	app := newTestApp(t)

	Register(app)

	found := false
	for _, j := range app.Cron().Jobs() {
		if j.Id() == jobID {
			found = true
		}
	}
	if !found {
		t.Fatalf("Register() with backups enabled did not schedule a %q cron job", jobID)
	}
}

// TestRegister_InvalidCronFallsBackToDefault asserts that a malformed
// backup_cron setting does not panic (MustAdd) or leave no job scheduled —
// it falls back to DefaultCron instead.
func TestRegister_InvalidCronFallsBackToDefault(t *testing.T) {
	app := newTestApp(t)
	setSetting(t, app, KeyCron, "not a cron expression")

	Register(app)

	found := false
	for _, j := range app.Cron().Jobs() {
		if j.Id() == jobID {
			found = true
		}
	}
	if !found {
		t.Fatal("Register() with an invalid cron expression did not fall back to scheduling with the default")
	}
}
