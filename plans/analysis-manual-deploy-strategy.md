# Analysis: Manual Deploy Strategy — What We Say and How It Works

**Date**: 2026-03-23
**Task**: Study what ZCP currently says about the "manual" deploy strategy — what guidance is provided, what the code does, and how it's expected to function end-to-end.
**Task type**: codebase-analysis
**Complexity**: Deep (ultrathink, 4 agents)

**Reference files**:
- `internal/workflow/service_meta.go` — Strategy constants (StrategyManual = "manual"), ServiceMeta struct
- `internal/tools/workflow_strategy.go` — handleStrategy(), buildStrategyGuidance(), buildStrategySelectionResponse()
- `internal/workflow/deploy_guidance.go` — StrategyToSection mapping, buildPrepareGuide(), buildDeployGuide(), writeStrategyNote()
- `internal/content/workflows/deploy.md` — deploy-manual section (3 lines of guidance)
- `internal/content/workflows/bootstrap.md` — close section mentioning strategy selection
- `internal/workflow/bootstrap_guide_assembly.go` — BuildTransitionMessage() with strategy presentation
- `internal/workflow/bootstrap_outputs.go` — writeBootstrapOutputs() persisting strategy to ServiceMeta
- `internal/workflow/router.go` — strategyOfferings() routing for manual strategy
- `internal/workflow/deploy.go` — DeployState, BuildDeployTargets(), deploy step machinery
- `internal/tools/workflow_deploy.go` — handleDeployStart() with strategy gate
- `docs/spec-bootstrap-deploy.md` — Spec sections 4.2-4.6 covering strategy gate and deploy flow
