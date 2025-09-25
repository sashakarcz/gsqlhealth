# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt

# Go cache directories (fix for Ubuntu permission issues)
GOCACHE=$(shell $(GOCMD) env GOCACHE)
GOMODCACHE=$(shell $(GOCMD) env GOMODCACHE)

# Environment variables to avoid /tmp permission issues
export GOCACHE := $(PWD)/.cache/go-build
export GOMODCACHE := $(PWD)/.cache/go-mod
export GOTMPDIR := $(PWD)/.cache/tmp

# Binary info
BINARY_NAME=gsqlhealth
BINARY_DIR=bin
MAIN_PATH=./cmd/gsqlhealth

# Build flags
LDFLAGS=-ldflags "-s -w"

# Default target
.PHONY: all
all: clean deps test build

# Build the binary
.PHONY: build
build: setup-cache
	mkdir -p $(BINARY_DIR)
	chmod 755 $(BINARY_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	chmod 755 $(BINARY_DIR)/$(BINARY_NAME)

# Build for multiple platforms
.PHONY: build-all
build-all: clean deps setup-cache
	mkdir -p $(BINARY_DIR)
	chmod 755 $(BINARY_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME)-darwin-amd64 $(MAIN_PATH)
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME)-windows-amd64.exe $(MAIN_PATH)
	chmod 755 $(BINARY_DIR)/*

# Setup cache directories
.PHONY: setup-cache
setup-cache:
	@mkdir -p .cache/go-build .cache/go-mod .cache/tmp
	@chmod 755 .cache .cache/go-build .cache/go-mod .cache/tmp

# Run tests
.PHONY: test
test: setup-cache
	$(GOTEST) -v ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage: setup-cache
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Run tests with race detection
.PHONY: test-race
test-race: setup-cache
	$(GOTEST) -v -race ./...

# Format code
.PHONY: fmt
fmt:
	$(GOFMT) ./...

# Lint code
.PHONY: lint
lint:
	golangci-lint run

# Install dependencies
.PHONY: deps
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Clean build artifacts
.PHONY: clean
clean:
	$(GOCLEAN)
	rm -rf $(BINARY_DIR) .cache
	rm -f coverage.out coverage.html

# Run the application
.PHONY: run
run: build
	./$(BINARY_DIR)/$(BINARY_NAME) -config config.yaml

# Validate configuration
.PHONY: validate-config
validate-config: build
	./$(BINARY_DIR)/$(BINARY_NAME) -config config.yaml -validate

# Run with live reload (requires air: go install github.com/cosmtrek/air@latest)
.PHONY: dev
dev:
	air

# Install the binary
.PHONY: install
install: build
	cp $(BINARY_DIR)/$(BINARY_NAME) /usr/local/bin/

# Create Docker image
.PHONY: docker-build
docker-build:
	docker build -t $(BINARY_NAME):latest .

# Run Docker container
.PHONY: docker-run
docker-run:
	docker run -p 8080:8080 -v $(PWD)/config.yaml:/app/config.yaml $(BINARY_NAME):latest

# Security scan
.PHONY: security
security:
	gosec ./...

# Check for updates
.PHONY: update-deps
update-deps:
	$(GOCMD) get -u ./...
	$(GOMOD) tidy

# Generate documentation
.PHONY: docs
docs:
	$(GOCMD) doc -all ./...

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build          - Build the binary"
	@echo "  build-all      - Build for multiple platforms"
	@echo "  test           - Run tests"
	@echo "  test-coverage  - Run tests with coverage report"
	@echo "  test-race      - Run tests with race detection"
	@echo "  fmt            - Format code"
	@echo "  lint           - Lint code"
	@echo "  deps           - Install dependencies"
	@echo "  clean          - Clean build artifacts and cache"
	@echo "  run            - Build and run the application"
	@echo "  validate-config- Validate configuration file"
	@echo "  dev            - Run with live reload"
	@echo "  install        - Install binary to /usr/local/bin"
	@echo "  docker-build   - Create Docker image"
	@echo "  docker-run     - Run Docker container"
	@echo "  security       - Run security scan"
	@echo "  update-deps    - Update dependencies"
	@echo "  docs           - Generate documentation"
	@echo "  setup-cache    - Setup local cache directories (fixes Ubuntu permissions)"
	@echo "  help           - Show this help message"