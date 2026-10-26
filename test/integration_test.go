package test

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/agent"
	"github.com/khangtruong-ui/kaggle-workaround/pkg/client"
	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
	"github.com/khangtruong-ui/kaggle-workaround/pkg/server"
)

// TestThreeClientsAndMultipleControllers verifies the exact user topology:
// Machine A (Controller 1 & Controller 2) -> Machine B (Single Port Relay) <- Machine C (3 Kaggle Agents)
func TestThreeClientsAndMultipleControllers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const secret = "kaggle-super-secret-key"

	// Step 1: Start Machine B Relay Server (Google Cloud VM with 1 open port)
	srv := server.NewServer(server.ServerConfig{
		Addr:   "127.0.0.1:0", // Ephemeral port
		Secret: secret,
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start relay server: %v", err)
	}
	defer srv.Stop()

	relayAddr := srv.Addr().String()

	// Step 2: Spin up 3 Machine C clients (Kaggle instances connecting back to Machine B)
	agentNames := []string{"kaggle-t4-node1", "kaggle-p100-node2", "kaggle-cpu-node3"}
	agents := make([]*agent.Agent, len(agentNames))

	for i, name := range agentNames {
		ag := agent.NewAgent(agent.Config{
			ServerAddr:        relayAddr,
			Secret:            secret,
			ID:                name,
			Name:              name,
			Persistent:        true,
			AutoReconnect:     true,
			HeartbeatInterval: 1 * time.Second,
			ReconnectInterval: 500 * time.Millisecond,
		})
		agents[i] = ag
		go func(a *agent.Agent) {
			_ = a.Run(ctx)
		}(ag)
	}
	defer func() {
		for _, a := range agents {
			a.Stop()
		}
	}()

	// Wait for all 3 agents to register on Machine B
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		list := srv.Registry().ListAgents()
		if len(list) == 3 {
			break
		}
	}

	list := srv.Registry().ListAgents()
	if len(list) != 3 {
		t.Fatalf("expected 3 agents registered on relay, got %d", len(list))
	}

	// Step 3: Connect Machine A (Controller 1)
	c1Conn, err := net.Dial("tcp", relayAddr)
	if err != nil {
		t.Fatalf("controller 1 dial failed: %v", err)
	}
	defer c1Conn.Close()
	c1Stream := protocol.NewFramedStream(c1Conn)

	// Controller 1 Handshake
	_ = c1Stream.SendJSON(protocol.TypeHandshake, protocol.HandshakePayload{
		Role:  "controller",
		Token: secret,
	})
	ackPkt, err := c1Stream.ReadPacket()
	if err != nil {
		t.Fatalf("controller 1 read handshake ack failed: %v", err)
	}
	var ack protocol.HandshakeAckPayload
	_ = ackPkt.UnmarshalJSON(&ack)
	if !ack.Success {
		t.Fatalf("controller 1 handshake rejected: %s", ack.Error)
	}

	// Controller 1 queries available machines
	_ = c1Stream.SendJSON(protocol.TypeListAgents, map[string]string{})
	listPkt, err := c1Stream.ReadPacket()
	if err != nil {
		t.Fatalf("read agent list failed: %v", err)
	}
	var agentList protocol.AgentListPayload
	_ = listPkt.UnmarshalJSON(&agentList)
	if len(agentList.Agents) != 3 {
		t.Fatalf("expected controller 1 to see 3 agents, got %d", len(agentList.Agents))
	}

	// Controller 1 attaches to Client 1 (kaggle-t4-node1)
	_ = c1Stream.SendJSON(protocol.TypeAttach, protocol.AttachPayload{
		AgentID: "kaggle-t4-node1",
		Rows:    24,
		Cols:    80,
	})
	c1AttachAck, err := c1Stream.ReadPacket()
	if err != nil {
		t.Fatalf("c1 attach ack failed: %v", err)
	}
	var c1Ack protocol.AttachAckPayload
	_ = c1AttachAck.UnmarshalJSON(&c1Ack)
	if !c1Ack.Success {
		t.Fatalf("c1 attach failed: %s", c1Ack.Error)
	}

	// Test command execution on Client 1
	_ = c1Stream.SendData([]byte("echo HELLO_FROM_CLIENT_1\n"))
	c1Output := readUntil(c1Stream, "HELLO_FROM_CLIENT_1", 4*time.Second)
	if !c1Output {
		t.Fatal("did not receive output from Client 1")
	}

	// Test Ctrl+C on Client 1
	_ = c1Stream.SendData([]byte("sleep 20\n"))
	time.Sleep(200 * time.Millisecond)
	_ = c1Stream.SendData([]byte{0x03}) // Ctrl+C
	_ = c1Stream.SendData([]byte("echo C1_PROMPT_INTACT\n"))
	if !readUntil(c1Stream, "C1_PROMPT_INTACT", 3*time.Second) {
		t.Fatal("Client 1 prompt not recovered after Ctrl+C")
	}

	// Step 4: Connect Machine A' (Controller 2) concurrently
	c2Conn, err := net.Dial("tcp", relayAddr)
	if err != nil {
		t.Fatalf("controller 2 dial failed: %v", err)
	}
	defer c2Conn.Close()
	c2Stream := protocol.NewFramedStream(c2Conn)

	_ = c2Stream.SendJSON(protocol.TypeHandshake, protocol.HandshakePayload{
		Role:  "controller",
		Token: secret,
	})
	_, _ = c2Stream.ReadPacket() // Read HandshakeAck

	// Controller 2 queries agent list - should see Client 1 is ATTACHED, Client 2 & 3 are IDLE
	_ = c2Stream.SendJSON(protocol.TypeListAgents, map[string]string{})
	c2ListPkt, err := c2Stream.ReadPacket()
	if err != nil {
		t.Fatalf("c2 read list failed: %v", err)
	}
	var c2List protocol.AgentListPayload
	_ = c2ListPkt.UnmarshalJSON(&c2List)

	for _, a := range c2List.Agents {
		if a.ID == "kaggle-t4-node1" && !a.Attached {
			t.Error("expected kaggle-t4-node1 to be marked attached")
		}
		if a.ID == "kaggle-p100-node2" && a.Attached {
			t.Error("expected kaggle-p100-node2 to be idle")
		}
	}

	// Controller 2 attaches to Client 2 (kaggle-p100-node2) concurrently!
	_ = c2Stream.SendJSON(protocol.TypeAttach, protocol.AttachPayload{
		AgentID: "kaggle-p100-node2",
		Rows:    30,
		Cols:    100,
	})
	c2AttachPkt, _ := c2Stream.ReadPacket()
	var c2Ack protocol.AttachAckPayload
	_ = c2AttachPkt.UnmarshalJSON(&c2Ack)
	if !c2Ack.Success {
		t.Fatalf("c2 attach failed: %s", c2Ack.Error)
	}

	// Step 5: Test concurrent execution across Controller 1 -> Client 1 and Controller 2 -> Client 2
	var wg sync.WaitGroup
	wg.Add(2)

	var c1ConcurrentOk, c2ConcurrentOk bool

	go func() {
		defer wg.Done()
		_ = c1Stream.SendData([]byte("echo CONCURRENT_C1\n"))
		c1ConcurrentOk = readUntil(c1Stream, "CONCURRENT_C1", 4*time.Second)
	}()

	go func() {
		defer wg.Done()
		_ = c2Stream.SendData([]byte("echo CONCURRENT_C2\n"))
		c2ConcurrentOk = readUntil(c2Stream, "CONCURRENT_C2", 4*time.Second)
	}()

	wg.Wait()

	if !c1ConcurrentOk || !c2ConcurrentOk {
		t.Fatalf("concurrent execution failure: c1=%v, c2=%v", c1ConcurrentOk, c2ConcurrentOk)
	}

	// Step 6: Controller 1 switches from Client 1 to Client 3
	_ = c1Stream.SendJSON(protocol.TypeDetach, protocol.DetachPayload{Reason: "switching to client 3"})
	_, _ = readNextNonDataPacket(c1Stream, 2*time.Second) // Read Detach ack

	// Attach to Client 3
	_ = c1Stream.SendJSON(protocol.TypeAttach, protocol.AttachPayload{
		AgentID: "kaggle-cpu-node3",
		Rows:    24,
		Cols:    80,
	})
	// Drain any trailing TypeData and wait for AttachAck
	c1SwitchPkt, err := readNextNonDataPacket(c1Stream, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to read attach ack for client 3: %v", err)
	}
	var c1SwitchAck protocol.AttachAckPayload
	_ = c1SwitchPkt.UnmarshalJSON(&c1SwitchAck)
	if !c1SwitchAck.Success {
		t.Fatalf("switch to client 3 failed (pkt %v): %s", c1SwitchPkt.Type, c1SwitchAck.Error)
	}

	_ = c1Stream.SendData([]byte("echo CLIENT_3_ACTIVE\n"))
	if !readUntil(c1Stream, "CLIENT_3_ACTIVE", 4*time.Second) {
		t.Fatal("did not receive output from Client 3 after switch")
	}
}

func readNextNonDataPacket(stream protocol.Stream, timeout time.Duration) (*protocol.Packet, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pkt, err := stream.ReadPacket()
		if err != nil {
			return nil, err
		}
		if pkt.Type != protocol.TypeData {
			return pkt, nil
		}
	}
	return nil, context.DeadlineExceeded
}

func readUntil(stream protocol.Stream, expected string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pkt, err := stream.ReadPacket()
		if err != nil {
			return false
		}
		if pkt.Type == protocol.TypeData {
			if strings.Contains(string(pkt.Payload), expected) {
				return true
			}
		}
	}
	return false
}

func TestDirectAttachClient(t *testing.T) {
	srv := server.NewServer(server.ServerConfig{
		Addr:   "127.0.0.1:0",
		Secret: "key123",
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	// Register agent
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ag := agent.NewAgent(agent.Config{
		ServerAddr: srv.Addr().String(),
		Secret:     "key123",
		ID:         "target-box",
		Name:       "Target Box",
	})
	defer ag.Stop()
	go func() { _ = ag.Run(ctx) }()

	// Wait for registration
	time.Sleep(200 * time.Millisecond)

	c := client.NewClient(client.Config{
		ServerAddr:  srv.Addr().String(),
		Secret:      "key123",
		TargetAgent: "target-box",
	})
	defer c.Close()

	// Direct attach with quick timeout
	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancelTimeout()

	// Run will attempt to attach and read stdin (which will timeout or finish)
	_ = c.Run(ctxTimeout)
}
