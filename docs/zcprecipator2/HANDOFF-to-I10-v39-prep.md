# HANDOFF-to-I10-v39-prep.md — land v38-surfaced fix stack, commission v39, analyse

**For**: fresh Claude Code instance picking up zcprecipator2 after v38 analysis shipped as PAUSE. Your job is to **land the eleven-Cx fix stack** in [`plans/v39-fix-stack.md`](plans/v39-fix-stack.md), tag the release, hand back to the user for v39 commission, then **analyse v39** when artifacts land.

**Reading order** (≈ 90 min):

1. This document front-to-back.
2. [`runs/v38/verdict.md`](runs/v38/verdict.md) — the first-pass verdict. Direction (PAUSE) stands.
3. [`runs/v38/CORRECTIONS.md`](runs/v38/CORRECTIONS.md) — what v38's first analysis missed; the real root causes. **This is the doc that sets v39's scope.**
4. [`runs/v38/verification-checklist.md`](runs/v38/verification-checklist.md) — per-file evidence.
5. [`runs/v38/dispatch-integrity/`](runs/v38/dispatch-integrity/) — the engine vs dispatched brief byte-diffs for all 3 guarded roles. Editorial-review's 460-line unified diff is here.
6. [`plans/v39-fix-stack.md`](plans/v39-fix-stack.md) — the implementation plan. **THIS IS THE FILE YOU EXECUTE.**
7. [`../spec-content-surfaces.md`](../spec-content-surfaces.md) — the content-surface contracts that every emitter must honor. Cx-1 gold-test binds against this. §5 per-surface test cheatsheet is load-bearing.
8. [`spec-recipe-analysis-harness.md`](spec-recipe-analysis-harness.md) — analysis harness Tier 1/2/3 (unchanged from v38).
9. [`HANDOFF-to-I8-v37-prep.md`](HANDOFF-to-I8-v37-prep.md) §6 — analysis discipline rules. Inherited unchanged for v39 analysis.
10. [`../../CLAUDE.md`](../../CLAUDE.md) — auto-loaded; reread for TDD + operating norms.

**Branch**: `main`. **Previous tag**: `v8.112.0`. **Your target**: `v8.113.0` (or bumped) once the 11 Cx commits land.

---

## 1. Slots to fill at start

```
FIX_STACK_TAG:            <unfilled — bump from v8.112.0, target v8.113.0>
FIX_STACK_COMMITS:
                          Cx-1 GO-TEMPLATE-GROUND              <sha>  (F-GO-TEMPLATE; headline)
                          Cx-2 MANIFEST-EXPORT-EXTEND          <sha>  (F-23)
                          Cx-3 ROUTETO-RECORD-ENFORCE          <sha>  (enables Cx-6)
                          Cx-4 CLASSIFY-RUNTIME-ACTION         <sha>  (enables Cx-6)
                          Cx-5 WRITER-SELF-REVIEW-AS-STEP-GATE <sha>  (closes writer folk-doctrine + wrong-surface)
                          Cx-6 WRITER-BRIEF-SLIM               <sha>  (60KB → 25KB brief)
                          Cx-7 MAIN-AGENT-COMMENT-TEACHING     <sha>  (F-ENFORCEMENT-ASYMMETRY Phase 1)
                          Cx-8 ENV-COMMENT-SCAFFOLD-DECISION   <sha>  (prevents spec §11 class)
                          Cx-9 STRIPPING-VISIBILITY-WARN       <sha>  (surfaces the loop)
                          Cx-10 SURFACE-DOC-COMMENT-LINT       <sha>  (systemic prevention)
                          Cx-11 DISPATCH-GUARD-AUTO-ENFORCE    <sha>  (F-17-runtime close — hardest)
V38_RETRO_REPORT:         /tmp/v38-retro-after-v113.json
V38_RETRO_EXPECTED_DELTAS_CONFIRMED:
                          TestFinalizeOutput_PassesSurfaceContractTests  FAILS  (CRIT #1 "expanded toolchain" trips spec test)
                          readmes_no_folk_doctrine                       observed=1  ("benign zcli warning" caught retroactively)
                          zerops_yaml_comment_voice                      observed=0  (v38 comments are clean)
                          readmes_dispatch_integrity                     observed=1  (editorial-review SHA mismatch)
                          env_comment_scaffold_decision_placement        observed=0  (no fires expected on v38)
V39_COMMISSION_DATE:      <unfilled — user commissions>
V39_SESSION_ID:           <unfilled>
V39_OUTCOME:              <unfilled — post-run>
V39_VERDICT:              <unfilled — post-analysis>
```

Fill slots as phases complete.

---

## 2. The v38 meta-lesson (most important — shape is different from v37)

The v37 meta-lesson was: **atom-source-at-HEAD is not atom-content-at-run**. Cx-5 fixed that for sub-agent dispatch.

The v38 meta-lesson is: **source-correct-function is not source-covered-content**. A commit can ship a sound function (Cx-5 `BuildSubagentBrief` returns clean bytes; Cx-4 `OverlayManifest` stages the manifest; `recipe_templates.go:envDiffFromPrevious` compiles + runs) without the CONTENT it produces ever going through a quality gate. Every layer has to answer "what blocks bad content at the emission point, not after."

Three rules fall out:

**Rule A — Enforce at call-site, not brief-time.** The writer brief teaches surface contracts + folk-doctrine prevention. The writer still shipped folk-doctrine in v38. Teaching that lives in advisory prose is dilution, not a gate. Move every "the writer must ensure X" rule from `self-review-per-surface.md` to an engine check that fires on `action=complete substep=readmes` and fails with specific remediation.

**Rule B — Every content-emitting function takes `plan` and emits only plan-derivable claims.** `recipe_templates.go:envDiffFromPrevious(envIndex)` hardcodes bullets per envIndex. No plan data reaches the function; therefore no plan mismatch can reject a bullet. Rewriting the function to take `plan *RecipePlan` and emit a bullet only when a field actually differs between `plan.EnvTemplates[envIndex-1]` and `plan.EnvTemplates[envIndex]` makes fabrication structurally impossible.

**Rule C — Main-agent teaching parity.** Writer has 60KB spec brief; main agent (which emits 3× the content volume) has one sub-section of `recipe.md` about comment style. This asymmetry is the shape under every "main-agent paraphrase" story from v36 onwards. Main agent deserves a `zerops_knowledge topic=comment-style` (and per-emission-site voice checks) to close this gap.

**Your job on v39 analysis: verify all three rules hold at runtime.** Retrospective harness runs (v8.113.0 checks against v38 deliverable) prove the new checks catch the known v38 defects. Forward run against v39 proves no new defects surface that the v39 Cx stack didn't anticipate.

---

## 3. Required reading (in order, ≈ 90 min)

1. **This document** — you're reading it.
2. **[`runs/v38/verdict.md`](runs/v38/verdict.md)** — PAUSE verdict. Direction correct; §4 "new findings" and §7 "what v39 needs" superseded by CORRECTIONS.md.
3. **[`runs/v38/CORRECTIONS.md`](runs/v38/CORRECTIONS.md)** — Headline read. Sets v39's scope.
4. **[`plans/v39-fix-stack.md`](plans/v39-fix-stack.md)** — 11 Cx commits, ordered + parallel-safe annotations. THIS IS THE PLAN YOU EXECUTE.
5. **[`../spec-content-surfaces.md`](../spec-content-surfaces.md)** — §4 six surfaces, §5 per-surface test cheatsheet, §11 counter-examples from v28. Cx-1 gold-test binds against §5.
6. **[`runs/v38/dispatch-integrity/`](runs/v38/dispatch-integrity/)** — reference for what "paraphrased" looks like. editorial-review's 460-line diff is the shape Cx-11 must catch.
7. **[`HANDOFF-to-I8-v37-prep.md`](HANDOFF-to-I8-v37-prep.md) §6** — analysis discipline rules. Inherited unchanged.
8. **[`../../CLAUDE.md`](../../CLAUDE.md)** — auto-loaded TDD + operating norms.

---

## 4. Defect inventory at v39-prep time

| # | Defect | Status at v38 close | v39 fix path |
|---|---|---|---|
| F-8 | Sessionless-export gate | Closed for live session | No v39 action |
| F-9 | Ghost env dirs | Closed at filesystem | No v39 action |
| F-10 | Writer markdown per-codebase staging | Closed | No v39 action |
| F-11 | Close-step gate advisory | Closed | No v39 action |
| F-12 | Marker exact form | Closed | No v39 action |
| F-13 | Standalone INTEGRATION-GUIDE.md / GOTCHAS.md | Closed | No v39 action |
| F-14/F-15/F-16 | Writer first-pass compliance | Partial (B-23 still 9) | Cx-5 step-gate reduces |
| F-17 (architectural) | `BuildSubagentBrief` byte-soundness | **Closed** (code-review byte-identical; writer 4-byte encoding-only; editorial-review's byte-diff shows main-agent paraphrase, not engine brief bug) | Cx-11 (runtime enforcement) closes the remaining half |
| **F-17-runtime** (new in v38) | Main-agent paraphrase of engine brief | **OPEN — headline v39 defect for dispatch** | Cx-11 DISPATCH-GUARD-AUTO-ENFORCE |
| F-18 | Main-agent hallucinates atom IDs | Closed (Cx-5 side-effect; main agent no longer fetches atoms by name for guarded roles) | No v39 action |
| F-21 | Finalize envComment factuality | Partial (check enforcement works; atom prevention partial) | Cx-7 MAIN-AGENT-COMMENT-TEACHING strengthens atom |
| F-22 | Version anchor false-positive | Closed | No v39 action |
| **F-23** (re-open in v38) | Manifest in deliverable | **OPEN — Cx-4 partial; export whitelist gap** | Cx-2 MANIFEST-EXPORT-EXTEND (one-line) |
| F-24 | Browser recovery | Closed | No v39 action |
| **F-GO-TEMPLATE** (new in v38) | `recipe_templates.go` env prose bypasses spec | **OPEN — headline v39 defect for engine-emitted content** | Cx-1 GO-TEMPLATE-GROUND |
| **F-ENFORCEMENT-ASYMMETRY** (new in v38) | Main-agent emits 110 prose blocks with thinnest teaching | **OPEN — systemic** | Cx-7 + Cx-8 + Cx-9 + Cx-10 |

Three OPEN headline defects (F-17-runtime, F-GO-TEMPLATE, F-23) plus systemic asymmetry. v39 closes all four.

---

## 5. Operating order: fix-stack first, then v39 commission, then analyse

### Phase 1 — Land the v39 fix-stack (5–7 days sequential; 3–4 days parallel)

Execute [`plans/v39-fix-stack.md`](plans/v39-fix-stack.md) Cx-by-Cx. Dependency graph:

- **Independent first (parallel):** Cx-2, Cx-5, Cx-7, Cx-8, Cx-9, Cx-11.
- **Cx-1** independent; its scope is the biggest (plan schema changes). Land early.
- **Cx-3 → Cx-4 → Cx-6** sequential (ROUTETO → CLASSIFY → BRIEF-SLIM).
- **Cx-10** after Cx-1 (needs initial doc comments on emitters).

Green gate per Cx: RED test present + implementation + `go test <package> -race -count=1` green + `make lint-local` green.

Tag as `v8.113.0` (or bumped) after all 11 land.

### Phase 2 — Hand back for v39 commission (15 min)

Once tagged + pushed:

1. Fill `FIX_STACK_COMMITS` slot in §1 with the 11 SHAs.
2. Run retrospective harness against v38 deliverable. Expected: 4 of the new checks catch v38's known defects; assertion in §1 `V38_RETRO_EXPECTED_DELTAS_CONFIRMED`.
3. Notify the user: fix stack shipped; v39 can be commissioned.
4. **Do not commission autonomously.**

### Phase 3 — v39 commission (≈ 2 h user-driven)

Commission spec at [`plans/v39-fix-stack.md`](plans/v39-fix-stack.md) §5. New during-run tripwires:

- Any `zerops_record_fact` without `routeTo` → engine refuses (Cx-3).
- Any `close/editorial-review` attest attempt where an Agent dispatch mismatches its engine-built brief SHA → `readmes_dispatch_integrity` check fails with SUBAGENT_MISUSE.
- Any envComment or zerops.yaml comment containing first-person/journal voice → `*_comment_voice` check fails.
- Any env comment teaching scaffold-decision content → `env_comment_scaffold_decision_placement` check fails.

### Phase 4 — v39 analysis (≈ 2 h)

Same discipline as v38 inherited from [`HANDOFF-to-I8-v37-prep.md §6`](HANDOFF-to-I8-v37-prep.md):

1. **Harness first.** `./bin/zcp analyze recipe-run <v39-dir> <logs-dir> --out runs/v39/machine-report.json` + `generate-checklist`. **Commit these two files immediately as evidence floor**, BEFORE any prose.
2. **Extract flow traces.** Author `runs/v39/role_map.json` from the subagent `meta.json` descriptions. Run `python3 docs/zcprecipator2/scripts/extract_flow.py ...`.
3. **Fill checklist by hand.** Read every writer-authored file PLUS every env README PLUS the root README. Grade each row-level against the spec's per-surface tests. DO NOT GRADE pass without verifying the claim maps to source data (the v38 first-pass failure mode).
4. **Read editorial-review's completion payload in full.** Summarize by CRIT count, WRONG count, STYLE count. If CRITs exist, list each + disposition (caller-revision or reviewer-inline-fix). v38's first analysis misread "editorial-review attested" as "returned clean" — that was wrong (5 CRITs were caller-revision-required).
5. **Byte-diff every captured dispatch prompt against `BuildSubagentBrief(plan, role, ...)` output.** Write a scratch test under `internal/workflow/v39_dispatch_integrity_test.go` exactly as [`runs/v38/` evidence](runs/v38/dispatch-integrity/) was produced. Delete the test after capturing output (not source code commit).
6. **Check every content-emitter output for source fabrications.** Specifically grep the current `recipe_templates.go` output against the rendered env READMEs — any divergence is the "stripping" pattern recurring. Cx-9's warning should make this visible.
7. **Draft verdict.md** with hook-enforced shape: front matter SHAs + citations + soft-keyword cap.
8. **One commit at end** for the analysis bundle (+ evidence floor commit from step 1).

### Phase 5 — Verdict decision

**PROCEED** gate (all must hold):

- Every v39 fix-stack target closes per §4 table.
- Dispatch-integrity byte-diff clean for every guarded role (writer / editorial-review / code-review).
- Close-step reached with ≤ 1 editorial-review round AND ≤ 1 finalize round.
- Editorial-review returns 0 CRITs on first pass.
- Manifest present in deliverable tarball.
- Harness machine-report clean.
- All new v39 checks pass on v39 deliverable.
- Retrospective harness run on v38 shows new checks catch v38's known defects.

**ACCEPT-WITH-FOLLOW-UP** gate: one signal-grade issue remains (e.g. a novel folk-doctrine pattern the current citation-map regex misses; one env-comment scaffold-decision slip). Document as targeted v8.113.x patch.

**PAUSE** gate: ANY fix-stack target regressed OR F-17-runtime still visible OR F-GO-TEMPLATE CRIT count > 0 OR main-agent voice-check fail count > 0. Write HANDOFF-to-I11 + defect stack.

**ROLLBACK-Cx** gate: specific Cx introduced a regression worse than the defect it closed.

---

## 6. Analysis discipline rules (inherited from I8, unchanged)

[`HANDOFF-to-I8-v37-prep.md §6`](HANDOFF-to-I8-v37-prep.md) — eight rules. Re-read before writing any v39 prose. The v38 first pass violated Rule 3 ("No unmeasured cells when files are on disk") and Rule 4 ("Read every file before grading it") — I graded env READMEs `pass` without verifying claims against plan data. Do not repeat.

**New rule for v39 (Rule 9 — specific to the v38 meta-lesson)**: **No PASS grade on content-quality cells without cross-checking the prose claims against source data.** If an env README says "Runtime containers carry an expanded toolchain", grep the env's import.yaml for what actually differs from the adjacent tier; if no field backs the claim, grade FAIL with fabrication citation. The harness can't catch this automatically (yet); the analyst must.

---

## 7. What v39 is NOT testing

- C-15 recipe.md deletion (R2-R7 per [`PLAN.md`](PLAN.md) §2).
- Framework diversity (stays nestjs).
- Minimal-tier independent track.
- Publish-pipeline end-to-end.

---

## 8. Traps and operating rules

Inherited from I8 + I9:

1. **Analysis is NOT implementation.** Document new defects; don't fix them in the analysis commit.
2. **Cite `file:line` or `row:timestamp`.** Never "probably".
3. **One commit at end for analysis.** Code changes in separate commits per CLAUDE.md.
4. **Stop at verdict.** Do NOT autonomously start HANDOFF-to-I11.

New for v39 (surfaced by v38 first-pass failure):

5. **Rule 9 enforced**: every content-quality PASS must cross-check against source data. Put the source-data citation in the checklist cell, not just the surface-test citation.
6. **Fetch editorial-review's full completion payload.** The last assistant turn in `SESSIONS_LOGS/subagents/agent-*.jsonl` for the editorial-review agent carries the per-surface findings table. Summarize by CRIT count in the verdict; list each CRIT in the checklist under "Editorial-review findings (analyst-fill)".
7. **Check for stripping.** When the main agent edits a file between editorial-review return and close-step attest, the Go source that emitted the original content should also change. If not, flag as "stripping residual" in the verdict — Cx-9 should emit the warning but analyst verifies.

---

## 9. Success definition (when is this handoff "done")

- [ ] 11 Cx commits merged to main; `v8.113.0` tagged + pushed.
- [ ] Green full test suite + `make lint-local` at HEAD.
- [ ] Retrospective harness v8.113.0 against v38 deliverable confirms new checks catch v38's known defects per §1 slot block.
- [ ] `FIX_STACK_COMMITS` slot filled with the 11 SHAs.
- [ ] User commissioned v39.
- [ ] `runs/v39/{machine-report.json, verification-checklist.md, verdict.md}` all present; verify-verdict hook passes.
- [ ] Verdict decision shipped (PROCEED / ACCEPT-WITH-FOLLOW-UP / PAUSE / ROLLBACK-Cx).
- [ ] Run-log entry appended to [`PLAN.md`](PLAN.md) §4 "v39" with verdict + plan delta.
- [ ] User decides next. If PROCEED: C-15 (recipe.md deletion) becomes the next handoff target per [`PLAN.md`](PLAN.md) §3.

---

## 10. Starting action

1. Read all 10 items in §3.
2. Verify tree is clean on `main` + tests green before touching anything.
3. Begin Phase 1 per [`plans/v39-fix-stack.md`](plans/v39-fix-stack.md). Start with Cx-1 (GO-TEMPLATE-GROUND) — highest leverage, biggest scope. Parallel Cx-2 + Cx-5 + Cx-7 + Cx-11 if multi-tasking.
4. Commit each Cx separately with its RED-GREEN tests; green `make lint-local` + `go test -race` each time.
5. Retrospective harness run against v38 deliverable.
6. Tag v8.113.0 after all 11 land with green CI.
7. Fill §1 slots. Notify user.
8. Wait for user to commission v39. **Do not commission autonomously.**
9. Post-commission, do analysis per Phase 4 discipline rules + new Rule 9.
10. Ship v39 verdict.
11. Append v39 entry to [`PLAN.md`](PLAN.md) §4 run-log.
12. Stop. Hand back. User decides next.

Good luck. v38 proved the dispatch boundary architecturally sound but surfaced two deeper layers: runtime enforcement of the guard, and engine-emitted content bypassing the spec. v39 closes both — or points at the next layer.
