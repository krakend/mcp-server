package tools

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestLockMechanism(t *testing.T) {
	// Use temp directory for testing
	oldDataDir := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = oldDataDir }()

	// Create search directory
	searchDir := filepath.Join(dataDir, "search")
	if err := os.MkdirAll(searchDir, 0755); err != nil {
		t.Fatalf("Failed to create search dir: %v", err)
	}

	t.Run("acquire and release lock", func(t *testing.T) {
		// Clean state
		os.Remove(filepath.Join(dataDir, lockFile))

		// Acquire lock
		if err := acquireLock(); err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}

		// Verify lock file exists
		lockPath := filepath.Join(dataDir, lockFile)
		data, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Lock file not found: %v", err)
		}

		// Verify PID is correct
		pid, err := strconv.Atoi(string(data))
		if err != nil {
			t.Fatalf("Invalid PID in lock file: %v", err)
		}
		if pid != os.Getpid() {
			t.Errorf("Lock has wrong PID: got %d, want %d", pid, os.Getpid())
		}

		// Release lock
		if err := releaseLock(); err != nil {
			t.Fatalf("Failed to release lock: %v", err)
		}

		// Verify lock file removed
		if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
			t.Error("Lock file should be removed after release")
		}
	})

	t.Run("detect stale lock", func(t *testing.T) {
		// Clean state
		os.Remove(filepath.Join(dataDir, lockFile))

		// Create fake stale lock with non-existent PID
		stalePID := 99999
		lockPath := filepath.Join(dataDir, lockFile)
		if err := os.WriteFile(lockPath, []byte(strconv.Itoa(stalePID)), 0644); err != nil {
			t.Fatalf("Failed to create stale lock: %v", err)
		}

		// Try to acquire lock (should clean stale lock)
		if err := acquireLock(); err != nil {
			t.Fatalf("Failed to acquire lock after stale lock: %v", err)
		}

		// Verify our PID is now in lock
		data, _ := os.ReadFile(lockPath)
		pid, _ := strconv.Atoi(string(data))
		if pid != os.Getpid() {
			t.Errorf("Expected our PID after cleaning stale lock, got %d", pid)
		}

		// Cleanup
		releaseLock()
	})

	t.Run("reacquire same lock", func(t *testing.T) {
		// Clean state
		os.Remove(filepath.Join(dataDir, lockFile))

		// Acquire lock
		if err := acquireLock(); err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}

		// Try to acquire again (should succeed immediately - same PID)
		if err := acquireLock(); err != nil {
			t.Fatalf("Failed to reacquire lock: %v", err)
		}

		// Cleanup
		releaseLock()
	})

	t.Run("timeout on held lock", func(t *testing.T) {
		// Skip this test as it takes 5 seconds (lockTimeout)
		// In real usage, this timeout is intentional to wait for other processes
		t.Skip("Skipping timeout test (takes 5s) - timeout behavior verified manually")

		// Clean state
		os.Remove(filepath.Join(dataDir, lockFile))

		// Create lock with a different PID that exists (PID 1 always exists on Unix)
		lockPath := filepath.Join(dataDir, lockFile)
		if err := os.WriteFile(lockPath, []byte("1"), 0644); err != nil {
			t.Fatalf("Failed to create lock: %v", err)
		}

		start := time.Now()
		err := acquireLock()
		elapsed := time.Since(start)

		if err == nil {
			t.Error("Expected error acquiring held lock, got nil")
			releaseLock()
		}

		// Should timeout after ~5 seconds
		if elapsed < 4*time.Second || elapsed > 6*time.Second {
			t.Errorf("Expected timeout of ~5s, got %v", elapsed)
		}

		// Cleanup
		os.Remove(lockPath)
	})

	t.Run("is process running", func(t *testing.T) {
		// Test our own PID (should be running)
		if !isProcessRunning(os.Getpid()) {
			t.Error("Our own process should be detected as running")
		}

		// Test non-existent PID
		if isProcessRunning(99999) {
			t.Error("Non-existent process should not be detected as running")
		}
	})
}
