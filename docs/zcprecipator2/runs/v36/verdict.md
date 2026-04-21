# v36 verdict — PAUSE pending fix stack + analysis harness

**Run**: nestjs-showcase v36 against tag `v8.108.0` + hotfix `v8.108.1` (Cx-PLAN-NIL-GUIDANCE)
**Close date**: 2026-04-21 (session `7743c6d8c8a912fd` / Claude UUID `8271b0d3-57d1-4dac-a17c-a36181150071`)
**Outcome reached**: finalize-complete. Close step never called; deliverable tree exported via sessionless `zcp sync recipe export` (bypassing staging).
**Verdict applied**: **PAUSE** — v37 blocked pending 6-commit fix stack + analysis harness build.
**Authored**: 2026-04-21 post-analysis, **revised** 2026-04-21 after deeper audit surfaced 5 additional defect classes the first pass missed.

---

## 0. Revision history

This verdict was originally shipped at commit `67221ba` as **ACCEPT-WITH-FOLLOW-UP**. That verdict was wrong. It missed F-9 through F-16 — five systemic defects plus three writer-compliance defects that sat in plain view in the deliverable tree and session JSONL. The revised verdict here is **PAUSE**, reflecting the corrected defect inventory.

Full list of first-pass failures + their evidence: [`CORRECTIONS.md`](CORRECTIONS.md).

The original [`analysis.md`](analysis.md) is preserved unchanged as historical context. Its claims about F-1..F-8 stand; its claims about deliverable integrity and content quality are superseded by CORRECTIONS.md + this verdict.

---

## 1. Decision

**PAUSE** on v37 commission until:

1. **Analysis harness lands** (Phase 1 of [`../HANDOFF-to-I8-v37-prep.md`](../HANDOFF-to-I8-v37-prep.md)): `cmd/zcp/analyze/` CLI tool that mechanically surfaces structural bars — specifically the bars that would have caught F-9, F-10, F-12, F-13 on v36 without needing user intervention.
2. **Fix stack lands** (Phase 2): six Cx-commits closing F-8..F-13. Tag as v8.109.0.
3. **Harness validates against v36**: run `zcp analyze recipe-run` against v36 deliverable; expected output flags F-9, F-10, F-12, F-13 retrospectively.
4. **THEN** commission v37 against v8.109.0.

**Do not roll back**. The rollout layer (C-7e..C-14) works. The v8.108.0 Cx-stack (HANDOFF-to-I6) works — F-1..F-6 all close under v36 live conditions. The v8.108.1 hotfix (Cx-PLAN-NIL-GUIDANCE) works — F-7 closed pre-run. The defects demanding PAUSE (F-8..F-13) are in **atom text accuracy** and **integration glue between writer output and export staging**, not in the rollout commits or the Cx-fix commits.

**Full defect inventory**: [`../HANDOFF-to-I8-v37-prep.md §4`](../HANDOFF-to-I8-v37-prep.md) + [`../../05-regression/defect-class-registry.md §16.1–16.13`](../../05-regression/defect-class-registry.md).
**Fix-stack spec**: [`../HANDOFF-to-I8-v37-prep.md §5 Phase 2`](../HANDOFF-to-I8-v37-prep.md).
**Harness spec**: [`../spec-recipe-analysis-harness.md`](../spec-recipe-analysis-harness.md).

---

## 2. Why not ROLLBACK

Thirteen T-triggers evaluated; zero fire. Full matrix in [`rollback-criteria.md §1`](rollback-criteria.md). Highlights:

- **T-1 Deploy rounds**: 2 (≤ 2 threshold). PASS.
- **T-2 Finalize rounds**: 1 (≤ 1 threshold). PASS.
- **T-3..T-10**: all clean or not-measurable-without-close.
- **T-11, T-12**: editorial-review not dispatched; bars unmeasurable.
- **T-13 (new) Cx-commit runtime regression**: zero. Cx-stack behaves as designed.

The defects mandating PAUSE are all **outside the T-trigger coverage** — they're atom-text and integration defects that v35's analysis didn't anticipate because v35 never reached the phases where they surface. v36 reached those phases for the first time.

**Inflating the verdict**: my first-pass ACCEPT-WITH-FOLLOW-UP missed these entirely. PAUSE is the right call; ACCEPT-WITH-FOLLOW-UP would have shipped v37 against broken atom text and produced another unusable deliverable.

---

## 3. Why not ROLLBACK-Cx

Per HANDOFF-to-I7 §Decision-tree, ROLLBACK-Cx fires when a Cx-commit introduces a worse regression than the defect it was meant to close. v36 shows the opposite:

| Cx-commit | Target | v36 observation |
|---|---|---|
| `2a60ee0` Cx-CHECK-WIRE-NOTATION | F-2 close | 0× Go struct.field in any check detail. Closed. |
| `0bc7ea1` Cx-ITERATE-GUARD | F-3 close | 0 iterate calls; guard exists but untriggered. UNREACHED-fix-shipped. |
| `6c3320f` Cx-BRIEF-OVERFLOW | F-1 close | Max dispatch-brief-atom response 11 532 B (cap 32 KB). Envelope fires correctly. Closed. |
| `a0c2069` Cx-GUIDANCE-TOPIC-REGISTRY | F-5 close | 0 unknown-topic responses + 0 "does not apply" on tier-gated topics. Closed. |
| `3fce7c7` Cx-KNOWLEDGE-INDEX-MANIFEST | F-6 close | 0 wire-contract knowledge queries issued; envelope-delivery short-circuited the path. UNREACHED-fix-shipped. |
| `c512757` Cx-PLAN-NIL-GUIDANCE | F-7 close | Research-step tier-only lookups resolved on v36. Closed (pre-run). |

All six commits do what they were spec'd to do. Reverting any of them re-introduces a v35-class defect. ROLLBACK-Cx has no target.

---

## 4. Why PAUSE, not ACCEPT-WITH-FOLLOW-UP

My original ACCEPT-WITH-FOLLOW-UP verdict was based on the thesis "fix stack works; one small follow-up (Cx-CLOSE-STEP-GATE) then v37". That's incorrect because:

1. **Six source-level defects** (F-8, F-9, F-10, F-11, F-12, F-13) reproduce every showcase run until fixed. v37 without the fix stack would produce the same broken deliverable tree. There is no "accept with follow-up"; the follow-up is the fix stack.

2. **Three writer-compliance defects** (F-14, F-15, F-16) surfaced 9 distinct check failures on writer-1's single first-pass output. Even with F-12 fixed (marker form), writer ignored brief instructions in multiple dimensions. Signal-grade on v36 but needs v37 data to distinguish single-run anomaly from systemic.

3. **Close-step coverage gap** (§7a editorial-review, §8 close-browser-walk, §11a reclassification) is the entire post-finalize promise of zcprecipator2 architecture. v36 exercised zero of it. ACCEPT-WITH-FOLLOW-UP implies the measured surface is acceptable; the measured surface on v36 is ~60% of the architecture — below "acceptable" threshold.

4. **The analysis harness is prerequisite, not optional**. Without it, v37's analysis will reproduce my v36 failure mode. The harness is not a follow-up to v37 — it gates v37.

PAUSE does not mean "the effort is failed". It means "hold v37 until the prerequisites to get a trustworthy v37 verdict are in place". The architecture is salvageable.

---

## 5. What v36 proved positive (confidence to continue)

- **v8.108.0 Cx-stack works** under live conditions. F-1..F-6 all close.
- **v8.108.1 hotfix works** — F-7 closed in-window.
- **Deploy + feature sweep + stage round-trip all functional**. 5/5 features green; jobs dispatch → NATS → worker → DB UPDATE in <500 ms.
- **Step gates catch real issues**. env 4 minContainers drift, version anchors — both caught at finalize, fixed inline, re-finalize green.
- **Multi-agent orchestration works**. 3 parallel scaffolders with coordinated SymbolContract; feature sub-agent single-author across 3 codebases; writer stitched 15-atom brief correctly.
- **Envelope pattern works under load**. 15 dispatch-brief-atom calls in 9 seconds, max 11.5 KB per response, cleanly under 32 KB cap.
- **Platform invariants hold**. `0.0.0.0` bind, trust-proxy, `zsc execOnce`, VXLAN routing, cross-service env-var auto-inject — all correctly integrated.
- **Import.yaml content quality is production-grade**. Substantive per-decision comments teaching platform mechanisms.
- **Finalize-emitted canonical env READMEs structurally correct**. Proper fragment markers, audience/context/promotion/operational sections.

These are not small wins. The **hard parts of the architecture — state machine, envelope pattern, sub-agent dispatch, content surface contracts, finalize template emission — all work**. What doesn't work is the **atom text accuracy** (template variables, marker form, standalone file spec) and the **integration glue between writer output and export staging**. Both are bounded engineering fixes. The thesis isn't invalidated.

---

## 6. What v36 proved open (gated by fix stack)

- **F-8**: sessionless export bypasses close-step gate. Agent's escape hatch. Systemic.
- **F-9**: writer brief references un-populated `{{.EnvFolders}}`; main agent invents slug names. 6 ghost env dirs every run. Systemic.
- **F-10**: writer markdown stranded — not committed to git, stripped by `git ls-files`-based export. Every sessionless run produces an incomplete deliverable. Systemic.
- **F-11**: close-step gate is advisory (duplicate framing of F-8 from the CLI side). Folded into F-8 fix.
- **F-12**: writer brief atom uses wrong marker form; writer writes wrong form; fix pass spends 20+ Edit cycles correcting. Systemic.
- **F-13**: writer atoms prescribe standalone INTEGRATION-GUIDE.md + GOTCHAS.md duplicating fragment content. Zero consumers. 6 dead files per showcase run. Systemic.
- **F-14/F-15/F-16**: writer first-pass compliance failures across 3 dimensions (missing markers, missing Gotchas H3, IG without fenced blocks). 9 distinct failures on v36's single writer-1 dispatch. Either single-run noise or "writer brief too dense" — v37 discriminates.

F-8..F-13 get fixed pre-v37. F-14..F-16 get observed on v37; if they persist, they escalate to a separate fix stack (likely "writer brief decomposition" or "sharper self-review atom").

---

## 7. Halted / not-halted

**Halted until Phase 1 (harness) + Phase 2 (fix stack) complete**:
- v37 showcase commissioning.
- Any analysis work that depends on v37 artifacts.

**Not halted**:
- v35.5 minimal commissioning — independent of showcase fix stack. Minimal-tier Path B writer concern would be addressed by Cx-CLOSE-STEP-STAGING (F-10) which lands anyway.
- Repository hygiene: atom-tree lint, test suite refresh, catalog sync, platform version refresh.
- Analysis harness development — explicitly **commissioned** as prerequisite work.
- Fix-stack development — explicitly commissioned.
- Atom-tree edits to close F-14/F-15/F-16 pre-emptively (e.g., sharpen `content-surface-contracts.md` to reduce writer non-compliance) — would reduce v37 risk, not v37-blocking.

---

## 8. Gate to PROCEED on v37

Harness (Phase 1):
- `cmd/zcp/analyze/` CLI builds + runs against v36 deliverable.
- Mechanically surfaces F-9, F-10, F-12, F-13 retrospectively.
- Commit-hook enforces verdict citation rule + checklist completeness.
- `tools/lint/atom_template_vars.go` catches unbound template vars at build.

Fix stack (Phase 2):
- Cx-ENVFOLDERS-WIRED (F-9)
- Cx-ATOM-TEMPLATE-LINT (F-9 prevention)
- Cx-MARKER-FORM-FIX (F-12)
- Cx-STANDALONE-FILES-REMOVED (F-13)
- Cx-CLOSE-STEP-STAGING (F-10)
- Cx-CLOSE-STEP-GATE-HARD (F-8 / F-11)

All six must land with green tests + lint. Tag as v8.109.0.

**Then** v37 commissioned against v8.109.0. v37 must reach close-step complete, exercise editorial-review + code-review + close-browser-walk. Harness runs on the v37 deliverable. Analyst works the checklist. Verdict binds to evidence.

**v37's verdict, not v36's, decides whether zcprecipator2 is validated.**

---

## 9. Appendix — decision-path audit trail

| Step | What happened | Timestamp |
|---|---|---|
| 1 | HANDOFF-to-I6 fix-stack delivered as v8.108.0 | 2026-04-21 AM |
| 2 | v36-attempt-1 died at research-step on F-7 | 2026-04-21 ~13:45 UTC |
| 3 | F-7 root-caused; Cx-PLAN-NIL-GUIDANCE fix + tests + v8.108.1 release | 2026-04-21 14:00–14:06 UTC |
| 4 | v36 commissioned against v8.108.1 | 2026-04-21 14:14 UTC |
| 5 | v36 reached finalize-complete; sessionless export produced .tar.gz | 2026-04-21 16:04 UTC |
| 6 | User handed off deliverable tree for analysis | 2026-04-21 ~18:00 UTC |
| 7 | First analysis pass shipped as commit `67221ba` with verdict ACCEPT-WITH-FOLLOW-UP | 2026-04-21 ~19:30 UTC |
| 8 | User flagged ghost env directories not caught by first pass | 2026-04-21 post-commit |
| 9 | Deeper audit surfaced F-9 (envfolders), F-10 (writer stranding), F-11 (gate advisory), F-12 (marker form), F-13 (standalone files), F-14–F-16 (writer compliance) | 2026-04-21 later |
| 10 | Meta-analysis: v36 analysis process rewarded artifact-shape over evidence binding | 2026-04-21 later |
| 11 | Harness design (Tier 1/2/3) spec'd to prevent recurrence | 2026-04-21 later |
| 12 | Handoff package landed: HANDOFF-to-I8-v37-prep.md + CORRECTIONS.md + this revised verdict + spec-recipe-analysis-harness.md + defect-class-registry rows 16.7–16.13 | 2026-04-21 late |

No engine-level fixes applied during this verdict window. The Cx-CLOSE-STEP-GATE + 5 other Cx-commits are queued for a separate commission with HANDOFF-to-I8-v37-prep.md as driver.

---

## 10. Lesson to institutionalize

The v36 analysis process failed. The failure was not "insufficient analyst effort" — it was **the process allowed artifact-shape to pass for depth**. The handoff asked for 5 files in specific shapes; the analyst produced them with convincing surface structure; nothing forced verification of the claims inside.

The lesson: **every analysis claim must survive `grep` or `jq`**. Subjective judgment layers on top of mechanical bars that a shim produces and a commit hook enforces. The architecture at large works. The analysis process did not. The harness fixes this.

v37's analyst reads this verdict plus [`CORRECTIONS.md`](CORRECTIONS.md) plus [`../HANDOFF-to-I8-v37-prep.md`](../HANDOFF-to-I8-v37-prep.md) §6 before writing any prose. If they catch themselves writing "success" or "clean" without a citation, they stop.
