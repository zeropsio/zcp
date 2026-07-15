# Capture Inspector audit — Tests and verification review

## Verdict

**Test execution is broad and mostly green, but the test architecture misses multiple forensic state-machine faults and CI itself fails. Overall lane: FAIL.**

## Executed gates

| Check | Base | Head | Result |
|---|---:|---:|---|
| Native build | PASS | PASS | PASS |
| `go test ./... -short -count=1` with identical synced knowledge | PASS | PASS | PASS |
| Focused capture/Inspector/server/eval/workflow tests | n/a | PASS | PASS |
| Focused race | n/a | PASS | PASS |
| `go test ./... -race -count=1` | not repeated | PASS | PASS |
| `go vet ./...` | not repeated | PASS | PASS |
| `go vet -tags api ./...` | not repeated | PASS | PASS |
| `go vet -tags e2e ./...` | not repeated | PASS | PASS |
| Linux amd64 build | not repeated | PASS | PASS |
| Darwin arm64 build | not repeated | PASS | PASS |
| Windows amd64 build | PASS | FAIL | FAIL (`CI-001`) |
| `make lint-fast` | PASS | PASS | PASS |
| `make lint-local` | PASS, 0 | FAIL, 74 | FAIL (`CI-002`) |
| Inspector-only full lint | n/a | PASS, 0 | PASS |
| JavaScript syntax | n/a | PASS | PASS |
| `git diff --check` | n/a | PASS | PASS |
| Tagged synthetic Playwright | n/a | PASS | PASS |
| Tagged real-corpus Playwright | n/a | PASS | PASS |

The initial clean-worktree short suites failed at both revisions because synchronized gitignored knowledge Markdown was absent. Those environmental logs are retained. The qualified runs used the same local corpus in both controls, mirroring CI's sync precondition.

## Focused statement coverage

Coverage was recorded as inventory, not as a quality gate:

- `internal/capture`: 70.9%
- `internal/captureinspector`: 85.7%
- projection: 66.7%
- web: 51.9%
- server: 68.5%
- eval: 56.2%
- workflow: 85.4%

Evidence: `tmp/capture-inspector-audit-2026-07-15/focused-coverage.log`.

## Architecture-test quality

The boundary suite passed unmodified and failed all three injected mutations. This proves the direct import, nested-private, and cold-initializer scanners are not ceremonial.

Missing architecture visibility: direct-import tests do not report the broad transitive closure through shared `internal/capture`. The runtime control compensates for activation risk, but a closure report should remain part of review evidence.

## Browser-test quality

The tagged harness covered:

- one-time launch and pre-reveal denial;
- hostile-markup non-execution;
- Cards, Flow, and Split;
- keyboard edge selection and Escape;
- one inspector/drawer ownership;
- page-scroll preservation;
- 1024 and 2560 px overflow/layout checks;
- all four real captures;
- exact expected Cards/nodes/edges/error/difference/rewrite/phase counts;
- no page/console errors.

The tagged test is not run by default CI, so browser regressions can merge unless an explicit job invokes it with provisioned Playwright. That is a test-deployment gap, not a failure of the harness itself.

## Differential and positive probes

Passed:

- exact 184-byte disabled MCP transcript at base/head;
- normal CLI output/exit/side effects for three safe commands;
- cold-import runtime equivalence;
- partial MCP reader/writer `(n, err)` and accepted-prefix capture;
- five-class pre-reveal body-taint exclusion;
- launch-token history probe;
- real manager `on/status/off` happy path in a private sandbox;
- 107-file real-corpus read-only proof;
- 3,648-record credential-header structural scan.

## Audit RED probes that expose missing permanent coverage

The following deterministic tests fail at the audited head and should become permanent before fixes:

1. readiness failure must not orphan a daemon (`LIFE-001`);
2. late drain error must downgrade status (`LIFE-002`);
3. manifest ID must match provider/lifecycle IDs (`INT-001`);
4. symlink-parent evidence must be rejected (`INT-004`);
5. provider and MCP stream starts are mandatory (`INT-002`);
6. open lifecycle scope prevents completeness (`INT-003`);
7. incomplete SSE block is not a completed tool (`CORR-002`);
8. mismatched arguments do not become proven causality (`CORR-003`);
9. whole MCP/provider result evidence is required for exactness (`CORR-001`);
10. every known metric carries evidence coordinates (`EVID-001`);
11. graph basis cannot claim argument equality when false (`CORR-003`);
12. manifest symlinks are rejected during scan (`WEB-003`);
13. pinned/root duplicate IDs fail as ambiguous (`WEB-002`);
14. restored-metadata tampering invalidates cached integrity (`WEB-001`);
15. reveal rejects trailing JSON (`SEC-001`);
16. capture-off eval discovery does not create state (`ISO-002`).

Real-corpus analysis independently reproduces `PROJ-001` and `EVID-001`.

Probe source snapshots and logs are under `tmp/capture-inspector-audit-2026-07-15/probe-sources/`; no audit probe remains in production worktrees.

## Why existing tests missed the defects

- Stream tests are producer-happy-path fixtures, not consumer state-machine adversaries.
- Integrity tests mutate hashes/size but not same-size content with restored metadata after cache population.
- Correlation fixtures use text-only results and matching arguments.
- Projection tests assert selected IDs/edges, not global uniqueness or truth of every edge basis.
- Lifecycle tests close all nested scopes before stream close.
- Manager tests model successful control shutdown after readiness and do not assert owned-process cleanup on every rollback branch.
- Runtime tests inject errors before `Close`, not while `Shutdown` is draining.
- Eval capture tests assert active overrides but do not compare capture-off subprocess environment/filesystem behavior to base.
- Local verification used fast/scoped lint; CI's full lint behavior was not reproduced in the handoff claim.

## Unmeasured areas

Because the verdict is already NO-GO and operator policy prohibited risky/live operations, the audit did not claim evidence for:

- real provider/Zerops credentials and network traffic;
- actual power loss/`SIGKILL` at each journal phase;
- real disk-full/filesystem corruption;
- Linux runtime process supervision (Linux compilation passed);
- Windows runtime (compilation failed first);
- 250 MB/1 GB files or gzip decompression bombs;
- long-running cache memory/eviction behavior.

These require explicit owner-approved follow-up and cannot be silently converted into PASS.
