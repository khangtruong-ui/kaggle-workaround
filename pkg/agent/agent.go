package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
)

// Config configures Machine C's agent connection and behavior.
type Config struct {
	ServerAddr        string
	Secret            string
	ID                string
	Name              string
	Shell             string
	Persistent        bool
	AutoReconnect     bool
	HeartbeatInterval time.Duration
	ReconnectInterval time.Duration
}

// Agent represents Machine C running on Kaggle or a remote instance.
type Agent struct {
	config   Config
	term     TerminalSession
	stream   protocol.Stream
	attached bool
	termMu   sync.Mutex
	quit     chan struct{}
	stopOnce sync.Once
}

// NewAgent creates a new Agent instance.
func NewAgent(cfg Config) *Agent {
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 20 * time.Second
	}
	if cfg.ReconnectInterval == 0 {
		cfg.ReconnectInterval = 5 * time.Second
	}
	// By default, sessions are persistent so user training runs survive detachment/disconnects
	if !cfg.Persistent {
		cfg.Persistent = true
	}
	if cfg.Name == "" {
		host, _ := os.Hostname()
		if host != "" {
			cfg.Name = host
		} else {
			cfg.Name = "kaggle-agent"
		}
	}

	return &Agent{
		config: cfg,
		quit:   make(chan struct{}),
	}
}

// Run connects to Machine B relay and serves terminal requests until context is cancelled or Stop called.
func (a *Agent) Run(ctx context.Context) error {
	log.Printf("[Agent] Starting agent '%s' targeting relay at %s", a.config.Name, a.config.ServerAddr)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.quit:
			return nil
		default:
		}

		err := a.connectAndServe(ctx)
		if err != nil {
			log.Printf("[Agent] Connection error: %v", err)
		}

		if !a.config.AutoReconnect {
			return err
		}

		log.Printf("[Agent] Reconnecting to relay in %v...", a.config.ReconnectInterval)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-a.quit:
			return nil
		case <-time.After(a.config.ReconnectInterval):
		}
	}
}

// Stop cleanly terminates the agent and closes active sessions.
func (a *Agent) Stop() {
	a.stopOnce.Do(func() {
		close(a.quit)
		a.termMu.Lock()
		if a.stream != nil {
			_ = a.stream.Close()
		}
		if a.term != nil {
			_ = a.term.Close()
		}
		a.termMu.Unlock()
	})
}

func (a *Agent) connectAndServe(ctx context.Context) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", a.config.ServerAddr)
	if err != nil {
		return fmt.Errorf("dial relay %s: %w", a.config.ServerAddr, err)
	}
	defer conn.Close()

	stream := protocol.NewFramedStream(conn)
	a.termMu.Lock()
	a.stream = stream
	a.termMu.Unlock()

	hostname, _ := os.Hostname()
	meta := CollectMetadata()

	// 1. Send Handshake
	hs := protocol.HandshakePayload{
		Role:     "agent",
		Token:    a.config.Secret,
		ID:       a.config.ID,
		Name:     a.config.Name,
		Hostname: hostname,
		Metadata: meta,
		Version:  "1.0.0",
	}

	if err := stream.SendJSON(protocol.TypeHandshake, hs); err != nil {
		return fmt.Errorf("send handshake: %w", err)
	}

	// 2. Read HandshakeAck
	ackPkt, err := stream.ReadPacket()
	if err != nil {
		return fmt.Errorf("read handshake ack: %w", err)
	}

	var ack protocol.HandshakeAckPayload
	if err := ackPkt.UnmarshalJSON(&ack); err != nil {
		return fmt.Errorf("unmarshal handshake ack: %w", err)
	}

	if !ack.Success {
		return fmt.Errorf("handshake rejected by relay: %s", ack.Error)
	}

	a.config.ID = ack.AssignedID
	log.Printf("[Agent] Connected to relay! Assigned ID: %s (Name: %s)", a.config.ID, a.config.Name)

	// Heartbeat loop
	hbDone := make(chan struct{})
	defer close(hbDone)
	go a.heartbeatLoop(stream, hbDone)

	// Main packet reader loop
	for {
		pkt, err := stream.ReadPacket()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				log.Printf("[Agent] Relay closed connection")
			} else {
				log.Printf("[Agent] Read error from relay: %v", err)
			}
			return err
		}

		if err := a.handlePacket(stream, pkt); err != nil {
			log.Printf("[Agent] Error handling packet %v: %v", pkt.Type, err)
		}
	}
}

func (a *Agent) heartbeatLoop(stream protocol.Stream, done chan struct{}) {
	ticker := time.NewTicker(a.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-a.quit:
			return
		case <-ticker.C:
			if err := stream.WritePacket(protocol.NewPacket(protocol.TypeHeartbeat, nil)); err != nil {
				return
			}
		}
	}
}

func (a *Agent) handlePacket(stream protocol.Stream, pkt *protocol.Packet) error {
	switch pkt.Type {
	case protocol.TypeHeartbeatAck:
		// Heartbeat acknowledged
		return nil

	case protocol.TypeAttach:
		var attach protocol.AttachPayload
		if err := pkt.UnmarshalJSON(&attach); err != nil {
			return fmt.Errorf("unmarshal attach: %w", err)
		}
		return a.onAttach(stream, attach.Rows, attach.Cols)

	case protocol.TypeResize:
		var resize protocol.ResizePayload
		if err := pkt.UnmarshalJSON(&resize); err != nil {
			return fmt.Errorf("unmarshal resize: %w", err)
		}
		a.termMu.Lock()
		defer a.termMu.Unlock()
		if a.term != nil {
			return a.term.Resize(resize.Rows, resize.Cols)
		}
		return nil

	case protocol.TypeData:
		// Forward incoming bytes (e.g. keystrokes, Ctrl+C 0x03) into terminal PTY
		a.termMu.Lock()
		t := a.term
		a.termMu.Unlock()

		if t != nil {
			_, err := t.Write(pkt.Payload)
			return err
		}
		return nil

	case protocol.TypeDetach:
		a.termMu.Lock()
		a.attached = false
		if !a.config.Persistent && a.term != nil {
			_ = a.term.Close()
			a.term = nil
		}
		a.termMu.Unlock()
		log.Printf("[Agent] Controller detached (persistent: %v)", a.config.Persistent)
		return nil

	default:
		return nil
	}
}

func (a *Agent) onAttach(stream protocol.Stream, rows, cols uint16) error {
	a.termMu.Lock()
	defer a.termMu.Unlock()

	a.attached = true

	if a.term != nil {
		// Terminal is already running (re-attachment). Just resize it to controller's dimensions!
		_ = a.term.Resize(rows, cols)
		log.Printf("[Agent] Controller re-attached to existing persistent terminal (size: %dx%d)", cols, rows)
		return nil
	}

	// Spawn new terminal process
	term, err := StartTerminal(a.config.Shell, rows, cols)
	if err != nil {
		log.Printf("[Agent] Failed to start terminal: %v", err)
		_ = stream.SendJSON(protocol.TypeError, protocol.ErrorPayload{
			Code:    "TERMINAL_START_FAILED",
			Message: err.Error(),
		})
		return err
	}

	a.term = term
	log.Printf("[Agent] Started new interactive terminal session (size: %dx%d)", cols, rows)

	// Pump stdout/stderr from PTY master back to Machine B relay -> Controller
	go func(t TerminalSession, s protocol.Stream) {
		buf := make([]byte, 16*1024)
		for {
			n, err := t.Read(buf)
			if n > 0 {
				a.termMu.Lock()
				isAttached := a.attached
				a.termMu.Unlock()

				if isAttached {
					chunk := make([]byte, n)
					copy(chunk, buf[:n])
					if writeErr := s.SendData(chunk); writeErr != nil {
						// Stream disconnected
						break
					}
				}
			}

			if err != nil {
				// Terminal process exited
				log.Printf("[Agent] Terminal process exited: %v", err)
				a.termMu.Lock()
				a.term = nil
				a.attached = false
				a.termMu.Unlock()

				_ = s.SendJSON(protocol.TypeDetach, protocol.DetachPayload{
					Reason: "terminal process exited",
				})
				break
			}
		}
	}(term, stream)

	return nil
}
