# rollback-criteria.md — v36 go/no-go thresholds

**Snapshot status**: derivative of [`../v35/rollback-criteria.md`](../v35/rollback-criteria.md). All v35 tightenings (T-1, T-2, T-8, T-9) preserved. T-13 added per HANDOFF-to-I7 §"New trigger class v36 may surface" for Cx-commit runtime regression. Decision classes + rollback procedure unchanged from v35.

**v36 arbitration result**: **ACCEPT-WITH-FOLLOW-UP**. Zero T-triggers fire. Two PAUSE-triggers fire weakly (P-3 downstream-facts, P-4 TodoWrite full-rewrites) but neither warrants holding v37 — both are agent-habit signals, not architecture regressions. Close-step + editorial-review unreached is the gating follow-up (see F-8 in [`analysis.md`](analysis.md) → Cx-CLOSE-STEP-GATE patch before v37).

---

## 1. Decision matrix — ROLLBACK triggers evaluated against v36

| Trigger | Measurement (tightened) | v35 observed | **v36 observed** | Fires? |
|---|---|---|---|---|
| **T-1** Deploy rounds | `checkResult.passed==false` on `complete step=deploy` | **11** (stalled) | **2** (first generate check for env_self_shadow; first post-writer readme-readback) | **NO** (threshold >2; v36 = 2, at-threshold but within gate) |
| **T-2** Finalize rounds | `checkResult.passed==false` on `complete step=finalize` | N/A (never reached) | **1** (env 4 minContainers drift + v2/v3 anchors; re-finalize green) | **NO** (threshold >1; v36 = 1, at-threshold) |
| **T-3** Substrate invariant broken | any of O-3 / O-6 / O-7 / O-8 / O-9 / O-14 / O-15 / O-17 | 0 observed | **0** observed | **NO** |
| **T-4** Env-var coordination regression | CS-1 non-zero OR CS-2 > 0 | held | **held** — all 3 codebases use Zerops-native names; 0 runtime crashes on env-var | **NO** |
| **T-5** Manifest-honesty v34 class (M-3b) | `(routed_to=claude_md, published_gotcha)` mismatches | not measurable | **not re-audited close-side** (close unreached) — deploy-side honesty green | **NOT MEASURABLE → treat as NOT FIRED** |
| **T-6** Phantom output tree | `find /var/www -maxdepth 2 -type d -name 'recipe-*'` | 0 | **0** | **NO** |
| **T-7** Self-inflicted gotchas shipped (CQ-2) | writer-classification × README | 0 | **0 shipped** — env 4 drift + v2/v3 anchors caught by finalize, fixed before complete | **NO** |
| **T-8** Very-long main bash ≥ 120 s | per-call durations; evaluator pending | not measured | **scripted evaluator still pending**; manual spot-check shows no Bash call ≥ 120 s (longest = 22 s nohup+curl cycle) | **NO (manual)** |
| **T-9** Wall clock blowout (active-work) | active-work excl >300 s gaps | raw 175 / adjusted N/A | **~1:45** active (1:49:25 raw, one ~4-min gap after research-complete) | **NO** (threshold showcase > 120 min) |
| **T-10** Content-tree lint regression | `make lint-local` post-run | pending | **green post-v8.108.1 release** | **NO** |
| **T-11** Editorial-review wrong-surface CRIT shipped | editorial return payload | not applicable | **not measurable** — editorial-review never dispatched | **NOT MEASURABLE** |
| **T-12** Reclassification delta | editorial return payload | not applicable | **not measurable** — editorial-review never dispatched | **NOT MEASURABLE** |
| **T-13** (NEW) Cx-commit runtime regression | `MISSING_EVIDENCE`/`TOPIC_EMPTY`/envelope-stitch failure with no escape path | N/A | **0** — Cx-PLAN-NIL-GUIDANCE resolved cleanly at research step; no other Cx-related error surfaced | **NO** |

**Net**: 0 T-triggers fire. No ROLLBACK/ROLLBACK-Cx signal.

---

## 2. PAUSE triggers evaluated against v36

| Trigger | Measurement | v35 | **v36** | Fires? |
|---|---|---|---|---|
| **P-1** C-3 first-deploy-pass | first `complete step=deploy` accepted | 0/1 stalled | **0/1** passed (generate-first failed env_self_shadow, fixed on round 2) — acceptable under P-1 text if T-1 clean (≤ 2 rounds) | **NO** (accompanying T-1 clean) |
| **P-2** C-5 post-writer Edits on README ≤ 3 per file | Edit count post writer-return | not applicable | **2 Edits on workerdev, 3 on apidev** (v2/v3 scrubbing) — at threshold, fix-pass + inline polish | **NO** (at threshold) |
| **P-3** C-9 downstream-facts ≥ 2 | fact-record emission | 1 (partial) | **1** (row 136: agent-browser instability) | **WEAKLY — below 2 but same as v35** |
| **P-4** C-11 TodoWrite full-rewrites > 1 | TodoWrite content delta | 12 in v34 | **7 full-rewrites** | **WEAKLY — above 1 but agent-habit signal, not architecture regression** |
| **P-5** CQ-1 gotcha-origin ratio 60-80% | manual classification | not applicable | **not classified** — cold-read not run (close unreached) | **NOT MEASURED** |
| **P-6** Other M-3 mismatches | manifest × README grep | not applicable | **not re-audited** close-side | **NOT MEASURED** |
| **P-7** CR-1 CRIT shipped after close-fix | close audit | not applicable | **not measurable** — close not run | **NOT MEASURABLE** |
| **P-8** CR-3 WRONG > 3 | code-review output | not applicable | **not measurable** | **NOT MEASURABLE** |
| **P-9** Dispatch-integrity build lints on post-v35 atoms | build-time grep | green | **green** — no atom edits between v35 and v36; hotfix touched only `internal/tools/guidance.go` + tests | **NO** |
| **P-10** Substep out-of-order (C-6) | workflow-action-complete validation | 0 | **0** | **NO** |

P-3 and P-4 fire weakly. Evaluation:

- **P-3**: downstream-facts = 1. Below the 2-threshold. This is v35 carry-through — downstream-fact discipline was called out in v35 analysis. v36 agent recorded only the agent-browser environmental fact as `scope=downstream`. The execOnce gotcha, env_self_shadow discovery, and Meilisearch ESM were recorded as `scope=content` / `type=gotcha_candidate` (agent-legitimate classification). Verdict: **not PAUSE-worthy** — same bar as v35 but agent-habit-level, not architecture.

- **P-4**: TodoWrite full-rewrites = 7. Above 1-threshold. 7 rewrites across 2-hour session ≈ 1 per ~15 min, aligned with step/substep transitions. Not backfill bursts (which is what C-11 gates). Verdict: **not PAUSE-worthy** — signal-grade agent hygiene, no structural issue.

**Net**: no PAUSE decision. Both weak signals fold into the ACCEPT-WITH-FOLLOW-UP lane.

---

## 3. ACCEPT-WITH-FOLLOW-UP triggers

| Trigger | v36 observed | Action |
|---|---|---|
| **F-1** Wall clock at upper bound | 1:45 active-work << 120 min threshold | **NOT FIRED** — well under |
| **F-2** Main bash total 10-15 min | est. ~5 min | **NOT FIRED** |
| **F-3** CR-3 WRONG = 3 (v34 baseline held, no improvement) | not measurable (close unreached) | **NOT MEASURABLE** |
| **F-4** IG item standalone borderline | not run post-run | **NOT MEASURED** |
| **F-5** Cross-README dedup multi-round | not run | **NOT MEASURED** |
| **F-6** Deferred-deletion check fire-count = 0 | not measured | **NOT MEASURED** |
| **F-7** Minimal-tier Path A surfaced | v35.5 minimal still pending | **NOT RUN** |
| **F-8** data-flow-minimal §11 escalation | minimal not run | **NOT RUN** |

**F-follow-up-specific-to-v36** (NEW):

| Trigger | v36 observed | Follow-up action |
|---|---|---|
| **F-v36-a** Close-step gate bypassed via sessionless export (class F-8) | 1 sessionless export at 16:02:29 UTC producing `.tar.gz` without editorial-review | **Cx-CLOSE-STEP-GATE patch before v37** — require `--session` on export OR detect live session and refuse |
| **F-v36-b** Editorial-review + code-review unrun | close step never called; agent elected to wrap at finalize-complete | v37 commission must force close-step completion (or at minimum, exercise it manually) before export |
| **F-v36-c** agent-browser persistent CDP timeout on zcp container | 4/5 calls timed out on dev subdomain; 1/1 passed on example.com | Environmental, not recipe-workflow; flag for agent-browser team |
| **F-v36-d** `extract_flow.py` size-count drop on Unicode atom bodies | rows 85, 107, 150, 166 report `result_size=0` despite non-empty responses | extractor maintenance — no runtime impact |
| **F-v36-e** Downstream-facts discipline below bar (P-3 signal) | 1/2 minimum | Atom-tree tweak: sharpen Theme-B coaching in atoms/*.md about recording env/observation facts on `scope=downstream` |

---

## 4. Decision

**ACCEPT-WITH-FOLLOW-UP**. Rationale:

- All 13 ROLLBACK triggers clean (0 fired).
- Two weak PAUSE signals (P-3, P-4) are agent-habit, not architecture regression. v35 showed the same shape and was still graded PAUSE-specifically-for-engine-defects, not for these.
- Coverage gap is the dominant observation: close-step + editorial-review + code-review never ran. That's a follow-up (Cx-CLOSE-STEP-GATE + v37 commission), not a rollback signal.
- The **fix stack empirically worked**: F-1 through F-6 all closed or UNREACHED-fix-shipped. The Cx-commits do what the HANDOFF-to-I6 spec said they would.

Decision ordering per v35 verdict §2 (strictest applicable): **ROLLBACK > ROLLBACK-Cx > PAUSE > ACCEPT-WITH-FOLLOW-UP > PROCEED**.

- ROLLBACK: not justified — zero triggers.
- ROLLBACK-Cx: not justified — Cx-stack behaves as designed. Cx-PLAN-NIL-GUIDANCE hotfix confirms the only observed defect in the guidance path is closed.
- PAUSE: not justified — weak signals don't warrant holding v37.
- ACCEPT-WITH-FOLLOW-UP: **lands here** — one targeted patch (Cx-CLOSE-STEP-GATE) + v37 commission as second-confirmation.
- PROCEED to v37 without patch: **not justified** — F-8 lets agents skip the editorial-review + code-review gate by exporting without session. Patch first.

---

## 5. Rollback procedure (not invoked)

If ROLLBACK had fired, procedure from [`../v35/rollback-criteria.md §4`](../v35/rollback-criteria.md) would apply. Unchanged from v35. Not executed in v36.

If ROLLBACK-Cx had fired (per HANDOFF-to-I7 §Decision tree), procedure:
1. Identify offending Cx-commit from defect evidence.
2. `git revert <sha>` — preserves history.
3. Re-run tests + lint green.
4. Commit revert with v36 evidence reference.
5. Tag as v8.108.2.
6. Commission v37 against reverted state.

Not executed in v36 — all Cx-commits behaving as designed.

---

## 6. Follow-up work

Ordered by priority before v37 commission:

1. **Cx-CLOSE-STEP-GATE** (P0): make `zcp sync recipe export` refuse sessionless execution when a live recipe session exists for the target directory; OR make close-step complete mandatory before export is permitted.
2. **v37 commission** (P1): second-confirmation showcase run that reaches close-step complete including editorial-review + code-review + close-browser-walk. Measure §7a + §8 + §11a bars.
3. **agent-browser stability on zcp** (P2): environmental investigation (out of scope for zcprecipator2 engine); flag for tools team.
4. **v35.5 minimal commission** (P2): now unblocked per v36 PROCEED-class result on showcase.
5. **T-8 + T-9 evaluator scripts** (P2 carry-forward from v35): still pending. Measurement definitions work manually; automation is nice-to-have.
6. **`extract_flow.py` Unicode size fix** (P3): cosmetic — doesn't affect analysis accuracy.

---

## 7. Audit trail

| Step | What happened | Timestamp |
|---|---|---|
| 1 | v8.108.0 released (Cx-BRIEF-OVERFLOW + Cx-CHECK-WIRE-NOTATION + Cx-ITERATE-GUARD + Cx-GUIDANCE-TOPIC-REGISTRY + Cx-KNOWLEDGE-INDEX-MANIFEST) | 2026-04-21 pre-morning |
| 2 | v36-attempt-1 commissioned (session `43814d9c5e09e85d`); died at research step on "does not apply" messages (F-7 surfaced) | 2026-04-21 ~13:45 UTC |
| 3 | F-7 root-caused in `internal/tools/guidance.go:73-74`; Cx-PLAN-NIL-GUIDANCE fix written + tests; commit `c512757` | 2026-04-21 14:00 UTC |
| 4 | v8.108.1 released | 2026-04-21 14:06 UTC |
| 5 | v36 commissioned against live v8.108.1 (session `7743c6d8c8a912fd`) | 2026-04-21 14:14 UTC |
| 6 | v36 reached finalize-complete; agent exported via sessionless `zcp sync recipe export` (F-8 surfaced) | 2026-04-21 16:04 UTC |
| 7 | User handed off deliverable tree for analysis | 2026-04-21 18:00 UTC |
| 8 | Flow extracted via `extract_flow.py`; 6 subagents mapped; 208 main tool calls traced | 2026-04-21 18:45 UTC |
| 9 | F-1..F-6 evidence scan; F-7 (pre-run-closed) + F-8 (new signal-grade) documented | 2026-04-21 ~19:00 UTC |
| 10 | This document + verdict + analysis + calibration-bars + README all landed | 2026-04-21 ~19:30 UTC |
| 11 | Commit as single `docs(zcprecipator2): v36 run analysis + verdict` | next |
