# Run-52 engine fixes — plan (review before implement)

Goal: make run-52 walk smoothly and keep the content the strongest yet, by
fixing the run-51 root causes. Per your decision this is **plan-first** for the
engine changes and **parent + generic-nodejs scope** for the corpus scrub.
Source findings: [plans/run-51-validation.md](run-51-validation.md).

**What run-51 actually cost us** (the cascade to kill): `complete-phase
feature` guidance told the agent to dispatch codebase-content workers *before*
`enter-phase codebase-content`; nothing stopped it; the workers ran in the
wrong phase; the scaffold bare-yaml gate masked 7 citation defects; the fix
couldn't resume warm authors (`SendMessage` absent) → 3 cold fix agents. Plus
one content regression: a stale `zsc noop --silent` on the worker dev setup
that no phase could remove.

The fixes below are ordered by leverage. **Fix 2 is the keystone** — it alone
prevents the dispatch-before-enter cascade; the others harden and de-risk.

---

## Fix 1 — Phase-walk guidance must sequence `enter-phase` before dispatch

**Root cause (verified).** `completePhase` returns the *next* phase's entry
atom as guidance ([`handlers.go:1977-1997`](internal/recipe/handlers.go#L1977-L1997)),
and only `PhaseFinalize` auto-advances — every other transition leaves
`sess.Current` on the old phase (pinned by
`TestCompletePhaseEarlierPhases_DoNotAutoAdvance`). But the atom body
[`content/phase_entry/codebase-content.md:3`](internal/recipe/content/phase_entry/codebase-content.md#L3)
**leads with `build-subagent-prompt` + "dispatch all briefs in parallel"** and
the only `enter-phase` it names is `env-content` (the phase *after*). So an
agent that follows the guidance dispatches into a not-yet-entered phase.
`scaffold.md:3` and `env-content.md:3` share the identical anti-pattern.

**Change (content-only, no Go).** Prepend the explicit step as the first
next-call in all three atoms, e.g. codebase-content.md:3:
> **Next call:** `zerops_recipe action=enter-phase slug=<slug> phase=codebase-content` (advances the pointer + mints fact shells + retires the scaffold gate — required before any dispatch). THEN for each codebase, `build-subagent-prompt … briefKind=codebase-content …` AND `briefKind=claudemd-author …`; dispatch in parallel via `Agent`; then `complete-phase … phase=codebase-content` → `enter-phase … phase=env-content`.

**Bonus staleness (fix in the same pass).**
[`content/phase_entry/feature.md:106-120`](internal/recipe/content/phase_entry/feature.md#L106-L120)
"After complete-phase phase=feature" says the next action is `enter-phase
phase=finalize` — skipping codebase-content + env-content entirely (stale
since those phases were inserted). Correct it to point at
`enter-phase phase=codebase-content`. Requires updating the substring anchors
in `TestPhaseEntry_FeatureCarriesAfterCompletePhaseTeaching`
(`phase_entry_c_test.go:66`).

**RED test.** `TestPhaseAtoms_SequenceEnterPhaseBeforeDispatch` — table over
`{scaffold, codebase-content, env-content}`: assert `enter-phase … phase=<self>`
appears and its index precedes the first `build-subagent-prompt` token. Fails
today (no `enter-phase … phase=codebase-content` token in the atom).

**Pinning tests / risk.** No golden snapshot pins the literal string; existing
substring-anchor tests (`phase_entry_b_test.go`, `phase_entry_test.go`,
`handlers_finalize_advance_test.go`) won't break. Only the feature.md anchor
test needs updating. Low risk.

---

## Fix 2 — `enter-phase` precondition on `build-subagent-prompt` (KEYSTONE)

**Root cause (verified).** `handleBuildSubagentPrompt`
([`handlers.go:518-642`](internal/recipe/handlers.go#L518-L642)) validates only
the refinement2+codebase combo and codebase-in-plan — it does **not** check
`sess.Current` against the requested `briefKind`'s phase, and it calls
`seedEngineEmittedFacts` ([`handlers.go:535`](internal/recipe/handlers.go#L535))
*unconditionally*. So dispatching `codebase-content` while at `feature`
succeeds and mints shells into a phase never entered — the run-51 smoking gun.

**Change.**
1. Add helper `phaseForBriefKind(BriefKind) (Phase, bool)` next to
   `requiresCodebase` ([`briefs_subagent_prompt.go:224`](internal/recipe/briefs_subagent_prompt.go#L224)),
   encoding the 1:1 (+2:1) map: scaffold→scaffold, feature→feature,
   codebase-content→codebase-content, **claudemd-author→codebase-content**,
   env-content→env-content, finalize→finalize, refinement→refinement,
   **refinement2→refinement**.
2. Insert a precondition in `handleBuildSubagentPrompt` **between the
   refinement2 guard (`:529`) and `seedEngineEmittedFacts` (`:535`)** — before
   any seeding side-effect — that refuses when `sess.Current != want` with a
   text recovery hint (the package's convention; no `Recovery` struct here):
   *"build-subagent-prompt: briefKind=%s belongs to phase %q but the session is
   at %q. Call action=enter-phase phase=%s first, then re-dispatch."*

   Refuse-with-recovery, **not** auto-enter (auto-enter would bypass
   `EnterPhase`'s adjacency + prior-phase-completion checks at
   [`workflow.go:197-202`](internal/recipe/workflow.go#L197-L202)).

**Why this is the keystone.** With it, the agent *cannot* dispatch
codebase-content workers while at `feature` — so the workers always
self-validate under `PhaseCodebaseContent`, where the citation validators run
(which is what Fix 3 otherwise patches). It directly closes the cascade.

**RED test.** `TestBuildSubagentPrompt_CodebaseContentInFeaturePhase_RefusesWithEnterPhaseRecovery`
— drive `sess.Current=PhaseFeature`, dispatch `briefKind=codebase-content`;
assert `OK==false`, error names `phase "codebase-content"` + `phase "feature"`
+ `action=enter-phase` + `phase=codebase-content`, `Prompt=="" && BriefPath==""`,
**and the per-codebase fact shells were NOT seeded** (pins the insertion point
before `seedEngineEmittedFacts`). Plus a GREEN companion (in-phase dispatch
still succeeds).

**Blast radius — the main implementation cost.** ~4–10 existing tests dispatch
while still at `PhaseResearch` (e.g. `briefs_dispatch_test.go:162,196,226` do
`OpenOrCreate`+`update-plan`+dispatch). They will all refuse. Mitigation: add a
test helper `forcePhase(sess, PhaseX)` (sets `Current` + populates `Completed`
for priors) and thread it through every `build-subagent-prompt` call site
(grep `build-subagent-prompt` in `*_test.go`, audit each). Scope the
precondition to `build-subagent-prompt` only — leave `build-brief` (debug, no
seeding) ungated.

---

## Fix 3 — Decouple citation validators from the bare-yaml gate (defense-in-depth)

**Root cause (verified).** Not an in-gate short-circuit — it's **phase-scoped
gate selection**. The self-validate switch
([`handlers.go:1867-1880`](internal/recipe/handlers.go#L1867-L1880)) picks the
gate set by `sess.Current`. At `PhaseFeature` it selects `FeatureGates()`,
which includes the bare-yaml gate but **not** `gateCodebaseSurfaceValidators`
(the citation validators, registered only in `CodebaseContentGates()` at
[`gates.go:157`](internal/recipe/gates.go#L157)). So pre-enter-phase
self-validate never ran citation checks; they first surfaced at
`complete-phase` after the pointer advanced.

**Interaction with Fix 2.** Fix 2 prevents the wrong-phase self-validate
entirely, so this masking can't recur on the primary path. **Recommendation:
implement Fix 3 as lower-priority defense-in-depth** — valuable if any future
path self-validates content fragments outside codebase-content, but not
load-bearing once Fix 2 lands. Include it for robustness; it is the only fix
that's optional if we must cut scope.

**Change (Option B).** Add a citation-only gate appended to the scaffold/feature
*scoped* path (the `handlers.go` switch), **conditioned on the codebase having
IG/KB fragments recorded** (`plan.Fragments["codebase/<h>/integration-guide"]`
or `…/knowledge-base` non-empty). Do **not** add it to the named
`CodebaseScaffoldGates()`/`FeatureGates()` slices — that would break
`TestCodebaseScaffoldGates_OnlyFactQuality` (`gates_run17_test.go:13-27`, which
asserts the scaffold set excludes surface validators).

**RED test.** `TestCompletePhaseScoped_FeaturePhase_RunsCitationValidatorsOnContentFragment`
(content fragment present + clean bare yaml → expect `kb-citation-missing`) +
`…_NoContentFragment_SkipsCitationValidators` (no fragment → no false positive).

**Risk.** Low — citation validators already early-return on missing/empty
IG/KB markers, so scaffold-era bare yaml produces no false positives even
before the presence guard.

---

## Fix 4 — Scaffold lint: dev-runtime must omit `run.start` (Issue 3 structural catch)

**Root cause (verified).** Nothing catches a stale `start:` on a dev dynamic
runtime at authoring time. The schema validator accepts `run.start` (it's a
valid field); `gate_worker_dev_server.go` was **already refactored off the
literal `zsc noop`** (run-49 issue 3 — it now keys on dynamic-runtime class +
the `worker_dev_server_started` fact), so the R49 "still enforces zsc noop"
note is stale and a new lint **won't conflict** with it.

**Change.** New gate `gateDevRuntimeNoRunStart` (new file
`internal/recipe/gate_dev_runtime_no_run_start.go`), registered in
`CodebaseScaffoldGates()` right after `worker-dev-server-started`
([`gates.go:90`](internal/recipe/gates.go#L90)) — so it also runs at feature +
finalize transitively. Reuse `extractDevRunBase` + `topology.RuntimeClassFor`
(dynamic-only scope, which excludes static/php-nginx/managed); add a sibling
`extractDevRunStart` (same `setup: dev`→`run:`→directive walk, matches `start:`,
terminates at the next `- setup:` so prod blocks are never inspected). Fire
`SeverityBlocking` when a dynamic dev block carries any `run.start`. Absolute
rule ("dynamic dev runtime omits `run.start`") — not the weaker
sibling-comparison variant.

**RED tests.** `TestGateDevRuntimeNoRunStart_FlagsZscNoopOnDynamicDev` (the
run-51 reproducer), `…_FlagsAnyRunStartOnDynamicDev`,
`…_PassesWhenDevOmitsRunStart`, `…_SkipsProdStart`,
`…_SkipsStaticAndImplicitWebserverDev`, plus `TestExtractDevRunStart_*` table.

**Blast radius — zero.** No tracked recipe (`internal/knowledge/recipes/*.import.yml`),
test, golden, or fixture carries a real `start:` directive with `zsc noop` (the
~12 import.yml hits are all in YAML *comments*, which a directive lint ignores);
no test asserts a literal `start: zsc noop`. Hard-reject is safe and matches the
sibling scaffold gates.

---

## Corpus scrub — parent + generic nodejs (your chosen scope)

`zsc noop` is endemic in the gitignored, Strapi-synced recipe corpus
(`internal/knowledge/recipes/`, 43 `.md` files). Per your decision we scrub
**only the two that feed run-52's scaffold**:
- `nestjs-minimal.md` (the run-52 parent — `:145` ships `start: zsc noop --silent`)
- `nodejs-hello-world.md` (the generic nodejs dev template — `:137` + the `:91` "stays idle (zsc noop)" comment)

**Mechanism (outward-facing — needs the sync workflow + creates PRs).** For
each slug: `zcp sync pull recipes <slug>` → edit the dev setup to **omit
`run.start`** and rewrite the comment to "the dynamic dev container stays alive
without an explicit start command; SSH in and run the watcher" → `zcp sync push
recipes <slug>` (→ GitHub PR) → merge → `zcp sync cache-clear <slug>` → pull.
I will prepare the exact edits; **the `zcp sync push` (PR creation) is
outward-facing — I'll confirm with you before pushing.** The other ~41 files
are explicitly out of scope for now (noted as a future corpus sweep).

Fix 4 backstops this: even if a corpus file regresses, the scaffold lint
catches the stale directive in the agent's output.

---

## Not code-fixable here — surfaced for your decision

- **`SendMessage` availability — DROPPED as moot (owner decision).** It only
  bit *because* the cascade forced a gate-rejection fix pass in the first
  place. Once Fixes 1+2 make the walk enter-then-dispatch, the codebase-content
  workers self-validate their citations **in-phase** (Fix 3 ensures the
  validators run) and fix their own fragments before terminating — so there is
  no post-termination fix pass, and "resume the warm author" never arises.
  Fixing the root removes the need for `SendMessage`; no harness/config change
  required. (It remains a Claude Code agent-config matter if ever wanted, not a
  zcp Go change.)
- **Schema bare-form→composite stance.** Recipe corpus emits `nodejs@22` /
  `postgresql@18`; the current published schema is composite-only but the API
  accepts bare as BC (`internal/topology/type_equivalence.go`). Decide: migrate
  the corpus to composite forms, or record BC-acceptance as the stance. Not a
  defect; affects every recipe. Out of scope for run-52 unless you want it.
- **Facts retraction** (low priority): scaffold-era facts (`facts.jsonl:28-29`
  burn framing, `:1,18` "omitted (zsc noop)") aren't retracted when a later
  phase supersedes the decision. Surface-clean today; a separate small
  engine task if desired.

---

## Sequencing, TDD, and expected outcome

One coordinated branch (these all touch `internal/recipe/`; a parallel fleet
would collide). Order: **Fix 2 (keystone) → Fix 1 (guidance) → Fix 4 (lint) →
Fix 3 (defense-in-depth)**, each RED→GREEN before the next, then the full suite
(`go test ./... -race`) green before any commit. Per CLAUDE.md this is
recipe-engine scope: unit + recipe-package tests (no MCP tool signature
changes, so tool/integration/e2e layers are unaffected — confirm `annotations`
unaffected). The Fix-2 test-fixture migration (`forcePhase` helper across ~4–10
dispatch tests) is the bulk of the effort.

**Expected run-52 outcome if all land:** the phase walk is enter-then-dispatch
by construction (Fix 1 guidance + Fix 2 precondition) → no dispatch-before-enter,
no gate-masking, no cold-fix-agent churn → smooth flow; the worker dev `zsc
noop` cannot ship (Fix 4 lint + parent corpus scrub) → the one content
regression is gone. Content was already the strongest yet, so this targets the
*process* surface that actually made run-51 rough.

**Decisions recorded (2026-06-01):**
1. ✅ **Implement Fixes 1–4 on a branch for review** — proceeding.
2. ✅ **`SendMessage` DROPPED as moot** — it wouldn't have happened without the
   root problems; Fixes 1+2+3 remove the fix pass that needed it.
3. ⏸ **Schema bare→composite — deferred** (not a run-51 defect; revisit as a
   separate corpus decision).
4. ⏸ **Corpus PRs (`nestjs-minimal` + `nodejs-hello-world`)** — I'll prepare the
   exact edits; confirm before `zcp sync push` (creates PRs).
