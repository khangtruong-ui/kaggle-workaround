package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestStatusNonExistent(t *testing.T) {
	running, pid, err := Status("/tmp/nonexistent-test-pid.pid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if running {
		t.Errorf("expected not running, got running (pid %d)", pid)
	}
}

func TestStatusCurrentProcess(t *testing.T) {
	tempPid := filepath.Join(t.TempDir(), "current.pid")
	currentPid := os.Getpid()
	if err := os.WriteFile(tempPid, []byte(strconv.Itoa(currentPid)+"\n"), 0644); err != nil {
		t.Fatalf("failed to write pid file: %v", err)
	}

	running, pid, err := Status(tempPid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !running {
		t.Error("expected current process to be running")
	}
	if pid != currentPid {
		t.Errorf("expected pid %d, got %d", currentPid, pid)
	}
}

func TestStopProcess(t *testing.T) {
	tempDir := t.TempDir()
	tempPid := filepath.Join(tempDir, "sleep.pid")

	// Start a dummy sleep process
	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start sleep: %v", err)
	}
	pid := cmd.Process.Pid

	if err := os.WriteFile(tempPid, []byte(strconv.Itoa(pid)+"\n"), 0644); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("failed to write pid file: %v", err)
	}

	// Verify running
	running, _, err := Status(tempPid)
	if err != nil || !running {
		_ = cmd.Process.Kill()
		t.Fatalf("expected sleep to be running: running=%v, err=%v", running, err)
	}

	// Stop daemon
	if err := Stop(tempPid, "TestSleep"); err != nil {
		_ = cmd.Process.Kill()
		t.Fatalf("Stop failed: %v", err)
	}

	_ = cmd.Wait()

	time.Sleep(100 * time.Millisecond)

	// Verify process stopped
	if isProcessRunning(pid) {
		t.Errorf("expected process %d to be stopped", pid)
		_ = cmd.Process.Kill()
	}

	// PID file should be removed
	if _, err := os.Stat(tempPid); !os.IsNotExist(err) {
		t.Errorf("expected pid file to be removed, got err: %v", err)
	}
}
