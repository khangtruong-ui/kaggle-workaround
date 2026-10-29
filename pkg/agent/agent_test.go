package agent

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
	"github.com/khangtruong-ui/kaggle-workaround/pkg/server"
)

func TestCollectMetadata(t *testing.T) {
	meta := CollectMetadata()
	if meta["os"] == "" {
		t.Error("expected non-empty OS metadata")
	}
	if meta["cpus"] == "" {
		t.Error("expected non-empty CPU metadata")
	}
	if meta["environment"] == "" {
		t.Error("expected non-empty environment metadata")
	}

	// Test TPU detection via environment
	t.Setenv("TPU_ACCELERATOR_TYPE", "v3-8")
	metaTPU := CollectMetadata()
	if !strings.Contains(metaTPU["gpu"], "TPU v3-8") {
		t.Errorf("expected TPU detection, got %v", metaTPU["gpu"])
	}
	if metaTPU["accelerator"] != "TPU" {
		t.Errorf("expected accelerator=TPU, got %v", metaTPU["accelerator"])
	}
}

func TestTerminalLifecycle(t *testing.T) {
	term, err := StartTerminal("", 24, 80)
	if err != nil {
		t.Fatalf("failed to start terminal: %v", err)
	}
	defer term.Close()

	if err := term.Resize(30, 100); err != nil {
		t.Fatalf("failed to resize terminal: %v", err)
	}

	// Write command
	cmd := "echo HELLO_AGENT_TERMINAL\n"
	if _, err := term.Write([]byte(cmd)); err != nil {
		t.Fatalf("failed to write to terminal: %v", err)
	}

	buf := make([]byte, 4096)
	found := false
	deadline := time.Now().Add(3 * time.Second)

	for time.Now().Before(deadline) {
		n, err := term.Read(buf)
		if n > 0 {
			out := string(buf[:n])
			if strings.Contains(out, "HELLO_AGENT_TERMINAL") {
				found = true
				break
			}
		}
		if err != nil {
			break
		}
	}

	if !found {
		t.Fatal("did not receive expected echo from terminal")
	}
}

func TestAgentServerIntegrationWithCtrlC(t *testing.T) {
	// 1. Start Relay Server
	srv := server.NewServer(server.ServerConfig{
		Addr:   "127.0.0.1:0",
		Secret: "secret-abc",
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	serverAddr := srv.Addr().String()

	// 2. Start Agent
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ag := NewAgent(Config{
		ServerAddr:        serverAddr,
		Secret:            "secret-abc",
		ID:                "test-agent-1",
		Name:              "Kaggle Agent",
		HeartbeatInterval: 1 * time.Second,
		ReconnectInterval: 500 * time.Millisecond,
		Persistent:        true,
	})
	defer ag.Stop()

	go func() {
		_ = ag.Run(ctx)
	}()

	// Wait for agent registration
	var agentInfo *server.AgentSession
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		agentInfo = srv.Registry().GetAgent("test-agent-1")
		if agentInfo != nil {
			break
		}
	}
	if agentInfo == nil {
		t.Fatal("agent failed to register with server")
	}

	// 3. Connect mock Controller
	ctrlConn, err := serverAddrDial(serverAddr)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer ctrlConn.Close()

	ctrlStream := protocol.NewFramedStream(ctrlConn)
	_ = ctrlStream.SendJSON(protocol.TypeHandshake, protocol.HandshakePayload{
		Role:  "controller",
		Token: "secret-abc",
	})
	_, err = ctrlStream.ReadPacket()
	if err != nil {
		t.Fatalf("failed to read handshake ack: %v", err)
	}

	// Attach to agent
	_ = ctrlStream.SendJSON(protocol.TypeAttach, protocol.AttachPayload{
		AgentID: "test-agent-1",
		Rows:    24,
		Cols:    80,
	})

	ackPkt, err := ctrlStream.ReadPacket()
	if err != nil {
		t.Fatalf("read attach ack failed: %v", err)
	}
	var attachAck protocol.AttachAckPayload
	_ = ackPkt.UnmarshalJSON(&attachAck)
	if !attachAck.Success {
		t.Fatalf("attach failed: %s", attachAck.Error)
	}

	// 4. Send command to agent via Data packet
	testMsg := "echo TEST_EXEC_OK\n"
	_ = ctrlStream.SendData([]byte(testMsg))

	foundEcho := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pkt, err := ctrlStream.ReadPacket()
		if err != nil {
			break
		}
		if pkt.Type == protocol.TypeData {
			if strings.Contains(string(pkt.Payload), "TEST_EXEC_OK") {
				foundEcho = true
				break
			}
		}
	}
	if !foundEcho {
		t.Fatal("did not receive command output from agent")
	}

	// 5. Test Ctrl+C (0x03) handling
	// Launch a sleep command that would block for 30s
	_ = ctrlStream.SendData([]byte("sleep 30\n"))
	time.Sleep(300 * time.Millisecond)

	// Send Ctrl+C
	_ = ctrlStream.SendData([]byte{0x03})

	// Now send a quick echo command to verify prompt is back immediately
	_ = ctrlStream.SendData([]byte("echo PROMPT_RECOVERED\n"))

	foundRecovered := false
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pkt, err := ctrlStream.ReadPacket()
		if err != nil {
			break
		}
		if pkt.Type == protocol.TypeData {
			if strings.Contains(string(pkt.Payload), "PROMPT_RECOVERED") {
				foundRecovered = true
				break
			}
		}
	}
	if !foundRecovered {
		t.Fatal("prompt did not recover after sending Ctrl+C (process was not interrupted)")
	}

	// 6. Test Detach and Re-attach (persistence)
	_ = ctrlStream.SendJSON(protocol.TypeDetach, protocol.DetachPayload{Reason: "test detach"})
	for {
		detachPkt, err := ctrlStream.ReadPacket()
		if err != nil {
			t.Fatalf("failed to read detach packet: %v", err)
		}
		if detachPkt.Type == protocol.TypeDetach {
			break
		}
	}

	// Re-attach
	_ = ctrlStream.SendJSON(protocol.TypeAttach, protocol.AttachPayload{
		AgentID: "test-agent-1",
		Rows:    30,
		Cols:    100,
	})
	var ackPkt2 *protocol.Packet
	for {
		ackPkt2, err = ctrlStream.ReadPacket()
		if err != nil {
			t.Fatalf("read second attach ack failed: %v", err)
		}
		if ackPkt2.Type == protocol.TypeAttachAck {
			break
		}
	}
	var attachAck2 protocol.AttachAckPayload
	_ = ackPkt2.UnmarshalJSON(&attachAck2)
	if !attachAck2.Success {
		t.Fatalf("second attach failed: %s", attachAck2.Error)
	}

	_ = ctrlStream.SendData([]byte("echo REATTACH_OK\n"))
	foundReattach := false
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pkt, err := ctrlStream.ReadPacket()
		if err != nil {
			break
		}
		if pkt.Type == protocol.TypeData {
			if strings.Contains(string(pkt.Payload), "REATTACH_OK") {
				foundReattach = true
				break
			}
		}
	}
	if !foundReattach {
		t.Fatal("did not receive output after reattaching")
	}
}

func serverAddrDial(addr string) (net.Conn, error) {
	return net.Dial("tcp", addr)
}
