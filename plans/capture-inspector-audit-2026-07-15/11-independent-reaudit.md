# Capture Inspector audit — independent aggregate re-review

Date: 2026-07-15

## Scope and verdict

This review is frozen to the remediation range:

```text
b5bd502ad8ab31d854bb938460612936e221887c
...
5e5c693813eef205bfdcdee9b0354e18202b31fe
```

The implementation response is materially better, but it is **not sufficient for
GO**. The independent verdict remains **NO-GO**.

Fifteen of the nineteen prior ledger findings have enough implementation and
runtime evidence to be treated as closed at this head. Four are not fully
closed: `CI-002` is reopened by the required Linux lint gate, and the root-cause
acceptance criteria for `INT-002`, `INT-003`, and `INT-004` remain incomplete.
Three deterministic audit-only probes still produce false integrity-complete or
out-of-root results.

The historical verdict in `09-final-verdict.md` remains frozen. This document is
the aggregate decision on the later GO candidate; it does not rewrite history or
authorize release.

## Blocking findings

### `INT-002-R1` — Blocker — protocol transitions are still not validated

The new stream validator correctly requires the expected start at sequence 1,
rejects duplicate starts, and requires a final terminal record. It still does
not validate legal provider exchange transitions. In particular:

- `validateInspectionRecordSequence` validates only the outer stream envelope
  (`internal/capture/inspect_provider.go:284`);
- provider reconstruction accepts a request start/end with neither a response
  nor an exchange error because a missing response start is skipped
  (`internal/capture/inspect_provider.go:211-213`).

An audit fixture containing `session.start`, `request.start`, `request.end`, and
`session.end(status=complete)`, with a matching terminal manifest, returned
`Integrity.Complete=true`. This is an open provider exchange presented as a
complete capture. It violates the no-silent-loss and completeness distinction in
`docs/spec-capture-inspector.md` section 2 and the request/response validation
order in section 8.

The exact terminal-only regression is fixed, but the prior required correction
also called for legal record-state transitions. That root cause remains open.

### `INT-003-R1` — Blocker — lifecycle hierarchy can close out of order

The lifecycle parser now rejects unknown parents and duplicate starts and marks
unclosed scopes incomplete. It does not model parent open/closed state. A run can
end and then gain a scenario; similarly, parent ends do not require children to
be closed first (`internal/capture/inspect_lifecycle.go:70-99`).

An audit fixture emitted:

```text
stream.start → eval.run.start → eval.run.end → scenario.start →
scenario.end → stream.end(status=complete)
```

Inspection accepted the impossible hierarchy as valid and complete. This is a
false top-level integrity claim under the lifecycle hierarchy and incomplete
annotation contract in `docs/spec-capture-inspector.md` section 5.1.

The exact open-scope regression is fixed, but the required nested lifecycle
state validation is incomplete.

### `INT-004-R1` — Blocker — legacy inspection still follows symlink evidence

Manifest-backed paths now pass through component-wise `Lstat` checks. The
supported pre-manifest path does not:

- legacy files are assembled with direct joins/globs
  (`internal/capture/inspect.go:446-476`);
- manifest file validation, including regular-file checks, returns immediately
  for a legacy capture (`internal/capture/inspect.go:515-517`).

An audit fixture placed `provider.jsonl` and `lifecycle.jsonl` symlinks in a
legacy session, both targeting a different capture directory. `InspectSession`
followed them and returned `valid=true, complete=true`.

This contradicts the regular, contained evidence contract in
`docs/spec-capture-inspector.md` sections 7-8. It also makes the remediation
addendum's claim that core inspection rejects symlinked ancestors/components
broader than the implementation.

The exact manifest-parent regression is fixed, but the shared core reader still
has an accepted symlink path.

### `CI-002-R1` — Major — required cross-platform lint is red

`make lint-local` passes. The required `make lint` target does not:

```text
GOOS=linux GOARCH=amd64 ./bin/golangci-lint run ./...
internal/captureinspector/internal/projection/raw.go:460:7:
string `builtin` has 3 occurrences, make it a constant (goconst)
```

Darwin lint passes; execution stops at Linux, so Windows lint is not reached.
The candidate therefore does not satisfy the repository/CI-equivalent quality
gate.

## Prior-finding disposition

| Finding(s) | Re-review disposition |
|---|---|
| `CI-001` | Verified closed: Linux, Darwin, and Windows builds pass. |
| `CI-002` | **Open/reopened:** full Linux lint fails with one issue. |
| `LIFE-001`, `LIFE-002` | Verified closed by implementation review, rollback/late-error regressions, and an isolated manager `on/status/off` run. |
| `INT-001` | Verified closed: manifest identity is propagated to provider, lifecycle, MCP, and provenance validation. |
| `INT-002` | **Partially closed:** outer start/terminal checks pass; legal exchange transitions remain unvalidated. |
| `INT-003` | **Partially closed:** missing terminal scopes downgrade completeness; illegal nested ordering is accepted. |
| `INT-004` | **Partially closed:** manifest-backed symlink fixture is rejected; supported legacy discovery still follows symlinks. |
| `CORR-001`–`CORR-003` | Verified closed by normalized structural equality, conservative argument correlation, and graph-basis tests/review. |
| `PROJ-001` | Verified closed: zero duplicate MCP IDs across all five real captures. |
| `EVID-001` | Verified closed: all 112 known metrics per real capture carry 1-64 evidence references. |
| `WEB-001`–`WEB-003` | Verified closed by restored-mtime tamper, duplicate-ID, manifest-symlink, and browser/API regressions. |
| `SEC-001` | Verified closed: reveal requires EOF after the one allowed JSON object. |
| `ISO-001`, `ISO-002` | Verified closed by capture-off HOME/state tests and cold runtime proof. |

## Gate evidence

| Gate | Result |
|---|---|
| `go test ./... -short -count=1` | PASS |
| `go test -race ./... -count=1` | PASS |
| `go vet ./...`, `-tags api`, `-tags e2e` | PASS |
| Linux amd64, Darwin arm64, Windows amd64 builds | PASS |
| `make lint-local` | PASS |
| `make lint` | **FAIL — Linux `goconst`** |
| JavaScript syntax and `git diff --check` | PASS |
| Tagged synthetic Playwright smoke | PASS |
| Real-corpus CLI/API/Playwright traversal | PASS over five captures |
| Real metric evidence and MCP ID uniqueness | PASS: 0 missing/over-limit evidence; 0 duplicate IDs |
| Canonical read-only proof | PASS: bytes and mode/size/mtime unchanged |
| Credential-header structural scan | PASS: 0 occurrences in 5,549 provider records |
| Inspector cold import | PASS: no files, child processes, or network descriptors |
| Three deliberate architecture mutations | PASS: all three were rejected |
| Isolated manager `on/status/off` | PASS: settings restored, daemon exited, manifest completed |
| Three aggregate adversarial probes above | **RED as expected: all three reproduced** |

Gitignored evidence is under
`tmp/capture-inspector-reaudit-2026-07-15/`. The RED source is preserved at
`probe-sources/audit_aggregate_probe_test.go`; its output is in
`aggregate-red-probes.log`. No audit probe remains in the detached worktree.

## Required before another GO review

1. Promote the three aggregate probes into permanent production regressions.
2. Implement an explicit provider/MCP record state machine: allowed kinds,
   exchange start/end/error exclusivity, no open request at a complete terminal,
   and request/response ordering.
3. Implement lifecycle open/closed state transitions, including child-before-
   parent-close and duplicate end/bind rejection.
4. Apply the same symlink-safe resolver and regular-file policy to supported
   legacy discovery, including MCP and provenance glob results.
5. Fix the Linux lint issue and run the complete Darwin/Linux/Windows lint target.
6. Repeat the full test/race/build/lint/browser/read-only matrix and obtain a
   fresh independent aggregate verdict.

## Bounded limitations

As in the original audit, this re-review did not use live provider credentials,
perform destructive disk-full/power-loss tests, run Windows behavior on a
Windows host, or execute 250 MB/1 GB/decompression-bomb workloads. Those areas
remain unproven and are not represented as passing.

No production code, installation, release artifact, canonical capture, or live
credential was changed by this review.
