package daemon

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// EnvWorker identifies a process spawned as a detached background worker.
const EnvWorker = "_KAGGLE_WORKAROUND_DAEMON"

// Options configures daemon execution.
type Options struct {
	LogFile     string
	PidFile     string
	ServiceName string
}

// Daemonize checks if the current process is already the background child.
// If not, it spawns a detached child process, writes the PID file, and returns isParent=true.
// If it is the child, it redirects logs and returns isParent=false.
func Daemonize(opts Options) (isParent bool, err error) {
	if os.Getenv(EnvWorker) == "1" {
		// We are the child background worker!
		if opts.LogFile != "" {
			_ = os.MkdirAll(filepath.Dir(opts.LogFile), 0755)
			f, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
			if err == nil {
				log.SetOutput(f)
				// Redirect os.Stdout and os.Stderr
				redirectStdFiles(f)
			}
		}
		return false, nil
	}

	// We are the parent process. Check if already running.
	if opts.PidFile != "" {
		if running, pid, _ := Status(opts.PidFile); running {
			return true, fmt.Errorf("%s is already running in background (PID %d)", opts.ServiceName, pid)
		}
	}

	// Ensure log directory exists
	if opts.LogFile != "" {
		_ = os.MkdirAll(filepath.Dir(opts.LogFile), 0755)
	}

	// Prepare child command
	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Env = append(os.Environ(), EnvWorker+"=1")

	// Set OS-specific detached process attributes
	setDetachedAttrs(cmd)

	// Open or create log file for the child
	var logWriter io.Writer = io.Discard
	if opts.LogFile != "" {
		f, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			cmd.Stdout = f
			cmd.Stderr = f
			logWriter = f
		}
	}

	if err := cmd.Start(); err != nil {
		return true, fmt.Errorf("failed to start background daemon: %w", err)
	}

	pid := cmd.Process.Pid

	// Write PID file
	if opts.PidFile != "" {
		_ = os.MkdirAll(filepath.Dir(opts.PidFile), 0755)
		_ = os.WriteFile(opts.PidFile, []byte(strconv.Itoa(pid)+"\n"), 0644)
	}

	// Give the child a brief moment to verify it didn't immediately crash
	time.Sleep(100 * time.Millisecond)
	if !isProcessRunning(pid) {
		return true, fmt.Errorf("daemon failed to start (exited immediately). Check logs: %s", opts.LogFile)
	}

	fmt.Printf("[✓] %s started in background successfully!\n", opts.ServiceName)
	fmt.Printf("    PID:      %d\n", pid)
	if opts.LogFile != "" {
		fmt.Printf("    Log file: %s\n", opts.LogFile)
	}
	if opts.PidFile != "" {
		fmt.Printf("    PID file: %s\n", opts.PidFile)
	}

	_ = logWriter

	return true, nil
}

// Status checks if the background service is running.
func Status(pidFile string) (running bool, pid int, err error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false, 0, nil
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err = strconv.Atoi(pidStr)
	if err != nil {
		return false, 0, fmt.Errorf("corrupt pid file: %w", err)
	}

	if isProcessRunning(pid) {
		return true, pid, nil
	}

	// Stale pid file
	_ = os.Remove(pidFile)
	return false, pid, nil
}

// Stop terminates the background daemon process identified by pidFile.
func Stop(pidFile, serviceName string) error {
	running, pid, err := Status(pidFile)
	if err != nil {
		return err
	}
	if !running {
		fmt.Printf("[i] %s is not currently running (no active process).\n", serviceName)
		_ = os.Remove(pidFile)
		return nil
	}

	fmt.Printf("[*] Stopping %s (PID %d)...\n", serviceName, pid)
	if err := killProcess(pid); err != nil {
		return fmt.Errorf("failed to stop process %d: %w", pid, err)
	}

	_ = os.Remove(pidFile)
	fmt.Printf("[✓] %s stopped.\n", serviceName)
	return nil
}
