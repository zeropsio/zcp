# Analysis: Workflow System Fat Trimming

**Date**: 2026-03-21
**Task**: Identify unnecessary accumulated complexity in the workflow system (internal/workflow/ + internal/tools/workflow_*.go) that can be trimmed, similar to the recent ServiceMeta cleanup commits (998dd33, 4e358aa).

**Reference files**:
- `internal/workflow/engine.go` (484L) — Engine orchestration, workflow lifecycle methods
- `internal/workflow/session.go` (234L) — Session persistence, atomic init, iteration
- `internal/workflow/bootstrap.go` (347L) — BootstrapState, steps, response building
- `internal/workflow/deploy.go` (344L) — DeployState, targets, step progression
- `internal/workflow/cicd.go` (183L) — CICDState, provider selection
- `internal/workflow/router.go` (320L) — Flow routing, intent matching, offerings
- `internal/workflow/service_meta.go` (118L) — ServiceMeta persistence (recently trimmed)
- `internal/workflow/validate.go` (276L) — Plan validation, type checking
- `internal/workflow/guidance.go` (133L) — Guidance assembly, knowledge injection
- `internal/workflow/bootstrap_guidance.go` (142L) — Section extraction, progressive guidance
- `internal/workflow/bootstrap_guide_assembly.go` (88L) — Bootstrap-specific guide building
- `internal/workflow/deploy_guidance.go` (86L) — Deploy step guidance resolution
- `internal/workflow/cicd_guidance.go` (25L) — CI/CD guidance resolution
- `internal/workflow/bootstrap_steps.go` (61L) — Step definitions (6 steps)
- `internal/workflow/bootstrap_outputs.go` (75L) — Meta/reflog writing on completion
- `internal/workflow/bootstrap_checks.go` (24L) — StepChecker type definition
- `internal/workflow/managed_types.go` (80L) — Managed service detection, project state
- `internal/workflow/reflog.go` (48L) — CLAUDE.md reflog entries
- `internal/workflow/state.go` (39L) — WorkflowState, ProjectState types
- `internal/workflow/environment.go` (19L) — Environment detection
- `internal/workflow/registry.go` (235L) — Session registry, locking, pruning
- `internal/tools/workflow.go` (347L) — MCP tool handler, action routing
- `internal/tools/workflow_bootstrap.go` (173L) — Bootstrap complete/skip/status handlers
- `internal/tools/workflow_deploy.go` (67L) — Deploy complete/skip/status handlers
- `internal/tools/workflow_cicd.go` (64L) — CI/CD complete/status handlers
- `internal/tools/workflow_strategy.go` (144L) — Strategy updates, route handling
- `internal/tools/workflow_checks.go` (338L) — Step checkers (provision, generate, deploy, verify)

**Total**: ~4,133 lines across 27 files (workflow package + tools handlers)

**Context**: Recent commits trimmed ServiceMeta (removed Status, Type, Decisions fields; removed managed dep metas; removed stale API shadows). Looking for similar fat — fields/types/functions/patterns that accumulated during development but are no longer necessary.
