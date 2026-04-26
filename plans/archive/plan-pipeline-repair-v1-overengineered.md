> **SUPERSEDED 2026-04-26.** Replaced by `plans/plan-pipeline-repair.md`
> after the user identified Posture A creep across this plan. v1 (this
> file) was framed as "Final" on 2026-04-25 after Codex critique, but
> retained five Posture A items that did not contribute to genuine
> robustness: F1 full handler migration through `RespondWithLifecycle`,
> F4b tool-affordances corpus, F4c-suggestion migration, M4 truth
> registry, and the architecture test pinning workflow-aware tools to
> the lifecycle helper. The v2 reconciled plan keeps F2 / F3 / A1 / A2 /
> A3 + direct drift fixes + two narrowly-scoped lint pins (no production
> `atom.Body` reads, tool-description-specific drift lint with
> tool-specific patterns) + spec P4 revision. Preserved here for audit
> history.
>
> ---

# Plan: ZCP Atom + Workflow Pipeline Repair

Date: 2026-04-25
Status: **Final**
Inputs:
- `docs/audit-knowledge-atom-pipeline.md` (atom-corpus narrow)
- `docs/audit-workflow-llm-information-flow.md` (broader info-flow)
- `docs/plan-pipeline-repair-prefinal.md` (synthesis + Codex critique pass)

This plan supersedes the pre-final. Decisions are committed; open questions
are resolved; phasing is dependency-checked. The pre-final document moves
to `plans/archive/` after this plan's Phase 1 lands.

---

## 0. What Changed From Pre-Final

Codex critique tightened the plan in five concrete ways:

1. **F2 ships before F1.** F2 is small and the two existing pipeline paths
   benefit immediately. The pre-final's F1 → F2 ordering blocked the only
   user-visible improvement on a multi-week refactor.
2. **Lifecycle-aware errors are Phase 1, not Phase 4.** `convertError` is
   the universal error sink; designing `WorkflowResponse` without error
   shape forces a second migration. F4's error subset rides Phase 1.
3. **F4 split into three independent items.** Lifecycle errors (Phase 1),
   tool-affordances corpus (Phase 5), static priors / CLAUDE.md template /
   eval prompts (Phase 5). Treating them as one mega-phase obscured the
   per-item priority.
4. **State-filtered atoms for MCP descriptions rejected.** MCP
   `Description:` is read at registration time — phase is not known.
   Putting init-time content in phase-tagged atoms is a fake abstraction.
   Tool descriptions live in a flat `internal/content/affordances/*.md`
   set, linted but not phase-filtered.
5. **Five missed surfaces named.** Bootstrap state machine, progress
   notification strings, subagent-brief system, CLAUDE.md template
   contradiction, eval prompts. Folded into appropriate phases.

---

## 1. Decisions Table

| ID | Verdict | Phase | Notes |
|----|---------|-------|-------|
| F1 | KEEP, split implementation | Phase 1 (type+errors), Phase 4 (handler migration) | Dependency claim was overstated; do the type design first, migrate handlers as a second touch |
| F2 | KEEP, ships first | Phase 1 | Days of work; no F1 dependency |
| F3 | KEEP | Phase 3 | Surgical; weekend |
| F4a — lifecycle errors + free-form `next` removal | KEEP, promoted | Phase 1 | Co-designed with `WorkflowResponse` type |
| F4b — tool-affordances corpus | KEEP, demoted | Phase 5 | Flat corpus, not atoms; lint-checked |
| F4c — static-prior governance (CLAUDE.md template, eval prompts) | KEEP, demoted | Phase 5 | Explicit drift today: `claude_shared.md:48` contradicts spec O3 |
| A1 | KEEP, merged with A2 | Phase 2 | Same root: atom rendering has non-authoritative paths |
| A2 | MERGE into A1 | Phase 2 | — |
| A3 | KEEP | Phase 2 | Frontmatter strictness alongside A1+A2 |
| T1 | POSTPONE | Captured explicitly | Cleanup, not pipeline repair; separate session |
| T2 | KEEP, split | Phase 5a (measurement gate), Phase 5b (policy if needed) | Don't pre-design overflow policy without proof of overflow |
| T3 | MERGE into F1 | Phase 1 | `ComputeEnvelope` naming truthfulness rides Phase 1 |
| **M1** Bootstrap state machine exempt from lifecycle envelope | NEW | Phase 4 | `handleBootstrapStatus` doesn't go through pipeline; folds into F1 handler migration |
| **M2** Progress notification strings ungoverned | NEW, low | Phase 5 | `PollBuild` / `PollProcess` emit free-form status text |
| **M3** Subagent-brief system parallel knowledge | NEW | Captured, deferred | Recipe-only; track separately like T1 |
| **M4** CLAUDE.md template contradicts spec O3 | NEW | Phase 5 | `claude_shared.md:48` smoking gun |
| **M5** Eval prompts misalignment | NEW, low | Phase 5 | Eval asks for workflow-step failures but never requires workflow use |
| **M6** 225 `PlatformError.Suggestion` strings ungoverned | NEW | Phase 1 (shape), Phase 5 (content migration) | Lifecycle-aware error wraps the structure; suggestion text moves to affordances later |

---

## 2. Resolved Open Questions

| Q | Answer | One-line reasoning |
|---|--------|--------------------|
| Q1 | Workflow-aware tools only | Pure-read tools have a different success criterion (return data fast); mixing homogenizes a useful distinction |
| Q2 | `Reason` + `FailureClass` + `Setup` + `Strategy` + `Summary`. **No log tails.** | Logs balloon envelopes; codes + summaries are enough for `BuildPlan` to dispatch a recovery action; `zerops_logs` has its own tool when more is needed |
| Q3 | Render once per matched service | The conjunction fix is precisely about per-service correctness; aggregate framing pushes enumeration onto the LLM, reversing the win |
| Q4 | Separate `internal/content/affordances/` corpus | MCP descriptions are init-time; phase-filtered atoms there are a fake abstraction. Same lint pipeline applies |
| Q5 | Defer T1 (recipe action split) | Hygiene work; doesn't change runtime correctness; capture explicitly so it doesn't drift |
| Q6 | Trim by priority for runtime atoms | Runtime guidance must stay single-turn; chunking is acceptable for subagent briefs (recipe-only) |
| Q7 | Yes — five missed surfaces named (M1–M5) above; M6 added by author re-evaluation | Both audits focused on runtime atom delivery; static priors and parallel guidance channels need governance |

---

## 3. Phased Implementation Plan

Each phase is independently verifiable. Per CLAUDE.md "phased refactors":
each phase MUST leave the codebase coherent before the next starts. RED →
GREEN → REFACTOR for behavior changes; pure refactors keep all layers green.

### Phase 1 — Wire Format Foundation + Error Shape + Compaction Recovery

**Lands:** F2 + F1 (type design only) + F4a + T3 + M6 (error structure).

**Touches.**

- `internal/workflow/envelope.go` — extend `AttemptInfo` with `Reason`,
  `FailureClass` (typed enum), `Setup`, `Strategy`, `Summary`.
- `internal/workflow/compute_envelope.go` — projection in
  `deployAttemptsToInfo` / `verifyAttemptsToInfo` copies the new fields;
  fold the bootstrap-envelope construction into a sister function or
  rename `ComputeEnvelope` → `ComputeLifecycleEnvelope` (T3 decision —
  default to renaming + adding `ComputeBootstrapEnvelope` so the
  carve-out is explicit).
- `internal/workflow/render.go` — `lastAttemptText` includes reason when
  present.
- `internal/workflow/build_plan.go` — Primary action favours the failure
  shape when `FailureClass` is set.
- `internal/workflow/response.go` (new) — define `WorkflowResponse{Envelope,
  Plan, Guidance, Result any, Error *LifecycleError}` as the wire-level
  shape.
- `internal/platform/errors.go` — `LifecycleError{Code, Message, Suggestion,
  APICode, Diagnostic, APIMeta}` becomes a typed wire shape (matches today's
  `PlatformError` JSON projection plus envelope/plan from caller).
- `internal/tools/convert.go` — `convertError` becomes a thin wrapper that
  expects callers to provide envelope + plan; new `RespondError(ctx,
  engine, ..., err)` helper does the lifecycle compute.
- New `internal/tools/lifecycle_response.go` — `RespondWithLifecycle(ctx,
  engine, ..., result any)` and `RespondError` helpers.

**Acceptance gates.**

1. `AttemptInfo` carries `Reason`, `FailureClass`, `Setup`, `Strategy`,
   `Summary`. Existing two pipeline callers
   (`handleLifecycleStatus`, `renderDevelopBriefing`) render
   "api ✗ build timeout (3m ago)" instead of "api: deploy failed" in
   `render_test.go` fixtures.
2. `WorkflowResponse` type defined and used by the existing two
   pipeline callers. Architecture test asserts the type's invariants
   (Envelope present, Plan.Primary populated).
3. `RespondError` exists and is used by the existing two pipeline
   callers when their handlers fail. Errors carry envelope + plan.
4. `ComputeEnvelope` either now populates Bootstrap/Recipe correctly
   OR has been renamed and the bootstrap path uses
   `ComputeBootstrapEnvelope`. The "single entry point" claim becomes
   accurate.
5. No regression in existing tests.

**Out of scope (defers to Phase 4).** Migrating other handlers through
`RespondWithLifecycle`. The type exists; the migration is one-by-one.

**Estimated size.** ~1 week. F2 is days; the type + error shape design
takes longer because it's the foundation for Phase 4.

### Phase 2 — Atom Pipeline Authoritativeness

**Lands:** A1 + A2 (merged) + A3.

**Touches.**

- `internal/workflow/bootstrap_guide_assembly.go::buildGuide` — synthesis
  errors propagate (no `return ""` swallow).
- `internal/workflow/strategy_guidance.go::BuildStrategyGuidance` —
  routed through `Synthesize` with a constructed envelope; raw `atom.Body`
  read removed.
- `internal/tools/workflow_strategy.go::handleStrategy` — silent error
  swallow at line 148-150 fixed; errors propagate.
- `internal/workflow/atom.go::parseFrontmatter` and `::parseYAMLList` —
  reject non-empty malformed lists; validate frontmatter keys against a
  known set; validate enum values per axis; reject missing required
  metadata.
- `internal/content/atoms/bootstrap-recipe-close.md` — remove unsupported
  `{slug}` placeholder OR add it to the typed-substitution set if the
  slug belongs in the envelope (preferred: remove; recipe slug is recipe
  authoring concern, not bootstrap close).
- New `internal/architecture_test.go` rule — production reads of
  `KnowledgeAtom.Body` outside parser/synthesizer/test code fail the
  build.

**Acceptance gates.**

1. Force a synthesis error in a unit test — caller fails loudly with a
   `LifecycleError`, not empty guidance.
2. `handleStrategy` for `push-dev`/`manual` returns guidance with real
   hostnames (no literal `{hostname}`).
3. Architecture test rejects production `atom.Body` reads.
4. Invalid atom frontmatter (`environments: local` non-list form,
   unknown keys, malformed enum) fails `LoadAtomCorpus`.
5. Every embedded atom synthesizes against its representative envelope
   without unknown-placeholder errors.

**Out of scope.** F4b's tool-affordances corpus governance — even though
the lint engine extends here, the affordances content migration is
Phase 5.

**Estimated size.** ~3 days.

### Phase 3 — Multi-Service Render Correctness

**Lands:** F3.

**Touches.**

- `internal/workflow/synthesize.go` — `Synthesize` returns
  `[]MatchedRender{Atom, MatchedService}`; per-render replacer built from
  `MatchedService.Hostname` and `StageHostname`. Atoms with no
  service-scoped axis keep the global `primaryHostnames` picker.
- Multi-match handling: if more than one service satisfies all axes,
  render once per matched service. Verify size budget compatibility (no
  over-cap; if cross-product hits the cap, T2's gate fires in Phase 5
  — no need to over-engineer here).
- `internal/workflow/render.go` — consumer adapts to the new shape (the
  return type changes from `[]string` to `[]MatchedRender`; the
  `RenderStatus` Guidance section flattens it the same way as before
  per atom).
- New regression test: alphabet-rotated dev/stage pair —
  - `apidev` (deployed) + `appdev` (never-deployed):
    `develop-first-deploy-write-app` renders `targetService="appdev"`.
  - Reverse: `apidev` (never-deployed) + `appdev` (deployed):
    same atom renders `targetService="apidev"`.

**Acceptance gates.**

1. The two-service rotation test passes both directions.
2. Existing single-service scenarios in `scenarios_test.go` keep the
   same output (the global picker's path is preserved when no
   service-scoped axis is declared).
3. Compaction-safety invariant test (`synthesize_test.go`) still holds:
   byte-equal envelopes → byte-equal output.

**Estimated size.** ~2-3 days including regression suite.

### Phase 4 — Handler Migration Through Lifecycle Helpers

**Lands:** F1 (full migration) + M1.

**Touches.** Every workflow-aware handler. Identified by the
allowlist (initial set):

```
zerops_workflow actions: start, complete, skip, status, reset,
                         iterate, resume, list, route, strategy,
                         classify, adopt-local, record-deploy, close
zerops_deploy
zerops_verify
zerops_subdomain
zerops_manage
zerops_scale
zerops_env
zerops_mount
zerops_dev_server
```

Each handler is rewritten to:

1. Compute envelope at the top.
2. Run its operation.
3. Return through `RespondWithLifecycle(envelope, plan, result)` or
   `RespondError(envelope, plan, err)`.

`handleBootstrapStatus` (M1) goes through the same pipeline but with
`ComputeBootstrapEnvelope` so Bootstrap is populated.

Free-form `next` strings are deleted from handler responses; `BuildPlan`
extends to cover post-mutation states (e.g. "you just set strategy on X
— primary next is `start workflow=develop`").

**Acceptance gates.**

1. AST-pinned architecture test: every handler whose tool name is in
   the workflow-aware allowlist returns through the helper. Failing the
   test fails the build.
2. After `action="strategy"` → response carries envelope + plan; LLM
   has no need to call `action="status"` to re-orient.
3. After `action="reset"` → response carries envelope + plan; LLM
   sees the new state without a follow-up call.
4. `zerops_deploy` failure → response carries envelope, plan, and
   `LifecycleError{FailureClass}`; `BuildPlan.Primary` names the
   actual failure shape.
5. Bootstrap status now carries an envelope with `Bootstrap` populated
   (via `ComputeBootstrapEnvelope`).
6. Grep: zero hand-rolled `next` strings remain in handler responses
   (allowed: `nextHint` constants used by `Plan.Primary.Rationale`).

**Estimated size.** ~2-3 weeks. The handler-by-handler migration is the
longest single item in the plan.

### Phase 5 — Static-Prior Governance + Cap Measurement

**Lands:** F4b + F4c + M2 + M4 + M5 + T2a + M6 (content migration).

Five sub-items, independently shippable:

#### 5a — Tool-affordances corpus

- New `internal/content/affordances/{tool-name}.md` set with frontmatter
  declaring MCP fields (description, annotation hints, jsonschema
  per-input pointers).
- Build-time generator wires affordances into `mcp.AddTool` calls.
- Atom-lint rules extend to the affordances corpus (forbidden patterns,
  spec invariant ID exclusions).
- Architecture test: every `mcp.AddTool` call sources its `Description`
  from an affordance file (no inline string literals).

Acceptance gate: a contradictory tool description (e.g. subdomain
"call after first deploy") fails the affordances lint pass.

#### 5b — Suggestion-text migration into affordances

The 225 `platform.NewPlatformError(...)` `Suggestion` strings move to
typed error-recovery affordance files keyed by error code. The
`LifecycleError.Suggestion` field is filled by an `affordances` lookup
in `RespondError` rather than hand-typed at the call site.

Acceptance gate: zero hand-typed `Suggestion:` literals remain in
production code; all suggestion text lives in lint-checked affordance
files.

#### 5c — CLAUDE.md template fix

`internal/content/templates/claude_shared.md:48` claims `zerops_subdomain`
"skips the workflow". Per spec O3 + atom corpus, subdomain is a
deploy-handler concern; agents never call `zerops_subdomain action=enable`
in happy path. Rewrite the template line. Add a content-test that the
generated CLAUDE.md text agrees with the atom corpus on a fixed list of
factual claims (subdomain, strategy, first-deploy mechanics).

#### 5d — Eval-prompt alignment

`internal/eval/prompt.go` asks agents to report "workflow step" failures
but the task prompt never requires workflow use. Rewrite eval prompts so
they exercise the repaired pipeline (every eval scenario calls
`zerops_workflow action=start` first, then proceeds). Add a meta-eval
test that an eval scenario fails when the agent skips workflow start.

#### 5e — Progress notification governance

Move `PollBuild` / `PollProcess` status message strings into a
governed format (constants in `internal/ops/` referenced by
notification emitters; lint rule prohibits free-form prose in
`onProgress` callbacks). Lower priority — not a major correctness issue.

#### 5f — T2a: corpus-size gate

Add a test that for representative envelopes (single dev, dev+stage,
multi-service standard with strategy+trigger), `Synthesize` output stays
under the 28KB inline-threshold. If a representative envelope exceeds
the threshold, `T2b` (overflow policy) becomes a real work item;
otherwise, the gate is a guardrail.

**Acceptance gates (Phase 5 overall).**

1. AtomLint catches contradictory affordance content.
2. CLAUDE.md template's factual claims agree with atom corpus on a
   fixed truth list.
3. Eval prompts exercise the workflow pipeline.
4. No free-form progress notification strings.
5. Corpus-size gate is green for representative envelopes; if it fails,
   T2b is added to the deferred list.

**Estimated size.** ~1-2 weeks total. Sub-items are independently
shippable.

---

## 4. Deliberately Deferred Items

These are captured here so the next session does not re-litigate them.

### T1 — Recipe action split from `zerops_workflow`

`build-subagent-brief`, `verify-subagent-dispatch`, `dispatch-brief-atom`,
`classify`, `generate-finalize` should move to `zerops_recipe`. Postponed
because:

- Doesn't change runtime correctness.
- Recipe v3 engine has its own scope; this is finishing a v2→v3 split.
- Doing it during Phase 4 would expand handler-migration scope.

Track in `plans/` as a separate work item. Re-open after Phase 5.

### M3 — Subagent-brief system as a parallel knowledge channel

Recipe agent prompts are stitched from brief atoms + filesystem YAML +
hard-coded prose (`internal/workflow/subagent_brief.go`). This is a
parallel knowledge system worth governance, but recipe-only and out of
scope for the runtime pipeline repair. Track alongside T1.

### T2b — Overflow policy for runtime atoms

If T2a's measurement gate proves overflow risk for representative
envelopes, T2b (chunk vs trim policy) becomes real. Until then, no
policy is needed. Q6 already commits to "trim by priority" if/when
needed, so the decision is on file but not implemented.

---

## 5. Phase Dependencies

```
Phase 1 ──── (foundation)
   │
   ├─→ Phase 2 (depends on Phase 1's error shape + LifecycleError type)
   │
   ├─→ Phase 3 (independent of 1, 2 — surgical change inside synthesize)
   │
   ├─→ Phase 4 (depends on Phase 1's WorkflowResponse + RespondWithLifecycle)
   │       │
   │       └─→ M1 (Bootstrap state machine through pipeline) folds in
   │
   └─→ Phase 5 (5a/5b depend on Phase 1; 5c/5d/5e/5f independent)
```

**Critical path:** Phase 1 → Phase 4. Phase 2 and 3 can run in parallel
with Phase 4 if the team has bandwidth; Phase 5 sub-items are mostly
independent.

**Total wall-clock estimate (sequential, one engineer):** ~5-7 weeks.

---

## 6. Concrete Acceptance Tests (Aggregated)

For traceability, the full list of new or extended tests this plan
introduces:

| Test name | Phase | What it pins |
|-----------|-------|--------------|
| `TestAttemptInfo_CarriesFailureContext` | 1 | F2 — Reason/FailureClass/Setup/Strategy/Summary survive projection |
| `TestRender_DeployFailureShowsReason` | 1 | F2 — render shows "build timeout" not just "failed" |
| `TestComputeEnvelope_BootstrapPopulated` OR `TestComputeBootstrapEnvelope_Exists` | 1 | T3 — single entry point claim is accurate |
| `TestRespondWithLifecycle_PopulatesEnvelopeAndPlan` | 1 | F1 type contract |
| `TestRespondError_BundlesEnvelopeAndPlan` | 1 | F4a — errors are lifecycle-aware |
| `TestSynthesisError_PropagatesToCaller` | 2 | A1 — no silent empty guidance |
| `TestStrategyGuidance_RoutedThroughSynthesize` | 2 | A2 — no atom.Body bypass |
| `TestArchitecture_NoProductionAtomBodyReads` | 2 | A2 — architecture pin |
| `TestLoadAtomCorpus_RejectsMalformedFrontmatter` | 2 | A3 — strict parsing |
| `TestSynthesize_PerServicePlaceholderBinding` | 3 | F3 — alphabet rotation, both directions |
| `TestSynthesize_CompactionSafetyHolds` | 3 | F3 — byte-equal envelope → byte-equal output (extended to multi-match case) |
| `TestArchitecture_WorkflowAwareToolsUseLifecycleHelper` | 4 | F1 — handler migration pin |
| `TestStrategyAction_NoFollowupStatusNeeded` | 4 | F1 — envelope on mutation responses |
| `TestResetAction_CarriesEnvelopeAndPlan` | 4 | F1 — same |
| `TestDeployFailure_LifecycleErrorWithFailureClass` | 4 | F4a + F2 integration |
| `TestBootstrapStatus_CarriesPopulatedEnvelope` | 4 | M1 — bootstrap path through pipeline |
| `TestNoFreeFormNextStrings` | 4 | F4a — grep test |
| `TestArchitecture_ToolDescriptionsFromAffordances` | 5a | F4b — no inline tool description literals |
| `TestAffordancesLint_CatchesContradictions` | 5a | F4b — lint extension |
| `TestNoHandTypedSuggestions` | 5b | M6 — Suggestion text from affordances |
| `TestClaudeMDTemplate_AgreesWithAtomCorpus` | 5c | M4 — static-prior consistency |
| `TestEvalScenarios_ExerciseWorkflow` | 5d | M5 — eval pipeline alignment |
| `TestProgressMessages_FromConstants` | 5e | M2 — progress governance |
| `TestSynthesize_FitsResponseCap` | 5f | T2a — measurement gate |

Total new/extended tests: 22. Per CLAUDE.md "TDD mandatory" — RED before
GREEN at every behavior change. Pure refactor steps (e.g. moving inline
strings to affordances) verify all layers stay green.

---

## 7. Rollback Considerations

Each phase commits behind a single failing-test gate; if a phase introduces
unexpected regression, revert by reverting the phase's commit range. Per
CLAUDE.md "atomic consistency": every commit compiles, runs, and makes
sense on its own.

`WorkflowResponse` introduced in Phase 1 is the structural foundation;
reverting Phase 1 reverts everything. Phases 2–5 each touch independent
slices and can revert without disturbing Phase 1.

---

## 8. Open Items After This Plan

After all five phases land, the following remain explicitly open:

1. **T1** (recipe action split) — separate session.
2. **M3** (subagent-brief system governance) — separate session.
3. **T2b** (overflow policy) — only if T2a's gate proves runtime overflow.
4. **Cross-tool envelope reach** — Q1's answer scopes envelope to
   workflow-aware tools. If experience post-Phase 4 shows pure-read
   tools also benefit, revisit Q1.

The plan's success criterion: an LLM, after context compaction in mid-task,
calls `action="status"` once and receives every piece of state it needs to
continue the task — including the actual reason any prior deploy or verify
failed. The "single tool call recovery" promise of `spec-work-session.md`
§10.1 becomes operational, not aspirational.
