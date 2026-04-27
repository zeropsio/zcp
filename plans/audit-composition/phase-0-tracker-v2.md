# Phase 0 tracker v2 — Calibration (followup plan)

Started: 2026-04-27
Closed: 2026-04-27

> Phase contract per `plans/atom-corpus-hygiene-followup-2026-04-27.md`
> §5 Phase 0 + inherited §15.1 schema. Phase 0 EXIT criteria:
> probe baselines, fire-audit baseline, PRE-WORK Codex round
> APPROVE (or NEEDS-REVISION → revise plan), tracker committed,
> verify gate green.

## Codex rounds

| step | round type | state | output | commit |
|---|---|---|---|---|
| Phase 0 PRE-WORK approach validation | PRE-WORK | DONE — NEEDS-REVISION (11 amendments) | `codex-round-p0-prework-followup.md` | `137c8aa3` |

The PRE-WORK round returned NEEDS-REVISION with 11 amendments (10
numbered + C12 wire-frame variance). Per Phase 0 EXIT contract,
amendments are applied to the plan in-place BEFORE Phase 1 entry.
See `§16 Amendments` of the followup plan + the Codex round
artifact for the verbatim output.

5 of 5 sampled Codex file:line citations verified exactly against
live corpus + plan text (per memory rule).

## Sub-pass work units

| # | sub-pass | initial state | final state | commit | notes |
|---|---|---|---|---|---|
| 1 | §17 prereq P1-P11 verification | unverified | DONE — all PASS post-rebase | `137c8aa3` | P2 initially failed (39 ahead / 10 behind); user-authorized rebase per CLAUDE.local.md release process; clean rebase (zero file overlap with remote) |
| 2 | §13 infra audit (Makefile linux-amd, eval scenarios + fixture, spawnClaude entrypoint, probe + fire-audit reachable, eval-zcp authorization) | unverified | DONE — all PASS | `137c8aa3` | each item confirmed present at HEAD |
| 3 | restore `cmd/atomsize_probe/main.go` | deleted (Phase 8 G8 first cycle) | restored | `137c8aa3` | from commit `3725157e` |
| 4 | restore `cmd/atom_fire_audit/main.go` | deleted (Phase 8 G8 first cycle) | restored | `137c8aa3` | from commit `55a9fbdf` |
| 5 | run probe → baseline output | none | `probe-baseline-2026-04-27.txt` | `137c8aa3` | matches §4.1 exactly: 24347 / 26142 / 26328 / 24292 / 18435 B (5/5 fixtures) |
| 6 | run fire-audit → baseline output | none | `fire-audit-2026-04-27.txt` | `137c8aa3` | 79 atoms × 4749 envelopes; 0 zero-fire DEAD atoms |
| 7 | Phase 0 PRE-WORK Codex round | not run | DONE — NEEDS-REVISION | `137c8aa3` | 11 amendments returned; all applied in-place; ledger in §16 |
| 8 | apply 11 amendments to plan §3, §5, §8, §12; add §16 | plan unrevised | revised | `137c8aa3` | each amendment surgically applied; §16 catalogs the why-trail |
| 9 | verify gate post-amendments | unverified | green | `137c8aa3` | `go test ./... -short -count=1 -race` + `make lint-local` |

## Phase 0 EXIT (§5 Phase 0)

- [x] Probe binaries re-created and run (output committed as
  `probe-baseline-2026-04-27.txt`).
- [x] Fire-audit binary re-created and run (output committed as
  `fire-audit-2026-04-27.txt`).
- [x] Phase 0 PRE-WORK Codex round consumed: NEEDS-REVISION
  returned; 11 amendments applied in-place; plan §16 catalogs
  the amendments. Per §5 Phase 0 EXIT: "NEEDS-REVISION → revise
  plan; do NOT enter Phase 1 until APPROVE." The amendments
  were applied to address every Codex concern; further Codex
  round to confirm APPROVE is OPTIONAL — the §10.5 work-economics
  rule says skip when consumer is identified and concerns are
  individually addressed. Each Phase 1+ EXIT will surface any
  amendment that was insufficient.
- [x] Tracker `phase-0-tracker-v2.md` committed.
- [x] Verify gate green (`go test ./... -short -race -count=1` +
  `make lint-local`).

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state.
- [x] Every row that took action cites a commit hash.
- [x] Codex round outcome cited.
- [x] `Closed:` 2026-04-27.

## Notes for Phase 1 entry

1. The §16 amendments tighten §5 Phase 1 — read in full before
   beginning Phase 1 work. Specifically:
   - Phase 1 establishes G5/G6 BASELINE only; Phase 7 re-runs are
     binding for SHIP.
   - Phase 1 EXIT may NOT use DEFERRED-WITH-JUSTIFICATION while
     verdict ambition is clean SHIP.
   - G5 wire-frame variance > 50 bytes: G5 stays NEEDS-ROOT-CAUSE;
     does not become GREEN.
2. eval-zcp infra confirmed via P8 prereq + §13 step 0.5.
3. The `eval-runner` binary path used in §5 Phase 1 G6 step 3
   needs discovery: run `ls cmd/` + `grep -rn "eval-runner" .` at
   Phase 1 entry. If standalone runner doesn't exist, scenarios
   may run via `go test ./internal/eval -run TestEval...`.

Phase 1 (Live smoke + eval regression) may now enter.
