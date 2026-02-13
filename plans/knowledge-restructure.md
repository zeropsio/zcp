# Knowledge Base Restructure: Core Principles Architecture

## Mission

Transform the ZCP knowledge base from **66 topic-based reference documents** (5000+ lines, ~18% signal for typical tasks) into a **Core Principles + Exceptions + Service Cards + Recipes** architecture (~350-400 lines core, ~95% signal) optimized for LLM-driven infrastructure generation.

---

## Why

An LLM already knows programming languages and frameworks. It knows how `composer install` works, how PostgreSQL connection strings look, what `npm build` does. What it **does not** know are **Zerops-specific rules** — things that deviate from what it would expect.

Current approach: 66 reference docs → BM25 search → LLM reads 500+ lines → 18% relevant.
Target approach: ~350-400 lines of core knowledge (principles + exceptions + service cards + wiring) + on-demand recipes → 95% relevant → correct YAML on first pass.

**Example**: For PHP + PostgreSQL bootstrap, current approach loads ~568 lines from 4+ documents. The LLM only needs ~90 lines of Zerops-specific rules + the PHP exception (build base ≠ run base) + PostgreSQL wiring pattern.

---

## Source Materials

All paths relative to `/Users/macbook/Documents/Zerops-MCP/`.

| Source | Location | Content | Volume |
|--------|----------|---------|--------|
| **Zerops Docs** | `zerops-docs/apps/docs/content/` | 324 MDX files — full platform docs | ~40K lines |
| **Zerops Docs Data** | `zerops-docs/apps/docs/static/data.json` | **ONLY source of service version numbers** — canonical version list for all runtimes/services | 1 JSON file |
| **Zerops Docs Shared** | `zerops-docs/apps/docs/content/shared/` | 13 shared MDX components (partials) defining generic platform behavior (scaling, logs, controls, etc.) — understanding these reveals what's generic vs Zerops-specific | ~13 files |
| **Recipe Extracts** | `recipe-extracts/recipe-*/` | 48 deployment recipes (zerops.yml, import.yml, EXTRACT.md) | 48 dirs, ~196 files |
| **Current KB** | `knowledge/` | 66 MD files — existing distilled knowledge | ~5K lines |
| **JSON Schema** | `https://api.app-prg1.zerops.io/api/rest/public/settings/import-project-yml-json-schema.json` | Machine-readable import.yml schema | ~500 fields |
| **ZCP Code** | `zcp/internal/` | Knowledge engine, workflows, tools | Go source |
| **Current Workflows** | `zcp/internal/content/workflows/` | bootstrap.md, deploy.md, debug.md, configure.md, scale.md, monitor.md | 6 files |

### Critical Note: zerops-docs Structure & Repetition

The zerops-docs site has **14-15 MDX files per runtime** (create, build-pipeline, build-process, deploy-process, env-variables, logs, scaling, controls, upgrade, filebrowser, shared-storage, customize-runtime, trigger-pipeline, overview). **~90% of this content is GENERIC platform behavior** that is identical across all runtimes (how to create a service, how to view logs, how to scale). The same applies to managed services.

**The Zerops-specific nuggets are buried inside generic pages.** For example:
- `php/how-to/build-pipeline.mdx` — mostly generic build pipeline docs, but contains PHP-specific: `base: php@X` for build, `php-nginx@X` for run, `documentRoot`, `--ignore-platform-reqs`
- `postgresql/how-to/create.mdx` — mostly generic "click create" docs, but contains: version list, HA/NON_HA options, auto-injected env var names
- `nodejs/how-to/env-variables.mdx` — mostly generic env var docs (same for all runtimes), but contains: Node.js-specific `PORT` convention

**Extraction strategy**: Don't read all 324 files linearly. Instead:
1. **Start from existing KB** — `knowledge/` already distills much Zerops-specific content. Use it as baseline, then validate against zerops-docs
2. Read `data.json` first — get canonical version numbers for ALL services
3. Read `shared/` partials — understand which content is generic platform behavior (reused across all runtimes via MDX imports)
4. Read ONE runtime's full set (e.g., `nodejs/`) to understand the generic template
5. Then for other runtimes, only read pages that DIFFER (overview, build-pipeline, deploy-process)
6. For managed services, focus on overview + create + tech-specific pages
7. Skip pages that are 100% generic across services (logs, scaling, controls, upgrade, filebrowser)

### Critical Note: Services ARE Zerops-Specific

Unlike runtimes (where LLM already knows Node.js/PHP/Go), **managed service behavior on Zerops IS genuinely platform-specific**. The LLM cannot derive from general PostgreSQL knowledge:
- Which ports Zerops assigns (5432 primary, 5433 replicas, 6432 external TLS)
- Which env vars are auto-injected (`hostname`, `port`, `user`, `password`, `connectionString`, `dbName`)
- How HA works on Zerops specifically (streaming replication, immutable mode)
- Connection string patterns using `${hostname_var}` syntax
- Version-specific constraints (`valkey@7.2` only, not v8)
- Backup behavior, pgBouncer for external access, etc.

**Service cards must be rich enough** (~8-20 lines each, depending on service complexity) to cover this. The goal is NOT to minimize lines for services — it's to capture all genuinely Zerops-specific behavior while excluding generic database knowledge the LLM already has. PostgreSQL and Valkey need more lines (ports, replicas, pgBouncer, env vars) than simpler services like NATS or Qdrant.

---

## Target Architecture

```
┌───────────────────────────────────────────────────────────────┐
│  NEW: Core Knowledge (~350-400 lines, loaded for infra tasks)  │
│                                                                │
│  ┌──────────────────────────────────────────────────────┐     │
│  │ core-principles.md (~90 lines)                        │     │
│  │ Universal Zerops rules that apply to EVERYTHING:      │     │
│  │ - zerops.yml structure + mandatory fields              │     │
│  │ - import.yml structure + mandatory fields              │     │
│  │ - Build pipeline (prepare → build → deploy)            │     │
│  │ - Port rules, internal networking (http only)          │     │
│  │ - Env var system (${hostname_var}, underscores)        │     │
│  │ - deployFiles mandatory, tilde syntax                  │     │
│  │ - mode: NON_HA|HA mandatory for managed services       │     │
│  │ - #yamlPreprocessor=on, <@generateRandomString>        │     │
│  │ - zsc run/execOnce for init commands                   │     │
│  │ - ${zeropsSubdomain}, ${appVersionId} variables        │     │
│  │ - RUNTIME_ env prefix for base image vars              │     │
│  │ - os: choice (alpine/ubuntu) per runtime               │     │
│  │ - priority field (startup ordering)                    │     │
│  │ - enableSubdomainAccess in import.yml                  │     │
│  │ - Dev vs prod pattern (NON_HA→HA, SHARED→DEDICATED)    │     │
│  └──────────────────────────────────────────────────────┘     │
│  ┌──────────────────────────────────────────────────────┐     │
│  │ runtime-exceptions.md (~60-70 lines)                   │     │
│  │ Per-runtime: ONLY what deviates from default pattern: │     │
│  │ - PHP: build php@X ≠ run php-nginx@X, port 80, etc.  │     │
│  │ - Python: addToRunPrepare for pip, WSGI/ASGI          │     │
│  │ - Node.js: node_modules in deployFiles, SSR patterns  │     │
│  │ - Java: server.address=0.0.0.0, JAR packaging         │     │
│  │ - .NET: ASPNETCORE_URLS, publish output               │     │
│  │ - Rust: target/release binary location                │     │
│  │ - etc. (4-8 lines per runtime with exceptions)        │     │
│  └──────────────────────────────────────────────────────┘     │
│  ┌──────────────────────────────────────────────────────┐     │
│  │ service-cards.md (~150-180 lines)                      │     │
│  │ Per managed service: Zerops-specific behavior          │     │
│  │ that LLM CANNOT derive from general knowledge:        │     │
│  │ - Ports (primary, replicas, external TLS)             │     │
│  │ - Auto-injected env vars (exact names)                │     │
│  │ - Connection string template with ${hostname_var}     │     │
│  │ - HA behavior specifics (failover, replication type)  │     │
│  │ - Version constraints (valkey@7.2 only, etc.)         │     │
│  │ - Mode requirements (mandatory/optional, immutable)   │     │
│  │ - Service-specific gotchas                            │     │
│  │ (~8-20 lines per service, 13 services)                │     │
│  └──────────────────────────────────────────────────────┘     │
│  ┌──────────────────────────────────────────────────────┐     │
│  │ wiring-patterns.md (~30 lines)                        │     │
│  │ How to connect services:                              │     │
│  │ - ${hostname_var} reference pattern                   │     │
│  │ - Connection string templates per service             │     │
│  │ - envSecrets vs envVariables                          │     │
│  │ - import.yml wiring example                           │     │
│  └──────────────────────────────────────────────────────┘     │
├───────────────────────────────────────────────────────────────┤
│  NEW: Complex Recipes (on-demand, loaded only when needed)    │
│                                                                │
│  recipes/                                                      │
│  ├── laravel-jetstream.md  (multi-base, S3, Redis, Mailpit)  │
│  ├── ghost.md              (maxContainers:1, MariaDB, wsrep)  │
│  ├── django.md             (CSRF_TRUSTED_ORIGINS, wsgi)       │
│  ├── phoenix.md            (multi-step release, alpine)       │
│  ├── discord-bot.md        (no ports, background process)     │
│  ├── static-spa.md         (build nodejs, run static, tilde)  │
│  ├── medusa.md             (multi-service e-commerce)         │
│  ├── payload.md            (headless CMS, specific wiring)    │
│  └── ...                   (extracted from recipe-extracts)   │
│  NOTE: nextjs-ssr follows STANDARD pattern (not complex).     │
│  Recipe list finalized in Phase 1.4 based on actual analysis. │
├───────────────────────────────────────────────────────────────┤
│  KEPT: Reference docs (existing 66 files, BM25 searchable)   │
│                                                                │
│  knowledge/                                                    │
│  ├── services/  platform/  operations/  networking/  config/  │
│  └── (unchanged — fallback for ad-hoc queries)                │
└───────────────────────────────────────────────────────────────┘
```

### How It Flows

**Before (4-8 tool calls, 500+ lines of noise):**
```
zerops_workflow("bootstrap") → playbook says "search for each component"
zerops_knowledge("php zerops.yml") → BM25 results → read full 174-line doc
zerops_knowledge("postgresql import") → BM25 results → read full 82-line doc
zerops_knowledge("import patterns") → BM25 results → read full 243-line doc
...repeat for each service...
```

**After (1-2 tool calls, ~200 lines of high-signal content):**
```
zerops_workflow("bootstrap") → playbook
zerops_knowledge(runtime: "php-nginx@8.4", services: ["postgresql@16", "valkey@7.2"])
  → assembled briefing:
     - Core principles: zerops.yml/import.yml structure, port rules, env system (~90 lines)
     - PHP exceptions: build php@X ≠ run php-nginx@X, port 80, documentRoot (~8 lines)
     - PostgreSQL card: ports, env vars, conn string, HA, mode, gotchas (~15 lines)
     - Valkey card: ports, env vars, conn string, version constraint (~10 lines)
     - Wiring patterns: envSecrets example, ${hostname_var} system (~25 lines)
     - Filtered gotchas: networking + build + databases only (~15 lines)
  → ~165 lines of exactly what's needed, ALL Zerops-specific
  → (optional) if Laravel Jetstream detected → also loads recipe (~25 lines)
```

---

## Execution Plan

### Phase 1: Research & Extract (READ-ONLY)

**Goal**: Process all source materials, extract every Zerops-specific rule, exception, and gotcha. No code changes yet.

**Input**: zerops-docs (324 MDX), recipe-extracts (48 recipes), current knowledge/ (66 files), JSON schema

**Output**: A comprehensive extraction document (`plans/knowledge-extraction.md`) containing:

#### Task 1.1: Extract Core Zerops Rules from All Sources

Read through these specific sections of zerops-docs and extract EVERY rule that an LLM couldn't derive from general programming knowledge:

**Primary sources** (highest value — read these carefully):
- `zerops-docs/apps/docs/content/zerops-yaml/` — 3 files, zerops.yml spec
- `zerops-docs/apps/docs/content/references/` — 22 files, API/CLI/networking reference
- `zerops-docs/apps/docs/content/features/` — 11 files, platform features
- `knowledge/config/` — 5 files, existing config knowledge
- `knowledge/platform/` — 9 files, existing platform knowledge
- `knowledge/gotchas/common.md` — existing gotchas
- JSON schema — structural rules

**For each rule found, classify as**:
- **UNIVERSAL** — applies to all runtimes/services (→ core-principles.md)
- **RUNTIME-SPECIFIC** — applies to one runtime only (→ runtime-exceptions.md)
- **SERVICE-SPECIFIC** — applies to one managed service only (→ service-cards.md)
- **WIRING** — about connecting services (→ wiring-patterns.md)

#### Task 1.2: Extract Runtime Exceptions

For EACH runtime, compare zerops-docs + recipe-extracts + current knowledge and identify what's Zerops-specific:

| Runtime | zerops-docs source | recipe-extracts | knowledge/ |
|---------|-------------------|-----------------|------------|
| Node.js | `nodejs/` (15 MDX) | recipe-nodejs, recipe-nextjs-*, recipe-nestjs, recipe-nuxt-*, recipe-discord-nodejs, recipe-react-*, recipe-remix-*, recipe-analog-*, recipe-astro-*, recipe-svelte-*, recipe-solidjs-*, recipe-qwik-*, recipe-payload, recipe-ghost, recipe-medusa, recipe-vue, recipe-angular-static, recipe-bun | `services/nodejs.md` |
| PHP | `php/` (15 MDX) | recipe-php, recipe-laravel-*, recipe-symfony, recipe-nette*, recipe-filament, recipe-twill, recipe-ghost (MariaDB) | `services/php.md` |
| Python | `python/` (14 MDX) | recipe-python, recipe-django, recipe-discord-py | `services/python.md` |
| Go | `go/` (14 MDX) | recipe-go, recipe-echo | `services/go.md` (if exists) |
| Java | `java/` (14 MDX) | recipe-java, recipe-spring | `services/java.md` (if exists) |
| Rust | `rust/` (14 MDX) | recipe-rust | `services/rust.md` (if exists) |
| .NET | `dotnet/` (14 MDX) | recipe-dotnet | `services/dotnet.md` (if exists) |
| Elixir | `elixir/` (14 MDX) | recipe-elixir, recipe-phoenix | `services/elixir.md` (if exists) |
| Gleam | `gleam/` (14 MDX) | recipe-gleam | `services/gleam.md` (if exists) |
| Bun | `bun/` (14 MDX) | recipe-bun, recipe-discord-bun | `services/bun.md` (if exists) |
| Deno | `deno/` (14 MDX) | recipe-deno | `services/deno.md` (if exists) |
| Static | `static/` (1 MDX) | recipe-*-static (12 recipes) | `services/static.md` (if exists) |
| Docker | `docker/` (1 MDX) | — | `services/docker.md` (if exists) |
| Alpine/Ubuntu | `alpine/`, `ubuntu/` | — | existing files |

**For each runtime, extract ONLY**:
1. Build base vs run base differences (if any)
2. Default port (if non-standard)
3. deployFiles specifics (what MUST be included)
4. Required env vars (framework-specific bind address, etc.)
5. Cache paths for build optimization
6. Any other Zerops-specific deviation

**Target**: 2-5 lines per runtime. If a runtime has NO exceptions (follows default pattern exactly), note that — it means the core principles are sufficient.

#### Task 1.3: Extract Service Cards

For EACH managed service, extract connection-specific knowledge:

| Service | zerops-docs source | recipe usage | knowledge/ |
|---------|-------------------|-------------|------------|
| PostgreSQL | `postgresql/` (9 MDX) | 15+ recipes | `services/postgresql.md` |
| MariaDB | `mariadb/` (10 MDX) | recipe-ghost | `services/mariadb.md` |
| Valkey | `valkey/` (1 MDX) | — | `services/valkey.md` |
| KeyDB | `keydb/` (6 MDX) | recipe-echo, recipe-laravel-*, recipe-filament, etc. | `services/keydb.md` |
| Elasticsearch | `elasticsearch/` (1 MDX) | — | `services/elasticsearch.md` |
| Object Storage | `object-storage/` (6 MDX) | recipe-echo, recipe-laravel-jetstream, recipe-ghost, etc. | `services/object-storage.md` |
| Shared Storage | `shared-storage/` (7 MDX) | — | `services/shared-storage.md` |
| Kafka | `kafka/` (1 MDX) | — | `services/kafka.md` |
| NATS | `nats/` (1 MDX) | — | `services/nats.md` |
| Meilisearch | `meilisearch/` (1 MDX) | — | `services/meilisearch.md` |
| ClickHouse | `clickhouse/` (1 MDX) | — | `services/clickhouse.md` |
| Qdrant | `qdrant/` (1 MDX) | — | `services/qdrant.md` |
| Typesense | `typesense/` (1 MDX) | — | `services/typesense.md` |

**For each service, extract ALL Zerops-specific behavior** (the LLM knows general PostgreSQL/MariaDB/Redis but does NOT know how Zerops configures them):

1. **Ports**: primary, replicas, external TLS — exact numbers
2. **Auto-injected env vars**: exact variable names the service creates (hostname, port, user, password, connectionString, dbName, etc.)
3. **Connection string template**: using `${hostname_var}` pattern, ready for import.yml envSecrets
4. **HA behavior**: what HA means for this specific service (streaming replication? Galera cluster? read replicas?), failover behavior
5. **Mode requirements**: is mode mandatory? Is it immutable after creation?
6. **Version constraints**: which versions actually work, known broken versions
7. **import.yml snippet**: minimal working service definition
8. **Service-specific gotchas**: protocol quirks, backup behavior, external access, admin accounts

**Target**: ~8-20 lines per service (complex services like PostgreSQL need more, simpler like NATS need less). Services are genuinely Zerops-specific — don't over-minimize. The goal is: if the LLM has the service card, it can correctly define and wire the service in import.yml without any other lookup.

**Approach for zerops-docs**: Each managed service has overview + create + tech pages. Focus on overview and any "tech details" pages. Skip generic "how to scale/logs/controls" pages (identical across services). Cross-reference with `knowledge/services/` and `knowledge/examples/connection-strings.md` which already distill much of this.

#### Task 1.4: Extract Complex Recipe Patterns

Read ALL 48 recipe-extracts. For each, determine:
- **STANDARD** — follows core principles exactly (no recipe file needed)
- **COMPLEX** — requires specific knowledge beyond core principles (needs recipe file)

**Explicit classification criteria:**

A recipe is **STANDARD** if:
- Single runtime, single service (or no service)
- Standard build command (npm build, go build, etc.)
- No framework-specific gotchas beyond what runtime-exceptions covers
- Example: Next.js SSR (standard Node.js pattern with `.next` + `node_modules` deploy — covered by runtime-exceptions)

A recipe is **COMPLEX** if it involves ANY of:
- Multi-base build (e.g., `[php@8.4, nodejs@18]`)
- Non-obvious framework configuration (Ghost maxContainers:1, Django CSRF, Spring bind)
- Multiple interacting services with specific wiring beyond basic DB connection
- Unusual deploy patterns (e.g., Elixir release to Alpine)
- Framework-specific init/prepare commands (artisan, manage.py migrate)
- Service-specific constraints that override defaults (wsrep for MariaDB HA with Ghost)

**For each COMPLEX recipe, capture**:
- Complete zerops.yml (from recipe-extracts)
- Complete import.yml relevant sections
- The non-obvious parts that make it complex
- Framework-specific gotchas

### Phase 2: Author Core Documents

**Goal**: Write the new knowledge files based on Phase 1 extraction.

**Output**: New files in `knowledge/core/` directory:

#### Task 2.1: Write core-principles.md

**Requirements**:
- ~90 lines target (increased from 80 to accommodate yamlPreprocessor, zsc, dev-vs-prod patterns)
- ONLY universal rules (apply to all runtimes and services)
- Organized by decision point (what LLM needs to know WHEN generating YAML)
- Every statement must be something an LLM could NOT derive from general knowledge
- Include the zerops.yml and import.yml structural skeleton
- Validate against JSON schema — no rules that contradict it

**Structure** (suggested, adapt based on Phase 1 findings):
```markdown
# Zerops Core Principles

## zerops.yml Structure
<skeleton + mandatory field rules, os: choice (alpine/ubuntu)>

## import.yml Structure
<skeleton + mandatory fields, #yamlPreprocessor=on, <@generateRandomString>,
 priority field, enableSubdomainAccess, mode: NON_HA|HA mandatory>

## Build Pipeline
<prepare → build → deploy, caching behavior, zsc run/execOnce for init>

## Port Rules
<range 10-65435, httpSupport vs protocol, reserved ports, TRUSTED_PROXIES>

## Internal Networking
<http only, hostname = DNS, L7 balancer>

## Environment Variables
<${hostname_var} with underscores, scopes, isolation, cross-phase,
 ${zeropsSubdomain}, ${appVersionId}, RUNTIME_ prefix>

## Deploy Patterns
<deployFiles mandatory, tilde syntax, build→run file transfer>

## Dev vs Production
<NON_HA→HA transition (immutable, must recreate), SHARED→DEDICATED cpu,
 startWithoutCode for dev containers, Mailpit/Adminer for dev>

## Critical Gotchas
<top 10-12 universal gotchas that catch everyone>
```

**Target ~90 lines** (increased from 80 due to additional patterns discovered in simulation).

**IMPORTANT**: Core docs must NOT reference workflows. No "call zerops_workflow(...)" or "next run zerops_knowledge(...)". Core docs are pure knowledge — workflow orchestration belongs in workflow playbooks only.

#### Task 2.2: Write runtime-exceptions.md

**Requirements**:
- ~60-70 lines (Node.js alone needs 6-10 lines: node_modules deploy, SSR patterns, framework-specific start commands, .output for Nuxt, .next for Next.js, etc.)
- One section per runtime, ONLY deviations from default pattern
- If a runtime follows core principles exactly, say so in one line: "Go: follows default pattern (compiled binary)"
- Format: bullet list of exceptions, not prose
- **Runtime name normalization**: Document how MCP input names map to sections (e.g., `php-nginx@8.4` → PHP section, `php-apache@8.3` → PHP section, `nodejs@22` → Node.js). The assembly code must handle this (see Phase 3)

#### Task 2.3: Write service-cards.md

**Requirements**:
- ~150-180 lines total (~8-20 lines per service, 13 services; PostgreSQL/Valkey/MariaDB need more, NATS/Qdrant need less)
- One H2 section per managed service
- Each card MUST contain: ports, auto-injected env var names, connection string template, HA specifics, mode requirement, version constraints, import.yml snippet, critical gotchas
- These ARE genuinely Zerops-specific — the LLM knows PostgreSQL in general but not Zerops's port layout, env var naming, or HA implementation
- A complete service card means: with ONLY this card + core-principles + wiring-patterns, the LLM can correctly define the service in import.yml AND wire it to any runtime

#### Task 2.4: Write wiring-patterns.md

**Requirements**:
- ~30 lines maximum
- The ${hostname_var} system explained concisely
- envSecrets vs envVariables decision rule
- One concrete import.yml wiring example (app + db + cache)
- Cross-service reference gotchas

#### Task 2.5: Write complex recipes

**Requirements**:
- One .md file per complex recipe identified in Phase 1
- Location: `knowledge/core/recipes/`
- 15-30 lines each: complete zerops.yml + import.yml snippet + non-obvious gotchas
- MUST include the actual YAML (copy-paste ready), not just descriptions

### Phase 3: Implement Contextual Assembly

**Goal**: Modify the ZCP Go code to support contextual knowledge retrieval alongside existing BM25 search.

#### Task 3.1: Extend Knowledge Store (concrete *Store struct, NOT the Provider interface)

**CRITICAL**: The codebase has a `Provider` **interface** (engine.go:30-36) and a `*Store` **concrete struct** (engine.go:39). The Provider interface has 4 methods: `List()`, `Get()`, `Search()`, `GenerateSuggestions()`. New methods go on the **concrete `*Store` struct** — do NOT modify the Provider interface, which serves as a minimal contract for tools.

Add methods to the concrete Store:

```go
// New methods on *Store (NOT on Provider interface)
func (s *Store) GetCorePrinciples() string { ... }
func (s *Store) GetBriefing(runtime string, services []string) string { ... }
func (s *Store) GetRecipe(name string) (string, error) { ... }
func (s *Store) ListRecipes() []string { ... }
```

The tools layer (`internal/tools/knowledge.go`) already receives `*knowledge.Store` directly — no interface change needed.

**Briefing assembly logic** (in `GetBriefing`):
1. Always include: core-principles.md content
2. If runtime provided: include that runtime's section from runtime-exceptions.md
3. For each service: include that service's section from service-cards.md
4. If any services provided: include wiring-patterns.md
5. Filter relevant gotchas based on runtime + services

**Important**: Parse core documents at startup (like existing documents), not on every call. The section extraction should be pre-indexed by runtime name and service name.

**H2 Section Parser Requirements**:
- Split markdown by `## ` headers to extract per-runtime and per-service sections
- **CRITICAL**: The parser MUST be fenced-code-block-aware. YAML code blocks inside sections will contain `##` characters (e.g., YAML comments). A naive `strings.Split(content, "\n## ")` will break on these. Parse state: track ``` fences and only split on H2 headers that are NOT inside code blocks.

**Runtime Name Normalization**:
- MCP receives names like `php-nginx@8.4`, `php-apache@8.3`, `nodejs@22`, `bun@1.2`
- Must normalize to section names: `php-nginx@8.4` → "PHP", `php-apache@8.3` → "PHP", `nodejs@22` → "Node.js"
- Implement a normalizer map (strip version, map type prefixes to canonical section names)
- Handle unknown runtimes gracefully (return core principles only, no error)

#### Task 3.2: Extend zerops_knowledge Tool

Add new input parameters to the existing tool:

```go
type KnowledgeInput struct {
    // Existing (BM25 search) — NOTE: Query does NOT have omitempty in current code
    Query string `json:"query"`
    Limit int    `json:"limit,omitempty"`

    // New (contextual assembly)
    Runtime  string   `json:"runtime,omitempty"`   // e.g., "php-nginx@8.4"
    Services []string `json:"services,omitempty"`  // e.g., ["postgresql@16", "valkey@7.2"]
    Recipe   string   `json:"recipe,omitempty"`    // e.g., "laravel-jetstream"
}
```

**NOTE**: The existing `Query` field has NO `omitempty` tag. Adding it would change the MCP tool JSON schema (removing Query from `required` fields). This is intentional — we need Query to become optional when Runtime/Services are provided. Verify this schema change is backward-compatible with Claude's MCP tool calling.

**Behavior**:
- If `query` provided → existing BM25 search (backward compatible)
- If `runtime` or `services` provided → contextual briefing from core docs
- If `recipe` provided → specific complex recipe
- If nothing provided → return list of available recipes + hint to use runtime/services params

#### Task 3.3: Embed Core Documents

Add new embed directory alongside existing:
```
internal/knowledge/
├── embed/          (existing 66 reference docs)
├── core/           (NEW: core principles + exceptions + recipes)
│   ├── core-principles.md
│   ├── runtime-exceptions.md
│   ├── service-cards.md
│   ├── wiring-patterns.md
│   └── recipes/
│       ├── laravel-jetstream.md
│       ├── ghost.md
│       └── ...
└── engine.go, documents.go, query.go
```

Extend `documents.go` to load core docs separately from reference docs. Core docs are parsed into structured sections (by H2 headers) for assembly.

#### Task 3.4: Update Tests

**TDD approach — write tests FIRST:**

1. `TestGetBriefing_NodejsPostgresql` — verify briefing contains Node.js exceptions + PostgreSQL card + wiring
2. `TestGetBriefing_PhpPostgresqlValkey` — verify PHP exceptions + both service cards
3. `TestGetBriefing_GoOnly` — verify Go follows default (no exceptions section)
4. `TestGetBriefing_RuntimeOnly` — no services → no wiring section
5. `TestGetBriefing_UnknownRuntime` — graceful fallback to core principles only
6. `TestGetRecipe_LaravelJetstream` — verify recipe content returned
7. `TestGetRecipe_NotFound` — error handling
8. `TestListRecipes` — all recipes listed
9. `TestKnowledgeTool_BackwardCompatible` — query param still works via BM25
10. `TestKnowledgeTool_ContextualMode` — runtime/services params work
11. `TestRuntimeNormalization_PhpNginx` — php-nginx@8.4 maps to PHP section
12. `TestRuntimeNormalization_PhpApache` — php-apache@8.3 maps to PHP section
13. `TestRuntimeNormalization_Unknown` — unknown runtime returns core only, no error
14. `TestH2SectionParser_CodeBlockSafe` — YAML with ## inside code blocks doesn't split
15. `TestH2SectionParser_BasicSplit` — normal H2 headers split correctly
16. `TestContextOutput_SlimmedRules` — context.go no longer contains full rules (if Option B-lite applied)
17. `TestGetBriefing_ServiceOnly` — services without runtime → core + cards + wiring, no runtime section
18. `TestGetBriefing_EmptyInput` — no runtime, no services → returns available recipes hint

### Phase 4: Update Workflows

**Goal**: Update workflow playbooks to use contextual assembly instead of BM25 search instructions.

#### Task 4.1: Update bootstrap.md

**Current Step 3** (fragile):
```markdown
Load knowledge — call zerops_knowledge for each component:
- zerops_knowledge query="{runtime} {framework} zerops.yml"
- zerops_knowledge query="{db} import connection"
Then read each document via read_resource.
```

**New Step 3** (deterministic):
```markdown
Load knowledge — call zerops_knowledge with the identified stack:
  zerops_knowledge(runtime: "{identified-runtime}", services: ["{svc1}", "{svc2}"])
This returns a complete implementation guide with rules, exceptions, and wiring patterns.
If you recognize a complex framework (Laravel Jetstream, Ghost, WordPress, etc.):
  zerops_knowledge(recipe: "{framework-name}")
```

#### Task 4.2: Update deploy.md

Same pattern — replace multi-search with contextual call. The conditional logic stays (skip if zerops.yml exists).

#### Task 4.3: Update debug.md

The 7 common issues in debug.md are already embedded. Most of them come from core principles. Validate that all 7 are covered by core-principles.md. If the LLM has core principles in context (from a prior bootstrap/deploy flow), it may not even need to call knowledge during debug.

For cases where debug is the FIRST operation (no prior context), add:
```markdown
If core principles not yet loaded:
  zerops_knowledge(runtime: "{service-runtime}", services: [])
```

#### Task 4.3b: Review ALL 6 Workflows

All 6 workflow files must be reviewed, not just bootstrap/deploy/debug:
- `bootstrap.md` — primary consumer, update to contextual mode (Task 4.1)
- `deploy.md` — update to contextual mode (Task 4.2)
- `debug.md` — validate coverage (Task 4.3)
- `configure.md` — check for knowledge references, update if needed
- `scale.md` — **DOES reference zerops_knowledge** (contrary to earlier assumption), update to contextual mode
- `monitor.md` — check for knowledge references, update if needed

#### Task 4.4: Address context.go Overlap with Core Principles

**CRITICAL**: `internal/ops/context.go` contains ~260 lines of static context returned by `zerops_context`. Much of this overlaps with proposed core-principles.md content (port rules, env system, networking). If both exist with different wording, the LLM gets contradictory signals.

**Option A** (keep both, accept duplication) — **NOT RECOMMENDED**: Creates a "time bomb" where future updates to one but not the other cause contradictions. The LLM may get context.go content AND core-principles in the same session.

**Option B-lite** (slim context.go, no breaking change) — **RECOMMENDED**:
1. Keep context.go structure and tool contract unchanged
2. Replace the "Critical Rules" section in context.go (~100 lines) with a compact summary + pointer: "Full rules available via zerops_knowledge(runtime: ..., services: [...])"
3. Keep platform overview, service types, and tool catalog in context.go (this is genuinely context, not rules)
4. Result: context.go shrinks from ~260 to ~160 lines, core-principles.md is the single source of truth for rules

This is a behavioral change — write tests first (verify context output format, verify the compact summary is present, verify full rules are NOT duplicated).

### Phase 5: Validate & Sync

#### Task 5.1: Run All Tests
```bash
go test ./... -count=1 -short
```
All existing tests MUST pass. New tests MUST pass.

#### Task 5.2: Sync Knowledge Embed
```bash
# Ensure core/ directory is synced to embed
cp -r knowledge/core/ zcp/internal/knowledge/core/
```

Update `sync-knowledge.sh` if needed to handle the new `core/` subdirectory.

#### Task 5.3: Integration Validation

Test the full flow manually:
1. Build ZCP: `go build -o bin/zcp ./cmd/zcp`
2. Call zerops_knowledge with runtime + services → verify briefing quality
3. Call zerops_knowledge with query → verify BM25 still works
4. Call zerops_knowledge with recipe → verify recipe returned
5. Call zerops_workflow("bootstrap") → verify updated playbook references contextual mode

#### Task 5.4: Lint
```bash
make lint-fast
```

---

## Team Structure Recommendation

### Option A: Sequential (safer, 2 agents)

```
Agent 1 (Researcher): Phase 1 → produces extraction document
Agent 2 (Implementer): Phase 2-5 → uses extraction to write docs + code
```

Pro: Clean handoff, no conflicts. Con: Slower.

### Option B: Parallel (faster, 3-4 agents)

```
Agent 1 (Research-Docs): Task 1.1 + 1.3 — process zerops-docs + current KB
Agent 2 (Research-Recipes): Task 1.2 + 1.4 — process recipe-extracts
Agent 3 (Author): Phase 2 — write core docs (blocked by Phase 1)
Agent 4 (Engineer): Phase 3 — implement Go code (can start with interface design while Phase 1 runs)
Lead: Phase 4-5 — workflow updates + validation (blocked by Phase 2-3)
```

Pro: Faster. Con: Needs careful coordination.

### Recommended: Option B with dependencies

```
Phase 1 (parallel):
  Agent 1: Research zerops-docs + KB → extraction-docs.md
  Agent 2: Research recipe-extracts → extraction-recipes.md

Phase 2 (parallel, after Phase 1):
  Agent 3: Write core docs (core-principles, exceptions, cards, wiring, recipes)
  Agent 4: Design + implement Go code (store interface, tool handler, tests)

Phase 3 (lead):
  Update workflows + validate + sync
```

---

## Acceptance Criteria

1. **Core principles cover 90% of use cases**: A standard Node.js + PostgreSQL bootstrap should require ZERO additional knowledge lookups beyond the core briefing
2. **Backward compatible**: `zerops_knowledge(query: "...")` still works via BM25
3. **All existing tests pass**: `go test ./... -count=1 -short` green
4. **New tests pass**: At least 18 new tests for contextual assembly (including parser, normalizer, and tool-level tests)
5. **Briefing size**: Contextual briefing for any runtime + 2 services < 350 lines (larger than original ~300 target due to richer service cards and runtime exceptions)
6. **Complex recipes exist**: At least 6-8 recipe files for identified complex cases (exact count determined by Phase 1.4 analysis)
7. **Workflows updated**: ALL 6 workflows reviewed; bootstrap.md, deploy.md, scale.md updated to reference contextual mode
8. **No information loss**: Existing 66 reference docs remain available via BM25
9. **No contradictions**: core-principles.md and context.go do NOT contain conflicting statements about the same rules
10. **H2 parser is code-block-safe**: Markdown sections are correctly split even when YAML code blocks contain `##` characters
11. **Runtime normalization works**: All Zerops runtime types (php-nginx, php-apache, nodejs, bun, etc.) correctly map to their section names

---

## Non-Goals (explicitly out of scope)

- Deleting or restructuring existing 66 knowledge/ files
- Changing the BM25 engine (Bleve) — it stays as-is
- Changing MCP server instructions (internal/server/instructions.go)
- Adding integration or E2E tests (unit + tool tests only for this phase)
- Optimizing BM25 ranking or query aliases

## Changed from Original Non-Goals

- **zerops_context IS in scope**: context.go must be slimmed to avoid contradicting core-principles.md (Task 4.4, Option B-lite)
- **All 6 workflows in scope**: not just bootstrap/deploy/debug — scale.md also references knowledge

---

## Appendix: Simulation Findings (v2 update)

This plan was validated by 4 simulation agents (Research-Docs, Research-Recipes, Go Engineer, Workflow Integrator). Below are the findings that were incorporated into this version:

### CRITICAL (plan would fail without fix)
1. **Provider vs Store interface** — plan incorrectly proposed extending `Store interface`; actual code has `Provider` interface (minimal contract) + `*Store` struct (concrete). New methods go on `*Store`, not the interface. → Fixed in Task 3.1
2. **Query field omitempty** — plan assumed `Query` has `omitempty`; it does not. Adding it changes MCP tool JSON schema. → Fixed in Task 3.2 with migration note
3. **H2 section parser** — naive string split on `## ` breaks on YAML code blocks containing `##`. Must be fenced-code-block-aware. → Fixed in Task 3.1 parser requirements
4. **context.go contradiction risk** — Option A ("keep both") creates divergence time bomb. → Replaced with Option B-lite in Task 4.4
5. **data.json is only version source** — zerops-docs/static/data.json is the ONLY canonical source of service versions. Plan missed it. → Added to Source Materials

### HIGH (significant quality/accuracy impact)
6. **13 shared MDX partials** — zerops-docs uses shared/ components for generic behavior. Understanding these is key to separating generic from specific. → Added to Source Materials
7. **Runtime name normalization** — non-trivial mapping (php-nginx→PHP, php-apache→PHP). → Added requirements in Task 3.1
8. **Runtime-exceptions too small** — 40 lines unrealistic; Node.js alone needs 6-10. → Raised to 60-70 lines
9. **Service-cards too small** — 8-12 lines per service unrealistic for PostgreSQL/Valkey. → Changed to 8-20 lines per service
10. **Recipe list inaccurate** — nextjs-ssr is STANDARD (not complex); wordpress/monorepo have no recipe-extract source material. → Fixed recipe list
11. **scale.md references knowledge** — plan incorrectly marked as "unchanged". → Added to Task 4.3b
12. **Missing Zerops patterns** — zsc execOnce, yamlPreprocessor, zeropsSubdomain, appVersionId, RUNTIME_ prefix, priority, os:, TRUSTED_PROXIES, dev-vs-prod. → Added to core-principles scope
13. **Extraction order** — should start from existing KB (already distilled), then validate against zerops-docs. → Fixed extraction strategy
14. **STANDARD vs COMPLEX criteria** — plan lacked explicit classification rules. → Added explicit criteria in Task 1.4
15. **Core docs must not reference workflows** — separation of concerns. → Added content rule in Task 2.1
