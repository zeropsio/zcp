# Phase 2 tracker v2 — Axis K (abstraction-leak) sweep (followup plan)

Started: 2026-04-27
Closed: 2026-04-27

> Phase contract per `plans/atom-corpus-hygiene-followup-2026-04-27.md`
> §5 Phase 2 (post-Phase-0 amendments 1 + 8). HIGH-risk leaks get
> mandatory Codex PER-EDIT review; LOW-risk DROPs go in
> `axis-k-drops-ledger.md`; POST-WORK Codex round samples
> trigger-term-flagged rows.

## Codex rounds

| step | round type | state | output | commit |
|---|---|---|---|---|
| Phase 2 CORPUS-SCAN — abstraction-leak survey | CORPUS-SCAN | DONE — 79 leaks found (8 DROP, 11 REPHRASE, 60 KEEP) | `axis-k-candidates.md` | `f2825dc5` |
| Phase 2 PER-EDIT — REPHRASE diff review (batched single round on full diff) | PER-EDIT | DONE — APPROVE on all 11 atoms | `codex-round-p2-peredit-rephrase.md` | `66f49f4d` |
| Phase 2 POST-WORK — gap-find on Phase 2 commits | POST-WORK | DONE — initial NEEDS-REVISION (one ledger trigger-term miss) → APPROVE post-remediation | `codex-round-p2-postwork.md` | `<phase-2-exit-commit>` |

Per amendment 1: PER-EDIT was MANDATORY for the 11 REPHRASE atoms
(each has a HIGH-risk signal). Single batched round (not 11
separate) used per §10.5 work-economics rule #3 — the
CORPUS-SCAN already provided per-leak proposals; PER-EDIT
validates the post-edit text preserves each proposal's signal.
Codex APPROVE on all 11 with file:line citations.

Per amendment 8: POST-WORK MUST sample every LOW-risk DROP whose
pre-edit sentence contains any of: `no `, `never`, `do not`,
`SSHFS`, `container`, `local`, `SSH`, `git`, `deploy`, or any
`zerops_*` tool name. Round ran the sample on rows #2, #5, #6,
#7 (executor flagged 3; Codex Check C surfaced #6 as a
trigger-term miss). All four NO-LOSS.

## Sub-pass work units

### DROP commits (LOW-risk; no PER-EDIT round needed)

| # | atom-id | pre-edit lines | bytes recovered | state | commit | notes |
|---|---|---|---:|---|---|---|
| D1 | bootstrap-close | bootstrap-close.md:23-26 | 208 B | DONE | `f2825dc5` | ServiceMeta projection; no signal |
| D2 | bootstrap-recipe-import | bootstrap-recipe-import.md:34-35 | 125 B | DONE | `f2825dc5` | Comparative timing trivia; trigger-flagged for POST-WORK (NO-LOSS confirmed) |
| D3 | idle-orphan-cleanup | idle-orphan-cleanup.md:22-24 | 27 B | DONE | `f2825dc5` | Reset mechanism detail |
| D4 | develop-push-dev-workflow-dev | develop-push-dev-workflow-dev.md:37-39 | 6 B | DONE | `f2825dc5` | Implementation phrasing |
| D5 | strategy-push-git-trigger-actions | strategy-push-git-trigger-actions.md:90-93 | 256 B | DONE | `f2825dc5` | First-build outcome; trigger-flagged (NO-LOSS) |
| D6 | bootstrap-wait-active | bootstrap-wait-active.md:22-23 | 127 B | DONE | `f2825dc5` | Polling-cost note; Codex POST-WORK Check C identified trigger `no` (from "no side effects"); inline Check A.4 audit confirmed NO-LOSS |
| D7 | export | export.md:232-233 | 86 B | DONE | `f2825dc5` | Repo-share topology note; trigger-flagged (NO-LOSS) |
| **DROP total** | — | — | **835 B** | — | `f2825dc5` | Codex estimated 845; observed 835; −1.2% |

### REPHRASE commits (HIGH-risk signal; Codex PER-EDIT APPROVE on each)

| # | atom-id | signal-check | bytes recovered | state | commit | notes |
|---|---|---|---:|---|---|---|
| R1 | develop-platform-rules-local | #1/#3/#4 | 278 B | DONE | `66f49f4d` | git-push verification + ZCP-does-not-init guardrail preserved |
| R2 | export | #5 | 152 B | DONE | `66f49f4d` | Do-NOT-copy guardrail preserved (line-spanning) |
| R3 | develop-ready-to-deploy | #4 | 183 B | DONE | `66f49f4d` | startWithoutCode+override recovery preserved |
| R4 | develop-first-deploy-write-app | #1/#4 | 97 B | DONE | `66f49f4d` | Don't-run-git-init + recovery preserved |
| R5 | strategy-push-git-push-local | #2/#5 | 114 B | DONE | `66f49f4d` | Local creds + ZCP-never-runs-git-init preserved |
| R6 | develop-first-deploy-asset-pipeline-container | #1/#3 | 80 B | DONE | `66f49f4d` | Frontend HMR + Do-NOT-add-build preserved |
| R7 | develop-first-deploy-asset-pipeline-local | #1/#2 | 103 B | DONE | `66f49f4d` | Local Vite HMR + Do-NOT-add-build preserved |
| R8 | develop-manual-deploy | #2/#3 | 59 B | DONE | `66f49f4d` | Dev-services-don't-auto-start + tool-selection preserved |
| R9 | develop-dev-server-triage | #2/#3 | 292 B | DONE | `66f49f4d` | Dev-mode-dynamic-only manual action preserved (table → prose) |
| R10 | develop-static-workflow | #2 | 121 B | DONE | `66f49f4d` | Build-runs-in-build-container preserved |
| R11 | develop-dynamic-runtime-start-container | impl-detail compression | 105 B | DONE | `66f49f4d` | Read-response-fields-before-next-call preserved; supersedes Codex DROP #7 scope |
| **REPHRASE total** | — | — | **1,584 B** | — | `66f49f4d` | Codex estimated 1,540; observed 1,584; +2.9% |

## Probe re-measurement (Phase 0 baseline → post-Phase-2)

| Fixture | Phase 0 baseline | Post-Phase-2 | Δ |
|---|---:|---:|---:|
| standard | 24,347 | 24,145 | −202 B |
| implicit-webserver | 26,142 | 25,965 | −177 B |
| two-pair | 26,328 | 26,021 | −307 B |
| single-service | 24,292 | 24,090 | −202 B |
| simple-deployed | 18,435 | 18,435 | 0 B (no firing atom in Phase 2 set) |
| **Aggregate first-deploy slice (4)** | — | — | **−888 B** |
| **Aggregate 5-fixture** | — | — | **−888 B** |

Off-probe recovery (atoms not firing on baseline 5 fixtures):
~ 1,531 B (835 DROPs + 696 REPHRASE off-probe atoms — local-env,
strategy-setup, manual, export, idle).

Phase 2 cumulative: ~ 2,419 B aggregate corpus reduction.

## Phase 2 EXIT (§5 Phase 2)

- [x] All axis-K candidates classified + actioned (or KEEP-tracked).
- [x] DROP ledger committed at `axis-k-drops-ledger.md`.
- [x] Codex POST-WORK clean (post-remediation APPROVE).
- [x] Probe re-run shows monotone or improved body-join (−888 B
  aggregate first-deploy; 0 simple-deployed; off-probe gains
  documented).
- [x] `phase-2-tracker-v2.md` committed.

## §15.2 EXIT enforcement

- [x] Every row above has non-empty final state.
- [x] Every row that took action cites a commit hash (filled at
  Phase 2 EXIT commit).
- [x] Codex round outcomes cited.
- [x] `Closed:` 2026-04-27.

## Cumulative time + cost

- 1 CORPUS-SCAN (~7 min Codex compute; produced 28 KB structured artifact).
- 1 PER-EDIT batched round (~3 min Codex compute; reviewed 11 diffs).
- 1 POST-WORK round (~3 min Codex compute; surfaced one ledger flag miss).
- ~30 min executor wall time (apply 18 edits + tracker + ledger).

## Notes for Phase 3 entry

1. **Phase 2 work surface aligns with amendment 11's "Phase 1
   establishes baseline only"** — neither G5 nor G6 was rerun after
   Phase 2 (correct per amendment 5: Phase 7 re-run is binding).
2. **Phase 3 (axis L title hygiene) is mechanical** — most atoms
   have env-only qualifier suffixes. Per amendment 2 / Codex C2,
   token-level edits MUST preserve mechanism qualifiers (e.g.
   `(GIT_TOKEN + .netrc)` in `strategy-push-git-push-container`).
3. **Phase 3 may proceed without Codex per-atom review** (amendment
   2 rule says "low risk; AST atom-ID pins are immune"); a final
   POST-WORK round per Phase 3 catches accidental drops.

Phase 3 (axis L title hygiene) entry unblocked.
