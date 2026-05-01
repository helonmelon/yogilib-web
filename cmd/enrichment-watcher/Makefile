#!/usr/bin/env make

# Makefile for enrichment-watcher daemon

BIN := enrichment-watcher
MAIN := enrichment-watcher.go

all: build

.PHONY: build
build:
	@echo "Building $(BIN)..."
	go build -o $(BIN) .
	@echo "✓ Built: $(BIN)"

.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	rm -f $(BIN) go.sum
	go clean

.PHONY: run
run: build
	@echo "Running daemon..."
	./$(BIN)

.PHONY: deps
deps:
	go mod download
	go mod tidy

.PHONY: test
test:
	go test -v ./...

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: lint
lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed"; exit 1; }
	golangci-lint run

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make build     - Build the daemon binary"
	@echo "  make clean     - Remove build artifacts"
	@echo "  make run       - Build and run the daemon"
	@echo "  make deps      - Download dependencies"
	@echo "  make test      - Run tests"
	@echo "  make fmt       - Format code"
	@echo "  make lint      - Run linter"
	@echo "  make help      - Show this help"
