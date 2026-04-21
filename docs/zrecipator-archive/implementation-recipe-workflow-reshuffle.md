# Implementation guide: recipe workflow knowledge reshuffle

**Reader**: opus-level implementor running against the ZCP codebase.
**Companion**: `research-recipe-workflow-reshuffle.md` — the "why" behind every move here.
**Invariant**: every change is verifiable via a grep, a test, or a byte-count measurement.

---

## Operating model

This guide is a sequence of phases. Each phase is self-contained — you can pause after any phase, run tests, and the repo is in a coherent state. Within a phase, tasks are ordered by dependency. The last task of every phase is "run the measurement harness and confirm targets". If a phase measurement fails, do not proceed — fix the phase before moving on.

**Do not batch phases.** The reshuffle is ~200 edits across ~15 files and the temptation to combine phases is high. Resist. Each phase lands on green tests or you roll back to the previous phase.

**Never treat a passing cap test as proof of correctness.** The cap test is a floor — it tells you the response isn't catastrophically large. The correctness check is the grep tests (content is where you expect, not somewhere else) and a manual read of the assembled guide at each step.

**TDD order**: for every behavioral move, write the failing grep/presence test FIRST, then implement, then verify. For pure content compressions with no behavioral change, a post-hoc cap test is sufficient.

---

## Phase 0 — Measurement harness

**Goal**: land the tests that prove each phase's correctness before making any content changes.

### 0.1 — Replace the broken cap test with a whole-response test

**File**: `internal/workflow/recipe_guidance_test.go`

**Problem**: current `TestResolveRecipeGuidance_Generate_ShowcaseUnderSizeCap` measures `resolveRecipeGuidance` alone, missing the chain recipe injection added in `buildGuide`. This is the v8.52.0 mistake — the test passed while the real assembled guide was 48+ KB.

**Change**: add a new test `TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap` that:
- For each step (research, provision, generate, deploy, finalize, close), builds a showcase state and calls `BuildResponse(...).Current.DetailedGuide`.
- Uses `knowledge.GetEmbeddedStore()` (not nil, not a mock) so chain injection is counted.
- Asserts each step's detailedGuide byte count is under its target cap.

**Initial caps** (these are the post-reshuffle targets — all must be RED when you add the test, that's the point):

```go
var showcaseStepCaps = map[string]int{
    RecipeStepResearch:  10 * 1024, // current: 15.1 KB; floor is ~9 KB (showcase+minimal concat)
    RecipeStepProvision: 14 * 1024, // current: 26.8 KB; R&P split retains ~6 KB provision-side
    RecipeStepGenerate:  32 * 1024, // current: 48.3 KB; after dashboard delete + fragments trim
    RecipeStepDeploy:    32 * 1024, // current: 28.2 KB (target after move-in from generate)
    RecipeStepFinalize:  14 * 1024, // current: 17.5 KB; drop vestigial schema + add voice content
    RecipeStepClose:     12 * 1024, // current: 16.1 KB; dedupe browser walk
}
```

**Cap justification**:

- **Research 10 KB**: [recipe_guidance.go:63-67](../internal/workflow/recipe_guidance.go#L63-L67) concatenates `research-showcase + research-minimal` at showcase tier. research-showcase is 7.5 KB today and Phase 2.4 trims it to ~5 KB; research-minimal is 7.9 KB today and Phase 2.3 trims it to ~2 KB. Realistic floor: ~7 KB + separator. An 8 KB cap would be unachievable without rewriting the concat path; 10 KB cap gives ~3 KB headroom.
- **Provision 14 KB**: provision section is 8.3 KB today, plus import.yaml Schema injection 4.9 KB, plus Rules & Pitfalls 14.2 KB (not 8 KB as earlier drafts claimed). Phase 1 split retains ~6 KB of R&P at provision ("Provision Rules" H2). Phase 3 trims the provision section ~2 KB and adds ~1 KB (git config + discover strengthening). Realistic floor: 6.3 + 4.9 + 1 (env vars straddle duplication) = ~12 KB + attestation context. 14 KB cap has ~2 KB headroom.
- **Deploy 32 KB**: Phase 4 moves ~7 KB of sub-agent brief IN from generate-dashboard, adds ~1 KB of new pitfalls warnings (execOnce, Vite collision, targetService, git config). Current 28 KB + net add ~2 KB = ~30 KB. 32 KB cap has 2 KB headroom.
- **Other caps**: generate 32 KB is the post-dashboard-deletion target; finalize 14 KB removes the vestigial 4.9 KB import.yaml Schema injection but adds voice content; close 12 KB removes the duplicated browser walk block.

**Step advancement helper**: add a private helper that walks a `RecipeState` forward through steps in one place so every per-step test uses the same setup. Shape:

```go
// advanceTo returns a RecipeState with steps [0..step-1] marked complete and
// `step` in progress. Plan, discoveredEnvVars, and outputDir are populated as
// they would be at that point in a real showcase run.
func advanceShowcaseStateTo(step string, plan *RecipePlan) *RecipeState {
    rs := NewRecipeState()
    rs.Tier = RecipeTierShowcase
    rs.Plan = plan
    stepOrder := []string{RecipeStepResearch, RecipeStepProvision, RecipeStepGenerate, RecipeStepDeploy, RecipeStepFinalize, RecipeStepClose}
    for i, s := range stepOrder {
        if s == step {
            rs.CurrentStep = i
            rs.Steps[i].Status = stepInProgress
            if i >= 2 { // discover ran at provision completion
                rs.DiscoveredEnvVars = realisticDiscoveredEnvs()
            }
            if i >= 4 { // outputDir exists by finalize
                rs.OutputDir = "/tmp/zcprecipator/nestjs-showcase"
            }
            return rs
        }
        rs.Steps[i].Status = stepComplete
        rs.Steps[i].Attestation = "test fixture: " + s + " done"
    }
    return rs
}

func realisticDiscoveredEnvs() map[string][]string {
    return map[string][]string{
        "db":      {"hostname", "port", "user", "password", "dbName", "connectionString"},
        "redis":   {"hostname", "port", "password", "connectionString"},
        "queue":   {"hostname", "port", "user", "password", "connectionString"},
        "storage": {"apiHost", "apiUrl", "accessKeyId", "secretAccessKey", "bucketName"},
        "search":  {"hostname", "port", "masterKey", "defaultAdminKey", "defaultSearchKey"},
    }
}
```

**Test assertion**:

```go
func TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap(t *testing.T) {
    t.Parallel()
    store, err := knowledge.GetEmbeddedStore()
    if err != nil {
        t.Fatalf("embedded store: %v", err)
    }
    plan := testDualRuntimePlan()
    plan.Slug = "nestjs-showcase"
    plan.Framework = "nestjs"
    // Worker SEPARATE codebase — match the reshuffled default.
    for i := range plan.Targets {
        if plan.Targets[i].IsWorker {
            plan.Targets[i].SharesCodebaseWith = ""
        }
    }

    for step, cap := range showcaseStepCaps {
        step, cap := step, cap
        t.Run(step, func(t *testing.T) {
            t.Parallel()
            rs := advanceShowcaseStateTo(step, plan)
            resp := rs.BuildResponse("sess-"+step, "Create a NestJS showcase recipe", 0, EnvLocal, store)
            if resp.Current == nil {
                t.Fatalf("no Current on response")
            }
            guide := resp.Current.DetailedGuide
            if len(guide) == 0 {
                t.Fatalf("empty detailedGuide")
            }
            if len(guide) > cap {
                t.Errorf("step %q detailedGuide = %d bytes (%.1f KB), cap = %d bytes — reshuffle regressed", step, len(guide), float64(len(guide))/1024, cap)
            }
        })
    }
}
```

### 0.2 — Delete the old misleading cap test

Remove `TestResolveRecipeGuidance_Generate_ShowcaseUnderSizeCap` from the same file. It measured the wrong thing; the new test subsumes it.

### 0.3 — Verify Phase 0 state

```
go test ./internal/workflow/ -run TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap -v
```

Expected: all 6 subtests RED. The NestJS showcase state at each step exceeds the target cap. This is the baseline — every subsequent phase turns one or more subtests GREEN.

**Do not proceed until all 6 are RED for the expected reason.** If any is GREEN, the cap was set too loose; tighten it.

### 0.4 — Add the content-placement grep test infrastructure

**File**: new `internal/workflow/recipe_content_placement_test.go`

**Purpose**: a set of tests that assert "string X appears in section Y only" to prevent content duplication. These are the correctness gate, separate from the size cap.

Add a helper:

```go
// sectionContent extracts a named <section>...</section> from recipe.md.
func sectionContent(t *testing.T, name string) string {
    t.Helper()
    md, err := content.GetWorkflow("recipe")
    if err != nil {
        t.Fatalf("load recipe.md: %v", err)
    }
    s := ExtractSection(md, name)
    if s == "" {
        t.Fatalf("section %q not found in recipe.md", name)
    }
    return s
}

// assertPresentIn asserts a string appears in exactly the named sections and nowhere else.
func assertPresentIn(t *testing.T, needle string, sections ...string) {
    t.Helper()
    md, _ := content.GetWorkflow("recipe")
    wanted := make(map[string]struct{}, len(sections))
    for _, s := range sections {
        wanted[s] = struct{}{}
    }
    allSections := []string{"research-minimal", "research-showcase", "provision", "generate", "generate-fragments", "deploy", "finalize", "close"}
    for _, s := range allSections {
        body := ExtractSection(md, s)
        has := strings.Contains(body, needle)
        _, shouldHave := wanted[s]
        if shouldHave && !has {
            t.Errorf("needle %q: expected in section %q but missing", needle, s)
        }
        if !shouldHave && has {
            t.Errorf("needle %q: unexpected in section %q (should only appear in %v)", needle, s, sections)
        }
    }
}
```

No test cases yet — each phase adds its own assertions.

**Core.md H2 helper** (needed for Phase 1's Env Vars straddle verification — `assertPresentIn` above works on recipe.md sections only):

```go
// coreH2Section extracts a named H2 block from internal/knowledge/themes/core.md.
// Returns the body of "## {name}" up to the next H2 or EOF.
func coreH2Section(t *testing.T, name string) string {
    t.Helper()
    b, err := os.ReadFile("../knowledge/themes/core.md")
    if err != nil {
        t.Fatalf("read core.md: %v", err)
    }
    re := regexp.MustCompile(`(?m)^## .+$`)
    md := string(b)
    locs := re.FindAllStringIndex(md, -1)
    headers := re.FindAllString(md, -1)
    for i, loc := range locs {
        if strings.TrimSpace(strings.TrimPrefix(headers[i], "##")) != name {
            continue
        }
        start := loc[1]
        end := len(md)
        if i+1 < len(locs) {
            end = locs[i+1][0]
        }
        return md[start:end]
    }
    return ""
}

// assertIdenticalInCoreH2 asserts a substring appears byte-identically in the
// named H2 blocks of core.md. Used to verify the Env Vars straddle duplication
// stays byte-identical across Provision Rules and Generate Rules.
func assertIdenticalInCoreH2(t *testing.T, needle string, h2Names ...string) {
    t.Helper()
    if len(h2Names) < 2 {
        t.Fatalf("assertIdenticalInCoreH2 needs ≥2 sections, got %d", len(h2Names))
    }
    var first string
    for i, name := range h2Names {
        body := coreH2Section(t, name)
        idx := strings.Index(body, needle)
        if idx == -1 {
            t.Errorf("core.md H2 %q: missing expected substring starting %.60q", name, needle)
            continue
        }
        // Capture a reasonable window around the needle for drift detection.
        // Take from the needle start to the next blank line after it.
        end := strings.Index(body[idx:], "\n\n")
        if end == -1 {
            end = len(body) - idx
        }
        chunk := body[idx : idx+end]
        if i == 0 {
            first = chunk
            continue
        }
        if chunk != first {
            t.Errorf("core.md H2 %q: duplicated block drifted from first occurrence in %q — byte-identical required", name, h2Names[0])
        }
    }
}
```

---

## Phase 1 — Split `themes/core.md` Rules & Pitfalls by lifecycle phase

**Goal**: let provision and generate inject different subsets of the platform rules instead of both getting the full 8 KB block.

**Why**: the current `Rules & Pitfalls` H2 is a single 8 KB block covering networking, build & deploy, base image, env vars, import & service creation, import generation, build & runtime, and scaling. At provision, only import & service creation + import generation + env vars + a sliver of scaling actually apply to what the agent is doing. The rest (build & deploy, base image, build & runtime, networking port ranges) fires at generate or deploy.

### 1.1 — Split the H2 block

**File**: `internal/knowledge/themes/core.md`

**Current structure**:

```
## Rules & Pitfalls
### Networking
### Build & Deploy
### Base Image & OS
### Environment Variables — Three Levels
### Import & Service Creation
### Import Generation (dev/stage patterns)
### Build & Runtime
### Scaling & Platform
```

**New structure** — split into three H2 blocks the workflow can inject independently:

```
## Provision Rules
### Import & Service Creation
### Import Generation (dev/stage patterns)
### Environment Variables — Three Levels
### Hostname & Port Conventions     # moved from Networking: the platform-reservation rules only
### Scaling & Platform              # mode immutability, hostname immutability

## Generate Rules
### Build & Deploy                  # buildCommands/initCommands/prepareCommands rules
### Base Image & OS                 # apk vs apt, sudo, alpine/ubuntu specifics
### Build & Runtime                 # 0.0.0.0 binding, proxy trust, CGO_ENABLED, fat JARs
### Deploy Semantics                # tilde syntax, deployFiles path matching, .deployignore

## Runtime Rules                    # the rest: cache architecture, public access, zsc commands
### Cache Architecture (Two-Layer)
### Public Access
### zsc Commands
```

**Content moves**:
- "Networking" block: the Cloudflare Full-Strict rule is deploy-time; move to Generate Rules > Build & Runtime. The port-range rule (10-65435) is provision-time (it's about what to declare in import.yaml). Put it in Provision Rules > Hostname & Port Conventions.
- "Schema Rules > Deploy Semantics" currently lives as its own H2. Merge into Generate Rules > Deploy Semantics.

**The Environment Variables — Three Levels H3 straddle (mandatory handling)**:

The existing `### Environment Variables — Three Levels` subsection inside Rules & Pitfalls ([core.md:151-172](../internal/knowledge/themes/core.md#L151-L172)) is a unified block covering:
- `project.envVariables` (import.yaml, provision concern)
- `envSecrets` (import.yaml per-service, provision concern)
- `run.envVariables` with `${}` cross-service refs (zerops.yaml, generate concern)

You cannot cleanly assign this H3 to either "Provision Rules" or "Generate Rules" — it straddles both lifecycle phases. **The resolution is duplication, not split**:

1. Keep the ENTIRE `### Environment Variables — Three Levels` subsection **byte-identical** in both Provision Rules and Generate Rules. Same 1.5 KB block in two places.
2. The rationale: provision needs the provision-side rules at the moment it's writing import.yaml; generate needs the generate-side rules at the moment it's writing zerops.yaml. Removing either side from either step re-creates the LOG2 bug 7 class.
3. The cost: ~1.5 KB of extra injection at one of the two steps. Provision's cap (14 KB) and generate's cap (32 KB) both accommodate this.
4. The duplication must be **byte-identical** so a grep test can enforce "this substring appears in exactly these two sections" (Phase 0.4's `assertPresentIn` helper).

**Do NOT attempt to split the block into provision-side / generate-side halves.** That requires rewriting the rule text, which violates the "don't edit rule text" constraint and creates drift risk between two now-different phrasings of the same rules.

**Constraint**: do NOT edit any rule text itself. Every line that currently exists must still exist somewhere after the split. If the implementor is tempted to rewrite a rule "while they're in there", stop — that's a separate change with a separate commit. Duplication of the Env Vars H3 is the one sanctioned exception to "every line exists once".

### 1.2 — Update workflow injection to pull the right block per step

**File**: `internal/workflow/recipe_guidance.go`

**Current**:

```go
case RecipeStepProvision:
    if s := getCoreSection(kp, "import.yaml Schema"); s != "" {
        parts = append(parts, "## import.yaml Schema\n\n"+s)
    }
    if s := getCoreSection(kp, "Rules & Pitfalls"); s != "" {
        parts = append(parts, "## Rules & Pitfalls\n\n"+s)
    }
```

**New**:

```go
case RecipeStepProvision:
    if s := getCoreSection(kp, "import.yaml Schema"); s != "" {
        parts = append(parts, "## import.yaml Schema\n\n"+s)
    }
    if s := getCoreSection(kp, "Provision Rules"); s != "" {
        parts = append(parts, "## Provision Rules\n\n"+s)
    }
```

**Current generate**:

```go
case RecipeStepGenerate:
    // ... chain + discoveredEnvs ...
    if !chainInjected {
        if s := getCoreSection(kp, "zerops.yaml Schema"); s != "" {
            parts = append(parts, "## zerops.yaml Schema\n\n"+s)
        }
    }
```

**New generate**: no change to chain/fallback logic, but ADD generate-phase rules injection:

```go
case RecipeStepGenerate:
    // ... chain + discoveredEnvs (unchanged) ...
    if !chainInjected {
        if s := getCoreSection(kp, "zerops.yaml Schema"); s != "" {
            parts = append(parts, "## zerops.yaml Schema\n\n"+s)
        }
    }
    // Generate-phase rules — only the build/deploy/runtime-relevant slice
    // (plus the Env Vars H3 duplicated from Provision Rules). This replaces
    // the prior decision to not inject R&P at generate at all: now the slice
    // is smaller (~5 KB vs 14 KB) and more precisely matches what generate
    // actually does, so the concern about triple-teaching no longer applies.
    if s := getCoreSection(kp, "Generate Rules"); s != "" {
        parts = append(parts, "## Generate Rules\n\n"+s)
    }
```

**Delete the stale comment** at [recipe_guidance.go:157-162](../internal/workflow/recipe_guidance.go#L157-L162):

```go
// DELETE:
// Rules & Pitfalls: NOT injected here. The agent already received the full
// 13KB at provision (one step ago). The chain recipe demonstrates the rules
// in practice. Re-injecting would triple-teach the same lifecycle rules
// (recipe.md static text + chain example + R&P). If the agent needs a
// specific rule, zerops_knowledge is available for on-demand queries.
```

This comment reflected the v8.51.0 decision under the unsplit 14 KB R&P block. After the split, Generate Rules is a ~5 KB slice of build/deploy/runtime-specific content + the duplicated Env Vars H3 — not the full R&P. The "triple-teach" concern no longer applies because provision no longer receives the generate-side rules. Leaving the old comment creates a contradiction between code behavior and inline documentation.

Runtime Rules are injected nowhere by default — they're on-demand via `zerops_knowledge scope="theme" query="core"` if the agent actually needs them (rare, usually for debugging).

### 1.3 — Update the `themes/core.md` test if it grep-asserts the old H2 name

**File**: `internal/knowledge/*_test.go`

Run `grep -rn "Rules & Pitfalls" internal/knowledge/*_test.go internal/workflow/*_test.go` and update any grep expectation to the new names.

### 1.4 — Verify Phase 1

```
go test ./internal/knowledge/ ./internal/workflow/ -run TestRecipe -v
```

Expected:
- `TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap/provision`: should show improvement (was 26.8 KB, drop to ~20 KB — Provision Rules is ~6 KB after the split vs R&P's 14.2 KB). Still above the 14 KB cap — Phase 3 delivers the rest.
- `TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap/generate`: small increase (Generate Rules injection adds ~5 KB that wasn't there before, since the old code explicitly didn't inject R&P at generate). Pre-Phase-1: 48.3 KB. Post-Phase-1: ~53 KB. Phase 4 delivers the cut that offsets this.
- Chain/provision/generate knowledge injection tests (bootstrap-phase tests) should still pass with updated needle strings.

**Phase 1 is a temporary regression for the generate cap.** The size win comes in Phase 4. If you measure after Phase 1 and panic because generate grew, that's expected. Keep going.

Commit this phase as "refactor(knowledge): split core Rules & Pitfalls by lifecycle phase".

---

## Phase 2 — Trim research step

**Goal**: drop research from 15.1 KB to ~10 KB by deleting prose the agent fills from training data. **Both** `research-minimal` AND `research-showcase` need trimming because [recipe_guidance.go:63-67](../internal/workflow/recipe_guidance.go#L63-L67) concatenates them at showcase tier — trimming only research-minimal leaves the research-showcase body (7.5 KB) as an un-movable floor.

### 2.1 — Add failing grep assertions first

**File**: `internal/workflow/recipe_content_placement_test.go`

Add these cases to a new `TestRecipe_ResearchContent` test. Each is currently RED; each turns GREEN as Phase 2's edits land.

```go
// Research should NOT contain form-field prose for fields the agent fills from training data.
forbidden := []string{
    "**Package manager**",      // prose restating what a package manager is
    "**HTTP port**",            // restating what a port is
    "**Build commands**",       // restating what build commands are
    "**Migration command**",    // restating what migrations are
    "**Logging driver**",       // trivial restatement
    "**Needs app secret**",     // field name restatement
    "zerops_knowledge recipe=\"{hello-world-slug}\"",  // reference loading block
    "The research load is for filling the plan form",   // redundant explanation
}
for _, needle := range forbidden {
    if s := sectionContent(t, "research-minimal"); strings.Contains(s, needle) {
        t.Errorf("research-minimal still contains forbidden string %q — should be removed in reshuffle", needle)
    }
}

// Research showcase must retain the classification rule and the 3-test worker rule.
required := []string{
    "Full-stack",                   // classification rule
    "API-first",                    // classification rule
    "sharesCodebaseWith",           // worker field
    "framework's own bundled CLI",  // 3-test rule #1
    "independent dependency manifest", // 3-test rule #2
}
for _, needle := range required {
    if s := sectionContent(t, "research-showcase"); !strings.Contains(s, needle) {
        t.Errorf("research-showcase missing required content %q", needle)
    }
}
```

### 2.2 — Delete the reference-loading block from research-minimal

**File**: `internal/content/workflows/recipe.md`

**Delete** the entire `### Reference Loading` block in `<section name="research-minimal">` (currently lines ~20-45). The chain recipe is auto-injected at generate — the research-time manual load is redundant and invites the agent to pre-read recipes it won't use.

**Delete** the accompanying note block:

> Note: at the generate step, the system automatically injects knowledge from lower-tier recipes...

(This note is only needed because the reference-loading instruction exists. Remove both together.)

### 2.3 — Compress the form-field description blocks

**File**: `internal/content/workflows/recipe.md` — `<section name="research-minimal">`

**Delete these subsections entirely** (the agent's training data covers them):

- `### Framework Identity` — lines describing service type, package manager, HTTP port. The `ResearchData` jsonschema already carries field descriptions; the tool-input schema the agent sees is enough.
- `### Build & Deploy Pipeline` — buildCommands, deployFiles, startCommand, cacheStrategy prose.
- `### Database & Migration` — DB driver, migration command, seed command prose.
- `### Environment & Secrets` — needs app secret, logging driver prose.
- `### Decision Tree Resolution` — five decisions with platform defaults. The agent picks the obvious default every time; the platform doesn't offer meaningful alternatives in practice. **Exception**: keep decision 5 (scaffold preservation) — it's the rule that stops the agent stripping Vite/Tailwind from framework scaffolds. Move that single bullet to the top of the section as a standalone note.

**Replace `### Targets` subsection** with a compressed version that keeps the decision rules without the field catalog:

```markdown
### Targets

Define workspace services based on recipe type:
- **Type 1 (runtime hello world)**: app + db
- **Type 2a (frontend static)**: app only (NO database)
- **Type 2b (frontend SSR)**: app + db
- **Type 3 (backend framework)**: app + db

**Target fields** — see the `RecipeTarget` input schema on `zerops_workflow` for field-level descriptions (`hostname`, `type`, `isWorker`, `role`, `sharesCodebaseWith`). The decisions you make when filling targets:

- **Hostname** — lowercase alphanumeric only. Use conventional names (`app`, `db`, `cache`, `queue`, `search`, `storage`).
- **Type** — pick the **highest available version** from `availableStacks` for each stack.
- **isWorker: true** — set for background/queue workers (no HTTP). Ignored for managed/utility services.
- **role** — `app` / `api` for dual-runtime repo routing. Empty for managed services.
```

### 2.4 — Trim research-showcase's "Additional Showcase Fields" and worker decision

**File**: `internal/content/workflows/recipe.md` — `<section name="research-showcase">`

**Replace `### Additional Showcase Fields`** with a minimal version:

```markdown
### Additional Showcase Fields

Five showcase-only fields on the research plan: `cacheLib`, `sessionDriver`, `queueDriver`, `storageDriver`, `searchLib`. Each is the npm/composer/pip library the framework uses for that feature — pick whatever is idiomatic. The `queueDriver` value is the client library the framework uses to talk to the NATS broker (the showcase provisions NATS as the messaging layer regardless of what the framework's own queue library polls).
```

The existing 500 B paragraph on Laravel Horizon / Rails Sidekiq / Django Celery queue-driver exceptions is wrong content for research — it conflates the client-library choice with the worker-command choice. The worker-command decision belongs in the worker codebase decision block.

**Replace `### Worker codebase decision`** with a tighter version that focuses on the 3-test rule and drops the SHARED/SEPARATE shape re-explanation:

```markdown
### Worker codebase decision

Every showcase has a worker. The worker is always a separate **service**; whether it's a separate **codebase** is a research-step decision on the target via `sharesCodebaseWith`.

**SEPARATE codebase (default)** — leave `sharesCodebaseWith` empty. Worker has its own repo, its own `zerops.yaml`, its own dev+stage pair. This is the normal shape for API-first showcases and any worker consuming from a standalone broker.

**SHARED codebase (opt-in)** — set `sharesCodebaseWith: "{host hostname}"`. One repo, two process entry points in one `zerops.yaml` (the host target's zerops.yaml gets a third `setup: worker` block).

Choose SHARED **only when ALL three tests pass**:

1. **The worker command is the framework's own bundled CLI**, not a generic library call. CLIs that ship with the framework and exist to run the framework's bootstrapped process. Custom entry points (`{packageManager} start worker.{ext}`, `{runtime} worker.{ext}`, any script you had to write) do NOT qualify.
2. **No independent dependency manifest.** Separate `package.json` / `composer.json` / `pyproject.toml` / `go.mod` / `Cargo.toml` disqualifies SHARED.
3. **Cannot run without the app's bootstrap.** Job logic references app-level models, ORM bindings, or framework services that need the framework's config graph.

**When in doubt, SEPARATE.** Generic queue libraries (BullMQ, agenda, etc.) fail test 1 and land on SEPARATE. Cross-runtime sharing is rejected by validation. The 3-repo case (frontend + API + worker, all separate repos, worker and API on the same runtime base) is fully supported — leave `sharesCodebaseWith` empty.

Provision and generate will use this decision to shape the import.yaml, the zerops.yaml files, and the deploy flow. You don't need to think about the mechanics now — just make the decision.
```

Removes ~1 KB of SHARED/SEPARATE shape prose that belongs in provision and generate.

### 2.5 — Verify Phase 2

```
go test ./internal/workflow/ -run "TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap/research|TestRecipe_ResearchContent" -v
```

Expected:
- `research` subtest of the cap test: GREEN (drops from 15.1 KB to ~9-10 KB). Note: showcase tier returns research-showcase (~5 KB after trim) + separator + research-minimal (~2 KB after trim) + plan-context, total ~9 KB under the 10 KB cap.
- `TestRecipe_ResearchContent`: GREEN.
- All other subtests: unchanged (research trim doesn't affect other steps).

If the cap test still fails at research, measure whether it's research-showcase or research-minimal that's the blocker. Phase 2.3 handles minimal; Phase 2.4 handles showcase. If one dropped but the other didn't, check the section boundaries.

Commit: "refactor(recipe): trim research step to decisions only".

---

## Phase 3 — Rebuild provision step around physical actions + add missing content

**Goal**: provision goes from 26.8 KB → ~13 KB. Content the agent doesn't use at provision moves out. Missing content (git config, discover instruction strengthening) moves in. Target revised upward from earlier drafts (was 12 KB) because Rules & Pitfalls is actually 14.2 KB, not 8 KB, and the "Provision Rules" slice retains ~6 KB including the duplicated Env Vars H3.

### 3.1 — Content placement tests (RED first)

**File**: `internal/workflow/recipe_content_placement_test.go`

Add to a new `TestRecipe_ProvisionContent`:

```go
// Git safe.directory for SSHFS mount MUST be documented at provision (where the mount first exists).
assertPresentIn(t, "safe.directory", "provision")

// The zerops_discover instruction must be strengthened with an explicit
// catalog/record step, not just "call this and use the output".
if !strings.Contains(sectionContent(t, "provision"), "catalog the output") {
    t.Error("provision must tell the agent to CATALOG zerops_discover output for reference at generate, not just call the tool")
}

// Managed service env var discovery must be at provision, NOT duplicated in generate.
assertPresentIn(t, "zerops_discover includeEnvs=true", "provision")

// Worker SHARED/SEPARATE shape prose (which parts go where) must NOT be in research — it moved here.
forbiddenInResearch := []string{
    "workerdev",  // shape detail
    "own repo",   // shape detail
    "3-repo case", // shape detail
}
for _, needle := range forbiddenInResearch {
    if strings.Contains(sectionContent(t, "research-showcase"), needle) {
        t.Errorf("research-showcase still contains shape detail %q — should be in provision/generate only", needle)
    }
}
```

### 3.2 — Remove container-state block from provision

**File**: `internal/content/workflows/recipe.md` — `<section name="provision">`

Container state during generate currently lives at the top of the generate section. It's correctly placed there. But if you find any "container state at provision" content in provision, move it to generate — provision's container state is simple ("dev services start RUNNING via startWithoutCode, stage services are READY_TO_DEPLOY") and that one sentence is the whole story.

### 3.3 — Trim framework secrets block

**File**: `internal/content/workflows/recipe.md` — `<section name="provision">`, "Framework secrets" subsection

**Problem**: the current block is ~2 KB with the `base64:` / `hex:` rejection warning expanded for Laravel's APP_KEY shape. The warning is correct but Laravel-specific; compress to a 2-line generic statement.

**Replace**:

```markdown
**Framework secrets**: If `needsAppSecret == true`, determine during research whether the secret is used for encryption/sessions (shared by services hitting the same DB) or is per-service.
- **Shared** (used for encryption, CSRF, session signing — any secret that multiple services must agree on): do NOT add to workspace import (see above — no `project:` allowed). After services reach RUNNING, set the value at project level with `zerops_env` **using the same preprocessor expression the deliverable uses** — zcp expands it locally via the official zParser library before calling the platform API, producing byte-for-byte the same value that the platform's own preprocessor will produce at recipe-import time:
  ```
  zerops_env project=true action=set variables=["{SECRET_KEY_NAME}=<@generateRandomString(<32>)>"]
  ```
  Because zcp uses zParser (the same library the platform uses), the workspace value and the deliverable's `project.envVariables: <@generateRandomString(<32>)>` output values with identical length, alphabet, and byte-per-char encoding. A secret that boots the app at workspace time is guaranteed to boot it at recipe-import time. Services auto-restart so the new value takes effect.

  > **Do NOT prepend `base64:` to the preprocessor expression.** Many frameworks document their shared secret in base64 form (Laravel's `APP_KEY=base64:{44chars}`, etc.) because their `key:generate` outputs that shape. The preprocessor emits a 32-char string from a URL-safe 64-char alphabet (`[a-zA-Z0-9_-.]`), which frameworks accept **directly as the raw key** — Laravel's `Encrypter::supported()` checks `mb_strlen($key, '8bit') === 32`, other AES implementations do the same. Prepending `base64:` tells the framework to DECODE the suffix, turning 32 single-byte chars into ~24 bytes, failing the cipher's fixed-length check. **`zerops_env` rejects `base64:<@...>` and `hex:<@...>` shapes to catch this at set time** — if you see that rejection, drop the prefix.

  `zerops_env set` is **upsert** — calling it with an existing key replaces the value cleanly. No delete+set dance needed if you want to change a secret. The response includes a `stored` list echoing what actually landed on the platform; read it to verify the final value shape matches your expectation (length, prefix, character set).

  For correlated secrets, encoded variants, or key pairs, call `zerops_preprocess` directly — same library, exposes batch + setVar/getVar across keys.
- **Per-service** (unique API tokens, webhook secrets): add as service-level `envSecrets` in import.yaml.
```

**with**:

```markdown
**Framework secrets**: if `needsAppSecret == true`, decide where the secret lives.

- **Shared** (encryption keys, CSRF secrets, session signing keys — anything multiple services must agree on): set at project level after provision completes:
  ```
  zerops_env project=true action=set variables=["{SECRET_KEY_NAME}=<@generateRandomString(<32>)>"]
  ```
  Do NOT wrap the preprocessor expression in `base64:` / `hex:` — `zerops_env` rejects those shapes because frameworks accept the raw 32-char output directly. If your framework's docs show a `base64:` prefix on the secret, drop it. `zerops_env set` is upsert and auto-restarts affected services so the new value takes effect.

- **Per-service** (unique API tokens, webhook secrets): add to `envSecrets` in the import.yaml under that service.

For correlated secrets, encoded variants, or key pairs, call `zerops_preprocess` directly.
```

Drops ~1 KB.

### 3.4 — Add the git config subsection at provision (LOG2 bugs 2, 3, 13)

**File**: `internal/content/workflows/recipe.md` — `<section name="provision">`

**Add** a new subsection after `### 3. Mount dev filesystem`:

```markdown
### 3a. Configure git on the mount (MANDATORY before first commit)

SSHFS mounts surface the container's `/var/www/` to zcp as a root-owned directory. Git treats this as a security risk and refuses to operate on it — both on zcp (where you edit files) and inside the target container (where `zerops_deploy` runs `git push` on first deploy). You must configure both sides BEFORE any commit.

**On zcp (once per mounted service)**:
```
git config --global --add safe.directory /var/www/{hostname}
git config --global user.email "recipe@zerops.io"
git config --global user.name "Zerops Recipe"
```

**On the target container (once per service, before first `zerops_deploy`)**:
```
ssh {hostname} "git config --global --add safe.directory /var/www && git config --global user.email 'recipe@zerops.io' && git config --global user.name 'Zerops Recipe'"
```

Without zcp-side config: `git commit` on the mount fails with `fatal: detected dubious ownership`. Without container-side config: `zerops_deploy` fails with `fatal: not in a git directory`. Both errors are 100% reproducing on first use — do not try to "commit without configuring and see what happens".

For a dual-runtime showcase with 3 codebases (apidev, appdev, workerdev), repeat both commands for each mount.
```

### 3.5 — Strengthen the zerops_discover instruction

**File**: `internal/content/workflows/recipe.md` — `<section name="provision">`, "Discover env vars" subsection

**Replace** the current subsection with:

```markdown
### 4. Discover env vars (mandatory before generate — skip if no managed services)

Run `zerops_discover includeEnvs=true` AFTER services reach RUNNING. The response contains the real env var keys every managed service exposes. **You MUST use the names from this response, not guess them from training data.** Guessed names (`${search_apiKey}` when the real key is `${search_masterKey}`) fail silently — the platform interpolator treats unknown cross-service refs as literal strings, and your app sees `"${search_apiKey}"` as the value at runtime.

**Catalog the output.** Record the list of env var keys for every managed service in the provision-step attestation so the generate step (which writes the zerops.yaml `run.envVariables` using these references) has the authoritative list. Example attestation shape:

```
Services: apidev, apistage, appdev, appstage, workerdev, workerstage, db, redis, queue, storage, search.
Env var catalog:
  db: hostname, port, user, password, dbName, connectionString
  redis: hostname, port, password, connectionString
  queue: hostname, port, user, password, connectionString
  storage: apiHost, apiUrl, accessKeyId, secretAccessKey, bucketName
  search: hostname, port, masterKey, defaultAdminKey, defaultSearchKey
Dev mounts: apidev, appdev, workerdev
```

If a managed service returns a set that surprises you (no `hostname`, or a `key` name you don't recognize), STOP and investigate — do not proceed with guessed names.

**If the plan has no managed services** (type 2a static frontend): skip this step entirely.
```

Critical: the attestation pattern above is what gets recorded in `RecipeState.Steps[provision].Attestation`, which `buildPriorContext` injects into the generate step's response. The generate step can reference "the env var catalog you recorded at provision" instead of re-injecting it.

### 3.6 — Update `formatEnvVarsForGuide` to match the attestation catalog format

**File**: [`internal/workflow/bootstrap_guide_assembly.go`](../internal/workflow/bootstrap_guide_assembly.go#L43)

**Current shape** (verified — live code as of the reshuffle):

```go
func formatEnvVarsForGuide(envVars map[string][]string) string {
    var sb strings.Builder
    sb.WriteString("## Discovered Environment Variables (zerops.yaml wiring — not yet active)\n\n")
    sb.WriteString("**Cross-service references for `run.envVariables` in zerops.yaml. NOT active as OS env vars on the dev container — they activate only after `zerops_deploy`.**\n\n")
    for hostname, vars := range envVars {
        sb.WriteString("**" + hostname + "**: ")
        refs := make([]string, len(vars))
        for i, v := range vars {
            refs[i] = "`${" + hostname + "_" + v + "}`"
        }
        sb.WriteString(strings.Join(refs, ", "))
        sb.WriteString("\n\n")
    }
    return sb.String()
}
```

This is already called from `assembleRecipeKnowledge` at generate ([recipe_guidance.go:147](../internal/workflow/recipe_guidance.go#L147)). The output shape is a per-host bolded list of references — good for "here's what you can reference" but poor for the catalog-attestation pattern the Phase 3.5 provision instruction tells the agent to record.

**Required change**: render both the raw key list (for attestation parity) AND the cross-service reference form (for direct copy-paste). This closes the loop: agent sees the same format at provision (in the attestation the doc tells it to write) and at generate (in the injected catalog).

**New shape**:

```go
func formatEnvVarsForGuide(envVars map[string][]string) string {
    // Stable ordering — iterating a map is non-deterministic, and the agent
    // reading this guide benefits from consistent shape across runs.
    hostnames := make([]string, 0, len(envVars))
    for h := range envVars {
        hostnames = append(hostnames, h)
    }
    sort.Strings(hostnames)

    var sb strings.Builder
    sb.WriteString("## Discovered Managed-Service Env Var Catalog\n\n")
    sb.WriteString("Recorded at provision via `zerops_discover includeEnvs=true`. **These are the authoritative names** — do not guess alternative spellings; unknown cross-service references resolve to literal strings at runtime and fail silently.\n\n")
    sb.WriteString("| Service | Keys | Cross-service reference shape |\n")
    sb.WriteString("|---|---|---|\n")
    for _, hostname := range hostnames {
        vars := envVars[hostname]
        keys := strings.Join(vars, ", ")
        refs := make([]string, len(vars))
        for i, v := range vars {
            refs[i] = "`${" + hostname + "_" + v + "}`"
        }
        sb.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", hostname, keys, strings.Join(refs, " ")))
    }
    sb.WriteString("\n**Usage**: reference these in `run.envVariables` of your app's zerops.yaml (they resolve at deploy time — they are NOT active as OS env vars on a dev container that was started with `startWithoutCode: true`).\n")
    return sb.String()
}
```

**Test update**: whatever test covers `formatEnvVarsForGuide` needs updated expected output. Grep for its test and update the golden fixtures. Add `"sort"` and `"fmt"` to the imports if not already present.

**Why this matters**: the Phase 3.5 provision guidance tells the agent to write an attestation with the shape

```
Env var catalog:
  db: hostname, port, user, password, dbName, connectionString
  ...
```

When generate's injected catalog table uses a different shape (bolded hostname + inline refs), the agent has to mentally translate between the two. Matching the shapes means the agent sees the same table at provision (from its own attestation) and generate (from the auto-injection) — no translation burden, no drift risk.

### 3.7 — Verify Phase 3

```
go test ./internal/workflow/ -run "TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap/provision|TestRecipe_ProvisionContent" -v
```

Expected:
- `provision` subtest: GREEN at ~13 KB (was 26.8 KB). Composition: provision section ~6 KB + import.yaml Schema 4.9 KB + Provision Rules ~1.5 KB (after R&P split, minus generate/runtime halves) + Env Vars H3 duplicate ~1.5 KB — roughly ~13 KB.
- `TestRecipe_ProvisionContent`: GREEN.
- `formatEnvVarsForGuide` tests: GREEN with updated golden fixtures.

Manual check: read the assembled provision guide and confirm the git config + strengthened discover instruction are visible and clear. Read the assembled generate guide and confirm the env var catalog table uses the new shape.

Commit: "refactor(recipe): reshuffle provision — filtered rules, git config, strengthened env discovery".

---

## Phase 4 — Move sub-agent + browser content from generate → deploy

**Goal**: the big move. Sub-agent brief, "where app-level commands run" rule, and all browser-walk narrative are deploy concerns, not generate. Net: generate drops ~9 KB, deploy gains ~7 KB.

### 4.1 — Content placement tests (RED first)

**File**: `internal/workflow/recipe_content_placement_test.go`

Add `TestRecipe_SubAgentBriefPlacement` and `TestRecipe_BrowserWalkPlacement`:

```go
// Sub-agent brief content belongs at deploy, not generate.
assertPresentIn(t, "sub-agent", "deploy", "close")  // deploy dispatches feature sub-agent; close has static review sub-agent
assertPresentIn(t, "Where app-level commands run", "deploy", "close")  // same two places, once each

// Browser walk canonical flow belongs at deploy only. Close REFERENCES it, not duplicates.
assertPresentIn(t, "canonical verification flow", "deploy")
assertPresentIn(t, "Phase 1 — Dev walk", "deploy")
assertPresentIn(t, "Phase 2 — Kill dev processes", "deploy")
assertPresentIn(t, "Phase 3 — Stage walk", "deploy")

// generate-dashboard section should NOT exist any more — content moved to deploy.
md, _ := content.GetWorkflow("recipe")
if ExtractSection(md, "generate-dashboard") != "" {
    t.Error("generate-dashboard section should be removed in Phase 4 — content moves to deploy")
}

// generate section should still have the skeleton write checklist (compressed inline).
if !strings.Contains(sectionContent(t, "generate"), "skeleton") {
    t.Error("generate must still have skeleton-write guidance inline (compressed from old generate-dashboard)")
}
```

### 4.2 — Identify what in generate-dashboard is sub-agent brief vs skeleton instruction

**File**: `internal/content/workflows/recipe.md` — `<section name="generate-dashboard">`

Walk the current content and tag each block:

- `### Required endpoints` — STAYS inline at generate. The agent writes `GET /`, `/health`, `/status` as part of the skeleton. Not sub-agent work.
- `### Feature sections — one per provisioned service` — the SERVICE-TO-FEATURE MAPPING stays at generate (agent writes route stubs per section). The IMPLEMENTATION DETAILS (what each section exercises) move to deploy sub-agent brief.
- `### Dashboard must work on first deploy — verify at deploy Step 3` — MOVE to deploy (it's deploy-step guidance).
- `### Dashboard style — quality bar` — MOVE to deploy sub-agent brief (the sub-agent writes the styled code; main agent writes placeholder slots).
- `### Skeleton boundary — generate vs deploy sub-agent (showcase)` — SPLIT. The table stays at generate (agent needs to know what goes in the skeleton). The narrative explanation ("Why this boundary" etc.) moves to deploy.
- `### Sub-agent brief — what to include` — MOVE entirely to deploy.
- `### Asset pipeline consistency` — STAYS inline at generate. The agent is writing the view NOW and needs the rule.

### 4.3 — Expand the generate section's inline skeleton-write guidance

**File**: `internal/content/workflows/recipe.md` — `<section name="generate">`

**Replace** the current `### Dashboard spec` pointer subsection with a concrete `### Write the dashboard skeleton` subsection:

```markdown
### Write the dashboard skeleton

What you write now (main agent) vs what the sub-agent writes later (deploy step):

| Now (generate — you write this) | Later (deploy sub-agent — do NOT write this now) |
|---|---|
| Layout template with empty partial/component slots per feature section | Feature-section controllers/handlers |
| Placeholder text in each slot ("Section available after deploy") | Feature-section views with interactive UI |
| Primary model + migration + factory + seeder (15-25 records) | Feature-specific JavaScript |
| DashboardController with `/`, `/health`, `/status` endpoints returning real data | Feature-specific model mixins/traits |
| Service connectivity panel (CONNECTED/DISCONNECTED per provisioned service) | |
| All routes registered — GET + POST for every feature action, returning placeholder responses | |
| `zerops.yaml` (every setup), each repo's `README.md` (all 3 fragments), `.env.example` | |

Feature sections map to the plan's targets:

- **Database** — list seeded records + create-record form route
- **Cache** (if provisioned) — store-value-with-TTL route, cached-vs-fresh demonstration
- **Object storage** — upload-file + list-files routes
- **Search engine** — live search route over seeded records
- **Messaging broker + worker** — dispatch-job POST that publishes to a NATS subject the worker consumes; status poll that reads the worker's result

You write the routes (pre-registered with placeholder handlers) and the layout partials that WILL hold each section. The actual controllers and views that exercise the live services come later, when a framework-expert sub-agent runs at the deploy step against running containers.

Endpoint requirements:

- **Server-side (types 1, 2b, 3, 4)**: `GET /` (HTML dashboard), `GET /health` or `GET /api/health` (JSON), `GET /status` (JSON with connectivity checks — DB ping, cache ping, latency).
- **Static frontend (type 2a)**: single `GET /` page with framework name, greeting, timestamp, environment indicator. No server-side health endpoint.

For a single-feature minimal recipe you skip the skeleton/sub-agent split entirely — write everything inline in this step and move on.
```

This is ~1.5 KB inline at generate; the rest moves to deploy.

### 4.4 — Create the deploy step's sub-agent dispatch subsection

**File**: `internal/content/workflows/recipe.md` — `<section name="deploy">`, extend `**Step 4b: Showcase feature sections — MANDATORY for Type 4**`

Current Step 4b is brief (~2 KB). **Expand it** with the sub-agent brief content moved in from `generate-dashboard`:

```markdown
**Step 4b: Dispatch the feature sub-agent (MANDATORY for Type 4 showcase)**

After appdev is deployed and verified with the skeleton (connectivity panel, seeded data, health endpoint), dispatch ONE framework-expert sub-agent to fill in the feature sections. **This is where feature implementation happens — generate writes the skeleton only.** Writing feature code at generate means writing blind against disconnected services; the sub-agent writes against live services and can test each feature as it goes.

**Sub-agent brief — required contents**:

- Exact file paths (framework-conventional locations for controllers, views, partials)
- Installed packages relevant to each feature (from the plan's `cacheLib`, `storageDriver`, `searchLib` etc.)
- Service-to-feature mapping (from the generate-step skeleton)
- **UX quality contract** (see below) — what the rendered dashboard must look like
- Pre-registered route paths the sub-agent must fill (agent wrote them as stubs at generate)
- **Where app-level commands run** (hard rule, see below)
- Instruction to **test each feature against the live service immediately after writing** — the sub-agent has SSH access to appdev and every managed service is reachable. After writing a controller+view, hit the endpoint via `ssh {devHostname} "curl -s localhost:{port}/…"` (or the framework's test runner over SSH) and verify the response. Fix immediately; do not write ahead of verification.

**API-first**: the sub-agent works on BOTH apidev AND appdev mounts (plus workerdev if the worker has a public-facing component). Include every mount path in the brief.

**UX quality contract** (what "dashboard style" means — include verbatim in the sub-agent brief):

The dashboard must be **polished** — minimalistic does NOT mean unstyled browser defaults. A developer deploying this recipe should not be embarrassed.

- **Styled form controls** — never raw browser-default `<input>` / `<select>` / `<button>`. Use scaffolded CSS (Tailwind if present) or clean inline styles: padding, border-radius, consistent sizing, focus ring, button hover
- **Visual hierarchy** — section headings delineated, consistent vertical rhythm, tables with headers + cell padding + alternating row shading
- **Status feedback** — success/error flash after submissions, loading text for async operations, meaningful empty states
- **Readable data** — aligned columns, relative timestamps ("3 minutes ago"), monospace for IDs
- System font stack, generous whitespace, monochrome palette + ONE accent color, mobile-responsive via simple CSS
- **Avoid**: component libraries, icon packs, animations, dark-mode toggles, JS frameworks for interactivity, inline `<style>` alongside a build pipeline
- **XSS protection (mandatory)**: all dynamic content escaped. `textContent` for JS-injected text; framework template auto-escaping for server-rendered content. Never use raw/unescaped output mode.

**Where app-level commands run** (hard rule — include verbatim in the sub-agent brief):

The sub-agent runs on the zcp orchestrator container. `{appDir}` is an SSHFS network mount — a bridge to the target container's `/var/www/`, not a local directory. File reads and edits through the mount are fine. **Target-side commands — anything in the app's own toolchain — MUST run via SSH on the target container**, not on zcp against the mount.

The principle is WHICH CONTAINER'S WORLD the tool belongs to:

- **SSH (target-side)** — compilers (`tsc`, `nest build`, `go build`), type-checkers (`svelte-check`, `tsc --noEmit`), test runners (`jest`, `vitest`, `pytest`, `phpunit`), linters (`eslint`, `prettier`), package managers (`npm install`, `composer install`), framework CLIs (`artisan`, `nest`, `rails`), and any app-level `curl`/`node`/`python -c` that hits the running app or managed services.
- **Direct (zcp-side)** — `zerops_*` MCP tools, `zerops_browser`, Read/Edit/Write against the mount, `ls`/`cat`/`grep`/`find` against the mount, `git status`/`add`/`commit` (with the safe.directory config from provision).

Correct shape:
```
ssh {hostname} "cd /var/www && {command}"   # correct — app's world
cd /var/www/{hostname} && {command}          # WRONG — zcp against the mount
```

Running app-level commands on zcp uses the wrong runtime, the wrong dependencies, the wrong env vars, has no managed-service reachability, AND exhausts zcp's fork budget. Symptom: `fork failed: resource temporarily unavailable` cascades. Recovery is `pkill -9 -f "agent-browser-"` on zcp + waiting for process reaping; the real fix is to stop running target-side commands zcp-side.

**After the sub-agent returns**:
1. Read back feature files — verify they exist and aren't empty
2. Git add + commit on every mount the sub-agent touched (apidev, appdev, workerdev as applicable)
3. Redeploy each affected dev service — fresh container, all SSH processes died, restart them (Step 2)
4. HTTP-level verification via curl on every feature endpoint
5. If anything fails, fix on mount, iterate (counts toward the 3-iteration limit)
```

This is ~4 KB moving in from generate-dashboard.

### 4.5 — Delete the `<section name="generate-dashboard">` entirely

**File**: `internal/content/workflows/recipe.md`

**Delete** the entire `<section name="generate-dashboard">...</section>` block.

**File**: `internal/workflow/recipe_guidance.go`

**Remove** the `generate-dashboard` injection case:

```go
// DELETE:
if planNeedsDashboardSpec(plan) {
    if s := ExtractSection(md, "generate-dashboard"); s != "" {
        parts = append(parts, s)
    }
}
```

Also delete `planNeedsDashboardSpec` helper (no longer called).

**File**: `internal/workflow/recipe_guidance_test.go`

**Delete** `TestResolveRecipeGuidance_Generate_HelloWorldSkipsDashboardSpec` and `TestResolveRecipeGuidance_Generate_ShowcaseKeepsDashboardAndFragments` — they test the removed section. Keep the fragment tests (those stay via Phase 5).

### 4.6 — Trim the browser walk narrative at deploy

**File**: `internal/content/workflows/recipe.md` — `<section name="deploy">`, Step 4c (browser verification)

**Identify** the 3 KB of v4/v5/v6 history narrative (the paragraphs that read like "v4 crashed because... v5 crashed because... the tool exists because..."). **Replace** with a 3-line statement:

```markdown
**Why `zerops_browser` is mandatory** — raw `agent-browser` CLI calls left Chrome running when a batch didn't close cleanly, exhausting the fork budget (crashed v4 and v5), and two parallel calls raced the persistent daemon. The tool auto-wraps `[open url] + your commands + [errors] + [console] + [close]` so close is guaranteed, serializes all calls through a process-wide mutex, and auto-runs pkill recovery on fork exhaustion. Never call `agent-browser` directly from Bash.
```

Keep: rule table (efficient command vocabulary), 3-phase canonical flow, "what to avoid" list.

### 4.7 — Add the `targetService` parameter warning

**File**: `internal/content/workflows/recipe.md` — `<section name="deploy">`, top of Step 1

**Add** a one-line callout:

```markdown
> **Parameter naming**: the deploy parameter is `targetService` (NOT `serviceHostname`). `serviceHostname` is used by `zerops_mount`, `zerops_subdomain`, `zerops_verify`, `zerops_logs`, and `zerops_env` — but deploy is the exception. If you get `unexpected additional properties ["serviceHostname"]`, you used the wrong name.
```

This prevents LOG2 bug 4.

### 4.8 — Add the `zsc execOnce` trap warning

**File**: `internal/content/workflows/recipe.md` — `<section name="deploy">`, Step 3a

**Add** after the existing "never work around missing output" paragraph:

```markdown
**The `zsc execOnce` burn-on-failure trap**: `zsc execOnce` keys on `${appVersionId}`, which doesn't change between retries of the same deploy version. If the first attempt runs the seed, the seed crashes mid-insert, and the container dies — the next retry with the same `appVersionId` will NOT re-run the seed. The platform thinks it already ran. Symptom: the seeder output appears in the FIRST deploy's logs, then is absent on every subsequent retry, and the database contains partial data.

Recovery: either (a) modify something that forces a new `appVersionId` (touch a source file, even a whitespace change, then redeploy — the new version ID makes `execOnce` re-fire), or (b) manually run the seed command via SSH once (`ssh {hostname} "cd /var/www && {seed_command}"`) then redeploy to verify the fix lands. Option (a) is preferred because it preserves the "never manually patch workspace state" rule; option (b) is the escape hatch when the seed depends on a schema that only exists after a successful initCommand run.
```

### 4.9 — Add the Vite port-collision warning

**File**: `internal/content/workflows/recipe.md` — `<section name="deploy">`, Step 2b (asset dev server)

**Add** before the start command example:

```markdown
**Before starting an asset dev server, check if one is already running.** The deploy framework may have started the dev server on first deploy; starting a second instance via background SSH creates a port collision. The second instance silently falls back to an incremented port (Vite: 5173 → 5174), and the public subdomain doesn't route to the new port — it routes to the original.

```
ssh {hostname} "pgrep -f 'vite' || true"    # check for existing Vite
ssh {hostname} "pgrep -f 'npm run dev' || true"
```

If a process is already running, skip the start. If you need to restart (e.g., after a config change), kill first: `ssh {hostname} "pkill -f 'vite' || true"` then start once.
```

### 4.10 — Verify Phase 4

```
go test ./internal/workflow/ -run "TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap|TestRecipe_SubAgentBriefPlacement|TestRecipe_BrowserWalkPlacement" -v
```

Expected:
- `generate` subtest: GREEN or near-GREEN at ~32 KB (was 51 KB). The generate-dashboard block is gone (−9 KB), browser walk narrative trimmed, but generate still has the skeleton-write checklist + per-setup rules + dual-runtime URL pattern + chain injection.
- `deploy` subtest: GREEN at ~32 KB or below. Sub-agent brief added, browser narrative trimmed.
- `TestRecipe_SubAgentBriefPlacement`, `TestRecipe_BrowserWalkPlacement`: GREEN.

If generate is still over cap, the next lever is compressing the per-setup rules block at generate — DO NOT touch chain injection in this phase.

Commit: "refactor(recipe): move sub-agent brief and browser walk from generate to deploy".

---

## Phase 5 — Trim `generate-fragments` and consolidate comment style at finalize

**Goal**: drop the 6 KB writing-style deep-dive from generate-fragments to ~2 KB of hard rules. Move the writing-style voice content to finalize where the agent actually writes the comments that are measured.

### 5.1 — Content placement tests (RED first)

**File**: `internal/workflow/recipe_content_placement_test.go`

```go
// Writing-style voice lecture belongs at finalize, not generate.
voiceMarkers := []string{
    "Voice — three dimensions",
    "Comment shape — match existing recipes exactly",
    "Example of correct style",
}
for _, m := range voiceMarkers {
    if strings.Contains(sectionContent(t, "generate-fragments"), m) {
        t.Errorf("generate-fragments still contains writing-style voice content %q — move to finalize", m)
    }
    if !strings.Contains(sectionContent(t, "finalize"), m) {
        t.Errorf("finalize missing writing-style voice content %q — move from generate-fragments", m)
    }
}

// generate-fragments must still contain the hard structural rules.
hardRules := []string{
    "Comment ratio in zerops.yaml",  // threshold rule
    "blank line required after the start marker",  // fragment syntax
    "H3",  // heading level rule
}
for _, r := range hardRules {
    if !strings.Contains(sectionContent(t, "generate-fragments"), r) {
        t.Errorf("generate-fragments missing hard rule %q", r)
    }
}
```

### 5.2 — Move voice content to finalize

**File**: `internal/content/workflows/recipe.md`

**From**: `<section name="generate-fragments">` → `### Writing Style — Developer to Developer` subsection and its "Voice — three dimensions", "Comment shape", "Example of correct style", and "Anti-patterns" blocks (~4 KB).

**To**: `<section name="finalize">` → add a new `### Comment style (applies to both envComments and zerops.yaml fragments)` subsection containing the moved content.

Why finalize and not generate-fragments: the agent writes the envComments AT finalize, against the ratio check that runs AT finalize. Voice guidance at finalize is in-context. The same rules also apply to the zerops.yaml comments written at generate, but those are briefly stated at generate and the agent can re-fetch the voice content via `zerops_knowledge` if needed (or carry the finalize guidance from reading it once).

### 5.3 — Trim generate-fragments to hard rules only

After the move, `<section name="generate-fragments">` should contain:

- Fragment structure rules (markers, H3 inside/H2 outside, blank line after marker, exactly 3 fragments mandatory)
- Comment ratio hard rule (30% minimum, aim 35%)
- Placeholders forbidden, env var references must use discovered names, comment explains WHY
- Max 80 chars per line

That's ~2 KB. Drop the voice lecture.

### 5.4 — Rename the section (optional, if tests survive)

`<section name="generate-fragments">` is no longer a "fragment deep-dive" — it's now a "fragment structural rules" section. Consider renaming to `<section name="generate-fragment-rules">` and updating the injection. OR leave the name alone to minimize churn; the name is an internal identifier, not user-facing.

### 5.5 — Verify Phase 5

```
go test ./internal/workflow/ -run "TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap|TestRecipe_FragmentsContent" -v
```

Expected:
- `generate` subtest: further improvement (generate-fragments is ~6 KB → ~2 KB inline injection).
- `finalize` subtest: small increase (voice content moves in, ~4 KB added).
- Voice-content placement tests: GREEN.

Commit: "refactor(recipe): move writing-style voice from generate-fragments to finalize".

---

## Phase 6 — Dedupe close step's browser walk

**Goal**: close goes from 17 KB → ~10 KB by referencing deploy's browser walk rules instead of duplicating them.

### 6.1 — Content placement test

**File**: `internal/workflow/recipe_content_placement_test.go`

```go
// The browser walk 3-phase flow exists in deploy only. Close references it.
assertPresentIn(t, "canonical verification flow", "deploy")  // already asserted in Phase 4
assertPresentIn(t, "Phase 1 — Dev walk", "deploy")

// Close must NOT re-duplicate the full 3-phase flow table/example.
closeSection := sectionContent(t, "close")
if strings.Contains(closeSection, "canonical verification flow") {
    t.Error("close should reference deploy's canonical browser flow, not re-duplicate it")
}

// Close must have a clear pointer to deploy step 4c.
if !strings.Contains(closeSection, "Step 4c") {
    t.Error("close must reference deploy's Step 4c for browser walk rules")
}
```

### 6.2 — Replace close's browser-walk content with a reference

**File**: `internal/content/workflows/recipe.md` — `<section name="close">`, `### 1b. Main Agent Browser Walk`

**Replace** the 2.5 KB "Procedure — three phases, strict order" + "Why this order (v6 post-mortem)" block with:

```markdown
### 1b. Main Agent Browser Walk (showcase only — MANDATORY; skip for minimal)

After 1a completes and any redeployments have settled, run the same 3-phase browser walk you ran at deploy Step 4c: Phase 1 (dev walk while dev processes are running) → Phase 2 (kill dev processes via SSH) → Phase 3 (stage walk after dev processes are dead). See deploy **Step 4c: Browser verification** for the full rules, the `zerops_browser` tool usage, the command vocabulary, and the `forkRecoveryAttempted` recovery procedure — they are unchanged at close.

**Close-specific rules** (on top of the deploy-step rules):

- Do NOT delegate browser work to a sub-agent. The 1a static review sub-agent explicitly forbids `zerops_browser` (v5 proved fork exhaustion during a sub-agent's browser walk kills the parent chat). Main agent runs single-threaded.
- Do NOT call `zerops_workflow action="complete" step="close"` until `zerops_browser` has returned clean output (`errorsOutput` empty, all sections populated, `forkRecoveryAttempted: false`) for BOTH the dev walk AND the stage walk AND any regressions surfaced have been fixed and re-verified.
- If a walk surfaces a problem: the tool has already closed the browser, so fix on mount, redeploy the affected target, re-call `zerops_browser` for the affected subdomain. This counts toward the 3-iteration close-step limit.
```

### 6.3 — **BLOCKER** — the CLI is hardcoded to `{slug}-app` and cannot publish multi-repo today

**Verified against live code** at [publish_recipe.go:191](../internal/sync/publish_recipe.go#L191) and [publish_recipe.go:236](../internal/sync/publish_recipe.go#L236):

```go
func CreateRecipeRepo(cfg *Config, slug string, dryRun bool) (PushResult, error) {
    org := cfg.Push.Recipes.Org
    repoName := slug + "-app"   // <-- HARDCODED
    ...
}

func PushAppSource(cfg *Config, slug, appDir string, dryRun bool) (PushResult, error) {
    org := cfg.Push.Recipes.Org
    repoName := slug + "-app"   // <-- HARDCODED
    ...
}
```

Passing `nestjs-showcase-app` as the slug would compute `repoName = "nestjs-showcase-app-app"` (double suffix). There is no way, via the current CLI, to publish a second or third app repo for the same recipe slug.

**You must decide which path to take BEFORE writing the recipe.md content in 6.4**:

**Path A — Scope-limit the reshuffle**: the showcase publishes ONLY the primary `{slug}-app` repo with the full multi-codebase source tree (apidev/, appdev/, workerdev/ as top-level directories inside the single repo). Document this as "for now". The "3-repo showcase" becomes a follow-up tracked in a separate plan.

- Pros: no CLI changes, ships fast, unblocks the rest of the reshuffle.
- Cons: users cloning `{slug}-app` get a confusing multi-dir layout. Deploy-button semantics may still work (the import.yaml at root references the subdirectories) but one-click-deploy from a cleanly-structured single-codebase repo is lost.
- Recipe.md content in 6.4 writes the 1-call form with a note explaining the limitation.

**Path B — Extend the CLI**: add a `--repo-name` flag to `create-repo` and `push-app` that overrides the `slug + "-app"` computation:

```go
func CreateRecipeRepo(cfg *Config, slug, repoNameOverride string, dryRun bool) (PushResult, error) {
    org := cfg.Push.Recipes.Org
    repoName := slug + "-app"
    if repoNameOverride != "" {
        repoName = repoNameOverride
    }
    ...
}
```

Same pattern for `PushAppSource`. Update [cmd/zcp/sync.go:196](../cmd/zcp/sync.go#L196) to parse `--repo-name <name>` flag in both `create-repo` and `push-app` handlers. Add tests covering the override path.

- Pros: clean semantics, each codebase gets its own properly-named repo, matches the product decision.
- Cons: adds ~1 day of CLI work + tests to the reshuffle. Creates a new code path that needs e2e coverage.
- Recipe.md content in 6.4 writes the N-call form using the new flag.

**Make the decision. Write the decision in the commit message of 6.3.** Then proceed to 6.4 matching the chosen path. Do not write recipe.md content for a shape that isn't supported.

If Path B is chosen, the CLI extension lands as part of this phase's commit (alongside the recipe.md content update). If Path A is chosen, the commit is doc-only and a follow-up item is added to `docs/` tracking the CLI extension as future work.

### 6.4 — Write the recipe.md content matching the chosen path

**File**: `internal/content/workflows/recipe.md` — `<section name="close">`, `### 2. Export & Publish`

**Current** `Create app repo and push source` block:

```markdown
**Create app repo and push source**:
```
zcp sync recipe create-repo {slug}
zcp sync recipe push-app {slug} /var/www/appdev
```
Creates `zerops-recipe-apps/{slug}-app` on GitHub, then pushes the app source code.
```

**If Path A (scope-limit)**, replace with:

```markdown
**Create app repo and push source**:

Currently the publish CLI creates a single `{slug}-app` repo containing ALL codebases as top-level subdirectories (`apidev/`, `appdev/`, `workerdev/`). For a dual-runtime showcase, users land on one repo with the multi-codebase layout.

```
zcp sync recipe create-repo {slug}
zcp sync recipe push-app {slug} /var/www/{primary-mount}
```

Where `{primary-mount}` is the top-level mount that contains ALL codebases as subdirectories — typically `appdev` for a single-repo layout, or a wrapper directory created explicitly for publishing. The README at that directory's root should cover the multi-codebase layout.

> **Follow-up**: tracking multi-repo publish as a future CLI extension. See docs/plan-multi-repo-publish.md (TODO after reshuffle merges).
```

**If Path B (CLI extension)**, replace with:

```markdown
**Create app repo(s) and push source**:

Each codebase in a showcase recipe is published as its own GitHub repo so users land on a specific feature view (`{slug}-app` for the frontend, `{slug}-api` for the backend, `{slug}-worker` for the worker). The number of `create-repo` + `push-app` calls equals the number of distinct codebases:

- **Single-runtime + shared worker** (`sharesCodebaseWith` set): 1 repo — `{slug}-app`
- **Single-runtime + separate worker**: 2 repos — `{slug}-app`, `{slug}-worker`
- **Dual-runtime + shared worker**: 2 repos — `{slug}-app`, `{slug}-api` (worker lives inside api)
- **Dual-runtime + separate worker** (3-repo case, default for API-first): 3 repos — `{slug}-app`, `{slug}-api`, `{slug}-worker`

Use `--repo-name` to override the default `{slug}-app` computation for secondary repos:

```
# One pair per codebase — run in parallel as separate tool calls
zcp sync recipe create-repo {slug}                                           # default: {slug}-app
zcp sync recipe push-app {slug} /var/www/appdev

zcp sync recipe create-repo {slug} --repo-name {slug}-api
zcp sync recipe push-app {slug} /var/www/apidev --repo-name {slug}-api

zcp sync recipe create-repo {slug} --repo-name {slug}-worker
zcp sync recipe push-app {slug} /var/www/workerdev --repo-name {slug}-worker
```

Each `push-app` uses the matching codebase's mount path. Each repo ends up with ITS OWN `README.md` (the 3 fragments you wrote at generate), ITS OWN `zerops.yaml`, and ITS OWN source tree — all three repos were committed independently at generate.
```

### 6.5 — Verify Phase 6

```
go test ./internal/workflow/ -run "TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap/close" -v
```

Expected: GREEN at ~10 KB.

Commit: "refactor(recipe): close references deploy's browser walk, clarifies multi-repo push".

---

## Phase 7 — Remove vestigial `import.yaml Schema` injection at finalize

**Goal**: drop finalize from 18 KB → ~12 KB.

### 7.1 — Verify finalize's assembled guide currently contains the schema

```
go test ./internal/workflow/ -run TestMeasureFinalizeContent_Tmp -v
```

(Add a temporary test that prints the full assembled guide for manual inspection, then delete it.)

**Expected**: finalize currently injects `import.yaml Schema` + `Rules & Pitfalls` (Phase 1's split already trimmed R&P down for other steps — check whether finalize still gets the old or new injection).

### 7.2 — Remove finalize's schema injection

**File**: `internal/workflow/recipe_guidance.go`

**Current**:

```go
case RecipeStepFinalize:
    if s := getCoreSection(kp, "import.yaml Schema"); s != "" {
        parts = append(parts, "## import.yaml Schema\n\n"+s)
    }
```

**Remove** this case entirely. At finalize the agent calls `zerops_workflow action=generate-finalize` with structured `envComments` and `projectEnvVariables` input — it does not hand-write YAML. The schema injection is vestigial.

### 7.3 — Verify Phase 7

```
go test ./internal/workflow/ -run "TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap/finalize" -v
```

Expected: GREEN at ~12 KB (was 18 KB; dropped 6 KB schema injection).

Commit: "refactor(recipe): drop vestigial import.yaml Schema injection at finalize".

---

## Phase 8 — Add the dev-server runtime env var rule to generate (LOG2 bug 15)

**Goal**: document the `VITE_API_URL` in `run.envVariables` requirement for `setup: dev` so framework-bundled dev servers read client-side env vars at startup.

### 8.1 — Content placement test

```go
// Generate must document that Vite-family dev servers read process.env at startup,
// meaning client-side env vars belong in run.envVariables on setup: dev, not just build.envVariables.
generateSection := sectionContent(t, "generate")
requiredPhrases := []string{
    "dev server reads",     // any phrasing about dev server evaluating env vars at startup
    "run.envVariables",     // placement instruction
    "setup: dev",           // which setup this applies to
}
for _, p := range requiredPhrases {
    if !strings.Contains(generateSection, p) {
        t.Errorf("generate missing dev-server env var rule phrase %q", p)
    }
}
// The rule must be in the dual-runtime URL pattern section specifically.
```

### 8.2 — Add the rule

**File**: `internal/content/workflows/recipe.md` — `<section name="generate">`, within the "Dual-runtime URL env-var pattern" subsection

**Add** (after the env 0-1 / env 2-5 shape blocks, before the "Consumption" paragraph):

```markdown
**Dev-server runtime env vars — `setup: dev` needs `run.envVariables`**:

Framework-bundled dev servers (Vite, webpack dev server, Next dev, Nuxt dev) read `process.env.VITE_*` / `process.env.NEXT_PUBLIC_*` / equivalent **at dev-server startup**, not at build time. For `setup: dev`, the client-side env vars must be in `run.envVariables` — or they must be passed on the start command line (`VITE_API_URL=$DEV_API_URL npm run dev`). The `build.envVariables` placement is ONLY correct for `setup: prod` because prod builds bake the values into the bundle via a build step that doesn't exist in dev mode.

```yaml
zerops:
  - setup: dev
    run:
      base: nodejs@22
      envVariables:
        # Client-side vars must be in run.envVariables so the Vite/webpack/
        # Next dev server picks them up at startup. build.envVariables is
        # build-time only and dev servers don't have a build step.
        VITE_API_URL: ${DEV_API_URL}
        NODE_ENV: development

  - setup: prod
    build:
      base: nodejs@22
      envVariables:
        # Client-side vars in build.envVariables get baked into the bundle.
        # This is the prod pattern — `npm run build` substitutes at build time.
        VITE_API_URL: ${STAGE_API_URL}
    run:
      base: static
```

Symptom of the wrong placement: the frontend loads in the browser but every `fetch()` call returns HTML (the Vite dev server's own 404 page) instead of JSON. In the browser devtools, `console.log(import.meta.env.VITE_API_URL)` prints `undefined`. This is LOG2's session-breaking bug 15.
```

### 8.3 — Verify Phase 8

```
go test ./internal/workflow/ -run "TestRecipe_DevServerEnvVar" -v
```

Commit: "fix(recipe): document dev-server runtime env var rule (LOG2 bug 15)".

---

## Phase 9 — Fix per-repo README expectation (LOG2 bugs 9, 12)

**Goal**: each codebase in the showcase (`apidev`, `appdev`, `workerdev` as applicable) gets its own README with all 3 fragments. The generate check ALREADY enforces this correctly ([workflow_checks_recipe.go:111-123](../internal/tools/workflow_checks_recipe.go#L111-L123) iterates targets using `{hostname}_readme_exists`) — the failures are in the doc wording AND in three production-code sites that still hardcode `appdev/README.md`. This phase fixes all four.

### 9.1 — Content placement test

```go
// Generate must explicitly state that each codebase needs its own README.
generateSection := sectionContent(t, "generate")
requiredPhrases := []string{
    "each codebase",              // explicit per-codebase wording
    "its own README",             // ownership clarification
    "all 3 fragments",            // fragment count
}
for _, p := range requiredPhrases {
    if !strings.Contains(generateSection, p) {
        t.Errorf("generate missing per-repo README rule phrase %q", p)
    }
}

// The misleading "The API's README.md contains the integration guide" wording must be gone.
forbidden := "The API's README.md contains the integration guide"
if strings.Contains(generateSection, forbidden) {
    t.Errorf("generate still contains misleading wording %q — caused LOG2 bugs 9, 12", forbidden)
}
```

### 9.2 — Fix the wording at generate (runs before 9.3-9.6)

**File**: `internal/content/workflows/recipe.md` — `<section name="generate">`, `### WHERE to write files`

**Find and replace** the paragraph containing "The API's README.md contains the integration guide" with:

```markdown
**Dual-runtime** (API-first showcase): write each codebase to its own mount. For a 3-repo showcase (frontend + API + separate worker), that's three distinct source trees:

- `/var/www/apidev/` — the API framework project (NestJS, Django, Rails, etc.)
- `/var/www/appdev/` — the frontend SPA (Svelte, React, Vue, etc.)
- `/var/www/workerdev/` — the worker project (may be a separate framework project or a minimal runtime script)

**Each codebase needs its own README.md with all 3 extract fragments** (intro, integration-guide, knowledge-base). At publish time, each codebase becomes its own GitHub repo (`{slug}-app`, `{slug}-api`, `{slug}-worker`), and the README you write is what users see when they land on that repo. The integration-guide fragment in each README contains THAT codebase's zerops.yaml, fully commented. The knowledge-base fragment in each README lists the gotchas specific to THAT codebase's role (e.g., the frontend README covers allowedHosts and dev-server runtime env vars; the API README covers CORS and TypeORM synchronize; the worker README covers NATS connection and job idempotency).
```

### 9.3 — Confirm the generate check is already correct

**Verified against live code**: [workflow_checks_recipe.go:85-123](../internal/tools/workflow_checks_recipe.go#L85-L123) already iterates each runtime target and for each:
1. Computes `ymlDir` as `{projectRoot}/{hostname}dev` (or `{hostname}` for simple mode)
2. Looks for `README.md` at `filepath.Join(ymlDir, "README.md")`
3. Emits check `{hostname}_readme_exists` with pass/fail

So the agent receives `appdev_readme_exists`, `apidev_readme_exists`, `workerdev_readme_exists` as independent checks. Each must be satisfied. **No check-side fix needed.**

The LOG2 bug was: the template code only ever WRITES `appdev/README.md`, so for a dual-runtime recipe the `apidev_readme_exists` check fails. Phase 9.4 below fixes the write path.

### 9.4 — Update `BuildFinalizeOutput` to write per-codebase READMEs

**File**: [`internal/workflow/recipe_templates.go`](../internal/workflow/recipe_templates.go#L57)

**Current** (line 55-57):

```go
// App README scaffold (correct markers, deploy button, cover).
// Agent fills in integration-guide and knowledge-base content.
files["appdev/README.md"] = GenerateAppREADME(plan)
```

**New** — iterate runtime targets:

```go
// Per-codebase README scaffolds. Each runtime target with its own codebase
// (sharesCodebaseWith empty) gets its own README at {hostname}dev/README.md,
// so all future repos (published as separate GitHub repos) have matching
// landing docs. The agent fills in integration-guide and knowledge-base
// content for each.
for _, target := range plan.Targets {
    if !isRuntimeTarget(target) {
        continue
    }
    // Skip targets that share a codebase with another target — the host
    // target owns the README, the sharer doesn't get its own.
    if target.SharesCodebaseWith != "" {
        continue
    }
    mountName := target.Hostname + "dev"
    files[mountName+"/README.md"] = GenerateAppREADME(plan) // TODO: vary by target role
}
```

**Helper** (may already exist — grep for `isRuntimeTarget`; if not, add):

```go
func isRuntimeTarget(t RecipeTarget) bool {
    // Runtime = has a runtime base (not managed/storage/search etc.)
    // The simplest test is non-empty RuntimeType or Type matching known runtime stacks.
    // Grep existing code for the canonical predicate and reuse.
    return t.Role != "" || t.IsWorker || t.Type == "" // placeholder — use the real predicate
}
```

**Required**: find the canonical "is this a runtime target with its own codebase" predicate in the existing codebase. Look at how `workflow_checks_recipe.go` decides which targets to iterate for its README check — use the SAME predicate here, so the write and check sides stay in lock-step.

**Note on `GenerateAppREADME(plan)`**: today it returns one template. If the template should differ by role (frontend vs API vs worker), split into `GenerateAppREADME`, `GenerateAPIREADME`, `GenerateWorkerREADME` OR pass the target to `GenerateAppREADME` so it can specialize. For v8.54.0 the minimum is "write a README at every codebase path" — content differentiation is a follow-up unless trivial.

### 9.5 — Update `OverlayRealAppREADME` to overlay every codebase README

**File**: [`internal/workflow/recipe_overlay.go`](../internal/workflow/recipe_overlay.go#L18)

**Current**:

```go
// OverlayRealAppREADME replaces files["appdev/README.md"] with the real
// README from the dev mount, if it exists.
func OverlayRealAppREADME(files map[string]string, plan *RecipePlan) bool {
    // ... reads from a hardcoded appdev/README.md path ...
    files["appdev/README.md"] = content
}
```

**New**:

```go
// OverlayRealREADMEs replaces per-codebase README entries in the finalize
// output with the real READMEs written on each mount during the generate
// step. Returns the number of READMEs overlaid.
func OverlayRealREADMEs(files map[string]string, plan *RecipePlan) int {
    overlaid := 0
    for _, target := range plan.Targets {
        if !isRuntimeTarget(target) || target.SharesCodebaseWith != "" {
            continue
        }
        mountName := target.Hostname + "dev"
        mountPath := filepath.Join("/var/www", mountName, "README.md")
        content, err := os.ReadFile(mountPath)
        if err != nil {
            continue
        }
        files[mountName+"/README.md"] = string(content)
        overlaid++
    }
    return overlaid
}
```

**Rename the old function** and update callers in [engine_recipe.go:336](../internal/workflow/engine_recipe.go#L336) which currently logs `"overlaid appdev/README.md from mount"`. Update that log to report the number of READMEs overlaid:

```go
if overlaid := workflow.OverlayRealREADMEs(files, plan); overlaid > 0 {
    fmt.Fprintf(os.Stderr, "zcp: overlaid %d README(s) from mount\n", overlaid)
}
```

### 9.6 — Update tests

**File**: [`internal/workflow/recipe_templates_test.go`](../internal/workflow/recipe_templates_test.go#L825)

The existing test at line 825 asserts:

> 1 main README + 6 * (import.yaml + README.md) + 1 app README = 14 files.

For a dual-runtime showcase plan with 3 codebases, the expected count becomes `1 + 12 + 3 = 16`. Update the count AND update the per-file assertions:

```go
if _, ok := files["appdev/README.md"]; !ok {
    t.Error("missing appdev/README.md")
}
if _, ok := files["apidev/README.md"]; !ok {
    t.Error("missing apidev/README.md — dual-runtime plan must write per-codebase README")
}
if _, ok := files["workerdev/README.md"]; !ok {
    t.Error("missing workerdev/README.md — separate worker codebase must write its own README")
}
```

Add a single-runtime plan test to confirm it still writes only `appdev/README.md`.

### 9.7 — Verify Phase 9

```
go test ./internal/workflow/ -run "TestRecipe_PerRepoReadme|TestBuildFinalizeOutput" -v
go test ./internal/tools/ -run "TestWorkflowChecksRecipe" -v
go test ./internal/workflow/ -run "TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap/generate" -v
```

Expected:
- Template count tests: updated for dual-runtime plan, GREEN.
- Existing single-runtime tests: still GREEN (backwards compatibility).
- Generate check tests: GREEN — the check side was already correct, the write side now matches.
- Generate cap test: unchanged (wording fix is neutral in size).

Manual check: generate a showcase recipe for a dual-runtime plan end-to-end, confirm the finalize output contains `appdev/README.md`, `apidev/README.md`, `workerdev/README.md` as separate files.

Commit: "fix(recipe): per-repo README requirement and write-side hardcoding (LOG2 bugs 9, 12)".

---

## Phase 10 — Final measurements + document the new caps

### 10.1 — Run the full test suite

```
go test ./... -count=1 -short
make lint-local
```

Every test must pass. No lint issues.

### 10.2 — Manually read every step's assembled guide

For each step, write a temporary test that prints the full `resp.Current.DetailedGuide` and MANUALLY read it top to bottom. You are looking for:

- Any content that no longer makes sense in context (orphaned references to moved content).
- Any duplication that crept back in.
- Any rule that's present but hard to find (needs a heading upgrade).
- Any transition where the agent would logically wonder "what do I do with this".

Delete the temporary tests after reading.

### 10.3 — Final byte counts

Run the cap test with a print statement in the body that logs each step's actual size:

```go
fmt.Printf("[cap test] %s: %d / %d bytes (%.0f%% of cap)\n", step, len(guide), cap, float64(len(guide))/float64(cap)*100)
```

Expected final table (matches Phase 0 caps):

| Step | Current (measured) | Cap | Actual (fill in) |
|---|---|---|---|
| research | 15.1 KB | 10 KB | ? |
| provision | 26.8 KB | 14 KB | ? |
| generate | 48.3 KB | 32 KB | ? |
| deploy | 28.2 KB | 32 KB | ? |
| finalize | 17.5 KB | 14 KB | ? |
| close | 16.1 KB | 12 KB | ? |

If generate is still over 32 KB despite all Phase 4-8 cuts, the remaining lever is:

1. **Move the dual-runtime URL env-var pattern to a deferred-load reference**. The pattern is recipe-unique but the agent only needs it when writing a dual-runtime zerops.yaml — that's a fraction of recipes.
2. **Move the chain recipe injection to a pointer**. Replace the 7 KB auto-injection with "call `zerops_knowledge recipe={framework}-minimal` before writing zerops.yaml". The agent loses 7 KB of context it has to manually reload — tradeoff.

Try (1) first. (2) is the nuclear option.

### 10.4 — Archive the research + implementation guides

After the reshuffle commits are merged, **move** both `docs/research-recipe-workflow-reshuffle.md` and `docs/implementation-recipe-workflow-reshuffle.md` into `docs/archive/`. Add a brief note in the archive README pointing back to the commits that executed the reshuffle.

The documents are reference material for the reshuffle; they're not living docs.

---

## Commit sequence (one per phase)

Each phase is one commit on main. Phases are independent enough that any can be held back if it fails testing.

```
Phase 1: refactor(knowledge): split core Rules & Pitfalls by lifecycle phase
Phase 2: refactor(recipe): trim research step to decisions only
Phase 3: refactor(recipe): reshuffle provision — filtered rules, git config, strengthened env discovery
Phase 4: refactor(recipe): move sub-agent brief and browser walk from generate to deploy
Phase 5: refactor(recipe): move writing-style voice from generate-fragments to finalize
Phase 6: refactor(recipe): close references deploy's browser walk, clarifies multi-repo push
Phase 7: refactor(recipe): drop vestigial import.yaml Schema injection at finalize
Phase 8: fix(recipe): document dev-server runtime env var rule (LOG2 bug 15)
Phase 9: fix(recipe): per-repo README requirement (LOG2 bugs 9, 12)
```

Release as `v8.54.0` after all 9 phases land and pass. `make release`.

---

## Rollback strategy per phase

| Phase | Rollback | Impact |
|---|---|---|
| 1 | Revert Phase 1 commit; provision and generate get the merged R&P block back | Neutral — sizes regress but nothing breaks |
| 2 | Revert Phase 2 commit; research bloat returns | Neutral |
| 3 | Revert Phase 3 commit; provision guidance loses git config and strengthened discover instruction | **Regresses LOG2 bugs 2, 3, 7, 13** — do not rollback without compensating fix |
| 4 | Revert Phase 4 commit; generate bloat returns, deploy loses sub-agent brief | Neutral size; deploy step needs the sub-agent brief back |
| 5 | Revert Phase 5 commit; generate-fragments voice lecture returns, finalize loses comment style | Neutral |
| 6 | Revert Phase 6 commit; close re-duplicates deploy's browser walk | Neutral |
| 7 | Revert Phase 7 commit; finalize re-injects schema | Neutral |
| 8 | Revert Phase 8 commit; dev-server env var rule gone | **Regresses LOG2 bug 15** — do not rollback without compensating fix |
| 9 | Revert Phase 9 commit; per-repo README wording reverts to misleading form | **Regresses LOG2 bugs 9, 12** — do not rollback without compensating fix |

Phases 3, 8, 9 carry bug fixes that cannot simply be reverted. If any of those fail testing, the fix goes forward WITH test corrections, not backward.

---

## Non-goals for this reshuffle (explicit)

These ideas came up in the research document and are deliberately OUT OF SCOPE for this reshuffle. They may be good ideas; they are not THIS change.

- `zerops_env_check` tool that validates env var references in a zerops.yaml against the authoritative catalog before deploy.
- `10303/env/` endpoint integration for "needs restart to apply" detection.
- Splitting the workflow state machine (generate-config + generate-code).
- Deferred-load mechanism for the chain recipe (via `zerops_knowledge` on-demand).
- Framework-specific env var catalog tables injected at provision (Phase 3 strengthens the discover-then-record instruction, which is architecturally simpler).
- `zsc execOnce` trap fix at the platform level.
- `zerops_env set` UserData-lock error message improvement.

Each of these is tracked in the research document's open questions section and is a candidate for a future dedicated change.

---

## Post-reshuffle validation checklist

Once all 9 phases are merged and v8.54.0 is released:

1. Run a fresh NestJS showcase recipe creation end-to-end in a real workspace. Measure:
   - How many tool calls to read any single step's detailedGuide (target: 1 — no persisted-output wrappers, no python heredoc slices).
   - Whether the agent hallucinates any env var name at generate (target: none — it uses the catalog from the provision attestation).
   - Whether git commit + first deploy succeed without debugging (target: yes — git config is in the provision guide).
   - Whether the Vite dev server port collision + VITE_API_URL runtime env var issues occur (target: neither — both are documented at generate/deploy).
   - Whether the feature sub-agent dispatches cleanly at deploy step 4b (target: yes — the brief is in deploy, not generate).
   - Whether all 3 READMEs are written at generate (target: yes — the per-repo requirement is explicit).

2. Walk the session timeline and identify any NEW failures the reshuffle introduced. For each, file as a separate bug against the reshuffle release, NOT retroactively as a reshuffle scope item.

3. Measure the new v8.54.0 assembled guide sizes and compare to v8.53.0. Every step should be strictly smaller or the same. If any step grew, that's a regression — investigate.

The reshuffle ships IFF (1) through (3) all pass on a live NestJS showcase run. Unit tests and lint are necessary but not sufficient.
