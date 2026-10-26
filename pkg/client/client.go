package client

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
	"github.com/khangtruong-ui/kaggle-workaround/pkg/ui"
	"golang.org/x/term"
)

// Config configures Machine A's controller terminal.
type Config struct {
	ServerAddr        string
	Secret            string
	TargetAgent       string
	NonInteractive    bool
	HeartbeatInterval time.Duration
}

// Client represents Machine A controlling remote Machine C instances through Machine B.
type Client struct {
	config Config
	stream protocol.Stream
	conn   net.Conn
	mu     sync.Mutex
	quit   chan struct{}
}

// NewClient creates a new Client instance.
func NewClient(cfg Config) *Client {
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = 20 * time.Second
	}
	return &Client{
		config: cfg,
		quit:   make(chan struct{}),
	}
}

// Run connects to Machine B relay and runs the interactive control session.
func (c *Client) Run(ctx context.Context) error {
	conn, err := net.Dial("tcp", c.config.ServerAddr)
	if err != nil {
		return fmt.Errorf("connect to relay %s failed: %w", c.config.ServerAddr, err)
	}
	defer conn.Close()

	c.conn = conn
	c.stream = protocol.NewFramedStream(conn)

	// Perform Handshake
	hs := protocol.HandshakePayload{
		Role:    "controller",
		Token:   c.config.Secret,
		Version: "1.0.0",
	}

	if err := c.stream.SendJSON(protocol.TypeHandshake, hs); err != nil {
		return fmt.Errorf("handshake send failed: %w", err)
	}

	ackPkt, err := c.stream.ReadPacket()
	if err != nil {
		return fmt.Errorf("read handshake ack failed: %w", err)
	}

	var ack protocol.HandshakeAckPayload
	if err := ackPkt.UnmarshalJSON(&ack); err != nil {
		return fmt.Errorf("unmarshal handshake ack failed: %w", err)
	}

	if !ack.Success {
		return fmt.Errorf("relay authentication failed: %s", ack.Error)
	}

	// Direct attach if specified
	if c.config.TargetAgent != "" {
		return c.attach(ctx, c.config.TargetAgent, c.config.TargetAgent)
	}

	// Interactive Menu Loop
	reader := bufio.NewReader(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.quit:
			return nil
		default:
		}

		agents, err := c.QueryAgents()
		if err != nil {
			return fmt.Errorf("query agents: %w", err)
		}

		ui.PrintBanner(os.Stdout, c.config.ServerAddr)
		ui.RenderAgentTable(os.Stdout, agents)

		fmt.Printf("  %sEnter [#] to attach, [r] to refresh, [q] to exit:%s ", ui.Bold+ui.BrightCyan, ui.Reset)
		input, err := reader.ReadString('\n')
		if err != nil {
			return nil
		}
		input = strings.TrimSpace(input)

		if input == "q" || input == "exit" {
			fmt.Println("\n  Exiting.")
			return nil
		}

		if input == "r" || input == "" {
			// Refresh
			continue
		}

		// Try numeric selection
		idx, err := strconv.Atoi(input)
		if err == nil && idx >= 1 && idx <= len(agents) {
			target := agents[idx-1]
			err := c.attach(ctx, target.ID, target.Name)
			if err != nil {
				fmt.Printf("\n  %s[!] Session ended with error: %v%s\n\n", ui.Red+ui.Bold, err, ui.Reset)
			}
			continue
		}

		// Check by exact ID or name match
		found := false
		for _, a := range agents {
			if a.ID == input || a.Name == input {
				found = true
				err := c.attach(ctx, a.ID, a.Name)
				if err != nil {
					fmt.Printf("\n  %s[!] Session ended with error: %v%s\n\n", ui.Red+ui.Bold, err, ui.Reset)
				}
				break
			}
		}

		if !found {
			fmt.Printf("  %sInvalid option: '%s'%s\n\n", ui.Yellow, input, ui.Reset)
		}
	}
}

// QueryAgents requests the active list of Machine C agents from Machine B.
func (c *Client) QueryAgents() ([]protocol.AgentInfo, error) {
	if err := c.stream.SendJSON(protocol.TypeListAgents, map[string]string{}); err != nil {
		return nil, err
	}

	pkt, err := c.stream.ReadPacket()
	if err != nil {
		return nil, err
	}

	if pkt.Type != protocol.TypeAgentList {
		return nil, fmt.Errorf("unexpected packet type %v", pkt.Type)
	}

	var listPayload protocol.AgentListPayload
	if err := pkt.UnmarshalJSON(&listPayload); err != nil {
		return nil, err
	}

	return listPayload.Agents, nil
}

// attach switches Machine A's local terminal to raw mode and pipes I/O to Machine C.
func (c *Client) attach(ctx context.Context, agentID, agentName string) error {
	stdinFd := int(os.Stdin.Fd())
	stdoutFd := int(os.Stdout.Fd())
	isTTY := term.IsTerminal(stdinFd)

	rows, cols := uint16(24), uint16(80)
	if isTTY {
		w, h, err := term.GetSize(stdoutFd)
		if err == nil && w > 0 && h > 0 {
			cols = uint16(w)
			rows = uint16(h)
		}
	}

	// 1. Send Attach request
	err := c.stream.SendJSON(protocol.TypeAttach, protocol.AttachPayload{
		AgentID: agentID,
		Rows:    rows,
		Cols:    cols,
	})
	if err != nil {
		return fmt.Errorf("send attach request: %w", err)
	}

	// 2. Read Attach Ack
	ackPkt, err := c.stream.ReadPacket()
	if err != nil {
		return fmt.Errorf("read attach ack: %w", err)
	}

	var ack protocol.AttachAckPayload
	if err := ackPkt.UnmarshalJSON(&ack); err != nil {
		return fmt.Errorf("unmarshal attach ack: %w", err)
	}

	if !ack.Success {
		return fmt.Errorf("attach rejected: %s", ack.Error)
	}

	ui.PrintAttachNotice(os.Stdout, agentName, agentID)

	// Switch terminal to RAW mode
	var oldState *term.State
	if isTTY {
		oldState, err = term.MakeRaw(stdinFd)
		if err != nil {
			return fmt.Errorf("set raw terminal: %w", err)
		}
		defer func() {
			_ = term.Restore(stdinFd, oldState)
		}()
	}

	// Setup SIGWINCH window resize listener
	resizeDone := make(chan struct{})
	defer close(resizeDone)
	c.watchWindowResize(stdoutFd, resizeDone)

	// Session control channels
	sessionDone := make(chan struct{})
	var once sync.Once
	closeSession := func() {
		once.Do(func() {
			close(sessionDone)
		})
	}

	// Goroutine 1: Read incoming terminal packets from Machine B -> Machine C stdout
	go func() {
		defer closeSession()
		for {
			select {
			case <-sessionDone:
				return
			default:
			}

			pkt, err := c.stream.ReadPacket()
			if err != nil {
				return
			}

			switch pkt.Type {
			case protocol.TypeData:
				_, _ = os.Stdout.Write(pkt.Payload)

			case protocol.TypeDetach:
				var d protocol.DetachPayload
				_ = pkt.UnmarshalJSON(&d)
				return

			case protocol.TypeError:
				var e protocol.ErrorPayload
				_ = pkt.UnmarshalJSON(&e)
				_, _ = fmt.Fprintf(os.Stderr, "\r\n[Error from remote: %s]\r\n", e.Message)
				return
			}
		}
	}()

	// Goroutine 2: Read stdin keystrokes from local user -> Machine B -> Machine C
	buf := make([]byte, 1024)
	for {
		select {
		case <-sessionDone:
			goto Finished
		default:
		}

		n, err := os.Stdin.Read(buf)
		if n > 0 {
			// Check for detach hotkey: Ctrl+] (0x1D)
			if n == 1 && buf[0] == 0x1D {
				_ = c.stream.SendJSON(protocol.TypeDetach, protocol.DetachPayload{
					Reason:  "controller hotkey detach",
					AgentID: agentID,
				})
				goto Finished
			}

			// Forward raw input (including Ctrl+C 0x03) straight to remote PTY
			data := make([]byte, n)
			copy(data, buf[:n])
			if err := c.stream.SendData(data); err != nil {
				goto Finished
			}
		}

		if err != nil {
			if errors.Is(err, io.EOF) {
				goto Finished
			}
			break
		}
	}

Finished:
	if isTTY && oldState != nil {
		_ = term.Restore(stdinFd, oldState)
	}

	ui.PrintDetachNotice(os.Stdout, agentName)
	return nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
