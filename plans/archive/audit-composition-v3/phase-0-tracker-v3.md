# Phase 0 tracker — Calibration (cycle 3)

Started: 2026-04-27
Closed: 2026-04-27

Plan: `plans/atom-corpus-hygiene-followup-2-2026-04-27.md` §5 Phase 0.

## §17 prereq verification

| # | prereq | status | note |
|---|---|---|---|
| P1 | Working tree clean | PASS | `git status` empty pre-Phase-0 |
| P2 | Branch current with origin | PASS (post-rebase) | Local was 62 ahead / origin 5 ahead pre-prereq. `git pull --rebase origin main` clean (5 origin commits in `internal/recipe/`, `internal/ops/`, `internal/tools/deploy_*`, `docs/zcprecipator3/`, `docs/spec-content-surfaces.md` — no overlap with cycle-3 work surface). Post-rebase HEAD: `76238d56` (was `f08bb34a`); cycle-2 PLAN COMPLETE: `4e0d4d5f` (was `281fb79f`). |
| P3 | Prior trim plan exists | PASS | `plans/atom-corpus-context-trim-2026-04-26.md` |
| P4 | Probe source reachable in git | PASS | `3725157e:cmd/atomsize_probe/main.go` byte-identical to `9c8980b9~1:cmd/atomsize_probe/main.go` (228 lines). |
| P5 | CLAUDE.md + CLAUDE.local.md readable | PASS | both present |
| P6 | Auto-memory codex-verify entry | PASS | `feedback_codex_verify_specific_claims.md` present |
| P7 | `codex:codex-rescue` subagent available | DEFERRED-TO-PRE-WORK | Subagent listed in registry; cycle-2 completed using it (known-working). Live test via Phase 0 PRE-WORK round (this phase). |
| P8 | VPN + SSH to eval-zcp | PASS | `ssh -o StrictHostKeyChecking=no zcp 'echo ok'` → `ok` |
| P9 | `make lint-local` green | PASS | `0 issues.` recipe atom lint + atom-template-vars lint + golangci-lint all clean. Re-run post-rebase: still PASS. |
| P10 | `go test ./... -short -count=1 -race` green | PASS | All packages OK. Re-run post-rebase: still PASS. |
| P11 | Empirical §4 still matches reality | PASS (count) / DRIFT-INFO (cycle-1 §4.2) | 79 atoms total ✅. Cycle-1 §4.2 pin coverage drifted: 0% zero-pins now vs 86% baseline — IMPROVEMENT (cycles 1-2 added pins), not regression. Cycle-3 §4.1 baseline (atom count + 5 fixture body sizes) is what gates this cycle. |

## Step 0.5 corpus baseline check (cycle-3 plan §4.1)

Probe re-built from `cmd/atomsize_probe/main.go` (recreated from `9c8980b9~1`, byte-identical to `3725157e` ref in plan). All 5 fixture body sizes match §4.1 EXACTLY (0% drift):

| Fixture | §4.1 | re-rendered | match |
|---|---:|---:|---|
| develop_first_deploy_standard_container | 20,643 | 20,643 | EXACT |
| develop_first_deploy_implicit_webserver_standard | 21,947 | 21,947 | EXACT |
| develop_first_deploy_two_runtime_pairs_standard | 22,394 | 22,394 | EXACT |
| develop_first_deploy_standard_single_service | 20,588 | 20,588 | EXACT |
| develop_simple_deployed_container | 16,085 | 16,085 | EXACT |

Atom count: 79 (matches §4.1).

## Phase 0 work units

| # | work unit | initial state | final state | commit | codex round | notes |
|---|---|---|---|---|---|---|
| 1 | §17 prereq P1-P11 walked | unverified | PASS (P2 rebased, P7 deferred to PRE-WORK round, P11 cycle-1 §4.2 drift = improvement) | 00459f02 | – | per §17 table above |
| 2 | Step 0.5 baseline re-render | uncomputed | PASS (5/5 fixture sizes match §4.1 EXACTLY) | 00459f02 | – | per Step 0.5 table above |
| 3 | Probe binary recreated | absent (deleted G8 cycle-2) | rebuilt to /tmp/probe; source at `cmd/atomsize_probe/main.go` | (uncommitted; G8 to delete at Phase 5 EXIT) | – | source from `9c8980b9~1` |
| 4 | Tracker dir + probe baseline file | absent | `plans/audit-composition-v3/probe-baseline-2026-04-27-v3.txt` written (146 lines, 5 fixtures) | 00459f02 | – | text dump of probe stdout |
| 5 | Phase 0 PRE-WORK Codex round 1 | not run | NEEDS-REVISION | – | NEEDS-REVISION | 4 real concerns surfaced (F3 platform-rules-local row over-drops; F4 rewrite drops diagnostic fields; F5 static-workflow has 2nd leak at L27-28; Axis N inverse rule needs DO-NOT-UNIFY exception). Plan revised; round 2 dispatched. Detail: `codex-round-p0-prework-v3.md`. |
| 6 | Plan revisions per round 1 (4 edits to plan §3 + §5 Phase 2/3/4) | not applied | APPLIED | 00459f02 | – | §3 DO-NOT-UNIFY exception; §5 Phase 2 platform-rules-local REPHRASE not DROP; §5 Phase 3 F4 retains response fields; §5 Phase 4 F5 (b) added |
| 7 | Phase 0 PRE-WORK Codex round 2 | not run | APPROVE | 00459f02 | APPROVE | All 4 plan revisions confirmed correct; all round-1 APPROVE items still hold (F1, F3 [other 7], F5 base, Axis K vs N distinction, Axis N example quality). Detail: `codex-round-p0-prework-v3.md` Round 2 section. |

## Phase 0 EXIT readiness (per §5 Phase 0 EXIT)

- [x] Probe baseline output committed to `plans/audit-composition-v3/probe-baseline-2026-04-27-v3.txt`. 00459f02
- [x] Phase 0 PRE-WORK Codex round APPROVE. (round 2 APPROVE 2026-04-27 after round-1-driven plan revisions)
- [x] Tracker `phase-0-tracker-v3.md` committed. 00459f02
- [x] Verify gate green. (lint+tests green pre-Phase-0 + re-run post-rebase + re-run pre-commit; see commit message)

## Notes / amendments

- §10 Step 0 mentions `PROBE_DUMP_DIR=...` for per-fixture markdown
  dump. The probe at `9c8980b9~1` (= `3725157e`) does NOT support
  that env var. The text output (sizes + per-atom listing) is
  sufficient for §4.1 baseline + Phase 5 re-score per §5 Phase 5
  Step 1. No per-fixture markdown dump needed at this phase.
- Probe binary lives at `cmd/atomsize_probe/`; will be deleted per G8
  at Phase 5 EXIT (mirroring cycle-1 G8 + cycle-2 §15.3 G8).
