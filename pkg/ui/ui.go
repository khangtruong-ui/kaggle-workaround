package ui

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/protocol"
)

// ANSI color codes
const (
	Reset       = "\033[0m"
	Bold        = "\033[1m"
	Dim         = "\033[2m"
	Italic      = "\033[3m"
	Underline   = "\033[4m"
	Red         = "\033[31m"
	Green       = "\033[32m"
	Yellow      = "\033[33m"
	Blue        = "\033[34m"
	Magenta     = "\033[35m"
	Cyan        = "\033[36m"
	White       = "\033[37m"
	Gray        = "\033[90m"
	BrightCyan  = "\033[96m"
	BrightGreen = "\033[92m"
	BgBlue      = "\033[44m"
	BgDarkGray  = "\033[100m"
)

// PrintBanner renders the header banner.
func PrintBanner(w io.Writer, relayAddr string) {
	fmt.Fprintln(w, Cyan+Bold+`  ╭──────────────────────────────────────────────────────────────╮`+Reset)
	fmt.Fprintln(w, Cyan+Bold+`  │             KAGGLE WORKAROUND RELAY TERMINAL                 │`+Reset)
	fmt.Fprintln(w, Cyan+Bold+`  │        Indirect Machine Control System for Remote GPUs       │`+Reset)
	fmt.Fprintln(w, Cyan+Bold+`  ╰──────────────────────────────────────────────────────────────╯`+Reset)
	if relayAddr != "" {
		fmt.Fprintf(w, "  %sConnected to Relay:%s %s%s%s\n\n", Gray, Reset, BrightCyan, relayAddr, Reset)
	}
}

// FormatDuration formats time duration into human readable format.
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// RenderAgentTable prints a table of available agents.
func RenderAgentTable(w io.Writer, agents []protocol.AgentInfo) {
	if len(agents) == 0 {
		fmt.Fprintln(w, Yellow+`  [i] No active Machine C agents connected yet.`+Reset)
		fmt.Fprintln(w, Gray+`      Run an agent on your Kaggle instance to see it listed here.`+Reset)
		fmt.Fprintln(w)
		return
	}

	header := fmt.Sprintf("  %-4s  %-18s  %-16s  %-20s  %-10s  %-10s",
		"#", "NAME / ID", "ENVIRONMENT", "HARDWARE / GPU", "STATUS", "UPTIME")
	separator := "  " + strings.Repeat("─", 88)

	fmt.Fprintln(w, Bold+header+Reset)
	fmt.Fprintln(w, Gray+separator+Reset)

	now := time.Now()
	for i, a := range agents {
		indexStr := fmt.Sprintf("[%d]", i+1)

		nameDisplay := a.Name
		if len(nameDisplay) > 17 {
			nameDisplay = nameDisplay[:14] + "..."
		}

		envDisplay := a.Metadata["environment"]
		if envDisplay == "" {
			envDisplay = a.Hostname
		}
		if len(envDisplay) > 15 {
			envDisplay = envDisplay[:12] + "..."
		}

		gpuDisplay := a.Metadata["gpu"]
		if gpuDisplay == "" {
			gpuDisplay = a.Metadata["cpus"]
		}
		if len(gpuDisplay) > 19 {
			gpuDisplay = gpuDisplay[:16] + "..."
		}

		statusBadge := BrightGreen + "● IDLE" + Reset
		if a.Attached {
			statusBadge = Yellow + "● ATTACHED" + Reset
		}

		uptime := FormatDuration(now.Sub(a.ConnectedAt))

		line := fmt.Sprintf("  %-4s  %-18s  %-16s  %-20s  %-19s  %-10s",
			BrightCyan+indexStr+Reset,
			Bold+nameDisplay+Reset,
			envDisplay,
			Magenta+gpuDisplay+Reset,
			statusBadge,
			Gray+uptime+Reset,
		)
		fmt.Fprintln(w, line)

		if a.ID != a.Name {
			fmt.Fprintf(w, "        %sID: %s%s\n", Gray, a.ID, Reset)
		}
	}
	fmt.Fprintln(w)
}

// PrintAttachNotice displays instructions when entering raw terminal mode.
func PrintAttachNotice(w io.Writer, agentName, agentID string) {
	fmt.Fprintln(w, BrightGreen+Bold+`  ╭──────────────────────────────────────────────────────────────╮`+Reset)
	fmt.Fprintf(w, BrightGreen+Bold+`  │ ATTACHED TO: %-47s │`+Reset+"\n", agentName)
	fmt.Fprintf(w, BrightGreen+Bold+`  │ Agent ID:    %-47s │`+Reset+"\n", agentID)
	fmt.Fprintln(w, BrightGreen+Bold+`  ├──────────────────────────────────────────────────────────────┤`+Reset)
	fmt.Fprintln(w, BrightGreen+Bold+`  │  • Detach hotkey:    Ctrl+]  (Session keeps running on Kaggle)│`+Reset)
	fmt.Fprintln(w, BrightGreen+Bold+`  │  • Normal Ctrl+C:    Interrupts foreground process on Kaggle │`+Reset)
	fmt.Fprintln(w, BrightGreen+Bold+`  │  • Exit shell:       Type 'exit' to end Kaggle shell session │`+Reset)
	fmt.Fprintln(w, BrightGreen+Bold+`  ╰──────────────────────────────────────────────────────────────╯`+Reset)
	fmt.Fprintln(w)
}

// PrintDetachNotice displays confirmation after returning from an active session.
func PrintDetachNotice(w io.Writer, agentName string) {
	fmt.Fprintf(w, "\n%s[✓] Detached from %s.%s Process continues in background on remote instance.\n\n", BrightGreen+Bold, agentName, Reset)
}
