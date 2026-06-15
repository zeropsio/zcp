# Research phase

Next call: `zerops_recipe action=update-plan slug=<slug> plan=<payload>`.
Don't call `zerops_knowledge` — the tool's own description says it's the
wrong tool during recipe authoring, and this atom supplies the
authoritative service set + runtime table below.

## Where to write outputs

Pass `outputRoot=/var/www/zcprecipator/<slug>/` at `action=start`. The
workspace at `/var/www/` holds the SSHFS dev mounts (`apidev/`,
`appdev/`, `workerdev/` — one per codebase) — recipe outputs nest one
level down under `/var/www/zcprecipator/<slug>/` so they don't shadow
the dev codebases. The engine refuses `outputRoot=/var/www` directly.

## Canonical services — authoritative versions

Do not guess versions. Use these exactly. Do not add services not in
this table.

| Kind    | Hostname  | Type             |
|---------|-----------|------------------|
| db      | `db`      | `postgresql@18`  |
| cache   | `cache`   | `valkey@7.2`     |
| broker  | `broker`  | `nats@2.12`      |
| storage | `storage` | `object-storage` |
| search  | `search`  | `meilisearch@1.20` |

Mail / SMTP / Mailpit are NOT canonical showcase services. Production
users bring their own SMTP. The research gate rejects non-canonical
hostnames at `complete-phase`.

## Runtime types

Match the framework family: `nodejs@22` / `php-nginx@8.4` / `python@3.14`
/ `go@1` / `rust@stable` / `java@21` / `dotnet@9` / `ruby@3.4` /
`bun@1.2` / `deno@2` / `elixir@1.16`. Pick the latest from the family
your framework uses.

## Service set per tier

- `hello-world-{lang}` → runtime only, **0** managed services.
- `{framework}-minimal` → runtime + `db` (1 managed service).
- `{framework}-showcase` → runtime + all **5** canonical services above.

## Classification — full-stack vs API-first

Apply to pick codebase shape. Use your framework knowledge:

- **Full-stack** (built-in view engine — Laravel/Blade, Rails/ERB,
  Django/Jinja2, Phoenix/HEEx, SvelteKit+server, Next.js+server):
  → **shape 1** (monolith). One codebase, `role=monolith`.
- **API-first** (JSON-only — NestJS, Express, Fastify, Hono, FastAPI,
  Flask API, Spring Boot, Gin, Axum, Actix): → **shape 2 or 3**.
  - Shape 2: 2 codebases. `role=api` + `role=frontend`. Worker shares
    api's codebase (queue library runs as sibling process).
  - Shape 3: 3 codebases. Same two + separate worker codebase.
    Use when the framework's worker needs a first-class long-lived
    context distinct from the API (NestJS `createApplicationContext`,
    Express standalone worker). For NestJS specifically: shape 3.

hello-world and minimal tiers are always shape 1 regardless of
framework family — they test runtime + platform, not service fan-out.

## Frontend default (shape 2/3 only)

Svelte + Vite compiled to static. Deploys on `static` runtime in prod.
Build: `npm ci && npm run build`. Don't pick React/Vue/Angular unless
the user asked for one by name.

## Parent recipe handling

The `parentStatus` field in the `start` response tells you whether a
parent recipe is available for your slug — it's a prediction, not a
loaded body. Resolution is lazy: the parent `.md` body is loaded on
first scaffold-brief dispatch and surfaced inline to that sub-agent
(and the codebase-content / env-content / refinement composers
downstream) as the "Parent recipe baseline (embedded)" section.

- **`"embedded"`**: parent recipe `internal/knowledge/recipes/
  <parent-slug>.md` exists in the binary's embedded knowledge corpus.
  The scaffold sub-agent will see the full body inline when its
  brief composes. At research phase the body is NOT in the start
  response — if you want to read it now for convention inheritance
  (setup naming, project-secret posture, codebase yaml shape), call
  `zerops_knowledge recipe=<parent-slug>`. This is the one
  legitimate parent-content use of `zerops_knowledge` at
  recipe-authoring time. Inherit setup names, project-env posture,
  and structural patterns — don't re-invent. The embedded minimal
  recipe has been deployment-verified; your showcase extension
  should be a superset of its conventions, not a parallel one.
- **`"absent"`**: no parent for this slug — the recipe genuinely
  has none (`hello-world-*`, `*-minimal`) OR the parent hasn't been
  published yet (no embedded `.md`). Three cases govern what's
  allowed:
  - **For the canonical service set + runtime versions**: proceed
    from this atom only. Don't call `zerops_knowledge` to substitute
    for these — they're authoritative whether or not parent exists.
  - **For convention inheritance** (setup naming, project-secret
    posture, comment style, codebase yaml shape): without an
    embedded or mounted parent, the agent extrapolates from
    `zerops_knowledge query=<topic>` guides.
  - **For platform mechanics** (env-var rules, alias contracts,
    L7 balancer behavior): use `zerops_knowledge query=<topic>`
    for the relevant guide. Always preferred over agent
    extrapolation.
- **`"mounted"`**: parent recipe came from a filesystem-mounted
  tree (`~/recipes/<parent-slug>/` with full per-codebase READMEs +
  per-tier import.yamls). Legacy CDE shape. Read
  `parent.codebases[].readme` and `parent.envImports["0"]` verbatim
  in addition to the embedded body.

## Payload shape for update-plan

```json
{
  "framework": "<slug root, e.g. \"nestjs\">",
  "tier": "hello-world | minimal | showcase",
  "research": {
    "codebaseShape": "1 | 2 | 3",
    "needsAppSecret": true,
    "appSecretKey": "<env-var name, e.g. APP_SECRET / APP_KEY>",
    "description": "<one sentence>"
  },
  "codebases": [
    {"hostname": "api",    "role": "api",      "baseRuntime": "nodejs@22"},
    {"hostname": "app",    "role": "frontend", "baseRuntime": "nodejs@22"},
    {"hostname": "worker", "role": "worker",   "baseRuntime": "nodejs@22",
     "isWorker": true, "sharesCodebaseWith": ""}
  ],
  "services": [
    {"hostname": "db",      "type": "postgresql@18",    "kind": "managed", "priority": 10},
    {"hostname": "cache",   "type": "valkey@7.2",       "kind": "managed", "priority": 10},
    {"hostname": "broker",  "type": "nats@2.12",        "kind": "managed", "priority": 10},
    {"hostname": "storage", "type": "object-storage",   "kind": "storage"},
    {"hostname": "search",  "type": "meilisearch@1.20", "kind": "managed", "priority": 10}
  ]
}
```

Above example is a NestJS showcase (shape 3). Swap framework/tier/
codebases/roles for other combinations.

## Then

1. `zerops_recipe action=update-plan slug=<slug> plan=<payload>` (merges
   into session — you can send partials)
2. `zerops_recipe action=complete-phase slug=<slug>` → runs the research
   gate. Read `violations` on failure, patch via another update-plan,
   retry complete-phase. Do NOT call `zerops_knowledge` to understand
   violations — the violation message itself names the field + fix.
3. `zerops_recipe action=enter-phase slug=<slug> phase=provision` →
   advance into the next phase. **`complete-phase` does NOT auto-
   advance** — it marks the current phase done; the explicit
   `enter-phase` call is what moves the session forward. Skipping it
   leaves the session at `phase=research` and the next `complete-phase`
   call re-runs research gates.
