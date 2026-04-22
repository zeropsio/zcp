# PLAN — zcprecipator2 long-term plan + current state

**Status**: living doc. Every run analyzer edits this file as part of writing a `runs/vN/verdict.md`.
**Last updated**: 2026-04-22 (post-v37 analysis, pre-v38 commission — §2 re-verified end-to-end against source)
**Supersedes**: [`README.md`](README.md) §1–§2 as the *current* statement of intent. README stays frozen as the original design rationale; this doc tracks the moving target.

---

## 1. North star (unchanged from README, 2026-04-20)

**Delete the 3,438-line `internal/content/workflows/recipe.md` monolith. Replace it with atom files under `internal/content/workflows/recipe/` that the engine stitches at dispatch time.**

Secondary invariants, equal weight:

1. **Sub-agent briefs transit the dispatch boundary byte-identically** from engine-authored atoms. No main-agent paraphrase surface.
2. **Convergence gate**: deploy rounds ≤ 2, finalize rounds ≤ 1 on `nestjs-showcase`.
3. **Content-quality gate**: gotcha-origin ≥ 80% genuine; 0 manifest↔content inconsistencies; 0 self-inflicted gotchas shipped.
4. **Cross-scaffold symbol contract** (env vars, endpoints, hostnames) byte-identical across parallel scaffold dispatches.

The full principle list is [principles.md](03-architecture/principles.md) P1–P8. The full calibration bars are [calibration-bars-v35.md](05-regression/calibration-bars-v35.md) — 102 bars, 6 headline.

---

## 2. Current state (2026-04-22, triple-verified against source)

### Two-layer guidance architecture

The main agent gets guidance from two paths:

- **Step-entry guide** — delivered when a phase begins (no active substep). Served by [`resolveRecipeGuidance`](../../internal/workflow/recipe_guidance.go#L102) at [recipe_guidance.go:80](../../internal/workflow/recipe_guidance.go#L80). **Always reads recipe.md** via `content.GetWorkflow("recipe")` + `extractSection`.
- **Sub-step guide** — delivered as each substep activates within Generate/Deploy/Close. Served by [`buildSubStepGuide`](../../internal/workflow/recipe_guidance.go#L498) at [recipe_guidance.go:63](../../internal/workflow/recipe_guidance.go#L63). Prefers `phases/` atom via `LoadAtomBody`; falls back to `ResolveTopic` → recipe.md if no atom mapping.

Research/Provision/Finalize have **no substeps** ([recipe_substeps.go:78-80](../../internal/workflow/recipe_substeps.go#L78) — `initSubSteps` returns nil for those). Their only guidance is step-entry → recipe.md.

### Per-surface cutover status (verified 2026-04-22)

| Surface | Current source | Status | Evidence |
|---|---|---|---|
| Research step-entry | recipe.md §`research-showcase` + `research-minimal` | ❌ recipe.md | [recipe_guidance.go:116-125](../../internal/workflow/recipe_guidance.go#L116) |
| Provision step-entry | recipe.md §`provision` | ❌ recipe.md | [recipe_guidance.go:152-155](../../internal/workflow/recipe_guidance.go#L152) |
| Generate step-entry | recipe.md §`generate-skeleton` + blocks | ❌ recipe.md | [recipe_guidance.go:127-150](../../internal/workflow/recipe_guidance.go#L127) |
| Deploy step-entry | recipe.md §`deploy-skeleton` + blocks | ❌ recipe.md | [recipe_guidance.go:157-167](../../internal/workflow/recipe_guidance.go#L157) |
| Finalize step-entry | recipe.md §`finalize-skeleton` + blocks | ❌ recipe.md | [recipe_guidance.go:169-175](../../internal/workflow/recipe_guidance.go#L169) |
| Close step-entry | recipe.md §`close-skeleton` + blocks | ❌ recipe.md | [recipe_guidance.go:177+](../../internal/workflow/recipe_guidance.go#L177) |
| Generate × 4 substeps | `phases/generate/**/entry.md` | ✅ atoms | [atomIDForSubStep:556-566](../../internal/workflow/recipe_guidance.go#L556) |
| Deploy × 12 substeps | `phases/deploy/*.md` | ✅ atoms | [atomIDForSubStep:567-593](../../internal/workflow/recipe_guidance.go#L567) |
| Close × 3 substeps | `phases/close/*.md` | ✅ atoms | [atomIDForSubStep:594-602](../../internal/workflow/recipe_guidance.go#L594) |
| Sub-agent dispatch briefs (5 roles) | `briefs/**/*.md` via engine-built brief | ✅ atoms (Cx-5 guard unverified at runtime) | [subagent_brief.go](../../internal/workflow/subagent_brief.go), [recipe_guidance.go:512-514](../../internal/workflow/recipe_guidance.go#L512) |
| `principles/*` atoms | pointer-included at stitch time | ✅ atoms | [recipe/principles/](../../internal/content/workflows/recipe/principles/) — 16 files, 671 LoC |
| `phases/research/` (3 files), `phases/provision/` (13), `phases/finalize/` (6) | ⚠️ **orphaned** — registered in `atom_manifest_phases.go`, never served (no substep mapping, not read by step-entry path) | grep confirms: only test fixtures read these | [atom_manifest_phases.go:27-44](../../internal/workflow/atom_manifest_phases.go#L27) |

### Summary — cutover is partial

- **19 / 19** atom-covered substep guides serve from `phases/`. Sub-step path is **fully cut over**.
- **0 / 6** step-entry guides serve from atoms. Step-entry path is **not cut over**.
- **~22 orphan atoms** exist for research/provision/finalize under `phases/` but are unreachable from any runtime code path.
- **recipe.md remains authoritative** for ≥ 6 step-entry surfaces (research / provision / generate / deploy / finalize / close) + all the topic `<block>` bodies those compositions pull in via `composeSkeleton`/`composeSection`.
- **Two runtime call sites** still load recipe.md: [recipe_guidance.go:103](../../internal/workflow/recipe_guidance.go#L103) and [recipe_topic_registry.go:450](../../internal/workflow/recipe_topic_registry.go#L450).
- **7 test files** depend on recipe.md being loaded (`recipe_mandatory_blocks_test.go`, `recipe_topic_registry_test.go`, `recipe_tool_use_policy_test.go`, `recipe_content_placement_test.go`, `recipe_close_framing_test.go`, `recipe_v8_104_test.go`, `recipe_section_catalog_test.go`).

### What remains to finish C-15

| # | Work | Blocking | Target |
|---|---|---|---|
| R1 | Verify Cx-5 closes F-17 (sub-agent paraphrase) at runtime | v38 commission | v38 verdict |
| R2 | Wire step-entry paths for Research / Provision / Finalize to serve from existing `phases/{research,provision,finalize}/entry.md` atoms (atoms already exist but are orphaned) | R1 | post-v38 |
| R3 | Migrate Generate / Deploy / Close step-entry composition from recipe.md `*-skeleton` sections + `composeSkeleton`/`composeSection` to atom-based composition; wire through existing `phases/{generate,deploy,close}/entry.md` atoms | R1 | post-v38 |
| R4 | Migrate any recipe.md `<block>` content still referenced by `subStepToTopic` fallback into the relevant `phases/` atoms; delete the fallback branch at [recipe_guidance.go:529-545](../../internal/workflow/recipe_guidance.go#L529) | R3 | post-v38 |
| R5 | Delete `recipe_topic_registry.go` — no callers remain once R4 lands | R4 | post-v38 |
| R6 | Delete `internal/content/workflows/recipe.md` + the `//go:embed workflows/*.md` directive that loads it | R5 | post-v38 |
| R7 | Migrate the 7 test files off `content.GetWorkflow("recipe")` — assert against atom tree instead | R6 | post-v38 |

### Why C-15 didn't land

Original rollout ([rollout-sequence.md](06-migration/rollout-sequence.md)) was C-0..C-15, cleanroom, 2026-04-21 target. What actually happened:

- **v8.105.0 (2026-04-21)** — C-7e..C-14 shipped. Atom tree landed, stitcher cut over **for sub-agent briefs only**. Main-agent path not touched.
- **v35 (PAUSE)** — writer_manifest_completeness + 6 engine defects. No progress on R2–R7.
- **v36 (analysis failure)** — analyst accepted artifact shape as evidence; forced I8 harness rewrite.
- **v37 (PAUSE)** — F-17 surfaced: main agent paraphrases atoms when composing Task() prompts. v8.109.0 atom fixes had zero runtime effect. No progress on R2–R7.
- **v38 (pending, v8.110.0)** — 8 Cx commits. Cx-5 (SUBAGENT-BRIEF-BUILDER) closes F-17 for sub-agent dispatch only. **Does not touch main-agent guide path or recipe.md deletion.**

Every cycle prioritized defect-class closure over the recipe.md cutover. The cutover is not obsolete. It's blocked behind "prove the runtime reaches the gate first."

---

## 3. Decision on C-15 (2026-04-22)

**C-15 is still the goal.** The original plan stands.

Post-v38 ordering, assuming v38 clears the gate:

1. **v38 proves Cx-5 closes F-17** (R1) → sub-agent dispatch boundary confirmed byte-identical at runtime.
2. **R2: un-orphan research/provision/finalize entry atoms** — these atoms already exist under `phases/`; add a case in `resolveRecipeGuidance` that prefers atom over recipe.md section for these three phases. Lowest-risk migration (no substep stitching, just one atom body per phase). Validates the entry-guide atom path end-to-end before tackling Generate/Deploy/Close.
3. **R3: migrate Generate/Deploy/Close step-entry composition** — the recipe.md `*-skeleton` sections + `composeSkeleton` + eager-topic injection are doing real work (plan-shape-aware block composition). Rewrite the composition layer to pull from atom tree. Entry atoms exist (`phases/generate/entry.md`, etc.) but the skeleton/topics-map machinery still references recipe.md.
4. **R4: delete `subStepToTopic` fallback** — the dead branch in `buildSubStepGuide`. All 19 substeps are atom-mapped; no substep should ever fall through. Safe to delete + lock with a lint.
5. **R5: delete `recipe_topic_registry.go`** — no runtime callers once R3 + R4 land.
6. **R6: delete `recipe.md`** — the headline deletion. Individual revertable commit.
7. **R7: clean up 7 test files** — migrate assertions from recipe.md blocks to atom-tree reads.

Estimated migration scope: recipe.md is 3,438 LoC; the orphan atoms already cover ~620 LoC of what needs to move (research + provision + finalize entries). The Generate/Deploy/Close step-entry bodies in recipe.md total ~2,000 LoC of skeleton + blocks that need to land as atoms or be composed from existing atoms.

If v38 regresses, R1 is re-closing F-17 via another Cx stack; R2–R7 push another cycle.

---

## 4. Run log (append-only; analyzers edit here)

Each run analyzer appends one entry after writing `runs/vN/verdict.md`. Entry format: run ID, tag, verdict, plan delta. Do **not** rewrite past entries — if a past entry was wrong, add a correction entry dated now.

### v38 — pending (target: v8.110.0)

*To be filled by the v38 analyzer. Required fields: commission date, verdict (PROCEED / ACCEPT-WITH-FOLLOW-UP / PAUSE / ROLLBACK-Cx), plan delta (if any — does this run change the R1–R7 ordering?).*

### v37 — PAUSE (2026-04-21, v8.109.0)

- **Outcome**: close-complete; deliverable structurally broken.
- **Root cause**: F-17 — main agent paraphrases atoms at dispatch. 4 of 6 source-HEAD Cx fixes had zero runtime effect.
- **Plan delta**: exposed that the sub-agent dispatch boundary needed engine ownership (Cx-5); did not change R2–R7.
- **Artifacts**: [runs/v37/verdict.md](runs/v37/verdict.md), [runs/v37/verification-checklist.md](runs/v37/verification-checklist.md).

### v36 — analyst rewrite (2026-04-21)

- **Outcome**: original analyst accepted artifact structure as proxy for fix correctness; CORRECTIONS.md filed; harness spec written to prevent recurrence.
- **Plan delta**: added analysis-harness prerequisite to every subsequent run. No change to R1–R7 work list.
- **Artifacts**: [runs/v36/CORRECTIONS.md](runs/v36/CORRECTIONS.md), [spec-recipe-analysis-harness.md](spec-recipe-analysis-harness.md).

### v35 — PAUSE (pre-v8.109)

- **Outcome**: stuck on `writer_manifest_completeness`; 6 engine defects pre-rollout.
- **Plan delta**: forced PAUSE-not-ROLLBACK; introduced Cx fix-stack pattern. No change to R1–R7 work list.
- **Artifacts**: [HANDOFF-to-I6.md](HANDOFF-to-I6.md).

### v8.105.0 — briefs/ cutover shipped (2026-04-21)

- **Outcome**: C-7e..C-14 landed. Sub-agent brief stitching cut over to atoms. Main-agent guide path untouched.
- **Plan delta**: split C-15 into "sub-agent path done" (✅) and "main-agent path + recipe.md deletion" (deferred — what became R2–R7).

---

## 5. How analyzers update this doc

After writing `runs/vN/verdict.md`, before committing:

1. **Append a run-log entry** to §4 with: run ID, tag, verdict, plan delta (one paragraph).
2. **If the run changed the R1–R7 work list** — add / reorder / cross-out items in §2 "What remains". Never delete a row; strike through with `~~...~~` + add a correction row citing the run.
3. **If the run invalidated a §1 invariant** — do **not** edit §1 silently. Add a §6 "Proposed amendments" entry citing the run's evidence; let the user decide whether the invariant survives.
4. **Update `Last updated` at the top** to the run's verdict date.
5. **If the run proved C-15 unblocked** — set the R6 row "Target" column to the concrete commit date, link the migration PR, and flag in the run-log entry.

Do not edit §3 "Decision on C-15" unless a run produces evidence that deleting recipe.md is no longer desirable. That's a user-level decision, not an analyst one.

---

## 6. Proposed amendments (analyst-surfaced, user-resolved)

*Empty. Analysts append proposed changes to §1 invariants here; user resolves.*

---

## 7. Superseded framings (for audit)

- **"C-15 is effectively obsolete" (stated 2026-04-22 in session, retracted same day)** — incorrect. recipe.md is still the runtime source for main-agent guides via `recipe_topic_registry.go:450`. C-15 is deferred, not retired. See §3.
- **"`phases/` is skeleton only, not wired" (stated 2026-04-22 in session, retracted same day)** — incorrect. Triple-verification in §2 confirms `phases/` is wired for all 19 substep guides via [atomIDForSubStep](../../internal/workflow/recipe_guidance.go#L554) + [buildSubStepGuide:506-527](../../internal/workflow/recipe_guidance.go#L506). What's unwired is the step-entry layer (6 phases) — not the same thing. Additionally, `phases/research/`, `phases/provision/`, `phases/finalize/` contain atoms that ARE registered in the manifest but have no runtime caller (orphaned, not skeleton). See §2 "Per-surface cutover status".
