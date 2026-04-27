# Axis N candidates — cycle 3 CORPUS-SCAN

Date: 2026-04-27
Reviewer: Codex (round id `a2c7421f89a390a54`)
Scope: 53 universal atoms (no `environments:` axis) in `internal/content/atoms/`.
Spec: `docs/spec-knowledge-distribution.md` §11.6.

## DROP-LEAK candidates

| # | atom | line | phrase | priority | rationale |
|---|---|---:|---|---|---|
| 1 | `develop-first-deploy-intro.md` | 31 | "SSHFS mounts" | 1 | Universal first-deploy branch hard-codes container storage detail; the rule is "do not skip first deploy", while per-env edit/storage detail belongs in platform-rules atoms. |
| 2 | `develop-http-diagnostic.md` | 25 | "on the mount" | 2 | Universal HTTP diagnostic orders a container mount access path; per-env read-location/tooling belongs in platform-rules-container/local. |
| 3 | `develop-http-diagnostic.md` | 26 | "/var/www/{hostname}" | 3 | Container-only path inside a universal diagnostic atom; retain "framework log file" concept but rely on per-env platform rules for where/how to read it. |
| 4 | `develop-implicit-webserver.md` | 24 | "/var/www/<hostname>/" | 4 | Universal implicit-webserver workflow hard-codes container edit location; the universal truth is "write/edit files," with location supplied by per-env platform rules. |
| 5 | `develop-strategy-awareness.md` | 13 | "dev container" | 5 | Strategy overview defines push-dev using container-specific source shape; per-env deploy source is already handled by push-dev local/container atoms. |

## KEEP-LOAD-BEARING (no action)

19 universal atoms carry env-specific tokens that are load-bearing per Codex classification (signal #3 tool-selection / #4 recovery / #5 do-not). Inventory preserved verbatim from CORPUS-SCAN:

| atom | line | phrase | guardrail signal | rationale |
|---|---:|---|---|---|
| `bootstrap-close.md` | 15 | "dev containers" | #4 recovery/state handoff | Bootstrap close tells develop what `bootstrapped: true` means; dev-container readiness is observable platform state, not agent edit-location leakage. |
| `bootstrap-env-var-discovery.md` | 41 | "dev container" | #5 do-not | Prevents checking OS env inside never-deployed `startWithoutCode` containers; dropping it loses the "catalogue, not process.env" caveat. |
| `bootstrap-mode-prompt.md` | 16 | "SSHFS-mountable" | #3 mode selection | The SSHFS note distinguishes dev mode from standard/simple during bootstrap planning; it is part of mode semantics. |
| `bootstrap-mode-prompt.md` | 22 | "no SSHFS" | #3 mode selection | The simple-mode contrast is load-bearing for choosing mode and lifecycle. |
| `bootstrap-provision-rules.md` | 48 | "SSHFS and SSH" | #4 recovery/readiness | Explains why `startWithoutCode: true` is required for dev/simple readiness before first deploy. |
| `develop-deploy-modes.md` | 30 | "locally" | #5 do-not | Warns not to reject cross-deploy `deployFiles` because `./out` is absent in the editor tree. |
| `develop-dev-server-triage.md` | 29 | "container env" | #3 tool-selection | Triage selects `zerops_dev_server` vs local curl; dropping label makes command ambiguous. |
| `develop-dev-server-triage.md` | 32 | "local env — runs on your machine" | #3 tool-selection | Local status check must run against localhost via harness/Bash. |
| `develop-dev-server-triage.md` | 53 | "container env" | #3 tool-selection | Container start path must use `zerops_dev_server`. |
| `develop-dev-server-triage.md` | 56 | "local env" | #3 tool-selection | Local start path must use background harness. |
| `develop-first-deploy-execute.md` | 13 | "container env" | #5 do-not | "Deploy first, then inspect" includes container-specific failed pre-check; warning prevents invalid SSH probing before code exists. |
| `develop-manual-deploy.md` | 24 | "container env" | #3 tool-selection | Manual deploy distinguishes `zerops_dev_server` from local background harness. |
| `develop-manual-deploy.md` | 28 | "container env" | #3 tool-selection | Command block label binds `zerops_dev_server` to container env. |
| `develop-manual-deploy.md` | 31 | "local env — runs on your machine" | #3 tool-selection | Command block label binds `Bash run_in_background=true` to local env. |
| `strategy-push-git-intro.md` | 31 | "container env" | #3 tool-selection | Repo URL discovery differs by env before trigger selection. |
| `strategy-push-git-intro.md` | 37 | "local env" | #3 tool-selection | Local repo URL check uses `git -C`; label loses which command applies. |
| `strategy-push-git-trigger-actions.md` | 57 | "local env / container env" | #3 tool-selection | Workflow-file creation differs by local repo write vs SSH write. |
| `strategy-push-git-trigger-actions.md` | 86 | "via SSH" | #3 tool-selection | Container commit/push action follows env-specific SSH push atom. |
| `strategy-push-git-trigger-actions.md` | 87 | "Local env / locally" | #3 tool-selection | Local commit/push path is different from container SSH path. |

## SPLIT-CANDIDATE

| atom | line | phrase | proposed axis split | status |
|---|---:|---|---|---|
| `develop-push-git-deploy.md` | 12 | "dev container" | Add `environments: [container]` axis restriction to current atom. | **DEFERRED to follow-up** (see below). |

### SPLIT-CANDIDATE deferral rationale

Codex per-edit round 1 (`aef2a81baf1a7eefa`) flagged that tightening the axis would break 3 test pins on local-env push-git develop-active fixtures:
- `internal/workflow/scenarios_test.go:810` — `develop-active/push-git/standard/local-deployed` envelope.
- `internal/workflow/scenarios_test.go:948` — bulk-pin atom union expects `develop-push-git-deploy` to fire (still PASSes after tighten because container envelope at L821 keeps it in the union).
- `internal/workflow/corpus_coverage_test.go:624-639` — `develop_local_push_git` fixture has `MustContain: ["git-push", "GIT_TOKEN"]`. Currently the only atom firing on this envelope that supplies "GIT_TOKEN" is `develop-push-git-deploy`. After tightening, NO atom would supply it; MustContain FAILS.

The gap surfaces a real corpus issue: there is no `develop-push-git-deploy-local` atom for local-env push-git develop-active envelopes. The container atom (currently universal) is firing on local envelopes with WRONG content (SSH commands, project `GIT_TOKEN`). The proper fix is:
1. Author `develop-push-git-deploy-local.md` for local-env push-git deploy guidance (uses user's git credentials per `strategy-push-git-push-local.md`; no SSH; no project `GIT_TOKEN`).
2. Then tighten `develop-push-git-deploy.md` axis to `environments: [container]`.

Authoring a new atom is content-authoring work, not corpus-hygiene cleanup; it is outside cycle 3 plan scope (§1 + §2 = "5 content findings surfaced in audit"). Cycle 3 defers this SPLIT-CANDIDATE to a follow-up plan. Status quo (atom fires on local-env push-git with wrong content) is pre-existing — cycle 3 does not introduce or worsen it.

**Deferred follow-up entry**: `plans/audit-composition-v3/deferred-followups-v3.md` (created this phase) tracks this for future cycles.

## UNIFICATION-CANDIDATE

None. All universal-atom env splits encode tool-selection or do-not guardrails; DO-NOT-UNIFY exception (per spec §11.6) applies to all candidate pairs.

## APPLIED in earlier phases (skipped)

- `develop-static-workflow.md` L13 + L27-28 — Phase 4 F5 explicit work units.
- `develop-strategy-review.md` L15 parenthetical — Phase 2 F3 dropped (zcli push parenthetical; secondary effect addresses Axis N too).

## Summary

- Total Axis N candidates surfaced: 25
- DROP-LEAK: 5 (priority 1-3 broad atoms: 3; priority 4-5 narrow atoms: 2)
- KEEP-LOAD-BEARING: 19
- SPLIT-CANDIDATE: 1
- UNIFICATION-CANDIDATE (post DO-NOT-UNIFY exception): 0
- APPLIED in earlier phases: 3 work units

## Work plan (per plan §5 Phase 4 step 4-5)

- ✅ Per-edit Codex round 1 (`aef2a81baf1a7eefa`): APPROVE on edits 1, 4, 5; NEEDS-REVISION on edit 2 (add platform-rules-local cross-link); SPLIT-CANDIDATE deferred per above.
- Edit 2 revised: add `develop-platform-rules-local` to references-atoms + body cross-link.
- Per-edit Codex round 2: verify Edit 2 revision + confirm Edit 3 deferral.
- Apply edits 1, 2 (revised), 4, 5 after round-2 APPROVE. Edit 3 NOT applied (deferred).
- POST-WORK Codex round verifies platform-rules cross-link still co-fires after all Axis N applies.
