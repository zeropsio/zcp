# Recipe Creation System — Summary

How ZCP creates complete Zerops recipes end-to-end.

---

## The Problem

Creating a Zerops recipe today requires producing **17 files across 2 repos** (app code + zerops.yaml + 13 recipe files + Strapi entry), deploying and verifying the app, and writing quality documentation. The existing meta-prompt approach burns 2200 lines of context trying to encode everything a standalone LLM needs. ZCP already has 80% of that knowledge.

## The Solution

A **recipe workflow** inside ZCP — 6 steps that guide an LLM through the entire process, injecting only what it needs at each step.

## The Pipeline

```
    ┌──────────┐      ┌───────────┐      ┌──────────┐
    │ RESEARCH │─────▶│ PROVISION │─────▶│ GENERATE │
    │          │      │           │      │          │
    │ LLM fills│      │ ZCP creates│     │ LLM writes│
    │ framework│      │ workspace  │     │ app code, │
    │ research │      │ services   │     │ zerops.yaml│
    │ form     │      │ from plan  │     │ README    │
    └──────────┘      └───────────┘     └────┬─────┘
                                              │
    ┌──────────┐      ┌───────────┐      ┌───▼──────┐
    │  CLOSE   │◀─────│ FINALIZE  │◀─────│  DEPLOY  │
    │          │      │           │      │          │
    │ Present  │      │ ZCP generates│   │ Bootstrap │
    │ publish  │      │ recipe files,│   │ deploys + │
    │ commands │      │ LLM adds    │   │ verifies  │
    └──────────┘      │ comments    │   │ health    │
                      └───────────┘    └──────────┘
```

## Step by Step

**RESEARCH** — The LLM loads the runtime's existing hello-world recipe via `zerops_knowledge` (proven baseline config). It loads the runtime briefing (platform rules, wiring syntax). It fills in a framework research form: build commands, migration system, libraries, port, OS choice, cache strategy. For showcase tier, also: cache/queue/storage/search/mail libraries. Output: a `RecipePlan` with all decisions resolved.

**PROVISION** — Reuses bootstrap. ZCP generates a workspace import YAML from the plan and creates: `appdev` (dev), `appstage` (prod), `db`, and showcase services if type 4. Hard-checks: all services RUNNING, env vars discovered.

**GENERATE** — The creative step. The LLM writes to the SSHFS mount:
- **App code**: framework project with health dashboard at `/`, JSON health endpoint at `/api/health`, migrations using the framework's own system
- **`zerops.yaml`**: base + prod + dev setups (+ worker for showcase), using `extends: base` for shared env vars. Comments explain *why*, not *what*. Links to Zerops docs.
- **App `README.md`** with three fragments:
  - `intro`: 1-3 lines naming framework + services + purpose
  - `integration-guide`: the complete commented zerops.yaml + framework-specific adaptation steps (S3 library install, mailer config, etc.)
  - `knowledge-base`: platform gotchas, base image contents, operational tips (maintenance mode, `zsc scale`, multi-container patterns)

ZCP injects: YAML schemas, runtime briefing, discovered env vars, existing recipes as style reference. Hard-checks: zerops.yaml valid, setup entries exist, env var refs correct.

**DEPLOY** — Reuses bootstrap. Deploys dev setup to `appdev`, prod setup to `appstage`. Enables subdomains. Verifies health endpoints return 200 with real service connections. Iteration loop (up to 10 retries with escalating diagnostics) if anything fails.

**FINALIZE** — Mostly code generation:

| What | Who | How |
|------|-----|-----|
| Recipe `README.md` | ZCP | Template substitution (8 placeholders) |
| 6 env `README.md` files | ZCP | Fixed boilerplate per tier (only name/slug varies) |
| 6 env `import.yaml` structures | ZCP | RecipePlan + env scaling matrix (NON_HA→HA, minContainers, cpuMode, corePackage) |
| 6 env `import.yaml` comments | LLM | Reviews generated YAML, adds explanatory comments |
| `recipe-output.yaml` | ZCP | Strapi registration fields |

Hard-checks: fragment format valid, project naming correct, YAML valid, primitives coverage, `verticalAutoscaling` nesting, comment width ≤80 chars.

**CLOSE** — Presents publish commands: `zcp sync push recipes {slug}`, git push to app repo, Strapi registration.

## What the LLM's Prompt Looks Like

Instead of 2200 lines up front:

```
Create a NestJS minimal recipe for Zerops.
ZCP will guide you through research, provisioning, code generation, deployment, and recipe file creation.
```

~50 lines. ZCP handles everything else via step-specific guidance injection.

## Automation Layer

Once the workflow exists, headless creation is one command:

```bash
zcp eval create --framework nestjs --tier minimal
zcp eval create --framework django --tier showcase
zcp eval create-suite --tier minimal --frameworks nestjs,django,rails,spring-boot,phoenix,express,fastapi
```

Spawns Claude CLI → Claude calls `zerops_workflow action="start" workflow="recipe"` → ZCP guides it through all 6 steps → recipe files extracted and validated → `zcp sync push` publishes.

## Extensibility

The workflow handles all 7 taxonomy types through `RecipePlan` configuration:

| Type | Template | Envs | Key difference |
|------|----------|------|----------------|
| 1. Runtime Hello World | `_template` | 6 | Single app + DB, raw HTTP server |
| 2a. Frontend Static | `_template` | 6 | `type: static` for prod, `nodejs` for dev, no DB |
| 2b. Frontend SSR | `_template` | 6 | Framework SSR build output |
| 3. Backend Framework Minimal | `_template` | 6 | Framework + DB, dashboard + migration |
| 4. Backend Framework Showcase | `_template` | 6 | + Valkey, S3, Meilisearch, Mailpit, worker service |
| 5. Starter Kit | `_template` | 6 | Real project scaffold, HA-readiness baseline |
| 6. CMS/E-commerce OSS | `_template_oss` | 2 | Scaffolding command, maintenance-guide fragment |
| 7. Software OSS | `_template_oss` | 2 | No app code, just zerops.yaml + upstream git clone |

Same 6 workflow steps. Only the plan content and injected guidance change.

## Where Knowledge Lives

```
                          ┌─────────────────────┐
                          │   LLM Training Data  │
                          │  (framework expertise)│
                          └──────────┬───────────┘
                                     │
┌────────────────┐    ┌──────────────▼──────────────┐    ┌──────────────────┐
│ Knowledge Store│    │       Recipe Workflow         │    │   Live Platform   │
│                │───▶│                               │◀───│                   │
│ 33 recipes     │    │ RESEARCH: load baseline recipe│    │ Service catalog   │
│ Runtime briefs │    │          + briefing + schemas │    │ YAML JSON schemas │
│ Service cards  │    │ GENERATE: inject knowledge    │    │ Env var discovery │
│ Wiring syntax  │    │ FINALIZE: validate against    │    │ Health checks     │
│ Platform rules │    │           platform rules      │    │                   │
└────────────────┘    └──────────────────────────────┘    └──────────────────┘
```

No single 2200-line document. Knowledge flows in from three sources — each step gets only what it needs.

## What Makes This Work

1. **Deploy before document** — The LLM writes integration-guide and knowledge-base AFTER deploying. It documents what actually worked, not what it hopes will work.

2. **Progressive disclosure** — ~100-150 lines of guidance per step instead of 2200 up front. No context burn.

3. **Hard verification at every step** — Not checklists in a prompt. Real code that validates YAML structure, fragment format, naming conventions, env var refs, health endpoints.

4. **Formulaic files generated by code** — 11 of 17 files are template substitution. The LLM focuses on the 6 files that need framework judgment.

5. **Existing infrastructure** — Bootstrap workflow, knowledge store, sync system, eval runner. We're composing proven pieces, not building from scratch.
