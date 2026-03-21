# Analysis: Bootstrap Flow Gates & Termination

**Date**: 2026-03-21
**Task**: Study the current bootstrap workflow implementation, compare with spec (`docs/spec-bootstrap-deploy.md`), document discrepancies. Then analyze and propose:
1. A gate at bootstrap start to ensure mode is determined early
2. Refined bootstrap termination — bootstrap should close before CI/CD strategy, with strategy presented as a "next step"
3. Whether to block certain steps until the user decides on deployment strategy

**Reference files**:
- `docs/spec-bootstrap-deploy.md` — Bootstrap & deploy workflow specification
- `internal/workflow/engine.go` — Workflow engine (session lifecycle)
- `internal/workflow/bootstrap.go` — Bootstrap state, steps, completion logic
- `internal/workflow/bootstrap_steps.go` — Step definitions (6 steps)
- `internal/workflow/bootstrap_outputs.go` — Final meta writing, reflog
- `internal/workflow/bootstrap_guide_assembly.go` — Guidance assembly, transition message
- `internal/workflow/bootstrap_guidance.go` — Guidance extraction from markdown
- `internal/workflow/bootstrap_checks.go` — StepChecker types
- `internal/workflow/validate.go` — Plan validation, target types
- `internal/workflow/service_meta.go` — ServiceMeta persistence
- `internal/workflow/router.go` — Flow routing based on project state
- `internal/workflow/cicd.go` — CI/CD workflow (3 steps)
- `internal/workflow/cicd_guidance.go` — CI/CD guidance
- `internal/workflow/state.go` — WorkflowState types
- `internal/tools/workflow.go` — MCP tool handler, action routing
- `internal/tools/workflow_bootstrap.go` — Bootstrap tool handlers
- `internal/tools/workflow_strategy.go` — Strategy tool + route handler
- `internal/tools/workflow_checks.go` — Step checkers (provision, deploy, verify)
- `internal/tools/workflow_checks_strategy.go` — Strategy checker
