package logs

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CogniDevAI/nexwatch/internal/agent/config"
)

const (
	// defaultFlushEvery and defaultMaxBatchSize implement "flush every 2s
	// or 200 entries, whichever comes first".
	defaultFlushEvery   = 2 * time.Second
	defaultMaxBatchSize = 200

	// rawQueueSize bounds the buffer between source readers and the
	// batching loop. Collectors (journald/file readers) must never block
	// on a full queue — see emit's non-blocking send below — so this is a
	// backpressure valve, not a correctness requirement.
	rawQueueSize = 2000

	// journaldReconnectDelay is how long Manager waits before restarting a
	// journald reader whose stream ended (e.g. journalctl exited because
	// the journal was rotated out from under it).
	journaldReconnectDelay = 2 * time.Second

	// fileMissingRetryDelay is how long Manager waits before retrying a
	// file source whose tailer returned early (should be rare — FileTailer
	// itself retries a missing file internally — but guards against a
	// tight restart loop on any other early return).
	fileMissingRetryDelay = 2 * time.Second
)

// Manager runs one reader goroutine per configured source, rate-limits
// and batches everything they emit, and hands finished batches to a
// caller-supplied send function.
type Manager struct {
	sources        []config.LogSource
	maxLinesPerSec int

	journaldRunner JournaldRunner
	pollInterval   time.Duration
	flushEvery     time.Duration
	maxBatchSize   int

	mu          sync.Mutex
	windowStart time.Time
	windowCount int
	dropped     atomic.Uint64
}

// NewManager creates a Manager for sources, rate-limited to maxLinesPerSec
// (a value <= 0 disables rate limiting).
func NewManager(sources []config.LogSource, maxLinesPerSec int) *Manager {
	return &Manager{
		sources:        sources,
		maxLinesPerSec: maxLinesPerSec,
		pollInterval:   defaultPollInterval,
		flushEvery:     defaultFlushEvery,
		maxBatchSize:   defaultMaxBatchSize,
	}
}

// SetJournaldRunner overrides the JournaldRunner used for every journald
// source — a seam for tests.
func (m *Manager) SetJournaldRunner(r JournaldRunner) { m.journaldRunner = r }

// SetPollInterval overrides the file-tailer poll interval — a seam for
// tests to avoid waiting out the real 500ms default.
func (m *Manager) SetPollInterval(d time.Duration) { m.pollInterval = d }

// SetFlushInterval overrides the batch flush interval — a seam for tests.
func (m *Manager) SetFlushInterval(d time.Duration) { m.flushEvery = d }

// SetMaxBatchSize overrides the entry-count flush threshold — a seam for
// tests.
func (m *Manager) SetMaxBatchSize(n int) { m.maxBatchSize = n }

// Run starts a reader per configured source and the batching loop,
// calling send for every flushed Batch, until ctx is cancelled. It blocks
// until ctx is done, so call it in its own goroutine.
func (m *Manager) Run(ctx context.Context, send func(Batch)) {
	if len(m.sources) == 0 {
		<-ctx.Done()
		return
	}

	raw := make(chan Entry, rawQueueSize)

	var wg sync.WaitGroup
	for _, src := range m.sources {
		wg.Add(1)
		go func(src config.LogSource) {
			defer wg.Done()
			m.runSource(ctx, src, raw)
		}(src)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	m.batchLoop(ctx, raw, send)
	<-done
}

// runSource dispatches to the right reader for src.Type and keeps
// restarting it (with a short delay) until ctx is done, so a transient
// failure (journalctl exiting, a file briefly disappearing) doesn't
// permanently stop that source.
func (m *Manager) runSource(ctx context.Context, src config.LogSource, raw chan<- Entry) {
	emit := m.emitFunc(raw)

	switch src.Type {
	case "journald":
		reader := NewJournaldReader(m.journaldRunner)
		for {
			if ctx.Err() != nil {
				return
			}
			if err := reader.Run(ctx, src, emit); err != nil {
				log.Printf("[logs/manager] journald source ended: %v", err)
			}
			if ctx.Err() != nil {
				return
			}
			select {
			case <-time.After(journaldReconnectDelay):
			case <-ctx.Done():
				return
			}
		}
	case "file":
		if src.Path == "" {
			log.Printf("[logs/manager] file source has no path configured, skipping")
			return
		}
		tailer := NewFileTailer(src.Path)
		tailer.SetPollInterval(m.pollInterval)
		for {
			if ctx.Err() != nil {
				return
			}
			if err := tailer.Run(ctx, emit); err != nil {
				log.Printf("[logs/manager] file source %s ended: %v", src.Path, err)
			}
			if ctx.Err() != nil {
				return
			}
			select {
			case <-time.After(fileMissingRetryDelay):
			case <-ctx.Done():
				return
			}
		}
	default:
		log.Printf("[logs/manager] unknown log source type %q, skipping", src.Type)
	}
}

// emitFunc returns an emit callback that applies rate limiting and
// backpressure (never blocking the caller), incrementing the shared
// dropped counter for anything it discards.
func (m *Manager) emitFunc(raw chan<- Entry) func(Entry) {
	return func(e Entry) {
		if !m.allow() {
			m.dropped.Add(1)
			return
		}
		select {
		case raw <- e:
		default:
			m.dropped.Add(1)
		}
	}
}

// allow enforces maxLinesPerSec using a simple fixed one-second window
// shared across all sources. A maxLinesPerSec <= 0 disables the limit.
func (m *Manager) allow() bool {
	if m.maxLinesPerSec <= 0 {
		return true
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	if now.Sub(m.windowStart) >= time.Second {
		m.windowStart = now
		m.windowCount = 0
	}
	if m.windowCount >= m.maxLinesPerSec {
		return false
	}
	m.windowCount++
	return true
}

// batchLoop accumulates entries from raw and flushes them (via send) every
// flushEvery, or immediately once maxBatchSize entries have accumulated,
// whichever comes first. On ctx cancellation it flushes once more before
// returning, so nothing buffered is silently lost on shutdown.
func (m *Manager) batchLoop(ctx context.Context, raw <-chan Entry, send func(Batch)) {
	var buf []Entry
	ticker := time.NewTicker(m.flushEvery)
	defer ticker.Stop()

	flush := func() {
		dropped := m.dropped.Swap(0)
		if len(buf) == 0 && dropped == 0 {
			return
		}
		send(Batch{Entries: buf, Dropped: dropped})
		buf = nil
	}

	for {
		select {
		case e := <-raw:
			buf = append(buf, e)
			if len(buf) >= m.maxBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			// Drain whatever is already queued without blocking further.
			for {
				select {
				case e := <-raw:
					buf = append(buf, e)
				default:
					flush()
					return
				}
			}
		}
	}
}
