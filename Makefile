.PHONY: help setup test test-short test-race lint lint-fast lint-local vet build install all clean release release-patch schema-sync catalog-sync e2e-build e2e-deploy e2e-zcp e2e-zcp-fast e2e-zcp-deploy flow-eval-local dc-live dc-live-full dc-live-remote

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

schema-sync: build ## Refresh embedded schemas + version catalog from the public schemas (one fetch)
	./bin/zcp schema sync

catalog-sync: schema-sync ## Alias for schema-sync (kept for backward compatibility)

lint-local: ## Full lint (native only, offline)
	$(LINT) run ./...

vet: ## Run go vet
	go vet ./...

vet-tags: ## Compile-check build-tagged test files (api/e2e) so they can't silently rot
	# go test ./... (default tags) never compiles these, so a signature/struct
	# change leaves them broken + invisible until an operator runs them. go vet
	# type-checks them without executing (no TestMain side effects).
	go vet -tags api ./...
	go vet -tags e2e ./...

build: ## Build binary
	go build -ldflags "$(LDFLAGS)" -o bin/zcp ./cmd/zcp

install: build ## Build + install zcp to /usr/local/bin/zcp (uses sudo on Mac)
	sudo install -m 0755 bin/zcp /usr/local/bin/zcp

flow-eval-local: install ## Run local-mode behavioral scenario (ID=<scenario-id> required)
	@test -n "$(ID)" || (echo "ID=<scenario-id> required, e.g.: make flow-eval-local ID=local-auto-adopt-node-postgres-first-deploy" >&2 && exit 1)
	zcp eval behavioral run-local --id $(ID)

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
# E2E target host inside the eval-zcp project. Default is the `zcp`
# service-stack (the ZCP runtime container itself). Override via
# `ZCP_HOST=<hostname> make e2e-zcp` to point at a different service.
ZCP_HOST ?= zcp
ZCP_SSH  := ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ServerAliveInterval=30 -o ServerAliveCountMax=60

e2e-build: ## Cross-compile E2E test binary for the remote target (linux/amd64)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -tags e2e -o builds/e2e-test ./e2e/

e2e-deploy: e2e-build ## Deploy E2E test binary to $(ZCP_HOST)
	@echo "==> Deploying E2E test binary to $(ZCP_HOST)..."
	@scp -o StrictHostKeyChecking=no builds/e2e-test $(ZCP_HOST):/var/www/e2e-test
	@$(ZCP_SSH) $(ZCP_HOST) "chmod +x /var/www/e2e-test"
	@echo "==> E2E binary deployed"

e2e-zcp: e2e-deploy ## Run ALL E2E tests on $(ZCP_HOST) (includes deploy + subdomain)
	$(ZCP_SSH) $(ZCP_HOST) "/var/www/e2e-test -test.v -test.timeout 3600s"

e2e-zcp-fast: e2e-deploy ## Run fast E2E tests on $(ZCP_HOST) (read-only subset)
	$(ZCP_SSH) $(ZCP_HOST) "/var/www/e2e-test \
		-test.run 'TestE2E_Events|TestE2E_Discover|TestE2E_Export_|TestE2E_Knowledge|TestE2E_APIErrorMeta_ValidateZeropsYaml|TestE2E_APIErrorMeta_TransportFailure_NotReclassified|TestE2E_PruneServiceMetas' \
		-test.v -test.timeout 120s"

e2e-zcp-deploy: e2e-deploy ## Run deploy E2E tests on $(ZCP_HOST) (~10 min)
	$(ZCP_SSH) $(ZCP_HOST) "/var/www/e2e-test \
		-test.run 'TestE2E_Deploy|TestE2E_FailureClassification|TestE2E_DeployPrepare' \
		-test.v -test.timeout 900s"

zcp-dev-deploy: linux-amd ## Install the locally-built zcp on $(ZCP_HOST) + re-init (dev loop — no release; VPN up first)
	@scp -o StrictHostKeyChecking=no builds/zcp-linux-amd64 $(ZCP_HOST):/tmp/zcp-dev
	@$(ZCP_SSH) $(ZCP_HOST) "sudo install -m 0755 /tmp/zcp-dev /usr/local/bin/zcp && rm -f /tmp/zcp-dev && zcp version && cd /var/www && zcp init"
	@echo "==> dev zcp installed on $(ZCP_HOST) + init done — reload the code-server window to activate the new extension"

# Full welcome auth-bridge loop against a REAL code-server (this container or
# a remote one over its subdomain): a localhost page stands in for the Zerops
# GUI — embeds code-server, receives the broadcast trigger, validates it by
# the same contract the frontend receiver pins, and acks. Proves
# click → gate → broadcast → receive → ack → phase without the cloud GUI.
# Runbook: tools/welcome-bridge-harness/README.md (docs/spec-local-dev.md).
welcome-bridge-e2e: ## Welcome auth-bridge E2E vs a real code-server (ZCP_CS_URL + ZCP_CS_PASSWORD required)
	cd tools/welcome-bridge-harness && npm install --silent && node run.mjs

# Deterministic, secret-free self-test of the harness's OWN §4.3 embed
# command-channel scenario matrix (docs/spec-welcome-mode.md §4.3): drives a
# locally-generated stub embed double (no live code-server, no
# ZCP_CS_URL/PASSWORD) through every scenario and asserts the observable
# outcome. Proves the RIG's drivers/assertions, not the product — that's
# internal/content/welcomejs/'s own suite. Runbook + the RED (old-contract)
# proof + the live-invocation matrix: tools/welcome-bridge-harness/README.md.
welcome-bridge-selftest: ## Deterministic welcome-bridge §4.3 scenario battery (no live rig, no secrets)
	cd tools/welcome-bridge-harness && npm install --silent
	cd tools/welcome-bridge-harness && SELFTEST_CONTRACT=new MODE=launch node run.mjs
	cd tools/welcome-bridge-harness && SELFTEST_CONTRACT=new MODE=reload node run.mjs
	cd tools/welcome-bridge-harness && SELFTEST_CONTRACT=new MODE=set-mode DIRECTIVE=standard node run.mjs
	cd tools/welcome-bridge-harness && SELFTEST_CONTRACT=new MODE=set-mode DIRECTIVE=onboarding node run.mjs
	cd tools/welcome-bridge-harness && SELFTEST_CONTRACT=new MODE=launch-failed node run.mjs
	cd tools/welcome-bridge-harness && SELFTEST_CONTRACT=new MODE=launch-idempotent node run.mjs
	cd tools/welcome-bridge-harness && SELFTEST_CONTRACT=new MODE=launch-eventid-reuse node run.mjs
	cd tools/welcome-bridge-harness && SELFTEST_CONTRACT=new MODE=no-directive node run.mjs

#####################
# DATA CONSOLE LIVE #
#####################
# Live data-plane conformance suite for the Data Console: dials managed
# engines directly over the project VPN (no ZCP_API_KEY). Full runbook +
# DC_LIVE_CONFIG JSON shape: internal/dataconsole/console/provider/
# conformance/doc.go. Needs `zcli vpn up <projectId>` first.
DC_LIVE_CONFIG   ?= dc-live-config.json
DC_LIVE_SUMMARY  ?= dc-live-summary.json
DC_LIVE_REVISION := $(shell git rev-parse HEAD)
# The release manifest: one typed hostname=baseType entry per full+view-only
# type on zcp-eval-clean (11 — spec-dataconsole-testing.md §5). Override for
# a partial run against a different subset.
DC_LIVE_MANIFEST ?= db=postgresql,mariadb=mariadb,ch=clickhouse,cache=valkey,storage=object-storage,es=elasticsearch,search=meilisearch,docs=typesense,vectors=qdrant,events=kafka,queue=nats

dc-live: ## Run Data Console live conformance (partial profile; needs VPN up + DC_LIVE_CONFIG)
	DC_LIVE_CONFIG=$(abspath $(DC_LIVE_CONFIG)) DC_LIVE_REVISION=$(DC_LIVE_REVISION) DC_LIVE_SUMMARY=$(DC_LIVE_SUMMARY) go test -tags e2e -count=1 ./internal/dataconsole/console/provider/conformance/

dc-live-full: ## Run Data Console live conformance (full profile release gate; DC_LIVE_MANIFEST defaults to all 11 typed engines)
	@test -n "$(DC_LIVE_MANIFEST)" || (echo "DC_LIVE_MANIFEST=<hostname>=<baseType>[@version][,...] required, e.g.: make dc-live-full DC_LIVE_MANIFEST=db=postgresql,cache=valkey,storage=object-storage" >&2 && exit 1)
	DC_LIVE_CONFIG=$(abspath $(DC_LIVE_CONFIG)) DC_LIVE_PROFILE=full DC_LIVE_MANIFEST=$(DC_LIVE_MANIFEST) DC_LIVE_REVISION=$(DC_LIVE_REVISION) DC_LIVE_SUMMARY=$(DC_LIVE_SUMMARY) go test -tags e2e -count=1 ./internal/dataconsole/console/provider/conformance/

# The canonical release run: executes ON the container over SSH (in-project
# network + REST creds — no VPN, no local DC_LIVE_CONFIG). DC_REMOTE_HOST is
# the ssh alias (separate from ZCP_HOST/ZCP_SSH above, which disable host key
# checking — this target deliberately relies on the operator's known_hosts).
DC_REMOTE_HOST ?= zcp

dc-live-remote: ## Run Data Console live conformance ON the zcp container over SSH (canonical release run; no VPN needed). DC_REMOTE_HOST overrides the ssh alias (default zcp).
	DC_REMOTE_HOST=$(DC_REMOTE_HOST) DC_LIVE_PROFILE=full DC_LIVE_MANIFEST=$(DC_LIVE_MANIFEST) DC_LIVE_REVISION=$(DC_LIVE_REVISION) bash scripts/dc-live-remote.sh

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
