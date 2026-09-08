// Package uptime provides small, dependency-free interval-overlap math
// shared by two features that both derive an agent's uptime from its alert
// history rather than a direct connectivity log: the public status page
// (internal/hub/api's public_status.go) and the weekly email report
// (internal/hub/report). Extracted here — rather than one importing the
// other — since /api/custom's report routes need to call into
// internal/hub/report, which would make an api->report->api import cycle
// if this lived in either of those packages instead.
package uptime

import (
	"sort"
	"time"
)

// Interval is a half-open [Start, End) span during which something (e.g.
// an alert) was active.
type Interval struct {
	Start, End time.Time
}

// Merge sorts and merges overlapping/touching intervals so two
// simultaneously-active intervals (e.g. two alert rules firing at once for
// the same agent) don't double-count the same span when summed. The input
// slice is not mutated; Merge returns a new sorted, non-overlapping slice.
func Merge(intervals []Interval) []Interval {
	if len(intervals) == 0 {
		return nil
	}

	sorted := make([]Interval, len(intervals))
	copy(sorted, intervals)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })

	merged := []Interval{sorted[0]}
	for _, cur := range sorted[1:] {
		last := &merged[len(merged)-1]
		if !cur.Start.After(last.End) {
			if cur.End.After(last.End) {
				last.End = cur.End
			}
			continue
		}
		merged = append(merged, cur)
	}
	return merged
}

// UptimePercent returns the percentage of [windowStart, windowEnd] NOT
// covered by any of the given (ideally already-Merge'd) intervals, clamped
// to [0, 100]. A zero-or-negative window reports 100 — there is no elapsed
// time in which downtime could have occurred.
func UptimePercent(merged []Interval, windowStart, windowEnd time.Time) float64 {
	span := windowEnd.Sub(windowStart).Seconds()
	if span <= 0 {
		return 100
	}

	var downtime float64
	for _, iv := range merged {
		start := iv.Start
		if start.Before(windowStart) {
			start = windowStart
		}
		end := iv.End
		if end.After(windowEnd) {
			end = windowEnd
		}
		if end.After(start) {
			downtime += end.Sub(start).Seconds()
		}
	}

	pct := 100 * (span - downtime) / span
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}
