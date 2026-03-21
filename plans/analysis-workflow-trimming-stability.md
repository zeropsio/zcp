# Analysis: Workflow Trimming & Stability for LLM Consumption

**Date**: 2026-03-21
**Task**: Study spec-bootstrap-deploy.md and the full current implementation. Identify what can be further trimmed (ballast removal, continuing recent refactoring trend), while strengthening stability and cleanliness of workflow flows to maximally help LLM agents consuming the guidance.

**Reference files**:
- `docs/spec-bootstrap-deploy.md` — Authoritative spec for bootstrap & deploy workflows (965 lines)
- `internal/workflow/engine.go` — Workflow engine orchestration (484 lines)
- `internal/workflow/bootstrap.go` — Bootstrap state machine, step progression (347 lines)
- `internal/workflow/deploy.go` — Deploy workflow state machine (344 lines)
- `internal/workflow/service_meta.go` — Persistent service metadata (119 lines, recently trimmed)
- `internal/workflow/validate.go` — Plan validation against live catalog (276 lines)
- `internal/workflow/guidance.go` — Guidance assembly coordination (134 lines)
- `internal/workflow/bootstrap_guidance.go` — Section extraction, iteration delta (143 lines)
- `internal/workflow/bootstrap_guide_assembly.go` — Knowledge injection for bootstrap (89 lines)
- `internal/workflow/deploy_guidance.go` — Deploy step guidance resolution (87 lines)
- `internal/workflow/bootstrap_steps.go` — Step definitions and metadata (61 lines)
- `internal/workflow/bootstrap_outputs.go` — ServiceMeta + reflog writing (75 lines)
- `internal/workflow/bootstrap_checks.go` — StepChecker type definition (24 lines)
- `internal/workflow/router.go` — Workflow routing logic (320 lines)
- `internal/workflow/cicd.go` — CI/CD workflow state (184 lines)
- `internal/workflow/cicd_guidance.go` — CI/CD guidance resolution (26 lines)
- `internal/workflow/managed_types.go` — Managed service detection + project state (81 lines)
- `internal/workflow/session.go` — Session persistence (234 lines)
- `internal/workflow/state.go` — State types (line count TBD)
- `internal/workflow/registry.go` — Session registry (235 lines)
- `internal/workflow/reflog.go` — Reflog append to CLAUDE.md
- `internal/tools/workflow.go` — MCP tool handler, action dispatch (348 lines)
- `internal/tools/workflow_bootstrap.go` — Bootstrap tool handlers (174 lines)
- `internal/tools/workflow_deploy.go` — Deploy tool handlers (68 lines)
- `internal/tools/workflow_checks.go` — Step checkers (339 lines)
- `internal/tools/workflow_checks_generate.go` — Generate step checker
- `internal/tools/workflow_checks_deploy_test.go` — Deploy checker tests
- `internal/tools/workflow_checks_strategy.go` — Strategy checker
- `internal/tools/workflow_strategy.go` — Strategy action handler (119 lines)
- `internal/tools/workflow_cicd.go` — CI/CD tool handlers
- `internal/content/workflows/bootstrap.md` — Bootstrap guidance content (824 lines)
- `internal/content/workflows/deploy.md` — Deploy guidance content (200 lines)
- `internal/content/workflows/cicd.md` — CI/CD guidance content (160 lines)

**Recent trimming commits** (context for the review direction):
- `998dd33` refactor: trim dead fields, managed dep metas, and stale API shadows from ServiceMeta
- `4e358aa` refactor: simplify ServiceMeta — remove Status, Type, Decisions fields
- `9fce717` feat: enforce step checkers as real gates
- `b17191d` refactor: redesign guidance assembly with layer-based architecture

**Key questions for analysts**:
1. What duplication or dead weight remains in the workflow package after recent trims?
2. Are there abstractions that exist for only one use and could be inlined?
3. Is the guidance assembly chain (guidance.go → bootstrap_guidance.go → bootstrap_guide_assembly.go → deploy_guidance.go) unnecessarily split?
4. Does the response structure (BootstrapResponse, DeployResponse, CICDResponse) carry fields that LLMs don't use?
5. Is the router over-engineered for current needs?
6. Are there code paths that exist for edge cases that never happen in practice?
7. What would make the flows clearer for an LLM consuming the guidance output?
