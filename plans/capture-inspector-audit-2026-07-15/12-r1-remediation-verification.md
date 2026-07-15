# Capture Inspector audit — R1 remediation verification

This addendum responds to the independent aggregate re-review frozen at
`5e5c6938` in `11-independent-reaudit.md`. It does not alter that historical
`NO-GO` verdict and does not self-authorize release. It records the production
fixes and local gate evidence prepared for another independent review.

## Blocking finding disposition

| Reopened finding | Remediation |
|---|---|
| `INT-002-R1` | `ea216206` adds an explicit per-exchange provider state machine. Only request start → body* → request end → response start → body* → response end, or an exchange error while the request is pending, is legal. Foreign kinds, duplicate/out-of-order boundaries, records after terminal, and missing exchange IDs are invalid. A stream terminal with an open request/response remains hash-valid but forces `Integrity.Complete=false`. MCP streams now accept only MCP chunk/error kinds between matching process-bound start/end records. |
| `INT-003-R1` | `ea216206` models eval runs and scenarios as open/closed parents. Children cannot start after parent close; invocations must end before scenario close; scenarios before run close; duplicate run/scenario/invocation ends and duplicate invocation binds are rejected. |
| `INT-004-R1` | `ea216206` routes legacy provider/lifecycle paths and MCP/provenance directory entries through the same component-wise `Lstat` resolver and regular-file validation as manifest-backed evidence. `filepath.Glob` was removed so a symlinked optional evidence directory is not traversed even for discovery. |
| `CI-002-R1` | `c0b2e5f9` centralizes the `builtin` projection category constant. Native full lint and the complete Darwin/Linux `make lint` target pass. |

The three exact audit probes were promoted into
`internal/capture/inspect_state_machine_test.go` and expanded with provider
ordering, MCP allowed-kind/process identity, duplicate lifecycle bind/end, and
legacy MCP/provenance directory/file symlink cases.

The original audit-only probe source was moved byte-identically outside the Go
module after promotion so it cannot itself create an invalid package during
`go test ./...` or lint:

```text
/tmp/zcp-capture-inspector-reaudit-probe-sources-2026-07-15/audit_aggregate_probe_test.go
SHA-256 acd5f73234dfd98ab41800fad6cdb229158ad162e20b5849e95e13f0acc150c3
```

Copying that exact source back into `internal/capture` temporarily and running
`go test ./internal/capture -run '^TestAuditAggregate_' -count=1` now passes.

## Fresh verification matrix

The following passed after both remediation commits:

```text
go test ./... -short -count=1
go test -race ./... -count=1
go test ./internal/capture -count=1
go test -race ./internal/capture -count=1
go vet ./...
go vet -tags api ./...
go vet -tags e2e ./...
GOOS=linux  GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/zcp
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/zcp
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/zcp
make lint-local
make lint
node --check internal/captureinspector/internal/web/assets/app.js
node --check internal/captureinspector/browsertest/smoke.cjs
go test -tags captureinspector_browser \
  ./internal/captureinspector/internal/web -run '^TestBrowserSmoke$' -count=1
git diff --check
```

A fresh real-corpus CLI/API/Playwright pass opened all five captures with valid,
complete integrity and no browser/page errors. Each capture still exposes 112
metrics with 1–64 evidence references for every known value; projected MCP call
IDs contain no duplicates. A structural scan found zero credential-header keys
in 5,549 provider records.

Before/after SHA-256 plus mode/size/mtime inventories of all 82 canonical capture
files compare byte-for-byte identical. No production installation, release,
canonical binary replacement, capture rewrite, or live platform mutation was
performed by this remediation.

## Status

The four reported R1 reproductions are locally closed and the candidate is ready
for a fresh independent aggregate review. The verdict remains `NO-GO` until that
review is performed; the bounded limitations from `11-independent-reaudit.md`
remain unchanged.
