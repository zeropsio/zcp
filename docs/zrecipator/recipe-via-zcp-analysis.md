# Recipe Creation via ZCP — Complete Analysis

Snapshot of full codebase research for building a system that creates Zerops recipes through ZCP.
Target: recipe taxonomy types 3 (Backend Framework Minimal) and 4 (Backend Framework Showcase).

---

## Table of Contents

1. [Current State — What Exists](#current-state)
2. [The Meta-Prompt — What It Does and Why It's Bloated](#the-meta-prompt)
3. [ZCP Systems Inventory](#zcp-systems-inventory)
4. [Overlap Analysis — Meta-Prompt vs ZCP](#overlap-analysis)
5. [Recipe Deliverables — What We Must Produce](#recipe-deliverables)
6. [Architecture Proposal — Recipe Workflow](#architecture-proposal)
7. [Step-by-Step Workflow Design](#step-by-step-workflow-design)
8. [Eval Integration](#eval-integration)
9. [Implementation Phases](#implementation-phases)
10. [Key Files Reference](#key-files-reference)

---

## Current State

### What exists today

**1. The meta-prompt** (`docs/zrecipator/zrecipator-be-framework-meta.md`)

A ~2200-line monolith designed for a two-layer agent system:
- Layer 1: Meta-agent (Opus) reads meta-prompt, produces per-framework prompts (`zrecipator-{framework}-minimal.md` + `zrecipator-{framework}-showcase.md`)
- Layer 2: Executing agent reads the framework prompt, SSHes into a Zerops project, builds the app, deploys, creates all recipe files

The meta-prompt has 5 architectural gates: Research, Bootstrap Imports, Minimal Prompt, Showcase Prompt, Verification.

No per-framework prompts have been generated yet for type 3/4. The three "done" prompts for types 1, 2a, 2b are not stored in `docs/zrecipator/`.

**2. The eval system** (`internal/eval/`)

- `runner.go` — Orchestrates a single recipe evaluation end-to-end
- `prompt.go` — Prompt construction: `ParseRecipeMetadata`, `GenerateHostnames`, `BuildTaskPrompt`, `BuildFullPrompt`, `assessmentInstructions`
- `suite.go` — Runs multiple recipe evaluations sequentially
- `extract.go` — Log parsing: `ExtractAssessment`, `ExtractToolCalls` from Claude stream-json JSONL output
- `cleanup.go` — Post-eval cleanup: `CleanupProject`, `CleanupEvalServices`, `cleanClaudeMemory`
- `instruction_eval.go` + `instruction_variants.go` — A/B testing of MCP instruction text variants

Data flow: Loads recipe from knowledge store → parses metadata (runtime type, services from YAML blocks) → generates random hostnames → builds prompt (task + assessment instructions) → spawns Claude CLI headless (`claude -p {prompt} --output-format stream-json --verbose --no-session-persistence --dangerously-skip-permissions`) → extracts assessment ("## EVAL REPORT") → extracts tool calls → writes results.

Currently evaluates *existing* recipes (deploy from knowledge store), not recipe *creation*.

**3. The knowledge system** (`internal/knowledge/`)

- `documents.go` — `go:embed themes/*.md bases/*.md all:recipes all:guides all:decisions`
- `engine.go` — `Store` with `Provider` interface: `List()`, `Get()`, `Search()`, `GetCore()`, `GetUniversals()`, `GetBriefing()`, `GetRecipe()`
- `briefing.go` — 7-layer stack-specific knowledge assembly: live stacks, runtime guide, recipe hints, service cards, wiring syntax, decision hints, version check
- `recipe_lint_test.go` — Validates ALL embedded recipes against structural/content rules

33 embedded recipe `.md` files (gitignored, pulled via `zcp sync pull`). Categories:
- 11 Runtime Hello Worlds (type 1): bun, nodejs, go, python, ruby, php, rust, deno, dotnet, gleam, java
- 12 Frontend Static (type 2a): angular, react, vue, svelte, sveltekit, solidstart, qwik-city, nuxt, nextjs, astro, analog, react-router
- 9 Frontend SSR (type 2b): angular, sveltekit, solidstart, qwik-city, nuxt, nextjs, astro, analog, react-router
- 1 Showcase: zerops-showcase (Bun+Python)

**4. The sync system** (`internal/sync/`)

Bidirectional:
- `pull_recipes.go` — Fetches from Zerops API (`https://api.zerops.io/api/recipes`), builds markdown from `sourceData`, writes to `internal/knowledge/recipes/{slug}.md`
- `push_recipes.go` — Reads local `.md`, extracts fragments (KnowledgeBase, IntegrationGuide, Intro, ZeropsYAML), injects between `ZEROPS_EXTRACT` markers in GitHub app repo README, creates PR
- `pull_guides.go` — Fetches `.mdx` from `zeropsio/docs` via GitHub API, converts to MD
- `push_guides.go` — Converts local `.md` back to `.mdx`, creates PR via Git Trees API
- `cache.go` — `CacheClear` POSTs to Strapi API to invalidate recipe cache
- `transform.go` — Fragment extraction/injection, MDX<->MD conversion
- `github.go` — `GH` struct wrapping `gh` CLI calls

Config: `.sync.yaml` — API URL, slug remapping, category exclusions, push targets (org, repo patterns), output path.

**5. ZCP's bootstrap workflow** (`internal/workflow/`)

Already does ~80% of what recipe creation needs:

5 sequential steps: discover → provision → generate → deploy → close

- `engine.go` — Engine with bootstrap lifecycle methods
- `bootstrap.go` — BootstrapState, steps, BuildResponse
- `bootstrap_steps.go` — 5 step definitions with tools and verification gates
- `guidance.go` — assembleGuidance with knowledge injection layers
- `bootstrap_guide_assembly.go` — buildGuide, formatEnvVarsForGuide
- `bootstrap_checks.go` — StepChecker/DeployStepChecker interfaces
- `bootstrap_outputs.go` — writeBootstrapOutputs, writeProvisionMetas
- `validate.go` — Plan validation (hostnames, types, resolution)
- `router.go` — Route() returns FlowOfferings based on project state
- `service_meta.go` — ServiceMeta CRUD, strategy constants
- `session.go` — Session CRUD, atomic init, iteration (max 10)
- `state.go` — WorkflowState struct, immediate workflow map
- `registry.go` — Registry with file locking

Step categories:
- **fixed** (discover, provision, close) — deterministic tool call sequences
- **creative** (generate) — LLM generates code with judgment
- **branching** (deploy) — per-service iteration with retry loops

Bootstrap modes: standard (dev+stage pairs), dev (single dev), simple (single with real start).

Hard checks before step completion:
- Provision: all services exist with expected status, types match, managed dep env vars discovered
- Generate: zerops.yml valid, setup entries exist, env var refs valid, ports present
- Deploy: all runtimes RUNNING, subdomain access enabled, health checks pass

Knowledge injection per step:
- Provision: import.yml schema from `themes/core`
- Generate: runtime briefing, dependency briefing, discovered env vars, zerops.yml schema + rules
- Deploy: schema rules from core

---

## The Meta-Prompt

### What it contains (~2200 lines)

| Section | Lines (approx) | Purpose |
|---------|----------------|---------|
| Research Phase (framework identity, build pipeline, database, env, dev, showcase libs) | 200 | Tables the meta-agent fills per framework |
| Bootstrap Import Files (minimal + showcase workspace YAML) | 80 | Workspace imports (NOT recipe deliverables) |
| Research → Output Mapping | 30 | Ensures every researched value appears in output |
| Architectural Decision Trees (4 trees) | 80 | Web server, build base, OS, dev tooling |
| Output Prompt Structure (minimal 21 sections, showcase delta) | 100 | Section ordering and authoring guide |
| Shared Zerops Platform Foundation | 500 | Terminology, pipeline, platform model, env vars, zerops.yaml intelligence, deployFiles, cache strategy, OS, comment system, service priority, ports, health checks, process managers, resources, initCommands |
| The Six Environments | 200 | Env 0-5 comparison matrix, base template, deltas |
| README Templates | 200 | Fragment system, main/env/app README templates, intro rules |
| Import.yaml Conventions | 150 | Top-level comments, service comment patterns, primitives coverage, naming |
| Operational Workflow (steps 0-10) | 200 | File system setup, git init, deploy dev, test, deploy prod, verify, create recipe structure, create files, final verify |
| Tier 1: Minimal specific rules | 150 | Naming, services per env, health dashboard, migration, env vars, zerops.yaml structure, dev setup, verification gates |
| Tier 2: Showcase specific rules | 200 | Service stack, services per env, storage sizing, showcase dashboard, queue worker, zerops.yaml structure, verification gates |
| Reference: Laravel on Zerops | 70 | Concrete example patterns |
| Framework Research Checklist | 50 | Must-know blocking items |
| Writing the Output Prompts | 50 | Process, content rules, what not to include |

### Why it's bloated

The meta-prompt must be **self-contained** because the executing agent has no infrastructure. Everything the agent might need is inlined. This means:

1. **~500 lines of platform knowledge** that ZCP already has in `themes/core.md`, `themes/services.md`, briefings, and live schemas
2. **~200 lines of decision tree logic** that could be programmatic code in ZCP
3. **~200 lines of template boilerplate** (env READMEs, import.yaml patterns) that could be generated
4. **~150 lines of verification gates** that could be hard checkers
5. **~200 lines of operational workflow** (SSH, git init, zcli push, polling) that ZCP's bootstrap workflow already handles

---

## ZCP Systems Inventory

### 15 MCP Tools

| # | Tool | Read-Only | Workflow-Gated | Key for recipes |
|---|------|-----------|----------------|-----------------|
| 1 | `zerops_workflow` | No | No | Orchestration hub |
| 2 | `zerops_discover` | Yes | No | Service/env var discovery |
| 3 | `zerops_knowledge` | Yes | No | 4 modes: briefing, scope, query (BM25), recipe |
| 4 | `zerops_logs` | Yes | No | Debug failures |
| 5 | `zerops_events` | Yes | No | Activity timeline |
| 6 | `zerops_process` | Yes | No | Check async processes |
| 7 | `zerops_verify` | Yes | No | Health checks |
| 8 | `zerops_deploy` | No | No | SSH-based deploy |
| 9 | `zerops_manage` | No | No | Start/stop/restart |
| 10 | `zerops_scale` | No | No | CPU/RAM/disk |
| 11 | `zerops_env` | No | No | Set/delete env vars |
| 12 | `zerops_import` | No | **Yes** | Create services from YAML |
| 13 | `zerops_delete` | No | No | Delete services |
| 14 | `zerops_subdomain` | No | No | Enable/disable subdomain |
| 15 | `zerops_mount` | No | Partial | SSHFS mount/unmount |

### Knowledge Store Contents

| Directory | Count | Content |
|-----------|-------|---------|
| `themes/` | 4 | `core.md` (YAML schemas, rules), `operations.md`, `services.md` (service cards, wiring), `universals.md` |
| `bases/` | 5 | `alpine.md`, `docker.md`, `nginx.md`, `static.md`, `ubuntu.md` |
| `recipes/` | ~35 | Per-framework recipe guides |
| `guides/` | ~25 | Operational guides (backup, cdn, logging, networking, etc.) |
| `decisions/` | ~6 | Decision trees (choose-cache, choose-database, choose-queue, etc.) |

### Guidance Injection Layers

1. **System prompt** (MCP Instructions): base instructions, workflow hints, environment context, project summary + router offerings
2. **Step guidance** (per-step): static content from `workflows/bootstrap.md` via `<section>` tags, environment-aware, mode-specific, failure-aware
3. **Knowledge injection** (appended to step guidance): import.yml schema, runtime briefing, env vars, zerops.yml schema
4. **Iteration escalation**: `BuildIterationDelta` — diagnose → checklist → ask user
5. **On-demand knowledge** (LLM-initiated): `zerops_knowledge` with scope, briefing, query, recipe modes

### Session State Model

```go
type WorkflowState struct {
    Version, SessionID, ProjectID, Workflow, Intent string
    PID, Iteration int
    Bootstrap *BootstrapState  // nil unless bootstrap workflow
    Deploy    *DeployState     // nil unless deploy workflow
}
```

Persisted at `.zcp/state/sessions/{id}.json`. Survives process restarts. Max 10 iterations.

---

## Overlap Analysis

| Meta-prompt section | ZCP equivalent | Overlap % |
|---|---|---|
| Zerops Platform Terminology | `themes/core.md` | 100% |
| Build & Deploy Pipeline | `themes/core.md` + bootstrap guidance | 100% |
| Environment Variables — Three Categories | `themes/core.md` "Env Variables" section | 100% |
| Referencing Pattern | `themes/services.md` wiring syntax | 100% |
| zerops.yaml Intelligence Principle | `themes/core.md` + zerops.yml JSON schema | 90% |
| deployFiles and start Command Validation | `themes/core.md` "Rules & Pitfalls" | 90% |
| Build Cache Strategy | Runtime briefings + `themes/core.md` | 80% |
| .gitignore Alignment | Not in ZCP (new content) | 0% |
| Runtime Base Image OS | Runtime briefings, `bases/*.md` | 80% |
| Comment System (85% standard) | Not in ZCP (new content, recipe-specific) | 0% |
| Service Priority | `themes/core.md` import schema | 90% |
| Port Configuration | Runtime briefings | 100% |
| healthCheck vs readinessCheck | `themes/core.md` zerops.yml schema | 100% |
| Process Managers | Runtime briefings per language | 100% |
| Resource Configuration | `runtime_resources.go` + briefings | 90% |
| initCommands Intelligence | `themes/core.md` + briefings | 80% |
| The Six Environments | Not in ZCP (new content, recipe-specific) | 0% |
| README Templates / Fragment System | Not in ZCP, but `sync/transform.go` knows fragments | 20% |
| Import.yaml Conventions | Partially in `themes/core.md` schema | 40% |
| Operational Workflow (steps 0-10) | Bootstrap workflow handles 80% of this | 80% |
| Decision Trees (4 trees) | Not in ZCP as code (new logic) | 0% |
| Verification Gates | Bootstrap hard checkers cover ~60% | 60% |

**Summary**: ~60% of the meta-prompt content already exists in ZCP. ~25% is recipe-specific new content (comment system, environments, README templates, decision trees). ~15% is operational workflow that bootstrap already handles.

---

## Recipe Deliverables

What a complete recipe produces (from `docs/zrecipator/how-to-write-recipe.md` and `docs/recipes/create-recipe.md`):

### App Repository (in `zerops-recipe-apps/{slug}-app`)

1. **Application source code** — framework project with health dashboard, migrations, models
2. **`zerops.yaml`** — Fully commented, with `base` + `prod` + `dev` setups (+ `worker` for showcase)
3. **`README.md`** — With `intro` and `integration-guide` fragments
4. **`.gitignore`** — Framework-appropriate

### Recipe Repository (in `zeropsio/recipes/{slug}/`)

5. **Main `README.md`** — Recipe description with `intro` fragment, deploy button, environment list
6. **6 environment folders** (`0 — AI Agent` through `5 — Highly-available Production`), each containing:
   - `import.yaml` — Self-contained, fully commented, environment-specific
   - `README.md` — With `intro` fragment (fixed text per tier + optional services line)

### External

7. **Strapi CMS entry** — Name, slug, icon, categories, language/framework tags

### Total: 1 app repo (4 files) + 1 recipe folder (13 files) + 1 CMS entry

### Fragment Content Quality Spec

The app README is the highest-value deliverable — it's what the Zerops GUI shows users as integration documentation and what ZCP's knowledge store ingests for future LLM guidance. Quality matters more here than anywhere else.

#### `intro` fragment

Short (1-3 lines). Names the framework (linked), the infrastructure it connects to, and its purpose on Zerops.

**Simple recipe** (bun-hello-world):
```
A minimal [Bun](https://bun.sh) application with a [PostgreSQL](https://www.postgresql.org/) connection,
demonstrating idempotent database migrations and a health check endpoint at `/`.
Used within [Bun Hello World recipe](https://app.zerops.io/recipes/bun-hello-world) for [Zerops](https://zerops.io) platform.
```

**Complex recipe** (laravel-jetstream):
```
[Laravel Jetstream](https://jetstream.laravel.com/introduction.html) is an advanced starter kit by Laravel.
This showcases how to best integrate Jetstream apps with Zerops to setup the deploy pipeline,
configure for high availability and handle migrations and upgrades.
```

Rules: unique per recipe (never reuse phrasing), mention framework + services + scope, no HTTP codes/ports/paths.

#### `integration-guide` fragment

This is the **primary user-facing documentation**. Structure varies by recipe complexity:

**Simple recipe** (bun-hello-world) — one section:
```
### 1. Adding `zerops.yaml`
{one-line explanation}

{complete commented zerops.yaml with ALL setups}
```

The zerops.yaml IS the integration guide. Every comment explains *why*, not *what*:
- `# --frozen-lockfile: fail if bun.lock would change — reproducible builds` (why this flag)
- `# Only bundled artifacts — no node_modules, no source. 156 KB total.` (what this achieves)
- `# zsc execOnce runs migration exactly once per version across all containers` (why this wrapper)
- `# Zerops starts nothing — developer drives via SSH` (why noop)

**Complex recipe** (laravel-jetstream) — multiple sections:
```
### 1. Add `zerops.yaml`
{explanation of what this config achieves}
{complete commented zerops.yaml — even larger, base+prod+dev}

### 2. Add Support For Object Storage
{specific package to install, specific config file to modify, with GitHub links to exact lines}

### 3. Utilize Environment Variables
{how env vars and secrets wire services together, links to relevant files}

### 4. Setup Production Mailer
{what to change for real production SMTP}
```

Quality patterns observed:
- **Comments explain decisions, not syntax**: `# Since PHP is spawned as a worker process by Nginx FastCGI, we have to tell the PHP application to log to the syslog directly` — not `# Set LOG_CHANNEL to syslog`
- **Links to specific source lines**: `[config/jetstream.php](https://github.com/zerops-recipe-apps/laravel-jetstream-app/blob/main/config/jetstream.php#L79)` — deep links to exact code
- **Links to Zerops docs**: `https://docs.zerops.io/features/build-cache`, `https://docs.zerops.io/zerops-yaml/specification#readinesscheck-`
- **Framework-specific adaptation steps**: Not generic "configure your app" — specific packages, specific config keys, specific commands
- **Sections beyond zerops.yaml only when needed**: Bun needs nothing extra (single zerops.yaml section). Laravel Jetstream needs S3 library install, env var wiring, mailer config.

#### `knowledge-base` fragment

Platform-specific operational knowledge. **Only things an LLM would get wrong without this.**

**Simple recipe** (bun-hello-world) — 2 items:
```
### Base Image
Includes: Bun, `npm`, `yarn`, `git`, `bunx`.
NOT included: `pnpm`.

### Gotchas
- **`BUN_INSTALL: ./.bun` for build caching** — Zerops can only cache paths inside the project tree.
- **Use `bunx` instead of `npx`** — `npx` may not resolve correctly in the Bun runtime.
```

**Complex recipe** (laravel-jetstream) — operational guidance:
```
### Maintenance Mode
{How to safely use `php artisan down` on Zerops — must disable health check first via `zsc health-check disable`}
{CAUTION block with exact command sequence}
{Why: multi-container cache driver, link to Laravel docs}

### Temporary Upscaling when Playing Around
{When and why to use `zsc scale` for ad-hoc tasks}
{Link to zsc docs, link to where it's used in the recipe's zerops.yaml}
```

Quality patterns:
- **Actionable, not theoretical**: exact commands, exact sequence, exact consequence of getting it wrong
- **Cross-references to the recipe's own files**: "as defined in environment variables section of [zerops.yaml](...)"
- **Caution/warning blocks** for dangerous operations (maintenance mode + health check interaction)
- **Zerops-specific tools** (`zsc health-check`, `zsc scale`) that the framework docs don't mention
- **Multi-container awareness**: patterns that only matter when running >1 container (maintenance mode driver, execOnce, session driver)

#### `maintenance-guide` fragment (OSS recipes only)

For `_template_oss` recipes. Covers upgrade procedures, backup strategies, data migration. Only present in OSS environment READMEs.

#### Content quality rules for the generate step

1. **Integration-guide zerops.yaml comments must explain WHY** — every non-obvious line gets a comment that passes the "so what?" test
2. **Additional integration sections only when framework requires specific adaptation** — don't pad with generic "add env vars" sections
3. **Knowledge-base must be platform-specific** — if the LLM already knows it from framework docs, it doesn't belong here
4. **Deep links to source** — link to exact files and line numbers in the app repo, not just "see the config"
5. **Deep links to Zerops docs** — reference specific doc pages for features used (build cache, scaling, zsc, pipeline)
6. **Post-deploy knowledge** — the LLM writes these AFTER deploying, so it knows what actually worked, what broke, what was non-obvious. This is the key advantage over the meta-prompt approach (which asks for documentation before deployment)
7. **Graduated complexity** — simple recipes get minimal knowledge-base (base image + gotchas). Complex recipes get operational guides (maintenance, scaling, multi-container patterns)

#### How ZCP ensures quality

**During generate step** — ZCP's guidance tells the LLM:
- Load the runtime hello-world recipe as a style reference (`zerops_knowledge recipe="{lang}-hello-world"`)
- Load a complex reference recipe for framework-level recipes (`zerops_knowledge recipe="laravel-jetstream"` or similar)
- Platform briefing provides the wiring patterns, env var names, gotchas, and doc URLs to reference
- Schemas provide valid config options (so comments describe real features, not hallucinated ones)

**Hard checkers validate**:
- `integration-guide` fragment exists and contains a `zerops.yaml` code block
- `zerops.yaml` in the code block has at least `prod` and `dev` setups
- `knowledge-base` fragment exists with at least one `### Gotchas` sub-section
- Comments in the zerops.yaml code block have a minimum ratio (>0.3 comment lines per config line)
- No placeholder text (`PLACEHOLDER_*`, `<your-...>`, `TODO`)

**Eval validates** (Path B automation):
- Deploy the recipe from its own import.yaml (not the bootstrap workspace)
- Hit `/api/health` — all service statuses return "OK"
- The recipe's knowledge-base gotchas are verified by actual deployment (if a gotcha says "X fails without Y", the eval confirms Y is configured)

### Minimal (type 3) services per environment

| Env | App Services | Data Services |
|-----|-------------|---------------|
| 0 — AI Agent | appdev (dev) + appstage (prod) | db (PostgreSQL) |
| 1 — Remote (CDE) | appdev (dev) + appstage (prod) | db (PostgreSQL) |
| 2 — Local | app (prod) | db (PostgreSQL) |
| 3 — Stage | app (prod) | db (PostgreSQL) |
| 4 — Small Prod | app (prod) | db (PostgreSQL) |
| 5 — HA Prod | app (prod) | db (PostgreSQL, HA) |

### Showcase (type 4) services per environment

| Env | App Services | Data Services |
|-----|-------------|---------------|
| 0 — AI Agent | appdev + appstage + workerdev + workerstage | db, redis, storage, mailpit, search |
| 1 — Remote (CDE) | appdev + appstage + workerdev + workerstage | db, redis, storage, mailpit, search |
| 2 — Local | app + worker | db, redis, storage, mailpit, search |
| 3 — Stage | app + worker | db, redis, storage, mailpit, search |
| 4 — Small Prod | app + worker | db, redis, storage, search (no mailpit) |
| 5 — HA Prod | app + worker | db (HA), redis (HA), storage (100GB), search (HA) |

---

## Architecture Proposal

### Path A: Recipe Workflow (recommended)

Add a `recipe` workflow type alongside `bootstrap` and `deploy`. The LLM uses ZCP tools natively. ZCP injects step-specific guidance instead of the 2200-line monolith.

```
┌──────────┐   ┌───────────┐   ┌──────────┐   ┌────────┐   ┌──────────┐   ┌───────┐
│ RESEARCH │──▶│ PROVISION │──▶│ GENERATE │──▶│ DEPLOY │──▶│ FINALIZE │──▶│ CLOSE │
│ (new)    │   │ (reuse)   │   │ (reuse+) │   │(reuse) │   │ (new)    │   │(reuse)│
└──────────┘   └───────────┘   └──────────┘   └────────┘   └──────────┘   └───────┘
```

**Why**: The meta-prompt is bloated because the executing agent has no infrastructure. ZCP *is* the infrastructure. A recipe workflow gives the agent everything it needs at each step.

### Path B: Eval-based Recipe Factory (automation layer)

Extend `zcp eval` with `zcp eval create --framework laravel --tier minimal`. This spawns Claude CLI headlessly against a recipe workflow session.

**Why**: Path B is Path A + automation. Once the recipe workflow exists, eval just spawns it headlessly.

### Recommendation: Build Path A first, then Path B on top.

---

## Step-by-Step Workflow Design

### Step 1: RESEARCH (new step)

**Purpose**: Fill in framework-specific decisions before any provisioning.

**Tools**: `zerops_knowledge` (load runtime hello-world recipe, runtime briefing, schemas)

**Guidance injected by ZCP** (~100 lines):
- Framework identity form (service type, build base, package manager, HTTP port, OS preferences)
- Build & deploy pipeline form (build commands, deploy files, start command, cache strategy)
- Database & migration form (driver, migration command, seeding)
- Environment & secrets form (app secret, logging, trusted proxy)
- Showcase libraries form (cache, session, queue, storage, search, mail) — only for showcase tier
- Decision tree resolution instructions (4 trees: web server, build base, OS, dev tooling)

**Completion check**: All required research fields filled. Service types validated against live catalog. Decision tree branches recorded.

**Output**: `RecipePlan` struct stored in session state:
```go
type RecipePlan struct {
    Framework       string            // "laravel", "nestjs", "django"
    Tier            string            // "minimal" or "showcase"
    Slug            string            // "laravel-hello-world" or "laravel-showcase"
    RuntimeType     string            // "php-nginx@8.4"
    BuildBases      []string          // ["php@8.4", "nodejs@22"]
    Decisions       DecisionResults   // Resolved decision tree branches
    Research        ResearchResults   // Filled research tables
    Targets         []RecipeTarget    // Derived from tier + framework
}
```

### Step 2: PROVISION (reuse bootstrap)

**Purpose**: Create workspace project with all services.

**Tools**: `zerops_import`, `zerops_process`, `zerops_discover`, `zerops_mount`

**Guidance**: Same as bootstrap provision, plus recipe-specific import template generation. ZCP generates the bootstrap import YAML from `RecipePlan` (the meta-prompt's "Bootstrap Import Files" section becomes code).

**Completion check**: Same as bootstrap — all services exist, types match, env vars discovered.

### Step 3: GENERATE (reuse bootstrap + recipe additions)

**Purpose**: Write zerops.yml + app code on mounted filesystem.

**Tools**: `zerops_knowledge`, file operations via mount

**Guidance injected by ZCP** (~150 lines):
- Health dashboard spec (endpoint schema, panels, HTTP status codes)
- Migration spec (framework migration system, `zsc execOnce` wrapping, greetings table)
- Env var pattern (`extends: base`, actual discovered env vars from provision)
- zerops.yml structure template (base + prod + dev, with all research values substituted)
- Comment conventions for zerops.yml (lighter than import.yaml, 0.3-0.4 ratio)
- `.gitignore` rules aligned with cache strategy
- For showcase: queue worker setup, additional panels (cache, queue, storage, search, mail)

All platform knowledge (schemas, briefings, wiring syntax) injected via existing `assembleKnowledge()`.

**Completion check**: Same as bootstrap generate — zerops.yml valid, setup entries exist, env var refs valid, ports present.

### Step 4: DEPLOY (reuse bootstrap)

**Purpose**: Deploy dev + stage, verify health.

**Tools**: `zerops_deploy`, `zerops_discover`, `zerops_subdomain`, `zerops_logs`, `zerops_verify`, `zerops_manage`

**Guidance**: Same as bootstrap deploy. Iteration loop with escalating diagnostics.

**Completion check**: Same as bootstrap — all runtimes RUNNING, subdomain access enabled, health checks pass.

### Step 5: FINALIZE (new step)

**Purpose**: Generate all recipe deliverables (6 import.yamls, 7 READMEs).

**Tools**: File operations (write to recipe directory on mount or local filesystem)

**Guidance injected by ZCP** (~150 lines):
- Fragment format spec (`ZEROPS_EXTRACT_START/END` exact syntax)
- README templates (main recipe, environment, app) — with slot filling from RecipePlan
- Import.yaml conventions (top-level comments, service comment patterns, three-tier strategy)
- Comment system rules (85% standard, 80-char line wrap, "so what?" test)
- Minimum Zerops primitive coverage checklist (13 concepts to explain at least once per import.yaml)
- Environment-specific deltas (env 0-5 differences — resource config, service modes, HA settings)
- Project naming convention (`{slug}-{env-suffix}`)

ZCP provides **generated boilerplate** that the LLM customizes:
- Env 3-5 README content is 100% fixed (generic descriptions, no services line)
- Env 0-2 README content is fixed first line + variable services line
- Import.yaml base structure per env is templated (ZCP fills in service types, modes, resources from RecipePlan + decisions)
- The LLM writes comments and adapts per framework

**Completion checks** (new hard checkers):
- All 13 files exist (6 import.yaml + 6 env README + 1 main README)
- App README exists with `integration-guide` fragment containing full zerops.yaml
- Fragment tags use exact format (regex validation)
- `intro` fragments don't contain titles, deploy buttons, or images
- Project names follow `{slug}-{env-suffix}` convention
- All import.yaml files are valid YAML
- All import.yaml files have `priority: 10` on data services
- Env 5 has `corePackage: SERIOUS`, HA modes, dedicated CPU
- Env 4 has `minContainers: 2`
- `envSecrets` present where framework needs app secret
- `# zeropsPreprocessor=on` present when using `<@generateRandomString>`
- `verticalAutoscaling` nesting correct (minRam, minFreeRamGB, cpuMode under it)
- Comment line width <= 80 chars in YAML files
- No cross-environment references in comments

### Step 6: CLOSE (reuse bootstrap)

**Purpose**: Administrative closure, present next steps.

**Output**: Recipe metadata written. Transition message with:
- `zcp sync push recipes {slug}` to publish to GitHub
- Strapi CMS entry creation instructions
- Test launch instructions for all 6 environments

---

## Eval Integration

### `zcp eval create`

Once the recipe workflow exists, headless creation is straightforward:

```go
// internal/eval/recipe_create.go

func (r *Runner) CreateRecipe(ctx context.Context, framework, tier string) (*RunResult, error) {
    // 1. Build prompt: "Create a {framework} {tier} recipe using ZCP"
    prompt := BuildRecipeCreatePrompt(framework, tier)
    
    // 2. Spawn Claude CLI (same as eval.Runner.Run)
    // Claude calls zerops_workflow action="start" workflow="recipe"
    // ZCP guides it through all 6 steps
    
    // 3. Extract results (recipe files from output directory)
    
    // 4. Validate (run recipe lint on generated files)
    
    // 5. Optionally: run zcp sync push to publish
}
```

CLI:
```bash
zcp eval create --framework laravel --tier minimal     # Single recipe
zcp eval create --framework laravel --tier showcase    # Showcase variant
zcp eval create-suite --tier minimal --frameworks laravel,nestjs,django  # Batch
```

### Quality loop

```
zcp eval create → recipe files generated
  → zcp sync push (creates PR)
  → zcp sync pull (after merge)
  → zcp eval run --recipe laravel-hello-world (test the recipe via normal eval)
  → iterate if eval fails
```

---

## Implementation Phases

### Phase 1: Recipe workflow skeleton

**New files**:
- `internal/workflow/recipe.go` — `RecipeState`, `RecipePlan`, `RecipeTarget`, `DecisionResults`, `ResearchResults`
- `internal/workflow/recipe_steps.go` — 6 step definitions (tools, verification gates)

**Modified files**:
- `internal/workflow/state.go` — Add `Recipe *RecipeState` to `WorkflowState`, add `"recipe"` to workflow constants
- `internal/workflow/engine.go` — Add `RecipeStart`, `RecipeComplete`, `RecipeStatus` methods
- `internal/tools/workflow.go` — Wire recipe actions into the workflow tool dispatch

**Reuse**: Provision, generate, deploy steps reuse bootstrap step logic with recipe-specific guidance.

### Phase 2: Research step + decision logic

**New files**:
- `internal/workflow/recipe_decisions.go` — 4 decision trees as Go functions
- `internal/workflow/recipe_research.go` — Research form templates, validation

**New content**:
- `internal/content/workflows/recipe.md` — Section-tagged guidance for all 6 steps

### Phase 3: Finalize step + recipe file generation

**New files**:
- `internal/workflow/recipe_templates.go` — Env 0-5 import.yaml generators, README boilerplate generators
- `internal/workflow/recipe_checks.go` — Hard checkers for finalize step (fragment validation, naming, YAML structure, primitives coverage, comment width)
- `internal/tools/workflow_recipe.go` — Recipe-specific action handlers (if needed beyond workflow.go)

### Phase 4: Eval integration

**New files**:
- `internal/eval/recipe_create.go` — Headless recipe creation prompt builder + runner
- `cmd/zcp/eval.go` — Add `create` and `create-suite` subcommands

### Phase 5: Polish

- Comment quality scoring (automated ratio checking, "so what?" heuristics)
- Template completeness validation
- Cross-recipe consistency checks
- Integration with `zcp sync push` for one-command publish

---

## Key Files Reference

### Existing files that will be modified

| File | What changes |
|------|-------------|
| `internal/workflow/state.go` | Add `Recipe *RecipeState` field, `WorkflowRecipe` constant |
| `internal/workflow/engine.go` | Add recipe lifecycle methods |
| `internal/workflow/session.go` | Handle recipe session init/persistence |
| `internal/tools/workflow.go` | Wire recipe actions |
| `internal/server/server.go` | No changes needed (recipe uses existing tools) |
| `internal/server/instructions.go` | Add recipe workflow hints to system prompt |

### Existing files that inform the design

| File | What it teaches |
|------|----------------|
| `internal/workflow/bootstrap.go` | BootstrapState structure to mirror |
| `internal/workflow/bootstrap_steps.go` | Step definition pattern |
| `internal/workflow/bootstrap_checks.go` | StepChecker interface pattern |
| `internal/workflow/guidance.go` | assembleGuidance + assembleKnowledge pattern |
| `internal/workflow/bootstrap_guide_assembly.go` | buildGuide, formatEnvVarsForGuide |
| `internal/workflow/validate.go` | Plan validation patterns |
| `internal/content/workflows/bootstrap.md` | Section-tagged guidance format |
| `internal/eval/runner.go` | Headless Claude spawning pattern |
| `internal/eval/prompt.go` | Prompt construction patterns |
| `internal/sync/push_recipes.go` | Fragment injection patterns |
| `internal/sync/transform.go` | ExtractKnowledgeBase, ExtractIntegrationGuide, InjectFragment |
| `internal/knowledge/recipe_lint_test.go` | Recipe validation rules to reuse |

### New files to create

| File | Purpose |
|------|---------|
| `internal/workflow/recipe.go` | RecipeState, RecipePlan, types |
| `internal/workflow/recipe_steps.go` | 6 step definitions |
| `internal/workflow/recipe_decisions.go` | Decision tree logic |
| `internal/workflow/recipe_research.go` | Research form templates |
| `internal/workflow/recipe_templates.go` | Env import.yaml + README generators |
| `internal/workflow/recipe_checks.go` | Finalize step hard checkers |
| `internal/workflow/recipe_guidance.go` | Step guidance assembly |
| `internal/content/workflows/recipe.md` | Section-tagged guidance content |
| `internal/tools/workflow_recipe.go` | Recipe-specific handlers (if needed) |
| `internal/eval/recipe_create.go` | Headless recipe creation |

---

## Key Design Decisions (to be made)

1. **Should recipe workflow reuse bootstrap engine or have its own?** Bootstrap engine is tightly coupled to BootstrapState. Options: (a) extend engine with recipe methods, (b) create RecipeEngine. Recommendation: (a) — the engine pattern is sound, just add methods.

2. **Where do recipe files get written?** Options: (a) to the SSHFS mount alongside app code, (b) to a separate local directory. Recommendation: (a) for app code, (b) for recipe structure — recipe files are deliverables, not part of the app.

3. **How much should the LLM write vs ZCP generate?** Options: (a) LLM writes everything with guidance, (b) ZCP generates boilerplate, LLM customizes. Recommendation: (b) for env 3-5 READMEs and import.yaml skeletons (they're formulaic), (a) for app code and env 0-2 import.yaml comments (they need framework judgment).

4. **Should RecipePlan be submitted in the research step or discovered?** Options: (a) LLM fills a form and submits like bootstrap's plan, (b) ZCP derives from framework name + tier. Recommendation: (a) — the research tables capture framework-specific decisions that only the LLM (with web search + training data) can make.

5. **How to handle the comment system?** The 85% standard, three-tier strategy, and self-containment rules are recipe-specific content that doesn't exist in ZCP. Options: (a) encode in guidance markdown, (b) encode as hard checker rules. Recommendation: both — guidance teaches the LLM, checkers validate the output.

6. **How to validate import.yaml comment quality?** Hard to automate "educational quality." Options: (a) ratio checking only (0.3-0.6 comment lines per config line), (b) keyword checking (Zerops primitives mentioned), (c) manual review. Recommendation: (a) + (b) automated, with (c) for initial recipes.

---

## Meta-Prompt Content Decomposition

Where each meta-prompt section goes in the new architecture:

| Meta-prompt section | New location | Type |
|---|---|---|
| Research tables (Framework Identity, Build Pipeline, etc.) | `recipe.md` section `research-{tier}` | Guidance (LLM fills in) |
| Bootstrap Import Files | `recipe_templates.go` `GenerateBootstrapImport()` | Code (auto-generated) |
| Research → Output Mapping | Implicit in step guidance | Eliminated |
| Decision Trees (4 trees) | `recipe_decisions.go` | Code (programmatic) |
| Zerops Platform Terminology | Already in `themes/core.md` | Existing knowledge |
| Build & Deploy Pipeline | Already in `themes/core.md` | Existing knowledge |
| Environment Variables | Already in `themes/core.md` + `services.md` | Existing knowledge |
| zerops.yaml Intelligence | Already in `themes/core.md` | Existing knowledge |
| deployFiles/start Validation | Already in `themes/core.md` | Existing knowledge |
| Build Cache Strategy | `recipe.md` section `generate-cache` | Guidance (compact) |
| .gitignore Alignment | `recipe.md` section `generate-gitignore` | Guidance (compact) |
| Runtime Base Image OS | Already in runtime briefings + `bases/*.md` | Existing knowledge |
| Comment System (85% standard) | `recipe.md` section `finalize-comments` | Guidance (new content) |
| Service Priority | Already in `themes/core.md` | Existing knowledge |
| Port Configuration | Already in runtime briefings | Existing knowledge |
| healthCheck vs readinessCheck | Already in `themes/core.md` | Existing knowledge |
| Process Managers | Already in runtime briefings | Existing knowledge |
| Resource Configuration | Already in `runtime_resources.go` | Existing knowledge |
| initCommands Intelligence | Already in `themes/core.md` + briefings | Existing knowledge |
| The Six Environments | `recipe_templates.go` env generation + `recipe.md` section `finalize-environments` | Code + guidance |
| README Templates | `recipe_templates.go` README generation + `recipe.md` section `finalize-readmes` | Code + guidance |
| Import.yaml Conventions | `recipe.md` section `finalize-imports` + `recipe_checks.go` | Guidance + checker |
| Operational Workflow | Bootstrap workflow handles most; recipe-specific in `recipe.md` | Mostly eliminated |
| Minimal specific rules | `recipe.md` section `research-minimal` + step guidance | Guidance |
| Showcase specific rules | `recipe.md` section `research-showcase` + step guidance | Guidance |
| Verification Gates | `recipe_checks.go` hard checkers | Code |
| Laravel reference | Not needed (LLM loads laravel recipe via `zerops_knowledge`) | Eliminated |
| Framework Research Checklist | `recipe_research.go` validation | Code |

**Result**: ~2200 lines of meta-prompt → ~400 lines of new guidance in `recipe.md` + ~800 lines of new Go code. The rest is eliminated (already in ZCP) or automated (generated by code).

---

## The Recipes Repo — What Actually Exists

### Discovery: zeropsio/recipes already has its own `_llm/` system

The recipes repo (`/www/zerops-recipes/`, `github.com/zeropsio/recipes`) has a parallel LLM automation system in `_llm/`:

| File | Purpose |
|------|---------|
| `how-to-write-a-recipe.md` | Architecture diagram, data flow, step-by-step process, placeholder table, fragments reference, human-written reference list |
| `vibe-agent-prompt.md` | System prompt for "terminal agent" that creates recipes: constraints, file structure, YAML syntax, fragment requirements, comment style, workflow checklist |
| `vibe-research-prompt.md` | System prompt for "research agent": output schema template for `research.md` files |
| `vibe.prompt` | Planning TODOs for the vibe-coding workflow |
| `reword-yaml-comments.prompt` | Prompt for revising YAML comments in existing recipes |

This is a **three-agent pipeline**: research agent → `research.md` → terminal agent → recipe files. Similar to the meta-prompt's two-layer approach, but simpler and focused on the recipes repo side only (no app code creation).

### Key insight: Two parallel systems, neither complete

| System | Lives in | Creates app code? | Creates recipe files? | Has platform knowledge? | Has hard verification? |
|--------|----------|-------------------|----------------------|------------------------|----------------------|
| `zrecipator` meta-prompt (ZCP) | `docs/zrecipator/` | Yes (SSH + deploy) | Yes (6 envs, READMEs) | Yes (2200 lines inline) | No (checklists only) |
| `_llm/` vibe system (recipes repo) | `zerops-recipes/_llm/` | No (filesystem only) | Yes (template + replace) | Minimal (links to docs) | No (human testing) |
| ZCP bootstrap workflow | `internal/workflow/` | Yes (mount + deploy) | No | Yes (knowledge store) | Yes (hard checkers) |

Neither system alone does everything. The meta-prompt tries to be self-contained but is bloated. The `_llm/` system is lightweight but can't create or deploy apps. ZCP's bootstrap can create and deploy but doesn't know about recipe file structure.

### Actual folder structure (em-dash numbered, not slug-based)

The `_llm/vibe-agent-prompt.md` references slug-based folder names (`agent/`, `remote/`, `local/`, `stage/`, `small-production/`, `highly-available-production/`), but the actual folders on disk use em-dash numbered format:

```
bun-hello-world/
├── 0 — AI Agent/
├── 1 — Remote (CDE)/
├── 2 — Local/
├── 3 — Stage/
├── 4 — Small Production/
├── 5 — Highly-available Production/
└── README.md
```

The em-dash (U+2014) + numbered prefix is the canonical format in the actual repo. The `_llm/` docs describe a different (slug-based) naming that doesn't match reality. This is a bug in the `_llm/` docs, not in the actual recipes.

### `.human` marker files

9 recipes/templates are marked as human-written (never auto-modify):
- `_template`, `_template_oss` (the templates themselves)
- `django`, `elk`, `laravel-jetstream`, `mailpit`, `nestjs-hello-world`, `strapi`, `umami`

All other recipes (~37) lack `.human` and were either generated or closely follow the template.

### Recipe inventory: 46 recipes total

| Category | Count | Examples |
|----------|-------|---------|
| Language hello-world (type 1) | 11 | bun, nodejs, go, python, ruby, php, rust, deno, dotnet, gleam, java |
| Frontend static (type 2a) | 12 | react, vue, angular, svelte, sveltekit, solidstart, qwik-city, nuxt, nextjs, astro, analog, react-router |
| Frontend SSR (type 2b) | 9 | angular, sveltekit, solidstart, qwik-city, nuxt, nextjs, astro, analog, react-router |
| Framework (type 3) | 4 | django, laravel-hello-world, laravel-jetstream, nestjs-hello-world |
| OSS (types 6-7) | 4 | strapi, umami, elk, mailpit |
| Showcase | 1 | zerops-showcase |
| Elixir | 1 | elixir-hello-world |
| Duplicate | 1 | reactrouterv7-static (alongside react-router-static) |

**Type 3 gap**: Only 4 framework recipes exist. No Spring Boot, no Rails, no Phoenix, no Express, no FastAPI. This is exactly what we need to produce.

**Type 4 gap**: Only `zerops-showcase` exists (Bun+Python, not a framework showcase). No framework-specific showcases at all.

### Environment scaling progression (observed from real recipes)

| Property | Agent/Remote | Local | Stage | Small Prod | HA Prod |
|----------|-------------|-------|-------|------------|---------|
| App services | appdev + appstage | app | app | app | app |
| App zeropsSetup | dev + prod | prod | prod | prod | prod |
| DB mode | NON_HA | NON_HA | NON_HA | NON_HA* | HA |
| minContainers | — | — | — | 2 | 2 |
| cpuMode | — | — | — | — | DEDICATED |
| corePackage | — | — | — | — | SERIOUS |
| minFreeRamGB | — | — | 0.25 | 0.125 | 0.25 |
| enableSubdomainAccess | all app services | yes | yes | yes | yes |

*Note: Laravel uses HA even for small-prod DB. Most hello-worlds use NON_HA. This is a framework decision.

### Template placeholder system

The `_template/` uses 8 `PLACEHOLDER_*` tokens that are find-and-replaced:

| Placeholder | Example |
|-------------|---------|
| `PLACEHOLDER_PROJECT_NAME` | `laravel-hello-world` |
| `PLACEHOLDER_PRETTY_RECIPE_NAME` | `Laravel Hello World` |
| `PLACEHOLDER_RECIPE_DIRECTORY` | `laravel-hello-world` |
| `PLACEHOLDER_RECIPE_SOFTWARE` | `[Laravel](https://laravel.com) applications` |
| `PLACEHOLDER_RECIPE_DESCRIPTION` | `Laravel app with PostgreSQL...` |
| `PLACEHOLDER_COVER_SVG` | `cover-php.svg` |
| `PLACEHOLDER_RECIPE_TAGS` | `php,laravel` |
| `PLACEHOLDER_PRETTY_RECIPE_TAGS` | `PHP` |

The template import.yaml files are stubs (`hostname: todo, type: runtime`) — they must be fully rewritten per framework, not just placeholder-replaced.

---

## Revised Architecture: Unified Recipe Creation

### What changed after exploring zerops-recipes

The original proposal focused on ZCP workflow steps. After seeing the recipes repo's `_llm/` system and actual recipe structure, the design should account for:

1. **Two output targets**: app repo files (zerops.yaml, app code, app README) AND recipe repo files (6 import.yamls, 7 READMEs)
2. **The template system is useful** — don't reinvent it, generate from it
3. **The `_llm/` research→agent pipeline is sound** — ZCP can be the "research agent" (via knowledge store) AND the "terminal agent" (via MCP tools)
4. **Recipe files are highly formulaic** — env 3-5 READMEs are identical across all recipes (only name/slug changes). Import.yaml structure per env follows rigid patterns. Only comments and service config vary.
5. **The `.human` marker convention** should be respected — ZCP-generated recipes should NOT get `.human`

### Revised step design

#### RESEARCH step: ZCP replaces the research agent

Instead of a separate research agent writing `research.md`, ZCP's knowledge system provides:
- Runtime hello-world recipe (proven base config) via `zerops_knowledge recipe="{lang}-hello-world"`
- Runtime briefing (stack rules, wiring) via `zerops_knowledge` briefing mode
- Live service type catalog (validated versions)
- Import/zerops.yml JSON schemas (from API)

The LLM fills in framework-specific research (build commands, libraries, migration system) from its training data + optional web search. Output: `RecipePlan` with all decisions resolved.

#### PROVISION step: unchanged

#### GENERATE step: creates app code + zerops.yaml

Writes to SSHFS mount: framework project, zerops.yaml (base+prod+dev+worker), app README with fragments. ZCP injects knowledge, schemas, env vars.

#### DEPLOY step: unchanged (dev + stage, verify health)

#### FINALIZE step: generates recipe repo files

This is the key new insight — **most recipe files can be generated programmatically from RecipePlan + templates**:

**Auto-generated (no LLM needed)**:
- Main recipe `README.md` — pure template substitution (8 placeholders)
- Env 3-5 `README.md` files — fixed boilerplate text, only name/slug varies
- Env 0-2 `README.md` files — fixed first line + services line derived from RecipePlan
- `recipe-output.yaml` for Strapi registration

**Generated skeleton + LLM comments**:
- 6 `import.yaml` files — ZCP generates the YAML structure (services, types, modes, scaling per env), LLM writes the explanatory comments

**Already created in GENERATE step**:
- App README with integration-guide fragment (contains commented zerops.yaml)
- The zerops.yaml itself

This means the FINALIZE step is mostly **code generation**, not LLM creativity. The LLM's job is to review and add comments, not to create the structure from scratch.

#### CLOSE step: presents publish commands

```bash
# Publish to recipe repo
cd /path/to/recipes && git add {slug}/ && git commit && git push

# Publish to app repo  
cd /path/to/app && git push

# Or via ZCP sync
zcp sync push recipes {slug}

# Register in Strapi (manual or API)
# Fields from recipe-output.yaml
```

### What ZCP generates vs what the LLM writes

| Artifact | Who generates | How |
|----------|--------------|-----|
| App source code (dashboard, migrations, models) | LLM | Creative, framework-specific |
| `zerops.yaml` (base+prod+dev) | LLM with ZCP guidance | Schemas + briefings + discovered env vars |
| App `README.md` with fragments | LLM | Template + framework-specific content |
| `.gitignore` | LLM | Framework-specific |
| Recipe `README.md` | ZCP code | Pure template substitution |
| Env `README.md` (all 6) | ZCP code | Fixed boilerplate |
| Env `import.yaml` structure (all 6) | ZCP code | Derived from RecipePlan + env scaling matrix |
| Env `import.yaml` comments | LLM | Review + add comments to generated YAML |
| `recipe-output.yaml` | ZCP code | Derived from RecipePlan |

**This dramatically reduces what the LLM needs to do in the finalize step.** Instead of writing 13 files from scratch, it reviews and comments on 6 generated import.yamls. Everything else is mechanical.

### Import.yaml generation algorithm

```
For each env in [agent, remote, local, stage, small-prod, ha-prod]:
  1. Start with project: name: {slug}-{env-suffix}
  2. If env needs preprocessor (framework has secrets): add # zeropsPreprocessor=on
  3. For each service in RecipePlan:
     a. Apply env-specific overrides (mode, scaling, containers)
     b. For app services:
        - If env is agent/remote: create appdev (dev) + appstage (prod)
        - If env is local/stage/prod: create app (prod)
        - Add buildFromGit, zeropsSetup, enableSubdomainAccess
        - If framework needs secrets: add envSecrets
     c. For data services:
        - Apply mode (NON_HA for dev/stage, HA for ha-prod)
        - Apply scaling (minRam, minFreeRamGB, cpuMode per env)
        - Add priority: 10
  4. If env is ha-prod: add corePackage: SERIOUS
  5. If env is small-prod: add minContainers: 2
```

This is ~100 lines of Go code that replaces ~200 lines of meta-prompt environment specification.
