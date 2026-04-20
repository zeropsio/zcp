# Develop Flow Enhancements — Implementation Plan

> **Status**: In-progress implementation plan.
> **Date**: 2026-04-20
> **Scope**: Make develop flow self-describing at start, surface strategy/mode awareness, enforce code-change → deploy discipline, enable dev→standard expansion.

## Vision alignment

1. Start of every develop flow summarizes state + per-service recommendations + initial knowledge hand-off
2. Agent is told about deploy strategy (awareness, not full catalog) + how to change it; same for mode expansion when available
3. Any code change should trigger active deploy strategy (framing + warn-at-close)
4. Free development until task done ✓ (already correct)
5. Keep existing Phase enum (`develop-active` + `develop-closed-auto`) — 3-phase notion is expressed via BuildPlan shape, not new Phase values

## Phase 1: Content polish (~1 day, low risk)

**Goal**: 2 new atoms + 1 priority tweak. Pure content.

- NEW `internal/content/atoms/develop-strategy-awareness.md` — priority 5, `phases: [develop-active]`, no strategy filter
- NEW `internal/content/atoms/develop-change-drives-deploy.md` — priority 2, `phases: [develop-active]`
- EDIT `internal/content/atoms/develop-knowledge-pointers.md` — priority 8 → 3

**Tests**: `corpus_coverage_test.go` updated to assert new atoms surface for representative develop envelopes. `scenarios_test.go` substring asserts may need tweak.

**Ship check**: `go test ./internal/workflow/... ./internal/content/...` green.

## Phase 2: Plan per-service (~1-2 days, medium risk)

**Goal**: `Plan` carries per-service actions; render shows `Per service:` section for multi-service scope.

- EDIT `internal/workflow/plan.go` — add `PerService map[string]NextAction` field
- EDIT `internal/workflow/build_plan.go` — `planDevelopActive` populates `PerService` for each service in scope (deploy/verify based on last attempt state; skip green services)
- EDIT `internal/workflow/render.go` — `renderPlan` emits `Per service:` block when `len(PerService) > 1`

**Tests**: `build_plan_test.go`, `render_test.go`, scenarios S6/S8.

**Ship check**: scenario tests green + new snapshot for multi-service render.

## Phase 3: Close warning (~0.5 day, medium risk)

**Goal**: `action=close workflow=develop` refuses when no successful deploy unless `force=true` given. Auto-close path untouched.

- EDIT `internal/tools/workflow.go` — `WorkflowInput` gets `Force bool`; `handleWorkSessionClose` checks `HasSuccessfulDeploy(ws)`; warn+exit without delete when false && !force
- EDIT `internal/workflow/work_session.go` — new `HasSuccessfulDeploy(ws *WorkSession) bool`
- Optional: reuse `ErrInvalidParameter` or add `ErrCloseNoDeploy` in `internal/platform/errors.go`

**Tests**: new `workflow_close_test.go` — (1) successful deploy → close OK, (2) no deploy → warning, (3) no deploy + force=true → close OK. Verify auto-close path still closes without force.

**Ship check**: integration test confirms normal develop flow closes without needing `force=true`.

## Phase 4: Mode expansion dev → standard (~3-5 days, high risk)

**Goal**: §9.1 Planned Feature implemented. Existing dev service gets a new stage sibling added without redeploy; ServiceMeta upgrades `Mode: dev → standard` with `StageHostname` set.

**Mechanism**: bootstrap plan target `isExisting=true + bootstrapMode=standard + stageHostname=<new>` treats dev as adopted (code preserved) and creates new stage from scratch (provision new stage service → write stage zerops.yaml entry → cross-deploy dev→stage → verify stage).

- EDIT `internal/workflow/validate.go` — allow `IsExisting=true + BootstrapMode=standard + ExplicitStage` combination
- EDIT `internal/workflow/bootstrap_outputs.go` (`writeProvisionMetas`) — detect expansion (existing meta Mode=dev + plan BootstrapMode=standard); update meta in place: `Mode: dev → standard`, `StageHostname: plan.stage`. Keep `BootstrappedAt`, `DeployStrategy`, `StrategyConfirmed`, `BootstrapSession`.
- EDIT bootstrap flow (`internal/workflow/bootstrap.go` / related) — fast-path: skip generate+deploy for existing dev part, run full flow for new stage
- Import YAML generator — generate fragment only for the new stage service (no touch of existing dev)
- NEW `internal/content/atoms/develop-mode-expansion.md` — `modes: [dev]`, priority 6
- UPDATE `docs/spec-workflows.md` §9.1 — mark implemented

**Tests**:
- `bootstrap_test.go` — expansion flow end-to-end
- `validate_test.go` — new valid combination
- `service_meta_test.go` — Mode upgrade path
- `integration/` — dev → standard full scenario
- `bootstrap_outputs_test.go` — writeProvisionMetas expansion branch

**Ship check**:
- existing dev is NOT redeployed (code preserved, `BootstrappedAt` unchanged)
- new stage created + successful cross-deploy + ServiceMeta updated
- `develop-mode-expansion` atom stops firing after expansion (filter `modes: [dev]` no longer matches)

## Order & sizing

| Phase | Size | Risk | Depends on |
|---|---|---|---|
| 1 | 1 day | low | — |
| 2 | 1-2 days | medium | — |
| 3 | 0.5 day | medium | — |
| 4 | 3-5 days | high | — |

All independent. Ship 1 → 2 → 3 → 4 in order. Each commit green before next.
