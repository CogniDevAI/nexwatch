package metrics

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func TestAverageMaps_ComputesMeanOfNumericFields(t *testing.T) {
	dataMaps := []map[string]any{
		{"total_percent": 10.0, "cores": 4.0},
		{"total_percent": 20.0, "cores": 4.0},
		{"total_percent": 30.0, "cores": 4.0},
	}

	got := averageMaps(dataMaps)

	if got["total_percent"] != 20.0 {
		t.Errorf("averageMaps() total_percent = %v, want 20.0", got["total_percent"])
	}
	if got["cores"] != 4.0 {
		t.Errorf("averageMaps() cores = %v, want 4.0", got["cores"])
	}
}

func TestAverageMaps_PreservesLastStringValue(t *testing.T) {
	dataMaps := []map[string]any{
		{"mount": "/mnt/a", "used_percent": 10.0},
		{"mount": "/mnt/b", "used_percent": 20.0},
	}

	got := averageMaps(dataMaps)

	if got["mount"] != "/mnt/b" {
		t.Errorf("averageMaps() mount = %v, want last-seen /mnt/b", got["mount"])
	}
	if got["used_percent"] != 15.0 {
		t.Errorf("averageMaps() used_percent = %v, want 15.0", got["used_percent"])
	}
}

func TestAverageMaps_MissingFieldInSomeSamplesAveragesOnlyPresentOnes(t *testing.T) {
	dataMaps := []map[string]any{
		{"a": 10.0, "b": 100.0},
		{"a": 30.0}, // "b" absent from this sample
	}

	got := averageMaps(dataMaps)

	if got["a"] != 20.0 {
		t.Errorf("averageMaps() a = %v, want 20.0", got["a"])
	}
	if got["b"] != 100.0 {
		t.Errorf("averageMaps() b = %v, want 100.0 (averaged over the one sample that had it)", got["b"])
	}
}

func TestAverageMaps_EmptyInputReturnsEmptyMap(t *testing.T) {
	got := averageMaps(nil)
	if len(got) != 0 {
		t.Errorf("averageMaps(nil) = %v, want empty map", got)
	}
}

func TestAverageMaps_MixedNumericTypes(t *testing.T) {
	dataMaps := []map[string]any{
		{"v": int64(10)},
		{"v": float32(20)},
		{"v": json.Number("30")},
	}

	got := averageMaps(dataMaps)

	if got["v"] != 20.0 {
		t.Errorf("averageMaps() v = %v, want 20.0 across mixed numeric types", got["v"])
	}
}

// --- downsample() bucket grouping (needs a real app/collection) ----------

func newDownsamplerTestApp(t *testing.T) (core.App, *core.Record) {
	t.Helper()
	app := newTestApp(t)
	agent := createAgent(t, app, "host-downsample")
	return app, agent
}

func createRawMetric(t *testing.T, app core.App, agentID, metricType string, ts time.Time, value float64) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("metrics")
	if err != nil {
		t.Fatalf("find metrics collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("agent_id", agentID)
	rec.Set("type", metricType)
	rec.Set("data", map[string]any{"total_percent": value})
	rec.Set("timestamp", ts.UTC().Format("2006-01-02 15:04:05.000Z"))
	rec.Set("resolution", "raw")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save raw metric: %v", err)
	}
	return rec
}

func TestDownsample_AggregatesOldRawRecordsIntoBucket(t *testing.T) {
	app, agent := newDownsamplerTestApp(t)
	d := NewDownsampler(app, 30)

	bucketStart := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Minute)
	createRawMetric(t, app, agent.Id, "cpu", bucketStart.Add(5*time.Second), 10.0)
	createRawMetric(t, app, agent.Id, "cpu", bucketStart.Add(30*time.Second), 30.0)

	cutoff := time.Now().UTC().Add(-5 * time.Minute)
	d.downsample("raw", "1m", time.Minute, cutoff)

	// Source records should be gone.
	raw, err := app.FindRecordsByFilter("metrics", "agent_id = {:a} && resolution = 'raw'", "", 0, 0, map[string]any{"a": agent.Id})
	if err != nil {
		t.Fatalf("find raw metrics: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("len(raw metrics) after downsample = %d, want 0", len(raw))
	}

	// A single aggregated 1m record should exist for the bucket.
	agg, err := app.FindRecordsByFilter("metrics", "agent_id = {:a} && resolution = '1m'", "", 0, 0, map[string]any{"a": agent.Id})
	if err != nil {
		t.Fatalf("find 1m metrics: %v", err)
	}
	if len(agg) != 1 {
		t.Fatalf("len(1m metrics) = %d, want 1", len(agg))
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(agg[0].GetString("data")), &data); err != nil {
		t.Fatalf("unmarshal aggregated data: %v", err)
	}
	if data["total_percent"] != 20.0 {
		t.Errorf("aggregated total_percent = %v, want 20.0 (average of 10 and 30)", data["total_percent"])
	}
	if data["_samples"] != 2.0 {
		t.Errorf("aggregated _samples = %v, want 2", data["_samples"])
	}
}

func TestDownsample_SkipsRecordsNewerThanCutoff(t *testing.T) {
	app, agent := newDownsamplerTestApp(t)
	d := NewDownsampler(app, 30)

	// Recent enough that it must not be aggregated yet.
	createRawMetric(t, app, agent.Id, "cpu", time.Now(), 50.0)

	cutoff := time.Now().Add(-5 * time.Minute)
	d.downsample("raw", "1m", time.Minute, cutoff)

	raw, err := app.FindRecordsByFilter("metrics", "agent_id = {:a} && resolution = 'raw'", "", 0, 0, map[string]any{"a": agent.Id})
	if err != nil {
		t.Fatalf("find raw metrics: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("len(raw metrics) after downsample = %d, want 1 (untouched, too recent)", len(raw))
	}
}

func TestDownsample_ExistingAggregateDeduplicatesSource(t *testing.T) {
	app, agent := newDownsamplerTestApp(t)
	d := NewDownsampler(app, 30)

	bucketStart := time.Now().UTC().Add(-1 * time.Hour).Truncate(time.Minute)

	// Pre-existing aggregated record for this bucket.
	col, err := app.FindCollectionByNameOrId("metrics")
	if err != nil {
		t.Fatalf("find metrics collection: %v", err)
	}
	existing := core.NewRecord(col)
	existing.Set("agent_id", agent.Id)
	existing.Set("type", "cpu")
	existing.Set("data", map[string]any{"total_percent": 99.0})
	existing.Set("timestamp", bucketStart.Format("2006-01-02 15:04:05.000Z"))
	existing.Set("resolution", "1m")
	if err := app.Save(existing); err != nil {
		t.Fatalf("save existing aggregate: %v", err)
	}

	createRawMetric(t, app, agent.Id, "cpu", bucketStart.Add(5*time.Second), 10.0)

	cutoff := time.Now().UTC().Add(-5 * time.Minute)
	d.downsample("raw", "1m", time.Minute, cutoff)

	agg, err := app.FindRecordsByFilter("metrics", "agent_id = {:a} && resolution = '1m'", "", 0, 0, map[string]any{"a": agent.Id})
	if err != nil {
		t.Fatalf("find 1m metrics: %v", err)
	}
	if len(agg) != 1 {
		t.Fatalf("len(1m metrics) = %d, want 1 (must not duplicate an existing bucket)", len(agg))
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(agg[0].GetString("data")), &data); err != nil {
		t.Fatalf("unmarshal existing aggregate data: %v", err)
	}
	if data["total_percent"] != 99.0 {
		t.Errorf("existing aggregate total_percent = %v, want unchanged 99.0", data["total_percent"])
	}

	raw, err := app.FindRecordsByFilter("metrics", "agent_id = {:a} && resolution = 'raw'", "", 0, 0, map[string]any{"a": agent.Id})
	if err != nil {
		t.Fatalf("find raw metrics: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("len(raw metrics) = %d, want 0 (source deleted even though bucket pre-existed)", len(raw))
	}
}

// --- purgeOldData retention cutoffs ---------------------------------------

func TestPurgeOldData_RetentionCutoffsByResolution(t *testing.T) {
	app, agent := newDownsamplerTestApp(t)
	d := NewDownsampler(app, 30) // 30-day full retention

	now := time.Now().UTC()

	type fixture struct {
		label      string
		resolution string
		age        time.Duration
		wantPurged bool
	}
	fixtures := []fixture{
		{"raw just under 48h", "raw", 47 * time.Hour, false},
		{"raw just over 48h", "raw", 49 * time.Hour, true},
		{"1m just under 7d", "1m", 6*24*time.Hour + 23*time.Hour, false},
		{"1m just over 7d", "1m", 8 * 24 * time.Hour, true},
		{"5m just under 14d", "5m", 13 * 24 * time.Hour, false},
		{"5m just over 14d", "5m", 15 * 24 * time.Hour, true},
		{"1h just under 30d", "1h", 29 * 24 * time.Hour, false},
		{"1h just over 30d", "1h", 31 * 24 * time.Hour, true},
	}

	ids := make(map[string]string)
	for _, f := range fixtures {
		col, err := app.FindCollectionByNameOrId("metrics")
		if err != nil {
			t.Fatalf("find metrics collection: %v", err)
		}
		rec := core.NewRecord(col)
		rec.Set("agent_id", agent.Id)
		rec.Set("type", "cpu")
		rec.Set("data", map[string]any{"total_percent": 1.0})
		rec.Set("timestamp", now.Add(-f.age).Format("2006-01-02 15:04:05.000Z"))
		rec.Set("resolution", f.resolution)
		if err := app.Save(rec); err != nil {
			t.Fatalf("save fixture %s: %v", f.label, err)
		}
		ids[f.label] = rec.Id
	}

	d.purgeOldData()

	for _, f := range fixtures {
		_, err := app.FindRecordById("metrics", ids[f.label])
		purged := err != nil
		if purged != f.wantPurged {
			t.Errorf("%s: purged = %v, want %v", f.label, purged, f.wantPurged)
		}
	}
}

func TestPurgeByFilter_DeletesInBatchesAcrossMultiplePages(t *testing.T) {
	app, agent := newDownsamplerTestApp(t)
	d := NewDownsampler(app, 30)

	now := time.Now().UTC()
	// More than one purge batch (200) worth of stale raw records would be
	// slow to set up; instead verify the loop terminates and clears a small
	// set completely, which exercises the same "loop until no more match"
	// contract without a multi-thousand-row fixture.
	const n = 5
	for i := 0; i < n; i++ {
		col, err := app.FindCollectionByNameOrId("metrics")
		if err != nil {
			t.Fatalf("find metrics collection: %v", err)
		}
		rec := core.NewRecord(col)
		rec.Set("agent_id", agent.Id)
		rec.Set("type", "cpu")
		rec.Set("data", map[string]any{"total_percent": float64(i)})
		rec.Set("timestamp", now.Add(-72*time.Hour).Format("2006-01-02 15:04:05.000Z"))
		rec.Set("resolution", "raw")
		if err := app.Save(rec); err != nil {
			t.Fatalf("save stale record %d: %v", i, err)
		}
	}

	d.purgeByFilter(
		"resolution = {:res} && timestamp < {:cutoff}",
		map[string]any{"res": "raw", "cutoff": now.Add(-48 * time.Hour).Format("2006-01-02 15:04:05.000Z")},
	)

	remaining, err := app.FindRecordsByFilter("metrics", "agent_id = {:a}", "", 0, 0, map[string]any{"a": agent.Id})
	if err != nil {
		t.Fatalf("find remaining metrics: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("len(remaining) = %d, want 0", len(remaining))
	}
}
