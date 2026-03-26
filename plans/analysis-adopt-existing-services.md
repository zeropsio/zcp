# Analysis: Adopting Pre-Existing Services into ZCP Workflows

**Date**: 2026-03-26
**Task**: Design support for workflows to handle pre-existing services in a project that weren't originally bootstrapped via ZCP. Scenario: user has a project with existing services, adds ZCP, and wants ZCP to work with those runtime services. Deep analysis of consequences, dependencies, and potential fundamental changes needed.
**Task Type**: implementation-planning
**Complexity**: Deep (ultrathink) — 4 agents

**Reference files**:
- `internal/workflow/bootstrap.go` (337L) — Bootstrap state machine, plan modes, step completion
- `internal/workflow/validate.go` (277L) — Plan validation, BootstrapTarget/RuntimeTarget types, resolution logic
- `internal/workflow/managed_types.go` (86L) — Project state detection (FRESH/CONFORMANT/NON_CONFORMANT)
- `internal/workflow/engine.go` (351L) — Workflow engine, session lifecycle, bootstrap orchestration
- `internal/workflow/service_meta.go` (118L) — ServiceMeta persistence, IsComplete()
- `internal/workflow/bootstrap_outputs.go` (72L) — Meta writing on provision/completion
- `internal/workflow/bootstrap_guidance.go` (142L) — Guidance assembly, progressive sections
- `internal/workflow/bootstrap_steps.go` (44L) — Step definitions
- `internal/workflow/state.go` (38L) — ProjectState enum, WorkflowState
- `internal/tools/workflow_checks.go` (290L) — Step checkers (provision, generate, deploy)
- `internal/tools/workflow_checks_generate.go` (229L) — Generate step validation
- `internal/tools/workflow_checks_deploy.go` (232L) — Deploy step validation
- `internal/tools/workflow.go` (249L) — MCP tool handler
- `internal/tools/workflow_bootstrap.go` (163L) — Bootstrap-specific handlers
- `internal/tools/workflow_strategy.go` (200L) — Strategy assignment
- `internal/content/workflows/bootstrap.md` (831L) — Bootstrap workflow guidance
- `docs/spec-bootstrap-deploy.md` (971L) — Authoritative workflow spec
