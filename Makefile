# Ancora - Persistent memory for AI agents
.PHONY: build install test clean lint cross help dev run release release-check

BINARY_NAME=ancora
MAIN_PATH=./cmd/ancora
BUILD_DIR=./bin
GO=go
GOFLAGS=-v
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-s -w -X main.version=$(VERSION)

## build: Build the syfra binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Binary built at $(BUILD_DIR)/$(BINARY_NAME)"

## install: Install syfra to $GOPATH/bin
install:
	@echo "Installing $(BINARY_NAME) to $(shell go env GOPATH)/bin..."
	$(GO) install $(GOFLAGS) -ldflags="$(LDFLAGS)" $(MAIN_PATH)
	@echo "Installed! Run with: $(BINARY_NAME)"

## dev: Build without optimization for development
dev:
	@echo "Building $(BINARY_NAME) (dev mode)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Dev binary built at $(BUILD_DIR)/$(BINARY_NAME)"

## run: Build and run the binary
run: build
	@echo "Running $(BINARY_NAME)..."
	@$(BUILD_DIR)/$(BINARY_NAME)

## test: Run all tests
test:
	@echo "Running tests..."
	$(GO) test -v -race -coverprofile=coverage.out ./...

## test-coverage: Run tests with coverage report
test-coverage: test
	@echo "Generating coverage report..."
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report at coverage.html"

## lint: Run golangci-lint (requires golangci-lint installed)
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not found. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

## cross: Cross-compile for multiple platforms
cross:
	@echo "Cross-compiling for multiple platforms..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	GOOS=linux GOARCH=arm64 $(GO) build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 $(MAIN_PATH)
	GOOS=darwin GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	GOOS=darwin GOARCH=arm64 $(GO) build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)
	GOOS=windows GOARCH=amd64 $(GO) build -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)
	@echo "Cross-compilation complete. Binaries in $(BUILD_DIR)/"

## clean: Remove build artifacts
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out coverage.html
	@echo "Clean complete"

## tidy: Run go mod tidy
tidy:
	@echo "Running go mod tidy..."
	$(GO) mod tidy

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download

## release-check: Verify everything is ready for release
release-check:
	@echo "Checking release prerequisites..."
	@if [ -z "$$(git status --porcelain)" ]; then \
		echo "✓ Working directory is clean"; \
	else \
		echo "✗ Working directory has uncommitted changes"; \
		exit 1; \
	fi
	@if git describe --exact-match --tags HEAD >/dev/null 2>&1; then \
		echo "✓ Current commit is tagged"; \
	else \
		echo "✗ Current commit is not tagged. Create a tag first: git tag -a vX.Y.Z -m 'Release vX.Y.Z'"; \
		exit 1; \
	fi
	@echo "✓ All checks passed"

## release: Create a new release (requires version tag)
release: release-check test lint
	@echo "Triggering release for version $(VERSION)..."
	@echo "Pushing tag to GitHub..."
	git push origin $(VERSION)
	@echo ""
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
	@echo "Release triggered! GitHub Actions will:"
	@echo "  1. Build binaries for all platforms"
	@echo "  2. Create GitHub release with artifacts"
	@echo "  3. Update Homebrew formula"
	@echo ""
	@echo "Monitor progress: https://github.com/syfra-io/syfra/actions"
	@echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

## help: Show this help message
help:
	@echo "Available targets:"
	@echo "  build          - Build the syfra binary"
	@echo "  install        - Install syfra to \$$GOPATH/bin"
	@echo "  dev            - Build without optimization (faster for development)"
	@echo "  run            - Build and run the binary"
	@echo "  test           - Run all tests with race detection"
	@echo "  test-coverage  - Run tests and generate HTML coverage report"
	@echo "  lint           - Run golangci-lint"
	@echo "  cross          - Cross-compile for all platforms"
	@echo "  release-check  - Verify prerequisites for release"
	@echo "  release        - Create and push a new release (requires git tag)"
	@echo "  clean          - Remove build artifacts"
	@echo "  tidy           - Run go mod tidy"
	@echo "  deps           - Download dependencies"
	@echo "  help           - Show this help message"
