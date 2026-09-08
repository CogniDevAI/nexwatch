// Package logs ingests agent-shipped log batches (protocol.LogsPayload)
// into the "logs" collection and enforces its retention policy.
package logs

import (
	"encoding/json"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"

	"github.com/CogniDevAI/nexwatch/internal/shared/protocol"
)

// dateFormat matches the format every other collection in this hub stores
// PocketBase DateField values as (see internal/hub/metrics/service.go and
// friends).
const dateFormat = "2006-01-02 15:04:05.000Z"

// maxMessageBytes caps a stored log message to the "logs.message" field's
// schema limit (internal/hub/migrations/logs.go), truncating on a valid
// UTF-8 boundary rather than mid-rune.
const maxMessageBytes = 8192

// defaultLevel is used when an entry arrives with no (or an unrecognized)
// level.
const defaultLevel = "info"

var validLevels = map[string]bool{"error": true, "warning": true, "info": true, "debug": true}

// Service ingests log batches into the "logs" collection.
type Service struct {
	app core.App
}

// NewService creates a new log ingestion service.
func NewService(app core.App) *Service {
	return &Service{app: app}
}

// IngestLogs saves every entry in payload as a "logs" record inside a
// single transaction, then best-effort bumps the agent's cumulative
// logs_lines_total gauge (outside the transaction — it's a nice-to-have,
// not part of the ingest's correctness).
func (s *Service) IngestLogs(app core.App, agentID string, payload *protocol.LogsPayload) {
	if payload == nil || len(payload.Entries) == 0 {
		return
	}

	col, err := app.FindCollectionByNameOrId("logs")
	if err != nil {
		slog.Error("logs collection not found", "error", err)
		return
	}

	saved := 0
	err = app.RunInTransaction(func(txApp core.App) error {
		for _, entry := range payload.Entries {
			record := core.NewRecord(col)
			record.Set("agent_id", agentID)
			record.Set("ts", entryTime(entry.Ts).Format(dateFormat))
			record.Set("source", entry.Source)
			record.Set("unit", entry.Unit)
			record.Set("level", normalizeLevel(entry.Level))
			record.Set("message", truncateUTF8(entry.Message, maxMessageBytes))
			if len(entry.Fields) > 0 {
				fieldsJSON, err := json.Marshal(entry.Fields)
				if err == nil {
					record.Set("fields", string(fieldsJSON))
				}
			}
			if err := txApp.Save(record); err != nil {
				slog.Error("failed to save log entry", "agent_id", agentID, "error", err)
				continue
			}
			saved++
		}
		return nil
	})
	if err != nil {
		slog.Error("logs ingest transaction failed", "agent_id", agentID, "error", err)
	}

	if saved > 0 {
		bumpLinesTotal(app, agentID, saved)
	}
}

func entryTime(unixMillis int64) time.Time {
	if unixMillis <= 0 {
		return time.Now().UTC()
	}
	return time.UnixMilli(unixMillis).UTC()
}

func normalizeLevel(level string) string {
	if validLevels[level] {
		return level
	}
	return defaultLevel
}

// truncateUTF8 shortens s to at most maxBytes bytes, trimming back to the
// nearest valid UTF-8 rune boundary rather than splitting a multi-byte
// rune in half. A cut that lands mid-sequence leaves an incomplete
// trailing rune (utf8.DecodeLastRuneInString reports RuneError with
// size 1 for that), so it keeps trimming one byte at a time until what
// remains ends on a complete rune.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	b := s[:maxBytes]
	for len(b) > 0 {
		r, size := utf8.DecodeLastRuneInString(b)
		if r != utf8.RuneError || size > 1 {
			break
		}
		b = b[:len(b)-1]
	}
	return b
}

// bumpLinesTotal increments the agent's optional "logs_lines_total" gauge
// field by count. Best-effort: a missing agent record or field is not an
// error worth logging loudly since this is a nice-to-have counter, not
// part of the ingest path's correctness.
func bumpLinesTotal(app core.App, agentID string, count int) {
	agent, err := app.FindRecordById("agents", agentID)
	if err != nil {
		return
	}
	if agent.Collection().Fields.GetByName("logs_lines_total") == nil {
		return
	}
	agent.Set("logs_lines_total", agent.GetFloat("logs_lines_total")+float64(count))
	if err := app.Save(agent); err != nil {
		slog.Warn("failed to update agent logs_lines_total", "agent_id", agentID, "error", err)
	}
}
