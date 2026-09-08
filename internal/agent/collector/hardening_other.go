//go:build !windows

package collector

// checkRDPStatus is a no-op on non-Windows platforms: the check reads a
// Windows-only registry value (see hardening_windows.go), which does not
// exist elsewhere. Always skipped via evaluateRDPDenyFlag(_, false), the
// same shared "could not read the value" path hardening_windows.go itself
// falls back to on a read failure.
func (c *HardeningCollector) checkRDPStatus() checkResult {
	return evaluateRDPDenyFlag(0, false)
}
