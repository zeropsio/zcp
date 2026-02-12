.PHONY: help setup test test-short test-race lint lint-fast lint-local vet build all clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILT   ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
MODULE  := github.com/zeropsio/zcp
LINT    := $(shell [ -x ./bin/golangci-lint ] && echo "./bin/golangci-lint" || { command -v golangci-lint 2>/dev/null || echo "./bin/golangci-lint"; })
LDFLAGS  = -s -w \
  -X $(MODULE)/internal/server.Version=$(VERSION) \
  -X $(MODULE)/internal/server.Commit=$(COMMIT) \
  -X $(MODULE)/internal/server.Built=$(BUILT)

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

setup: ## Bootstrap development environment (install all tools)
	@echo "==> Checking prerequisites..."
	@command -v go >/dev/null 2>&1 || { echo "ERROR: Go not installed"; exit 1; }
	@command -v jq >/dev/null 2>&1 || { echo "ERROR: jq not installed (brew install jq)"; exit 1; }
	@echo "==> Installing golangci-lint..."
	@./tools/install.sh
	@echo "==> Configuring git hooks..."
	@git config core.hooksPath .githooks
	@chmod +x .githooks/* 2>/dev/null || true
	@chmod +x .claude/hooks/*.sh 2>/dev/null || true
	@echo "==> Verifying..."
	@go version
	@$(LINT) version
	@jq --version
	@echo "==> Setup complete."

test: ## Run all tests
	go test ./... -count=1

test-short: ## Run tests (short mode, ~3s)
	go test ./... -count=1 -short

test-race: ## Run tests with race detection
	go test -race ./... -count=1

lint: ## Run linter for all target platforms
	GOOS=darwin GOARCH=arm64 $(LINT) run ./...
	GOOS=linux GOARCH=amd64 $(LINT) run ./...

lint-fast: ## Fast lint (native platform, fast linters only, ~3s)
	$(LINT) run ./... --fast-only

lint-local: ## Full lint (native platform only)
	$(LINT) run ./...

vet: ## Run go vet
	go vet ./...

build: ## Build binary
	go build -ldflags "$(LDFLAGS)" -o bin/zcp ./cmd/zcp

clean: ## Remove build artifacts
	rm -rf bin/ builds/

#########
# BUILD #
#########
all: linux-amd linux-386 darwin-amd darwin-arm windows-amd ## Cross-build all platforms

linux-amd: ## Build for Linux amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o builds/zcp-linux-amd64 ./cmd/zcp

linux-386: ## Build for Linux 386
	CGO_ENABLED=0 GOOS=linux GOARCH=386 go build -ldflags "$(LDFLAGS)" -o builds/zcp-linux-386 ./cmd/zcp

darwin-amd: ## Build for macOS amd64
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o builds/zcp-darwin-amd64 ./cmd/zcp

darwin-arm: ## Build for macOS arm64
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o builds/zcp-darwin-arm64 ./cmd/zcp

windows-amd: ## Build for Windows amd64
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o builds/zcp-win-x64.exe ./cmd/zcp
