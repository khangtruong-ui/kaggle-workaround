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
	"github.com/khangtruong-ui/kaggle-workaround/pkg/daemon"
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

A single binary to control remote Kaggle / TPU instances through a relay VM.
Architecture: Machine A (Local) -> Machine B (Relay VM) <- Machine C (Kaggle/TPU)

COMMANDS:
  server    Run Machine B (Relay Server) on a cloud VM (requires only 1 open port)
  agent     Run Machine C (Agent) inside Kaggle / TPU instance (supports -daemon)
  client    Run Machine A (Controller) on your local terminal (interactive UI)

USAGE EXAMPLES:
  1. Start Relay Server in Background (Machine B, Google Cloud VM):
     $ kaggle-workaround server -daemon -port 9090 -secret mysecret

  2. Connect Kaggle Instance in Background (Machine C, Kaggle / TPU cell):
     $ kaggle-workaround agent -daemon -server 34.12.34.56:9090 -secret mysecret -name "kaggle-tpu"

  3. Connect Local Terminal (Machine A, Laptop):
     $ kaggle-workaround client -server 34.12.34.56:9090 -secret mysecret

  4. Process Control for Background Daemons:
     $ kaggle-workaround server -status    # Check server status
     $ kaggle-workaround server -stop      # Stop background server
     $ kaggle-workaround agent -status     # Check agent status
     $ kaggle-workaround agent -stop       # Stop background agent

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
	daemonFlag := fs.Bool("daemon", false, "Run relay server in background as a detached daemon")
	dFlag := fs.Bool("d", false, "Alias for -daemon")
	stopFlag := fs.Bool("stop", false, "Stop background relay server")
	statusFlag := fs.Bool("status", false, "Check background relay server status")
	logFile := fs.String("log-file", "/tmp/kaggle-server.log", "Log file path for background server")
	pidFile := fs.String("pid-file", "/tmp/kaggle-server.pid", "PID file path for background server")

	_ = fs.Parse(args)

	// Handle background status query
	if *statusFlag {
		running, pid, err := daemon.Status(*pidFile)
		if err != nil {
			log.Fatalf("[Error] %v", err)
		}
		if running {
			fmt.Printf("%s[✓] Relay Server (Machine B) is running in background.%s\n    PID:      %d\n    Log file: %s\n",
				ui.BrightGreen+ui.Bold, ui.Reset, pid, *logFile)
		} else {
			fmt.Printf("[i] Relay Server is not running.\n")
		}
		return
	}

	// Handle background stop request
	if *stopFlag {
		if err := daemon.Stop(*pidFile, "Relay Server (Machine B)"); err != nil {
			log.Fatalf("[Error] %v", err)
		}
		return
	}

	// Handle daemonization
	if *daemonFlag || *dFlag {
		isParent, err := daemon.Daemonize(daemon.Options{
			LogFile:     *logFile,
			PidFile:     *pidFile,
			ServiceName: "Relay Server (Machine B)",
		})
		if err != nil {
			log.Fatalf("[Error] Daemonize failed: %v", err)
		}
		if isParent {
			return // Parent exits immediately
		}
	}

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

	log.Printf("[Server] Machine B Relay Server active on %s", addr)
	if *secret != "" {
		log.Printf("[Server] Authentication enabled (secret configured)")
	} else {
		log.Printf("[Server] Warning: Running without -secret (open relay)")
	}

	<-ctx.Done()
	log.Println("[Server] Shutting down relay...")
	_ = srv.Stop()
}

func runAgent(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	serverAddr := fs.String("server", getEnvOrDefault("RELAY_SERVER", "127.0.0.1:9090"), "Relay server host:port (Machine B)")
	secret := fs.String("secret", getEnvOrDefault("RELAY_SECRET", getEnvOrDefault("SECRET", "")), "Shared secret authentication token")
	name := fs.String("name", "", "Friendly name for this Kaggle / TPU instance")
	id := fs.String("id", "", "Custom ID for this agent (default: auto)")
	shell := fs.String("shell", "", "Shell path to launch (default: $SHELL or /bin/bash)")
	persistent := fs.Bool("persistent", true, "Keep shell session running across detaches/disconnects")
	daemonFlag := fs.Bool("daemon", false, "Run agent in background (unblocks Kaggle / TPU notebook cells)")
	dFlag := fs.Bool("d", false, "Alias for -daemon")
	stopFlag := fs.Bool("stop", false, "Stop background agent")
	statusFlag := fs.Bool("status", false, "Check background agent status")
	logFile := fs.String("log-file", "/tmp/kaggle-agent.log", "Log file path for background agent")
	pidFile := fs.String("pid-file", "/tmp/kaggle-agent.pid", "PID file path for background agent")

	_ = fs.Parse(args)

	// Handle background status query
	if *statusFlag {
		running, pid, err := daemon.Status(*pidFile)
		if err != nil {
			log.Fatalf("[Error] %v", err)
		}
		if running {
			fmt.Printf("%s[✓] Kaggle Agent (Machine C) is running in background.%s\n    PID:      %d\n    Log file: %s\n",
				ui.BrightGreen+ui.Bold, ui.Reset, pid, *logFile)
		} else {
			fmt.Printf("[i] Kaggle Agent is not running.\n")
		}
		return
	}

	// Handle background stop request
	if *stopFlag {
		if err := daemon.Stop(*pidFile, "Kaggle Agent (Machine C)"); err != nil {
			log.Fatalf("[Error] %v", err)
		}
		return
	}

	if *name == "" {
		host, _ := os.Hostname()
		if host != "" {
			*name = host
		} else {
			*name = "kaggle-agent"
		}
	}

	// Handle daemonization
	if *daemonFlag || *dFlag {
		isParent, err := daemon.Daemonize(daemon.Options{
			LogFile:     *logFile,
			PidFile:     *pidFile,
			ServiceName: fmt.Sprintf("Agent '%s' (Machine C)", *name),
		})
		if err != nil {
			log.Fatalf("[Error] Daemonize failed: %v", err)
		}
		if isParent {
			return // Parent exits immediately, unblocking the Kaggle / TPU cell!
		}
	}

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
