# SHIP — deploy-strategy decomposition

Closed: 2026-04-28
Plan: `plans/archive/deploy-strategy-decomposition-2026-04-28.md`

## Status

**SHIP — clean uniform implementation, no historical mess.**

The full 12-phase decomposition is complete. The legacy `DeployStrategy`
+ `PushGitTrigger` enums and `StrategyConfirmed` field are deleted from
the codebase. `ServiceMeta` carries three orthogonal axes
(`CloseDeployMode`, `GitPushState`+`RemoteURL`, `BuildIntegration`)
with no compatibility shims. Atom corpus, tools API, tests, specs,
CLAUDE.md — all aligned to the new vocabulary.

## What landed

| phase | risk | status | notes |
|---|---|---|---|
| Phase 0 | calibration | DONE | Pre-work Codex round APPROVE |
| Phase 1 | LOW | DONE | Topology types added (CloseDeployMode/GitPushState/BuildIntegration enums, IsPushSource predicate) |
| Phase 2 | MEDIUM | DONE | ServiceMeta migration with `migrateOldMeta` (deleted in Phase 10) |
| Phase 3 | MEDIUM | DONE | Envelope/router wiring; ServiceSnapshot fields |
| Phase 4 | HIGH | DONE | Atom synthesizer axis support (closeDeployModes/gitPushStates/buildIntegrations) |
| Phase 5 | HIGH | DONE | Handler validation (close-mode/git-push-setup/build-integration actions) |
| Phase 6 | MEDIUM-HIGH | DONE | Tool API split — close-mode/git-push-setup/build-integration |
| Phase 7 | LOW | DONE | Auto-close gate honors CloseDeployMode |
| Phase 8 | MEDIUM | DONE | Atom corpus restructure: 11 legacy atoms deleted, 8 new atoms added (setup-git-push-{container,local}, setup-build-integration-{webhook,actions}, develop-close-mode-{auto,git-push,manual}, develop-build-observe), 9 atoms migrated to new axis vocabulary |
| Phase 9 | LOW | DONE | Events hint pinned |
| Phase 10 | HIGH | DONE | Full legacy field removal: `topology.DeployStrategy` + `PushGitTrigger` enums deleted, `ServiceMeta.DeployStrategy/PushGitTrigger/StrategyConfirmed` deleted, `ServiceSnapshot.Strategy/Trigger` deleted, `migrateOldMeta` deleted, `WorkflowInput.Strategies/Trigger` deleted |
| Phase 11 | LOW | DONE | Spec + CLAUDE.md updated to new vocabulary across spec-workflows.md, spec-architecture.md, spec-knowledge-distribution.md, spec-local-dev.md, spec-work-session.md |

## Verify gates (final)

- `go test ./... -short -count=1` — all packages PASS
- `go test ./... -count=1 -race` — all packages PASS with race detector
- `make lint-local` — 0 issues (golangci-lint full rule set + recipe_atom_lint + atom_template_vars)
- `grep -rn "DeployStrategy\|PushGitTrigger\|StrategyConfirmed\|migrateOldMeta\|StrategyPush\|TriggerWebhook\|TriggerActions\|TriggerUnset" internal/ cmd/` — only `validateDeployStrategyParam` (function name describing the wire-level `strategy=` param of `zerops_deploy`, not the deleted enum)

## What changed in the late phase 8/10 reset

Initial SHIP-WITH-NOTES had Phase 8 and Phase 10 deferred. After the
reset directive ("zadnou kompatibilitu ani zpenou kompatibilkitu ners…
vse udelej poradne a at je to jednotne"), both phases were completed
in-session:

- Atom corpus: 11 deleted, 8 new, 9 migrated; references-fields AST-pinned
  to new ServiceSnapshot fields where applicable.
- Production code: every `topology.DeployStrategy*` and
  `topology.PushGitTrigger*` reference removed.
- Tests: scenarios_test, corpus_coverage_test, service_meta_test,
  bootstrap_outputs_test, router_test, atom_test, work_session_test,
  workflow_close_test, verify_test, workflow_develop_test,
  workflow_adopt_local_test, compute_envelope_test, synthesize_test,
  envelope_test, mode_expansion_filter_test, render_test, deploy_failure_test,
  integration tests — all migrated to new axis vocabulary.
- ComputeEnvelope normalizes empty `CloseDeployMode` → `CloseModeUnset`
  so the develop-strategy-review atom's `closeDeployModes: [unset]`
  filter matches services that haven't picked a close-mode yet.

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

## Lessons captured

The reset directive ("no compatibility shims, do it uniformly") proved
the engineering priority from `CLAUDE.local.md`: a 200-line clean cut
beats a 2-line patch when the cut is what root-cause correction needs.
The atom corpus had to move to new axes; the topology enum had to be
deleted; tests had to migrate signature-by-signature. Doing it all at
once in one commit (this commit) leaves the repo coherent — agents
reading it later will not encounter "what is `DeployStrategy` and why
does the codebase pretend it doesn't exist?"
