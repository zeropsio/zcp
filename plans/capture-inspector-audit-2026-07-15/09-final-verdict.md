# Capture Inspector audit — final verdict

Frozen review: `8f1ef6f29e3b70809a43dbbe6363f0e8dafa03d3...b5bd502ad8ab31d854bb938460612936e221887c`

# Verdict: **NO-GO**

The implementation must not be released or merged as complete in its audited form.

## Finding count

- 7 Blockers
- 9 Majors
- 3 Minors
- 19 confirmed feature-introduced findings total

No production remediation was authorized or performed.

## Independent lane verdicts

| Lane | Verdict | Reason |
|---|---|---|
| Standards | FAIL | Windows CI build fails; full lint adds 74 issues; manager/runtime/cache correctness defects |
| Spec | FAIL | incomplete/mixed evidence can be valid/complete; exact propagation and causal basis can be false; metrics lack evidence |
| Isolation | FAIL | facade/cold path pass, but capture-disabled eval HOME and filesystem behavior changed |
| Security/Evidence | FAIL | symlink/identity/cache defects undermine canonical trust; browser access gates otherwise pass |
| Codebase design | FAIL for shipment | strong facade, but duplicated parsers, weak lifecycle/equality seams, and platform/error ownership cause confirmed faults |
| Tests | FAIL | broad native/race/browser coverage passes, but CI build/lint fail and adversarial state-machine gaps are material |

## Ship-gate disposition

### Passed

- Frozen hashes and clean detached controls
- Native base/head builds
- Qualified base/head full short suites
- Head full race suite
- vet and tagged vet
- Linux and Darwin cross-builds
- fast lint and Inspector-only full lint
- JavaScript syntax and diff hygiene
- architecture guards plus three live mutations
- cold-import runtime equivalence
- byte-identical capture-off MCP transcript
- normal CLI differential for safe commands
- real private manager happy path
- exact partial MCP I/O behavior
- structural credential-header exclusion
- pre-reveal body-taint exclusion
- one-time launch history check
- synthetic and four-corpus Playwright acceptance
- 107-file canonical read-only proof

### Failed

- Windows cross-build (`CI-001`)
- full repository lint (`CI-002`)
- manager rollback ownership (`LIFE-001`)
- late-failure terminal truth (`LIFE-002`)
- cross-file capture identity (`INT-001`)
- required stream starts (`INT-002`)
- lifecycle completeness (`INT-003`)
- canonical symlink containment (`INT-004`, `WEB-003`)
- whole-result exactness (`CORR-001`)
- incomplete SSE semantics (`CORR-002`)
- correlation/edge basis truth (`CORR-003`)
- projection identity uniqueness (`PROJ-001`)
- per-metric evidence (`EVID-001`)
- cache integrity freshness (`WEB-001`)
- pinned/root ID ambiguity (`WEB-002`)
- strict reveal-body parsing (`SEC-001`)
- capture-off eval non-interference (`ISO-001`, `ISO-002`)

## Highest-risk failure chains

1. **False green evidence:** terminal-only/open/mixed streams can pass hashes and become complete; cached views can remain green after later tampering.
2. **False exact causality:** lossy text flattening discards result content, same-name calls can be paired despite different arguments, and graph output then asserts argument equality.
3. **Lost lifecycle ownership:** a readiness failure can orphan a daemon after deleting its journal; a drain-time capture error can still write complete.
4. **Normal-path regression:** capture-off eval can change subprocess HOME and create capture state.
5. **Unmergeable baseline:** the audited head fails the repository's Windows build and full lint CI gates.

## Required remediation order

No code change begins without explicit user approval. If remediation is approved, use reviewable batches in this order:

1. **CI/platform baseline:** restore Windows build and full lint.
2. **Forensic validator:** bind capture IDs; enforce provider/MCP/lifecycle state machines; reject physical symlink traversal.
3. **Equality/correlation:** compare whole result evidence; represent incomplete/ambiguous calls honestly; derive graph basis from actual join facts.
4. **Lifecycle ownership:** make startup rollback prove process exit and make final status sample post-drain failures.
5. **Projection evidence:** unique IDs from full canonical coordinates and evidence-bearing metrics.
6. **Web identity/cache:** reject pinned/root ambiguity and manifest symlinks; make integrity-bearing cache cryptographically fresh.
7. **Non-interference/security hardening:** restore capture-off eval behavior/side effects and enforce reveal EOF.

For each batch:

- first promote the corresponding audit RED probe into the production test suite;
- fix the smallest root cause;
- rerun focused tests and the full affected lane;
- repeat independent Standards and Spec review;
- do not weaken assertions, lint, security gates, or evidence semantics to obtain green.

## Re-review entry criteria

A new GO review requires at minimum:

- every Blocker and Major closed with a deterministic RED→GREEN test;
- every Minor closed or explicitly accepted by the user with rationale;
- base/head capture-off differential green for MCP, CLI, eval environment, and filesystem effects;
- Linux/Darwin/Windows build matrix green;
- full golangci-lint green;
- full short and race suites green;
- synthetic and real-corpus browser gates green;
- canonical before/after read-only proof green;
- no new finding in a final aggregate diff review.

## Unmeasured areas retained as limitations

The audit did not use live credentials or run real provider/Zerops traffic, destructive disk-full/power-loss tests, Windows runtime tests, or 250 MB/1 GB/decompression-bomb workloads. Those areas remain explicitly unproven; they do not soften the NO-GO decision.

## Packet

Tracked reports:

```text
plans/capture-inspector-audit-2026-07-15/
  00-baseline.md
  01-contract-matrix.md
  02-standards-review.md
  03-spec-review.md
  04-isolation-review.md
  05-security-evidence-review.md
  06-codebase-design-review.md
  07-test-coverage-review.md
  08-finding-ledger.md
  09-final-verdict.md
```

Gitignored evidence: `tmp/capture-inspector-audit-2026-07-15/`.

No capability URL, reveal cookie, credential, prompt, result, or raw capture body is included in the tracked packet.
