# Research: recipe workflow knowledge reshuffle

**Source material**: LOG.txt + LOG2.txt from session `da5429e9-73ae-4b82-b0ba-a73cd263c5a9` (v8.52.0), plus the v8.52.0 slimdown post-mortem, plus a full walk of `internal/content/workflows/recipe.md` and the `assembleRecipeKnowledge` injection path.

**Scope**: not just size, but **whether each piece of knowledge is needed at the moment it's delivered**. Size is downstream of correctness.

**Outcome**: a proposed content placement and the rationale for every move. This document is the "why"; `implementation-recipe-workflow-reshuffle.md` is the "how".

---

## The real question

For every piece of content we inject at a step, we ask one thing: **between the moment this step starts and the moment the agent submits `zerops_workflow action=complete`, does the agent's next physical action depend on this content?** If no, it's either in the wrong step or it's speculative ambient material the agent absorbs and discards.

A secondary question: **does this piece of content reliably produce the right behavior, or did the LOG show it being ignored / misapplied?** Content that's physically relevant but ignored in practice needs a different treatment than content that's simply in the wrong place.

---

## Methodology

1. **Measure**: for each of the 6 recipe steps, boot the workflow engine against a NestJS-showcase plan and measure the `RecipeResponse.Current.DetailedGuide` byte count. This is what the agent actually reads.
2. **Walk**: for each step, list the agent's physical actions (tool calls, file writes, decisions) in order.
3. **Trace**: for each physical action, name the knowledge it consumes and where that knowledge currently lives.
4. **Classify**: each current piece of content as (a) needed here, keep; (b) needed elsewhere, move; (c) needed nowhere, delete; (d) missing, add.
5. **Cross-reference**: for every LOG2 failure, identify which bucket it falls into.

---

## Baseline measurements

Running `BuildResponse` at each step with the embedded knowledge store, NestJS showcase plan shape (live-measured against current main):

| Step | detailedGuide | Status |
|---|---|---|
| research | 15.1 KB | borderline (Claude Code inline limit ~25 KB) |
| provision | 26.8 KB | **overflow** |
| generate | 48.3 KB | **overflow (eight python heredoc calls to read)** |
| deploy | 28.2 KB | **overflow** |
| finalize | 17.5 KB | borderline |
| close | 16.1 KB | borderline |

Overflow means Claude Code persists the response to disk and the agent must shell into Python to read it. LOG.txt caught this at provision→generate (52 KB), LOG2.txt caught it again at generate checks (>50 KB).

The v8.52.0 slimdown trimmed the `generate` static section from 43 KB to 26 KB but did not fix the overflow because the chain recipe injection (~7 KB, added one layer up in `buildGuide`) wasn't counted in the cap test. The real assembled guide at generate today is ~48 KB.

**Section-level measurements** (raw `<section>` bodies in [recipe.md](../internal/content/workflows/recipe.md), excluding injection):

| Section | Bytes |
|---|---|
| research-minimal | 7.9 KB |
| research-showcase | 7.5 KB |
| provision | 8.3 KB |
| generate | 25.6 KB |
| generate-fragments | 5.9 KB |
| generate-dashboard | 8.4 KB |
| deploy | 28.2 KB |
| finalize | 12.7 KB |
| close | 16.1 KB |

**Core.md H2 measurements** (what gets injected via `getCoreSection`):

| H2 | Bytes |
|---|---|
| import.yaml Schema | 4.9 KB |
| zerops.yaml Schema | 2.0 KB |
| **Rules & Pitfalls** | **14.2 KB** |
| Schema Rules | 3.0 KB |

**Important correction**: earlier drafts of this document referred to the Rules & Pitfalls block as "8 KB" — the actual size is **14.2 KB**. This substantially changes the arithmetic behind the Phase 1 split: what we take out of provision is larger than originally estimated, and what we'd add to generate (if we did) is also larger. The split math below is recomputed against the real size.

**Research step composition** ([recipe_guidance.go:63-67](../internal/workflow/recipe_guidance.go#L63-L67)): at showcase tier the research step returns `research-showcase + "\n\n---\n\n" + research-minimal` — both sections concatenated. research-minimal trimming alone cannot drop research below ~9 KB unless research-showcase is also compressed. The Phase 0 caps below account for this.

---

## The physical walkthrough — NestJS showcase session

For each step I list (a) the agent's next physical actions, (b) the knowledge it actually consumes, and (c) what current content is bloat.

### Step 1 — Research

**Physical actions**:
1. Read the detailedGuide once.
2. Compose a `RecipePlan` JSON object with framework identity, build pipeline, targets, showcase fields, worker codebase decision.
3. Call `zerops_workflow action=complete step=research recipePlan={...}`.

**That's it.** No files written, no services created, no platform state change. One form submission.

**Knowledge the agent uses** (each justified by a specific field it populates):

| Field | Knowledge needed | Source |
|---|---|---|
| `framework` | The user's intent ("NestJS showcase") parses to `nestjs`. | Free-form agent reasoning, no doc needed. |
| `packageManager` / `httpPort` / `buildCommands` / `deployFiles` / `startCommand` / `cacheStrategy` / `dbDriver` / `migrationCmd` / `seedCmd` / `needsAppSecret` / `appSecretKey` / `loggingDriver` | Everything about NestJS: npm, port 3000, `npm ci && npm run build`, `dist/`, `node dist/main.js`, Postgres, TypeORM migrations, JWT secret, stderr logging. | **Agent training data.** Everything here is public NestJS knowledge. |
| `cacheLib` / `sessionDriver` / `queueDriver` / `storageDriver` / `searchLib` (showcase) | Library choices: `ioredis`, `redis` sessions, `nats.js`, `@aws-sdk/client-s3`, `meilisearch`. | **Agent training data.** npm ecosystem. |
| `slug` | `{framework}-showcase` naming convention. | Recipe-specific. One table row. |
| `runtimeType` / `buildBases` | Must match live catalog (`nodejs@22`, etc.). | `availableStacks` (live catalog injected at start). |
| `targets[]` | The 8-service API-first showcase topology (app/api/worker/db/redis/queue/storage/search). | Recipe-specific. One list. |
| `targets[].type` for managed services | `postgresql@17`, `valkey@7.2`, `nats@2.12`, `meilisearch@1.20`, `object-storage`, `static`. | `availableStacks`. |
| Full-stack vs API-first classification | "Built-in view engine = full-stack, JSON-only = API-first." | Recipe-specific. One rule. |
| `targets[].sharesCodebaseWith` (worker) | 3-test rule for SHARED vs SEPARATE (framework's bundled CLI, no independent manifest, cannot run without app bootstrap). NestJS+BullMQ fails test 1 → SEPARATE. | Recipe-specific. 8 lines. |
| Submission command | `zerops_workflow action=complete step=research recipePlan={...}`. | 2 lines. |

**Total knowledge actually needed at research**: ~4–5 KB (classification rule, target topology list, worker 3-test rule, slug pattern, submission command, plus the `availableStacks` block which is ~1–2 KB).

**Current 15 KB breakdown**:

| Block | Size | Needed? |
|---|---|---|
| "What type of recipe?" table | 0.5 KB | Keep — agent needs to classify intent. |
| Reference Loading (recipe=X) instructions | 1.3 KB | **Delete** — the agent's training data fills research fields better than a recipe file. And the generate step auto-injects the chain recipe anyway, so this is a redundant pre-load. |
| Framework Identity prose (service type / package manager / HTTP port) | 0.8 KB | **Delete** — agent already knows what an HTTP port is. |
| Build & Deploy Pipeline prose | 0.8 KB | **Delete** — agent knows what build commands are. |
| Database & Migration prose | 0.5 KB | **Delete** — agent knows what a migration command is. |
| Environment & Secrets prose | 0.4 KB | **Delete** — trivial. |
| Decision Tree Resolution 5-item list | 0.8 KB | **Trim** — most decisions are "agent picks the obvious default", not platform-specific. Keep the scaffold preservation rule; delete the rest. |
| Targets field description | 1.5 KB | **Trim** — hostname format + type validation are schema concerns already on the `RecipeTarget` struct's jsonschema tags. Keep `sharesCodebaseWith` explanation. |
| Additional Showcase Fields prose | 1 KB | **Delete** — the field names (`cacheLib`, `sessionDriver`, etc.) are self-explanatory; the agent fills them from training data. The queueDriver exceptions paragraph is in the wrong place; it belongs in the worker decision block. |
| Full-stack vs API-first classification | 0.6 KB | **Keep** — this is the rule that determines topology. |
| Showcase Targets lists (full-stack + API-first) | 1.2 KB | **Keep** — the 8-service topology is not in training data. |
| Worker codebase decision (3-test rule + SHARED/SEPARATE shapes) | 2.4 KB | **Trim** — the 3-test rule (800 B) is the only thing the agent uses at research. The SHARED/SEPARATE shape prose (what `workerdev` gets generated as, what `setup: worker` looks like) is a generate/provision concern — move it. |
| Submission box | 0.1 KB | Keep. |

**Cuts**: ~6 KB deleted from research-minimal, ~2 KB moved to later steps, ~2 KB trimmed from research-showcase (Additional Showcase Fields + worker decision compression). **Target: ~10 KB combined research guide** (research-showcase ~5 KB + separator + research-minimal ~2 KB + plan-context ~2 KB = ~9-10 KB). The showcase+minimal concat path means research-minimal trim alone cannot reach 5 KB.

**Missing from research** (would prevent LOG2 bug 7, `${search_apiKey}` hallucination): nothing at research. The correct fix is at provision, where the agent has just run `zerops_discover` and has the real data. See provision below.

---

### Step 2 — Provision

**Physical actions**:
1. Compose workspace `import.yaml` (`services:` only, NO `project:`).
2. Call `zerops_import content="..."`.
3. Wait for services to reach their target status (ACTIVE for managed/infra, ACTIVE for dev runtimes via `startWithoutCode`, READY_TO_DEPLOY for stage runtimes).
4. Call `zerops_mount action=mount serviceHostname=apidev` (and appdev, workerdev for the 3-repo case).
5. Call `zerops_discover includeEnvs=true` — **this is where the agent learns real env var names for every managed service**.
6. Call `zerops_env project=true action=set variables=[APP_SECRET=<@generateRandomString(<32>)>, DEV_API_URL=..., STAGE_API_URL=..., DEV_FRONTEND_URL=..., STAGE_FRONTEND_URL=...]` (API-first specific).
7. Record the discovered env var catalog somewhere the agent can reference it at generate.
8. Call `zerops_workflow action=complete step=provision`.

**Knowledge the agent uses**:

| Action | Knowledge needed | Source |
|---|---|---|
| Compose import.yaml | Field schema (hostname rules, type, mode, priority, minContainers, enableSubdomainAccess, startWithoutCode, envSecrets, buildFromGit, verticalAutoscaling, preprocessor functions). Workspace-specific rules: NO `project:`, NO `zeropsSetup` (would require `buildFromGit`). | `import.yaml Schema` from `themes/core.md` + provision-specific wrapper. |
| Standard-mode dev/stage pair shape | Dev: `startWithoutCode: true`, `minContainers: 1`, `enableSubdomainAccess: true`. Stage: no `startWithoutCode`, `enableSubdomainAccess: true`. | **Recipe-specific, not in knowledge store.** Must stay in recipe.md. |
| Worker target shape | Separate-codebase worker: own dev+stage pair. Shared-codebase worker: only stage (because the host target's dev container runs both processes). | **Recipe-specific.** Must stay. |
| Static frontend shape | Type `static` for BOTH dev and stage. `startWithoutCode: true` on dev. Build runtime comes from `zerops.yaml build.base`, not the service type. | **Recipe-specific.** Must stay. |
| Shared secret provisioning | `zerops_env project=true action=set variables=[APP_KEY=<@generateRandomString(<32>)>]`. NO `base64:` / `hex:` prefix. | Recipe-specific. Must stay. |
| Dual-runtime URL constants (API-first only) | Set 4 project env vars `DEV_API_URL`, `STAGE_API_URL`, `DEV_FRONTEND_URL`, `STAGE_FRONTEND_URL` derived from `${zeropsSubdomainHost}` + known hostnames + HTTP port. | Recipe-specific. Must stay. |
| **Managed service env var discovery** | `zerops_discover includeEnvs=true` returns the real variable names. Agent must USE this output rather than guessing names from training data. | **LOG2 bug 7 root cause: this instruction is weak in the current recipe.md.** |
| Git config on the mount | `git config --global --add safe.directory /var/www/{hostname}` + `user.email` + `user.name` ON ZCP (where the mount appears) before the first git operation. | **LOG2 bugs 2, 3, 13 root cause: this is undocumented anywhere in recipe.md.** |
| Hostname convention, priority, mode immutability, port ranges | `Rules & Pitfalls` subset. | Currently injected via `getCoreSection("Rules & Pitfalls")` — but the full 8 KB block covers build/deploy/runtime rules too. Only ~2 KB is provision-relevant. |

**Current 27 KB breakdown** (provision section + knowledge injection):

| Block | Size | Status |
|---|---|---|
| Container state transition diagram | 0.5 KB | Wrong step — belongs at generate ("what's available when I scaffold"). **Move.** |
| Standard-mode dev/stage property table | 0.8 KB | Keep. |
| Static frontend rules | 0.3 KB | Keep. |
| Workspace cannot contain `project:` section | 0.2 KB | Keep — critical. |
| Framework secrets shared vs per-service | 2 KB | **Trim** — the `base64:`/`hex:` rejection warning (~800 B) is Laravel-specific; keep a 2-line summary and move the detail to a decision guide if ever needed. |
| Dual-runtime URL constants setup block | 1.5 KB | Keep. |
| Import services + mount + discover subsections | 1 KB | Keep. |
| Discover env vars instruction | 0.3 KB | **Strengthen** — current wording says "Record which env vars exist. ONLY use variables that were actually discovered". LOG2 shows the agent ignored it. Needs to be: (a) explicit action to catalog the output, (b) warning that guessing names silently fails at runtime, (c) pointer to expected var names per service type as a sanity check (not a substitute). |
| import.yaml Schema injection (from themes/core.md) | ~6 KB | **Keep** — the agent is writing YAML right now against this schema. Load-bearing. |
| Rules & Pitfalls injection (from themes/core.md) | ~14.2 KB | **Filter** — most of this covers build/deploy/runtime rules the agent doesn't touch until later. Provision needs: hostname format, priority, mode immutability, port range declaration, preprocessor functions, envSecrets rules, `project:` rejection, cross-service reference syntax. Realistically ~5-7 KB of the 14.2 KB R&P block belongs to provision after the split (Import & Service Creation ~2 KB, Import Generation ~3 KB, Scaling & Platform ~0.5 KB, port range rule from Networking, plus the provision-side half of the Env Vars block). Split `themes/core.md` into a `Provision Rules` H2 and a `Generate Rules` H2 (plus leftover `Runtime Rules`), inject only `Provision Rules` here. |
| Completion command | 0.1 KB | Keep. |

**Missing content** (LOG2 bugs):

1. **Git configuration on the ZCP side of the mount** (bugs 2, 3, 13). Before any commit on the mount, the agent must run:
   ```
   git config --global --add safe.directory /var/www/{hostname}
   git config --global user.email 'recipe@zerops.io'
   git config --global user.name 'Zerops Recipe'
   ```
   And before any `zerops_deploy` to that service, the container-side git needs the same treatment:
   ```
   ssh {hostname} "git config --global --add safe.directory /var/www && git config --global user.email 'recipe@zerops.io' && git config --global user.name 'Zerops Recipe'"
   ```
   This is a 100% reproducing error on first commit and first deploy. Document once at provision step (mount subsection) and reference from generate + deploy.

2. **Managed service env var catalog** — not a static list, but an explicit instruction to **discover and record** the names from `zerops_discover` output, with a small reference table of "expected shapes" for sanity checking. The reference table is not authoritative — `zerops_discover` is — but a PostgreSQL service with no `hostname` key in its discovery output is a red flag the agent should catch and stop on, not treat as authoritative.

**Cuts + adds at provision**: ~27 KB → target ~14 KB. Drops the generate/runtime-rules portion of Rules & Pitfalls (~8 KB after keeping the ~6 KB provision-relevant slice), trims the framework secrets block (~1 KB), moves container state to generate (~0.5 KB). Adds git config section (~0.5 KB) and strengthened env var discovery instruction (~0.5 KB). Target ~14 KB reflects the corrected R&P arithmetic — earlier drafts under-estimated R&P at 8 KB and over-promised 12 KB.

**The Environment Variables straddle problem**: the `### Environment Variables — Three Levels` H3 inside R&P ([core.md:151-172](../internal/knowledge/themes/core.md#L151-L172)) is a single unified block covering `project.envVariables` (provision concern, import.yaml), `envSecrets` (provision concern, import.yaml), AND `run.envVariables` (generate concern, zerops.yaml). It cannot cleanly land in either "Provision Rules" or "Generate Rules". Three options, in order of preference:
1. **Duplicate the block** — keep the full Env Vars H3 in both Provision Rules and Generate Rules. Violates the "do not edit rule text" constraint minimally (no rewriting, just duplication) and costs ~1.5 KB of extra injection. Acceptable trade-off.
2. **Put it in Provision Rules only** — provision sees the rules once, generate has the chain recipe (which demonstrates `run.envVariables` in practice). Cheapest. Risk: agent forgets by generate time.
3. **Split the block** — separate "declarative side" (project.envVariables, envSecrets) into Provision Rules, "runtime side" (run.envVariables with `${}` refs) into Generate Rules. Requires rewriting the unified block into two halves, violating the constraint.

**Recommended**: option 1. The ~1.5 KB cost is offset by the provision cuts elsewhere, and duplication is harmless as long as the text is byte-identical (grep tests in Phase 0 can verify "identical-substring in exactly two sections").

---

### Step 3 — Generate

This is the largest step. The agent is writing the entire recipe in this one call, so it needs the most knowledge — but a lot of what's currently delivered is for LATER actions.

**Physical actions** (NestJS showcase, API-first with 3-repo worker):

1. Scaffold NestJS on apidev mount (`cd /var/www/apidev && ssh apidev "nest new ."` via the target container).
2. Scaffold Svelte frontend on appdev mount.
3. Scaffold standalone worker project on workerdev mount.
4. Write `/var/www/apidev/zerops.yaml` (dev + prod setups).
5. Write `/var/www/appdev/zerops.yaml` (dev with nodejs@22 run.base override, prod with static run.base).
6. Write `/var/www/workerdev/zerops.yaml` (dev + prod setups, no ports, worker start command).
7. Write all app code: entities, migrations, seed script, dashboard layout with placeholder slots, connectivity panel endpoint, route registrations with placeholder handlers, NATS producer (API), NATS consumer (worker), `.env.example` for each repo.
8. Write `/var/www/apidev/README.md` with 3 fragments (intro, integration-guide, knowledge-base).
9. Write `/var/www/appdev/README.md` with 3 fragments.
10. Write `/var/www/workerdev/README.md` with 3 fragments. **Each of the three codebases is a future standalone repo (`{slug}-app`, `{slug}-api`, `{slug}-worker`), and each needs its own full README.**
11. `git init` + configure safe.directory + configure user + `git add -A` + `git commit` on each mount.
12. Call `zerops_workflow action=complete step=generate` — runs structural checks.

**Knowledge the agent uses**:

| Action | Knowledge needed | Source |
|---|---|---|
| Where to write files | Per-codebase mount path mapping. For single-runtime showcase: `appdev` only. For API-first: `apidev` + `appdev` + (worker: either in host or own mount). | Recipe-specific. 5 lines. |
| Scaffold via SSH, not zcp-side | Framework CLIs need the base image's tools. Run `nest new .` inside the container, not against the mount. | Recipe-specific. 2 sentences. |
| Connection errors during generate are expected | `run.envVariables` aren't OS vars until deploy. Migrations/seeds run via initCommands later. Don't create `.env` files, don't hardcode credentials. | Recipe-specific. 1 paragraph. |
| Per-setup zerops.yaml rules | `setup: dev` MUST have `deployFiles: [.]`, NO healthCheck, mode flag (dev); `setup: prod` cross-deploy target, no `prepareCommands` installing build-only runtimes; shared worker → 3rd `setup: worker` block in host's zerops.yaml, separate worker → own repo with own dev/prod. | Recipe-specific. Already in recipe.md — keep. |
| Serve-only runtime override (static frontends) | `run.base: static` for prod only — dev needs a toolchain-bearing base (`nodejs@22`). `run.base` can differ between setups in the same zerops.yaml. | Recipe-specific, critical for Svelte/React/Vue recipes. Keep. |
| Dev-server host-check allow-list | `vite.config.ts` needs `server.allowedHosts: ['.zerops.app']`; webpack, angular, next have equivalents. Without it, browser returns "Blocked request" via public subdomain. | Recipe-specific (v6 fix). Keep. |
| **Dev-server runtime env vars** | Vite-family dev servers (Vite, webpack dev server, Next dev) read `process.env.VITE_*` / `process.env.NEXT_PUBLIC_*` **at server startup**, not at build time. For `setup: dev`, client-side env vars must be in `run.envVariables` too, or passed on the start command line. The `build.envVariables` placement bakes them into the bundle and is ONLY correct for `setup: prod`. | **MISSING. LOG2 bug 15 root cause.** My v8.52.0 compression even made this worse by replacing YAML examples with prose. |
| Chain recipe (nestjs-minimal) | The working reference for NestJS zerops.yaml shape. Currently auto-injected at generate (~7 KB). | Knowledge store `recipes/nestjs-minimal.md`. Keep — this is the most load-bearing thing in the generate step. |
| Dual-runtime URL env-var pattern | Env 0-1 shape (`DEV_*` + `STAGE_*`), env 2-5 shape (`STAGE_*` only, different hostname prefix). Direct `${STAGE_API_URL}` reference in both `build.envVariables` (bakes into bundle for prod) AND `run.envVariables` (for Vite dev server — see above). | Recipe-specific. Already in recipe.md. Keep, but add the dev-server runtime placement rule. |
| **Managed service env var names** | `${search_masterKey}` not `${search_apiKey}`. `${queue_hostname}` / `${queue_port}` / `${queue_user}` / `${queue_password}` / `${queue_connectionString}` for NATS. `${storage_apiUrl}` / `${storage_accessKeyId}` / `${storage_secretAccessKey}` / `${storage_bucketName}` for object-storage. | **Agent learned these at provision via `zerops_discover`. Must reference that catalog, not re-hallucinate.** |
| Skeleton boundary table | What generate writes (layout with placeholder slots, routes registered with stubs, model + migration + seed, connectivity panel, all 3 zerops.yaml files + READMEs) vs what deploy sub-agent writes (feature controllers, feature views, feature JS). | Recipe-specific, critical for showcase. Keep. |
| README fragment shape | 3 markers (intro, integration-guide, knowledge-base), blank line after start marker, H3 inside markers, H2 outside, comment ratio ≥30% in the embedded zerops.yaml code block. | Recipe-specific, must stay. Some content currently duplicated between generate and generate-fragments — deduplicate. |
| **Per-repo README requirement** | Each future repo (`{slug}-app`, `{slug}-api`, `{slug}-worker`) needs its OWN full README with all 3 fragments. Dual-runtime is NOT a special case — it's THREE standalone repos that happen to be in the same project. | **LOG2 bugs 9, 12 root cause.** Current doc says "The API's README.md contains the integration guide" — misleading, the agent inferred "only API needs README". |
| Writing style for comments | Aim for 35% ratio, dev-to-dev tone, explain WHY, no section decorators, max 70 chars per comment line. | Recipe-specific. Currently in generate-fragments (6 KB). Trim the voice lecture but keep the hard rules. |
| .env.example preservation | Keep the scaffolded file with empty values. Add any recipe-added keys (search host, S3 endpoint) with sensible local defaults. | Recipe-specific. Keep. |
| **Git commit on mount** | safe.directory + user.email + user.name (ZCP side) before `git add`. | **LOG2 bug 2 root cause.** Currently undocumented. Move the provision-step git config reference here as a reminder. |

**Knowledge the agent does NOT use at generate**:

- Deploy sequencing (step 4+).
- Sub-agent dispatch for feature implementation (step 4b). **Currently in generate-dashboard section (~9 KB).**
- "Where app-level commands run — ssh vs zcp-side" rule for sub-agents (~1 KB). This is a sub-agent brief concern, not generate.
- Browser walk rules (~3 KB of narrative history). Step 4c.
- Stage cross-deploy (step 5+).
- Finalize envComments shape.
- Close step sub-agent.

**Current 51 KB assembled breakdown**:

| Block | Size | Status |
|---|---|---|
| Container state during generate | 0.4 KB | Move from provision → keep here. |
| WHERE to write files | 0.5 KB | Keep — per-codebase mount paths. |
| Scaffold each codebase in its own mount | 0.8 KB | Keep — concrete anti-cross-contamination rule. |
| What to generate per recipe type | 0.8 KB | Trim — redundant with the skeleton boundary table. |
| Two kinds of import.yaml (workspace vs deliverable) | 0.5 KB | Keep. |
| Execution order — no sub-agents for zerops.yaml or README | 1 KB | Keep — specifically says "write these yourself, not via sub-agent". Load-bearing. |
| zerops.yaml Write ALL setups at once | 0.5 KB | Keep. |
| Per-codebase zerops.yaml shape variants | 0.8 KB | Keep. |
| Dual-runtime URL env-var pattern (full) | 2.8 KB | Keep — recipe-unique. **Add dev-server runtime placement rule.** |
| Per-setup dev/prod/worker rules | 2.5 KB | Keep — v6 fixes. |
| .env.example preservation | 0.2 KB | Keep. |
| Framework environment conventions | 0.1 KB | Keep. |
| "Dashboard spec" pointer | 0.4 KB | **Expand into a concrete skeleton-write checklist right here** — agent's next action is to write skeleton. |
| App README with extract fragments | 1.5 KB | Keep but deduplicate with generate-fragments. **Fix the per-repo README requirement.** |
| Code Quality (comment ratio, no placeholders) | 0.3 KB | Keep. |
| Pre-deploy checklist | 1 KB | Keep — this is the mental checklist that runs the generate checks. |
| Completion | 0.1 KB | Keep. |
| **generate-dashboard section (injected after generate)** | 9 KB | **Move to deploy.** The dashboard-implementation deep-dive (XSS, quality bar, sub-agent brief, "where app-level commands run" rule) is for sub-agent dispatch, which happens at deploy step 4b. Keep only a minimal `skeleton-write-rules` block (3-4 lines: placeholder slots, routes registered, connectivity panel, seeded model) inline in generate. |
| **generate-fragments section (injected after generate)** | 6 KB | **Trim to ~2 KB.** Hard rules stay (ratio target, blank line after marker, H3 inside / H2 outside, 3 mandatory fragments per repo); voice lecture goes. |
| Chain recipe auto-injection (nestjs-minimal) | ~7 KB | Keep — the working reference the agent is copying from. |

**Cuts + moves at generate**:
- `generate-dashboard` content (9 KB): move ~7 KB to deploy, keep ~2 KB of skeleton-write checklist inline.
- `generate-fragments` content (6 KB): trim to ~2 KB.
- Deduplications (~1 KB).
- Add: dev-server runtime env var rule (~0.5 KB), per-repo README clarification (~0.3 KB), git commit reminder (~0.2 KB).

**Target: ~30 KB assembled guide at generate**, down from 51 KB. Still large — generate really does need all of that content. But 30 KB is below the Claude Code persistence threshold (empirically ~25 KB triggers disk-persist, but 30 KB is right at the boundary; see the "remaining overflow" section below for what to do if it still overflows).

---

### Step 4 — Deploy

**Physical actions** (NestJS showcase, API-first, SEPARATE worker):

1. Deploy apidev first (`zerops_deploy targetService=apidev setup=dev`).
2. SSH apidev, start nest dev server in background.
3. Enable apidev subdomain, `zerops_verify apidev`, curl `/api/health`.
4. Deploy appdev (`zerops_deploy targetService=appdev setup=dev`).
5. SSH appdev, start Vite dev server in background.
6. Deploy workerdev (`zerops_deploy targetService=workerdev setup=dev`).
7. SSH workerdev, start worker process in background.
8. Enable appdev subdomain, `zerops_verify appdev`.
9. Fetch logs from all three dev containers, check initCommands ran.
10. **Dispatch feature sub-agent** — a framework expert with a brief to fill in the feature controllers + views + worker consumers against the live services (sub-agent runs against all 3 mounts).
11. Main agent resumes, `git add + commit` on each mount, redeploy apidev and appdev (and workerdev if changed), restart processes via SSH, curl-verify features.
12. **Browser walk phase 1** — `zerops_browser` on appdev while dev processes are running.
13. Kill dev processes via SSH on apidev, appdev, workerdev.
14. Cross-deploy stage targets in parallel: `zerops_deploy sourceService=apidev targetService=apistage setup=prod`, same for appstage, workerstage.
15. Enable stage subdomains, verify each.
16. **Browser walk phase 3** — `zerops_browser` on appstage after dev processes are dead.
17. Call `zerops_workflow action=complete step=deploy`.

That's ~17 distinct action-blocks in one step. Deploy is denser than generate in terms of action count.

**Knowledge the agent uses**:

| Action | Knowledge needed | Source |
|---|---|---|
| `zerops_deploy` parameter signature | Parameter is `targetService` (not `serviceHostname` — LOG2 bug 4). `setup` maps to the zerops.yaml setup name. `sourceService` for cross-deploy. | Recipe-specific. Needs explicit warning about the param name. |
| API-first ordering | Deploy API first so the frontend can verify against a running backend; the frontend bakes `VITE_API_URL` at build time (prod) or reads it from `process.env` at dev-server startup. | Recipe-specific. Keep. |
| Start dev processes via SSH | Primary server + asset dev server + worker process — all via background SSH. Redeploy = new container = all SSH processes die = restart. | Recipe-specific. Keep. |
| **Vite port collision** | The deploy framework may start a dev server; if the agent also starts one via background SSH, the second instance silently falls back to port 5174 which the subdomain does not route to. Always check first: `ssh appdev "pgrep -f vite || true"`. | **LOG2 bug 14 root cause. Currently undocumented.** |
| `zerops_verify` + curl for the health endpoint | Standard verification flow. | Recipe-specific. Keep. |
| `zerops_logs` to verify initCommands ran | Look for seeder output, migration output, search-index import output. | Recipe-specific. Keep. |
| **`zsc execOnce` idempotency trap** | If a deploy fails after execOnce fires (e.g., seed runs but app crashes later), the next retry with the same `appVersionId` will NOT re-run the seed. Silent data staleness. | **LOG2 bug 6 root cause. Currently undocumented.** Fix: check logs for the seed output on every retry; if the output is missing or shows a stale timestamp, `ssh {hostname} "cd /var/www && {seed_command}"` once manually to recover, then re-deploy and verify. |
| **Feature sub-agent brief** (currently in generate-dashboard) | Framework-expert sub-agent that writes feature controllers/views against live services. Brief must include: file paths, framework-conventional locations, service-to-feature mapping, UX quality contract, XSS protection, route paths, "where app-level commands run" rule (ssh vs zcp), test each feature after writing via curl/framework test runner. | **Move from generate-dashboard → here.** |
| **"Where app-level commands run"** | The sub-agent runs on zcp against SSHFS mounts. App-level commands (compilers, test runners, linters, package managers, framework CLIs, app-level curl) MUST run via SSH on the target container. Running them on zcp against the mount exhausts the fork budget (`fork failed: resource temporarily unavailable`). | **Move from generate-dashboard → here** — it's a sub-agent brief concern. |
| `zerops_browser` tool rules | Use the tool, never raw `agent-browser`. One call per URL. Auto-wraps open/commands/errors/console/close. Auto-recovers fork exhaustion. | Recipe-specific. Keep but compress — the 3 KB of v4/v5/v6 narrative history can become 3 lines ("v4-v6 crashed because of raw CLI. Use `zerops_browser`. Here's why: lifecycle + concurrency."). |
| 3-phase browser walk | Phase 1: dev walk while processes running. Phase 2: kill dev processes. Phase 3: stage walk. Never reorder. v6 tried killing first → 502 on dev walk. | Recipe-specific. Keep (compressed). |
| Stage cross-deploy parallel dispatch | All `*stage` targets are independent; dispatch in one message as parallel tool calls. | Recipe-specific. Keep. |
| Deploy failure status classification | BUILD_FAILED (buildLogs), PREPARING_RUNTIME_FAILED (buildLogs), DEPLOY_FAILED (runtime logs, NOT buildLogs, `error.meta[].metadata.command` identifies failing initCommand), CANCELED. | Recipe-specific. Keep. |
| **Git config on container side** (LOG2 bugs 3, 13) | Before the first `zerops_deploy` to any service, the container-side git needs `safe.directory` for `/var/www`. Without it, deploy fails with `fatal: not in a git directory`. | **Currently undocumented.** Move the provision-step git config reference to include this. |
| CORS for API-first | Framework-agnostic: the API's CORS middleware must allow the frontend subdomain origin. | Recipe-specific. Keep (already deframeworked). |
| Completion | 1 line. | Keep. |

**Knowledge the agent does NOT use at deploy**:

- Recipe plan fields (research).
- Workspace import shape (provision).
- zerops.yaml composition rules (generate).
- README fragment shape (generate).
- Finalize envComments.

**Current 29 KB breakdown**:

| Block | Size | Status |
|---|---|---|
| Dev deployment flow + step numbering | 4 KB | Keep — execution-order table is load-bearing for API-first. |
| Step 1 (deploy appdev) + Step 1-API | 0.8 KB | Keep. |
| Step 2 (start processes) | 2.5 KB | Keep. **Add Vite port-collision warning.** |
| Step 3 (verify) + Step 3a (check initCommands ran) | 2 KB | Keep. **Add `zsc execOnce` trap warning.** |
| Step 3-API | 0.4 KB | Keep. |
| CORS paragraph | 0.2 KB | Keep. |
| Step 4 (iterate) | 0.3 KB | Keep. |
| **Step 4b (sub-agent dispatch)** | 2 KB | **Expand** — needs the full sub-agent brief moved in from generate-dashboard. |
| Step 4c (browser walk) narrative | 3 KB | **Compress** — v4/v5/v6 history is motivational; trim to "use `zerops_browser`, here's the rule, here are the failure modes". |
| Step 4c canonical 3-phase flow | 2 KB | Keep. |
| Step 5 (verify prod setup) | 0.3 KB | Keep. |
| Step 6 (cross-deploy parallel) | 2 KB | Keep. |
| Step 7 (enable stage subdomains + verify) | 0.5 KB | Keep. |
| Deploy failure status classification table | 2 KB | Keep. |
| Common deployment issues table | 1 KB | Keep. |

**Adds**: sub-agent brief (~2 KB from generate-dashboard), container-side git config (~0.3 KB), Vite port collision warning (~0.2 KB), `zsc execOnce` trap warning (~0.3 KB), explicit `targetService` parameter warning (~0.1 KB).

**Cuts**: browser walk narrative history (~2 KB).

**Target: ~29 KB**, roughly the same size but reshuffled. The deploy step genuinely needs all of this content because it's doing the most — the goal is to make the content match what the agent actually does at this moment, and to stop importing things at generate that belong here.

---

### Step 5 — Finalize

**Physical actions**:
1. Review auto-generated recipe files in `outputDir/environments/` (6 envs × [import.yaml, README.md] + root README).
2. Write tailored `envComments` per env (6 blocks).
3. Write `projectEnvVariables` per env (dual-runtime URL constants with env 0-1 vs env 2-5 shape split).
4. Call `zerops_workflow action=generate-finalize envComments={...} projectEnvVariables={...}`.
5. Review generated import.yaml files, maybe iterate on comments if the ratio check fails.
6. Call `zerops_workflow action=complete step=finalize`.

**Knowledge the agent uses**:

| Action | Knowledge needed | Source |
|---|---|---|
| 6-env taxonomy | Env 0 AI agent / 1 Remote CDE / 2 Local / 3 Stage / 4 Small prod / 5 HA prod. | Recipe-specific. Keep. |
| Per-env service key list | Envs 0-1 use `{appdev, appstage, ...}`; envs 2-5 collapse to `{app, ...}`. Showcase worker variations (shared → only workerstage in envs 0-1; separate → workerdev + workerstage). | Recipe-specific. Keep. |
| envComments shape | Per-env block with `service` map and optional `project` field. Merge semantics (atomic per-env replace). | Recipe-specific. Keep. |
| projectEnvVariables shape | Env 0-1: all DEV_* + STAGE_*. Envs 2-5: STAGE_* only, different hostname prefix. Values emitted verbatim (platform resolves `${zeropsSubdomainHost}` at user-project-import time). | Recipe-specific. Keep. |
| Comment style rules | Aim 35% ratio, dev-to-dev tone, explain WHY, NO section decorators, NO cross-env references, 2-3 sentences per service. | Recipe-specific. **Currently duplicated with generate-fragments.** Consolidate here, since this is where the agent writes the comments. |
| Hand-edit warning | Re-running `generate-finalize` wipes manual edits. Always pass through the tool. | Recipe-specific. Keep. |
| Completion | 1 line. | Keep. |

**Knowledge the agent does NOT use at finalize**:

- How to write zerops.yaml (already written at generate).
- How to deploy (already deployed).
- Browser walk (belongs in close step).

**Current 18 KB breakdown**:

| Block | Size | Status |
|---|---|---|
| Auto-generated files listing | 0.3 KB | Keep. |
| Do NOT edit import.yaml files by hand | 0.4 KB | Keep. |
| Step 1: envComments per env | 8 KB | Keep — includes the full 6-env example which is the template the agent copies. |
| Step 1b: projectEnvVariables | 3 KB | Keep — concrete example with env 0-1 vs env 2-5 split. |
| Step 2: review READMEs | 0.2 KB | Keep. |
| Step 3: complete | 0.1 KB | Keep. |
| import.yaml Schema re-injection | 6 KB | **Remove** — the agent already wrote workspace import.yaml at provision with the same schema. By finalize, the agent is calling `generate-finalize` which has its OWN structured input (envComments + projectEnvVariables); the actual YAML emission is template-driven. The agent doesn't hand-write YAML at finalize. Import schema injection is vestigial here. |

**Target: ~12 KB**, down from 18 KB. Just by removing the re-injected import.yaml schema.

---

### Step 6 — Close

**Physical actions**:
1. Dispatch the static code review sub-agent (framework expert, NO browser, NO platform config review).
2. Apply fixes from the sub-agent's report, redeploy as needed.
3. Main agent browser walk phase 1 (dev), phase 2 (kill), phase 3 (stage).
4. Decide publish vs skip (user must have asked explicitly to publish).
5. If publish: export, create-repo, push-app (for EACH codebase — apidev/appdev/workerdev in the 3-repo case), publish environments.
6. Call `zerops_workflow action=complete step=close`.

**Knowledge the agent uses**:

| Action | Knowledge needed | Source |
|---|---|---|
| 1a / 1b / 2 structure | Static review → browser walk → maybe publish. | Recipe-specific. Keep. |
| Sub-agent prompt template | Framework expert, no platform, no browser, direct-fix scope vs symptom-only scope vs out-of-scope. Report format: CRITICAL / WRONG / STYLE / SYMPTOM. | Recipe-specific. Keep. |
| Browser walk 3-phase flow | Same as deploy step 4c. | **Currently duplicated.** Should reference deploy, not re-explain. |
| `zerops_browser` rules + tool usage | Same as deploy step 4c. | **Currently duplicated.** Reference deploy. |
| `forkRecoveryAttempted: true` recovery procedure | Kill lingering dev processes, verify on-disk state. | Recipe-specific. Can be a 3-line reference with a pointer back to deploy step 4c rules. |
| Publish commands | `zcp sync recipe export`, `create-repo`, `push-app`, `publish`, `push recipes`, `cache-clear`, `pull recipes`. | Recipe-specific. Keep. But only relevant if user asked to publish. |
| **Multi-repo push** (LOG2 implication) | For 3-repo showcase: 3 separate `create-repo` + `push-app` calls, one per codebase (`{slug}-app`, `{slug}-api`, `{slug}-worker`), each with its own source tree. | **Currently not explicit.** The close section has `--app-dir` export variants, but `push-app` doesn't parallel it. |
| Completion / skip | 1 line. | Keep. |

**Current 17 KB breakdown**:

| Block | Size | Status |
|---|---|---|
| 1a / 1b / 2 structure intro | 0.8 KB | Keep. |
| Static code review sub-agent prompt template | 3.5 KB | Keep — load-bearing, the exact prompt is the deliverable. |
| 1b main-agent browser walk procedure | 2.5 KB | **Trim to ~0.5 KB by referencing deploy step 4c.** Just the "run phases 1-3 from step 4c here" pointer, plus the close-specific gate rule ("do not call complete until both walks clean"). |
| If walk reveals a problem section | 0.5 KB | Keep. |
| `forkRecoveryAttempted: true` recovery | 1 KB | **Trim** — reference the deploy-step 4c rule. |
| 2 Export & Publish section | 4 KB | Keep but clarify multi-repo push for 3-repo case. |
| Closing completion/skip command | 0.2 KB | Keep. |

**Target: ~10 KB**, down from 17 KB. The cut comes almost entirely from removing browser-walk duplication.

---

## Size summary before vs after (target)

Targets revised against measured baselines + corrected R&P size.

| Step | Current (measured) | Target | Delta | Notes |
|---|---|---|---|---|
| research | 15.1 KB | 10 KB | −5 KB | showcase+minimal concat — cap limited by research-showcase floor |
| provision | 26.8 KB | 14 KB | −13 KB | R&P split keeps ~6 KB at provision, drops ~8 KB |
| generate | 48.3 KB | 32 KB | −16 KB | generate-dashboard deletion, fragments trim, browser narrative trim |
| deploy | 28.2 KB | 32 KB | +4 KB | sub-agent brief + pitfalls move in |
| finalize | 17.5 KB | 14 KB | −3 KB | drop vestigial import.yaml Schema, add voice content |
| close | 16.1 KB | 10 KB | −6 KB | dedupe browser walk reference |
| **Total** | **152 KB** | **112 KB** | **−40 KB** | |

The critical case — generate — goes from 48 KB to ~32 KB. That's still above the ~25 KB Claude Code inline-display threshold, which means the agent may STILL hit persisted-output wrapping. But 32 KB is one persistence event the agent can handle with a single `Read` of the persisted file, not an eight-slice python heredoc loop. And if even 32 KB is too much, the next lever is a deferred-load mechanism for the chain recipe (Option A from the earlier post-mortem: replace the auto-injection with an explicit pointer to `zerops_knowledge recipe={framework}-minimal`).

**Research cap (10 KB)**: research cannot hit 5 KB because [recipe_guidance.go:63-67](../internal/workflow/recipe_guidance.go#L63-L67) concatenates research-showcase + research-minimal at showcase tier. Even aggressive trimming of research-minimal to 2 KB leaves research-showcase as a 7.5 KB floor — and research-showcase itself only compresses to ~5 KB under Phase 2.4's Additional Showcase Fields + worker decision trim. Total realistic floor: ~7 KB with headroom for the separator and attestation context. 10 KB cap gives ~3 KB breathing room.

**Provision cap (14 KB)**: earlier drafts promised 12 KB against an under-counted R&P (claimed 8 KB, actual 14.2 KB). With the correct split retaining 6-7 KB of provision-relevant rules, the realistic floor is ~12 KB of assembled content; 14 KB cap accommodates the Env Vars straddle duplication.

Provision and generate drop below the threshold partially. Research, finalize, and close are comfortably below. Deploy is the one we cannot shrink because the agent genuinely needs all of that content at deploy time — the fix for deploy is reshuffling, not cutting.

---

## The LOG2 bugs, reclassified against the reshuffle

| # | LOG2 bug | Current root cause | Fix location |
|---|---|---|---|
| 1 | 53 KB tool result overflow | Generate section bloated because sub-agent brief and dashboard spec were inlined here instead of at deploy | Reshuffle generate → deploy |
| 2 | `git commit` missing user.email/name | Undocumented | Add to provision (mount subsection) |
| 3 | git "dubious ownership" (SSHFS, ZCP side) | Undocumented | Add to provision (mount subsection) |
| 4 | `zerops_deploy` rejected `serviceHostname` | Param name mismatch not explicitly called out | Add explicit warning to deploy (step 1) |
| 5 | `MeiliSearch` vs `Meilisearch` | Agent hallucination, not a ZCP concern | Not ours to fix |
| 6 | Seed silently skipped because `zsc execOnce` burned | Platform behavior undocumented | Add to deploy (step 3a) |
| 7 | `${search_apiKey}` hallucination | Missing env var catalog + weak `zerops_discover` guidance | Strengthen provision (discover instruction) + reference catalog from generate |
| 8 | `zerops_env set` rejected "UserData key not unique" | Platform behavior undocumented | Add to provision as sidebar under env var section |
| 9 | Generate check `app_readme_exists` (appdev README missing) | Doc implies only API needs README; check requires all codebases | **Product decision received: each repo needs its own README**. Fix doc, keep check. Add to generate per-repo README requirement. |
| 10 | Generate check `comment_ratio` 16% < 30% | Guidance says 35% target but agent underestimated anyway | Strengthen at generate — show a before/after example, not just a number |
| 11 | Fragment intro blank-line missing | Rule exists but agent missed it | Strengthen at generate — move rule to a HARD RULE callout |
| 12 | appdev README missing 2 of 3 fragments | Same as 9 | Same fix |
| 13 | Container-side git `safe.directory` | Undocumented | Add to deploy (step 1 pre-flight) |
| 14 | Vite port collision 5173 vs 5174 | Undocumented | Add to deploy (step 2) |
| 15 | `VITE_API_URL` in build.envVariables not available to Vite dev server | Doc bug — dev-server runtime env var placement wrong | Add to generate (dual-runtime URL env-var pattern, dev-server subsection) |

---

## The env var architecture problem — bigger than LOG2

The user raised a deep concern: env vars and their propagation are one of the biggest pain points in Zerops recipe work. Let me map the current state of things to surface why.

### Three sources of truth exist today

1. **Zerops REST API** (`GetServiceStackEnv` / `GetProjectEnv`) — what ZCP currently uses via `zerops_discover includeEnvs=true`. Returns the "declared" env state (what's been SET via the API). This is what the platform will inject into the NEXT container start.

2. **In-container `http://localhost:10303/env/`** — available inside every Zerops container. Returns the latest desired state from the sandbox daemon's perspective, including values that were set AFTER the current container started. This is the same information (usually), just fetched from a different path.

3. **The running container's actual `os.environ`** — what the running process sees. This is the PREVIOUS state: set from whatever was in the REST API at the moment the container started, frozen from then on (env vars are cached at process start; a mid-run `zerops_env set` does not reach a running process until restart).

These three sources can disagree:
- After `zerops_env set` but before service restart: REST API (new) ≠ `10303/env/` (new) ≠ `os.environ` (old).
- After `zerops_env set` with auto-restart: all three converge.
- When cross-service references like `${db_hostname}` are used: REST API stores the literal template; `10303/env/` returns the resolved value; `os.environ` has whatever was resolved at start time.

The v8.53.0 cross-service-ref warning in `environment-variables.md` addresses the literal-template issue for REST API reads. But we have no tooling for "what does the running process actually see", and we have no way to detect the "stale-until-restart" window other than by the agent remembering whether `skipRestart=true` was used.

### LOG2 in this light

LOG2 bug 7 (`${search_apiKey}` hallucination) is actually three bugs stacked:

1. Agent didn't read the env var names from `zerops_discover` output (caller's fault, but also weak doc).
2. Agent's wrong name `${search_apiKey}` landed in zerops.yaml `run.envVariables`.
3. At runtime, `${search_apiKey}` resolved to the literal string `"${search_apiKey}"` because the platform interpolator treats unknown cross-service refs as literals, not errors.

Bug 3 is the silent failure mode that makes env var mistakes catastrophically hard to debug. The correct name `${search_masterKey}` was one docs lookup away; the wrong name gave a 401 from Meilisearch at runtime with no platform error, no validation, nothing. Half a day of debugging.

### What would fix this class of problem

Three layers of defense, each independent:

1. **Authoritative env var catalog per managed service type, injected at provision**. Not a suggestion ("run zerops_discover and record the names"), but a delivered table the agent can cross-reference. `postgresql@17` → `hostname, port, user, password, dbName, connectionString`. `meilisearch@1` → `hostname, port, masterKey, defaultAdminKey, defaultSearchKey`. This goes into `themes/services.md` (already has partial coverage, make it authoritative) and gets injected at provision as a reference card.

2. **A `zerops_env_check` tool** that, given a service hostname and a set of proposed env var references (from a zerops.yaml), validates each reference against the authoritative catalog and the live `zerops_discover` output. Flag unknown vars BEFORE deploy. This is a new MCP tool.

3. **A `needs-restart` detector**, leveraging the `10303/env/` endpoint inside a target container. Two sub-options:
   - **(3a)** `zerops_env action=check serviceHostname=X` — SSH into the container, curl `10303/env/`, diff against `os.environ` of a running process on that container (`cat /proc/{pid}/environ`). Surfaces "env X is set at the platform but not yet loaded by the running process, restart needed". This would catch the "I set an env var 10 minutes ago and forgot to restart, my code is still using the old value" class of bug.
   - **(3b)** A watcher inside the tool call flow: after any `zerops_env set`, auto-compare before/after and report what changed vs what the running process sees. Same data as 3a but surfaced at the moment of the change.

Option 3 is not urgent — it's a sharp tool for a specific debugging flow, not something every recipe run needs. Options 1 and 2 directly fix LOG2 bug 7's class.

---

## Per-repo README requirement (user's product decision)

Confirmed: the showcase recipe publishes to 3 GitHub repos (`{slug}-app`, `{slug}-api`, `{slug}-worker`), each with its own README that includes the integration guide for THAT codebase's zerops.yaml. The "only API needs README" wording in current recipe.md is a doc bug — fix the doc, the check is right.

**Implication for generate**: for a 3-repo showcase, the agent writes 3 full READMEs, each with all 3 extract fragments. Each README's integration-guide fragment contains the matching zerops.yaml for that codebase. Each README's knowledge-base fragment lists the framework-specific gotchas specific to that role (API vs frontend vs worker).

**Implication for the generate check**: `app_readme_exists` must be renamed or extended to check every codebase's README. Already does this implicitly (it looks at the list of mount paths), but the error message should say "README missing at /var/www/apidev, /var/www/appdev, /var/www/workerdev" to make the expectation explicit.

**Implication for close/publish**: `zcp sync recipe push-app` needs to be called once per codebase, OR updated to accept multiple `--app-dir` flags (it already does for export — make push-app symmetric).

---

## Anti-patterns in the current content placement

Patterns I noticed while walking every step:

1. **Narrating history in guidance.** The deploy step has ~3 KB on "v4 and v5 crashed because of raw agent-browser, here's the v4 incident, here's the v5 incident, here's the fork budget mechanics, here's why pkill recovery exists". The agent doesn't need to read the history to follow the rule; it needs the rule. History belongs in the post-mortem directory, not in the tool response.

2. **Pre-loading content because it'll be needed later.** Research has the chain recipe loading instruction, but generate auto-injects the same recipe. Provision has the full Rules & Pitfalls block, but most of the rules fire at generate or deploy. Finalize re-injects the import.yaml schema, but the agent doesn't hand-write YAML at finalize. Each of these is "hold this for later" content the agent absorbs and forgets before it's needed.

3. **Describing internal structures the agent never touches.** Research spends ~1 KB describing `RecipeTarget` field semantics — but the `RecipeTarget` jsonschema tags already carry those descriptions at the tool-input schema level. The agent sees the schema directly. Doc is redundant.

4. **Mixing motivational commentary into action instructions.** "This is the only order that works. v6 tried reversing it and hit 502s." → "Walk dev first, kill second, walk stage third." The reason becomes a footnote, not the main text.

5. **Content duplication across steps for reference purposes.** Close step duplicates deploy's browser walk rules. Provision and generate both describe `zerops_discover`. Generate and finalize both describe comment style. Each duplicate is a fork in the road for the agent: which version do I follow? Deduplicate by keeping the content at the moment of first use and referencing from later steps.

6. **Treating the tool response as a teaching document.** Research currently tries to teach the agent what an HTTP port is, what a package manager is, what a migration command is. The agent knows these things. The tool response is not a tutorial — it's a set of decisions the agent has to make that it can't make from training data alone. Scope the content to what the agent CAN'T derive.

---

## Known constraints discovered during triple-verification

These are not new findings — they're cross-checks from walking the codebase that the implementation guide must respect.

### The existing "don't re-inject Rules & Pitfalls at generate" comment

[recipe_guidance.go:157-162](../internal/workflow/recipe_guidance.go#L157-L162) contains an explicit, intentional comment explaining why R&P is NOT injected at generate today:

> Rules & Pitfalls: NOT injected here. The agent already received the full 13KB at provision (one step ago). The chain recipe demonstrates the rules in practice. Re-injecting would triple-teach the same lifecycle rules (recipe.md static text + chain example + R&P). If the agent needs a specific rule, zerops_knowledge is available for on-demand queries.

(The "13KB" in the comment is stale — real size is 14.2 KB.)

The Phase 1 split proposes to add a new "Generate Rules" injection at generate. This reverses the prior decision. It is defensible because:
- After the split, Generate Rules is a SMALLER slice (~4-5 KB) than the full R&P, not the full 14 KB block.
- The generate-step content no longer includes provision-side rules, so the "triple-teach" concern is weaker.
- The chain recipe shows how to apply build/runtime rules in practice, which complements rather than duplicates the textual rules.

**But**: the implementation doc must explicitly DELETE that comment as part of Phase 1.2 and replace it with a new comment explaining the split. Leaving the old comment creates a contradiction between code behavior and documentation.

### Per-repo README writing is hardcoded in production code, not just recipe.md

LOG2 bugs 9 and 12 are doc-symptoms of a deeper hardcoding: [recipe_templates.go:57](../internal/workflow/recipe_templates.go#L57) writes `files["appdev/README.md"] = GenerateAppREADME(plan)` — a single README at a single hardcoded path. Likewise [recipe_overlay.go:43,58,64](../internal/workflow/recipe_overlay.go#L43) overlays `files["appdev/README.md"]` — also hardcoded.

Meanwhile, the generate-step check at [workflow_checks_recipe.go:85-123](../internal/tools/workflow_checks_recipe.go#L85-L123) correctly iterates every runtime target and looks for `{hostname}dev/README.md` per target. **So the check is already correct — the templates and overlay are the broken side.**

This means Phase 9 (fixing the per-repo README regression) is not purely a doc fix. It must ALSO:
- Update `recipe_templates.go:57` to iterate runtime targets and write `{hostname}dev/README.md` per target
- Update `recipe_overlay.go:OverlayRealAppREADME` to overlay every codebase's README, not just `appdev`
- Update the stderr message at [engine_recipe.go:336](../internal/workflow/engine_recipe.go#L336) which hardcodes `"appdev/README.md"`
- Update corresponding tests in `recipe_templates_test.go` (currently asserts exactly `files["appdev/README.md"]` exists)

Scope-wise this is still in the reshuffle because LOG2 flagged it; just heavier than a doc edit.

### Publish CLI is hardcoded to `{slug}-app` — multi-repo publish does NOT work today

[publish_recipe.go:191](../internal/sync/publish_recipe.go#L191) and [publish_recipe.go:236](../internal/sync/publish_recipe.go#L236) both compute `repoName := slug + "-app"`. There is no way to publish a second or third app repo for the same slug. The 3-repo showcase model (`{slug}-app`, `{slug}-api`, `{slug}-worker`) cannot be executed via the current CLI — passing `nestjs-showcase-app` as the slug produces a repo named `nestjs-showcase-app-app` (double suffix).

**Implication for Phase 6**: the recipe.md content describing multi-repo publish (showing `create-repo {slug}-app` / `{slug}-api` / `{slug}-worker`) cannot be written until the CLI is extended. Two paths:
1. **Scope-limit Phase 6**: for now, the showcase publishes ONLY the primary `{slug}-app` repo with the full source tree (including api + worker subdirectories). Document this explicitly. The "3-repo showcase" is a future feature.
2. **Extend the CLI in Phase 6**: add a `--repo-suffix` (or similar) flag to `create-repo` and `push-app` so the caller can override the `-app` suffix. Then write the recipe.md example against the extended shape.

Path 2 is the clean fix but adds ~1 day of CLI work to the reshuffle. Path 1 is the conservative scope-limit that ships faster. Whichever path is chosen, Phase 6's ordering must put the CLI decision BEFORE the recipe.md edit, not after.

---

## What we are not doing

For clarity, the reshuffle deliberately does NOT do these things:

- **No new workflow steps.** We're not splitting generate into generate-config + generate-code, or any other state-machine changes. The 6-step shape stays.
- **No new deferred-load mechanism.** The chain recipe injection stays auto-loaded. The dashboard spec stays inline (at deploy now instead of generate). If generate still overflows at 30 KB, we reconsider, but one thing at a time.
- **No changes to validation or checks.** The generate checks still require the same fragments, the same comment ratio, the same file existence. We're only changing what the agent is told to produce, not what ZCP verifies.
- **No changes to the MCP tool surface.** FlexBool, `get` action, etc. from v8.53.0 stay. We're not adding `zerops_env_check` or the `needs-restart` detector in this round — those are separate follow-ups called out above.
- **No framework-specific content.** Every example stays deframeworked (as per v8.52.0's Fix F). NestJS appears in this document only as the session-under-analysis.

---

## Explicit test coverage plan

To prevent my v8.52.0 measurement mistake, the implementation guide will add one test per step asserting that `BuildResponse.Current.DetailedGuide` (the thing the agent actually reads, not `resolveRecipeGuidance` alone) stays under the target:

```go
func TestRecipe_DetailedGuide_ShowcaseUnderCap(t *testing.T) {
    tests := []struct {
        step string
        cap  int
    }{
        {RecipeStepResearch, 8 * 1024},
        {RecipeStepProvision, 18 * 1024},
        {RecipeStepGenerate, 32 * 1024},
        {RecipeStepDeploy, 32 * 1024},
        {RecipeStepFinalize, 16 * 1024},
        {RecipeStepClose, 14 * 1024},
    }
    // ... for each, advance a showcase state to the step, call BuildResponse, assert
}
```

Each cap is the target + a small buffer. The test must run against the real embedded knowledge store (`knowledge.GetEmbeddedStore()`), not a mock, so chain recipe injection is counted.

Additionally, every piece of content moved between steps gets a grep test: "the string X appears in exactly ONE step", so future edits can't re-duplicate content by accident.

---

## Open questions for the implementation

1. **`zsc execOnce` trap recovery** — when the agent detects a burned `appVersionId` with a failed initCommand, the correct recovery is either (a) manually run the failed command via SSH then redeploy (breaks the "never manually patch" rule) or (b) force a new deploy with a different version ID. Is there a `zerops_deploy` flag to force a new version ID even without source changes? If not, the recovery is ugly.

2. **`zerops_env set` UserData-lock error (LOG2 bug 8)** — the error says "UserData key not unique" when the agent tries to set a var that's declared in zerops.yaml `run.envVariables`. Currently no way to unset a `run.envVariables` entry via `zerops_env` — the only path is to edit zerops.yaml and redeploy. Worth a note at provision/deploy, but not something to fix in the reshuffle.

3. **Browser walk rule deduplication** — deploy has the full rules, close currently duplicates them in the markdown `<section name="close">` body (not via knowledge injection — the browser rules are not injected anywhere). The proposal is to keep the full rules at deploy's section and reference from close's section. Close runs the browser walk independently of deploy, but the agent processed deploy's response earlier in the same session, so referencing "see deploy Step 4c" is workable. The risk is minimal because no cross-session recall is required.

---

## Next action

Implementation guide in the companion document. This research document is for understanding the "why"; the implementation guide is a sequenced, verifiable list of edits the opus-level implementor runs against the codebase.
