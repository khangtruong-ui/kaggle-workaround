.PHONY: all build build-linux build-darwin-arm64 build-darwin-amd64 build-windows release test clean docker-build

BINARY_NAME=kaggle-workaround
BUILD_DIR=bin
LDFLAGS=-s -w

all: build

# Native build for current host
build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/relay
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# Machine B (GCP VM) & Machine C (Kaggle Linux x86_64)
build-linux:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/relay
	@echo "Built Linux amd64: $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64"

# Machine A: macOS Apple Silicon (M1/M2/M3/M4)
build-darwin-arm64:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/relay
	@echo "Built macOS arm64: $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64"

# Machine A: macOS Intel
build-darwin-amd64:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/relay
	@echo "Built macOS amd64: $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64"

# Machine A: Windows x86_64
build-windows:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/relay
	@echo "Built Windows x64: $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe"

# Build all release binaries
release:
	@./scripts/build.sh --all

test:
	go test -v -race ./...

clean:
	rm -rf $(BUILD_DIR)

docker-build:
	docker build -t $(BINARY_NAME):latest .
