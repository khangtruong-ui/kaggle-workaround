#!/usr/bin/env bash
set -e

# ==============================================================================
# Kaggle Workaround Bootstrap Script
# Starts Machine C (Agent) on Kaggle or any remote Linux/GPU instance.
# ==============================================================================

SERVER_ADDR=""
SECRET=""
NAME=""
SHELL_BIN="/bin/bash"

print_help() {
    echo "Usage: $0 --server <RELAY_IP:PORT> --secret <SECRET> [--name <NAME>]"
    echo ""
    echo "Options:"
    echo "  --server   Machine B Relay address (e.g. 34.12.34.56:9090)"
    echo "  --secret   Shared authentication secret"
    echo "  --name     Friendly name for this instance (default: hostname)"
    echo "  --shell    Shell binary to launch (default: /bin/bash)"
    exit 1
}

while [[ "$#" -gt 0 ]]; do
    case $1 in
        --server) SERVER_ADDR="$2"; shift ;;
        --secret) SECRET="$2"; shift ;;
        --name) NAME="$2"; shift ;;
        --shell) SHELL_BIN="$2"; shift ;;
        -h|--help) print_help ;;
        *) echo "Unknown parameter: $1"; print_help ;;
    esac
    shift
done

if [ -z "$SERVER_ADDR" ]; then
    echo "[!] Error: --server <RELAY_IP:PORT> is required."
    print_help
fi

# Detect hostname / Kaggle
if [ -z "$NAME" ]; then
    if [ -d "/kaggle" ]; then
        NAME="kaggle-$(hostname)"
    else
        NAME="$(hostname)"
    fi
fi

echo "========================================================"
echo " Starting Kaggle Workaround Agent"
echo " Relay Server: $SERVER_ADDR"
echo " Instance Name: $NAME"
echo "========================================================"

# Check if binary already exists in current directory or /tmp
BIN_PATH="./kaggle-workaround"
if [ ! -f "$BIN_PATH" ]; then
    BIN_PATH="/tmp/kaggle-workaround"
fi

if [ ! -f "$BIN_PATH" ]; then
    echo "[*] Checking for Go compiler..."
    if command -v go >/dev/null 2>&1; then
        echo "[*] Compiling kaggle-workaround binary..."
        go build -o /tmp/kaggle-workaround ./cmd/relay
        BIN_PATH="/tmp/kaggle-workaround"
    else
        echo "[*] Installing Go or downloading binary..."
        # Download standalone binary if available, or install go
        if command -v apt-get >/dev/null 2>&1; then
            DEBIAN_FRONTEND=noninteractive apt-get update -qq && apt-get install -y -qq golang-go
            go build -o /tmp/kaggle-workaround ./cmd/relay
            BIN_PATH="/tmp/kaggle-workaround"
        else
            echo "[!] Could not find go or apt-get to build the agent."
            exit 1
        fi
    fi
fi

chmod +x "$BIN_PATH"

echo "[*] Launching agent in background..."
"$BIN_PATH" agent \
    -server "$SERVER_ADDR" \
    -secret "$SECRET" \
    -name "$NAME" \
    -shell "$SHELL_BIN" \
    -persistent=true
