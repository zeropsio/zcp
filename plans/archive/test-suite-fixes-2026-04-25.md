# Test-suite Audit Fix Plan — 2026-04-25

Source: consolidated audit (Claude + Codex independent passes) — `/tmp/codex_test_audit_2026-04-25.md` + chat synthesis.

Plan reviewed by Codex independently (review at `/tmp/codex_plan_review_2026-04-25.md`). Amendments applied: helper extraction over public-API refactor (1.1), narrowed deny scope (1.2), soft cap with concrete pre-flight numbers and ratchet plan (2.2), pointer-aliasing fix in mock (3.1), minimal golden helper folded in (4.0 NEW), **4.1 recipe_templates rewrite DROPPED (frozen v2 cluster, recipe-team scope)**, 4.2 split into production-API change + test rewrite (4.2a/4.2b), eval scenarios actually executed (4.3), synthesis-routing boundary pin (4.4 NEW), e2e mutex-only first move (5.1), test-naming convention enforcement DROPPED (6.5), regression-injection cadence clarified.

**Reconciliation 2026-04-26 with `plans/plan-pipeline-repair.md`** (fully shipped today, commits `248ce3b5..733c1234`): pipeline-repair's C2 (Phase 2) shipped `MatchedRender{AtomID, Body, Service}` — **exactly Phase 4.2a of this plan**. Phase 4.2a is therefore marked DONE; Phase 4.2b (scenarios_test.go migration to atom-ID assertions) remains needed and is now pure test-side cleanup against the existing API. C4 added `atom_body_access_test.go` (pin against direct `KnowledgeAtom.Body` reads outside parser/synth) — orthogonal to anything in this plan, no overlap. C7 added `description_drift_test.go` (forbidden-pattern lint over MCP tool descriptions + CLAUDE.md templates) — orthogonal to Phase 6.2 (which is about description LENGTH, not content). All other items in this plan remain unaddressed by pipeline-repair and are still needed.

**Codex v3 review amendments** (review at `/tmp/codex_plan_review_v3_2026-04-26.md`): start implementation from Phase 4.2b (not 1.1) — the pipeline-repair C2 API is ready and scenarios_test.go is the immediate consumer. Phase 1.1 rationale rescoped from "regex breakage went undetected" (unverifiable from repo history) to "lint-engine self-test gap" (structurally clear). Phase 2.2 (file-line-cap with ratchet) DROPPED — pipeline-repair §1 explicitly rejects ratcheting lints; cleanup-then-enforce belongs in a separate plan. Phase 4.4 narrowed to `handleLifecycleStatus` only (canonical recovery primitive per P4); broader per-handler boundary tests deferred until per-shape evidence justifies them.

## Engineering principles for this plan

- **No backward-compat constraints** (CLAUDE.local.md): if a test concept is wrong, replace it; don't accrete around it.
- **Quality over speed**: each phase ships only when its acceptance criteria pass at every layer (unit + tool + integration + e2e where applicable).
- **TDD discipline** (CLAUDE.md): RED first when the fix introduces new behaviour; refactor-only changes verify all layers stay green.
- **Pin every new convention**: any rule we want enforced must land with an executable test (CLAUDE.md: "New invariant or convention → add bullet + pin with a test").
- **Phase cap = verifiability, not file count**: a phase may touch 30 files if root-cause correctness requires it (CLAUDE.local.md "Simplicity is not line count").
- **Regression-injection cadence**: each phase below lists a "Verify" step that injects a deliberate failure to prove the new test catches the right bug. **This is a one-shot manual proof per new lint family at PR time, documented in the PR description — not a recurring dirty-tree ritual on every gate run.** The repeated `make lint-local && go test ./...` gate stays clean.

## Phase ordering rationale

Phases are ordered by **leverage / risk ratio**, not by ease:

1. Lint completeness (high leverage, ~zero risk — pure addition)
2. Convention pins (high leverage, low risk — AST-level reads, no behavioural change)
3. Mock fidelity (high leverage, medium risk — platform layer, blast radius across all callers)
4. Assertion-strength rewrites (high leverage, medium risk — many test files)
5. E2E discipline (medium leverage, medium risk — operational ergonomics)
6. Cleanup wins (low leverage, low risk — bundle into a single PR at the end)

**Implementation start order (after Codex v3 reconciliation)**: **start with Phase 4.2b**, not 1.1. Rationale: pipeline-repair C2 already shipped the `MatchedRender{AtomID, Body, Service}` API (Phase 4.2a is DONE), and `scenarios_test.go` still consumes the back-compat `SynthesizeBodies` wrapper with prose-substring assertions — the migration is the immediate-actionable cheap win. After 4.2b lands, proceed with 1.1 → 1.2 → Phase 2 → Phase 3 → 4.0 → 4.3 → 4.4 → Phase 5 → Phase 6.

Each phase has a hard verification gate (`make lint-local && go test ./... -race -count=1`) before the next phase starts.

---

## Phase 1 — Lint completeness (HIGH / zero-risk)

### 1.1 — atom-lint fires-on-fixture (Finding C1)

**Diagnosis**: `internal/content/atoms_lint_test.go` only verifies the production corpus is clean. The 6 regexes in `atomLintRules` (`internal/content/atoms_lint.go:33-64`) have **zero coverage of their own pattern set** — there is no test that proves any rule actually fires. This is a lint-engine self-test gap, not a pinned past-incident regression. Reframed per Codex v3 review: a past silent-regex break is unverifiable from repo, but the harness gap is structurally clear and `tools/lint/recipe_atom_lint_test.go::TestRunLint_FiresOnKnownViolations` already shows the correct shape for a sibling lint engine in this repo.

**Reference pattern**: `tools/lint/recipe_atom_lint_test.go::TestRunLint_FiresOnKnownViolations` does this correctly for the recipe lint — fixture per rule, assert each fires.

**Files**:
- `internal/content/atoms_lint_test.go` — add new test
- `internal/content/atoms_lint.go` — extract unexported helper `lintAtomCorpus(atoms []KnowledgeAtom) []AtomLintViolation` so the rule engine is callable with synthetic atoms. Public `LintAtomCorpus()` becomes a thin wrapper that calls `ReadAllAtoms()` then the helper. **No public API change.**

**Acceptance**:
- New test `TestAtomAuthoringLint_FiresOnKnownViolations` constructs one synthetic `KnowledgeAtom` per rule (`spec-id`, `handler-behavior-handler`, `handler-behavior-tool-auto`, `handler-behavior-zcp`, `invisible-state`, `plan-doc`) and passes them to `lintAtomCorpus` directly.
- Each fixture trips exactly the targeted rule (assert violation count = 1, category matches).
- Bonus assertion: a clean fixture with only allowlist-style content yields zero violations.
- A 7th fixture combines two rules and asserts both fire.

**Verify**: introduce a typo into one regex (e.g. change `\bDM-[0-9]` → `\bDX-[0-9]`); old regression test still passes (corpus-clean), new fires-on-fixture test FAILS for spec-id rule. Revert.

**Estimated**: ~80 lines test, ~10 lines helper extraction in atoms_lint.go (no exported API change).

---

### 1.2 — Pin "tools/eval reach platform via ops" convention

**Diagnosis**: CLAUDE.md was just updated (this session) with a new convention bullet:
> tools/eval reach platform via ops — `client.ListServices` / `client.GetServiceEnv` is forbidden outside of `internal/ops/`, `internal/platform/`, and `internal/workflow/`.

No pinning test exists. CLAUDE.md says any new invariant requires a test pin.

**Files**:
- `internal/topology/architecture_test.go` — extend with new layered rule (parallel to existing 4 layer rules), OR
- `internal/architecture_test.go` (sibling) — new test `TestNoDirectClientCallsInToolsEval`

**Pre-flight already done** (Codex): current direct callers are all in allowed layers — `internal/ops/lookup.go:11` (legal — ops), `internal/workflow/compute_envelope.go:56` (legal — workflow), `e2e/helpers_test.go:78` (test setup, must remain legal). So the deny scope is **only** production `internal/tools/`, `internal/eval/`, and `cmd/` — NOT "every non-whitelisted package".

**Acceptance**:
- AST walk over **`internal/tools/`, `internal/eval/`, `cmd/`** (and only those) finds zero call expressions matching `client.ListServices` or `client.GetServiceEnv`.
- Allowed layers (no scan): `internal/ops/`, `internal/platform/`, `internal/workflow/`.
- Test files (`*_test.go`) under e2e are exempt — direct platform setup in tests is legal.
- Failure message names file:line + which call + the convention bullet to read.

**Verify**: introduce a `client.ListServices(ctx, projectID)` call into any handler in `internal/tools/`; test FAILS with named violation. Revert. Then add the same call to `internal/workflow/` — test PASSES (proving the whitelist works).

**Estimated**: ~60 lines test, reuses the AST helpers already in `architecture_test.go`.

---

### 1.3 — Phase 1 verification gate

```
make lint-local && go test ./... -race -count=1
```

Both new tests pass on clean tree, demonstrably fail on injected violations (manually verified once, then commit).

---

## Phase 2 — Convention pins (HIGH / low-risk)

CLAUDE.md `Conventions` section currently has bullets WITHOUT executable pins. Each bullet below gets one.

### 2.1 — `TestNoStdoutOutsideJSONPath` (Finding D1)

**Diagnosis**: CLAUDE.md says **"JSON-only stdout — debug to stderr; MCP protocol depends on it."** A stray `fmt.Println` anywhere in the request-response path silently breaks Claude Desktop integration. No test pins this.

**Files**:
- New test, e.g. `internal/server/stdout_purity_test.go`

**Acceptance**:
- AST walk over `cmd/zcp/`, `internal/server/`, `internal/tools/`, `internal/ops/`, `internal/workflow/` (the request-path packages).
- Forbid call expressions: `fmt.Print*`, `fmt.Fprint*` where target is `os.Stdout`, `os.Stdout.Write*`, `println`.
- Allowlist: any test files (`_test.go`), CLI-only entrypoints under `cmd/zcp/check/` and `cmd/zcp/sync/` (CLI subcommands genuinely write to stdout — verify each is documented as non-MCP-server).
- Stderr writes (`os.Stderr`, `fmt.Fprint(os.Stderr, ...)`, `log.*`) are fine.

**Verify**: inject `fmt.Println("hi")` into a handler in `internal/tools/`; test FAILS. Revert.

**Estimated**: ~100 lines test (AST walk + allowlist resolution). Reuse helpers from `architecture_test.go`.

---

### 2.2 — DROPPED: `TestFileLineCap` (Finding D3)

**Original Finding D3**: CLAUDE.md says "350-line soft cap per `.go` file"; no automated check exists.

**Pre-flight done** (Codex v2): **40 non-test `.go` files currently exceed 350 lines.** Top offenders: `internal/workflow/recipe_guidance.go` (1002), `internal/tools/workflow_checks_recipe.go` (960), `internal/tools/workflow.go` (813). A hard 350 gate would immediately break the build, so the original v2 design proposed a soft cap with a multi-PR ratchet plan (350 → 700 → 600 → 350).

**Decision (after Codex v3 review): DROP from this plan.** This conflicts directly with `plans/plan-pipeline-repair.md §1`: *"A lint that needs a ratchet plan to enforce is admitting it can't be enforced."* The same author shipped pipeline-repair today with that principle as a stated rule; introducing a ratcheting lint here would silently relitigate it. Either the cleanup happens first (split the 40 offenders, then a hard 350 cap is real), or the cap stays as authorial guidance only — there is no honest middle ground.

**What stays**: the convention in CLAUDE.md is unchanged. New code SHOULD aim under 350 lines; reviewers can flag overshoots in PR review.

**Future option (separate plan)**: a "split-the-overlong-files" plan does the cleanup first (40 files split as a multi-PR pass with explicit acceptance criteria per split), and only after that pass lands does an enforced `TestFileLineCap_350` (hard, no ratchet) become a real pin. Out of scope here.

---

### 2.3 — `TestNoCrossCallHandlerState` (Finding D2)

**Diagnosis**: CLAUDE.md says **"Stateless STDIO tools — each MCP call is a fresh operation."** No test forbids handler-package-level mutable globals.

**Files**:
- New test, e.g. `internal/tools/stateless_test.go`

**Acceptance**:
- AST walk over `internal/tools/*.go` (excluding `_test.go`).
- Find package-level `var` declarations.
- Allow: `var _ <Interface> = ...` (compile-time interface assertions), `var <Name> = <constant-expression>` (effectively constants), `sync.Once`, `sync.Pool`-like patterns.
- Forbid: any other mutable package-level var.
- Failure: file:line + var name + suggest move into a struct field of the handler context.

**Verify**: add `var lastRequestID string` to any tools file; test FAILS.

**Estimated**: ~80 lines test.

---

### 2.4 — Phase 2 verification gate

All three tests pass; manually verified each fails with injected violation. Commit.

---

## Phase 3 — Mock fidelity (HIGH / medium-risk)

### 3.1 — Promote platform mock to transition fixtures (Findings E1 + N2)

**Diagnosis**: `internal/platform/mock_methods.go:113, 277, 317` returns synthetic `PENDING` for mutating ops; `GetProcess:282` returns stored state. The real platform documents transitions: `startWithoutCode` → `ACTIVE` after deploy (per `../zerops-docs/.../zerops-complete-knowledge.md:248-252`). Tests pass against static states that don't match reality. The log mock already does this correctly (`mock.go:203-210, 241-260` mirror production filtering) — replicate the pattern.

**Approach**: introduce a "scenario fixture" abstraction.

```go
type ProcessScenario struct {
    InitialStatus string
    Transitions   []ProcessTransition  // {AfterCalls: 1, Status: "RUNNING"}, ...
    TerminalStatus string
}
```

`Mock.SetProcessScenario(processID, scenario)` lets a test express "starts PENDING, becomes RUNNING after 2 GetProcess calls, terminates FINISHED after 4".

**Files**:
- `internal/platform/mock.go` (or split into `mock_scenarios.go`) — new types + methods
- `internal/platform/mock_methods.go` — wire mutating ops to drive scenarios
- All callers using the old `WithProcess(..., "PENDING")` pattern — keep a `WithStaticProcess` shim for tests that genuinely don't care about transitions, but flag them for review

**Composability check done** (Codex): `internal/platform/mock.go:14` defines `Mock`, `WithProcess` returns `*Mock` at `:106`, all `With*` setters lock `m.mu`. `WithProcessScenario` fits naturally as another builder; reuse the existing `sync.RWMutex`. **Pointer-aliasing risk** identified: `GetProcess` at `mock_methods.go:282` returns an internal pointer — scenario transitions must **copy the struct under the lock before returning** so callers can't see torn state under concurrent reads.

**Acceptance**:
- New mock method `WithProcessScenario(processID string, scenario ProcessScenario)` — returns `*Mock`, locks `m.mu`, fits builder pattern.
- `GetProcess` reworked to copy `Process` struct under lock before return (no callers should observe internal pointer aliasing).
- New test `TestMockProcessScenario_TransitionsAsConfigured` proves the scenario fires in order.
- New test `TestMockProcessScenario_NoCallerAliasing` runs `go test -race`-style: spawns 50 goroutines hammering `GetProcess` while another mutates the scenario; no data race detected, no caller observes a torn read.
- At least 1 existing high-value test consolidates onto the new API:
  - **`internal/ops/progress_test.go::processSequencer` → `WithProcessScenario`** — eliminated the parallel ad-hoc wrapper (custom struct + own mutex + sequence/idx fields) in favour of the canonical Mock-native scenario. Two parallel patterns collapse to one (per CLAUDE.local.md "single path engineering").

- The plan's original two other migration targets turned out **not applicable** on closer inspection (documented honestly rather than forced):
  - `integration/deploy_flow_test.go::TestIntegration_DeploySSHSelfDeploy` — `mockSSHDeployer{output: []byte("push ok")}` is an SSH **command output**, not a process state stream; the new scenario API governs GetProcess responses and doesn't model command stdout. Static "push ok" remains correct.
  - `internal/tools/process_test.go` — tests zerops_process tool's `status`/`cancel` actions, which are single-call (no polling). Static state is the right shape; transitions don't fit.
- A regression test asserts that the OLD static-state path still works (back-compat for tests that haven't migrated yet).

**Verify**: change one converted test's scenario terminal status from `RUNNING` → `FAILED`; test FAILS at the right assertion (not at parsing/setup). Run `go test ./internal/platform/... -race -count=20`; zero race detector hits.

**Estimated**: ~150 lines new mock infrastructure (incl. copy-under-lock fix in GetProcess), ~3 test files migrated (~60 lines each). Phase 3 ships scaffolding + 3 worked examples; bulk migration left for follow-up.

**Risk**: blast radius is platform layer. Mitigation: additive API (no removed methods until all callers migrated), comprehensive `mock_methods_test.go` for the new behaviour, `go test ./internal/platform/... -race -count=20` in the verification gate to catch concurrency regressions.

---

### 3.2 — Phase 3 verification gate

```
go test ./internal/platform/... -race -count=10
go test ./... -race -count=1
make lint-local
```

---

## Phase 4 — Assertion-strength rewrites (HIGH / medium-risk)

### 4.0 — Minimal golden-diff helper (folded from H2)

**Diagnosis**: Phase 4 rewrites ~400 lines of substring-matching tests across `recipe_templates_test.go`, `scenarios_test.go`, and `eval/grade.go`. A minimal golden-text helper makes the rewrites cleaner and the diffs reviewable; deferring all of H2 leaves Phase 4 weaker than it needs to be.

**Scope (intentionally narrow)**: thin wrapper around `github.com/google/go-cmp/cmp` for multi-line text → unified-diff output. NOT a full golden-file framework with auto-regeneration (that's the deferred H2).

**Files**:
- New helper: `internal/testutil/diff/diff.go` (or wherever fits the cross-cutting test-utility convention)
- Tests for the helper itself: `internal/testutil/diff/diff_test.go`

**Acceptance**:
- `diff.Lines(want, got string) string` returns `""` on equality, unified-diff output otherwise.
- `diff.JSON(want, got any) string` marshals both to canonical JSON and diffs.
- Helper has its own table-driven test covering: equal/unequal/whitespace-difference/multiline.
- Documented in package godoc: "use only when output IS structured text; for parsed structures prefer cmp.Diff directly."

**Verify**: write a fixture test using the helper; intentionally break the expected; failure output is a readable unified diff.

**Estimated**: ~80 lines helper + ~60 lines test.

---

### 4.1 — DROPPED: `recipe_templates_test.go` rewrite

**Original Finding F1**: 102 `strings.Contains` calls in `internal/workflow/recipe_templates_test.go` — golden-substring brittleness.

**Decision**: **DROP from this plan.** The file lives in the frozen v2 cluster (`internal/workflow/recipe_*.go`) which CLAUDE.md marks "exempt until deletion." Investing in test-quality rewrites for code scheduled for deletion is wasted work. The finding is real but moot; when the v2 cluster is deleted, the offending substring assertions go with it.

**Out of scope for THIS plan, and for ZCP test infrastructure work generally** — recipe v2/v3 lives in a separate ownership scope. Any rewrite belongs in a recipe-team plan, not here.

---

### 4.2 — Scenario tests assert atom IDs, not phrase substrings (Findings A2 + C2)

**Pre-flight done** (Codex): `Synthesize` at `internal/workflow/synthesize.go:25` returns only `[]string`. No atom-ID exposure exists. **`scenarios_test.go` calls `Synthesize` at lines `:40`, `:76`, `:154` and uses phrase checks at `:81-83`.** A direct rewrite to atom-ID assertions hides a production API change inside a test cleanup. **Split into two sub-phases with their own RED→GREEN.**

#### 4.2a — DONE: ID-bearing synthesis result type (shipped via pipeline-repair C2)

**Status update 2026-04-26**: This item is **already shipped** by commit `c984b3d7` (pipeline-repair Phase 2 — C2). The current `internal/workflow/synthesize.go` exposes:

```go
type MatchedRender struct {
    AtomID  string
    Body    string
    Service *ServiceSnapshot
}
func Synthesize(envelope StateEnvelope, corpus []KnowledgeAtom) ([]MatchedRender, error)
func SynthesizeBodies(envelope StateEnvelope, corpus []KnowledgeAtom) ([]string, error)  // back-compat wrapper
```

The MatchedRender type carries `AtomID` exactly as planned, plus `MatchedService` (a feature beyond the original 4.2a scope — it lets per-render placeholder substitution bind to the matching service, addressing pipeline-repair's F3 multi-service render bug).

**No work remaining for 4.2a.** Phase 4.2b can proceed directly against the existing API.

#### 4.2b — Migrate scenarios_test.go from `SynthesizeBodies` to atom-ID assertions

**Current state** (verified 2026-04-26): `scenarios_test.go` still calls the back-compat wrapper `SynthesizeBodies` at lines 40, 76, 154, 199, 246, 316, 371, 540, 572, 628 and uses `strings.Contains(joined, ...)` substring matching at lines 82, 208, 252, 322, 636 — exactly the brittle prose-coupling pattern flagged by Findings A2 + C2.

**Diagnosis**: 4.2a landed `MatchedRender{AtomID, Body, Service}` but didn't migrate scenario tests. Migrating now is pure test-side cleanup; production API is stable.

**Files**:
- `internal/workflow/scenarios_test.go`

**Acceptance**:
- Each scenario test (S1, S2, ..., S12) calls `Synthesize(...)` (not `SynthesizeBodies`) and asserts on the **set of atom IDs** synthesized, not on substring matches in joined bodies.
- Where a test specifically validates that an atom **renders a specific value** (e.g. a service hostname interpolation in a multi-service envelope), keep a single targeted substring assertion AS WELL AS the atom-ID assertion.
- The `strings.Contains(joined, ...)` body matchers in `scenarios_test.go` drop from 5 to ≤2 (only legitimate value-interpolation cases — likely the per-service hostname checks F3 introduced).
- Use `diff.JSON` from Phase 4.0 for the atom-ID set comparison failure output.

**Verify**: rename an atom file ID (e.g. change `bootstrap-recipe-provision.md`'s frontmatter ID); the scenario test FAILS at the atom-ID assertion with a clear message ("expected atom 'bootstrap-recipe-provision', got [...]"). Restore.

**Estimated**: ~150 lines rewrite (smaller than original estimate since 4.2a is done).

---

### 4.3 — Eval transcript contract (Finding N6)

**Diagnosis**: `internal/eval/grade.go:127-168` grades scenarios via substring checks over JSON-ish text. Final URL probe is optional. Eval can produce false-greens.

**Approach**:
- Define a typed transcript type in `internal/eval/types.go` — fields for tool calls, responses, errors.
- Replace string-based grading with structural assertions on the typed transcript.
- Make final-URL probe required for any scenario whose `expects` includes "deploy" or "verify" steps.

**Files**:
- `internal/eval/grade.go`
- `internal/eval/types.go` (new or extended)
- `internal/eval/scenarios_test.go` (extend coverage to scenario execution, not just parseability)

**Acceptance**:
- New `Transcript` type with strongly typed entries.
- `Grade(transcript Transcript, expectations Expectation) Result` replaces the substring grader.
- `internal/eval/grade_test.go` includes fixtures for: transcript-passes-grade, transcript-fails-on-missing-tool, transcript-fails-on-wrong-arg, scenario-requires-final-url.
- **`internal/eval/scenarios_test.go` extended to actually EXECUTE at least one representative scenario end-to-end** (not just parse the file + check non-empty expectations). Per Codex: current `scenarios_test.go:10-43` only validates parseability; tightening grading without tightening execution coverage leaves the eval surface still hollow.

**Risk**: eval is the kind of test that "everyone trusts but nobody reads" — tightening it may surface scenarios that were silently passing under the old loose grader. Treat surfaced failures as findings, not regressions.

**Estimated**: ~250 lines grader rewrite + ~50 lines scenario-execution test.

---

### 4.4 — Pin `handleLifecycleStatus` synthesis routing (Codex M1, narrowed by v3 review)

**Diagnosis**: commit `8eeb74b2` ("route StateEnvelope through engine API") added the new handler boundary; pipeline-repair C2 (`c984b3d7`) updated the callsite at `internal/tools/workflow.go:774` to consume `Synthesize` + `BodiesOf`, but neither pinned the tool-boundary contract — a regression that zeros an envelope field or swallows a synth error would fail in `internal/workflow/` tests only by accident, or not at all.

**Narrowed scope (v3 review)**: only `handleLifecycleStatus` (the `action="status"` recovery primitive — pipeline-repair P4 designates it canonical for compaction recovery). Other workflow handlers (`handleStart`, `handleIterate`, `handleStrategy`, `handleClose`) call `Synthesize` too but their routing risk is lower priority and a broad table without per-shape evidence is the over-coverage anti-pattern pipeline-repair §1 warns against. Scope can expand later if a real per-shape bug surfaces.

**Files**:
- New test, e.g. `internal/tools/workflow_status_routing_test.go`

**Acceptance**:
- Table-driven test of `handleLifecycleStatus` for the representative envelope shapes that hit the routing path (idle, bootstrap-active, develop-active — three cases, not six).
- Per case: assert the envelope passed to `Synthesize` matches the input envelope (no field corruption / drop in the routing layer); the atom-ID set returned propagates into the response; errors from `Synthesize` propagate as the right MCP error code (not silently swallowed).
- Use Phase 3.1 mock scenarios for any platform calls the routing layer makes pre-synthesis.

**Verify**: introduce an envelope-field zeroing bug in the routing layer (e.g. clear `WorkSession` before passing); test FAILS at the boundary, not downstream.

**Estimated**: ~80 lines (narrowed from ~150).

### 4.5 — Phase 4 verification gate

```
go test ./... -race -count=1
make lint-local
# spot-check: every test in scenarios_test.go and recipe_templates_test.go
# fails for a CORRECTNESS reason when run against an intentionally-broken
# implementation, not a phrasing reason.
```

---

## Phase 5 — E2E discipline (MED / medium-risk)

### 5.1 — DROPPED: e2e provisioning serialization (Finding N1)

**Original Finding N1**: parallel e2e instruction tests read shared project state; concurrent provisioning could perturb them.

**Decision after empirical inspection**: **DROP from this plan.** The premise doesn't hold under verification:

- E2E tests are **sequential by default**: zero `t.Parallel()` calls across 30+ e2e files except `e2e/instructions_test.go`.
- The 5 parallel tests in `instructions_test.go` (lines 36, 90, 132, 152, 198) are ALL **read-only** observers (BuildInstructions, zerops_discover) — no mutation, no contention between them.
- Sequential provisioning tests don't run concurrently with the parallel observer pool: Go testing waits for all sequential tests to finish before parallel tests start, within the same package.
- No history of e2e flakes traced to this concern (`git log --all | grep -i 'flake\|parallel\|concurrent'` returns one race fix unrelated to e2e).

Adding a mutex infrastructure proactively to a non-bug is exactly the "Real-bug-driven only" pattern pipeline-repair §1 forbids. If a real e2e flake surfaces traceable to parallel observation, add the mutex then.

---

### 5.2 — DROPPED: `testing.Short()` adherence (Finding N3)

**Original Finding N3**: only 7 tests check `testing.Short()`; heavy e2e advertises 600-900s timeouts but uses build-tag gating.

**Decision after empirical inspection**: **DROP from this plan.** The premise doesn't hold:

- Zero non-test-code `time.Sleep > 1*Second` calls across `internal/`. The slow-test risk vector this finding assumed doesn't manifest.
- The 7 tests that DO gate on `testing.Short()` cover the actual heavy paths (`integration/bootstrap_realistic_test.go`, `internal/workflow/registry_test.go`, `internal/ops/deploy_ssh_test.go::TestBuildSSHCommand_FreshInitPath`, `internal/ops/import_test.go`, `e2e/update_test.go`). They're self-gating correctly.
- E2E tests gate via `//go:build e2e` + `ZCP_API_KEY` requirement — they don't run under default `go test ./...` at all. `testing.Short()` would be a redundant gate.
- `go test ./... -short -count=1` already runs in ~30-40s on the dev box — the target the original Phase 5.2 was driving toward.

A scanner-test that audits for unguarded sleeps would be a no-op today. Adding it preventively (per pipeline-repair §1) doesn't justify the maintenance cost.

---

### 5.3 — Phase 5 verification gate

```
go test ./... -short -count=1   # must complete in <30s
go test ./e2e/... -tags e2e -count=3  # zero flakes
make lint-local
```

---

## Phase 6 — Cleanup (LOW / low-risk)

Bundle into a single PR.

### 6.1 — `TestIntegration_DeploySSHWithWorkingDir` (Finding A1)

**Decision**: either delete the test (real coverage lives in `internal/ops/deploy_ssh_test.go::TestDeploy_WorkingDir_MountPath_Rejected`) OR strengthen the integration mock to track the SSH command and assert workingDir reaches it.

**Recommendation**: strengthen, not delete. The mock SSH deployer in `integration/deploy_flow_test.go:33-39` discards command args; change it to record them, then assert the recorded command contains `cd /tmp/myapp` (or whatever workingDir was passed).

**Files**: `integration/deploy_flow_test.go`

**Estimated**: ~30 lines.

---

### 6.2 — Annotations word-cap allowlist (Finding B1)

**Diagnosis**: `annotations_test.go:196 trimmedTools` excludes 6 of 17 tools by name with no rationale.

**Decision**: either apply the cap to every tool (preferred, root-cause fix) or document why each excluded tool is exempt.

**Recommendation**: apply to every tool. If a tool's description is currently > 60 words, that's the bug — trim the description. The cap exists for a reason; carve-outs hollow it out.

**Files**: `internal/tools/annotations_test.go` + likely several tool description constants.

**Estimated**: ~100 lines (mostly description trimming).

---

### 6.3 — Annotations setup duplication (Finding G2)

**Diagnosis**: `TestAnnotations_AllToolsHaveTitleAndAnnotations` reimplements MCP server setup that `listAllTools` already does.

**Files**: `internal/tools/annotations_test.go`

**Acceptance**: the All test calls `listAllTools` instead of duplicating setup. Test still passes.

**Estimated**: ~30 lines deletion.

---

### 6.4 — DROPPED: `// non-parallel:` convention enforcement (Finding N4)

**Original Finding N4**: CLAUDE.md requires `// non-parallel: <reason>` for tests not running parallel; literal occurrence found only once.

**Decision after empirical inspection**: **DROP from this plan.**

Empirical state:
- 28 of 316 internal test files have NO `t.Parallel()` calls at all.
- 0 of those files have a `// non-parallel:` comment with a documented rationale.
- No observed bug class traceable to undocumented non-parallel tests (no flake fix in git log naming this as cause).

A debt-tracking lint (parallel to Phase 6.2's `untrimmedTools` map) would need 28 entries with **unknown** rationales — opaque clutter, not transparent debt. Without per-file investigation to recover each test's reason for being non-parallel, the lint either catches every legitimate sequential test as a violation (false positives) or is rendered useless by a 28-entry exemption list.

If a real flake surfaces traceable to "test ran parallel when it shouldn't have", revisit then with concrete evidence. For now the convention stays as authorial guidance in CLAUDE.md for new tests.

---

### 6.5 — DROPPED: test naming convention enforcement

**Original Finding N5**: `Test{Op}_{Scenario}_{Result}` unevenly applied; ~8/30 sample violations.

**Decision (after Codex pre-flight)**: **DROP from this plan.** Codex grep counted **2,444 non-conforming test declarations** under that regex — effectively the entire suite. A global enforcement is massive low-value churn (CLAUDE.local.md: "simplicity is not line count" cuts both ways — width matters too). The convention stays in CLAUDE.md as authorial guidance for new/changed tests; no executable lint.

**If we want to revisit**: future plan could add `TestNamingConvention_NewOnly` that only enforces on tests added/touched in the same PR (via git diff against base), preserving the convention forward without a 2,444-test rename. Out of scope here.

---

### 6.6 — Phase 6 verification gate

```
go test ./... -race -count=1
make lint-local
```

---

## Open questions / decisions deferred

Resolved during plan review:
- ~~**350-line cap: hard or soft?**~~ → **DROPPED** (2.2). Pipeline-repair §1 forbids ratcheting lints. Cleanup-then-enforce is its own future plan.
- ~~**Test naming: rename existing or grandfather?**~~ → **DROPPED** (6.5). 2,444 non-conforming declarations make global enforcement low-leverage.
- ~~**Non-parallel comment: enforce or just document?**~~ → **DROPPED** (6.4). 28 files with no documented rationales would fill any debt-tracking lint with opaque entries.
- ~~**`Synthesize` API: extend vs sibling?**~~ → **DONE** by pipeline-repair C2 (commit `c984b3d7`). API extended; `SynthesizeBodies` kept as back-compat wrapper, used in places that don't need provenance.

Still open:
1. **Mock scenario: should every existing static-state test migrate, or only new tests?** (3.1) — Phase 3.1 shipped scaffolding + 1 worked example (`progress_test.go::processSequencer` consolidation). Bulk migration deferred until a real bug class (false-green from static state) materializes.

## Out of scope for this plan

- Property-based tests (Finding H1) — distinct concept that warrants its own design pass; can be added once the lint/contract test foundation is solid.
- Live-schema contract tests (Finding H3) — needs design (when to fetch, how to cache, CI dependency on live API). Worth doing eventually but high risk of CI flakiness.
- Full golden-file framework with auto-regeneration (most of Finding H2) — Phase 4.0 ships a **minimal** diff helper (just `cmp.Diff` wrapper for multi-line text). A full golden-file framework with `-update` flag, file-on-disk fixtures, auto-regeneration etc. stays out — adopt after Phase 4 demonstrates the smaller helper is sufficient.

These remain valid "Concept gaps" but require their own plans — keeping them out keeps this plan executable.

## Acceptance criteria for the plan as a whole

- All 6 phases ship.
- `go test ./... -race -count=1` green.
- `make lint-local` green.
- A future `git grep 'strings.Contains' --include='*_test.go' | wc -l` returns a number meaningfully smaller than today's count, **outside** the frozen recipe v2 cluster (which keeps its 102 substrings until cluster deletion).
- A future `git grep 'reflect.DeepEqual\|cmp.Diff' --include='*_test.go' | wc -l` returns a number meaningfully larger than 10.
- CLAUDE.md `Conventions` section: every bullet has a referenced pinning test.
- A regression injected at any layer FAILS at the layer that owns the invariant — not at a downstream consumer.

---

## Appendix — finding ↔ phase index

| Finding | Phase |
|---------|-------|
| C1 atom-lint fires-on-fixture | 1.1 |
| New "tools/eval via ops" pin | 1.2 |
| D1 JSON-only stdout | 2.1 |
| D3 350-line cap | **DROPPED** (2.2 — ratcheting lints conflict with pipeline-repair §1; cleanup-then-enforce in a separate plan) |
| D2 stateless STDIO | 2.3 |
| E1 + N2 mock fidelity (incl. pointer-aliasing fix) | 3.1 |
| (NEW) Minimal golden-diff helper | 4.0 |
| F1 recipe_templates substring | **DROPPED** (4.1 — frozen v2 cluster, recipe-team scope) |
| A2 + C2 scenario assertions — production API split | **DONE** (4.2a — shipped via pipeline-repair C2 in commit c984b3d7) |
| A2 + C2 scenario assertions — test rewrite | 4.2b |
| N6 eval transcript contract + actual scenario execution | 4.3 |
| (NEW Codex M1) Synthesis routing boundary pin | 4.4 |
| N1 e2e state coupling (mutex-only) | 5.1 |
| N3 testing.Short adherence | 5.2 |
| A1 workingDir integration test | 6.1 |
| B1 annotations word-cap allowlist | 6.2 |
| G2 annotations setup duplication | 6.3 |
| N4 non-parallel convention | 6.4 |
| N5 naming convention | **DROPPED** (6.5 — see section) |
| A3 assertion strength skew | implicit across 4.0, 4.2b, 4.3 (4.1 dropped) |
| H1 property tests | OUT OF SCOPE (own plan) |
| H2 golden diff infrastructure | partial (minimal helper folded into 4.0); full framework OUT OF SCOPE |
| H3 live-schema contract tests | OUT OF SCOPE (own plan) |
| H4 fires-on-fixture for lints | folded into 1.1 |
| (NEW Codex M2) Eval scenarios actually executed | acceptance update on 4.3 |
