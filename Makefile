# kubectl-create-resource Makefile

BINARY_NAME=kubectl-create-resource
VERSION?=0.1.0
BUILD_DIR=bin
GOPATH=$(shell go env GOPATH)

.PHONY: all build clean install uninstall test lint

all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -ldflags="-X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/kubectl-create-resource

# Install to GOPATH/bin (kubectl will discover it)
install: build
	@echo "Installing $(BINARY_NAME) to $(GOPATH)/bin..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/
	@echo "Installed! You can now use 'kubectl create-resource'"

# Uninstall from GOPATH/bin
uninstall:
	@echo "Removing $(BINARY_NAME) from $(GOPATH)/bin..."
	@rm -f $(GOPATH)/bin/$(BINARY_NAME)
	@echo "Uninstalled."

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@go clean

# Run tests
test:
	go test -v ./...

# Run linter
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed"; exit 1; }
	golangci-lint run ./...

# Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/kubectl-create-resource
	GOOS=linux GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/kubectl-create-resource
	GOOS=darwin GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/kubectl-create-resource
	GOOS=darwin GOARCH=arm64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/kubectl-create-resource
	GOOS=windows GOARCH=amd64 go build -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/kubectl-create-resource

# Development mode with hot reload (requires entr)
dev:
	@command -v entr >/dev/null 2>&1 || { echo "entr not installed (brew install entr)"; exit 1; }
	find . -name '*.go' | entr -r make build
