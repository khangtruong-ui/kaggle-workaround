# Kaggle Workaround: Indirect Multi-Machine Control Terminal

A high-performance, single-port TCP terminal relay and controller built in Go.

Designed to overcome the limitations of Kaggle's web terminal (which lacks concurrency, drops connections, and sits behind restrictive firewalls/NATs with no public inbound IP addresses).

---

## Architecture

The system uses an indirect **Rendezvous Relay** topology where both your local machine and your Kaggle instances establish outbound connections to a single central relay server:

```
┌─────────────────────────────────┐               ┌─────────────────────────────────┐
│     Machine A (Local Laptop)    │               │    Machine A' (Controller 2)    │
│  Interactive TUI Controller     │               │  Interactive TUI Controller     │
└────────────────┬────────────────┘               └────────────────┬────────────────┘
                 │ Outbound TCP (:9090)                            │ Outbound TCP (:9090)
                 ▼                                                 ▼
     ══════════════════════════════════════════════════════════════════════════════
     │                       Machine B (Google Cloud VM Relay)                   │
     │                      Listens on ONLY ONE open TCP port (:9090)             │
     │                 Multiplexes Controllers <───> Agent Sessions              │
     ══════════════════════════════════════════════════════════════════════════════
                 ▲                                                 ▲
                 │ Outbound TCP (:9090)                            │ Outbound TCP (:9090)
┌────────────────┴────────────────┐               ┌────────────────┴────────────────┐
│   Machine C1 (Kaggle GPU #1)    │               │   Machine C2 (Kaggle GPU #2)    │
│   PTY Shell Agent (Tesla T4)    │               │   PTY Shell Agent (Tesla P100)  │
└─────────────────────────────────┘               └─────────────────────────────────┘
```

### Key Highlights

- **Single Open Port**: The relay (Machine B) only requires **one** open TCP port (e.g. `9090`). Both operators (Machine A) and target agents (Machine C) connect to this exact same port. Machine B multiplexes streams based on an initial framed handshake.
- **Background Daemon Mode (`-daemon`)**: Both Machine B (Relay Server) and Machine C (Agent) can run completely detached in the background. In Kaggle or TPU notebook environments, launching the agent with `-daemon` completes in **~0.2 seconds and leaves the notebook unblocked**, allowing you to execute TPU benchmarks and measurement cells concurrently!
- **Auto-Detection of GPUs & TPUs**: Automatically detects accelerators:
  - NVIDIA GPUs via `nvidia-smi` (Tesla T4, P100, V100, A100)
  - Google Cloud TPUs (TPU v3-8, v2-8, TPU VM environments)
- **True `Ctrl+C` Support**: Operates local terminals in raw mode. Keystrokes like `Ctrl+C` (`0x03`) are forwarded straight to the remote PTY master on Machine C, delivering `SIGINT` to the remote foreground process (e.g. cancelling a running python command) **without terminating your local controller or closing the connection**.
- **Dynamic Terminal Resizing (`SIGWINCH`)**: Terminal width and height changes are communicated in real time, so `htop`, `vim`, `nano`, `tmux`, and shell prompts wrap correctly.
- **Session Persistence**: Detaching with `Ctrl+]` does **not** kill your running shell or training script. Reconnecting later re-attaches to the existing persistent shell.
- **Multi-Client & Multi-Controller**: Manage 3 or more Kaggle instances simultaneously from a unified interactive menu, with support for multiple concurrent controller operators.
- **Zero-Dependency Single Binary**: Compiled into a single self-contained Go binary that serves all three roles: `server`, `agent`, and `client`.

---

## Quick Start Guide

### 1. Machine B: Relay Server (Google Cloud VM)

Run on your public VM (only port `9090` needs to be open in your firewall):

#### In Background (Daemon mode):
```bash
# Starts detached in background, unblocking your shell
./bin/kaggle-workaround server -daemon -port 9090 -secret my-secret-key

# Check status or stop anytime:
./bin/kaggle-workaround server -status
./bin/kaggle-workaround server -stop
```

#### Or using Docker:
```bash
docker run -d \
  --name kaggle-relay \
  -p 9090:9090 \
  -e SECRET=my-secret-key \
  ghcr.io/khangtruong-ui/kaggle-workaround:latest
```

---

### 2. Machine C: Agent (Kaggle / TPU Instance)

In your Kaggle Notebook code cell, paste and run:

```python
# Clone and launch in background (unblocks the cell instantly!)
!git clone https://github.com/khangtruong-ui/kaggle-workaround.git
!bash kaggle-workaround/scripts/kaggle_bootstrap.sh \
    --server <YOUR_GCP_VM_IP>:9090 \
    --secret my-secret-key \
    --name "kaggle-tpu-v3"

# Cell finishes in ~0.2s! Subsequent cells (e.g. measuring TPU) run completely unblocked.
```

The agent automatically detects:
- TPUs (Google Cloud TPU v3-8, v2-8, TPU VM)
- GPUs via `nvidia-smi` (Tesla T4, P100, etc.)
- Kaggle environment metadata and CPU topology

To check status or stop the background agent from a notebook cell:
```python
!kaggle-workaround agent -status
!kaggle-workaround agent -stop
```

---

### 3. Machine A: Local Terminal (Your Laptop)

On your local development machine:

```bash
./bin/kaggle-workaround client -server <YOUR_GCP_VM_IP>:9090 -secret my-secret-key
```

You will be greeted with the interactive terminal dashboard:

```
  ╭──────────────────────────────────────────────────────────────╮
  │             KAGGLE WORKAROUND RELAY TERMINAL                 │
  │        Indirect Machine Control System for Remote GPUs       │
  ╰──────────────────────────────────────────────────────────────╯
  Connected to Relay: 34.12.34.56:9090

  #     NAME / ID           ENVIRONMENT       HARDWARE / GPU        STATUS      UPTIME
  ────────────────────────────────────────────────────────────────────────────────────────
  [1]   kaggle-t4-gpu       Kaggle Notebook   Tesla T4 (15GB)       ● IDLE      4m 12s
        ID: agent-1a2b3c4d
  [2]   kaggle-cpu-fast     Kaggle Notebook   4 cores, 30GB RAM     ● IDLE      18m 05s
        ID: agent-9f8e7d6c

  Enter [#] to attach, [r] to refresh, [q] to exit: 1
```

Once attached, your terminal operates seamlessly as if you were SSH'd directly into Kaggle.

---

## Hotkeys & Terminal Navigation

| Key Combination | Action | Description |
|-----------------|--------|-------------|
| **`Ctrl+]`**    | Detach Session | Leaves remote training processes running in the background and returns to the local controller menu. |
| **`Ctrl+C`**    | Remote Interrupt | Forwards `0x03` (`SIGINT`) to the foreground process in Kaggle (e.g., interrupts `python train.py`). |
| **`Ctrl+D`** / `exit` | Shell Exit | Gracefully closes the Kaggle shell session. |
| **`Ctrl+Z`**    | Remote Suspend | Suspends remote foreground process on Kaggle (`SIGTSTP`). |

---

## CLI Reference

### Global Help
```bash
./bin/kaggle-workaround --help
```

### Server (Machine B)
```bash
./bin/kaggle-workaround server [flags]

Flags:
  -bind string       Network interface to bind (default "0.0.0.0")
  -port int          Port to listen on (default 9090, or $PORT)
  -secret string     Shared authentication secret ($RELAY_SECRET or $SECRET)
  -daemon, -d        Run in background as a detached daemon process
  -status            Check background server status
  -stop              Stop running background server
  -log-file string   Log file path (default "/tmp/kaggle-server.log")
  -pid-file string   PID file path (default "/tmp/kaggle-server.pid")
```

### Agent (Machine C)
```bash
./bin/kaggle-workaround agent [flags]

Flags:
  -server string       Relay host:port (default "127.0.0.1:9090")
  -secret string       Shared authentication secret
  -name string         Friendly display name (defaults to hostname)
  -id string           Custom agent ID (default auto-generated)
  -shell string        Shell binary to launch (default $SHELL or /bin/bash)
  -persistent bool     Keep shell running across disconnects (default true)
  -daemon, -d          Run in background (unblocks Kaggle / TPU notebook cells)
  -status              Check background agent status
  -stop                Stop running background agent
  -log-file string     Log file path (default "/tmp/kaggle-agent.log")
  -pid-file string     PID file path (default "/tmp/kaggle-agent.pid")
```

### Client (Machine A)
```bash
./bin/kaggle-workaround client [flags]

Flags:
  -server string       Relay host:port (default "127.0.0.1:9090")
  -secret string       Shared authentication secret
  -attach string       Directly attach to an agent by ID or name without opening menu
```

---

## Wire Protocol Specification

All communications across the single TCP port use a binary framed header:

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                      Payload Length (32 bits)                 |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  Type (8 bits)|             Payload Data (N bytes)...         |
+-+-+-+-+-+-+-+-+                                               |
```

| Type Code | Name | Description |
|-----------|------|-------------|
| `0x01` | `TypeHandshake` | Initial role declaration ("agent" or "controller") + auth token |
| `0x02` | `TypeHandshakeAck` | Relay confirmation with assigned session ID |
| `0x03` | `TypeHeartbeat` | Periodic NAT keep-alive |
| `0x04` | `TypeHeartbeatAck` | Heartbeat acknowledgment |
| `0x05` | `TypeListAgents` | Controller query for active agents |
| `0x06` | `TypeAgentList` | Relay response containing array of `AgentInfo` |
| `0x07` | `TypeAttach` | Controller request to attach to target agent with window dimensions |
| `0x08` | `TypeAttachAck` | Attach status confirmation |
| `0x09` | `TypeData` | Raw bidirectional terminal bytes (stdin / stdout / signals) |
| `0x0A` | `TypeResize` | Dynamic window size update (`rows`, `cols`) |
| `0x0B` | `TypeDetach` | Session detachment notification |
| `0x0C` | `TypeError` | Diagnostic / error messages |

---

## Building and Compilation

See the comprehensive [Compilation Guide (BUILD.md)](BUILD.md) for detailed platform-specific and cross-compilation instructions.

### Quick Build Commands

#### Native Build (Current Host)
```bash
make build
# or: ./scripts/build.sh
```

#### Machine A (Local Terminal - Mac / Windows / Linux)
```bash
# macOS Apple Silicon (M1/M2/M3/M4)
make build-darwin-arm64

# macOS Intel
make build-darwin-amd64

# Windows x86_64
make build-windows
```

#### Machine B (Relay Server - Google Cloud VM)
```bash
make build-linux
# or with Docker:
docker build -t kaggle-relay:latest .
```

#### Machine C (Kaggle Notebook)
In a Kaggle code cell:
```python
!apt-get update -qq && apt-get install -y -qq golang-go
!git clone https://github.com/khangtruong-ui/kaggle-workaround.git
%cd kaggle-workaround
!make build
```
Or run the automated bootstrap script:
```python
!bash scripts/kaggle_bootstrap.sh --server <GCP_IP>:9090 --secret <SECRET> --name kaggle-gpu
```

#### Build All Release Targets
```bash
make release
# or: ./scripts/build.sh --all
```

---

## Testing

```bash
make test
# or: go test -v -race ./...
```

The test suite covers:
- Protocol encoding/decoding, edge cases, buffer bounds, and concurrent streaming
- Authentication verification and rejection of invalid secrets
- Pseudo-terminal (PTY) lifecycle and dynamic window resizing
- Full multi-client integration with 3 concurrent agents and 2 concurrent controllers
- Real `Ctrl+C` interrupt handling and prompt recovery

---

## License

MIT License. See [LICENSE](LICENSE) for details.
