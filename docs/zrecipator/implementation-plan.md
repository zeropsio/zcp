# Zrecipator Implementation Plan

Opus-optimized plan for automated recipe creation via ZCP workflow.
Derived from `recipe-via-zcp-analysis.md` + full codebase audit.

---

## Executive Summary

Add a `recipe` workflow type to ZCP — 6 orchestrated steps that replace the 2200-line meta-prompt with ~400 lines of guidance markdown + ~1200 lines of Go. The LLM uses existing ZCP tools (discover, import, deploy, knowledge) guided by recipe-specific step checkers and knowledge injection. 60% of meta-prompt content is already in ZCP's knowledge store; 25% becomes new guidance content; 15% becomes Go code (decision trees, template generation, hard checkers).

**Two paths, sequential**:
- **Path A** (this plan): Recipe workflow — LLM creates recipes interactively via MCP
- **Path B** (future): `zcp eval create` — headless automation wrapping Path A

---

## Architecture Decision Record

| # | Decision | Choice | Rationale |
|---|----------|--------|-----------|
| 1 | Engine model | Extend existing `Engine` with recipe methods | Engine pattern is sound; `BootstrapStart`/`DeployStart` pattern scales to a third workflow. Avoids parallel abstraction. |
| 2 | State model | `RecipeState` field on `WorkflowState` (like `Bootstrap`, `Deploy`) | Consistent persistence, session management, registry integration for free. |
| 3 | Step reuse | PROVISION/DEPLOY steps delegate to bootstrap step logic; RESEARCH/FINALIZE are new | Avoids duplicating service creation and deploy verification. Step checkers compose. |
| 4 | Recipe file output | App code → SSHFS mount (like bootstrap). Recipe repo files → local output dir (configurable). | App code needs deploy. Recipe structure is deliverable metadata, not deployable. |
| 5 | Template generation | ZCP generates import.yaml skeletons + README boilerplate from `RecipePlan` in Go | Env 3-5 READMEs and import.yaml structure are 100% formulaic. LLM adds comments only. |
| 6 | Comment quality | Guidance teaches conventions + hard checker validates ratio + keyword coverage | Neither alone is sufficient. Guidance for quality, checkers for minimum bar. |
| 7 | Research submission | LLM fills structured form, submits via `zerops_workflow action="complete" step="research"` | Framework-specific decisions require LLM judgment (build commands, migration system, library choices). ZCP validates structure, not content. |
| 8 | Naming convention | `recipe` workflow (not `recipator`/`zrecipator`) | Consistent with `bootstrap`/`deploy` naming. CLI can alias. |

---

## Workflow: 6 Steps

```
┌──────────┐   ┌───────────┐   ┌──────────┐   ┌────────┐   ┌──────────┐   ┌───────┐
│ RESEARCH │──▶│ PROVISION │──▶│ GENERATE │──▶│ DEPLOY │──▶│ FINALIZE │──▶│ CLOSE │
│   (new)  │   │  (reuse)  │   │ (reuse+) │   │(reuse) │   │  (new)   │   │(reuse)│
└──────────┘   └───────────┘   └──────────┘   └────────┘   └──────────┘   └───────┘
  fixed          fixed           creative      branching      creative      fixed
```

| Step | Category | Skippable | Hard checker |
|------|----------|-----------|--------------|
| research | fixed | No | All research fields filled, types validated against live catalog, decision branches recorded |
| provision | fixed | No | Same as bootstrap: services exist, types match, env vars discovered |
| generate | creative | No | Same as bootstrap + recipe-specific: app README has fragments, zerops.yaml has base+prod+dev setups, comment ratio ≥ 0.3 |
| deploy | branching | No | Same as bootstrap: all runtimes RUNNING, subdomains enabled, health checks pass |
| finalize | creative | No | All 13+ recipe files exist, fragment tags valid, import.yaml valid YAML, env scaling correct, no placeholders |
| close | fixed | Yes | Administrative (metas written, publish commands presented) |

---

## Verification Errata

Issues found during triple-confirmation against actual codebase (2026-04-02):

### E1: `detectActiveWorkflow` defaults to bootstrap (CRITICAL)

`tools/workflow.go:184-196` — `detectActiveWorkflow()` returns `workflowBootstrap` as fallback when the active workflow is not deploy. Without updating this function, an active recipe session gets misrouted to bootstrap handlers for `complete`/`skip`/`status` actions.

**Fix**: Add `state.Recipe != nil && state.Recipe.Active` check, return `workflowRecipe`.

### E2: `Start()` auto-reset only checks Bootstrap/Deploy

`engine.go:43-44` — The auto-reset logic explicitly checks `bootstrapDone` and `deployDone` but has no `recipeDone` path. Completed recipe sessions won't auto-reset on next `Start()`.

**Fix**: Add `recipeDone := existing.Recipe != nil && !existing.Recipe.Active` and include in the OR condition.

### E3: `IterateSession` only resets Bootstrap/Deploy

`session.go:106-111` — `ResetForIteration()` is called on `state.Bootstrap` and `state.Deploy` but not `state.Recipe`.

**Fix**: Add `if state.Recipe != nil { state.Recipe.ResetForIteration() }`.

### E4: `assembleGuidance` is bootstrap-only — NOT a shared router

`guidance.go:25` — The plan originally said "modify `guidance.go` to route recipe guidance." In reality, `assembleGuidance` is called exclusively from `BootstrapState.buildGuide()`. Deploy has its own separate `DeployState.buildGuide()` that never touches `assembleGuidance`.

**Fix**: Recipe should follow deploy's pattern — own `RecipeState.buildGuide()` method. Can reuse `assembleKnowledge()` (which IS generic) and `ExtractSection()` (which IS generic). Do NOT modify `assembleGuidance()`.

### E5: Fragment marker format is `#ZEROPS_EXTRACT_START:name#`, not `ZEROPS_EXTRACT_START name`

`sync/transform.go:60` — Actual markers use colon separator and trailing hash: `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->`, not the space-separated format used in the plan's checker descriptions.

**Fix**: All checker regex patterns must use the actual `#ZEROPS_EXTRACT_START:{name}#` / `#ZEROPS_EXTRACT_END:{name}#` format.

### E6: `StepChecker` type is bootstrap-specific

`bootstrap_checks.go` — `StepChecker func(ctx, plan *ServicePlan, state *BootstrapState)` takes bootstrap-specific types. `DeployStepChecker func(ctx, state *DeployState)` takes deploy-specific types.

**Fix**: Define `RecipeStepChecker func(ctx context.Context, plan *RecipePlan, state *RecipeState) (*StepCheckResult, error)` — own type following the established per-workflow pattern.

### E7: `baseInstructions` hardcodes workflow list

`server/instructions.go:17-23` — The available workflows are hardcoded as `deploy, bootstrap, cicd`. Recipe must be added to this list for the LLM to know it exists.

**Fix**: Add `recipe` to `baseInstructions` constant.

---

## Phase 1: Recipe State & Engine Skeleton

**Goal**: Wire `recipe` as a recognized workflow. Start/status/complete/skip work. No step logic yet — just the state machine.

### New files

#### `internal/workflow/recipe.go` (~180 lines)

Core types and state lifecycle. Mirrors `bootstrap.go` structure.

```go
const WorkflowRecipe = "recipe"

// Recipe tier constants.
const (
    RecipeTierMinimal  = "minimal"   // type 3
    RecipeTierShowcase = "showcase"  // type 4
)

type RecipeState struct {
    Active            bool                `json:"active"`
    CurrentStep       int                 `json:"currentStep"`
    Steps             []RecipeStep        `json:"steps"`
    Plan              *RecipePlan         `json:"plan,omitempty"`
    DiscoveredEnvVars map[string][]string `json:"discoveredEnvVars,omitempty"`
    OutputDir         string              `json:"outputDir,omitempty"` // recipe repo files
}

type RecipeStep struct {
    Name        string `json:"name"`
    Status      string `json:"status"` // pending, in_progress, complete, skipped
    Attestation string `json:"attestation,omitempty"`
    SkipReason  string `json:"skipReason,omitempty"`
    CompletedAt string `json:"completedAt,omitempty"`
}

type RecipePlan struct {
    Framework   string          `json:"framework"`   // "laravel", "nestjs", "django"
    Tier        string          `json:"tier"`         // "minimal" or "showcase"
    Slug        string          `json:"slug"`         // "laravel-hello-world"
    RuntimeType string          `json:"runtimeType"`  // "php-nginx@8.4"
    BuildBases  []string        `json:"buildBases"`   // ["php@8.4", "nodejs@22"]
    Decisions   DecisionResults `json:"decisions"`
    Research    ResearchData    `json:"research"`
    Targets     []RecipeTarget  `json:"targets"`      // derived from tier + framework
}

type RecipeTarget struct {
    Hostname     string   `json:"hostname"`
    Type         string   `json:"type"`
    Role         string   `json:"role"`      // "app", "worker", "db", "cache", etc.
    Environments []string `json:"environments"` // which envs include this service
}

type DecisionResults struct {
    WebServer  string `json:"webServer"`  // "builtin", "nginx-sidecar", "nginx-proxy"
    BuildBase  string `json:"buildBase"`  // runtime type for build phase
    OS         string `json:"os"`         // "ubuntu-22", "alpine"
    DevTooling string `json:"devTooling"` // "hot-reload", "watch", "manual"
}

type ResearchData struct {
    // Framework identity
    ServiceType    string `json:"serviceType"`
    PackageManager string `json:"packageManager"`
    HTTPPort       int    `json:"httpPort"`
    // Build & deploy pipeline
    BuildCommands  []string `json:"buildCommands"`
    DeployFiles    []string `json:"deployFiles"`
    StartCommand   string   `json:"startCommand"`
    CacheStrategy  []string `json:"cacheStrategy"`
    // Database & migration
    DBDriver       string `json:"dbDriver"`
    MigrationCmd   string `json:"migrationCmd"`
    SeedCmd        string `json:"seedCmd,omitempty"`
    // Environment & secrets
    NeedsAppSecret bool   `json:"needsAppSecret"`
    LoggingDriver  string `json:"loggingDriver"`
    // Showcase-only
    CacheLib       string `json:"cacheLib,omitempty"`
    SessionDriver  string `json:"sessionDriver,omitempty"`
    QueueDriver    string `json:"queueDriver,omitempty"`
    StorageDriver  string `json:"storageDriver,omitempty"`
    SearchLib      string `json:"searchLib,omitempty"`
    MailLib        string `json:"mailLib,omitempty"`
}

// NewRecipeState, CompleteStep, SkipStep, ResetForIteration, BuildResponse,
// BuildProgress — mirror bootstrap.go methods adapted for 6 recipe steps.
```

#### `internal/workflow/recipe_steps.go` (~50 lines)

Step definitions with tools and verification gates.

```go
const (
    RecipeStepResearch  = "research"
    RecipeStepProvision = "provision"
    RecipeStepGenerate  = "generate"
    RecipeStepDeploy    = "deploy"
    RecipeStepFinalize  = "finalize"
    RecipeStepClose     = "close"
)

var recipeStepDetails = []StepDetail{
    {Name: RecipeStepResearch,  Tools: []string{"zerops_knowledge", "zerops_discover", "zerops_workflow"}, Verification: "SUCCESS WHEN: RecipePlan submitted with all research fields, types validated against live catalog, decision branches resolved."},
    {Name: RecipeStepProvision, Tools: []string{"zerops_import", "zerops_process", "zerops_discover", "zerops_mount"}, Verification: "SUCCESS WHEN: all workspace services exist with expected status AND types match AND managed dep env vars recorded."},
    {Name: RecipeStepGenerate,  Tools: []string{"zerops_knowledge"}, Verification: "SUCCESS WHEN: zerops.yml valid with base+prod+dev setups AND app README has integration-guide fragment with commented zerops.yaml AND knowledge-base fragment exists with Gotchas section AND comment ratio ≥ 0.3."},
    {Name: RecipeStepDeploy,    Tools: []string{"zerops_deploy", "zerops_discover", "zerops_subdomain", "zerops_logs", "zerops_mount", "zerops_verify", "zerops_manage"}, Verification: "SUCCESS WHEN: all runtime services deployed, accessible, AND healthy."},
    {Name: RecipeStepFinalize,  Tools: []string{"zerops_workflow"}, Verification: "SUCCESS WHEN: all recipe repo files generated (6 import.yaml + 7 READMEs), fragment tags valid, YAML valid, env scaling correct, no placeholders."},
    {Name: RecipeStepClose,     Tools: []string{"zerops_workflow"}, Verification: "SUCCESS WHEN: recipe administratively closed, publish commands presented."},
}
```

### Modified files

#### `internal/workflow/state.go`

```go
// ADD field to WorkflowState:
Recipe *RecipeState `json:"recipe,omitempty"`
```

#### `internal/workflow/engine.go` → `internal/workflow/engine_recipe.go` (new, ~120 lines)

Recipe lifecycle methods in a SEPARATE file (engine.go is already 396 lines, over 350 limit). Follows `engine_deploy.go` (107 lines) precedent:

- `RecipeStart(projectID, intent, tier string) (*WorkflowState, error)` — calls `e.Start()` with `WorkflowRecipe`, initializes `RecipeState`
- `RecipeComplete(step, attestation string, checker RecipeStepChecker) (*RecipeResponse, error)` — delegates research step to `RecipeCompletePlan`, runs checker for others
- `RecipeCompletePlan(plan RecipePlan, attestation string) (*RecipeResponse, error)` — validates plan, stores in state
- `RecipeSkip(step, reason string) (*RecipeResponse, error)` — only close is skippable
- `RecipeStatus() (*RecipeResponse, error)` — returns current state with guidance

#### `internal/workflow/engine.go` (modify ~5 lines)

Fix auto-reset logic at line 43-44 **(errata E2)**:

```go
// BEFORE (only checks bootstrap/deploy):
bootstrapDone := existing.Bootstrap != nil && !existing.Bootstrap.Active
deployDone := existing.Deploy != nil && !existing.Deploy.Active
if bootstrapDone || deployDone {

// AFTER (adds recipe):
bootstrapDone := existing.Bootstrap != nil && !existing.Bootstrap.Active
deployDone := existing.Deploy != nil && !existing.Deploy.Active
recipeDone := existing.Recipe != nil && !existing.Recipe.Active
if bootstrapDone || deployDone || recipeDone {
```

#### `internal/workflow/session.go` (modify ~3 lines)

Add recipe iteration reset at line ~111 **(errata E3)**:

```go
if state.Recipe != nil {
    state.Recipe.ResetForIteration()
}
```

`InitSessionAtomic` is already generic (takes `workflowName` string) — no changes needed there.

#### `internal/workflow/bootstrap_checks.go` (add type, ~3 lines)

Add recipe-specific checker type **(errata E6)**:

```go
// RecipeStepChecker validates recipe step postconditions.
type RecipeStepChecker func(ctx context.Context, plan *RecipePlan, state *RecipeState) (*StepCheckResult, error)
```

#### `internal/tools/workflow.go`

Wire recipe into dispatch. Three changes **(errata E1)**:

**1. Add constant (line ~16):**
```go
workflowRecipe = workflow.WorkflowRecipe
```

**2. Fix `detectActiveWorkflow` (line ~184) — CRITICAL:**
```go
// BEFORE: defaults to bootstrap
if state.Deploy != nil && state.Deploy.Active { return workflowDeploy }
return workflowBootstrap

// AFTER: explicit three-way check
if state.Deploy != nil && state.Deploy.Active { return workflowDeploy }
if state.Recipe != nil && state.Recipe.Active { return workflowRecipe }
return workflowBootstrap
```

**3. Add recipe routing in switch cases (lines ~100-117):**
```go
case "complete":
    active := detectActiveWorkflow(engine)
    if active == workflowDeploy { return handleDeployComplete(...) }
    if active == workflowRecipe { return handleRecipeComplete(...) }
    return handleBootstrapComplete(...)
// Same pattern for "skip" and "status"
```

**4. Add to `handleStart` (line ~159):**
```go
if input.Workflow == workflowRecipe {
    return handleRecipeStart(ctx, projectID, engine, client, cache, input)
}
```

#### `internal/tools/workflow_recipe.go` (~150 lines, new)

Recipe-specific action handlers:

- `handleRecipeStart` — validates tier param, calls `engine.RecipeStart()`, injects stacks
- `handleRecipeComplete` — routes research step to plan submission, others to checkers
- `handleRecipeSkip` — validates skip rules (only close)
- `handleRecipeStatus` — returns current state

### Tests (RED first)

- `internal/workflow/recipe_test.go` — `TestNewRecipeState`, `TestRecipeCompleteStep`, `TestRecipeSkipStep`, `TestRecipeResetForIteration`
- `internal/tools/workflow_recipe_test.go` — `TestRecipeStart_Success`, `TestRecipeStart_InvalidTier`, `TestRecipeComplete_Research`

### Verification

```bash
go test ./internal/workflow/... -run TestRecipe -v
go test ./internal/tools/... -run TestRecipe -v
go build -o bin/zcp ./cmd/zcp
```

---

## Phase 2: Research Step — Decision Logic & Validation

**Goal**: The RESEARCH step works end-to-end. LLM can start a recipe workflow, fill research, submit plan, and advance to provision.

### New files

#### `internal/workflow/recipe_decisions.go` (~120 lines)

Four decision trees as pure functions:

```go
// ResolveWebServer determines web server strategy.
// Inputs: framework's native HTTP support, runtime type.
// Outputs: "builtin" | "nginx-sidecar" | "nginx-proxy"
func ResolveWebServer(runtimeType string, hasNativeHTTP bool) string

// ResolveBuildBase determines build-phase runtime.
// Inputs: framework needs (e.g., Laravel needs nodejs for Vite).
// Outputs: primary build base type string
func ResolveBuildBase(runtimeType string, needsNodeBuild bool) string

// ResolveOS determines base OS preference.
// Inputs: runtime type, framework requirements.
// Outputs: "ubuntu-22" | "alpine"
func ResolveOS(runtimeType string) string

// ResolveDevTooling determines dev iteration strategy.
// Inputs: framework's hot-reload support, language characteristics.
// Outputs: "hot-reload" | "watch" | "manual"
func ResolveDevTooling(framework string, runtimeType string) string
```

These are **advisory** — the LLM can override. They provide defaults for the research form so the LLM doesn't hallucinate incorrect combinations.

#### `internal/workflow/recipe_validate.go` (~120 lines)

Plan validation (extends `validate.go` patterns):

```go
func ValidateRecipePlan(plan RecipePlan, liveTypes []platform.ServiceStackType) []string
```

Validates:
- Framework non-empty, slug follows `{framework}-hello-world` or `{framework}-showcase` pattern
- Tier is `minimal` or `showcase`
- RuntimeType exists in live catalog
- BuildBases all exist in live catalog
- Research fields: serviceType, packageManager, httpPort, buildCommands, deployFiles, startCommand non-empty
- For showcase: cacheLib, sessionDriver, queueDriver required
- Targets derived correctly from tier (minimal = app+db, showcase = app+worker+db+redis+storage+mailpit+search)

#### `internal/content/workflows/recipe.md` (~400 lines)

Section-tagged guidance for all 6 steps. Structure:

```markdown
<section name="research-minimal">
## Research — Minimal Recipe (Type 3)

Fill in all research fields. Load the runtime's hello-world recipe as a reference:
`zerops_knowledge recipe="{runtime}-hello-world"`

### Framework Identity
- Service type (from available stacks): ___
- Package manager: ___
- HTTP port: ___
...

### Decision Tree Resolution
Use `zerops_knowledge` scope mode to load import.yml schema.
- Web server: {guidance on when builtin vs nginx-sidecar}
- Build base: {guidance on multi-runtime builds}
...
</section>

<section name="research-showcase">
## Research — Showcase Recipe (Type 4)
{extends minimal with showcase-specific fields}
</section>

<section name="provision">
{reuses bootstrap provision guidance + recipe-specific import template}
</section>

<section name="generate">
{reuses bootstrap generate guidance + recipe-specific additions:
 health dashboard spec, migration spec, fragment quality rules}
</section>

<section name="generate-fragments">
## Fragment Quality Requirements
{comment conventions, integration-guide structure, knowledge-base structure}
{reference recipe loading instructions}
</section>

<section name="deploy">
{reuses bootstrap deploy guidance}
</section>

<section name="finalize">
## Finalize — Recipe Repository Files
{template generation instructions, comment conventions for import.yaml,
 environment scaling matrix, minimum primitives coverage}
</section>

<section name="close">
## Close — Publish
{sync push commands, Strapi registration, test instructions}
</section>
```

### Modified files

#### `internal/workflow/guidance.go` — NO CHANGES (errata E4)

`assembleGuidance` is bootstrap-only. Recipe follows deploy's pattern: own `buildGuide` method on `RecipeState`. Recipe CAN reuse `assembleKnowledge()` (generic) and `ExtractSection()` (generic) from this file.

#### `internal/workflow/recipe_guidance.go` (~120 lines, new)

Follows deploy pattern (`deploy_guidance.go`) — guidance is a method on `RecipeState`:

```go
// buildGuide assembles step-specific guidance with knowledge injection.
func (r *RecipeState) buildGuide(step string, iteration int, env Environment, kp knowledge.Provider) string {
    // 1. If iteration > 0, return BuildIterationDelta (reuse from bootstrap_guidance.go)
    // 2. Extract static guidance from recipe.md via ExtractSection
    //    - research: "research-{tier}" section
    //    - generate: "generate" + "generate-fragments" sections
    //    - finalize: "finalize" section
    //    - other steps: section matching step name
    // 3. Append knowledge via assembleKnowledge (reuse from guidance.go)
    //    - provision: import.yml schema
    //    - generate: runtime briefing + env vars + zerops.yml schema
    //    - deploy: schema rules
}
```

### Tests

- `internal/workflow/recipe_decisions_test.go` — table-driven tests for all 4 decision trees
- `internal/workflow/recipe_validate_test.go` — `TestValidateRecipePlan_Valid`, `TestValidateRecipePlan_MissingFields`, `TestValidateRecipePlan_InvalidTypes`

---

## Phase 3: Generate Step — Fragment Quality + Recipe-Specific Checks

**Goal**: GENERATE step has recipe-specific hard checkers that validate app README fragments, zerops.yaml structure, and comment quality.

### New files

#### `internal/tools/workflow_checks_recipe.go` (~200 lines)

Generate step checker (extends bootstrap's `checkGenerate`):

```go
func checkRecipeGenerate(ctx context.Context, plan *RecipePlan, state *RecipeState, client platform.Client, projectID string) (*workflow.StepCheckResult, error)
```

Validates everything `checkGenerate` does, plus:
- App README exists on mount with `<!-- #ZEROPS_EXTRACT_START:integration-guide# -->` markers
- Integration-guide fragment contains complete zerops.yaml code block
- zerops.yaml code block has `base`, `prod`, and `dev` setups (showcase: + `worker`)
- `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->` markers present
- Knowledge-base has at least one `### Gotchas` H3 section
- Comment ratio in zerops.yaml code block ≥ 0.3 (comment lines / total config lines)
- No `PLACEHOLDER_*`, `<your-...>`, `TODO` strings
- `<!-- #ZEROPS_EXTRACT_START:intro# -->` markers present, intro is 1-3 lines

### Modified files

#### `internal/tools/workflow_recipe.go`

Wire `checkRecipeGenerate` as the checker for the generate step in recipe workflow.

### Tests

- `internal/tools/workflow_checks_recipe_test.go` — table-driven with fixture zerops.yaml + README content:
  - `TestCheckRecipeGenerate_ValidMinimal`
  - `TestCheckRecipeGenerate_MissingFragments`
  - `TestCheckRecipeGenerate_LowCommentRatio`
  - `TestCheckRecipeGenerate_PlaceholderText`

---

## Phase 4: Finalize Step — Template Generation + Hard Checkers

**Goal**: FINALIZE step generates recipe repo files from `RecipePlan` and validates completeness.

### New files

#### `internal/workflow/recipe_templates.go` (~250 lines)

Template generators — pure functions, no I/O:

```go
// GenerateRecipeREADME returns the main recipe README.md content.
func GenerateRecipeREADME(plan *RecipePlan) string

// GenerateEnvREADME returns the README.md for a specific environment tier.
func GenerateEnvREADME(plan *RecipePlan, envIndex int) string

// GenerateEnvImportYAML returns the import.yaml skeleton for a specific env.
// Returns YAML with service structure but WITHOUT comments — LLM adds those.
func GenerateEnvImportYAML(plan *RecipePlan, envIndex int) string

// GenerateRecipeOutputYAML returns the recipe-output.yaml for Strapi registration.
func GenerateRecipeOutputYAML(plan *RecipePlan) string
```

Environment scaling matrix (hardcoded from observed patterns):

| Property | Env 0-1 (Agent/Remote) | Env 2 (Local) | Env 3 (Stage) | Env 4 (Small Prod) | Env 5 (HA Prod) |
|----------|----------------------|---------------|---------------|--------------------|-----------------| 
| App hostnames | appdev + appstage | app | app | app | app |
| App setups | dev + prod | prod | prod | prod | prod |
| DB mode | NON_HA | NON_HA | NON_HA | NON_HA | HA |
| minContainers | — | — | — | 2 | 2 |
| cpuMode | — | — | — | — | DEDICATED |
| corePackage | — | — | — | — | SERIOUS |
| minFreeRamGB | — | — | 0.25 | 0.125 | 0.25 |
| enableSubdomainAccess | all apps | yes | yes | yes | yes |

Env folder names: `0 — AI Agent`, `1 — Remote (CDE)`, `2 — Local`, `3 — Stage`, `4 — Small Production`, `5 — Highly-available Production` (em-dash U+2014, matches actual repo).

#### `internal/workflow/recipe_finalize.go` (~150 lines)

Finalize step orchestration:

```go
// BuildFinalizeOutput generates all recipe repo files and returns them as a map.
// Keys are relative paths (e.g., "0 — AI Agent/import.yaml").
// Values are file content strings.
func BuildFinalizeOutput(plan *RecipePlan) map[string]string

// The LLM receives this map, reviews import.yaml files, adds comments,
// and writes everything to the output directory.
```

#### `internal/tools/workflow_checks_finalize.go` (~200 lines)

Finalize step checker:

```go
func checkRecipeFinalize(ctx context.Context, plan *RecipePlan, outputDir string) (*workflow.StepCheckResult, error)
```

Validates:
- All 13+ files exist (6 import.yaml + 6 env README + 1 main README)
- Fragment tags use exact `<!-- #ZEROPS_EXTRACT_START:{name}# -->` / `<!-- #ZEROPS_EXTRACT_END:{name}# -->` format (regex) **(errata E5)**
- `intro` fragments: no titles, no deploy buttons, no images, 1-3 lines
- Project names follow `{slug}-{env-suffix}` convention
- All import.yaml files parse as valid YAML
- All import.yaml files have `priority: 10` on data services
- Env 5: `corePackage: SERIOUS`, HA modes on applicable services, DEDICATED cpuMode
- Env 4: `minContainers: 2` on app services
- `envSecrets` present where `plan.Research.NeedsAppSecret == true`
- `#zeropsPreprocessor=on` present when `envSecrets` uses `<@generateRandomString>`
- `verticalAutoscaling` nesting correct (minRam, minFreeRamGB, cpuMode under it)
- Comment line width ≤ 80 chars in YAML files
- No `PLACEHOLDER_*` strings anywhere
- No cross-environment references in comments
- Comment ratio ≥ 0.3 per import.yaml

### Tests

- `internal/workflow/recipe_templates_test.go` — table-driven for each generator function:
  - `TestGenerateRecipeREADME_Minimal`, `TestGenerateRecipeREADME_Showcase`
  - `TestGenerateEnvImportYAML_AllEnvs` (parametric over env 0-5)
  - `TestGenerateEnvREADME_FixedContent`
- `internal/tools/workflow_checks_finalize_test.go` — fixture-based validation tests

---

## Phase 5: Router + System Prompt Integration

**Goal**: Recipe workflow appears in router offerings. System prompt includes recipe context.

### Modified files

#### `internal/workflow/router.go`

Add recipe offering logic:

```go
// In Route(): if project has no recipes yet (no RecipeMeta) and
// bootstrap is complete, offer recipe workflow at priority 3.
// If recipe session is active, offer resume at priority 1.
```

Recipe workflow is an **optional offering** — it appears when:
1. Bootstrap completed (services exist with metas)
2. No active recipe session
3. User intent suggests recipe creation

#### `internal/server/instructions.go`

Two changes **(errata E7)**:

**1. Update `baseInstructions` constant (line ~17)** — add `recipe` to the hardcoded workflow list so the LLM knows it exists:
```go
// BEFORE: "Workflows: deploy, bootstrap, cicd"
// AFTER:  "Workflows: deploy, bootstrap, recipe, cicd"
```

**2. `buildWorkflowHint` (line ~89)** — already generic (iterates sessions by `s.Workflow` name), will handle recipe sessions automatically. No changes needed. Optionally add recipe-specific progress info (step/plan summary) similar to bootstrap's step progress block.

#### `internal/tools/workflow.go`

Update tool description to include `recipe`:

```go
Description: "... Workflows: bootstrap, deploy, recipe, cicd ..."
```

Update `WorkflowInput` jsonschema tag similarly.

### Tests

- `internal/workflow/router_test.go` — add `TestRoute_RecipeOffering` cases
- `internal/tools/workflow_test.go` — update annotation tests for new workflow name

---

## Phase 6: Close Step + Sync Integration

**Goal**: Close step writes recipe metadata, presents publish commands, integrates with `zcp sync push`.

### Modified files

#### `internal/workflow/recipe.go`

Add `RecipeMeta` type and persistence (analogous to `ServiceMeta`):

```go
type RecipeMeta struct {
    Slug        string `json:"slug"`
    Framework   string `json:"framework"`
    Tier        string `json:"tier"`
    RuntimeType string `json:"runtimeType"`
    CreatedAt   string `json:"createdAt"`
    OutputDir   string `json:"outputDir"`
}

// Stored at .zcp/state/recipes/{slug}.json
```

#### `internal/workflow/engine.go`

`RecipeClose` method:
- Writes `RecipeMeta`
- Builds transition message with:
  - `zcp sync push recipes {slug}` command
  - Strapi CMS entry creation instructions (from `recipe-output.yaml`)
  - Test launch instructions for all 6 environments
  - Link to eval: `zcp eval run --recipe {slug}`

### Tests

- `internal/workflow/recipe_meta_test.go` — CRUD tests for `RecipeMeta`

---

## Phase 7: Eval Integration (Path B)

**Goal**: `zcp eval create` spawns Claude CLI headlessly against a recipe workflow session.

### New files

#### `internal/eval/recipe_create.go` (~150 lines)

```go
func (r *Runner) CreateRecipe(ctx context.Context, framework, tier string) (*RunResult, error) {
    prompt := BuildRecipeCreatePrompt(framework, tier)
    // Spawns Claude CLI with ZCP MCP server
    // Claude calls: zerops_workflow action="start" workflow="recipe" intent="..."
    // ZCP guides through all 6 steps
    // Extract: assessment, tool calls, output files
}

func BuildRecipeCreatePrompt(framework, tier string) string {
    // "Create a {framework} {tier} recipe. Start with:
    //  zerops_workflow action='start' workflow='recipe'
    //  Then follow ZCP's step-by-step guidance."
}
```

#### `internal/eval/recipe_suite.go` (~80 lines)

Batch creation:

```go
func (s *Suite) CreateRecipes(ctx context.Context, frameworks []string, tier string) (*SuiteResult, error)
```

### Modified files

#### `cmd/zcp/eval.go`

Add subcommands:

```bash
zcp eval create --framework laravel --tier minimal
zcp eval create-suite --tier minimal --frameworks laravel,nestjs,django
```

### Quality loop

```
zcp eval create → recipe files generated
  → zcp sync push recipes {slug} (creates PR)
  → merge PR
  → zcp sync cache-clear {slug}
  → zcp sync pull recipes {slug} (gets merged version)
  → zcp eval run --recipe {slug} (test recipe via normal eval)
  → iterate if eval fails
```

---

## File Manifest

### New files (17, corrected)

| File | Phase | Lines (est) | Purpose |
|------|-------|-------------|---------|
| `internal/workflow/recipe.go` | 1 | 180 | RecipeState, RecipePlan, types, lifecycle |
| `internal/workflow/recipe_steps.go` | 1 | 50 | 6 step definitions |
| `internal/workflow/recipe_test.go` | 1 | 200 | State machine tests |
| `internal/workflow/engine_recipe.go` | 1 | 120 | Engine recipe methods (split from engine.go) |
| `internal/workflow/recipe_decisions.go` | 2 | 120 | 4 decision trees |
| `internal/workflow/recipe_decisions_test.go` | 2 | 150 | Decision tree tests |
| `internal/workflow/recipe_validate.go` | 2 | 120 | Plan validation |
| `internal/workflow/recipe_validate_test.go` | 2 | 150 | Validation tests |
| `internal/workflow/recipe_guidance.go` | 2 | 120 | Guidance assembly (own buildGuide, NOT via assembleGuidance) |
| `internal/content/workflows/recipe.md` | 2 | 400 | Section-tagged guidance |
| `internal/tools/workflow_recipe.go` | 1 | 150 | Recipe action handlers |
| `internal/tools/workflow_recipe_test.go` | 1 | 200 | Handler tests |
| `internal/tools/workflow_checks_recipe.go` | 3 | 200 | Generate step checker |
| `internal/tools/workflow_checks_recipe_test.go` | 3 | 200 | Checker tests |
| `internal/workflow/recipe_templates.go` | 4 | 250 | Import.yaml + README generators |
| `internal/workflow/recipe_templates_test.go` | 4 | 200 | Generator tests |
| `internal/tools/workflow_checks_finalize.go` | 4 | 200 | Finalize step checker |
| `internal/tools/workflow_checks_finalize_test.go` | 4 | 150 | Finalize checker tests |
| `internal/eval/recipe_create.go` | 7 | 150 | Headless recipe creation |

### Modified files (9, corrected)

| File | Phase | Change | Errata |
|------|-------|--------|--------|
| `internal/workflow/state.go` | 1 | Add `Recipe *RecipeState` field | — |
| `internal/workflow/engine.go` | 1 | Fix auto-reset logic (~5 lines) | E2 |
| `internal/workflow/session.go` | 1 | Add recipe iteration reset (~3 lines) | E3 |
| `internal/workflow/bootstrap_checks.go` | 1 | Add `RecipeStepChecker` type (~3 lines) | E6 |
| `internal/tools/workflow.go` | 1 | Fix `detectActiveWorkflow`, wire dispatch (~25 lines) | E1 |
| `internal/workflow/router.go` | 5 | Add recipe offering | — |
| `internal/server/instructions.go` | 5 | Add `recipe` to `baseInstructions` + workflow hints | E7 |
| `internal/tools/workflow.go` | 5 | Update tool description | — |
| `cmd/zcp/eval.go` | 7 | `create` and `create-suite` subcommands | — |

### Estimated totals

- **New Go code**: ~1,350 lines (implementation) + ~1,250 lines (tests)
- **New guidance content**: ~400 lines (recipe.md)
- **Modified code**: ~240 lines across 9 files
- **Replaces**: ~2,200 lines of meta-prompt

---

## Execution Order & Dependencies

```
Phase 1 ──→ Phase 2 ──→ Phase 3 ──→ Phase 4 ──→ Phase 5 ──→ Phase 6 ──→ Phase 7
(skeleton)  (research)   (generate)   (finalize)   (router)    (close)     (eval)
```

**No phase can be parallelized** — each depends on the prior. Within each phase, tests and implementation can be interleaved (TDD: RED → GREEN → REFACTOR).

**Phase 1 is the critical path** — once the state machine works, all subsequent phases are incremental additions to a working system. Each phase produces a testable, usable increment:

| After phase | What works |
|-------------|-----------|
| 1 | `zerops_workflow action="start" workflow="recipe"` creates session, step progression works |
| 2 | Research step accepts plan, validates types, injects guidance |
| 3 | Generate step validates app README fragments and comment quality |
| 4 | Finalize step generates recipe repo files and validates completeness |
| 5 | Recipe appears in router, system prompt shows recipe context |
| 6 | Close step writes meta, presents publish commands |
| 7 | `zcp eval create --framework X --tier Y` works headlessly |

---

## Risk Register

| Risk | Impact | Mitigation |
|------|--------|------------|
| Generate step checker too strict → LLM can't pass | Blocks all recipe creation | Start with loose thresholds (comment ratio 0.2), tighten after first 3 recipes |
| Finalize import.yaml templates don't match actual repo conventions | Wrong output format | Validate generators against 3 existing recipes (bun, laravel, nestjs) in tests |
| Recipe workflow guidance too verbose → context window pressure | LLM loses track | Keep each section under 100 lines, use knowledge pointers not inlined content |
| Decision trees produce wrong defaults for edge-case frameworks | Bad research step | Trees are advisory — LLM can override. Log overrides for future tree refinement |
| Engine coupling: adding 3rd workflow type bloats engine.go | File over 350 lines | Recipe methods go in `engine_recipe.go` (follows `engine_deploy.go` pattern) |
| `recipe_lint_test.go` helpers are unexported | Can't reuse validation logic in recipe checkers | Export needed helpers (e.g. `validateZeropsYml`, `validateImportYml`) or duplicate targeted subsets in checker code |

---

## Success Criteria

1. **Functional**: `zerops_workflow action="start" workflow="recipe" intent="Create a Laravel minimal recipe"` → 6-step guided flow → all 17+ files produced → `zcp sync push recipes laravel-hello-world` publishes to GitHub
2. **Quality**: Generated recipes pass `recipe_lint_test.go` validation rules
3. **Efficiency**: Recipe creation via ZCP is faster than meta-prompt two-layer system (fewer tokens, no duplication of platform knowledge)
4. **Testable**: Every step has hard checkers. Every checker has table-driven tests. Every template generator has fixture-based tests.
5. **Eval-ready**: Path B (`zcp eval create`) works within 1 week of Path A completion
