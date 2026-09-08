package logs

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// collector is a small thread-safe sink for entries emitted by a reader
// under test.
type collector struct {
	mu      sync.Mutex
	entries []Entry
}

func (c *collector) emit(e Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, e)
}

func (c *collector) snapshot() []Entry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Entry, len(c.entries))
	copy(out, c.entries)
	return out
}

// waitForCount polls until c has at least n entries or the timeout elapses.
func waitForCount(t *testing.T, c *collector, n int, timeout time.Duration) []Entry {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if entries := c.snapshot(); len(entries) >= n {
			return entries
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d entries, got %d", n, len(c.snapshot()))
	return nil
}

func TestFileTailer_StartsAtEndAndReadsNewLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	if err := os.WriteFile(path, []byte("pre-existing line, should not be read\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tailer := NewFileTailer(path)
	tailer.SetPollInterval(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &collector{}
	go func() { _ = tailer.Run(ctx, c.emit) }()

	// Give the tailer time to open and seek to end before we append.
	time.Sleep(50 * time.Millisecond)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := f.WriteString("first new line\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := f.WriteString("ERROR second new line\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = f.Close()

	entries := waitForCount(t, c, 2, 2*time.Second)
	if entries[0].Message != "first new line" {
		t.Errorf("entries[0].Message = %q, want %q", entries[0].Message, "first new line")
	}
	if entries[1].Message != "ERROR second new line" {
		t.Errorf("entries[1].Message = %q, want %q", entries[1].Message, "ERROR second new line")
	}
	if entries[1].Level != "error" {
		t.Errorf("entries[1].Level = %q, want %q", entries[1].Level, "error")
	}
	for _, e := range entries {
		if e.Source != "file:"+path {
			t.Errorf("entry Source = %q, want %q", e.Source, "file:"+path)
		}
	}
}

func TestFileTailer_HandlesTruncation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tailer := NewFileTailer(path)
	tailer.SetPollInterval(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &collector{}
	go func() { _ = tailer.Run(ctx, c.emit) }()
	time.Sleep(50 * time.Millisecond)

	if err := os.WriteFile(path, []byte("line before truncate\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	waitForCount(t, c, 1, 2*time.Second)

	// Truncate (same inode, shorter) and write new content.
	if err := os.Truncate(path, 0); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("reopen after truncate: %v", err)
	}
	if _, err := f.WriteString("line after truncate\n"); err != nil {
		t.Fatalf("write after truncate: %v", err)
	}
	_ = f.Close()

	entries := waitForCount(t, c, 2, 2*time.Second)
	last := entries[len(entries)-1]
	if last.Message != "line after truncate" {
		t.Errorf("last entry Message = %q, want %q", last.Message, "line after truncate")
	}
}

func TestFileTailer_HandlesRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	tailer := NewFileTailer(path)
	tailer.SetPollInterval(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &collector{}
	go func() { _ = tailer.Run(ctx, c.emit) }()
	time.Sleep(50 * time.Millisecond)

	if err := appendLine(path, "before rotation"); err != nil {
		t.Fatalf("append: %v", err)
	}
	waitForCount(t, c, 1, 2*time.Second)

	// Simulate logrotate: rename the old file away, create a fresh one at
	// the same path (a different inode).
	rotated := filepath.Join(dir, "app.log.1")
	if err := os.Rename(path, rotated); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("recreate: %v", err)
	}
	if err := appendLine(path, "after rotation"); err != nil {
		t.Fatalf("append after rotation: %v", err)
	}

	entries := waitForCount(t, c, 2, 2*time.Second)
	last := entries[len(entries)-1]
	if last.Message != "after rotation" {
		t.Errorf("last entry Message = %q, want %q", last.Message, "after rotation")
	}
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(line + "\n")
	return err
}

func TestFileTailer_RetriesMissingFileUntilCreated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-yet.log")

	tailer := NewFileTailer(path)
	tailer.SetPollInterval(20 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := &collector{}
	go func() { _ = tailer.Run(ctx, c.emit) }()

	time.Sleep(60 * time.Millisecond) // confirm it doesn't panic/exit while the file is missing

	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("create file: %v", err)
	}
	// Give the tailer a poll tick to discover and open the new file (at
	// its current, empty end) before writing to it — otherwise the write
	// could land before the tailer opens the file and, per "starts at end
	// of file", would be skipped.
	time.Sleep(60 * time.Millisecond)
	if err := appendLine(path, "now it exists"); err != nil {
		t.Fatalf("append: %v", err)
	}

	entries := waitForCount(t, c, 1, 2*time.Second)
	if entries[0].Message != "now it exists" {
		t.Errorf("entries[0].Message = %q, want %q", entries[0].Message, "now it exists")
	}
}
