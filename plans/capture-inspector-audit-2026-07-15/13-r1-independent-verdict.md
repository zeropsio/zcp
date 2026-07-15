# Capture Inspector audit — independent R1 verdict

Date: 2026-07-15

## Scope and verdict

This review is frozen to:

```text
5e5c693813eef205bfdcdee9b0354e18202b31fe
...
2c4357289173a41e5345895e08608f71882a717f
```

The three R1 forensic/isolation blockers are independently verified closed.
The candidate is nevertheless **NO-GO** because `CI-002` is not deterministically
closed: the configured Linux/native `goconst` gate intermittently reports a new
three-occurrence `client-result` literal group.

No release or production change is authorized by this review.

## Finding

### `CI-002-R2` — Major — full lint is cache/run-order sensitive

The remediation replaced the three production `builtin` literals with
`toolCategoryBuiltin`. Two production `client-result` literals remain at:

- `internal/captureinspector/internal/projection/client.go:247`
- `internal/captureinspector/internal/projection/raw.go:510`

A third occurrence is in
`internal/captureinspector/internal/projection/view_test.go:62`.
The lint configuration suppresses diagnostics anchored in `_test.go`, but the
test literal still participates in goconst's three-occurrence set. Across fresh
lint caches, the diagnostic is sometimes anchored in the test and suppressed,
and sometimes anchored in production and reported:

```text
internal/captureinspector/internal/projection/client.go:247:25:
string `client-result` has 3 occurrences, make it a constant (goconst)
```

Observed independent results at the same frozen head:

- one exact `make lint-local` run: FAIL;
- the following exact `make lint` Darwin phase: FAIL on the same issue;
- a later clean-Go-cache exact `make lint-local && make lint`: PASS;
- 10 fresh-cache native goconst-only `./...` runs: 3 FAIL, 7 PASS;
- 10 fresh-cache Linux/amd64 goconst-only `./...` runs: 3 FAIL, 7 PASS.

The CI workflow uses golangci-lint v2.8.0 with tests enabled, so a passing local
sample does not make this gate reliably green. `CI-002-R1` is therefore only
partially closed.

Smallest correction: define one production `propagationClientResult` constant
and use it at both production sites (and preferably in the same-package test),
then repeat clean-cache native and Linux lint runs.

## R1 blocker disposition

### `INT-002-R1` — Verified closed

The original unchanged audit probe now passes. The provider validator models
request-open, request-complete, response-open, and terminal phases; open request
or response state forces `Integrity.Complete=false`, while illegal ordering,
foreign kinds, missing exchange IDs, duplicate boundaries, and records after a
terminal are rejected. MCP records are restricted to the declared stream kinds
and process identity.

### `INT-003-R1` — Verified closed

The original unchanged child-after-parent-end probe now passes. The lifecycle
implementation tracks open/closed parent state, requires invocations to close
before scenarios and scenarios before runs, and rejects duplicate run/scenario/
invocation ends and duplicate binds.

### `INT-004-R1` — Verified closed

The original unchanged legacy symlink probe now passes. Legacy provider and
lifecycle files, optional MCP/provenance directories, and their JSONL entries
all pass through component-wise no-symlink resolution and regular-file checks.
The permanent suite also covers directory and file symlinks for both optional
subtrees.

The unchanged audit probe source matched the claimed SHA-256:

```text
acd5f73234dfd98ab41800fad6cdb229158ad162e20b5849e95e13f0acc150c3
```

All three `TestAuditAggregate_*` tests passed after copying that exact source
into the detached worktree and were then removed.

## Independent gate matrix

| Gate | Result |
|---|---|
| `go test ./... -short -count=1` | PASS |
| `go test -race ./... -count=1` | PASS |
| `go vet ./...`, `-tags api`, `-tags e2e` | PASS |
| Linux amd64, Darwin arm64, Windows amd64 builds | PASS |
| JavaScript syntax and `git diff --check` | PASS |
| Synthetic tagged Playwright smoke | PASS |
| Real-corpus CLI/API/Playwright | PASS over all five captures |
| Real capture integrity | 5/5 valid and complete |
| Metric evidence | 112 known metrics per capture; 0 missing or over 64 refs |
| MCP projection identity | 0 duplicate IDs |
| Credential-header structural scan | 0 occurrences in 5,549 provider records |
| Canonical read-only proof | 82/82 files byte/mode/size/mtime identical |
| Exact three audit probes | PASS, byte-identical source |
| `make lint-local`, `make lint` repeatability | **UNSTABLE — both PASS and FAIL observed** |

The first short-suite attempt used a temporary directory nested under another
Git checkout, which invalidated one not-a-repository fixture. It was discarded;
the qualified rerun used an external isolated temp root and passed. Likewise, a
browser attempt without the existing Playwright browser cache was discarded;
the qualified isolated-HOME rerun used the read-only browser cache and passed.
Neither harness failure is attributed to the candidate.

Gitignored evidence is retained under:

```text
tmp/capture-inspector-r1-independent-2026-07-15/
```

Key logs are `original-probes.log`, `lint.log`, `lint-final.log`,
`goconst-full-repeat.log`, `goconst-linux-repeat.log`, `test-short.log`,
`test-race.log`, and the real-corpus browser/API directories.

## Required before GO

1. Remove the `client-result` three-literal goconst group with a production
   constant.
2. Run repeated fresh-cache native and Linux goconst/full-lint gates, not only
   one cached sample.
3. Repeat the normal short/race/vet/build/browser checks and obtain a fresh
   independent verdict.

The bounded limitations from the prior audit remain: no live credentials,
destructive disk-full/power-loss testing, Windows runtime host, or very-large/
decompression-bomb workload was exercised.

No production code, installed binary, canonical capture, or live credential was
changed by this review.
