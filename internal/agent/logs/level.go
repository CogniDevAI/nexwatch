package logs

import "regexp"

// levelTokenPattern matches the level-indicating tokens DetectLevel looks
// for. Alternation order does not imply precedence — matchLevel below
// decides precedence explicitly (error > warn > info > debug) regardless
// of which token appears first in the line, since a message mentioning
// several tokens (e.g. "info: retrying after warning from upstream")
// should still be classified by its most severe token.
var levelTokenPattern = regexp.MustCompile(`(?i)\b(error|fatal|panic|warn(?:ing)?|info|notice|debug|trace)\b`)

// DetectLevel classifies a plain-text log line (a file-tailed source has
// no structured level of its own, unlike journald's PRIORITY field) by
// searching for a level token, defaulting to "info" when none is found.
// Precedence when a line mentions more than one token: error > warning >
// info > debug.
func DetectLevel(line string) string {
	matches := levelTokenPattern.FindAllString(line, -1)
	if len(matches) == 0 {
		return "info"
	}

	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		seen[normalizeLevelToken(m)] = true
	}

	switch {
	case seen["error"]:
		return "error"
	case seen["warning"]:
		return "warning"
	case seen["info"]:
		return "info"
	case seen["debug"]:
		return "debug"
	default:
		return "info"
	}
}

// normalizeLevelToken maps a matched token (any case, "warn" or
// "warning", "fatal"/"panic" as error-equivalents, "notice" as an
// info-equivalent, "trace" as a debug-equivalent) onto one of the four
// canonical levels.
func normalizeLevelToken(token string) string {
	switch toLower(token) {
	case "error", "fatal", "panic":
		return "error"
	case "warn", "warning":
		return "warning"
	case "info", "notice":
		return "info"
	case "debug", "trace":
		return "debug"
	default:
		return "info"
	}
}

// toLower is a tiny ASCII-only lowercase helper so this file has no
// dependency beyond "regexp".
func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// mapSyslogPriority converts a journald PRIORITY value (the syslog
// severity number, "0" through "7", as a string) into one of the four
// canonical levels: 0-3 (emerg/alert/crit/err) -> error, 4 (warning) ->
// warning, 5-6 (notice/info) -> info, 7 (debug) -> debug. An unrecognized
// or empty value defaults to "info".
func mapSyslogPriority(priority string) string {
	switch priority {
	case "0", "1", "2", "3":
		return "error"
	case "4":
		return "warning"
	case "5", "6":
		return "info"
	case "7":
		return "debug"
	default:
		return "info"
	}
}
