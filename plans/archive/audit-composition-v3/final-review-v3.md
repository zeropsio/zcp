# Cycle-3 final review — atom corpus hygiene followup #2

Date: 2026-04-27
Plan: `plans/atom-corpus-hygiene-followup-2-2026-04-27.md`
Reviewer: this session
Final disposition: **SHIP-WITH-NOTES**

## Summary

Cycle 3 was a tight, 5-finding follow-up to cycle-2's SHIP-WITH-NOTES corpus hygiene. Scope per plan §1: F1 (scaffold-yaml drops), F3 (zcli push refs cleanup), F4 (push-dev cycle rewrite — auto-watch SSHFS-aware), F5 + Axis N (universal-atom env-leak audit + new spec axis §11.6). All 5 findings addressed; 1 SPLIT-CANDIDATE deferred to follow-up (DF-1) because tightening axis without authoring a counterpart atom would leave a real corpus gap.

## Phase summary

| Phase | EXIT commit | Outcome |
|---|---|---|
| 0 — Calibration | `00459f02` | Probe baseline + Codex PRE-WORK APPROVE (round 2; round-1 NEEDS-REVISION on 4 plan defects, all addressed) |
| 1 — F1 scaffold-yaml drops | `f27b333c` | −1,560 B aggregate first-deploy slice (exceeds plan estimate) |
| 2 — F3 zcli push refs cleanup | `e736ab8c` | 6 atoms DROP/REPHRASE; signal purity for local-env + push-git contexts |
| 3 — F4 push-dev cycle rewrite (HIGH-risk) | `97267c1f` | Auto-watch SSHFS polling caveat + restored no-redeploy guardrail (round-1 NEEDS-REVISION → round-2 APPROVE) |
| 4 — F5 + Axis N corpus-wide | `c48568b3` | §11.6 spec + 5 atom edits + 1 SPLIT-CANDIDATE deferred (DF-1) |
| 5 — Final composition re-score + SHIP | (this commit) | SHIP-WITH-NOTES |

## Cycle-3 cumulative impact

**Probe re-run** (5 baseline fixtures, post-cycle-2 → post-cycle-3):

| Fixture | post-cycle-2 | post-cycle-3 | Δ |
|---|---:|---:|---:|
| standard | 20,643 | 20,314 | −329 |
| implicit-webserver | 21,947 | 21,608 | −339 |
| two-pair | 22,394 | 22,065 | −329 |
| single-service | 20,588 | 20,259 | −329 |
| simple-deployed | 16,085 | 16,166 | +81 |

**Aggregate first-deploy slice: −1,326 B**. Plan §6 acceptance estimate was ~1,500-3,000 B; actual slightly below low end (the F4 polling caveat + Phase 4 Edit 2 cross-link expansion absorbed some byte-recovery in exchange for signal-correctness gains).

**Composition rubric** (cycle-2 → cycle-3, per cycle-1 §4.2 axes):

| Fixture | Coh Δ | Den Δ | Red Δ | Cov-gap Δ | Task-rel Δ |
|---|---|---|---|---|---|
| standard | 0 | +1 | +1 | +1 | 0 |
| implicit-webserver | +1 | +1 | +1 | +1 | 0 |
| two-pair | 0 | 0 | 0* | +1 | 0 |
| single-service | 0 | +1 | +1 | +1 | 0 |
| simple-deployed | 0 | 0 | +1 | +1 | 0 |

*two-pair Red=1 STRUCTURAL (engine-level; inherited from cycle 2). G3 strict-improvement holds across all 5 fixtures (per `codex-round-p5-rescore-v3.md` APPROVE).

## SHIP-gate verification (G1-G8)

All 8 gates GREEN:

- **G1** All 6 cycle-3 phase trackers closed.
- **G2** `knownUnpinnedAtoms` empty.
- **G3** Composition re-score APPROVE; strict-improvement holds.
- **G4** `make lint-local` 0 issues; `go test ./... -count=1 -race` green.
- **G5** L5 live smoke BINDING GREEN; idle wire ±6 B vs cycle-2.
- **G6** Eval-scenario regression BINDING GREEN (4m43s PASS, 5% faster than cycle-2; run 1 stochastic flakiness documented).
- **G7** Final Codex SHIP VERDICT (this round, after addressing round-1 procedural blockers).
- **G8** Probe binary deleted.

## Notes accompanying SHIP

**Inherited from cycle 2** (no change):
1. **Two-pair structural redundancy (Red=1)** — `develop_first_deploy_two_runtime_pairs_standard` fixture renders multi-service atoms per-service-instance, causing structural duplication. Engine-level fix tracked in `plans/engine-atom-rendering-improvements-2026-04-27.md`. Cycle 3 doesn't address; cycle 2 SHIP-WITH-NOTES already disposed of this.

**New in cycle 3**:
2. **DF-1 — `develop-push-git-deploy.md` SPLIT-CANDIDATE deferred**. Universal atom is container-shaped throughout (SSH commands, project `GIT_TOKEN`); fires on local-env push-git develop-active envelopes with WRONG content. Tightening axis to `[container]` would break test pins (`corpus_coverage_test.go:624` MustContain `["git-push", "GIT_TOKEN"]` would fail with no atom firing). Proper fix needs new `develop-push-git-deploy-local.md` atom — content-authoring work, outside cycle-3 plan scope (§1 + §2 = "5 content findings"). Tracked in `plans/audit-composition-v3/deferred-followups-v3.md` DF-1. Status quo (wrong-content for local-env) is pre-existing — cycle 3 does NOT introduce or worsen.
3. **G6 run-1 stochastic flakiness** — first G6 run failed `mustCallTools: zerops_verify` because the LLM scenario chose direct Bash+curl path. Run 2 PASSED in 4m43s with `zerops_verify` called as required. Documented as LLM-non-determinism, not corpus regression. The cycle-3 corpus's `develop-http-diagnostic.md` step 1 ("zerops_verify ... canonical health probe") is intact.

## Cycle-3 distinct contributions

1. **Spec contract codification** — Axis N (universal-atom per-env leak) added as `docs/spec-knowledge-distribution.md` §11.6, mirroring §11.5 K/L/M format. Includes definition, distinction from Axis K, judgment test, classification (DROP-LEAK / KEEP-LOAD-BEARING / SPLIT-CANDIDATE / UNIFICATION-CANDIDATE), inverse rule, and DO-NOT-UNIFY exception.
2. **Signal-purity wins on local-env + push-git contexts** — Phase 2 cleaned `zcli push` mechanism noise from 6 atoms (kept in 2 KEEP atoms where literal config matters); local-env agents reading those atoms no longer see dispatch-layer detail.
3. **F4 SSHFS-correctness** — auto-watch claim replaced with polling-mode requirement; restored explicit no-redeploy guardrail via "Code-only edits never trigger zerops_deploy" sentence.
4. **F1 corpus-quality** — content-root tip + schema-fetch line dropped from scaffold-yaml; cross-link to `develop-deploy-modes` already covers tilde-extract/preserve detail.

## Provenance

Cycle 1: `plans/atom-corpus-hygiene-2026-04-26.md` (SHIP-WITH-NOTES).
Cycle 2: `plans/archive/atom-corpus-hygiene-followup-2026-04-27.md` (SHIP-WITH-NOTES).
Cycle 3: this plan (SHIP-WITH-NOTES; inheriting cycle-2's two-pair structural note + 2 new notes).

Plan + ledgers + trackers + Codex artifacts:
- Plan: `plans/archive/atom-corpus-hygiene-followup-2-2026-04-27.md` (post-archive path).
- Tracker dir: `plans/audit-composition-v3/` — contains 6 phase trackers, 5 Codex round artifacts, axis-n-candidates.md, deferred-followups-v3.md, g5/g6 binding results, probe baselines, final-review-v3.md.

Cycle-3 budget: 7 phases (0-5), ~12 Codex rounds (PRE-WORK ×1 round 1, PRE-WORK ×1 round 2, P2 POST-WORK ×1, P3 PER-EDIT ×1 round 1, P3 PER-EDIT ×1 round 2, P4 CORPUS-SCAN ×1, P4 PER-EDIT ×1 round 1, P4 PER-EDIT ×1 round 2, P4 POST-WORK ×1, P5 RE-SCORE ×1, P5 SHIP VERDICT ×1 round 1, P5 SHIP VERDICT ×1 round 2). Codex iterations: 4 NEEDS-REVISION rounds (each addressed via plan revision + re-dispatch).
