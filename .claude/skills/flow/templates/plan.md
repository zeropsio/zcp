# Plan: <slug>

## Run State
- `phase:` <frame|prove|shape|awaiting-approval|build|assemble|awaiting-retest|land|archived>
- `base:` <SHA the plan was approved against>
- `integration:` <current integration SHA + landed commit range>
- `approved:` <Rev-N, date — owner approval>
- `codex:` <verdict + review file path>
- `next:` <single next action, zero-context executable>
<!-- material edit to Frame or Slice Register after approval resets phase to awaiting-approval -->

## Frame
**Outcome**: <observable result>
| obs | evidence |
|---|---|
| <observed fact> | <path/command/output> |
- AC1: <criterion> — planned evidence: <check/scenario>
- AC2: <criterion> — planned evidence: <check/scenario>
**Non-goals**: <explicitly excluded> · **Constraints**: <compat/security/data/ops>
**Risk class**: <low|medium|high> — trigger: <named FULL trigger, or "owner asked">
**Assumptions**:
- [VERIFIED] <claim> — <cite path:line>
- [PROBE] <uncertain + load-bearing claim>
- [ASSUMED] <uncertain, not load-bearing claim>
## Evidence Ledger
| claim | gates | surface | command | observed | verdict | promote |
|---|---|---|---|---|---|---|
| <claim> | <ACx/assumption it gates> | repo\|mcp\|verifier\|spike | <exact command> | <result> | CONFIRMED\|REFUTED\|INCONCLUSIVE | <permanent test/spec §> |
## Slice Register
| ID | Title | Depends | Files | Layers | Gate | State |
|---|---|---|---|---|---|---|
| S1 | <tracer bullet> | — | <write-set> | <unit/tool/integration/e2e> | autonomous\|review\|owner | pending |
Gate ∈ autonomous\|review\|owner · State ∈ pending\|building\|landed\|blocked. Overlapping `Files` never share a wave.

## Verify Trace
| ACx | check | result | evidence |
|---|---|---|---|
| AC1 | <deterministic check/scenario> | passed\|failed\|blocked\|not-run | <output/log path> |
| — | negative/regression: <case> | passed\|failed\|blocked\|not-run | <evidence> |

## Promotion
- Contracts → `docs/spec-<name>.md` §<n>
- Invariants → test `Test<Op>_<Scenario>_<Result>` + spec §<n>
- CLAUDE.md trap line (≤1): <line, or "none">
- This plan → `plans/archive/` on LAND close
