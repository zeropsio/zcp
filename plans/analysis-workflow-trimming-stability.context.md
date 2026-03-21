# Review Context: analysis-workflow-trimming-stability

**Last updated**: 2026-03-21
**Reviews completed**: 1
**Resolution method**: Evidence-based

## Decision Log

| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
| 1 | Keep CompleteStep triplicated (don't unify) | 80L total, interface overhead exceeds savings | R1 | Adversarial C1: Go interface/embedding complexity defeats DRY benefit for 3x25L methods |
| 2 | Keep 5-file guidance chain (don't collapse) | Each layer has single responsibility, 350L limit prevents collapse | R1 | Architecture F4 + Adversarial C2: separation is justified |
| 3 | Keep response types separate (don't share Progress/StepInfo) | 18L total duplication, workflows may diverge (DeployTargetOut has Role) | R1 | Architecture F7: coupling cost > duplication cost |
| 4 | Remove StepCategory from response JSON, keep in internal metadata | Zero if/switch usage, pure serialization pass-through | R1 | Correctness F6 + Architecture F2: false API signal |
| 5 | Keep `intent` field in response | Near-zero cost, marginal context recovery value | R1 | Adversarial context recovery argument |
| 6 | Optimize availableStacks: discover-only, not generate | Agent already chose types at generate; discover needs it | R1 | Correctness F8 + Adversarial D2 compromise |

## Rejected Alternatives

| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
| 1 | Unify CompleteStep into shared interface | Go interface overhead, embedding complicates JSON marshaling | R1 | Adversarial C1: complexity > savings |
| 2 | Collapse guidance files into single file | Would exceed 350L; lose testability separation | R1 | Architecture F4: justified modularity |
| 3 | Remove all echo fields from response | `intent` has marginal context recovery value | R1 | Adversarial: session interruption recovery |
| 4 | Rename PlanModeDev → PlanModeRuntimeOnly | No evidence of actual LLM confusion; 46 usages = high churn | R1 | Adversarial G2: unverified concern |

## Resolved Concerns

| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| 1 | extractDeploySection duplication | Byte-identical with ExtractSection | R1 | R1 | Action item A1: delete duplicate |
| 2 | Managed prefix list divergence risk | Two lists, same prefixes, no sync enforcement | R1 | R1 | Action item A2: consolidate |
| 3 | Dead deploy target tracking cluster | UpdateTarget/DevFailed/Error/LastAttestation unused | R1 | R1 | Action items A4-A6: delete all |
| 4 | Environment type dead weight | Threaded through all BuildResponse, `_ ignored` | R1 | R1 | Action item A7: delete entirely |

## Open Questions (Unverified)

| # | Question | Context |
|---|---------|---------|
| 1 | Does SSH stderr actually contain credentials in practice? | Security F1: code path confirmed, content unverified |
| 2 | Does PlanModeDev naming cause LLM confusion? | Adversarial G2: no evidence either way |
| 3 | Should CI/CD workflow have knowledge injection? | Currently bypasses assembleGuidance entirely |
| 4 | Should CompleteStep checker asymmetry be documented/tested? | Adversarial G1: state-level has no validation |

## Confidence Map

| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Dead code cluster (deploy.go) | HIGH | 4 analysts confirmed, grep verified |
| Duplication (ExtractSection, managed prefixes) | HIGH | Byte-identical code, 3 analysts confirmed |
| Environment type dead | HIGH | 3 analysts confirmed, grep verified |
| StepCategory useless in response | HIGH | Grep: zero conditional logic |
| Guidance chain justified | HIGH | Architecture + Adversarial agreement |
| CompleteStep keep separate | MEDIUM | Adversarial argument strong but unquantified |
| SSH error leakage | MEDIUM | Code path verified, content unverified |
| Response echo field values | LOW | Subjective LLM consumption assessment |
