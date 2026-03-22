# Analysis: Bootstrap Flow for Managed-Only Services
**Date**: 2026-03-22
**Task**: Trace the exact flow when bootstrap runs for a single managed service (user says "add a database/MQ/cache"). Full analysis from LLM perspective, adversarial review, assess if changes needed.
**Task type**: flow-tracing
**Complexity**: Deep (ultrathink) — 4 agents

**Reference files**:
- `internal/workflow/bootstrap.go` — Bootstrap state machine, step progression, skip validation
- `internal/workflow/bootstrap_steps.go` — 5-step definition (discover, provision, generate, deploy, close)
- `internal/workflow/bootstrap_checks.go` — StepChecker types
- `internal/workflow/bootstrap_guidance.go` — Section extraction, progressive guidance
- `internal/workflow/bootstrap_guide_assembly.go` — buildGuide, env var formatting
- `internal/workflow/guidance.go` — assembleGuidance, knowledge injection
- `internal/workflow/validate.go` — Plan validation, BootstrapTarget types
- `internal/workflow/managed_types.go` — IsManagedService, DetectProjectState
- `internal/workflow/router.go` — Workflow routing
- `internal/workflow/engine.go` — Engine: BootstrapStart, BootstrapComplete, BootstrapSkip
- `internal/workflow/bootstrap_outputs.go` — writeBootstrapOutputs, writeProvisionMetas
- `internal/workflow/session.go` — Session lifecycle
- `internal/tools/workflow.go` — MCP tool handler routing
- `internal/tools/workflow_bootstrap.go` — handleBootstrapComplete, handleBootstrapSkip
- `internal/tools/workflow_checks.go` — Provision/deploy checkers
- `internal/tools/workflow_checks_generate.go` — Generate step checker
- `internal/content/workflows/bootstrap.md` — Workflow guidance content

## Key Question

When a user requests "add a PostgreSQL database" (or any managed service) without any runtime service, what is the exact step-by-step flow through the bootstrap system? Is the guidance correct, are checkers appropriate, is the fast-path (skip generate/deploy/close) well-supported?
