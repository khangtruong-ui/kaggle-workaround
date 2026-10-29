//go:build !windows

package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func setDetachedAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Creates a new session, detaching from caller's terminal
	}
}

func redirectStdFiles(f *os.File) {
	// Duplicate file descriptor to stdout and stderr
	_ = syscall.Dup2(int(f.Fd()), int(os.Stdout.Fd()))
	_ = syscall.Dup2(int(f.Fd()), int(os.Stderr.Fd()))
}

func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks for existence without delivering a signal
	err = process.Signal(syscall.Signal(0))
	if err != nil {
		return false
	}

	// On Linux, check if process is a zombie (waiting for parent to wait)
	statusBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err == nil {
		statusStr := string(statusBytes)
		if strings.Contains(statusStr, "State:\tZ") || strings.Contains(statusStr, "State: Z") {
			return false
		}
	}

	return true
}

func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	// Try graceful SIGTERM
	_ = proc.Signal(syscall.SIGTERM)

	// Wait up to 3 seconds for exit
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if !isProcessRunning(pid) {
			return nil
		}
	}

	// Force kill if still running
	_ = proc.Signal(syscall.SIGKILL)
	time.Sleep(100 * time.Millisecond)
	return nil
}
