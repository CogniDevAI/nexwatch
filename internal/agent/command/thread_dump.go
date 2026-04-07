package command

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ThreadDump executes a thread dump for the given PID.
// It auto-detects the jstack binary from the process's own JDK,
// falling back to any jstack found in PATH.
func ThreadDump(ctx context.Context, pid int) (string, error) {
	jstack, err := findJstack(pid)
	if err != nil {
		return "", fmt.Errorf("jstack not found: %w", err)
	}

	// 60 second timeout for the dump itself.
	dumpCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(dumpCtx, jstack, "-l", fmt.Sprintf("%d", pid))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return "", fmt.Errorf("jstack failed: %s", errMsg)
	}

	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("jstack returned empty output")
	}

	return out, nil
}

// findJstack resolves the jstack binary to use for a given PID.
// Priority:
//  1. jstack from the same JDK that launched the process (/proc/<pid>/exe → java → ../bin/jstack)
//  2. jstack in PATH
func findJstack(pid int) (string, error) {
	// Try to read the java binary path via /proc/<pid>/exe symlink.
	exeLink := fmt.Sprintf("/proc/%d/exe", pid)
	javaPath, err := os.Readlink(exeLink)
	if err == nil {
		// javaPath is e.g. /home/fitbank/jdk1.8.0_91/bin/java
		// jstack lives in the same bin/ directory.
		jstackPath := filepath.Join(filepath.Dir(javaPath), "jstack")
		if _, statErr := os.Stat(jstackPath); statErr == nil {
			return jstackPath, nil
		}
	}

	// Fallback: look for jstack in PATH.
	if path, err := exec.LookPath("jstack"); err == nil {
		return path, nil
	}

	return "", fmt.Errorf("no jstack found for PID %d (tried %s and PATH)", pid, exeLink)
}
