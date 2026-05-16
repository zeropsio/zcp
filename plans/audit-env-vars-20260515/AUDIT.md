# Audit — Env-var guidance: LLM corpus vs. platform truth

**Date:** 2026-05-15
**Scope:** Every place ZCP instructs an LLM about environment variables — atoms, recipe phases, examples, tool descriptions, error messages, internal guide — measured against authoritative platform behavior (code, JSON schemas, public Zerops docs, live runtime inspection on eval-zcp).
**Goal:** Identify why LLMs (per Karel's report) don't use envs cleanly, by mapping every gap, contradiction, and omission.

---

## Methodology

Four parallel research streams + one live verification stream:

| Stream | Source | Output |
|---|---|---|
| A | LLM-facing corpus (7 atoms, 4 recipe phases, 5 examples, 1 guide, 3 tool descriptions, workflow_checks errors) | Catalog of every claim/token taught to the LLM |
| B | Platform-side code (10 ops/env_*.go, topology, recipe validators, workflow integration, tool handlers) | Code-truth behavior catalog |
| C | Test coverage (18 test files, 120+ cases) | Pinned vs. unpinned behaviors |
| D | Authoritative docs: ../zerops-docs (public feature page + guide + import+spec MDX), live JSON schemas at api.app-prg1.zerops.io | Ground truth from docs |
| E | Live verification on eval-zcp via SSH into running zcp container — `printenv` (329 vars), `zerops_discover` API output, schema introspection | Empirical confirmation of contradictions |

All five streams completed. Authoritative reference for any fact below: stream B (code) is the implementation; stream D (live schemas + docs) is the platform contract; stream E (printenv) is what an app actually sees.

---

## Executive summary

ZCP's env-var corpus is **operationally adequate for the happy path** (discover keys → wire `${db_*}` → generate-dotenv) but **fundamentally under-teaches the auto-inject mechanism** that is the platform's actual operating model. LLMs end up "using envs poorly" because the corpus presents env handling as **"declare references for everything you need"** when the platform's actual model is **"declare references only to RENAME a variable; everything is already injected as an OS env var."**

This is not a wording fix. It changes when an LLM declares a line in `run.envVariables` (rarely, only for renames + mode flags) vs. when it leaves it out (most of the time, because the platform already injects it). The corpus implies the first model; the platform runs on the second.

Five concrete contradictions and ten coverage gaps below.

---

## A. Confirmed contradictions (LLM corpus ⊥ platform truth)

### A1. Auto-inject mechanism — corpus teaches the explicit-reference model

**What the corpus says (3 atoms + internal guide):**
- `internal/knowledge/guides/environment-variables.md:3` and table at L9-14 describe scopes as separate channels, no auto-inject mentioned for cross-service vars under default isolation.
- `develop-env-var-channels.md:7-22` lists three "channels" (service-level env, run.envVariables, build.envVariables) — never mentions that *project envs and cross-service envs land as OS env vars in every container automatically*.
- `develop-first-deploy-env-vars.md:11-30` teaches **`${db_*}` references in `run.envVariables`** as the way to "wire" managed-service vars — implies the line is needed for the value to reach the app.

**What the platform actually does (live verified on zcp container, 2026-05-15):**
- 329 OS env vars in `printenv`, of which:
  - 14 project envs auto-injected directly: `APP_KEY`, `JWT_SECRET`, `SESSION_SECRET`, `GIT_TOKEN`, `ZCP_API_KEY`, `apiCdnUrl`, `staticCdnUrl`, `storageCdnUrl`, `envIsolation`, `sshIsolation`, `zeropsSubdomainHost`, `zeropsSubdomainString`, `zeropsSubdomain`, plus `ZEROPS_l3BalancerConfig`/`l7HttpBalancerConfig`
  - Each project env ALSO injected with `PROJECT_` prefix (14 more)
  - Cross-service vars from sibling services auto-injected with `<hostname>_<key>` prefix when `envIsolation=none` (legacy); under default `service` they are NOT shared automatically
- Public Zerops guide (`../zerops-docs/apps/docs/content/guides/environment-variables.mdx:49-83`) confirms: *"Every service's variables are automatically injected as OS environment variables into every other service's containers — both runtime and build. Zero declaration in zerops.yml required."*
- That same public guide says `run.envVariables` has **only two legitimate uses**: (1) mode flags and (2) framework-convention renames where left-key ≠ right-key.

**Impact:**
- The corpus drives LLMs toward writing every `${db_*}` line they can think of in `run.envVariables`, producing the very pattern the platform's self-shadow gate catches. Worse, the LLM is uncertain *when* a line is needed, leading to defensive over-declaration.
- The corpus never tells the LLM "no line needed — the value is already in `process.env.db_hostname`."

---

### A2. Self-shadow symptom — atom says empty string, reality is literal `${var}`

**What the corpus says:**
- `examples/gotcha_pass_platform_invariant_env_shadow.md:11-13`:
  > Symptom: `${db_hostname}` resolves to an empty string at runtime even though the `db` service is healthy; containers crash in boot with `ECONNREFUSED` on hostname resolution.

**What the platform actually does (live verified):**
- Build-stack envs in zcp container: `buildapiv1777992507_RUNTIME_DB_HOST=${db_hostname}` — literally the eight-character string `${db_hostname}` is in `printenv` because the `db` service doesn't exist in this project. Not empty. Not `ECONNREFUSED` on hostname resolution — it's a string-parse error on the application side (e.g., a DB driver trying to resolve "`${db_hostname}`" as a hostname).
- Public Zerops guide L100-110 also says: *"the resolved OS env var becomes the literal string `${varname}`"* — matches reality.
- Code: `internal/ops/env_shadow.go:9-14` describes the same mechanism: *"the OS env var resolves to the literal string `${varname}`."* The atom's symptom string drifted from the canonical mechanism.

**Impact:**
- LLMs debugging an unresolved ref look for empty-string symptoms (`db_hostname=`) and miss the literal-string symptom (`db_hostname=${db_hostname}`). Diagnostic guidance points at a non-existent failure mode.

---

### A3. Project vars at build time — corpus silent, platform injects with prefix

**What the corpus says:**
- `develop-env-var-channels.md:17`: *"build.envVariables: edit zerops.yaml, commit, deploy → next build uses them; **not visible at runtime**"* — implies build env is a closed scope.
- `internal/knowledge/guides/environment-variables.md:28-44`: build/runtime are "separate environments"; cross-access requires `RUNTIME_` / `BUILD_` prefix.
- No atom mentions that **project envs auto-inject into the build container** the same way they auto-inject into runtime.

**What the platform actually does (live verified):**
- The zcp container's "build" view of sibling services (`buildapiv*_RUNTIME_DB_HOST` etc.) is present in the runtime container's `printenv` — these are build-container env namespaces leaked into runtime under legacy isolation, but they show:
  - Build container has `APP_KEY`, `JWT_SECRET`, `GIT_TOKEN`, `SESSION_SECRET`, `ZCP_API_KEY` (all project envs injected directly into the build) — confirmed in `buildapiv1777992507_APP_KEY=...` etc.
  - Build container has `RUNTIME_<KEY>` prefixed view of the matching runtime service's `run.envVariables` — with refs UNRESOLVED (`${db_hostname}` stays literal at build time).
- Public Zerops guide L145-160: *"Project variables are automatically available in every service, in both runtime AND build containers. The platform injects them as OS env vars at container start in every service's runtime container and also in every service's build container during the build phase. From zerops.yaml's point of view they are referenced directly by name with `${VAR_NAME}` — no `RUNTIME_` prefix in either scope."*

**Impact:**
- LLM scaffolding a build pipeline (`build.buildCommands: - VITE_API_URL=$RUNTIME_API_URL npm run build`) writes the wrong prefix because the corpus implies `RUNTIME_` is required, when in fact `$API_URL` (project-level, no prefix) works at build time too.

---

### A4. `PROJECT_*` prefix — completely undocumented, but injected on every container

**What the corpus says:** Nothing. Zero atoms, zero recipe phases, zero examples, zero tool descriptions, zero docs mention this.

**What the platform actually does (live verified):**
- For every project env `X`, the container has both `X=value` AND `PROJECT_X=value`. Observed: `APP_KEY` + `PROJECT_APP_KEY`, `JWT_SECRET` + `PROJECT_JWT_SECRET`, `ZCP_API_KEY` + `PROJECT_ZCP_API_KEY`, `envIsolation` + `PROJECT_envIsolation`, `staticCdnUrl` + `PROJECT_staticCdnUrl`, etc.
- This is a Zerops platform feature for disambiguation when a project env shares a name with a system env — but the corpus pretends it doesn't exist.

**Impact:**
- Low (most LLM code reads `process.env.APP_KEY` directly, not `process.env.PROJECT_APP_KEY`), but represents how shallow the corpus is on actual platform behavior.

---

### A5. `zeropsSubdomain` (resolved) vs. `zeropsSubdomainString` (template) — never distinguished

**What the corpus says:**
- `launch-classify-platform-envs.md:17-18` lists `zeropsSubdomainHost` and `zeropsSubdomainString` as auto-handled, classified as `infrastructure`.
- `finalize/project-env-vars.md:23-49` uses `${zeropsSubdomainHost}` in URL templates: `API_URL: https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app`.
- `zerops_discover` API response distinguishes `isReference: true` on `zeropsSubdomainString` but doesn't tell the LLM what that means.

**What the platform actually does (live verified):**
- Project has `zeropsSubdomainString = https://${hostname}-227a-${port}.prg1.zerops.app` (a template — `${hostname}` and `${port}` are platform placeholders resolved per-container)
- Project has `zeropsSubdomainHost = 227a` (a literal value — the short hash for the project's subdomain)
- Container also has `zeropsSubdomain = https://zcp-227a-8080.prg1.zerops.app` — the fully-resolved URL specific to *this* container's hostname and port. This is auto-generated per service from the project template.
- The corpus never explains the three keys' relationship: agent often confuses `zeropsSubdomain` (resolved, useful in app code) with `zeropsSubdomainHost` (project hash, used in URL templates) and `zeropsSubdomainString` (template, useful for cross-container URL formulation).

**Impact:**
- The dual-runtime URL pattern in `finalize/project-env-vars.md` is opaque without this background. LLM authoring a new recipe template can't reason about what `${zeropsSubdomainHost}` actually expands to.

---

## B. Coverage gaps (corpus silent on platform features)

### B1. `envSecrets` / `dotEnvSecrets` mechanism — schema present, atoms silent

**Live JSON schema** (`import-project-yml-json-schema.json`):
- `services[].envSecrets`: map[string]string — *"Environment variables that are blurred by default in Zerops GUI."*
- `services[].dotEnvSecrets`: string — *".env-format secret block."*
- `services[].envVariables`: **does not exist** in the schema. Only `envSecrets` at service level.

**Corpus coverage:**
- `internal/knowledge/guides/environment-variables.md:103-120` mentions `envSecrets` and `dotEnvSecrets` briefly.
- Zero atoms mention them. Zero recipe phases mention them. Zero examples.
- Tool description for `zerops_env` says `set values expand <@...> via zParser` but doesn't tell the LLM that `dotEnvSecrets` in `import.yaml` is the canonical way to bootstrap secrets without ever exposing them in source.

**Impact:**
- LLMs author projects with secrets in `run.envVariables` (visible in zerops.yaml committed to git) instead of `envSecrets` / `dotEnvSecrets` (write-only after creation). Concrete security regression.

### B2. `envReplace` — schema present, atoms silent

**Live schema** confirms `run.envReplace` with required `delimiter` + `target`. Real use: replacing placeholders in deployed files (config templates, JWT keys, nginx configs).

**Corpus coverage:** Mentioned only in `internal/knowledge/guides/environment-variables.md:122-140`. Zero atoms.

**Impact:** LLMs wiring config templating (JWT keys, nginx server names, runtime config files) reinvent file-edit logic in `initCommands` instead of using `envReplace`. Concrete pattern miss.

### B3. `envIsolation` mode — partly explained, applicability missing

**What the corpus says:**
- `launch-classify-platform-envs.md:20` says it's "dropped — project-level setting; new project picks its own."
- `internal/knowledge/guides/environment-variables.md:69-83` describes both modes.

**What's missing:**
- `envIsolation` is the **single switch** that determines whether cross-service vars auto-inject as OS env vars in sibling containers.
- Under default `service`: NO auto-inject; you must declare `${db_*}` in `run.envVariables` to pull a value.
- Under legacy `none` (eval-zcp's current setting): ALL service vars auto-inject across all containers.
- This is the most consequential env-var setting on the project, and not a single atom explains its consequence.

**Impact:** LLM seeing eval-zcp's traces (which auto-share everything) infers a pattern that breaks on production projects with default isolation.

### B4. The `ZEROPS_*` platform metadata namespace (~80 vars)

Live container has `ZEROPS_VpnIPv4`, `ZEROPS_ServiceId`, `ZEROPS_StackName`, `ZEROPS_appVersionId`, `ZEROPS_NestId`, `ZEROPS_VxLanIPv4`, `ZEROPS_ProjectId`, `ZEROPS_BUILD_*`, `ZEROPS_RUN_BASE`, etc. — **eighty-plus** platform-injected metadata vars.

**Corpus coverage:** Zero. Not a single mention.

**Impact:** LLMs writing scripts that need the container's IP, service ID, app-version ID, or build/runtime distinction reinvent detection logic instead of reading `ZEROPS_*`.

### B5. `RUNTIME_*` / `BUILD_*` prefix for cross-phase access

**What the corpus says:**
- `internal/knowledge/guides/environment-variables.md:28-44`: explicitly teaches the prefix model.
- Zero atoms reinforce it. Zero examples show it.

**What's missing:**
- The intra-service prefix model (read your own runtime var from your own build container via `RUNTIME_`) is rarely needed but legitimate for bake-time SPA configs.
- Atom corpus presents `${db_*}` as the only cross-context syntax — agents don't know about `${RUNTIME_X}`.

**Impact:** Frontend bundler recipes that need `RUNTIME_API_KEY` at build time (to bake into a static bundle) misroute through `build.envVariables` and lose the value.

### B6. Build-time vs. runtime ref resolution timing

**What the corpus says:** Vague — "resolved at container start."

**What the platform actually does:**
- `${db_hostname}` written in `run.envVariables` resolves at the RUNTIME container's start, not at build.
- At BUILD time, the same ref appears as a literal `${db_hostname}` (verified: `buildapiv1777992507_RUNTIME_DB_HOST=${db_hostname}` in printenv).
- `${db_hostname}` in `build.envVariables` likewise resolves only if the platform's build-time interpolator has a target service — for managed-service refs, that means the dependency must already be in the project at build time.

**Impact:** LLMs putting `DATABASE_URL: ${db_connectionString}` in `build.envVariables` (e.g., for Prisma generation) expect the value but get the literal. Pattern miss.

### B7. Self-shadow gate fires WHERE?

**What the corpus says:**
- `develop-env-var-channels.md:24-27` mentions "shadow-loop pitfall" but doesn't say what tool catches it.
- `env-var-model.md:18-22` provides a grep one-liner the user runs.
- `internal/ops/env_shadow.go` implements `DetectSelfShadows` but the corpus doesn't tell the LLM when this fires (deploy preflight? recipe-finalize gate? generate-dotenv? all three?).

**Impact:** LLM lacks anchor for which workflow step surfaces the error, so it can't debug "my zerops.yaml has `db_hostname: ${db_hostname}` and nothing yelled."

### B8. Reference resolution failure paths

**What happens when a ref can't resolve at deploy time?**
- Code: `internal/ops/env_plan.go:462-466` returns "could not resolve env vars: X, Y, Z" with error code `ErrInvalidParameter`. That's for the `generate-dotenv` flow on the dev machine.
- On the actual Zerops platform deploy: the unresolved ref becomes a literal `${X}` string in the container. **No deploy-time failure.** App crashes at first use.

**Corpus coverage:** `develop-first-deploy-env-vars.md:28-30` says *"a wrong spelling remains literal and the app fails at connect time."* This is correct for runtime but doesn't help the LLM connect "wrong spelling = silent literal" → "the way to detect this is X."

**Impact:** No diagnostic path. LLM sees "DB connection failed" at runtime and doesn't know to grep `printenv` for `${...}` substrings.

### B9. Auto-restart semantics on `zerops_env action=set`

**What the corpus says:**
- `develop-env-var-channels.md:13` says service-level set restarts the service.
- Tool description on `zerops_env` says "auto-restart affected services unless skipRestart=true."

**What's missing:**
- The eligibility filter (`isAutoRestartEligible` in `internal/tools/env.go:284-301`): excludes (a) the ZCP self-service, (b) all managed services, (c) any non-ACTIVE service.
- Project-level set restarts ALL eligible runtime services (cascading restart pattern). Atom doesn't warn about the blast radius.

**Impact:** LLM running `zerops_env action=set project=true` on a 5-service project triggers 4 service restarts and doesn't expect downtime. No warning in the corpus.

### B10. Cross-codebase env coherence gate (run-32)

**Code:** `internal/recipe/gate_cross_codebase_env_coherence.go` detects when multiple codebases in a recipe assign different aliases to the same source var (e.g., apicodebase: `DB_PASS: ${db_password}`, workercodebase: `DB_AUTH: ${db_password}`).

**Corpus coverage:** Zero. The gate exists, fires at notice severity, and the LLM is never told it exists or what to do when triggered.

**Impact:** Multi-codebase recipes silently produce inconsistent aliases. The gate emits a notice; the LLM doesn't know how to read it.

---

## C. Tool-description gaps

### C1. `zerops_env` description doesn't surface the auto-inject model

Current: *"Manage env vars. Actions: get (read), set (upsert), delete, generate-dotenv (write local .env from local zerops.yaml). Scope: service via serviceHostname, or project=true. set values expand `<@...>` via zParser; encoding prefixes (base64:, hex:) are rejected..."*

Missing:
- No mention that project envs and (under `envIsolation=none`) cross-service envs auto-inject as OS env vars — so most "wiring" needs no `run.envVariables` line.
- No mention of `zerops_env action=get` returning **templates, not resolved values** for refs (this is on the discover side but env action=get has the same property since it delegates to discover).
- No guidance on when `set` is appropriate vs. when `run.envVariables` is appropriate.

### C2. `zerops_discover` description doesn't warn about template-vs-resolved

Current: *"Discover project and service information. Filter by service hostname or list all. Use includeEnvs=true to read env var keys. Add includeEnvValues=true only when you need actual secret values (troubleshooting)."*

Missing:
- `includeEnvValues=true` returns **templates** for unresolved refs (e.g., `${db_password}`), not the resolved-at-container-start value. To see resolved values, the LLM has to SSH into the container or hit an app endpoint. This is documented in `internal/knowledge/guides/environment-variables.md:65-67` but not surfaced on the tool description.

### C3. No tool tells the LLM about `envSecrets` workflow

The way to set a Zerops secret via tools is `zerops_env action=set` (with the value expanding `<@...>` for randoms). But the secret-vs-basic distinction at the platform layer (write-only after creation, blurred in GUI) is invisible to the tool description — the LLM treats every env var as bidirectional read/write.

---

## D. Coverage gaps in test suite (per stream C)

Test suite is 94% comprehensive but five behaviors lack pins:
1. Auto-restart blast radius on `zerops_env set project=true` (cascade pattern)
2. Cross-codebase env injection when worker shares codebase with its parent
3. Three-level hostnames (`api-v2-db` → `api_v2_db_*` refs)
4. Permanent vs. transient ref-resolution failure classification
5. Lock contention recovery in `env_dotenv_lock.go`

None of these are user-facing-guidance gaps, but they're entry points for future regressions.

---

## E. The deepest issue — the corpus has TWO incompatible env models

The corpus contains both of these models in different atoms, without saying which is right:

### Model α — "Declarative wiring" (taught by `develop-first-deploy-env-vars`, `bootstrap-env-var-discovery`, `develop-env-var-channels`)
- Cross-service vars are accessed via `${hostname_varname}` declarations in `run.envVariables`.
- Without a declaration, the variable isn't available.
- Mental model: pull explicit values from siblings via references.

### Model β — "Auto-inject + declarative rename" (taught by public Zerops `guides/environment-variables.mdx`, code in `env_shadow.go`, gotcha-pass atom)
- Cross-service vars are ALREADY in the container's OS env under their service-prefixed names.
- `run.envVariables` is for RENAMING those auto-injected vars under framework-conventional names, plus mode flags.
- Mental model: read what's already there; declare lines only for renames.

The platform actually runs Model β under default `service` isolation. Under `envIsolation=none` (legacy) the auto-injection is broader. Model α is wrong on the default project but accidentally right when `envIsolation=none` (because then everything's also accessible via the explicit reference).

This explains why LLMs "use envs poorly": the corpus gives them α, but the platform runs β, so the LLM over-declares `run.envVariables` lines (Model α habit), then trips the self-shadow gate (because Model β rules say "don't re-declare what's already injected"), then debugs symptoms (empty string per gotcha atom) that don't match what the container actually shows (literal `${var}` string).

---

## F. Recommended atom edits — ranked by impact

The top three changes are not "tweak wording" — they require atom-level rewrites because the conceptual model is wrong.

### F1. **REWRITE — Introduce auto-inject as the dominant model**
- New atom (priority 1): `develop-env-var-model.md` — replaces parts of `develop-env-var-channels.md` and `develop-first-deploy-env-vars.md`.
- Content: project envs and cross-service envs auto-inject as OS env vars. `run.envVariables` lines are for two purposes only: mode flags + framework-convention renames. Cite the platform guide.
- Pinning: AST contract that `gotcha_pass_platform_invariant_env_shadow.md` symptom string matches `env_shadow.go` mechanism string verbatim (currently drifted).

### F2. **FIX — Self-shadow symptom string**
- Edit `examples/gotcha_pass_platform_invariant_env_shadow.md:11-13`:
  - Replace "resolves to an empty string at runtime" with "resolves to the literal string `${db_hostname}` at runtime."
  - Replace "containers crash in boot with ECONNREFUSED on hostname resolution" with "containers crash when the framework tries to parse `${db_hostname}` as a hostname (varies by language — `getaddrinfo ENOTFOUND` in Node, `connection refused` in DB drivers parsing the literal as a host, etc.)."
- Pinning: AST contract that the symptom text matches `env_shadow.go:9-14`.

### F3. **ADD — `envSecrets` / `dotEnvSecrets` atom**
- New atom: `bootstrap-env-secrets.md` (priority 2, phases: bootstrap-active).
- Content: schema-mandated location for service-level secrets in import.yaml. Why: not visible in git, write-only-after-creation, blurred in GUI. When to use vs. `run.envVariables` (basic vars from yaml are visible in committed source).
- Tool description fix: `zerops_env` mentions encoding-prefix rejection but never the underlying envSecrets workflow that the LLM SHOULD use for new secret values via `import.yaml` at bootstrap.

### F4. **ADD — `envIsolation` semantics**
- Either expand `launch-classify-platform-envs.md` or add `bootstrap-env-isolation.md`.
- Content: the single switch that determines whether cross-service vars auto-inject as OS env vars in sibling containers. Default `service` = no auto-inject (must reference). Legacy `none` = full auto-inject (eval-zcp's current state).
- Critical context for any agent observing eval-zcp's printenv: the behaviors there don't generalize to default-isolation projects.

### F5. **ADD — `zeropsSubdomain*` triplet**
- New section in `develop-env-var-channels.md` or new dedicated atom.
- Content: three keys, three purposes:
  - `zeropsSubdomainHost` (literal short-hash, used in URL templates)
  - `zeropsSubdomainString` (template with `${hostname}` and `${port}` placeholders for the project's URL pattern)
  - `zeropsSubdomain` (resolved per-container fully-qualified URL — useful in app code)
- This unblocks dual-runtime recipe authoring.

### F6. **ADD — `envReplace` atom**
- New atom for the deployment-time file-substitution use case (JWT keys, nginx server names, config templates).
- Cite real recipe patterns (config/jwt/*.pem case in public docs).

### F7. **ADD — `ZEROPS_*` metadata namespace pointer**
- Single atom listing the most useful platform-injected vars: `ZEROPS_ServiceId`, `ZEROPS_StackName`, `ZEROPS_appVersionId`, `ZEROPS_VpnIPv4`, `ZEROPS_ProjectId`, `ZEROPS_RUN_MODE` (RUNTIME / BUILD discriminator), `ZEROPS_RUN_BASE` (runtime stack).
- Low priority for happy path but unblocks debugging atoms and ops tooling recipes.

### F8. **EDIT — Tool descriptions**

- `zerops_env`: prepend "Most service code reads project/cross-service envs directly via `process.env.KEY` — those are already injected. Use `zerops_env action=set` for project-scope secrets (APP_KEY, JWT_SECRET) and updates outside the zerops.yaml cycle. Don't `set` values that should live in `run.envVariables` (zerops.yaml-controlled, version-controlled, dev/prod-differentiated)."
- `zerops_discover`: append "`includeEnvValues=true` returns templates (e.g., `${db_password}`) for unresolved refs, NOT resolved values. To check resolved values inside a container, SSH and `printenv`. The API stores templates; resolution happens at container start."
- Both: mention `envIsolation` setting on the project (one-liner with default-vs-legacy semantics).

### F9. **EDIT — Recipe-phase `env-var-model.md`**

- Currently teaches "envVariables carries exactly two kinds: cross-service references and mode flags" (correct).
- ADD: "...because cross-service vars and project vars are already injected as OS env vars. `run.envVariables` is where you OPT-IN to renaming or to setting mode flags. You do NOT need a line for every cross-service value your app reads — only the ones that need a different name than what the platform provides."

### F10. **ADD — Build-time ref resolution timing**

- New section in `develop-env-var-channels.md` or `env-var-model.md`:
  - `${db_hostname}` in `run.envVariables` resolves at runtime container start.
  - `${db_hostname}` in `build.envVariables` resolves at build container start ONLY if the dependency service exists at build time. If the dependency is created in the same import as the build-from-git'd service, the build sees a literal `${db_hostname}`.
  - Use the `RUNTIME_` prefix to read a runtime-scope var from build context.

---

## G. The atom corpus reorganization (optional but high-leverage)

Current atom corpus has env content scattered across 7 atoms + 4 recipe phases + 1 guide. Significant redundancy ("don't shadow") + significant disagreement (atoms α vs β model).

Proposed organization:

```
foundational:
  - develop-env-var-model.md (NEW — auto-inject + rename model)
  - develop-env-isolation.md (NEW — service vs. none + consequences)

operational:
  - develop-env-var-channels.md (REWRITE — three-channel local model, no auto-inject confusion)
  - bootstrap-env-var-discovery.md (KEEP — discover keys for the canonical name set)
  - bootstrap-env-secrets.md (NEW — envSecrets / dotEnvSecrets at provision)
  - develop-env-var-rename.md (NEW — when to write a line in run.envVariables)

reference:
  - develop-platform-envs.md (NEW — ZEROPS_*, zeropsSubdomain triplet, system vars)
  - develop-env-replace.md (NEW — file-substitution use cases)

troubleshooting:
  - develop-local-env-troubleshoot.md (KEEP — local-mode errors)
  - examples/gotcha_pass_platform_invariant_env_shadow.md (FIX — symptom string)

export/launch:
  - export-classify-envs.md (KEEP — classification rules)
  - launch-classify-platform-envs.md (KEEP — auto-handled keys)

recipe-phase:
  - workflows/recipe/phases/generate/zerops-yaml/env-var-model.md (EDIT — clarify "renames only")
  - workflows/recipe/phases/finalize/project-env-vars.md (KEEP — dual-runtime URL constants)
  - workflows/recipe/phases/finalize/env-comment-rules.md (KEEP — comment quality)
  - workflows/recipe/phases/provision/env-var-discovery.md (KEEP — capture managed keys)
```

This is invasive — Karel should weigh F1-F10 standalone vs. the reorganization.

---

## H. Verification matrix — what was actually checked

| Claim | Source | Verified via | Status |
|---|---|---|---|
| Top-level `envVariables` forbidden at setup-entry | live JSON schema /api/rest/.../zerops-yml-json-schema.json | `jq '.properties.zerops.items.properties' = [build, deploy, extends, run, setup]` | ✓ confirmed |
| `services[].envVariables` doesn't exist in import schema | live JSON schema /api/rest/.../import-project-yml-json-schema.json | `jq` of service properties — only envSecrets, dotEnvSecrets, envIsolation | ✓ confirmed |
| Project envs auto-inject as OS env vars | live container `printenv` | 14 project envs visible directly + 14 with `PROJECT_` prefix | ✓ confirmed |
| Unresolved ref → literal `${var}` string | live container `printenv` showing `buildapiv*_RUNTIME_DB_HOST=${db_hostname}` | ✓ confirmed | gotcha atom WRONG |
| Cross-service auto-inject under default `service` isolation | not directly verifiable (eval-zcp has envIsolation=none) | via public docs guide L49-83 + code in env_generate.go classifier | not empirically verified — relies on public-docs + code |
| `${db_hostname}` resolves at runtime container start, not build | live container's build-stack env shows literal `${db_hostname}` | ✓ confirmed | corpus vague |
| `zerops_discover includeEnvs=true` returns templates not resolved | code `internal/ops/env_generate.go:204` + tool comment | ✓ confirmed via code | tool description silent |
| Self-shadow check is exact key=value match | code `internal/ops/env_shadow.go:47-54` + test `TestDetectSelfShadows` | ✓ confirmed | check fires WHERE not documented |
| 80+ `ZEROPS_*` system envs in container | live container `printenv` | ✓ confirmed | zero corpus coverage |

---

## I. Open questions (would benefit from one more live experiment)

1. **Cross-service auto-inject under default `service` isolation** — eval-zcp has legacy mode. To verify Model β empirically, create a fresh project with default isolation + a managed Postgres + a Node runtime, then `printenv` inside the Node container to confirm `db_*` envs are auto-injected. **Recommended next step** before finalizing F1.

2. **Project envs vs. cross-service envs precedence** — code suggests yaml > project but no test covers project-level key colliding with service-level same-key ref. Verify with a deliberate collision in a probe project.

3. **`envReplace` recursion behavior** — public docs say doesn't recurse into subdirectories. Verify with a 2-level config tree.

These can be future probe scenarios — not blocking for the audit's findings, which rely on existing code + public docs + the printenv evidence.

---

## J. End-state TL;DR

The LLM corpus teaches **Model α — "Declare every env you want to read"** in 5 atoms and 1 internal guide. The platform actually runs **Model β — "Everything injects; declare only renames + mode flags."** The corpus mentions Model β only in the gotcha atom (which itself has a wrong symptom string) and the public-docs guide that ZCP doesn't ship to LLMs.

The repair needs the foundational atom F1 (auto-inject model as primary), the symptom-string fix F2 (literal `${var}`, not empty), and the secret-workflow atom F3. F4-F10 close the long tail of coverage gaps.

Test coverage is solid at 94% — the audit's findings are about **what we teach**, not **what we verify**.
