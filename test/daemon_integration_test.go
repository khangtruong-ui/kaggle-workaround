package test

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
)

func TestBackgroundDaemonMode(t *testing.T) {
	tempDir := t.TempDir()
	serverPidFile := filepath.Join(tempDir, "server.pid")
	serverLogFile := filepath.Join(tempDir, "server.log")
	agentPidFile := filepath.Join(tempDir, "agent.pid")
	agentLogFile := filepath.Join(tempDir, "agent.log")

	// Pick a free random TCP port
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	serverAddr := fmt.Sprintf("127.0.0.1:%d", port)
	const secret = "bg-daemon-test-key"

	// Find or build the binary
	binPath := filepath.Join(tempDir, "kaggle-workaround")
	buildCmd := exec.Command("go", "build", "-o", binPath, "../cmd/relay")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build binary: %v, out: %s", err, string(out))
	}
	defer os.Remove(binPath)

	// 1. Launch Machine B (Server) in daemon background mode
	srvCmd := exec.Command(binPath, "server",
		"-daemon",
		"-port", fmt.Sprintf("%d", port),
		"-secret", secret,
		"-pid-file", serverPidFile,
		"-log-file", serverLogFile,
	)
	if out, err := srvCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to launch server daemon: %v, out: %s", err, string(out))
	}

	// Verify server PID file exists
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(serverPidFile); err != nil {
		t.Fatalf("server pid file missing: %v", err)
	}

	// 2. Check server status via CLI
	statusCmd := exec.Command(binPath, "server",
		"-status",
		"-pid-file", serverPidFile,
		"-log-file", serverLogFile,
	)
	statusOut, err := statusCmd.CombinedOutput()
	if err != nil || !strings.Contains(string(statusOut), "running in background") {
		t.Fatalf("expected server running in background, got: %s (err: %v)", string(statusOut), err)
	}

	// 3. Launch Machine C (Agent) in daemon background mode (simulating Kaggle unblocked cell)
	agentCmd := exec.Command(binPath, "agent",
		"-daemon",
		"-server", serverAddr,
		"-secret", secret,
		"-name", "kaggle-tpu-v3",
		"-pid-file", agentPidFile,
		"-log-file", agentLogFile,
	)
	if out, err := agentCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to launch agent daemon: %v, out: %s", err, string(out))
	}

	// Verify agent PID file exists
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(agentPidFile); err != nil {
		t.Fatalf("agent pid file missing: %v", err)
	}

	// 4. Check agent status via CLI
	agentStatusCmd := exec.Command(binPath, "agent",
		"-status",
		"-pid-file", agentPidFile,
		"-log-file", agentLogFile,
	)
	agentStatusOut, err := agentStatusCmd.CombinedOutput()
	if err != nil || !strings.Contains(string(agentStatusOut), "running in background") {
		t.Fatalf("expected agent running in background, got: %s (err: %v)", string(agentStatusOut), err)
	}

	// 5. Connect Machine A (Controller) to verify background agent is registered
	conn, err := net.Dial("tcp", serverAddr)
	if err != nil {
		t.Fatalf("failed to connect to background relay: %v", err)
	}
	defer conn.Close()

	stream := protocol.NewFramedStream(conn)
	_ = stream.SendJSON(protocol.TypeHandshake, protocol.HandshakePayload{
		Role:  "controller",
		Token: secret,
	})
	ackPkt, err := stream.ReadPacket()
	if err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	var ack protocol.HandshakeAckPayload
	_ = ackPkt.UnmarshalJSON(&ack)
	if !ack.Success {
		t.Fatalf("handshake rejected: %s", ack.Error)
	}

	// Query agents
	_ = stream.SendJSON(protocol.TypeListAgents, map[string]string{})
	listPkt, err := stream.ReadPacket()
	if err != nil {
		t.Fatalf("failed to read agent list: %v", err)
	}
	var list protocol.AgentListPayload
	_ = listPkt.UnmarshalJSON(&list)

	found := false
	for _, a := range list.Agents {
		if a.Name == "kaggle-tpu-v3" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("background agent 'kaggle-tpu-v3' not found in server list: %+v", list.Agents)
	}

	// 6. Stop Agent via CLI
	stopAgentCmd := exec.Command(binPath, "agent", "-stop", "-pid-file", agentPidFile)
	if out, err := stopAgentCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to stop agent: %v, out: %s", err, string(out))
	}

	// 7. Stop Server via CLI
	stopServerCmd := exec.Command(binPath, "server", "-stop", "-pid-file", serverPidFile)
	if out, err := stopServerCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to stop server: %v, out: %s", err, string(out))
	}

	// Verify PID files removed
	if _, err := os.Stat(agentPidFile); !os.IsNotExist(err) {
		t.Error("expected agent pid file to be cleaned up")
	}
	if _, err := os.Stat(serverPidFile); !os.IsNotExist(err) {
		t.Error("expected server pid file to be cleaned up")
	}
}
