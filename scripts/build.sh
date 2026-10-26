#!/usr/bin/env bash
set -e

# ==============================================================================
# Build Script for kaggle-workaround
# Supports native compilation and cross-compilation for all platforms.
# ==============================================================================

BIN_DIR="./bin"
mkdir -p "$BIN_DIR"

LDFLAGS="-s -w"

print_usage() {
    echo "Usage: $0 [TARGET]"
    echo ""
    echo "Targets:"
    echo "  (no arg)        Build for current OS / Architecture"
    echo "  --all           Build release binaries for all supported platforms"
    echo "  --linux-amd64   Build for Linux x86_64 (Machine B Relay, Machine C Kaggle)"
    echo "  --linux-arm64   Build for Linux ARM64"
    echo "  --darwin-arm64  Build for macOS Apple Silicon (M1/M2/M3/M4)"
    echo "  --darwin-amd64  Build for macOS Intel"
    echo "  --windows-amd64 Build for Windows x86_64"
    exit 0
}

build_target() {
    local os="$1"
    local arch="$2"
    local ext="$3"
    local output="$BIN_DIR/kaggle-workaround-${os}-${arch}${ext}"

    echo "[*] Building for ${os}/${arch} -> ${output}"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -ldflags="$LDFLAGS" -o "$output" ./cmd/relay
    chmod +x "$output" 2>/dev/null || true
    echo "    Size: $(du -h "$output" | cut -f1)"
}

case "$1" in
    ""|"native")
        echo "[*] Building native binary for $(go env GOOS)/$(go env GOARCH)..."
        CGO_ENABLED=0 go build -ldflags="$LDFLAGS" -o "$BIN_DIR/kaggle-workaround" ./cmd/relay
        echo "[✓] Built: $BIN_DIR/kaggle-workaround ($(du -h "$BIN_DIR/kaggle-workaround" | cut -f1))"
        ;;
    "--all"|"all")
        echo "=========================================================="
        echo " Building kaggle-workaround binaries for all platforms"
        echo "=========================================================="
        build_target "linux" "amd64" ""
        build_target "linux" "arm64" ""
        build_target "darwin" "arm64" ""
        build_target "darwin" "amd64" ""
        build_target "windows" "amd64" ".exe"
        echo "=========================================================="
        echo "[✓] All binaries built successfully in $BIN_DIR"
        ;;
    "--linux-amd64"|"linux-amd64")
        build_target "linux" "amd64" ""
        ;;
    "--linux-arm64"|"linux-arm64")
        build_target "linux" "arm64" ""
        ;;
    "--darwin-arm64"|"darwin-arm64")
        build_target "darwin" "arm64" ""
        ;;
    "--darwin-amd64"|"darwin-amd64")
        build_target "darwin" "amd64" ""
        ;;
    "--windows-amd64"|"windows-amd64")
        build_target "windows" "amd64" ".exe"
        ;;
    "-h"|"--help"|"help")
        print_usage
        ;;
    *)
        echo "[!] Unknown target: $1"
        print_usage
        ;;
esac
