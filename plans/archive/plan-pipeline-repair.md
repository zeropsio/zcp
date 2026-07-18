# ZCP Pipeline Repair — Plan

Date: 2026-04-26
Status: **Final**

Supersedes:
- `plans/archive/plan-pipeline-repair-v0-prefinal.md`
- `plans/archive/plan-pipeline-repair-v1-overengineered.md`

Audits this plan addresses:
- `docs/audit-knowledge-atom-pipeline.md`
- `docs/audit-workflow-llm-information-flow.md`

This plan is intentionally short. It does seven concrete things, each
real-bug-driven. It explicitly drops architecture from the v1 plan that
was Posture A creep — preventive systems for problems that hadn't
manifested.

## §0 — Why this plan is shorter than v1

v1 framed three problem classes:

1. **Real bugs** (F2, F3, A1, A2, A3) — captured-then-thrown-away
   diagnostic data, multi-service render bug, silent error swallows,
   atom-body bypass, tolerant frontmatter parsing. All real, all visible
   in code today.
2. **Genuine drift** in 2 specific places (`subdomain.go:28`,
   `claude_shared.md:48`) — observable contradictions with spec O3.
3. **Hypothetical drift surfaces** — 14 tool descriptions, 225 error
   suggestion strings, the entire CLAUDE.md template, every workflow
   handler — for which v1 proposed corpus migrations, typed
   RecoveryID systems, truth registries, and a full handler migration
   to a `RespondWithLifecycle` helper.

(1) and (2) are real engineering. (3) was over-application of the atom
architecture pattern to surfaces that did not have a recurring drift
problem. Costs of (3) are not just implementation effort — corpus
governance has its own failure modes (file structure, generators, lint
mismatches, allowlist creep), and parallel envelope-everywhere channels
multiply the surfaces a bug can land in.

This plan keeps (1) and (2) and adds **two narrowly-scoped lint pins**
that mechanically prevent recurrence of the specific drift patterns we
have already observed. It does not migrate any content, does not add a
governance layer, does not migrate any handler.

`action="status"` is the canonical lifecycle recovery primitive. After
this plan, it returns the diagnostic data the LLM needs post-compaction.
Other tool responses may be terse — recovery is via status.

## §1 — Engineering principles

- **Real-bug-driven only.** Every architecture/lint addition must point
  at code that already exhibited the bug at least once. Hypothetical
  recurrence is not sufficient.
- **Mechanical guarantees beat manual vigilance** when the cost is
  proportionate — but the cost includes the rule's own maintenance,
  false-positive risk, and drift-of-the-rule. A lint that needs a
  ratchet plan to enforce is admitting it can't be enforced.
- **`action="status"` is the canonical recovery primitive.** Other
  responses do not need to mirror it. Spec P4 is revised to match.
- **Single canonical paths beat redundant carry-along channels.** Each
  parallel path is a place a bug can hide.
- **No content migration without a real corpus drift problem** that is
  not solvable by fixing the few drifted strings directly + a narrow
  lint that prevents recurrence of the specific pattern.

## §2 — The seven substantive changes

| # | Change | Files | Rationale |
|---|--------|-------|-----------|
| C1 | Promote `Reason`, `FailureClass`, `Setup`, `Strategy`, `Summary` from `WorkSession` into `AttemptInfo`; fix projection; render reason | `internal/workflow/envelope.go`, `compute_envelope.go`, `render.go`, `build_plan.go` | F2: persisted diagnostic data is captured then dropped before the envelope ships |
| C2 | `Synthesize` returns `[]MatchedRender{Atom, MatchedService}`; per-render replacer; service-scoped axes bind placeholder to matching service | `internal/workflow/synthesize.go`, `render.go`, all callers | F3: per-service match vs global placeholder substitution causes wrong-host commands in multi-service projects |
| C3 | Propagate synthesis errors instead of `return ""` | `internal/workflow/bootstrap_guide_assembly.go`, `internal/tools/workflow_strategy.go` | A1: silent guidance loss masquerades as intentional silence |
| C4 | Route `BuildStrategyGuidance` through `Synthesize`; new architecture test rejecting production `atom.Body` reads outside parser/synthesizer/test | `internal/workflow/strategy_guidance.go`, new `internal/workflow/atom_body_access_test.go` | A2: `atom.Body` is a public field already abused once; mechanical pin prevents recurrence |
| C5 | Strict frontmatter parsing: reject malformed non-empty list values, unknown keys, invalid enum values | `internal/workflow/atom.go::parseYAMLList`, `parseFrontmatter` | A3: tolerant parsing turns malformed axes into wildcards silently |
| C6 | Direct drift fixes | `internal/tools/subdomain.go:28`, `internal/content/templates/claude_shared.md:48` | Both contradict spec O3 today; observable, fixable in one edit each |
| C7 | Tool-description drift lint with tool-description-specific forbidden patterns; spec P4 revision | new `internal/tools/description_drift_test.go`, `docs/spec-workflows.md`, `docs/spec-knowledge-distribution.md` | Mechanical prevention of recurrence of the C6 drift class; spec aligned with reality (status as canonical recovery) |

## §3 — What this plan explicitly does NOT do

These are deliberate non-goals; future sessions should not re-litigate
them without new evidence.

| Dropped | Reason |
|---------|--------|
| F1 full handler migration through `RespondWithLifecycle` | `action=status` is the canonical recovery primitive (after C1). Carry-along envelope on every mutation creates parallel failure surfaces (every handler must Compute correctly; one bug = inconsistency vs status). Single path is more reliable. |
| F4b tool-affordances corpus migration | 14 tools. Direct drift fixes (C6) + narrow lint (C7) prevent recurrence. Corpus governance has its own failure modes (file layout, generators, lint mismatches, allowlist creep) without robustness gain. |
| M6 RecoveryID typed system for 225 PlatformError suggestions | Suggestions are call-site contextual ("for service %q", "in directory X"). Typed lookup with templates adds indirection failure modes (lookup can fail, template can be wrong, RecoveryID can be wrong). Hand-written contextual strings have only one bug per call site; the system has many. |
| M4 truth registry for CLAUDE.md template | 66-line template. C6 fixes the drifting line; C7's lint catches recurrence. Registry is speculative architecture without a real second drift instance. |
| Architecture test pinning workflow-aware handlers to `RespondWithLifecycle` | The helper is dropped (F1 dropped). The test would pin a non-existent invariant. |
| Lint extension blindly reusing atom_lint regexes for inline Go strings | `atoms_lint.go` regexes target spec IDs, "auto-*" verbs, invisible state, plan-doc paths. None obvious-matches the actual drift text in `subdomain.go:28` ("need one enable call after first deploy"). C7 uses tool-description-specific patterns instead. |
| Eval prompts alignment (M5), progress notification governance (M2), recipe action split from `zerops_workflow` (T1), corpus-size cap measurement (T2), subagent-brief governance (M3) | None real-bug-driven against the runtime pipeline. Defer to separate sessions if real failure modes appear. |

## §4 — Phased implementation

Each phase ships independently, with its own acceptance gate. Phases are
ordered by dependency, not difficulty.

### Phase 1 — Compaction recovery actually works (C1)

**Touches.**
- `internal/workflow/envelope.go` — extend `AttemptInfo` with `Reason`,
  `FailureClass` (typed enum: `BuildFailure | StartFailure |
  VerifyFailure | NetworkFailure | ConfigFailure | OtherFailure`),
  `Setup`, `Strategy`, `Summary`.
- `internal/workflow/compute_envelope.go` — `deployAttemptsToInfo` and
  `verifyAttemptsToInfo` copy the new fields from `DeployAttempt` /
  `VerifyAttempt`.
- `internal/workflow/render.go::lastAttemptText` — render reason when
  present (e.g. `"deploy failed: build timeout"`).
- `internal/workflow/build_plan.go` — when the failing attempt has a
  `FailureClass`, `Plan.Primary.Rationale` names the failure shape
  (e.g. `"build failed; fix and redeploy"` not just `"deploy api"`).
- `internal/tools/deploy_ssh.go`, `deploy_local.go`, `verify.go` — at
  the existing record sites, populate the new fields from the operation
  result (these record sites already exist; only the field set widens).

**Acceptance.**
- `TestAttemptInfo_PreservesFailureContext` — fixture WorkSession with
  failed deploy; envelope projection carries Reason / FailureClass /
  Setup / Strategy.
- `TestRenderStatus_DeployFailureShowsReason` — render output includes
  the reason string, not just "deploy failed".
- `TestBuildPlan_FailureTargetedRationale` — when AttemptInfo carries
  FailureClass, BuildPlan.Primary.Rationale references it.
- Manual regression-injection: change a failing test's expected reason;
  test fails at the assertion (not at parsing).

### Phase 2 — Atom pipeline correctness (C2 + C3 + C4 + C5)

Four independent fixes that share a verification gate. Order within the
phase: C5 → C3 → C4 → C2 (frontmatter strict first so the rest can rely
on it; pipeline correctness last because it has the largest blast
radius).

**Touches.**
- `internal/workflow/atom.go::parseYAMLList`, `parseFrontmatter` (C5):
  - Non-empty value not in `[a, b, c]` form → error, not nil.
  - Unknown frontmatter keys → error.
  - Each axis enum validated against its known set.
- `internal/workflow/bootstrap_guide_assembly.go` (C3): `buildGuide`
  errors propagate to the caller.
- `internal/tools/workflow_strategy.go:147` (C3): silent
  `SynthesizeStrategySetup` error swallow → propagate.
- `internal/workflow/strategy_guidance.go` (C4): construct minimal
  `StateEnvelope` from target service metas, call `Synthesize`, return
  rendered bodies.
- New `internal/workflow/atom_body_access_test.go` (C4): AST scan over
  `internal/{workflow,tools,ops}/*.go` (excluding `_test.go`,
  `atom.go`, `synthesize.go`). Find selector expressions matching
  `<expr>.Body` where the receiver type is `KnowledgeAtom` or
  `*KnowledgeAtom`. Forbid all hits. Allowlist: parser + synthesizer.
- `internal/workflow/synthesize.go` (C2): `Synthesize` returns
  `[]MatchedRender{Atom KnowledgeAtom, MatchedService *ServiceSnapshot}`.
  Per-render replacer built from `MatchedService.Hostname` and
  `StageHostname` when service-scoped axes are declared; otherwise
  global `primaryHostnames` (preserved for atoms without service-scoped
  axes).
- `internal/workflow/render.go` (C2 consumer): renderGuidance flattens
  `[]MatchedRender` to text bodies; behavior unchanged for single-match
  / no-service-axis atoms.
- All other `Synthesize` callers updated for the return type change.
  Concretely (4 production sites + 1 internal):
  - `internal/tools/workflow.go:774` (handleLifecycleStatus)
  - `internal/tools/workflow_develop.go:166` (renderDevelopBriefing)
  - `internal/workflow/bootstrap_guide_assembly.go:37`
    (BootstrapState.buildGuide)
  - `internal/workflow/synthesize.go:300` (SynthesizeImmediateWorkflow,
    internal call)
  Plus C4's new `BuildStrategyGuidance` rewrite calls Synthesize too.

**Multi-match policy** (atoms whose service-scoped axes are satisfied
by more than one service): render once per matched service. Justified
by F3's correctness claim (per-service binding); cost of verbose
output is acceptable since Phase 1 fixed the diagnostic-data side and
multi-match atoms in practice are rare (gated by phase + axis
conjunction).

**Acceptance.**
- `TestLoadAtomCorpus_RejectsMalformedFrontmatter` (C5) — fixtures with
  non-list malformed values, unknown keys, invalid enum values, all
  fail load.
- `TestBuildGuide_SynthesisErrorPropagates` (C3) — force a
  synthesizer error; bootstrap caller fails loudly.
- `TestStrategyGuidance_RoutedThroughSynthesize` (C4) — push-dev
  strategy guidance contains real hostnames, no literal `{hostname}`.
- `TestNoProductionAtomBodyReads` (C4) — AST test passes; injecting a
  raw `atom.Body` read in a non-allowlisted file fails the build.
- `TestSynthesize_PerServicePlaceholderBinding` (C2; in
  `internal/workflow/synthesize_test.go`) — alphabet-rotated dev/stage
  pair, both directions, atom matched via the never-deployed service
  renders that service's hostname.
- `TestSynthesize_CompactionSafety` (C2 regression; same file) —
  byte-equal envelopes still produce byte-equal output across the new
  render shape.

### Phase 3 — Direct drift fixes (C6)

**Touches.**
- `internal/tools/subdomain.go:28` — rewrite description. Old:
  `"...New services need one enable call after first deploy to activate
  the L7 route..."`. New (proposal): `"...The L7 route is enabled by
  zerops_deploy on first deploy for eligible modes (dev / stage /
  simple / standard / local-stage). This tool is for explicit recovery,
  production opt-in, or disable operations..."` (MUST NOT use the verb
  family `auto-enable*` — caught by Phase 4's lint).
- `internal/content/templates/claude_shared.md:48` — rewrite the
  "skip the workflow" claim about `zerops_subdomain`. Aligned with spec
  O3 wording.

**Acceptance.**
- Visual diff review confirms both lines align with spec O3 / atoms.
- No unit tests required (these are doc/string fixes); Phase 4's
  drift lint will pin them post-fix.

### Phase 4 — Tool-description drift lint (C7)

**Touches.**
- New `internal/tools/description_drift_test.go`. Two-input scanner:
  - **Input A** — AST walk over `internal/tools/*.go` (the only
    location where `mcp.AddTool` is called; workflow/ops do not
    register MCP tools). Extract every `Description:` string from
    `mcp.Tool` literals and every `Description:` string from
    `jsonschema:"...description..."` field tags on input structs.
  - **Input B** — markdown read of `internal/content/templates/claude_*.md`
    files (CLAUDE.md generation templates).
  Run the fixed pattern set (below) against both input streams.
  **Implementation note:** this is a SEPARATE scanner, not an extension
  of `internal/content/atoms_lint.go::LintAtomCorpus` (which only walks
  the embedded atom corpus via `ReadAllAtoms()`). The two lint passes
  share the regex/violation style but have independent input scopes.

**Initial pattern set** (extends with each newly observed drift, never
shrinks):
1. `(?i)\bneed\b.*\benable\b.*\b(after|first)\b.*\bdeploy\b` —
   matches `subdomain.go:28` pattern
2. `(?i)\bskip(s|ping)? the workflow\b` — matches
   `claude_shared.md:48` pattern (caught via Input B — markdown
   template scanning, scoped above)
3. `(?i)\bauto[- ]?enable[s]?\b` in tool descriptions — same
   forbidden-verb class as atom_lint, but scoped to tool descriptions

**Out-of-scope clarification.** Lint does NOT cover
`platform.NewPlatformError` `Suggestion` strings (those are call-site
contextual, not descriptive; the typed-lookup approach was rejected
as M6 in §3 above). Lint also does NOT cover atom prose — atoms have
their own lint (`internal/content/atoms_lint.go`).

**Acceptance.**
- Lint passes after Phase 3's drift fixes land.
- Manual regression-injection: write `subdomain.go:28`'s old text into
  any tool's description; lint fails at the named pattern.
- Manual regression-injection: write a new contradictory tool
  description (e.g. `"...Run zerops_subdomain action=enable after every
  deploy..."`); lint fails.

### Phase 5 — Spec hygiene (no code change)

**Touches.**
- `docs/spec-workflows.md` §1.3, §1.6, P4: revise to:
  > **P4 (revised)**: `action="status"` returns the canonical lifecycle
  > envelope (envelope + plan + guidance) and is the supported recovery
  > primitive after context compaction. Mutation responses (start,
  > complete, strategy, close, deploy, verify, manage, scale, env,
  > mount, dev_server, subdomain) MAY be terse — their lifecycle context
  > is recovered via `status`. Free-form `next` strings remain rejected
  > everywhere; tools that point at a next action return a typed `Plan`.
- `docs/spec-knowledge-distribution.md` KD-01: align with the revised
  P4 — "every workflow-aware tool response goes through the pipeline"
  becomes "the lifecycle status response (`action="status"`) goes
  through the pipeline; mutation responses MAY".
- `CLAUDE.md` Conventions section: the existing bullet
  "No progress notification immediately before a tool response"
  unchanged. Add a new bullet:
  > **Lifecycle recovery is via `action="status"`** — mutation tool
  > responses MAY be terse (do not require the lifecycle envelope);
  > error responses MUST remain leaf payloads (`convertError` does not
  > attach an envelope). Spec: P4.
- `docs/spec-workflows.md` (same section as P4 revision): explicit
  carve-out for the error path:
  > `convertError` MUST NOT attach the lifecycle envelope to error
  > responses. Error responses remain leaf error payloads. Lifecycle
  > recovery is exclusively via `zerops_workflow action="status"`.

**Acceptance.**
- Spec changes reviewed for internal consistency (no other section
  references the old P4 wording without revision).
- The error-path carve-out appears alongside the P4 revision so the
  two halves of the contract (mutations terse, errors terse,
  recovery via status) read together.

## §5 — Phase verification gates

Per phase: `go test ./... -count=1 -short && make lint-local`.

After all five phases: `go test ./... -count=1 -race && make
lint-local && go test ./e2e/... -tags e2e -count=1`.

The plan as a whole succeeds when:

1. An LLM after compaction calls `action="status"` once and receives the
   reason for the most recent failed deploy / verify (Phase 1).
2. An atom matched via service B never substitutes service A's hostname
   in its rendered body (Phase 2 C2).
3. Synthesis errors propagate as visible platform errors instead of
   empty guidance (Phase 2 C3).
4. Production code outside parser/synthesizer/test cannot read
   `KnowledgeAtom.Body` directly (Phase 2 C4).
5. Atom corpus with malformed frontmatter fails the build instead of
   loading as wildcards (Phase 2 C5).
6. The two known-drifting strings (`subdomain.go:28`,
   `claude_shared.md:48`) align with spec O3 (Phase 3).
7. Re-introducing either drift pattern fails the build via the tool
   description drift lint (Phase 4).
8. The spec accurately describes the recovery contract — `status` is
   canonical, mutations may be terse, free-form `next` strings still
   forbidden (Phase 5).

## §6 — What we will learn from this plan

This plan deliberately ships small. After it lands, three questions
remain open and are NOT pre-answered here:

- **Does drift recur in surfaces this plan didn't lint?** If yes, add a
  narrowly-scoped lint then. Example surfaces: progress notification
  message strings, `platform.NewPlatformError` Suggestion strings,
  bootstrap transition messages.
- **Does any handler genuinely benefit from inline lifecycle envelope?**
  E.g. is there a class of error where the LLM has to call `status`
  immediately after, and that round-trip noticeably hurts user
  experience? If yes, add envelope to that specific handler — not to
  all 28.
- **Does the corpus-size cap measurement (T2 in v1) prove a real
  overflow risk?** The dispatch-brief envelope split exists for recipe
  briefs because they hit ~32KB. Runtime atoms have not been measured.
  Add a measurement gate in a separate plan if anyone reports a
  truncated response.

These questions answer with experience, not pre-design.

## §7 — Resolved: errors stay terse, recovery via `action="status"`

The earlier-open question was: should `convertError` carry the lifecycle
envelope on error responses?

**Resolved 2026-04-26 (after Codex independent review): NO.**

Three concrete reasons grounded in code, not aesthetic preference:

1. **`convertError` lacks the inputs.** `internal/tools/convert.go:43`
   takes only `err`. Computing an envelope requires `context.Context`,
   `platform.Client`, `stateDir`, `projectID`, `runtime.Info`, and the
   `*workflow.Engine`. Threading those six parameters through every
   call site is a refactor of the same shape and cost as F1
   (envelope-on-every-response), which this plan explicitly rejected.
   B/C are F1 in disguise.

2. **No observed failure mode.** Deploy errors are persisted to the
   work session BEFORE return (`internal/tools/deploy_ssh.go:135-138`,
   `internal/tools/deploy_local.go:126-129`). After Phase 1's C1, a
   follow-up `action="status"` reconstructs the failed deploy state
   from work session + ServiceMeta + live services. There is no
   code/test/eval evidence that "missing envelope on error" has caused
   any LLM mis-decision; it is a hypothesis without grounding.

3. **Error-in-error-path concern.** `ComputeEnvelope` itself can fail
   on `client.ListServices`, `ListServiceMetas`, or
   `CurrentWorkSession` (`internal/workflow/compute_envelope.go:54-72`,
   `internal/workflow/work_session.go:101-112`). Inside `convertError`,
   that secondary failure has no clean response shape — replace
   primary error (terrible), drop silently (defeats the mechanical
   guarantee), or wrap both (confused LLM). Option A avoids the entire
   class.

**Spec wording locked into Phase 5:**

> `convertError` MUST NOT attach the lifecycle envelope to error
> responses. Error responses remain leaf error payloads (code, error
> message, suggestion, apiCode, diagnostic, apiMeta). Lifecycle
> recovery is exclusively via `zerops_workflow action="status"`, which
> recomputes the canonical envelope from fresh state.

This applies to both `*platform.PlatformError` (typed) and generic
errors. Same answer for both — convertError stays a leaf serializer.

---

## Implementation order summary

```
Phase 1 (C1)           → ~2-3 days  → action=status carries failure reason
Phase 2 (C2-C5)        → ~1 week    → atom pipeline correctness
Phase 3 (C6)           → ~1 hour    → drift fixes
Phase 4 (C7)           → ~1 day     → drift lint pin
Phase 5 (spec)         → ~half day  → P4 alignment
```

Five phases. Real bugs only. No content corpus, no governance system,
no carry-along channel. After this plan, ZCP's compaction recovery
works mechanically and the two known drift patterns cannot recur
silently.
