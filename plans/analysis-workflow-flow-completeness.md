# Analysis: Complete Workflow Flow Completeness Review
**Date**: 2026-03-21
**Task**: Analyze complete flow for all workflows (bootstrap, deploy, CI/CD, immediate). For each step, analyze what happens, how gates/checkers work and evaluate. Identify: unused code, non-functional parts, gaps, bad implementations, deficiencies. Container mode only.
**Reference files**:
- `docs/spec-bootstrap-deploy.md` — Authoritative spec document (may not match reality in all details)
- `internal/workflow/engine.go` (489L) — Core engine: start, complete, skip, iterate, resume for all workflow types
- `internal/workflow/bootstrap.go` (337L) — Bootstrap state, step progression, conditional skip, prior context
- `internal/workflow/bootstrap_steps.go` (48L) — Step definitions (5 steps with tools, verification, skippable flags)
- `internal/workflow/bootstrap_guidance.go` (142L) — Guidance resolution, progressive sections, iteration delta
- `internal/workflow/bootstrap_checks.go` (24L) — StepChecker type definition
- `internal/workflow/bootstrap_guide_assembly.go` (169L) — BuildGuide, formatEnvVars, BuildTransitionMessage, routeFromBootstrapState
- `internal/workflow/bootstrap_outputs.go` (72L) — writeBootstrapOutputs, writeProvisionMetas
- `internal/workflow/deploy.go` (344L) — Deploy state, targets, BuildDeployTargets, iteration reset
- `internal/workflow/deploy_guidance.go` (86L) — Deploy step guidance resolution
- `internal/workflow/cicd.go` (183L) — CI/CD state, steps, provider selection
- `internal/workflow/cicd_guidance.go` (25L) — CI/CD guidance from content sections
- `internal/workflow/guidance.go` (144L) — Unified guidance assembly (bootstrap + deploy)
- `internal/workflow/session.go` (240L) — Session lifecycle, atomic init, iteration, load/save
- `internal/workflow/state.go` (39L) — WorkflowState struct, immediate workflows
- `internal/workflow/validate.go` (277L) — Plan validation (targets, hostnames, types, deps, modes)
- `internal/workflow/router.go` (320L) — Flow routing, intent matching, strategy offerings
- `internal/workflow/service_meta.go` (118L) — ServiceMeta CRUD
- `internal/workflow/managed_types.go` (80L) — Managed service detection, project state detection
- `internal/workflow/registry.go` (219L) — Session registry with file locking
- `internal/workflow/environment.go` (19L) — Container vs local detection
- `internal/workflow/reflog.go` (48L) — CLAUDE.md reflog entry append
- `internal/tools/workflow.go` (358L) — MCP tool handler, action dispatch
- `internal/tools/workflow_bootstrap.go` (163L) — Bootstrap handlers (complete, skip, status, stacks)
- `internal/tools/workflow_deploy.go` (67L) — Deploy handlers (complete, skip, status)
- `internal/tools/workflow_checks.go` (291L) — Step checkers (provision, deploy)
- `internal/tools/workflow_checks_generate.go` (229L) — Generate checker (zerops.yml validation)
- `internal/tools/workflow_strategy.go` (178L) — Strategy handler, selection response, route handler
- `internal/tools/workflow_cicd.go` (64L) — CI/CD handlers (complete, status)
- `internal/content/content.go` (57L) — Embedded workflow markdown loader
