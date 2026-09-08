//go:build !linux && !windows

package collector

import (
	"context"
	"runtime"
)

// Collect reports that the services collector has no implementation on
// this platform (only Linux's systemd and Windows' Service Control
// Manager are supported) rather than guessing at a shape another OS's
// service manager might use.
func (c *ServicesCollector) Collect(_ context.Context) (map[string]any, error) {
	return map[string]any{
		"available": false,
		"reason":    "services collector not implemented for " + runtime.GOOS,
		"services":  []map[string]any{},
		"total":     0,
		"running":   0,
		"failed":    0,
		"other":     0,
	}, nil
}
