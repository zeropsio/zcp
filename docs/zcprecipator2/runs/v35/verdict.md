# v35 verdict — PAUSE + engine-level defects identified

**Run**: nestjs-showcase v35 against tag `v8.105.0`
**Close date**: 2026-04-21 (session `8324884b199361d9`)
**Verdict applied**: **PAUSE** (not ROLLBACK, despite literal rollback-criteria measurements being ambiguous)
**Authored**: 2026-04-21 post-analysis

---

## 1. Decision

**PAUSE on v36 planning** until the five-commit engine fix stack (Cx-BRIEF-OVERFLOW, Cx-CHECK-WIRE-NOTATION, Cx-ITERATE-GUARD, Cx-GUIDANCE-TOPIC-REGISTRY, Cx-KNOWLEDGE-INDEX-MANIFEST) lands. **Do not roll back v8.105.0**. The rollout commits C-7e..C-14 are not the cause of v35's failure and reverting them would destroy working improvements without fixing anything.

**Full analysis**: [`analysis.md`](analysis.md).
**Defect rows**: [`../../05-regression/defect-class-registry.md §16.1–16.6`](../../05-regression/defect-class-registry.md#v35--engine-level-defects-surfaced-showcase-stuck-on-writer_manifest_completeness).
**Fix-stack handoff**: [`../HANDOFF-to-I6.md`](../HANDOFF-to-I6.md).

---

## 2. Why not ROLLBACK

[`rollback-criteria.md §2.1`](rollback-criteria.md) lists 12 T-triggers, any one of which fires a rollback decision. Walking every trigger against v35:

| Trigger | Measurement | v35 observed | Fires? |
|---|---|---|---|
| **T-1** Deploy-complete rounds | `is_error:true` on `complete step=deploy` | `1` (only the `action=skip` INVALID_PARAMETER registers as `is_error:true`); `checkResult.passed=false` count = **11** | **Ambiguous**: literal `is_error` reading → no fire; spirit reading (`checkResult.passed=false`) → fires hard. See §3 for measurement tightening. |
| **T-2** Finalize rounds | same measurement on `complete step=finalize` | N/A — finalize never ran | No fire |
| **T-3** Substrate invariant | any of O-3 / O-6 / O-7 / O-8 / O-9 / O-14 / O-15 / O-17 events | not measured scripted; manual scan shows no obvious O-6 (SUBAGENT_MISUSE), O-7 (git-lock), or O-14 (git-repo-missing) events | **Not fired** under manual read; formal measurement pending scripted O-* evaluators (HANDOFF-to-I5 Front A, Cx-TELEMETRY-EVALUATORS) |
| **T-4** Cross-scaffold env-var coordination regression | `zcp check symbol-contract-env-consistency` + runtime crash logs | scaffolders produced coordinated env-var names (no DB_PASS/DB_PASSWORD-style drift observed); all 5 service kinds wired consistently across apidev/appdev/workerdev | **Not fired** |
| **T-5** Manifest-honesty v34 class regression | `(routed_to=claude_md, published_gotcha)` mismatches | manifest never reached correct shape to honesty-check | **Not measurable** — upstream defect F-1 blocks the path to this gate |
| **T-6** Phantom output tree | `find /var/www -maxdepth 2 -type d -name 'recipe-*'` | not measured scripted; `/var/www` tree stayed under the dev-mount convention | **Not fired** under manual read |
| **T-7** Self-inflicted gotchas shipped | writer-classification manifest cross-check | nothing shipped (force-export is not a ship; deploy gate never passed) | **Not applicable** |
| **T-8** Very-long main bash ≥ 120s | bash calls ≥ 120s | stats show `173 Bash calls totaling 16366s latency`, avg 95s. Individual durations not broken out — some SSH calls (dev-server start ~13s; npm install ~90s) are expected. True O-3 would be an idle 120s+ wait outside a legitimately long command. Not measured individually. | **Unknown** — needs per-call measurement |
| **T-9** Wall clock blowout | showcase > 135 min OR minimal > 90 min | showcase raw wall 175 min; **AFK permission-prompt wait inflated this**; active-work-time is not extractable from the raw timestamps | **Not fired** (measurement definition fails on AFK; see §3) |
| **T-10** Content-tree lint regression | `make lint-local` post-run | not run post-v35 (no atom-tree edits); presumably clean | **Not fired** pending scripted post-run lint |
| **T-11** Editorial-review wrong-surface CRIT | editorial-review return payload | N/A — close never ran | **Not applicable** |
| **T-12** Classification-error-at-source regression | editorial-review reclassification_delta | N/A — close never ran | **Not applicable** |

**Net**: zero T-triggers cleanly fire under the current measurement text. T-1 is the closest call; its measurement definition ambiguity must be resolved before it can arbitrate this run.

Even if T-1 were tightened to the spirit-reading (`checkResult.passed=false` count > 2 → fires), the prescribed rollback action is "revert the rollout-sequence commits and hold on v36 until the regression class is root-caused". Root-causing v35 shows:

- The regression class is **not** in the rollout commits (F-1..F-6 are all pre-rollout engine/harness/knowledge-engine).
- Reverting C-7e..C-14 leaves F-1..F-6 fully intact.
- Reverting C-7e..C-14 additionally **removes**: author-runnable `zcp check` shim tree, editorial-review role, expanded writer_manifest_honesty dimensions, shrunk StepCheck payload, DISPATCH.md guidance, build-time atom-tree lints, `zcp dry-run recipe` harness.

Rollback's cure is worse than the disease for a defect class the rollback doesn't touch. **PAUSE** is the correct disposition.

---

## 3. Measurement-definition tightening — required before next run arbitrates

Three rollback-criteria text defects surfaced during this verdict:

### 3.1. T-1 `is_error` vs `checkResult.passed`

Current text: `count of `is_error:true` on `complete step=deploy` `. In v35, `is_error:true` fires only on hard engine errors (`INVALID_PARAMETER`, schema violations). A check-failure response has `is_error:false` at the MCP layer and `checkResult.passed=false` inside the response body. **Eleven deploy-check failures in v35 surfaced as `is_error:false`.** The literal reading tracks only hard engine errors — it cannot measure check-round count, which is what the trigger was designed to measure.

**Proposed tightening** (to apply before arbitrating any future run):

> **T-1** Deploy-complete content-check rounds (C-1) | count of `complete step=deploy` responses where `json.loads(response).checkResult.passed == false` | **> 2** rounds

Same tightening applies to T-2 (finalize rounds).

### 3.2. T-9 wall clock with AFK-adjustment

Current text: `showcase > 135 min`. v35's raw wall is 175 min; ~88 min of this is the provision-complete gap where the main agent waited on a user permission prompt while the user was AFK. Raw-timestamp wall time is not a reliable measure when the run contains permission-prompt waits.

**Proposed tightening**:

> **T-9** Wall clock blowout (O-1 / O-1M) | sum of `(tool_result.timestamp - tool_use.timestamp)` across session (active-work time; excludes any gap between consecutive tool events > 300 s which is assumed AFK or permission-wait) | showcase `> 120 min` OR minimal `> 75 min`

The 300 s AFK-exclusion threshold conservatively keeps legitimate long-running tool calls (npm install, zerops build ~2 min) in-budget while excluding multi-minute permission waits.

### 3.3. T-8 per-call bash duration is not in the session log summary — needs scripted measurement

Current text: `bash calls ≥ 120s`. The session log's timeline.py stats reports aggregate `total latency` per tool, not per-call durations. An evaluator must parse per-call `(tool_use.ts, tool_result.ts)` pairs and count those ≥ 120 s. This is a measurement-script gap, not a definition gap — documented here because the T-8 arbitration failed on v35 due to evaluator absence.

**Deliverable**: `scripts/extract_calibration_evidence.py::extract_long_bash_calls(log_path, threshold=120)` returning list of `(ts, duration_s, command_first_line)` — already implied by HANDOFF-to-I5 Front A, but not yet implemented.

---

## 4. New bars added to the measurement sheet

v35 exposed six defects outside the 97-bar coverage of the original [`calibration-bars.md`](calibration-bars.md) §1–§11 sheet. Landed as new bars B-9..B-14 extending §9 (dispatch + brief-composition integrity), with `[signal]` severity until the corresponding Cx-commits from [`../../HANDOFF-to-I6.md`](../../HANDOFF-to-I6.md) land evaluators (after which they upgrade to `[gate]`):

- **B-9** max(`zerops_workflow` tool_result size) ≤ 32 KB per session (catches F-1 brief-overflow)
- **B-10** zero Go-struct-field dot-notation patterns in any check `Detail` string (catches F-2; lint enforceable)
- **B-11** zero substep `action=complete` calls following `action=iterate` within same iteration without new tool evidence (catches F-3)
- **B-12** zero `zerops_guidance` unknown-topic responses + zero zero-byte guidance responses (catches F-5)
- **B-13** `zerops_knowledge` top-3 for canonical wire-contract queries includes the target atom (catches F-6)
- **B-14** zero `action=skip` attempts on mandatory steps per session — retry-exhaustion telemetry (catches F-4)

---

## 5. What PAUSE means in practice

**Halted**:
- v36 commissioning — no fresh showcase run until the fix stack closes ≥ 4 of 6 defects.
- C-15 (delete `recipe.md` monolith) — held until Cx-BRIEF-OVERFLOW lands. The monolith may still be the source of truth for language the atom tree hasn't fully captured; deleting it while the brief-delivery path is broken risks losing recoverable content.
- v35.5 minimal commission — held; minimal-tier Path B (main-inline writer) should not be exercised against the broken brief-delivery until we confirm F-1 doesn't manifest differently for inline-writer dispatches.

**Not halted**:
- **Front A measurement evaluators** (HANDOFF-to-I5) — still valuable; fills the T-1/T-8/T-9 definition-tightening gaps above. Lower urgency than the fix stack but useful baseline work.
- **Atom-tree edits** that close v35-surfaced content-authoring gotchas (if any) — the v35 run itself showed the atom corpus shape is correct (`fact_title` is named right in `manifest-contract.md`); no atom-tree work is called for unless the fix stack produces new needs.
- **Repository hygiene** — lint, test, catalog sync, platform version refresh.

**Gate to PROCEED**:
- Cx-BRIEF-OVERFLOW + Cx-CHECK-WIRE-NOTATION + Cx-ITERATE-GUARD landed with green tests.
- At least one of Cx-GUIDANCE-TOPIC-REGISTRY / Cx-KNOWLEDGE-INDEX-MANIFEST landed.
- Measurement evaluators for T-1 (tightened), T-8, T-9 (AFK-adjusted) implemented.
- **Then**: commission v36 as a full showcase run to confirm the fix stack under live conditions. v36's verdict, not v35's, determines PROCEED vs. further PAUSE.

---

## 6. Appendix — decision-path audit trail

| Step | What happened | Timestamp |
|---|---|---|
| 1 | v35 commissioned against `v8.105.0`; force-exported at deploy-step checker block | 2026-04-21 ~08:52 UTC |
| 2 | User handed off deliverable tree + SESSIONS_LOGS for analysis | 2026-04-21 later |
| 3 | Flow traces + dispatches extracted via `timeline.py` + `extract_flow.py` → `flow-main.md` + `sub-*.md` + `flow-dispatches/` (in this dir) | 2026-04-21 |
| 4 | Narrative analysis written → [`analysis.md`](analysis.md) | 2026-04-21 |
| 5 | Defect-class registry grew by 6 rows (16.1–16.6) | 2026-04-21 |
| 6 | Verdict written (this document) | 2026-04-21 |
| 7 | Fix-stack handoff → [`../HANDOFF-to-I6.md`](../HANDOFF-to-I6.md) | 2026-04-21 |

No engine-level fixes applied during the verdict window. The handoff-to-I6 document carries the fix-stack plan; implementation is deferred so that a fresh instance can pick up the work with full context intact.
