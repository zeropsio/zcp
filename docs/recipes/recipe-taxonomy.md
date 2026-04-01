# Recipe Taxonomy

7 recipe types, from simplest to most complex. Each type has a specific purpose, prompt architecture, and set of services.

---

## 1. Runtime Hello World [DONE]

**What it is**: Simplest possible app for a language/runtime (Node.js, Go, Python, Rust, PHP, Bun, Deno, etc.) running on Zerops with PostgreSQL. No framework — just the language's standard HTTP library + a DB driver.

**What the app does**: Single `GET /` endpoint returning JSON with DB status and a greeting queried from a migrated `greetings` table.

**What it proves**: "This language runs on Zerops, connects to a database, and migrations work."

**Services**: App + PostgreSQL.

**Prompt**: Single prompt covers all languages (`zrecipator-runtime-hello-world.md`).

---

## 2a. Frontend Static Hello World [DONE]

**What it is**: A static/SPA frontend framework (React Vite, Vue Vite, Svelte Vite, etc.) compiled to HTML/CSS/JS and served by Zerops' built-in Nginx (`run.base: static`).

**What the app does**: Simple page showing framework name, greeting, timestamp, and environment indicator. Demonstrates build-time environment variable injection.

**What it proves**: "This frontend framework builds on Zerops and serves via Nginx." Key: `build.base: nodejs@22` for compilation, `run.base: static` for serving — no Node.js at runtime.

**Services**: App only (no database).

**Prompt**: Single prompt covers all static frameworks (`zrecipator-fe-static-hello-world.md`).

---

## 2b. Frontend SSR Hello World [DONE]

**What it is**: An SSR/fullstack frontend framework (Next.js, Nuxt, SvelteKit, React Router v7, Astro, etc.) running as a Node.js process with PostgreSQL.

**What the app does**: Same as runtime hello world — `GET /` with JSON health check, DB connection, migrated greeting — using the framework's SSR and API route mechanisms.

**What it proves**: "This SSR framework runs on Zerops with a database." Key: self-contained vs node_modules split, framework-specific build output, Nitro bundling.

**Services**: App + PostgreSQL.

**Prompt**: Single prompt covers all SSR frameworks (`zrecipator-fe-ssr-hello-world.md`).

---

## 3. Backend Framework Minimal [IN PROGRESS]

**What it is**: A backend framework (Laravel, NestJS, Django, Rails, Spring Boot, Phoenix, etc.) with PostgreSQL. Uses the framework's own project structure, template engine, migration system.

**What the app does**: HTML dashboard at `GET /` showing DB connection status and greeting. JSON health check at `GET /api/health`.

**What it proves**: "This framework runs on Zerops with its native tooling." Uses Eloquent/TypeORM/Django ORM, Blade/Jinja/EJS templates, Artisan/manage.py/nest CLI.

**Services**: App + PostgreSQL.

**How it differs from runtime hello world**: Framework migrations (not raw SQL), template-rendered dashboard (not JSON-only), `extends: base` pattern, may need multi-base builds (PHP + Node.js), `envSecrets` for app keys.

**Prompt**: Meta-prompt generates per-framework prompts (`zrecipator-be-framework-meta.md` → `zrecipator-{framework}-minimal.md`).

---

## 4. Backend Framework Showcase [IN PROGRESS]

**What it is**: Same framework as minimal, wired to the full Zerops service stack: PostgreSQL + Valkey + S3 Object Storage + Mailpit + Meilisearch. Includes a queue worker as a separate service.

**What the app does**: Dashboard at `GET /` showing status of ALL services — cache read/write demo, queue job dispatch + worker processing, file upload to S3, search index query, SMTP transport status. **Proof of wiring**, not a functional app.

**What it proves**: "This framework's entire ecosystem works on Zerops — cache, queues, storage, search, mail."

**Services**: App + Worker + PostgreSQL + Valkey + Object Storage + Mailpit + Meilisearch.

**Integration guide required**: Must include a step-by-step guide on how to go from default framework installation to full Zerops integration across all services (DB, cache, storage, search, mail, queues).

**Prompt**: Meta-prompt generates per-framework prompts (`zrecipator-{framework}-showcase.md`).

---

## 5. Framework Starter Kit / Dashboard Utility [NOT STARTED]

**What it is**: A well-known project built on top of a base framework — Laravel Jetstream, Laravel Filament, Django Cookiecutter, NestJS + Prisma starter, Rails with Devise, etc.

**What the app does**: Whatever the starter kit does — real, usable applications, not demos.

**What it proves**: "This popular project works on Zerops out of the box."

**Services**: Varies. Minimum: App + PostgreSQL + Valkey + Object Storage + properly configured env vars.

**HA-readiness baseline** (every starter kit must configure):
- Cache/sessions via Valkey (not filesystem)
- Storage via S3 (not local disk)
- Logging via syslog (not file)
- Maintenance mode via cache (not file)

**Prompt**: Per-project (`zrecipator-laravel-jetstream.md`, etc.).

---

## 6. CMS / E-commerce OSS [NOT STARTED]

**What it is**: Open-source CMS or e-commerce platforms (Strapi, Directus, Medusa, Ghost, WordPress, etc.) deployed on Zerops. Recipe app repo contains **actual generated code** from the CMS's scaffolding command.

**What it proves**: "This OSS platform runs on Zerops with all its features working."

**Services**: Varies. Typically: App + PostgreSQL + Object Storage. May need Valkey, search, queue worker, separate frontend.

**Key difference**: Contains scaffolded/generated code (output of `npx create-strapi-app`, etc.) with Zerops config applied.

**Prompt**: Per-project.

---

## 7. Software OSS [NOT STARTED]

**What it is**: Standalone open-source software (Plausible, n8n, Gitea, Umami, etc.) deployed on Zerops. Recipe app repo contains **just a zerops.yaml** and config — no forked source code.

**What it proves**: "You can self-host this on Zerops."

**Services**: Varies. Build commands `git clone` upstream repo or `curl` pre-built binaries.

**Key difference**: We don't maintain a fork. We write the glue (zerops.yaml + config) that makes the upstream release work on Zerops.

**Prompt**: Per-project.

---

## Summary Matrix

| # | Type | Status | Prompt | App Code | Services | Purpose |
|---|------|--------|--------|----------|----------|---------|
| 1 | Runtime Hello World | **DONE** | Single, multi-language | Raw HTTP + SQL | App + DB | "This language runs" |
| 2a | Frontend Static | **DONE** | Single, multi-framework | Static SPA | App only | "This SPA builds & serves" |
| 2b | Frontend SSR | **DONE** | Single, multi-framework | SSR framework | App + DB | "This SSR framework runs" |
| 3 | Framework Minimal | **IN PROGRESS** | Meta → per-framework | Framework project | App + DB | "This framework runs natively" |
| 4 | Framework Showcase | **IN PROGRESS** | Meta → per-framework | Framework + all services | App + Worker + 5 services | "Full stack works" |
| 5 | Starter Kit | Not started | Per-project | Real project | Varies (HA-ready) | "This popular project works" |
| 6 | CMS / E-commerce | Not started | Per-project | Generated code | Varies | "Self-host this platform" |
| 7 | Software OSS | Not started | Per-project | Just zerops.yaml | Varies | "One-click self-hosting" |

## Prompt Architecture

```
Single prompt, covers many:          Per-project prompts:
+-------------------------+          +------------------+
| runtime-hello-world.md  | ← 1     | laravel-jetstream | ← 5
| fe-static-hello-world   | ← 2a    | laravel-filament  | ← 5
| fe-ssr-hello-world      | ← 2b    | strapi            | ← 6
+-------------------------+          | medusa            | ← 6
                                     | plausible         | ← 7
Meta-prompt → generates per-fw:      | n8n               | ← 7
+-------------------------+          +------------------+
| be-framework-meta.md    | ← 3+4
|  → laravel-minimal.md   |
|  → laravel-showcase.md  |
|  → nestjs-minimal.md    |
|  → ...                  |
+-------------------------+
```
