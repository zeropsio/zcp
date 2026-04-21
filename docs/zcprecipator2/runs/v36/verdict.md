# v36 verdict — ACCEPT-WITH-FOLLOW-UP (Cx-stack works; close-step gate needs hardening before v37)

**Run**: nestjs-showcase v36 against tag `v8.108.0` + hotfix `v8.108.1` (Cx-PLAN-NIL-GUIDANCE)
**Close date**: 2026-04-21 (session `7743c6d8c8a912fd` / Claude UUID `8271b0d3-57d1-4dac-a17c-a36181150071`)
**Outcome reached**: finalize-complete. Close step never called; deliverable tree exported via sessionless `zcp sync recipe export`.
**Verdict applied**: **ACCEPT-WITH-FOLLOW-UP** — one targeted patch (Cx-CLOSE-STEP-GATE) + v37 commission as second-confirmation.
**Authored**: 2026-04-21 post-analysis

---

## 1. Decision

**ACCEPT-WITH-FOLLOW-UP before v37**.

The v8.108.0 Cx-fix-stack works. F-1..F-6 are all closed (PASS) or UNREACHED-cleanly-fix-shipped under live v36 conditions. Zero ROLLBACK T-triggers fire. The writer_manifest_completeness blocker that ate v35 is gone; writer needs 1 fix pass (vs 3 on v35). The F-7 regression that ate v36-attempt-1 is closed by the in-window Cx-PLAN-NIL-GUIDANCE hotfix (v8.108.1), confirmed live at rows 5 and 6 of the main session.

The gating observation is a **coverage gap, not a regression**: close-step, editorial-review, code-review, and close-browser-walk all went unexercised because the agent elected to export via `zcp sync recipe export` without the `--session` flag, which emits the note `"skipping close-step gate"` and produces a `.tar.gz` regardless. This is F-8 in [`analysis.md`](analysis.md#f-8-close-step-gate-bypass-via-sessionless-export) — a new defect class v36 surfaced that the Cx stack doesn't cover because it sits in the export CLI, not the workflow engine.

Before v37, one focused patch: make the close-step gate non-bypassable (either enforce `--session` or detect a live recipe session and refuse sessionless export). Then commission v37 as a second-confirmation run that measures §7a editorial-review + §8 close-browser-walk + §11a reclassification-delta bars.

**Full analysis**: [`analysis.md`](analysis.md).
**Defect rows**: [`../../05-regression/defect-class-registry.md §16.1–16.8`](../../05-regression/defect-class-registry.md).
**Fix-stack handoff that drove v36**: [`../HANDOFF-to-I6.md`](../HANDOFF-to-I6.md).
**Analysis handoff this verdict was authored against**: [`../HANDOFF-to-I7-v36-analysis.md`](../HANDOFF-to-I7-v36-analysis.md).

---

## 2. Why not ROLLBACK or ROLLBACK-Cx

[`rollback-criteria.md §1`](rollback-criteria.md) walks 13 T-triggers. **Zero fire**:

| Trigger | v36 observed | Fires? |
|---|---|---|
| T-1 Deploy rounds | 2 (threshold > 2) | no (at-threshold, within gate) |
| T-2 Finalize rounds | 1 (threshold > 1) | no (at-threshold) |
| T-3 Substrate invariant | 0 events | no |
| T-4 Env-var coordination | all 3 codebases match | no |
| T-5 Manifest-honesty v34 class | not measurable (close unreached) | not measurable |
| T-6 Phantom output tree | 0 | no |
| T-7 Self-inflicted gotchas shipped | 0 (env 4 + v2/v3 caught by finalize, fixed) | no |
| T-8 Very-long bash ≥ 120 s | 0 (spot-check) | no |
| T-9 Wall clock (active-work, tightened) | ~1:45 (showcase threshold > 120 min) | no |
| T-10 Content-tree lint | green | no |
| T-11 Editorial-review CRIT shipped | not measurable | not measurable |
| T-12 Reclassification delta | not measurable | not measurable |
| T-13 (NEW) Cx-commit runtime regression | 0 | no |

ROLLBACK-Cx (per HANDOFF-to-I7 §Decision-tree) requires evidence that a Cx-commit introduced a new regression. v36 shows the opposite: Cx-PLAN-NIL-GUIDANCE fixed the one guidance-path defect seen pre-v36 (F-7), and the five HANDOFF-to-I6 commits closed their targets as specified.

---

## 3. Why not PAUSE

[`rollback-criteria.md §2`](rollback-criteria.md) walks 10 PAUSE triggers. Two fire weakly:

- **P-3 (downstream-facts < 2)**: v36 has 1 fact with `scope=downstream` (agent-browser instability). Below the 2-threshold, but so was v35 (1). The other observations (execOnce, env_self_shadow, Meilisearch ESM) were recorded as `scope=content` / `gotcha_candidate` — the agent's classification is defensible. Signal-grade, not PAUSE-class.
- **P-4 (TodoWrite full-rewrites > 1)**: 7 rewrites across ~2 h. Not backfills (which is what C-11 gates); aligned with phase transitions. Agent-habit signal, not architecture regression.

Both fold into the ACCEPT-WITH-FOLLOW-UP lane as atom-tree tweaks (P-3: sharpen Theme-B coaching in `briefs.writer.fact-recording-discipline` atom) — not v37-blocking.

---

## 4. Why not PROCEED (to v37 without patch)

The dominant v36 observation is F-8: **close-step gate bypassable by sessionless export**. If v37 is commissioned without a patch, the agent retains the same escape hatch. Editorial-review, code-review, and close-browser-walk would remain at risk of being skipped by agent election. Since:

- §7a editorial-review is the *spec-prescribed* reviewer role whose absence post-v8.105 is the #1 open question.
- §11a reclassification-delta is the T-12 surface — the only mechanism that catches writer classification errors at source.
- §8 close-browser-walk is the post-review verification that v34 got right and post-v34 runs have avoided measuring.

…the value of v37 depends on close-phase actually running. Patch first, then commission.

---

## 5. What v36 closed (fix-stack empirical outcomes)

Grade per HANDOFF-to-I7 taxonomy: PASS (closed under triggering condition), REGRESSED-CLOSURE (closure mechanism failed), NEW-CLASS (new defect surfaced), UNREACHED (triggering condition didn't occur).

| # | Defect | v35 behavior | v36 observation | Closed on v36? |
|---|---|---|---|---|
| F-1 | Dispatch brief overflow | 71 KB response → spillover → broken writer dispatch; 3 writer passes | Envelope fired at readmes; 15 dispatch-brief-atom calls; max 11.5 KB per response; 1 writer + 1 cosmetic fix pass | **PASS** |
| F-2 | Check Detail Go notation | 11× `FactRecord.Title` in check detail; 14 envelope variants retried | 0× Go struct.field in any check detail (grepped). Writer atom names both `fact_title` + `FactRecord.Title` as pedagogy (intentional) | **PASS** |
| F-3 | Iterate fake-pass | 12 substeps walked in 84 s with no tool work | No iterate calls issued; guard never triggered; build-time test stands in | **UNREACHED (fix-shipped)** |
| F-4 | Skip on mandatory step | not attempted on v35 | 1× skip attempted (deploy/browser-walk); engine refused; agent recovered via fact-record + fallback curl | **PASS (gate held; recovery worked)** |
| F-5 | Unknown-topic / zero-byte guidance | 3× hallucinated topics | 0× unknown, 0× does-not-apply, 0× TOPIC_EMPTY; Cx-PLAN-NIL-GUIDANCE confirmed live at research | **PASS** |
| F-6 | Knowledge misses manifest atoms | `choose-queue` top hit for manifest-schema | Agent didn't issue wire-contract knowledge queries (envelope delivered them directly); build-time test stands in | **UNREACHED-no-query (fix-shipped)** |
| F-7 (NEW pre-run) | Predicate-gated topics on nil plan → misleading "does not apply" at research | v36-attempt-1 died 4× on this | Cx-PLAN-NIL-GUIDANCE (`c512757`, v8.108.1) closed pre-run; confirmed live | **PASS-pre-run** |
| F-8 (NEW) | Close-step gate bypassable via sessionless export | (v36 first to reach finalize, first to surface) | Agent exported via `zcp sync recipe export` without `--session` → "skipping close-step gate" → .tar.gz produced; editorial-review + code-review + close-browser-walk never run | **OPEN — signal-grade; Cx-CLOSE-STEP-GATE patch needed** |

Five closures, two new classes. The two new classes split clean by domain: F-7 was an engine bug (closed by in-window hotfix); F-8 is a CLI contract bug (queued for v8.108.2 patch).

---

## 6. New bars added to the measurement sheet

| Bar | Closes | Source |
|---|---|---|
| **B-15** Close-step gate enforcement: `zcp sync recipe export` refuses sessionless execution when a live recipe session exists for the target directory | F-8 | [`calibration-bars.md §10`](calibration-bars.md) |

Other bars in [`calibration-bars.md`](calibration-bars.md) that were signal-grade on v35 are now graduated to gate-grade post-v36 per the inline notes:
- B-9 (response size ≤ 32 KB): evaluated live; passed at 11.5 KB max. Gate ready.
- B-10 (check detail uses JSON key notation): passed; lint test merged.
- B-12 (unknown/zero-byte guidance == 0): passed live.
- B-14 (skip-attempts on mandatory): 1 attempt but gate held + recovery clean; now `[signal]` confirmed behavior.

---

## 7. Halted / not-halted for v37

**Halted** until Cx-CLOSE-STEP-GATE lands:
- v37 showcase commissioning.
- Any close-phase coverage measurement depending on editorial-review / code-review / close-browser-walk running.

**Not halted**:
- v35.5 minimal commissioning (previously held pending fix stack — now **unblocked** per v36 PROCEED-on-F-1..F-6). Minimal-tier Path B (main-inline writer) can proceed independently; its close-step gate concern is the same as v37's and would be addressed by the same patch. Can also wait for the patch, owner's call.
- Repository hygiene: atom-tree lint, test suite, catalog sync, platform version refresh.
- Front A measurement evaluators (T-8, T-9 scripted): still useful, lower urgency.
- Atom-tree tweak for P-3 downstream-facts signal: `briefs.writer.fact-recording-discipline` atom could be sharpened.

---

## 8. Gate to PROCEED on v37

- Cx-CLOSE-STEP-GATE landed with green tests. Minimum shape: `zcp sync recipe export` exits non-zero when invoked without `--session` AND a live recipe session exists for the `--app-dir`. Alternative: export requires prior `zerops_workflow action=complete step=close` for the session. User picks the shape.
- Tag v8.108.2 released.
- v37 commission against v8.108.2. v37 must reach close-step complete (not just finalize). The editorial-review + code-review + close-browser-walk sub-agents must dispatch.
- **Then**: v37's verdict, not v36's, decides ultimate PROCEED on the architecture. If v37 closes §7a + §8 + §11a cleanly, the zcprecipator2 rewrite is fully validated.

---

## 9. Appendix — decision-path audit trail

| Step | What happened | Timestamp |
|---|---|---|
| 1 | HANDOFF-to-I6 fix-stack delivered as v8.108.0 (5 Cx-commits) | 2026-04-21 AM |
| 2 | v36-attempt-1 commissioned (session `43814d9c5e09e85d`); died at research-step on "does not apply" (F-7) | 2026-04-21 ~13:45 UTC |
| 3 | F-7 root-caused in `internal/tools/guidance.go`; Cx-PLAN-NIL-GUIDANCE fix + tests + release | 2026-04-21 14:00–14:06 UTC |
| 4 | v8.108.1 tagged | 2026-04-21 14:06 UTC |
| 5 | v36 commissioned against v8.108.1 (session `7743c6d8c8a912fd`) | 2026-04-21 14:14 UTC |
| 6 | v36 reached finalize-complete; 6 subagents dispatched (3 scaffold + 1 feature + 1 writer + 1 writer-fix); close never called; sessionless export produced `.tar.gz` | 2026-04-21 16:04 UTC |
| 7 | User handed off deliverable tree + SESSIONS_LOGS for analysis | 2026-04-21 ~18:00 UTC |
| 8 | Flow traces extracted (208 main events, 291 sub-agent events, 6 dispatches) | 2026-04-21 ~18:45 UTC |
| 9 | Per-defect evidence scan: F-1..F-6 graded (PASS / UNREACHED-fix-shipped); F-7 (pre-run-closed) + F-8 (new open signal) registered | 2026-04-21 ~19:00 UTC |
| 10 | README + analysis + calibration-bars + rollback-criteria + this verdict authored | 2026-04-21 ~19:30 UTC |
| 11 | Registry rows 16.7 (F-7) + 16.8 (F-8) appended | ≤ commit |
| 12 | Single `docs(zcprecipator2): v36 run analysis + verdict` commit lands | next |

No engine-level fixes applied during this verdict window. The Cx-CLOSE-STEP-GATE patch is queued for a separate commission of a fresh instance with its own handoff (HANDOFF-to-I8-v36-followup or similar).
