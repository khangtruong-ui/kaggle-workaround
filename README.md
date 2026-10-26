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
- **True `Ctrl+C` Support**: Operates local terminals in raw mode. Keystrokes like `Ctrl+C` (`0x03`) are forwarded straight to the remote PTY master on Machine C, delivering `SIGINT` to the remote foreground process (e.g. cancelling a running python command) **without terminating your local controller or closing the connection**.
- **Dynamic Terminal Resizing (`SIGWINCH`)**: Terminal width and height changes are communicated in real time, so `htop`, `vim`, `nano`, `tmux`, and shell prompts wrap correctly.
- **Session Persistence**: Detaching with `Ctrl+]` does **not** kill your running shell or training script. Reconnecting later re-attaches to the existing persistent shell.
- **Multi-Client & Multi-Controller**: Manage 3 or more Kaggle instances simultaneously from a unified interactive menu, with support for multiple concurrent controller operators.
- **Zero-Dependency Single Binary**: Compiled into a single self-contained Go binary that serves all three roles: `server`, `agent`, and `client`.

---

## Quick Start Guide

### 1. Machine B: Relay Server (Google Cloud VM)

Run on your public VM (only port `9090` needs to be open in your firewall):

#### Using Docker:
```bash
docker run -d \
  --name kaggle-relay \
  -p 9090:9090 \
  -e SECRET=my-secret-key \
  ghcr.io/khangtruong-ui/kaggle-workaround:latest
```

#### Or using the native binary:
```bash
./bin/kaggle-workaround server -port 9090 -secret my-secret-key
```

---

### 2. Machine C: Agent (Kaggle Instance)

In a Kaggle Notebook code cell, run:

```python
# Clone and launch the agent in the background
!git clone https://github.com/khangtruong-ui/kaggle-workaround.git
!bash kaggle-workaround/scripts/kaggle_bootstrap.sh --server <YOUR_GCP_VM_IP>:9090 --secret my-secret-key --name "kaggle-t4-gpu"
```

The agent automatically detects:
- GPU hardware via `nvidia-smi` (e.g. `Tesla T4 15360 MiB`)
- Kaggle environment flags
- Hostname and CPU topology

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
  -bind string     Network interface to bind (default "0.0.0.0")
  -port int        Port to listen on (default 9090, or $PORT)
  -secret string   Shared authentication secret ($RELAY_SECRET or $SECRET)
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

## Building and Testing

### Build Binary
```bash
make build
# or: go build -o bin/kaggle-workaround ./cmd/relay
```

### Run Tests
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
