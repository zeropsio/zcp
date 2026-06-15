# Audit: Workflow → LLM Information Flow

Date: 2026-04-25

Joint audit — Claude Code (deep dive on data-flow + handler shape) + Codex
(independent fresh-eyes pass). Existing `docs/audit-knowledge-atom-pipeline.md`
(same date) covered the atom corpus narrowly; this audit goes broader and
reaches deeper structural conclusions.

Codebase: pre-production. CLAUDE.local.md priority applies — fix at the root,
no compatibility shims, structural correction beats minimal patches.

This document is self-contained handoff material for follow-up sessions. It
does not implement fixes.

## Executive Summary

`spec-workflows.md` §1.3, §1.6, P1, P4, KD-01, KD-02; `spec-knowledge-distribution.md`
§2.2; `spec-work-session.md` §10.1 describe a uniform pipeline: every
workflow-aware tool response is `Response{Envelope, Guidance, Plan}` produced
by `ComputeEnvelope → BuildPlan → Synthesize → RenderStatus`, byte-deterministic
and compaction-safe.

The codebase only honors that contract for `action="status"` (idle/develop) and
`start workflow="develop"`. Every other workflow handler returns ad-hoc JSON or
markdown without an envelope or plan — and several silently strip the
diagnostic data the spec relies on for post-compaction re-orientation. The
atom architecture is not yet authoritative; the wire format isn't
authoritative either.

There are four fundamental issues plus the three already in
`audit-knowledge-atom-pipeline.md` (still live).

## F1. The pipeline-as-fiction: P4/KD-01 holds in 2 of 28 production handlers

**Issue.** Spec invariants P1 (`ComputeEnvelope` is the single state-gathering
entry point) and P4/KD-01 (every workflow-aware response carries
`Response{Envelope, Guidance, Plan}`) are core to the design. In code they
are honored only in `internal/tools/workflow.go::handleLifecycleStatus` and
`internal/tools/workflow_develop.go::renderDevelopBriefing`.

**Where.**

- `internal/tools/workflow.go:758` — `handleLifecycleStatus` does
  `ComputeEnvelope → LoadAtomCorpus → Synthesize → BuildPlan → RenderStatus`.
- `internal/tools/workflow_develop.go:151` — same pipeline.
- All 26 other production `internal/tools/workflow*.go` files (bootstrap,
  strategy, recipe, adopt-local, classify, record-deploy, immediate, all 22
  recipe checks) return tool-specific JSON or markdown, never the canonical
  triple. Verified with `rg -L ComputeEnvelope internal/tools/workflow*.go -g '!*_test.go'`.
- Cross-tool: `internal/tools/deploy_ssh.go`, `internal/tools/verify.go`,
  `internal/tools/subdomain.go`, `internal/tools/scale.go`,
  `internal/tools/manage.go`, `internal/tools/env.go`,
  `internal/tools/discover.go` — none carry an envelope or plan.
- `internal/tools/convert.go:77-90` (`jsonResult` / `textResult`) is the
  universal sink: every tool returns `mcp.TextContent` with either raw
  markdown OR a JSON-encoded text blob. There is no structured envelope on
  the wire.

**Why fundamental.** The compaction-safety story (`spec-work-session.md`
§10.1) only works if every tool response that touches lifecycle state
carries the envelope, so the LLM can re-derive the plan from the most recent
response in transcript. With the current shape: after `action="strategy"`
the LLM sees `{status, services, next, guidance?}`, after `action="reset"`
it sees `{cleared, preserved, next}`, after `start workflow="bootstrap"` it
sees `BootstrapDiscoveryResponse` — none of these tell the LLM "what phase
am I in, what's primary next". The LLM must follow every state-mutating
call with `action="status"` to re-orient. That's a round-trip tax with no
architectural justification — the handler already has the engine; computing
the envelope adds one parallel I/O at most.

This isn't a missing feature; it's the spec's central invariant being absent
from the wire. Patching individual handlers gets parity for a moment; the
actual fix is making the response envelope a first-class wire type that
handlers wrap their operation result inside.

**Fix direction.**

1. Define `WorkflowResponse{Envelope, Plan, Guidance, Result any}` as the
   wire-level type for all workflow-aware tools (`zerops_workflow` actions,
   `zerops_deploy`, `zerops_verify`, `zerops_subdomain`, `zerops_manage`,
   `zerops_scale`, `zerops_env`, `zerops_mount`, `zerops_dev_server`).
2. Provide a `RespondWithLifecycle(ctx, engine, ..., result any)` helper that
   runs the pipeline and merges the per-tool result. Every handler returns
   through this helper.
3. Add an architecture test: every handler in `internal/tools/` whose tool
   name is in the workflow-aware allowlist MUST call `RespondWithLifecycle`
   (AST-pinned).
4. Replace `convertError` with a lifecycle-aware version that bundles the
   current envelope + a recovery action — see F4.

This will make `ComputeEnvelope`'s parallel I/O justified (it's currently
called twice per turn at most; it would become every turn) — which is
precisely the design assumption.

## F2. Compaction-recovery is silently lossy: WorkSession captures diagnostics, the envelope deletes them

**Issue.** `spec-work-session.md` §10.1 promises that `action="status"` is
the post-compaction re-orientation call: "what failed, what next". The
persisted `WorkSession` actually carries the failure reasons. The envelope
projection drops them.

**Where.**

- `internal/workflow/work_session.go:58-72` — `DeployAttempt` carries
  `Error string`, `Setup string`, `Strategy string`; `VerifyAttempt`
  carries `Summary string`, `Passed bool`.
- `internal/workflow/envelope.go:110-114` — `AttemptInfo` has only
  `{At, Success, Iteration}`.
- `internal/workflow/compute_envelope.go:347-369` — `deployAttemptsToInfo`
  and `verifyAttemptsToInfo` literally project away `Error`, `Setup`,
  `Strategy`, `Summary`. The fields exist in `DeployAttempt` /
  `VerifyAttempt`, get persisted to `.zcp/state/work/{pid}.json`, and are
  simply not copied into the envelope.
- `internal/workflow/render.go:104-113` — `lastAttemptText` returns
  "deploy ok" / "deploy failed" / "deploy pending". No "why".

Compare `spec-work-session.md` §5.2 example:

```text
Deploys: web ✓ 3m ago | api ✗ 2 attempts (last: build timeout 1m ago)
```

Actual rendered output:

```text
Progress:
  - web: deploy ok, verify ok
  - api: deploy failed, verify pending
```

The "build timeout" reason is captured in `DeployAttempt.Error`, written to
disk, then deleted at envelope-build time. Same for `VerifyAttempt.Summary`.

**Why fundamental.** The whole compaction-survival rationale collapses.
After context compaction the LLM has nothing to act on except "deploy
failed" — same as if no work session existed at all. The spec text
describes a story the wire format cannot deliver. This is not a missing
feature in the renderer; the data is gone before the renderer sees it. Any
prose-level fix to `render.go` would have nothing to render.

**Fix direction.** Promote `Reason`, `Setup`, `Strategy` to `AttemptInfo`.
The envelope is already serialized verbatim into the response; making it
carry the reason costs only width on the wire — and it's already the
canonical post-compaction surface. `RenderStatus` then renders
"api ✗ build timeout (3m ago)". `BuildPlan` becomes more useful too: it
can pick a primary action that targets the actual failure (e.g. "fix build
timeout, redeploy api" instead of just "deploy api").

## F3. Atom matching is per-service, placeholder substitution is global — multi-service render bug

**Issue.** The synthesizer matches an atom under a per-service conjunction
(`anyServiceMatchesAll`): an atom fires when *some* service satisfies
*every* declared service-scoped axis. But placeholder substitution then
uses `primaryHostnames(env.Services)` — a global pick that selects the
alphabetically-first dynamic runtime regardless of which service made the
atom match. In multi-service projects, an atom can be triggered by service
B and rendered with service A's hostname.

**Where.**

- `internal/workflow/synthesize.go:100` — match check via
  `anyServiceMatchesAll(env.Services, atom.Axes)`.
- `internal/workflow/synthesize.go:39-44` —
  `hostname, stageHostname := primaryHostnames(envelope.Services)`
  builds a single `strings.NewReplacer` for the whole synthesis run. There
  is no per-atom rebinding to the matched service.
- `internal/workflow/synthesize.go:181-200` — `primaryHostnames` iterates
  services in envelope order (sort-by-hostname), picks the first dynamic /
  implicit-web / static service. No correlation with which atom needs to
  render.

Concrete failure mode: project has `apidev` (dynamic, mode=dev,
never-deployed) and `appdev` (dynamic, mode=dev, deployed). Atom
`develop-first-deploy-write-app` declares `modes:[dev],
deployStates:[never-deployed]`. `anyServiceMatchesAll` finds `apidev`
satisfies both axes. Atom matches. `primaryHostnames` returns `apidev`
(alphabetically first dynamic) — so this case happens to render correctly.
Reverse the alphabet (`appdev` never-deployed, `apidev` deployed): atom
still matches via `appdev`, but `{hostname}` is substituted with `apidev`
(alphabetically first), and the agent gets
`zerops_deploy targetService="apidev"` for a service that's already
deployed.

This is precisely the bug the spec §3.10 conjunction was added to prevent
— but the fix only reaches the *match* side. The *render* side is still
global.

**Why fundamental.** The atom architecture's main correctness claim is
"axes pick the right runtime cell, the renderer prints commands for that
cell." If renderer-substitution is unbound from match-context, the atom
system delivers precise-looking but wrong commands. AtomLint cannot detect
this: each atom in isolation is fine; the bug only manifests at synthesis
time with two-or-more-services state. No corpus-coverage test exercises
that shape because the test fixtures use single-service envelopes.

**Fix direction.** `Synthesize` returns `[]MatchedRender` where each entry
carries `(atom, matchedService)`; the replacer is built per-render from
`matchedService.Hostname` and its `StageHostname`. Atoms whose axes are
entirely envelope-scoped (no service-scoped axis declared) keep using
`primaryHostnames`. Multi-match atoms (more than one service satisfies all
axes) render once per matched service — or declare a "render-mode: multi"
flag that the synthesizer respects. Add a regression test with the
two-service rotation above.

## F4. Two parallel knowledge systems: atoms are governed, tool descriptions and errors are not

**Issue.** Runtime guidance lives in atoms with strict authoring contract
(`atoms_lint.go`, `references-fields` AST validation, three integrity
tests). But the LLM also reads — and acts on — three other surfaces that
are not part of that pipeline:

1. MCP tool descriptions (the `Description:` field on `mcp.AddTool`).
2. Error JSON from `convertError` (`internal/tools/convert.go:42-74`).
3. Free-form `next` strings scattered through handler responses
   (`workflow_strategy.go:155-160`, `workflow.go:474-484`,
   `strategyListResponse.Next`).

These three surfaces are written in Go, compiled in, and not subject to
atom lint, atom AST validation, or even spec invariants. They drift.

**Concrete drift, demonstrable now.**

- `internal/tools/subdomain.go:28` Description:
  > "Enable or disable zerops.app subdomain. Idempotent. **New services
  > need one enable call after first deploy to activate the L7 route.**"
- `spec-workflows.md` O3 + atom `develop-first-deploy-*`:
  > L7 subdomain activation is a deploy-handler concern, not an
  > agent-step concern. `zerops_deploy` auto-enables the subdomain on
  > first deploy for eligible modes. Agents/recipes never call
  > `zerops_subdomain action=enable` in happy path.
- The atom corpus is forbidden by lint (`atoms_lint.go` §11.2.1) from
  saying things like "the X handler auto-enables…". The tool description
  has no such restriction and tells the LLM the OPPOSITE story.

**Errors are even more silent.** `convertError` returns
`{code, error, suggestion, apiCode?, diagnostic?, apiMeta?}` as a
JSON-encoded text blob. No envelope, no plan, no atom-derived recovery
guidance. When `zerops_deploy` fails mid-flow the LLM gets a leaf error and
a hand-written suggestion string. After context compaction those errors
are gone — no work-session recovery surface helps because there's no
envelope being delivered to teach lifecycle context.

**Why fundamental.** The atom corpus solved the "many sources of truth,
drift forever" problem the spec §1 motivation describes — but only for
runtime atoms. The other three surfaces are exactly the same problem
class, untreated. Tool descriptions are read at MCP init (the only
init-time text the LLM sees), so they shape the *prior* the LLM holds when
reading any subsequent atom. A tool description that contradicts atoms
wins, because the description is read first and atoms are read per-turn.

This is also why `dispatch-brief-atom` exists: its existence (a runtime
"go fetch this atom by ID" action,
`internal/workflow/dispatch_brief_envelope.go`) signals the team has
already noticed that not all guidance fits in tool responses. The
architecture for *recipes* solved its overflow case by chunking. The
architecture for *runtime* hasn't even classified all its guidance
surfaces.

**Fix direction.**

1. **Tool descriptions.** Move them into atoms with `phases: [idle]`
   priority 1 OR into a dedicated `internal/content/tool-affordances/*.md`
   set that the registration code reads. Atom lint applies. The
   `Description:` field becomes a one-line affordance, not workflow
   doctrine.
2. **Error responses.** `convertError` becomes lifecycle-aware: it
   computes the current envelope (cheap; one Compute pass) and returns
   `{code, error, suggestion, envelope, plan}`. The LLM gets a
   compaction-safe error context: "this failed, here's where you are,
   here's what to try." Errors then teach lifecycle recovery, not just
   local fixes.
3. **`next` strings.** Delete every hand-rolled `next` string in handler
   responses; replace with the typed `Plan` (which already exists).
   `BuildPlan` extends to cover post-mutation states (e.g. "you just set
   strategy on X, primary next is `start workflow=develop`").

This is the structural correction the project's pre-production status
(CLAUDE.local.md) is designed for. The tool-description migration is the
largest single change — but it's a one-time migration that buys "one
source of truth, governed and tested" forever.

## Tier 2 — adjacent issues worth flagging but smaller in scope

### T1. Action-surface explosion on `zerops_workflow`

`internal/tools/workflow.go::handleWorkflowAction` dispatches 17 actions
including five recipe-only (`build-subagent-brief`,
`verify-subagent-dispatch`, `dispatch-brief-atom`, `classify`,
`generate-finalize`) even though the recipe workflow itself moved to
`zerops_recipe` (v3 engine). Each action returns a different JSON shape.
The schema sprawl is a direct cost of not having a uniform envelope (F1)
and not splitting recipe lifecycle into its own tool (the v3 split is
incomplete).

### T2. 32KB cap awareness is recipe-only

`internal/workflow/dispatch_brief_envelope.go` documents an MCP
tool-response cap at ~32KB. Recipes have a chunked-fetch escape hatch.
Runtime atoms don't — atom corpus is small per-match today, but
`corpus_coverage_test` only verifies non-empty, not bounded size. A
multi-service standard-mode envelope with strategy+trigger+runtime
cross-product matched atoms could plausibly approach the cap; the failure
mode would be the harness "spillover to scratch file" issue documented in
the dispatch-brief-envelope file. Add a corpus-size gate for the runtime
pipeline before it bites.

### T3. `StateEnvelope.Bootstrap` and `.Recipe` populated outside `ComputeEnvelope`

`compute_envelope.go:16-19` notes that `Bootstrap` is populated by
`bootstrap_guide_assembly.go::synthesisEnvelope`, not by `ComputeEnvelope`.
So the "single entry point" claim has a built-in carve-out. Either fold
synthesis-envelope construction into `ComputeEnvelope` (its data is read
anyway) or rename to `ComputeLifecycleEnvelope` to make the carve-out
explicit. Today the spec-vs-code mismatch is silent.

## Existing audit (`audit-knowledge-atom-pipeline.md`) findings — still active

Verified in code today. Not duplicated above; included for completeness:

- **A1.** `BootstrapState.buildGuide` swallows `Synthesize` errors with
  `return ""` (verified: `internal/workflow/bootstrap_guide_assembly.go`,
  `bodies, err := Synthesize(...); if err != nil { return "" }`). Silent
  guidance loss. Recipe-bootstrap-close atom uses unsupported `{slug}`
  placeholder which currently causes this swallow to fire.
- **A2.** `BuildStrategyGuidance` reads `atom.Body` directly
  (`internal/workflow/strategy_guidance.go:46-48`), bypassing the
  placeholder/unknown-token machinery. This is one concrete instance of
  F1 — the renderer that should run through `Synthesize` instead
  concatenates raw bodies.
- **A3.** Tolerant frontmatter parsing turns malformed
  `environments: local` (non-list form) into a wildcard via `parseYAMLList`
  returning `nil`. `internal/workflow/atom.go`. Atoms can broaden silently.

These three roll into F1 (atom pipeline not authoritative) and F4 (silent
failure in error semantics) at the structural level, but each has a
localized fix that's worth shipping independently.

## Recommended order of attack

If the goal is structural correctness (CLAUDE.local.md priority):

1. **F1 first** — wire-format envelope is the foundation everything else
   builds on. Without it, F2's diagnostic data has no carrier, F4's
   lifecycle-aware errors have nowhere to attach, and the existing
   audit's A1/A2 fixes don't cohere. ~2-3 weeks of phased migration
   across all workflow-aware tool handlers.
2. **F2** — small change (extend `AttemptInfo`, propagate through
   projection + render). Days, not weeks. Safe to do in parallel with F1.
3. **F3** — surgical change inside `synthesize.go` plus regression tests.
   A weekend.
4. **F4** — largest migration of the four. Tool-description
   move-to-atoms is a one-time touch of every `mcp.AddTool` call.
   Error-response lifecycle wrapping rides F1's helper. Free-form `next`
   strings die when F1 mandates `Plan`.
5. Existing audit's A1/A2/A3 fold in naturally during F1 + F2.

T1 (action sprawl) and T3 (envelope carve-out) are tidy-ups after the
structural work.

## Suggested Acceptance Tests

Add tests with these properties:

- Architecture test: every handler in `internal/tools/` whose tool name is
  in the workflow-aware allowlist returns through a single
  `RespondWithLifecycle` helper (AST-pinned), and the helper always
  populates `Envelope` and `Plan`.
- Wire-format test: a `zerops_deploy` failure response includes an
  envelope with the failure reason in `WorkSessionSummary.Deploys` so a
  follow-up `action=status` is not required to recover the diagnosis.
- Multi-service render test: two dynamic services `apidev` (deployed) and
  `appdev` (never-deployed); assert that
  `develop-first-deploy-write-app` renders `targetService="appdev"`,
  not `apidev`. Reverse the alphabet and assert again.
- Tool-description governance: every `mcp.AddTool` call's `Description`
  field is sourced from a content file (atom or affordance) that
  `atoms_lint` runs against — no inline string literals.
- Corpus-size gate: for representative envelopes (single dev, dev+stage,
  multi-service standard with strategy+trigger), assert that
  `Synthesize` output stays under the 28KB inline-threshold; over-cap
  triggers a chunked delivery path or a build error.

## Residual Risk

The spec is internally consistent and documents a strong design. The risk
is implementation drift: each of the four findings shows the spec saying
one thing while the code does another, with passing tests on each side
because the tests assert local invariants, not the cross-layer contract.
The acceptance tests above are deliberately cross-layer — they fail when
spec and code disagree, not just when one of the two changes shape.
