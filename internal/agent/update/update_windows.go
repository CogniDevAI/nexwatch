//go:build windows

package update

import "golang.org/x/sys/windows"

// init overrides the cross-platform scheduleDelayedReplace stub (update.go)
// with the real Windows implementation: installBinary's fallback for when
// the running executable's name is locked (without FILE_SHARE_DELETE) by
// something other than this process — a security product, an SCM handle,
// or antivirus scanning — and the immediate os.Rename(newPath, execPath)
// fails.
//
// windows.MoveFileEx with MOVEFILE_DELAY_UNTIL_REBOOT asks the OS to
// perform the rename during the next boot, before any user-mode process
// (including whatever was holding the lock) runs — the same mechanism
// Windows Update and most installers use to replace an in-use file.
// MOVEFILE_REPLACE_EXISTING is combined with it because installBinary
// restores the previous binary under execPath's name immediately after
// scheduling this (so the agent keeps running under its old version until
// the reboot actually happens), meaning a file already occupies execPath
// by the time this pending operation executes.
//
// This requires the calling process to hold SE_RESTORE_NAME (granted to
// LocalSystem, the account cmd/agent's service integration installs the
// Windows service to run as — see service_windows.go).
func init() {
	scheduleDelayedReplace = func(newPath, execPath string) error {
		from, err := windows.UTF16PtrFromString(newPath)
		if err != nil {
			return err
		}
		to, err := windows.UTF16PtrFromString(execPath)
		if err != nil {
			return err
		}
		return windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	}
}
