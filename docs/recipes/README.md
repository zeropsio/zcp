# Recipe Knowledge System

ZCP's `internal/knowledge/recipes/` contains operational knowledge for each Zerops recipe. This knowledge is **synced from canonical external sources** — ZCP is a consumer, not the owner.

## Architecture: Unified Recipe Layer

This branch replaces the **dual-layer knowledge model** (separate `runtimes/*.md` hand-written guides + `recipes/*.md` hand-written recipes) with a **single unified recipe layer** pulled dynamically from the Zerops Recipe API.

### What was abolished

| Old | New | Why |
|-----|-----|-----|
| `runtimes/` (17 files: 12 language runtimes + 5 infra bases) | Deleted — runtime knowledge resolves via recipe fallback chain; 5 infra bases moved to `bases/` | Maintaining separate runtime guides duplicated effort; the hello-world recipe for each runtime IS the authoritative runtime guide |
| `recipes/` (29 hand-written recipes: laravel.md, django.md, ...) | Deleted — all recipes pulled from API, gitignored | Hand-written recipes drifted from the canonical app repos; API is the source of truth |
| `guides/` and `decisions/` (committed) | Now pulled from docs repo, gitignored | Same drift problem — docs repo is canonical |
| `filterDeployPatterns()` | Removed entirely | Was mode-aware filtering of `### Deploy Patterns` sections; no longer needed since recipes now ship both dev and prod zerops.yml setups with inline comments |
| Keyword search scoring (`## Keywords` sections) | Removed — search uses title (2x) + content (1x) only | Keywords required manual maintenance; content-based matching is more robust for API-sourced recipes |
| Runtime guide prepending in `GetRecipe` | Removed — each recipe is standalone | Framework recipes (laravel) shouldn't have PHP runtime knowledge injected; the recipe's own knowledge-base fragment is the authoritative source |
| Verbose mode adaptation header (5 lines) | Concise single-line pointer to setup block | Recipes now include both setups with comments — the header was restating what the YAML already teaches |

### What's preserved

- `themes/*.md` (platform universals, services, import YAML docs) — unchanged, committed
- `bases/*.md` (alpine, docker, nginx, static, ubuntu) — renamed from `runtimes/`, committed
- `runtimeRecipeHints` map — runtime→recipe matching for briefing hints
- `runtimeNormalizer` — maps `php-nginx` → `php`, etc.
- Mode adaptation headers (simplified, not removed)
- Weighted substring search engine — scoring simplified (keywords removed, title 2x + content 1x)

### Runtime guide resolution chain

`getRuntimeGuide(slug)` resolves runtime knowledge through a 3-step fallback:

1. `recipes/{slug}-hello-world` — primary (e.g., `bun-hello-world` serves as the Bun runtime guide)
2. `recipes/{slug}` — direct match
3. `bases/{slug}` — infrastructure bases (alpine, docker, nginx, static, ubuntu)

This means the **hello-world recipe for each runtime IS the runtime guide**. No separate file to maintain.

### Recipe enrichment path

Most recipes start with only `description` + `## zerops.yml` + `## Service Definitions` from the API. To add operational knowledge:

1. Write `knowledge-base` fragment in the app README (Base Image, Binding, Resource Requirements, Gotchas)
2. Push to GitHub, refresh Strapi cache
3. `scripts/sync-knowledge.sh pull recipes` — content appears in ZCP
4. Lint tests validate the new content (`NoPlatformDuplication`, `ServiceDefinitionsValid`, etc.)

Currently only **Bun** has a `knowledge-base` fragment. The other 32 recipes have description + zerops.yml + service definitions.

### Key design decision: standalone recipes

In the old model, `GetRecipe("laravel")` would prepend the PHP runtime guide to the Laravel recipe content. This was wrong — Laravel has its own PHP configuration needs (Composer, Artisan, queue workers) that differ from a bare PHP hello-world. The runtime guide's generic advice created confusion.

Now each recipe is **standalone**: `GetRecipe` prepends only platform universals (`themes/universals.md`) which are truly universal. Runtime-specific knowledge lives in the recipe's own `knowledge-base` fragment.

## How It Works

```
App README (canonical)          Recipe API (Strapi)           ZCP (consumer)
─────────────────────          ──────────────────           ──────────────
knowledge-base fragment  →     extracts.knowledge-base  →   recipes/{slug}.md (knowledge sections)
integration-guide        →     extracts.integration-guide →  recipes/{slug}.md (zerops.yml + integration steps)
per-service intro        →     svc.extracts.intro       →   frontmatter description (preferred)
recipe-level intro       →     extracts.intro           →   frontmatter description (fallback)
import.yaml (env0,env4)  →     environments[].import    →   ## Service Definitions section
```

**Pull** (`scripts/sync-knowledge.sh pull recipes`): one API call to `api.zerops.io` fetches all non-utility recipes dynamically. No hardcoded list — new recipes appear automatically.

**Push** (`scripts/sync-knowledge.sh push recipes`): writes knowledge-base content back to local app repo clones (tries `{slug}-app/` then `{slug}/`). You review, commit, push to GitHub, then refresh the Strapi cache.

All synced files (`recipes/`, `guides/`, `decisions/`) are **gitignored** — run `scripts/sync-knowledge.sh pull` before build. Infrastructure bases (`bases/`) are committed.

## Recipe File Format

Each recipe in `internal/knowledge/recipes/` has up to 3 content sources from the app README, all synced via the API:

```markdown
---
description: "Per-service intro — what this app does."
---

# Bun Hello World on Zerops

## Base Image
Includes: Bun, npm, yarn, git, bunx. NOT included: pnpm.

## Gotchas
- `BUN_INSTALL: ./.bun` for build caching — default ~/.bun is outside the project tree
- Use `bunx` instead of `npx` — npx may not resolve correctly in Bun runtime

## 1. Adding `zerops.yaml`
(full commented YAML with both prod and dev setups — from integration-guide fragment)

## 2. Add Support For Object Storage
(framework-specific integration steps — from integration-guide fragment, e.g. Laravel S3, Django settings)

## Service Definitions
### Dev/Stage (from AI Agent environment)
(full import YAML from environment 0)
### Small Production
(full import YAML from environment 4)
```

**Three content sources:**

| Source | Fragment | What it contains |
|---|---|---|
| **knowledge-base** | `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->` | Runtime-specific gotchas, base image — only what you can't learn from platform docs or general runtime docs |
| **integration-guide** | `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->` | Full zerops.yml with inline comments PLUS framework-specific integration steps (S3, env vars, mailer, etc.) |
| **imports** | `environments[].import` | Battle-tested import YAML with proven scaling values |

Platform knowledge lives in `themes/universals.md` and is prepended automatically. Recipes contain only what's **irreducible to the specific runtime/framework**. The `NoPlatformDuplication` lint flags violations.

Most recipes currently have `description` + integration guide (zerops.yml) + service definitions. Knowledge-base sections (Base Image, Gotchas) come from the app README `knowledge-base` fragment — only Bun has this so far.

## Knowledge-Base Content Guidelines

Each item must be **irreducible to the specific runtime/framework** — not learnable from platform docs or general runtime docs:

- **What's in the Zerops base image** — not documented by the runtime itself (e.g., bunx included, pnpm not)
- **Runtime-specific cache/env workarounds** — BUN_INSTALL path, bunx vs npx
- **Real support ticket / agent failure patterns** — things that actually break on Zerops

**Do NOT include**: binding syntax (agent knows framework APIs, universals covers the platform rule), tilde behavior, build/run separation, L7 routing, autoscaling timing, minRam values — these are either platform universals (prepended automatically), general knowledge (LLM already knows), or structured data (in Service Definitions import YAML).

## How the Agent Uses Recipe Content

The agent does not copy-paste recipe YAML. It generates `zerops.yml` and `import.yaml` from scratch during bootstrap, because the user's app has different hostnames, dependencies, env vars, and code than the recipe.

Recipe content provides **knowledge through commented examples**:

- **Integration guide** teaches patterns: how to structure build vs run, where `BUN_INSTALL` goes, why `--frozen-lockfile`, how `initCommands` work with `zsc execOnce`, how dev and prod setups differ structurally. The inline comments are critical — they explain *why* each value was chosen, not just what it is.
- **Service definitions** teach scaling patterns: priority ordering, dev/stage pairing, verticalAutoscaling shape, minContainers for production.
- **Knowledge-base** teaches runtime-specific gotchas that aren't visible in the YAML itself.

The agent reads these patterns, then applies them to the user's specific situation using schema rules from `core.md` and actual env vars discovered during provision.

## Go Consumption Layer

All access goes through the `Provider` interface (`engine.go`). Key methods:

### GetRecipe (briefing.go)

`GetRecipe(name, mode)` returns the full recipe content with:
1. **Platform universals** prepended (from `themes/universals.md`) — always
2. **Mode adaptation header** — concise single-line pointer to the right setup block:
   - `dev`/`standard`: "Use the `dev` setup block from the zerops.yml below."
   - `simple`: "Use the `prod` setup block below, but override `deployFiles: [.]`."
   - Recipes now include both `dev` and `prod` zerops.yml setups with inline comments, so the header doesn't need to restate what the YAML teaches.

Resolution chain: exact URI match → fuzzy match (prefix/substring/content) → disambiguation list.

### GetBriefing (briefing.go)

`GetBriefing(runtime, services, mode, liveTypes)` assembles stack-specific knowledge in 7 layers:

1. **Live service stacks** — current deployed services with version checking
2. **Runtime guide** — resolved via the 3-step fallback chain (see above)
3. **Matching recipes hint** — links to relevant recipes for the runtime via `runtimeRecipeHints`
4. **Service cards** — per-service docs from `themes/services.md`
5. **Wiring patterns** — cross-service connection syntax
6. **Decision hints** — relevant service selection decisions from `themes/operations.md`
7. **Version check** — live stack version validation

### Search (engine.go)

Simple text-matching with field boosts and query expansion:
- **Scoring**: title match = 2.0x, content match = 1.0x per word (keywords removed)
- **Query aliases**: 25 expansions (e.g., `postgres` → `postgres postgresql`, `redis` → `redis valkey`)
- **Fuzzy recipe matching** (`findMatchingRecipes`): prefix → substring → content search (replaced old keyword matching)

### Document parsing (documents.go)

- **Frontmatter extraction**: YAML `description:` field parsed from `---` blocks (new)
- **Description priority**: frontmatter `description:` > `## TL;DR` > first paragraph
- **Disambiguation**: uses `doc.Description` instead of old `doc.TLDR`
- **Keywords and TL;DR**: still parsed (legacy) but not used in search scoring

### Service Definition Library (service_definitions.go)

#### The problem

The bootstrap provision step had nothing — it read abstract schema from `core.md` and the agent had to compose every `import.yaml` from first principles. Every field, every service block, every relationship between services was guessed from documentation.

The Recipe API provides 6 battle-tested, thoroughly commented `import.yaml` files per recipe (33 recipes × 6 environments = 198 proven configurations). The sync script was ignoring them entirely.

#### The solution

The sync script now extracts full import YAML from env0 (dev/stage) and env4 (small-prod) into a `## Service Definitions` section in each recipe `.md`. The Go layer parses these into a per-recipe library of complete, proven import configurations.

For any runtime the agent encounters, it can now look up a real, working `import.yaml` that includes the full service block structure with correct type versions, priority ordering, correct autoscaling shape, managed service patterns, and comments explaining why each value was chosen. The shift is **abstract schema documentation → concrete reference implementations**.

#### GetServiceDefinitions

`GetServiceDefinitions(name)` returns a `ServiceDefinitions` struct with:
- `DevStageImport` — full import YAML from env0 (AI Agent: dev+stage pair)
- `SmallProdImport` — full import YAML from env4 (Small Production: minContainers, minFreeRamGB)

#### TransformForBootstrap

`TransformForBootstrap(importYAML)` adapts recipe imports for bootstrap use. Recipe imports use `buildFromGit` + `zeropsSetup` (automated git-based provisioning). Bootstrap uses `startWithoutCode` (interactive SSH-based iteration):

| Recipe import has | Bootstrap transform |
|---|---|
| `buildFromGit: url` | **Remove** — bootstrap uses SSHFS + `zcli push` |
| `zeropsSetup: dev/prod` | **Remove** — bootstrap hostnames ARE the setup names |
| `enableSubdomainAccess: true` | **Remove** — with `startWithoutCode` no app is listening, subdomain points at nothing; developer adds it when they push a working app |
| (dev services) | **Add** `startWithoutCode: true` — bootstrap dev containers start empty, developer drives via SSH |
| `verticalAutoscaling` | **Keep** — proven scaling values |
| `minContainers`, `minFreeRamGB` | **Keep** — production patterns |
| `cpuMode: DEDICATED` | **Keep** — HA-prod only |

These rules are documented in: `core.md` (`startWithoutCode`, `buildFromGit`), `bootstrap.md` (strategy distinction), `service_meta.go` (`StrategyPushDev` vs `StrategyCICD`).

#### extractServiceEntries

`extractServiceEntries(importYAML)` splits an import into runtime (USER category) and managed (STANDARD category) service entries. This enables composite stack assembly from multiple recipes.

#### Composite stack workflow

When no single recipe matches a request (e.g., `bun api + nextjs frontend + python worker + postgres + valkey`), the provision step can:

1. **Look up bun-hello-world** → extract bun service definition (`minRam: 0.5` dev, `0.25` prod)
2. **Look up nextjs-ssr-hello-world** → extract nodejs service definition (`minRam: 0.25`, `minFreeRamGB: 0.125`)
3. **Look up python-hello-world** → extract python service definition (`minRam: 0.5` dev)
4. **Managed services** (postgres, valkey) → from `core.md` schema + service cards
5. **Merge** into one `import.yaml`, applying bootstrap transforms (drop `buildFromGit`, add `startWithoutCode` on dev)
6. **Deduplicate** — all three recipes ship their own `db`, but the composite stack only needs one

**Before**: Agent guesses `minRam: 1.0` for everything (bootstrap.md default for compiled runtimes).
**After**: bun gets `0.5`, nextjs gets `0.25`, python gets `0.5` — values proven in production.

#### Mode ↔ Environment tier mapping

Bootstrap's 3 modes map to recipe environment tiers:

| Bootstrap mode | What it creates | Closest recipe environment | Env index |
|---|---|---|---|
| **standard** (default) | `{name}dev` + `{name}stage` | 0 — AI Agent (dev+stage pair) | 0 |
| **dev** | `{name}dev` only | 0 — AI Agent (dev service only) | 0 |
| **simple** | `{name}` (single) | 3 — Stage (single prod-like service) | 3 |

For production tiers (not part of bootstrap, but relevant for scaling guidance):
- Small prod → environment index 4
- HA prod → environment index 5

### Lint Tests

- `NoPlatformDuplication` — warns when enriched recipes duplicate universals content (logged, not errored — content is API-sourced, fix at source)
- `ServiceDefinitionsValid` — validates structural integrity of extracted definitions
- `HasDescription` — replaced old `HasKeywords` / `HasTLDR` checks
- `HasGotchas`, `HasZeropsYml` — skip gracefully when recipe lacks knowledge-base enrichment
- `validateZeropsYml()` takes a `strict bool` — ports and healthCheck enforcement only on enriched recipes
- `healthCheckExemptSetups` — `"dev": true` (dev entries use `zsc noop --silent`)
- `VersionsKnown` — validates version refs against platform catalog snapshot
- All in `recipe_lint_test.go`, `runtime_lint_test.go`

## Description Priority

The sync script extracts descriptions in this priority order:

1. **Per-service intro** (`environments[0].services[].extracts.intro`) — describes what the app does ("Minimal Bun API with PostgreSQL")
2. **Recipe-level intro** (`extracts.intro`) — describes the recipe package ("A Bun application with six configurations")

Per-service intro is preferred because it's app-focused. The Go layer then uses `Document.Description` with fallback: frontmatter `description:` > `## TL;DR` > first paragraph.

## Recipe Taxonomy

7 types, from simplest to most complex:

| # | Type | Status | Purpose |
|---|------|--------|---------|
| 1 | Runtime Hello World | **DONE** | "This language runs on Zerops" |
| 2a | Frontend Static | **DONE** | "This SPA builds & serves" |
| 2b | Frontend SSR | **DONE** | "This SSR framework runs" |
| 3 | Backend Framework Minimal | IN PROGRESS | "This framework runs natively" |
| 4 | Backend Framework Showcase | IN PROGRESS | "Full stack works" |
| 5 | Framework Starter Kit | Not started | "This popular project works" |
| 6 | CMS / E-commerce OSS | Not started | "Self-host this platform" |
| 7 | Software OSS | Not started | "One-click self-hosting" |

Types 1-2 are done. Each has a hello-world app in `zerops-recipe-apps/`. Framework recipes (3-7) are standalone — they don't inherit from runtime hello-worlds.

## Recipe System Architecture

### Components

- **App repos** (`zerops-recipe-apps/*-app`): source code + `zerops.yaml` + `README.md` with fragments
- **Recipe repo** (`zeropsio/recipes`): per-environment `import.yaml` + recipe-level `README.md`
- **Strapi**: metadata (name, slug, categories, icon) + GitHub cache + fragment extraction
- **Recipe API** (`api.zerops.io/api/recipes`): structured JSON with all extracted data

### Fragment System

App READMEs use fragment markers to embed structured data:

```markdown
<!-- #ZEROPS_EXTRACT_START:intro# -->
Description of the app.
<!-- #ZEROPS_EXTRACT_END:intro# -->

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
Zerops-specific operational knowledge.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->
Full zerops.yaml with comments + explanation.
<!-- #ZEROPS_EXTRACT_END:integration-guide# -->
```

Strapi extracts these and serves them via the API. The sync script reads from the API.

### API Endpoints

**List all recipes (excluding service-utility):**
```
GET https://api.zerops.io/api/recipes
  ?filters[recipeCategories][slug][$ne]=service-utility
  &populate[recipeLanguageFrameworks][populate]=*
  &populate[recipeCategories]=true
  &pagination[pageSize]=100
```

**Per-recipe data** (in response):
- `sourceData.environments[].services[].extracts.intro` — per-service app description (preferred)
- `sourceData.extracts.intro` — recipe-level description (fallback)
- `sourceData.environments[].services[].extracts["integration-guide"]` — zerops.yml + integration steps (preferred)
- `sourceData.environments[].services[].zeropsYaml` — raw zerops.yaml (fallback if no integration guide)
- `sourceData.environments[].services[].extracts["knowledge-base"]` — runtime-specific gotchas
- `sourceData.environments[].import` — full import YAML per environment (6 environments)

### Cache Behavior

After pushing changes to GitHub, the Strapi cache must be refreshed:
1. Go to [Strapi admin](https://api-d89-1337.prg1.zerops.app/admin/content-manager/collection-types/api::recipe.recipe) → recipe detail → "Refresh Cache"
2. Or wait for automatic cache refresh

### 6 Lifecycle Environments

Each language/framework recipe offers:

| # | Environment | Purpose | Used by sync |
|---|-------------|---------|--------------|
| 0 | AI Agent | Development space for AI agents (dev+stage pair) | **Yes** — Service Definitions Dev/Stage |
| 1 | Remote (CDE) | Cloud Development Environment via SSH | No |
| 2 | Local | Local dev with zCLI VPN for DB access | No |
| 3 | Stage | Production config, single container | No |
| 4 | Small Production | Production-ready, moderate throughput | **Yes** — Service Definitions Small Prod |
| 5 | Highly-available Production | Enhanced scaling, dedicated resources, HA | No |

Bootstrap mode mapping: `standard`/`dev` → env0, `simple` → env3. Production tiers: small-prod → env4, HA-prod → env5.

## Creating a New Recipe

### 1. Create app repo

In [zerops-recipe-apps](https://github.com/zerops-recipe-apps): create `{slug}-app` repo with:
- Thoroughly commented `zerops.yaml`
- `README.md` with `intro`, `integration-guide`, `knowledge-base` fragments
- Working application code

### 2. Create recipe folder

In [zeropsio/recipes](https://github.com/zeropsio/recipes): copy `_template`, create `import.yaml` per environment.

### 3. Register in Strapi

Add entry with name, slug, icon, categories.

### 4. Sync to ZCP

Run `scripts/sync-knowledge.sh pull recipes` to pull the new recipe into ZCP's knowledge.

## Embedding and Build

The `go:embed` directive in `documents.go` embeds all knowledge at compile time:

```go
//go:embed themes/*.md bases/*.md all:recipes all:guides all:decisions
```

- `themes/*.md` and `bases/*.md` — committed, always available
- `all:recipes`, `all:guides`, `all:decisions` — gitignored, must be pulled before build (the `all:` prefix includes directories even when empty, preventing build failures)
- `knowledgeDirs` lists the walk order: `themes`, `bases`, `recipes`, `guides`, `decisions`

**Build prerequisite**: `scripts/sync-knowledge.sh pull` before `go build`.

## Current State

- **33 recipes** pulled dynamically from Recipe API (all non-utility recipes)
- **All recipe .md files are gitignored** — run `scripts/sync-knowledge.sh pull recipes` before build
- **Infrastructure bases** (alpine, docker, nginx, static, ubuntu) are in `internal/knowledge/bases/` (committed)
- **Bun** is the only recipe with `knowledge-base` fragment — others have intro + integration guide + service definitions only
- **elixir** is missing from API; **nodejs** has slug `recipe` (remapped to `nodejs-hello-world` by sync script)
- **Slug remapping**: API slug `"recipe"` → `"nodejs-hello-world"` (handled in sync script)

## FAQ

See [branch-review-response.md](branch-review-response.md) for detailed answers to:
- Why is the knowledge-base so small? (Point 1 — condensation)
- What happened to the runtime knowledge layer? (Point 2 — it's preserved, lives in the recipe now)
- Aren't service definitions pre-made templates? (Point 3 — no, they're reference data for the generative approach)
- How does the agent use recipe YAML? (Knowledge through commented examples, not copy-paste)
