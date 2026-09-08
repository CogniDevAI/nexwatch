//go:build windows

package collector

import "golang.org/x/sys/windows/registry"

// rdpRegistryKey/rdpRegistryValue locate the Remote Desktop enable/disable
// flag: HKEY_LOCAL_MACHINE\System\CurrentControlSet\Control\Terminal Server,
// value "fDenyTSConnections" (a DWORD; 0 = RDP enabled, non-zero =
// disabled). This is the same value the Windows "Remote Desktop" settings
// page and Group Policy's "Allow users to connect remotely" setting both
// write to.
const (
	rdpRegistryKey   = `System\CurrentControlSet\Control\Terminal Server`
	rdpRegistryValue = "fDenyTSConnections"
)

// checkRDPStatus reads fDenyTSConnections from the registry and reports it
// via the platform-independent evaluateRDPDenyFlag (hardening.go).
func (c *HardeningCollector) checkRDPStatus() checkResult {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, rdpRegistryKey, registry.QUERY_VALUE)
	if err != nil {
		return evaluateRDPDenyFlag(0, false)
	}
	defer func() { _ = k.Close() }()

	v, _, err := k.GetIntegerValue(rdpRegistryValue)
	if err != nil {
		return evaluateRDPDenyFlag(0, false)
	}
	return evaluateRDPDenyFlag(v, true)
}
