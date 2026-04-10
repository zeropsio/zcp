# nestjs-showcase-v5 Post-Mortem — Structural Fixes

First-principles implementation guide for fixes derived from
`nestjs-showcase-v5` run on ZCP v8.50.3 (73f4633). Every fix is
structural — no monkey patches, no hardcoding. Order is
dependency-respecting: foundations first, guidance edits last so they
reference the new capabilities.

## Contents

- [Scope boundaries](#scope)
- [Fix 1 — Dual-runtime URL env-var pattern (the real structural fix for v4's `setup: stage` invention)](#fix-1)
- [Fix 2 — `generate-finalize` accepts `projectEnvVariables` as a first-class input](#fix-2)
- [Fix 3 — Provision defaults `minRam: 1.0` for runtime dev services](#fix-3)
- [Fix 4 — `initCommands` are observed via logs, not assumed](#fix-4)
- [Fix 5 — Close-step verification splits sub-agent (code) from main agent (browser)](#fix-5)
- [Fix 6 — agent-browser lifecycle via batch mode and stop-dev-processes-first](#fix-6)
- [Fix 7 — MCP wrapper around agent-browser (scoping only — deferred)](#fix-7)
- [Implementation order and blast radius](#order)

## Ground truth established before planning {#scope}

Before proposing fixes, three facts about the Zerops platform were
verified against the user, the public env-variables docs, the live JSON
schema, and the existing recipe knowledge base:

1. **`zeropsSubdomainHost` is a generated project-scope env var.** It
   exists from project creation onward — available at workspace
   provision time, at import time for deliverables, and at runtime in
   every service. It is NOT specific to any service. The value is the
   subdomain host fragment (e.g. `2100.prg1.zerops.app` or just
   `2100`, depending on context).
2. **Project-level env vars auto-inherit into every service's runtime
   context.** They are NOT referenced via `${VAR}` from
   `run.envVariables` of the same service — that creates a shadow loop.
   They simply appear as OS env vars in the container.
3. **The `RUNTIME_` prefix lifts runtime-scope env vars into
   `build.envVariables` YAML.** Runtime-scope includes project-level
   env vars (since those are in every service's runtime context), so
   `${RUNTIME_STAGE_API_URL}` inside `build.envVariables` reads the
   project-level `STAGE_API_URL`. This is the only way to compose
   project-level vars into YAML-defined build vars.

These three facts together enable the correct dual-runtime URL pattern
(Fix 1) and retire every wrong attempt v4 and v5 made before it.

---

## Fix 1 — Dual-runtime URL env-var pattern {#fix-1}

### First principle

Every service in a Zerops project has a deterministic public URL
*the moment the project exists*, derived from three knowns:
`${hostname}`, `${zeropsSubdomainHost}` (project-scope, present from
project creation), and the service's HTTP port (or nothing for static
services). The URL format is a platform constant:

```
# dynamic runtime on port N:
https://{hostname}-${zeropsSubdomainHost}-{port}.prg1.zerops.app

# static (Nginx, no port segment):
https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app
```

Because `zeropsSubdomainHost` is a project-scope env var present from
project creation, **the URLs can be computed at import time** (either
by the platform preprocessor, or by the recipe author writing the
value with `${zeropsSubdomainHost}` interpolation — which the
platform resolves when writing the project env var store).

Every previous v4/v5 mistake can be traced to not knowing this:
- v4 invented `setup: stage` with hardcoded `api-{host}` because it
  thought URL values had to be committed to one environment per build.
- v5 tried `RUNTIME_VITE_API_URL` set via `zerops_env` on `appdev`,
  which doesn't propagate across services on cross-deploy.
- v5 then pivoted to `API_PUBLIC_URL` as a project var but never set
  it on the workspace (only on the deliverables), causing the post-close
  CORS regression when the workspace's `FRONTEND_URL` contained the
  literal unresolved `${FRONTEND_PUBLIC_URL}` string.

The structural fix is one pattern applied consistently across the
workspace AND all 6 deliverable import.yaml files AND the app's
zerops.yaml.

### The pattern

**Define project-level URL constants, once per env, in
`project.envVariables`.** Names are deterministic:

- `STAGE_API_URL` — the public URL of the "stage" (end-user-facing)
  API slot. In env 0-1 this resolves to `apistage`; in envs 2-5 this
  resolves to `api` (the single-container prod slot).
- `STAGE_FRONTEND_URL` — same but for the frontend. `appstage` in
  envs 0-1, `app` in envs 2-5.
- `DEV_API_URL` — dev-pair API URL. ONLY defined in envs 0-1
  (dev/stage pair envs). Resolves to `apidev`.
- `DEV_FRONTEND_URL` — dev-pair frontend URL. ONLY in envs 0-1.
  Resolves to `appdev`.

Naming convention: `{SLOT}_{ROLE}_URL` where `SLOT ∈ {DEV, STAGE}`
and `ROLE` is the service role (`API`, `FRONTEND`, or any other
role the recipe introduces — e.g. `WORKER` if the worker has a public
surface, which it usually doesn't).

### Values — exact form per env

**Env 0 — AI Agent** and **Env 1 — Remote (CDE)** (dev-pair envs):
```yaml
project:
  envVariables:
    DEV_API_URL: https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app
    DEV_FRONTEND_URL: https://appdev-${zeropsSubdomainHost}.prg1.zerops.app
    STAGE_API_URL: https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app
    STAGE_FRONTEND_URL: https://appstage-${zeropsSubdomainHost}.prg1.zerops.app
```

**Env 2 — Local**, **Env 3 — Stage**, **Env 4 — Small Production**,
**Env 5 — HA Production** (single-slot envs, no dev pair):
```yaml
project:
  envVariables:
    STAGE_API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app
    STAGE_FRONTEND_URL: https://app-${zeropsSubdomainHost}.prg1.zerops.app
```

Port `3000` in the API URL is the NestJS default used by the
showcase — recipes with different API ports substitute accordingly.
Static frontends have no port segment (Nginx serves on standard 443).

### Consuming in zerops.yaml — frontend (SPA)

`build.envVariables` uses the `RUNTIME_` prefix to lift the
project-level var into the build context, where the bundler bakes it
into the SPA:

```yaml
zerops:
  - setup: prod
    build:
      envVariables:
        VITE_API_URL: ${RUNTIME_STAGE_API_URL}
      # …
    run:
      base: static

  - setup: dev
    build:
      envVariables:
        VITE_API_URL: ${RUNTIME_DEV_API_URL}
      # …
    run:
      base: nodejs@22
```

Dev and prod use DIFFERENT project vars (`DEV_*` vs `STAGE_*`), but
the `setup: dev` block is only invoked in env 0-1 (where `DEV_*`
exists). `setup: prod` is invoked in every env via cross-deploy (to
`appstage` in env 0-1, to `app` in envs 2-5) — `STAGE_*` is defined
in all 6 envs, with values that differ per env.

### Consuming in zerops.yaml — API

`run.envVariables` references project vars directly by name. Do NOT
shadow them with same-name service-level vars — name the
service-level var differently if it's a derived value, or just let
the project var flow through.

```yaml
zerops:
  - setup: prod
    run:
      envVariables:
        FRONTEND_URL: ${STAGE_FRONTEND_URL}
        APP_URL: ${STAGE_API_URL}
      # …

  - setup: dev
    run:
      envVariables:
        FRONTEND_URL: ${DEV_FRONTEND_URL}
        APP_URL: ${DEV_API_URL}
      # …
```

The NestJS bootstrap reads `process.env.FRONTEND_URL` for CORS and
`process.env.APP_URL` for anywhere it needs its own public URL.

### Workspace parity — the v5 CORS regression

**The workspace must have the same `project.envVariables` as the
deliverables.** This is the root cause of v5's post-close CORS bug.
Two enforcement points:

1. **Provision step (workflow guidance)**: when the agent writes the
   workspace `import.yaml` for `zerops_import`, it MUST include a
   `project.envVariables` block with the dev-pair variant (envs 0-1
   shape, because the workspace IS an env-0 workspace — it has
   `appdev`/`apidev`/`appstage`/`apistage`).

2. **Finalize step (code + guidance)**: `generate-finalize` MUST
   accept `projectEnvVariables` as a first-class input (see Fix 2)
   so the deliverables are generated from the same declarations as
   the workspace. No hand-editing after `generate-finalize` returns.

### Concrete edits for Fix 1

- **`internal/content/workflows/recipe.md`**
  - **Replace** the current "Dual-runtime zerops.yaml" subsection
    ("How the frontend learns the API URL" — platform-level, added
    in v8.50.1 Error 2) with a **concrete, prescribed** pattern:
    the project-var declarations (envs 0-1 shape and envs 2-5 shape),
    the frontend `build.envVariables` consumption with `RUNTIME_`
    prefix, and the API `run.envVariables` consumption without prefix.
  - **Add** to the provision step guidance: "When writing the
    workspace `import.yaml`, include `project.envVariables` with
    `DEV_*` and `STAGE_*` URL constants derived from known hostnames.
    Use the env 0-1 shape because the workspace IS a dev-pair env."
    With a full working example.
  - **Remove** any remaining prescriptive text about `VITE_API_URL`
    or `${zeropsSubdomainHost}` in `build.envVariables` directly —
    replace with the `RUNTIME_{VAR}` indirection pattern.
- **`internal/content/workflows/bootstrap.md`**
  - **Add** to the env-variables-adjacent section: a note that
    project-level URL constants are the canonical pattern for
    cross-service URL reference, with a pointer to the recipe
    workflow's dual-runtime section for the full pattern.
- **`internal/knowledge/guides/environment-variables.md`**
  - **Add** one paragraph under "Project Variables — Auto-Inherited"
    showing the `RUNTIME_{project_var}` lift pattern for
    `build.envVariables`, because the current file only shows the
    `RUNTIME_` prefix for service-level runtime vars (line 40
    example uses a run-scope var of the same name).

### Regression guard for Fix 1

After edits, grep `internal/content/workflows/recipe.md` for:
- `setup: stage` — must return zero matches (banned; stage uses `prod`)
- `VITE_API_URL: https://` — must return zero matches (no hardcoded
  URLs in guidance; the URL must be a `${RUNTIME_STAGE_API_URL}`
  reference)
- `zeropsSubdomainHost` — must appear only inside `project.envVariables`
  value examples, never inside `build.envVariables`

---

## Fix 2 — `generate-finalize` accepts `projectEnvVariables` {#fix-2}

### First principle

`generate-finalize` is a template renderer. Everything that varies per
env but must remain consistent across the 6 env outputs — comments,
secrets, project env vars — belongs as a first-class input. v5 proved
that manually editing the output is a trap: a second call to
`generate-finalize` (triggered by any late fix) re-renders from
template and wipes hand-edits.

The current tool accepts `envComments` as a per-env map (`0..5` →
{service: {...}, project: "..."}). `projectEnvVariables` follows the
identical shape: a per-env map where each env supplies a flat
`map[string]string` of project-level env vars to bake into that env's
`project.envVariables` block.

### Shape

```jsonc
// generate-finalize workflow action input (alongside existing fields)
{
  "envComments": {
    "0": { "service": { ... }, "project": "..." },
    // ...
  },
  "projectEnvVariables": {
    "0": {
      "DEV_API_URL": "https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "DEV_FRONTEND_URL": "https://appdev-${zeropsSubdomainHost}.prg1.zerops.app",
      "STAGE_API_URL": "https://apistage-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://appstage-${zeropsSubdomainHost}.prg1.zerops.app"
    },
    "1": { /* same as 0 for dev-pair envs */ },
    "2": {
      "STAGE_API_URL": "https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app",
      "STAGE_FRONTEND_URL": "https://app-${zeropsSubdomainHost}.prg1.zerops.app"
    },
    "3": { /* same as 2 */ },
    "4": { /* same as 2 */ },
    "5": { /* same as 2 */ }
  }
}
```

The agent passes this once per recipe. Re-running `generate-finalize`
with the same input produces identical output. No hand edits.

### Merge semantics — match envComments

`UpdateRecipeComments()` in `internal/workflow/engine_recipe.go`
implements "non-empty value overwrites, empty string deletes, missing
key untouched". `projectEnvVariables` gets a parallel function
`UpdateRecipeProjectEnvVariables()` with the same semantics:

- Passing an env key with a non-empty value map → that env's project
  env vars are replaced with the new map (not merged — deterministic).
- Passing an env key with an empty map (`{}`) → that env's project
  env vars are cleared.
- Omitting an env key → that env's project env vars are left
  untouched (useful for partial re-runs like the v5 env-4 cross-env
  comment fix).
- An individual value within a map can be an empty string to delete
  just that one key (matches `envComments.service`'s per-key delete
  semantics).

### Template rendering

`writeProjectSection()` in
`internal/workflow/recipe_templates_import.go` currently renders:

```go
project:
  name: ...
  corePackage: SERIOUS   // env 5 only
  envVariables:
    APP_SECRET: <@generateRandomString(<32>)>   // only the shared secret
```

After Fix 2 it merges the shared-secret line with the env's
`projectEnvVariables` map, rendering them as YAML `key: value` lines
underneath. Key ordering: shared secrets first, then the sorted
`projectEnvVariables` keys alphabetically (deterministic for diff
stability). Values are emitted verbatim — no escaping — because
`project.envVariables` values are plain strings per schema; Zerops's
preprocessor handles `${zeropsSubdomainHost}` resolution.

If `projectEnvVariables` is empty/absent and no shared secret exists,
the `envVariables:` line is omitted entirely (no empty block).

### Concrete edits for Fix 2 — file-by-file

1. **`internal/workflow/recipe.go`** — add field to `RecipePlan`:
   ```go
   type RecipePlan struct {
       // ... existing fields ...
       EnvComments          map[string]EnvComments
       ProjectEnvVariables  map[string]map[string]string // new
   }
   ```

2. **`internal/workflow/engine_recipe.go`** — add
   `UpdateRecipeProjectEnvVariables(plan, input)` parallel to
   `UpdateRecipeComments`. Same merge rules. Validate env keys are
   `"0".."5"`; validate var names match `[a-zA-Z_][a-zA-Z0-9_]*` (the
   standard env var name regex already present in env var knowledge);
   reject duplicates after normalization.

3. **`internal/workflow/recipe_templates_import.go`** —
   `writeProjectSection()` pulls from `plan.ProjectEnvVariables[envKey]`
   and emits sorted `key: value` lines inside `envVariables:`,
   merged with the existing shared-secret line.

4. **`internal/tools/workflow.go`** — add
   `ProjectEnvVariables map[string]map[string]string` to
   `WorkflowInput`.

5. **`internal/tools/workflow_recipe.go`** — in
   `handleRecipeGenerateFinalize`, call
   `UpdateRecipeProjectEnvVariables(plan, input.ProjectEnvVariables)`
   before `BuildFinalizeOutput(plan)`.

6. **`internal/tools/workflow_recipe.go`** — JSON schema for the
   tool's action parameter: declare the new field so MCP clients see
   it. Description text should point at Fix 1's pattern.

### Tests for Fix 2

- **Unit (`workflow/engine_recipe_test.go`)**:
  - `TestUpdateRecipeProjectEnvVariables_*` — seed test for the merge
    function, same pattern as the existing comments merge tests.
    Cases: fresh insert, overwrite, empty-map clear, empty-string
    per-key delete, missing-env-key untouched, invalid env key
    rejected, invalid var name rejected, duplicate-after-case-normalize
    rejected.

- **Template (`workflow/recipe_templates_import_test.go`)**:
  - `TestWriteProjectSection_WithProjectEnvVars` — golden-output
    test; assert sorted order, shared-secret-first, no-empty-block
    when both are absent.

- **Tool (`tools/workflow_recipe_test.go` or sibling)**:
  - `TestGenerateFinalize_WithProjectEnvVariables` — end-to-end
    through `handleRecipeGenerateFinalize`, assert the 6 generated
    import.yaml files each contain the expected `project.envVariables`.

- **Finalize checker (`tools/workflow_checks_finalize_test.go`)**:
  - Add a case exercising the existing parser against output with
    dense `project.envVariables` — should pass without modification
    since the parser already supports the field.

### Regression guard for Fix 2

After implementation, re-run the v5 recipe plan (or a
`TestGenerateFinalize_Idempotent` test): call `generate-finalize`
twice with identical input, assert byte-identical output both times.
This is the property that v5 violated.

---

## Fix 3 — Provision `minRam: 1.0` for runtime dev services {#fix-3}

### First principle

A nodejs/bun/deno dev container running `npm install` for a
showcase-scale dependency tree (200-600 MB on disk, 1500+ transitive
packages) needs at least 1 GB of RAM or the OOM killer takes out the
install mid-run. v5 hit this on `appdev` (type `static` defaulted to
0.25 GB — fine for Nginx but the dual-runtime dev override runs Node).

The current template
(`internal/workflow/recipe_templates_import.go:241-289`,
`writeAutoscaling()`) emits `minRam: 0.5` for env 0-1 runtime services.
`0.5` is too low for Node dev containers; even `1.0` is tight for
large dep trees but is the defensible default.

**Static-type services with dev nodejs overrides** are the edge case:
they're classified as "utility" by type but behave as "runtime" under
the dev override. The fix is NOT to special-case static types — it's
to recognize that **any target in the recipe plan marked as a runtime
role (including `RecipeRoleApp` on a static type)** gets the runtime
scaling profile in env 0-1.

### Concrete edits for Fix 3

1. **`internal/workflow/recipe_templates_import.go`** — in
   `writeAutoscaling()`, change the env 0-1 runtime minRam from
   `0.5` to `1.0`. The existing runtime-vs-utility classification
   covers static-type runtime targets correctly (they already take
   the runtime branch when `IsRuntimeType(target.Type) || target.Role
   != ""`).

2. **`internal/content/workflows/bootstrap.md`** — the provision
   section's scaling table already mentions `1.0` for compiled
   runtimes but not for Node/PHP. Tighten the text: "env 0-1 dev
   runtime services default to `minRam: 1.0` regardless of runtime
   family — `npm install`/`composer install`/`pip install` on a
   showcase-scale dependency tree OOMs below 1 GB."

3. **`internal/content/workflows/recipe.md`** — in the dual-runtime
   guidance, mention this as a note: "Dual-runtime showcase recipes
   with a static-type frontend service need the 1 GB default for
   `appdev` because the dev override runs Node. The provision
   template handles this automatically."

### Tests for Fix 3

- Existing `TestGenerateEnvImportYAML` or equivalent template tests —
  update assertions to expect `minRam: 1` (or the exact stringified
  form the template emits) for env 0-1 runtime services.

- A regression test that specifically asserts a `type: static`
  target with `Role: app` and the dual-runtime marker also gets
  `minRam: 1` in env 0-1 (and NOT `0.25` from the utility default).

### Regression guard for Fix 3

Run the existing recipe template golden tests. They'll either have to
be updated (expected — the baseline shifts) or they already assert
`>= 1.0` in which case nothing changes. No silent regressions.

---

## Fix 4 — `initCommands` observed via logs, not assumed {#fix-4}

### First principle

Per user: `initCommands` DO run on idle-start containers. v5's
agent incorrectly concluded they don't, based on the absence of
visible dashboard data after deploy. The conclusion was wrong — the
agent never actually LOOKED at the logs to see whether
`migrate.js`/`seed.js`/`search-index.js` emitted output. It just
assumed non-execution and re-ran them manually.

The structural fix is a workflow rule: **after any deploy that has
`initCommands`, the agent MUST pull logs for the target service and
assert the commands emitted their expected output (or an error).** No
assumption; no re-run without evidence.

### Concrete edits for Fix 4

1. **`internal/content/workflows/recipe.md`** — in Step 4
   (first dev deploy) and in Step 6 (stage cross-deploy), after the
   `zerops_deploy` call:
   ```
   After deploy returns ACTIVE, pull logs for the target service with
   `zerops_logs serviceHostname="{hostname}" limit=100 since=5m` and
   verify each `initCommands` entry produced output (success line,
   row count, or error). If logs show the command ran and failed,
   fix the code and redeploy — do NOT re-run the command manually
   via SSH. If logs show the command did not run at all, investigate
   the `zsc execOnce` gate — do NOT work around by running manually.
   Manual re-execution hides platform-level bugs and ensures the
   recipe is broken for end users who never see your manual fix.
   ```

2. **`internal/content/workflows/recipe.md`** — in the
   "Common deployment issues" table, add a row:
   ```
   | Dashboard shows empty state after deploy | Assumed initCommands
   didn't fire; did not check logs | `zerops_logs
   serviceHostname={hostname} since=5m` — verify the migrate/seed/index
   output is present. If missing, investigate (don't re-run manually). |
   ```

### Regression guard for Fix 4

Guidance-only edit; no code change. Verification is next recipe
run — if the agent ever says "initCommands didn't run, I ran them
manually" again without log evidence, the guidance failed and we
iterate.

---

## Fix 5 — Close-step verification split {#fix-5}

### First principle

The v8.50.3 design packed static code review + browser walkthrough
into ONE verification sub-agent. v5 proved this is fragile: the
sub-agent completed static review, started the browser walk, died
mid-walk from fork exhaustion, and the recipe presumed close because
static review was already done. The CORS regression slipped through.

The root cause is scope coupling. Static code review is
capability-bounded (the sub-agent has framework expertise). Browser
walk is state-bounded (the process budget of the container). Coupling
them means a state failure kills capability work already done and
presumed-complete.

**Split into two phases**, each owned by the right actor:

1. **Phase 1 — Static code review sub-agent** — runs framework-expert
   review, returns findings + diffs, exits. Scope: everything the
   current brief covers EXCEPT the browser walk. No agent-browser
   invocations inside this phase.
2. **Phase 2 — Main-agent browser walk** — after Phase 1 completes,
   the main agent (single-threaded against the fork budget) runs
   `agent-browser` itself against appdev and appstage. If it dies,
   `close` fails with "browser verification incomplete" — retry
   explicitly, don't auto-proceed.

This also fixes the "sub-agent spawns agent-browser from inside
a Task" pattern which was concurrently competing with whatever the
parent chat was doing for fork budget.

### Concrete edits for Fix 5

1. **`internal/content/workflows/recipe.md`** — in the "Verification
   Sub-Agent" section (current line ~1079):
   - Rename the section to "**1a. Static Code Review Sub-Agent**"
     to make the split explicit.
   - **Delete the entire "Browser walkthrough" block** from the
     sub-agent brief (currently lines ~1102-1123).
   - Add a closing sentence to the sub-agent brief: "Do NOT open
     agent-browser. Browser verification is a separate phase run by
     the main agent after this review completes. If you encounter
     something that would require a browser to diagnose, report it as
     a `[SYMPTOM]` and stop."
   - Add a new section **"1b. Main Agent Browser Walk"** immediately
     after 1a, containing the moved browser walkthrough content +
     Fix 6's agent-browser lifecycle rules (stop dev processes,
     batch mode, close).
   - The "Symptom reporting" and "Out of scope" blocks stay inside
     the sub-agent brief — those are code-review scope rules.

2. **`internal/content/workflows/recipe.md`** — in the same close
   section's preamble, update the close-step overview: "Close runs
   in three sub-phases: (1a) static code review sub-agent, (1b) main
   agent browser walk, (2) export/publish only if the user asked.
   Each sub-phase must complete before the next starts."

### Regression guard for Fix 5

Grep `recipe.md` after the edit for:
- `Use .agent-browser. to open` inside a `>` blockquote (the
  sub-agent brief) — must return zero matches
- "Main Agent Browser Walk" as a section header — must return one match

---

## Fix 6 — agent-browser lifecycle via batch mode + stop-dev-processes {#fix-6}

### First principle

v8.50.3's "always close when done" rule didn't prevent two fork-budget
crashes in v5. The rule was necessary but not sufficient. The missing
constraints:

1. **Opening agent-browser when dev processes (npm run start:dev,
   ts-node worker, nohup jobs) are running exceeds the zcp container's
   fork budget.** Chrome's ~10 helpers + the agent's background tree
   + the parent chat's subprocess tree together can peak above the
   per-user limit.
2. **Commands issued individually, one per Bash call, spend the
   daemon round-trip on every step.** A verification sequence of
   10 commands is 10 process starts on the zcp side even though the
   daemon is persistent.

The structural rule is: **use `agent-browser batch` exclusively for
verification, with explicit `open` at the start and `close` at the
end of every batch, and stop all background dev processes before
running the batch.**

### The pattern

```
# 1. Stop all background dev processes on the target containers.
ssh apidev "pkill -f 'nest start' || true; pkill -f 'ts-node' || true"
ssh apidev "pkill -f 'node dist/worker' || true"
# (repeat for every dev container that has background tasks.)

# 2. Run the verification as ONE agent-browser batch call.
echo '[
  ["open", "https://appstage-<sub>.prg1.zerops.app"],
  ["errors", "--clear"],
  ["console", "--clear"],
  ["snapshot", "-i", "-c"],
  ["get", "text", "[data-connectivity]"],
  ["get", "count", "[data-article-row]"],
  ["find", "role", "button", "Submit", "click"],
  ["get", "text", "[data-result]"],
  ["errors"],
  ["console"],
  ["close"]
]' | agent-browser batch --json

# 3. Repeat the batch for appdev (if dev verification is needed).

# 4. If further dev iteration is needed, restart background processes
#    on the target containers AFTER the batch completes.
```

Key properties:
- `open` and `close` are inside the batch — no leaked daemons.
- One `batch` call = one Bash call = minimal fork churn on zcp.
- Dev processes are stopped before the browser runs, so fork budget
  has headroom for Chrome + helpers.
- Restarting dev processes is an explicit step, not implicit.

### Concrete edits for Fix 6

1. **`internal/content/workflows/recipe.md`** — in Step 4c (existing
   browser verification) and in the new "1b. Main Agent Browser Walk"
   section from Fix 5:
   - Delete the current multi-step `open → get → click → close`
     canonical flow (one Bash call per command).
   - Replace with the canonical batch-mode flow above.
   - Prepend an explicit "stop background dev processes" step with
     the exact pkill commands.
   - Append the "restart if continuing to iterate" step.
   - Keep the hygiene rules (close inside batch, one session at a
     time, pkill recovery recipe) but reframe them as derived from
     the batch pattern rather than standalone rules.

2. **Update the process-hygiene table** to reflect that `open` and
   `close` are batch elements, not standalone calls.

### Regression guard for Fix 6

Grep `recipe.md` after edit:
- `agent-browser open` outside a JSON batch example — must return
  zero matches in the verification sections
- `agent-browser close` as a standalone Bash example — must return
  zero matches in the verification sections (only inside batch
  examples)

---

## Fix 7 — MCP wrapper around agent-browser (scoped, deferred) {#fix-7}

### Scope

Wrapping agent-browser as an MCP tool is tractable but non-trivial.
The cleanest design:

- New MCP tool: `zerops_browser_batch` (or just `browser_batch`)
- Single input: a JSON array of commands (same shape as
  `agent-browser batch` stdin)
- Implementation: spawns `agent-browser` with a unique session
  name, prepends an `open` if the first command isn't one, appends
  `close` unconditionally, captures the combined output, returns it
- Timeout + SIGKILL fallback: if the batch exceeds a bounded wall
  clock (60s default), force-kill the session and its chrome tree
- Serialization: the MCP server is already sequential per connection;
  the tool adds no additional concurrency

### Why this is better than the guidance-only fix

- **Lifecycle enforced at the tool level**, not by agent discipline.
  An agent that forgets `close` can't — the tool always closes.
- **Leaked processes impossible**: the tool owns the session name
  and force-kills on exit.
- **Timeout enforcement**: a hung Chrome doesn't hang the agent;
  the tool times out and returns an error.
- **Single contract**: `commands: [[...], [...], ...]`. No shell
  concerns, no JSON-in-bash escaping.

### Why it's deferred this session

- Non-trivial code: tool registration, shell exec with timeout, SIGKILL
  tree handling, output multiplexing, timeout configuration, tests.
- Fix 6's guidance-based pattern gets most of the value for zero code
  risk right now.
- The MCP wrapper should be a focused follow-up: one PR, one release,
  easy to review in isolation.

### Concrete plan (for the follow-up)

- New package: `internal/browser/` with `Exec(commands, opts)` API.
- New MCP tool: `internal/tools/browser.go` registering
  `zerops_browser_batch`.
- Unit tests: mock `agent-browser` via executable stub in `PATH`.
- Integration test: skippable via `-short` or build tag if
  `agent-browser` binary is absent.
- Guidance: once shipped, rewrite Fix 6's canonical flow to call
  the MCP tool instead of raw `agent-browser batch`.

---

## Implementation order and blast radius {#order}

Order is dependency-respecting. Within each phase, work is scoped to
leave the codebase green.

1. **Fix 2 first** — `generate-finalize` code change + tests. This
   adds capability the later guidance assumes. Code-only; no
   user-facing behavior change until the guidance in Fix 1 says to
   use it. Pure RED/GREEN cycle.

2. **Fix 3 second** — provision scaling default. Single-line code
   change + test update. Independent of the other fixes; ships at
   the same time as Fix 2 to minimize release churn.

3. **Fix 1 third** — the URL env-var pattern guidance. References
   the new `projectEnvVariables` capability from Fix 2 in its
   "finalize step" edits, so Fix 2 must be shipped first.

4. **Fix 4 fourth** — initCommands log-verification guidance. Single
   paragraph added in two places. Pure guidance.

5. **Fix 5 fifth** — close-step verification split. Guidance
   restructure in recipe.md. Depends on Fix 6's batch-mode flow
   being defined so the split's "Phase 1b" can reference it.

6. **Fix 6 sixth** — agent-browser batch-mode canonical flow. Guidance
   restructure in recipe.md. Depends on Fix 5's split to know
   where the flow lives.

7. **Fix 7 deferred** — MCP wrapper, scoped here, implemented
   in a follow-up PR/release.

Fixes 2 + 3 ship in one commit (Go code); Fixes 1 + 4 + 5 + 6 ship in
one commit (pure guidance, all in `recipe.md`/`bootstrap.md`).
Release cadence: one patch for code, one patch for guidance. Fix 7
becomes its own minor/patch later.

## Out of scope

- Changing the name of `zeropsSubdomainHost` or the URL format
- Introducing any new zerops.yaml setup name other than `dev`,
  `prod`, `worker`
- Auto-publishing v5 to `zeropsio/recipes` — that's a separate
  workflow once v6 is green
- The agent-browser MCP wrapper implementation (deferred per Fix 7)
- Adjustments to `zerops_env` tool behavior (workspace env vars are
  set via the existing `zerops_import` project.envVariables path,
  not via `zerops_env project=true` — that tool path stays as-is for
  runtime edits)
