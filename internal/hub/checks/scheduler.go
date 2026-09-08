package checks

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Snapshot is a check's latest known result, kept in memory so the summary
// API can answer without a database round trip. It always mirrors the most
// recently persisted "check_results" row for that check.
type Snapshot struct {
	Status        string // "up" or "down" — the debounced state, see nextDebouncedState
	LatencyMs     float64
	StatusCode    int
	Error         string
	TLSExpiresAt  time.Time
	LastCheckedAt time.Time
}

// Scheduler runs one goroutine per enabled "checks" record on its own
// jittered interval, persists every probe attempt as a "check_results" row,
// and keeps the latest debounced result per check in memory (Snapshot).
// Create NewScheduler once per hub process and call Start.
type Scheduler struct {
	app        core.App
	pingRunner PingRunner
	clock      func() time.Time

	mu               sync.Mutex
	cancels          map[string]context.CancelFunc // checkID -> stop its runLoop
	consecutiveFails map[string]int
	latest           map[string]Snapshot

	wg              sync.WaitGroup
	retentionCtx    context.Context
	retentionCancel context.CancelFunc
}

// NewScheduler creates a Scheduler bound to app. Call Start to begin
// running enabled checks.
func NewScheduler(app core.App) *Scheduler {
	return &Scheduler{
		app:              app,
		pingRunner:       execPingRunner{},
		clock:            time.Now,
		cancels:          make(map[string]context.CancelFunc),
		consecutiveFails: make(map[string]int),
		latest:           make(map[string]Snapshot),
	}
}

// SetPingRunner overrides the PingRunner used for ICMP checks — a seam for
// tests to avoid shelling out to the real "ping" binary.
func (s *Scheduler) SetPingRunner(r PingRunner) {
	s.pingRunner = r
}

// Start launches a runner goroutine for every currently-enabled check,
// begins the hourly check_results retention sweep, and binds hooks on the
// "checks" collection so a create/update/delete reloads the affected
// check's runner without requiring a hub restart.
func (s *Scheduler) Start() {
	s.reloadAll()

	s.app.OnRecordAfterCreateSuccess("checks").BindFunc(func(e *core.RecordEvent) error {
		s.reloadOne(e.Record.Id)
		return e.Next()
	})
	s.app.OnRecordAfterUpdateSuccess("checks").BindFunc(func(e *core.RecordEvent) error {
		s.reloadOne(e.Record.Id)
		return e.Next()
	})
	s.app.OnRecordAfterDeleteSuccess("checks").BindFunc(func(e *core.RecordEvent) error {
		s.stopRunner(e.Record.Id)
		return e.Next()
	})

	s.retentionCtx, s.retentionCancel = context.WithCancel(context.Background())
	go s.runRetentionLoop(s.retentionCtx)

	slog.Info("checks scheduler started")
}

// Stop cancels every running check goroutine and the retention loop. It
// does not wait for in-flight probes to finish.
func (s *Scheduler) Stop() {
	if s.retentionCancel != nil {
		s.retentionCancel()
	}

	s.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(s.cancels))
	for id, cancel := range s.cancels {
		cancels = append(cancels, cancel)
		delete(s.cancels, id)
	}
	s.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

// Snapshot returns a copy of every check's latest known result.
func (s *Scheduler) Snapshot() map[string]Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[string]Snapshot, len(s.latest))
	for k, v := range s.latest {
		out[k] = v
	}
	return out
}

// reloadAll starts a runner for every currently-enabled check. Called once
// at startup.
func (s *Scheduler) reloadAll() {
	checks, err := s.app.FindRecordsByFilter("checks", "enabled = true", "", 1000, 0)
	if err != nil {
		slog.Error("failed to load checks", "error", err)
		return
	}
	for _, c := range checks {
		s.startRunner(c.Id)
	}
}

// reloadOne restarts checkID's runner from scratch: always stops any
// existing runner, then starts a fresh one only if the check still exists
// and is enabled. This is what makes an interval/timeout/target edit take
// effect immediately instead of waiting for the next scheduled tick.
func (s *Scheduler) reloadOne(checkID string) {
	s.stopRunner(checkID)

	check, err := s.app.FindRecordById("checks", checkID)
	if err != nil {
		return // deleted, or otherwise unreadable
	}
	if check.GetBool("enabled") {
		s.startRunner(checkID)
	}
}

func (s *Scheduler) startRunner(checkID string) {
	s.mu.Lock()
	if _, exists := s.cancels[checkID]; exists {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancels[checkID] = cancel
	s.mu.Unlock()

	s.wg.Add(1)
	go s.runLoop(ctx, checkID)
}

func (s *Scheduler) stopRunner(checkID string) {
	s.mu.Lock()
	cancel, exists := s.cancels[checkID]
	if exists {
		delete(s.cancels, checkID)
	}
	s.mu.Unlock()

	if exists {
		cancel()
	}
}

// runLoop re-reads the check's current config before every tick (so a
// reload racing with an in-flight wait still picks up the latest interval
// on the following cycle) and waits interval±10% jitter between runs.
func (s *Scheduler) runLoop(ctx context.Context, checkID string) {
	defer s.wg.Done()

	for {
		check, err := s.app.FindRecordById("checks", checkID)
		if err != nil {
			return // deleted
		}

		wait := jitter(time.Duration(intervalSeconds(check)) * time.Second)
		timer := time.NewTimer(wait)

		select {
		case <-timer.C:
			_, _ = s.RunOnce(checkID)
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

// jitter returns d adjusted by a random amount within ±10%.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	delta := float64(d) * 0.10
	if delta <= 0 {
		return d
	}
	offset := time.Duration(rand.Float64()*2*delta - delta) //nolint:gosec // scheduling jitter, not security-sensitive
	return d + offset
}

// RunOnce executes a single probe attempt for checkID right now, persists
// the resulting "check_results" row, updates the in-memory Snapshot and
// debounce counters, and returns the saved record. It is used both by the
// scheduled runLoop and by the "run now" API endpoint, so a manual run
// counts toward the same consecutive-failure streak a scheduled run would.
func (s *Scheduler) RunOnce(checkID string) (*core.Record, error) {
	check, err := s.app.FindRecordById("checks", checkID)
	if err != nil {
		return nil, fmt.Errorf("check not found: %w", err)
	}

	result := s.execute(check)
	status := s.debounce(checkID, result.Success, failuresBeforeDown(check))

	collection, err := s.app.FindCollectionByNameOrId("check_results")
	if err != nil {
		return nil, fmt.Errorf("check_results collection not found: %w", err)
	}

	now := s.clock().UTC()
	record := core.NewRecord(collection)
	record.Set("check_id", checkID)
	record.Set("status", status)
	record.Set("latency_ms", result.LatencyMs)
	if result.StatusCode != 0 {
		record.Set("status_code", result.StatusCode)
	}
	record.Set("error", result.Error)
	if !result.TLSExpiresAt.IsZero() {
		record.Set("tls_expires_at", result.TLSExpiresAt.UTC().Format("2006-01-02 15:04:05.000Z"))
	}
	record.Set("checked_at", now.Format("2006-01-02 15:04:05.000Z"))

	if err := s.app.Save(record); err != nil {
		return nil, fmt.Errorf("failed to save check result: %w", err)
	}

	s.mu.Lock()
	s.latest[checkID] = Snapshot{
		Status:        status,
		LatencyMs:     result.LatencyMs,
		StatusCode:    result.StatusCode,
		Error:         result.Error,
		TLSExpiresAt:  result.TLSExpiresAt,
		LastCheckedAt: now,
	}
	s.mu.Unlock()

	slog.Info("check executed", "check_id", checkID, "type", check.GetString("type"), "status", status, "latency_ms", result.LatencyMs)

	return record, nil
}

// execute dispatches to the prober for check's type.
func (s *Scheduler) execute(check *core.Record) probeResult {
	switch check.GetString("type") {
	case "http":
		return execHTTP(check)
	case "tcp":
		return execTCP(check)
	case "icmp":
		return s.execICMP(check)
	default:
		return probeResult{Success: false, Error: fmt.Sprintf("unknown check type %q", check.GetString("type"))}
	}
}

// debounce applies nextDebouncedState for checkID and records the updated
// consecutive-failure count.
func (s *Scheduler) debounce(checkID string, success bool, failuresBeforeDown int) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	prevStatus := s.latest[checkID].Status
	status, fails := nextDebouncedState(prevStatus, s.consecutiveFails[checkID], success, failuresBeforeDown)
	s.consecutiveFails[checkID] = fails
	return status
}
