# Run 12 readiness — implementation plan

Run 11 (`nestjs-showcase`, 2026-04-25) was the first dogfood after the
post-`run-11-readiness` cleanup pass. The cleanup demoted ~14 string-
pattern validators from blocking to notice; ran clean (no regressions);
the V-5 abstract litmus held. But the dogfood revealed that the
catalog-drift problem the cleanup targeted was not the load-bearing
content-quality problem. The published recipe ships the **wrong env
pattern** (cross-service refs read directly from `process.env`,
platform-coupled code), the **CLAUDE.md template is not porter-runnable**
(every dev-loop block tells the porter to invoke `zerops_dev_server`,
an MCP tool the porter doesn't have), and **finalize never reached
`ok:true` through `record-fragment`** — six minutes of direct file
mutation closed the run. The single root cause for the env pattern is
in [`briefs/scaffold/platform_principles.md`](../../../internal/recipe/content/briefs/scaffold/platform_principles.md):
the brief instructs the agent NOT to declare `DB_HOST: ${db_hostname}`
on the false claim that it self-shadows. Different keys don't shadow;
only same-key redeclaration shadows. The on-demand
[`internal/knowledge/guides/environment-variables.md`](../../../internal/knowledge/guides/environment-variables.md)
is correct (lines 64-91 explicitly endorse `DB_HOST: ${db_hostname}` as
the "framework-convention rename" pattern). The atom contradicts the
guide. The agent followed the atom.

Run 12 ships the content-side teaching corrections (E, A, C, I, M) so
the recipe stops teaching the wrong env pattern and the CLAUDE.md
content stops referencing MCP tools as the porter dev loop. Then ships
the engine-flow fixes (G, R, B, D) so finalize closes through
documented APIs without hand-edits. Then the engine cosmetic cluster
(Y1, Y2, Y3) cleans up the three visible yaml emitter defects.

Reference material:

- [docs/zcprecipator3/runs/11/ANALYSIS.md](../runs/11/ANALYSIS.md) — run 11 verdict, completes-phase trace, three engine bugs (B5-a..f) recorded as facts #7/#8/#9.
- [docs/zcprecipator3/runs/11/CONTENT_COMPARISON.md](../runs/11/CONTENT_COMPARISON.md) — surface-by-surface vs `/Users/fxck/www/laravel-showcase-app/`. Honest aggregate **6/10**. Top single-fix levers: env-var-model rewrite + CLAUDE.md MCP-tool removal.
- [docs/zcprecipator3/runs/11/PROMPT_ANALYSIS.md](../runs/11/PROMPT_ANALYSIS.md) — 5 dispatch prompts extracted, brief-vs-engine sizing, 18 named smell items (R-1..R-18), fix-stack ordered by leverage.
- [docs/zcprecipator3/system.md](../system.md) §4 — TEACH/DISCOVER line. Audience model in §1: every published surface reader is a **porter**, not a recipe author, not the recipe-authoring agent.
- [/Users/fxck/www/laravel-showcase-app/](/Users/fxck/www/laravel-showcase-app/) — apps-repo reference. **Important caveat**: laravel-showcase itself is imperfect in what we strive for. Reference is a floor, not a ceiling. Specifically: laravel CLAUDE.md still mentions zcp MCP for platform operations; we want apps-repo CLAUDE.md to be completely non-zcp (the porter has their own Zerops project + their own editor, no zcp control plane). Reference IG mixes generic "deploy on Zerops" content with framework-specific (Laravel) integration — we want the IG to focus on platform mechanics that change for Zerops, not framework configuration.
- [docs/zcprecipator3/plans/run-11-readiness.md](run-11-readiness.md) — prior run's plan. U / V / M / N / O / P / R / Q / S all shipped.
- [docs/zcprecipator3/CHANGELOG.md](../CHANGELOG.md) — top entries: "2026-04-25 — cleanup: gates → notices per system.md §4" + "architectural reframe".

---

## 0. Preamble — context a fresh instance needs

### 0.1 What v3 is (one paragraph)

zcprecipator3 (v3) is the Go recipe-authoring engine at [internal/recipe/](../../../internal/recipe/). Given a slug, it drives a five-phase pipeline (research → provision → scaffold → feature → finalize) producing a deployable Zerops recipe. The engine never authors prose — it composes briefs from atoms + Plan, renders templates, runs validators on stitched output, and classifies recorded facts. Sub-agents (Claude Code `Agent` dispatch) author codebase-scoped fragments at the moment they hold the densest context; the main agent authors root + env fragments at finalize. Per-codebase apps-repo content (README + CLAUDE.md + zerops.yaml + source) lives at `<cb.SourceRoot>` = `/var/www/<hostname>dev/` (the SSHFS-mounted dev slot). Recipe-repo content (recipe-root README + 6 tier folders) lives at `<outputRoot>/`.

### 0.2 Two audiences, two voice rules

System.md §1 names the audience explicitly: every published surface's reader is a **porter**. Not a recipe author. Not someone learning Zerops generally. Not the agent that ran the recipe. This is the load-bearing audience constraint and the source of run 11's biggest content-quality miss.

Two voice rules flow from this:

- **Apps-repo CLAUDE.md** is read by an AI agent or human developer working in this codebase, with their OWN Zerops project (not zcp's authoring project) and their OWN editor. They run `npm run dev` directly, not via MCP. They do not have `zerops_dev_server`, `zerops_deploy`, `zerops_verify`, or any zcp control-plane primitive. Run 11's CLAUDE.md content fails this — every dev-loop section tells them to use `zerops_dev_server`. Workstream **C**.

- **Apps-repo zerops.yaml** is what THIS PORTER deploys with. The runtime container has platform-injected cross-service env vars under platform-specific names (`db_hostname`, `cache_port`, `broker_user`, `*_zeropsSubdomain`). Code that reads those names directly is coupled to Zerops; the same code can't run locally without renames. The right pattern is to declare own-key aliases in `run.envVariables` (`DB_HOST: ${db_hostname}`, `APP_URL: ${zeropsSubdomain}`) and read clean own-key names in code. Laravel reference does this. Run 11's nestjs apps don't. Workstream **E**.

Both targets exceed laravel-showcase reference quality — the user explicitly noted laravel-showcase is "far from perfect" on these axes. Reference is a floor.

### 0.3 Where run 11 stopped + classes of defect

Run 11 closed all five phases and produced a publishable deliverable, but only after 6 minutes of direct file edits in finalize that bypassed `record-fragment`. The agent recorded three engine-bug facts during the run (#7 brief vs validator contradiction, #8 tier-5 Meilisearch HA contradiction, #9 finalize-residual-violations-unfixable-via-record-fragment). Honest content grade: **6/10 vs reference**, where reference (laravel-showcase-app) is itself a floor.

**Foundation bugs** (the three teaching errors that cascade across the run):

- **E** — `briefs/scaffold/platform_principles.md:21-23` instructs the agent: "Do NOT declare `DB_HOST: ${db_hostname}` — the platform alias and the redeclaration self-shadow and blank at container start." This is wrong. `DB_HOST` and `db_hostname` are different keys; different-key aliasing does not shadow. The on-demand guide [`internal/knowledge/guides/environment-variables.md`](../../../internal/knowledge/guides/environment-variables.md#L64-L91) is correct — the brief contradicts the guide. The orphan atom [`principles/env-var-model.md`](../../../internal/recipe/content/principles/env-var-model.md#L35-L38) carries the same wrong rule but is not actually loaded by any brief.

- **A** — No atom or brief explains that `${<host>_zeropsSubdomain}` and `${zeropsSubdomain}` are **full HTTPS URLs**, not hostnames. Three sub-agents in run 11 independently shipped the `https://${alias}` double-prefix bug (scaffold-app dispatch wrapper line 25 literally embedded it: `VITE_API_URL: "https://${apidev_zeropsSubdomain}"`; scaffold-api line 435 wrote the CORS code with the same shape; the feature-phase CORS bug took 8 minutes to diagnose). The env-var-model atom only covers `${zeropsSubdomainHost}` (the templating-time variant), not the runtime-injected aliases.

- **C** — `briefs/scaffold/content_authoring.md` line 29 says CLAUDE.md/notes carries "operator notes (dev loop, SSH)". Line 107 routes "Dev loop / SSH / curl → claude-md/notes". `principles/dev-loop.md` teaches `zerops_dev_server` to the AUTHORING agent. Nothing tells the agent that the `zerops_dev_server` invocation is the AUTHORING-time tool and the porter-facing CLAUDE.md should carry the framework-canonical command. All three run-11 CLAUDE.md files have `zerops_dev_server action=start hostname=...` as the dev loop.

**Engine-flow bugs** (finalize cannot close cleanly through `record-fragment`):

- **G + R** — Codebase-scoped surface validators (`SurfaceCodebaseIG`, `SurfaceCodebaseKB`, `SurfaceCodebaseCLAUDE`, `SurfaceCodebaseZeropsComments`) run only at finalize complete-phase ([gates.go::FinalizeGates](../../../internal/recipe/gates.go#L56)). But their target content is authored at scaffold/feature. The lag means finalize gets violations on content it was told (correctly) not to touch. `record-fragment` is append-only on those ids ([handlers_fragments.go::isAppendFragmentID](../../../internal/recipe/handlers_fragments.go#L81-L94)) — re-recording APPENDS instead of replacing, so finalize can ADD content but cannot REMOVE or REWRITE bad lines. Run-11 fact #9 captured this. Six-minute hand-edit pass was the only path forward.

- **B** — `BuildFinalizeBrief` ([briefs.go:228-290](../../../internal/recipe/briefs.go#L228-L290)) emits 3.4 KB. Main agent wraps with 10 KB of hand-typed content (tier map, hostname sets, fragment list, audience paths, citation list). Same shape as run-10 F1 carry-forward; run-11 S-1 partially shipped (action exists, content too sparse to forgo wrapping).

- **D** — `phase_entry/scaffold.md:75-82` mandates "pass `brief.body` byte-identical" and notes `verify-subagent-dispatch` is "planned but not yet implemented in v3 — for now, do not paraphrase." Run 11 main agent paraphrased anyway: scaffold-app dispatch prompt is **9,047 bytes vs the engine brief's 14,582 bytes** (62% — main truncated the brief). scaffold-worker dispatch is **7,359 vs 14,344** (51%). The wrong-env teaching from the brief made it through paraphrase intact — but other content didn't.

**Engine cosmetic bugs** (visible in published yaml, mechanical fixes):

- **Y1** — `writeComment` ([yaml_emitter.go:380](../../../internal/recipe/yaml_emitter.go#L380)) unconditionally prepends `# ` per line. Agents author fragment bodies with leading `# ` per line. Result: every comment line is `# # …`. Counts: 32/33/48/48/55/56 across 6 published tier yamls = **272 lines disfigured**.

- **Y2** — `writeRuntimeDev/Stage` ([yaml_emitter.go:212-226](../../../internal/recipe/yaml_emitter.go#L212-L226)) looks up `comments[cb.Hostname + "dev"]` and `+ "stage"`. Agents record `env/<N>/import-comments/<bare codebase name>` as the brief instructs. The map keys differ. Result: tier 0 + 1 yaml runtime services have NO comments at all (16-line gap between dev-pair tiers' 32-33 `# #` lines and single-slot tiers' 48 — three codebases × ~5 lines each silently dropped).

- **Y3** — `writeNonRuntimeService` ([yaml_emitter.go:281-283](../../../internal/recipe/yaml_emitter.go#L281-L283)) applies `tier.ServiceMode` to every `ServiceKindManaged` uniformly, no per-service capability check. Tier 5 emits `mode: HA` for `meilisearch@1.20`, which is not HA-capable. The comment block 6 lines above correctly says "Meilisearch stays single-node"; the yaml field contradicts. Run-11 fact #8.

**Content discipline** (one-shot rewrite, low risk):

- **I** — IG scope contamination. Run-11 apidev IG has 11 items; items 9-11 (NATS subject naming, cache key shape, image storage layout) are recipe-internal contracts, not "what changes for Zerops" content. Reference (laravel) keeps these clean. Brief should mandate IG = "deploy your app on Zerops" only; recipe-internal contracts go to KB or claude-md/notes.

- **M** — Mount-state pre-condition. Three scaffold sub-agents in run 11 independently rediscovered that `/var/www/<host>dev/` arrives with a `.git/` repo (created by `ops.InitServiceGit` at mount time) and `deploy` commits (created by `zerops_deploy` on each call). Three different recoveries, ~13s each. Brief should describe the state in advance and prescribe the recovery once.

### 0.4 Workstream legend

Each workstream maps to one named gap above. Tranche structure sequences by dependency.

| Letter | Scope | Tranche | Type |
|---|---|---|---|
| E | Rewrite scaffold brief env teaching: same-key shadow only; endorse own-key aliasing as recommended pattern | 1 | content (~15 lines) |
| A | Alias-type contracts atom: `*_zeropsSubdomain` is full HTTPS URL, `*_hostname` is bare host, `*_port` is port number | 1 | content (~25 lines) |
| C | CLAUDE.md is porter-facing, no zcp MCP refs in dev-loop section; framework-canonical commands instead | 1 | content (~10 lines) |
| I | IG scope rule: items 2+ are "what changes for Zerops" only; recipe-internal contracts route to KB or claude-md/notes | 1 | content (~5 lines) |
| M | Scaffold brief preamble describing `.git` mount state + recovery pattern | 1 | content (~10 lines) |
| G | Split `FinalizeGates()` into `CodebaseGates()` (IG/KB/CLAUDE/yaml-comments) + `EnvGates()` (root/env READMEs/import-comments). `gatesForPhase()` runs codebase gates at scaffold + feature complete-phase | 2 | engine (~40 LoC) |
| R | Add `record-fragment mode=replace` parameter; brief tells agent to use replace when correcting their own previously-recorded fragment | 2 | engine (~25 LoC) |
| B | Engine-composed finalize brief — `BuildFinalizeBrief` emits the full prompt, including tier map / hostname sets / fragment list / citation list. Main agent passes byte-identical | 2 | engine (~150 LoC) |
| D | Implement `verify-subagent-dispatch` action; scaffold/feature dispatch tool calls verify dispatched prompt against engine brief; mismatch returns actionable error | 2 | engine (~40 LoC) |
| Y1 | `writeComment` strips a leading `# ` from each fragment line before re-prefixing | 3 | engine (~5 LoC) |
| Y2 | `writeRuntimeDev/Stage` falls back to `comments[cb.Hostname]` if the slot-keyed lookup is empty | 3 | engine (~6 LoC) |
| Y3 | `Service.SupportsHA bool`; `writeNonRuntimeService` substitutes `NON_HA` when `tier.ServiceMode==HA && !svc.SupportsHA`. Initial table: postgresql/valkey/nats HA; meilisearch NOT HA | 3 | engine (~15 LoC) |

Tranche 1 unblocks the foundation teaching errors. Tranche 2 unblocks finalize. Tranche 3 polishes published yaml. Run 12 can ship Tranche 1+2 and be viable; Tranche 3 strongly recommended but not structurally blocking.

---

## 1. Goals for run 12

A `nestjs-showcase` (or fresh slug) recipe run that, compared to run 11 + the laravel-showcase-app reference:

1. **Apps-repo `zerops.yaml run.envVariables` declares own-key aliases** for every cross-service var the codebase reads. `DB_HOST: ${db_hostname}`, `APP_URL: ${zeropsSubdomain}`, `BROKER_USER: ${broker_user}`, etc. Code reads `process.env.DB_HOST`, `process.env.APP_URL`, `process.env.BROKER_USER` — clean platform-agnostic names.

2. **Apps-repo IG #3 (or wherever env teaching lands) endorses own-key aliasing** as the recommended pattern. Same-key shadow trap explained as the actual trap (declaring `db_hostname: ${db_hostname}` shadows; declaring `DB_HOST: ${db_hostname}` does not). Reference: [environment-variables.md:64-91](../../../internal/knowledge/guides/environment-variables.md#L64-L91).

3. **Apps-repo `*_zeropsSubdomain` references are correct** — code that builds an Origin/Referer/fetch URL uses the alias as-is (already includes `https://`). Zero occurrences of `https://${<host>_zeropsSubdomain}` in any committed source file or zerops.yaml run.envVariables block.

4. **Apps-repo CLAUDE.md is porter-runnable** — dev-loop section is `npm run start:dev` (or framework-canonical equivalent), not an MCP tool invocation. Zero references to `zerops_dev_server`, `zerops_deploy`, `zerops_verify`, or any zcp tool name. Zero `zcp` mentions outside an explicit "Working with this app via zcp (optional)" section if at all.

5. **Apps-repo IG focuses on "deploy your app on Zerops"** — no recipe-internal contracts (subject naming, cache key shape, image storage layout) inside the integration-guide extract markers. Recipe-internal contracts route to KB or claude-md/notes.

6. **Finalize closes through `record-fragment`** — zero direct file edits in finalize. Codebase-scoped validators surface at scaffold/feature complete-phase, the right author fixes via `record-fragment mode=replace`. By finalize, codebase-scoped surfaces already pass.

7. **Engine-composed finalize brief** — `BuildFinalizeBrief` emits the full prompt; main agent passes byte-identical. Zero hand-typed math errors, zero obsolete paths. Dispatch prompt size ≈ engine brief size (no truncation).

8. **Dispatch integrity verified** — every Agent dispatch call carries a brief whose body matches the most recently composed `build-brief` response for that kind+codebase. Engine-side check rejects paraphrased dispatches.

9. **Published tier yaml comments are single-prefixed** — zero `# # ` doubled-prefix lines across the 6 emitted import.yamls.

10. **Tier 0 + 1 runtime services have per-codebase comments** — same shape as tier 2-5 (single-slot). The codebase comment renders above both `<host>dev` and `<host>stage` blocks.

11. **Tier 5 yaml mode field matches platform capability** — `mode: NON_HA` for non-HA-capable managed services (meilisearch); `mode: HA` for HA-capable (postgresql, valkey, nats).

Stretch:
- Mount state preamble eliminates the ~13s/sub-agent git-init confusion.
- IG scope rule reduces apidev IG from 11 items to ~5-7 focused items.
- Cross-codebase scaffold-filename leak (workerdev KB referencing apidev's `migrate.ts`) caught by an extended validator.

---

## 2. Workstreams

### 2.0 Guiding principles

Five invariants the implementation session must hold:

1. **No architectural work.** Each workstream is small (~5-150 LoC). None justifies redesigning state, renaming types, splitting packages, or reshaping the spec.

2. **Reference is a floor, not a ceiling.** [/Users/fxck/www/laravel-showcase-app/](/Users/fxck/www/laravel-showcase-app/) is the comparison baseline but not the target. Specifically: laravel CLAUDE.md still mentions zcp MCP for "platform operations" — run 12 targets a stricter rule (zero zcp refs in apps-repo CLAUDE.md, period). Laravel IG mixes platform mechanics with framework-specific configuration — run 12 targets IG = platform-mechanics-only.

3. **System.md §1 audience model is authoritative.** "Every published surface's reader is a porter." When a brief, atom, or template tells the agent something the porter cannot use (an MCP invocation, an authoring-time tool name), that content is mis-targeted. Run 12 tightens the audience boundary.

4. **TEACH side stays positive.** Per system.md §4, every fix expressed as "what the engine produces or requires by construction" rather than "what the agent must avoid." E (env-var-model rewrite) endorses own-key aliasing as the rule; it doesn't ban same-key (which the existing same-key-shadow rule covers correctly). C (CLAUDE.md) tells the agent what to write (`npm run dev`) instead of forbidding `zerops_dev_server`.

5. **Fail loud at engine boundaries.** Run 11's silent failures (Y2's silent comment drop on tier 0/1; Y3's silent HA mode application; record-fragment's silent append where replace was needed) are the structural cause of confusion. G+R surfaces the codebase-scoped violations at the right phase; D rejects paraphrased dispatches; Y1+Y2+Y3 fix-or-fail at emit time.

### 2.E — Rewrite scaffold brief env teaching

**What run 11 showed**: [`briefs/scaffold/platform_principles.md:21-23`](../../../internal/recipe/content/briefs/scaffold/platform_principles.md#L21-L23):

> Cross-service env vars auto-inject project-wide. Do NOT declare
> `DB_HOST: ${db_hostname}` — the platform alias and the redeclaration
> self-shadow and blank at container start. Cite `env-var-model`.

This is wrong. `DB_HOST` and `db_hostname` are different keys; different-key aliasing does not shadow. Only same-key (`db_hostname: ${db_hostname}`) shadows because the per-service envVariables write happens after the project-wide auto-inject and the literal token wins.

The on-demand guide ([environment-variables.md:64-91](../../../internal/knowledge/guides/environment-variables.md#L64-L91)) is correct:

> `run.envVariables` and `build.envVariables` have **two legitimate uses only**:
> 1. Mode flags (`NODE_ENV: production`)
> 2. Framework-convention renames — forward a platform var under a different name. **The key on the left MUST DIFFER from the source var name on the right:**
>    ```yaml
>    DB_HOST: ${db_hostname}          # TypeORM expects uppercase DB_HOST
>    DATABASE_URL: ${db_connectionString}
>    ```

The brief contradicts the guide. The agent followed the brief. Three of run 11's apidev modules (`db.module.ts`, `cache.module.ts`, `broker.module.ts`) read platform-injected names directly (`process.env.db_connectionString`, `process.env.cache_hostname`, `process.env.broker_hostname`). apidev/zerops.yaml run.envVariables block has only `NODE_ENV` and `PORT`. Code is platform-coupled.

The orphan atom [`principles/env-var-model.md`](../../../internal/recipe/content/principles/env-var-model.md#L35-L38) carries the same wrong rule but is not actually loaded by any brief composer (verified: `BuildScaffoldBrief` loads `briefs/scaffold/platform_principles.md` + `dev-loop.md` + `mount-vs-container.md` + `yaml-comment-style.md` + conditionally `init-commands-model.md` — never `principles/env-var-model.md`). The orphan should align with the brief and the guide, or be deleted.

**Fix direction**:

(a) Rewrite [`briefs/scaffold/platform_principles.md`](../../../internal/recipe/content/briefs/scaffold/platform_principles.md) "Managed services" section:

```markdown
## Managed services

Cross-service env vars auto-inject under platform-specific keys
(`${db_hostname}`, `${cache_port}`, `${broker_user}`,
`${storage_apiUrl}`, `${<host>_zeropsSubdomain}`, etc.). Do NOT read
those names directly in code — the code becomes platform-coupled.

**Recommended pattern** — declare own-key aliases in
`zerops.yaml run.envVariables`; code reads the own-key names:

    run:
      envVariables:
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_PASSWORD: ${db_password}
        APP_URL: ${zeropsSubdomain}
        API_URL: ${apistage_zeropsSubdomain}
        NODE_ENV: production

    // application code
    const host = process.env.DB_HOST;
    const port = process.env.DB_PORT;

The keys on the left differ from the platform-side keys on the right.
Different-key aliasing does NOT self-shadow — the platform injects
`db_hostname=...` and your envVariables writes `DB_HOST=...`; two
distinct env entries.

**Same-key shadow trap** — declaring `db_hostname: ${db_hostname}`
(SAME key as the platform's auto-inject) self-shadows. The
per-service envVariables write runs after the project-wide
auto-inject; the literal `${db_hostname}` token wins; the OS env var
becomes the literal string. Symptom: NATS `Invalid URL`, Postgres
`getaddrinfo ENOTFOUND ${db_hostname}`. Never use the same key as
the source.

Reference: `internal/knowledge/guides/environment-variables.md` —
fetch via `zerops_knowledge query=env-var-model` for the full
treatment of project vs cross-service vars, build-time vs runtime
scopes, and isolation modes.
```

(b) Delete or align [`principles/env-var-model.md`](../../../internal/recipe/content/principles/env-var-model.md). Since it's an orphan atom (not loaded by any brief composer), simplest is delete. If kept, rewrite §2 to match the brief's new shape.

(c) Brief content_authoring.md "Validator tripwires" section adds a positive rule:

```markdown
- zerops.yaml `run.envVariables` declares own-key aliases for every
  cross-service var the codebase reads. Code reads own-key names
  (`process.env.DB_HOST`), never platform-side names
  (`process.env.db_hostname`).
```

**Tests**:

```go
// internal/recipe/briefs_test.go
func TestBrief_Scaffold_TeachesOwnKeyAliasing(t *testing.T) {
    // Brief body must contain the own-key alias example.
    plan := syntheticShowcasePlan()
    brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
    if err != nil { t.Fatal(err) }
    mustContain(t, brief.Body, "DB_HOST: ${db_hostname}")
    mustContain(t, brief.Body, "process.env.DB_HOST")
    // Same-key shadow trap kept, but expressed correctly.
    mustContain(t, brief.Body, "db_hostname: ${db_hostname}")
    mustContain(t, brief.Body, "Same-key shadow trap")
    // Wrong rule must not appear.
    mustNotContain(t, brief.Body, "Do NOT declare `DB_HOST: ${db_hostname}")
}
```

**Acceptance**:
- Run 12 apps-repo `zerops.yaml run.envVariables` blocks declare own-key aliases for every cross-service var the codebase reads.
- Run 12 apps-repo source code reads own-key names; zero occurrences of `process.env.db_hostname`, `process.env.cache_hostname`, `process.env.broker_hostname`, `process.env.storage_apiUrl`, etc. in committed code.

**Cost**: content (~25 lines rewrite + 1 test). **Value**: highest single-fix lever in the engine. Cascades to every brief composed for every recipe. Aligns with on-demand guide.

### 2.A — Alias-type contracts

**What run 11 showed**: three sub-agents independently shipped the `https://${<host>_zeropsSubdomain}` double-prefix bug:

- scaffold-app dispatch wrapper line 25 literally embedded the bug:
  > Wire `zerops.yaml run.envVariables.VITE_API_URL: "https://${apidev_zeropsSubdomain}"`

  The agent shipped this verbatim. First deploy: `VITE_API_URL=https://https://apidev-2204-3000.prg1.zerops.app`. Caught via `ssh ... env | grep` at 13:09:53Z, ~17s of debug, fixed.

- scaffold-api dispatch wrapper line 435 (CORS code template):
  > `app.enableCors({ origin: [...].map(s => \`https://${s}\`) })`

  Agent shipped → manifested at feature phase as the CORS double-scheme bug → 8 minutes of investigation (env grep, bundle inspection, OPTIONS curl, `node_modules/cors/lib/index.js`, `dist/main.js`).

No atom or brief explains that `${<host>_zeropsSubdomain}` and `${zeropsSubdomain}` are full HTTPS URLs. The env-var-model atom only mentions `${zeropsSubdomainHost}` (the templating-time variant that's literal in deliverable yaml and substituted at end-user import).

**Fix direction**:

(a) Add an "Alias-type contracts" section to [`briefs/scaffold/platform_principles.md`](../../../internal/recipe/content/briefs/scaffold/platform_principles.md), immediately after the rewritten "Managed services" section:

```markdown
## Alias-type contracts

The platform injects cross-service references under predictable
shapes. Use them as-is; do not compose, prefix, or transform.

| Alias pattern              | Resolves to                | Use as                                   |
|----------------------------|----------------------------|------------------------------------------|
| `${<host>_hostname}`       | bare hostname (`db`)       | host in `host:port` URLs                 |
| `${<host>_port}`           | port number                | port                                     |
| `${<host>_user}`           | username                   | auth user                                |
| `${<host>_password}`       | password                   | auth pass                                |
| `${<host>_<keyname>}`      | the value as-is            | direct value                             |
| `${<host>_connectionString}` | full DSN                 | pass to client constructor               |
| `${<host>_zeropsSubdomain}`  | **full HTTPS URL** (e.g. `https://apistage-2204-3000.prg1.zerops.app`) | Origin / Host / fetch URL — do NOT prepend `https://` |
| `${zeropsSubdomain}`       | **this service's own full HTTPS URL** | APP_URL, callback URL, redirect target     |

`${zeropsSubdomainHost}` is a different beast — it's the
deliverable-template variable that stays LITERAL in published
import.yaml; the platform substitutes the end-user's host suffix at
their click-deploy. Use only inside finalize-phase tier yaml
templates.

When the ORIGIN must be derived (CORS allow-list, Referer check):

    const origins = [
      process.env.APISTAGE_URL,  // own-key alias of ${apistage_zeropsSubdomain}
      process.env.APIDEV_URL,    // own-key alias of ${apidev_zeropsSubdomain}
    ].filter(Boolean);
    // The values are already full https:// URLs — do NOT prepend.
```

(b) Validator extension: at scaffold complete-phase, scan committed source files in `<cb.SourceRoot>/` for the literal substring `https://${...zeropsSubdomain` or `\`https://${process.env...zeropsSubdomain`. Report as `subdomain-double-scheme` violation with a fix hint.

```go
// internal/recipe/validators_codebase.go
// New: scanSourceForSubdomainDoubleScheme
var subdomainDoubleSchemeRE = regexp.MustCompile(`https://\$\{[a-z_]+_zeropsSubdomain\}`)
var subdomainDoubleSchemeJSRE = regexp.MustCompile("`https://\\$\\{process\\.env\\.[a-z_]+_zeropsSubdomain\\}")
```

This is a TEACH-side positive shape (the engine knows the alias is a full URL and refuses the prefix) per system.md §4 — it's the same lesson for every recipe.

**Tests**:

```go
func TestBrief_Scaffold_TeachesAliasTypeContracts(t *testing.T) {
    plan := syntheticShowcasePlan()
    brief, _ := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
    mustContain(t, brief.Body, "Alias-type contracts")
    mustContain(t, brief.Body, "full HTTPS URL")
    mustContain(t, brief.Body, "do NOT prepend `https://`")
}

func TestValidator_SourceCode_FlagsDoubleSchemePrefix(t *testing.T) {
    src := `const url = "https://${apistage_zeropsSubdomain}/api/items";`
    vs := scanSourceForSubdomainDoubleScheme("/tmp/x", src)
    mustHaveCode(t, vs, "subdomain-double-scheme")
}
```

**Acceptance**:
- Run 12 commits zero source files matching the double-scheme pattern.
- If any sub-agent ships the pattern, validator surfaces at scaffold complete-phase.

**Cost**: content (~30 lines) + engine (~25 LoC for the validator). **Value**: prevents the highest-recurrence-rate gotcha in run 11 (3 independent occurrences).

### 2.C — CLAUDE.md is porter-facing, no zcp MCP refs

**What run 11 showed**: all three CLAUDE.md files instruct the porter to start the dev server with `zerops_dev_server`:

- `apidev/CLAUDE.md`:
  > zerops_dev_server action=start hostname=apidev command="npm run start:dev" port=3000 healthPath="/health"
- `appdev/CLAUDE.md`: same shape with `npm run dev -- --host 0.0.0.0 --port 3000`
- `workerdev/CLAUDE.md`: same with `noHttpProbe=true`

A porter cloning the apps-repo cannot run an MCP tool. They run `npm run start:dev`. The CLAUDE.md content collapses two distinct audiences into one — the authoring-time agent uses `zerops_dev_server`, but the published file is for the porter.

System.md §1 names the audience: every published surface reader is a porter. Per-codebase CLAUDE.md is a published surface.

The agent's path to this anti-pattern: `briefs/scaffold/content_authoring.md` line 29 says CLAUDE.md/notes carries "operator notes (dev loop, SSH)". Line 107 routes "Dev loop / SSH / curl → claude-md/notes". `principles/dev-loop.md` (composed into the scaffold brief) teaches `zerops_dev_server` as THE dev-loop primitive. The agent learns about `zerops_dev_server` and writes that into CLAUDE.md/notes.

User policy (stricter than laravel-showcase-app, which still mentions zcp): "they are supposed to be standalone, completely non-zerops / zcp / zerops mcp related."

**Fix direction**:

(a) Rewrite the "CLAUDE.md" subsection in [`briefs/scaffold/content_authoring.md`](../../../internal/recipe/content/briefs/scaffold/content_authoring.md):

```markdown
### CLAUDE.md — porter-facing, codebase-scoped, 30–50 lines (cap 60)

The reader is an AI agent or human developer working in this codebase
in their own editor with their own Zerops project. They do NOT have
zcp's control plane. Write framework-canonical commands, never MCP
tool invocations.

GOOD `Dev loop: \`npm run start:dev\` (Nest CLI watches src/**, reloads on change).`
BAD  `Dev loop: \`zerops_dev_server action=start hostname=apidev command="npm run start:dev"\`.`

GOOD `Deploy: edit, then commit + push to your Zerops-connected branch.`
BAD  `Deploy: \`zerops_deploy targetService=apidev\`.`

The platform's `start: zsc noop --silent` is background context — one
line, factual, not the dev loop the porter follows. The porter starts
the watcher themselves.

What goes here:
- **Zerops service facts** — hostnames, port, runtime, subdomain, etc.
  Concise list. Reference: `laravel-showcase-app/CLAUDE.md` (33 lines).
- **Dev loop** — framework-canonical command (`npm run start:dev`,
  `npm run dev`, `php artisan serve`, etc.).
- **Notes** — codebase-scoped operational facts that don't fit
  service-facts (cross-codebase rules, things-NOT-to-add).

What does NOT go here:
- MCP tool invocations (`zerops_*`, `zcp *`).
- zcli commands (`zcli push`, `zcli vpn`).
- Cross-codebase runbooks (those live in the recipe-root README).
- Quick curls / Smoke tests / Boot-time connectivity narration.
```

(b) Validator: at scaffold complete-phase, scan `<cb.SourceRoot>/CLAUDE.md` for `zerops_dev_server`, `zerops_deploy`, `zerops_verify`, `zerops_logs`, `zerops_browser`, `zerops_recipe`, `zerops_env`, `zerops_discover`, `zerops_mount`, `zerops_subdomain`, `zerops_dev_server`, `zerops_manage`, `zcli `, `zcp `. Each occurrence = `claude-md-zcp-tool-leak` violation.

```go
// internal/recipe/validators_codebase.go (extend validateCodebaseCLAUDE)
var zcpToolLeakRE = regexp.MustCompile(`\b(zerops_(dev_server|deploy|verify|logs|browser|recipe|env|discover|mount|subdomain|manage|knowledge|import|workflow|events)|zcli\s|zcp\s)\b`)
```

**Tests**:

```go
func TestValidator_CodebaseCLAUDE_RejectsZcpToolReferences(t *testing.T) {
    body := "## Dev loop\n`zerops_dev_server action=start hostname=apidev`\n"
    vs := validateCodebaseCLAUDE(ctx, "/tmp/CLAUDE.md", body)
    mustHaveCode(t, vs, "claude-md-zcp-tool-leak")
}

func TestValidator_CodebaseCLAUDE_AcceptsFrameworkCanonical(t *testing.T) {
    body := "## Dev loop\n`npm run start:dev`\n"
    vs := validateCodebaseCLAUDE(ctx, "/tmp/CLAUDE.md", body)
    mustNotHaveCode(t, vs, "claude-md-zcp-tool-leak")
}

func TestBrief_Scaffold_CLAUDEMDIsPorter(t *testing.T) {
    plan := syntheticShowcasePlan()
    brief, _ := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
    mustContain(t, brief.Body, "framework-canonical")
    mustContain(t, brief.Body, "MCP tool invocations")
}
```

**Acceptance**:
- Run 12 apps-repo CLAUDE.md files contain zero `zerops_*` MCP tool names, zero `zcli ` invocations, zero `zcp ` invocations.
- Dev-loop section is `npm run <script>` (or framework equivalent).

**Cost**: content (~25 lines rewrite + ~10 LoC validator + 3 tests). **Value**: highest porter-perspective failure in run 11; one-line fix per file once the brief and validator agree.

### 2.I — IG scope rule

**What run 11 showed**: apidev/README.md IG had 11 `### N.` items. Items 9-11 (NATS subject naming, cache key shape, image storage layout) are recipe-internal contracts — useful for someone customizing THIS recipe, not generic platform-on-NestJS knowledge.

Reference (laravel-showcase-app) keeps IG = "what changes for Zerops" (5 items, all framework integration concerns) cleanly separate from app-internal content. The user explicitly noted laravel-showcase IS imperfect here too (mixes framework-config with platform-mechanics), but it's still cleaner than the run-11 nestjs IG.

**Fix direction**:

Add a "## IG scope" subsection to [`briefs/scaffold/content_authoring.md`](../../../internal/recipe/content/briefs/scaffold/content_authoring.md):

```markdown
### IG scope — "what changes for Zerops" only

IG items 2+ describe what changes about a NestJS / Laravel / SvelteKit
app to deploy on Zerops:

- Bind 0.0.0.0 (instead of 127.0.0.1)
- Trust the L7 proxy
- Read cross-service env vars from own-key aliases (not platform-side names)
- Cache control / SIGTERM drain — only when there's a Zerops-specific shape

What does NOT go here:
- Framework configuration that doesn't change for Zerops (route declarations,
  middleware ordering, controller decoration patterns).
- Recipe-internal contracts (NATS subject naming, cache key shape,
  image storage layout, queue topic conventions). Those are
  customization points for someone extending THIS recipe; they go in
  KB or claude-md/notes.
- Application architecture (module structure, class hierarchy).

Aim for 4-7 IG items. More usually means recipe-internal content
crept in. Reference (laravel-showcase-app): 5 items.
```

**Tests**:

```go
func TestBrief_Scaffold_IGScopeRule(t *testing.T) {
    plan := syntheticShowcasePlan()
    brief, _ := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
    mustContain(t, brief.Body, "IG scope")
    mustContain(t, brief.Body, "Aim for 4-7 IG items")
}
```

No engine validator — IG bloat is recipe-judgment territory; brief teaching is sufficient.

**Cost**: content (~15 lines + 1 test). **Value**: medium — sharpens the IG audience.

### 2.M — Mount-state preamble

**What run 11 showed**: three scaffold sub-agents independently rediscovered that `/var/www/<host>dev/` arrives with a `.git` repo (created by [`ops.InitServiceGit`](../../../internal/ops/service_git_init.go#L24) at mount time, identity `agent@zerops.io`) and `deploy` commits (created by `zerops_deploy` on each call).

scaffold-api recovery: `rm -rf .git && git init -q -b main`. ~13s.
scaffold-app recovery: `git commit --allow-empty`. ~13s.
scaffold-worker recovery: `git commit --allow-empty`. ~13s.

Same trap, three different workarounds.

**Fix direction**:

Add a "## Mount state at scaffold start" section to [`phase_entry/scaffold.md`](../../../internal/recipe/content/phase_entry/scaffold.md), before the per-codebase steps:

```markdown
## Mount state at scaffold start

When your scaffold sub-agent receives control, the SSHFS mount at
`/var/www/<hostname>dev/` already has:

- `.git/` initialized — created by zcp's mount machinery
  (`ops.InitServiceGit`). Identity: `agent@zerops.io`,
  branch: `main`.
- One or more `deploy` commits — created by `zerops_deploy` if any
  prior deploy ran. Visible in `git log --oneline`.

Recovery for the scaffold commit:

    cd /var/www/<hostname>dev
    git reset --soft $(git rev-list --max-parents=0 HEAD) 2>/dev/null || \
      (rm -rf .git && git init -q -b main)
    git config user.email recipe@zerops.io
    git config user.name 'Recipe Author'
    git add -A
    git commit -q -m 'scaffold: initial structure + zerops.yaml'

Pick the recovery once and apply consistently across all three scaffold
sub-agents — wipe-and-reinit is acceptable for a dogfood run; in
production, the publish path may want to preserve any meaningful deploy
history. For run 12, wipe-and-reinit.
```

**Acceptance**:
- Run 12 scaffold sub-agents do not waste cycles diagnosing the `.git` collision (zero "let me check" Bash calls between first failed `git init` and successful commit).

**Cost**: content (~15 lines). **Value**: low (saves ~30s per run).

### 2.G — Codebase-scoped validators run at scaffold + feature complete-phase

**What run 11 showed**: codebase-scoped surface validators (`SurfaceCodebaseIG`, `SurfaceCodebaseKB`, `SurfaceCodebaseCLAUDE`, `SurfaceCodebaseZeropsComments`) currently run only at finalize complete-phase ([`gates.go::FinalizeGates`](../../../internal/recipe/gates.go#L56)). But their target content is authored at scaffold/feature. The lag means finalize gets violations on content it was told (correctly) not to touch. Fact #7 captured this exactly: "finalize complete-phase reports codebase-ig / kb / claude-md violations on existing scaffold-authored fragments, but the finalize wrapper brief explicitly says 'Do NOT touch codebase/<h>/intro|integration-guide|knowledge-base|claude-md/* — those are scaffold/feature phase artifacts.'"

**Fix direction**:

(a) Split [`gates.go::FinalizeGates`](../../../internal/recipe/gates.go#L56) into two sets:

```go
// CodebaseGates returns the gate set that runs at scaffold + feature
// complete-phase. These validators target codebase-scoped surfaces
// (IG, KB, CLAUDE, source-code comments, zerops.yaml comments) — the
// content authored by the scaffold/feature sub-agent in their own
// session.
func CodebaseGates() []Gate {
    return []Gate{
        {Name: "codebase-surface-validators", Run: gateCodebaseSurfaceValidators},
        {Name: "source-comment-voice", Run: gateSourceCommentVoice},
    }
}

// EnvGates returns the gate set that runs only at finalize close.
// These validators target finalize-authored surfaces (root README,
// per-tier env READMEs, env import-comments).
func EnvGates() []Gate {
    return []Gate{
        {Name: "env-imports-present", Run: gateEnvImportsPresent},
        {Name: "env-surface-validators", Run: gateEnvSurfaceValidators},
    }
}

func gateCodebaseSurfaceValidators(ctx GateContext) []Violation {
    return runSurfaceValidatorsForKinds(ctx, []Surface{
        SurfaceCodebaseIG, SurfaceCodebaseKB, SurfaceCodebaseCLAUDE,
        SurfaceCodebaseZeropsComments,
    })
}

func gateEnvSurfaceValidators(ctx GateContext) []Violation {
    return runSurfaceValidatorsForKinds(ctx, []Surface{
        SurfaceRootREADME, SurfaceEnvREADME, SurfaceEnvImportComments,
    })
}
```

(b) Update [`phase_entry.go::gatesForPhase`](../../../internal/recipe/phase_entry.go#L21):

```go
func gatesForPhase(p Phase) []Gate {
    base := DefaultGates()
    switch p {
    case PhaseScaffold, PhaseFeature:
        return append(base, CodebaseGates()...)
    case PhaseFinalize:
        // Finalize runs codebase gates again (catches any feature appends)
        // plus env gates.
        out := append(base, CodebaseGates()...)
        return append(out, EnvGates()...)
    }
    return base
}
```

(c) `gateCodebaseSurfaceValidators` only validates surfaces whose content has been recorded — no "missing fragment" violations during scaffold close just because feature hasn't run yet. Surface registry already supports per-codebase iteration via `cb.SourceRoot`; validator skips when `SourceRoot == ""` or when the target file is empty.

(d) `stitch-content` already runs at scaffold + feature close (engine writes per-codebase `<SourceRoot>/{README, CLAUDE, zerops.yaml}` after each fragment record). After G ships, `complete-phase scaffold` runs `gateCodebaseSurfaceValidators` against the scaffold-authored content. If violations, sub-agent fixes in-session via `record-fragment mode=replace` (workstream R).

**Tests**:

```go
// internal/recipe/phase_entry_test.go
func TestGatesForPhase_Scaffold_IncludesCodebaseGates(t *testing.T) {
    gates := gatesForPhase(PhaseScaffold)
    mustHaveGate(t, gates, "codebase-surface-validators")
    mustHaveGate(t, gates, "source-comment-voice")
    mustNotHaveGate(t, gates, "env-imports-present")
    mustNotHaveGate(t, gates, "env-surface-validators")
}

func TestGatesForPhase_Feature_IncludesCodebaseGates(t *testing.T) {
    gates := gatesForPhase(PhaseFeature)
    mustHaveGate(t, gates, "codebase-surface-validators")
}

func TestGatesForPhase_Finalize_IncludesAllGates(t *testing.T) {
    gates := gatesForPhase(PhaseFinalize)
    mustHaveGate(t, gates, "codebase-surface-validators")
    mustHaveGate(t, gates, "env-surface-validators")
    mustHaveGate(t, gates, "env-imports-present")
}

func TestCompletePhase_Scaffold_FlagsCodebaseIGViolations(t *testing.T) {
    sess := newTestSession(t)
    sess.RecordFragment("codebase/api/integration-guide", "1. plain ordered\n2. list shape\n")
    sess.StitchContent()
    blocking, _, _ := sess.CompletePhase(gatesForPhase(PhaseScaffold))
    mustHaveCode(t, blocking, "codebase-ig-plain-ordered-list")
}
```

**Acceptance**:
- Run 12 scaffold complete-phase fires codebase-IG/KB/CLAUDE validators against scaffold-authored content. The right author (scaffold sub-agent) gets the violation in their own session.
- Run 12 finalize complete-phase produces zero codebase-scoped violations (because scaffold + feature already cleared them).
- Zero direct file edits in finalize.

**Cost**: engine (~50 LoC + 4 tests). **Value**: highest engine-flow lever — eliminates the 6 minutes of finalize hand-editing.

### 2.R — `record-fragment mode=replace`

**What run 11 showed**: re-recording `codebase/api/integration-guide` at finalize APPENDED instead of replaced (fact #9 mechanism). The scaffold sub-agent's session was closed by then — only finalize sub-agent or main could touch it. `isAppendFragmentID` ([handlers_fragments.go:81-94](../../../internal/recipe/handlers_fragments.go#L81-L94)) returns true for codebase IG/KB/claude-md ids; second write concatenates with `\n\n` separator. The new "### 1. Adding zerops.yaml" item landed BELOW the existing items 2-8 from scaffold; the validator then fired `codebase-ig-first-item-not-zerops-yaml` because the new "first" item sat after the originals.

After workstream G ships, the codebase-scoped violations surface at scaffold complete-phase — but the same append problem remains for the SAME-PHASE re-record case. Sub-agent records `codebase/api/integration-guide` body v1; validator says `plain-ordered-list`; sub-agent calls `record-fragment` v2 to fix → appends to v1; the violation persists.

**Fix direction**:

Add `mode` parameter to `record-fragment`:

```go
// internal/recipe/handlers.go
type Input struct {
    // ... existing fields ...
    Mode string `json:"mode,omitempty" jsonschema:"For record-fragment: 'append' (default for codebase IG/KB/claude-md ids; concatenates with prior body) or 'replace' (overwrites prior body). Use 'replace' to correct a fragment you authored earlier in the same recipe session."`
}
```

```go
// internal/recipe/handlers_fragments.go::recordFragment
func recordFragment(sess *Session, id, body, mode string) (int, bool, error) {
    // ... existing validation ...
    if isAppendFragmentID(id) && mode != "replace" {
        existing := sess.Plan.Fragments[id]
        if existing == "" {
            sess.Plan.Fragments[id] = body
            return len(body), false, nil
        }
        combined := existing + "\n\n" + body
        sess.Plan.Fragments[id] = combined
        return len(combined), true, nil
    }
    // mode=replace OR non-append id — overwrite
    sess.Plan.Fragments[id] = body
    return len(body), false, nil
}
```

Brief tells the sub-agent in `briefs/scaffold/content_authoring.md`:

```markdown
### Correcting a fragment you authored

If `complete-phase scaffold` flags a violation on a fragment you
authored, fix it in-session via `record-fragment mode=replace`:

    zerops_recipe action=record-fragment slug=<slug>
      fragmentId=codebase/api/integration-guide
      mode=replace
      fragment=<corrected body>

Default mode is append for codebase IG/KB/claude-md ids (so feature
phase can extend scaffold's content). `mode=replace` overwrites — use
when correcting your own previously-recorded fragment within the same
phase. Feature sub-agent can also use `mode=replace` to correct
scaffold's content if scaffold wrote something feature needs to
rewrite (rare; prefer extending).
```

**Tests**:

```go
// internal/recipe/handlers_test.go
func TestRecordFragment_DefaultModeAppendsOnCodebaseID(t *testing.T) {
    sess := newTestSession(t)
    sess.RecordFragment("codebase/api/integration-guide", "first body", "")
    sess.RecordFragment("codebase/api/integration-guide", "second body", "")
    body := sess.Plan.Fragments["codebase/api/integration-guide"]
    mustEqual(t, body, "first body\n\nsecond body")
}

func TestRecordFragment_ModeReplaceOverwrites(t *testing.T) {
    sess := newTestSession(t)
    sess.RecordFragment("codebase/api/integration-guide", "first body", "")
    sess.RecordFragment("codebase/api/integration-guide", "corrected body", "replace")
    body := sess.Plan.Fragments["codebase/api/integration-guide"]
    mustEqual(t, body, "corrected body")
}

func TestRecordFragment_ModeReplaceOnEnvIDIsEquivalentToOverwrite(t *testing.T) {
    // env/<N>/intro is already overwrite by default; mode=replace doesn't change behavior.
    sess := newTestSession(t)
    sess.RecordFragment("env/0/intro", "v1", "")
    sess.RecordFragment("env/0/intro", "v2", "replace")
    mustEqual(t, sess.Plan.Fragments["env/0/intro"], "v2")
}

func TestRecordFragment_UnknownModeRejected(t *testing.T) {
    sess := newTestSession(t)
    _, _, err := sess.RecordFragment("env/0/intro", "v1", "concat")
    mustErrorContaining(t, err, "mode must be 'append' or 'replace'")
}
```

**Acceptance**:
- Run 12 sub-agents use `mode=replace` to correct their own fragments after a complete-phase violation.
- Zero append-duplication artifacts in published apps-repo content.

**Cost**: engine (~25 LoC + 4 tests + brief change ~10 lines). **Value**: prerequisite for G's effectiveness.

### 2.B — Engine-composed finalize brief

**What run 11 showed**: `BuildFinalizeBrief` ([briefs.go:228-290](../../../internal/recipe/briefs.go#L228-L290)) emits 3,427 bytes. Main agent dispatches a 13,492-byte prompt — wrapper carries 10 KB of hand-typed content (tier map, hostname sets, fragment list, audience paths, citation list, anti-pattern list, "what NOT to do", workflow, done criterion). Same shape as run-10 F1 carry-forward.

**Fix direction**:

Extend [`briefs.go::BuildFinalizeBrief`](../../../internal/recipe/briefs.go#L228) to compose the full prompt:

```go
func BuildFinalizeBrief(plan *Plan) (Brief, error) {
    // ... existing header + atoms ...

    // NEW: tier map (was hand-typed)
    b.WriteString("## Tier map\n\n")
    for _, t := range Tiers() {
        fmt.Fprintf(&b, "- **%d — %s** (`%s`): %s\n", t.Index, t.Label, t.Folder, tierAudienceLine(t))
    }
    b.WriteByte('\n')
    parts = append(parts, "tier_map")

    // NEW: hostname sets per tier (was hand-typed)
    b.WriteString("## Hostname sets\n\n")
    b.WriteString("| Tier | Runtime hosts | Managed hosts |\n")
    b.WriteString("|---|---|---|\n")
    for _, t := range Tiers() {
        runtimeHosts := strings.Join(runtimeHostnamesForTier(plan, t), ", ")
        managedHosts := strings.Join(managedHostnames(plan), ", ")
        fmt.Fprintf(&b, "| %d | %s | %s |\n", t.Index, runtimeHosts, managedHosts)
    }
    b.WriteByte('\n')
    parts = append(parts, "hostname_sets")

    // NEW: fragment list with paths (was hand-typed; replaces math-error-prone wrapper)
    b.WriteString("## Fragments to author\n\n")
    b.WriteString(formatFinalizeFragmentList(plan))
    b.WriteByte('\n')
    parts = append(parts, "fragment_list")

    // NEW: anti-patterns inline (was hand-typed)
    b.WriteString(readAtomMust("briefs/finalize/anti_patterns.md"))
    parts = append(parts, "anti_patterns")

    // existing fragment math, audience paths, validator tripwires...
}

func formatFinalizeFragmentList(plan *Plan) string {
    var lines []string
    lines = append(lines, "- `root/intro`")
    for i := range 6 {
        t, _ := TierAt(i)
        lines = append(lines, fmt.Sprintf("- `env/%d/intro` (tier %d — %s)", i, i, t.Label))
        lines = append(lines, fmt.Sprintf("- `env/%d/import-comments/project`", i))
        for _, cb := range plan.Codebases {
            lines = append(lines, fmt.Sprintf("- `env/%d/import-comments/%s`", i, cb.Hostname))
        }
        for _, svc := range plan.Services {
            lines = append(lines, fmt.Sprintf("- `env/%d/import-comments/%s`", i, svc.Hostname))
        }
    }
    return strings.Join(lines, "\n")
}
```

Anti-patterns atom (new file `briefs/finalize/anti_patterns.md`):

```markdown
## What NOT to do

- Do NOT re-run `emit-yaml shape=workspace` — that shape is provision-only.
- Do NOT pass your live workspace's secret as a `project_env_vars` value.
- Do NOT resolve `${zeropsSubdomainHost}` to a literal URL.
- Do NOT hand-edit stitched files; use `record-fragment` (default `append`)
  + `record-fragment mode=replace` for corrections.
- Do NOT touch `codebase/<h>/{intro,integration-guide,knowledge-base,
  claude-md/*}` ids — scaffold + feature have already validated their
  content at their own complete-phase. By finalize, those surfaces are
  green.
```

The dispatch path: main agent calls `zerops_recipe action=build-brief
briefKind=finalize slug=<slug>`, takes the response's `brief.body`, and
passes it byte-identical as the `Agent` tool's `prompt`. No
hand-typing.

**Tests**:

```go
func TestBuildFinalizeBrief_IncludesTierMap(t *testing.T) {
    plan := syntheticShowcasePlan()
    brief, _ := BuildFinalizeBrief(plan)
    mustContain(t, brief.Body, "## Tier map")
    mustContain(t, brief.Body, "0 — AI Agent")
    mustContain(t, brief.Body, "5 — Highly-available Production")
}

func TestBuildFinalizeBrief_IncludesFragmentList(t *testing.T) {
    plan := syntheticShowcasePlan()
    brief, _ := BuildFinalizeBrief(plan)
    mustContain(t, brief.Body, "## Fragments to author")
    mustContain(t, brief.Body, "`root/intro`")
    mustContain(t, brief.Body, "`env/0/intro`")
    mustContain(t, brief.Body, "`env/0/import-comments/api`")
}

func TestBuildFinalizeBrief_FragmentMathMatchesPlan(t *testing.T) {
    plan := syntheticShowcasePlan() // 3 codebases + 5 services = 8 hosts
    brief, _ := BuildFinalizeBrief(plan)
    // 1 root intro + 6 env intros + 6 project comments + 6*8 service comments = 61
    mustContain(t, brief.Body, "= 61 fragments")
}

func TestBuildFinalizeBrief_SizeApproximatesDispatchPromptSize(t *testing.T) {
    // After ship, dispatch prompt should be within 10% of brief size.
    // Run 11: brief 3,427 vs dispatch 13,492 (393%). Run 12 target: <110%.
    plan := syntheticShowcasePlan()
    brief, _ := BuildFinalizeBrief(plan)
    if brief.Bytes < 8000 {
        t.Errorf("finalize brief too small for dispatch-as-is: %d bytes", brief.Bytes)
    }
}
```

**Acceptance**:
- Run 12 finalize dispatch prompt size ≈ engine brief size (within 10%).
- Zero math errors in dispatch (fragment count matches actual count).
- Zero obsolete paths (audience paths derived from `Plan.Codebases[i].SourceRoot`).

**Cost**: engine (~150 LoC + 4 tests + 1 new atom). **Value**: medium — eliminates run-10/11 F1 carry-forward.

### 2.D — Dispatch integrity verification

**What run 11 showed**: `phase_entry/scaffold.md:75-82` mandates "pass `brief.body` byte-identical" but main agent paraphrased anyway. scaffold-app dispatch prompt: 9,047 bytes vs engine brief 14,582 (62%). scaffold-worker: 7,359 vs 14,344 (51%). The wrong-env teaching cascaded through paraphrase intact, but other content didn't. Run 11 acted as if `verify-subagent-dispatch` (mentioned in scaffold.md as "planned but not yet implemented in v3") still doesn't exist.

**Fix direction**:

Implement the `verify-subagent-dispatch` action:

```go
// internal/recipe/handlers.go
case "verify-subagent-dispatch":
    if in.BriefKind == "" {
        r.Error = "verify-subagent-dispatch: briefKind required"
        return
    }
    if in.DispatchedPrompt == "" {
        r.Error = "verify-subagent-dispatch: dispatchedPrompt required"
        return
    }
    expected, err := composeBriefForKind(sess.Plan, in.BriefKind, in.Codebase, parent)
    if err != nil {
        r.Error = err.Error()
        return
    }
    if !strings.Contains(in.DispatchedPrompt, expected.Body) {
        r.Error = fmt.Sprintf(
            "verify-subagent-dispatch: dispatch missing engine brief body. " +
            "Engine brief is %d bytes; dispatched prompt is %d bytes. " +
            "Pass brief.body byte-identical — main agent must NOT paraphrase or truncate.",
            len(expected.Body), len(in.DispatchedPrompt),
        )
        return
    }
    r.Status = ... // ok
```

Brief tells main agent in `phase_entry/scaffold.md`:

```markdown
## Dispatch integrity (mandatory)

After composing each scaffold brief, dispatch the sub-agent via
`Agent` with `prompt=<engine brief body><wrapper notes>`. The engine
brief body MUST appear byte-identical inside the dispatched prompt.

After the sub-agent terminates (or before, with the prompt prepared),
verify:

    zerops_recipe action=verify-subagent-dispatch \
      slug=<slug> briefKind=scaffold codebase=<host> \
      dispatchedPrompt=<the prompt you sent>

A mismatch returns an actionable error. Fix the prompt and re-dispatch.

This is now enforced — run-11 main agents truncated scaffold-app
brief from 14,582 bytes to 9,047 (62%) and lost teaching content.
```

**Tests**:

```go
func TestVerifyDispatch_MatchingBriefAccepted(t *testing.T) {
    sess := newTestSession(t)
    brief, _ := BuildScaffoldBrief(sess.Plan, sess.Plan.Codebases[0], nil)
    err := sess.VerifyDispatch("scaffold", "api", brief.Body+"\n\n## Wrapper notes\nx")
    mustNoError(t, err)
}

func TestVerifyDispatch_TruncatedBriefRejected(t *testing.T) {
    sess := newTestSession(t)
    brief, _ := BuildScaffoldBrief(sess.Plan, sess.Plan.Codebases[0], nil)
    truncated := brief.Body[:len(brief.Body)/2]
    err := sess.VerifyDispatch("scaffold", "api", truncated)
    mustErrorContaining(t, err, "dispatch missing engine brief body")
}

func TestVerifyDispatch_ParaphrasedBriefRejected(t *testing.T) {
    sess := newTestSession(t)
    paraphrased := "You are the api scaffold agent. Bind 0.0.0.0. ..."
    err := sess.VerifyDispatch("scaffold", "api", paraphrased)
    mustErrorContaining(t, err, "dispatch missing engine brief body")
}
```

**Acceptance**:
- Run 12 scaffold + feature + finalize dispatches verified pre-dispatch.
- Zero paraphrased dispatches in run 12 session log.

**Cost**: engine (~40 LoC + 3 tests + brief change). **Value**: medium — closes a gap that's been open since run 8.

### 2.Y1 — `writeComment` strips leading `#` before re-prefixing

**What run 11 showed**: 272 lines of `# # …` doubled-prefix across 6 published tier yamls. Mechanism: [`yaml_emitter.go:380`](../../../internal/recipe/yaml_emitter.go#L380):

```go
fmt.Fprintf(b, "%s# %s\n", indent, line)
```

unconditionally prepends `# `. Agents author fragment bodies with leading `# ` per line (probably treating fragments as already-comment-formatted). Result: doubled.

**Fix direction**:

```go
// internal/recipe/yaml_emitter.go::writeComment
func writeComment(b *strings.Builder, text, indent string) {
    if strings.TrimSpace(text) == "" {
        return
    }
    width := max(80-len(indent)-2, 20)
    for _, line := range wrapPara(text, width) {
        if line == "" {
            fmt.Fprintf(b, "%s#\n", indent)
            continue
        }
        // Strip a leading "# " or "#" from agent-authored lines so
        // re-prefixing produces single-`#` comments. Run-11 §Y1.
        line = strings.TrimPrefix(line, "# ")
        line = strings.TrimPrefix(line, "#")
        line = strings.TrimSpace(line)
        if line == "" {
            fmt.Fprintf(b, "%s#\n", indent)
            continue
        }
        fmt.Fprintf(b, "%s# %s\n", indent, line)
    }
}
```

**Tests**:

```go
// internal/recipe/yaml_emitter_test.go
func TestWriteComment_StripsLeadingHashFromAuthoredFragment(t *testing.T) {
    var b strings.Builder
    writeComment(&b, "# This is a comment line\n# Second line", "  ")
    got := b.String()
    if strings.Contains(got, "# # ") {
        t.Errorf("doubled-prefix found in:\n%s", got)
    }
    mustContain(t, got, "  # This is a comment line")
    mustContain(t, got, "  # Second line")
}

func TestWriteComment_BareProseUnchanged(t *testing.T) {
    var b strings.Builder
    writeComment(&b, "Plain prose with no prefix", "  ")
    got := b.String()
    mustContain(t, got, "  # Plain prose with no prefix")
    if strings.Contains(got, "# # ") {
        t.Errorf("doubled-prefix found in:\n%s", got)
    }
}
```

**Acceptance**:
- Run 12 published tier yamls have zero `# # ` doubled-prefix lines.

**Cost**: engine (~5 LoC + 2 tests). **Value**: high cosmetic — 272 lines disfigured per recipe today.

### 2.Y2 — `writeRuntimeDev/Stage` falls back to bare codebase name

**What run 11 showed**: tier 0 + 1 yaml runtime services have NO comments. Mechanism:

[`yaml_emitter.go:212-226`](../../../internal/recipe/yaml_emitter.go#L212-L226):
```go
func writeRuntimeDev(b *strings.Builder, plan *Plan, cb Codebase, comments map[string]string) {
    host := cb.Hostname + "dev"
    writeComment(b, comments[host], "  ")
    ...
}
```

Looks up `comments["apidev"]`. The agent records `env/<N>/import-comments/api`; [`handlers_fragments.go::applyEnvComment`](../../../internal/recipe/handlers_fragments.go#L50-L76) stores under `comments["api"]`. Brief instructs the agent to use the bare codebase name. Map keys differ; emitter finds nothing.

**Fix direction**:

```go
// internal/recipe/yaml_emitter.go::writeRuntimeDev
func writeRuntimeDev(b *strings.Builder, plan *Plan, cb Codebase, comments map[string]string) {
    host := cb.Hostname + "dev"
    // Look up by slot host first, fall back to bare codebase name.
    // Brief instructs agents to use bare codebase name; emitter must
    // honor that. Run-11 §Y2.
    comment := comments[host]
    if comment == "" {
        comment = comments[cb.Hostname]
    }
    writeComment(b, comment, "  ")
    fmt.Fprintf(b, "  - hostname: %s\n", host)
    ...
}

// Same fallback in writeRuntimeStage.
```

**Tests**:

```go
func TestWriteRuntimeDev_FallsBackToBareCodebaseName(t *testing.T) {
    plan := syntheticShowcasePlan()
    plan.EnvComments = map[string]EnvComments{
        "0": {Service: map[string]string{
            "api": "api comment authored under bare codebase name",
        }},
    }
    got, _ := EmitImportYAML(plan, 0)
    mustContain(t, got, "api comment authored under bare codebase name")
    // Renders ABOVE the apidev block (slot host).
    apidevIdx := strings.Index(got, "- hostname: apidev")
    commentIdx := strings.Index(got, "api comment authored under bare codebase name")
    if commentIdx > apidevIdx {
        t.Errorf("comment did not render above apidev block")
    }
}

func TestWriteRuntimeDev_SlotKeyTakesPrecedence(t *testing.T) {
    plan := syntheticShowcasePlan()
    plan.EnvComments = map[string]EnvComments{
        "0": {Service: map[string]string{
            "api":    "bare-name comment",
            "apidev": "slot-keyed comment",
        }},
    }
    got, _ := EmitImportYAML(plan, 0)
    // Slot-keyed wins for the dev slot.
    mustContain(t, got, "slot-keyed comment")
}
```

**Acceptance**:
- Run 12 tier 0 + 1 yaml runtime services carry per-codebase comments.

**Cost**: engine (~6 LoC + 2 tests). **Value**: medium — restores ~15 lines of useful per-tier-per-runtime comments on tiers 0/1.

### 2.Y3 — `Service.SupportsHA` capability flag

**What run 11 showed**: tier 5 yaml emits `mode: HA` for `meilisearch@1.20`, which is not HA-capable on Zerops. Comment block 6 lines above correctly says "Meilisearch stays single-node because Zerops does not yet offer a clustered Meilisearch build" — yaml field contradicts. Fact #8.

**Fix direction**:

(a) Add capability field to `Service`:

```go
// internal/recipe/plan.go
type Service struct {
    Kind        ServiceKind       `json:"kind"`
    Hostname    string            `json:"hostname"`
    Type        string            `json:"type"`
    Priority    int               `json:"priority,omitempty"`
    SupportsHA  bool              `json:"supportsHA,omitempty"`
    ExtraFields map[string]string `json:"extraFields,omitempty"`
}
```

(b) Capability table in plan composition:

```go
// internal/recipe/plan.go (new helper)
func managedServiceSupportsHA(serviceType string) bool {
    // Type strings include version, e.g. "postgresql@18", "meilisearch@1.20"
    // Match the family prefix.
    family := strings.SplitN(serviceType, "@", 2)[0]
    switch family {
    case "postgresql", "valkey", "redis", "nats", "rabbitmq", "elasticsearch":
        return true
    case "meilisearch", "kafka": // single-node only on Zerops
        return false
    default:
        return false // conservative default
    }
}
```

Update plan composition (where `Service` is built from import yaml or research output) to set `SupportsHA` from the family.

(c) Emit conditional mode:

```go
// internal/recipe/yaml_emitter.go::writeNonRuntimeService
case ServiceKindManaged:
    mode := tier.ServiceMode
    if mode == "HA" && !svc.SupportsHA {
        mode = "NON_HA"
    }
    fmt.Fprintf(b, "    mode: %s\n", mode)
    writeAutoscaling(b, serviceKindManaged, tier)
```

**Tests**:

```go
// internal/recipe/yaml_emitter_test.go
func TestEmitDeliverable_Tier5_MeilisearchNonHA(t *testing.T) {
    plan := syntheticShowcasePlan()
    // Find meilisearch in services and confirm SupportsHA=false
    for i, svc := range plan.Services {
        if strings.HasPrefix(svc.Type, "meilisearch@") {
            plan.Services[i].SupportsHA = false
        }
    }
    got, _ := EmitImportYAML(plan, 5)
    // Postgres/valkey/nats stay HA at tier 5
    mustContain(t, got, "type: postgresql@18\n    priority: 10\n    mode: HA")
    // Meilisearch is NOT HA at tier 5
    mustContain(t, got, "type: meilisearch@1.20\n    priority: 10\n    mode: NON_HA")
}

func TestManagedServiceSupportsHA_FamilyTable(t *testing.T) {
    cases := []struct{ in string; want bool }{
        {"postgresql@18", true},
        {"valkey@7.2", true},
        {"nats@2.12", true},
        {"meilisearch@1.20", false},
        {"kafka@3", false},
        {"unknown@1", false},
    }
    for _, tc := range cases {
        if got := managedServiceSupportsHA(tc.in); got != tc.want {
            t.Errorf("managedServiceSupportsHA(%q) = %v, want %v", tc.in, got, tc.want)
        }
    }
}
```

**Acceptance**:
- Run 12 tier 5 yaml emits `mode: NON_HA` for meilisearch.
- Run 12 facts.jsonl does NOT contain `tier5-meilisearch-mode-ha-emitted` (or any analogous engine-bug fact for HA-capability mismatches).

**Cost**: engine (~15 LoC + 2 tests). **Value**: medium — eliminates the highest-visibility yaml contradiction.

---

## 3. Ordering + commits

### Tranche structure

**Tranche 1 — content fixes** (E, A, C, I, M):
- Smallest fixes (~50 lines content + ~25 LoC validator for A, ~10 LoC validator for C).
- No engine flow changes; pure brief / atom edits + two new validator codes.
- Cascades to every brief composed for every recipe.

**Tranche 2 — engine flow** (G, R, B, D):
- Larger engine changes (~255 LoC total + tests).
- Depends on Tranche 1 only loosely (the new validators in C and A piggyback on the gate split in G).

**Tranche 3 — engine cosmetic** (Y1, Y2, Y3):
- Smallest engine changes (~26 LoC total).
- Independent of Tranches 1/2.
- Defer to run 12 stretch if time pressure.

### Commit order

**Tranche 1**:

1. **commit 1** — E content rewrite + test
   - Edit [`internal/recipe/content/briefs/scaffold/platform_principles.md`](../../../internal/recipe/content/briefs/scaffold/platform_principles.md) Managed services section.
   - Delete [`internal/recipe/content/principles/env-var-model.md`](../../../internal/recipe/content/principles/env-var-model.md) (orphan atom).
   - Add `TestBrief_Scaffold_TeachesOwnKeyAliasing` to `briefs_test.go`.

2. **commit 2** — A content + validator + tests
   - Add "Alias-type contracts" subsection to `platform_principles.md`.
   - Add `subdomain-double-scheme` validator code in `validators_codebase.go`.
   - Add `TestValidator_SourceCode_FlagsDoubleSchemePrefix` + `TestBrief_Scaffold_TeachesAliasTypeContracts`.

3. **commit 3** — C content + validator + tests
   - Rewrite `briefs/scaffold/content_authoring.md` CLAUDE.md subsection.
   - Add `claude-md-zcp-tool-leak` validator code in `validators_codebase.go::validateCodebaseCLAUDE`.
   - Add `TestValidator_CodebaseCLAUDE_RejectsZcpToolReferences` + `TestBrief_Scaffold_CLAUDEMDIsPorter`.

4. **commit 4** — I content + test
   - Add "IG scope" subsection to `briefs/scaffold/content_authoring.md`.
   - Add `TestBrief_Scaffold_IGScopeRule`.

5. **commit 5** — M content
   - Add "Mount state at scaffold start" section to `phase_entry/scaffold.md`.

**Tranche 2**:

6. **commit 6** — R: `record-fragment mode=replace`
   - Edit `internal/recipe/handlers.go::Input` to add `Mode` field.
   - Edit `handlers_fragments.go::recordFragment` to honor mode.
   - Add 4 tests in `handlers_test.go`.
   - Brief change: `briefs/scaffold/content_authoring.md` "Correcting a fragment" subsection.

7. **commit 7** — G: split `FinalizeGates` into `CodebaseGates` + `EnvGates`
   - Edit `gates.go`: split `FinalizeGates()`, add `CodebaseGates()`, add `gateCodebaseSurfaceValidators` + `gateEnvSurfaceValidators` + helper `runSurfaceValidatorsForKinds`.
   - Edit `phase_entry.go::gatesForPhase` to dispatch by phase.
   - Add 4 tests in `phase_entry_test.go`.
   - **Important**: this commit DEPENDS on commit 6 (R) being already merged so sub-agents have a way to fix violations surfaced at scaffold/feature complete-phase.

8. **commit 8** — B: engine-composed finalize brief
   - Extend `briefs.go::BuildFinalizeBrief` with tier map / hostname sets / fragment list.
   - New atom `briefs/finalize/anti_patterns.md`.
   - Add 4 tests in `briefs_test.go`.

9. **commit 9** — D: `verify-subagent-dispatch` action
   - Edit `handlers.go` to add the action handler.
   - Edit `phase_entry/scaffold.md` to mandate the verify call.
   - Add 3 tests in `handlers_test.go`.

**Tranche 3**:

10. **commit 10** — Y1: `writeComment` strips leading `#`
    - Edit `yaml_emitter.go::writeComment`.
    - Add 2 tests in `yaml_emitter_test.go`.

11. **commit 11** — Y2: `writeRuntimeDev/Stage` fallback
    - Edit `yaml_emitter.go::writeRuntimeDev` + `writeRuntimeStage`.
    - Add 2 tests.

12. **commit 12** — Y3: `Service.SupportsHA`
    - Edit `plan.go` to add field + `managedServiceSupportsHA` helper.
    - Edit `yaml_emitter.go::writeNonRuntimeService` to honor the flag.
    - Update plan composition (provision phase) to populate `SupportsHA`.
    - Add 2 tests.

**Tranche 4 — CHANGELOG + readiness sign-off**:

13. **commit 13** — CHANGELOG entry summarizing all twelve fixes; system.md §4 verdict table updated to add `subdomain-double-scheme` (TEACH), `claude-md-zcp-tool-leak` (TEACH), Service.SupportsHA (TEACH).

### Fast-path

If time pressure forces a partial run-12 dogfood: **Tranche 1 alone** is viable. The content fixes (E + A + C + I + M) cascade to brief composition immediately; a dogfood run against a Tranche-1-only engine produces a recipe that teaches the right env pattern and ships porter-runnable CLAUDE.md. The finalize hand-edit pattern persists, but the recipe content is correct.

**Tranche 1 + 2** is the recommended must-ship.

**Tranche 3** is strongly recommended — the cosmetic defects are visible to anyone reading the published yaml.

---

## 4. Acceptance criteria for run 12 green

### Inherited from run 11 (continue to hold)

1. All five phases close `ok:true`.
2. Three sub-agents in parallel for scaffold (single-message Agent dispatch).
3. Per-codebase apps-repo content lands at `<cb.SourceRoot>/`.
4. Per-codebase `.git/` initialized with at least one scaffold commit.
5. Recipe-root README templated; per-tier READMEs ≥ 40 lines.
6. Workspace yaml inline-imported; deliverable yamls written at `<outputRoot>/`.
7. V-5 abstract litmus holds (no run-10 anti-patterns reintroduced).
8. Run-11 cleanup demotions hold (no validator regressed to blocking).

### New for run 12

9. **Apps-repo `zerops.yaml run.envVariables` declares own-key aliases.** Every cross-service var the codebase reads has a corresponding `<OWN_KEY>: ${<platform_key>}` line in run.envVariables. Code reads `process.env.<OWN_KEY>`.

10. **Zero `process.env.<platform_key>` references in committed code** for any cross-service var. No `process.env.db_hostname`, `process.env.cache_hostname`, `process.env.broker_hostname`, `process.env.search_masterKey`, `process.env.storage_apiUrl`, `process.env.<host>_zeropsSubdomain`. All cross-service reads go through own-key aliases.

11. **Zero `https://${<host>_zeropsSubdomain}` occurrences** in any committed source file or zerops.yaml run.envVariables block. Validator `subdomain-double-scheme` would have caught the run-11 bug at scaffold complete-phase.

12. **Apps-repo CLAUDE.md is porter-runnable.** Zero `zerops_*` MCP tool names, zero `zcli ` invocations, zero `zcp ` invocations. Dev-loop section uses framework-canonical commands.

13. **Apps-repo IG focuses on platform mechanics.** Zero recipe-internal contracts (subject naming, cache key shape, image storage layout, queue topic conventions) inside the integration-guide extract markers. Recipe-internal content routes to KB or claude-md/notes.

14. **Finalize closes through `record-fragment`.** Zero direct file edits in finalize. Codebase-scoped violations surface at scaffold/feature complete-phase and get fixed there via `record-fragment mode=replace`.

15. **Finalize dispatch prompt size ≈ engine brief size.** Within 10%. No hand-typed math errors, no obsolete paths.

16. **Every Agent dispatch verified via `verify-subagent-dispatch`.** Engine brief body byte-identical inside the dispatched prompt.

17. **Zero `# # ` doubled-prefix lines** in any of the 6 published tier import.yamls.

18. **Tier 0 + 1 runtime services carry per-codebase comments.** Same shape as tier 2-5 for the per-codebase comment block.

19. **Tier 5 yaml mode field matches platform capability.** `mode: NON_HA` for meilisearch (or any other non-HA-capable managed service in the recipe); `mode: HA` for HA-capable services.

20. **Zero engine-bug facts recorded** that name surface defects E / A / C / G / R / B / D / Y1 / Y2 / Y3 fixed in this readiness. Equivalent to: run 12 fact #N entries don't include `record-fragment-append-only`, `tier5-mode-ha-uniform`, `*-double-scheme`, `claude-md-mcp-tool-leak`, etc.

### Stretch criteria

21. **Apps-repo IG has 4-7 items per codebase.** No bloat.
22. **Zero scaffold-only filename references in cross-codebase content.** Workerdev KB doesn't mention apidev's `migrate.ts` by name.
23. **Click-deployable end-to-end.** A porter clones `zerops-recipe-apps/<slug>-<codebase>`, gets `npm install && npm run dev` working without modification (assuming they have a Zerops project + the same managed services in their project).

---

## 5. Non-goals for run 12

- **No re-design of the env-var-model atom architecture.** Workstream E rewrites the brief content; the on-demand guide stays as is. The orphan atom gets deleted (not kept-and-rewritten) to reduce surface area.
- **No new spec PRs.** The spec at [`docs/spec-content-surfaces.md`](../../spec-content-surfaces.md) and [`docs/zcprecipator3/system.md`](../system.md) are authoritative. Run 12 enforces the spec; doesn't modify it.
- **No re-promotion of run-11-cleanup-demoted validators.** They stay as notices.
- **No new fragment ids or new surfaces.** Run 12 fixes existing surfaces; no new content categories.
- **No publish-path work.** `zcp sync recipe publish` stays out of scope. Run 12's apps-repo content is structurally publishable; whether the publish action is wired is a separate concern.
- **No multi-codebase recipe-shape changes.** The 3-codebase showcase shape stays.
- **No catalog-of-frameworks atom additions.** A: alias-type contracts is platform-invariant (every recipe uses `${<host>_zeropsSubdomain}`). C: CLAUDE.md is porter-facing is platform-invariant. Neither requires per-framework knowledge.

---

## 6. Risks + watches

### Risk: G's gate split may surface previously-quiet violations

Codebase-scoped validators that ran only at finalize will now run at scaffold + feature. Some run-9/10/11 codebase content that was passing finalize (because the agent fixed in finalize) might fail at scaffold. The scaffold sub-agent then has to fix in-session, possibly multiple times.

**Watch**: monitor scaffold complete-phase iteration count in run 12. If sub-agents iterate more than 2-3 times on codebase-IG/KB/CLAUDE violations, the brief teaching is unclear. Adjust C / E / I content rather than weakening the gate.

### Risk: D's verify-dispatch may break parallel scaffold timing

Currently, main agent dispatches 3 scaffold sub-agents in a single message. After D, main has to call `verify-subagent-dispatch` for each — adds 3 tool calls. If verify is slow or sub-agents start before verify completes, the parallel-dispatch K directive (run-10/11 carry-forward) may break.

**Mitigation**: D's verify-dispatch is a pre-dispatch check — main agent verifies the prompt BEFORE calling Agent. If verify fails, main fixes the prompt and re-verifies; only then dispatches. Sub-agents start with verified prompts. The 3 verify calls add ~3-9s; parallel dispatch's ~17m wall time absorbs that.

### Risk: `*_zeropsSubdomain` full-URL contract changes between recipes

The user's framing assumes `${apidev_zeropsSubdomain}` always carries `https://`. Verify this against the platform: cross-service env-var injection format. If the format ever resolves to bare hostname for legacy/internal reasons, A's table needs caveats.

**Watch**: provision-phase run 12, `zerops_discover includeEnvs=true`. The values returned for `apidev_zeropsSubdomain` should be the literal template `${apidev_zeropsSubdomain}` (per `environment-variables.md` lines 117-119). Resolved values only visible inside the running container via `ssh ... env`. Verify shape before accepting A as written.

### Risk: Y3's family table is incomplete

`managedServiceSupportsHA` covers the families in run-11's recipe (postgresql/valkey/nats/meilisearch). Other families a future recipe might use (kafka, mailpit, custom services) need correct entries. Conservative default = `false` errs on the safe side (NON_HA emit) but a porter expecting HA gets it wrong silently.

**Mitigation**: Y3's table starts with the families we have explicit knowledge for; default `false` for unknown. Add a `TODO` comment at the helper. Run 13+ recipes that use new managed-service families add to the table.

### Risk: E's "own-key alias" pattern conflicts with framework conventions

Some frameworks have IDIOSYNCRATIC env-var conventions. NestJS has none particular; Laravel uses `DB_HOST`/`REDIS_HOST` (which align well); Vite expects `VITE_*`; Astro/SvelteKit/Next have their own patterns. The brief should let the agent pick framework-canonical keys (which is what laravel-showcase does).

**Mitigation**: E's brief content says "framework-convention renames" — uses the framework's idiomatic keys, not ad-hoc names. The example shows Laravel-style `DB_HOST` + Vite-style `VITE_*` would coexist. Sub-agent picks per-framework.

### Risk: C's zcp-tool-name list misses framework-specific zcli idioms

The list bans `zerops_*`, `zcli `, `zcp `. But laravel-showcase-app/CLAUDE.md uses `ssh appdev '...'` — that's NOT zcp/zcli, it's plain SSH against a Zerops-mount alias. Should that be banned?

**Decision**: NO. `ssh <alias>` is the porter's own SSH client invocation against a host they configured. Not a Zerops-control-plane primitive. C bans only the zcp control-plane tool names.

---

## 7. Open questions

1. **Should `principles/env-var-model.md` be deleted or rewritten?** It's currently an orphan atom (not loaded by any brief composer). Run-12 readiness picks delete. Alternative: rewrite to align with the brief + load it via `BuildScaffoldBrief`'s atom list. Loading it would be more authoritative but adds ~50 lines of brief content. The brief's own "Managed services" section is the practical teaching site; the atom's three timelines (workspace / scaffold / deliverable) might be useful elsewhere. Resolution: delete for now, recreate later if a need surfaces.

2. **Should D's `verify-subagent-dispatch` be enforced (refuse if mismatch) or warned (proceed but log)?** Run-12 readiness picks enforced. Alternative: warn on first run, enforce on next. Risk: enforced breaks if main agent has a legitimate reason to extend the brief (parent recipe excerpt, codebase-specific decisions). Mitigation: D's check is "engine brief body APPEARS in dispatched prompt" — wrapper additions are allowed; truncations aren't. Run-12 main agent may add ~1-3 KB wrapper for codebase-specific decisions; that's fine.

3. **Is the IG scope rule too strict?** Run 12 target: 4-7 IG items. apidev IG in run 11 had 11. If run 12 produces a 4-item IG that misses a real platform-mechanic concern (e.g. forgets to mention SIGTERM drain because "drain is in KB"), the IG is too thin.

   Mitigation: I's brief content lists what DOES belong (bind 0.0.0.0, trust proxy, env aliasing, SIGTERM if Zerops-specific) AND what doesn't. Sub-agent decides per-codebase.

4. **What's the right CLAUDE.md length target post-C?** Reference is 33 lines. Run-11 was 36-48. After C tightens (no MCP refs, no dev_server invocation), the dev-loop becomes 1 line instead of 5. CLAUDE.md may shrink to 25-35 lines. Validator's 30-50 target may need to drop to 20-50.

   Resolution: keep 30-50 target; if CLAUDE.md falls below 30 lines, the brief teaching is too aggressive. Adjust based on run-12 output.

5. **Run 12 fresh slug or replay nestjs-showcase?** Replay isolates the engine changes (same plan, same managed services, same fact-discovery surface). Fresh slug tests transferability (does the brief generalize to a different framework / scenario).

   Recommendation: **replay nestjs-showcase**. Run 12 is testing whether the engine fixes produce a better deliverable for the SAME inputs. Fresh slug introduces noise. Replay every readiness until a fresh slug specifically tests transferability.

---

## 8. After run 12 — what's next

If run 12 closes green on criteria 9-19:

- Run 12's content quality should be **8/10 vs reference** (today 6/10). The env-pattern + CLAUDE.md fixes (E + A + C) lift content quality the most. The engine-flow fixes (G + R + B + D) eliminate the hand-edit pattern. The cosmetic fixes (Y1 + Y2 + Y3) clean up published yaml.

- Run 13 readiness focuses on remaining smell items from run-11 PROMPT_ANALYSIS that didn't make run-12: R-15 (sub-agent startup ToolSearch tax — harness change, out of v3 scope), R-16 (mid-IG `## ` prose subsection validator), R-17 (cross-codebase scaffold-filename leak validator), R-18 (long-term wrapper-drift elimination via `build-subagent-prompt` action).

If run 12 closes RED on any of 9-19:

- ANALYSIS will name the structural cause. Most likely places it goes wrong:
  - **E partial** — agent picks own-key aliases inconsistently (some yaml has them, others don't). Brief content needs a stronger "always alias every cross-service var you read" rule.
  - **C partial** — agent puts `npm run start:dev` in dev-loop but adds an MCP-reference elsewhere (e.g. CLAUDE.md "Notes" mentions `zerops_logs` for debugging). Validator's tool-name list catches it; brief content needs to extend the rule beyond the dev-loop section.
  - **G iteration loop** — scaffold sub-agent iterates 4+ times on codebase-IG violations because the brief teaching for IG shape is unclear. Adjust I or content_authoring.md's IG section; don't weaken G.

The whole-engine path forward stays:
- Tighter audience boundary (porter vs authoring agent) per system.md §1.
- TEACH-side positive shapes per system.md §4.
- Catalog drift stays managed via the cleanup-demote pattern (run-11 model).
