# Analysis: Improving ZCP Initial Instructions for LLM Clients
**Date**: 2026-03-30
**Task**: Analyze how to improve initial instructions so LLM clients better understand ZCP, handle services without bootstrap state/meta, check knowledge before code changes, and run proper workflows
**Task Type**: codebase-analysis
**Complexity**: Deep (4 agents)

## Reference files
- `internal/server/instructions.go` — System prompt builder, project state detection, routing
- `internal/server/instructions_orientation.go` — Post-bootstrap per-service guidance
- `internal/workflow/router.go` — Workflow routing based on project state + metas
- `internal/workflow/service_meta.go` — ServiceMeta structure, IsComplete()
- `internal/workflow/engine.go` — Workflow orchestration, session management
- `internal/workflow/guidance.go` — Guidance assembly per step
- `internal/workflow/bootstrap_guide_assembly.go` — Bootstrap guidance layers
- `internal/ops/discover.go` — Discovery without ServiceMeta awareness
- `internal/tools/workflow.go` — zerops_workflow MCP tool
- `internal/tools/knowledge.go` — zerops_knowledge MCP tool
- `internal/knowledge/engine.go` — BM25 search, provider interface
- `internal/knowledge/briefing.go` — Stack-specific briefing assembly
- `internal/content/workflows/bootstrap.md` — Bootstrap workflow guidance template

## Analysis Output
- [analysis-1](analysis-initial-instructions-improvement.analysis-1.md) — Full findings, recommendations, implementation sequence
- [context](analysis-initial-instructions-improvement.context.md) — Decision log, rejected alternatives, confidence map

## Implementation Status: COMPLETE (2026-03-30)
ProjectState removed. Fact-based routing implemented. All tests pass, lint clean.
