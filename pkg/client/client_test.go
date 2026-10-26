package client

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
	"github.com/khangtruong-ui/kaggle-workaround/pkg/server"
)

func TestClientQueryAgents(t *testing.T) {
	// Start server
	srv := server.NewServer(server.ServerConfig{
		Addr:   "127.0.0.1:0",
		Secret: "secret-test",
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	serverAddr := srv.Addr().String()

	// Register a mock agent directly on server
	aConn, _ := net.Pipe()
	defer aConn.Close()
	srv.Registry().RegisterAgent(&server.AgentSession{
		ID:          "kaggle-t4",
		Name:        "kaggle-t4-worker",
		Hostname:    "kaggle-node",
		RemoteAddr:  "10.0.0.1:9999",
		ConnectedAt: time.Now(),
		Stream:      protocol.NewFramedStream(aConn),
		Metadata: map[string]string{
			"gpu": "Tesla T4",
		},
	})

	// Connect client
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("dial failed: %v", err)
	}
	defer conn.Close()

	c := &Client{
		config: Config{
			ServerAddr: serverAddr,
			Secret:     "secret-test",
		},
		conn:   conn,
		stream: protocol.NewFramedStream(conn),
	}

	// Send handshake
	err = c.stream.SendJSON(protocol.TypeHandshake, protocol.HandshakePayload{
		Role:  "controller",
		Token: "secret-test",
	})
	if err != nil {
		t.Fatalf("send handshake failed: %v", err)
	}

	ackPkt, err := c.stream.ReadPacket()
	if err != nil {
		t.Fatalf("read handshake ack failed: %v", err)
	}
	var ack protocol.HandshakeAckPayload
	_ = ackPkt.UnmarshalJSON(&ack)
	if !ack.Success {
		t.Fatalf("handshake failed: %s", ack.Error)
	}

	// Query agents
	agents, err := c.QueryAgents()
	if err != nil {
		t.Fatalf("query agents failed: %v", err)
	}

	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}

	if agents[0].ID != "kaggle-t4" || agents[0].Metadata["gpu"] != "Tesla T4" {
		t.Errorf("unexpected agent: %+v", agents[0])
	}
}

func TestClientAuthFailure(t *testing.T) {
	srv := server.NewServer(server.ServerConfig{
		Addr:   "127.0.0.1:0",
		Secret: "secret-test",
	})
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	c := NewClient(Config{
		ServerAddr: srv.Addr().String(),
		Secret:     "wrong-secret",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := c.Run(ctx)
	if err == nil {
		t.Fatal("expected client to fail authentication with wrong secret")
	}
}
