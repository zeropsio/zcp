# Recipe Accuracy Fixes — First-Principles Implementation Guide

Post-mortem synthesis from `nestjs-showcase-v4` (first successful dual-runtime
recipe run on v8.49). Four independent structural errors were discovered.
Fixing them correctly — from first principles, not patches — doubles recipe
accuracy and eliminates the entire class of "verification sub-agent invents
platform concepts" failures.

## Contents

- [Error 1: Serve-only runtimes were allowed as dev `run.base`](#error-1)
- [Error 2: Framework-specific build-time URL baking was prescribed as guidance](#error-2)
- [Error 3: Verification sub-agents review platform config they cannot understand](#error-3)
- [Error 4: Recipe-specific knowledge is not captured for reuse](#error-4)
- [Implementation order](#implementation-order)
- [Out of scope](#out-of-scope)

---

## Error 1 — Serve-only runtimes were allowed as dev `run.base` {#error-1}

### First principle

The purpose of `setup: dev` is an **SSH-able workspace where the agent edits
source in place and runs the framework's dev loop**. For this to work, the
container at `run.base` must be able to host the dev toolchain: shell, package
manager, file editors, and whatever process the framework uses for hot reload
(`npm run dev`, `php artisan serve`, `bun --hot`, `cargo watch`, etc.).

Some runtimes are **serve-only** by design: `static` runs Nginx and serves
files, has no Node/Python/Ruby, and cannot evaluate source. For these
runtimes, `run.base: static` is a **prod-only concern**. The dev setup must
use a different `run.base` — one that CAN host the dev toolchain.

This is not a dual-runtime rule, a Svelte rule, or an API-first rule. It's a
property of the runtime itself. It applies whenever any recipe's prod artifact
runs on a serve-only runtime.

### What broke in v4

The main agent followed v8.49 guidance that said:

> **Exception for `run.base: static`** — a static container serves only the
> compiled bundle, there is no runtime evaluation. A static dev setup MUST
> still run `npm run build` and set `deployFiles: dist/~`.

This guidance was backwards. It applied static-runtime behavior to the dev
setup, which meant:

1. Dev deploys flattened the compiled bundle into the mount root, **wiping
   source files** on every deploy.
2. The agent had no Node in the dev container because `run.base: static`, so
   it couldn't run `npm run dev` or `npm install` there.
3. Workaround: the agent authored SPA source on the adjacent `apidev` (which
   had Node), tar-piped to `/var/www/appdev/`, then deployed. This is fragile
   — any `apidev` redeploy wiped the scratch copy.
4. Workaround to the workaround: canonical source at
   `/home/zerops/spa-source/` on the zcp container itself (not on any mount).
5. Every iteration required a sync-tar-deploy dance.

### Structural fix

**Generalize the dev-setup rule in `internal/content/workflows/recipe.md`
(section "`setup: dev`") to distinguish serve-only runtimes:**

The current rule mandates `deployFiles: [.]` on dev with an exception for
static. The correct rule is one level higher:

> `setup: dev` MUST give the agent a container that can host the framework's
> dev toolchain. For dynamic runtimes (nodejs, python, php-nginx, go, rust,
> bun, ubuntu, …), `run.base` is the same as prod and `deployFiles: [.]`
> preserves source across deploys. For serve-only runtimes (`static`,
> standalone `nginx`, any future serve-only base), the dev setup MUST pick a
> different `run.base` that can host the toolchain — typically the same base
> that already exists under `build.base` for that setup. `run.base` may
> differ from prod's `run.base` within the same zerops.yaml; the platform
> supports this and it's the intended pattern for serve-only prod artifacts.
> `deployFiles: [.]` still applies on dev regardless of `run.base` choice.

**Delete the v8.49 "Exception for `run.base: static`" paragraph entirely.**
It was a wrong fix based on a wrong diagnosis. Its replacement is the general
rule above.

### Concrete edit

- File: `internal/content/workflows/recipe.md`
- Section: `### zerops.yaml — Write ALL setups at once` → `**\`setup: dev\`**` bullet list
- Action: replace the first bullet (`deployFiles: [.]` — **MANDATORY for
  self-deploy on dynamic runtimes** …) with the two-part rule above. No new
  examples, no framework names, no VITE references.

### Regression guard

No code change, no checker — the rule can't be mechanically enforced because
"can this runtime host a dev toolchain?" is a runtime-category question the
schema doesn't expose. Guard via the existing structural comment test on
recipe.md (the comment ratio and section presence tests already cover it).

---

## Error 2 — Framework-specific build-time URL baking was prescribed as guidance {#error-2}

### First principle

An SPA bundle is an artifact. Any value the artifact depends on is either a
**build-time input** (fixed in the artifact) or a **runtime input** (variable
per container). Which one an SPA uses is a **property of the SPA's code**,
not a platform mandate.

Three code shapes, three Zerops patterns:

1. **Build-time baked** (`import.meta.env.VITE_*`, webpack `DefinePlugin`).
   Value is a literal string in the bundle. Same bundle can only serve one
   value. Requires either one build per environment OR a
   deploy-time-substituted literal placeholder.
2. **Runtime config file** (`fetch('/config.json')` on app startup). Bundle
   is identical across environments. Config file is generated/substituted
   per container at deploy time or first boot.
3. **Runtime window global** (`window.__CONFIG__` injected via a `<script>`
   tag in `index.html`). Bundle is identical; index.html is substituted
   per deploy.

Shape #1 is Vite-specific. Shapes #2 and #3 are framework-agnostic.
**Choosing between them is a recipe decision**, not a platform rule.

Zerops provides one relevant primitive: `run.envReplace`, which performs
deploy-time placeholder substitution in deployed files. It works with any
runtime including `static`. It's orthogonal to which code shape the recipe
chooses.

### What broke in v4

v8.48 recipe.md guidance contained this block:

```
The frontend's `build.envVariables` constructs the API URL from known parts:
VITE_API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app
```

This committed every API-first recipe to **code shape #1 with build-time
baking AND a single hardcoded prod-tier hostname**. Consequences:

1. The bundle baked `api-` (env 2-5 hostname). In envs 0-1, the paired
   service is `apistage`, not `api`. Appstage's bundle hit a nonexistent
   `api-${host}-3000` URL and got 502s.
2. The v4 main agent's fix (after the verification sub-agent misreported
   the problem — see Error 3) was to invent `setup: stage` with a different
   baked URL. This invented platform concept doesn't exist, breaks the
   hostname convention, and was wrong.
3. No recipe in the knowledge base demonstrates shape #2 or #3, so future
   agents will default to shape #1 indefinitely.
4. The "construct from known parts" example is Vite-syntax
   (`${zeropsSubdomainHost}`) which isn't even a general Zerops primitive —
   it's a build-context variable usable only inside `build.envVariables`.

### Structural fix

**Delete the entire "construct VITE_API_URL from known parts" block** from
recipe.md. It's framework-specific prescription dressed as general guidance.

**Replace it with a platform-level statement of what dual-runtime recipes
need to expose between services, and leave the "how" to the recipe's own
integration guide:**

> In dual-runtime recipes, the frontend needs to know the API's public URL
> for the environment it's running in. The API's public URL is different
> per environment tier (env 0-1 pairs `appdev`/`apidev` and
> `appstage`/`apistage`; env 2-5 uses single-container `app` and `api`).
>
> Zerops supports two patterns for supplying per-environment values to a
> service:
>
> 1. **Project-level env var** — define the value in `project.envVariables`
>    in each environment's `import.yaml`. Every service in that project sees
>    it. Different environments supply different values.
> 2. **Service-level env var** — define the value in the service's entry in
>    `import.yaml`. Visible only to that service. Use this when two services
>    in the same environment need different values (e.g., env 0-1 where
>    `appdev` points at `apidev` and `appstage` points at `apistage`).
>
> Whether the frontend reads the URL at build time (baked into the bundle)
> or at runtime (via a config file, a window global, or server-side
> templating) is a framework-level decision and belongs in the recipe's own
> README integration-guide, not in this workflow. When baking is chosen,
> Zerops's `run.envReplace` can substitute a build-time placeholder with a
> deploy-time value so the same built bundle serves different environments.
> See the chain-loaded predecessor recipe's knowledge file for the specific
> pattern used by the framework you're building.

This moves the framework decision out of the workflow and into **per-recipe
knowledge**, which is where it belongs. See Error 4 for how that knowledge
file gets authored.

### Concrete edits

- File: `internal/content/workflows/recipe.md`
- Section: `### zerops.yaml — Write ALL setups at once` → `**Dual-runtime zerops.yaml**` block
- Action: delete lines referencing `VITE_API_URL: https://api-${zeropsSubdomainHost}-3000...`
  and the "Components: hostname (`api`, defined in import.yaml) + …" explanation.
  Replace with the scope-level statement above.

- Same file, section: `### Deploy` → "Step 1-API" and "Step 3-API" blocks
- Action: remove phrases like "the frontend builds with the API URL baked in"
  which assume shape #1. Replace with neutral wording like "the frontend
  needs the API running before it can deploy (in build-time-baked
  configurations) or before it can be verified (in runtime-config
  configurations)".

- Same file, section: "Dual-runtime (API-first) additional checks" inside
  the verification sub-agent brief
- Action: delete the line `> - Does the frontend's \`VITE_API_URL\` build
  variable correctly construct the API URL?`. This line is half of what
  produced the `stage` invention. The other half is Error 3.

### Regression guard

After editing, run a full grep for `VITE_API_URL`, `zeropsSubdomainHost`,
`Vite`, `vite` in recipe.md and bootstrap.md. The only remaining mentions
should be inside code block examples that explicitly demonstrate a
framework-agnostic concept. Nothing prescribing a specific Vite shape
belongs in workflow files.

---

## Error 3 — Verification sub-agents review platform config they cannot understand {#error-3}

### First principle

Sub-agents spawned via the Task tool receive **no injected context**. They
see only the main agent's brief and their own training data. For the
verification sub-agent:

- The main agent has the full Zerops platform knowledge (bootstrap.md,
  recipe.md, chain-loaded predecessor recipe knowledge, live JSON schemas,
  platform rules delivered at provision time).
- The sub-agent has none of this. Its knowledge of Zerops is whatever was
  in its training data when it was trained, which is stale and shallow.
  It's been trained deeply on frameworks: NestJS conventions, Svelte
  patterns, TypeORM migrations, React hooks, Laravel artisan commands.

**The verification sub-agent is a framework expert, not a platform expert.**
Asking it to review `zerops.yaml`, `import.yaml`, `buildFromGit`, `zeropsSetup`,
`envReplace`, `corePackage`, or any Zerops primitive sets it up to either:

1. Apply stale/hallucinated platform knowledge ("I think stage environments
   need a `setup: stage`"), or
2. Reach for framework-shaped solutions to platform problems ("add a new
   environment variant"), or
3. Flag things as wrong that are actually correct because its training data
   predates the feature (v4: flagging Svelte 5 / Vite 8 / TypeScript 6 as
   "unreleased majors").

The main agent has already written the platform config with full context.
The finalize step's automated checks validate that config against the live
schema. The verification sub-agent's unique contribution is **code-level
framework expertise** the automated checks and the main agent can miss.
That's what it should focus on, and nothing else.

### What broke in v4

The sub-agent brief in recipe.md (section "Verification Sub-Agent") asks
the {framework} expert to review:

- `setup: dev` and `setup: prod` build/deploy/run config
- `healthCheck` and `deploy.readinessCheck`
- `deployFiles` completeness
- Zerops env var wiring
- `us-east-1` S3 region env var
- `run.base` vs `build.base` distinction
- All 6 import.yaml files
- `buildFromGit` presence on runtime services
- `startWithoutCode` absence
- Hostname suffixing conventions
- `corePackage: SERIOUS` placement
- `envSecrets` scoping
- Scaling matrix
- Service type versions
- `VITE_API_URL` build variable construction
- Priority ordering

**Every item in this list is a Zerops platform concern** the sub-agent has
no context for. It's being asked to be a platform expert. The result in v4:
it flagged appstage as hitting the wrong URL (correct observation) and
recommended the fix as "add a `setup: stage`" (wrong solution, invented
concept). The main agent accepted the fix because it made the browser
verification pass, not realizing it had been led off-platform.

### Structural fix

**Split the verification sub-agent brief into two explicitly labeled halves
with a bright line between them:**

1. **Framework-expert scope** (the sub-agent reviews these directly and can
   suggest concrete fixes):
   - App code: controllers, services, models, entities, modules, routes,
     middleware, guards, pipes, interceptors, event handlers
   - Framework config files: `tsconfig.json`, `nest-cli.json`,
     `vite.config.ts`, `svelte.config.js`, `package.json` dependencies and
     scripts (but NOT the Zerops-consumed zerops.yaml fields)
   - Dead code, unused imports, unused dependencies, scaffold leftovers
   - `.env.example` completeness and key names matching framework conventions
   - Security: XSS in templates/components, input validation, framework
     auth patterns, secret leakage
   - Test files and framework-specific test patterns
   - Framework idiom: DI order, middleware ordering, async boundaries,
     error propagation
   - README framework sections describing what the app does and how its
     code is wired

2. **Symptom-only scope** (the sub-agent observes and reports; the main
   agent decides the fix):
   - Does the app actually work end-to-end in the browser? (This is the
     browser verification — the sub-agent's output is an observation like
     "appstage's connectivity panel shows API unreachable; console shows
     failed fetch to `https://api-20fe-3000.prg1.zerops.app`".)
   - Does anything in the running app behave differently between `appdev`
     and `appstage`? (Observation only.)
   - Does any feature section fail to render or interact? (Observation
     only.)
   - **The sub-agent reports symptoms. It does NOT propose fixes that
     touch zerops.yaml, import.yaml, or any Zerops primitive. If its
     investigation points to a platform-level cause, it SAYS SO and stops
     — the main agent fixes it.**

3. **Explicit exclusions** (never in the sub-agent brief):
   - zerops.yaml field review
   - import.yaml field review
   - `zeropsSetup`, `buildFromGit`, `envReplace`, `envSecrets`,
     `corePackage`, `cpuMode`, `mode`, `priority`, `verticalAutoscaling`,
     `minContainers`, scaling matrix
   - Hostname naming conventions
   - Env var cross-service references (`${hostname_varname}`)
   - Comment ratio / comment style
   - Service type version choice
   - Schema field validity
   - Anything already covered by the automated finalize checks

### Concrete edits

- File: `internal/content/workflows/recipe.md`
- Section: `### 1. Verification Sub-Agent (ALWAYS — mandatory)` → "Sub-agent
  prompt template" block
- Action: rewrite the brief as follows.

New brief template:

```
> You are a {framework} expert reviewing the CODE of a Zerops recipe.
> You have deep knowledge of {framework} but NO knowledge of the Zerops
> platform beyond what's in this brief. Do NOT review platform config
> files (zerops.yaml, import.yaml) — the main agent has platform context
> and has already validated them against the live schema. Your job is to
> catch things only a {framework} expert catches.
>
> **Read and review (direct fixes allowed):**
> - All source files in {appDir}/ — controllers, services, models,
>   migrations, modules, templates/views, client-side code
> - Framework config: tsconfig, build config, lint config, package.json
>   dependencies and scripts (but NOT the Zerops-managed zerops.yaml)
> - .env.example — are all required keys present with framework-standard
>   names?
> - Test files — do they exercise the feature sections, or are they
>   scaffold leftovers?
> - README framework sections (what the app does, how its code is wired).
>   Do NOT review the README's zerops.yaml integration-guide fragment —
>   that's platform content the main agent owns.
>
> **Framework-expert review checklist:**
> - Does the app actually work? Check routes, views, config, migrations,
>   framework conventions (env mode flag, proxy trust, idiomatic patterns).
> - Is there dead code, unused dependencies, missing imports, scaffold
>   leftovers?
> - Are framework security patterns followed? (XSS-safe templating, input
>   validation, auth middleware order, secret handling)
> - Does the test suite match what the code does?
> - Are framework asset helpers used correctly (not inline CSS/JS when a
>   build pipeline exists)?
>
> **Browser walkthrough (showcase only — MANDATORY):**
> Use agent-browser to open the live dashboard on BOTH appdev and
> appstage subdomains. For each: walk every feature section, interact
> with every control, open the browser console, check the network tab.
> Report, for each subdomain:
> - Connectivity panel state (services connected, latencies)
> - Each feature section's render state (populated, empty, errored)
> - Console error count and exact messages (expected: zero)
> - Failed network request count and exact URLs (expected: zero)
>
> **Symptom reporting (NO fixes):**
> If anything in the browser walk points to a platform-level cause (wrong
> service URL, missing env var, CORS failure, container misrouting,
> deploy-layer issue), STOP and report the symptom. Do NOT propose
> zerops.yaml, import.yaml, or platform-config changes. The main agent
> has full Zerops context and will fix platform issues. Your report on
> a platform symptom should be shaped like: "appstage's console shows
> `Failed to fetch https://api-20fe-3000.prg1.zerops.app/status`. This
> URL appears to target a service named `api` which doesn't exist in
> the running environment (only `apidev` and `apistage` do). Platform
> root cause unclear — main agent to investigate."
>
> **Out of scope (do NOT review):**
> - zerops.yaml fields — build.base, run.base, healthCheck,
>   readinessCheck, deployFiles, buildFromGit, zeropsSetup, envReplace,
>   envSecrets, cpuMode, mode, priority, verticalAutoscaling,
>   minContainers, corePackage, anything prefixed with zsc
> - import.yaml fields — any of them
> - Service hostname naming, suffix conventions, env-tier progression
> - Schema-level field validity
> - Comment ratios or comment style in platform files
> - Zerops platform primitives you haven't seen before — don't guess,
>   don't invent new ones, don't suggest fixes that would introduce them
>
> Report issues as:
>   [CRITICAL] (breaks the app), [WRONG] (incorrect code but works),
>   [STYLE] (quality improvement), [SYMPTOM] (observed behavior that
>   might have a platform cause — main agent to investigate).
```

Key changes from current brief:

- Framed sub-agent as "code reviewer" not "recipe reviewer"
- Removed every zerops.yaml / import.yaml review item
- Removed the "Dual-runtime additional checks" block including the
  VITE_API_URL line
- Added explicit "out of scope" section naming the platform primitives
- Added `[SYMPTOM]` severity for observations with platform root causes
- Made "do not propose platform fixes" an explicit rule with an example
  of the correct report shape

### Regression guard

After the edit, re-read the brief and confirm **zero mentions** of:
zerops.yaml field names, import.yaml field names, zsc, zeropsSetup,
buildFromGit, envReplace, corePackage, VITE_API_URL, or any specific
framework-config syntax the main agent would be responsible for.

---

## Error 4 — Reference Loading mis-names the hello-world for static frontends {#error-4}

### First principle

The research step's Reference Loading block is the ONLY mechanism that
tells the agent which predecessor recipe's knowledge to load. Its slug
rule has to resolve to a file that actually exists in
`internal/knowledge/recipes/`, or the agent loads nothing (or the wrong
thing) and the whole "chain-load lower tiers" model silently breaks.

### What broke in v4

Reference Loading said:

> One exists per runtime — match the base runtime, not the framework
> name. Example: for a php-nginx framework, load `php-hello-world`. For
> a nodejs framework, load `nodejs-hello-world`.

For a Svelte static SPA:
- The prod `run.base` is `static`.
- There is **no** `static-hello-world.md` — `static` is a framework-free
  Nginx runtime, so a generic hello-world doesn't make sense.
- What actually exists is framework-named: `svelte-hello-world`,
  `sveltekit-static-hello-world`, `react-static-hello-world`,
  `vue-static-hello-world`, `angular-static-hello-world`, etc.

Following the rule verbatim, the v4 agent building a Svelte frontend
either loaded nothing (no `static-hello-world`) or loaded the wrong thing
(`nodejs-hello-world`, which is a Node *server* recipe with no SPA
patterns). Either way, the file that would have demonstrated the
dev-uses-nodejs-not-static pattern (svelte-hello-world.md, which already
uses `run.base: nodejs@22` for dev) was never loaded.

The knowledge was already there. The lookup was broken.

### Structural fix

Rewrite the Reference Loading slug rule in recipe.md to correctly name
the three cases:

1. **Dynamic-runtime frameworks** (backends, SSR) — use `{runtime}-hello-world`.
2. **Static-frontend frameworks** (SPAs, SSGs) — use
   `{framework}-static-hello-world` (or legacy `{framework}-hello-world`
   for the few that predate the naming convention). There is NO generic
   `static-hello-world`.
3. **Dual-runtime (API-first) recipes** — load BOTH: the backend's
   runtime hello-world AND the frontend's static hello-world. Each
   chain-load teaches one side of the dual-runtime pattern.

### Concrete edits

- File: `internal/content/workflows/recipe.md`
- Section: `### Reference Loading` → `**1. Hello-world**` block
- Action: replace the runtime-only slug rule with the three-case rule
  above, and add the dual-runtime "load both" instruction.

No new files. No new Go code. No new tests.

### What was explicitly NOT done here (and why)

The original plan proposed hand-authoring
`internal/knowledge/recipes/nestjs-showcase.md` and
`internal/knowledge/recipes/svelte-spa.md` from v4's TIMELINE. This was
wrong on two counts:

1. **That directory is the sync cache, not hand-authored content.**
   `internal/knowledge/recipes/*.md` is gitignored — the files are
   pulled from the live Zerops API via `zcp sync pull recipes`, which
   fetches the README fragments of recipes **already published** to
   `zeropsio/recipes` → Strapi. Hand-writing files there would get them
   wiped on the next sync. The canonical source of truth is the
   published recipe.
2. **nestjs-showcase knowledge should land via the normal publish
   flow**, not by bypassing it. When the v4 NestJS showcase is
   published via `zcp sync recipe publish nestjs-showcase …`, the
   README's knowledge-base extract becomes the machine-loadable
   knowledge automatically. No hand-authoring required.
3. **svelte-spa knowledge already exists** under the existing
   framework-static hello-world files (`svelte-hello-world`,
   `sveltekit-static-hello-world`, etc.). v4's failure to benefit from
   them wasn't a content gap — it was a lookup gap, which this Error 4
   fix closes.

Publishing the v4 NestJS showcase is a separate follow-up task. It is
NOT in scope for this plan.

---

## Implementation order

1. **Error 3 first** (verification sub-agent brief). Highest leverage —
   without this, even a perfect platform fix can be re-broken by the next
   sub-agent that reviews platform config it doesn't understand.
2. **Error 1 second** (dev runtime rule). Makes every future dual-runtime
   recipe run actually iterable.
3. **Error 2 third** (remove VITE prescription). Clears the way for
   Error 4's knowledge files to own the framework-specific guidance.
4. **Error 4 last** (Reference Loading slug rule). Single-block edit in
   the same recipe.md file as phases 1-3; can ship with them.

All four phases are single-file edits to `internal/content/workflows/recipe.md`.
No new files. No Go code changes. No schema changes. No new checkers.
No new tests.

Commit each phase separately so regression risk is scoped and the diff
for each structural statement is reviewable in isolation.

## Out of scope

Things that look related but are NOT part of this plan:

- **Adding a `run.base` checker that rejects serve-only bases on dev
  setups.** Could be done but requires a runtime-category list the schema
  doesn't provide. Not worth hardcoding. The rule is clear enough in
  guidance; the agents will follow it.
- **Auto-generating the per-recipe knowledge file from TIMELINE.md.**
  Tempting but the TIMELINEs mix durable lessons with one-off incidents
  (e.g., v4's agent-browser fork exhaustion). A human/LLM distillation
  pass is the right scope boundary.
- **Changing anything in `internal/sync/`, `internal/eval/`, or
  `internal/tools/`.** These are platform mechanics that worked correctly
  in v4. The errors were all in the guidance layer.
- **Changing the hostname naming convention or the env-tier progression.**
  These were correct in v4 and would break backward compatibility with
  every existing recipe.
- **Adding a `setup: stage`.** There is no `setup: stage`. Stage uses
  `prod`. This plan's entire purpose is to ensure no future run invents
  one.
