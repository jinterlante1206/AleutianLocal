package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestProcessLock_DefaultConfig(t *testing.T) {
	config := DefaultProcessLockConfig()

	if config.LockDir == "" {
		t.Error("DefaultProcessLockConfig should set LockDir")
	}
	if config.LockName != "aleutian" {
		t.Errorf("DefaultProcessLockConfig LockName = %q, want %q", config.LockName, "aleutian")
	}
}

func TestProcessLock_NewProcessLock(t *testing.T) {
	tests := []struct {
		name   string
		config ProcessLockConfig
		want   struct {
			lockDir  string
			lockName string
		}
	}{
		{
			name:   "default values",
			config: ProcessLockConfig{},
			want: struct {
				lockDir  string
				lockName string
			}{
				lockDir:  os.TempDir(),
				lockName: "aleutian",
			},
		},
		{
			name: "custom values",
			config: ProcessLockConfig{
				LockDir:  "/custom/dir",
				LockName: "myapp",
			},
			want: struct {
				lockDir  string
				lockName string
			}{
				lockDir:  "/custom/dir",
				lockName: "myapp",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lock := NewProcessLock(tt.config)

			expectedLockPath := filepath.Join(tt.want.lockDir, tt.want.lockName+".lock")
			if lock.LockPath() != expectedLockPath {
				t.Errorf("LockPath() = %q, want %q", lock.LockPath(), expectedLockPath)
			}

			expectedPIDPath := filepath.Join(tt.want.lockDir, tt.want.lockName+".pid")
			if lock.PIDPath() != expectedPIDPath {
				t.Errorf("PIDPath() = %q, want %q", lock.PIDPath(), expectedPIDPath)
			}
		})
	}
}

func TestProcessLock_AcquireRelease(t *testing.T) {
	// Use temp directory for test isolation
	tmpDir := t.TempDir()

	lock := NewProcessLock(ProcessLockConfig{
		LockDir:  tmpDir,
		LockName: "test",
	})

	// Initially not held
	if lock.IsHeld() {
		t.Error("Lock should not be held initially")
	}

	// Acquire should succeed
	err := lock.Acquire()
	if err != nil {
		t.Fatalf("Acquire() failed: %v", err)
	}

	// Should be held now
	if !lock.IsHeld() {
		t.Error("Lock should be held after Acquire()")
	}

	// PID file should exist and contain our PID
	pid := lock.HolderPID()
	if pid != os.Getpid() {
		t.Errorf("HolderPID() = %d, want %d", pid, os.Getpid())
	}

	// Double acquire should be idempotent
	err = lock.Acquire()
	if err != nil {
		t.Errorf("Double Acquire() should succeed: %v", err)
	}

	// Release should succeed
	err = lock.Release()
	if err != nil {
		t.Fatalf("Release() failed: %v", err)
	}

	// Should not be held after release
	if lock.IsHeld() {
		t.Error("Lock should not be held after Release()")
	}

	// PID file should be removed
	if _, err := os.Stat(lock.PIDPath()); !os.IsNotExist(err) {
		t.Error("PID file should be removed after Release()")
	}

	// Double release should be safe
	err = lock.Release()
	if err != nil {
		t.Errorf("Double Release() should succeed: %v", err)
	}
}

func TestProcessLock_BlocksSecondInstance(t *testing.T) {
	// Use temp directory for test isolation
	tmpDir := t.TempDir()

	lock1 := NewProcessLock(ProcessLockConfig{
		LockDir:  tmpDir,
		LockName: "test",
	})
	lock2 := NewProcessLock(ProcessLockConfig{
		LockDir:  tmpDir,
		LockName: "test",
	})

	// First lock should succeed
	err := lock1.Acquire()
	if err != nil {
		t.Fatalf("First Acquire() failed: %v", err)
	}
	defer lock1.Release()

	// Second lock should fail
	err = lock2.Acquire()
	if err == nil {
		lock2.Release()
		t.Fatal("Second Acquire() should fail when lock is held")
	}

	// Error should mention another instance
	if !strings.Contains(err.Error(), "another aleutian instance") {
		t.Errorf("Error should mention another instance, got: %v", err)
	}

	// Should be able to get holder PID
	holderPID := lock2.HolderPID()
	if holderPID != os.Getpid() {
		t.Errorf("HolderPID() = %d, want %d", holderPID, os.Getpid())
	}
}

func TestProcessLock_ReleaseMakesAvailable(t *testing.T) {
	tmpDir := t.TempDir()

	lock1 := NewProcessLock(ProcessLockConfig{
		LockDir:  tmpDir,
		LockName: "test",
	})
	lock2 := NewProcessLock(ProcessLockConfig{
		LockDir:  tmpDir,
		LockName: "test",
	})

	// Acquire and release first lock
	if err := lock1.Acquire(); err != nil {
		t.Fatalf("First Acquire() failed: %v", err)
	}
	if err := lock1.Release(); err != nil {
		t.Fatalf("Release() failed: %v", err)
	}

	// Second lock should now succeed
	if err := lock2.Acquire(); err != nil {
		t.Fatalf("Second Acquire() should succeed after release: %v", err)
	}
	defer lock2.Release()
}

func TestProcessLock_HolderPID_NoFile(t *testing.T) {
	tmpDir := t.TempDir()

	lock := NewProcessLock(ProcessLockConfig{
		LockDir:  tmpDir,
		LockName: "test",
	})

	// Without acquiring, HolderPID should return 0
	pid := lock.HolderPID()
	if pid != 0 {
		t.Errorf("HolderPID() without lock = %d, want 0", pid)
	}
}

func TestProcessLock_HolderPID_InvalidFile(t *testing.T) {
	tmpDir := t.TempDir()

	lock := NewProcessLock(ProcessLockConfig{
		LockDir:  tmpDir,
		LockName: "test",
	})

	// Write invalid PID file
	if err := os.WriteFile(lock.PIDPath(), []byte("not-a-number"), 0644); err != nil {
		t.Fatalf("Failed to write invalid PID file: %v", err)
	}

	pid := lock.HolderPID()
	if pid != 0 {
		t.Errorf("HolderPID() with invalid file = %d, want 0", pid)
	}
}

func TestProcessLock_InterfaceCompliance(t *testing.T) {
	// Verify ProcessLock implements ProcessLocker
	var _ ProcessLocker = (*ProcessLock)(nil)
}

func TestErrLockHeld_Error(t *testing.T) {
	tests := []struct {
		name string
		err  ErrLockHeld
		want string
	}{
		{
			name: "with PID",
			err:  ErrLockHeld{HolderPID: 12345, LockPath: "/tmp/test.lock"},
			want: "another aleutian instance is running (PID 12345)",
		},
		{
			name: "without PID",
			err:  ErrLockHeld{HolderPID: 0, LockPath: "/tmp/test.lock"},
			want: "another aleutian instance is running (check: lsof /tmp/test.lock)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("ErrLockHeld.Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestProcessLock_CrossProcess tests locking behavior across actual processes.
// This is an integration test that spawns a subprocess.
func TestProcessLock_CrossProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping cross-process test in short mode")
	}

	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "crosstest.lock")
	pidPath := filepath.Join(tmpDir, "crosstest.pid")

	// Create a simple Go program that acquires the lock and sleeps
	helperCode := `
package main

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

func main() {
	lockPath := os.Args[1]
	pidPath := os.Args[2]

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to create lock file:", err)
		os.Exit(1)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to acquire lock:", err)
		os.Exit(1)
	}

	os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
	fmt.Println("READY")

	// Sleep for a bit to hold the lock
	time.Sleep(5 * time.Second)
}
`
	// Write helper program
	helperDir := t.TempDir()
	helperFile := filepath.Join(helperDir, "helper.go")
	if err := os.WriteFile(helperFile, []byte(helperCode), 0644); err != nil {
		t.Fatalf("Failed to write helper: %v", err)
	}

	// Build helper
	helperBin := filepath.Join(helperDir, "helper")
	buildCmd := exec.Command("go", "build", "-o", helperBin, helperFile)
	if output, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build helper: %v\n%s", err, output)
	}

	// Start helper process
	helperProc := exec.Command(helperBin, lockPath, pidPath)
	helperProc.Stdout = os.Stdout
	helperProc.Stderr = os.Stderr
	if err := helperProc.Start(); err != nil {
		t.Fatalf("Failed to start helper: %v", err)
	}
	defer helperProc.Process.Kill()

	// Wait for helper to acquire lock
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(pidPath); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Read the helper's PID
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("Failed to read helper PID: %v", err)
	}
	helperPID, _ := strconv.Atoi(strings.TrimSpace(string(pidData)))

	// Now try to acquire the lock from this process
	lock := NewProcessLock(ProcessLockConfig{
		LockDir:  tmpDir,
		LockName: "crosstest",
	})

	err = lock.Acquire()
	if err == nil {
		lock.Release()
		t.Fatal("Should not be able to acquire lock held by another process")
	}

	if !strings.Contains(err.Error(), "another aleutian instance") {
		t.Errorf("Error should mention another instance, got: %v", err)
	}

	// Verify we can read the holder's PID
	reportedPID := lock.HolderPID()
	if reportedPID != helperPID {
		t.Errorf("HolderPID() = %d, want %d (helper process)", reportedPID, helperPID)
	}

	// Kill helper and verify we can acquire
	helperProc.Process.Kill()
	helperProc.Wait()

	// Lock file is released when process exits, but PID file remains
	// We should now be able to acquire
	time.Sleep(100 * time.Millisecond) // Brief delay for OS cleanup

	err = lock.Acquire()
	if err != nil {
		t.Fatalf("Should be able to acquire lock after holder exits: %v", err)
	}
	lock.Release()
}
