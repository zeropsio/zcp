# Capture Inspector audit — independent Standards review

Lane: repository rules, diff correctness, maintainability, and enforced tooling. This report does not use product-spec compliance to waive standards defects.

## Verdict

**FAIL / NO-GO.** The audited head does not satisfy the repository's own build and lint gates and contains transactional/error-ordering defects confirmed by isolated probes.

## Confirmed findings

### `CI-001` — Major — Windows CI build is broken

The base builds for `windows/amd64`; the audited head does not. `internal/capture/manager.go:525`, `:530`, and `:641` directly use Unix-only flock/process APIs. Additional Unix process-group/session operations are compiled unconditionally in `cmd/zcp/capture.go` and `cmd/zcp/capture_daemon.go`.

The repository CI workflow explicitly builds Linux, Darwin, and Windows. This is a feature-introduced release-gate regression, not an unsupported local experiment.

Deterministic command:

```text
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...
```

Head fails with undefined `unix.Flock`, `unix.LOCK_EX`, `unix.LOCK_UN`, and `syscall.Kill`; base passes.

### `CI-002` — Major — full repository lint adds 74 issues

`make lint-local` reports zero issues at the base and 74 at the audited head. Categories are:

- contextcheck 29
- staticcheck 10
- modernize 9
- noctx 9
- goconst 4
- tagliatelle 4
- usetesting 2
- errchkjson, gocritic, musttag, nilerr, tparallel, unparam, unused: one each

`make lint-fast` and inspector-only full lint pass, but CI uses full golangci-lint `v2.8.0` over `./...`; scoped/fast success is not an alternative gate.

### `LIFE-001` — Major — readiness rollback can discard ownership while leaving the started process alive

Every post-readiness failure in `Manager.On` calls `shutdownReadyDaemon` (`internal/capture/manager.go:253–298`). That helper only attempts a control request and ignores its result (`:506–514`); it never falls back to the configured identity-checked terminate/kill functions or waits for exit. The deferred rollback then removes `active.json`.

A detached RED probe returned valid process readiness with an unavailable control endpoint. `On` failed, `TerminateProcess` was never called, the journal disappeared, and the child remained alive. That turns a recoverable startup failure into an unowned daemon/listener.

### `LIFE-002` — Blocker — runtime snapshots capture errors before the operation that can create them

`Runtime.close` samples `proxy.CaptureError()` at `internal/capture/runtime.go:186`, then drains the proxy at `:200–205`. A capture error raised during that drain is never sampled again. The recorder and manifest can therefore finalize `complete` while `Close` returns `("complete", nil)`.

The deterministic fault probe paused an active upstream exchange, entered shutdown, injected the late capture error, released the exchange, and observed exactly that false terminal result.

### `WEB-001` — Blocker — finalized-view cache trusts restorable file metadata for integrity

`captureViewCacheKey` includes manifest bytes but only size, mtime, and mode for canonical files (`internal/captureinspector/internal/web/server.go:612–645`). A terminal view is cached indefinitely under that key.

The RED probe built a valid cached view, changed canonical evidence without changing size, restored its mtime, and proved a fresh inspection rejected the file. The same server still returned the cached view with `integrity.valid=true`.

Selected detail readers do re-hash and reject the changed file, which limits plaintext exposure, but it does not repair the incorrect integrity result returned by the primary view.

## Positive standards results

- Native base/head builds succeeded.
- Qualified full short suites passed at both revisions.
- Head full race suite passed.
- `go vet ./...`, `go vet -tags api ./...`, and `go vet -tags e2e ./...` passed.
- Linux and Darwin cross-builds passed.
- `make lint-fast` passed at both revisions.
- Inspector-only full lint passed.
- JavaScript syntax checks and `git diff --check` passed.
- Audit probes were removed; detached controls were clean.

These positives do not offset the full CI build/lint failures or confirmed correctness defects.

## Smallest standards remediation

1. Introduce platform-specific process/lock adapters with build constraints and preserve Windows command behavior.
2. Make full `golangci-lint run ./...` clean rather than narrowing CI.
3. Give readiness rollback an owned-process terminate/kill/wait fallback and retain recoverable state until exit is proven.
4. Re-sample component errors after each drain/close and before terminal status is committed.
5. Either re-hash before serving a cached integrity claim or cache a verified immutable identity that cannot be reproduced by size/mtime restoration; bound/evict stale cache entries.

Each audit RED probe should be promoted to a permanent regression test before correction.
