package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/khangtruong-ui/kaggle-workaround/pkg/agent"
	"github.com/khangtruong-ui/kaggle-workaround/pkg/client"
	"github.com/khangtruong-ui/kaggle-workaround/pkg/server"
	"github.com/khangtruong-ui/kaggle-workaround/pkg/ui"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch command {
	case "server":
		runServer(ctx, os.Args[2:])
	case "agent":
		runAgent(ctx, os.Args[2:])
	case "client", "ctl", "controller", "connect":
		runClient(ctx, os.Args[2:])
	case "version", "-v", "--version":
		fmt.Printf("kaggle-workaround v%s\n", version)
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	banner := `
╔══════════════════════════════════════════════════════════════════╗
║              KAGGLE WORKAROUND RELAY TERMINAL                    ║
║         Indirect Multi-Machine Control System (v1.0.0)           ║
╚══════════════════════════════════════════════════════════════════╝

A single binary to control remote Kaggle instances through a relay VM.
Architecture: Machine A (Local) -> Machine B (Relay VM) <- Machine C (Kaggle)

COMMANDS:
  server    Run Machine B (Relay Server) on a cloud VM (requires only 1 open port)
  agent     Run Machine C (Agent) inside Kaggle Notebook / remote GPU instance
  client    Run Machine A (Controller) on your local terminal (interactive UI)

USAGE EXAMPLES:
  1. Start Relay Server (Machine B, Google Cloud VM):
     $ kaggle-workaround server -port 9090 -secret mysecret

  2. Connect Kaggle Instance (Machine C, Kaggle Notebook):
     $ kaggle-workaround agent -server 34.12.34.56:9090 -secret mysecret -name kaggle-t4

  3. Connect Local Terminal (Machine A, Laptop):
     $ kaggle-workaround client -server 34.12.34.56:9090 -secret mysecret

HOTKEYS IN ACTIVE TERMINAL:
  • Ctrl+]     Detach from remote instance (keeps training running in background)
  • Ctrl+C     Normal interrupt (forwards SIGINT to remote process group)
`
	fmt.Println(banner)
}

func getEnvOrDefault(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}

func runServer(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	port := fs.Int("port", 9090, "TCP port to listen on for both controllers and agents")
	bind := fs.String("bind", "0.0.0.0", "Network address to bind")
	secret := fs.String("secret", getEnvOrDefault("RELAY_SECRET", getEnvOrDefault("SECRET", "")), "Shared secret token for authentication")

	_ = fs.Parse(args)

	// Environment variable fallback for PORT
	if envPort := os.Getenv("PORT"); envPort != "" && *port == 9090 {
		if p, err := strconv.Atoi(envPort); err == nil {
			*port = p
		}
	}

	addr := fmt.Sprintf("%s:%d", *bind, *port)
	srv := server.NewServer(server.ServerConfig{
		Addr:   addr,
		Secret: *secret,
	})

	if err := srv.Start(); err != nil {
		log.Fatalf("[Error] Failed to start server: %v", err)
	}

	fmt.Printf("\n%s[✓] Machine B Relay Server active on %s%s\n", ui.BrightGreen+ui.Bold, addr, ui.Reset)
	if *secret != "" {
		fmt.Printf("    Authentication enabled (secret configured)\n")
	} else {
		fmt.Printf("    %sWarning: Running without -secret (open relay)%s\n", ui.Yellow, ui.Reset)
	}
	fmt.Printf("    Single open port handles both incoming controllers and agents.\n\n")

	<-ctx.Done()
	fmt.Println("\n[Server] Shutting down relay...")
	_ = srv.Stop()
}

func runAgent(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	serverAddr := fs.String("server", getEnvOrDefault("RELAY_SERVER", "127.0.0.1:9090"), "Relay server host:port (Machine B)")
	secret := fs.String("secret", getEnvOrDefault("RELAY_SECRET", getEnvOrDefault("SECRET", "")), "Shared secret authentication token")
	name := fs.String("name", "", "Friendly name for this Kaggle instance")
	id := fs.String("id", "", "Custom ID for this agent (default: auto)")
	shell := fs.String("shell", "", "Shell path to launch (default: $SHELL or /bin/bash)")
	persistent := fs.Bool("persistent", true, "Keep shell session running across detaches/disconnects")

	_ = fs.Parse(args)

	ag := agent.NewAgent(agent.Config{
		ServerAddr:        *serverAddr,
		Secret:            *secret,
		ID:                *id,
		Name:              *name,
		Shell:             *shell,
		Persistent:        *persistent,
		AutoReconnect:     true,
		HeartbeatInterval: 20 * time.Second,
		ReconnectInterval: 5 * time.Second,
	})

	go func() {
		<-ctx.Done()
		ag.Stop()
	}()

	if err := ag.Run(ctx); err != nil && err != context.Canceled {
		log.Fatalf("[Agent] Exited with error: %v", err)
	}
}

func runClient(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	serverAddr := fs.String("server", getEnvOrDefault("RELAY_SERVER", "127.0.0.1:9090"), "Relay server host:port (Machine B)")
	secret := fs.String("secret", getEnvOrDefault("RELAY_SECRET", getEnvOrDefault("SECRET", "")), "Shared secret authentication token")
	attach := fs.String("attach", "", "Directly attach to agent ID or Name without interactive menu")

	_ = fs.Parse(args)

	c := client.NewClient(client.Config{
		ServerAddr:  *serverAddr,
		Secret:      *secret,
		TargetAgent: *attach,
	})

	go func() {
		<-ctx.Done()
		_ = c.Close()
	}()

	if err := c.Run(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "\n%s[Error] Controller error: %v%s\n", ui.Red+ui.Bold, err, ui.Reset)
		os.Exit(1)
	}
}
