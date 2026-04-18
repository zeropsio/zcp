# Content Surfaces — Classification & Routing Spec

**Authoritative reference** for what content belongs on which recipe surface, how to classify observations from a run, and how to route each classified fact to exactly one destination.

This spec exists because recipe content quality has drifted below the bar across v20–v28 despite passing every token-level check. The root cause is that the agent which debugs the recipe also writes the reader-facing content, and after 85+ minutes of debug-spiral its mental model is "what confused me" rather than "what a reader needs." This spec formalizes the reader-facing purpose of each surface so content authoring can be tested against it.

The spec is the ground truth that the content-authoring sub-agent (see [implementation-v8.94-content-authoring.md](implementation-v8.94-content-authoring.md)) reads as part of its brief, and the ground truth that editorial reviews evaluate against.

---

## Why this exists — the content-quality failure mode

Every cross-version regression since v20 traces to a single pattern: the agent writes a **journal**, not a **reader-facing document**. Three sub-pathologies flow from this:

1. **Fabricated mental models.** When the agent can't explain a symptom (e.g., "why did apidev's self-shadow work but workerdev's didn't?"), it invents a mechanism. v23 shipped "Recovering `zsc execOnce` burn" as a fictional per-workspace burn; v28 shipped "the interpolator resolved before the shadow formed" as a fictional timing-dependent resolver. Both were written despite the agent having access to the correct platform teaching via `zerops_knowledge`.
2. **Wrong-surface placement.** The knowledge-base / gotchas fragment becomes a dumping ground for anything the agent stumbled over during scaffolding — NestJS's `setGlobalPrefix` collision, npm peer-dep errors, framework bootstrap API — content that is not Zerops-specific and does not belong on a Zerops recipe surface at all.
3. **Self-referential decoration.** The agent documents its own scaffold code as if it were a platform contract. "Our `api.ts` helper's content-type check catches SPA fallbacks" is the agent describing its own implementation in knowledge-base voice.

None of these are caught by token-level checks ("names a Zerops mechanism", "names a concrete failure mode"), because all three failure modes satisfy those patterns trivially. The correction has to happen at the mental-model layer — BEFORE content is authored.

---

## The six content surfaces

Each recipe has exactly six kinds of content surface. Every fact lives on exactly ONE surface. Cross-surface references are fine; cross-surface duplication is not.

### Surface 1 — Root README

**Reader**: A developer browsing zerops.io/recipes deciding whether to click deploy.

**Purpose**: Name the services, name the environment tiers, show one-click deploy buttons.

**The test**: *"Can a reader decide in 30 seconds whether this recipe deploys what they need, and pick the right tier?"*

**Belongs here**:
- One-sentence intro naming every managed service
- List of environment tiers with deploy buttons
- Pointer to the recipe-category page (zerops.io/recipes?lf={framework})

**Does not belong here**:
- Gotchas, debugging, code details (→ per-codebase README)
- Deployment shape decisions (→ env import.yaml comments)
- Architecture explanations beyond a single paragraph

**Typical length**: 20–30 lines.

---

### Surface 2 — Environment README

**Reader**: Someone deciding WHICH tier to deploy, or evaluating whether to promote to the next tier.

**Purpose**: Teach the tier's audience, use case, and how it differs from the adjacent tiers.

**The test**: *"Does this teach me when I'd outgrow this tier, and what the next tier changes?"*

**Belongs here**:
- Who this tier is for (AI agent iterating, remote dev, local dev, stage reviewer, small prod, HA prod)
- What scale this tier handles (single replica / multi-replica / HA)
- What changes relative to the adjacent tier
- Tier-specific operational concerns (e.g., "stage data is ephemeral", "HA prod requires DEDICATED CPU")

**Does not belong here**:
- Service-by-service rationale (→ env import.yaml comments)
- Framework quirks (→ per-codebase README / gotchas)
- Platform mechanisms (→ zerops_knowledge guides)

**Typical length**: 40–80 lines of teaching (NOT 7 lines of boilerplate, which is the current failure mode).

---

### Surface 3 — Environment `import.yaml` comments

**Reader**: Someone who deployed this tier and is reading the manifest in the Zerops dashboard to understand what they're running.

**Purpose**: Explain every decision — service presence, scale, mode — at this tier.

**The test**: *"Does each service-block comment explain a decision (scale, mode, why this service exists at this tier), not just narrate what the field does?"*

**Belongs here**:
- **Why this service at this tier** (why db is NON_HA on stage, why worker is HA on env 5)
- **Why this scale** (throughput vs HA rationale for `minContainers: 2`, cost-trade rationale for single replica)
- **Why this mode** (NON_HA durability trade-off, HA failover cost justification)
- **Cross-tier promotion context** (what changes when promoting to the next tier)
- **Tier-specific env-var reasoning** (why `DEV_*` + `STAGE_*` constants exist at project level)

**Does not belong here**:
- App-code details, framework quirks, library versions (→ per-codebase zerops.yaml comments or README)
- Cross-codebase contract explanations (→ workerdev README or IG)
- General Zerops platform facts (→ zerops_knowledge guides; cite don't duplicate)

**Anti-pattern**: Templated per-service opening repeated across services ("enables zero-downtime rolling deploys" appearing verbatim on app, api, worker blocks). Each block's reasoning is service-specific.

---

### Surface 4 — Per-codebase README: Integration Guide fragment

**Reader**: A porter bringing their own existing application (Svelte app they built, NestJS API they built). They are NOT using this recipe as a template — they are extracting the Zerops-specific steps to adapt their own code.

**Purpose**: Enumerate the concrete changes a porter must make in their own codebase to run on Zerops.

**The test**: *"Does a porter who ISN'T using this recipe as a template, but bringing their own code, need to copy THIS exact content into their own app?"*

**Belongs here**:
- Adding `zerops.yaml` (the shape + commented fields)
- Binding to `0.0.0.0` instead of `127.0.0.1`
- `trust proxy` for Express / equivalent for other frameworks
- Reading env vars from `process.env` directly (not from `.env` files)
- `initCommands` with `zsc execOnce` for migrations
- Platform-driven code adjustments (forcePathStyle, allowedHosts, httpSupport)
- Worker pattern: `createMicroservice`, queue group, SIGTERM drain — each as a concrete diff

**Does not belong here**:
- Framework setup the porter already has (Svelte `mount()`, Nest CLI, Vite init) — the porter already did these for their own app
- Helper code the recipe authored (`api.ts` wrapper, custom types files) — describe the PRINCIPLE, not the recipe's specific implementation
- Debugging narratives ("then I saw X, so I did Y") — IG is imperative, not journal
- Gotchas (→ knowledge-base fragment)

**Each IG item carries**:
- A concrete action ("Bind to 0.0.0.0", "Add `forcePathStyle: true`")
- A one-sentence reason tied to Zerops mechanism
- A code block with the actual change the porter copies

**Anti-pattern**: IG item that describes the recipe's own scaffold file (like v28's IG #3 explaining the `api.ts` wrapper). The porter has no `api.ts`.

---

### Surface 5 — Per-codebase README: Knowledge Base / Gotchas fragment

**Reader**: A developer hitting a confusing failure on Zerops and searching for what's wrong.

**Purpose**: Surface platform traps that are non-obvious even to someone who read the docs.

**The test**: *"Would a developer who read the Zerops docs AND the relevant framework docs STILL be surprised by this?"*

If the answer is "no, it's in the docs" → remove, don't ship as gotcha.
If the answer is "no, it's in the framework docs" → framework quirk, not a gotcha.
If the answer is "yes, it surprises you even knowing both" → this is a gotcha.

**Belongs here**:
- **Platform behaviors that surprise** (cross-service vars auto-inject project-wide → self-shadow trap)
- **Platform × library intersections** (Zerops injects NATS creds as separate vars; nats.js v2 strips URL-embedded creds silently)
- **Zerops-specific mechanisms with non-obvious failure modes** (MinIO-backed Object Storage rejects virtual-hosted style; `./dist/~` tilde strips directory wrapper)
- **Cross-codebase contracts the recipe enforces** (schema ownership, entity duplication)
- **Each gotcha has a concrete observable symptom** — HTTP status, quoted error string, measurable wrong-state — not just "it breaks"

**Does not belong here**:
- **Self-inflicted incidents** — code bugs the recipe accidentally shipped and then fixed. A silently-exiting seed script is a seed-script bug, not a platform trap. `zsc execOnce` honoring exit 0 is doing what its docs say.
- **Framework-only quirks** — `setGlobalPrefix` collision with `@Controller`, Svelte 5 `mount()` vs legacy constructor, plugin-svelte peer-dep — these belong in framework docs, not here.
- **npm / tooling metadata** — EPEERINVALID, package-lock conflicts, Node version mismatches — the porter's own tooling concern.
- **Scaffold-code decisions** — "our `api.ts` does X", "we chose pattern Y" — these belong in zerops.yaml comments or code comments.
- **Restatements of IG items** — if IG #4 teaches forcePathStyle, the gotcha must add value beyond that (e.g. the symptom, not the fix). v28 restated IG items as gotchas — fails `gotcha_distinct_from_guide` check.

**The citation rule**: If the gotcha's topic is covered by a `zerops_knowledge` guide (env-var-model, execOnce, rolling-deploys, object-storage, cross-service-refs), the gotcha MUST cite that guide. Writing new mental models for topics the platform already documents is how folk-doctrine ships.

**Anti-pattern ("folk-doctrine defect")**: Gotcha invents a mechanism because the author couldn't explain an observation. Example from v28 workerdev gotcha #1: *"The API codebase avoided the symptom because its resolver path happened to interpolate before the shadow formed; do not rely on that."* — This is fabricated. Both codebases had the same shadow pattern; both were broken. The correct rule (from the `env-var-model` guide) is "cross-service vars auto-inject project-wide — never declare `key: ${key}` at all." The author had access to that guide and didn't consult it.

---

### Surface 6 — Per-codebase CLAUDE.md

**Reader**: Someone (human or AI agent) with THIS repo checked out locally, working on the codebase.

**Purpose**: Operational guide for running the dev loop, iterating on the repo, exercising features by hand.

**The test**: *"Is this useful for operating THIS repo specifically — not for deploying it to Zerops, not for porting it to other code?"*

**Belongs here**:
- Dev loop (SSH commands, dev-server startup, port, health path)
- Migration and seed commands (how to run them by hand for iteration)
- Resetting dev state without a redeploy (truncate scripts, index deletes)
- Driving a feature end-to-end by hand (curl recipes for each endpoint)
- Repo-local container traps (SSHFS uid issues, dev-deps pruning gotchas)
- Testing commands (typecheck, build, feature sweep scripts)

**Does not belong here**:
- Deploy instructions (→ zerops.yaml comments, IG)
- Platform gotchas (→ knowledge-base fragment)
- Framework basics (assume the operator knows the framework)

**Must clear**: 1200-byte floor + ≥2 custom sections beyond the template (Dev Loop / Migrations / Container Traps / Testing).

---

### Surface 7 — Per-codebase `zerops.yaml` comments

**Reader**: Someone reading the deploy config to understand or modify it.

**Purpose**: Explain non-obvious choices the reader couldn't infer from field names alone.

**The test**: *"Does each comment explain a trade-off or consequence the reader couldn't infer from the field name?"*

**Belongs here**:
- WHY `execOnce` wraps migrate (per-deploy gate, concurrent-replica safety)
- WHY `--retryUntilSuccessful` (bounded retry during boot)
- WHY `httpSupport: true` (L7 balancer registration)
- WHY `deployFiles: ./` in dev vs `./dist/~` in prod (mount preservation vs tilde-strip)
- WHY `zsc noop --silent` in dev (keep container alive for SSH-driven iteration)
- WHY `npm install` in dev vs `npm ci` + `npm prune --omit=dev` in prod
- WHY identical env-var maps across dev/prod (divergence check requires difference beyond just `NODE_ENV`)

**Does not belong here**:
- App-code teaching (→ IG)
- Framework-agnostic Zerops concepts (→ zerops_knowledge guides)
- Gotcha content (→ KB fragment)

**Anti-pattern**: Comment narrates what the field does instead of why the choice was made. "`deployFiles: ./` ships the working tree" is narration; "`deployFiles: ./` is mandatory on dev self-deploys because cherry-picking would destroy source on redeploy" teaches the trade-off.

---

## Fact classification taxonomy

Every observation from a recipe run — whether a surprise, an incident, a scaffold decision, or an operational hack — is classified **before** it is placed on any surface. Classification determines routing. Facts that classify as self-inflicted or framework-only are **discarded**, not published.

| Classification | Test | Route to |
|---|---|---|
| **Platform invariant** | Fact is true of Zerops regardless of this recipe's scaffold choices. A different NestJS app, a different framework entirely, would hit the same trap. | Knowledge-Base gotcha (with `zerops_knowledge` guide citation if one exists) |
| **Platform × framework intersection** | Fact is specific to this framework AND caused by a platform behavior. Neither side alone would produce it. | Knowledge-Base gotcha, naming both sides clearly |
| **Framework quirk** | Fact is about the framework's own behavior, unrelated to Zerops. Any user of that framework hits it regardless of where they deploy. | **DISCARD** — belongs in framework docs, not a Zerops recipe |
| **Library metadata** | Fact is about npm, composer, pip, cargo — dependency-resolution or version-pinning concerns. | **DISCARD** — belongs in dep manifest comments, not recipe content |
| **Scaffold decision** | "We chose X over Y for this recipe; reader should understand why." Non-obvious design choice in the recipe's own code. | Per-codebase `zerops.yaml` comment (if config choice) or IG prose (if code choice) or `CLAUDE.md` (if operational) |
| **Operational detail** | How to iterate / test / reset this specific repo locally. | `CLAUDE.md` |
| **Self-inflicted** | Our code had a bug; we fixed it; a reasonable porter would not hit it because their code doesn't have that specific bug. | **DISCARD** entirely — not content material |

### How to classify — concrete rules

1. **Separate mechanism from symptom.** The mechanism (what Zerops does) is the platform invariant; the symptom (what our code did wrong) may or may not be. Classify based on mechanism.
2. **Ask "would they hit this with different scaffold code?"** If no → scaffold decision or self-inflicted. If yes → platform invariant or intersection.
3. **Check for a matching `zerops_knowledge` guide.** If one exists, the fact is probably a platform invariant the platform already documents — route as gotcha WITH citation, don't duplicate the guide's content.
4. **Self-inflicted litmus test**: Could this observation be summarized as "our code did X, we fixed it to do Y"? If yes, discard. The fix belongs in the code; there is no teaching for a porter.
5. **When in doubt between Intersection and Framework-quirk**: does the Zerops side contribute materially to the failure mode? If not, it's a framework quirk.

---

## Counter-examples from v28 (the wrong-surface catalog)

Concrete, named examples of each classification failure, so the content author can pattern-match against them.

### Self-inflicted (should have been discarded, were shipped as gotchas)

- **"`zsc execOnce` can record a successful seed that produced zero output"** (v28 apidev gotcha #1) — The seed script silently exited 0 with no stdout. `execOnce` correctly honored the exit code. This is a seed-script bug ("our script silently exited without inserting rows"), NOT a platform trap. `execOnce` is doing what its docs say. Fix: inspect the seed script and make it fail loudly on empty inserts. Discard the gotcha.

### Framework quirks (should have been discarded)

- **"`app.setGlobalPrefix('api')` collides with `@Controller('api/...')` decorators"** (v28 apidev gotcha #5) — Pure NestJS framework fact. Zerops is not involved. A porter using NestJS already knows or will learn this from NestJS docs. Belongs in framework docs or code comments.
- **"`@sveltejs/vite-plugin-svelte@^5` peer-requires Vite 6, not Vite 5"** (v28 appdev gotcha #5) — npm registry metadata. Zero Zerops involvement. Belongs in `package.json` notes.

### Scaffold decisions disguised as gotchas

- **"`api.ts`'s `application/json` content-type check is what catches the SPA-fallback class of bug"** (v28 appdev gotcha #4) — `api.ts` is the recipe's own scaffold helper. A porter bringing their own Svelte app does not have `api.ts`. The underlying fact (Nginx SPA fallback returns `200 text/html` on `/api/*` misses) IS a platform invariant worth teaching — route that to IG (principle-level) and the specific implementation to code comments. The current gotcha teaches neither audience cleanly.

### Folk-doctrine defects (real trap, fabricated explanation)

- **"The API codebase avoided the symptom because its resolver path happened to interpolate before the shadow formed; do not rely on that."** (v28 workerdev gotcha #1) — The env-shadow trap is real and load-bearing. But the explanation is invented. Both apidev and workerdev shipped identical `db_hostname: ${db_hostname}` patterns; both were broken. The correct platform rule (from the `env-var-model` guide, which existed and was accessible during the run): **cross-service vars auto-inject project-wide; never declare `key: ${key}` in run.envVariables — the line is redundant AND it breaks the container env.** Fix: citation to guide, use guide's framing.

### Factually wrong content (should have been caught by self-review)

- **Env 5 `import.yaml`**: *"NATS 2.12 in mode: HA — clustered broker with JetStream-style durability."* — The recipe uses core NATS pub/sub with queue groups (`Transport.NATS` + `queue: 'jobs-workers'`), NOT JetStream. "JetStream-style" conflates distinct NATS subsystems. Fix: describe the clustered core-NATS behavior without invoking JetStream.
- **Env 1 `import.yaml` workerdev block**: *"The tsbuildinfo is gitignored, so the first watch cycle always emits dist cleanly."* — `nest start --watch` uses ts-node (not `nest build`); the `.tsbuildinfo` issue is specific to the prod `nest build` path. The comment misleadingly suggests gitignoring fixes the dev watch path. Fix: move the tsbuildinfo note to the prod build section where it actually applies.

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
| `zsc execOnce` gate, `appVersionId`, init commands | `init-commands` (or relevant platform guide) | Per-deploy gate semantics, `--retryUntilSuccessful` |
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
- **Env README item** → "Does this teach me when to outgrow this tier and what changes at the next one?"
- **Env import.yaml comment** → "Does each service block explain a decision (why this scale / mode / presence), not narrate what the field does?"
- **IG item** → "Would a porter bringing their own code need to copy THIS exact content into their own app?"
- **Gotcha** → "Would a developer who read the Zerops docs AND the framework docs STILL be surprised by this?"
- **CLAUDE.md entry** → "Is this useful for operating THIS repo — not for deploying or porting?"
- **zerops.yaml comment** → "Does this explain a trade-off the reader couldn't infer from the field name?"

Items that fail their surface's test are **removed, not rewritten to pass**. The test fails because the content doesn't belong, not because it's phrased wrong.

---

## How this spec is used

1. **Content-authoring sub-agent brief** includes the surface contracts + classification table + counter-examples + citation map. See [implementation-v8.94-content-authoring.md](implementation-v8.94-content-authoring.md).
2. **Editorial review** (human or agent) walks each surface and applies the one-question test per item; removes anything that fails.
3. **New recipe authors** (human contributors) read this spec before writing content for a new recipe.
4. **When a gotcha or content issue is spotted post-publish**, the discussion pattern is: "which surface does this belong on per the spec?" and "does the item pass that surface's test?" — grounded classification instead of case-by-case debate.

---

## Maintenance

This spec is additive. When a new class of wrong-surface item or folk-doctrine shows up in a future recipe run:
- Add the example to [Counter-examples from v28](#counter-examples-from-v28-the-wrong-surface-catalog) under the appropriate classification heading.
- If the pattern reveals a gap in the surface contracts or classification taxonomy, amend the relevant section.
- If a new `zerops_knowledge` guide needs to be cited by future content, add it to [Citation map](#citation-map--which-topics-require-zerops_knowledge-citation).

The spec stays canonical by being updated with new concrete examples rather than becoming more abstract.
