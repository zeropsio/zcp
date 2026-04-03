# Analysis: Bootstrap Flow Instructions + yml→yaml Migration
**Date**: 2026-04-03
**Task**: Two-part analysis:
1. Improve bootstrap workflow instructions to include example tool calls (prevent LLM parameter guessing)
2. Migrate default file extension from `.yml` to `.yaml` across ZCP (with `.yml` fallback)
**Reference files**:
- `internal/content/workflows/bootstrap.md` — Bootstrap workflow step guidance (1136 lines)
- `internal/workflow/guidance.go` — Knowledge section lookups for guidance assembly
- `internal/workflow/bootstrap_guidance.go` — Step guidance resolution
- `internal/workflow/bootstrap_guide_assembly.go` — Guide assembly with injected knowledge
- `internal/tools/import.go` — zerops_import MCP tool handler
- `internal/ops/import.go` — Import business logic
- `internal/ops/deploy_validate.go` — zerops.yml parsing (ParseZeropsYml, ZeropsYmlDoc types)
- `internal/workflow/recipe_templates.go` — Recipe file generation (already uses import.yaml)
- `internal/sync/transform.go` — Recipe markdown extraction (handles both .yml/.yaml)
- `internal/tools/workflow_checks_finalize.go` — Recipe finalization (import.yaml validation)
- `internal/knowledge/themes/core.md` — Knowledge section headers including "zerops.yml Schema"
