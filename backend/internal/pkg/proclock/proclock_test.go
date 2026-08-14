package proclock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// helper: create a temp lock path that doesn't collide with real services.
func tempLockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "aibot-test-"+t.Name()+".lock")
}

// TestAcquireAndRelease verifies basic acquire → release cycle.
func TestAcquireAndRelease(t *testing.T) {
	path := tempLockPath(t)
	service := "test-acquire-release"

	err := writeLock(path, service, os.Getpid())
	if err != nil {
		t.Fatalf("writeLock: %v", err)
	}

	// Verify file content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	var info lockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("parse lock file: %v", err)
	}
	if info.PID != os.Getpid() {
		t.Errorf("expected pid %d, got %d", os.Getpid(), info.PID)
	}
	if info.Service != service {
		t.Errorf("expected service %q, got %q", service, info.Service)
	}

	// Release should remove the file.
	lock := &Lock{path: path, pid: os.Getpid(), service: service}
	lock.Release()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("lock file should be removed after release")
	}
}

// TestAcquireStaleLock verifies that a stale lock (dead PID) is cleaned up.
func TestAcquireStaleLock(t *testing.T) {
	service := "test-stale-" + strconv.Itoa(os.Getpid())
	realPath := lockPath(service)
	defer os.Remove(realPath)

	// Write a lock with a PID that doesn't exist.
	fakePID := 4000000
	staleInfo := lockInfo{PID: fakePID, AcquiredAt: time.Now().UnixMilli(), Service: service}
	data, _ := json.Marshal(staleInfo)
	if err := os.WriteFile(realPath, data, 0644); err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	// Acquire should succeed by cleaning up the stale lock.
	lock := Acquire(service)
	defer lock.Release()

	// Verify the lock file now belongs to us.
	info, err := readLock(realPath)
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if info.PID != os.Getpid() {
		t.Errorf("expected pid %d, got %d", os.Getpid(), info.PID)
	}
}

// TestAcquireSamePID verifies that acquiring with the same PID is detected.
func TestAcquireSamePID(t *testing.T) {
	service := "test-samepid-" + strconv.Itoa(os.Getpid())
	path := lockPath(service)
	defer os.Remove(path)

	// Write a lock pretending to be our own PID.
	info := lockInfo{PID: os.Getpid(), AcquiredAt: time.Now().UnixMilli(), Service: service}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	// Acquire should panic with "already running".
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for same-pid lock")
		}
		msg := fmt.Sprintf("%v", r)
		if !contains(msg, "already running") {
			t.Errorf("expected 'already running' in panic, got: %s", msg)
		}
	}()
	Acquire(service)
}

// TestReleaseNotOurs verifies that Release skips if the lock belongs to another PID.
func TestReleaseNotOurs(t *testing.T) {
	path := tempLockPath(t)

	// Write lock with a different PID.
	info := lockInfo{PID: 9999999, AcquiredAt: time.Now().UnixMilli(), Service: "test"}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}

	lock := &Lock{path: path, pid: os.Getpid(), service: "test"}
	lock.Release()

	// File should still exist (we didn't own it).
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("lock file should NOT be removed when PID doesn't match")
	}
}

// TestReleaseNil verifies that Release on nil lock is safe.
func TestReleaseNil(t *testing.T) {
	var lock *Lock
	lock.Release() // should not panic
}

// TestIsProcessAliveCurrentProcess verifies the current process is detected as alive.
func TestIsProcessAliveCurrentProcess(t *testing.T) {
	if !isProcessAlive(os.Getpid()) {
		t.Error("current process should be alive")
	}
}

// TestIsProcessAliveDeadPID verifies a non-existent PID returns false.
func TestIsProcessAliveDeadPID(t *testing.T) {
	if isProcessAlive(4000000) {
		t.Error("PID 4000000 should not be alive")
	}
}

// TestIsProcessAliveZeroPID verifies edge case.
func TestIsProcessAliveZeroPID(t *testing.T) {
	if isProcessAlive(0) {
		t.Error("PID 0 should not be reported as alive")
	}
}

// TestKillProcessDeadPID verifies killing a dead PID returns true.
func TestKillProcessDeadPID(t *testing.T) {
	if !killProcess(4000000) {
		t.Error("killing a dead process should return true")
	}
}

// TestWriteLockExcl verifies that O_EXCL prevents double creation.
func TestWriteLockExcl(t *testing.T) {
	path := tempLockPath(t)
	err := writeLock(path, "test", os.Getpid())
	if err != nil {
		t.Fatalf("first writeLock: %v", err)
	}
	defer os.Remove(path)

	// Second write to same path should fail with EEXIST.
	err = writeLock(path, "test", os.Getpid())
	if err == nil {
		t.Fatal("expected EEXIST error on second writeLock")
	}
	if !os.IsExist(err) {
		t.Errorf("expected os.IsExist error, got: %v", err)
	}
}

// TestReadLockCorrupt verifies that a corrupt lock file returns an error.
func TestReadLockCorrupt(t *testing.T) {
	path := tempLockPath(t)
	if err := os.WriteFile(path, []byte("not json"), 0644); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}
	_, err := readLock(path)
	if err == nil {
		t.Error("expected error reading corrupt lock file")
	}
}

// TestReadLockInvalidPID verifies that a lock with PID 0 is rejected.
func TestReadLockInvalidPID(t *testing.T) {
	path := tempLockPath(t)
	info := lockInfo{PID: 0, AcquiredAt: time.Now().UnixMilli(), Service: "test"}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
	_, err := readLock(path)
	if err == nil {
		t.Error("expected error for PID 0")
	}
}

// TestLockPath verifies the lock path format.
func TestLockPath(t *testing.T) {
	p := lockPath("api")
	expected := filepath.Join(os.TempDir(), "aibot-api.lock")
	if p != expected {
		t.Errorf("lockPath(api) = %q, want %q", p, expected)
	}
}

// TestAcquireRealPath uses the real Acquire function end-to-end
// with a unique service name to avoid collisions.
func TestAcquireRealPath(t *testing.T) {
	service := "test-acquire-real-" + strconv.Itoa(os.Getpid())
	realPath := lockPath(service)
	defer os.Remove(realPath)

	lock := Acquire(service)
	if lock == nil {
		t.Fatal("Acquire returned nil")
	}
	if lock.path != realPath {
		t.Errorf("lock.path = %q, want %q", lock.path, realPath)
	}
	if lock.pid != os.Getpid() {
		t.Errorf("lock.pid = %d, want %d", lock.pid, os.Getpid())
	}

	// Lock file should exist.
	if _, err := os.Stat(realPath); os.IsNotExist(err) {
		t.Fatal("lock file should exist after Acquire")
	}

	// Verify content.
	info, err := readLock(realPath)
	if err != nil {
		t.Fatalf("readLock: %v", err)
	}
	if info.PID != os.Getpid() {
		t.Errorf("lock PID = %d, want %d", info.PID, os.Getpid())
	}

	lock.Release()

	// File should be gone.
	if _, err := os.Stat(realPath); !os.IsNotExist(err) {
		t.Error("lock file should be removed after Release")
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
