//go:build windows

package daemon

import (
	"os"
	"os/exec"
	"syscall"
)

func setDetachedAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

func redirectStdFiles(f *os.File) {
	// Not supported via dup2 on Windows
}

func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = process
	return true
}

func killProcess(pid int) error {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	return proc.Kill()
}
