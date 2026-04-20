# RUNBOOK — executing the zcprecipator2 research

You are a fresh Opus instance about to run a multi-session research project to redesign the recipe workflow system from scratch. The plan is already written. You are **executing**, not designing.

---

## 1. What you're doing

Produce structured markdown artifacts under `docs/zcprecipator2/` that answer the six-step research protocol in [`README.md`](README.md). No code changes. No edits to `internal/content/workflows/recipe.md` or any Go source. Research mode only.

Every artifact goes under the directory structure defined in [`README.md` §4](README.md). If you hit something the plan doesn't cover — a genuinely novel decision, not just an ambiguous wording — pause and ask the user. Don't improvise scope.

---

## 2. Required reading before you start (in this order)

1. **[`README.md`](README.md)** — the full 12-section research plan. Read end-to-end. This is the spec you're executing.
2. **[`../recipe-version-log.md`](../recipe-version-log.md)** — 34-version trajectory ground truth. Skim the top (rating methodology, cross-version summary, architectural insights). Read v25 through v34 entries in detail. These contain the defect classes you're closing.
3. **[`../../CLAUDE.md`](../../CLAUDE.md)** — project conventions, TDD rules, directory map.
4. **[`../../internal/content/workflows/recipe.md`](../../internal/content/workflows/recipe.md)** — the current 3,438-line monolith. You'll reference specific block names during step 2; you don't need to memorize it up front.

Expect ~2 hours of reading before you produce your first artifact. This is correct. The plan's cost estimates assume the reading is done.

---

## 3. Operating constraints

- **Research mode, no code changes.** If a step's output would require editing Go or recipe.md, you've misread the step.
- **Every artifact is markdown under `docs/zcprecipator2/`.** Follow the directory structure in `README.md` §4 exactly.
- **Cite evidence with file:line or trace timestamp.** No cell in any matrix reads "probably" or "I think." Find the evidence or mark the cell as gap.
- **Sequential by default.** Steps 1 → 2 → 3 run in order. Steps 4 and 5 can parallelize after step 3. Step 6 is last. Do not start step N+1 until step N's success criteria are met.
- **Pause between major steps and ask the user.** Each step produces a coherent artifact set; the user will review before you proceed to the next step. Do not chain steps silently.
- **Preserve tier coverage.** Every step produces minimal-tier AND showcase-tier artifacts. If you produce only showcase output, the step is incomplete.

---

## 4. Open decisions — user answers needed BEFORE step 1

These are flagged in `README.md` §8. The user must answer before you start step 1 (except #6, which can be decided later). Ask explicitly if they haven't already:

1. **Minimal-tier input**: commission a fresh minimal run for SESSIONS_LOGS capture, OR reconstruct from code + showcase-as-reference? Default recommendation: commission new run.
2. **Session-log reading granularity**: split per sub-agent, OR one artifact per run? Default recommendation: split.
3. **TodoWrite disposition**: check-off-only, OR drop entirely? Default recommendation: check-off-only.
4. **Migration shape**: parallel-run vs cleanroom? Deferred to step 6 — don't ask now.
5. **Check deletion threshold**: conservative (only when provably redundant) vs aggressive? Default recommendation: conservative.
6. **Step parallelization**: sequential 1→2→3, then 4+5 parallel, then 6? Default recommendation: yes.

If the user has already answered these in the conversation that kicked this off, use those answers. If not, ask before producing any artifact.

---

## 5. Current state

**What's done** (as of 2026-04-20):
- v34 of `nestjs-showcase` ran and is logged in `recipe-version-log.md`
- The research plan is written at `docs/zcprecipator2/README.md`
- This runbook exists
- **Nothing else.** No step-1 artifacts yet.

**What to do next**: read the required materials (§2), confirm the open decisions (§4) with the user, then begin step 1.

---

## 6. Execution shape per step

### Step 1 — Flow reconstruction

For each sub-task (showcase main / showcase each sub-agent / minimal main / minimal each sub-agent):

1. Read the session log end-to-end (or use `eval/scripts/timeline.py` for a skim; use raw JSONL for detail)
2. For each tool use, extract the columns named in `README.md` §3 step 1 ("Activity" table)
3. Flag every (error, mis-ordered guidance, `scope=downstream` fact, TodoWrite full-rewrite, Agent dispatch)
4. For every Agent dispatch, save the transmitted `prompt` parameter verbatim into `flow-showcase-v34-dispatches/<agent-role>.md` (one file per dispatch)
5. Write the trace to `docs/zcprecipator2/01-flow/flow-<tier>-<ref>-<source>.md`

Produce `flow-comparison.md` last, after every individual trace is written.

**Session log paths**:
- Showcase: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v34/SESSIONS_LOGS/`
- Minimal: location TBD per open decision #1

### Step 2 — Knowledge inventory

Walk every substep in each tier's flow from step 1. For each cell in the matrix:

1. Identify the knowledge source delivering that cell
2. Cite file:line (recipe.md block) or trace timestamp (step 1 artifact)
3. Measure the delivery size (bytes or lines)
4. Check for redundancy: is this fact delivered via other paths at the same phase?
5. Check for gap: is the delivery present when the agent actually needs it?

Produce the matrix first (both tiers), then derive the three maps.

### Step 3 — Architecture design

Start with the stake-in-the-ground principles in `README.md` §5. For each:

1. Pressure-test against step 1+2 evidence. Does it hold? Does it close the defect class it claims to?
2. Either keep, refine, or cut. Add new principles if step 1+2 surface defect classes none of the existing principles close.

Then atomize the layout. Every atom ≤ 300 lines. Every atom has one audience (main agent OR one sub-agent role, never both). No atom has version anchors.

Then sketch data flow diagrams (mermaid or ASCII) per phase per tier. These are the spec for Go-layer stitching.

Then walk every current check. Disposition: keep / rewrite-to-runnable / delete. Every disposition has a one-sentence rationale.

### Step 4 — Context verification

For each (sub-agent role × tier) in §3's new architecture:

1. Compose the brief the new system would transmit (concatenate atoms, interpolate placeholders)
2. Simulate cold-read: document every ambiguity, contradiction, or impossible-to-act-on instruction
3. Diff against the v34 captured dispatch (from step 1). For every removed line, state: scar / noise / dispatcher / load-bearing-moved-where. For every added line, state: which defect class it closes.
4. Defect-class coverage table: every v20-v34 closed class has a prevention mechanism in the composed brief or in the new check suite or in Go-injected runtime data.

### Step 5 — Regression fixture

Walk `recipe-version-log.md` end-to-end. For every defect class with a ✅/❌ verdict or a named fix (v8.XX), produce a registry row with the fields defined in `README.md` §3 step 5.

Then aggregate into v35 calibration bars — all numeric/grep-verifiable.

### Step 6 — Migration path

After steps 1-5 are done, write the migration proposal. Compare parallel-run vs cleanroom based on concrete delta evidence from step 3 (how much Go code changes, whether old and new can coexist, rollback cost).

---

## 7. Failure modes to watch for

1. **Losing tier coverage.** You produce a great showcase artifact and forget minimal. Check at every step: did I produce minimal output too?
2. **Scope creep into implementation.** You start sketching Go function signatures. Stop. That's after the plan. The plan only produces markdown.
3. **Proposing a principle without data backing.** Every principle in step 3 must trace to a specific v20-v34 defect class. If you can't cite one, the principle is speculation.
4. **Taking the current system at face value.** The whole point is to distinguish load-bearing from scar tissue. If a piece of current guidance has no defect-class provenance, flag it as scar candidate.
5. **Chaining steps silently.** Step N's artifacts need user review before step N+1 begins. Produce the artifacts, pause, ask.
6. **Re-deriving what's already done.** `recipe-version-log.md` has ~60 pages of analysis. The v34 entry in it has already named most defect classes. Don't recompute — cite.
7. **Writing a 2000-line matrix with no redundancy/gap insights.** The matrix is the input; the insight is in the redundancy/gap/misroute maps. If the maps are thin, the research didn't land.

---

## 8. Resume state

This runbook lives at `docs/zcprecipator2/RUNBOOK.md`. When the user returns between sessions, they'll point the fresh instance at this file. Keep `docs/zcprecipator2/RESUME.md` updated after every step with:

- Which steps are complete
- Which artifacts exist
- What the next action is
- Any user decisions since last session

First instance: create `RESUME.md` after finishing the required reading, before starting step 1.

---

## 9. One-screen quick reference

- Plan: [`README.md`](README.md)
- Prior trajectory: [`../recipe-version-log.md`](../recipe-version-log.md)
- Current system: [`../../internal/content/workflows/recipe.md`](../../internal/content/workflows/recipe.md)
- Session logs: `/Users/fxck/www/zcprecipator/nestjs-showcase/nestjs-showcase-v34/SESSIONS_LOGS/`
- v34 entry in the log: sections at the bottom of `../recipe-version-log.md` (search for `### v34`)
- Output root: `docs/zcprecipator2/0N-*/` per the plan's §4
