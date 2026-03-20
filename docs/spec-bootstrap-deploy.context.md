# Review Context: spec-bootstrap-deploy
**Last updated**: 2026-03-20
**Reviews completed**: 1
**Resolution method**: Evidence-based

## Decision Log
| # | Decision | Evidence | Review | Rationale |
|---|----------|----------|--------|-----------|
| D1 | Keep 3-mode system in code, unify guidance | ~220L clean mode code; standard/dev guidance >80% identical | R1 | Modes are real at platform level but guidance duplication is wasteful |
| D2 | Delete StepChecker interface | bootstrap_checks.go:21 never implemented, all callers pass nil | R1 | Dead abstraction, zero production value |
| D3 | Remove ServiceMeta intermediate states (planned/provisioned) | Written at engine.go:164,240 but never read for decisions | R1 | Ghost state machine — no code branches on these values |
| D4 | Session/registry locking is justified | File locks, PID tracking, atomic writes all verified correct | R1 | Prevents real race conditions in multi-process scenarios |
| D5 | Env var 2-tier design (values transient, names persistent) is correct | Security analysis confirmed no secret leaks | R1 | Intentional asymmetry, verified safe |
| D6 | Reduce guidance from narrative scripts to structural guardrails | Architecture analysis: text doesn't prevent errors, code gates do | R1 | Agents follow same flow regardless of guidance length |

## Rejected Alternatives
| # | Alternative | Evidence Against | Review | Why Rejected |
|---|------------|-----------------|--------|--------------|
| X1 | Unify Standard+Dev modes into single mode with skipStage flag | Architecture: modes ARE genuinely different (~20L code); merging adds conditional complexity | R1 | Code handles modes cleanly; only guidance needs unification |
| X2 | Remove session persistence entirely | Security: simplification reduces defenses against concurrent corruption; Architecture: crash recovery is real use case | R1 | Infrastructure justified for crash recovery + exclusivity |
| X3 | Remove attestation minimum | Adversarial: prevents "ok" attestations; cost is ~3 lines | R1 | Negligible cost, provides minimal quality gate |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| C1 | Is the system over-engineered? | ~85% essential, ~15% ballast (3 dead code items + content duplication) | R1 | R1 | PARTIALLY — macro architecture sound, micro-level cleanup needed |
| C2 | Does complexity introduce security risks? | Security analysis: zero new attack surfaces, all file ops atomic | R1 | R1 | NO — complexity reduces attack surface |
| C3 | Are all 10 platform behaviors real? | kb-verifier confirmed all 10 (9 full, 1 partial) | R1 | R1 | YES — complexity reflects genuine Zerops quirks |

## Open Questions (Unverified)
- Does guidance actually improve LLM success rates? (needs A/B telemetry)
- Session resumption never E2E tested (crash→resume→complete)
- Attestation 10-char boundary never boundary-tested

## Confidence Map
| Section/Area | Confidence | Evidence Basis |
|-------------|------------|---------------|
| Platform behavior claims (§3-4) | HIGH | All 10 claims verified on live platform |
| Session/registry architecture (§5.1) | HIGH | Code inspection + security analysis |
| ServiceMeta lifecycle (§5.2) | MEDIUM | Status field write-only, spec claim contradicts code |
| Mode behavior matrix (§6) | HIGH | Code confirmed, modes real but guidance duplicated |
| Invariants (§8) | MEDIUM | Some invariants reference dead code (StepChecker, intermediate metas) |
| Recovery patterns (§9) | HIGH | Platform behaviors verified, patterns match real failures |
| Known gaps (§10) | HIGH | Accurately identifies real code gaps |
