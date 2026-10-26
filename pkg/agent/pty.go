package agent

import "io"

// TerminalSession defines the interface for an interactive terminal process.
type TerminalSession interface {
	io.ReadWriteCloser
	Resize(rows, cols uint16) error
	Wait() error
}
