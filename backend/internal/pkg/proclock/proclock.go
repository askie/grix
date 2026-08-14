// Package proclock provides cross-platform (Windows/macOS/Linux) single-instance
// process locking using O_EXCL atomic file creation with PID-based staleness detection.
//
// Lock files are placed in os.TempDir() as "aibot-<service>.lock".
// If an old process is still running, it is terminated automatically before acquiring.
package proclock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	maxRetries      = 3
	termWaitTimeout = 3 * time.Second
	killWaitTimeout = 1 * time.Second
	pollInterval    = 100 * time.Millisecond
)

// lockInfo is the JSON structure stored in the lock file.
type lockInfo struct {
	PID        int    `json:"pid"`
	AcquiredAt int64  `json:"acquired_at"`
	Service    string `json:"service"`
}

// Lock represents a held process lock. Call Release() on shutdown.
type Lock struct {
	path    string
	pid     int
	service string
}

// Acquire attempts to acquire an exclusive lock for the given service.
// If an old process is still running, it is terminated automatically.
// Returns a *Lock that must be released on shutdown.
// Panics if the lock cannot be acquired after retries.
func Acquire(service string) *Lock {
	path := lockPath(service)
	pid := os.Getpid()

	for attempt := 0; attempt <= maxRetries; attempt++ {
		err := writeLock(path, service, pid)
		if err == nil {
			return &Lock{path: path, pid: pid, service: service}
		}
		if !os.IsExist(err) {
			panic(fmt.Sprintf("proclock: failed to acquire lock for %q: %v", service, err))
		}

		// Lock file exists — inspect the owner.
		existing, rerr := readLock(path)
		if rerr != nil {
			// Corrupt or unreadable lock file — remove and retry.
			os.Remove(path)
			continue
		}

		if existing.PID == pid {
			panic(fmt.Sprintf("proclock: %s already running (pid %d, acquired at %s)",
				service, pid, time.UnixMilli(existing.AcquiredAt).Format(time.RFC3339)))
		}

		if !isProcessAlive(existing.PID) {
			// Stale lock — owner is dead.
			os.Remove(path)
			continue
		}

		// Old process is alive — terminate it.
		fmt.Fprintf(os.Stderr, "proclock: %s terminating old process (pid %d)\n", service, existing.PID)
		if !killProcess(existing.PID) {
			panic(fmt.Sprintf("proclock: cannot terminate old %s process (pid %d)", service, existing.PID))
		}
		os.Remove(path)
	}

	panic(fmt.Sprintf("proclock: failed to acquire lock for %q after %d retries", service, maxRetries))
}

// Release removes the lock file if it still belongs to this process.
func (l *Lock) Release() {
	if l == nil || l.path == "" {
		return
	}
	existing, err := readLock(l.path)
	if err != nil {
		return
	}
	if existing.PID == l.pid {
		os.Remove(l.path)
	}
}

// lockPath returns the lock file path for a service.
func lockPath(service string) string {
	return filepath.Join(os.TempDir(), "aibot-"+service+".lock")
}

// writeLock atomically creates the lock file using O_EXCL.
func writeLock(path, service string, pid int) error {
	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	defer fd.Close()

	info := lockInfo{
		PID:        pid,
		AcquiredAt: time.Now().UnixMilli(),
		Service:    service,
	}
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	_, err = fd.Write(data)
	return err
}

// readLock reads and parses the lock file.
func readLock(path string) (*lockInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var info lockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	if info.PID <= 0 {
		return nil, fmt.Errorf("invalid pid %d in lock file", info.PID)
	}
	return &info, nil
}

// waitForExit polls until the process exits or the deadline elapses.
func waitForExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isProcessAlive(pid) {
			return true
		}
		time.Sleep(pollInterval)
	}
	return !isProcessAlive(pid)
}
