package checks

import (
	"net"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// execTCP performs one TCP probe attempt: a plain dial-and-close against
// check.target ("host:port"), recording latency and any dial error.
func execTCP(check *core.Record) probeResult {
	timeout := timeoutDuration(check)
	target := check.GetString("target")

	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, timeout)
	latency := time.Since(start)
	if err != nil {
		return probeResult{Success: false, LatencyMs: msFloat(latency), Error: err.Error()}
	}
	_ = conn.Close()

	return probeResult{Success: true, LatencyMs: msFloat(latency)}
}
