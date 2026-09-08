// Package logs collects log lines on the agent host from journald and
// plain files, batches them, and hands them off to the caller (cmd/agent)
// to ship to the hub over the existing WebSocket transport (see the new
// protocol.MessageTypeLogs / protocol.LogsPayload).
package logs

// Entry is one parsed log line, independent of which source produced it.
type Entry struct {
	// Ts is the entry's timestamp in unix milliseconds. Journald sources
	// use the journal's own __REALTIME_TIMESTAMP; file sources use the
	// time the line was read (files carry no reliable structured
	// timestamp of their own).
	Ts int64
	// Source identifies where the entry came from: "journald", or
	// "file:<path>" for a tailed file.
	Source string
	// Unit is the systemd unit name (journald only); empty for files.
	Unit string
	// Level is one of "error", "warning", "info", "debug".
	Level string
	// Message is the raw log line text.
	Message string
	// Fields carries a small set of extra structured attributes. Kept
	// deliberately small since it rides in every shipped entry.
	Fields map[string]string
}

// Batch is one flush of accumulated entries, plus how many additional
// lines were dropped since the previous flush (rate-limited or discarded
// because an internal queue was full — never a parse failure).
type Batch struct {
	Entries []Entry
	Dropped uint64
}
