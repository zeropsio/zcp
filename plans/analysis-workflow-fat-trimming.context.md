# Review Context: analysis-workflow-fat-trimming

**Last updated**: 2026-03-21
**Reviews completed**: 1
**Resolution method**: Evidence-based

## Decision Log

| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
| 1 | `DeployTarget.LastAttestation` IS dead code | `GuidanceParams.LastAttestation` populated from `BootstrapState.lastAttestation()`, not `DeployTarget` | R1 | Adversarial confused two different `LastAttestation` fields on different structs |
| 2 | `StepCategory` type should be removed | Type never branched on (grep: zero `switch`/`if` on category) | R1 | String literals preserve JSON output without type overhead |
| 3 | `hasDevStagePattern` function stays | Called from `DetectProjectState`, real business logic | R1 | Single-entry loop is trivial cruft, not worth separate change |
| 4 | Deploy per-target tracking is a dead cluster | `UpdateTarget`, `DevFailed`, `Error`, `LastAttestation` all connected, none used | R1 | Engine is step-based, not target-based |
| 5 | `InitSession` is superseded | `InitSessionAtomic` wraps both operations atomically with exclusivity | R1 | Old function has race condition between file write and registry |

## Rejected Alternatives

| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
| 1 | Keep `DeployTarget.Error` for future use | Zero prod callers, git preserves history | R1 | Dead fields mislead maintainers; re-add when needed |
| 2 | Extract shared response base type | Each workflow has unique fields (AvailableStacks, Targets, Provider) | R1 | Abstraction cost > duplication cost |
| 3 | Merge guidance files | Each workflow has distinct guidance logic | R1 | Separation by workflow is correct |

## Resolved Concerns

| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| 1 | `LastAttestation` might be active | Traced data flow: different structs | R1 | R1 | Dead — `BootstrapState` has its own `lastAttestation()` |
| 2 | `StepCategory` might gate behavior | Grep for switch/if on category: zero hits | R1 | R1 | Informational only — string literals sufficient |
| 3 | `extractDeploySection` cross-package issue | `tools/` already imports `workflow/` | R1 | R1 | No import barrier — direct call works |

## Open Questions (Unverified)

- Evidence directory cleanup: should `ResetSessionByID` also remove `.zcp/state/evidence/{id}/`?
- Are there external consumers of `DeployTarget` JSON that depend on `Error`/`LastAttestation` fields?

## Confidence Map

| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Deploy dead code cluster (V2-V5) | HIGH | grep: zero prod callers, 4 analysts agree |
| `InitSession` removal (V1) | HIGH | grep: zero prod callers, superseded by atomic |
| Duplicate functions (V6-V7) | HIGH | diff: identical logic, 3 analysts agree |
| `StepCategory` removal (V8) | MEDIUM | grep: never branched on, but LLM reads the value |
| `SaveSessionState` removal (V9) | MEDIUM | 1 test caller needs migration |
| Evidence cleanup (L2) | LOW | Only orphaned files observed, no code audit |
