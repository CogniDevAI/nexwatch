// Package platform decides which agent collectors have a working
// implementation on the current runtime.GOOS, independent of what an
// operator's config file asks for. cmd/agent's registerCollectors consults
// this before registering each collector, so an agent.yaml that lists a
// Linux-only collector on a Windows host (e.g. copied verbatim from a
// Linux fleet's config) degrades to a clear, logged skip instead of a
// collector that silently returns empty/broken data.
package platform

import "runtime"

// unsupported maps a collector name to the GOOS values on which it has no
// working implementation, and why. A collector absent from this map is
// assumed supported everywhere its own build succeeds.
var unsupported = map[string]map[string]string{
	// vulnerabilities inspects world-writable files and SUID bits via
	// find(1) and POSIX permission bits — none of which exist on Windows.
	"vulnerabilities": {
		"windows": "checks world-writable files and SUID bits via POSIX permissions and find(1), neither of which exist on Windows",
	},
	// oracle shells out to sqlplus assuming a Linux/Unix Oracle Instant
	// Client or full RDBMS install; a Windows Oracle client uses a
	// different install layout this collector was never written against.
	"oracle": {
		"windows": "shells out to sqlplus assuming a Linux/Unix Oracle client install",
	},
}

// Supported reports whether collector name has a working implementation on
// the current GOOS. When it does not, reason explains why, suitable for a
// single log line at registration time.
func Supported(name string) (ok bool, reason string) {
	if byGOOS, found := unsupported[name]; found {
		if reason, disabled := byGOOS[runtime.GOOS]; disabled {
			return false, reason
		}
	}
	return true, ""
}
