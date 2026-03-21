# Analysis: Meta Status — Current Usage, Purpose, and What Should Meta Store

**Date**: 2026-03-20
**Task**: Analyze how meta status is used throughout the workflow system. It's currently being written but never read for decisions. Instead of declaring it dead code, think about how it could be used. Also think about what we actually need in meta — avoid creating a different source of truth than what's actually in the project. Meta should serve as a meta store for ZCP's needs only.

**Reference files**:
- `~/.claude/plans/memoized-noodling-spindle.md` — Previous deep review that identified V4 (MetaStatusDeployed unused) and V5 (MetaStatusPlanned written but never read) as findings
- `internal/workflow/service_meta.go` — ServiceMeta struct definition, CRUD operations, status constants
- `internal/workflow/bootstrap_outputs.go` — Where metas are written (writeBootstrapOutputs, writeServiceMetas)
- `internal/workflow/engine.go` — Calls writeServiceMetas at plan completion (planned) and provision completion (provisioned), writeBootstrapOutputs at bootstrap end (bootstrapped)
- `internal/workflow/service_context.go` — BuildServiceContextSummary reads metas for markdown summary (renders status as text)
- `internal/workflow/deploy_guidance.go` — ResolveDeployGuidance reads meta for strategy→guidance mapping
- `internal/workflow/router.go` — Route() uses metas for workflow offerings (uses Decisions, Mode, StageHostname — NOT Status)
- `internal/workflow/deploy.go` — BuildDeployTargets reads metas for deploy targets (uses Mode, Type, StageHostname, Dependencies — NOT Status)
- `internal/tools/workflow.go` — handleDeployStart/handleCICDStart read metas to build targets/hostnames
- `internal/tools/workflow_strategy.go` — handleStrategy reads/writes meta for strategy decisions; handleRoute passes metas to router
- `internal/tools/delete.go` — DeleteServiceMeta cleanup on service deletion
- `internal/server/instructions.go` — ListServiceMetas for system prompt routing

**Key observations**:
1. Status is WRITTEN at 3 points: planned (after plan completion), provisioned (after provision), bootstrapped (at bootstrap end)
2. Status is RENDERED in BuildServiceContextSummary (service_context.go:29) as text decoration
3. Status is NEVER used for any conditional logic, routing, or decision-making
4. All actual consumers use: Hostname, Type, Mode, StageHostname, Dependencies, Decisions — NOT Status
5. The previous review (memoized-noodling-spindle.md) recommended removing MetaStatusDeployed and MetaStatusPlanned as dead code
6. The user wants to think about what meta SHOULD store vs what creates a redundant source of truth
