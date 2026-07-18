# Phase 3 tracker — F4 develop-push-dev-workflow-dev rewrite

Started: 2026-04-27
Closed: 2026-04-27

Plan: `plans/atom-corpus-hygiene-followup-2-2026-04-27.md` §5 Phase 3.

## ENTRY check

- [x] Phase 2 EXIT met (commit `e736ab8c`; tracker `phase-2-tracker-v3.md` closed).
- [x] HIGH-risk classification per axis-b (cycle 2): MANDATORY PER-EDIT Codex round.

## MustContain pin migration check (plan §5 Phase 3 step 3)

- `internal/workflow/scenarios_test.go:671` pins atom by ID (`develop-push-dev-workflow-dev`) only — no body-phrase pin.
- `internal/content/atoms/develop-change-drives-deploy.md:7,15` cross-references atom by ID — no body-phrase pin.
- **Conclusion**: NO MustContain pin migration needed; rewrite is free to change body text.

## Phase 3 work units

| # | work unit | initial state | final state | commit | codex round | notes |
|---|---|---|---|---|---|---|
| 1 | F4 atom rewrite (auto-reload aware, start/restart split clarified) | atom L13-L40 says "After each edit, run `action=restart`" (over-eager, wrong about SSHFS auto-watch) | atom rewritten with polling caveat + no-redeploy guardrail + start/restart split | 97267c1f | round 2 APPROVE | 2 round-1 catches addressed: no-redeploy guardrail restored; SSHFS polling caveat replaces false auto-watch claim |
| 2 | PER-EDIT Codex round 1 | not run | NEEDS-REVISION | – | NEEDS-REVISION | (1) lost no-redeploy guardrail at current atom L25; (2) false auto-watch claim — SSHFS doesn't surface inotify events |
| 3 | Plan revisions per round 1 (§5 Phase 3 rewrite block) | original rewrite | revised rewrite (no-redeploy guardrail + polling caveat) | 97267c1f | – | annotated in plan with "Round-1 PER-EDIT Codex round flagged two defects (now resolved above)" note |
| 4 | PER-EDIT Codex round 2 | not run | APPROVE | 97267c1f | APPROVE | both revisions verified; round-1 APPROVE items still hold (signals #3, #4, AST references-fields pin) |
| 5 | Apply F4 atom edit | not applied | APPLIED | 97267c1f | – | atom file 1,396 → 1,974 B (+578 B; net-positive due to round-1 corrections); first-deploy fixtures unaffected (axis mismatch) |
| 6 | Verify gate | unknown | PASS | 97267c1f | – | lint 0 issues; AST references-fields integrity test PASS |

## Probe re-run

F4 atom (`develop-push-dev-workflow-dev`) has axes
`[develop-active] + [deployed] + [dev] + [push-dev] + [container]`
which don't match any of the 5 measured fixtures (4 first-deploy
fixtures are `never-deployed`; simple-deployed has `[simple]` mode
not `[dev]`). So per-fixture body sizes unchanged:

| Fixture | post-F3 | post-F4 | Δ |
|---|---:|---:|---:|
| develop_first_deploy_standard_container | 20,260 | 20,260 | 0 |
| develop_first_deploy_implicit_webserver_standard | 21,568 | 21,568 | 0 |
| develop_first_deploy_two_runtime_pairs_standard | 22,011 | 22,011 | 0 |
| develop_first_deploy_standard_single_service | 20,205 | 20,205 | 0 |
| develop_simple_deployed_container | 16,092 | 16,092 | 0 |

The byte impact lives on `develop_push_dev_dev_container`-style
envelopes (not in the 5-fixture probe set). Atom-byte file change:
1,396 → 1,974 B (+578 B; clarity-positive net per plan §5 Phase 3
"net-neutral bytes; clarity-positive" — round-1-driven polling
caveat + restored guardrail justify the file growth).

## Verify gate

- [x] `make lint-local` 0 issues post-F4.
- [x] `go test ./internal/content/... ./internal/workflow/... -short -count=1 -race` green post-F4.
- [x] Codex PER-EDIT round 2 APPROVE (`codex-round-p3-peredit-v3.md`).
- [x] AST references-fields integrity test PASS (all 5 fields — Reason, Running, HealthStatus, StartMillis, LogTail — still resolved in body).

## Phase 3 EXIT readiness (per §5 Phase 3 EXIT)

- [x] F4 atom rewrite committed.
- [x] Codex per-edit round APPROVE (round 2 after round-1-driven revision).
- [x] Pin migration handled (none needed; pins are atom-ID only).
- [x] Probe re-run; first-deploy fixture sizes unchanged (axis mismatch is expected).
- [x] `phase-3-tracker-v3.md` committed.
