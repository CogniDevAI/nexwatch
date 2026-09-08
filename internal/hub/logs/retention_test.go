package logs

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func createLogRecord(t *testing.T, app core.App, agentID string, ts time.Time) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("logs")
	if err != nil {
		t.Fatalf("find logs collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("agent_id", agentID)
	rec.Set("ts", ts.UTC().Format(dateFormat))
	rec.Set("source", "journald")
	rec.Set("level", "info")
	rec.Set("message", "test entry")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save log record: %v", err)
	}
	return rec
}

func setIntSetting(t *testing.T, app core.App, key string, value int) {
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

func countLogRecords(t *testing.T, app core.App) int {
	t.Helper()
	n, err := app.CountRecords("logs")
	if err != nil {
		t.Fatalf("count logs: %v", err)
	}
	return int(n)
}

func TestPurgeOld_DeletesOnlyEntriesOlderThanRetention(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "purge-test")

	setIntSetting(t, app, SettingRetentionDays, 3)

	now := time.Now().UTC()
	createLogRecord(t, app, agent.Id, now.Add(-10*24*time.Hour)) // old, should be purged
	createLogRecord(t, app, agent.Id, now.Add(-1*time.Hour))     // recent, should survive

	PurgeOld(app)

	remaining := fetchLogsForAgent(t, app, agent.Id)
	if len(remaining) != 1 {
		t.Fatalf("got %d remaining logs, want 1", len(remaining))
	}
	if !remaining[0].GetDateTime("ts").Time().After(now.Add(-24 * time.Hour)) {
		t.Errorf("the surviving record should be the recent one")
	}
}

func TestPurgeOld_DefaultRetentionAppliesWithoutSetting(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "purge-default")

	now := time.Now().UTC()
	createLogRecord(t, app, agent.Id, now.Add(-10*24*time.Hour)) // older than default 3 days

	PurgeOld(app)

	if got := len(fetchLogsForAgent(t, app, agent.Id)); got != 0 {
		t.Fatalf("got %d remaining logs, want 0 (default retention should have purged it)", got)
	}
}

func TestEnforceMaxRows_TrimsOldestWhenOverCap(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "cap-test")

	setIntSetting(t, app, SettingMaxRows, 3)

	base := time.Now().UTC().Add(-time.Hour)
	ids := make([]string, 5)
	for i := 0; i < 5; i++ {
		ids[i] = createLogRecord(t, app, agent.Id, base.Add(time.Duration(i)*time.Minute)).Id
	}
	if got := countLogRecords(t, app); got != 5 {
		t.Fatalf("setup: got %d logs, want 5", got)
	}

	EnforceMaxRows(app)

	if got := countLogRecords(t, app); got != 3 {
		t.Fatalf("got %d logs after EnforceMaxRows, want 3", got)
	}

	// The two oldest (i=0, i=1) should have been the ones removed; the
	// three newest (i=2,3,4) should survive. Asserted by id rather than by
	// re-comparing timestamps, since a DateField only stores millisecond
	// precision and these records were created microseconds apart.
	for i, id := range ids {
		_, err := app.FindRecordById("logs", id)
		wantDeleted := i < 2
		if wantDeleted && err == nil {
			t.Errorf("record %d (oldest) should have been deleted by EnforceMaxRows, but still exists", i)
		}
		if !wantDeleted && err != nil {
			t.Errorf("record %d (newest) should have survived EnforceMaxRows, but was deleted: %v", i, err)
		}
	}
}

func TestEnforceMaxRows_NoOpUnderCap(t *testing.T) {
	app := newTestApp(t)
	agent := createAgent(t, app, "under-cap")

	setIntSetting(t, app, SettingMaxRows, 100)
	createLogRecord(t, app, agent.Id, time.Now())

	EnforceMaxRows(app)

	if got := countLogRecords(t, app); got != 1 {
		t.Fatalf("got %d logs, want 1 (no-op under cap)", got)
	}
}
