package main

import (
	"os"
	"sync"
)

// rotatingWriter is an io.Writer over a single log file that renames the
// current file to "<path>.1" (overwriting any previous one) and starts a
// fresh file once writing would take it past maxBytes. It backs
// service_windows.go's %ProgramData%\NexWatch\agent.log — a Windows
// service has no console to write to and stdout/stderr are simply
// discarded by the Service Control Manager, so without this the agent's
// entire log output would be lost, or (using a plain unbounded append)
// grow forever. Kept dependency-free and platform-independent (no build
// tag) so it can be exercised by ordinary unit tests on any OS, even
// though only the Windows service path actually wires it up.
type rotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxBytes int64
	f        *os.File
	size     int64
}

// newRotatingWriter opens (creating if necessary) path for appending and
// returns a writer that rotates once the file would exceed maxBytes.
func newRotatingWriter(path string, maxBytes int64) (*rotatingWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644) //nolint:gosec // an agent log file is not sensitive, and this matches deploy/nexwatch-agent.service's journald-equivalent readability.
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &rotatingWriter{path: path, maxBytes: maxBytes, f: f, size: info.Size()}, nil
}

// Write implements io.Writer, rotating first if p would push the current
// file past maxBytes. A rotation failure (e.g. the backup path is
// momentarily locked by a log-reading tool) is not fatal to logging
// itself — it falls back to appending to the current, temporarily
// oversized file rather than losing log output entirely.
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.maxBytes > 0 && w.size+int64(len(p)) > w.maxBytes {
		_ = w.rotateLocked() // best-effort; fall through to writing either way.
	}

	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotateLocked closes the current file, renames it to "<path>.1"
// (replacing any previous backup), and reopens path fresh. Called with
// w.mu already held.
func (w *rotatingWriter) rotateLocked() error {
	if err := w.f.Close(); err != nil {
		return err
	}

	backupPath := w.path + ".1"
	_ = os.Remove(backupPath)
	renameErr := os.Rename(w.path, backupPath)

	// Reopen path regardless of whether the rename above succeeded, so a
	// rotation failure never leaves the writer without an open file.
	flag := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if renameErr == nil {
		flag = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(w.path, flag, 0o644) //nolint:gosec // see newRotatingWriter's identical comment.
	if err != nil {
		return err
	}
	w.f = f
	if renameErr == nil {
		w.size = 0
	} else if info, statErr := f.Stat(); statErr == nil {
		w.size = info.Size()
	}
	return renameErr
}

// Close closes the underlying file.
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.f.Close()
}
