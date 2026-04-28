# Codex PRE-WORK round — Phase 0 calibration

Date: 2026-04-28
Plan SHA at round time: `1172e427`
Round duration: ~3m 44s (223,887 ms)
Round status: NEEDS-REVISION → 1 amendment applied → effectively APPROVE

## Round prompt summary

Codex was handed the just-committed plan and asked to validate against current corpus state. Two main questions + two bonus checks:

- **Q1**: any §4 decision invalidated by post-2026-04-27 commits?
- **Q2**: any atom already rendering the proposed three-dimension shape?
- **Bonus 1**: spot-check 5 plan-cited file:line locations.
- **Bonus 2**: confirm parser-rejects-unknown-keys (Phase 1.0 ordering safety).

## Verdict

**NEEDS-REVISION** — single citation fix on §1 R3 row. No decision-text invalidation. No atom already implements proposed shape. Phase 1.0 ordering structurally required.

After applying the single amendment, the round effectively converges to APPROVE per the §10.5 work-economics rule (consumer identified, concern addressed in-place, no further round needed).

## Q1 — §4 decision invalidation

ALL DECISIONS VALID. Codex inspected post-2026-04-27 commits and confirmed:

| Decision | Status | Notes |
|---|---|---|
| F1 (CloseModeAuto + auto-close gate) | VALID | `CloseDeployMode` does not exist yet |
| F2 (BuildIntegration per-pair) | VALID | E3 auto-prune (`80188128`) operates on hostname presence; no conflict |
| F3 (RemoteURL = git origin) | VALID | `currentEffectiveOrigin` logic at `deploy_local_git.go:272-279` compatible |
| F5 (Local-only refuse webhook) | VALID | No commit added local-only webhook setup |
| D1 (CloseDeployMode naming) | VALID | New name absent at HEAD; old `DeployStrategy` at `service_meta.go:33-43` |
| D2 (GitPushState enum) | VALID | New name absent; old `PushGitTrigger` at `service_meta.go:37-39` |
| D3 (BuildIntegration utility framing) | VALID | Atoms still use "trigger" framing |
| Migration (parseMeta hook) | VALID | `parseMeta` is shared read path; integration point correct |
| Drop LastBuildLandedAt | VALID | No commit added it |
| Atom prereq chaining | VALID | Parser only knows existing axes |

### R10 deferral re-check

Codex confirmed R10 (TimelineEvent failureClassification) deferral language stays accurate:
- E2 commit `821f6113` added classification at deploy-response level (`ops/deploy_common.go:44-52`, `tools/errwire.go:37-43`) — but `TimelineEvent` at `ops/events.go:28-41` still has no `failureClassification` field.
- R10 is about TimelineEvent specifically; deferral remains correct.

### G5 ship gate re-check

Codex confirmed E1 single-render aggregate (`ed5e1382`) does NOT cover the §G5 gate:
- E1 added two-pair aggregate coverage for first-deploy atoms with `StrategyUnset`
- §G5 requires `develop-active/git-push/standard/container` dev-hostname single-render
- Push-git scenario fixtures still single-snapshot
- G5 gate remains pending — Phase 3 must add the two-snapshot fixture

### Phase 1 axis naming compatibility

Codex confirmed long-form axis names (`closeDeployModes`, `gitPushStates`, `buildIntegrations`) are camelCase frontmatter keys; axis K/L/M/N hygiene targets body prose and headings (not frontmatter keys). No conflict.

### Atom split re-check

All split files from `016e6dbe` (`atoms: split push-git-deploy by environment`) exist and match §5.3/§8 deletion inventory: container/local push setup and deploy/close variants all present.

## Q2 — Atoms already rendering proposed shape

**NONE FOUND.** No atom uses `CloseDeployMode`, `GitPushState`, `BuildIntegration`, "close mode", "build integration", "git push state", or "ZCP-managed integration" language.

Partial mechanics present but NOT the proposed model:

| location | content | gap |
|---|---|---|
| `strategy-push-git-trigger-actions.md:12-14` | Actions CI runs `zcli push` | Doesn't call it "mechanically push-dev" |
| `strategy-push-git-trigger-actions.md:74-75` | `zcli push --serviceId ... --setup ...` | Mechanism shown but not in orthogonality framing |
| `strategy-push-git-trigger-webhook.md:61-62` | Either ZCP git-push or user `git push` fires webhook | Doesn't articulate orthogonality of close-mode vs build-integration |

## Bonus 1 — Plan-cited file:line accuracy

| # | claim | result | notes |
|---|---|---|---|
| 1 | `compute_envelope.go:243-256` shows Deployed split semantic | **FAIL** | Actual: assignment at L206-210, body at L262-272, push-git stamp site at `deploy_git_push.go:215-229` |
| 2 | `deploy_ssh.go:195-202` vs `deploy_git_push.go:215-232` — subdomain auto-enable SKIP for git-push | PASS | `deploy_ssh.go:195-202` calls `maybeAutoEnableSubdomain`; `deploy_git_push.go:215-232` records success and returns without subdomain enable |
| 3 | `deploy_local_git.go:212-296` — `trackTriggerMissingWarning` in local but not container | PASS | Helper called L208-214, defined L282-296; no container parity |
| 4 | `workflow_develop.go:101-114` — auto-delete behavior | PASS | L101-115 deletes existing work session for different intent and unregisters |
| 5 | `develop-close-push-dev-local.md:6` is `modes: [dev, stage]` | PASS | Confirmed exactly at L6 |

**Action**: Bonus 1 #1 → §1 R3 row citation amendment (applied in-place).

## Bonus 2 — Phase ordering safety

| check | result |
|---|---|
| `validAtomFrontmatterKeys` is closed set | YES — `internal/workflow/atom.go:108-135` |
| `validateAtomFrontmatter` rejects unknown keys | YES — `atom.go:250-266` |
| Phase 1.0 (parser extension) is correct blocker for Phase 8 | YES — Phase 8 introduces `closeDeployModes`/`gitPushStates`/`buildIntegrations` keys; current parser would reject all |

## Amendment applied

```diff
-| R3 | Envelope | 🔴 | `Deployed` field has split semantic (push-dev=build landed; push-git=git push succeeded) | `compute_envelope.go:243-256` |
+| R3 | Envelope | 🔴 | `Deployed` field has split semantic (push-dev=build landed; push-git=git push succeeded) | `compute_envelope.go:206-210`, `compute_envelope.go:262-272`, `deploy_git_push.go:215-229` (Codex PRE-WORK 2026-04-28: stale `:243-256` citation corrected) |
```

Applied to `plans/deploy-strategy-decomposition-2026-04-28.md` §1 root-problem table; commits in same Phase 0 EXIT commit as baseline + tracker.

## Convergence

Per §5 Phase 0 EXIT contract and the followup-2 plan §10.5 work-economics rule:
- NEEDS-REVISION returned with a single amendment.
- Amendment applied surgically to plan in-place.
- No further Codex round required — concern is fully addressed and consumer (Claude executing Phase 1+) is identified.
- Effective verdict: APPROVE.

Phase 1 may enter on user go.
