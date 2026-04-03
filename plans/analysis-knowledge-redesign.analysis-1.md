# Knowledge System Redesign — Final Analysis

**Date**: 2026-04-02
**Scope**: `internal/knowledge/`, `internal/workflow/`, `internal/tools/`, `internal/content/workflows/`
**Agents**: 7 (KB specialist, Primary Architect, Adversarial Challenger, Correctness Reviewer, Completeness Reviewer + orchestrator)
**Verification**: All 11 findings VERIFIED against code. Correctness review: SOUND. Completeness review: 3 critical gaps found and resolved.

---

## Problem Statement

The knowledge system has three structural problems:

1. **Content misplacement** — `core.md` (464L) mixes universal platform rules with flow-specific bootstrap patterns and runtime-specific framework rules. `operations.md` (273L) mixes general ops with bootstrap workflow content.

2. **Lost universal concepts** — Old recipes (v6.35.0) had Stack Composition patterns (Layer 0-4), cross-service wiring tables, framework secret management. These universal patterns disappeared when recipes were restructured to hello-world format.

3. **Broken knowledge routing** — 25 files (guides/ + decisions/, 2420L) are unreachable from structured delivery. `getRelevantDecisions()` reads from operations.md instead of authoritative `decisions/` files. Provision step gets only import.yml schema, missing examples, universals, resource hints, and decision context. Generate step dumps 130 unfiltered lines of Rules & Pitfalls for all runtimes.

---

## Architecture: How Knowledge Flows

### Delivery Mechanisms (4 modes, unchanged API)

| Mode | Returns | When Used |
|------|---------|-----------|
| `scope=infrastructure` | universals.md + core.md + live stacks | Before generating YAML (full reference) |
| `briefing(runtime, services)` | Runtime guide + recipe hints + service cards + wiring + decision hints | Stack-specific context |
| `recipe=name` | universals.md + recipe content | Framework-specific knowledge |
| `query=text` | Search results | Ad-hoc lookup |

### Workflow Guidance Injection (step-aware, `guidance.go:27-124`)

The workflow engine already does step-aware knowledge injection. This architecture is CORRECT and unchanged. We fix WHAT gets injected, not HOW.

**Current vs proposed per-step injection:**

| Step | Current Inject | Proposed Inject | Change |
|------|---------------|-----------------|--------|
| **discover** | NONE | NONE | — |
| **provision** | import.yml Schema | import.yml Schema + **Multi-Service Examples** + **Universals** + **Resource Hints** + **Decision Hints** | +4 layers |
| **generate** | Runtime briefing + Dep briefing + Env vars + zerops.yml Schema + Rules & Pitfalls (130L unfiltered) | Runtime briefing **(with ## Gotchas)** + Dep briefing + Env vars + zerops.yml Schema + Rules & Pitfalls **(filtered, ~80L)** | Enriched + filtered |
| **deploy** | Schema Rules | Schema Rules (troubleshooting added to static guidance) | Static change only |
| **close** | NONE | NONE (adoption: knowledge hint in transition message) | Adoption only |

### Dimensions Affecting Delivery

| Dimension | Changes | Does NOT Change |
|-----------|---------|-----------------|
| **Workflow** (bootstrap/deploy/recipe/cicd) | Static guidance template, inject points | Core facts, runtime guides |
| **Step** (discover→close) | Which layers injected | Content of each layer |
| **Mode** (standard/dev/simple) | Static guidance sections | Knowledge inject (identical across modes) |
| **Environment** (container/local) | Static guidance addenda | Knowledge inject (identical) |
| **Runtime** (bun/go/php/java...) | Which recipe loaded, resource hints | Platform universals, schema |
| **Dependencies** (postgresql/valkey...) | Service cards, wiring, decision hints | Core rules |
| **isExisting** (adoption) | Deploy checker behavior, transition message | Static guidance text (LLM-driven skip) |

---

## Content Changes

### core.md: 464→~410 lines (remove ~50L)

| Lines | Content | Action |
|-------|---------|--------|
| 1-276 | Platform model, schemas, universal rules | KEEP |
| **277-290** | Import Generation (dev/stage naming, startWithoutCode, mode routing) | **REMOVE** → already in bootstrap.md provision section |
| **292-299** | Runtime-Specific (7 framework rules: Java, PHP, Django, Phoenix, Rust, Go, PHP ext) | **REMOVE** → distribute to 7 hello-world recipes `## Gotchas` |
| 300-357 | Schema Rules, Cache, Public Access, zsc | KEEP |
| **360-375** | Causal Chains (11 rows) | **SPLIT** → keep 6 universal rows, move 5 runtime-specific to recipes |
| 378-464 | Multi-Service Examples (4 complete import.yml) | KEEP (newly injected at provision step) |

### operations.md: 273→~148 lines (remove ~125L)

| Lines | Content | Action |
|-------|---------|--------|
| 3-145 | Networking, CI/CD, Logging, SMTP, CDN, Scaling, RBAC, S3, Production Checklist | KEEP |
| **147-185** | Service Selection Decisions | **REMOVE** → decisions/ files are authoritative |
| **187-210** | Tool Access Patterns | **MOVE** → bootstrap.md |
| **212-227** | Dev vs Stage | **MOVE** → bootstrap.md |
| **229-251** | Verification & Attestation | **MOVE** → bootstrap.md |
| **253-273** | Troubleshooting | **MOVE** → bootstrap.md deploy section + deploy.md investigate section |

### universals.md: 55→~75 lines (add ~20L)

Keep concise — prepended to every recipe response. Add CONCEPTS only, no YAML examples:

```markdown
## Stack Composition
Build incrementally: start with runtime, add managed services as needed.
Layers: runtime → +database → +cache → +storage → +search/queue.
Each layer adds a service to import.yml with priority:10 and wiring via ${hostname_varName}.
Use zerops_knowledge scope="infrastructure" for complete import.yml examples.

## Immutable Decisions
These CANNOT be changed after creation — choose correctly or delete+recreate:
hostname, mode (HA/NON_HA), object storage bucket name, service type category.

## Container Lifecycle
Survives: restart, reload, stop/start, vertical scaling (same container).
Lost on: deploy (new container), scale-up (new container fresh), scale-down (removed container).
All persistent data MUST use: database, object storage, or shared storage.

## Project-Level Secrets
Secrets shared across dev+stage (APP_KEY, SECRET_KEY_BASE) MUST be project-level env vars.
Per-service envSecrets generate different values per service — breaks cross-environment sessions.
```

Import.yml YAML examples stay in core.md `## Multi-Service Examples` — injected at provision step via `getCoreSection()`, NOT prepended to every recipe.

### recipes/*.md: Add `## Gotchas` to 7 hello-world recipes

Each gets its runtime's platform rules (moved from core.md:292-299) + runtime-specific Causal Chains:

| Recipe | Gotchas to add |
|--------|---------------|
| `java-hello-world.md` | `server.address=0.0.0.0` mandatory; fat JAR required; Maven wrapper; thin JAR → ClassNotFoundException |
| `php-hello-world.md` | `TRUSTED_PROXIES`; `--ignore-platform-reqs` on Alpine; `php84-<ext>` version prefix; php-nginx is run base only |
| `python-hello-world.md` | `CSRF_TRUSTED_ORIGINS`; `SECURE_PROXY_SSL_HEADER`; `addToRunPrepare` pattern; `--no-cache-dir` |
| `ruby-hello-world.md` | `config.hosts`; `--deployment` flag; `RAILS_SERVE_STATIC_FILES` |
| `go-hello-world.md` | Never `run.base: alpine` (glibc/musl); `CGO_ENABLED=0` |
| `gleam-hello-world.md` | `os: ubuntu` required (not Alpine) |
| `deno-hello-world.md` | `os: ubuntu` required (not Alpine) |

Naming: `## Gotchas` (matches `recipe_lint_test.go:127` validation + sync system expectations).

---

## Code Changes

### Phase 3: Fix `getRelevantDecisions()` (TDD)

Refactor `sections.go:255-301` to read from `decisions/` files instead of `operations.md` H3 subsections.

Current: `s.Get("zerops://themes/operations")` → parse H2 "Service Selection Decisions" → H3 subsections
New: `s.Get("zerops://decisions/choose-{type}")` → use document content directly

Test impact: 8 tests in `sections_test.go:375-433` must be updated to expect content from decisions/ files.

### Phase 5: Workflow integration (TDD)

**WK1: Enrich provision step** (`guidance.go:78-81`):

```go
if params.Step == StepProvision {
    // Existing: import.yml Schema
    getCoreSection(kp, "import.yml Schema")
    // NEW: Multi-Service Examples (complete import.yml patterns)
    getCoreSection(kp, "Multi-Service Examples")
    // NEW: Platform Universals (Stack Composition concept, ~20 lines)
    kp.GetUniversals()
    // NEW: Runtime Resource Hints (devMinRAM, stageMinRAM)
    knowledge.GetRuntimeResources(runtimeBase)
    // NEW: Decision Hints (from decisions/ files, depends on Phase 3)
    getDecisionHintsForDeps(kp, params.DependencyTypes)
}
```

**WK2: Rules & Pitfalls filtering** — automatic. Phase 1 removes Import Generation + Runtime-Specific from core.md. No code change needed.

**WK3: Troubleshooting** — content move to bootstrap.md deploy section AND deploy.md investigate section. Static guidance change, no code change.

**WK4: Recipe research decisions** — add `RecipeStepResearch` case to `assembleRecipeKnowledge()` switch at `recipe_guidance.go:98`.

### Adoption Flow Changes

**checkDeploy** (`workflow_checks.go:142`): For adopted targets (`IsExisting=true`), verify only `GET / → 200`. Skip full VerifyAll (HTTP /health, /status, logs, startup). Fresh targets keep full verification.

```go
// In checkDeploy, before VerifyAll loop:
adoptedHostnames := buildAdoptedSet(plan)
// For adopted: simple HTTP 200 on /
// For fresh: full VerifyAll
```

**checkGenerate** (`workflow_checks_generate.go:38`): Already skips adopted targets. No change needed.

**BuildTransitionMessage** (`bootstrap_guide_assembly.go:63`): For adopted services, include knowledge hint:
```
"zmon (go@1, adopted) — For runtime guidance: zerops_knowledge runtime=go services=[postgresql@16]"
```

---

## Complete Knowledge Flow

### Fresh Bootstrap

```
DISCOVER:  Static guidance only. Agent plans stack.
PROVISION: Schema + Examples + Universals + Resource Hints + Decisions → agent builds import.yml
GENERATE:  Runtime briefing (with Gotchas) + Service cards + Env vars + Schema + Filtered Rules
           Agent optionally calls: zerops_knowledge recipe="laravel" (framework-specific)
DEPLOY:    Schema Rules. Static: troubleshooting table. Deploy → verify → iterate.
CLOSE:     Strategy selection → deploy/cicd/manual workflow.
```

### Adoption

```
DISCOVER:  Agent sees existing services, proposes adoption with isExisting=true.
PROVISION: Same inject (agent uses env var discovery only, skips import).
GENERATE:  checkGenerate SKIPS adopted targets. Agent attestates "existing service".
DEPLOY:    Adopted: GET / → 200 only. Fresh (if mixed plan): full VerifyAll.
CLOSE:     Transition message + knowledge hints for adopted services.
```

### Adoption + New Dependency (e.g., add Valkey to existing Go API)

```
DISCOVER:  Adopted runtime + CREATE cache dependency.
PROVISION: Import ONLY cache. Discover env vars for all services.
GENERATE:  Skip adopted runtime. Static guidance (NEW): "For adopted services with
           new dependencies: SSH → edit zerops.yml → add wiring → redeploy."
DEPLOY:    Adopted: GET / → 200. After zerops.yml modification: redeploy to activate.
CLOSE:     Transition with hints.
```

---

## Implementation Sequence

```
Phase 1a: Safe content moves (no test breakage, no code changes)
  1. Distribute runtime rules (core.md:292-299) → 7 recipes ## Gotchas
  2. Split Causal Chains (core.md:360-375) → keep universal, move runtime to recipes
  3. Move flow sections (operations.md:187-273) → bootstrap.md + deploy.md
  4. Move troubleshooting (operations.md:253-273) → bootstrap.md deploy + deploy.md investigate

Phase 2: Expand universals.md (+20 lines)
  5. Add Stack Composition concept (3-4 lines, no YAML)
  6. Add Immutable Decisions callout (2 lines)
  7. Add Container Lifecycle Facts (3 lines)
  8. Add Project-Level Secrets pattern (3 lines)

Phase 3: Fix decisions routing (TDD — tests FIRST)
  9.  RED: Failing test for getRelevantDecisions() reading from decisions/
  10. GREEN: Refactor getRelevantDecisions() in sections.go
  11. Remove operations.md Service Selection H2 (147-185) — now safe
  12. Update sections_test.go (8 affected tests)

Phase 1b: Content moves depending on Phase 3
  13. Remove Import Generation from core.md (277-290) — bootstrap.md already has this

Phase 5: Workflow integration (TDD)
  14. RED: Test for Multi-Service Examples + universals in provision guidance
  15. GREEN: Add getCoreSection(kp, "Multi-Service Examples") + GetUniversals() at provision
  16. RED: Test for resource hints in provision guidance
  17. GREEN: Add formatResourceHint() call at provision
  18. RED: Test for decisions in provision guidance
  19. GREEN: Add getDecisionHintsForDeps() call at provision (uses Phase 3)
  20. RED: Test for recipe research decisions
  21. GREEN: Add research case to recipe_guidance.go switch
  22. Adoption: checkDeploy soft-verify (GET / → 200) for isExisting targets
  23. Adoption: BuildTransitionMessage knowledge hints for adopted services

Phase 4: Validation
  24. go test ./internal/knowledge/... -v
  25. go test ./internal/workflow/... -v
  26. go test ./internal/tools/... -v
  27. Manual: verify each knowledge mode output completeness
```

---

## Decisions Log

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | Do NOT rename services.md | 17 hardcoded URI references in code + tests + cross-refs |
| D2 | universals.md: concepts only (~20L), no YAML examples | Prepended to every recipe. YAML examples stay in core.md Multi-Service Examples, injected at provision step. |
| D3 | Import.yml examples: keep in core.md, inject at provision via getCoreSection() | Agent needs examples when building import.yml (provision step), not when loading recipes |
| D4 | Runtime rules → recipes ## Gotchas (not separate file) | getRuntimeGuide() loads recipe as runtime guide. Gotchas flow through existing briefing delivery. |
| D5 | Flow content → workflow docs (not new theme files) | content/workflows/ already exists for flow-specific guidance |
| D6 | Knowledge inject is step-aware, NOT mode-aware | Mode = procedure (static guidance). Knowledge = facts (inject). Correct separation. |
| D7 | Adoption deploy: GET / → 200 only | User's existing app may not have /health, /status endpoints. Trust that it works. |
| D8 | No import.yml in recipes, no multi-service wiring in recipes | universals.md teaches Stack Composition concept; services.md has per-service wiring templates; core.md has complete examples. Agent composes from these three sources. |
| D9 | Stack Layers concept is UNIVERSAL, not framework-specific | Old laravel/django/symfony all had same Layer 0-4 pattern. Generic version goes in universals. |
| D10 | Old recipe patterns NOT restored per-framework | Framework secret management, full import.yml, framework-specific wiring tables — all derivable from universal knowledge + recipe zerops.yml examples + services.md wiring templates |

---

## Evidence Summary

| Category | Count | Confidence |
|----------|-------|------------|
| VERIFIED (code-checked) | 11 findings | All confirmed by 2+ reviewers |
| Code changes identified | 6 files | guidance.go, sections.go, recipe_guidance.go, workflow_checks.go, bootstrap_guide_assembly.go, bootstrap.md |
| Content changes identified | 10 files | core.md, operations.md, universals.md, 7 recipes |
| Tests affected | ~12 tests | sections_test.go (8), bootstrap_guidance_test.go (~2), recipe_guidance_test.go (~2) |
| Regression risks | 4 identified, all mitigated | Phase ordering prevents breakage |
