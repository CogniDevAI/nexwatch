package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingWriter_WritesUnderLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	w, err := newRotatingWriter(path, 1024)
	if err != nil {
		t.Fatalf("newRotatingWriter() error = %v", err)
	}
	defer func() { _ = w.Close() }()

	if _, err := w.Write([]byte("line one\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := w.Write([]byte("line two\n")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if string(content) != "line one\nline two\n" {
		t.Errorf("log content = %q, want both lines in order", content)
	}
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Error("did not expect a .1 backup file before the size limit was reached")
	}
}

func TestRotatingWriter_RotatesPastLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	// A tiny limit so two writes force a rotation between them.
	w, err := newRotatingWriter(path, 10)
	if err != nil {
		t.Fatalf("newRotatingWriter() error = %v", err)
	}
	defer func() { _ = w.Close() }()

	first := "0123456789" // exactly at the limit — fits without rotating.
	if _, err := w.Write([]byte(first)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	second := "next-entry-that-does-not-fit"
	if _, err := w.Write([]byte(second)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("expected a .1 backup after rotation, read error: %v", err)
	}
	if string(backup) != first {
		t.Errorf("backup content = %q, want %q", backup, first)
	}

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read current log file: %v", err)
	}
	if string(current) != second {
		t.Errorf("current log content = %q, want %q", current, second)
	}
}

func TestRotatingWriter_ReopensExistingFileAndTracksItsSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(path, []byte("pre-existing content"), 0o644); err != nil {
		t.Fatalf("seed existing log file: %v", err)
	}

	w, err := newRotatingWriter(path, 1000)
	if err != nil {
		t.Fatalf("newRotatingWriter() error = %v", err)
	}
	defer func() { _ = w.Close() }()

	if _, err := w.Write([]byte(" appended")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.HasPrefix(string(content), "pre-existing content") {
		t.Errorf("log content = %q, want it to preserve the pre-existing content", content)
	}
	if !strings.HasSuffix(string(content), " appended") {
		t.Errorf("log content = %q, want the new write appended, not overwritten", content)
	}
}

func TestRotatingWriter_RotationReplacesOldBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.log")
	if err := os.WriteFile(path+".1", []byte("stale backup"), 0o644); err != nil {
		t.Fatalf("seed stale backup: %v", err)
	}

	w, err := newRotatingWriter(path, 5)
	if err != nil {
		t.Fatalf("newRotatingWriter() error = %v", err)
	}
	defer func() { _ = w.Close() }()

	if _, err := w.Write([]byte("first")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := w.Write([]byte("second-write-forces-rotation")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	backup, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != "first" {
		t.Errorf("backup content = %q, want the rotated-out content, not the stale backup", backup)
	}
}
