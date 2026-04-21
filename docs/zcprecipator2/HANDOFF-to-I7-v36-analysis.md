# HANDOFF-to-I7-v36-analysis.md — post-v36 run analysis + verdict

**For**: fresh Claude Code instance picking up zcprecipator2 after the v36 showcase run has been commissioned and closed (or force-exported). Your job is **analysis**, not implementation. Produce structured artifacts under `docs/zcprecipator2/runs/v36/` and a verdict that arbitrates v8.108.0 → v37 commission vs. PAUSE vs. ROLLBACK.

**Commission-side vs. analysis-side**: the user commissions the run and hands you the artifacts. You do not drive the run itself. If you find yourself tempted to fix the agent mid-run or re-run, stop and hand control back to the user.

**Branch**: `main`. **Tag under test**: `v8.108.0`. **Comparison baseline**: `v8.105.0` (v35) — the v35 analysis is your calibration reference.

---

## Slots to fill at start (the user hands these to you)

Copy this block into the first heading of your first artifact and populate before doing anything else. If any slot is `<unknown>`, stop and ask the user — don't proceed on guesses.

```
RUN_REF:            v36
SESSION_ID:         <fill-in from session JSONL filename or zerops_workflow action=status response>
CLOSE_DATE:         <fill-in UTC>
TIER:               <showcase | minimal>
SLUG:               <fill-in, e.g. nestjs-showcase / laravel-showcase / bun-hello-world>
RUN_OUTCOME:        <close-complete | force-exported | session-abandoned>
DELIVERABLE_TREE:   <fill-in, e.g. /Users/fxck/www/zcprecipator/{slug}/{slug}-v36/>
SESSIONS_LOGS:      <DELIVERABLE_TREE>/SESSIONS_LOGS/
MAIN_JSONL:         <SESSIONS_LOGS>/main-session.jsonl
TIMELINE_MD:        <DELIVERABLE_TREE>/TIMELINE.md  (user-authored qualitative read)
COMMISSIONED_BY:    user
AGENT_MODEL:        <fill-in, e.g. claude-opus-4-7[1m]>
```

---

## The story v36 was commissioned to tell

v35 stalled at `writer_manifest_completeness` through 11 check-rounds. Analysis identified six defects (F-1..F-6) all pre-rollout. v8.108.0 closed five with the Cx-commit stack:

| Commit | Defect | Mechanism |
|---|---|---|
| `2a60ee0` Cx-CHECK-WIRE-NOTATION | F-2 | Check `Detail` strings now name wire contracts by JSON key (`fact_title`), not Go struct.field (`FactRecord.Title`) |
| `0bc7ea1` Cx-ITERATE-GUARD | F-3 | `action=iterate` sets `RecipeState.AwaitingEvidenceAfterIterate=true`; substep completes rejected with `MISSING_EVIDENCE` until `zerops_record_fact` clears the gate |
| `6c3320f` Cx-BRIEF-OVERFLOW | F-1 | Readmes-substep guide emits an envelope listing atom IDs when composed brief > 28 KB; main agent retrieves each atom via new `zerops_workflow action=dispatch-brief-atom` |
| `a0c2069` Cx-GUIDANCE-TOPIC-REGISTRY | F-5 | Unknown topics return top-3 Levenshtein matches; predicate-match-empty surfaces `TOPIC_EMPTY`; `RecipeResponse.GuidanceTopicIDs` lists the closed universe on start |
| `3fce7c7` Cx-KNOWLEDGE-INDEX-MANIFEST | F-6 | Five wire-contract atoms (manifest-contract, routing-matrix, classification-taxonomy, content-surface-contracts, citation-map) routed via explicit keyword synonyms at `synonymBoostScore=100` |

F-4 (skip-attempt on mandatory step) stayed open as telemetry-only; engine refusal was already correct pre-rollout.

**Your headline question**: did the fix stack close F-1..F-6 under live conditions, and did it avoid introducing new defect classes? The answer drives PROCEED vs PAUSE vs ROLLBACK-Cx vs ROLLBACK.

---

## Required reading (in order, ~45 min)

Absorb these before producing your first artifact. The v35 analysis is your calibration reference — every v36 observation should be framed as "same as v35", "better", "worse", or "new class".

1. **[`runs/v35/verdict.md`](runs/v35/verdict.md)** — the PAUSE decision + the three measurement-definition tightenings (T-1, T-8, T-9) applied below. Tells you what the bars for v36 actually are, not what they were.
2. **[`runs/v35/analysis.md`](runs/v35/analysis.md)** — six-defect narrative + evidence timestamps. This is what "not-regressed" looks like; your v36 analysis mirrors this shape.
3. **[`runs/v35/README.md`](runs/v35/README.md)** — artifact-index template. Your `runs/v36/README.md` follows the same schema.
4. **[`HANDOFF-to-I6.md`](HANDOFF-to-I6.md)** — fix-stack spec. Per-Cx design + acceptance criteria + invariants the fix stack must preserve. Cross-reference when you evaluate whether each fix actually fired in v36.
5. **[`implementation-notes.md`](implementation-notes.md) §Cx-CHECK-WIRE-NOTATION..§Cx-KNOWLEDGE-INDEX-MANIFEST** (the bottom five sections) — what actually shipped, including which alternatives were rejected. Needed when you decide whether an observed v36 behavior is a fix-stack bug or expected.
6. **[`05-regression/defect-class-registry.md §16.1–16.6`](05-regression/defect-class-registry.md)** — structured defect rows. Each has a `test_scenario` + `calibration_bar` you grade v36 against.
7. **[`runs/v35/rollback-criteria.md`](runs/v35/rollback-criteria.md)** — T-1..T-12 decision matrix with v35 tightenings applied. `runs/v36/rollback-criteria.md` starts as a copy.
8. **[`runs/v35/calibration-bars.md`](runs/v35/calibration-bars.md)** — 108-bar measurement sheet. Sections §3–§8 + §9 B-9..B-14 + §11a editorial-review were **unmeasurable on v35** because the run never reached finalize/close. v36's first job is to measure them.
9. **[`DISPATCH.md`](DISPATCH.md) §1-§8** — §8 (envelope delivery) is the Cx-BRIEF-OVERFLOW dispatcher contract. Needed when you judge whether the main agent stitched the writer brief correctly on v36.
10. **[`../../CLAUDE.md`](../../CLAUDE.md)** — auto-loaded; re-read if you drift on TDD / operating norms.

Raw v36 source artifacts (slot-fill from the header):
- **Main session JSONL**: `<MAIN_JSONL>` (typically 2-6 MB; 200-1200 events)
- **Subagent streams**: `<SESSIONS_LOGS>/subagents/*`
- **TIMELINE.md**: `<TIMELINE_MD>` — user-authored qualitative read, captures AFK gaps + live observations
- **Deliverable tree**: `<DELIVERABLE_TREE>` — every file written, including `ZCP_CONTENT_MANIFEST.json` + per-codebase README.md + zerops.yaml + environments/*.yaml

---

## Artifact production checklist (in order)

Produce each artifact in full before moving to the next. **Stop after the verdict** — do not chain into the next migration or implementation step. The user reviews the verdict and decides.

### Step 1 — Extract flow traces

Run the flow extractor against the v36 tree. You will author `role_map.json` if subagent roles differ from v35's (scaffold-{api,app,worker}, feature, writer-{1..N}, code-review, editorial-review).

```bash
cd /Users/fxck/www/zcp
python3 docs/zcprecipator2/scripts/extract_flow.py \
  <DELIVERABLE_TREE> \
  --tier <TIER> --ref v36 \
  --role-map docs/zcprecipator2/runs/v36/role_map.json \
  --out-dir docs/zcprecipator2/runs/v36
```

Expected output: `flow-main.md` + `sub-<role>.md` per subagent + `flow-dispatches/<role>.md` per Agent-tool invocation.

Author `role_map.json` by inspecting `<SESSIONS_LOGS>/subagents/` — each subagent directory has a prefix (first 3 chars of the agent ID) → the role slug you assign it. Template below in §"runs/v36/role_map.json template".

### Step 2 — Index + flag

Produce **`runs/v36/README.md`** following the v35 schema: slug + session ID + tier + TL;DR + defect-closure summary table + file index + "where else to look" pointers. Keep TL;DR to ≤12 lines.

Per-defect closure table columns (one row per F-1..F-6):

| # | Defect | v35 behavior | v36 observation | Closed on v36? |

**"Closed on v36"** means: under the same triggering conditions that surfaced the defect on v35, v36 did not reproduce it. Grade each one of PASS / REGRESSED-CLOSURE / NEW-CLASS / UNREACHED (run didn't get far enough to exercise the fix). See §"Per-defect verification" below for the exact check each grade requires.

### Step 3 — Narrative analysis

Produce **`runs/v36/analysis.md`** mirroring the v35 shape: executive summary (≤20 lines), per-defect verification sections, secondary observations (labeled `S-1..S-N`), cross-check against HANDOFF-to-I5 invariants + HANDOFF-to-I6 invariants 7–11 (the Cx-invariants added), calibration-bar coverage gap list, appendix A (key timestamps), appendix B (dispatch prompt lengths + subagent wall times).

Every claim cites evidence as `flow-main.md row N (HH:MM:SS)` or `<file_path>:<line>` — no "probably" / "looks like" without a file:line pointer.

### Step 4 — Calibration snapshot

Copy `runs/v35/calibration-bars.md` → `runs/v36/calibration-bars.md`. Mark each bar measurable / unmeasurable for v36 based on how far the run reached. For measurable bars, fill in observed values. Add new bars if v36 surfaces a class the v35 sheet doesn't cover (e.g. a new F-class defect).

Do the same for `runs/v35/rollback-criteria.md` → `runs/v36/rollback-criteria.md`. Apply T-1/T-8/T-9 tightenings inline (already done in v35; preserve them). If v36 surfaces a new trigger class, add T-13 / etc. with measurement + threshold.

### Step 5 — Verdict

Produce **`runs/v36/verdict.md`** mirroring v35's shape: decision (PROCEED / ACCEPT-WITH-FOLLOW-UP / PAUSE / ROLLBACK-Cx / ROLLBACK) + rationale + defect-by-defect closure grades + new calibration bars if any + halted/not-halted lists + Appendix (decision-path audit trail).

See §"Decision tree" below for the class taxonomy + tie-breakers.

### Step 6 — Append defect-class-registry rows if needed

If v36 surfaces a new defect class (F-7 / F-8 / ...), add rows 16.7+ to [`05-regression/defect-class-registry.md`](05-regression/defect-class-registry.md) with the structured schema (origin_run / class / mechanism_closed_in / current_enforcement / new_enforcement / test_scenario / calibration_bar).

### Step 7 — Commit as a single `docs(zcprecipator2): v36 run analysis + verdict` commit

No code changes, no behavior changes. The commit is pure documentation. Follow the v35 commit pattern (`a41c715` + `6c3320f` for size reference).

**Stop here.** Do not proceed to HANDOFF-to-I8 or new Cx-commits without user review.

---

## Per-defect verification — what PASS looks like per F-1..F-6

For each defect, the evidence in v36 that proves closure. Grep the main session JSONL + deliverable tree for these signals.

### F-1 (brief-overflow, Cx-BRIEF-OVERFLOW)

**Closure-evidence**:
- At `complete substep=feature-sweep-stage` (or whichever substep precedes readmes), the response contains the envelope heading `"Dispatch brief (retrieve + stitch before transmitting)"` AND contains the string `"action=dispatch-brief-atom"`.
- Main agent subsequently emits ≥10 calls to `zerops_workflow action=dispatch-brief-atom atomId=...` in sequence (10 body + 5 principles = 15 atom fetches for the writer brief; actual count depends on plan).
- None of the `zerops_workflow` tool-result responses for the readmes context exceed 32 KB.
- Writer-1 dispatch prompt contains the actual brief body (fragment-markers, manifest-contract content) rather than a reference to a spillover file. Grep writer-1 dispatch for `tool-results/mcp-zerops` — absence = closure.

**Regression signals**:
- Any tool-result response > 32 KB (grep for spillover notice string "Output has been saved to").
- Writer dispatch prompt references a spillover file.
- Main agent fails to stitch atoms in the documented order (envelope-stitched brief diverges from `BuildWriterDispatchBrief` output — would be caught by `TestEnvelopeAtoms_StitchToFullBrief` at build time but a runtime divergence means the agent departed from the envelope instructions).

**Regressed-closure vs new-class**: if the envelope fires but the agent ignores it and paraphrases the brief, that's a NEW CLASS (agent-behavior defect, not engine defect). Name it F-7 and add a registry row.

### F-2 (check Detail Go notation, Cx-CHECK-WIRE-NOTATION)

**Closure-evidence**:
- Zero `zerops_workflow` responses contain `checkResult.checks[i].detail` text matching `\b(FactRecord|ContentManifest|ContentManifestFact|ManifestFact|StepCheck)\.[A-Z]\w+\b`. The lint-test enforces this at build time; v36 runtime evidence = no observed Go-notation in any check detail.
- Specifically: if `writer_manifest_completeness` fails on v36, the detail string names `fact_title` in backticks, not `FactRecord.Title`.

**Regression signals**:
- Any check detail with Go struct.field dot-notation. Would indicate the lint test missed a code path (likely in `internal/tools/workflow_checks_*.go` — extend `inspectFileForGoNotationInCheckDetails` coverage).

### F-3 (iterate fake-pass, Cx-ITERATE-GUARD)

**Closure-evidence**:
- If `action=iterate` is called: subsequent `complete substep=*` within the same iteration (before any `zerops_record_fact`) returns `MISSING_EVIDENCE` error.
- If agent hits the gate, it records a fact via `zerops_record_fact`, THEN retries the substep complete — which succeeds.
- If `action=iterate` is NOT called, F-3 is UNREACHED (the gate exists but was never triggered).

**Regression signals**:
- Agent calls iterate, then walks substep completes back-to-back without an intervening `zerops_record_fact` and the engine accepts them. That's F-3 regressing.
- Agent calls iterate, hits the gate, then attempts `action=skip` or force-exports instead of recording a fact — that's a NEW CLASS (gate too aggressive; agent has no escape path). Name it F-8, add a registry row, and recommend the gate be loosened OR the error message be reshaped to be more actionable.

### F-4 (skip on mandatory step, telemetry only)

**Closure-evidence**: zero `zerops_workflow action=skip step=deploy|generate|provision|research` calls in the session log. F-4 is not a code defect; any skip-on-mandatory attempt is diagnostic of F-1/F-2/F-3 pressure upstream.

**Regression signals**: `> 0` skip-attempts on mandatory steps. Record as a retry-exhaustion signal and trace back to the upstream defect that drove the agent to try skip.

### F-5 (unknown-topic hallucinations, Cx-GUIDANCE-TOPIC-REGISTRY)

**Closure-evidence**:
- `zerops_workflow action=start workflow=recipe` response includes `guidanceTopicIds` with the full topic ID list.
- Zero `zerops_guidance` calls return bare `"unknown guidance topic"` text. If any unknown topic is queried, the response contains `"Did you mean:"` + 3 topic IDs.
- Zero zero-byte `zerops_guidance` responses. Zero responses with `TOPIC_EMPTY` code (that would indicate a registry drift — server bug, not agent bug).

**Regression signals**:
- `result_size=0` from `zerops_guidance` — that's F-5 still open.
- `TOPIC_EMPTY` error surfacing — that's a NEW CLASS (registry drift since v8.108.0 merge; root-cause the atom tree rename or block deletion that caused it).

### F-6 (knowledge engine misses manifest atoms, Cx-KNOWLEDGE-INDEX-MANIFEST)

**Closure-evidence**:
- If the agent queries `zerops_knowledge` with any of: `ZCP_CONTENT_MANIFEST.json schema`, `writer_manifest_completeness`, `fact_title format`, `manifest routing`, `routing matrix`, `classification taxonomy`, `fragment markers`, `citation map` — the top-3 hits include the target atom URI (`zerops://recipe-atom/briefs.writer.<atom-name>`) and the snippet contains `action=dispatch-brief-atom`.
- Agent successfully uses the retrieval pointer: following the `zerops_knowledge` hit, agent emits `zerops_workflow action=dispatch-brief-atom atomId=<matched>` and retrieves the full body.

**Regression signals**:
- `zerops_knowledge` query for wire-contract terms returns unrelated hits (`decisions/choose-queue` etc.) — synonym index didn't fire. Check `internal/knowledge/wire_contract_synonyms.go`; the keyword list may need extending for v36's query phrasing.
- If agent never queries `zerops_knowledge` at all, F-6 is UNREACHED.

---

## Decision tree

Arbitration runs in strictest-outcome order: **ROLLBACK > ROLLBACK-Cx > PAUSE > ACCEPT-WITH-FOLLOW-UP > PROCEED**. Land the strictest applicable class.

### ROLLBACK (revert C-7e..C-14 rollout)

Fires only if v36 exposes a regression that sits in C-7e..C-14 (the original rollout) and was masked by v35 not reaching the affected phase. Almost certainly won't fire — C-7e..C-14 held under v35 AS MEASURED and the Cx-commits didn't touch those surfaces.

**Triggers** (any one):
- T-1 tightened: `checkResult.passed==false` count on `complete step=deploy` > 2 **and** the root cause is in the rollout commits (not the fix stack).
- T-3: substrate invariant broken (O-3/O-6/O-7/O-8/O-9/O-14/O-15/O-17) AND not caused by a Cx-commit.
- T-4: cross-scaffold env-var coordination regression (CS-1 non-zero OR CS-2 > 0 events) AND SymbolContract-related (C-1 commit surface).

Executes the rollback procedure in `runs/v35/rollback-criteria.md §4`.

### ROLLBACK-Cx (revert the offending Cx-commit(s))

**This is a new class v35 didn't have, added by this handoff.** Fires when a Cx-commit introduced a regression.

**Triggers** (any one):
- **Cx-ITERATE-GUARD over-aggressive**: agent hits `MISSING_EVIDENCE` on a legitimate work path (e.g., called `iterate` after a checker-exception refactor, then couldn't clear the gate because the iteration didn't actually need new evidence). Revert `0bc7ea1`; keep the other four Cx-commits.
- **Cx-BRIEF-OVERFLOW stitch divergence**: envelope fires but the agent-stitched brief isn't byte-identical to `BuildWriterDispatchBrief` output, causing sub-agent wire-contract mismatch. Revert `6c3320f`; inline-brief path regresses but sub-agent correctness is preserved.
- **Cx-CHECK-WIRE-NOTATION wording regressed check clarity**: agent fix loop got SLOWER on a check class than v35 because the JSON-key-named detail is less clear than the original. Unlikely but possible; revert `2a60ee0`.
- **Cx-GUIDANCE-TOPIC-REGISTRY nearest-match misleads**: Levenshtein suggestions pointed the agent at wrong topics, causing more round-trips than a bare "unknown" would have. Unlikely; revert `a0c2069`.
- **Cx-KNOWLEDGE-INDEX-MANIFEST synonym over-triggering**: agent's legit knowledge queries get polluted by wire-contract hits that aren't relevant. Unlikely; revert `3fce7c7` or extend the no-match-passthrough test.

**Procedure**:
1. Identify the offending Cx-commit from the defect evidence.
2. `git revert <sha>` — preserves git history (per CLAUDE.md discipline).
3. Re-run `go test ./... -count=1` + `make lint-local` green.
4. Commit revert with message referencing v36 evidence.
5. Tag as v8.108.1 (patch bump) per `make release-patch`.
6. Recommend user commission v37 with the reverted state before further Cx work.

### PAUSE (hold v37, fix stack extension needed)

Fires when v36 surfaces a new defect class (F-7+) that wasn't in F-1..F-6 and sits outside the rollout commits. Analogous to v35's PAUSE but scoped narrower because fewer surfaces are untested.

**Triggers** (any one):
- New F-N defect class identified with evidence (add registry row at 16.7+).
- T-5 fires (manifest-honesty regression) AND the agent reached the manifest-honesty check (v35 couldn't, v36 should).
- T-11 or T-12 fires (editorial-review wrong-surface or reclassification-delta). Editorial-review didn't exercise on v35; v36 PAUSE on either firing.

**Procedure**:
1. Produce a new HANDOFF-to-I8-v36-PAUSE-fix-stack.md that names the new defect class, proposes Cx-commits to close it, and documents the gate for v37.
2. Do not commission v37 until the new fix stack lands and a subsequent v37 PROCEED verdict fires.

### ACCEPT-WITH-FOLLOW-UP

Fires when gates pass but some signal warrants a targeted patch before v37.

**Triggers** (any one):
- F-1..F-6 all closed + F-1 Wall clock (tightened T-9) between 90 and 120 min (showcase) — active-work in gate but close to the ceiling.
- Code-review WRONG count = 3 (v34 baseline held, no improvement).
- IG item standalone borderline on one or two facts.
- Close-step reached but with non-structural churn (e.g. 2 rounds of close-complete instead of 1).

**Procedure**: land a targeted chore commit for the follow-up (no new feature surface) + proceed to v37 commission.

### PROCEED (v37 commissioned as second-confirmation)

All 6 defects closed or UNREACHED (for UNREACHED-but-fix-shipped, the build-time tests stand in for runtime evidence). No new F-classes. T-1..T-12 all clean. Finalize + close reached; editorial-review exercised without CRITs surviving.

**Procedure**: commission v37 as a second-confirmation run per `runs/v35/rollback-criteria.md §7`. Update implementation-notes.md with the PROCEED verdict reference.

---

## Calibration-bar evaluation — what's measurable on v36 vs v35

v35 was unable to measure sections §3–§8 + §9 B-9..B-14 + §11a editorial-review because the run stalled at deploy. v36 should reach further. Evaluate coverage by **run phase**:

| Phase | Reached? | Bars measurable |
|---|---|---|
| research + provision | always | §1 substrate, §2 plan-shape, §10 tier |
| generate | if research passes | §1-§2 + scaffold integrity + symbol-contract-consumption |
| deploy substeps 1-11 | if generate passes | §1-§2 + §4 runtime-integrity + §9 dispatch-integrity B-1..B-8 |
| deploy.readmes (writer) | if deploy 1-11 passes | adds §5 writer-brief-integrity (Cx-BRIEF-OVERFLOW) + §9 B-9 (new) |
| finalize | if deploy passes | adds §6 finalize-integrity + T-2 |
| close (editorial-review + code-review + browser-walk) | if finalize passes | adds §7-§8 + §11a editorial-review + T-11, T-12 |
| post-close export | if close passes | adds §6 phantom-tree + manifest-honesty cross-check |

If v36 reaches close, **every bar graduates from unmeasurable to measurable**. That's the first time the post-v8.105 zcprecipator2 architecture gets a full-surface reading. Document each bar's observed value in `runs/v36/calibration-bars.md` — the sheet graduates from "v35-snapshot with unmeasurables" to "v36-snapshot with observed values".

If v36 stalls before close (same class or different), name the earliest unreachable bar + the last reached phase in the verdict.

---

## T-trigger evaluation (tightened)

Apply the tightened T-1/T-8/T-9 measurements from `runs/v35/verdict.md §3`. Evaluator scripts for T-8 and T-9 still pending in `scripts/extract_calibration_evidence.py` (HANDOFF-to-I5 Front A). If they're still stubs when you analyze v36:

- **T-1/T-2**: parse `<MAIN_JSONL>` with `jq` / `python3`: count responses where `json.loads(text).checkResult.passed == false` on `action=complete step=deploy|finalize`.
- **T-8**: parse tool_use/tool_result pairs for `Bash`, compute per-call duration, count calls ≥ 120 s. Document at least a manual scan if the script isn't ready.
- **T-9 AFK-adjusted**: sum tool_use → tool_result durations; exclude gaps > 300 s between consecutive tool events. Report both raw wall and adjusted active-work.

**New trigger class v36 may surface**: T-13 **Cx-commit runtime regression** — any Cx-commit firing a gate in a way that blocks legitimate work. Evidence shape: `MISSING_EVIDENCE`/`TOPIC_EMPTY`/envelope-stitch failure AND the agent had no clean path to resolve. If you add T-13 to `runs/v36/rollback-criteria.md`, flow it into the decision matrix above under ROLLBACK-Cx.

---

## What v36 validates that v35 couldn't

v35 told us the **fix-stack needs to exist**. v36 tells us the **fix-stack and rollout together work**. Specifically:

| Question v35 couldn't answer | v36 evidence that answers it |
|---|---|
| Does C-7.5 editorial-review role work end-to-end under live conditions? | Editorial-review substep reaches `complete`; reclassification_delta + CRIT_count populated in `checkResult`; inline-fix applied between editorial-review and code-review. |
| Does the §16a dispatch-runnable contract hold for editorial-review? | Dispatch-runnable pre-attest shell runs + returns 0 before the agent attests. |
| Does writer_manifest_honesty close v34's wrong-surface-gotcha class across all 6 dimensions? | All 6 honesty dimensions pass on close; routing-matrix check clean. |
| Does C-10 `StepCheck` shrunk payload leak any unnecessary fields? | `checkResult.checks[i]` JSON shape matches `{name, detail, preAttestCmd, expectedExit}` — no legacy fields. |
| Does C-11 `NextSteps=[]` on close completion hold? | Close-completion response has `nextSteps=[]` (empty); post-completion guidance in `PostCompletionSummary` + `PostCompletionNextSteps` only. |
| Does the envelope-pattern stitch correctly in practice? | Writer-1 dispatch prompt is byte-identical to what `BuildWriterDispatchBrief(plan, factsLogPath)` would emit. Compare programmatically: extract the prompt from `sub-writer-1.md`, compare against `BuildWriterDispatchBrief` output for the plan shape. |
| Does the minimal-tier Path A main-inline writer work? | If `<TIER>=minimal`, readmes substep runs inline (no subagent dispatch). Validate no envelope emitted + main agent writes README inline. If `<TIER>=showcase`, mark as UNREACHED-minimal and recommend v35.5 minimal commission next. |

Any of these returning "not as expected" is a PAUSE-class observation.

---

## runs/v36/role_map.json template

The flow extractor needs a mapping from subagent ID prefix (first 3 chars of `sessionId` field in the subagent stream) to role slug. Start with the v35 set and update after a manual scan of `<SESSIONS_LOGS>/subagents/`:

```json
{
  "<prefix1>": "scaffold-apidev",
  "<prefix2>": "scaffold-appdev",
  "<prefix3>": "scaffold-workerdev",
  "<prefix4>": "feature",
  "<prefix5>": "writer-1",
  "<prefix6>": "writer-2-fix",
  "<prefix7>": "writer-3-fix",
  "<prefix8>": "code-review",
  "<prefix9>": "editorial-review"
}
```

Showcase tiers typically dispatch 3 scaffolds + 1 feature + 1–N writers + 1 code-review + 1 editorial-review = 7–N subagents. Minimal-tier dispatches only the app scaffold (or app+api if dual-runtime) + 1 code-review.

**Caveat**: if v36 reaches close with N=1 writer dispatch (fix stack succeeded), the trace will show only `writer-1` with no `-fix` variants. That's a headline PROCEED signal on its own — v35 needed 3 writer dispatches to get broken output; v36 should need 1 to get correct output.

---

## Operating rules

Unchanged from HANDOFF-to-I5 / HANDOFF-to-I6:

1. **Analysis, not implementation.** No code changes in this analysis window. If you find yourself itching to revert or patch, stop — the verdict directs whether/how to patch, then a separate fresh instance (with its own handoff) executes.
2. **Cite evidence file:line or timestamp.** No "probably". Trace the claim back to the session JSONL, a delivered file, or a Go source line.
3. **Tier coverage.** If v36 is showcase, v35.5 minimal is still pending. Call it out. If v36 is minimal, flag that showcase close-reach on v8.108.0 remains unvalidated.
4. **One commit at end.** The full analysis + verdict + registry-row additions land as a single commit like v35's `a41c715`. Don't stage partial work.
5. **Stop at verdict.** Do not start an I8 handoff or new Cx work. Hand control back to user after committing.

---

## Invariants that must still hold post-v36

If any of these break in v36, that's ROLLBACK-Cx (the fix-stack itself regressed something).

1. **121-atom tree lint-clean** (`make lint-local` green).
2. **Gate↔shim one-implementation**: both paths surface same text from same Go source.
3. **§16a dispatch-runnable contract** (editorial-review).
4. **Fix D ordering** (code-review needs editorial-review complete).
5. **`Build*DispatchBrief` pure composition** — envelope is delivery-layer only; `BuildWriterDispatchBrief` output byte-identical to envelope-stitched output.
6. **`StepCheck` post-C-10 shape** `{name, status, detail, preAttestCmd, expectedExit}`.
7. **Dispatch-brief delivery ≤ 32 KB per response** (B-9).
8. **Check `Detail` strings use JSON-key notation** (B-10 / Cx-CHECK-WIRE-NOTATION lint).
9. **`iterate` resets substep completion state** (Cx-ITERATE-GUARD).
10. **`zerops_guidance` unknown-topic responses carry top-3 matches; no zero-byte valid-topic responses** (Cx-GUIDANCE-TOPIC-REGISTRY).
11. **Wire-contract atoms recoverable via canonical keyword queries in `zerops_knowledge` top-3** (Cx-KNOWLEDGE-INDEX-MANIFEST).

---

## Starting action

1. **Baseline check**: `git log --oneline -5` should show 5 Cx-commits on top of `a41c715` v35-analysis + 2 chore + 1 merge. Latest tag is `v8.108.0`.
2. **Populate the slot block at the top of this document** with the v36 run's session ID, deliverable tree path, and tier/slug/outcome. If any slot is missing, stop and ask the user.
3. **Read the required-reading list in order**. Absorb ~45 min before touching artifacts.
4. **Extract flow traces** per Step 1. Author `role_map.json` from a scan of `<SESSIONS_LOGS>/subagents/`.
5. **Produce the five artifacts in order** (README, analysis, calibration-bars, rollback-criteria, verdict). Do not skip the copy-from-v35 step — v36's sheets are derivatives of v35's, not rewrites.
6. **Commit as a single `docs(zcprecipator2): v36 run analysis + verdict` commit**, following the shape of v35's `a41c715`.
7. **Stop and hand back to user.** Do not write HANDOFF-to-I8 autonomously. The user decides what comes next based on your verdict.

---

## If something is unclear, ask the user

- If v36 reached further than v35 but stopped before close, the decision is less clear — may be ACCEPT-WITH-FOLLOW-UP, may be PAUSE. Frame the observations and ask the user to arbitrate.
- If you disagree with a v35 analysis framing, say so before inheriting it. The v35 artifacts are authoritative as run-snapshots but can be amended if v36 reveals the framing was narrow.
- If the fix-stack-invariant test pass (at build time) diverges from runtime behavior (e.g. the envelope-stitch byte-identity test passes but the agent's actual stitched output differs), that's a subtle bug class — flag it specifically.
- If the run artifacts are missing or corrupted (main-session.jsonl truncated, no TIMELINE.md, force-export before writer dispatch), the analysis scope narrows. Document what's available + what isn't; don't guess.

Good luck. v36 is where the zcprecipator2 architecture earns its keep.
