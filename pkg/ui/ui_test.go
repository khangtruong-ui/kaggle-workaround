package ui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d        time.Duration
		expected string
	}{
		{5 * time.Second, "5s"},
		{65 * time.Second, "1m 5s"},
		{3665 * time.Second, "1h 1m"},
	}

	for _, tt := range tests {
		actual := FormatDuration(tt.d)
		if actual != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, actual)
		}
	}
}

func TestRenderAgentTable(t *testing.T) {
	var buf bytes.Buffer

	// Empty table
	RenderAgentTable(&buf, nil)
	if !strings.Contains(buf.String(), "No active Machine C agents") {
		t.Error("expected empty notice")
	}

	// Populated table
	buf.Reset()
	agents := []protocol.AgentInfo{
		{
			ID:          "kaggle-gpu-1",
			Name:        "kaggle-gpu-1",
			Hostname:    "worker-node",
			RemoteAddr:  "127.0.0.1:1234",
			ConnectedAt: time.Now().Add(-10 * time.Minute),
			Attached:    false,
			Metadata: map[string]string{
				"environment": "Kaggle Notebook",
				"gpu":         "Tesla T4 (15GB)",
			},
		},
	}

	RenderAgentTable(&buf, agents)
	out := buf.String()
	if !strings.Contains(out, "kaggle-gpu-1") || !strings.Contains(out, "Tesla T4") || !strings.Contains(out, "IDLE") {
		t.Errorf("rendered table missing expected content: %s", out)
	}
}

func TestPrintBannerAndNotices(t *testing.T) {
	var buf bytes.Buffer
	PrintBanner(&buf, "34.123.45.67:9090")
	if !strings.Contains(buf.String(), "KAGGLE WORKAROUND RELAY TERMINAL") {
		t.Error("banner missing title")
	}

	buf.Reset()
	PrintAttachNotice(&buf, "kaggle-agent", "agent-xyz")
	if !strings.Contains(buf.String(), "ATTACHED TO") || !strings.Contains(buf.String(), "Ctrl+]") {
		t.Error("attach notice missing key info")
	}

	buf.Reset()
	PrintDetachNotice(&buf, "kaggle-agent")
	if !strings.Contains(buf.String(), "Detached") {
		t.Error("detach notice missing key info")
	}
}
