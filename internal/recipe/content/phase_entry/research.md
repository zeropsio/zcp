# Research phase — classify framework, decide codebase shape, submit typed plan

Your job in this phase: classify the framework and produce a typed Plan
describing the recipe. No code written yet — this is the upstream
decision that every downstream phase depends on.

## Classification — full-stack vs API-first

Apply this decision tree using your knowledge of the framework:

```
Does the framework have a built-in view/template engine designed for
rendering HTML from the same process that handles routing?
│
├─ YES → full-stack. Single codebase ("monolith") serves HTML and API
│        routes. Dashboard uses the framework's templates. Worker runs
│        the same codebase in a different process model.
│        Examples of shape: Laravel+Blade, Rails+ERB, Django+Jinja2,
│        Phoenix+HEEx, ASP.NET+Razor, SvelteKit w/ server routes,
│        Next.js w/ server routes.
│
└─ NO  → API-first. Framework serves JSON/plain text only. Dashboard
         lives in a separate codebase (static frontend — default Svelte).
         Worker lives in a third codebase OR shares the API's codebase
         depending on the framework's process model (see below).
         Examples of shape: NestJS, Express, Fastify, Hono, FastAPI,
         Flask (API mode), Spring Boot (API mode), Go (chi/fiber/echo),
         Rust (axum/actix), Phoenix as API-only.
```

Classification rule of thumb: if the predecessor `{framework}-minimal`
(or `hello-world-{lang}`) renders HTML via a framework-integrated
template engine, it's full-stack. If the predecessor returns JSON or
plain text, it's API-first.

## Codebase shape

After classification, pick shape:

- **Shape 1 (monolith)** — full-stack frameworks. One codebase owns
  routes, views, and worker process. `role=monolith`.
- **Shape 2 (api + frontend, worker shares api)** — API-first frameworks
  whose queue library runs naturally as a sibling process of the API
  (shared codebase, different zeropsSetup). Two codebases.
  `role=api, role=frontend`. Worker declared with
  `isWorker=true, sharesCodebaseWith="<api-hostname>"`.
- **Shape 3 (api + frontend + worker-separate)** — API-first frameworks
  whose worker process uses a first-class long-lived context
  distinct from the API (e.g. NestJS `createApplicationContext`,
  Express standalone worker). Three codebases. Worker is
  `isWorker=true, sharesCodebaseWith=""`.

Hello-world and minimal tiers collapse to shape 1 regardless of framework
— they prove the language+platform contract, not service fan-out. Shape
2/3 only applies at showcase tier.

## Default service set per tier

- **hello-world-{lang}** — no managed services. Runtime only.
- **{framework}-minimal** — framework + 1 database (PostgreSQL default).
  Framework-idiomatic ORM + migrations + one CRUD endpoint.
- **{framework}-showcase** — framework + `db` + `cache` + `broker` +
  `storage` + `search`. Managed-service hostnames: `db`, `cache`,
  `broker`, `storage`, `search`. Types: default PostgreSQL, Valkey,
  NATS, Object Storage, Meilisearch. Mail (SMTP) is NOT part of the
  showcase service set — Zerops customers use external SMTP providers.

Showcase MUST NOT add services beyond this set without a signal in the
parent recipe. Laravel-showcase's Mailpit is Laravel-specific and does
not transfer.

## Frontend default (API-first only)

When shape is 2 or 3, the frontend codebase defaults to **Svelte
(Vite) compiled to static assets**. Rationale: smallest bundle, HTML-
superset syntax, deploys on `static` runtime (pure Nginx) in prod,
single `npm ci && npm run build`. Don't pick React/Vue/Angular unless
the user asked for one by name.

## Parent recipe inheritance

If `parent` is populated in the session (the chain resolver found a
published parent), do NOT re-derive the parent's decisions:

- Service hostnames + types: copy from parent, add showcase-new services.
- Runtime type (e.g. `nodejs@22`): copy from parent, don't bump unless
  the framework released a new stable that the parent pre-dates.
- Codebase hostnames: preserve (showcase extends minimal, same names).
- Gotchas / IG items: the writer cross-references the parent later;
  don't plan to re-author.

Parent content is inlined in `zerops_recipe action=start` response under
`parent.codebases[].readme` and `parent.envImports["0"]`. Read it there
— do not call `zerops_knowledge` with freeform queries for the parent.

## Required output — submit via action=update-plan

Build a payload of shape:

```json
{
  "framework": "<slug without -minimal/-showcase, e.g. nestjs>",
  "tier": "hello-world | minimal | showcase",
  "research": {
    "codebaseShape": "1 | 2 | 3",
    "needsAppSecret": true/false,
    "appSecretKey": "<env-var name the framework expects, or empty>",
    "description": "one-sentence recipe purpose"
  },
  "codebases": [
    {"hostname": "<host>", "role": "monolith|api|frontend|worker",
     "baseRuntime": "<type@version>", "isWorker": false,
     "sharesCodebaseWith": ""}
  ],
  "services": [
    {"hostname": "db", "type": "postgresql@18", "kind": "managed",
     "priority": 10}
  ]
}
```

Call: `zerops_recipe action=update-plan slug=<slug> plan=<payload>`.

When the plan is in place, call `zerops_recipe action=complete-phase
slug=<slug>` to run the research gate (checks classification/shape
consistency, required services per tier, parent inheritance). Gate
failures return structured violations — fix the plan and retry.

Do NOT call `build-brief` before `update-plan`: scaffold briefs read
codebases from the plan, so an empty plan causes the brief composer to
fail with `unknown role`.
