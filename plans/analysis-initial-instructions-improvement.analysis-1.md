# Analysis: Improving ZCP Initial Instructions for LLM Clients — Iteration 1
**Date**: 2026-03-30
**Scope**: internal/server/instructions.go, instructions_orientation.go, internal/workflow/router.go, managed_types.go, service_meta.go, internal/ops/discover.go, internal/tools/*.go, internal/knowledge/engine.go, briefing.go, internal/content/workflows/bootstrap.md
**Agents**: kb (zerops-knowledge), explorer-instructions (Explore), explorer-state-gaps (Explore), primary-analyst (Explore), adversarial (Explore)
**Complexity**: Deep (4 agents + orchestrator)
**Task**: Improve initial instructions so LLM clients better understand ZCP, handle services without bootstrap state/meta, and check knowledge/run proper workflows before code changes

## Summary

ZCP's instruction delivery system has a **structural short-circuit** that suppresses routing guidance in the most common failure scenario: mixed projects where some services are bootstrapped and others are external. The root cause is `instructions.go:208-211` — when ANY complete ServiceMeta exists, `buildPostBootstrapOrientation()` returns early, **skipping the router entirely**. This means adoption hints, workflow offerings, and strategic guidance disappear exactly when they're most needed. Additionally, `zerops_discover` provides no indicator of bootstrap status, making it impossible for the LLM to distinguish ZCP-managed from external services. Knowledge injection is tightly coupled to the bootstrap workflow, leaving LLMs operating outside workflows without platform context.

## Findings by Severity

### Critical

| # | Finding | Evidence | Source | Confidence |
|---|---------|----------|--------|------------|
| F1 | **System prompt short-circuit**: `buildPostBootstrapOrientation()` at instructions.go:208-211 returns early when ANY complete ServiceMeta exists, SKIPPING router entirely. Mixed projects (bootstrapped + external services) lose all adoption hints and workflow offerings. | instructions.go:208-211 — `return b.String()` before router code at lines 214-229 | Adversarial (MF1), verified by orchestrator | VERIFIED |
| F2 | **Discovery has no bootstrap indicator**: `zerops_discover` response (`ServiceInfo` struct at ops/discover.go:28-40) contains no field indicating whether a service has ServiceMeta. LLM cannot distinguish ZCP-managed from external services. | ops/discover.go:28-40 — struct lacks any meta-awareness field | Primary + Explorer | VERIFIED |
| F3 | **Bootstrap adoption guidance is a stub**: bootstrap.md discover section lists option (c) "work with existing" with one line, zero concrete steps for setting `IsExisting=true`, mapping existing services, or preserving existing code. | bootstrap.md discover section, router.go:129-142 hint text | Explorer-state-gaps | VERIFIED |

### Major

| # | Finding | Evidence | Source | Confidence |
|---|---------|----------|--------|------------|
| F4 | **Project state detection ignores ServiceMeta**: `DetectProjectState()` at managed_types.go:36-62 classifies by hostname pattern only (dev/stage suffixes). "CONFORMANT" means "naming matches" not "services are ZCP-managed". External services with conformant names are invisible. | managed_types.go:36-62 — no ServiceMeta check | Primary (F8) | VERIFIED |
| F5 | **Knowledge injection bootstrap-only**: Knowledge (briefings, recipes, schemas) is assembled and injected ONLY during bootstrap generate step (guidance.go:assembleKnowledge). Direct tool operations (deploy, manage, mount) provide zero platform context. | guidance.go knowledge assembly, tools/manage.go:47-107, tools/deploy.go | Primary (F5) | VERIFIED |
| F6 | **IsExisting flag manual, not auto-detected**: `RuntimeTarget.IsExisting` (validate.go:39) must be manually supplied by LLM in the bootstrap plan. No code path auto-detects external services or suggests this flag. | validate.go:36-41 | Explorer-state-gaps | VERIFIED |
| F7 | **Routing instructions don't mention adoption**: The two-path routing guide (instructions.go:47-65) explains "workflow sessions" vs "direct tools" but never mentions that workflows also ADOPT existing services. LLM has no reason to start a workflow for pre-existing services. | instructions.go:47-65 routingInstructions | Adversarial (challenge to R1) | VERIFIED |
| F8 | **Provision checker rejects READY_TO_DEPLOY for existing dev services**: `checkServiceRunning()` (workflow_checks.go:55) always requires RUNNING status for dev services regardless of `IsExisting` flag. External services that exist but haven't been deployed (READY_TO_DEPLOY) fail provision. | workflow_checks.go:55-68 | Explorer-state-gaps | VERIFIED |

### Minor

| # | Finding | Evidence | Source | Confidence |
|---|---------|----------|--------|------------|
| F9 | **No visual meta status in service list**: Service list in system prompt shows "- appdev (nodejs@22) — RUNNING" uniformly. No indicator of ZCP-managed vs external. | instructions.go:155-166 | Primary (F7) | VERIFIED |
| F10 | **ActiveAppVersion unmapped**: Zerops API provides `ActiveAppVersion` field on `EsServiceStack` (nil = never deployed) but ZCP's `platform.ServiceStack` doesn't map it. Could serve as deploy-state indicator. | KB agent finding, not in current platform types | KB | LOGICAL |

## Recommendations (max 7, evidence-backed)

| # | Recommendation | Priority | Evidence | Effort |
|---|---------------|----------|----------|--------|
| R1 | **Fix the short-circuit**: Merge orientation + router in instructions.go:208-211. Don't `return` after orientation — continue to router so mixed projects get both per-service guidance AND adoption/workflow offerings. | P0 | F1 — instructions.go:208-211 | ~10 lines |
| R2 | **Add `managedByZCP: bool` to discover response**: Enrich `ServiceInfo` in ops/discover.go by checking ServiceMeta existence. LLM immediately knows which services lack ZCP state. | P0 | F2 — ops/discover.go:28-40 | ~15 lines (ops + tools layers) |
| R3 | **Expand routing instructions for adoption**: Add to routingInstructions (instructions.go:47-65) that workflows also adopt existing services. Mention "services without ZCP state should go through bootstrap with isExisting=true". | P1 | F7, F3 — instructions.go:47-65 | ~5 lines of text |
| R4 | **Enrich adoption guidance in bootstrap.md**: Expand option (c) "work with existing" with concrete steps: discover → identify external services → set IsExisting=true in plan → skip import for existing → preserve code. | P1 | F3 — bootstrap.md discover section | ~20 lines of guidance |
| R5 | **Label services in system prompt**: In service listing (instructions.go:155-166 and instructions_orientation.go), annotate each service as "(ZCP-managed)" or "(external — needs adoption)". | P2 | F9 — instructions.go:155-166 | ~10 lines |
| R6 | **Auto-suggest IsExisting**: When router detects services without ServiceMeta, include explicit guidance in the offering hint listing which services need `isExisting=true`. | P2 | F6 — validate.go:39, router.go:129-142 | ~15 lines |
| R7 | **Fix provision checker for existing READY_TO_DEPLOY**: Allow `checkServiceRunning` to accept READY_TO_DEPLOY when `IsExisting=true` for dev services. | P2 | F8 — workflow_checks.go:55-68 | ~5 lines |

## Evidence Map

| Finding | Confidence | Basis |
|---------|-----------|-------|
| F1 | VERIFIED | instructions.go:208-211 read by orchestrator + adversarial |
| F2 | VERIFIED | ops/discover.go:28-40 struct verified by explorer + primary |
| F3 | VERIFIED | bootstrap.md + router.go:129-142 verified by explorer |
| F4 | VERIFIED | managed_types.go:36-62 verified by primary + explorer |
| F5 | VERIFIED | guidance.go + tools/ verified by primary |
| F6 | VERIFIED | validate.go:36-41 verified by explorer |
| F7 | VERIFIED | instructions.go:47-65 verified by adversarial |
| F8 | VERIFIED | workflow_checks.go:55-68 verified by explorer |
| F9 | VERIFIED | instructions.go:155-166 verified by primary |
| F10 | LOGICAL | KB agent — API field exists but not mapped in ZCP |

## Adversarial Challenges

### Challenged Recommendations

**[CH1] Re: R2 (isBootstrapped in discover)**
Challenge: Adding a JSON field is INCOMPLETE. Even with `managedByZCP: false`, the system prompt never tells the LLM to check this field or what to do about it.
**Resolution**: Valid challenge. R2 is necessary but insufficient alone. R3 (routing instruction text) addresses the gap. Both needed together.

**[CH2] Re: R5-equivalent (knowledge in direct tools)**
Challenge: Injecting knowledge into direct tool responses violates ZCP DNA ("MCP = dumb"). The real fix is routing clarity in the system prompt so LLM chooses workflows over direct tools when appropriate.
**Resolution**: Accepted. Removed from recommendations. R1 (fix short-circuit) + R3 (routing text) solve this upstream. If LLM is properly routed to workflows, knowledge injection happens naturally via guidance assembly.

### Critical Missed Finding (from adversarial)

**[MF1] — Became F1**: The short-circuit at instructions.go:208-211 was the adversarial's key contribution. Primary analyst identified F2 (adoption hint conditional) but did NOT trace WHERE those hints appear in the system prompt or discover the early return that suppresses them. This is the root cause: orientation and routing are treated as mutually exclusive when they should be complementary.

## Architecture Insight

The instruction delivery has three layers that should be independent but are accidentally exclusive:

```
Layer 1: Base routing (workflow vs direct) — ALWAYS shown
Layer 2: Project orientation (per-service detail) — CONDITIONAL on ServiceMeta
Layer 3: Strategic routing (workflow offerings) — SUPPRESSED when Layer 2 exists
```

The fix is simple: Layer 2 and Layer 3 should both be shown when applicable. Remove the early return at line 211, let the code flow through to the router. Then enrich the router to account for mixed projects (some metas, some external).

## Implementation Sequence

```
Phase 1 — Critical (P0): Fix instruction architecture
  1a. Remove short-circuit at instructions.go:208-211 (~10 lines)
  1b. Add managedByZCP to ServiceInfo in ops/discover.go (~15 lines)
  Tests: instructions_test.go, discover_test.go

Phase 2 — Major (P1): Improve guidance content
  2a. Expand routing instructions text (instructions.go:47-65, ~5 lines)
  2b. Expand bootstrap.md adoption guidance (~20 lines)
  Tests: instruction content assertions, guidance assembly tests

Phase 3 — Polish (P2): Visual clarity + auto-detection
  3a. Label services in system prompt (~10 lines)
  3b. Auto-suggest IsExisting in router hints (~15 lines)
  3c. Fix provision checker for READY_TO_DEPLOY (~5 lines)
  Tests: router_test.go, workflow_checks_test.go
```

Total estimated change: ~80 lines of Go + ~20 lines of markdown guidance.
