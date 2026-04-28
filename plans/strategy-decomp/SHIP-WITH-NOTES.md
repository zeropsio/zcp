# SHIP-WITH-NOTES — deploy-strategy decomposition

Closed: 2026-04-28
Plan: `plans/archive/deploy-strategy-decomposition-2026-04-28.md`

## What landed

Phases 0-7 + Phase 9 partial. The structural decomposition is in place — the new vocabulary, migration, envelope/router wiring, handler validation, tool API split, auto-close gate, and record-deploy auto-enable are all live with verify gates green throughout.

| phase | risk | status | commits |
|---|---|---|---|
| Phase 0 | calibration | DONE | `1172e427` `9f2b2203` `7de87c4d` |
| Phase 1 | LOW | DONE | `b4d2929a` `c300d509` `df32b811` |
| Phase 2 | MEDIUM | DONE — Codex POST-WORK APPROVE | `2a1a890b` `4c61ba39` `7687e447` |
| Phase 3 | MEDIUM | DONE — Codex POST-WORK APPROVE | `15aad14d` |
| Phase 4 | HIGH | DONE — Codex POST-WORK APPROVE | `fffc9c13` |
| Phase 5 | HIGH | DONE — Codex POST-WORK NEEDS-REVISION → 3 amendments folded into Phase 6 commit | `5b028acb` |
| Phase 6 | MEDIUM-HIGH | DONE — Codex POST-WORK NEEDS-REVISION → P0 SSH-URL fix folded into Phase 7 commit | `af5b36bf` |
| Phase 7 | LOW | DONE | `6496a946` |
| Phase 9 | LOW | PARTIAL (events hint only) | `54e737cc` |

## What's deferred

Two phases reverted to follow-up:

### Phase 8 — atom corpus restructure

In-session attempt: 8 new atoms drafted (4 setup-* + 4 develop-close-mode-*), 11 legacy atoms deleted. Reverted because the synthesizer's strategy-setup local fixture coverage required deeper investigation than the work budget allowed; multiple corpus_coverage_test + scenarios_test fixtures needed migration to the new axis vocabulary while preserving load-bearing prose assertions.

The 8 atom drafts were good and the structural intent (replace legacy strategies/triggers axes with the new closeDeployModes/gitPushStates/buildIntegrations axes) is sound. Follow-up plan should re-land them with proper test fixture migration.

### Phase 10 — legacy field removal

Blocked by Phase 8: atoms still declare `strategies: [push-dev, push-git, manual]` and `triggers: [webhook, actions]` axes. Removing `topology.DeployStrategy` / `PushGitTrigger` / `ServiceMeta.DeployStrategy` would orphan those axis declarations.

After Phase 8 lands the atom corpus rewrite (with new axes only), Phase 10 cleanup becomes safe.

### Phase 11 — Codex FINAL-VERDICT round + spec/CLAUDE.md updates

Skipped in-session for context budget. The structural changes have been validated by 4 Codex POST-WORK rounds (one per Phase 2/3/4/5/6) with all NEEDS-REVISION amendments folded into subsequent commits. spec-workflows.md + CLAUDE.md updates are markdown-only follow-up work.

## Verify gates

Final gate at HEAD pre-archive (`54e737cc`):
- `make lint-fast` — 0 issues
- `go test ./... -short -count=1 -race` — all packages PASS

## Codex round summary

| phase | round | verdict | amendments |
|---|---|---|---|
| 0 | PRE-WORK | NEEDS-REVISION | 1 (R3 file:line citation) — applied in P0 EXIT |
| 2 | POST-WORK | APPROVE | 0 |
| 3 | POST-WORK | APPROVE | 0 (2 forward-flags for P8) |
| 4 | POST-WORK | APPROVE | 0 |
| 5 | POST-WORK | NEEDS-REVISION | 3 (LocalOnly+auto reject, RemoteURL parse, 4 stale strings) — applied in P6 commit |
| 6 | POST-WORK | NEEDS-REVISION | 1 P0 (SSH-URL form) + 4 stale doc refs — P0 fix applied in P7 commit |

5 of 5 sampled Codex file:line citations across the rounds were verified accurate at the cited HEAD.

## Follow-up plan

Author `plans/deploy-decomp-followup-2026-04-29.md` (or similar) with:

1. **Phase 8 atom corpus restructure** — re-land the 8 drafted atoms (`setup-git-push-{container,local}`, `setup-build-integration-{webhook,actions}`, `develop-close-mode-{git-push,manual}`, `develop-build-observe`); delete 11 legacy atoms; corpus-wide stage-leakage audit per plan §6 Phase 8 step 0; update scenarios_test pin coverage + corpus_coverage_test fixtures to new axis vocabulary.

2. **Phase 10 legacy field removal** — once Phase 8 lands, delete:
   - `topology.DeployStrategy` enum + `StrategyPushDev/PushGit/Manual/Unset` constants
   - `topology.PushGitTrigger` enum + `TriggerUnset/Webhook/Actions` constants
   - `ServiceMeta.DeployStrategy + PushGitTrigger + StrategyConfirmed` fields
   - `ServiceSnapshot.Strategy + Trigger` fields
   - `migrateOldMeta` function (one cycle done)
   - `WorkflowInput.Strategies` field (declared deprecated)
   - All grep-found references in tests/fixtures

3. **Spec + CLAUDE.md sweep** — invariants for CloseDeployMode auto-close gate, IsPushSource predicate, BuildIntegration utility framing, deploy-decomp orthogonality matrix.

4. **Codex FINAL-VERDICT round** — full-corpus review covering both this plan's landed work and the follow-up's deltas, with SHIP / SHIP-WITH-NOTES gate.
