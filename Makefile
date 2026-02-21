.PHONY: help build test test-coverage lint fmt vet clean install deps run

# Variables
BINARY_NAME=kubenow
BUILD_DIR=bin
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
VERSION_NUM=$(VERSION:v%=%)
LDFLAGS=-ldflags "-X main.Version=$(VERSION_NUM)"

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOCLEAN=$(GOCMD) clean
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOVET=$(GOCMD) vet

help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary
	@echo "Building $(BINARY_NAME) version $(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -trimpath $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/$(BINARY_NAME)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

test: ## Run tests
	@echo "Running tests..."
	$(GOTEST) -v -race ./...

test-coverage: ## Run tests with coverage report
	@echo "Running tests with coverage..."
	$(GOTEST) -v -coverprofile=coverage.out ./... || true
	@echo "Generating HTML coverage report..."
	@if [ -f coverage.out ]; then \
		$(GOCMD) tool cover -html=coverage.out -o coverage.html; \
		echo "Coverage report generated: coverage.html"; \
	else \
		echo "No coverage data generated"; \
	fi

test-short: ## Run tests without race detector (faster)
	@echo "Running tests (short)..."
	$(GOTEST) -v -short ./...

lint: ## Run golangci-lint
	@echo "Running golangci-lint..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Install with: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin"; exit 1)
	golangci-lint run

fmt: ## Format code with gofmt
	@echo "Formatting code..."
	$(GOFMT) -s -w .

vet: ## Run go vet
	@echo "Running go vet..."
	$(GOVET) ./...

clean: ## Remove build artifacts
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html
	@echo "Clean complete"

install: build ## Install binary to GOPATH/bin
	@echo "Installing to $(GOPATH)/bin..."
	cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/
	@echo "Installed: $(GOPATH)/bin/$(BINARY_NAME)"

deps: ## Download and tidy dependencies
	@echo "Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy
	@echo "Dependencies updated"

run: build ## Build and run the binary
	@echo "Running $(BINARY_NAME)..."
	./$(BUILD_DIR)/$(BINARY_NAME) --help

# Multi-platform builds
build-linux: ## Build for Linux (amd64)
	@echo "Building for Linux amd64..."
	GOOS=linux GOARCH=amd64 $(GOBUILD) -trimpath $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/$(BINARY_NAME)

build-linux-arm: ## Build for Linux (arm64)
	@echo "Building for Linux arm64..."
	GOOS=linux GOARCH=arm64 $(GOBUILD) -trimpath $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/$(BINARY_NAME)

build-darwin: ## Build for macOS (amd64)
	@echo "Building for macOS amd64..."
	GOOS=darwin GOARCH=amd64 $(GOBUILD) -trimpath $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/$(BINARY_NAME)

build-darwin-arm: ## Build for macOS (arm64)
	@echo "Building for macOS arm64..."
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -trimpath $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/$(BINARY_NAME)

build-windows: ## Build for Windows (amd64)
	@echo "Building for Windows amd64..."
	GOOS=windows GOARCH=amd64 $(GOBUILD) -trimpath $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/$(BINARY_NAME)

build-all: build-linux build-linux-arm build-darwin build-darwin-arm build-windows ## Build for all platforms
	@echo "All platform builds complete"

# Development helpers
watch: ## Watch for changes and rebuild (requires entr)
	@which entr > /dev/null || (echo "entr not installed. Install with: brew install entr (macOS) or apt install entr (Linux)"; exit 1)
	@echo "Watching for changes (Ctrl+C to stop)..."
	find . -name '*.go' | entr -c make build

check: fmt vet lint test ## Run all checks (fmt, vet, lint, test)
	@echo "All checks passed!"

ci: deps check ## Run CI checks (for GitHub Actions)
	@echo "CI checks complete"
