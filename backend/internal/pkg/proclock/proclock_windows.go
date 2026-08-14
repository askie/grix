//go:build windows

package proclock

import (
	"os/exec"
	"strconv"
	"syscall"
)

const processQueryLimitedInfo = 0x1000 // PROCESS_QUERY_LIMITED_INFORMATION

// isProcessAlive checks whether a process with the given PID is still running (Windows).
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(processQueryLimitedInfo, false, uint32(pid))
	if err != nil {
		return false
	}
	syscall.CloseHandle(handle)
	return true
}

// killProcess terminates the process using taskkill /T /F (process tree, forced).
func killProcess(pid int) bool {
	if pid <= 0 {
		return true
	}
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	_ = cmd.Run()
	return waitForExit(pid, termWaitTimeout)
}
