# Plan: Atom corpus verification — composition-level correctness via golden scenarios

**Status**: Proposed.
**Surfaced**: 2026-05-02 — recipe-route session exposed a class of bugs (atoms making declarative-state assertions that lie for some configurations matching their axes); independent corpus audit identified ~13 LIE-CLASS atoms across the corpus plus one architectural gap (export sub-status overmatch) plus one architectural duplication (export atom guidance lives in atoms AND in `handleExport` inline strings — drift surface). Existing test infrastructure pins atom selection via `requireAtomIDsContain` / `requireAtomIDsExact` in `scenarios_test.go` but does not pin the rendered guidance text the agent actually receives.

This plan is self-contained — no external references required beyond `CLAUDE.md`, `CLAUDE.local.md`, and the source tree.

---

## How an LLM implementer should approach this plan

1. **Read top-to-bottom before starting any phase.** Architecture sections introduce decisions that downstream phases depend on.
2. **Order is strict.** Phase 0a → 0b → 0c → 1 → 2 → 3 → 4 → 5. Skipping or reordering invalidates downstream assumptions.
3. **TDD per CLAUDE.md.** Every step is marked `RED` (failing test first), `GREEN` (implementation makes test pass), `(audit)` (no test, produces artifact), or `(doc)` (documentation-only).
4. **Pause points are explicit.** Phase 2 step 1, step 2, and step 8 require human approval before continuing — DO NOT improvise past them.
5. **Cycle cap is hard.** Phase 2 has max 3 batch iterations. If defects remain after 3rd cycle, STOP and return to human with structural-issue analysis.
6. **No silent decisions.** Every `audit` step has an explicit output format and decision criteria. If criteria don't match observed reality, STOP and ask — do not invent a path.
7. **Acceptance gates per phase are deterministic.** `go test ...` commands and file existence checks. No subjective "looks reasonable" judgments.

---

## Why

The atom corpus is composed at runtime: envelope → `Synthesize` (axis filter + priority sort) → rendered guidance. Correctness is a **composition property**, not a per-atom property. Today's tests verify which atom IDs render for a scenario; they do not verify what the composed text says, whether two atoms contradict each other in the same render, whether priority ordering is sane, or whether the prose lies for some configurations matching the atom's axes.

Two architectural defects exposed by the recipe-route session:

1. **State-declarative-leak class.** 13 atoms assert state ("mount is empty", "every service is ACTIVE", "you are at status X") that holds for some envelope configurations matching their axes but not others. Existing scenario tests pinned atom selection, so the lies passed CI.

2. **Export sub-status overmatch + dual source of truth.** 6 export atoms render simultaneously during `export-active` because the atom-model has no `exportStatus:` axis to gate them — `internal/workflow/scenarios_test.go::TestScenario_S12_ExportActiveEmptyPlan` currently pins this overmatch as expected behavior. **Worse**: `internal/tools/workflow_export.go::handleExport` bypasses atom synthesis entirely (`workflow_export.go:36-39` says verbatim "no atom-axis routing ... SynthesizeImmediatePhase passes no service context, so service-scoped axes silently never fire"), returning hardcoded inline `guidance` and `nextSteps`. Same content lives in atoms AND in handleExport branches → drift surface. Agent calling `workflow="export"` gets inline strings; agent calling `action="status"` during export-active gets atom-rendered output. Two surfaces can drift independently.

The fix is structural: shift verification from atom selection to rendered text, build a 30-scenario golden suite reviewed by humans (with Codex pair-review limited to mechanical contradiction-spotting), close the export sub-status gap with a real axis, **route handleExport through atom synthesis with explicit service context** (eliminates dual source of truth AND the silent-axis-non-firing problem), codify the procedural-form principle in the authoring spec, and add narrow commit-time enforcement (lint + coverage gate test).

---

## Architecture — 3 verification layers + 1 commit-time discipline

```
┌─────────────────────────────────────────────────────────────┐
│  L1: BY CONSTRUCTION (partial)                              │
│  Procedural-form principle (§11 spec) +                     │
│  exportStatus: axis (atom-model) +                          │
│  handleExport routed through atom synthesis with service    │
│    context (service-scoped axes can fire on targetService)  │
│  → atoms guided away from lying via spec discipline +       │
│    structural axes; full coverage requires L2+L4 below.     │
│    NOT "atoms cannot lie by definition" — partial guard.    │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│  L2: GOLDENS                                                │
│  30 canonical scenarios in testdata/atom-goldens/           │
│  Reviewed text pinned; CI compares deterministically;       │
│  ZCP_UPDATE_ATOM_GOLDENS=1 regenerates locally only         │
│  → composition correctness gated at every PR                │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│  L3: COMMIT-TIME DISCIPLINE                                 │
│  Narrow Axis O lint (5 high-signal anti-phrases) +          │
│  Coverage gate test (every atom in ≥1 golden expected set   │
│    OR atom frontmatter coverageExempt: <reason>) +          │
│  marker convention for intentional invariants               │
│  → forces principle compliance OR explicit waiver           │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│  L4: LIVE EVALS (existing, eval-zcp project)                │
│  Periodic real-platform verification                        │
│  → catches fixture-reality drift                            │
└─────────────────────────────────────────────────────────────┘

(implicit base: parser tests, references-fields/atoms integrity,
 K/L/M/N atom prose lints — existing, untouched)
```

### Failure-mode coverage

| Failure mode | Caught by |
|---|---|
| Author writes a known anti-phrase (`is empty`, `every service`, etc.) | L3 lint at commit time |
| State-leak in atom prose surfacing in a covered scenario | L2 golden diff |
| Two atoms contradict each other in the same composed render | L2 — composed text shows it (caveat: only for atom pairs that co-fire in one of the 30 scenarios; see Risks) |
| Atom fires in wrong scenario (axis-tagging error) | L2 golden diff |
| Priority/order regression | L2 golden diff |
| New atom whose axes match no scenario | L3 coverage gate test fails CI |
| State-leak in atom rendered ONLY in envelope shape outside the 30 scenarios | **Not caught at commit time** — surfaces post-merge via L4 live eval or production friction |
| Real envelope shape differs from fixture | L4 live eval + Phase 2 pre-bless spot-check |
| Export atom prose drifts from handleExport inline guidance | **Eliminated**: handleExport now renders atoms via synthesis with service context (Phase 0b) — single source for prose; `nextSteps` retains separate drift surface (see Phase 0b documentation). |

### LLM-as-judge — explicitly deferred (not in scope)

Considered as a fifth layer. Defer to follow-up plan — promote when corpus exceeds ~50 scenarios or when observed quality regression demands automated semantic checks. Adds non-deterministic verification layer that itself needs calibration; not justified at 30 scenarios.

---

## Architectural decision — service context in export synthesis (load-bearing for Phase 0)

**The problem**: existing `SynthesizeImmediatePhase` passes no service context, so service-scoped axes (`runtimes:`, `serviceStatus:`, `closeDeployModes:`, `gitPushStates:`, `buildIntegrations:`, `modes:`) **silently never fire** when called from `handleExport`. This is a documented constraint per `workflow_export.go:36-39` and was the original reason export bypassed synthesis.

**Default decision**: `BuildExportEnvelope` populates Services based on three cases:

| Status case | Services slice | Rationale |
|---|---|---|
| `scope-prompt` (target unknown — UX is asking which service) | `[]` (empty) | No targetService selected yet. Atoms for scope-prompt MUST NOT use service-scoped axes (only `phases: [export-active], exportStatus: [scope-prompt]`). |
| Other 6 statuses (target known) | `[snapshot for targetService]` (single entry) | Service-scoped axes fire on the actual service being exported. |

**Audit confirmation step (Phase 0a.0 below)**: explicit audit of current 6 export atoms + 2 planned new atoms. Decision criteria:

- **Pass option (single-entry Services)** if audit finds NO existing or planned atom asserts about non-target services (managed deps, other runtimes). This is the expected outcome — current export atoms describe the export workflow for the target, not project-wide reasoning.
- **Upgrade to full Services + ExportTarget field** if audit finds at least one atom requires reasoning about non-target services. In that case, `BuildExportEnvelope` populates ALL project services + sets `env.ExportTarget = targetServiceHostname` for atoms to filter explicitly.

Audit output is a committed artifact (`internal/workflow/synthesize_export_audit.md`) with one row per atom: atom-id | matches scope-prompt? | references non-target service? | decision rationale.

This decision is **load-bearing for Phase 0a** — implementer needs explicit semantics for service population before writing the helper. The audit is mandatory; do not skip.

---

## The procedural-form principle (Phase 3 spec amendments)

**§11.1 bullet 7 (positive):**

> 7. **Procedural rules over declarative state.** An atom MAY assert a fact about the world iff that fact is universally true across every envelope configuration the atom's frontmatter axes match. State that varies WITHIN matching axes must be inspected (instruct the agent to consult the observable signal and act on what's seen) or the axis must be tightened to a scope where the assertion is universal. Verbs that implicitly presume a starting state (`scaffold` → `establish`, `write from scratch` → `adapt or scaffold`) carry the same risk.

**§11.2 bullet 5 (negative):**

> 5. **Configuration-conditional state as universal fact.** *"Bootstrap does NOT ship a stub"* (lie for recipe-route), *"every service reports ACTIVE"* (managed services report RUNNING), *"the dev process is already running"* (post-redeploy / first-time break it), *"Recipes with X..."* (pattern reachable from any route), *"You are at status=X"* (when other atoms in the same phase assume different status). Use disk/status inspection, a tighter frontmatter axis, or a new axis if the atom-model lacks the dimension you need to filter.

**§11.x rule — verify-state cannot be asserted from `deployStates: [deployed]` alone:**

> Atoms MUST NOT assert that verify has passed unless the envelope axis model exposes that condition explicitly. The synthesizer derives only `never-deployed` vs `deployed` from `ServiceSnapshot.Deployed` (`internal/workflow/synthesize.go::envelopeDeployStateMatches`); a service can be `deployed=true` yet have a failing verify. If the atom needs to differentiate, propose a new axis (e.g. `verifyState:`) — do not encode the assertion in prose.

**§11.x rule — non-obvious platform-mechanics claims require evidence:**

> When an atom asserts platform behavior beyond what `references-fields`/`references-atoms` covers (e.g. "Zerops L7 balancer terminates SSL", "managed services report RUNNING not ACTIVE", "subdomain takes 440ms-1.3s to propagate"), the assertion MUST be backed by either (a) an observable response field cited in `references-fields`, (b) a comment pointer to a live-eval that exercised the claim, or (c) a Zerops docs reference. This is a review gate, not a lint — reviewer asks "where's the evidence?" for any non-obvious mechanics claim during golden bless.

---

## The 30 canonical scenarios

Each scenario pins:
- typed Go envelope fixture in `internal/workflow/scenarios_test.go` (or new `scenarios_fixtures.go` if size warrants)
- reviewed golden body in `internal/workflow/testdata/atom-goldens/<scenario-id>.md`
- expected atom ID order in golden frontmatter

| # | scenario-id | Phase / shape |
|---|---|---|
| 1 | `idle/empty` | Idle, no services |
| 2 | `idle/bootstrapped-with-managed` | Idle, runtime + DB bootstrapped, deployed |
| 3 | `idle/adopt-only` | Idle, unmanaged runtime, no meta |
| 4 | `idle/incomplete-resume` | Idle, resumable runtime |
| 5 | `bootstrap/recipe/provision` | recipe route, provision step, ACTIVE service |
| 6 | `bootstrap/recipe/close` | recipe route, close step |
| 7 | `bootstrap/classic/discover-standard-dynamic` | classic route, dynamic standard pair |
| 8 | `bootstrap/classic/provision-local` | classic route, local env |
| 9 | `bootstrap/adopt/discover-existing-pair` | adopt route, existing dev/stage |
| 10 | `develop/first-deploy-dev-dynamic-container` | develop-active, dev mode, never-deployed, container |
| 11 | `develop/first-deploy-recipe-implicit-standard` | develop-active, php+DB, implicit-webserver, standard pair, never-deployed |
| 12 | `develop/post-adopt-standard-unset` | adopted/running, standard pair, deployed, closeMode unset |
| 13 | `develop/mode-expansion-source` | deployed dev/simple, closeMode auto |
| 14 | `develop/steady-dev-auto-container` | deployed dev dynamic, auto, container |
| 15 | `develop/standard-auto-pair` | dev+stage pair, auto, deployed |
| 16 | `develop/git-push-configured-webhook` | standard, git-push, configured, webhook |
| 17 | `develop/git-push-unconfigured` | standard, git-push, unconfigured |
| 18 | `develop/failure-tier-3` | failed deploy history, iteration 3, session active |
| 19 | `develop/multi-service-scope-narrow` | several runtimes, work session scope = one host |
| 20 | `develop/closed-auto-complete` | develop-closed phase, closeReason=auto-complete |
| 21 | `develop/closed-iteration-cap` | develop-closed phase, closeReason=iteration-cap (`internal/workflow/work_session.go::CloseReasonIterationCap`, emitted by `internal/workflow/engine.go:199`) |
| 22 | `strategy-setup/container-unconfigured` | strategy-setup, container, gitPushState unconfigured |
| 23 | `strategy-setup/configured-build-integration` | strategy-setup, configured, buildIntegration none |
| 24 | `export/scope-prompt` | export-active + `exportStatus=scope-prompt`, **target empty** (per service-context decision); fixture sets `Services: []` |
| 25 | `export/variant-prompt` | export-active + `exportStatus=variant-prompt`, target = standard-pair-dev |
| 26 | `export/scaffold-required` | export-active + `exportStatus=scaffold-required` |
| 27 | `export/git-push-setup-required` | export-active + `exportStatus=git-push-setup-required` |
| 28 | `export/classify-prompt` | export-active + `exportStatus=classify-prompt` |
| 29 | `export/validation-failed` | export-active + `exportStatus=validation-failed` |
| 30 | `export/publish-ready` | export-active + `exportStatus=publish-ready` |

The set is **canonical lifecycle states + focused submatrices** on dimensions where the atom-model has known discriminators. Not a Cartesian product — that would balloon to hundreds without proportional value.

**Coverage expansion policy** is in Phase 5.

---

## Export status → atom mapping (Phase 0b reference)

The export workflow currently emits 7 distinct `status` values (per `internal/tools/workflow_export.go` — search `"status":` literals). After Phase 0b, each maps to exactly one status-specific atom rendered into the response, with `export-intro` providing universal framing:

| # | Export status | Atom file | Status | Action |
|---|---|---|---|---|
| 1 | `scope-prompt` | `export-scope-prompt.md` | NEW | Write atom (~10-15 lines prose). NO service-scoped axes (target unknown at scope-prompt). Migrate inline guidance. |
| 2 | `variant-prompt` | `export-variant-prompt.md` | NEW | Write atom (~10-15 lines prose); migrate inline guidance |
| 3 | `scaffold-required` | `scaffold-zerops-yaml.md` | EXISTING | Add `exportStatus: [scaffold-required]` frontmatter; migrate inline guidance to body if not already present |
| 4 | `git-push-setup-required` | `export-publish-needs-setup.md` | EXISTING | Add `exportStatus: [git-push-setup-required]` frontmatter; migrate inline guidance |
| 5 | `classify-prompt` | `export-classify-envs.md` | EXISTING | Add `exportStatus: [classify-prompt]` frontmatter; migrate inline guidance |
| 6 | `validation-failed` | `export-validate.md` | EXISTING | Add `exportStatus: [validation-failed]` frontmatter; migrate inline guidance |
| 7 | `publish-ready` | `export-publish.md` | EXISTING | Add `exportStatus: [publish-ready]` frontmatter; migrate inline guidance |
| (universal) | (any export-active) | `export-intro.md` | EXISTING | Decision during Phase 0b atom review: no `exportStatus:` filter (fires for all export-active) OR specific subset. Default: no filter — provides universal framing across all statuses. |

After Phase 0b:
- `guidance` field becomes composed atom-rendered output.
- `nextSteps` array stays as inline structural data (carries dynamic content like `targetService` hostname).
- **Documented exception**: `nextSteps` remains inline because (a) it carries per-call dynamic content (URLs, hostnames) that atoms don't have template substitution for, (b) it's structurally short (<3 entries × <80 chars), (c) prose-like guidance content belongs in atoms.
- **Tripwire**: if any `nextSteps` entry exceeds 80 characters or starts looking like prose explanation rather than action description, return that content to atom body. Phase 4 lint rule (extension): `nextSteps` entries longer than 80 chars or matching regex `(because|so that|in order to|note that|explanation)` flagged for review.

---

## Phases

Each phase lands as one or more atomic commits. Each phase is verifiable green before the next starts. Phase 0 has hard dependencies that everything else depends on, so it splits into three sub-phases (0a → 0b → 0c). **Every implementation step follows TDD** per CLAUDE.md `RED → GREEN → REFACTOR`: tests are written failing before implementation, then made green, then refactored.

### Phase 0a — `exportStatus:` axis (atom-model architecture)

Pure infrastructure: add the axis to the model + helper API. **No production behavior change yet** — atoms not migrated, `handleExport` not refactored. Verifies in isolation.

| Step | Mode | File | Change |
|---|---|---|---|
| 0a.0 | (audit) | `internal/workflow/synthesize_export_audit.md` (new) | **Mandatory pre-implementation audit.** For each of 6 existing export atoms (`export-intro`, `export-publish`, `export-publish-needs-setup`, `export-classify-envs`, `export-validate`, `scaffold-zerops-yaml`) AND for each of 2 planned new atoms (`export-scope-prompt`, `export-variant-prompt` — sketch from current inline guidance content), answer in audit doc: <br/>1. Does atom prose reference any service other than the export target (managed deps by name, sibling runtimes, project-wide list)? <br/>2. If YES: which non-target services and why? <br/>3. Does atom semantically need to fire in scope-prompt state (target unknown)? <br/>**Decision rule**: if all answers to Q1+Q2 = "no non-target reference", commit decision = `single-entry Services` (default). If any atom needs non-target reference, escalate to user with finding — do NOT proceed; this changes Phase 0a.6 helper signature. |
| 0a.0.5 | (audit) | `internal/workflow/synthesize_cache_audit.md` (new, optional 1-page note) | **Synthesize cache verification.** Grep for any `Synthesize` caching layer (cache by envelope hash, memoization). If found: verify cache key includes `ExportStatus` field (post-Phase 0a.4) so envelopes with same Phase but different ExportStatus produce different cache entries. Currently `internal/workflow/synthesize.go` does NOT cache (verify by reading). If verification passes: empty audit doc with `"No cache layer found — Synthesize is stateless per call. ExportStatus field addition has no cache key impact."`. If cache exists: STOP, escalate to user. |
| 0a.1 | RED | `internal/workflow/atom_test.go` | Write failing parser test: atom with `exportStatus: [scope-prompt, publish-ready]` parses into `AxisVector.ExportStatuses`; unknown values rejected with parser error. Fails because field doesn't exist. |
| 0a.2 | GREEN | `internal/workflow/atom.go` | Add `AxisVector.ExportStatuses []string`; add `"exportStatus"` to `validAtomFrontmatterKeys` + `listAxisKeys`; parser; enum validation rejects values outside the closed set (`scope-prompt`, `variant-prompt`, `scaffold-required`, `git-push-setup-required`, `classify-prompt`, `validation-failed`, `publish-ready`). Test passes. |
| 0a.3 | RED | `internal/workflow/synthesize_test.go` | Write failing filter test: atom with `exportStatus: [publish-ready]` matches when `env.ExportStatus == "publish-ready"`, doesn't match otherwise; doesn't match when env has no ExportStatus. Fails because envelope field doesn't exist. |
| 0a.4 | GREEN | `internal/workflow/envelope.go` + `synthesize.go::atomEnvelopeAxesMatch` | Add `StateEnvelope.ExportStatus string` field; filter logic mirroring `serviceStatus:` axis pattern. Test passes. |
| 0a.5 | RED | `internal/workflow/synthesize_export_test.go` (new) | Write failing test for helper API: <br/>(a) `BuildExportEnvelope("appdev", ExportStatusPublishReady, opts)` returns envelope with `Phase=PhaseExportActive`, `ExportStatus="publish-ready"`, `Services=[snapshot for "appdev"]`. <br/>(b) `BuildExportEnvelope("", ExportStatusScopePrompt, opts)` returns envelope with `Services=[]` (empty target case). <br/>(c) `RenderExportGuidance(env, corpus)` returns non-empty string when matching atoms exist. <br/>Fails because helpers don't exist. |
| 0a.6 | GREEN | `internal/workflow/synthesize_export.go` (new) | Implement helpers with concrete signatures: <br/>```go<br/>type ExportEnvelopeOpts struct {<br/>    Client     platform.Client  // for live ServiceStack lookup<br/>    ProjectID  string           // for ListServices call<br/>    StateDir   string           // for ServiceMeta lookup<br/>}<br/>func BuildExportEnvelope(targetServiceHostname string, status ExportStatus, opts ExportEnvelopeOpts) (StateEnvelope, error)<br/>func RenderExportGuidance(env StateEnvelope, corpus []KnowledgeAtom) (string, error)<br/>```<br/>**Implementation behavior**: <br/>• If `targetServiceHostname == ""`: returns envelope with `Services: []`, `Phase: PhaseExportActive`, `ExportStatus: status`. <br/>• Else: calls `opts.Client.ListServices(ctx, opts.ProjectID)` to fetch live ServiceStack; reads `ServiceMeta` from `opts.StateDir`; calls existing `buildOneSnapshot(serviceStack, meta, ws)` (located in `compute_envelope.go:198`) to construct snapshot for target; returns envelope with `Services: [snapshot]`. <br/>• `RenderExportGuidance` calls `Synthesize(env, corpus)`, joins `MatchedRender.Body` for each match in priority order. Test passes. |
| 0a.7 | RED+GREEN | `internal/workflow/synthesize_export_test.go` | Pin service-scoped axis behavior: atom with `runtimes: [implicit-webserver]` AND `exportStatus: [scaffold-required]` matches when targetService is php-nginx in scaffold-required state; doesn't match for dynamic runtime; doesn't match when target is empty (scope-prompt). **This explicitly verifies the service-context decision is implemented.** |

**Acceptance**: `go test ./internal/workflow/... -race -count=1` green. Audit artifacts (`synthesize_export_audit.md`, `synthesize_cache_audit.md`) committed and confirm single-entry Services + no cache impact. New axis parses and filters correctly. Helpers ready for Phase 0b. No atom or handler changes yet.

### Phase 0b — Migrate atoms + reroute `handleExport` through synthesis

The substantive change. Atoms gain `exportStatus:` frontmatter, two new atoms written, `handleExport` refactored to render atoms instead of hardcoded inline guidance.

| Step | Mode | File | Change |
|---|---|---|---|
| 0b.0 | (audit) | `internal/tools/workflow_export_phrase_audit.md` (new) | **Pre-step phrase classification.** Extract per-status inline guidance strings from `workflow_export.go` (lines ~270-415; search "status": literals). For each extracted phrase (estimated 2-5 phrases per status), classify in 3 states: <br/>• `survived-as-correct` — phrase is content-correct, just relocated to atom body. Goes into expectedSubstrings test. <br/>• `survived-with-question` — phrase content questionable but kept in expectedSubstrings; flagged with TODO comment for Phase 2 review. <br/>• `dropped-pre-migration` — phrase contains identifiable state-leak / lie / outdated reference. Excluded from expectedSubstrings. Each entry MUST have rationale in audit doc. <br/>**Pause point**: classification is collaborative (human + Codex contradiction-spotting). Do NOT proceed to 0b.1 until human approves the classification. |
| 0b.1 | RED | `internal/tools/workflow_export_test.go` | Failing assertion: for each of 7 statuses, calling handleExport at that status returns `guidance` containing every entry classified `survived-as-correct` or `survived-with-question` (NOT `dropped-pre-migration` ones) per `workflow_export_phrase_audit.md`. Fails today because atoms don't carry the content. |
| 0b.2 | GREEN | New `internal/content/atoms/export-scope-prompt.md` | Write per mapping table. Frontmatter: `phases: [export-active], exportStatus: [scope-prompt], priority: 2`. **No service-scoped axes** (target unknown). Body migrated from `workflow_export.go` scope-prompt branch's hardcoded guidance string — every key phrase from `expectedSubstrings[scope-prompt]` (post-classification) present. |
| 0b.3 | GREEN | New `internal/content/atoms/export-variant-prompt.md` | Same pattern for variant-prompt status |
| 0b.4 | GREEN | 6 existing export atoms | Per mapping table: add `exportStatus: [<status>]` frontmatter; migrate inline guidance content from `handleExport` into atom body so `expectedSubstrings[status]` (post-classification) entries all present in atom body |
| 0b.5 | GREEN | `internal/content/atoms/export-intro.md` | Decision: no `exportStatus:` filter (universal framing) OR specific subset. Default per mapping table: no filter. Adjust body if needed for universal applicability. |
| 0b.6 | GREEN | `internal/tools/workflow_export.go::handleExport` | Refactor each status branch (currently lines ~270-415): instead of returning hardcoded `guidance` string, build envelope via `BuildExportEnvelope(targetService, status, opts)` (passing `opts.Client + opts.ProjectID + opts.StateDir`), call `RenderExportGuidance(env, corpus)`, embed rendered atom output in response `guidance` field. Keep `nextSteps` array as inline structural data (carries dynamic `targetService` hostname). Comment at L36-39 ("no atom-axis routing") gets removed; new comment explains synthesis path with service context. Test from 0b.1 now passes. |
| 0b.7 | GREEN | `internal/workflow/scenarios_test.go::TestScenario_S12_ExportActiveEmptyPlan` | Rewrite: replace "exactly 6 atoms" assertion with sub-tests (one per status), each pinning expected atom selection for that status. |
| 0b.8 | (audit) | Repo grep | grep entire repo for any dependency on exact inline export guidance strings (e.g. "Pick the runtime service to export", "Bundle composed", etc.). Any non-test caller using exact-match on these strings: update or remove. |

**Acceptance**: `go test ./internal/... -race -count=1` green (broadened from `internal/workflow + internal/content` to all of `internal/...` because Phase 0b modifies `internal/tools/workflow_export.go`), including expectedSubstrings test (every key phrase classified `survived-*` from old inline guidance present in new atom-rendered output). Manual smoke against eval-zcp: invoke `workflow="export"` at known status with provisioned service, verify response `guidance` is atom-derived AND contains expected key phrases. Old inline strings removed. Single source of truth: prose lives in atoms, structural data (`nextSteps` URLs) lives in handler.

### Phase 0c — Spec doc for new axis

| Step | Mode | File | Change |
|---|---|---|---|
| 0c.1 | (doc) | `docs/spec-knowledge-distribution.md` §3 | New `§3.11 exportStatus: (envelope-scoped, optional)` paragraph documenting the axis, mirroring §3.9 `serviceStatus:` style. Document service-context behavior: atoms with `exportStatus:` AND service-scoped axes (`runtimes:`, `serviceStatus:`, etc.) fire on `targetService`'s snapshot per `BuildExportEnvelope` semantics. **Document maintenance burden**: new export response status (e.g. handler grows new substatus like `git-push-conflict`) requires updating closed enum + parser + atom + scenario + golden + this spec section. Cost of structured axis. |

**Acceptance**: spec coherent. Phase 0 closed.

### Phase 1 — Goldens infrastructure + raw output generation

> **Note**: Phase 1 commit is a **working-branch checkpoint, NOT main-mergeable state.** UNREVIEWED golden files + golden comparison test gated off. Phase 2 must complete in the same branch before merge to main. CI on this branch is expected to pass via skip-with-TODO; main branch protection should NOT allow merge until Phase 2 enables comparison.

| Step | Mode | File | Change |
|---|---|---|---|
| 1.1 | RED | `internal/workflow/scenarios_golden_test.go` (new) | Failing test: load envelope fixture from scenario list, run `Synthesize`+render (use `RenderExportGuidance` for export scenarios, regular pipeline for others), compare to `testdata/atom-goldens/<scenario>.md`. Test gated by `ZCP_GOLDEN_COMPARE` env var — when unset (default), test skips with `TODO: Phase 2 blesses` message. When set, performs comparison. Failing because goldens don't exist yet. |
| 1.2 | GREEN | `internal/workflow/atom_golden_helper.go` | Helpers: parse golden file frontmatter at first `---` delimiter (mirror `internal/workflow/atom.go::parseAtomFrontmatter` style); body opaque after frontmatter close; **compare atom ID order separately from body diff** so ID changes don't hide inside large markdown diffs; `ZCP_UPDATE_ATOM_GOLDENS=1` env-gated regenerate path; explicit assertion that `ZCP_UPDATE_ATOM_GOLDENS` is unset when running in CI environment (panic with clear message if both `ZCP_UPDATE_ATOM_GOLDENS=1` and `CI=true` are set). |
| 1.3 | GREEN | `internal/workflow/scenarios_test.go` (or new `scenarios_fixtures.go`) | 30 envelope fixtures as Go-typed `StateEnvelope{...}`; **service order is behavior** — fixtures must deliberately set `Services []ServiceSnapshot` and `WorkSession.Services []string` order to match desired render output (refer `internal/workflow/compute_envelope.go` service sorting + `internal/workflow/build_plan.go` for iteration semantics). Test from 1.1 now passes its skip path. |
| 1.4 | GREEN | `internal/workflow/testdata/atom-goldens/<scenario>.md` (30 files) | Generate via `ZCP_UPDATE_ATOM_GOLDENS=1`. Each file: frontmatter (scenario id, expected atom IDs in order, scenario human description) + raw rendered body marked `<!-- UNREVIEWED -->` at top of body |
| 1.5 | GREEN | `internal/workflow/testdata/atom-goldens/_coverage-map.md` | Generated artifact: for each atom in corpus, list which scenarios render it; flag atoms appearing in 0 scenarios as `TODO: explicit decision required` (Phase 4 coverage gate test will fail until each unflagged atom has scenario coverage or `coverageExempt:` frontmatter entry). Auto-regenerated whenever goldens regenerate. |

**Acceptance**: `go test ./internal/workflow/... ./internal/content/... -race -count=1` **green** — parser, helper, fixture compilation, raw output generation tests pass; golden comparison test exists but skips with TODO message (passes via skip). 30 raw outputs generated and saved with `<!-- UNREVIEWED -->` markers. Coverage map artifact committed showing which atoms render where.

### Phase 2 — Pre-bless validation + batch review + atom fixes (defect-ledger flow)

**Process MUST be batched, not per-scenario.** Per-scenario fix-and-bless cycles cause cascading edits.

```
Step 0 — Pre-bless spot-check against reality:
  Pick 5 fixtures spanning phases (one each from idle / bootstrap /
  develop / strategy-setup / export). For each:
  - Provision/ensure matching service exists in eval-zcp
  - Drive into target state (develop/first-deploy = deploy history;
    standard-auto-pair = dev+stage with cross-deploy history;
    export/publish-ready = run full export workflow to publish state)
  - Run zerops_workflow action="status" on real service
  - Dump real envelope from response
  - Diff real envelope vs scenario fixture envelope (field-by-field)
  - Investigate any divergence: fixture wrong (fix fixture), or production
    state out of expected envelope shape (file separate plan)
  Goal: catch fixture-reality drift BEFORE pinning 30 goldens as truth.
  Realistic time: 4-8 hours (provisioning + driving services to specific
  states is non-trivial; standard-auto-pair with deploy history requires
  two deploy iterations minimum).

Step 1 — PAUSE POINT: present Step 0 findings to human.
  Do NOT proceed to Step 2 without explicit human approval that fixture
  shapes match reality (or that divergence is documented and acceptable).

Step 2 — Generate all 30 raw outputs (Phase 1 already did this; verify
  outputs are current).

Step 3 — Review pass — read all 30 outputs WITHOUT FIXING ANYTHING.
   For each scenario, answer:
   a. Is every claim in the body universally true for this envelope shape?
   b. Do any two atoms contradict each other in the composed text?
   c. Is anything critical missing for an agent in this configuration?
   d. Is the atom priority / order rational (most important first, unless intentional)?
   e. Are there redundancies — same instruction repeated by two atoms?
   f. Is any non-obvious platform-mechanics claim unbacked? (cite evidence rule)
   g. (export scenarios only) Are all `survived-*` expectedSubstrings entries present?

Step 4 — Write defect ledger: a single document listing every issue.
   Format per defect: scenario-id | atom-id | claim | why wrong | proposed fix

Step 5 — PAUSE POINT: present defect ledger to human.
  Do NOT proceed to Step 6 without explicit human approval of the ledger
  and the proposed fixes. Human may add/remove/edit defects.

Step 6 — Triage defects by atom (not by scenario):
   atom-X has 3 defects across scenarios A/B/C → fix once
   atom-Y has 1 defect in scenario D → fix once

Step 7 — Batch atom fixes: edit all affected atoms based on triage.
   Apply procedural-form principle: rewrite assertions as inspection,
   tighten axes, or split atoms (per §11).

   Codex pair-review scope DURING Step 7:
   - Codex job: spot CONCRETE CONTRADICTIONS between two atom bodies in
     same composed render. Mechanical task.
   - Codex job is NOT: "is this assertion factually true?" — semantic
     task with hallucination risk per memory feedback_codex_verify_specific_claims.md.
   - Codex prompt template: paste two atom bodies + envelope summary,
     ask "do these bodies contradict each other when rendered together?
     Reply yes/no with one citation per finding." Codex output is signal,
     not ground truth — human verifies each claimed contradiction.

Step 8 — Regenerate ALL 30 raw outputs (atom edits may cascade).

Step 9 — Re-review every output one final time:
   - Does it look right?
   - Did fixes for one defect introduce another?

Step 10 — Cycle cap: maximum 3 batch iterations of steps 6-9.
   Cycle escape semantics (when 3rd iteration still has defects):
   - STOP work, do not push more fixes.
   - Return to user with: ledger of unresolved defects + analysis of why
     batch fix isn't converging (likely indicates structural issue:
     missing axis, atom needs split, scenario fixture wrong, principle
     unclear).
   - User decides: expand plan with new axis / split atom / amend scenario /
     accept residual divergence with documented rationale.
   Implementer does NOT improvise past cap.

Step 11 — If clean:
   - Enable golden comparison test (set ZCP_GOLDEN_COMPARE flag default-on
     by removing the skip in scenarios_golden_test.go)
   - Strip <!-- UNREVIEWED --> markers from each golden file
   - Bless as final goldens
   - Commit ledger as testdata/atom-goldens/_review-ledger.md
```

**Effort estimate (recalibrated, conservative)**: ~30-35 hours total floor, distributed:
- Step 0 pre-bless spot-check: 4-8 hours (provisioning + driving services to specific states is non-trivial)
- Step 3 review pass: 30 scenarios with mixed complexity. Simple idle scenarios ~15 min each. Complex composed scenarios (e.g. `develop/first-deploy-recipe-implicit-standard` with 8-12 atoms) ~30-40 min each + ledger writeup. Realistic floor ~15-20 hours just review pass.
- Step 7 batch atom fixes: 4-6 hours
- Step 8+9 regenerate + re-review: 2-3 hours per cycle × ~2 cycles realistic ≈ 5-6 hours

Spread across **4-5 focused sessions** to avoid review fatigue. Single-session attempt produces inconsistent local judgments.

**Acceptance**: every golden file has `<!-- UNREVIEWED -->` removed. Atom corpus reflects fixes. `go test ./internal/...` green **with golden comparison test enforced** (ZCP_GOLDEN_COMPARE on by default). Defect ledger committed alongside as `internal/workflow/testdata/atom-goldens/_review-ledger.md` for archival, including pre-bless spot-check findings + per-defect resolution + any cycle-cap escape decisions + Codex-flagged contradictions (resolved or accepted with rationale).

### Phase 3 — Princip + supplementary rules into spec §11

| Step | Mode | File | Change |
|---|---|---|---|
| 3.1 | (doc) | `docs/spec-knowledge-distribution.md` §11.1 | Append bullet 7 (positive — verbatim text in "The procedural-form principle" section above) |
| 3.2 | (doc) | `docs/spec-knowledge-distribution.md` §11.2 | Append bullet 5 (negative examples — verbatim text above) |
| 3.3 | (doc) | `docs/spec-knowledge-distribution.md` §11.x | Add verify-state assertion rule (verbatim text above; cite `internal/workflow/synthesize.go::envelopeDeployStateMatches`) |
| 3.4 | (doc) | `docs/spec-knowledge-distribution.md` §11.x | Add evidence-required-for-platform-mechanics rule (verbatim text above) |
| 3.5 | (doc) | `docs/spec-knowledge-distribution.md` §11 | Cross-reference: Phase 2's defect-ledger / batch-review process is the operational application of the principle; pin reference to `internal/workflow/testdata/atom-goldens/_review-ledger.md` as a worked example |

**Acceptance**: spec coherent. New atom authors directed at principle by design.

### Phase 4 — Commit-time discipline (Axis O lint + coverage gate test)

Two automated checks at commit time. **Markers only — no per-axis allowlist for Axis O.** Coverage gate uses **atom frontmatter field** (not external map) — keeps exemption decisions local to the atom.

| Step | Mode | File | Change |
|---|---|---|---|
| 4.1 | RED | `internal/content/atoms_lint_test.go` | Failing test: synthetic atoms with each anti-phrase fail axis O check; same atoms with `<!-- axis-o-keep: <reason> -->` markers pass. Fails because lint doesn't exist. |
| 4.2 | GREEN | `internal/content/atoms_lint_axes.go` | New `axisOCheck` function. Patterns (case-insensitive, regex with word boundaries; **NOT including `status="..."` — would false-positive on legitimate tightly-axed export atoms post-Phase-0b**): <br/>• `\balready running\b` <br/>• `\bevery service\b` (followed by verb) <br/>• `\bis empty\b` <br/>• `\blanded and verified\b` <br/>• `\bBootstrap does NOT ship\b` |
| 4.3 | GREEN | `internal/content/atoms_lint_axes.go` (regex extension) | **Extend `axisMarkerPattern` and `axisMarkerStripPattern` regexes** (currently support `k|m|n|hot-shell` per L379) to include `o`. Without this extension, `axis-o-keep` markers won't strip from agent-visible text. |
| 4.4 | GREEN | atoms_lint_axes.go marker handling | `<!-- axis-o-keep: <reason> -->` on same/prior/next non-blank line suppresses one match. Reasons: `platform-invariant`, `route-gated`, `tightly-axed`. Mirror axis-K/M/N marker handling per `docs/spec-knowledge-distribution.md` §11.7. **No allowlist file** — markers are the only escape mechanism. |
| 4.5 | RED | `internal/workflow/coverage_gate_test.go` (new) | Failing test: walk corpus → walk all goldens' expected atom IDs → fail if any atom is in zero golden expected sets AND has no `coverageExempt:` frontmatter field. Fails because some atoms might be uncovered. |
| 4.6 | GREEN | `internal/workflow/atom.go` | Add `coverageExempt string` to `KnowledgeAtom` (parsed from frontmatter; empty when absent). Add `"coverageExempt"` to `validAtomFrontmatterKeys`. Field value is human-readable rationale. |
| 4.7 | GREEN | atom corpus (case-by-case during Phase 4 implementation) | For atoms uncovered by 30 scenarios, decide per atom: <br/>• **Prefer scenario expansion** if the atom adds unique content / fire-set / response-status that's missing from the 30. <br/>• **Prefer `coverageExempt:`** if the atom is a genuine error-path edge case that doesn't warrant a scenario fixture (e.g. recovery atom rendered only on rare API failures). <br/>**Heuristic rule**: if the atom's typical render-occasion appears in <1% of agent sessions, exemption is appropriate. Otherwise, add a scenario. <br/>Each `coverageExempt:` entry MUST have a one-line rationale referencing this heuristic. |
| 4.8 | (relationship) | `internal/workflow/corpus_pin_density_test.go` | **Document explicit relationship**: pin-density test verifies `requireAtomIDs*` literals reference reachable atoms (different concern). Coverage gate test verifies atoms appear in golden expected sets (composition concern). Both stay; add comment at top of each test pointing to the other. **Do NOT replace pin-density** — different test surface. |
| 4.9 | RED | `internal/content/atoms_lint_test.go` (extension) | Failing test: synthetic `nextSteps` entry > 80 chars OR matching regex `(because|so that|in order to|note that|explanation)` flagged as warning by lint. (Tripwire for prose creep into nextSteps per Phase 0b documentation.) |
| 4.10 | GREEN | `internal/content/atoms_lint.go` (or appropriate location) | Implement `nextStepsLengthCheck` per 4.9. Lint scans `internal/tools/workflow_export.go` source for `nextSteps` slice literals; warns on overlong or prose-like entries. |
| 4.11 | (doc) | `docs/spec-knowledge-distribution.md` §11.5 | Add §11.5.5 "Axis O — STATE-DECLARATIVE-LEAK (narrow)" subsection: rationale, pattern list (5 patterns; `status="..."` explicitly excluded with rationale), marker convention (no allowlist), false-positive expectation. |
| 4.12 | (doc) | `docs/spec-knowledge-distribution.md` §11.7 | Extend marker handling table to include `axis-o-keep`. Note that `axisMarkerPattern` regex was extended in step 4.3. |
| 4.13 | (doc) | `docs/spec-knowledge-distribution.md` §11.x | Add **Coverage gate** subsection: every atom must appear in ≥1 golden expected set OR carry `coverageExempt: <reason>` frontmatter field (lokálně u atomu). Strict by default. Frontmatter exemption is a code review red flag — reviewer demands strong justification per heuristic in step 4.7. |

**Acceptance**: `go test ./internal/... -race -count=1` green. Lint catches synthetic regressions in test fixtures; markers suppress legitimate cases; existing post-Phase-2 corpus passes lint without false positives (or has documented marker entries). Coverage gate passes for all 82 atoms — every atom either appears in scenario expected set or has `coverageExempt:` frontmatter entry with rationale. `axisMarkerPattern` strips axis-o-keep markers from agent-visible text. nextSteps tripwire lint active.

### Phase 5 — Coverage policy + maintenance docs + concrete live eval (with explicit owner)

Process discipline + maintenance guidance + falsifiable live-eval acceptance.

| Step | Mode | File | Change |
|---|---|---|---|
| 5.1 | (doc) | `docs/spec-knowledge-distribution.md` §11 | Add §11.x **Scenario growth policy**: the friction-fix PR adds a scenario when production friction reveals an envelope shape not pinned by any existing scenario. Owner: PR author. Timing: in the same PR as the atom fix. Reviewer asks "does this fix close the friction class, or just this instance?" — if class-level, scenario must be added. |
| 5.2 | (doc) | `docs/spec-knowledge-distribution.md` §11 | Add §11.x **Scenario pruning policy**: when the suite crosses 40 scenarios, audit for scenarios that don't add unique atom fire-set, response status, or known failure class. Merge or retire. Annual cadence sufficient unless count grows fast. |
| 5.3 | (doc) | `docs/spec-knowledge-distribution.md` §11 | Add §11.x **Fixture authoring guidance**: service order in `Services []ServiceSnapshot` and `WorkSession.Services []string` is behavior — fixtures must deliberately choose order. `compute_envelope.go` sorts services by hostname; `build_plan.go` iterates work session order. Disagreement between fixture and intended render is a fixture bug, not a synthesizer bug. |
| 5.4 | (doc) | `docs/spec-knowledge-distribution.md` §11 | Add §11.x **Maintenance**: when `StateEnvelope` shape changes (new field, renamed field), all scenario fixtures break compilation. Fix sequence: update fixtures → regenerate goldens → review diff (intended consequences only) → bless. Owner: PR author of the schema change. |
| 5.5 | (operational) | New file `internal/workflow/testdata/atom-goldens/_live-eval-protocol.md` | **Concrete live-eval acceptance for post-merge verification.** Protocol: <br/>**Owner**: explicit owner is the human reviewer who blessed Phase 2 goldens (named in `_review-ledger.md` header). Owner accountability is captured in `CODEOWNERS` for `internal/workflow/testdata/atom-goldens/_live-eval-runs.md` (added in this step). <br/>**Reminder mechanism**: GitHub issue created at merge with quarterly auto-close + reopen for next quarter (or equivalent calendar mechanism agreed with owner). Issue title: "Live eval verification — atom corpus goldens — Q<N> <year>". <br/>**Scenarios to walk** (5 of 30): `bootstrap/recipe/provision`, `develop/first-deploy-dev-dynamic-container`, `develop/standard-auto-pair`, `strategy-setup/configured-build-integration`, `export/publish-ready`. <br/>**Services**: provision matching shape on eval-zcp project (`i6HLVWoiQeeLv8tV0ZZ0EQ`); cleanup after each run. <br/>**Procedure**: for each scenario, drive agent through the workflow on real Zerops; capture rendered guidance from `action="status"` (and `workflow="export"` direct call for export scenario); diff against blessed golden — substantive divergence is post-merge defect, file follow-up plan. <br/>**Evidence written to ledger**: `internal/workflow/testdata/atom-goldens/_live-eval-runs.md` — append-only log: date / scenario / service hostname / divergence summary / disposition. <br/>**Run cadence**: at minimum once after merge of this plan; quarterly thereafter (issue mechanism); ad-hoc when production friction surfaces. <br/>**Live eval is post-merge follow-up — NOT a pre-merge blocker.** Cross-phase acceptance does NOT require first run logged before merge; first run is owner's first quarterly check post-merge. |
| 5.6 | (file) | `CODEOWNERS` | Add entry: `/internal/workflow/testdata/atom-goldens/_live-eval-runs.md @<owner>` to enforce review responsibility. |

**Acceptance**: spec amendments shipped. Coverage policy established (enforcement is automated via Phase 4 coverage gate test; this phase adds documentation / process context). Live-eval protocol committed; owner named in protocol header; CODEOWNERS entry committed; reminder mechanism (issue or calendar) created. **First run is post-merge follow-up, not a pre-merge gate.**

---

## Acceptance criteria (cross-phase)

- `go test ./internal/...` (and `./internal/workflow/... ./internal/content/...` where Phase 0b-irrelevant) green at every phase boundary. Phase 1 acceptance is "green for infrastructure tests" (golden comparison gated off via `ZCP_GOLDEN_COMPARE` env var, default off, test skips with TODO); Phase 2 acceptance enables golden comparison (default-on by removing skip).
- All 30 goldens reviewed and blessed (no `<!-- UNREVIEWED -->` markers remain).
- `internal/workflow/scenarios_test.go::TestScenario_S12_ExportActiveEmptyPlan` no longer pins the all-export-atoms-together overmatch — replaced with per-status sub-tests.
- Each known LIE-CLASS atom (listed in Phase 2) has been rewritten or had its axes tightened, validated against the relevant golden(s).
- `handleExport` renders atom-derived guidance with service context — no inline hardcoded prose for status branches. `nextSteps` remains as inline structural array. Every entry in `expectedSubstrings[status]` (post-classification) either present in rendered output OR explicit decision in defect ledger.
- Pre-implementation audits committed: `synthesize_export_audit.md` (service-context decision), `synthesize_cache_audit.md` (cache impact), `workflow_export_phrase_audit.md` (3-state phrase classification).
- Defect ledger committed at `internal/workflow/testdata/atom-goldens/_review-ledger.md` documenting Phase 2 decisions, including pre-bless spot-check findings and any cycle-cap escape decisions and Codex-flagged contradictions.
- Coverage map artifact committed at `internal/workflow/testdata/atom-goldens/_coverage-map.md` showing every atom's scenario coverage.
- Live eval protocol committed at `internal/workflow/testdata/atom-goldens/_live-eval-protocol.md`; owner named; CODEOWNERS entry added; reminder mechanism in place. **First live-eval run is post-merge follow-up — NOT a pre-merge gate.**
- Spec §11 contains: bullet 7 (positive), bullet 5 (negative), verify-state rule, evidence rule, scenario growth/pruning, fixture authoring guidance, coverage gate documentation, axis O documentation, exportStatus enum maintenance burden documentation.
- Axis O lint catches synthetic regressions in test fixtures; existing corpus passes (with markers where needed); `axisMarkerPattern` extended to include `o`. nextSteps tripwire lint active.
- Coverage gate test passes — every atom covered or carries `coverageExempt:` frontmatter with rationale per heuristic in Phase 4.7.
- Pin-density test (`internal/workflow/corpus_pin_density_test.go`) preserved — different concern from coverage gate; cross-references added.

---

## Out of scope

- **LLM-as-judge (semantic verification of rendered output)** — defer to follow-up plan; manual review at 30 scenarios is feasible. Promote to standalone plan when corpus exceeds ~50 scenarios or review fatigue measurable.
- **Generic `responseStatus:` axis** (covering deploy/verify sub-statuses beyond export) — backlog entry. Add when second use case beyond export materializes.
- **Sweep of all 82 atoms manually** — Phase 2 review covers organically (atoms appearing in the 30 scenarios get scrutinized; atoms outside get implicit coverage via Phase 4 coverage gate test that flags uncovered atoms for explicit `coverageExempt:` decision).
- **Recipe app-repo content drift** (express + @types/express type version mismatch in 35+ external repos) — separate stream, recipe content QA.
- **`zerops.yaml` source-mount preflight bug** — separate plan, already addressed in prior session.
- **Replacing `corpus_coverage_test.go` or `corpus_pin_density_test.go`** — keep both; goldens own correctness; coverage tests own structure; pin density owns selection-test reachability. Cross-references added in Phase 4.8.
- **Renaming `scaffold-zerops-yaml.md` to `export-scaffold-required.md`** — scope-creep refactor; current name is functional even if non-canonical. Defer.

---

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| **Phase 0a service-context audit (0a.0) returns "non-target reasoning needed" for some atom** — would invalidate single-entry decision | STOP at audit; escalate to user. Plan provides escalation path explicitly: option (a-upgrade) full Services + ExportTarget field. Implementer does NOT proceed without user decision. |
| **Phase 0a axis change ripples** — `exportStatus:` introduction touches synthesizer, parser, lint, scenarios | Mirror existing `serviceStatus:` axis pattern (proven implementation); RED tests in 0a.1, 0a.3, 0a.5, 0a.7 catch behavior regressions; sub-phase split (0a vs 0b) means axis lands without behavior change first |
| **Phase 0b refactor blast radius** — handleExport touches 7 status branches, all related tests | Mapping table prevents implementer drift; `expectedSubstrings` test (0b.1) with 3-state classification (0b.0) catches content loss; sub-phase commits each branch atomic; manual smoke test on eval-zcp before declaring complete |
| **expectedSubstrings could conserve existing lies** | Phase 0b.0 3-state classification (`survived-as-correct` / `survived-with-question` / `dropped-pre-migration`) explicitly removes phrases identified as lies before they enter the test set. Pause point at 0b.0 requires human approval of classification. |
| **Phase 0b atom enrichment** — migrating inline guidance into atom bodies may exceed atom prose conventions | Apply CLAUDE.md "LLM-facing prose — compact, problem-first" rule; trim to trigger + action + failure mode; review pass in Phase 2 catches over-long atoms |
| **Phase 2 review fatigue** — 30 reviews × ~20-40min = 15-20h focused (plus pre-bless + fixes + re-review = 30-35h floor) | Batch process (review all → ledger → batch fix → regenerate → re-review) prevents per-scenario context switching; pair-review with Codex for objectivity (limited to mechanical contradiction-spotting, NOT semantic truth judgment per memory `feedback_codex_verify_specific_claims.md`); explicit review questionnaire prevents autopilot blessing; cycle cap (max 3 iterations) with defined escape semantics; effort spread across 4-5 focused sessions; explicit pause points at Step 1 and Step 5 |
| **Pre-bless fixture-reality drift** — fixture envelope shape doesn't match what production produces | Phase 2 step 0: spot-check 5 fixtures (one per phase) against eval-zcp via `action="status"`; diff envelope; investigate divergence before bless. Realistic 4-8 hours (not 1-2). |
| **Codex pair-review hallucination on semantic judgment** | Per memory `feedback_codex_verify_specific_claims.md`: Codex unreliable on "is it factually true?" questions (semantic judgment, hallucination risk). Phase 2 step 7 explicitly limits Codex to "spot concrete contradictions between two atom bodies in same composed render" (mechanical task). Codex output is signal, not ground truth — human verifies each claimed contradiction. |
| **Goldens brittleness** — every prose tweak forces golden update | Standard `*.golden.md` pattern with `ZCP_UPDATE_ATOM_GOLDENS=1` workflow; PR diff makes intent explicit; reviewer judgment on each diff is the gate; atom ID order compared separately so prose-only tweaks don't muddy ID-change reviews |
| **Coverage gap (atoms outside 30 scenarios)** | Phase 4 coverage gate test forces explicit decision: scenario coverage OR `coverageExempt:` frontmatter with rationale. Phase 4.7 heuristic prevents either suite-padding (add scenario for unique fire-set/status/failure-class) or over-exemption (exempt only if <1% session frequency). |
| **Pair composition gap** — coverage gate enforces per-atom membership but not per-pair co-firing. Two atoms that contradict each other only when co-rendered are caught only if their co-firing scenario is in the 30. | **Accepted as by-design trade-off** (Cartesian product would explode). Mitigation: scenario design intentionally puts likely-co-firing atoms in shared scenarios; new atom additions must consider what other atoms it co-fires with (PR review checkpoint); live evals catch post-merge contradictions. |
| **L1 not absolute coverage** | L1 description honest framing: spec discipline + structural axes guide authors away from lying, but full coverage requires L2 (goldens) + L4 (live evals). State-leaks in atom prose rendered ONLY in envelope shapes outside the 30 scenarios are NOT caught at commit time — they surface post-merge via live eval or production friction. This is a known gap accepted to avoid Cartesian explosion. |
| **Reviewer blesses wrong golden** | Periodic re-review (~quarterly via owner-tracked GitHub issue per Phase 5.5); Codex pair-review on each new bless (mechanical contradictions only); live eval surfaces post-merge if guidance is wrong in production; evidence rule (Phase 3) requires backing for non-obvious mechanics claims |
| **Phase 1 commit not main-mergeable as-is** | Documented: Phase 1 = working-branch checkpoint only. Phase 2 must complete in same branch before merge to main. Main branch protection should NOT allow merge of UNREVIEWED state. |
| **CI accidentally regenerates goldens** | `ZCP_UPDATE_ATOM_GOLDENS` env var is opt-in, never set by CI workflow; helper code defends with explicit assertion that the env var is unset when `CI=true` is set (panic with clear message). |
| **Service order in fixtures cascades on `compute_envelope.go` sort changes** | Cost of determinism, accepted. Cascade is GOOD signal — change to sort logic forces review of all renders. Alternative (non-deterministic ordering) is worse. |
| **Cycle cap escape ambiguous** | Explicit semantics in Phase 2 Step 10: implementer STOPS, returns to user with unresolved-defect ledger + structural-issue analysis. User decides next action (new axis / split atom / amend scenario / accept residual). Implementer never improvises past cap. |
| **nextSteps inline drift** — handler emits structural data; over time prose may creep in | Phase 4 lint tripwire (4.9-4.10): warns on `nextSteps` entries > 80 chars or matching prose-like regex (`because|so that|in order to|note that|explanation`). Catches creep early; reviewer returns content to atom body. |
| **Live-eval forgotten post-merge** | Phase 5.5 explicit owner + CODEOWNERS entry + reminder mechanism (quarterly issue or calendar). Owner accountability is named, not assumed. |
| **exportStatus enum maintenance burden** — handler-level new substatus requires multi-file update | Documented in Phase 0c.1 as known cost of structured axis. Worth the trade-off vs hardcoded inline strings (the original design). |
| **Synthesize cache (if it exists) collision** | Phase 0a.0.5 audit verifies no cache layer exists; if found, escalates to user. Default expectation: synthesize is stateless per call. |

---

## Verification of standalone executability

A new Claude session given this plan + standard project context (`CLAUDE.md`, `CLAUDE.local.md`, `MEMORY.md` auto-loaded) has everything needed:
- Why the plan exists (problem statement + history)
- Architectural decision on service context in export (with audit step + escalation path)
- Architecture (3 verification layers + commit-time discipline, with honest L1 framing)
- The principle (verbatim text for spec)
- 30 canonical scenarios (full list with envelope shapes)
- Export status → atom mapping table (Phase 0b reference)
- Phases with concrete file paths + line refs + RED/GREEN markers + audit steps
- Concrete Go function signatures (not pseudocode)
- Acceptance criteria (per phase + cross-phase + falsifiable substring tests for export)
- Out-of-scope and risks (explicit, including by-design trade-offs)
- Maintenance + coverage policies (Phase 5)
- Concrete live-eval protocol with scenarios + services + evidence format + owner + reminder mechanism
- "How an LLM implementer should approach this plan" guidance section at top

No external `/tmp/*.md` references. No conversation-history dependencies. Self-contained. **Pause points are explicit; audit decision criteria are explicit; no implicit "implementer decides" without rule.**

---

## How to launch in a fresh session

In a new Claude Code session at `/Users/macbook/Documents/Zerops-MCP/zcp`, paste this as the first user message:

```
Execute the plan at plans/atom-corpus-verification-2026-05-02.md.

Read CLAUDE.md, CLAUDE.local.md, and the plan first. Pay special attention
to the "How an LLM implementer should approach this plan" section near
the top — pause points and audit decision criteria are load-bearing.

Work the phases in order: Phase 0a → 0b → 0c → 1 → 2 → 3 → 4 → 5. Each
phase is one or more atomic commits. Follow TDD per CLAUDE.md: RED tests
first (failing), then GREEN implementation. Audit steps produce committed
artifacts with explicit decision criteria — if criteria don't match
observed reality, STOP and ask me, do not invent a path.

At every phase boundary: run go test ./internal/... -race -count=1 (or
the specific scope listed in that phase's acceptance) and verify green
before continuing. Phase 1 acceptance is green-for-infrastructure with
golden comparison gated off; Phase 2 enables full golden comparison.

Phase 0a includes a mandatory audit (0a.0) of export atoms for non-target
service references — this decides Services-population semantics for
BuildExportEnvelope. If audit finds any atom needs non-target reasoning,
STOP and escalate.

Phase 0b includes a refactor of handleExport — do a manual smoke test on
eval-zcp before declaring 0b complete, verifying expectedSubstrings test
passes for all 7 export statuses. Phase 0b.0 phrase classification is a
PAUSE POINT — do not proceed without my approval.

Phase 1 commit is a working-branch checkpoint, NOT main-mergeable — keep
working in the same branch through Phase 2 before merging to main.

Phase 2 has multiple PAUSE POINTS:
  - Step 1 (after pre-bless spot-check)
  - Step 5 (after defect ledger)
  Wait for my explicit approval at each. Cycle cap is 3 batch iterations;
  if defects remain after 3rd cycle, STOP and return to me with
  structural-issue analysis.

Phase 5 first live-eval run is post-merge follow-up, NOT a pre-merge gate.

Confirm the plan, then start Phase 0a with the audit step.
```
