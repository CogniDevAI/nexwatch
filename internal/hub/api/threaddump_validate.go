package api

import (
	"fmt"
	"strings"
)

// isDumpablePID reports whether pid is present in the given process
// snapshot and looks like a JVM process eligible for a jstack thread dump.
// It returns false with a human-readable reason when the PID is unknown or
// clearly not a JVM, so the caller can surface a clear 400 error instead of
// blindly forwarding an arbitrary PID to the agent's jstack execution.
func isDumpablePID(procs []processEntry, pid int) (bool, string) {
	if len(procs) == 0 {
		return false, "no process snapshot available for this agent yet"
	}

	for _, p := range procs {
		if p.PID != pid {
			continue
		}
		if looksLikeJVM(p.Name, p.Cmdline) {
			return true, ""
		}
		return false, fmt.Sprintf("pid %d (%s) does not look like a JVM process", pid, p.Name)
	}

	return false, fmt.Sprintf("pid %d was not found in the latest process snapshot", pid)
}

// looksLikeJVM reports whether a process name/cmdline pair looks like a Java
// virtual machine process: its base name is "java" or "jsvc", or its
// command line mentions "java" (e.g. a wrapper script invoking a JVM).
func looksLikeJVM(name, cmdline string) bool {
	base := name
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		base = name[idx+1:]
	}
	if base == "java" || base == "jsvc" {
		return true
	}
	return strings.Contains(cmdline, "java")
}
