package logs

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/CogniDevAI/nexwatch/internal/agent/config"
)

// batchCollector is a thread-safe sink for batches sent by a Manager.
type batchCollector struct {
	mu      sync.Mutex
	batches []Batch
}

func (b *batchCollector) send(batch Batch) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.batches = append(b.batches, batch)
}

func (b *batchCollector) totalEntries() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := 0
	for _, batch := range b.batches {
		n += len(batch.Entries)
	}
	return n
}

func (b *batchCollector) totalDropped() uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	var total uint64
	for _, batch := range b.batches {
		total += batch.Dropped
	}
	return total
}

func (b *batchCollector) snapshot() []Batch {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Batch, len(b.batches))
	copy(out, b.batches)
	return out
}

func waitForEntries(t *testing.T, c *batchCollector, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if c.totalEntries() >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d total entries, got %d", n, c.totalEntries())
}

func TestManager_FlushesOnSizeThreshold(t *testing.T) {
	fixture := ""
	for i := 0; i < 250; i++ {
		fixture += `{"MESSAGE":"line","PRIORITY":"6","__REALTIME_TIMESTAMP":"1700000000000000"}` + "\n"
	}
	runner := &fakeJournaldRunner{output: fixture}

	mgr := NewManager([]config.LogSource{{Type: "journald"}}, 0)
	mgr.SetJournaldRunner(runner)
	mgr.SetMaxBatchSize(200)
	mgr.SetFlushInterval(time.Hour) // effectively disable the timer path

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &batchCollector{}
	go mgr.Run(ctx, c.send)

	waitForEntries(t, c, 200, 2*time.Second)

	batches := c.snapshot()
	if len(batches[0].Entries) != 200 {
		t.Errorf("first batch size = %d, want 200 (size-triggered flush)", len(batches[0].Entries))
	}
}

func TestManager_FlushesOnTimer(t *testing.T) {
	runner := &fakeJournaldRunner{output: `{"MESSAGE":"one line","PRIORITY":"6","__REALTIME_TIMESTAMP":"1700000000000000"}` + "\n"}

	mgr := NewManager([]config.LogSource{{Type: "journald"}}, 0)
	mgr.SetJournaldRunner(runner)
	mgr.SetFlushInterval(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &batchCollector{}
	go mgr.Run(ctx, c.send)

	waitForEntries(t, c, 1, 2*time.Second)
	if c.snapshot()[0].Entries[0].Message != "one line" {
		t.Errorf("flushed entry = %q, want %q", c.snapshot()[0].Entries[0].Message, "one line")
	}
}

func TestManager_RateLimitDropsExcessAndReportsCount(t *testing.T) {
	fixture := ""
	for i := 0; i < 50; i++ {
		fixture += `{"MESSAGE":"line","PRIORITY":"6","__REALTIME_TIMESTAMP":"1700000000000000"}` + "\n"
	}
	runner := &fakeJournaldRunner{output: fixture}

	mgr := NewManager([]config.LogSource{{Type: "journald"}}, 10) // cap at 10/sec
	mgr.SetJournaldRunner(runner)
	mgr.SetFlushInterval(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &batchCollector{}
	go mgr.Run(ctx, c.send)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.totalEntries()+int(c.totalDropped()) >= 50 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	time.Sleep(50 * time.Millisecond) // let the final flush land

	entries := c.totalEntries()
	dropped := c.totalDropped()
	if entries > 10 {
		t.Errorf("accepted %d entries within the first second, want <= 10 (rate limit)", entries)
	}
	if dropped == 0 {
		t.Error("expected some entries to be reported as dropped, got 0")
	}
	if entries+int(dropped) != 50 {
		t.Errorf("entries(%d) + dropped(%d) = %d, want 50 (no lines silently vanish)", entries, dropped, entries+int(dropped))
	}
}

func TestManager_FileSourceEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	mgr := NewManager([]config.LogSource{{Type: "file", Path: path}}, 0)
	mgr.SetPollInterval(20 * time.Millisecond)
	mgr.SetFlushInterval(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &batchCollector{}
	go mgr.Run(ctx, c.send)

	time.Sleep(60 * time.Millisecond)
	if err := appendLine(path, "ERROR something broke"); err != nil {
		t.Fatalf("append: %v", err)
	}

	waitForEntries(t, c, 1, 2*time.Second)

	found := false
	for _, batch := range c.snapshot() {
		for _, e := range batch.Entries {
			if e.Message == "ERROR something broke" && e.Level == "error" {
				found = true
			}
		}
	}
	if !found {
		t.Error("did not find the expected file-sourced entry in any flushed batch")
	}
}

func TestManager_NoSourcesBlocksUntilContextDone(t *testing.T) {
	mgr := NewManager(nil, 0)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		mgr.Run(ctx, func(Batch) {})
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("Run() returned before context was cancelled")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}

func TestManager_UnknownSourceTypeDoesNotPanic(t *testing.T) {
	mgr := NewManager([]config.LogSource{{Type: "bogus"}}, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Should simply return once ctx expires, without panicking.
	mgr.Run(ctx, func(Batch) {})
}
