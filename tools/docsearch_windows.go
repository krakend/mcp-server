//go:build windows

package tools

import (
	"syscall"
)

// isProcessRunning checks if a process with given PID is running on Windows
func isProcessRunning(pid int) bool {
	// On Windows, FindProcess always succeeds, so use OpenProcess to confirm the process exists.
	const da = syscall.STANDARD_RIGHTS_READ | syscall.PROCESS_QUERY_INFORMATION | syscall.SYNCHRONIZE

	h, err := syscall.OpenProcess(da, false, uint32(pid))
	if err != nil {
		return false
	}

	syscall.CloseHandle(h)
	return true // Process exists — OpenProcess confirmed it
}
