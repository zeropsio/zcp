# Context: analysis-e2e-test-quality
**Last updated**: 2026-03-26
**Iterations**: 1
**Task type**: codebase-analysis

## Decision Log
| # | Decision | Evidence | Iteration | Rationale |
|---|----------|----------|-----------|-----------|
| D1 | Keep all 13 bootstrap tests (no consolidation) | Adversarial: each (mode, runtime, dep) triplet has unique assertions | 1 | Per-runtime diversity catches version drift; parametrizing loses isolation |
| D2 | Deploy error classification test is sound | Code: deploy_error_classification_test.go:130-191 exercises real SSH errors | 1 | Primary misread test structure; adversarial confirmed robustness |
| D3 | verify_test.go fix = add workflow start (not session-less mode) | Code: tools/verify.go has no requireWorkflow; tools/import.go:29 has it | 1 | Only import needs workflow; verify is workflow-free |

## Rejected Alternatives
| # | Alternative | Evidence Against | Iteration | Why Rejected |
|---|-------------|-----------------|-----------|--------------|
| A1 | Consolidate 5 bootstrap tests into 1 parametrized test | Each has unique dep type assertions (e.g., assertNoEnvVarCheck for storage) | 1 | Would lose per-test cleanup isolation and diagnostic clarity |
| A2 | Make verify tool accept nil session | verify.go already works without session; it's import that needs it | 1 | Wrong diagnosis — fix the test, not the tool |
| A3 | Flag deploy_error_classification_test as weak | Test exercises real SSH errors with negative assertions for RC2 bug | 1 | Primary's biggest finding was itself wrong |

## Resolved Concerns
| # | Concern | Evidence | Raised In | Resolved In | Resolution |
|---|---------|----------|-----------|-------------|------------|
| RC1 | "50% bootstrap redundancy" | Adversarial showed unique (mode, runtime, dep) per test | 1 | 1 | Intentional diversity |
| RC2 | "Deploy error classification has weak assertions" | Code shows real SSH errors + negative RC2 assertions | 1 | 1 | Test is robust |
| RC3 | "No negative tests exist" | bootstrap_negative_test.go has 3 failure scenarios | 1 | 1 | Already exist |

## Open Questions (Unverified)
- php@8.4 vs php-nginx@8.4 in recipe references — needs catalog verification
- Whether import_zeropsyaml stale session is from disk persistence or engine state

## Confidence Map
| Area | Confidence | Evidence Basis |
|------|------------|----------------|
| Broken tests (C1-C3) | HIGH | Code + live test results |
| Redundancy assessment | HIGH | Primary + 2 adversarial reviews |
| Deploy verification gap (M2) | HIGH | Code analysis (status-only polling) |
| Coverage gaps (m1-m4) | MEDIUM | Code grep, not exhaustive |
| php@8.4 recipe issue (m5) | LOW | Inferred from test output |
