// Package logging configures the hub's process-wide structured logger
// (log/slog) and provides an HTTP request-id/access-log middleware for the
// PocketBase router.
package logging

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// EnvLogFormat is the environment variable that selects the log format
// ("text" or "json") when --log-format is not passed on the command line.
const EnvLogFormat = "NEXWATCH_LOG_FORMAT"

// ResolveFormat determines the log format from an explicit --log-format
// flag value (highest priority) and the NEXWATCH_LOG_FORMAT environment
// variable, defaulting to "text" when neither is set.
func ResolveFormat(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv(EnvLogFormat); v != "" {
		return v
	}
	return "text"
}

// NewHandler builds an slog.Handler for the given format ("json" selects
// slog.JSONHandler; anything else, including "" or "text", selects
// slog.TextHandler), writing to w. It is split out from Setup so the
// format-selection logic can be unit tested against a buffer instead of
// the real os.Stdout.
func NewHandler(format string, w io.Writer) slog.Handler {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if format == "json" {
		return slog.NewJSONHandler(w, opts)
	}
	return slog.NewTextHandler(w, opts)
}

// Setup configures the process-wide slog default logger for the given
// format, writing to os.Stdout, and returns it.
func Setup(format string) *slog.Logger {
	logger := slog.New(NewHandler(format, os.Stdout))
	slog.SetDefault(logger)
	return logger
}

// GenerateRequestID returns a new random 16-character hex request
// identifier, used when an incoming request has no X-Request-ID header.
func GenerateRequestID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand failing is effectively never observed in practice; fall
		// back to a fixed-but-still-unique-enough value derived from time
		// rather than panicking over a request-tracing convenience.
		return hex.EncodeToString([]byte(time.Now().Format("20060102150405.000000000")))
	}
	return hex.EncodeToString(buf)
}

// RequestIDMiddleware returns a PocketBase router middleware that:
//  1. reads the caller's X-Request-ID header, or generates one;
//  2. echoes it back on the response's X-Request-ID header;
//  3. logs one structured access-log line per request (method, path,
//     status, duration_ms, request_id) via logger once the handler chain
//     completes.
func RequestIDMiddleware(logger *slog.Logger) func(e *core.RequestEvent) error {
	return func(e *core.RequestEvent) error {
		start := time.Now()

		requestID := e.Request.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = GenerateRequestID()
		}
		e.Response.Header().Set("X-Request-ID", requestID)

		err := e.Next()

		logger.Info("http request",
			"method", e.Request.Method,
			"path", e.Request.URL.Path,
			"status", statusOrDefault(e),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", requestID,
		)

		return err
	}
}

// statusOrDefault reads the response status via e.Status() (populated by
// PocketBase's status-tracking ResponseWriter), falling back to 200 for the
// rare case a handler never explicitly wrote a status before returning.
func statusOrDefault(e *core.RequestEvent) int {
	if status := e.Status(); status != 0 {
		return status
	}
	return http.StatusOK
}
