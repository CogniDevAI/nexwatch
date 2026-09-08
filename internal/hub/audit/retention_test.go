package audit

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func mustSaveAuditEntry(t testing.TB, app core.App, action string, created time.Time) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("audit_log")
	if err != nil {
		t.Fatalf("find audit_log collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("action", action)
	rec.Set("result", "success")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save audit_log entry: %v", err)
	}

	// "created" is an AutodateField: record.Set() is a no-op for it by
	// design (AutodateField.FindSetter returns a noopSetter), so backdating
	// it for this test requires SetRaw — the same escape hatch the field's
	// own Intercept logic checks for ("ignore if a date different from the
	// old one was manually set with SetRaw").
	dt, err := types.ParseDateTime(created.UTC())
	if err != nil {
		t.Fatalf("parse backdated time: %v", err)
	}
	rec.SetRaw("created", dt)
	if err := app.Save(rec); err != nil {
		t.Fatalf("backdate audit_log entry: %v", err)
	}
	return rec
}

func TestPurgeOld_DeletesOnlyEntriesOlderThanRetention(t *testing.T) {
	app := mustNewTestApp(t)

	old := mustSaveAuditEntry(t, app, "old.entry", time.Now().Add(-200*24*time.Hour))
	recent := mustSaveAuditEntry(t, app, "recent.entry", time.Now().Add(-1*time.Hour))

	PurgeOld(app) // default retention: 180 days

	if _, err := app.FindRecordById("audit_log", old.Id); err == nil {
		t.Error("PurgeOld() did not delete an entry older than the default retention period")
	}
	if _, err := app.FindRecordById("audit_log", recent.Id); err != nil {
		t.Errorf("PurgeOld() deleted a recent entry: %v", err)
	}
}

func TestResolveRetentionDays_DefaultsWhenSettingMissing(t *testing.T) {
	app := mustNewTestApp(t)

	if got := resolveRetentionDays(app); got != defaultRetentionDays {
		t.Errorf("resolveRetentionDays() = %d, want default %d", got, defaultRetentionDays)
	}
}

func TestResolveRetentionDays_ReadsSettingValue(t *testing.T) {
	app := mustNewTestApp(t)

	col, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		t.Fatalf("find settings collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("key", SettingRetentionDays)
	rec.Set("value", "30")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save settings entry: %v", err)
	}

	if got := resolveRetentionDays(app); got != 30 {
		t.Errorf("resolveRetentionDays() = %d, want 30", got)
	}
}
