# Capture Inspector v1 — architectural isolation and finish plan

Status: implemented and verified; ready for the v1 checkpoint

Implementation note (2026-07-14): V1.0–V1.3 are complete. The optional tagged
browser gate lives in `internal/captureinspector/browsertest` and uses a synthetic
finalized capture; Playwright remains outside the Go/runtime dependency graph.

## 1. Decision

Keep Capture Inspector in the main `zcp` binary, but make it a compiler-fenced,
cold CLI-only domain. The stable command remains:

```text
zcp capture ui ...
```

The binary co-location avoids recorder/reader format skew and keeps behavioral
eval operation atomic. It does not authorize any dependency from the MCP server
or core workflow into the inspector.

Target package layout:

```text
internal/capture/                                  canonical recorder/read contract
internal/captureinspector/                         tiny public facade
internal/captureinspector/internal/projection/     metadata projection/detail readers
internal/captureinspector/internal/web/            loopback HTTP + embedded UI
cmd/zcp/capture_ui.go                              sole composition entrypoint
```

`internal/capture/` remains outside the inspector domain. It is already a
cross-cutting recording side channel used by MCP transport, workflow provenance,
and eval. Import direction is strictly:

```text
cmd/zcp/capture_ui.go -> captureinspector facade -> internal/web
internal/web -> internal/projection -> capture
```

`capture` never imports `captureinspector`. The Go nested `internal/` rule makes
projection and web unimportable from `internal/server`, `tools`, `ops`,
`workflow`, other commands, or external modules.

## 2. Why not another binary or build tags

A separate binary introduces distribution and canonical-format version skew in
the eval path. Build tags create a second build matrix and make it unclear
whether an installed `zcp` can inspect the evidence it recorded. The embedded
assets are approximately 140 KB and the web implementation uses only the Go
standard library; there is no demonstrated dependency or binary-size reason to
split it.

Revisit this decision only if the inspector acquires a large runtime dependency
or an independently versioned deployment lifecycle. A test-only browser
dependency is not a reason to split the production binary.

## 3. Boundary invariants

### I1 — dependency direction

- Only `cmd/zcp/capture_ui.go` and the inspector subtree may import the facade.
- Nested projection and web packages are compiler-private to the facade subtree.
- Inspector internal imports are allowlisted to self and `internal/capture`.
- `internal/capture`, `internal/server`, tools, workflow, ops, integration, and
  e2e never import the inspector.

### I2 — cold import

Importing the facade has no observable runtime behavior:

- no `init()` functions;
- no package-level call-expression initializers;
- no listeners, goroutines, file reads, environment reads, or process changes
  before explicit `Start`;
- `//go:embed` storage without an initializer is the sole package-level asset
  declaration exception.

### I3 — CLI owns process behavior

Signal handling and browser process execution remain in
`cmd/zcp/capture_ui.go`. Inspector packages do not import `os/exec` or
`os/signal`, mutate environment variables, or write stdout/stderr.

### I4 — MCP non-interference

Normal MCP startup cannot reach inspector code. MCP stdout remains JSON-RPC
only, transport framing is unchanged, and no inspector listener exists unless
`zcp capture ui` is selected explicitly.

### I5 — immutable/read-only evidence

The inspector may read validated canonical capture files and disposable caches.
It exposes no canonical write endpoint. Invalid captures disable plaintext/raw
detail. Running captures use only a durable complete prefix.

### I6 — local security

Loopback validation, one-time capability launch, exact Origin/Host checks,
SameSite HTTP-only cookies, explicit reveal, strict CSP, bounded requests,
symlink-safe paths, and no external assets remain mandatory.

`--active` is the only read-only contact with live capture state: the CLI asks the
capture manager for the active session directory and immediately closes the
connection. The web/projection domain itself only receives paths.

## 4. Enforcement

Use three independent levels:

1. Go nested `internal/` compiler enforcement for projection/web.
2. Architecture AST tests that:
   - allow the facade import only from the exact CLI adapter;
   - enforce the inspector outgoing allowlist and reverse deny;
   - reject production `init()` and package-level call initializers;
   - reject process-control imports in the inspector;
   - prove scanners fire on synthetic fixtures.
3. Matching `.golangci.yaml` depguard rules so normal lint fails early.

Existing MCP transport/stdout tests and the full repository test suite are the
behavioral regression net.

## 5. Delivery phases

### V1.0 — RED boundary contract

- Add architecture tests for the target layout and import laws.
- Add scanner self-tests.
- Run the focused package test and record the expected failure while legacy flat
  packages still exist.

### V1.1 — mechanical package migration

- Move `captureview` to `captureinspector/internal/projection`.
- Move `captureui` and assets to `captureinspector/internal/web`.
- Rename Go package identifiers to `projection` and `web`.
- Add a tiny facade with `Config`, `Start`, `LaunchURL`, `URL`, and `Close`.
- Change only `cmd/zcp/capture_ui.go` to import the facade.
- Preserve CLI flags, HTTP API, projection format, assets, and behavior.

Gate: focused tests, architecture tests, and package lint are green.

### V1.2 — lint/spec enforcement

- Add depguard rules matching the tests.
- Update architecture and capture-inspector specs to name the compiler fence,
  cold-path contract, and `--active` exception.
- Remove all stale flat-package references.

Gate: no production import or durable spec references the legacy flat package
paths; only migration tests/plans may name them.

### V1.3 — first-version UX/security gate

- Preserve the single flow-detail workspace, formatted nested payloads, no
  nested scroll ownership, responsive layouts, and strict CSP.
- Keep the no-inline-style/event-handler regression test.
- Run browser smoke checks at 1024 and 2560 px: no stacked drawers, no CSP/page
  errors, no plaintext before reveal, and no body overflow.
- Verify path traversal/symlink, Host/Origin, reveal, invalid capture, and
  one-time launch tests remain green.

### V1.4 — final verification/checkpoint

- `go test ./... -short -count=1`
- targeted race tests for facade/web/projection and server transport
- `go vet ./...`
- `make lint-fast`
- full golangci lint on inspector packages
- JS syntax and `git diff --check`
- manual review over all four real captures
- binary-size report and cold-path import audit

No release, install, or canonical `/usr/local/bin/zcp` mutation.

### V1 acceptance result — 2026-07-14

- Full short suite, targeted race suite, vet, fast lint, inspector full lint,
  JavaScript syntax checks, tagged Playwright smoke, and diff hygiene passed.
- `internal/server` has zero transitive `captureinspector` dependencies; only
  `cmd/zcp/capture_ui.go` imports the facade.
- The final binary is 30,889,490 bytes, +24,160 bytes (+0.08%) versus the
  pre-isolation same-UI build. No separate binary or build tag is justified.
- The browser harness traversed Cards, Flow, and Split for all four real capture
  windows without CSP/page errors:

| Capture | Cards | Nodes | Edges | Error nodes | Different edges | Rewrite/reset edges | Phases |
|---|---:|---:|---:|---:|---:|---:|---:|
| recovery | 52 | 81 | 104 | 1 | 1 | 1 | 2 |
| greenfield | 70 | 107 | 120 | 0 | 0 | 1 | 2 |
| recipe/adopt | 49 | 58 | 65 | 0 | 0 | 10 | 12 |
| weather | 38 | 57 | 71 | 0 | 0 | 1 | 2 |

The recovery acceptance retains the explicit error node and `DIFFERENT` result
edge. The synthetic tagged gate additionally verifies pre-reveal denial, hostile
markup escaping, keyboard edge selection, one-detail ownership, and 1024/2560
px layouts.

## 6. Explicit non-goals for this checkpoint

- separate inspector binary;
- build-tag variants;
- changing provider/MCP recorder behavior;
- hot attachment or reboot supervision;
- semantic grading or a quality score;
- exact timestamp reconstruction inside historical gzip SSE chunks;
- 250 MB/1 GB optimization before a measured benchmark demonstrates a need.
