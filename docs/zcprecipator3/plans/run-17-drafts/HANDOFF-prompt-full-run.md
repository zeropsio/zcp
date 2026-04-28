# Handoff prompt for fresh instance — run-17 full implementation

Copy the section below verbatim into a fresh Opus 4.7 (1M context)
session running in `/Users/fxck/www/zcp`. It is self-contained — the
fresh instance has no prior conversation context and only the
referenced files to work from.

The wrapper text above this `## Prompt` heading is meta-context
for the human handing off. Don't copy it.

---

## Prompt

You are implementing the full run-17 program for the zcprecipator3
recipe-authoring engine. Tranche 0.5 (the load-bearing distillation
atoms + content quality rubric) shipped on 2026-04-28 by a prior
instance and is verified green: 7 reference atoms exist, all PASS-
example quotes are verbatim cross-checked, all FAIL examples are
verified against `runs/16/<codebase>/README.md`, `go test ./... -short`
clean, `make lint-local` clean (122 atoms, 0 violations). Your job
is everything else — Tranches -1 (optional), 0, 1, 2, 3, 4, 5, 6, 7, 8.

The implementation guide is the per-tranche contract. Read it once
end-to-end before starting; reread the per-tranche section before
each tranche.

**Read first, in this order**:

1. [docs/zcprecipator3/plans/run-17-implementation.md](docs/zcprecipator3/plans/run-17-implementation.md)
   — full per-tranche implementation contract. **This is the
   binding document for everything you do.** §0 (confidence model)
   through §17 (maintenance hooks) is the full scope; §14
   (consolidated gate criteria) is your closure checklist.
2. [docs/zcprecipator3/plans/run-17-prep.md](docs/zcprecipator3/plans/run-17-prep.md)
   — predecessor prep doc. Treat run-17-implementation.md as
   authoritative when they conflict (per the implementation guide's
   §1 corrections).
3. [docs/spec-content-surfaces.md](docs/spec-content-surfaces.md) —
   the seven content surfaces. §305-330 (friendly-authority voice)
   and §349-362 (Classification × surface compatibility table) are
   the verbatim sources for the synthesis_workflow.md rewrite at
   Tranche 1.
4. [docs/spec-content-quality-rubric.md](docs/spec-content-quality-rubric.md)
   — Tranche 0.5 rubric. **Don't edit.** Embed verbatim into
   `internal/recipe/content/briefs/refinement/embedded_rubric.md`
   when you reach Tranche 4.
5. [internal/recipe/content/briefs/refinement/](internal/recipe/content/briefs/refinement/)
   — the seven Tranche 0.5 atoms. **Don't edit.** Read them to
   extract verbatim quotes for Tranche 1's brief embed and Tranche
   4's refinement brief.
6. [docs/zcprecipator3/runs/16/](docs/zcprecipator3/runs/16/) — run-16
   dogfood artifacts. Used as frozen-fact replay for Tranche -1
   pre-flight harness and for FAIL-example cross-checks.
7. [CLAUDE.md](CLAUDE.md) — project invariants. The 350-line soft
   cap, dependency rule (topology / ops / workflow / platform), atom
   authoring contract, RED→GREEN→REFACTOR discipline.

---

## Tranche dispatch order

The implementation guide §2 enumerates 10 tranches. Order is binding.
Each tranche has a gate criterion (§14) that must close before the
next ships.

| # | Name | Sketched effort | Status as of 2026-04-28 |
|---|---|---:|---|
| -1 | Pre-flight harness | 1 day | NOT STARTED — **OPTIONAL** |
| 0 | Free quality wins (Why truncation fix) | 2 hours | NOT STARTED |
| 0.5 | Distillation atoms + rubric | n/a | **DONE — verified green** |
| 1 | Engine-emit retraction + brief embed | 2 days | NOT STARTED |
| 2 | KB symptom-first record-time refusal | 0.5 day | NOT STARTED |
| 3 | CodebaseGates split (R-16-1) | 0.5 day | NOT STARTED |
| 4 | Refinement sub-agent | 3 days | NOT STARTED |
| 5 | Refusal aggregation | 0.25 day | NOT STARTED |
| 6 | deployFiles narrowness validator | 0.5 day | NOT STARTED |
| 7 | v2 atom tree deletion | 1 day | NOT STARTED — **gates on Tranche 8 dogfood quality bar** |
| 8 | Sign-off (incl. dogfood × 2 + analysis) | 2 days | NOT STARTED — **HUMAN CHECKPOINT** |

**Total estimate**: ~10-14 days of careful work. Quality, not
wall-time, is the bar (per CLAUDE.md memory feedback —
`feedback_quality_not_walltime.md`).

---

## Tranche-specific notes

### Tranche -1 (optional pre-flight harness)

Implementation guide §3. Two CLIs (`cmd/zcp-preflight-codebase-content/`
+ `cmd/zcp-preflight-refinement/`) plus a loader package
(`internal/preflight/`) that reconstructs the `recipe.Plan` from
`docs/zcprecipator3/runs/16/SESSION_LOGS/main-session.jsonl`. The
harness lets you grade Tranche 1 brief output against rubric BEFORE
the run-17 dogfood.

**Decision**: skip Tranche -1 if you're confident in Tranche 1's
brief embed quality from atom inspection alone. If you skip, the
Tranche 1 gate (§6.10 — "pre-flight harness shows ≥8.0 average") is
unreachable; you fall back to "atom inspection + brief diff size +
manual smoke-test in fresh Claude Code session."

**Recommendation**: build it. The 60→80% confidence lift the
implementation guide claims (§0.2) depends on the pre-flight gating
between Tranche 1 and Tranche 4. Without it, you're shipping
Tranche 4 (the highest-DoF tranche) on faith that Tranche 1's
distillation transferred shape.

### Tranche 0 (free quality wins)

Implementation guide §4. Single 1-line edit at
`briefs_content_phase.go:330` (delete the `truncate(f.Why, 120)`
wrapper for `FactKindPorterChange` and `FactKindFieldRationale`)
plus 4 tests. Ship as a standalone commit. Net -3 LoC.

### Tranche 1 (engine-emit retraction + brief embed)

Implementation guide §6. The synthesis_workflow.md rewrite (§6.2)
draws from the Tranche 0.5 reference atoms in
`internal/recipe/content/briefs/refinement/`:

- Voice patterns (4 quotes): pull verbatim from
  [reference_voice_patterns.md](internal/recipe/content/briefs/refinement/reference_voice_patterns.md)
  Pass 1-4.
- KB symptom-first fail-vs-pass: pull from
  [reference_kb_shapes.md](internal/recipe/content/briefs/refinement/reference_kb_shapes.md)
  Pass 1, Pass 2, Fail 1 (with refined-to).
- IG one-mechanism: pull from
  [reference_ig_one_mechanism.md](internal/recipe/content/briefs/refinement/reference_ig_one_mechanism.md)
  Pass 1, 2, 3 (showcase IG #2-4 sequence).
- Citation map + cite-by-name: pull from
  [reference_citations.md](internal/recipe/content/briefs/refinement/reference_citations.md)
  Pass 1.
- Classification × surface table: verbatim from spec-content-surfaces.md
  §349-362.

The decision-recording worked examples (§6.6) are ALREADY appended
to scaffold + feature `decision_recording.md` by Tranche 0.5.

### Tranche 2 (KB symptom-first refusal)

Implementation guide §7. Mechanical regex check at
`slot_shape.go::checkCodebaseKB`. Test design has 8 cases (4 PASS,
2 FAIL, 1 directive-mapped, 1 aggregate). The note on
`synchronize: false` false-positive — recommend (A) accept the
false-positive in run-17, tune to (B) config-key allowlist if
dogfood evidence shows it's high-cost.

### Tranche 3 (CodebaseGates split — R-16-1 closure)

Implementation guide §8. Splits `CodebaseGates()` into
`CodebaseScaffoldGates()` (fact-quality only) and
`CodebaseContentGates()` (content-surface validators). Atom update
to `phase_entry/scaffold.md` to align teaching with gate. Backward-
compat shim retained for run-18 cleanup.

### Tranche 4 (refinement sub-agent — highest DoF)

Implementation guide §9. New phase, new composer, new dispatch
wiring, new snapshot/restore primitive. Reads the seven Tranche 0.5
atoms verbatim. The embedded rubric (§9.4) is byte-identical to
`docs/spec-content-quality-rubric.md` — `go:generate` directive
keeps them in sync; `TestEmbeddedRubric_MatchesSpec` pins drift.

**Pre-flight gate (§9.11)**: hand-grade 20 randomly-selected
refinement attempts against Tranche-1-output frozen fragments;
require ≥60% refinement-correct rate. If <60%, distillation atoms
get a second pass. **You can grade this yourself** (you've read
the rubric); the gate is not a hard human-checkpoint, but the
refinement-correct rate is a judgment call worth slowing down on.

### Tranches 5, 6 (small mechanical)

Implementation guide §10, §11. Aggregation + deployFiles validator.
Each is <1 hour of code + tests.

### Tranche 7 (v2 deletion — -2,500 LoC)

Implementation guide §12. **Gates on Tranche 8 dogfood quality bar**
— don't delete v2 until v3 has demonstrably shipped a recipe at the
quality bar. The implementation guide §12.3 has a step-by-step
sequence; follow it exactly. Recommend Path A (repoint analyzer at
v3) over Path B (delete analyzer entirely).

### Tranche 8 (sign-off — HUMAN CHECKPOINT)

Implementation guide §13. Two dogfoods:
- 17a — small-shape calibration recipe (analog-static-hello-world
  or similar single-codebase, no-managed-services).
- 17b — nestjs-showcase (direct comparison to run-16).

**The dogfood requires the user to drive** — running the actual
zcprecipator workflow against a real repo, capturing artifacts,
manually grading per the rubric. **Stop before Tranche 8 and hand
back to the user**; don't try to dogfood from this session.

---

## Working discipline

### Before each tranche

1. Re-read the relevant section of `run-17-implementation.md` (it's
   the binding contract).
2. Run `go test ./internal/recipe/... -short` to confirm baseline
   green.
3. Sketch the tranche's test set (RED) before the implementation
   (GREEN). Per CLAUDE.md, "Write tests + implementation in the
   same commit without RED first" is forbidden.

### After each tranche

1. Run `go test ./... -short` — all packages must stay green.
2. Run `make lint-local` — 0 violations.
3. Verify the §14 gate criterion explicitly. Don't proceed without
   meeting it.
4. Commit with a descriptive message. Use `recipe(run-17): tranche
   N — <one-line summary>` style; the repo's recent commits show
   the convention.

### When you hit ambiguity

The implementation guide has a §15 (open questions resolved) — Q1
through Q5 are pre-decided. If a new ambiguity surfaces, write a
short rationale and proceed; don't email-style ask the user. The
quality bar is judgment-driven, not committee-driven.

### When a tranche gate fails

The implementation guide §6.10 + §9.11 explicitly name what to do.
Tranche 1 fails → Tranche 4 holds at HEAD; distillation atoms get
a second pass. Tranche 4 refinement-correct <60% → second
distillation pass; threshold heuristic re-tuned. **Don't ship
Tranche 4 if the gate fails.** Tranches 2, 3, 5, 6 don't depend on
distillation quality — they MAY ship in parallel.

### When you finish each tranche

Update [docs/zcprecipator3/plan.md](docs/zcprecipator3/plan.md) with
the closure marker. Per project memory (`project_plan_living_doc.md`),
plan.md is the living plan doc — analyzers edit it.

---

## Important context the prior instance flagged

These are awareness items from the Tranche 0.5 author. Read once
before starting Tranche 1.

### 1. Engine-source-vs-draft drift on the appended worked examples

Tranche 0.5 appended worked examples to scaffold + feature
`decision_recording.md`. The draft claimed verbatim parity between
Worked examples 1-3 Why prose and `engine_emitted_facts.go:41-105`.
Cross-checking found the appended drafts are pedagogically expanded
beyond the engine source. The drift is moot at Tranche 1 because
you delete the engine-emit Why strings.

### 2. `${appVersionId}` literal in scaffold/decision_recording.md

The Tranche 0.5 author's first append violated the budget invariant
pinned by `TestBrief_Scaffold_OmitsInitCommandsModelWhenUnused` (the
test asserts `${appVersionId}` does NOT appear in the scaffold brief
when no codebase declares `HasInitCommands`). The literal was
replaced with prose pointing at the conditionally-loaded
`init-commands-model.md` atom. Don't reintroduce literal
`${appVersionId}` references in scaffold-loaded atoms during the
synthesis_workflow.md rewrite.

### 3. The handoff prompt's FAIL-shape was partially inaccurate

The Tranche 0.5 handoff named `# api in zeropsSetup: prod, 0.5 GB
shared CPU, minContainers: 2` as the run-16 tier-4 FAIL preamble.
The actual run-16 has this shape on the **appdev** (line 28) and
**worker** (line 41) blocks of `runs/16/environments/4 — Small
Production/import.yaml`, not on the **api** block. The Tranche 0.5
atoms cite the appdev + worker quotes verbatim; if you cite the
FAIL shape in synthesis_workflow.md, use those exact quotes.

### 4. Trade-off Criterion 4 actual run-16 miss rate is ~30-40%

The Tranche 0.5 handoff said "most run-16 KB bullets" are one-sided
on Criterion 4. Cross-checking, many run-16 KB bullets ARE
two-sided. The clearly one-sided run-16 FAILs are workerdev
`**Liveness without HTTP**` and `**Subject typo silently stops
delivery**`. Don't overstate the miss rate in synthesis_workflow.md.

### 5. Pre-flight harness is the load-bearing risk-mitigation

If you skip Tranche -1, the 80% first-dogfood-above-golden
confidence the implementation guide claims drops back to ~60%.
The harness is +300 LoC of deletable scaffolding (Tranche 8 §13.5
deletes it). **Strongly recommend building it.**

### 6. Tranche 0.5 Why drift was flagged but unfixed

The drafts' Why prose for worked examples 1-3 expanded beyond
engine source. Drafts were appended as-is per the pedagogical
intent. After Tranche 1 retracts engine-emit, these worked examples
become the canonical recording-shape teaching for the deploy-phase
agents. The drift becomes irrelevant.

---

## Constraints

- You implement Tranches -1 (optional), 0, 1, 2, 3, 4, 5, 6, 7. Stop
  before Tranche 8 and hand back to the user — Tranche 8 (dogfood)
  requires user-driven recipe creation against a real repo.
- Do NOT edit the rubric (`docs/spec-content-quality-rubric.md`),
  `refinement_thresholds.md`, or any of the `reference_*.md` atoms
  under `briefs/refinement/`. Those are upstream contracts shipped
  in Tranche 0.5.
- Do NOT edit `run-17-implementation.md` or `run-17-prep.md` —
  they're the contract. If you find a §1-style correction needed,
  surface it in the report at handoff back; don't silently amend.
- The 350-line soft cap per `.go` file applies (per CLAUDE.md). If
  you split a file, follow the existing peer-file naming convention
  in `internal/recipe/`.
- The 4-layer architecture is pinned (per CLAUDE.md). Watch the
  layers when adding new files: `topology/` is layer 2 (zero
  internal imports); `platform/` is layer 1 (no internal imports);
  `ops/` and `workflow/`/`recipe/` are layer-3 peers (no
  cross-imports). Tranche 4's refinement composer lives in
  `internal/recipe/`, peer to existing brief composers.
- Per CLAUDE.md, RED→GREEN→REFACTOR. Pure refactors skip RED but
  verify all layers stay green.
- Per CLAUDE.md, English everywhere; phased refactors with no
  half-finished states; rename safety (grep separately for calls,
  types, strings, tests).
- Per CLAUDE.md memory feedback (`feedback_lint_full.md`,
  `feedback_lint_before_release.md`): always run `make lint-local`
  (full lint), not `make lint-fast`. Run before every commit.

---

## What's already done

- All Tranche 0.5 deliverables (7 reference atoms, rubric, worked
  examples appended to scaffold + feature `decision_recording.md`).
- Self-grade across the 5 distilled atoms: 8.6 average — passes
  the §5.4 ≥8.5 gate.
- All verbatim quotes cross-checked against named source files.
- `go test ./... -short` clean as of 2026-04-28.
- `make lint-local` clean (122 atoms scanned, 0 violations).

The Tranche 0.5 verification report is at the end of the prior
session's transcript. Five anomalies were flagged; items 1-4 above
restate the load-bearing ones; the engine-source drift is the
mootest because Tranche 1 retracts engine-emit anyway.

---

## Estimated total effort

| Tranche | Effort (hours) |
|---|---:|
| -1 (optional) | 6-8 |
| 0 | 1-2 |
| 1 | 12-15 |
| 2 | 3-4 |
| 3 | 3-4 |
| 4 | 18-24 |
| 5 | 1-2 |
| 6 | 3-4 |
| 7 | 6-8 |
| **Total (excluding 8)** | **53-71 hours** |

Tranche 8 is user-driven (~12-16 hours of dogfood + analysis +
sign-off).

---

## Triple-verification report at full handoff back

When you finish Tranches -1 through 7, produce a §12-style report
covering:

1. **Tranche-by-tranche closure**: for each tranche, the §14 gate
   criterion + PASS/FAIL + LoC delta + commit hash.
2. **Pre-flight harness output** (if Tranche -1 was implemented):
   the rubric grade per surface for the post-Tranche-1 brief replay
   on at least apidev codebase from runs/16. The Tranche 1 gate is
   ≥8.0 average across 5 criteria, no criterion below 7.5.
3. **Refinement pre-flight result** (Tranche 4 §9.11): refinement-
   correct rate on the 20-fragment hand-graded sample. Gate is ≥60%.
4. **Test inventory**: every new test name, what it pins, in which
   tranche it landed.
5. **Build + lint**: final `go test ./... -short` + `make lint-local`
   output (expect all green; 0 violations).
6. **v2 deletion summary** (Tranche 7): files deleted, LoC removed,
   `zerops_workflow` MCP tool removed from registration.
7. **Anomalies**: any drift between the implementation guide's
   claims and the actual codebase you noticed during this work.
8. **Confidence on run-17 dogfood readiness**: HIGH / MEDIUM / LOW
   with reasoning. Hand back to user with explicit "ready for
   Tranche 8 dogfood" or "blocked at <tranche>; <gap>".

The user takes Tranche 8 from there: small-shape dogfood, then
showcase-shape dogfood, then sign-off per §13.6.
