# Recipe Workflow — Visual Map

## 1. Linear Flow (6 steps)

```
   ┌──────────┐   ┌───────────┐   ┌──────────┐   ┌────────┐   ┌──────────┐   ┌───────┐
   │ RESEARCH │──▶│ PROVISION │──▶│ GENERATE │──▶│ DEPLOY │──▶│ FINALIZE │──▶│ CLOSE │
   └──────────┘   └───────────┘   └──────────┘   └────────┘   └──────────┘   └───────┘
     plan +         create          write             deploy      emit           audit
     decisions      services        zerops.yaml       + verify    import.yaml    + fix
     + research     + discover      + app code        + subdom    + READMEs      critical
                    env vars        (validated)       + logs      (auto-gen)     bugs
                                                          │
                                                          ▼
                                          auto-writes template files on
                                            DEPLOY → FINALIZE transition
```

Loop-back only within a step (sub-step retries on validation fail). No conditional skipping of steps.

---

## 2. Branch Points (gate which guidance fires)

```
                        ┌─ tier: showcase? ──────────▶ dashboard skeleton + subagent briefs
                        │
                        ├─ dual-runtime? ────────────▶ URL shapes + deploy-api-first
   RecipePlan ──────────┤                              + project-env-vars pointer
   (shape detection)    │
                        ├─ hasWorker? ───────────────▶ worker setup + deploy-worker
                        │
                        ├─ multi-codebase? ──────────▶ git-init-per-codebase +
                        │                              scaffold subagent brief
                        │
                        ├─ multi-base runtime? ──────▶ dev-dep preinstall +
                        │  (non-JS + JS build)         build/run asymmetry warning
                        │
                        ├─ serve-only prod? ─────────▶ dev override + asset dev-server
                        │  (static prod, compile dev)
                        │
                        └─ bundler dev-server? ──────▶ host-check + asset dev-server
```

---

## 3. Retry / Adaptive Branch

```
   GENERATE fails validation ──▶ retry delta injected (fragment/comment ratio)
   DEPLOY fails health check ──▶ FailurePattern analyzed ──▶ adaptive retry delta
                                                              (or fallback delta)
```

---

## 4. Recipe Tiers (what the code actually knows)

Source of truth: [recipe.go:13-18](../../internal/workflow/recipe.go#L13-L18) — only **3 tiers** exist.

```
                          RECIPE TIERS
                               │
        ┌──────────────────────┼──────────────────────┐
        │                      │                      │
    HELLO-WORLD             MINIMAL                SHOWCASE
    "language/runtime      "framework +           "framework +
     runs + talks to DB"    DB + native tooling"   full wiring"
        │                      │                      │
     App + DB              App + DB              App + Worker + DB
     (or App only          (framework's own       + Valkey + S3
      for static FE)        ORM + migrations      + Mailpit
                            + templates)           + Meilisearch
        │                      │                      │
  Single prompt          Meta-prompt →         Meta-prompt →
  covers all             per-framework         per-framework
  languages/FEs          minimal               showcase
```

The tier is a guidance-gating string on `RecipeState.Tier`. Branch predicates (isShowcase, hasWorker, dual-runtime, etc.) compose with tier to decide which guidance blocks fire.

---

## 5. Frameworks by Tier

Any backend framework can be run at **minimal** or **showcase** depth — same code, different wiring breadth. Hello-world is a separate shape (no framework, or frontend-only).

### HELLO-WORLD — language/SPA/SSR proofs

| Shape | Examples |
|-------|----------|
| **Raw runtime** (App + DB, stdlib HTTP + SQL driver) | Node.js, Bun, Deno, Go, Python, Rust, PHP, Ruby, Elixir, Java, .NET |
| **Static frontend** (App only, build → Nginx) | React + Vite, Vue + Vite, Svelte + Vite, SolidJS, Preact, Qwik (static mode), Lit |
| **SSR frontend** (App + DB, Node runtime) | Next.js, Nuxt, SvelteKit, Astro, Remix / React Router v7, Qwik City, Analog, Fresh (Deno) |

### MINIMAL — framework + DB, native tooling only

Any backend framework with its ORM + migrations + template engine. Proves "this framework runs natively on Zerops."

| Language | Frameworks |
|----------|------------|
| **PHP** | Laravel, Symfony, CodeIgniter, Laminas, Slim |
| **Node/TS** | NestJS, Express, Fastify, Koa, Hono, AdonisJS, LoopBack |
| **Python** | Django, FastAPI, Flask, Pyramid, Starlette, Litestar |
| **Ruby** | Rails, Sinatra, Hanami, Roda |
| **Go** | Gin, Echo, Fiber, Chi, Buffalo |
| **Java/Kotlin** | Spring Boot, Quarkus, Micronaut, Ktor |
| **.NET** | ASP.NET Core, Minimal API |
| **Elixir** | Phoenix |
| **Rust** | Actix, Axum, Rocket |

### SHOWCASE — architectural shapes

Showcase must wire **worker + DB + Valkey + S3 + Mailpit + Meilisearch**. But frameworks don't all fit the same shape — a Laravel showcase looks fundamentally different from a NestJS one because Laravel renders HTML natively while NestJS is API-first.

Two shapes, decided by how the framework serves users:

#### Shape A — Monolith fullstack (`app` + `worker`)

Framework renders HTML itself (first-class view layer). Single user-facing service. Worker is a separate codebase using the **same** framework/language with a built-in queue system.

| Framework | View layer | Worker | Notes |
|-----------|-----------|--------|-------|
| **Laravel** | Blade + Livewire / Inertia | Horizon (`queue:work`) | first-class queues, multi-base PHP+Node build |
| **Symfony** | Twig | Messenger (`messenger:consume`) | first-class |
| **Rails** | ERB / Hotwire / Turbo | Sidekiq / ActiveJob | first-class |
| **Django** | Django templates | Celery (convention) | library-based but universal |
| **Phoenix** | LiveView / EEx | Oban | Elixir BEAM concurrency, near-universal |
| **AdonisJS** | Edge | BullMQ / @rlanz/bull-queue | first-class in v6 |
| **ASP.NET Core MVC** | Razor Pages | Hangfire / `BackgroundService` | first-class hosted services |
| **Spring Boot** (classic) | Thymeleaf / JSP | `@Async` / Spring Batch / `@Scheduled` | first-class |

Codebases: **2** (app monolith + worker). Dashboard is a Blade/Twig/ERB/Jinja/LiveView page.

#### Shape B — Dual-runtime (`api` + `app` + `worker`) ← NestJS canonical

API-first framework returns JSON only. Paired with a **separate** frontend codebase (SPA or SSR). Worker is a third codebase. 3 codebases total — the hardest shape, which is why nestjs-showcase is the regression canary.

| API framework | Typical FE pairing | Worker strategy |
|---------------|-------------------|-----------------|
| **NestJS** ✅ canonical | React+Vite / Vue / Next | BullMQ module (first-class) |
| **Express / Fastify / Hono / Koa** | any Vite SPA or Next/Nuxt | BullMQ / bee-queue / agenda |
| **FastAPI / Litestar / Starlette** | React/Vue SPA | Celery / Arq / Dramatiq |
| **Flask** | SPA | Celery / RQ |
| **Gin / Echo / Fiber / Chi** | SPA | goroutine worker in separate service (no framework queue) |
| **Spring Boot** (REST) | SPA or Next | Spring Batch / `@Scheduled` / ActiveMQ consumer |
| **Quarkus / Micronaut / Ktor** | SPA | reactive streams / coroutines / Kafka consumer |
| **ASP.NET Core Minimal API** | SPA or Blazor | Hangfire / `BackgroundService` |
| **Actix / Axum / Rocket** | SPA | tokio task in separate binary |
| **Phoenix** (API-only mode) | LiveView elsewhere or SPA | Oban |

Codebases: **3** (api + app + worker). FE pairing is agent's choice based on research step.

#### Ambiguous — can go either way

| Framework | Why ambiguous |
|-----------|---------------|
| **NestJS** | Has MVC module (Handlebars/EJS) but nobody uses it — always dual-runtime |
| **Spring Boot** | Thymeleaf = Shape A; `@RestController` = Shape B. Modern = B |
| **FastAPI** | Jinja2 supported but ecosystem is 99% JSON-API → Shape B |
| **AdonisJS** | Edge-native (Shape A) but v6 ships `@adonisjs/inertia` (Shape B possible) |
| **Phoenix** | LiveView (Shape A) vs `mix phx.new --no-html` (Shape B) |

Decision is made at research-step by the agent based on framework's idiomatic usage.

#### Current status

| Framework | Shape | Status |
|-----------|-------|--------|
| **NestJS** | B (dual-runtime) | ✅ canonical — v6–v18 regression runs |
| **Laravel** | A (monolith) | 🔄 WIP — multi-base PHP+Node build |
| **Django** | A (monolith) | 🔄 WIP |
| **Rails** | A (monolith) | planned |
| **Spring Boot** | B (REST) | planned |
| **Phoenix** | A (LiveView) | planned |

---

## 6. Service Stack by Tier

```
  Tier            App  DB   Valkey  S3   Mail  Search  Worker   Notes
  ──────────────  ───  ───  ──────  ───  ────  ──────  ──────   ─────────────
  hello-world     ●   ●/·    ·      ·    ·      ·       ·      DB optional
                                                                 (static FE = no DB)
  minimal         ●    ●     ·      ·    ·      ·       ·      framework-native
  showcase        ●    ●     ●      ●    ●      ●       ●      full wiring proof
```

---

## 7. Prompt Architecture

```
  SINGLE PROMPT                  META PROMPT
  (covers N shapes)              (generates per-framework prompts)
  ┌──────────────────────┐      ┌────────────────────────────┐
  │ runtime-hello-world  │      │ be-framework-meta.md       │
  │ fe-static-hello      │      │   ↓ generates              │
  │ fe-ssr-hello         │      │ laravel-minimal.md         │
  └──────────────────────┘      │ laravel-showcase.md        │
    tier: hello-world           │ nestjs-minimal.md          │
                                │ nestjs-showcase.md         │
                                │ django-minimal.md          │
                                │ ...                        │
                                └────────────────────────────┘
                                  tier: minimal | showcase
```

> **Note**: [recipe-taxonomy.md](recipe-taxonomy.md) lists 7 "types" (starter kits, CMS, OSS) but those are aspirational — code only implements the 3 tiers above.
