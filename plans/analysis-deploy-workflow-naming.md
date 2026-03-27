# Analysis: Deploy Workflow Naming and Conceptual Identity
**Date**: 2026-03-27
**Task**: Deep analysis of whether the "deploy workflow" should be named differently — examining what it actually does, how and when it's used, what it serves, and whether "deploy" accurately captures its purpose.
**Reference files**:
- `internal/workflow/deploy.go` (323L) — Deploy state model, step definitions, target ordering, response building
- `internal/workflow/engine_deploy.go` (107L) — Engine methods for deploy session lifecycle
- `internal/workflow/deploy_guidance.go` (293L) — Personalized step guidance generation (prepare/deploy/verify)
- `internal/workflow/router.go` (254L) — Strategy-to-workflow routing, flow offerings
- `internal/tools/workflow_deploy.go` (247L) — MCP tool handlers, preflight gates, strategy validation
- `internal/tools/workflow_checks_deploy.go` (232L) — Prepare/deploy step checkers (zerops.yml validation, env var refs)
- `internal/ops/deploy_local.go` (187L) — Local machine deployment via zcli push
- `internal/ops/deploy_ssh.go` (185L) — SSH self-deploy and cross-deploy
- `internal/ops/deploy_validate.go` (322L) — zerops.yml parsing, config validation
- `internal/content/workflows/deploy.md` (219L) — Embedded guidance sections
- `docs/spec-bootstrap-deploy.md` — Authoritative specification (§4 covers deploy)
- `internal/tools/workflow.go` (250L) — Workflow dispatcher, action routing
