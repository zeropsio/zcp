# Recipe Knowledge System

ZCP's `internal/knowledge/recipes/` contains operational knowledge for each Zerops recipe. This knowledge is **synced from canonical external sources** — ZCP is a consumer, not the owner.

## How It Works

```
App README (canonical)          Recipe API (Strapi)           ZCP (consumer)
─────────────────────          ──────────────────           ──────────────
knowledge-base fragment  →     extracts.knowledge-base  →   recipes/{slug}.md
intro fragment           →     extracts.intro           →   frontmatter description
zerops.yaml              →     services[].zeropsYaml    →   ## zerops.yml section
```

**Pull** (`scripts/sync-knowledge.sh pull recipes`): one API call to `api.zerops.io` fetches all non-utility recipes dynamically. No hardcoded list — new recipes appear automatically.

**Push** (`scripts/sync-knowledge.sh push recipes`): writes knowledge-base content back to local app repo clones (tries `{slug}-app/` then `{slug}/`). You review, commit, push to GitHub, then refresh the Strapi cache.

All synced files (`recipes/`, `guides/`, `decisions/`) are **gitignored** — run `scripts/sync-knowledge.sh pull` before build. Infrastructure bases (`bases/`) are committed.

## Recipe File Format

Each recipe in `internal/knowledge/recipes/` follows this structure:

```markdown
---
description: "From the intro fragment — what this recipe is."
---

# Bun Hello World on Zerops

## Base Image
What Zerops ships in this runtime's base image (and what's missing).

## Binding
Why 0.0.0.0 is required, per-framework patterns.

## Resource Requirements
Zerops autoscaling context + minRam sizing.

## Gotchas
Real failures on Zerops — platform-specific, not general runtime docs.

## zerops.yml
> Reference implementation — learn the patterns, adapt to your project.

(full commented YAML with both prod and dev setups)
```

**Every section must be Zerops-specific.** Don't restate general runtime documentation. The knowledge-base answers: "what's different about deploying {runtime} on Zerops vs anywhere else?"

Most recipes currently only have `description` + `## zerops.yml` (from API). The sections above (Base Image through Gotchas) come from the `knowledge-base` fragment in the app README — add it there, refresh Strapi cache, then `pull` to see it in ZCP.

## Knowledge-Base Content Guidelines

Each item should be one of:
- **What's in the Zerops base image** — not documented by the runtime itself
- **L7 balancer binding** — per-framework 0.0.0.0 patterns, Zerops-specific
- **Autoscaling behavior** — minRam must absorb startup spike (~10s reaction time)
- **Platform gotchas** — tilde extraction, build≠run containers, cache path rules
- **Real support ticket / agent failure patterns** — things that actually break

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
- `sourceData.extracts.intro` — app description
- `sourceData.environments[].services[].extracts["knowledge-base"]` — operational knowledge
- `sourceData.environments[].services[].zeropsYaml` — full zerops.yaml

### Cache Behavior

After pushing changes to GitHub, the Strapi cache must be refreshed:
1. Go to [Strapi admin](https://api-d89-1337.prg1.zerops.app/admin/content-manager/collection-types/api::recipe.recipe) → recipe detail → "Refresh Cache"
2. Or wait for automatic cache refresh

### 6 Lifecycle Environments

Each language/framework recipe offers:

| # | Environment | Purpose |
|---|-------------|---------|
| 0 | AI Agent | Development space for AI agents |
| 1 | Remote (CDE) | Cloud Development Environment via SSH |
| 2 | Local | Local dev with zCLI VPN for DB access |
| 3 | Stage | Production config, single container |
| 4 | Small Production | Production-ready, moderate throughput |
| 5 | Highly-available Production | Enhanced scaling, dedicated resources, HA |

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

## Current State

- **33 recipes** pulled dynamically from Recipe API (all non-utility recipes)
- **All recipe .md files are gitignored** — run `scripts/sync-knowledge.sh pull recipes` before build
- **Infrastructure bases** (alpine, docker, nginx, static, ubuntu) are in `internal/knowledge/bases/` (committed)
- **Bun** is the only recipe with `knowledge-base` fragment — others have intro + zerops.yml only
- **elixir** is missing from API; **nodejs** has slug `recipe` (remapped to `nodejs-hello-world` by sync script)
