.PHONY: help setup test test-short test-race lint lint-fast lint-local vet build all clean release release-patch catalog-sync e2e-build e2e-deploy e2e-zcpx e2e-zcpx-fast e2e-zcpx-deploy

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

sync: build ## Pull all knowledge from external sources
	./bin/zcp sync pull

sync-recipes: build ## Pull recipes from API
	./bin/zcp sync pull recipes

sync-push: build ## Push knowledge changes as GitHub PRs
	./bin/zcp sync push

catalog-sync: build ## Refresh platform version catalog from API
	./bin/zcp catalog sync

lint-local: catalog-sync ## Full lint (native platform only)
	$(LINT) run ./...

vet: ## Run go vet
	go vet ./...

build: ## Build binary
	go build -ldflags "$(LDFLAGS)" -o bin/zcp ./cmd/zcp

clean: ## Remove build artifacts
	rm -rf bin/ builds/

###########
# RELEASE #
###########
release: ## Minor bump, test, tag, push (e.g. v2.61.0 → v2.62.0). Use V=x.y.z for explicit version.
	@$(MAKE) _release BUMP=minor

release-patch: ## Patch bump, test, tag, push (e.g. v2.61.0 → v2.61.1). Use V=x.y.z for explicit version.
	@$(MAKE) _release BUMP=patch

_release:
	@if [ -n "$$(git diff --name-only 2>/dev/null)$$(git diff --cached --name-only 2>/dev/null)" ]; then \
		echo "ERROR: working tree is dirty. Commit first."; exit 1; \
	fi; \
	echo "Fetching remote tags..."; \
	git fetch --tags --force || { echo "ERROR: cannot fetch tags from remote"; exit 1; }; \
	if [ -n "$(V)" ]; then \
		NEXT="v$$(echo '$(V)' | sed 's/^v//')"; \
	else \
		LATEST=$$(git tag -l 'v*' --sort=-v:refname | head -1); \
		if [ -z "$$LATEST" ]; then echo "ERROR: no existing tags found"; exit 1; fi; \
		MAJOR=$$(echo "$$LATEST" | sed 's/^v//' | cut -d. -f1); \
		MINOR=$$(echo "$$LATEST" | sed 's/^v//' | cut -d. -f2); \
		PATCH=$$(echo "$$LATEST" | sed 's/^v//' | cut -d. -f3); \
		if [ "$(BUMP)" = "minor" ]; then \
			NEXT="v$$MAJOR.$$((MINOR + 1)).0"; \
		else \
			NEXT="v$$MAJOR.$$MINOR.$$((PATCH + 1))"; \
		fi; \
	fi; \
	LATEST=$${LATEST:-$$(git tag -l 'v*' --sort=-v:refname | head -1)}; \
	COMMITS=$$(git rev-list "$${LATEST:-HEAD}"..HEAD --count 2>/dev/null || echo 0); \
	if [ "$$COMMITS" = "0" ]; then \
		printf "\033[33mWarning:\033[0m no new commits since $${LATEST:-HEAD}\n"; \
		printf "Release \033[1m$$NEXT\033[0m anyway? [y/N] "; \
		read ans; \
		case "$$ans" in [yY]*) ;; *) echo "Aborted."; exit 1;; esac; \
	fi; \
	printf "Running tests...\n"; \
	go test ./... -count=1 -short || { echo "ERROR: tests failed, aborting release."; exit 1; }; \
	echo "Tagging $$NEXT ($$COMMITS commits since $${LATEST:-no previous tag})..."; \
	git tag -a "$$NEXT" -m "Release $$NEXT"; \
	echo "Pushing..."; \
	git push origin HEAD "$$NEXT"; \
	echo "Done: $$NEXT pushed. GitHub Actions will build and publish."

########
# E2E  #
########
ZCPX_HOST ?= zcpx
ZCPX_SSH  := ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ServerAliveInterval=30 -o ServerAliveCountMax=60

e2e-build: ## Cross-compile E2E test binary for zcpx (linux/amd64)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -tags e2e -o builds/e2e-test ./e2e/

e2e-deploy: e2e-build ## Deploy E2E test binary to zcpx
	@echo "==> Deploying E2E test binary to $(ZCPX_HOST)..."
	@scp -o StrictHostKeyChecking=no builds/e2e-test $(ZCPX_HOST):/var/www/e2e-test
	@$(ZCPX_SSH) $(ZCPX_HOST) "chmod +x /var/www/e2e-test"
	@echo "==> E2E binary deployed"

e2e-zcpx: e2e-deploy ## Run ALL E2E tests on zcpx (includes deploy + subdomain)
	$(ZCPX_SSH) $(ZCPX_HOST) "/var/www/e2e-test -test.v -test.timeout 3600s"

e2e-zcpx-fast: e2e-deploy ## Run fast E2E tests on zcpx (read-only, ~15s)
	$(ZCPX_SSH) $(ZCPX_HOST) "/var/www/e2e-test \
		-test.run 'TestE2E_Events|TestE2E_Process|TestE2E_Scaling|TestE2E_Knowledge|TestE2E_LogSearch' \
		-test.v -test.timeout 120s"

e2e-zcpx-deploy: e2e-deploy ## Run deploy E2E tests on zcpx (~10 min)
	$(ZCPX_SSH) $(ZCPX_HOST) "/var/www/e2e-test \
		-test.run 'TestE2E_Deploy|TestE2E_BuildLogs|TestE2E_DeployPrepare' \
		-test.v -test.timeout 900s"

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
