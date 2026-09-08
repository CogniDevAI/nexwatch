package logs

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"time"
)

// defaultPollInterval is how often a FileTailer checks the file for new
// data, truncation, or rotation.
const defaultPollInterval = 500 * time.Millisecond

// readChunkSize is the buffer size used to drain newly-written bytes on
// each poll tick.
const readChunkSize = 64 * 1024

// FileTailer follows one plain-text file, starting at its current end (it
// never re-reads history on startup), using no third-party dependencies.
// It detects:
//   - truncation: the file's size shrank since the last poll (e.g. a
//     logger that reopens the same path with O_TRUNC).
//   - rotation: the path now refers to a different inode (e.g. logrotate's
//     rename-then-recreate), detected via os.SameFile.
//
// In both cases it reopens the path from the beginning, since a rotated
// or truncated file's remaining content is new to this tailer.
type FileTailer struct {
	path         string
	pollInterval time.Duration
}

// NewFileTailer creates a FileTailer for path with the default 500ms poll
// interval.
func NewFileTailer(path string) *FileTailer {
	return &FileTailer{path: path, pollInterval: defaultPollInterval}
}

// SetPollInterval overrides the poll interval — a seam for tests to avoid
// waiting out the real 500ms default.
func (t *FileTailer) SetPollInterval(d time.Duration) {
	t.pollInterval = d
}

// tailState holds the mutable per-open-file state a FileTailer carries
// across poll ticks: the open handle, its stat snapshot (for rotation/
// truncation detection), and any bytes read since the last complete line
// (a line split across two poll ticks is buffered here rather than
// dropped).
type tailState struct {
	file    *os.File
	info    os.FileInfo
	pending []byte
}

func (s *tailState) close() {
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
}

// Run tails the file, calling emit for every complete line read, until ctx
// is cancelled. It never returns before ctx is done: a missing file (not
// yet created, or removed mid-tail) is retried on every poll tick rather
// than treated as fatal, since log files routinely appear and disappear
// around process restarts and rotation.
func (t *FileTailer) Run(ctx context.Context, emit func(Entry)) error {
	state := &tailState{}
	defer state.close()

	source := "file:" + t.path
	openAtEnd(state, t.path)

	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		if state.file == nil {
			openAtEnd(state, t.path)
			continue
		}

		currentInfo, err := os.Stat(t.path)
		if err != nil {
			// File disappeared (removed, or mid-rotation). Keep the
			// handle open in case it's a transient rename-in-progress;
			// the read below will just see no new data.
			continue
		}

		switch {
		case !os.SameFile(state.info, currentInfo):
			// Rotation: the path now refers to a different inode. Any
			// bytes not yet read from the old handle predate our start
			// point and are intentionally not drained — start clean at
			// the new file's beginning.
			log.Printf("[logs/file] %s rotated, following new file", t.path)
			reopenFromStart(state, t.path)
			continue
		case currentInfo.Size() < state.info.Size():
			// Truncation: same inode, but shorter than before.
			log.Printf("[logs/file] %s truncated, reopening from start", t.path)
			reopenFromStart(state, t.path)
			continue
		}

		state.info = currentInfo
		drainNewLines(state, emit, source)
	}
}

func openAtEnd(state *tailState, path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return
	}
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		_ = f.Close()
		return
	}
	state.close()
	state.file = f
	state.info = fi
	state.pending = nil
}

func reopenFromStart(state *tailState, path string) {
	f, err := os.Open(path)
	if err != nil {
		state.close()
		return
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return
	}
	state.close()
	state.file = f
	state.info = fi
	state.pending = nil
}

// drainNewLines reads every byte currently available on state.file,
// appends it to any pending (incomplete) line left from the previous
// tick, emits every complete newline-terminated line, and keeps a
// trailing partial line buffered in state.pending for the next tick.
func drainNewLines(state *tailState, emit func(Entry), source string) {
	buf := make([]byte, readChunkSize)
	for {
		n, err := state.file.Read(buf)
		if n > 0 {
			state.pending = append(state.pending, buf[:n]...)
		}
		if err != nil {
			break // io.EOF (no more data right now) or a real read error
		}
		if n == 0 {
			break
		}
	}

	for {
		idx := bytes.IndexByte(state.pending, '\n')
		if idx < 0 {
			break
		}
		line := trimCR(state.pending[:idx])
		state.pending = state.pending[idx+1:]
		if len(line) > 0 {
			emit(Entry{
				Ts:      time.Now().UnixMilli(),
				Source:  source,
				Level:   DetectLevel(string(line)),
				Message: string(line),
			})
		}
	}
}

func trimCR(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\r' {
		return b[:n-1]
	}
	return b
}
