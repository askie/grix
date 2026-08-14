//go:build !windows

package proclock

import (
	"syscall"
)

// isProcessAlive checks whether a process with the given PID is still running (Unix).
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if err == syscall.ESRCH {
		return false
	}
	// EPERM: process exists but we lack permission — still alive.
	return true
}

// killProcess terminates the process with SIGTERM, waits, then escalates to SIGKILL.
func killProcess(pid int) bool {
	if pid <= 0 {
		return true
	}
	// Graceful: SIGTERM
	_ = syscall.Kill(pid, syscall.SIGTERM)
	if waitForExit(pid, termWaitTimeout) {
		return true
	}
	// Force: SIGKILL
	_ = syscall.Kill(pid, syscall.SIGKILL)
	return waitForExit(pid, killWaitTimeout)
}
