//go:build windows

package tools

import (
	"os"
	"syscall"
)

// isProcessRunning checks if a process with given PID is running on Windows
func isProcessRunning(pid int) bool {
	// On Windows, we use a different approach since syscall.Kill doesn't exist
	// FindProcess returns a Process even if the PID doesn't exist on Windows,
	// so we need to test if we can actually access it

	const da = syscall.STANDARD_RIGHTS_READ | syscall.PROCESS_QUERY_INFORMATION | syscall.SYNCHRONIZE

	h, err := syscall.OpenProcess(da, false, uint32(pid))
	if err != nil {
		// Cannot open process - it doesn't exist or we don't have permission
		// For ESRCH equivalent on Windows (ERROR_INVALID_PARAMETER), process doesn't exist
		return false
	}

	// Successfully opened the process handle, so it exists
	syscall.CloseHandle(h)

	// Alternative: check exit code to see if process is still running
	// But if we can open it, it's likely still running
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Release the handle
	proc.Release()

	return true
}
