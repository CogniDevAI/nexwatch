// Package checks implements hub-side black-box monitoring: periodic
// HTTP/TCP/ICMP probes against arbitrary targets (URLs, host:port pairs, or
// bare hostnames/IPs — not necessarily a registered "agents" record), with
// TLS certificate expiry tracking for HTTP checks.
//
// Scheduler runs one goroutine per enabled "checks" record, each on its own
// jittered interval, and reloads automatically when checks are created,
// updated, or deleted (see Start). Every probe attempt is persisted as a
// "check_results" row; failures_before_down consecutive failures are
// required before a check's debounced status flips to "down" (and a single
// success flips it back to "up" immediately) — see nextDebouncedState.
package checks

import (
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// Default values applied when a "checks" record leaves the corresponding
// field unset (its Go zero value) — PocketBase field schemas have no
// server-side default, so these are applied at evaluation time instead,
// the same way other hub features apply their defaults in application code
// (e.g. agents.status defaulting to "pending" in the token-generation
// handler).
const (
	defaultIntervalSeconds          = 60
	minIntervalSeconds              = 10
	defaultTimeoutSeconds           = 5
	defaultHTTPMethod               = "GET"
	defaultExpectedStatus           = 200
	defaultFailuresBeforeDown       = 2
	defaultTLSExpiryWarnDays        = 14
	maxHTTPBodyReadBytes      int64 = 1 << 20 // 1 MiB, per the body-contains check's spec
	maxHTTPRedirects                = 3
)

func intervalSeconds(check *core.Record) int {
	v := check.GetInt("interval_seconds")
	if v < minIntervalSeconds {
		return defaultIntervalSeconds
	}
	return v
}

func timeoutDuration(check *core.Record) time.Duration {
	v := check.GetInt("timeout_seconds")
	if v <= 0 {
		v = defaultTimeoutSeconds
	}
	return time.Duration(v) * time.Second
}

func httpMethod(check *core.Record) string {
	m := check.GetString("method")
	if m != "GET" && m != "HEAD" {
		return defaultHTTPMethod
	}
	return m
}

func expectedStatus(check *core.Record) int {
	v := check.GetInt("expected_status")
	if v <= 0 {
		return defaultExpectedStatus
	}
	return v
}

func failuresBeforeDown(check *core.Record) int {
	v := check.GetInt("failures_before_down")
	if v < 1 {
		return defaultFailuresBeforeDown
	}
	return v
}

// TLSExpiryWarnDays returns the check's configured certificate-expiry
// warning threshold. It is read by the API layer (checks summary) to flag
// a check whose certificate expires soon; the alert engine's cert_expiry
// rule type uses its own alert_rules.threshold instead, so the two can
// differ (e.g. a dashboard badge at 14 days, an alert at 7).
func TLSExpiryWarnDays(check *core.Record) int {
	v := check.GetInt("tls_expiry_warn_days")
	if v <= 0 {
		return defaultTLSExpiryWarnDays
	}
	return v
}

// probeResult is the outcome of one probe attempt, independent of the
// check's debounced up/down status (see nextDebouncedState).
type probeResult struct {
	Success      bool
	LatencyMs    float64
	StatusCode   int       // 0 when not applicable (tcp, icmp)
	Error        string    // empty on success
	TLSExpiresAt time.Time // zero when not applicable or not observed
}

func msFloat(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

// nextDebouncedState computes a check's next debounced up/down status.
// prevStatus is the previous debounced status ("" if the check has never
// run before); prevConsecutiveFails is the failure streak going into this
// attempt. A success always resets the streak and reports "up"
// immediately. A failure only flips the status to "down" once it is the
// Nth consecutive one (N = failuresBeforeDown, clamped to at least 1);
// until then the previous status holds (or "up", if there is no previous
// status yet — a brand new check's first attempt failing does not
// immediately read as "down" unless failuresBeforeDown is 1).
func nextDebouncedState(prevStatus string, prevConsecutiveFails int, success bool, failuresBeforeDown int) (status string, consecutiveFails int) {
	if failuresBeforeDown < 1 {
		failuresBeforeDown = 1
	}
	if success {
		return "up", 0
	}

	consecutiveFails = prevConsecutiveFails + 1
	if consecutiveFails >= failuresBeforeDown {
		return "down", consecutiveFails
	}
	if prevStatus == "" {
		return "up", consecutiveFails
	}
	return prevStatus, consecutiveFails
}
