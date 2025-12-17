//go:build unix

package tools

import "syscall"

// isProcessRunning checks if a process with given PID is running on Unix systems
func isProcessRunning(pid int) bool {
	// Send signal 0 to check if process exists
	// Signal 0 is a special signal that doesn't actually kill the process
	// but checks if we can send a signal to it
	err := syscall.Kill(pid, syscall.Signal(0))

	if err == nil {
		// No error means process exists and we can signal it
		return true
	}

	// Check error type
	if err == syscall.ESRCH {
		// ESRCH = "no such process" - process doesn't exist
		return false
	}

	if err == syscall.EPERM {
		// EPERM = "operation not permitted" - process exists but we don't have permission
		// This still counts as "running" for our purposes
		return true
	}

	// Any other error, assume process is not running
	return false
}
