# Capture Inspector audit — Isolation review

## Verdict

**Facade isolation: PASS. Shared capture non-interference: FAIL. Overall lane: FAIL.**

The cold `internal/captureinspector` domain is genuinely compiler-fenced and inert. The broader feature also modifies shared `internal/capture`, server, workflow, and eval paths; two capture-disabled eval behaviors changed.

## Static boundary proof

### Passed

- Sole production facade importer: `cmd/zcp/capture_ui.go`.
- Nested `captureinspector/internal/projection` and `/web` packages are protected by Go's nested `internal` rule.
- Inspector direct imports satisfy the architecture allowlist.
- No `internal/server`, tools, ops, workflow, eval, integration, or e2e package imports the Inspector facade/private subtree.
- `go list -deps ./internal/server` contains no `captureinspector` package.
- Production Inspector files contain no `init()` or forbidden package-level call initializer.
- Process/signal ownership remains in the CLI adapter.

Mutation checks proved the guards are live:

1. a forbidden server→facade import failed the AST test;
2. a server→private-projection import failed compilation;
3. an injected Inspector `init()` failed the cold-import scanner.

Evidence: `tmp/capture-inspector-audit-2026-07-15/architecture-mutations.log`.

### Transitive closure observation

The Inspector closure is not small despite the direct allowlist:

```text
captureinspector → projection/web → capture → content/platform/runtime → Zerops SDK
```

It does not reach `internal/server`, tools, ops, workflow, or eval. The broad closure comes from Go package granularity in the explicitly allowed shared `internal/capture` package, which combines canonical readers with manager/eval/provenance concerns. This is a design-coupling risk, not evidence of runtime activation by itself.

Evidence: `tmp/capture-inspector-audit-2026-07-15/dependency-closure.log`.

## Runtime cold proof

A control program and an otherwise identical program blank-importing the facade were run with isolated `HOME`, `TMPDIR`, cwd, and environment. Snapshots were identical:

- one goroutine;
- same five file descriptors;
- no listener;
- no child process;
- no filesystem entry;
- no stdout/stderr difference;
- no environment mutation.

Evidence: `tmp/capture-inspector-audit-2026-07-15/cold-import-run/result.txt`.

## Disabled protocol/CLI differential

### Passed

- Base/head MCP initialization transcript: exactly 184 bytes and identical SHA-256.
- Capture-off partial I/O wrappers are not installed.
- `version`, `--version`, and `help` had identical exit codes, stdout, stderr, and filesystem effects.
- Focused server/workflow tests and full race suite passed.

### `ISO-001` — Major — capture-disabled user-simulator HOME changed

At base, the behavioral eval user simulator inherits parent `HOME`. At head, `Runner.userSimRunner` passes `r.claudeEnv()` (`internal/eval/behavioral_run.go:243`), so a configured sandbox `ClaudeHome` becomes the user simulator's `HOME` even when `Capture == nil`.

The base/head subprocess probe observed parent HOME at base and sandbox HOME at head. This can change Claude authentication/configuration, memory, and behavior in an existing non-capture eval path. It is not required merely to inject capture variables when capture is active.

Evidence: `tmp/capture-inspector-audit-2026-07-15/eval-home-differential.log`.

### `ISO-002` — Minor — capture-off eval discovery writes manager state

`activeEvalCapture` falls back to `newDefaultCaptureManager` and `ActiveConnection` on every normal eval when scoped capture variables are absent (`cmd/zcp/eval.go:312–327`). Construction creates a private control directory; manager locking creates the state tree and `lifecycle.lock` even when capture is OFF.

The isolated probe created:

```text
~/.local/state/zcp/capture-runtime/lifecycle.lock
$TMPDIR/zcp-capture-<uid>/
```

No recorder/listener/daemon started, but a disabled normal eval path now mutates capture-owned filesystem state. `capture ui --active` while OFF has the same ancillary write behavior. The narrow read-only active-state exception should not require creation.

Evidence:

- `tmp/capture-inspector-audit-2026-07-15/eval-off-state-side-effect-red.log`
- `tmp/capture-inspector-audit-2026-07-15/ui-active-off-side-effects/`

## Binary/dependency cost

The audited feature range adds four Go packages and 2,373,216 bytes to the native binary. This range includes the complete recorder/manager/inspection feature, not only the final isolation move, so it must not be compared with the handoff's same-feature pre/post-isolation delta.

## Smallest isolation correction

- Preserve base user-simulator environment exactly when capture is nil; add only capture overrides in the active branch.
- Make OFF discovery read-only before creating lock/control directories, or move global-active discovery behind a non-mutating existence check followed by the locked path only when state exists.
- Add transitive-closure reporting to the architecture test as a visibility check, while keeping the direct compiler fence as the enforceable boundary.
