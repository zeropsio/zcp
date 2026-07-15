# Capture Inspector audit — confirmed finding ledger

All entries are **confirmed**, **feature-introduced** in the frozen range, and independently reproduced or observed in the fixed real corpus. No candidate is included merely from a smell scan.

## Summary

| ID | Severity | Primary lane | Short title |
|---|---|---|---|
| `CI-001` | Major | Standards | Windows CI build broken |
| `CI-002` | Major | Standards | Full lint adds 74 issues |
| `LIFE-001` | Major | Standards/Lifecycle | Readiness rollback can orphan daemon |
| `LIFE-002` | Blocker | Spec/Evidence | Late drain failure finalizes complete |
| `INT-001` | Blocker | Spec/Evidence | Manifest and raw capture identities can differ |
| `INT-002` | Blocker | Spec/Evidence | Terminal-only provider/MCP streams are complete |
| `INT-003` | Blocker | Spec/Evidence | Open lifecycle scopes are complete |
| `INT-004` | Blocker | Security/Evidence | Inspector follows symlinked parent evidence |
| `CORR-001` | Blocker | Spec/Evidence | Lossy text equality reported as exact bytes |
| `CORR-002` | Major | Spec/Correlation | Incomplete SSE block becomes tool use |
| `CORR-003` | Major | Spec/Correlation | Argument mismatch becomes proven causal edge |
| `PROJ-001` | Major | Spec/Projection | MCP projection IDs collide in all real captures |
| `EVID-001` | Major | Spec/Evidence | All known metrics lack evidence coordinates |
| `WEB-001` | Blocker | Security/Evidence | Cached integrity remains valid after tampering |
| `WEB-002` | Major | Security/Projection | Pinned/root duplicate ID splits index and detail |
| `WEB-003` | Minor | Security/Projection | Root scan follows manifest symlink |
| `SEC-001` | Minor | Security | Reveal accepts trailing JSON value |
| `ISO-001` | Major | Isolation | Capture-off user simulator HOME changed |
| `ISO-002` | Minor | Isolation | Capture-off eval discovery writes state |

---

## `CI-001` — Major — Windows CI build broken

- **Lane/status/origin:** Standards; confirmed; feature-introduced.
- **Location:** `internal/capture/manager.go:525,530,641`; also Unix process policy in `cmd/zcp/capture.go:458–476` and `cmd/zcp/capture_daemon.go:58–183`.
- **Rule:** `.github/workflows/ci.yml` builds `windows/amd64` with `CGO_ENABLED=0`.
- **Impact/blast radius:** every pull request/release CI build fails on Windows; the main binary is no longer portable even when capture is unused.
- **Reproduction:** base Windows build exits 0; head `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ./...` fails on undefined flock/kill symbols.
- **Existing-test gap:** native Darwin tests and fast lint do not compile Windows; no platform build test sits beside capture process code.
- **Root cause:** Unix lock/signal/process-group policy is embedded in untagged portable files.
- **Smallest correction/RED:** introduce build-tagged platform adapters (including a Windows implementation or explicit compatible behavior); retain the exact cross-build matrix as the RED gate.

## `CI-002` — Major — Full lint adds 74 issues

- **Lane/status/origin:** Standards; confirmed; feature-introduced.
- **Location:** representative findings include `internal/capture/control.go:149`, `manager.go:253–298`, `claude_settings.go:203–204`, `cmd/zcp/capture.go:453`, and eval lifecycle paths; full list in the retained log.
- **Rule:** CI's golangci-lint action runs the full configured suite over `./...`.
- **Impact/blast radius:** CI lint fails; several diagnostics identify real context/platform/error-handling debt, not formatting only.
- **Reproduction:** base `make lint-local` reports 0; head reports 74. Fast and Inspector-only lint pass.
- **Existing-test gap:** handoff verification relied on fast/scoped lint and did not run the same full command as CI.
- **Root cause:** new capture/eval code was not closed against the repository's full v2.8.0 linter configuration.
- **Smallest correction/RED:** fix or narrowly justify each diagnostic without disabling categories globally; require base/head-equivalent full lint before handoff.

## `LIFE-001` — Major — Readiness rollback can orphan daemon

- **Lane/status/origin:** Standards/Lifecycle; confirmed; feature-introduced.
- **Location:** `internal/capture/manager.go:253–298`, `shutdownReadyDaemon` at `:506–514`.
- **Rule:** manager transaction must not lose ownership of a started daemon when enable fails.
- **Impact/blast radius:** failed `capture on` can remove its journal while leaving a process/listener alive; later `status/off` cannot identify it.
- **Reproduction:** fake readiness returned a live `/bin/sleep` PID and unavailable control socket; `On` failed, state rolled back, terminate callback stayed false, PID remained alive.
- **Existing-test gap:** manager tests use a cooperating runtime/control server and do not fail every post-readiness operation.
- **Root cause:** rollback is best-effort control-only; its error is ignored and process fallback/wait are absent.
- **Smallest correction/RED:** on any post-readiness failure, control-shutdown then identity-checked TERM/KILL/wait; retain journal/BROKEN state until exit is proven; promote the fault probe.

## `LIFE-002` — Blocker — Late drain failure finalizes complete

- **Lane/status/origin:** Spec/Security-Evidence; confirmed; feature-introduced.
- **Location:** `internal/capture/runtime.go:179–234`, especially error snapshots at `:186–194` before shutdown at `:196–205`.
- **Rule:** spec §2.3–2.4; any capture failure must be visible and cannot finalize integrity-complete.
- **Impact/blast radius:** raw evidence can be incomplete while status, terminal records, and manifest claim complete.
- **Reproduction:** an active exchange was paused; shutdown began; proxy capture error was injected during drain; `Close` returned `("complete", nil)`.
- **Existing-test gap:** runtime tests inject failures before close, not concurrently during drain/finalization.
- **Root cause:** component errors are sampled once before the operations that can produce late errors.
- **Smallest correction/RED:** close/drain, then sample each component error (and repeat before manifest commit); downgrade first; promote deterministic drain probe.

## `INT-001` — Blocker — Manifest and raw capture identities can differ

- **Lane/status/origin:** Spec/Security-Evidence; confirmed; feature-introduced.
- **Location:** manifest ID assigned in `internal/capture/inspect.go:352–380`; provider ID read at `internal/capture/inspect_provider.go:113–128`; lifecycle checks only self-consistency at `inspect_lifecycle.go:37–50`.
- **Rule:** spec §§5,7,8 and “every derived claim has evidence.”
- **Impact/blast radius:** evidence from capture A can be reported as valid/complete capture B; session attribution and all derived claims inherit a false identity.
- **Reproduction:** regular provider/lifecycle files from another capture were copied, hashes recomputed, and accepted under the declaring manifest ID.
- **Existing-test gap:** fixtures use one ID throughout and test hash mismatch, not cross-file identity mismatch.
- **Root cause:** the manifest, provider, lifecycle, MCP, and provenance validators do not share one expected capture identity.
- **Smallest correction/RED:** pass expected manifest ID into every stream validator and reject every mismatched/empty embedded ID; retain copied-record test.

## `INT-002` — Blocker — Terminal-only provider/MCP streams are complete

- **Lane/status/origin:** Spec/Security-Evidence; confirmed; feature-introduced.
- **Location:** `validateInspectionRecordSequence` at `internal/capture/inspect_provider.go:279–297`, reused by `inspectMCPFile` at `inspect_mcp.go:71–82`.
- **Rule:** spec §2.3–2.4 and canonical stream lifecycle requirements.
- **Impact/blast radius:** wholly absent protocol lifetimes can be represented as valid and complete with zero observed input/output.
- **Reproduction:** one `session.end` provider record and, separately, one `mcp.stream.end` record with empty hashes both yielded valid/complete reports.
- **Existing-test gap:** reader tests consume only recorder-produced happy-path streams.
- **Root cause:** generic sequence validation checks only continuity and final kind, not first kind or legal transitions.
- **Smallest correction/RED:** require exactly one expected start at seq 1, legal record-state transitions, and one terminal; add provider and MCP terminal-only tests.

## `INT-003` — Blocker — Open lifecycle scopes are integrity complete

- **Lane/status/origin:** Spec/Security-Evidence; confirmed; feature-introduced.
- **Location:** warnings at `internal/capture/inspect_lifecycle.go:146–160`; completeness calculation at `internal/capture/inspect.go:340–345`.
- **Rule:** spec §5.1 says missing lifecycle boundaries make annotation incomplete.
- **Impact/blast radius:** interrupted eval run/scenario/invocation hierarchy can display green `OK`, misleading timing, phase, and attribution consumers.
- **Reproduction:** lifecycle start, `eval.run.start`, and complete stream end produced warning plus `Integrity.Complete=true`.
- **Existing-test gap:** lifecycle fixtures close all nested scopes and do not assert warning→completeness semantics.
- **Root cause:** lifecycle parser returns warnings but no completeness result; top-level completeness sees only stream terminal status.
- **Smallest correction/RED:** implement nested lifecycle state validation and propagate `annotationComplete=false`; retain open run/scenario/invocation cases.

## `INT-004` — Blocker — Inspector follows symlinked parent evidence

- **Lane/status/origin:** Security/Evidence; confirmed; feature-introduced.
- **Location:** lexical resolver `internal/capture/inspect.go:413–423`; only final path is `Lstat`ed at `:450–457`.
- **Rule:** isolation I6 requires symlink-safe canonical paths.
- **Impact/blast radius:** CLI/core inspection can read files outside the declared session and validate them as its canonical evidence.
- **Reproduction:** manifest paths under `evidence/` traversed a symlinked parent to another complete session and were accepted.
- **Existing-test gap:** path tests cover `..`, absolute paths, and final symlinks, not symlinked ancestors.
- **Root cause:** lexical containment is treated as physical containment; core and projection use different resolvers.
- **Smallest correction/RED:** resolve/verify root and each component with no-follow semantics and reuse one canonical resolver; retain parent-symlink fixture.

## `CORR-001` — Blocker — Lossy text equality reported as exact bytes

- **Lane/status/origin:** Spec/Security-Evidence; confirmed; feature-introduced.
- **Location:** MCP flattening `internal/capture/inspect_mcp.go:277–300`; provider flattening `inspect_provider.go:379–403`; equality `inspect.go:535–540`.
- **Rule:** handoff §7.4 exact means provider-visible result bytes equal observed MCP result; spec §8.2 calls exact solid.
- **Impact/blast radius:** omitted images/resources/block boundaries/other result structure can still produce `exact`, corrupting propagation metrics, graph status, and review conclusions.
- **Reproduction:** MCP text plus image versus provider text only was reported exact with four bytes.
- **Existing-test gap:** correlation tests use text-only, same-shape results.
- **Root cause:** display-text extraction happens before forensic equality; original result representation is discarded from the comparison.
- **Smallest correction/RED:** compare explicitly defined whole canonical result bytes/structure first; derive display text separately; add multi-block/non-text/error/empty cases.

## `CORR-002` — Major — Incomplete SSE block becomes tool use

- **Lane/status/origin:** Spec/Correlation; confirmed; feature-introduced.
- **Location:** `parseProviderSSEToolUses`, `internal/capture/inspect_provider.go:442–507`, especially EOF finalization at `:493–505`.
- **Rule:** incomplete streams must be visible, not interpreted as complete claims.
- **Impact/blast radius:** partial tool input can be correlated to MCP and provider results as an observed model proposal.
- **Reproduction:** content-block start plus input delta without stop returned one tool use.
- **Existing-test gap:** SSE fixtures stop every content block; projection's incomplete status is not cross-checked with core report.
- **Root cause:** EOF cleanup shares the same finalize function/state as observed `content_block_stop`.
- **Smallest correction/RED:** retain unfinished blocks as explicit incomplete diagnostics and exclude them from completed correlation; promote probe.

## `CORR-003` — Major — Argument mismatch becomes proven causal edge

- **Lane/status/origin:** Spec/Correlation; confirmed; feature-introduced.
- **Location:** fallback join `internal/capture/inspect.go:479–548`; hard-coded graph basis `internal/captureinspector/internal/projection/graph.go:61`.
- **Rule:** unknown/ambiguous identity remains explicit; graph edges must state their true join basis.
- **Impact/blast radius:** parallel/repeated same-name calls can be paired in the wrong order; downstream graph then falsely says arguments were equal.
- **Reproduction:** provider `start` arguments and MCP `status` arguments produced a correlation with `ArgumentsEqual=false`, propagation exact, and `jsonrpc-and-argument-equality` edge.
- **Existing-test gap:** fixtures do not permute repeated same-name calls with different arguments; edge tests do not compare basis to source flags.
- **Root cause:** weak fallback is represented as a concrete execution, then graph construction ignores `CorrelationBasis`.
- **Smallest correction/RED:** leave non-unique mismatches unmatched/ambiguous or visibly weak; derive edge basis from the actual correlation; add parallel permutation tests.

## `PROJ-001` — Major — MCP projection IDs collide in all real captures

- **Lane/status/origin:** Spec/Projection; confirmed; feature-introduced.
- **Location:** `internal/captureinspector/internal/projection/mcp.go:136–154`.
- **Rule:** spec §8.1 deterministic identity from canonical coordinates; by-ID evidence must not be ambiguous.
- **Impact/blast radius:** distinct messages share UI/graph identity; edge deduplication and selection can collapse or open the wrong event.
- **Reproduction:** four real captures contain 2, 2, 12, and 2 duplicate groups; each collision occurs when multiple lines share one raw record seq.
- **Existing-test gap:** tests assert message counts/content but not global ID uniqueness over multi-line chunks.
- **Root cause:** ID uses file plus `SeqStart` and omits already-known `StreamOffset`/line ordinal.
- **Smallest correction/RED:** include stream offset (and direction if needed) in IDs; assert global uniqueness on synthetic multi-line chunks and real corpus.

## `EVID-001` — Major — All known metrics lack evidence coordinates

- **Lane/status/origin:** Spec/Evidence; confirmed; feature-introduced.
- **Location:** metric factory `internal/captureinspector/internal/projection/metrics.go:53–68` and all catalog builders.
- **Rule:** spec §2.5 requires every derived claim to identify raw evidence coordinates and equality basis.
- **Impact/blast radius:** more than 100 displayed claims cannot be navigated/audited back to canonical observations; comparison inherits unsupported aggregates.
- **Reproduction:** synthetic fixture: 102/102 known metrics empty; every real capture: 112/112 empty.
- **Existing-test gap:** tests assert IDs/values/missing semantics but not `Metric.Evidence`.
- **Root cause:** aggregation helpers accept scalar values and basis strings, not source observations.
- **Smallest correction/RED:** aggregate bounded deterministic evidence sets/ranges with each metric and test every non-null metric has coordinates.

## `WEB-001` — Blocker — Cached integrity remains valid after tampering

- **Lane/status/origin:** Security/Evidence; confirmed; feature-introduced.
- **Location:** terminal caching `internal/captureinspector/internal/web/server.go:584–609`; key `:612–645`.
- **Rule:** finalized hash failure must be visible; green integrity cannot outlive evidence identity.
- **Impact/blast radius:** `/view`, trace, provider metadata, compare, and integrity badge can remain stale/green after evidence changes. Detail endpoints do reject on re-hash.
- **Reproduction:** populate cache, same-size mutate provider file, restore mtime; fresh inspector rejects, cached `/view` returns valid.
- **Existing-test gap:** cache tests mutate size/mtime or test detail revalidation, not content with metadata restoration.
- **Root cause:** cryptographic result is keyed by non-cryptographic mutable file metadata.
- **Smallest correction/RED:** revalidate hashes before serving integrity-bearing cached data or key by verified immutable content identity; add eviction and restored-metadata test.

## `WEB-002` — Major — Pinned/root duplicate ID splits index and detail

- **Lane/status/origin:** Security/Projection; confirmed; feature-introduced.
- **Location:** `captureEntries` `internal/captureinspector/internal/web/server.go:537–556`; `sessionPath` `:558–572`.
- **Rule:** spec §8.1 requires duplicate identity to fail loudly.
- **Impact/blast radius:** user selects metadata for one directory and receives view/plaintext from another directory under the same ID.
- **Reproduction:** root and pinned fixtures shared an ID but labels differed; index returned root label, view returned pinned label.
- **Existing-test gap:** root-only duplicate test exists; pinned/root collision does not.
- **Root cause:** index dedup treats same ID as already present while detail gives pinned path priority.
- **Smallest correction/RED:** compare real paths/manifest identities and reject any same-ID different-directory ambiguity before serving index or detail.

## `WEB-003` — Minor — Root scan follows manifest symlink

- **Lane/status/origin:** Security/Projection; confirmed; feature-introduced.
- **Location:** `internal/captureinspector/internal/projection/raw.go:33–105`, particularly `os.Stat(manifestPath)` at `:82`.
- **Rule:** isolation I6 symlink-safe paths.
- **Impact/blast radius:** external manifest metadata can appear in the root index; later raw path checks limit detail escape.
- **Reproduction:** symlinked `alias/manifest.json` to an external fixture was indexed.
- **Existing-test gap:** scan tests reject symlink directories, not symlink files.
- **Root cause:** final manifest check/read follows symlinks.
- **Smallest correction/RED:** `Lstat` and require a regular non-symlink manifest within the resolved root; retain probe.

## `SEC-001` — Minor — Reveal accepts trailing JSON value

- **Lane/status/origin:** Security; confirmed; feature-introduced.
- **Location:** `internal/captureinspector/internal/web/server.go:395–411`.
- **Rule:** bounded strict confirmation request; one exact object only.
- **Impact/blast radius:** authenticated same-origin caller can send an ambiguous body that is accepted; no capability/Origin bypass.
- **Reproduction:** valid `REVEAL` object followed by a second object returned 204.
- **Existing-test gap:** tests cover wrong/unknown/oversized fields, not a second JSON value.
- **Root cause:** decoder performs one decode and never requires EOF.
- **Smallest correction/RED:** decode first object, decode again and require `io.EOF`; retain probe.

## `ISO-001` — Major — Capture-off user simulator HOME changed

- **Lane/status/origin:** Isolation; confirmed; feature-introduced.
- **Location:** `internal/eval/behavioral_run.go:235–244`, `claudeEnv` at `:305–321`.
- **Rule:** capture-off differential/non-interference; existing eval behavior is outside the Inspector feature.
- **Impact/blast radius:** behavioral eval user-simulator authentication, Claude config, memory, and output can change even with capture disabled.
- **Reproduction:** identical fake-Claude probe observed parent HOME at base and sandbox ClaudeHome at head with `Capture=nil`.
- **Existing-test gap:** active capture environment is tested; disabled subprocess environment is not compared with base.
- **Root cause:** active-capture environment plumbing reused a helper that also changes HOME.
- **Smallest correction/RED:** preserve nil/inherited environment when capture is off (or explicitly approve an independent eval change); add frozen base-compatible test.

## `ISO-002` — Minor — Capture-off eval discovery writes state

- **Lane/status/origin:** Isolation; confirmed; feature-introduced.
- **Location:** `cmd/zcp/eval.go:312–327`; control-dir creation `cmd/zcp/capture_daemon.go:22–46`; manager lock `internal/capture/manager.go:517–533`.
- **Rule:** disabled-path side effects must remain absent; `--active` is read-only contact.
- **Impact/blast radius:** every normal authenticated eval can create capture runtime directories and lock file despite OFF state; no daemon/listener starts.
- **Reproduction:** isolated `activeEvalCapture` created the state tree, lifecycle lock, and temp control directory. `capture ui --active` OFF did likewise.
- **Existing-test gap:** OFF tests assert returned state, not filesystem inventory.
- **Root cause:** constructors/lock eagerly create state before checking whether active state exists.
- **Smallest correction/RED:** use a non-mutating existence/read path for OFF and acquire/create lock only when state/on-off transition requires it; retain filesystem probe.
