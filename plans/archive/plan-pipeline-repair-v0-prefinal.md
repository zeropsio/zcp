> **SUPERSEDED 2026-04-26.** Replaced by `plans/plan-pipeline-repair.md`
> after the user identified Posture A creep in the v1 successor of this
> document. This pre-final and its v1 sibling
> (`plan-pipeline-repair-v1-overengineered.md`) over-applied the atom
> architecture pattern (corpus + lint + frontmatter + references-fields)
> to surfaces that did not need it (tool descriptions, error suggestions,
> CLAUDE.md template). The reconciled v2 plan keeps the real bug fixes
> (F2 / F3 / A1 / A2 / A3 / drift fixes) and two narrowly-scoped lint
> pins; drops F1 full handler migration, F4b corpus, M6 RecoveryID
> system, M4 truth registry. Preserved here for audit history.
>
> ---

# Pre-Final Plan: ZCP Atom + Workflow Pipeline Repair

Date: 2026-04-25
Status: **Pre-final** — pending Codex critique pass + author re-evaluation
Successor of: `docs/audit-knowledge-atom-pipeline.md`,
`docs/audit-workflow-llm-information-flow.md`
Successor `final` doc will be: `docs/plan-pipeline-repair.md` (after critique).

---

## 0. Context

Two audits ran today:

1. **`audit-knowledge-atom-pipeline.md`** — focused on the atom corpus: parser
   tolerance, silent error swallowing, one bypass site that reads `atom.Body`
   directly. Three findings (A1, A2, A3).
2. **`audit-workflow-llm-information-flow.md`** — broader: how the LLM
   actually receives state. Four fundamentals (F1–F4) plus three Tier-2
   issues (T1–T3). Both Claude and Codex contributed; Codex independently
   surfaced F2 and F3 which the atom audit hadn't seen.

This document consolidates the combined ten findings into a single
prioritized program, surfaces the critical decision points, and explicitly
asks Codex (and a re-evaluation pass by the author) to challenge each one
before any code is written.

## 1. Engineering Constraints

From `CLAUDE.local.md`:

- **Pre-production.** No backward-compat shims. Big refactors are on the
  table.
- **Quality > speed.** Symptom patches rejected even when they "work".
- **Root cause over trigger.** Wide-and-long change is the right tool when
  fundament correction needs it.
- **Structural correction beats minimal patch** unless the patch IS the
  correct design.

From the system invariants:

- TDD mandatory; layer impact dictates which test layers must turn green.
- Atom authoring contract is unified — no per-topic contract tests.
- 350-line soft cap per `.go` file.
- Phased refactors: each phase verifiable on its own.

## 2. Combined Findings — Master List

Ten items, grouped by structural position. Order WITHIN a tier reflects
dependency, not severity.

```
TIER 1 — WIRE FORMAT & STATE PROJECTION (everything else builds on these)
  F1 — Response{Envelope, Guidance, Plan} not the wire contract
  F2 — Compaction-recovery silently lossy (Error/Setup/Strategy/Summary stripped)

TIER 2 — PIPELINE CORRECTNESS (atoms produce the wrong rendered output)
  F3 — Atom match per-service, placeholder substitution global
  A1 — Synthesis errors swallowed → empty guidance
  A2 — BuildStrategyGuidance bypasses Synthesize entirely
  A3 — Tolerant frontmatter turns malformed axes into wildcards

TIER 3 — KNOWLEDGE-SYSTEM UNIFICATION (parallel ungoverned channels)
  F4 — Tool descriptions / errors / next strings ungoverned, drift from atoms
  T1 — Action surface explosion on zerops_workflow

TIER 4 — SMALLER SCOPE-TIGHTENING (clean-up after structural work)
  T2 — 32KB cap awareness recipe-only; runtime corpus could plausibly hit it
  T3 — StateEnvelope.Bootstrap/.Recipe populated outside ComputeEnvelope
```

## 3. Per-Finding Detail

### TIER 1

#### F1 — `Response{Envelope, Guidance, Plan}` is not the wire contract

**Issue.** Spec P1, P4, KD-01: every workflow-aware response carries the
canonical triple. Code has 2 production callers of `ComputeEnvelope`
(`internal/tools/workflow.go:758`,
`internal/tools/workflow_develop.go:151`); the remaining 26 workflow
handlers + cross-tools (`zerops_deploy`, `zerops_verify`, `zerops_subdomain`,
`zerops_manage`, `zerops_scale`, `zerops_env`) return tool-specific JSON or
markdown. After every state-mutating call the LLM must double-call
`action="status"` to re-orient.

**Concrete fix.**

1. `WorkflowResponse{Envelope, Plan, Guidance, Result any}` becomes the
   wire-level type.
2. `RespondWithLifecycle(ctx, engine, ..., result any)` helper runs the
   pipeline and merges per-tool result.
3. AST-pinned architecture test: every handler in the workflow-aware
   allowlist returns through this helper.

**Conceptual gain.** Single source of "what is the current state + what
should I do" surface. Compaction recovery becomes generic instead of being
two specific code paths. Adding a new workflow-aware tool no longer requires
wiring the pipeline by hand.

**Cost.** Touches every workflow-aware handler. Phased migration ~2-3 weeks
for one engineer with care.

**Dependency.** **Required for F2 to deliver value** (otherwise the
diagnostic-rich envelope has no carrier to most response paths) and for F4's
lifecycle-aware errors.

**Open question for critique.** Is it sufficient to gate on workflow-aware
tools, or should the envelope ride EVERY tool response? Pure-read tools
(`zerops_discover`, `zerops_logs`, `zerops_events`) currently don't compute
state. Bundling envelope into them might still be cheap given parallel I/O
but adds payload weight. **Decision needed.**

#### F2 — Compaction-recovery silently lossy

**Issue.** `WorkSession.DeployAttempt.Error`, `.Setup`, `.Strategy` and
`VerifyAttempt.Summary` are persisted to disk
(`internal/workflow/work_session.go:58-72`). The envelope projection in
`compute_envelope.go:347-369` discards all four. `RenderStatus` writes only
"deploy ok / failed / pending" — no "why".

The spec example (`spec-work-session.md` §5.2) shows
"api ✗ 2 attempts (last: build timeout 1m ago)". The actual render is
"api: deploy failed". The data is captured then thrown away before reaching
the LLM.

**Concrete fix.**

1. Promote `Reason`, `Setup`, `Strategy` into `AttemptInfo`.
2. Update the projection in `compute_envelope.go` to copy them.
3. Update `lastAttemptText` in `render.go` to include the reason.
4. Update `BuildPlan` to favour failure-targeted Primary actions when a
   reason is present.

**Conceptual gain.** The compaction-survival promise of the work session
becomes real. Errors teach lifecycle recovery instead of just local context.

**Cost.** Small. Days, not weeks.

**Dependency.** Independent of F1 in implementation, but its value lands
fully only once F1 ships (otherwise only the existing 2 status-handler paths
benefit).

**Open question for critique.** Is `Reason` enough, or do we need to also
carry the last few log lines / a `lastError.code` to make BuildPlan's
targeted Primary action robust? **Decision needed.**

### TIER 2

#### F3 — Atom match per-service, placeholder substitution global

**Issue.** `synthesize.go:100`'s `anyServiceMatchesAll` correctly requires a
single service to satisfy every service-scoped axis. But the replacer at
`synthesize.go:39-44` uses `primaryHostnames(env.Services)` — alphabetically
first dynamic service — for ALL atoms in this synthesis run. An atom that
matches because service B is `never-deployed` can render commands aimed at
service A.

**Concrete fix.**

1. `Synthesize` returns `[]MatchedRender{atom, matchedService}` where the
   matched service is the one that satisfied the conjunction.
2. Per-render replacer is built from `matchedService.Hostname` and its
   `StageHostname`.
3. Atoms with no service-scoped axis keep using `primaryHostnames` (the
   "global" picker is the right shape there).
4. Multi-match atoms (more than one service satisfies) — render once per
   matched service, OR declare a `render-mode: multi` flag. **Choose
   between two options below in critique.**

**Conceptual gain.** The atom architecture's correctness claim — "axes pick
the right runtime cell, the renderer prints commands for that cell" — is
restored. Without this, the atom system can deliver precise-looking but
wrong commands, which is worse than no guidance.

**Cost.** Surgical. A weekend including regression tests.

**Dependency.** Independent of F1/F2.

**Open question for critique.** When two services satisfy the same atom's
axes, render once per match (verbose) or once with explicit
"this-applies-to-multiple" framing (terser, but the LLM has to enumerate)?
**Decision needed.**

#### A1 — Synthesis errors swallowed → empty guidance

`internal/workflow/bootstrap_guide_assembly.go::buildGuide`:

```go
bodies, err := Synthesize(envelope, corpus)
if err != nil {
    return ""
}
```

Empty string on synthesis error masquerades as "intentionally no guidance".
Recipe-bootstrap-close atom hits this today via unsupported `{slug}`
placeholder.

**Fix.** Errors propagate to the response boundary as platform errors. No
empty fallback. Add a synthesis-error fixture test that asserts the caller
fails loudly.

**Conceptual gain.** "Empty guidance" becomes a meaningful signal again
(intentional silence). Every silent-fail vector is a defect waiting for the
next caller to write `_, _ := …` and bypass it for real.

#### A2 — `BuildStrategyGuidance` bypasses `Synthesize`

`internal/workflow/strategy_guidance.go:46-48`:

```go
parts := make([]string, 0, len(matched))
for _, atom := range matched {
    parts = append(parts, atom.Body)
}
```

Direct `atom.Body` read. Bypasses placeholder substitution, unknown-token
checking, service-scoped conjunction. Result: literal `{hostname}` can leak
to the LLM.

**Fix.** Construct a minimal `StateEnvelope` from target service metas, call
`Synthesize`. Add an architecture test that rejects production reads of
`KnowledgeAtom.Body` outside parser/synthesizer/test code (this also
prevents future regressions).

**Conceptual gain.** "Atom = input to synthesizer" becomes invariant.
Direct-body reads stop being a private affordance any handler can grab.

#### A3 — Tolerant frontmatter parsing → silent wildcards

`internal/workflow/atom.go::parseYAMLList` returns `nil` for non-empty
non-bracketed values. `nil` axis means wildcard. So
`environments: local` (intended as scoped) parses as
`environments: []` (wildcard).

**Fix.** Reject non-empty malformed list values at parse time. Validate all
frontmatter keys against a known set. Validate enum values for every axis.
Reject missing required metadata consistently.

**Conceptual gain.** Author-time configuration is treated as configuration,
not user input. The default failure mode shifts from "match everything" to
"build error". Atom corpus correctness is enforced before runtime.

### TIER 3

#### F4 — Two parallel knowledge systems

**Issue.** Atom corpus is governed (`atoms_lint.go`, AST validation, three
integrity tests). Three other guidance surfaces are not:

1. **MCP tool descriptions** (`Description:` field on `mcp.AddTool`).
2. **Error JSON** from `convertError`
   (`internal/tools/convert.go:42-74`).
3. **Free-form `next` strings** scattered through handler responses.

Concrete drift:
`internal/tools/subdomain.go:28` Description says "New services need one
enable call after first deploy". Spec O3 + atom corpus say the opposite
(deploy auto-enables; agents never call enable in happy path). MCP
descriptions are read at init — they shape the LLM's prior before any atom
fires. A description that contradicts atoms wins by recency.

**Concrete fix.**

1. **Tool descriptions.** Move into atom corpus (`phases:[idle]` priority 1)
   OR into a dedicated `internal/content/tool-affordances/*.md` set. Atom
   lint applies. The `Description:` becomes a one-line affordance, not
   workflow doctrine. Build-time generator wires descriptions from content.
2. **Error responses.** `convertError` becomes lifecycle-aware: bundles
   envelope + plan with the error. LLM gets a compaction-safe error
   context. (Only viable AFTER F1 ships.)
3. **`next` strings.** Delete every hand-rolled `next` string in handler
   responses; replace with the typed `Plan`. `BuildPlan` extends to cover
   post-mutation states.

**Conceptual gain.** "One source of truth, governed and tested" becomes
true for ALL guidance surfaces, not just atoms. Tool descriptions stop
being a shadow corpus that nobody lints.

**Cost.** Tool-description migration is the largest single touch. Touches
every `mcp.AddTool` call site. Estimated 1 week including tests.

**Dependency.** F4.2 (lifecycle-aware errors) requires F1. F4.1 (tool
descriptions) and F4.3 (next strings) are independent.

**Open question for critique.** Should tool descriptions live as full atoms
(rich, lintable, per-LLM-state filterable) or as simpler affordance-only
markdown? Atoms imply state-aware filtering of tool descriptions per-turn,
which MCP doesn't support natively (descriptions are sent at init only).
**Decision needed.**

#### T1 — Action-surface explosion on `zerops_workflow`

`handleWorkflowAction` dispatches 17 actions, including five recipe-only
(`build-subagent-brief`, `verify-subagent-dispatch`,
`dispatch-brief-atom`, `classify`, `generate-finalize`) even though the
recipe v3 engine moved to `zerops_recipe`. Each action returns a different
JSON shape.

**Fix.** Move recipe-only actions into `zerops_recipe`. Audit remaining
actions for shape uniformity (after F1 + F4.3, every workflow-aware action
returns `WorkflowResponse`).

**Conceptual gain.** Tool boundaries align with workflow responsibilities;
no leftover tendrils after the v3 split.

**Dependency.** Independent of F1, but easier to do once F1's uniform
response is in place.

**Open question for critique.** Is splitting recipe-related actions out a
genuine improvement, or busywork? They live on `zerops_workflow` historically
because the v2 engine wasn't yet split. v3 split was done; this is finishing
that. **Decision needed.**

### TIER 4

#### T2 — 32KB cap awareness recipe-only

`internal/workflow/dispatch_brief_envelope.go` documents the ~32KB MCP
tool-response cap and chunks recipe briefs accordingly. Runtime atom
synthesis has no equivalent gate — `corpus_coverage_test` only verifies
non-empty, not bounded size. A multi-service standard-mode envelope with
strategy×trigger×runtime cross-product matched atoms could plausibly
approach the cap.

**Fix.** Add a corpus-size gate test: for representative envelopes (single
dev, dev+stage, multi-service standard with strategy+trigger combinations),
assert `Synthesize` output stays under the inline-threshold; over-cap
triggers either a chunked delivery path or a build error.

**Conceptual gain.** Latent-failure mode prevented before it bites.

**Dependency.** Independent.

**Open question for critique.** If a representative envelope DOES exceed
the cap, what's the right response — chunk like recipes do, or trim atoms
by priority? **Decision needed.**

#### T3 — `StateEnvelope.Bootstrap`/`.Recipe` populated outside `ComputeEnvelope`

`compute_envelope.go:16-19` notes Bootstrap is populated by
`bootstrap_guide_assembly.go::synthesisEnvelope`. So "single entry point"
has a built-in carve-out. The spec-vs-code mismatch is silent.

**Fix.** Either fold synthesis-envelope construction into `ComputeEnvelope`
(its data is already read) or rename to `ComputeLifecycleEnvelope` and add
a sister `ComputeBootstrapEnvelope`. Either way, the carve-out becomes
explicit and testable.

**Conceptual gain.** Removes a footgun for new contributors who read the
"single entry point" claim and wire to `ComputeEnvelope`, only to find
`Bootstrap == nil` for non-bootstrap callers.

**Dependency.** Independent. Can land alongside F1.

## 4. Implementation Phasing (Pre-Critique Sketch)

This is the order I'd propose if every finding survives critique.

```
Phase 1 — Wire Format Foundation (F1 + F2 + T3)
  - Define WorkflowResponse{Envelope, Plan, Guidance, Result any}
  - Promote Reason/Setup/Strategy/Summary into AttemptInfo
  - RespondWithLifecycle helper
  - Architecture test pinning the wire contract
  - Either fold bootstrap envelope or split function names
  - All workflow-aware tools migrated through helper
  Acceptance: every workflow-aware response carries an envelope; failure
  reason from work session reaches the LLM without action=status.

Phase 2 — Atom Pipeline Authoritativeness (A1 + A2 + A3)
  - Strict frontmatter parsing
  - Architecture test rejecting production reads of atom.Body outside
    parser/synthesizer/test code
  - BuildStrategyGuidance routed through Synthesize
  - Synthesis errors propagate to response boundary
  Acceptance: malformed atom = build error; no atom-body reads outside the
  pipeline; synthesis errors are visible.

Phase 3 — Multi-Service Render Correctness (F3)
  - Synthesize returns []MatchedRender
  - Per-render replacer bound to matchedService
  - Multi-match atom handling decision applied
  - Regression test: alphabet-rotated dev/stage pair
  Acceptance: an atom matched via service B never substitutes service A
  hostnames.

Phase 4 — Knowledge-System Unification (F4)
  - Tool descriptions migrated into governed surface
  - convertError bundles envelope + plan
  - Free-form next strings replaced with typed Plan
  - Architecture test: every mcp.AddTool sources description from content
  Acceptance: AtomLint catches a contradictory tool description; error
  responses survive compaction; no inline next strings.

Phase 5 — Boundary tidy-up (T1 + T2)
  - Recipe-only actions moved to zerops_recipe
  - Corpus-size gate added with chunked-or-trim decision applied
  Acceptance: zerops_workflow surface narrowed; representative envelopes
  fit the cap.
```

## 5. Critical Questions to Resolve in Critique

These need a deliberate answer before phase 1 starts. Any "we'll figure it
out during implementation" answer is a bug.

### Q1 — Envelope on every tool, or only workflow-aware?
F1 currently scopes the wire-format change to "workflow-aware" tools.
Should pure-read tools (`zerops_discover`, `zerops_logs`, `zerops_events`)
also carry the envelope? Cheap given parallel I/O, but adds payload weight
and makes the contract universal. Universal contracts age better — but
read-only tools may not benefit.

### Q2 — How much failure context is enough?
F2 promotes Reason/Setup/Strategy/Summary. Should we also carry
`lastError.code`, the last N log lines, the iteration tier? Where does
"enough for compaction recovery" stop and "stuffing the envelope" start?

### Q3 — Multi-match atom render: per-service vs aggregate?
F3 needs one of two paths — render once per matched service (verbose, may
exceed cap) OR render once with multi-service framing (compact but the LLM
must enumerate). Which preserves the correctness claim while staying under
the size budget?

### Q4 — Tool descriptions as atoms or as a separate corpus?
F4 has two viable shapes. Atoms imply state-filtered tool descriptions per
turn (which MCP doesn't natively support — descriptions are sent at init).
A separate `tool-affordances/` corpus is simpler but creates a second
content-set that needs its own lint pass. Does the team value the unified
content surface enough to invent the indirection?

### Q5 — Recipe action split: do or defer?
T1 finishes the v3 engine split. Independently valuable but a sizable
touch. Worth the cost now (during the bigger refactor), or postpone until
its own session?

### Q6 — 32KB chunk vs trim?
T2 needs a policy. Recipes chunk via dispatch-brief-atom round-trips.
Runtime could do the same, OR drop low-priority atoms when budget is tight.
Chunking preserves all guidance at the cost of round-trips; trimming
preserves single-call latency at the cost of completeness. Which fits
runtime guidance shape?

### Q7 — Are there findings the audits missed?
Both audits focused on the runtime atom pipeline and its delivery. Were
there other channels we didn't probe? Examples to consider:
- Bootstrap state machine transitions (do they leak detail to the LLM?)
- Progress notification message strings (governed by what?)
- The `subagent-brief` system (lives in workflow but is recipe-only —
  audited?)
- CLAUDE.md generation (touched by atoms? by templates? both?)
- Eval scenarios (do the prompts encode guidance the atom corpus should own?)

## 6. What "Final Plan" Will Look Like

After Codex critique + author re-evaluation:

- Each finding has a verdict: **keep / merge / drop / postpone**.
- Each kept finding has its open questions resolved with a written
  decision.
- Each phase has acceptance gates expressed as concrete tests.
- The phasing order is committed and dependency-checked.
- A "what we deliberately did NOT do" section captures dropped/postponed
  items so the next contributor doesn't re-litigate them.

The final plan saves to `docs/plan-pipeline-repair.md`. This pre-final
file gets archived to `plans/archive/plan-pipeline-repair-prefinal.md` once
the final lands.

## 7. Critique Prompt for Codex

Codex is asked to read this document and answer:

1. For each of F1–F4, A1–A3, T1–T3: is it genuinely fundamental, or does
   the framing inflate severity? Be specific about what failure mode
   actually manifests in production today.
2. Which fixes solve root causes vs. polish symptoms? Which can be skipped
   without regret in a year?
3. Q1–Q7: what's your answer? Pick a side; "depends" answers are not
   accepted.
4. Are there findings the audits missed? Especially in the surfaces named
   in Q7.
5. Phase ordering: any reordering that gets more value sooner, or that
   reduces interdependency risk?
6. Any finding that should be split into two work items? Any that should
   be merged?

Codex's reply, plus a parallel author re-evaluation, drives the final
plan.
