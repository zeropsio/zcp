# Analysis: Knowledge System Redesign

**Date**: 2026-04-02
**Status**: Complete — ready for implementation
**Architecture & implementation**: `plans/analysis-knowledge-redesign.analysis-1.md`
**Content specification**: `plans/analysis-knowledge-redesign.content-spec.md`
**Decisions & context**: `plans/analysis-knowledge-redesign.context.md`

## Agents Used
- KB specialist (zerops-knowledge) — platform facts, delivery model analysis
- Primary Architect (Explore) — content classification, architecture proposal
- Adversarial Challenger (Explore) — challenge findings, identify routing as root cause
- Correctness Reviewer (Explore) — verify all 11 findings against code (SOUND)
- Completeness Reviewer (Explore) — find gaps: adoption flow, deploy paths, test impact

## Key Findings
1. core.md mixes universal + flow + runtime content (~50L to extract)
2. operations.md mixes ops + flow content (~125L to extract)
3. 25 files (guides/ + decisions/) orphaned from structured delivery
4. Stack Composition concept lost when recipes became hello-world format
5. Provision step gets only schema — needs examples + universals + resource hints + decisions
6. Adoption flow: checkDeploy verifies /health on services that may not have it

## Implementation: 27 steps across 5 phases
See analysis-1.md "Implementation Sequence" for full ordered plan.
