# Compilation Guide: Building `kaggle-workaround` on Machines A, B, and C

This document provides step-by-step instructions for compiling the single unified `kaggle-workaround` binary on:
- **Machine A**: Local operator terminal (macOS, Linux, or Windows)
- **Machine B**: Relay server (Google Cloud VM or any Linux cloud host)
- **Machine C**: Target instance (Kaggle notebook or remote GPU server)

Because `kaggle-workaround` is written in pure Go without CGo dependencies (`CGO_ENABLED=0`), you can **either compile natively on each machine** or **cross-compile from your local machine (Machine A) for all platforms** in seconds.

---

## Table of Contents

1. [Compilation Overview](#compilation-overview)
2. [Machine A: Local Terminal (Operator)](#machine-a-local-terminal-operator)
   - [macOS (Apple Silicon & Intel)](#macos-apple-silicon--intel)
   - [Linux (Ubuntu / Debian / Arch / Fedora)](#linux-local)
   - [Windows (PowerShell / Command Prompt)](#windows-local)
3. [Machine B: Google Cloud VM (Relay Server)](#machine-b-google-cloud-vm-relay-server)
   - [Native Compilation on GCP](#native-compilation-on-gcp)
   - [Docker Build](#docker-build-on-gcp)
   - [Systemd Service Setup](#running-as-a-systemd-service)
4. [Machine C: Kaggle Notebook (Target Agent)](#machine-c-kaggle-notebook-target-agent)
   - [Method 1: Automatic Bootstrap (Recommended)](#method-1-automatic-bootstrap-recommended)
   - [Method 2: Compile Manually in a Kaggle Notebook Cell](#method-2-compile-manually-in-a-kaggle-notebook-cell)
   - [Method 3: Cross-Compile on Machine A & Upload](#method-3-cross-compile-on-machine-a--upload)
5. [Cross-Compilation Reference Table](#cross-compilation-reference-table)
6. [Build Scripts and Make Targets](#build-scripts-and-make-targets)

---

## Compilation Overview

- **Go Version Requirement**: Go 1.22 or newer (Go 1.22 - 1.26+ are fully supported).
- **CGo Requirement**: None (`CGO_ENABLED=0`). Binaries are 100% statically linked with zero shared library dependencies.
- **Binary Size**: ~3.2 MB when built with `-ldflags="-s -w"` (symbol table and debug information stripped).

---

## Machine A: Local Terminal (Operator)

Machine A is the machine where you run the client interface (`kaggle-workaround client`).

### macOS (Apple Silicon & Intel)

1. **Install Go** (if not installed):
   ```bash
   brew install go
   ```
   Verify with `go version`.

2. **Clone and Compile**:
   ```bash
   git clone https://github.com/khangtruong-ui/kaggle-workaround.git
   cd kaggle-workaround

   # Compile native binary for your Mac
   make build
   # or manually:
   CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/kaggle-workaround ./cmd/relay
   ```

3. **Install to System PATH** (optional):
   ```bash
   sudo cp bin/kaggle-workaround /usr/local/bin/
   ```

4. **Verify**:
   ```bash
   kaggle-workaround --help
   ```

---

### Linux (Local)

1. **Install Go**:
   - Ubuntu / Debian:
     ```bash
     sudo apt update && sudo apt install -y golang-go
     ```
   - Arch Linux:
     ```bash
     sudo pacman -S go
     ```
   - Fedora / RHEL:
     ```bash
     sudo dnf install golang
     ```

2. **Clone and Compile**:
   ```bash
   git clone https://github.com/khangtruong-ui/kaggle-workaround.git
   cd kaggle-workaround

   make build
   # or manually:
   CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/kaggle-workaround ./cmd/relay
   ```

3. **Install to PATH**:
   ```bash
   sudo cp bin/kaggle-workaround /usr/local/bin/
   ```

---

### Windows (Local)

1. **Install Go**:
   - Using winget:
     ```powershell
     winget install GoLang.Go
     ```
   - Or using Chocolatey:
     ```powershell
     choco install golang
     ```
   Restart PowerShell so `$env:PATH` updates.

2. **Clone and Compile**:
   ```powershell
   git clone https://github.com/khangtruong-ui/kaggle-workaround.git
   cd kaggle-workaround

   # Compile Windows .exe
   go build -ldflags="-s -w" -o bin\kaggle-workaround.exe .\cmd\relay
   ```

3. **Verify**:
   ```powershell
   .\bin\kaggle-workaround.exe --help
   ```

---

## Machine B: Google Cloud VM (Relay Server)

Machine B acts as the central rendezvous relay. It requires **only one open TCP port** (e.g. `9090`) in your Google Cloud VPC firewall.

### Native Compilation on GCP

1. **SSH into your GCP VM**:
   ```bash
   gcloud compute ssh your-vm-name --zone=us-central1-a
   ```

2. **Install Go**:
   ```bash
   sudo apt-get update
   sudo apt-get install -y golang-go git make
   ```

3. **Clone and Build**:
   ```bash
   git clone https://github.com/khangtruong-ui/kaggle-workaround.git
   cd kaggle-workaround

   make build
   # The binary is placed at ./bin/kaggle-workaround
   ```

4. **Run the Relay Server**:
   ```bash
   # Run in background daemon mode (unblocking terminal)
   ./bin/kaggle-workaround server -daemon -bind 0.0.0.0 -port 9090 -secret "your-strong-secret"

   # Check status or stop anytime:
   ./bin/kaggle-workaround server -status
   ./bin/kaggle-workaround server -stop
   ```

---

### Docker Build on GCP

If you prefer running via Docker without installing Go on the host:

```bash
git clone https://github.com/khangtruong-ui/kaggle-workaround.git
cd kaggle-workaround

# Build the container image
docker build -t kaggle-relay:latest .

# Run in background with port 9090 exposed
docker run -d \
  --name kaggle-relay \
  --restart always \
  -p 9090:9090 \
  -e SECRET="your-strong-secret" \
  kaggle-relay:latest
```

Or using `docker-compose`:
```bash
RELAY_SECRET="your-strong-secret" docker compose up -d
```

---

### Running as a Systemd Service

To keep Machine B's relay server running in the background across reboots:

1. Copy binary to system bin:
   ```bash
   sudo cp bin/kaggle-workaround /usr/local/bin/kaggle-workaround
   ```

2. Create service unit `/etc/systemd/system/kaggle-relay.service`:
   ```ini
   [Unit]
   Description=Kaggle Workaround Relay Server
   After=network.target

   [Service]
   Type=simple
   User=nobody
   ExecStart=/usr/local/bin/kaggle-workaround server -port 9090 -secret "your-strong-secret"
   Restart=always
   RestartSec=3

   [Install]
   WantedBy=multi-user.target
   ```

3. Enable and start:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable --now kaggle-relay
   sudo systemctl status kaggle-relay
   ```

---

## Machine C: Kaggle Notebook (Target Agent)

Kaggle instances are isolated Linux containers (`Ubuntu x86_64`) behind NAT firewalls.

### Method 1: Automatic Bootstrap (Recommended)

In a Kaggle or TPU Notebook code cell, paste and run:

```python
!git clone https://github.com/khangtruong-ui/kaggle-workaround.git
!bash kaggle-workaround/scripts/kaggle_bootstrap.sh \
    --server "<YOUR_GCP_VM_IP>:9090" \
    --secret "your-strong-secret" \
    --name "kaggle-tpu-v3"
```

The bootstrap script automatically compiles the binary and runs the agent in **`-daemon` background mode**, which completes in **~0.2 seconds and unblocks the cell**. Subsequent cells (e.g. measuring your TPU or training models) run without obstruction!

---

### Method 2: Compile Manually in a Kaggle Notebook Cell

If you prefer to compile manually in a Kaggle code cell:

```python
# Step 1: Install Go compiler (takes ~10 seconds)
!apt-get update -qq && apt-get install -y -qq golang-go

# Step 2: Clone repository
!git clone https://github.com/khangtruong-ui/kaggle-workaround.git
%cd kaggle-workaround

# Step 3: Compile static binary
!CGO_ENABLED=0 go build -ldflags="-s -w" -o /tmp/kaggle-workaround ./cmd/relay

# Step 4: Launch Agent in Background (leaves cell unblocked!)
!/tmp/kaggle-workaround agent \
    -daemon \
    -server "<YOUR_GCP_VM_IP>:9090" \
    -secret "your-strong-secret" \
    -name "kaggle-tpu-node"
```

To check status or stop the background agent:
```python
!/tmp/kaggle-workaround agent -status
!/tmp/kaggle-workaround agent -stop
```
    -name "kaggle-gpu-node"
```

---

### Method 3: Cross-Compile on Machine A & Upload

You can compile a Linux binary directly on your local Mac or Windows machine in 2 seconds, with zero need for Go on Kaggle:

1. **On Machine A (Mac or Windows)**:
   ```bash
   # Cross-compile for Kaggle (Linux x86_64)
   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o kaggle-workaround-linux ./cmd/relay
   ```

2. **Transfer to Kaggle**:
   - Upload `kaggle-workaround-linux` as a Kaggle Dataset, OR
   - Host it on GitHub Releases and download with `wget`:
     ```python
     !wget -q https://github.com/khangtruong-ui/kaggle-workaround/releases/latest/download/kaggle-workaround-linux-amd64 -O /tmp/kaggle-workaround
     !chmod +x /tmp/kaggle-workaround
     !/tmp/kaggle-workaround agent -server "<IP>:9090" -secret "..."
     ```

---

## Cross-Compilation Reference Table

Because `CGO_ENABLED=0` is used, cross-compiling from any host machine to any target machine requires only setting two environment variables (`GOOS` and `GOARCH`):

| Target Machine | Target OS / Hardware | `GOOS` | `GOARCH` | Command |
|---|---|---|---|---|
| **Machine B (GCP VM)** | Linux x86_64 | `linux` | `amd64` | `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/kaggle-workaround-linux-amd64 ./cmd/relay` |
| **Machine C (Kaggle)** | Linux x86_64 | `linux` | `amd64` | `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o bin/kaggle-workaround-linux-amd64 ./cmd/relay` |
| **Machine A (Mac M1/M2/M3/M4)** | macOS ARM64 | `darwin` | `arm64` | `CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o bin/kaggle-workaround-darwin-arm64 ./cmd/relay` |
| **Machine A (Mac Intel)** | macOS x86_64 | `darwin` | `amd64` | `CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -o bin/kaggle-workaround-darwin-amd64 ./cmd/relay` |
| **Machine A (Windows)** | Windows x86_64 | `windows` | `amd64` | `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/kaggle-workaround-windows-amd64.exe ./cmd/relay` |
| **Edge / ARM Linux** | Linux ARM64 | `linux` | `arm64` | `CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o bin/kaggle-workaround-linux-arm64 ./cmd/relay` |

---

## Build Scripts and Make Targets

We provide pre-configured scripts and Makefile targets to automate building:

### Using the Build Script (`scripts/build.sh`)
```bash
# Build native binary for current machine
./scripts/build.sh

# Build all release binaries into bin/
./scripts/build.sh --all

# Build specific target:
./scripts/build.sh --linux-amd64
./scripts/build.sh --darwin-arm64
./scripts/build.sh --windows-amd64
```

### Using Make (`Makefile`)
```bash
make build              # Native build
make build-linux        # Linux x86_64 (Machine B & C)
make build-darwin-arm64 # macOS Apple Silicon (Machine A)
make build-darwin-amd64 # macOS Intel (Machine A)
make build-windows      # Windows x86_64 (Machine A)
make release            # Build all platforms
make test               # Run test suite with race detector
make clean              # Remove build artifacts
```
