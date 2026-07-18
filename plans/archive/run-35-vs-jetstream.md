# Run-35 ↔ Laravel Jetstream side-by-side

**Date:** 2026-05-10
**Run-35:** [`docs/zcprecipator3/runs/35/`](../docs/zcprecipator3/runs/35/) — nestjs-showcase recipe (3 codebases: api / app / worker; 5 managed services; 6 tiers)
**Jetstream golden:**
- recipe-repo: `/Users/fxck/www/recipes/laravel-jetstream/` — 27-line root README + 6 tier dirs (each: 6-8-line tier README + 53-76-line tier import.yaml)
- apps-repo: `/Users/fxck/www/laravel-jetstream-app/` — 290-line README + 183-line zerops.yaml + 31-line CLAUDE.md
**Method:** every artifact read end-to-end as a porter would, against the v9.77.0 substrate's audience-model bar.
**Codex-verified:** 8 load-bearing claims confirmed, 4 framed loose (IG line counts,
tier-yaml-head wording byte-vs-semantic, yaml density rounding, "single most concrete"
editorial). Loose framings noted inline with `[codex]` markers.

This complements [`run-35-validation.md`](run-35-validation.md). Validation
focused on closure-of-regressions; this doc compares Run-35 to the canonical
audience-model golden across **placement, voice, and big picture**.

---

## TL;DR — what's at-bar, what diverges

| Axis | Verdict |
|---|---|
| Major regressions from run-34 | **all closed** — RF1+PD1+Understand on apidev; tier-prefix gone; 0 voice-leak; 0 fact contamination; env coherence aligned |
| Tier-README intro voice | **below jetstream** — TY2 voice persists. Jetstream tier intros tell the porter WHAT THIS TIER IS FOR (audience + use case). Run-35 tier intros tell WHAT THIS TIER IS BUILT WITH (specs + dimensions). Same gap shape as TY2 in yaml comments. |
| Tier import.yaml per-service comment voice | **below jetstream** (deferred per user) — leads with descriptor + mechanism rather than imperative + role + relationship |
| Tier import.yaml head comment | **divergent** — jetstream mirrors the README intro; run-35 dives into project-envVariables-block explanation instead |
| apps-repo README H2 structure | **at-bar with order divergence** — same major sections, different order; KB-shape divergent (see below) |
| KB philosophy | **divergent** — jetstream `## Tips and Others` H2 → narrative `### <topic>` H3 (porter-workflow tips). Run-35 `### Gotchas` H3 → bullet `**X** — Y` intersection-trap (deployment-trap warnings). Different audiences, both internally coherent. |
| KB sibling consistency | **below sim-3** — apidev `### Gotchas` H3 / appdev bare `### <topic>` H3 / workerdev `## Knowledge Base` H2. Three distinct shapes; sim-3 had three consistent. |
| IG item length | **above jetstream** — IG#2-#5 run 2-7× longer per item. Substantively more rigorous (failure mode + fix + code snippet each); cost is read-time. |
| RF1 substance | **above jetstream substance, at-bar shape** — denser, more service-specific detail per bullet |
| PD1 substance | **at-bar** — same 3-axis bullets (HA mode / minContainers / production secret) |
| apidev/zerops.yaml structure | **deliberate divergence** — jetstream uses `extends: base`; run-35 uses flat yaml per user direction ("flat beats DRY-with-indirection"). Cost: +8pp comment density vs golden. |
| apidev/CLAUDE.md | **deliberate divergence** — jetstream Zerops-aware (port, siblings, hybrid-dev, zcp MCP); run-35 Zerops-free (`claude /init`-shape, pure framework). Two halves of the same job split between README + CLAUDE.md. |
| Title shape | **below jetstream** — V3-axis slug-stem leak (`# nestjs-showcase — api`); jetstream uses `# Zerops x <!-- markers --> Laravel Jetstream <!-- markers -->` |
| Root recipe README | **marginally below** — missing the porter-meta one-liner ("Offered in examples for the whole development lifecycle…") that frames the 6 tiers |

---

## 1. Root recipe README

### Jetstream (27 lines)

```
# Laravel Jetstream Recipe
<intro extract>
⬇️ Full recipe page and deploy with one-click
[deploy button]
[cover]
Offered in examples for the whole development lifecycle — from environments
for AI agents like Claude Code or opencode through environments for remote
(CDE) or local development of each developer to stage and productions of
all sizes.
- AI agent  — [info] [deploy]
- Remote (CDE) — [info] [deploy]
- Local — [info] [deploy]
- Stage — [info] [deploy]
- Small Production — [info] [deploy]
- Highly-available Production — [info] [deploy]
---
[catalog punt]
[community line]
```

### Run-35 (25 lines)

```
# nestjs-showcase
<intro extract>
⬇️ Full recipe page and deploy with one-click
[deploy button]
[cover]
- AI agent — [info] [deploy with one click]
- Remote (CDE) — [info] [deploy with one click]
- Local — [info] [deploy with one click]
- Stage — [info] [deploy with one click]
- Small Production — [info] [deploy with one click]
- Highly-available Production — [info] [deploy with one click]
---
[catalog punt]
[community line]
```

### Diff
- **Missing:** the porter-meta framing one-liner explaining what the 6-tier list is FOR ("Offered in examples for the whole development lifecycle…"). Jetstream sets up the lifecycle scope before the list; run-35 jumps straight into the list.
- **Title shape:** jetstream `# Laravel Jetstream Recipe` (clean human title); run-35 `# nestjs-showcase` (slug-stem, V3-axis bare slug).

### Verdict
**Marginally below** — the missing framing line is small but load-bearing. A first-time porter scanning the root README walks away from jetstream knowing "this is a 6-tier lifecycle showcase"; from run-35, "this is a 6-tier list whose purpose is implied".

---

## 2. apps-repo README — H2 structure + section order

### Jetstream apps-repo README structure (290 lines)

```
# Zerops x <NAME marker> Laravel Jetstream <NAME marker>
<intro extract>
[cover]
## Deploy to Zerops          ← dedicated H2 with deploy button + manual import link
## Integration Guide
  ### 1. Add `zerops.yaml`        (~190 lines incl. yaml block)
  ### 2. Add Support For Object Storage  (3 lines: bullet, code, bullet)
  ### 3. Utilize Environment Variables   (1 paragraph)
  ### 4. Setup Production Mailer         (1 line)
## Understand Zerops Core Concepts        ← here, not at the bottom
## Recipe features                          (8 bullets)
## Production vs. Development               (3 bullets)
## Tips and Others                          ← KB H2 wrapper
  ### Maintenance Mode                    (3-paragraph narrative + > [!CAUTION] block)
  ### Temporary Upscaling when Playing Around (3 paragraphs)
[community line]
```

### Run-35 apidev/README.md structure

```
# nestjs-showcase — api          ← V3 slug-stem leak in title
<intro extract>
[deploy button]                  ← inline, no H2 wrapper
[cover]
## Integration Guide
  ### 1. Adding `zerops.yaml`    (182 lines incl. yaml block)
  ### 2. Bind the listener to `0.0.0.0`             (12 lines)
  ### 3. Trust the reverse proxy                    (12 lines)
  ### 4. Avoid the self-shadow when re-declaring auto-injected env vars  (7 lines)
  ### 5. Decompose `execOnce` keys into migrate + seed                   (14 lines)
## Recipe features                  (8 bullets)
## Production vs. Development       (3 bullets)
## Understand Zerops Core Concepts  ← inserted between IG/RF1 group and KB
### Gotchas                         ← H3, no H2 wrapper
  - 8 bullets, `**X** — Y` intersection-trap shape
```

### Diffs

**1. Title format:**
- Jetstream: `# Zerops x <!-- markers --> Laravel Jetstream <!-- markers -->` — engine-extractable name slot, human-friendly title
- Run-35: `# nestjs-showcase — api` — slug-stem + codebase suffix; **V3 axis violation** (the rule list explicitly forbids slug-stem in title)

**2. Deploy section:**
- Jetstream: `## Deploy to Zerops` H2 with both the deploy button AND a manual `zerops-project-import.yml` link
- Run-35: deploy button inline above the IG H2, no manual import link

**3. Section ORDER:**
- Jetstream: IG → **Understand Core Concepts → RF1 → PD1 → KB**
- Run-35: IG → **RF1 → PD1 → Understand Core Concepts → KB**

Jetstream keeps porter's hands-on flow first (deploy → IG → Core Concepts as a "if you want to learn the platform deeper" exit), then "what this recipe gives you" (RF1) + "how to scale to prod" (PD1) + "operational tips" (KB).

Run-35 inserts RF1+PD1 between IG and Core Concepts. The Core Concepts link becomes more of a "by the way, here's a tutorial" footer rather than a load-bearing exit.

Both orderings are valid; they tell different stories about what the doc IS.

**4. KB shape — fundamentally different philosophy:**

Jetstream `## Tips and Others`:
- **2 entries**, each a narrative `### <Topic>` H3 with 3-paragraph body
- Subjects: `### Maintenance Mode` (how to use `php artisan down` safely on multi-replica) + `### Temporary Upscaling when Playing Around` (how to use `zsc scale ram` for ad-hoc work)
- **Audience: porter who's already deployed, now operating**
- Voice: pedagogical, includes a `> [!CAUTION]` callout with `zsc health-check disable` shell sequence

Run-35 apidev `### Gotchas`:
- **8 bullets**, each `**Name — body**` shape
- Subjects: TypeORM synchronize destructive on rolling deploy, APP_SECRET self-shadow, CORS_ORIGINS literal at first deploy, X-Cache cross-origin invisibility, NoSuchBucket virtual-host trap, UnknownError storage scheme, empty Meilisearch on redeploy, 502 loopback bind
- **Audience: porter evaluating + deploying, hits a trap during integration**
- Voice: intersection-trap — "this fails because X, do Y"

Both are coherent. They serve **different audiences**. Run-35's intersection-trap shape is *more pedagogically useful for first-time porters*; jetstream's tips-shape is *more useful for operating-the-deployed-recipe*. Whether to align run-35 onto jetstream's shape depends on which audience is primary.

### Verdict
**At-bar with order divergence + KB-shape divergence.** All major H2 sections are present (RF1+PD1+Understand all landed — six v9.77.0 fixes lit). The order swap is opinion-different, not wrong. The KB shape is **fundamentally a different KB philosophy** — intersection-trap warnings vs porter-workflow-tips. If the goal is "match jetstream", convert KB to narrative `### <topic>` H3 entries under a `## Tips and Others` H2. If the goal is "porter hits all the deployment traps", current shape is better.

---

## 3. apps-repo README — voice, tone, IG item length

### IG#1 (Adding zerops.yaml) — opening voice

**Jetstream IG#1 lead:**
> Add the following `zerops.yaml` file to the root of your repository. The `zerops.yaml` file defines how Zerops should build, deploy and run your application. Feel free to customize it to better suit your application's needs.
>
> This example includes idempotent migrations, caching, optimized build process and support for remote or agent development.

**Run-35 IG#1 lead:**
> The main configuration file — place at repository root. It tells Zerops how to build, deploy and run your app. This one declares 2 setups (`dev`, `prod`), runs `initCommands` at boot (migrations, seed), and ships readiness + health checks.

**Diff:** jetstream is conversational ("Feel free to customize"), run-35 is denser ("declares 2 setups", "ships readiness + health checks"). Run-35 telegraphs more in fewer words; jetstream warmer entry. Both at-bar; tone preference.

### IG#2-#5 length comparison

| Item | Jetstream (body lines, no heading) | Run-35 (body lines, no heading) | Ratio |
|---|---|---|---|
| IG#2 | bullet + code-fence + bullet (~5 body lines) | 2 paragraphs + code (~12 lines) | ~2-3× |
| IG#3 | 1 body line | 12 lines | 7-12× |
| IG#4 | 1 body line | 7 lines | 7× |
| IG#5 | (no IG#5) | 14 lines | n/a |

> [codex] Specific per-item line counts depend on counting method (with/without heading + blank lines). Directional 2-7× ratio holds; below numbers measure body-only.

**Substance comparison — IG#3 (env var setup):**

Jetstream IG#3 (1 paragraph):
> Utilize [environment variables](https://github.com/zerops-recipe-apps/laravel-jetstream-app/blob/main/zerops.yml#L25-L75) and [secrets](https://github.com/zerops-recipe-apps/laravel-jetstream-app/blob/main/zerops-project-import.yml#L12-L16) to set up S3 for file system, Redis for cache and sessions, and trusted proxies to work with reverse proxy load balancer.

Run-35 IG#3 (12 lines, "Trust the reverse proxy"):
> Zerops terminates SSL at the L7 balancer and forwards the request to your container with the original client info in `X-Forwarded-For`, `X-Forwarded-Proto`, and `X-Forwarded-Host`. Without an explicit trust hop, NestJS' Express adapter reports `req.ip` as the balancer's internal IP and `req.protocol` as `http`, breaking IP-based rate limits, audit logs, and any code that branches on HTTPS.
>
> Tell the underlying Express to trust the proxy. The `set()` method lives on the Express adapter, so the application factory must be typed to expose it:
>
> ```ts
> const app = await NestFactory.create<NestExpressApplication>(AppModule);
> app.set('trust proxy', true);
> ```

**Different teaching philosophy:**
- Jetstream: **points-to-source** — links anchored at specific GitHub line ranges, lets the reader load the actual code
- Run-35: **explains-in-place** — failure mode, mechanism, fix, code snippet all inline

Run-35 is more porter-friendly for someone reading the README cold (no jumping out to GitHub). Jetstream is more concise for someone who already understands the platform vocabulary.

### Verdict
**Above-jetstream substance, above-jetstream length.** Run-35 IG items are more rigorous + more verbose. Whether this is "above-bar" or "below-bar" is audience-dependent: a porter who reads-to-learn benefits from run-35; a porter who scans-to-paste benefits from jetstream. Counter-only readout (Counter #14 IG line bloat from run-32 baseline) would say run-35 is improving versus pre-fix-pack baselines (apidev IG avg ~11 lines vs run-32 baseline 33), but still above golden ~3-5 lines.

---

## 4. Tier README intros — voice gap

This is the **load-bearing voice axis** where run-35 still differs most from jetstream.

| Tier | Jetstream | Run-35 |
|---|---|---|
| 0 (AI Agent) | "**AI agent** environment provides a development space for AI agents to build and version the app. Comes with a dev service with the source code and necessary development tools, a staging service, email & SMTP testing tool, and a low-resource databases and storage." | "Agent workspace shape — single-instance managed services on shared CPU with the smallest managed RAM, dev-runtime containers for every codebase, and project-level secrets generated once at import. Sized for end-to-end iteration without juggling production-shaped scale." |
| 1 (Remote CDE) | "**Remote (CDE)** environment allows developers to build the app within Zerops via SSH, supporting the full development lifecycle without local tool installation. Comes with a dev service with the source code and necessary development tools, a staging service, email & SMTP testing tool, and a low-resource databases and storage." | "Remote workspace for a human developer (CDE) — same single-instance shape as the agent workspace with dev-runtime containers per codebase, sized for SSH-driven iteration and end-to-end testing rather than steady traffic." |
| 2 (Local) | "**Local** environment supports local app development using zCLI VPN for database access, while ensuring valid deployment processes using a staged app in Zerops. Comes with a dev service with the source code and necessary development tools, a staging service, email & SMTP testing tool, and a low-resource databases and storage." | "Runs the production setup of every codebase against single-instance managed services on shared CPU, no dev-runtime containers — closest the recipe gets to production while staying cheap to spin up and tear down." |
| 3 (Stage) | "**Stage** environment uses the same configuration as production, but runs on the lowest scaling settings." | "Production setup on shared CPU with single-instance managed services and a 0.25 GB free-RAM headroom buffer on every container, sized for rehearsal traffic and QA runs against snapshots-only durability." |
| 4 (Small Production) | "**Small production** environment offers a production-ready setup optimized for moderate throughput." | "Two runtime containers per codebase on shared CPU enable rolling deploys with zero downtime, running against single-instance managed services with the 0.25 GB free-RAM spike buffer." |
| 5 (HA Production) | "**Highly-available production** environment provides a production setup with enhanced scaling, dedicated resources, and HA components for improved durability and performance." | "Dedicated CPU, SERIOUS corePackage on the project, HA mode on db/cache/broker, single-instance Meilisearch and object-storage with the 1 GB managed-RAM floor, two runtime containers per codebase, doubled 0.5 GB spike headroom." |

### Pattern

- **Jetstream intros** open with `**Tier name** environment <verb> <audience purpose>` — verb is `provides` / `allows` / `supports` / `uses` / `offers` / `provides`. The first thing the porter learns is **what this tier is FOR**.
- **Run-35 intros** open with implementation-shape descriptors — `Agent workspace shape — single-instance managed services on shared CPU…` / `Production setup on shared CPU with…` / `Two runtime containers per codebase on shared CPU enable…` / `Dedicated CPU, SERIOUS corePackage on the project, HA mode on db/cache/broker…`. The first thing the porter learns is **what this tier is BUILT WITH**.

Both communicate. **Jetstream tells you the tier mission; run-35 tells you the tier specs.** Operating-engineer voice vs porter-card-audience voice — the same gap shape as the TY2 yaml-comment problem at import.yaml level.

This gap is what survived the v9.77.0 preStitchEnv fix. The fix landed end-to-end (refinement's 6 env/N/intro replaces persisted to disk; tier-prefix gone), but the **content refinement authored** is still operational-engineer-voiced. The `Tier N — ` prefix is gone, but the body voice is the same shape that was leading the prefix in run-34.

### Verdict
**Below jetstream — TY2-class voice persists at tier README level.** This is consistent with the TY2 yaml-comment voice problem the user explicitly deferred. If addressed, intros should mirror jetstream pattern: `**Tier name** <verb> <audience purpose>` with the implementation specs as the second sentence.

---

## 5. Tier import.yaml — head comment + per-service comments

### Head-comment voice

Jetstream tier-3 yaml head (after `# zeropsPreprocessor=on`):

```yaml
# Stage environment uses the same configuration as production,
# but runs on the lowest scaling settings.
```

**Semantically identical** to the README intro — same body, same voice (the README has the markdown bold `**Stage**` prefix that the yaml comment elides). The head comment IS the tier mission statement. [codex: byte-vs-semantic match noted; tier-3 sampled, not exhaustively verified across all 6 tiers.]

Run-35 tier-3 yaml head:

```yaml
# Same single-slot project envs as tier 2 — APP_SECRET regenerated
# at import; API_URL / FRONTEND_URL resolve to the bare api/app
# subdomains. Replace these with your own hostnames once you swap
# subdomain access for a custom stage domain.
```

This is a project-envVariables-block explanation, NOT the tier mission. The tier README intro and the yaml head comment serve different purposes in run-35; jetstream mirrors them.

**Pattern across all 6 tiers:** every run-35 tier yaml leads with a project-envs-block explanation; every jetstream tier yaml leads with the README intro mirror. [codex sampled tier-3 directly; tier 0/1/2/4/5 spot-checked during this run for the same shape — head comment is project-envs-explanation, not tier-mission-mirror.]

### Per-service comment voice — the TY2 axis

**Tier 0 db comment:**

Jetstream:
> Deploy low-resource PostgreSQL database, used by the app to store data. Both the staged 'appstage' service and AI agents from 'appdev' typically connect to this database.

Run-35:
> Single-node PostgreSQL — the api codebase stores its items table and per-deploy migration markers here. Smallest managed RAM is enough at this tier where data is disposable. Priority 10 guarantees Postgres accepts connections before the api's initCommand runs migrations.

**Pattern:**
- Jetstream: imperative + role + relationship — `Deploy X, used by app to store data, both A+B connect`
- Run-35: descriptor + role + mechanism — `Single-node X — Y stores Z. Priority 10 guarantees... runs migrations.`

**Tier 4 (Small Production) cache comment:**

Jetstream:
> Spin up Valkey, an open-source Redis-compatible key/value storage for caching, sessions, queues, etc.

Run-35:
> Single-instance Valkey — both api replicas share the same cache keyspace, so a SET from one is immediately visible to the other. Bump verticalAutoscaling.minRam if the production hit-rate degrades because the working set evicts under load.

**Tier 5 (HA) db comment:**

Jetstream:
> Deploy the PostgreSQL database in highly-available mode, used by the Laravel app to store data. Automatic, encrypted backups are enabled by default too.

Run-35:
> PostgreSQL in HA mode — primary plus read replicas with streaming replication and auto-failover, so a primary failure recovers without manual restore. The 0.5 GB minFreeRamGB headroom on a 1 GB managed-RAM floor absorbs production query bursts; bump verticalAutoscaling.minRam if monitoring shows the working set saturating under steady-state load.

**Pattern verdict:** run-35 is **2-3× longer per service comment**, leads with type-descriptor + mechanism (TY2 BAD pattern), and adds "bump <yaml field> if <condition>" tail — operational-engineer scaffolding the porter doesn't necessarily need at first read. Jetstream is concise and porter-instructional.

This is the deferred TY2-yaml-comment voice item. Net effect: yaml comment density at tier import.yaml level is also above golden — though I haven't measured tier-yaml density specifically (only codebase zerops.yaml).

### Verdict
**Below jetstream on both axes — head-comment placement diverges (project-envs-explanation vs tier-mission mirror), per-service comments lead with type-descriptor + mechanism rather than imperative + role + relationship.** Deferred per user; flagged here as the persistent voice gap.

---

## 6. apidev/zerops.yaml vs jetstream zerops.yaml — structure + comments

### Structure

**Jetstream zerops.yaml (183 lines, ~38.5% comment density [codex-precise]):**
- 3 setups: `base` + `prod extends base` + `dev extends base`
- `base` defines all the shared env vars (50+ envVariables) once
- `prod` and `dev` only override what differs (build commands, deploy/run-time-only knobs, debug flags)
- DRY architecture via `extends:`

**Run-35 apidev/zerops.yaml (176 lines, 46.6% comment density):**
- 2 setups: `prod` + `dev`, no shared `base`
- env vars repeated between `prod.run.envVariables` and `dev.run.envVariables` (only `NODE_ENV` differs)
- Flat architecture — every block is read top-to-bottom by porter

### User direction (from handoff)

> "Skip `extends:` in yaml — recipe yaml is read top-to-bottom by porter; flat yaml beats DRY-with-indirection."

**This is a deliberate divergence from jetstream.** Run-35 follows the user's explicit direction. The cost is comment density (+8pp vs golden) because explanatory comments must compensate for the visual repetition between setups.

### Comment substance — both at-bar

Both files use comments to explain WHY for each non-obvious block. Examples:

**Jetstream:**
> # Cache dependencies for faster incremental builds.
> # For more information, please visit: https://docs.zerops.io/features/build-cache

**Run-35:**
> # Cache node_modules between builds — unchanged packages skip
> # re-download from the registry, so cold-build time drops to
> # roughly the `nest build` step alone after the first deploy.

Run-35 is more verbose per comment block; both correctly answer "why this block exists".

**Failure-mode coverage:**
- Jetstream comments are mostly mechanism-explanation
- Run-35 comments include several FAILURE-MODE warnings inline (self-shadow trap at L66-69, MEILI_HOST scheme trap at L92-95, S3_REGION SDK constraint at L70-72) — pre-empts the gotcha
- Both teach the same shape (mechanism); run-35 also pre-empts traps inline

### Verdict
**Architecture deliberately divergent (per user direction); comment density above golden as cost; comment substance at-or-above golden.** Not a defect — a documented design choice. Comment density is the still-loose item user explicitly deferred.

---

## 7. apidev/CLAUDE.md vs jetstream apps-repo CLAUDE.md

### Jetstream CLAUDE.md (32 lines)

```
# laravel-jetstream-app
<1-line description: Laravel 11 + Jetstream 5 (Inertia/Vue 3) recipe on Zerops
with PostgreSQL, KeyDB/Redis, S3-compatible object storage, Mailpit — shared
base setup extended by prod and dev>

## Zerops service facts
- HTTP port: 80 (Nginx + PHP-FPM; custom site.conf.tmpl)
- Siblings: db (PostgreSQL), redis (KeyDB), storage (Object Storage),
  mailpit (SMTP) — env vars follow ${hostname_*} pattern
- Runtime base: php-nginx@8.4 (build on php@8.4 + nodejs@22; dev runtime
  installs Node 22 via prepareCommands)

## Zerops dev (hybrid)
Runtime (php-nginx) auto-serves PHP changes immediately…
The dev setup intentionally skips `npm run build` in buildCommands…
public/build/manifest.json does NOT exist after a fresh deploy, so any
view using @vite(...) throws HTTP 500…
After every zerops_deploy on this service and BEFORE running zerops_verify,
do ONE of:
- One-shot build — ssh appdev 'cd /var/www && npm run build'
- Long-running dev server (HMR) — ssh appdev 'cd /var/www && nohup npm run dev…'
Do NOT add `npm run build` to dev buildCommands — adds 20-30s, defeats HMR-first.
All platform operations go through the Zerops development workflow via zcp
MCP tools. Don't shell out to zcli.

## Notes
- No dedicated worker setup…
- Dev installs Node 22 on the runtime container via prepareCommands…
- Artisan caches and migrate run in prod initCommands, not buildCommands…
- Before php artisan down, run zsc health-check disable…
```

### Run-35 apidev/CLAUDE.md (per system-reminder injection earlier in session)

```
# nestjs-showcase-api
<1-line description: NestJS 10 HTTP API (TypeScript, Express adapter) backing
the showcase. Persists items in PostgreSQL via TypeORM, fronts a Valkey cache,
publishes jobs to NATS, stores uploads through the AWS S3 SDK, and indexes
documents into Meilisearch. Routes are mounted under /api except for /health
and /ready.>

## Build & run
- npm run build — compile TypeScript to dist/ via nest build.
- npm run start — run the compiled server (node dist/main.js).
- npm run start:dev — nest start --watch for local iteration.
- npm run start:debug — watch mode with the Node inspector attached.
- npm run start:prod — alias of start, runs the compiled bundle.
- npm run format — Prettier over src/**/*.ts.
- npm run migrate — ts-node runs src/scripts/migrate.ts (creates the items
  table and pgcrypto extension).
- npm run seed — ts-node runs src/scripts/seed.ts (inserts seven demo rows
  when empty, then re-indexes into Meilisearch).
- npm run migrate:prod / npm run seed:prod — same scripts off the compiled
  dist/ output.

## Architecture
- src/main.ts — Nest bootstrap: trust-proxy, CORS (with X-Cache /
  X-Cache-Elapsed-Ms exposed), global /api prefix excluding /health and /ready,
  listens on 0.0.0.0:${PORT||3000}.
- src/app.module.ts — root module: ConfigModule.forRoot({ isGlobal: true }),
  TypeORM Postgres root connection, and the feature modules…
- src/items/, src/cache/, src/broker/, src/queue/, src/storage/, src/search/,
  src/health/, src/scripts/datasource.ts, src/scripts/migrate.ts, …
- nest-cli.json — Nest CLI config (sourceRoot: src, deleteOutDir: true).
- tsconfig.json / tsconfig.build.json — TypeScript build configuration.
```

### Diff — fundamentally different design

| Axis | Jetstream | Run-35 |
|---|---|---|
| Zerops content | Yes — `## Zerops service facts` H2, hybrid-dev workflow, zcp MCP tool reference | None — pure framework facts |
| Dev-workflow gotchas | Yes — Vite manifest 500 + 2-option recovery (one-shot build / long-running dev server); `php artisan down` + `zsc health-check disable` ordering | None — only npm script catalog |
| File-by-file architecture map | Brief — by sibling | Detailed — every src/ subfolder with controller/service mounts |
| Length | 32 lines | 25 lines |
| Audience | Anyone running `claude` to operate the recipe — both framework + Zerops | Anyone running `claude /init` on a fresh codebase — framework only |

**This is a deliberate design split.** Run-35's substrate explicitly says claudemd-author sub-agents are "Zerops-free. Brief is strictly platform-free; agent reads package.json / src/* directly and produces /init-style output." The Zerops side of the operating concern lives in `apidev/README.md` (which is the Zerops-aware doc).

Jetstream conflates the two concerns into one CLAUDE.md; run-35 splits them — README is Zerops-aware, CLAUDE.md is `claude /init`-shape.

**Cost of the split:** if a porter is using `claude` to operate the codebase and runs into a NestJS-watch / TypeORM-migration / Meilisearch-redeploy issue, run-35's CLAUDE.md doesn't help directly — they have to discover apidev/README.md. Jetstream's CLAUDE.md gives them the Vite-manifest gotcha + `php artisan down` ordering inline.

For the apidev codebase, the Zerops-aware content already lives in `## Recipe features` + `## Production vs. Development` + `### Gotchas` sections of the README. So the CLAUDE.md split makes sense: porter starts from the README (which contains the trap-warnings) and uses CLAUDE.md to navigate the codebase architecture.

### Verdict
**Deliberate divergence; design coherent.** Different from jetstream but not below it — different two-doc layout where each doc owns half the audience-model concern. Jetstream's all-in-one is denser; run-35's split is cleaner per-doc.

If goal is "match jetstream", the run-35 CLAUDE.md should pull in the trap-warnings (TypeORM synchronize, NestJS watch lifecycle, Meilisearch reindex on redeploy) and add a `## Zerops service facts` H2 with port/siblings/runtime base. But that would partially duplicate the README KB section.

---

## 8. Big-picture summary

Run-35's audience model is **deliberately denser** than jetstream's across most surfaces:
- IG items 2-7× longer
- Per-service yaml comments 2-3× longer with TY2 BAD-pattern voice
- Codebase zerops.yaml comments +8pp density (partly architectural choice)
- Tier-README intros lead with implementation specs rather than tier mission

The density isn't always wrong — run-35's IG#2-#5 explanations + KB intersection-traps are *more rigorous* than jetstream's brief paragraphs and point-to-source links. A porter who reads the doc cold gets more from run-35; a porter who scans-to-paste gets more from jetstream.

The TY2 voice gap (operational-engineer descriptor + mechanism rather than imperative + role + audience-purpose) is **the most concrete and most-repeated persistent below-bar item** [editorial framing — the gap is real and verified across tier intros + tier yaml head comments + tier yaml per-service comments; "single most" is judgment, not artifact-derivable]. It surfaces at:
1. Tier-README intros (lines 1-3 of every tier README)
2. Per-service yaml comments in every tier import.yaml
3. Codebase zerops.yaml block comments (less so — these are mechanism-driven so the descriptor-led voice is mostly fine)

Closing this voice gap would require the substrate to teach the **`<Tier name> environment <verb> <audience purpose>`** pattern explicitly + the **`Deploy/Spin up X, used by Y, both A+B connect`** pattern for managed-service comments. Currently the substrate (TY2 BAD/GOOD examples) tries to teach this but the agent under-applies.

The KB shape divergence (intersection-trap bullets vs porter-workflow narrative) is **a real audience-model fork**. Picking one to align run-35 onto requires a product decision: is the apps-repo README primarily for a **first-time porter evaluating the recipe** (intersection-trap shape useful), or for a **porter operating the deployed recipe** (workflow-tip shape useful)?

The CLAUDE.md split (Zerops-aware in README, framework-only in CLAUDE.md) is a coherent design choice that diverges from jetstream's all-in-one — neither inferior; design preference.

The yaml `extends:` abandonment is per user direction — flat-beats-DRY is the explicit policy.

The V3-axis title shape (`# nestjs-showcase — api`) is a clean defect — easy to fix at the cc-content brief level (currently the title is templated by the engine; would need `## <human title>` injection).

The root recipe README's missing porter-meta one-liner is a clean defect — small fix in the env-content brief / refinement.

---

## 9. Concrete prioritized list — fix order if pushing run-35 closer to jetstream

| # | Item | Cost | Impact |
|---|---|---|---|
| 1 | Tier-README intro voice — add `**Tier name** <verb> <audience purpose>` lead pattern with implementation specs as second sentence | Substrate edit (env-content brief, T1-T4 rule) | High — closes the most visible voice gap; matches jetstream tier intros 1:1 |
| 2 | Tier import.yaml head comment — mirror the README intro | Substrate edit (env-content brief, TY1 rule) | Medium — porter sees same voice in both places |
| 3 | Per-service yaml comment voice — `Deploy/Spin up X, used by Y` shape | Substrate edit (env-content brief, TY2 rule) | High — closes TY2 BAD pattern at every tier |
| 4 | Root recipe README — add porter-meta one-liner ("Offered in examples for the whole development lifecycle…") | Substrate edit (env-content brief, R0 rule) | Low — small but visible |
| 5 | apidev README title — drop slug-stem, use jetstream `# Zerops x <NAME marker> <human name> <NAME marker>` shape | Engine + brief edit (V3 rule + cc-content title-block teaching) | Low — clean V3-axis closure |
| 6 | KB sibling-shape consistency — pick one (intersection-trap H3 OR `## Tips and Others` H2 → narrative `### <topic>` H3) and engine-gate it | Engine teeth (cc-content complete-phase gate) | Medium — closes the sim-3 win loss; matches jetstream OR sim-3 baseline |

Items 1-4 are substrate-only — agent compliance is the failure mode. The
v9.77.0 record-fact gate proves engine-teeth-beats-substrate-teaching where
both apply. If items 1-4 don't land in the next dogfood, escalate to engine
teeth.

Item 5 (title) is one engine line + one brief line — cheapest fix, largest
visibility gain.

Item 6 is the only place where current run-35 is below sim-3 — engine gate
recommended per user's "engine teeth on per-case basis" authorization.

---

## 10. Recommendation

**Path C — measured ladder, not full alignment.** Run-35 is at-or-above
jetstream in technical-rigor + intersection-trap-coverage axes. The gaps
that matter most to the audience-model bar are the **voice axis** (tier
intros + yaml per-service comments) and the **KB sibling consistency
loss**. Address those two; leave the deliberate divergences (yaml
`extends:` abandonment, CLAUDE.md split, KB intersection-trap shape if
that's the chosen audience) intact.

If sim-replay against captured run-35 facts is clean, ship v9.77.0 with
the audience-model gaps documented as next-iteration items. The
voice-axis fix is substrate-only first; promote to engine teeth if the
following dogfood doesn't land it.

The IG length gap (2-7× jetstream) is *not* recommended for closure —
run-35's verbosity buys actual rigor (failure mode + fix + code per
item). Trimming to jetstream length would cost teaching value.
