# Recipe content quality — architectural fixes after run-33

**Date:** 2026-05-09. Sibling to [run-33-analysis.md](run-33-analysis.md) +
[run-33-defect-comparison.md](run-33-defect-comparison.md).

This doc consolidates the diagnosis after run-33 + sim
33-postfixes-1 + the structural review of the recipe content pipeline.
It supersedes the substrate-iteration framing of
[run-32-content-quality-deep-diagnosis.md](run-32-content-quality-deep-diagnosis.md)
and
[run-32-phase-3-preflight-status.md](run-32-phase-3-preflight-status.md):
those proposed substrate edits as the primary lever; run-33 evidence shows
the lever doesn't move the audience-model axis until the substrate is
actually wired into the composers that author porter-facing content.
The architecture itself needs three integration fixes and one
self-review step.

**Codex deep verification (2026-05-09)** caught three structural errors
in the original draft of this doc:
1. cc-content multi-file does NOT already load `derived_rules.md` — line
   419 in `briefs_content_phase_multifile.go` is the refinement
   multi-file path, not cc-content. Every porter-content composer
   except refinement is missing the rule substrate. Change 1 expanded
   accordingly.
2. Mid-phase stitch + self-review (Change 3) is NOT brief-only; the
   engine writes the assembled README to disk via
   `preStitchCodebases` but no handler returns the path to the agent.
   Change 3 reframed with two implementation options.
3. The "substrate iteration exhausted" framing was too strong. The
   remaining wiring is part of the substrate plan, not a separate
   architectural class. Tightened.

The corrections are inline below.

---

## Context — what run-33 + sim 33-postfixes-1 established

After three rounds of audits + Opus + codex review between run-32 and
run-33, the recipe still ships porter-unfriendly content with the same
defect classes:

- Tier intros lead with `Tier N — ...` label-prefix prose (env/0..5 in
  both run-33 and sim).
- Cross-service alias tokens like `${apistage_zeropsSubdomain}` appear
  in porter prose without porter context (run-33: 7 hits; sim: 2 hits).
- KB bullets describe problems the recipe yaml already prevents
  (404-on-`/`, `SignatureDoesNotMatch`, TypeORM `synchronize: true`
  corruption — all fixed in committed yaml; porters won't hit them).
- KB cites internal corpus by descriptive English (`[Zerops rolling-deploys
  reference]`) — slug stem still leaks even with descriptive wrapper.
- Cross-recipe references to recipes the porter has no reason to know
  about (`parent recipe nestjs-minimal`).
- IG steps that are recipe-internal conventions (`Alias cross-service
  env vars`) instead of Zerops-forced actions.

These are not substrate gaps. They share one trait: the agent has full
recipe-internal context (facts.jsonl in recipe-author voice + 90 KB of
synthesis machinery + scaffold decisions) and authors as if **explaining
what the recipe author did** rather than **describing what the porter
encounters**.

Counter-driven measurement was a substrate-conformance test, not a quality
test. Counters report green while every published surface fails the
porter-empathy read. The pre-flight + iteration-cost fixes
(F-A/F-B/F-D/F-47..F-54) closed several mechanical defect classes. They
did not close the audience-model class because the failure mode is
information-isolation (agent has too much context), not knowledge gap
(agent doesn't know the rule).

**The sim is faithful to prod** — verified by reading
[`cmd/zcp-recipe-sim/emit.go:82,109,155`](../cmd/zcp-recipe-sim/emit.go#L82):
strips yaml comments back to bare scaffold state, copies CLAUDE.md
verbatim from captured run, calls the same `BuildSubagentPromptForReplay`
code path production uses for cc-content briefs. The replay-adapter
wrapper only maps `record-fragment → Write` and
`zerops_knowledge → Read`. **Run-33 production has the same defects as
the sim** — verified by re-reading run-33's apidev/appdev/workerdev
README content. The system produces this consistently, regardless of
which dispatch path runs.

---

## What works (do not change)

| Component | Evidence (file:line) |
|---|---|
| Phase state machine | [workflow.go:20-28](../internal/recipe/workflow.go#L20-L28) — 8 phases, clean transitions |
| Research → Provision phase | run-33 TIMELINE: `ok:true` first try; framework + codebases + services declared cleanly |
| Scaffold's source-code authoring | run-32 Probe F: idiomatic, "would-keep" quality NestJS/Vite/NATS code |
| Cross-codebase coherence Notice loop (Step 2) | run-33: scaffold flagged `DB_PASS` vs `DB_PASSWORD` drift → feature pass renamed → fixed end-to-end |
| Engine-emitted tier_decision Why (F-B) | run-33: 100% Why-fill (10/10 shells); was 0% in run-32 |
| Surface enum lock (F-A) | run-33: 4 canonical UPPER_SNAKE values; was 9 spellings in run-32 |
| Fragment/template/stitch architecture | [assemble.go:60-167](../internal/recipe/assemble.go#L60-L167) — engine owns structural skeleton + `{TOKEN}` binding; agent owns slot bodies via fragment markers; clean separation of structural vs editorial concerns |
| Per-codebase parallelism | [briefs_content_phase.go:62](../internal/recipe/briefs_content_phase.go#L62) — `FilterByCodebase(facts, hostname)` scopes each cc-content sub-agent to its own facts; legitimate parallelism + scoping |
| Surface contract validation at write time | [surfaces.go:128-244](../internal/recipe/surfaces.go#L128-L244) — line caps, item caps, classification × surface compatibility checked at `record-fragment` |
| Sim infrastructure | [`cmd/zcp-recipe-sim/emit.go`](../cmd/zcp-recipe-sim/emit.go) — faithful to prod; same `BuildSubagentPromptForReplay` code path |
| The 39 derived rules themselves | [content/briefs/refinement/derived_rules.md](../internal/recipe/content/briefs/refinement/derived_rules.md) — codex-verified, framework-agnostic, principle-shaped (when seen, they're applied) |

---

## What's actually broken

### 1. `derived_rules.md` wiring is half-done — corrected after codex verification

**Codex correction:** my earlier claim that cc-content multi-file already
loads `derived_rules.md` was wrong. Line 419 in
`briefs_content_phase_multifile.go` is inside
`buildRefinementBriefMultiFileWithFraming` (the refinement multi-file
path), NOT cc-content. Verified by re-reading the file: cc-content
multi-file's atom inclusion at lines 82-155 has NO derived_rules. The
actual inclusion graph is narrower than I claimed.

| Composer | Loads `derived_rules.md` | Authors | Citation |
|---|---|---|---|
| **cc-content single-file** | ✗ | per-codebase intro / IG / KB / zerops.yaml comments | [`briefs_content_phase.go:478-499`](../internal/recipe/briefs_content_phase.go#L478-L499) |
| **cc-content multi-file** | ✗ | same as above (production path) | [`briefs_content_phase_multifile.go:82-155`](../internal/recipe/briefs_content_phase_multifile.go#L82-L155) |
| **env-content single-file** | ✗ | root + tier intros + tier import-comments | [`briefs_content_phase.go:154-230`](../internal/recipe/briefs_content_phase.go#L154-L230) |
| **env-content multi-file** | ✗ | same as above (production path) | [`briefs_content_phase_multifile.go:266-310`](../internal/recipe/briefs_content_phase_multifile.go#L266-L310) |
| **finalize** | ✗ | safety-net authoring; overlaps with env-content scope | [`briefs.go:791-806`](../internal/recipe/briefs.go#L791-L806) loads only `briefs/finalize/{intro, validator_tripwires}` |
| refinement single-file | ✓ | reviews everything | [`briefs_refinement.go:95-98`](../internal/recipe/briefs_refinement.go#L95-L98) |
| refinement multi-file | ✓ (Part 3b) | reviews everything | [`briefs_content_phase_multifile.go:416-420`](../internal/recipe/briefs_content_phase_multifile.go#L416-L420) |
| scaffold / feature | not needed (write source code + bare yaml; no porter-facing prose) | — | — |

**Mechanical consequence:** every porter-content-authoring composer
EXCEPT refinement is missing `derived_rules.md`. cc-content sub-agents
(per-codebase IG / KB / yaml comments), env-content sub-agent (root +
tier surfaces), and finalize sub-agent (safety-net) all author without
seeing V1-V6 (voice), V3 (slug-stem test), IG6 (Zerops-forced gate),
Y8 (no tier-vocab in yaml comments), Y15 (density target), or any other
principle from the rule substrate. The rules only land at refinement
time — and refinement scores against the rubric (issue #2 below), so
the rules don't drive ACTs there either.

The `Tier N — ...` lead-prefix on every env intro, the `${peer_alias}`
prose in IGs, the KB items describing already-fixed problems — none of
them violate any rule the authoring agent saw. The agents are
operating in good faith against the briefs they have. The briefs don't
teach the rules.

### 2. Refinement applies the rubric, not the rules-against-stitched-output

[`phase_entry/refinement.md:9`](../internal/recipe/content/phase_entry/refinement.md#L9)
states: *"Your job: read the entire stitched output and refine where the
edit threshold holds."* But the implementation:
[`briefs_refinement.go:58-99`](../internal/recipe/briefs_refinement.go#L58-L99)
loads `phase_entry/refinement.md` + `synthesis_workflow.md` +
`embedded_rubric.md` (5 criteria × 3 anchors each, line-count / pattern
shaped) + `derived_rules.md` (39 principle-shaped rules) — and the brief
prose tells the agent to **score per rubric criterion**. `derived_rules`
is supplementary citation, not the primary scoring lens.

The rubric anchors are pattern-shaped. The rules are principle-shaped.
Agent applies the rubric (because the brief tells it to) and uses rules
as backup citation. The audience-model failures (tier-prefix intros,
${peer_alias} in prose, KB describing already-fixed problems,
cross-recipe references) all pass the rubric's pattern checks but fail
the rules' principle checks. Refinement misses them because it's
walking the rubric.

### 3. cc-content sub-agents author fragments without ever reading the assembled document

`preStitchCodebases` ([`handlers.go:936-939`](../internal/recipe/handlers.go#L936-L939))
fires during `complete-phase` calls in scaffold / feature /
codebase-content phases — but ONLY when the agent terminates and calls
`complete-phase codebase=<host>`. The agent records 5+ fragments
(`intro`, `integration-guide/2`, `integration-guide/3`,
`knowledge-base`, `zerops-yaml`), then declares done. The first time
those fragments compose into `apidev/README.md` is at
self-validate-on-termination, AFTER authoring is final.

**Consequences observed in run-33 + sim:**
- KB sibling-divergence: cc-api shipped `### Gotchas` + H3+CAUTION on
  one item, cc-app + cc-worker shipped flat-bullet under `### Gotchas`.
  Each agent authors its codebase in parallel, blind to siblings — and
  blind to its own assembled document.
- IG-KB redundancy: KB bullet 2 + IG #3 in run-33 appdev both teach the
  `${apistage_zeropsSubdomain}` build-time-bake race. Same fact, two
  surfaces, no agent reading what they're building.
- Surface caps overshoot: cc-worker IG body cap (≤30 lines) overran by
  1-3 lines twice in run-33 (got 33, 31). Agent only learned at
  complete-phase time.

### 4. `derived_rules.md` is missing one rule from the plan

[`run-32-rules-from-jetstream.md:405`](run-32-rules-from-jetstream.md#L405)
defines Y13 ("for causal command sequences, group the explanation above
the list"). It's in the plan; not in the file. Single-rule gap; small.

### 5. Scaffold-recorded facts carry recipe-author voice that contaminates downstream

Run-33 facts.jsonl carries 14 strict-token-contaminated records (13.6%
rate) including `the agent`, `during scaffold`, `we chose` in `why` /
`mechanism` / `fixApplied` fields. cc-content reads these facts at
synthesis time and the voice contamination propagates into porter prose.

This is NOT a derived_rules wiring gap (scaffold doesn't write
porter-facing content; it doesn't need V1-V6 there). It IS a
fact-recording-voice teaching gap in
[`scaffold/decision_recording_slim.md`](../internal/recipe/content/briefs/scaffold/decision_recording_slim.md):
the brief teaches what to record but doesn't forbid recipe-author tokens
in the recorded text. Different fix, different atom, separate concern
from items 1-3.

---

## Changes needed

In execution order, cheapest first.

### Change 1 — Wire `derived_rules.md` into cc-content + env-content + finalize composers

**Files (corrected after codex verification — cc-content was wrongly
claimed already-loaded):**

- [`briefs_content_phase_multifile.go`](../internal/recipe/briefs_content_phase_multifile.go)
  `buildCodebaseContentBriefMultiFileWithFraming`:
  add a new Part (e.g. "rules-from-goldens") in cc-content multi-file
  parallel to refinement's Part 3b. Place it after Part 5b "naming"
  and before Part 6 "context" so it's loaded but doesn't pile into a
  cap-pressured part.
- [`briefs_content_phase_multifile.go`](../internal/recipe/briefs_content_phase_multifile.go)
  `buildEnvContentBriefMultiFileWithFraming`:
  add a new Part (e.g. "rules-from-goldens") after Part 3 yaml-style.
- [`briefs.go`](../internal/recipe/briefs.go) `BuildFinalizeBrief`
  (line 791-806): add `derived_rules.md` to the atom slice. **Cap
  audit required:** `FinalizeBriefCap = 14 KB` ([briefs.go:333](../internal/recipe/briefs.go#L333));
  `derived_rules.md` is ~15 KB. Three options:
  1. Raise `FinalizeBriefCap` to ~32 KB.
  2. Convert finalize to multi-file shape (parallel to cc/env/refinement
     multi-file paths).
  3. Author a trimmed `derived_rules_finalize.md` containing only the
     rules finalize actors against (V1-V6 voice, R1-R6 root, T1-T4
     tier-README, TY1-TY5 tier-yaml, Y8 no-tier-vocab, RF1, PD1) —
     drop IG / KB / Y1-Y15 yaml-comment rules since finalize doesn't
     author IG/KB/codebase-yaml.
  Option 3 is the cheapest; Option 2 is the cleanest long-term.
- [`briefs_content_phase.go`](../internal/recipe/briefs_content_phase.go)
  cc-content + env-content single-file paths (lines 478-499 and 154-230):
  same atom additions for parity, even though multi-file is the
  production path. Keeping single + multi in sync prevents the kind of
  composer-drift this run-33 round already burned cycles on.

**Why:** cc-content + env-content + finalize are the porter-facing
content-authoring composers. The substrate's voice/principle rules must
be visible to the agent at authoring time, not just at refinement time.
Codex's deep verification confirmed every porter-content composer
(except refinement) is missing the rule substrate.

**Why not scaffold + feature:** they write source code and bare yaml.
No porter-facing prose. derived_rules is content-quality teaching, not
code-authoring teaching. Different surface, different audience, different
rules.

**Verification:** re-emit cc-api brief + env brief in sim; grep for
`V3` / `IG6` / `Y15` inside the brief contents. Should appear in their
new Parts. Re-dispatch cc-content + env-content sub-agents against
captured run-33 facts; check whether the audience-model defects drop:
- env intros stop leading with `Tier N — ...`
- cc-content stops shipping `${peer_alias}` in IG prose
- KB stops describing already-fixed problems

**Cost:** ~1 hour for the wiring + cap-audit decision on finalize.
Mechanical edits + one cap-related architecture call.

### Change 2 — Replace rubric-walk with rule-walk-against-stitched-output in refinement

**Files:**
- [`briefs_refinement.go`](../internal/recipe/briefs_refinement.go)
  `BuildRefinementBrief`: drop `embedded_rubric.md` from the atom list.
- [`briefs_content_phase_multifile.go`](../internal/recipe/briefs_content_phase_multifile.go)
  `BuildRefinementBriefMultiFile`: same — drop `embedded_rubric.md` part.
- [`content/briefs/refinement/synthesis_workflow.md`](../internal/recipe/content/briefs/refinement/synthesis_workflow.md):
  rewrite the scoring loop. Replace "score against 5 criteria × 3
  anchors" with "walk every rule in `derived_rules.md` against every
  stitched document; ACT on every violation citing rule + exact phrase
  + preserving edit". Keep snapshot/restore semantics for safety.
- [`content/briefs/refinement/embedded_rubric.md`](../internal/recipe/content/briefs/refinement/embedded_rubric.md):
  delete or archive (replaced by `derived_rules.md` as primary scoring
  substrate).

**Why:** [`phase_entry/refinement.md:9`](../internal/recipe/content/phase_entry/refinement.md#L9)
already states "read the entire stitched output and refine". The rubric
makes refinement walk pattern-shaped anchors fragment-by-fragment.
Pattern anchors miss principle-shaped failures (audience model, tier
prefix, slug-stem evolution) — exactly what slipped through run-33's
refinement. Rule-walk against stitched output is closer to "human reads
the recipe and applies their internal rule list".

**Verification:** dispatch refinement against captured run-33 stitched
output with new substrate; compare ACT count + ACT classes vs run-33's
12-fragment ACT pass. New substrate should ACT on tier-prefix intros,
${peer_alias} prose, KB-already-fixed bullets — classes refinement
currently misses.

**Test fallout (codex caught):** the rubric is currently pinned by
[`briefs_refinement_test.go:39-43`](../internal/recipe/briefs_refinement_test.go#L39-L43)
+ [`briefs_refinement_test.go:127-144`](../internal/recipe/briefs_refinement_test.go#L127-L144).
Those tests assert specific rubric atoms / anchors are present in the
refinement brief; deleting `embedded_rubric.md` will fail them. Need
to update the tests to assert derived_rules-walk shape instead. The
snapshot/restore primitive is phase/mode based ([handlers.go:707-748](../internal/recipe/handlers.go#L707-L748))
and does NOT depend on rubric structure — that path stays clean.

**Cost:** ~half a day. Most of the work is rewriting
`synthesis_workflow.md` to express rule-walk semantics + adapting the
test fixtures that pin the current rubric-walk behaviour.

### Change 3 — Mid-phase stitch + self-review per cc-content sub-agent (NOT brief-only — codex correction)

**Codex correction:** my earlier claim that this was a brief-only edit
was wrong. `preStitchCodebases` writes the assembled README to disk as
a side-effect of scoped `complete-phase` ([handlers.go:1230-1251,
1275-1293](../internal/recipe/handlers.go#L1230)). However, **no
handler returns the assembled document path back to the agent's
response payload** — only `status` / `violations` / `notices` ride on
the response ([handlers.go:962-967](../internal/recipe/handlers.go#L962-L967)).
For the agent to read its own stitched output, it must either be told
the disk-path convention explicitly OR a handler must surface the path.

**Two implementation options:**

1. **Brief edit + disk-path convention (cheaper).** Teach the agent
   the path: after scoped `complete-phase codebase=<self>`, the
   assembled README is at `<cb.SourceRoot>/README.md`. Tell the agent
   to `Read` from that path before terminating. Document the convention
   in `synthesis_workflow.md` so the agent doesn't have to derive it.
   Cost: ~1-2 hours brief edit.

2. **Handler change + brief edit (cleaner).** Extend the scoped
   `complete-phase` response payload with a `stitchedPath` field
   pointing at the just-written README. Update brief to read from the
   response payload. Cost: ~half day handler + test changes + brief
   edit.

Option 1 lands faster and uses an existing primitive. Option 2 is
robust against path-convention drift (e.g., if someone moves
`writeCodebaseSurfaces` later). Recommendation: ship Option 1 first
and only escalate to Option 2 if path-convention proves brittle.

**Files (Option 1):**
- [`content/briefs/codebase-content/synthesis_workflow.md`](../internal/recipe/content/briefs/codebase-content/synthesis_workflow.md):
  add a final step. After authoring all fragments, call scoped
  `complete-phase codebase=<self>` to trigger `preStitchCodebases`.
  When response is `ok:false` with violations, fix per existing
  batch-fix rule (F-48). When response is `ok:true` (or after
  resolving violations), `Read` the assembled README at
  `<cb.SourceRoot>/README.md` AND `<cb.SourceRoot>/zerops.yaml`.
  Walk the document against `derived_rules.md` rule-by-rule. ACT via
  `record-fragment mode=replace` on any rule violation found
  (sibling-shape divergence, IG-KB redundancy, audience-model leaks,
  tier-vocab on codebase surfaces, etc.). Then call un-scoped
  `complete-phase` to terminate.
- Engine confirmation: [handlers.go:932-967](../internal/recipe/handlers.go#L932-L967)
  scoped complete-phase pre-stitches without phase advance.
  [handlers.go:1230-1251](../internal/recipe/handlers.go#L1230-L1251)
  preStitchCodebases scopes to one codebase.
  [handlers.go:1275-1303](../internal/recipe/handlers.go#L1275-L1303)
  writeCodebaseSurfaces writes to disk at `<cb.SourceRoot>/README.md`.

**Why:** the agent currently authors 5+ fragments blind to the assembled
document. Sibling-divergence and IG-KB redundancy slip through because
no agent reads what they're building. Mid-phase stitch + re-read closes
this — the engine already supports the disk side-effect; the brief just
doesn't tell the agent to read from disk afterward.

**Verification:** sim cc-content sub-agent against captured run-33 facts
with new brief; check whether the agent (a) calls scoped complete-phase,
(b) Reads the assembled README, (c) issues at least one
`record-fragment mode=replace` call when violations are present.
Compare KB-IG redundancy + sibling-divergence on stitched output.

**Cost (Option 1):** ~1-2 hours brief edit + verification.

### Change 4 — Tighten fact-recording voice rule (scaffold + feature)

**Files (codex verification flagged the second one):**
- [`content/briefs/scaffold/decision_recording_slim.md`](../internal/recipe/content/briefs/scaffold/decision_recording_slim.md) —
  loaded by scaffold composer at [`briefs.go:468`](../internal/recipe/briefs.go#L468).
- [`content/briefs/feature/decision_recording.md`](../internal/recipe/content/briefs/feature/decision_recording.md) —
  loaded by feature composer at [`briefs.go:672`](../internal/recipe/briefs.go#L672).
  Feature phase ALSO records facts; same contamination risk applies. Codex
  confirmed both are reachable.
- (Note: the full `briefs/scaffold/decision_recording.md` is on disk
  but NOT loaded by any composer per codex sweep —
  [`briefs.go:447-455`](../internal/recipe/briefs.go#L447-L455) explicitly
  switched scaffold to the slim variant in Run-21 R2-1, kept the full
  file only as a worked-example fixture target. It can stay
  unloaded — no edit needed.)

**Edit (apply to both loaded files):** add an explicit "Forbidden tokens
in `why` / `mechanism` / `fixApplied` fields" block. List `the agent`,
`during scaffold`, `during feature`, `we chose`, `we use`, `we set`,
`recipe author`, `scaffold sub-agent`, `feature sub-agent`,
`record-fact`, `zerops_dev_server`. Reason: facts flow into
cc-content/env-content briefs; voice contamination at record time
propagates to porter prose at synthesis time.

**Why:** different fix from changes 1-3. Scaffold + feature don't write
porter-facing content; they record facts. The contamination source isn't
"these phases have wrong rules" — it's "their fact-recording teaching
doesn't forbid recipe-author tokens in fact text". Voice contamination
must be cut at record time, not at synthesis time.

**Verification:** sim a fresh scaffold + feature pass with updated briefs
on a captured plan; grep facts.jsonl for the forbidden tokens. Should
drop from 14/103 to near-0.

**Cost:** ~30 minutes. Two atom edits (slim + feature variant).

### Change 5 — Add Y13 to derived_rules

**File:**
[`content/briefs/refinement/derived_rules.md`](../internal/recipe/content/briefs/refinement/derived_rules.md).

**Edit:** add Y13 — *"For causal command sequences (`buildCommands`,
`initCommands`), group the explanation above the list rather than per-
command inline. Showcase line 82-92 carries the canonical shape: one
block comment narrating the whole sequence in causal order."* Cite
plan line 405.

**Why:** plan declared 39 surviving rules; file currently has 38 of
them (plus IG6 and Y15 added during run-33 fixes). Y13 was reworded but
the reworded text never landed in the file. Mechanical drop-in.

**Cost:** ~5 minutes.

---

## What we are deliberately NOT doing

| Not doing | Why |
|---|---|
| Iterating substrate further (more rules, sharper anchors) | Codex verification softened the "substrate iteration exhausted" framing: the wiring IS part of the substrate plan, not a separate class. The 39 rules are good when seen; what's unfinished is (a) the wiring across content-authoring composers, (b) the scoring lens at refinement. **Until both ship**, more rules add inventory, not effectiveness. After both ship, evaluate whether new rules are needed against measured residual defects, not against speculation. |
| Adding more counter regexes | Counters validate against substrate, not against porter empathy. Counter-conformance is necessary but insufficient. |
| Inlining a golden recipe (e.g. jetstream) as a reference exemplar | Principles need to be framework-agnostic. Inlining jetstream teaches "match this exact recipe"; the recipes go through cc-content for any framework. derived_rules is the right abstraction shape. |
| Loading derived_rules into scaffold + feature | They write source code and bare yaml. No porter-facing prose. Different surface, different audience, different teaching. |
| Running another full prod dogfood until changes 1-3 ship | Counter-driven measurement said pre-flight worked; reading the actual surfaces said it didn't. Until refinement scores against principles instead of patterns, fresh dogfoods will keep producing the same defects. Sim is cheaper and faithful. |
| RF1 (`## Recipe features`) + PD1 (`## Production vs Development`) authoring actor | Architectural decision pending. cc-content authors per-codebase fragments; finalize stitches; nobody owns codebase-README document-level synthesis. Defer until changes 1-3 land — those may close enough of the audience-model gap that RF1/PD1 becomes the most-visible remaining miss, at which point we decide who authors it. |
| Cold-read pass with starved context (proposed earlier) | Promote to backup plan if changes 1-3 don't move the audience-model axis. Cheaper interventions first. |

---

## Order of execution

1. **Change 5** (Y13 add) — 5 min, mechanical, no risk.
2. **Change 1** (env-content + finalize wiring) — 30 min. Re-run sim
   against captured run-33; measure whether env intros stop leading
   with `Tier N — ...`.
3. **Change 4** (scaffold fact-recording voice) — 20 min. Verify with
   captured-plan scaffold sim.
4. **Change 3** (mid-phase stitch + self-review) — 1-2 hours. Re-run
   cc-content sim; measure KB-IG redundancy + sibling-divergence on
   stitched output.
5. **Change 2** (refinement rule-walk) — half a day. Re-run refinement
   sim; measure ACT count + ACT classes against captured run-33's
   refinement output.

Each step is verifiable in the sim before proceeding. If any change
shows zero movement on its target metric, stop and re-diagnose before
adding more changes.

After all five land, run a fresh prod dogfood. Expected outcome based
on current diagnosis:
- Tier intros no longer lead with `Tier N — ...` (Change 1).
- Scaffold facts no longer carry `the agent` / `during scaffold` (Change 4).
- KB-IG redundancy + sibling-divergence drop (Change 3).
- Refinement ACTs on audience-model classes refinement currently misses
  (Change 2).
- Y13 has a citation path (Change 5).

If the dogfood still ships the same defect classes, the diagnosis is
wrong and the next iteration goes to the cold-read pass with starved
context.

---

## Verification approach

Per change: sim against captured run-33 output, measure delta on the
specific axis the change targets. NO new prod dogfood until all changes
land. Sim is cheaper, faithful (verified at
[`cmd/zcp-recipe-sim/emit.go`](../cmd/zcp-recipe-sim/emit.go)), and the
captured run-33 facts/source already exercise every defect class we're
testing against.

Counter measurement remains the last-mile substrate-conformance check
(Counter #1-#8 from
[run-32-phase-1-baselines.md](run-32-phase-1-baselines.md)) but is not
the primary signal. The primary signal is reading the stitched
documents as a porter would — does every token resolve, does every KB
bullet name a problem the porter will hit, do tier intros describe the
tier without prefixing the label.
