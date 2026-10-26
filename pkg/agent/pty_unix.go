//go:build !windows

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

type unixTerminalSession struct {
	cmd  *exec.Cmd
	ptmx *os.File
	mu   sync.Mutex
}

// StartTerminal starts the user's shell inside a real pseudo-terminal (PTY).
func StartTerminal(shell string, rows, cols uint16) (TerminalSession, error) {
	if shell == "" {
		shell = os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
			if _, err := os.Stat(shell); err != nil {
				shell = "/bin/sh"
			}
		}
	}

	cmd := exec.Command(shell)
	// Propagate environment and ensure TERM is xterm-256color for nice terminal output
	cmd.Env = os.Environ()
	hasTerm := false
	for _, env := range cmd.Env {
		if len(env) >= 5 && env[:5] == "TERM=" {
			hasTerm = true
			break
		}
	}
	if !hasTerm {
		cmd.Env = append(cmd.Env, "TERM=xterm-256color")
	}

	size := &pty.Winsize{
		Rows: rows,
		Cols: cols,
	}
	if size.Rows == 0 {
		size.Rows = 24
	}
	if size.Cols == 0 {
		size.Cols = 80
	}

	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return nil, fmt.Errorf("pty.StartWithSize failed: %w", err)
	}

	return &unixTerminalSession{
		cmd:  cmd,
		ptmx: ptmx,
	}, nil
}

func (s *unixTerminalSession) Read(p []byte) (n int, err error) {
	return s.ptmx.Read(p)
}

func (s *unixTerminalSession) Write(p []byte) (n int, err error) {
	return s.ptmx.Write(p)
}

func (s *unixTerminalSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var ptmxErr error
	if s.ptmx != nil {
		ptmxErr = s.ptmx.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return ptmxErr
}

func (s *unixTerminalSession) Resize(rows, cols uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ptmx == nil {
		return nil
	}
	return pty.Setsize(s.ptmx, &pty.Winsize{
		Rows: rows,
		Cols: cols,
	})
}

func (s *unixTerminalSession) Wait() error {
	return s.cmd.Wait()
}
