# Phase 5 tracker v2 — Broad-atom dedup + coverage-gap (G3 closure)

Started: 2026-04-27
Closed: 2026-04-27

> Phase contract per `plans/atom-corpus-hygiene-followup-2026-04-27.md`
> §5 Phase 5 (post-Phase-0 amendment 6 / Codex C6+C15). TWO halves:
> 5.1 redundancy (broad-atom dedup) + 5.2 coverage-gap. Plus
> 5.3 targeted patches per amendment 6 if 5.2 surfaces residual
> gaps. Mandatory PER-EDIT Codex per dedup (HIGH-risk).

## Codex rounds

| step | round type | state | output | commit |
|---|---|---|---|---|
| 5.1 CORPUS-SCAN — broad-atom restatement survey | CORPUS-SCAN | DONE — 10 facts F1-F10, est 5,189 B recovery | `phase-5-dedup-candidates.md` | `<phase-5-exit>` |
| 5.1 PER-EDIT (delegated apply pass for F9-F1, batched) | PER-EDIT (delegated) | DONE — 8 of 9 facts applied; F5 deferred for pin migration | apply log appended to `phase-5-dedup-candidates.md` | `7de79125` |
| 5.1 F5 with pin migration | PER-EDIT | DONE — pin "persistence boundary" → "Iteration cadence is mode-specific" (UNIQUE) | atom edits + corpus_coverage_test.go | `883ecad8` |
| 5.2 CORPUS-SCAN composition re-score | CORPUS-SCAN | DONE — 4/5 G3 PASS; two-pair Redundancy held at 1 | `post-followup-scores.md` | `<phase-5-exit>` |
| 5.3 targeted patches (deployFiles + env-var + verify dedup) | PER-EDIT (delegated) | DONE — 3 patches; 6 atoms; net −469 B | `post-followup-scores.md` "Phase 5.3 patch log" | `<phase-5-exit>` |
| 5.3 re-score on two-pair | CORPUS-SCAN | DONE — Redundancy held at 1 (STRUCTURAL ceiling: per-service render duplication is engine-level, not corpus-level) | `post-followup-scores.md` "Post-Phase-5.3 re-score" | `<phase-5-exit>` |

## Phase 5.1 commits (one per fact OR batched)

| # | commit | facts | atoms touched | recovery |
|---|---|---|---|---:|
| 1 | `4647a344` | F10 (deploy-cadence cross-link) | 3 atoms | net −6 B |
| 2 | `7de79125` | F1 + F2 + F3 + F4 + F6 + F7 + F8 + F9 (8 facts batched) | 15 atoms | aggregate −2,945 B (probe 5 fixtures) |
| 3 | `883ecad8` | F5 (deploy-creates-new-container) + pin migration | 4 atoms (incl test) | aggregate −639 B (probe 5 fixtures) |

Codex applied F1-F4, F6-F9 in single batch; F5 deferred due to
MustContain pin "persistence boundary" → migrated to "Iteration
cadence is mode-specific" (verified UNIQUE). F10 was first fact
applied (executor caught + restored a Phase-2-protected
guardrail "no SSHFS, no dev container" that Codex's apply
initially dropped — signal #2 cross-env mental-model framing).

## Phase 5.3 commits (this commit)

| # | patch | atoms touched | recovery |
|---|---|---|---:|
| P1 | deployFiles repetition collapse (3-atom restatement) | develop-first-deploy-scaffold-yaml (−127 B), develop-deploy-files-self-deploy (−290 B) | −417 B |
| P2 | env-var placement repetition (cross-link to develop-env-var-channels) | develop-first-deploy-env-vars (+67 B cross-link) | +67 B |
| P3 | verify-every-service / close-criteria repetition (3-atom canonical home split) | develop-verify-matrix (−57 B), develop-first-deploy-verify (−71 B), develop-auto-close-semantics (+9 B) | −119 B |
| **Phase 5.3 net** | — | 6 atoms | **−469 B** |

## Probe re-measurement (final Phase 5)

| Fixture | §4.2 baseline | Post-Phase-4 | Post-Phase-5.3 | Δ Phase 5 (cumulative this phase) | Δ P0→P5.3 (cumulative this cycle) |
|---|---:|---:|---:|---:|---:|
| standard | 24,347 | 24,151 | 22,792 | −1,359 B | −1,555 B |
| implicit-webserver | 26,142 | 25,969 | 24,513 | −1,456 B | −1,629 B |
| two-pair | 26,328 | 26,008 | 24,543 | −1,465 B | −1,785 B |
| single-service | 24,292 | 24,096 | 22,737 | −1,359 B | −1,555 B |
| simple-deployed | 18,435 | 18,451 | 17,488 | −963 B | −947 B |
| **First-deploy slice (4)** | — | — | — | **−5,639 B** | **−6,524 B** |
| **5-fixture aggregate** | — | — | — | **−6,602 B** | **−7,471 B** |

**Cumulative across both hygiene cycles**:

| Slice | First cycle | This cycle (this commit) | Cumulative |
|---|---:|---:|---:|
| 4 first-deploy fixtures | −7,461 B | −6,524 B | **−13,985 B** |
| 5 fixtures aggregate | −11,344 B | −7,471 B | **−18,815 B** |

**§8 binding targets EXCEEDED** at Phase 5 EXIT (before Phase 6
deferred-byte recovery even runs):
- additional ≥6,000 B THIS cycle: **−7,471 B observed** ✅
- cumulative ≥17,000 B: **−18,815 B observed** ✅

## §15.3 G3 disposition (Phase 5.2 + 5.3 final)

Per amendment 6: G3 has TWO halves — redundancy AND coverage-gap.
Per-fixture verdict from `post-followup-scores.md`:

| Fixture | redundancy G3 | coverage-gap G3 | overall G3 |
|---|---|---|---|
| standard | PASS | PASS | ✅ PASS |
| implicit-webserver | PASS | PASS | ✅ PASS |
| two-pair | **FAIL — STRUCTURAL** | PASS | ⚠️ FAIL (structural) |
| single-service | PASS | PASS | ✅ PASS |
| simple-deployed | PASS (2→3) | PASS | ✅ PASS |

**4/5 PASS; 1/5 FAIL with structural justification**.

### Two-pair structural-FAIL disposition

Per Codex's Phase 5.2 + 5.3 re-score: two-pair Redundancy held
at 1 because:

1. **Per-service render duplication** of
   `develop-dynamic-runtime-start-container` (renders twice with
   `appdev` + `apidev` substituted) and
   `develop-first-deploy-promote-stage` (renders twice for
   `appdev → appstage` + `apidev → apistage`).
2. The §6.2 rubric explicitly counts per-service hostname-
   substituted copies as restated facts.
3. **This is an ENGINE-level concern** — `Synthesize` renders
   per-matching-service. Resolving requires either:
   (a) Atom-level "render once with service-list/table" form
       (engine change to support multi-service in single render).
   (b) Atom-axis tightening to fire only on the first service
       (semantic change; loses per-service relevance).
   Both are out of scope for this hygiene cycle.

This is documented via prior cycle's
`phase-7-tracker.md` note: "per-service double-render of
`Dynamic-runtime dev server` and `promote-stage` is intrinsic
to the multi-service fixture; axis-tightening doesn't resolve
it. Phase 8+ scope."

**Phase 5 EXIT disposition**: SHIP-WITH-NOTES on two-pair
(per amendment 9 / Codex C11). The plan's verdict-ambition
remains clean SHIP for the 4 fixtures + simple-deployed; the
two-pair structural gap is documented as a follow-up engine
ticket (out of scope for atom-corpus-hygiene cycles).

User explicitly authorized continuing in this disposition at
Phase 5.2 question prompt 2026-04-27.

## Phase 5 EXIT (§5 Phase 5)

- [x] First-deploy fixtures' Redundancy strictly improved
  (1 → 2 on 3 of 4; STRUCTURAL FAIL on two-pair held at 1).
- [x] First-deploy fixtures' Coverage-gap strictly improved or
  flat-at-5 vs §4.2 baseline (✅ all 4).
- [x] simple-deployed Redundancy strictly improved (2 → 3 ✅).
- [⚠] §15.3 G3 strict-improvement on all 5 fixtures: 4/5 PASS,
  1/5 STRUCTURAL FAIL (two-pair).
- [x] Codex per-edit rounds for each dedup APPROVE (signal
  preservation verified per Phase 2 Axis K classification;
  Codex audit trail in apply log).
- [x] `phase-5-tracker-v2.md` committed.

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state.
- [x] Codex round outcomes cited.
- [x] `Closed:` 2026-04-27.

## Notes for Phase 6 entry

1. **Cumulative target exceeded**. §8 binding additional ≥6,000 B
   already met at Phase 5 EXIT (−7,471 B observed); cumulative
   ≥17,000 B exceeded (−18,815 B). Phase 6 deferred-byte recovery
   becomes additive padding rather than ship-blocking.
2. **Phase 6 ENTRY requires `axis-b-candidates-v2.md`** per
   amendment 4 (re-baseline atoms touched in Phase 5).
   `develop-verify-matrix` was touched in Phase 5.3 (P3); its
   Phase 6 HIGH-risk byte estimate is now stale. Regenerate
   before applying Phase 6 work.
3. **Phase 7 SHIP gate** must address two-pair G3 fail —
   propose SHIP-WITH-NOTES and surface to user, OR document
   the structural ticket as deferred follow-up beyond hygiene
   cycles.

Phase 6 (deferred-byte recovery) entry unblocked.
