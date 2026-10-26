//go:build windows

package agent

import (
	"io"
	"os"
	"os/exec"
	"sync"
)

type windowsTerminalSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	mu     sync.Mutex
}

// StartTerminal fallback for Windows environments using piped standard I/O.
func StartTerminal(shell string, rows, cols uint16) (TerminalSession, error) {
	if shell == "" {
		shell = os.Getenv("COMSPEC")
		if shell == "" {
			shell = "cmd.exe"
		}
	}

	cmd := exec.Command(shell)
	cmd.Env = os.Environ()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stderr = cmd.Stdout // merge stderr into stdout pipe

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, err
	}

	return &windowsTerminalSession{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
	}, nil
}

func (s *windowsTerminalSession) Read(p []byte) (n int, err error) {
	return s.stdout.Read(p)
}

func (s *windowsTerminalSession) Write(p []byte) (n int, err error) {
	return s.stdin.Write(p)
}

func (s *windowsTerminalSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = s.stdin.Close()
	_ = s.stdout.Close()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return nil
}

func (s *windowsTerminalSession) Resize(rows, cols uint16) error {
	// Not supported on standard Windows pipe without ConPTY
	return nil
}

func (s *windowsTerminalSession) Wait() error {
	return s.cmd.Wait()
}
