# Capture Inspector audit — R2 lint remediation verification

This addendum responds to the independent verdict frozen in
`13-r1-independent-verdict.md`. It preserves that report and its `NO-GO`
verdict unchanged. It records remediation evidence for a subsequent independent
aggregate review and does not authorize release or installation.

## `CI-002-R2` remediation

Commit `65261ad2` declares the package-level production constant
`propagationClientResult = "client-result"` and uses it at both production
sites and in the same-package projection test. The exact literal now occurs
only once, at the constant declaration, so test traversal/order can no longer
create a three-literal `goconst` group.

## Fresh-cache repeatability

Each run below used a newly created, otherwise empty
`GOLANGCI_LINT_CACHE` directory. Tests remained enabled (the golangci-lint
v2.8.0 default).

| Series | Runs | Result |
|---|---:|---:|
| Native `./bin/golangci-lint run ./... --enable-only goconst` | 10 | 10 PASS / 0 FAIL |
| Linux/amd64 `goconst`-only | 10 | 10 PASS / 0 FAIL |
| Native full `./bin/golangci-lint run ./...` | 10 | 10 PASS / 0 FAIL |
| Linux/amd64 full lint | 10 | 10 PASS / 0 FAIL |

The exact project targets also passed:

```text
make lint-local
make lint
```

The repeat logs are retained in the gitignored directory
`tmp/capture-inspector-r2-remediation-2026-07-15/`.

## Fresh regression matrix

The following passed after the correction:

```text
go test ./... -short -count=1
go test -race ./... -count=1
go vet ./...
go vet -tags api ./...
go vet -tags e2e ./...
GOOS=linux  GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/zcp
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/zcp
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/zcp
node --check internal/captureinspector/internal/web/assets/app.js
node --check internal/captureinspector/browsertest/smoke.cjs
go test -tags captureinspector_browser \
  ./internal/captureinspector/internal/web -run '^TestBrowserSmoke$' -count=1
git diff --check
```

A fresh real-corpus Playwright run completed over all five captures with no
page errors. A separate API pass found all five captures valid and complete,
112 metrics per capture with bounded non-empty evidence for each known metric,
and no duplicate projected MCP call IDs.

Before/after SHA-256 and mode/size/mtime inventories of all 82 canonical capture
files were identical. No capture, installed or canonical binary, release,
remote service, live credential, or platform state was changed.

## Status

The `client-result` reproduction is locally closed across 40 independent
fresh-cache lint samples plus the exact project targets. The candidate is ready
for a fresh independent aggregate review. The formal verdict remains `NO-GO`
until that review is completed; the bounded unexercised limitations listed in
`13-r1-independent-verdict.md` remain unchanged.
