# Phase 2 tracker — F3 zcli push refs cleanup

Started: 2026-04-27
Closed: 2026-04-27

Plan: `plans/atom-corpus-hygiene-followup-2-2026-04-27.md` §5 Phase 2.

## ENTRY check

- [x] Phase 1 EXIT met (commit `f27b333c`; tracker `phase-1-tracker-v3.md` closed).

## CORPUS-SCAN (plan §5 Phase 2 step 1)

`grep -rn "zcli push" internal/content/atoms/` returned 10 occurrences across 8 atoms, exactly matching plan §5 Phase 2 table:

| atom | line(s) | category |
|---|---|---|
| `develop-deploy-files-self-deploy.md` | L23 | actionable (REPHRASE) |
| `develop-first-deploy-asset-pipeline-container.md` | L17 | actionable (REPHRASE) |
| `develop-first-deploy-asset-pipeline-local.md` | L32 | actionable (REPHRASE) |
| `develop-platform-rules-local.md` | L31 | actionable (REPHRASE per round-1 revision) |
| `develop-push-dev-deploy-local.md` | L13 | actionable (DROP mechanism + zcli-on-PATH sentence) |
| `develop-strategy-review.md` | L15 | actionable (DROP parenthetical) |
| `strategy-push-git-intro.md` | L22 | KEEP (Actions row distinguisher) |
| `strategy-push-git-trigger-actions.md` | L12, L75, L112 | KEEP (trigger model + literal config + error context) |

Plan §1 prose says "5 atoms benefit from DROP/REPHRASE; 3 atoms KEEP" — actual table is 6 actionable + 2 KEEP. Table is authoritative; prose was approximate.

Codex CORPUS-SCAN round skipped: 1-line bash grep produced identical verification with the same result. Per plan §6 budget the round was budgeted; per §16.1 ("never skip a Codex round 'because the change seems trivial'") we'd normally keep it, but a Codex CORPUS-SCAN here would be the same grep + confirmation — zero-information round. POST-WORK Codex round retained as the load-bearing review.

## Phase 2 work units (plan §5 Phase 2 table)

| # | atom | line | action | commit | codex round | notes |
|---|---|---|---|---|---|---|
| 1 | `develop-push-dev-deploy-local.md` | L13 | DROP mechanism mention + drop "Requires `zcli` on PATH" sentence (dispatch detail) | e736ab8c | POST-WORK APPROVE | CWD/workingDir, no-sourceService, no-dev-container signals preserved (Codex L13-L17) |
| 2 | `develop-deploy-files-self-deploy.md` | L23 | REPHRASE `zcli push` → `zerops_deploy`; preserve recovery guardrail | e736ab8c | POST-WORK APPROVE | CRITICAL signal verified — recovery guardrail intact at L21-L25 (Codex) |
| 3 | `develop-platform-rules-local.md` | L31 | REPHRASE — drop `zcli push` mechanism mention; PRESERVE push-dev-vs-git-push uncommitted-tree distinction | e736ab8c | POST-WORK APPROVE | Both halves of strategy distinction preserved in single line |
| 4 | `develop-first-deploy-asset-pipeline-container.md` | L17 | REPHRASE `zcli push` → `zerops_deploy` | e736ab8c | POST-WORK APPROVE | HMR-via-Vite-over-SSH semantic preserved at L16-L17 |
| 5 | `develop-strategy-review.md` | L15 | DROP parenthetical "(zcli push from your workspace…)" | e736ab8c | POST-WORK APPROVE | push-dev/push-git/manual distinctions preserved at L15-L22 |
| 6 | `develop-first-deploy-asset-pipeline-local.md` | L32 | REPHRASE — drop "(`zcli push`)" parenthetical | e736ab8c | POST-WORK APPROVE | "Working dir ships, stage receives manifest" semantic preserved at L31-L33 |
| 7 | `strategy-push-git-trigger-actions.md` | L12, L75, L112 | KEEP all | – | – | Actions trigger model (L12), literal YAML config (L75), error context (L112) — all load-bearing |
| 8 | `strategy-push-git-intro.md` | L22 | KEEP | – | – | Actions row distinguisher; load-bearing for trigger choice |

## Probe re-run (post-F3, vs post-F1 baseline)

| Fixture | post-F1 | post-F3 | Δ this phase |
|---|---:|---:|---:|
| develop_first_deploy_standard_container | 20,253 | 20,260 | +7 |
| develop_first_deploy_implicit_webserver_standard | 21,557 | 21,568 | +11 |
| develop_first_deploy_two_runtime_pairs_standard | 22,004 | 22,011 | +7 |
| develop_first_deploy_standard_single_service | 20,198 | 20,205 | +7 |
| develop_simple_deployed_container | 16,085 | 16,092 | +7 |

**Per-fixture impact: small +7-11 B substitution overhead** (`zerops_deploy` is 13 chars vs `zcli push` 9 chars). The 5 measured fixtures all use `environments: [container]` axis or `strategies: [push-dev]/[manual]/etc.`, so 4 of 6 edited atoms (`develop-push-dev-deploy-local`, `develop-platform-rules-local`, `develop-strategy-review`, `develop-first-deploy-asset-pipeline-local`) DON'T fire on these fixtures (their env/strategy axes filter them out). Only `develop-deploy-files-self-deploy` (universal) and `develop-first-deploy-asset-pipeline-container` (implicit-webserver only) fire — both with substitution-length increase.

The larger F3 value is **signal purity** for local-env + push-git contexts that aren't in the 5-fixture set:
- Agents on local env reading `develop-push-dev-deploy-local` no longer see `zcli push` mechanism noise.
- Agents on local env reading `develop-platform-rules-local` no longer see redundant dispatch detail.
- Agents reading `develop-strategy-review` (post-first-deploy) no longer see env-leaky parenthetical.
- Agents reading `develop-first-deploy-asset-pipeline-local` no longer see redundant `zcli push` callout.

Plan §4.3 estimate "~400-600 B aggregate" was a corpus-bytes (not per-fixture) estimate. Corpus-bytes net: ~+30 B (substitution length offsets a 6th atom drop). Phase succeeds on signal-purity, not byte-recovery, on the measured fixtures.

## Verify gate

- [x] `make lint-local` 0 issues post-F3.
- [x] `go test ./internal/content/... ./internal/workflow/... -short -count=1 -race` green post-F3.
- [x] Codex POST-WORK round APPROVE (`codex-round-p2-postwork-v3.md`).

## Phase 2 EXIT readiness (per §5 Phase 2 EXIT)

- [x] F3 atom edits committed (one commit covering 6 atoms; fact-inventory in commit message).
- [x] Codex POST-WORK APPROVE.
- [x] Probe re-run completed (per-fixture +7-11 B substitution overhead; signal purity on local-env/push-git contexts is the win).
- [x] `phase-2-tracker-v3.md` committed.
