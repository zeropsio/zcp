# Content Surfaces — Classification & Routing Spec

**Authoritative reference** for what content belongs on which recipe surface, how to classify observations from a run, and how to route each classified fact to exactly one destination.

This spec exists because recipe content quality has drifted below the bar across v20–v28 despite passing every token-level check. The root cause is that the agent which debugs the recipe also writes the reader-facing content, and after 85+ minutes of debug-spiral its mental model is "what confused me" rather than "what a reader needs." This spec formalizes the reader-facing purpose of each surface so content authoring can be tested against it.

The spec is the ground truth that the content-authoring sub-agent (see [implementation-v8.94-content-authoring.md](implementation-v8.94-content-authoring.md)) reads as part of its brief, and the ground truth that editorial reviews evaluate against. It is also the source of truth for `internal/recipe/surfaces.go::SurfaceContract` — every surface's `FormatSpec` field anchors into a section of this file by URL fragment, so the heading anchors are load-bearing.

The empirical floor for every contract below is two reference recipes:

- [`/Users/fxck/www/recipes/laravel-jetstream/`](../../../recipes/laravel-jetstream/) + [`/Users/fxck/www/laravel-jetstream-app/`](../../../laravel-jetstream-app/) — human-authored, the readability + voice floor.
- [`/Users/fxck/www/recipes/laravel-showcase/`](../../../recipes/laravel-showcase/) + [`/Users/fxck/www/laravel-showcase-app/`](../../../laravel-showcase-app/) — early recipe-flow output, the mechanism-density floor.

Both references agree on the structural shape of every surface within ±20%. Where the contracts below name caps, those caps are observed in both references — not invented. When the contracts and a run's deliverable disagree, the deliverable is wrong.

---

## Why this exists — the content-quality failure mode

Every cross-version regression since v20 traces to a single pattern: the agent writes a **journal**, not a **reader-facing document**. Three sub-pathologies flow from this:

1. **Fabricated mental models.** When the agent can't explain a symptom (e.g., "why did apidev's self-shadow work but workerdev's didn't?"), it invents a mechanism. v23 shipped "Recovering `zsc execOnce` burn" as a fictional per-workspace burn; v28 shipped "the interpolator resolved before the shadow formed" as a fictional timing-dependent resolver. Both were written despite the agent having access to the correct platform teaching via `zerops_knowledge`.
2. **Wrong-surface placement.** The knowledge-base / gotchas fragment becomes a dumping ground for anything the agent stumbled over during scaffolding — NestJS's `setGlobalPrefix` collision, npm peer-dep errors, framework bootstrap API — content that is not Zerops-specific and does not belong on a Zerops recipe surface at all.
3. **Self-referential decoration.** The agent documents its own scaffold code as if it were a platform contract. "Our `api.ts` helper's content-type check catches SPA fallbacks" is the agent describing its own implementation in knowledge-base voice.

None of these are caught by token-level checks ("names a Zerops mechanism", "names a concrete failure mode"), because all three failure modes satisfy those patterns trivially. The correction has to happen at the mental-model layer — BEFORE content is authored.

---

## Per-surface line-budget table

Hard caps for every surface. Both reference recipes settle within these caps; run-14 violated three of them by 2-3×. The caps are part of every `SurfaceContract` and are enforced structurally at finalize.

| Surface | Hard line cap | Hard item cap | Reader |
|---|---|---|---|
| **Root README** | 35 lines total | 6 tier links + footer | Recipe-page browser |
| **Tier README extract** (between `<!-- #ZEROPS_EXTRACT_START:intro# -->` markers) | **1–2 sentences ≤ 350 chars** | n/a | Recipe-page tier-card hover |
| Tier README total (incl. body outside markers) | 10 lines (body optional; references leave empty) | n/a | Recipe-page browser |
| **Tier `import.yaml` comments** | 40 indented comment lines per tier | 3–5 lines per service block | Dashboard manifest reader |
| Apps-repo README intro extract (between markers) | 1–3 sentences ≤ 500 chars | n/a | Apps-repo browser |
| **Apps-repo Integration Guide** | n/a | **4–5 items per codebase** (incl. engine-emitted IG #1) | Porter bringing own code |
| **Apps-repo Knowledge Base / Gotchas** | n/a | **5–8 bullets per codebase** | Porter hitting a failure |
| Apps-repo CLAUDE.md | ~30–50 lines (no hard cap) | 2–4 H2 sections, `claude /init`-shape, Zerops-free | AI/human operating the repo |
| Apps-repo zerops.yaml comments | n/a | one comment block per directive group / per directive — author's choice | Porter editing the deploy config |

These are caps, not targets. Below the cap = clean. Over the cap = padding that signals the agent didn't understand the surface's purpose.

---

## The seven content surfaces

Each recipe has exactly seven kinds of content surface. Every fact lives on exactly ONE surface. Cross-surface references are fine; cross-surface duplication is not.

### Surface 1 — Root README

**Reader**: A developer browsing zerops.io/recipes deciding whether to click deploy.

**Purpose**: Name the services, name the environment tiers, show one-click deploy buttons.

**The test**: *"Can a reader decide in 30 seconds whether this recipe deploys what they need, and pick the right tier?"*

**Belongs here**:
- One-sentence intro between `<!-- #ZEROPS_EXTRACT_START:intro# -->` markers naming every managed service.
- Deploy button + cover image.
- Tier list with `[info]` link to the tier README and `[deploy]` link to the recipe page tier query.
- Pointer to the recipe-category page (`zerops.io/recipes?lf={framework}`) and Discord.

**Does not belong here**:
- Gotchas, debugging, code details (→ per-codebase README)
- Deployment shape decisions (→ env import.yaml comments)
- Architecture explanations beyond a single paragraph

**Structure (fixed)**:
```
# <Recipe Title>
<intro extract markers — 1 sentence>
Deploy button
Cover image
Tier list (6 entries)
Footer (related recipes link + Discord)
```

**Length**: 25–35 lines total.

---

### Surface 2 — Environment README

**Reader**: Someone hovering over a tier card on `zerops.io/recipes`. The recipe-page UI renders the content between `<!-- #ZEROPS_EXTRACT_START:intro# -->` and `<!-- #ZEROPS_EXTRACT_END:intro# -->` as the tier-card description.

**Purpose**: Be a tier-card description.

**The test**: *"Does a porter looking at six tier cards know which one to click?"*

**Belongs here (between extract markers)**:
- **1–2 sentences only, ≤ 350 chars.** State the tier's audience + the one defining property. Both reference recipes settle at this length.
- Examples:
  - *"Stage environment uses the same configuration as production, but runs on a single container with lower scaling settings."*
  - *"AI agent environment provides a development space for AI agents to build and version the app."*
  - *"Highly-available production environment provides a production setup with enhanced scaling, dedicated resources, and HA components for improved durability and performance."*

**Belongs here (outside extract markers, optional)**:
- Both reference recipes leave the body empty. If body content is added, it must NOT duplicate the intro and must NOT pad with ladder structures (Shape at glance / Who fits / etc.) that exist only to hit a line floor.
- A deploy button after the closing marker is fine.

**Does not belong here (anywhere)**:
- Ladder structures inside the extract markers (the recipe-page UI renders all of it).
- Tier-promotion narratives — neither reference recipe writes them; cross-tier shifts surface implicitly through the contrast between tiers.
- Service-by-service rationale (→ tier `import.yaml` comments).
- Framework quirks (→ per-codebase README / KB).
- Platform mechanisms (→ `zerops_knowledge` guides; cite, don't duplicate).

**Anti-pattern (run-14)**: 35-line ladder content (Shape at a glance / Who fits / How iteration works / What you give up) wrapped inside the extract markers. The recipe-page UI then renders 35 lines of ladder content as the tier-card description. Both reference recipes wrap a single sentence.

**Length**: 7–10 lines total. Body content (if any) caps at 5 additional lines after the closing marker.

---

### Surface 3 — Environment `import.yaml` comments

**Reader**: Someone who deployed this tier and is reading the manifest in the Zerops dashboard to understand what they're running.

**Purpose**: Explain every decision — service presence, scale, mode — at this tier.

**The test**: *"Does each service-block comment explain a decision (scale, mode, why this service exists at this tier), not just narrate what the field does?"*

**Belongs here**:
- **Why this service at this tier** (why db is NON_HA on stage, why worker is HA on env 5).
- **Why this scale** (throughput vs HA rationale for `minContainers: 2`, cost-trade rationale for single replica).
- **Why this mode** (NON_HA durability trade-off, HA failover cost justification).
- **Tier-specific env-var reasoning** (why `DEV_*` + `STAGE_*` constants exist at project level).

Cross-tier shifts surface implicitly through the contrast between adjacent tier yamls — neither reference recipe writes "promote to tier N when…" sentences in service blocks. Don't.

**Does not belong here**:
- App-code details, framework quirks, library versions (→ per-codebase zerops.yaml comments or README).
- Cross-codebase contract explanations (→ workerdev README or IG).
- General Zerops platform facts (→ `zerops_knowledge` guides; cite don't duplicate).
- Authoring-process language ("recipe author", "during scaffold", "we chose"). Comments speak about the porter's deployed runtime, never about the agent that wrote the yaml.
- Fabricated yaml field names. Every field-shaped token in a comment must exist as a key path in the yaml below; `project_env_vars` (snake_case) is wrong when the schema uses `project.envVariables` (camelCase, nested).

**Length**: ≤ 40 indented comment lines per tier; 3–5 lines per service block, max 8.

**Voice**: mechanism + reason in one breath. *"PostgreSQL HA — replicates data across multiple nodes so a single-node failure causes no data loss or downtime."* (mechanism: HA replication; reason: failure tolerance.)

**Comment granularity**: author's choice. Block-mode comments above a directive group AND single-line comments inline above a specific field both work. Both reference recipes mix the two freely. The rule is "explain the choice, never narrate the field name" — granularity follows from what makes the choice clear.

**Anti-pattern**: Templated per-service opening repeated across services ("enables zero-downtime rolling deploys" appearing verbatim on app, api, worker blocks). Each block's reasoning is service-specific.

---

### Surface 4 — Per-codebase README: Integration Guide fragment

**Reader**: A porter bringing their own existing application (Svelte app they built, NestJS API they built). They are NOT using this recipe as a template — they are extracting the Zerops-specific steps to adapt their own code.

**Purpose**: Enumerate the concrete changes a porter must make in their own codebase to run on Zerops.

**The test**: *"Does a porter who ISN'T using this recipe as a template, but bringing their own code, need to copy THIS exact content into their own app?"*

**Structure**:
- **IG item #1 is always "Adding `zerops.yaml`"** — full yaml verbatim with inline causal comments. **Engine-emitted from the codebase's own `zerops.yaml`; the agent does NOT author IG #1 directly.** This single item carries 60–80% of IG content in both reference recipes.
- **IG items #2..N (cap N=5):** verb-first heading; 1–3 sentences explaining what + why; one of {`composer require ...`, `npm install ...`, 3–5 line code diff}. Each item must have a copyable artifact.

**Belongs here**:
- Adding `zerops.yaml` (item #1, engine-emitted).
- Binding to `0.0.0.0` instead of `127.0.0.1`.
- `trust proxy` for Express / equivalent for other frameworks.
- Reading env vars from `process.env` directly (not from `.env` files).
- `initCommands` with `zsc execOnce` for migrations.
- Platform-driven code adjustments (`forcePathStyle`, `allowedHosts`, `httpSupport`).
- Worker pattern: `createMicroservice`, queue group, SIGTERM drain — each as a concrete diff.

**Does not belong here**:
- Framework setup the porter already has (Svelte `mount()`, Nest CLI, Vite init, `php artisan serve`) — the porter already did these.
- Helper code the recipe authored (`api.ts` wrapper, custom types files, `server.js` SIGTERM handler, `/healthz` design) — describe the PRINCIPLE, not the recipe's specific implementation.
- Debugging narratives ("then I saw X, so I did Y") — IG is imperative, not journal.
- Gotchas (→ knowledge-base fragment).

**Length**: **4–5 items per codebase including engine-emitted IG #1.** Showcase recipes do not get a higher cap; scope adds breadth via more codebases, not more IG items per codebase.

**Anti-pattern**: IG item that describes the recipe's own scaffold file (like v28's IG #3 explaining the `api.ts` wrapper, or run-14's IG #5 explaining the recipe's `sirv` config). The porter has no `api.ts` and may use a different static server.

---

### Surface 5 — Per-codebase README: Knowledge Base / Gotchas fragment

**Reader**: A developer hitting a confusing failure on Zerops and searching for what's wrong.

**Purpose**: Surface platform traps that are non-obvious even to someone who read the docs.

**The test**: *"Would a developer who read the Zerops docs AND the relevant framework docs STILL be surprised by this?"*

If the answer is "no, it's in the docs" → remove, don't ship as gotcha.
If the answer is "no, it's in the framework docs" → framework quirk, not a gotcha.
If the answer is "yes, it surprises you even knowing both" → this is a gotcha.

**Bullet shape (mandatory)**: `- **Topic** — 2–4 sentences.` Topic is bold, ≤ 6 words. Explanation is one paragraph. No sub-bullets. No code blocks inside KB unless the trap requires showing the offending string.

**Belongs here**:
- **Platform behaviors that surprise** (cross-service vars auto-inject project-wide → self-shadow trap; APP_KEY must be project-level for shared sessions across containers).
- **Platform × library intersections** (Zerops injects NATS creds as separate vars; nats.js v2 strips URL-embedded creds silently; `php-nginx@8.4` lacks `phpredis` so use `predis`).
- **Zerops-specific mechanisms with non-obvious failure modes** (MinIO-backed Object Storage rejects virtual-hosted style; `./dist/~` tilde strips directory wrapper; cache commands belong in `initCommands` not `buildCommands` because the build path differs from the runtime path).
- **Cross-codebase contracts the recipe enforces** (schema ownership, entity duplication).
- **Each gotcha has a concrete observable symptom** — HTTP status, quoted error string, measurable wrong-state — not just "it breaks".

**Does not belong here**:
- **Self-inflicted incidents** — code bugs the recipe accidentally shipped and then fixed. A silently-exiting seed script is a seed-script bug, not a platform trap. `zsc execOnce` honoring exit 0 is doing what its docs say.
- **Framework-only quirks** — `setGlobalPrefix` collision with `@Controller`, Svelte 5 `mount()` vs legacy constructor, plugin-svelte peer-dep — these belong in framework docs, not here.
- **npm / tooling metadata** — EPEERINVALID, package-lock conflicts, Node version mismatches — the porter's own tooling concern.
- **Scaffold-code decisions** — "our `api.ts` does X", "we chose pattern Y", "tabs-over-scroll for browser-walk verification", "the queue panel polls every 700ms" — these belong in zerops.yaml comments, code comments, or get discarded entirely.
- **Authoring-tool names** — `zerops_browser`, `zerops_subdomain`, `zerops_knowledge`, `zcli`, `zcp` are tools the recipe agent used, not tools the porter operates.
- **Restatements of IG items** — if IG #4 teaches `forcePathStyle`, the gotcha must add value beyond that (e.g. the symptom, not the fix).

**Length**: **5–8 bullets per codebase. Hard cap 8.** Run-14 shipped 11–12; that's over-collection.

**Citation rule**: If the gotcha's topic is covered by a `zerops_knowledge` guide (env-var-model, execOnce, rolling-deploys, object-storage, cross-service-refs), the gotcha MUST cite that guide by name. Pattern: *"The `<guide-id>` guide covers <basic mechanism>; the application-specific corollary is …"*. Writing new mental models for topics the platform already documents is how folk-doctrine ships.

**Anti-pattern ("folk-doctrine defect")**: Gotcha invents a mechanism because the author couldn't explain an observation. Example from v28 workerdev gotcha #1: *"The API codebase avoided the symptom because its resolver path happened to interpolate before the shadow formed; do not rely on that."* — This is fabricated. Both codebases had the same shadow pattern; both were broken. The correct rule (from the `env-var-model` guide) is "cross-service vars auto-inject project-wide — never declare `key: ${key}` at all." The author had access to that guide and didn't consult it.

---

### Surface 6 — Per-codebase CLAUDE.md

**Reader**: Someone (human or AI agent) with this repo checked out locally, working on the codebase.

**Purpose**: Generic codebase operating guide — the same shape `claude /init` would produce. Project overview, build/run/test commands, code architecture. Zero Zerops-specific content; the Zerops integration is documented in IG (Surface 4) / KB (Surface 5) / zerops.yaml comments (Surface 7).

**The test**: *"Would `claude /init` produce content of this shape for this repo? Is anything Zerops-specific here that belongs in IG / KB / yaml comments instead?"*

**Authoring**: SUB-AGENT-AUTHORED via a dedicated `claudemd-author` peer dispatched in parallel with the codebase-content sub-agent at phase 5. Brief is **strictly Zerops-free** (no platform principles, no env-var aliasing, no managed-service hints, no dev-loop teaching) so that the bleed-through that produced the old run-15 shape (`## Zerops service facts`) cannot recur. The sub-agent reads the codebase (`Read`, `Glob`, `Bash`) and produces `/init`-quality output for any framework — no engine-side framework registry.

**Structure (matches `/init` shape)**:

```
# <repo-name>

<1-sentence framing — framework, version, what this codebase does>

## Build & run

- <command from package.json/composer.json scripts, with one-line label>
- ...

## Architecture

- `src/<entry>` — <auto-derived label>
- `src/<dir>/` — <auto-derived label per framework convention>
- ...
```

Optional 3rd–4th H2 section when the codebase warrants — e.g. `## Adding a feature panel` for an SPA, `## Worker pattern` for a queue consumer, `## Testing` when a non-obvious test command exists. Sections follow the codebase's actual operational concerns; no fixed Zerops-flavoured headings.

**Belongs here**:
- Project overview line.
- Build/dev/test commands enumerated from `package.json` / `composer.json` scripts.
- Top-level src/ structure with one-line per-entry labels.
- Codebase-specific operational sections (feature flow, worker pattern, etc.) when the repo warrants them.

**Does not belong here**:
- Zerops platform mechanics (→ IG / KB / zerops.yaml comments).
- Managed-service hostnames or env-var aliases (→ zerops.yaml comments).
- Migration / seed recovery procedures (→ zerops.yaml comments at `initCommands`, OR code comments in the migration script).
- Cross-codebase contracts (→ code comments at publish/subscribe sites).
- Recipe-internal architectural decisions (→ code comments).
- Authoring-tool names (`zcli`, `zerops_*`, `zcp`, `zsc`, `zerops_dev_server`).

**Length**: ~30–50 lines depending on codebase complexity. No hard cap; shape and Zerops-content-absence are the contract.

**Validator**: `validateCodebaseCLAUDE` confirms the sub-agent's output shape held — title + framing line + 2–4 H2 sections, ≥200 bytes, ≤80 lines. Zerops-content absence is the brief's contract (`briefs/claudemd-author/zerops_free_prohibition.md`), not the validator's. Run-21 R2-5 dropped engine-side word-blacklisting (hostname mentions, `## Zerops` headings, `zsc`/`zerops_*`/`zcp`/`zcli` tokens) after run-21 evidence showed 4× rejection cycles around common English tokens (`db`, `cache`, `search` collide with prose). The validator is the structural-shape backstop; brief teaching is the content contract.

**Anti-pattern**: any of the older reference recipes' CLAUDE.md sections that embed Zerops platform facts (`## Zerops service facts` listing managed services; `## Zerops dev (hybrid)` describing dev-loop quirks). Those facts belong in zerops.yaml comments / IG / KB. The reference recipes set this precedent before the dedicated `claudemd-author` brief existed; they will be updated separately.

---

### Surface 7 — Per-codebase `zerops.yaml` comments

**Reader**: Someone reading the deploy config to understand or modify it.

**Purpose**: Explain non-obvious choices the reader couldn't infer from field names alone.

**The test**: *"Does each comment explain a trade-off or consequence the reader couldn't infer from the field name?"*

**Belongs here**:
- WHY `execOnce` wraps migrate (per-deploy gate, concurrent-replica safety).
- WHY `--retryUntilSuccessful` (bounded retry during boot).
- WHY `httpSupport: true` (L7 balancer registration).
- WHY `deployFiles: ./` in dev vs `./dist/~` in prod (mount preservation vs tilde-strip).
- WHY `zsc noop --silent` in dev (keep container alive for SSH-driven iteration).
- WHY `npm install` in dev vs `npm ci` + `npm prune --omit=dev` in prod.
- WHY `predis` not `phpredis`, WHY `forcePathStyle: true`, WHY `LOG_CHANNEL: stderr`.
- WHY identical env-var maps across dev/prod (divergence check requires real difference beyond `APP_DEBUG` / `APP_ENV`).

**Does not belong here**:
- App-code teaching (→ IG).
- Framework-agnostic Zerops concepts at length (→ `zerops_knowledge` guides; cite, don't duplicate).
- Gotcha content (→ KB fragment).

**Comment granularity**: author's choice. Block-mode comment above a directive group; single-line comment above an individual field; both forms freely mixed. Both reference recipes use both. The rule is the voice (explain the choice), not the layout.

**Voice**: comment block above each directive group OR single-line comment above the specific field; mechanism + reason. Both references use the form `# <one-sentence cause>. <one-sentence consequence>.`

**Anti-pattern**: Comment narrates what the field does instead of why the choice was made. *"`deployFiles: ./` ships the working tree"* is narration; *"`deployFiles: ./` is mandatory on dev self-deploys because cherry-picking would destroy source on redeploy"* teaches the trade-off.

---

## Friendly-authority voice

Both reference recipes speak TO the porter, not AT them. The porter is making this their own; comments give them permission to modify.

**Pattern**: declarative statement of fact + invitation to adapt.

Examples drawn from the references:

- *"Feel free to change this value to your own custom domain, after setting up the domain access."* (jetstream)
- *"Configure this to use real SMTP sinks in true production setups."* (jetstream)
- *"Replace with real SMTP credentials for production use."* (showcase)
- *"Disabling the subdomain access is recommended, after you set up access through your own domain(s)."* (jetstream tier yaml)

**Where it applies**:
- `zerops.yaml` comments (Surface 7) — primary site for friendly authority.
- Tier `import.yaml` comments (Surface 3) — secondary site, where a per-service decision has obvious adapt-for-your-needs follow-through (Mailpit removed at prod tiers, etc.).
- IG prose (Surface 4) — sparingly, where a config has multiple valid shapes.

**Where it does NOT apply**:
- KB bullets (Surface 5) — gotchas are imperative; "Feel free to" weakens the warning.
- CLAUDE.md (Surface 6) — operational guide; declarative.
- Root README (Surface 1) — factual catalog; no voice.

**Hedging is the wrong shape** ("you might want to consider", "perhaps"). The voice is "this is the choice; here's why; you can change it for your needs" — not "this could maybe be one option among many."

---

## Fact classification taxonomy

Every observation from a recipe run — whether a surprise, an incident, a scaffold decision, or an operational hack — is classified **before** it is placed on any surface. Classification determines routing. Facts that classify as self-inflicted or framework-only are **discarded**, not published.

| Classification | Test | Route to |
|---|---|---|
| **Platform invariant** | Fact is true of Zerops regardless of this recipe's scaffold choices. A different NestJS app, a different framework entirely, would hit the same trap. | Knowledge-Base gotcha (with `zerops_knowledge` guide citation if one exists) |
| **Platform × framework intersection** | Fact is specific to this framework AND caused by a platform behavior. Neither side alone would produce it. | Knowledge-Base gotcha, naming both sides clearly |
| **Framework quirk** | Fact is about the framework's own behavior, unrelated to Zerops. Any user of that framework hits it regardless of where they deploy. | **DISCARD** — belongs in framework docs, not a Zerops recipe |
| **Library metadata** | Fact is about npm, composer, pip, cargo — dependency-resolution or version-pinning concerns. | **DISCARD** — belongs in dep manifest comments, not recipe content |
| **Scaffold decision (config)** | "We chose X over Y for this recipe" — visible in `zerops.yaml` field values. | Per-codebase `zerops.yaml` comment (Surface 7) |
| **Scaffold decision (code)** | "We chose X over Y for this recipe" — code-level decision the porter literally copies as a diff. | IG item with the diff (Surface 4) |
| **Scaffold decision (recipe-internal)** | "Our scaffold has X" but the porter would have different code. | **DISCARD** or move principle (without specific implementation) to IG |
| **Operational** | How to iterate / test / reset this specific repo locally. | `CLAUDE.md` (Surface 6) |
| **Self-inflicted** | Our code had a bug; we fixed it; a reasonable porter would not hit it because their code doesn't have that specific bug. | **DISCARD** entirely — not content material |

### Classification × surface compatibility

The engine refuses incompatible (classification, fragmentId) pairs at `record-fragment` time:

| Classification | Compatible surfaces | Refused with redirect |
|---|---|---|
| platform-invariant | KB, IG (if porter applies a diff) | CLAUDE.md (→ KB), zerops.yaml comments (→ IG/KB) |
| intersection | KB | All others |
| framework-quirk / library-metadata | none | All — content does not belong on any published surface |
| scaffold-decision (config) | zerops.yaml comments, IG (if porter copies the config) | KB, CLAUDE.md |
| scaffold-decision (code) | IG (with diff) | KB, CLAUDE.md |
| scaffold-decision (recipe-internal) | none | All — discard or move principle to IG |
| operational | CLAUDE.md | All others |
| self-inflicted | none | All — discard |

### How to classify — concrete rules

1. **Separate mechanism from symptom.** The mechanism (what Zerops does) is the platform invariant; the symptom (what our code did wrong) may or may not be. Classify based on mechanism.
2. **Ask "would they hit this with different scaffold code?"** If no → scaffold decision or self-inflicted. If yes → platform invariant or intersection.
3. **Check for a matching `zerops_knowledge` guide.** If one exists, the fact is probably a platform invariant the platform already documents — route as gotcha WITH citation, don't duplicate the guide's content.
4. **Self-inflicted litmus test**: Could this observation be summarized as "our code did X, we fixed it to do Y"? If yes, discard. The fix belongs in the code; there is no teaching for a porter.
5. **When in doubt between Intersection and Framework-quirk**: does the Zerops side contribute materially to the failure mode? If not, it's a framework quirk.

---

## Counter-examples — the wrong-surface catalog

Concrete, named examples of each classification failure, so the content author can pattern-match against them.

### Self-inflicted (should have been discarded, were shipped as gotchas)

- **"`zsc execOnce` can record a successful seed that produced zero output"** (v28 apidev gotcha #1) — The seed script silently exited 0 with no stdout. `execOnce` correctly honored the exit code. This is a seed-script bug ("our script silently exited without inserting rows"), NOT a platform trap. `execOnce` is doing what its docs say. Fix: inspect the seed script and make it fail loudly on empty inserts. Discard the gotcha.

### Framework quirks (should have been discarded)

- **"`app.setGlobalPrefix('api')` collides with `@Controller('api/...')` decorators"** (v28 apidev gotcha #5) — Pure NestJS framework fact. Zerops is not involved. A porter using NestJS already knows or will learn this from NestJS docs. Belongs in framework docs or code comments.
- **"`@sveltejs/vite-plugin-svelte@^5` peer-requires Vite 6, not Vite 5"** (v28 appdev gotcha #5) — npm registry metadata. Zero Zerops involvement. Belongs in `package.json` notes.

### Scaffold decisions disguised as gotchas

- **"`api.ts`'s `application/json` content-type check is what catches the SPA-fallback class of bug"** (v28 appdev gotcha #4) — `api.ts` is the recipe's own scaffold helper. A porter bringing their own Svelte app does not have `api.ts`. The underlying fact (Nginx SPA fallback returns `200 text/html` on `/api/*` misses) IS a platform invariant worth teaching — route that to IG (principle-level) and the specific implementation to code comments. The current gotcha teaches neither audience cleanly.
- **"Demonstration panels — one tab per managed service"** (run-14 appdev KB #8) — The recipe's chosen SPA design. A porter bringing their own app may have completely different UX. Belongs in code comments at most; not KB.
- **"Queue panel polls the api every ~700ms"** (run-14 appdev KB #10) — Recipe's specific polling implementation. Not a platform trap.
- **"Tabs over single-column scroll for browser-walk verification"** (run-14 appdev KB #11) — Documents the agent's recovery from R-14-5 (`zerops_browser` viewport clipping). Names the authoring tool by name. Pure self-referential decoration.

### Folk-doctrine defects (real trap, fabricated explanation)

- **"The API codebase avoided the symptom because its resolver path happened to interpolate before the shadow formed; do not rely on that."** (v28 workerdev gotcha #1) — The env-shadow trap is real and load-bearing. But the explanation is invented. Both apidev and workerdev shipped identical `db_hostname: ${db_hostname}` patterns; both were broken. The correct platform rule (from the `env-var-model` guide, which existed and was accessible during the run): **cross-service vars auto-inject project-wide; never declare `key: ${key}` in run.envVariables — the line is redundant AND it breaks the container env.** Fix: citation to guide, use guide's framing.

### Surface-2 contract violation: ladder content inside extract markers

- **Run-14 tier READMEs (all six)** wrap ~35 lines of ladder content (Shape at a glance / Who fits / How iteration works / What you give up / When to outgrow / What changes at next tier) inside the `<!-- #ZEROPS_EXTRACT_START:intro# -->` markers. The recipe-page UI renders the marker contents as the tier-card description. Both reference recipes (laravel-jetstream + laravel-showcase) wrap a single sentence. The 35-line ladder shows up in the recipe page UI as a 35-line "card description" — wrong rendering, wrong reader.

### Fabricated yaml field names in import.yaml comments

- **Run-14 tier import.yaml preamble comments (all six tiers)** reference `project_env_vars` (snake_case). The actual yaml schema field is `project.envVariables` (camelCase, nested). A porter searching the yaml for `project_env_vars` finds nothing. The fabrication is structurally invisible — looks like a normal yaml field name but doesn't exist.

### Authoring-voice leak in import.yaml comments

- **Run-14 tier-1 + tier-5 import.yaml** mention "recipe author" in comment prose. The yaml comments speak about the porter's deployed runtime; "recipe author" is the agent that authored the yaml — wrong audience. Correct shape: the comment names the choice (e.g. *"APP_KEY generated fresh per project — change this if you migrate from a persistent secret store"*) without referring to the agent that wrote the comment.

### Factually wrong content (should have been caught by self-review)

- **Env 5 `import.yaml`**: *"NATS 2.12 in mode: HA — clustered broker with JetStream-style durability."* — The recipe uses core NATS pub/sub with queue groups (`Transport.NATS` + `queue: 'jobs-workers'`), NOT JetStream. "JetStream-style" conflates distinct NATS subsystems. Fix: describe the clustered core-NATS behavior without invoking JetStream.

### Cross-surface duplication (same fact, multiple surfaces)

These patterns appear repeatedly across v28:

- `.env` shadowing: apidev IG #3 **+** apidev `zerops.yaml` comment **+** apidev CLAUDE.md trap (3 surfaces, one fact)
- `forcePathStyle: true`: apidev IG #4 **+** apidev gotcha #2 **+** env 0/5 `import.yaml` comment **+** apidev `zerops.yaml` comment (4 surfaces, one fact)
- `nest build` tsbuildinfo: workerdev gotcha #2 **+** workerdev `zerops.yaml` comment **+** apidev CLAUDE.md **+** env 1 `import.yaml` (4 surfaces, one fact, with a factual error on one of them)

Rule: each fact lives on **one** surface. Other surfaces that need it **cross-reference** — they do not re-author.

---

## Citation map — which topics require `zerops_knowledge` citation

When content touches any of these topics, the author MUST fetch the guide first and its content must be consistent with the guide's framing. This prevents folk-doctrine.

| Topic area | Guide ID (via `zerops_knowledge`) | What the guide covers |
|---|---|---|
| Cross-service env vars, self-shadow, aliasing | `env-var-model` | Auto-inject semantics, never declare `key: ${key}`, legitimate renames (`DB_HOST: ${db_hostname}`), mode flags |
| `zsc execOnce` gate, `appVersionId`, init commands | `init-commands` | Per-deploy gate semantics, `--retryUntilSuccessful`, distinct keys per command |
| Rolling deploys, SIGTERM, HA replicas | `rolling-deploys` / `minContainers-semantics` | Two-axis `minContainers` (throughput + HA), SIGTERM-before-teardown, drain semantics |
| Object Storage (MinIO, forcePathStyle) | `object-storage` | MinIO-backed, path-style required, `storage_*` env vars |
| L7 balancer, `httpSupport`, VXLAN IP routing | `http-support` / `l7-balancer` | Why bind 0.0.0.0, TLS termination, `trust proxy` |
| Cross-service references, isolation modes | `env-var-model` (same guide) | `envIsolation` semantics, project-level vs service-level |
| Deploy files, tilde suffix, static base | `deploy-files` / `static-runtime` | `./dist/~` rationale, `base: static` limitations |
| Readiness check, health check, routing gates | `readiness-health-checks` | What routes traffic, what restarts the container |

The author's workflow:
1. For each candidate gotcha or IG item, scan its topic against this map.
2. If a match exists, call `zerops_knowledge` on that guide.
3. Read the guide. Align framing with it. Cite the guide by name in the published content.
4. Do NOT write new mental models for topics the guide already teaches.

If a gotcha's topic matches a guide but the guide is silent on the specific intersection — that's genuinely new content. But the author must have READ the guide first to know whether it's genuinely new.

---

## Per-surface test cheatsheet

Single-question tests to apply during self-review before publishing.

- **Root README item** → "Can a reader decide in 30 seconds whether this deploys what they need?"
- **Tier README extract** → "Does this 1–2 sentence card description tell a porter which tier to click?"
- **Tier import.yaml comment** → "Does each service block explain a decision (why this scale / mode / presence), not narrate what the field does?"
- **IG item** → "Would a porter bringing their own code need to copy THIS exact content into their own app?"
- **Gotcha (KB bullet)** → "Would a developer who read the Zerops docs AND the framework docs STILL be surprised by this?"
- **CLAUDE.md entry** → "Is this useful for operating THIS repo — not for deploying or porting?"
- **zerops.yaml comment** → "Does this explain a trade-off the reader couldn't infer from the field name?"

Items that fail their surface's test are **removed, not rewritten to pass**. The test fails because the content doesn't belong, not because it's phrased wrong.

---

## How this spec is used

1. **`internal/recipe/surfaces.go::SurfaceContract`** carries each surface's `FormatSpec` URL anchored into a section of this file. The spec text is the load-bearing source for the per-surface contract; the engine code references it by URL.
2. **`record-fragment` response payload** carries the `SurfaceContract` for the fragment's resolved surface, so the agent reads the relevant test verbatim at authoring decision time, not just at brief-preface time.
3. **`record-fragment` accepts `classification`**, and the engine refuses incompatible (classification, fragmentId) pairs per the compatibility table above, returning a redirect teaching message.
4. **Validators registered to each surface** check the structural caps from the line-budget table at `complete-phase` gate evaluation. Cap violations are blocking; voice / classification mismatches are notices.
5. **Editorial review** (human or agent) walks each surface and applies the one-question test per item; removes anything that fails.
6. **New recipe authors** (human contributors) read this spec before writing content for a new recipe.
7. **When a gotcha or content issue is spotted post-publish**, the discussion pattern is: "which surface does this belong on per the spec?" and "does the item pass that surface's test?" — grounded classification instead of case-by-case debate.

---

## Maintenance

This spec is additive. When a new class of wrong-surface item or folk-doctrine shows up in a future recipe run:

- Add the example to [Counter-examples](#counter-examples--the-wrong-surface-catalog) under the appropriate classification heading.
- If the pattern reveals a gap in the surface contracts or classification taxonomy, amend the relevant section.
- If a new `zerops_knowledge` guide needs to be cited by future content, add it to [Citation map](#citation-map--which-topics-require-zerops_knowledge-citation).
- **When the spec adds a new surface contract field, update `internal/recipe/surfaces.go::SurfaceContract` to carry it, and update the per-fragment-id contract values in `surfaceContracts`.** The spec edit and the engine struct extension must land in the same commit so the contract delivered at record-time stays consistent with this file.

The spec stays canonical by being updated with new concrete examples rather than becoming more abstract.

**What this spec must NOT become**: a per-fragmentId banned-string list. Adding hardcoded vocabulary forbidden in specific surfaces is the catalog-drift signature ([system.md §4](zcprecipator3/system.md)). Every contract here expresses a positive shape (line cap, item cap, structure, reader, test) or a positive routing rule (classification × surface table). Future amendments must follow the same pattern; if a new failure mode can only be expressed as "ban string X in surface Y", the underlying shape rule needs to be re-derived.
