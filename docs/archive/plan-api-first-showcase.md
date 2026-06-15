# Plan: API-First Showcase Architecture

## Problem

Showcase recipes for API-first frameworks (NestJS, Express, Fastify, Go, Rust, FastAPI, Spring Boot) force server-rendered HTML dashboards onto frameworks designed to serve JSON APIs. This causes:

1. **Template engine friction**: NestJS+Handlebars cost 3 iteration rounds in v1 (CJS/ESM mismatch, async registration, naming conventions)
2. **Anti-pattern teaching**: Users see NestJS rendering HTML — nobody does this in production
3. **Anti-Zerops**: Bundling everything in one service hides the platform's multi-service orchestration value

## Solution

Showcase recipes for API-first frameworks split into **two runtime services**:

- **`app`** — lightweight static frontend (Svelte SPA on `static` runtime)
- **`api`** — JSON API backend (the framework being showcased)
- **`worker`** — background processor (shared codebase with `api`)

The frontend calls the API via HTTP. Both are separate Zerops services with separate repos.

## Design Principles

1. **Framework-agnostic**: The decision tree uses structural properties ("has built-in templating?"), never framework names
2. **No new abstractions where predicates suffice**: Template dispatch stays on `IsRuntimeType`/`IsManagedService`/`IsWorker` — the new `Role` field is for repo routing and comment generation only
3. **Backward compatible**: `Role` defaults to empty string; all existing recipes work unchanged
4. **Platform-native**: Uses Zerops `priority` ordering, `zeropsSubdomainHost` for cross-service URL construction, and separate `buildFromGit` repos

## Decision Tree

```
Is the framework full-stack (has built-in view/template engine)?
│
├─ YES: Laravel/Blade, Rails/ERB, Django/Jinja2, Phoenix/HEEx, ASP.NET/Razor
│  └─ Single-app showcase (current model)
│     Targets: app + worker + managed services
│     Dashboard: framework's built-in templates
│     Repos: {slug}-app
│
└─ NO: NestJS, Express, Fastify, Hono, Go/Chi, Go/Fiber, Rust/Actix,
│      Rust/Axum, FastAPI, Flask (API mode), Spring Boot (API mode)
│
   └─ Dual-runtime showcase (new model)
      Targets: app (frontend) + api (backend) + worker + managed services
      Dashboard: Svelte SPA calling JSON API
      Repos: {slug}-app (frontend), {slug}-api (backend)
```

**Classification rule**: If the predecessor hello-world/minimal recipe renders HTML via a framework-integrated template engine (not a bolted-on third-party engine), it's full-stack. If the predecessor only returns JSON or plain text, it's API-first.

**Frontend choice**: The lightest static-output framework the agent has recipe knowledge for. Currently **Svelte (Vite)** — 2 build commands (`npm ci && npm run build`), deploys on `static` runtime (pure Nginx), smallest bundle size, HTML-superset syntax well-known to LLMs.

## Architecture

### Service Topology

```
app (static)  ──fetch──▶  api (nodejs@22)  ──▶  db, redis, storage, search
                           worker (nodejs@22) ─▶  redis (BullMQ)
```

### Import.yaml Priority Chain

```
priority 10:  db, redis, storage, search    (managed services — must accept connections first)
priority 5:   api                           (must be running before frontend builds)
priority 1:   app, worker                   (default — frontend builds last, worker starts last)
```

### Cross-Service URL Construction

The frontend needs the API's public URL at build time. Zerops build containers are isolated (no cross-service refs), but `zeropsSubdomainHost` is project-level:

```yaml
# Frontend zerops.yaml — build.envVariables
VITE_API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app
```

Components: `api` (hostname, defined by us) + `zeropsSubdomainHost` (project-level, dynamic) + `3000` (port, defined in API's zerops.yaml). All known at build time.

For env 0-1 (dev/stage pairs), the frontend's `setup: dev` uses `apidev-...` and `setup: prod` uses the env-appropriate API hostname.

### Hostname Conventions

| Role | Env 0-1 | Env 2-5 | buildFromGit | zeropsSetup |
|------|---------|---------|-------------|-------------|
| Frontend | `appdev` + `appstage` | `app` | `{slug}-app` | dev / prod |
| API backend | `apidev` + `apistage` | `api` | `{slug}-api` | dev / prod |
| Worker (shared w/ API) | `workerstage` only | `worker` | `{slug}-api` | worker |
| Managed services | `db`, `redis`, etc. | same | n/a | n/a |

### zerops.yaml Split

**`{slug}-app` repo** (Svelte frontend):
```yaml
zerops:
  - setup: prod
    build:
      base: nodejs@22
      envVariables:
        VITE_API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app
      buildCommands:
        - npm ci
        - npm run build
      deployFiles:
        - dist/~
    run:
      base: static

  - setup: dev
    build:
      base: nodejs@22
      envVariables:
        VITE_API_URL: https://apidev-${zeropsSubdomainHost}-3000.prg1.zerops.app
      buildCommands:
        - npm install
      deployFiles: ./
    run:
      base: nodejs@22
      os: ubuntu
      start: zsc noop --silent
```

**`{slug}-api` repo** (NestJS/Express/Go/etc. + worker):
```yaml
zerops:
  - setup: prod
    build: ...
    deploy:
      readinessCheck:
        httpGet:
          port: 3000
          path: /api/health
    run:
      start: node dist/main.js
      healthCheck:
        httpGet:
          port: 3000
          path: /api/health
      envVariables:
        DB_NAME: ${db_dbName}
        REDIS_HOST: ${redis_hostname}
        # ... all service refs

  - setup: worker
    build: ...
    run:
      start: node dist/worker.js
      # No ports, no healthCheck

  - setup: dev
    build: ...
    run:
      start: zsc noop --silent
```

---

## Code Changes

### Phase 1: RecipeTarget Role Field

**File: `internal/workflow/recipe.go`**

Add `Role` field to `RecipeTarget` struct (~line 76):

```go
type RecipeTarget struct {
    Hostname     string   `json:"hostname" ...`
    Type         string   `json:"type" ...`
    IsWorker     bool     `json:"isWorker,omitempty" ...`
    Role         string   `json:"role,omitempty" jsonschema:"description=Service role for repo routing: 'app' (frontend/default), 'api' (backend API), 'worker' (background processor). Empty for managed/utility services. Does NOT affect template dispatch — type predicates remain authoritative."`
    Environments []string `json:"environments,omitempty"`
}
```

Role values: `"app"` (frontend, default for non-worker runtimes), `"api"` (backend API), `"worker"` (set automatically when `IsWorker=true`), `""` (managed/utility).

Update comment block above struct (line 73-75) to acknowledge Role field.

**Backward compat**: `Role` is `omitempty` — existing plans with no Role work unchanged. All dispatch logic stays on type predicates.

**Tests**: No existing tests break (audit confirmed — no struct equality assertions on RecipeTarget).

### Phase 2: buildFromGit Routing

**File: `internal/workflow/recipe_service_types.go`**

Update `writeRuntimeBuildFromGit()` / `runtimeRepoSuffix()` (post-Phase A implementation):

```go
func runtimeRepoSuffix(plan *RecipePlan, target RecipeTarget) string {
    switch {
    case target.IsWorker && target.SharesCodebaseWith == "":
        return "-worker"
    case target.IsWorker && target.SharesCodebaseWith != "":
        // Shared worker: inherit host target's suffix.
        host := findTarget(plan, target.SharesCodebaseWith)
        if host != nil && host.Role == RecipeRoleAPI {
            return "-api"
        }
        return "-app"
    case target.Role == RecipeRoleAPI:
        return "-api"
    default:
        return "-app"
    }
}
```

Logic: Separate-codebase worker → `-worker`. Shared worker inherits its host
target's role suffix. Role=api → `-api`. Everything else (frontend, single
app) → `-app`. Implementation uses the explicit `SharesCodebaseWith` field
introduced in v8.50.7, not the old runtime-match heuristic in
`SharesAppCodebase(target, plan)`.

**Tests**: Add `TestWriteRuntimeBuildFromGit_APIRole` — verify `{slug}-api` URL emitted for targets with `Role: "api"`.

### Phase 3: Priority for API Services

**File: `internal/workflow/recipe_templates_import.go`**

Update `writeSingleService()` (~line 155) to assign priority 5 for API services:

```go
// Priority: managed services start before runtimes. API services start before frontends.
if !IsRuntimeType(target.Type) {
    b.WriteString("    priority: 10\n")
} else if target.Role == "api" {
    b.WriteString("    priority: 5\n")
}
```

Same logic in `writeDevService()` and `writeStageService()` where applicable for env 0-1.

**Tests**: Add `TestGenerateEnvImportYAML_APIServicePriority` — verify `priority: 5` appears for API targets.

### Phase 4: Explicit SharesCodebaseWith Field (SUPERSEDED by v8.50.7)

**File: `internal/workflow/recipe.go` + `recipe_service_types.go`**

The original plan compared worker runtime against `plan.RuntimeType` as a
heuristic for "shared codebase". v8.50.7 replaced that heuristic with an
explicit `SharesCodebaseWith` field on `RecipeTarget` naming the host
target by hostname. This made the 3-repo case (frontend + API + separate-
repo worker) expressible and flipped the default from "shared if same
runtime" to "separate codebase (opt-in to share)".

Current signature:

```go
// Returns true iff the worker explicitly declared SharesCodebaseWith.
func SharesAppCodebase(target RecipeTarget) bool {
    return target.IsWorker && target.SharesCodebaseWith != ""
}
```

Validation (`validateWorkerCodebaseRefs` in `recipe_validate.go`) enforces:
hostname exists, non-worker host, worker type is runtime, host type is
runtime, and base-runtime parity. Template helpers trust the validation
result and never re-check.

### Phase 5: buildServiceIncludesList Fix

**File: `internal/workflow/recipe_templates.go`**

`buildServiceIncludesList()` (~line 329) only describes the first runtime target. Fix to iterate all:

```go
func buildServiceIncludesList(plan *RecipePlan, envIndex int) string {
    var parts []string
    for _, target := range plan.Targets {
        if IsRuntimeType(target.Type) {
            if envIndex <= 1 {
                label := target.Hostname
                parts = append(parts,
                    fmt.Sprintf("a %s dev service with the code repository and necessary development tools", label),
                    fmt.Sprintf("a %s staging service", label),
                )
            }
        } else {
            parts = append(parts, dataServiceIncludesLabel(target.Type))
        }
    }
    if len(parts) == 0 {
        return ""
    }
    return "It includes " + naturalJoin(parts) + "."
}
```

**Tests**: Update `TestBuildServiceIncludesList` (if exists) to verify multi-runtime output.

### Phase 6: Generate Check — Multiple App Targets

**File: `internal/tools/workflow_checks_recipe.go`**

`checkRecipeGenerate()` (~line 68) breaks on first non-worker runtime. Fix to check all:

```go
// Collect ALL non-worker runtime targets (frontend + API in dual-runtime recipes).
var appTargets []workflow.RecipeTarget
for _, t := range plan.Targets {
    if topology.IsRuntimeType(t.Type) && !t.IsWorker {
        appTargets = append(appTargets, t)
    }
}
if len(appTargets) == 0 && len(plan.Targets) > 0 {
    appTargets = []workflow.RecipeTarget{plan.Targets[0]}
}

// Check zerops.yaml for each app target.
for _, appTarget := range appTargets {
    hostname := appTarget.Hostname
    ymlDir := projectRoot
    for _, candidate := range []string{hostname + "dev", hostname} {
        mountPath := filepath.Join(projectRoot, candidate)
        if info, err := os.Stat(mountPath); err == nil && info.IsDir() {
            ymlDir = mountPath
            break
        }
    }
    doc, parseErr := ops.ParseZeropsYml(ymlDir)
    if parseErr != nil {
        checks = append(checks, workflow.StepCheck{
            Name: hostname + "_zerops_yml_exists", Status: statusFail,
            Detail: fmt.Sprintf("zerops.yaml not found for %s: %v", hostname, parseErr),
        })
        continue
    }
    checks = append(checks, workflow.StepCheck{
        Name: hostname + "_zerops_yml_exists", Status: statusPass,
    })
    checks = append(checks, checkRecipeSetups(doc, hostname, plan)...)
    checks = append(checks, checkZeropsYmlFields(ymlDir, validFields)...)
}
```

Also update `checkRecipeSetups()` to handle per-target setup validation — each target needs its own dev+prod entries in its zerops.yaml.

**Tests**: Add `TestCheckRecipeGenerate_DualRuntime` — create temp dirs for both `app` and `api` with separate zerops.yaml files.

### Phase 7: README Overlay for Multiple Runtimes

**File: `internal/workflow/recipe_overlay.go`**

`mountREADMEPathForPlan()` (~line 48) returns first non-worker runtime's README. For dual-runtime recipes, the API service's README is the canonical one (it documents the framework being showcased):

```go
func mountREADMEPathForPlan(plan *RecipePlan) string {
    base := recipeMountBase
    // Prefer API service README (documents the showcased framework).
    // Fall back to first non-worker runtime.
    for _, t := range plan.Targets {
        if IsRuntimeType(t.Type) && !t.IsWorker && t.Role == "api" {
            return filepath.Join(base, t.Hostname+"dev", "README.md")
        }
    }
    for _, t := range plan.Targets {
        if IsRuntimeType(t.Type) && !t.IsWorker {
            return filepath.Join(base, t.Hostname+"dev", "README.md")
        }
    }
    return ""
}
```

**Tests**: Add `TestOverlayRealAppREADME_PrefersAPITarget`.

### Phase 8: Validation — Allow Multiple Non-Worker Runtimes

**File: `internal/workflow/recipe_validate.go`**

`validateShowcaseServices()` (~line 164) already uses a boolean `hasApp` which correctly allows 2+ non-worker runtimes. **No logic change needed.** Update the comment to clarify:

```go
// hasApp is true if at least one non-worker runtime exists.
// Dual-runtime showcases (frontend + API) have two — this is valid.
```

**Optional**: Add validation that dual-runtime recipes have exactly one `Role: "api"` and one `Role: "app"` target (not two APIs, not two frontends).

---

## Guidance Changes (recipe.md)

### Addition 1: Decision Tree (after line 16, before "Reference Loading")

Add framework classification section:

```markdown
### Full-Stack vs API-First Classification

Before loading predecessors, classify the framework:

**Full-stack** (has built-in view/template engine): The framework renders HTML
directly. Dashboard uses the built-in engine. Single `app` service.
Examples: Laravel/Blade, Rails/ERB, Django/Jinja2, Phoenix/HEEx.

**API-first** (no built-in templating): The framework serves JSON. Dashboard
is a lightweight Svelte SPA in a separate `app` service that calls the API.
The API is a separate `api` service. Worker shares codebase with `api`.

Classification rule: if the predecessor hello-world/minimal recipe renders HTML
via a framework-integrated template engine, it is full-stack. If the predecessor
only returns JSON or plain text, it is API-first.
```

### Addition 2: Showcase Targets (update lines 112-119)

Expand the showcase targets table:

```markdown
**Full-stack showcase targets**: app, worker, db, cache, storage, search
**API-first showcase targets**: app (frontend, static), api (backend), worker, db, cache, storage, search

For API-first recipes, set `role: "api"` on the backend target and
`role: "app"` on the frontend target. The worker target gets `isWorker: true`
as before — it shares the API's codebase (same runtime).
```

### Addition 3: Generate — Dual-Runtime File Locations (update lines 236-240)

```markdown
**Single-runtime**: Write all files to `/var/www/appdev/`.
**Dual-runtime (API-first showcase)**: Write API code to `/var/www/apidev/`
and frontend code to `/var/www/appdev/`. Each has its own zerops.yaml,
package.json, and source tree. The API's README.md contains the integration
guide (it documents the showcased framework).
```

### Addition 4: Generate — zerops.yaml for Dual-Runtime (update lines 276-310)

```markdown
**Dual-runtime zerops.yaml**: Each runtime service has its own zerops.yaml
in its own codebase root:
- `/var/www/apidev/zerops.yaml` — 3 setups: dev, prod, worker
- `/var/www/appdev/zerops.yaml` — 2 setups: dev, prod

The frontend's `build.envVariables` constructs the API URL from known parts:
```
VITE_API_URL: https://api-${zeropsSubdomainHost}-3000.prg1.zerops.app
```
Components: hostname (`api`, defined in import.yaml) + `zeropsSubdomainHost`
(project-level, resolved at build time) + port (from API's `run.ports`).
The dev setup uses the dev hostname: `apidev-${zeropsSubdomainHost}-3000...`.
```

### Addition 5: Deploy — Dual-Runtime Flow (update lines 598-716)

```markdown
**Dual-runtime deploy order** (API-first showcase):

1. Deploy `apidev` (setup=dev) — API must be running first
2. Start API server on apidev via SSH
3. Enable API subdomain, verify /api/health
4. Deploy `appdev` (setup=dev) — frontend builds with API URL baked in
5. Start frontend dev server on appdev via SSH
6. Enable frontend subdomain, verify dashboard loads + calls API
7. Dispatch sub-agent for feature implementation (works on BOTH services)
8. Redeploy both services after feature implementation
9. Cross-deploy apidev → apistage (setup=prod)
10. Cross-deploy apidev → workerstage (setup=worker)
11. Cross-deploy appdev → appstage (setup=prod)
12. Verify stage: frontend loads, API responds, worker processes jobs
```

### Addition 6: Finalize — envComments Keys (update lines 779-851)

Add dual-runtime example:

```markdown
**API-first showcase service keys**:
- Env 0: appdev, apidev, workerstage, db, redis, storage, search
- Env 1: appdev, appstage, apidev, apistage, workerstage, db, redis, storage, search
- Env 2-5: app, api, worker, db, redis, storage, search
```

---

## Test Plan

### New Tests Required

| File | Test Name | What It Verifies |
|------|-----------|-----------------|
| `recipe_service_types_test.go` | `TestWriteRuntimeBuildFromGit_APIRole` | `{slug}-api` URL for Role="api" targets |
| `recipe_templates_test.go` | `TestGenerateEnvImportYAML_DualRuntime` | Both app and api services appear in all 6 envs |
| `recipe_templates_test.go` | `TestGenerateEnvImportYAML_APIPriority` | API gets `priority: 5` in envs 2-5 |
| `recipe_templates_test.go` | `TestBuildServiceIncludesList_MultiRuntime` | Description mentions both frontend and API |
| `recipe_validate_test.go` | `TestValidateRecipePlan_DualRuntimeShowcase` | Two non-worker runtimes pass validation |
| `workflow_checks_recipe_test.go` | `TestCheckRecipeGenerate_DualRuntime` | Both services' zerops.yaml checked |
| `recipe_overlay_test.go` | `TestOverlayRealAppREADME_PrefersAPITarget` | API README overlaid, not frontend |
| `workflow_checks_finalize_test.go` | `TestCheckRecipeFinalize_DualRuntime` | Finalize checks pass with 2 runtime services |

### Existing Tests — Impact Assessment

All 113 existing tests remain green. Confirmed by audit:
- No struct equality assertions on RecipeTarget
- All assertions are on generated YAML content (string matching)
- `Role` field is `omitempty` — zero value doesn't appear in existing test plans

---

## Consequences Audit (Triple Verification)

### Consequence 1: plan.RuntimeType Semantics
- **Current**: Scalar field representing "the" app's runtime
- **Dual-runtime**: Represents the API's runtime (the framework being showcased)
- **Worker comparison**: Post-v8.50.7 — no longer uses `plan.RuntimeType` for
  shared-codebase detection. `SharesAppCodebase(target)` reads the explicit
  `SharesCodebaseWith` field off the target. `plan.RuntimeType` remains the
  API's runtime for non-worker purposes (README copy, repo suffix routing).
- **Verified**: No code path uses `plan.RuntimeType` to find the frontend
- **Risk**: LOW — the explicit field eliminates the "same runtime ⇒ shared"
  ambiguity entirely

### Consequence 2: Recipe Publishing
- **Current**: `sync recipe publish` creates one `{slug}-app` repo
- **Dual-runtime**: Needs TWO repos: `{slug}-app` + `{slug}-api`
- **File**: `internal/sync/push_recipes.go` — must iterate output directories per role
- **Risk**: MEDIUM — publish flow needs updating
- **Mitigation**: Phase 2 of implementation; single-app recipes unaffected

### Consequence 3: Recipe Knowledge
- **Current**: One knowledge file per recipe (`nestjs-showcase.md`)
- **Dual-runtime**: Still one knowledge file — it documents the API framework. Frontend is generic (Svelte) and doesn't need recipe-specific knowledge
- **Risk**: LOW

### Consequence 4: Eval/Headless Recipe Creation
- **File**: `internal/eval/recipe_create.go`
- **Current**: Creates one project, deploys one app
- **Dual-runtime**: Must create project with both services, deploy both
- **Risk**: MEDIUM — eval flow needs dual-deploy support
- **Mitigation**: Phase 3 of implementation

### Consequence 5: CORS
- **Frontend and API on different subdomains**: API must set CORS headers
- **Risk**: LOW — standard Express/NestJS CORS middleware, well-known to agents
- **Mitigation**: Add to recipe.md guidance: "API-first backends must enable CORS for the frontend subdomain"

### Consequence 6: Existing Single-App Showcase Recipes
- **Laravel, Rails, Django showcases**: Unchanged — `Role` field absent, all logic falls through to default
- **Risk**: ZERO — backward compatible by design

### Consequence 7: Agent Workload
- **Current**: Agent builds 1 app + 1 worker
- **Dual-runtime**: Agent builds 2 apps + 1 worker (frontend + API + worker)
- **Risk**: MEDIUM — more work per recipe build, longer sessions
- **Mitigation**: Frontend is lightweight (Svelte scaffold is minimal), API is what the agent already builds. Net new work is ~30% (frontend scaffold + fetch calls)

### Consequence 8: Sub-Agent Dispatch
- **Current**: One sub-agent implements all 5 feature sections in one app
- **Dual-runtime**: Sub-agent works on BOTH codebases — API endpoints in `/var/www/apidev/`, Svelte components in `/var/www/appdev/`
- **Risk**: MEDIUM — sub-agent brief must include both file paths
- **Mitigation**: Update sub-agent brief template in recipe.md to include both mount paths

---

## Implementation Phases

### Phase A: Core Type + Template Changes (this PR)
- Add `Role` field to RecipeTarget
- Update `writeRuntimeBuildFromGit()` for `-api` suffix
- Add priority 5 for API services
- Fix `buildServiceIncludesList()` for multi-runtime
- Fix `checkRecipeGenerate()` for multi-target
- Update `mountREADMEPathForPlan()` to prefer API
- Write all new tests
- **Files**: 7 Go files + tests
- **Risk**: Low — backward compatible, all existing tests pass

### Phase B: Recipe.md Guidance
- Decision tree
- Dual-runtime generate/deploy/finalize guidance
- Cross-service URL construction pattern
- CORS requirement
- Sub-agent brief for dual-runtime
- **Files**: 1 markdown file
- **Risk**: Low — guidance only, no code behavior change

### Phase C: Sync/Publish Support
- Update `sync recipe publish` for dual-repo output
- Update `sync recipe create-repo` to create both repos
- **Files**: 2-3 Go files in `internal/sync/`
- **Risk**: Medium — affects publishing workflow

### Phase D: Eval Support
- Update headless recipe creation for dual-deploy
- **Files**: 1-2 Go files in `internal/eval/`
- **Risk**: Medium — affects automated recipe builds

---

## Verification Checklist

After each phase:
- [ ] `go build ./...` compiles
- [ ] `go test ./... -count=1 -short` all pass
- [ ] `make lint-local` zero issues
- [ ] Existing single-app recipe knowledge still works (run laravel-showcase through eval)
- [ ] New dual-runtime test plan passes all 6 environment tiers
- [ ] Generated import.yaml has correct priority ordering
- [ ] Generated import.yaml has correct buildFromGit URLs per role
- [ ] Comment ratios >= 30% in all generated files
