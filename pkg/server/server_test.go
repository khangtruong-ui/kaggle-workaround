package server

import (
	"net"
	"testing"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
)

func TestRegistryOperations(t *testing.T) {
	reg := NewRegistry()

	// 1. Register agents
	a1Conn, a1Peer := net.Pipe()
	defer a1Conn.Close()
	defer a1Peer.Close()

	agent1 := &AgentSession{
		ID:          "agent-1",
		Name:        "kaggle-t4",
		Hostname:    "worker-1",
		RemoteAddr:  "10.0.0.1:1234",
		ConnectedAt: time.Now(),
		Stream:      protocol.NewFramedStream(a1Conn),
		Metadata:    map[string]string{"gpu": "T4"},
	}
	reg.RegisterAgent(agent1)

	list := reg.ListAgents()
	if len(list) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(list))
	}
	if list[0].Name != "kaggle-t4" || list[0].Metadata["gpu"] != "T4" {
		t.Errorf("agent metadata mismatch: %+v", list[0])
	}

	// 2. Attach controller
	c1Conn, c1Peer := net.Pipe()
	defer c1Conn.Close()
	defer c1Peer.Close()

	ctrl1 := &ControllerSession{
		ID:         "ctrl-1",
		RemoteAddr: "192.168.1.5:5678",
		Stream:     protocol.NewFramedStream(c1Conn),
	}
	reg.RegisterController(ctrl1)

	// Consume attach packet on agent peer goroutine
	go func() {
		peerStream := protocol.NewFramedStream(a1Peer)
		pkt, err := peerStream.ReadPacket()
		if err != nil {
			return
		}
		if pkt.Type != protocol.TypeAttach {
			t.Errorf("expected TypeAttach, got %v", pkt.Type)
		}
	}()

	err := reg.Attach(ctrl1, "agent-1", 24, 80)
	if err != nil {
		t.Fatalf("attach failed: %v", err)
	}

	// Check agent is marked attached
	list = reg.ListAgents()
	if !list[0].Attached {
		t.Error("expected agent to be marked attached")
	}

	// Attempt second controller attaching to same agent should fail
	c2Conn, _ := net.Pipe()
	defer c2Conn.Close()
	ctrl2 := &ControllerSession{
		ID:     "ctrl-2",
		Stream: protocol.NewFramedStream(c2Conn),
	}
	reg.RegisterController(ctrl2)

	err = reg.Attach(ctrl2, "agent-1", 24, 80)
	if err == nil {
		t.Fatal("expected error attaching to occupied agent, got nil")
	}

	// Detach ctrl1
	go func() {
		peerStream := protocol.NewFramedStream(a1Peer)
		_, _ = peerStream.ReadPacket() // Read TypeDetach
	}()

	err = reg.Detach(ctrl1, "user disconnect")
	if err != nil {
		t.Fatalf("detach failed: %v", err)
	}

	list = reg.ListAgents()
	if list[0].Attached {
		t.Error("expected agent to be idle after detach")
	}
}

func TestServerHandshakeAndAuth(t *testing.T) {
	srv := NewServer(ServerConfig{
		Addr:   "127.0.0.1:0",
		Secret: "correct-horse-battery-staple",
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	serverAddr := srv.Addr().String()

	// 1. Test invalid secret
	connFail, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer connFail.Close()

	streamFail := protocol.NewFramedStream(connFail)
	err = streamFail.SendJSON(protocol.TypeHandshake, protocol.HandshakePayload{
		Role:  "agent",
		Token: "wrong-secret",
	})
	if err != nil {
		t.Fatalf("send handshake failed: %v", err)
	}

	ackPkt, err := streamFail.ReadPacket()
	if err != nil {
		t.Fatalf("read ack failed: %v", err)
	}
	var ackFail protocol.HandshakeAckPayload
	if err := ackPkt.UnmarshalJSON(&ackFail); err != nil {
		t.Fatalf("unmarshal ack failed: %v", err)
	}
	if ackFail.Success {
		t.Fatal("expected authentication failure for wrong secret")
	}

	// 2. Test valid secret
	connSuccess, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer connSuccess.Close()

	streamSuccess := protocol.NewFramedStream(connSuccess)
	err = streamSuccess.SendJSON(protocol.TypeHandshake, protocol.HandshakePayload{
		Role:     "agent",
		Token:    "correct-horse-battery-staple",
		ID:       "kaggle-gpu-1",
		Name:     "Kaggle GPU 1",
		Hostname: "kaggle-worker",
	})
	if err != nil {
		t.Fatalf("send handshake failed: %v", err)
	}

	ackSuccessPkt, err := streamSuccess.ReadPacket()
	if err != nil {
		t.Fatalf("read ack failed: %v", err)
	}
	var ackSuccess protocol.HandshakeAckPayload
	if err := ackSuccessPkt.UnmarshalJSON(&ackSuccess); err != nil {
		t.Fatalf("unmarshal ack failed: %v", err)
	}
	if !ackSuccess.Success {
		t.Fatalf("expected successful handshake, got error: %s", ackSuccess.Error)
	}
	if ackSuccess.AssignedID != "kaggle-gpu-1" {
		t.Errorf("expected assigned ID 'kaggle-gpu-1', got '%s'", ackSuccess.AssignedID)
	}

	// Verify agent registered in server
	agent := srv.Registry().GetAgent("kaggle-gpu-1")
	if agent == nil {
		t.Fatal("agent was not registered in server")
	}
	if agent.Name != "Kaggle GPU 1" {
		t.Errorf("expected agent name 'Kaggle GPU 1', got '%s'", agent.Name)
	}
}

func TestServerDataAndControlRelay(t *testing.T) {
	srv := NewServer(ServerConfig{
		Addr:   "127.0.0.1:0",
		Secret: "test-secret",
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	serverAddr := srv.Addr().String()

	// Connect Agent
	agentConn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("agent dial failed: %v", err)
	}
	defer agentConn.Close()
	agentStream := protocol.NewFramedStream(agentConn)

	_ = agentStream.SendJSON(protocol.TypeHandshake, protocol.HandshakePayload{
		Role:     "agent",
		Token:    "test-secret",
		ID:       "target-agent",
		Hostname: "kaggle-worker",
	})
	_, _ = agentStream.ReadPacket() // Read HandshakeAck

	// Connect Controller
	ctrlConn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("ctrl dial failed: %v", err)
	}
	defer ctrlConn.Close()
	ctrlStream := protocol.NewFramedStream(ctrlConn)

	_ = ctrlStream.SendJSON(protocol.TypeHandshake, protocol.HandshakePayload{
		Role:  "controller",
		Token: "test-secret",
	})
	_, _ = ctrlStream.ReadPacket() // Read HandshakeAck

	// Controller queries agent list
	_ = ctrlStream.SendJSON(protocol.TypeListAgents, map[string]string{})
	listPkt, err := ctrlStream.ReadPacket()
	if err != nil {
		t.Fatalf("read agent list failed: %v", err)
	}
	var agentList protocol.AgentListPayload
	_ = listPkt.UnmarshalJSON(&agentList)
	if len(agentList.Agents) != 1 || agentList.Agents[0].ID != "target-agent" {
		t.Fatalf("unexpected agent list: %+v", agentList)
	}

	// Controller attaches to Agent
	_ = ctrlStream.SendJSON(protocol.TypeAttach, protocol.AttachPayload{
		AgentID: "target-agent",
		Rows:    40,
		Cols:    100,
	})

	// Agent should receive TypeAttach
	agentPkt, err := agentStream.ReadPacket()
	if err != nil {
		t.Fatalf("agent read attach failed: %v", err)
	}
	if agentPkt.Type != protocol.TypeAttach {
		t.Fatalf("expected TypeAttach, got %v", agentPkt.Type)
	}

	// Controller should receive TypeAttachAck
	ctrlPkt, err := ctrlStream.ReadPacket()
	if err != nil {
		t.Fatalf("ctrl read attach ack failed: %v", err)
	}
	var attachAck protocol.AttachAckPayload
	_ = ctrlPkt.UnmarshalJSON(&attachAck)
	if !attachAck.Success {
		t.Fatalf("attach failed: %s", attachAck.Error)
	}

	// Test Ctrl+C byte (0x03) forwarded from Controller -> Agent
	ctrlC := []byte{0x03}
	if err := ctrlStream.SendData(ctrlC); err != nil {
		t.Fatalf("send Ctrl+C failed: %v", err)
	}

	receivedPkt, err := agentStream.ReadPacket()
	if err != nil {
		t.Fatalf("agent read Ctrl+C failed: %v", err)
	}
	if receivedPkt.Type != protocol.TypeData || len(receivedPkt.Payload) != 1 || receivedPkt.Payload[0] != 0x03 {
		t.Fatalf("unexpected packet received on agent: %+v", receivedPkt)
	}

	// Test output forwarded from Agent -> Controller
	output := []byte("Process interrupted\n")
	if err := agentStream.SendData(output); err != nil {
		t.Fatalf("send output failed: %v", err)
	}

	receivedOutput, err := ctrlStream.ReadPacket()
	if err != nil {
		t.Fatalf("ctrl read output failed: %v", err)
	}
	if string(receivedOutput.Payload) != "Process interrupted\n" {
		t.Fatalf("expected output 'Process interrupted\\n', got '%s'", string(receivedOutput.Payload))
	}
}
