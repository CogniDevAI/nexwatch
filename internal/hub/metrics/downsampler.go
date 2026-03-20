package metrics

import (
	"encoding/json"
	"log"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Downsampler runs periodic aggregation and data retention jobs.
type Downsampler struct {
	app            core.App
	retentionDays  int
	stopCh         chan struct{}
}

// NewDownsampler creates a new downsampler with the given retention policy.
func NewDownsampler(app core.App, retentionDays int) *Downsampler {
	if retentionDays <= 0 {
		retentionDays = 30
	}
	return &Downsampler{
		app:           app,
		retentionDays: retentionDays,
		stopCh:        make(chan struct{}),
	}
}

// Start launches the background downsampling and retention goroutine.
// It runs aggregation every 5 minutes and retention cleanup every hour.
func (d *Downsampler) Start() {
	go d.runAggregationLoop()
	go d.runRetentionLoop()
	log.Printf("[downsampler] started (retention: %d days)", d.retentionDays)
}

// Stop signals the background goroutines to stop.
func (d *Downsampler) Stop() {
	close(d.stopCh)
}

// runAggregationLoop periodically aggregates raw metrics into downsampled resolutions.
func (d *Downsampler) runAggregationLoop() {
	// Initial delay to let the system stabilize.
	timer := time.NewTimer(1 * time.Minute)
	select {
	case <-timer.C:
	case <-d.stopCh:
		timer.Stop()
		return
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.aggregateMetrics()
		case <-d.stopCh:
			return
		}
	}
}

// runRetentionLoop periodically purges data older than the configured retention period.
func (d *Downsampler) runRetentionLoop() {
	// Initial delay.
	timer := time.NewTimer(2 * time.Minute)
	select {
	case <-timer.C:
	case <-d.stopCh:
		timer.Stop()
		return
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			d.purgeOldData()
		case <-d.stopCh:
			return
		}
	}
}

// aggregateMetrics performs multi-resolution downsampling:
// raw → 1m, 1m → 5m, 5m → 1h
func (d *Downsampler) aggregateMetrics() {
	now := time.Now().UTC()

	// Aggregate raw → 1m for data older than 5 minutes.
	d.downsample("raw", "1m", 1*time.Minute, now.Add(-5*time.Minute))

	// Aggregate 1m → 5m for data older than 30 minutes.
	d.downsample("1m", "5m", 5*time.Minute, now.Add(-30*time.Minute))

	// Aggregate 5m → 1h for data older than 6 hours.
	d.downsample("5m", "1h", 1*time.Hour, now.Add(-6*time.Hour))
}

// downsample aggregates metrics from sourceResolution into targetResolution
// using the given bucket duration, for data older than the cutoff time.
func (d *Downsampler) downsample(sourceRes, targetRes string, bucketDuration time.Duration, cutoff time.Time) {
	// Find distinct agent+type combinations with raw data older than cutoff.
	records, err := d.app.FindRecordsByFilter(
		"metrics",
		"resolution = {:res} && timestamp < {:cutoff}",
		"timestamp",
		500, // process in batches
		0,
		map[string]any{
			"res":    sourceRes,
			"cutoff": cutoff.Format("2006-01-02 15:04:05.000Z"),
		},
	)
	if err != nil || len(records) == 0 {
		return
	}

	// Group by agent_id + type + time bucket.
	type bucketKey struct {
		agentID string
		mtype   string
		bucket  time.Time
	}

	buckets := make(map[bucketKey][]*core.Record)
	for _, r := range records {
		ts := r.GetDateTime("timestamp").Time()
		// Truncate to bucket boundary.
		bucket := ts.Truncate(bucketDuration)
		key := bucketKey{
			agentID: r.GetString("agent_id"),
			mtype:   r.GetString("type"),
			bucket:  bucket,
		}
		buckets[key] = append(buckets[key], r)
	}

	collection, err := d.app.FindCollectionByNameOrId("metrics")
	if err != nil {
		log.Printf("[downsampler] metrics collection not found: %v", err)
		return
	}

	for key, recs := range buckets {
		if len(recs) == 0 {
			continue
		}

		// Check if aggregated record already exists for this bucket.
		existing, _ := d.app.FindRecordsByFilter(
			"metrics",
			"agent_id = {:agentId} && type = {:type} && resolution = {:res} && timestamp = {:ts}",
			"",
			1,
			0,
			map[string]any{
				"agentId": key.agentID,
				"type":    key.mtype,
				"res":     targetRes,
				"ts":      key.bucket.Format("2006-01-02 15:04:05.000Z"),
			},
		)
		if len(existing) > 0 {
			// Already aggregated, just delete source records.
			d.deleteRecords(recs)
			continue
		}

		// Aggregate: compute averages of numeric fields in data.
		aggregatedData := d.averageData(recs)
		dataJSON, err := json.Marshal(aggregatedData)
		if err != nil {
			continue
		}

		// Create aggregated record.
		agg := core.NewRecord(collection)
		agg.Set("agent_id", key.agentID)
		agg.Set("type", key.mtype)
		agg.Set("data", string(dataJSON))
		agg.Set("timestamp", key.bucket.Format("2006-01-02 15:04:05.000Z"))
		agg.Set("resolution", targetRes)

		if err := d.app.Save(agg); err != nil {
			log.Printf("[downsampler] save aggregated record error: %v", err)
			continue
		}

		// Delete the source records that were aggregated.
		d.deleteRecords(recs)
	}
}

// averageData computes the average of all numeric values across records' data fields.
func (d *Downsampler) averageData(records []*core.Record) map[string]any {
	sums := make(map[string]float64)
	counts := make(map[string]int)
	stringValues := make(map[string]string)

	for _, r := range records {
		dataStr := r.GetString("data")
		var data map[string]any
		if err := json.Unmarshal([]byte(dataStr), &data); err != nil {
			continue
		}

		for k, v := range data {
			if f, ok := toFloat64(v); ok {
				sums[k] += f
				counts[k]++
			} else if s, ok := v.(string); ok {
				stringValues[k] = s // keep last string value
			}
		}
	}

	result := make(map[string]any)
	for k, sum := range sums {
		if counts[k] > 0 {
			result[k] = sum / float64(counts[k])
		}
	}
	// Preserve string fields (e.g., mount points, interface names).
	for k, v := range stringValues {
		if _, numeric := result[k]; !numeric {
			result[k] = v
		}
	}
	// Add sample count for transparency.
	result["_samples"] = len(records)

	return result
}

// deleteRecords deletes a batch of records.
func (d *Downsampler) deleteRecords(records []*core.Record) {
	for _, r := range records {
		if err := d.app.Delete(r); err != nil {
			log.Printf("[downsampler] delete record %s error: %v", r.Id, err)
		}
	}
}

// purgeOldData deletes metrics and downsampled data older than the retention period.
// Raw data older than 2 days is always purged (should have been downsampled).
// Downsampled data follows the full retention period.
func (d *Downsampler) purgeOldData() {
	now := time.Now().UTC()

	// Purge raw metrics older than 2 days (should be downsampled by then).
	rawCutoff := now.Add(-48 * time.Hour)
	d.purgeByFilter(
		"resolution = {:res} && timestamp < {:cutoff}",
		map[string]any{
			"res":    "raw",
			"cutoff": rawCutoff.Format("2006-01-02 15:04:05.000Z"),
		},
	)

	// Purge 1m metrics older than 7 days.
	m1Cutoff := now.Add(-7 * 24 * time.Hour)
	d.purgeByFilter(
		"resolution = {:res} && timestamp < {:cutoff}",
		map[string]any{
			"res":    "1m",
			"cutoff": m1Cutoff.Format("2006-01-02 15:04:05.000Z"),
		},
	)

	// Purge 5m metrics older than 14 days.
	m5Cutoff := now.Add(-14 * 24 * time.Hour)
	d.purgeByFilter(
		"resolution = {:res} && timestamp < {:cutoff}",
		map[string]any{
			"res":    "5m",
			"cutoff": m5Cutoff.Format("2006-01-02 15:04:05.000Z"),
		},
	)

	// Purge 1h metrics older than the full retention period.
	fullCutoff := now.Add(-time.Duration(d.retentionDays) * 24 * time.Hour)
	d.purgeByFilter(
		"resolution = {:res} && timestamp < {:cutoff}",
		map[string]any{
			"res":    "1h",
			"cutoff": fullCutoff.Format("2006-01-02 15:04:05.000Z"),
		},
	)

	log.Printf("[downsampler] retention cleanup complete")
}

// purgeByFilter deletes records matching the given filter in batches.
func (d *Downsampler) purgeByFilter(filter string, params map[string]any) {
	for {
		records, err := d.app.FindRecordsByFilter(
			"metrics",
			filter,
			"",
			200, // batch size
			0,
			params,
		)
		if err != nil || len(records) == 0 {
			return
		}

		d.deleteRecords(records)

		if len(records) < 200 {
			return
		}
	}
}
