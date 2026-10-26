.PHONY: all build test clean docker-build

BINARY_NAME=kaggle-workaround
BUILD_DIR=bin

all: build

build:
	@mkdir -p $(BUILD_DIR)
	go build -v -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/relay
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

test:
	go test -v -race ./...

clean:
	rm -rf $(BUILD_DIR)

docker-build:
	docker build -t $(BINARY_NAME):latest .
