# Analysis: Infrastructure-to-Repo for buildFromGit
**Date**: 2026-04-03
**Task**: Study all documentation, knowledge, examples, and workflows to determine if a guide/workflow exists that enables: "from current infrastructure, create a repo that will work for buildFromGit". Identify what exists, what's missing, and how it could work.
**Task type**: codebase-analysis
**Complexity**: Deep (ultrathink) — 4 agents

**Reference files**:
- `internal/knowledge/themes/core.md` — core platform knowledge, includes buildFromGit examples
- `internal/knowledge/guides/` — all embedded guides (ci-cd, deployment-lifecycle, production-checklist, etc.)
- `internal/content/workflows/bootstrap.md` — bootstrap workflow (create/adopt services)
- `internal/content/workflows/deploy.md` — deploy workflow
- `internal/content/workflows/recipe.md` — recipe workflow (creates reference repos with 6 env tiers)
- `docs/spec-bootstrap-deploy.md` — workflow step specs
- `docs/zrecipator/zrecipator-be-framework-meta.md` — recipe creation meta-guide
- `../zerops-docs/apps/docs/content/references/import.mdx` — Import & Export YAML docs (has Export section)
- `internal/ops/discover.go` — discover operation (gets project/service/env info)
- `internal/ops/env_export.go` — .env file generation from discover
- `internal/tools/discover.go` — MCP discover tool handler

**File map** (relevant to buildFromGit reverse-engineering):
| File | Description | Lines |
|------|------------|-------|
| `internal/knowledge/themes/core.md` | Core platform knowledge | ~250 |
| `internal/content/workflows/bootstrap.md` | Bootstrap workflow conductor | ~1000 |
| `internal/content/workflows/recipe.md` | Recipe workflow (creates repos) | ~280 |
| `internal/content/workflows/deploy.md` | Deploy workflow | ~??? |
| `internal/ops/discover.go` | Discover operation | ~200 |
| `internal/ops/env_export.go` | .env generation | ~50 |
| `docs/spec-bootstrap-deploy.md` | Workflow specs | ~500 |
| `../zerops-docs/.../import.mdx` | Import & Export YAML reference | ~820 |
| `docs/zrecipator/zrecipator-be-framework-meta.md` | Recipe creation guide | ~1400 |
