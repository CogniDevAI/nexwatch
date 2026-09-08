package uptime

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func TestMerge_OverlappingIntervalsCombine(t *testing.T) {
	a := mustParse(t, "2026-01-01T00:00:00Z")
	b := mustParse(t, "2026-01-01T01:00:00Z")
	c := mustParse(t, "2026-01-01T00:30:00Z")
	d := mustParse(t, "2026-01-01T02:00:00Z")

	merged := Merge([]Interval{{Start: a, End: b}, {Start: c, End: d}})
	if len(merged) != 1 {
		t.Fatalf("Merge() returned %d intervals, want 1: %+v", len(merged), merged)
	}
	if !merged[0].Start.Equal(a) || !merged[0].End.Equal(d) {
		t.Errorf("Merge() = [%v, %v], want [%v, %v]", merged[0].Start, merged[0].End, a, d)
	}
}

func TestMerge_NonOverlappingIntervalsStaySeparate(t *testing.T) {
	a := mustParse(t, "2026-01-01T00:00:00Z")
	b := mustParse(t, "2026-01-01T01:00:00Z")
	c := mustParse(t, "2026-01-01T02:00:00Z")
	d := mustParse(t, "2026-01-01T03:00:00Z")

	merged := Merge([]Interval{{Start: c, End: d}, {Start: a, End: b}})
	if len(merged) != 2 {
		t.Fatalf("Merge() returned %d intervals, want 2: %+v", len(merged), merged)
	}
	if !merged[0].Start.Equal(a) || !merged[1].Start.Equal(c) {
		t.Errorf("Merge() did not sort by start time: %+v", merged)
	}
}

func TestMerge_EmptyInput(t *testing.T) {
	if got := Merge(nil); got != nil {
		t.Errorf("Merge(nil) = %+v, want nil", got)
	}
}

func TestUptimePercent_NoDowntimeIsFullyUp(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	end := mustParse(t, "2026-01-02T00:00:00Z")

	if got := UptimePercent(nil, start, end); got != 100 {
		t.Errorf("UptimePercent(nil, ...) = %v, want 100", got)
	}
}

func TestUptimePercent_HalfTheWindowDown(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	end := mustParse(t, "2026-01-02T00:00:00Z")
	downStart := mustParse(t, "2026-01-01T12:00:00Z")

	merged := []Interval{{Start: downStart, End: end}}
	got := UptimePercent(merged, start, end)
	if got != 50 {
		t.Errorf("UptimePercent() = %v, want 50", got)
	}
}

func TestUptimePercent_IntervalOutsideWindowIsIgnored(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	end := mustParse(t, "2026-01-02T00:00:00Z")
	before := mustParse(t, "2025-12-01T00:00:00Z")
	beforeEnd := mustParse(t, "2025-12-02T00:00:00Z")

	merged := []Interval{{Start: before, End: beforeEnd}}
	if got := UptimePercent(merged, start, end); got != 100 {
		t.Errorf("UptimePercent() = %v, want 100 (interval entirely before window)", got)
	}
}

func TestUptimePercent_FullyDownWindow(t *testing.T) {
	start := mustParse(t, "2026-01-01T00:00:00Z")
	end := mustParse(t, "2026-01-02T00:00:00Z")

	merged := []Interval{{Start: start.Add(-time.Hour), End: end.Add(time.Hour)}}
	if got := UptimePercent(merged, start, end); got != 0 {
		t.Errorf("UptimePercent() = %v, want 0", got)
	}
}

func TestUptimePercent_ZeroWidthWindowReturns100(t *testing.T) {
	ts := mustParse(t, "2026-01-01T00:00:00Z")
	if got := UptimePercent(nil, ts, ts); got != 100 {
		t.Errorf("UptimePercent(zero-width) = %v, want 100", got)
	}
}
