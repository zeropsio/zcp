# v27 First-Principles Implementation Guide

**Audience**: Opus implementor picking this up cold. Assume you have not watched the v27 run. This guide is self-contained: it frames the problems, derives fixes from first principles, specifies the changes by file + structure, and lists the verification bar.

**Read order if you're cold**:
1. §1 — what v27 produced (the symptom)
2. §2 — three first-principles breaks (the why)
3. §3 — fix set, in priority order (the how)
4. §4 — verification against v25 baseline + new regression guards (the checks)
5. §5 — explicit non-goals (what this release does NOT do)

---

## 1. What v27 produced

nestjs-showcase v27 shipped as a **single-codebase recipe with one runtime (`app`) and a shared-codebase worker** (`sharesCodebaseWith: app`). Published tree contains only `appdev/` — no `apidev/`, no separate frontend SPA `appdev/`, no separate-codebase `workerdev/`.

This is the wrong shape. nestjs-showcase v6 → v25 (21 consecutive runs, multiple models) all shipped as **API-first dual-runtime + separate-codebase worker (3-repo)**:

- `apidev` = NestJS JSON API
- `appdev` = Svelte SPA frontend
- `workerdev` = standalone NATS consumer in its own repo

The v27 shape collapse cascaded into:
- 28 min of sequential Writes in main context (scaffold done in-main because `recipe.md:466` routes single-codebase to "write yourself")
- 4 min recovery from eager-client-construction bugs (scaffold code crashed on bootstrap with undefined env vars)
- 3 zerops.yaml edits during deploy (initCommands missing, npm run build missing from dev buildCommands, worker setup missing SEARCH_* env vars)
- 7 min close-step browser-walk chaos ending with `pkill -f 'chrome'` killing the user's code-server editor
- 100 min wall-clock total for 1 codebase (v25: 71 min for 3 codebases = 4× slower per-codebase)
- 2 Agent dispatches total (feature + code-review), vs v25's 7 and v20's 10

---

## 2. Three first-principles breaks

### Break 1 — Classification is framework-identity, not predecessor-behavior

The rule in `internal/content/workflows/recipe.md:83` says:

> Classification rule: if the predecessor hello-world/minimal recipe renders HTML via a framework-integrated template engine, it is full-stack. If the predecessor only returns JSON or plain text, it is API-first.

This is wrong in the frame. Showcase classification is about **what the framework IS when used idiomatically for a multi-service dashboard** — not about what its hello-world happened to do. A hello-world returns JSON because hello-world is *one endpoint*. That tells you nothing about whether a showcase-scale app would be rendered by the framework or by a separate SPA.

- Laravel's showcase ships with Blade templates because that's Laravel's idiom.
- Rails' showcase would use ERB because that's Rails'.
- NestJS's showcase uses a separate SPA because NestJS is an API framework with no idiomatic templating layer (yes, `@nestjs/platform-express + hbs` is wireable, but it is not idiomatic — it is a workaround).

The classifier question is a static property of the framework. It should be declared, not inferred. The v27 agent's failure mode — pattern-matching off `laravel-showcase` loaded as a "reference" — is downstream of the classifier being inferrable at all.

### Break 2 — Delegation shape is coupled to codebase count, but the actual goal is "keep authoring out of main context"

The rule in `internal/content/workflows/recipe.md:466` says:

> for showcase multi-codebase plans dispatch scaffold sub-agents in parallel per the scaffold-subagent-brief topic; for everything else write yourself.

This couples "delegate" to "parallelism opportunity exists". But the actual goal of delegating scaffold is to prevent main-context from absorbing 20+ sequential file writes. Single-codebase showcase still benefits from one scaffold subagent — main keeps orchestrator state clean, one subagent batches the ~20 Writes, same wall cost as multi-codebase parallel dispatch on a per-codebase basis.

The routing rule is a premature optimization. It trades main-context hygiene for a "don't bother with the dispatch overhead for one codebase" saving that doesn't exist (dispatch overhead is ~1s, sequential in-main Writes cost 20 min).

### Break 3 — zerops.yaml written at generate has no cross-reference to the code that will run

Main authors zerops.yaml from templates. Templates don't know:
- what entities exist (→ whether `dist/migrate.js` exists → whether dev initCommands need to run migrate)
- what env vars the scaffold imports (→ whether worker setup needs SEARCH_*)
- whether TypeScript compilation is required (→ whether dev buildCommands need `npm run build`)

Generate emits a zerops.yaml, moves on. Deploy discovers the gaps one by one. v27 ran 3 deploy-time edits of the same file (09:27:19, 09:45:42, 09:53:33) — each one fixing a contract the generate step should have resolved against the scaffold it had just written.

The first-principles framing is: **zerops.yaml at generate-complete must be consistent with the codebase that generate produced**. Generate knows both sides of that contract at that moment. Not checking it at that moment is the bug.

---

## 3. Fix set in priority order

Three fixes. Each is framework-agnostic (no per-framework lookup tables), upstream (no new content-check machinery, consistent with the v20-substrate rollback principle), and independently verifiable.

### Fix 1 — Static `showcaseShape` on predecessor recipes + validator gate

**Scope**: `internal/knowledge/recipes/*-minimal.md` frontmatter + `RecipePlan` schema + research-complete validator.

**Changes**:

1. Add `showcaseShape` to `*-minimal.md` frontmatter. Values: `"api-first"` or `"full-stack"`. One-line rationale required.

   ```yaml
   ---
   description: "A minimal NestJS application..."
   repo: "https://github.com/zerops-recipe-apps/nestjs-minimal-app"
   showcaseShape: api-first
   showcaseShapeRationale: "NestJS is an API framework with no idiomatic templating layer. Showcases pair it with a separate SPA."
   ---
   ```

   Initial tagging: `nestjs-minimal: api-first`, `laravel-minimal: full-stack`, `django-minimal: full-stack`, `rails-minimal: full-stack`, `go-hello-world: api-first`, `bun-hello-world: api-first`, `nextjs-ssr-hello-world: full-stack`, `analog-ssr-hello-world: full-stack`, `nuxt-ssr-hello-world: full-stack`, `rust-hello-world: api-first` (case-by-case, not inferred — framework author/maintainer decides). `*-static-hello-world.md` doesn't need the tag (not a framework, not a showcase predecessor).

2. Add `ShapeClassification` field to `RecipePlan` in the MCP schema:

   ```go
   type RecipePlan struct {
       ...existing fields...
       ShapeClassification string `json:"shapeClassification" jsonschema:"enum=api-first,enum=full-stack,description=Required for showcase recipes. Must match the predecessor {framework}-minimal recipe's showcaseShape frontmatter. Rejected with INVALID_PARAMETER if mismatched."`
       ShapeClassificationReason string `json:"shapeClassificationReason" jsonschema:"description=One-sentence citation of the evidence from predecessor. Example: 'nestjs-minimal declared api-first because NestJS has no idiomatic template engine.'"`
   }
   ```

3. Validator in `internal/ops/research.go` (or wherever `complete step=research` runs):

   ```go
   func validateShapeClassification(plan RecipePlan, predecessor KnowledgeRecord) error {
       if plan.RecipeType != "showcase" {
           return nil  // only showcases carry this gate
       }
       if plan.ShapeClassification == "" {
           return platform.Error(platform.INVALID_PARAMETER,
               "showcase plans require shapeClassification (api-first or full-stack)")
       }
       if predecessor.ShowcaseShape == "" {
           return platform.Error(platform.INVALID_PRECONDITION,
               fmt.Sprintf("predecessor %s has no showcaseShape frontmatter; tag it before submitting a showcase plan",
                   predecessor.Slug))
       }
       if plan.ShapeClassification != predecessor.ShowcaseShape {
           return platform.Error(platform.INVALID_PARAMETER,
               fmt.Sprintf("shapeClassification=%q contradicts predecessor %s showcaseShape=%q (rationale: %s)",
                   plan.ShapeClassification, predecessor.Slug, predecessor.ShowcaseShape, predecessor.ShowcaseShapeRationale))
       }
       return nil
   }
   ```

4. Validator also enforces target-set consistency:

   ```go
   func validateTargetsMatchShape(plan RecipePlan) error {
       switch plan.ShapeClassification {
       case "full-stack":
           // must have exactly one non-worker runtime target, no role=api/app split
           // worker target must exist with isWorker=true
       case "api-first":
           // must have app (type: static OR role: app) + api (role: api) + worker
           // both app and api must be non-worker runtime targets
       }
       return nil
   }
   ```

5. Update `internal/content/workflows/recipe.md` §research-showcase:
   - Remove lines 76-83 (the predecessor-behavior classifier rule).
   - Replace with: "**Read the predecessor's `showcaseShape` frontmatter field. Declare it in your plan as `shapeClassification`. The validator rejects mismatch. Do not infer — the predecessor's tag is the source of truth.**"
   - Tighten the cross-recipe-load rule at line 64: "**Load ONLY `{framework}-minimal`. Do not load any other framework's minimal or showcase as a pattern reference. Each framework's showcase shape is self-declared — pattern-matching off another framework's shape is a common failure mode and is not permitted.**"

**Tests (RED first)**:

- `internal/ops/research_test.go`:
  - `TestComplete_Research_Showcase_MissingShapeClassification_Rejects`
  - `TestComplete_Research_Showcase_ShapeMismatch_Rejects` (predecessor api-first, plan full-stack)
  - `TestComplete_Research_Showcase_ShapeMatch_Accepts`
  - `TestComplete_Research_Minimal_ShapeClassification_NotRequired`
  - `TestComplete_Research_Showcase_FullStackShape_RejectsDualRuntimeTargets`
  - `TestComplete_Research_Showcase_APIFirstShape_RejectsSingleCodebaseSharedWorkerTargets`
- `internal/knowledge/engine_test.go`:
  - `TestKnowledge_MinimalRecipes_AllCarryShowcaseShape` — sweep all `*-minimal.md` files, fail if any lacks `showcaseShape`. This is the "don't forget to tag future frameworks" regression guard.

**Rollback safety**: if a predecessor hasn't been tagged yet, the validator returns `INVALID_PRECONDITION` with an explicit "tag it first" message — the user knows what's missing. It does not silently pass.

---

### Fix 2 — Single-codebase scaffold dispatches one subagent, not zero

**Scope**: `internal/content/workflows/recipe.md` generate section + scaffold-subagent-brief topic.

**Changes**:

1. Revise recipe.md:466 to:

   ```
   1. Scaffold the project (composer create-project, npx create-next-app, framework init, etc.) —
      for ALL showcase plans, dispatch one scaffold sub-agent per codebase per the scaffold-subagent-brief
      topic. Single-codebase plans dispatch one subagent; dual-runtime plans dispatch two or three in parallel.
      Main agent does not author scaffold files — scaffold content absorbs ~20+ file writes and cannot live
      in main context without inflating wall time proportionally.
   ```

2. Update scaffold-subagent-brief topic (`internal/workflow/recipe_topic_registry.go` → topic body):
   - Add rule: "**Lazy client construction mandatory.** Any managed-service client (PostgreSQL pool, Redis client, NATS connection, Meilisearch client, S3 SDK) MUST be constructed lazily — either in an on-demand `connect()` method, or wrapped in a null-safe getter. Clients that construct eagerly in module load or class ctor crash when env vars are not set. Dev container has `run.envVariables` applied only after the first deploy completes; your scaffold's smoke test runs BEFORE that."
   - Teach the failure mode: "Symptom: `Cannot read properties of undefined`, `getaddrinfo ENOTFOUND undefined`, `MeiliSearchError: The provided host is not valid`, or equivalent on dev-server start. Fix: move client construction into the first method call that uses it, guarded by a cached instance."

3. Generate step-entry guide distinguishes the two dispatch patterns:
   - Single-codebase → dispatch 1 scaffold subagent for `{appdev}`
   - Dual-runtime separate-codebase worker → dispatch 3 scaffold subagents for `{apidev, appdev, workerdev}`
   - Dual-runtime shared-codebase worker → dispatch 2 scaffold subagents for `{apidev, appdev}`
   - (Recipe.md already has a shape table under "zerops.yaml — Write ALL setups at once"; reference it here, don't duplicate.)

**Tests**:
- `internal/workflow/recipe_guide_test.go`:
  - `TestGenerateEntry_ShowcaseShape_SingleCodebase_DispatchesOneScaffoldSubagent`
  - `TestGenerateEntry_ShowcaseShape_DualRuntime_DispatchesTwoScaffoldSubagents`
  - `TestGenerateEntry_ShowcaseShape_DualRuntimeSeparateWorker_DispatchesThreeScaffoldSubagents`
- `internal/workflow/recipe_topic_registry_test.go`:
  - `TestScaffoldSubagentBrief_ContainsLazyClientConstructionRule`
  - `TestScaffoldSubagentBrief_ListsEagerCtorFailureSymptoms`

**No new check, no new dispatch gate.** Pure brief-content + routing clarification. Consistent with rollback principle.

---

### Fix 3 — `zerops.yaml` static completeness check at generate-complete

**Scope**: new file `internal/ops/zerops_yaml_completeness.go` + call site in `complete step=generate` handler.

**What it checks** (framework-agnostic — parses YAML + scans source tree, no per-framework tokens):

1. **Dev setup has initCommands if corresponding build artifacts exist.**
   - If scaffold tree contains `dist/migrate.*` / `migrate.js` / `migration/` / `migrations/` / any entity file OR `seed.*` / `db/seed*`, AND zerops.yaml's dev setup has no `initCommands`, fail with: `"dev setup has no initCommands but scaffold produced migration/seed artifacts at {paths}; dev deploy will run without applying schema"`
   - Logic: pattern-match on file path existence, not on framework syntax.

2. **Dev `buildCommands` produces artifacts referenced by `initCommands` or `start`.**
   - If `initCommands` or `start` contains `node dist/` / `./dist/` / `./build/` / `target/` / `bin/` path tokens AND dev `buildCommands` doesn't contain `npm run build` / `bun run build` / `go build` / `cargo build` / `mvn package` / equivalent, fail with: `"dev buildCommands={actual} does not produce {referenced-path} that dev initCommands/start reference"`

3. **Every `setup` block's env vars cover what its code reads.**
   - Parse zerops.yaml: enumerate each setup's effective env var names.
   - Scan scaffold tree: find all `process.env.X` / `os.Getenv("X")` / `ENV['X']` / `$_ENV['X']` / equivalent reads.
   - For each setup, determine which files run under that setup (main.ts for dev/prod, worker.ts for worker — by path convention `*/worker*` → worker setup, `*/main*` or `index.ts` → dev/prod).
   - Fail if a worker-file env read is not covered by worker setup's env vars.

**Framework-agnostic design principle**: this check reads file extensions, path patterns, and string tokens. It does NOT know NestJS from Laravel. The rules are "if the scaffold tree has these shape tokens, the zerops.yaml must have these cross-referencing tokens." Pure shape-consistency, no framework lookup table.

**API**:

```go
type CompletenessIssue struct {
    Severity string  // "fail" — this is a gate, not informational
    Rule     string  // e.g. "dev_init_commands_missing"
    Detail   string  // human-readable
    Evidence []string // paths / tokens observed
}

func CheckZeropsYamlCompleteness(scaffoldRoot string, zeropsYamlPath string) ([]CompletenessIssue, error)
```

**Call site**: `internal/tools/workflow.go` handler for `complete step=generate`, BEFORE returning the deploy step-entry guide. If any issue returned, reject with:

```json
{
  "code": "GENERATE_INCOMPLETE",
  "error": "zerops.yaml completeness check failed",
  "issues": [...],
  "suggestion": "Fix zerops.yaml to cover the listed cases, then resubmit complete step=generate"
}
```

**Tests (RED first)**:
- `internal/ops/zerops_yaml_completeness_test.go`:
  - `TestCheck_DevMissingInitCommands_WhenMigrationExists_Fails`
  - `TestCheck_DevBuildCmdsMissingBuild_WhenInitRefersToDist_Fails`
  - `TestCheck_WorkerSetupMissingEnvVar_WhenWorkerCodeReadsIt_Fails`
  - `TestCheck_CleanZerops_Passes` (v25's zerops.yaml reproduction as fixture)
  - `TestCheck_V27Reproduction_CatchesAllThreeClasses` — embed v27's initial zerops.yaml + scaffold tree as a fixture; assert 3 fails fire (initCommands, build, worker env).

**What this check does NOT do**:
- Does not validate content quality (no comment-ratio, no specificity) — that's rolled-back machinery territory.
- Does not classify gotcha authenticity — out of scope.
- Does not enforce framework idioms — framework-agnostic by design.

This is the minimal gate to prevent the class of defect v27 ran into three times in one session.

---

### Fix 5 — Chrome hard-reset in `RecoverFork` + `forceReset` input + CDP-timeout auto-recovery

**Scope**: `internal/ops/browser.go` + `internal/ops/browser_test.go`.

**Root cause of v27's 7-minute browser chaos**: `RecoverFork` uses `pkill -9 -f 'agent-browser-'` which matches the daemon and its `agent-browser-chrome-*` helpers, but does NOT match the actual Chrome binary (`chrome`, `chromium`, `headless_shell`, etc.). When Chrome wedges behind a stuck CDP connection but the daemon is still alive, the current recovery kills the daemon, Chrome hangs on, the next daemon attaches to the same wedged Chrome, and the whole cycle repeats. The agent's fallback of running `pkill -f 'chrome'` from Bash matched code-server's `--no-chrome` CLI arg and killed the user's editor.

**Discovery**: `agent-browser` writes its daemon PID to `~/.agent-browser/default.pid` and exposes a Unix socket at `~/.agent-browser/default.sock`. A proper reset reads the pidfile, kills the process GROUP (which captures Chrome as a child without needing to guess its binary name), removes the stale files so the next call starts fresh, then falls back to pattern-matching cleanup for processes that escaped the group.

**Changes**:

1. Rewrite `RecoverFork` for proper process-group kill + `pkill --exact` fallback:

   ```go
   func (execBrowserRunner) RecoverFork(ctx context.Context) {
       pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
       defer cancel()

       // Attempt 1: read daemon pidfile, kill its process group.
       // Process-group kill (negative PID) captures Chrome and all helpers
       // regardless of their binary names. No guessing chrome/chromium/
       // headless_shell/google-chrome variants.
       if home, err := os.UserHomeDir(); err == nil {
           pidPath := filepath.Join(home, ".agent-browser", "default.pid")
           if b, err := os.ReadFile(pidPath); err == nil {
               if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
                   _ = syscall.Kill(-pid, syscall.SIGKILL)  // process group
                   _ = syscall.Kill(pid, syscall.SIGKILL)   // the daemon itself
               }
           }
           // Remove stale pidfile + socket so next daemon starts fresh.
           _ = os.Remove(filepath.Join(home, ".agent-browser", "default.pid"))
           _ = os.Remove(filepath.Join(home, ".agent-browser", "default.sock"))
       }

       // Attempt 2: pattern fallback for anything that escaped the group.
       // --exact matches only on process basename (argv[0]), NEVER on
       // command-line args. Critical: this means code-server's `--no-chrome`
       // CLI flag is never matched, so the v27 editor-kill cannot recur.
       _ = exec.CommandContext(pctx, "pkill", "-9", "-f", "agent-browser-").Run()
       for _, name := range []string{"chrome", "chromium", "chromium-browser", "google-chrome", "headless_shell"} {
           _ = exec.CommandContext(pctx, "pkill", "-9", "--exact", name).Run()
       }
   }
   ```

2. Add `ForceReset` to `BrowserBatchInput`:

   ```go
   type BrowserBatchInput struct {
       URL             string     `json:"url"`
       Commands        [][]string `json:"commands,omitempty"`
       TimeoutSeconds  int        `json:"timeoutSeconds,omitempty"`

       // ForceReset, when true, runs RecoverFork BEFORE the batch — fully
       // kills any existing agent-browser daemon and Chrome process tree,
       // waits postRecoveryGrace for kernel reap, then starts fresh.
       // Use this when a previous call timed out, returned
       // "CDP command timed out", or reported forkRecoveryAttempted=true
       // without the retry succeeding. Adds ~2s pre-roll; do not enable
       // on every call — it defeats the persistent-daemon fast path.
       ForceReset bool `json:"forceReset,omitempty" jsonschema:"description=Force full reset of agent-browser daemon + Chrome before starting. Use after CDP-timeout or repeat-recovery failures."`
   }
   ```

3. Apply reset at batch start when requested:

   ```go
   // In BrowserBatch, after mutex acquire + before LookPath check:
   if input.ForceReset {
       browserRun.RecoverFork(ctx)
       time.Sleep(postRecoveryGrace)
   }
   ```

4. Auto-trigger recovery on CDP-timeout signals even when steps parsed:

   ```go
   // After the existing json.Unmarshal block around line 362:
   if runErr != nil && len(result.Steps) > 0 {
       for _, step := range result.Steps {
           if step.Error == nil {
               continue
           }
           errMsg := *step.Error
           if strings.Contains(errMsg, "CDP command timed out") ||
              strings.Contains(errMsg, "Target closed") ||
              strings.Contains(errMsg, "Protocol error") {
               browserRun.RecoverFork(ctx)
               result.ForkRecoveryAttempted = true
               recoveryNeeded = true
               result.Message = "Chrome wedged behind CDP (signal: " + errMsg + "). " +
                   "Full reset ran automatically. Retry with forceReset=true if this recurs."
               break
           }
       }
   }
   ```

5. Update tool description in `internal/tools/browser.go` to teach `forceReset` usage:

   > "Drive Chrome via agent-browser in ONE bounded batch. If a prior call returned `forkRecoveryAttempted: true` without the retry succeeding, OR returned `CDP command timed out` in step errors, pass `forceReset: true` on the next call to fully clean the daemon + Chrome state before starting. Never use raw Bash `pkill -f chrome` — that pattern matches unrelated processes and has killed code-server instances in past runs."

6. Update `internal/content/workflows/recipe.md` close-step browser-walk block:

   - Remove the `pkill -9 -f "agent-browser-"` suggestion (obsolete — tool handles it).
   - Replace with: "If `zerops_browser` returns a message containing `forkRecoveryAttempted` or `Chrome wedged`, retry the SAME call with `forceReset: true`. Do NOT run raw `pkill` from Bash — `pkill -f 'chrome'` will match code-server's `--no-chrome` CLI arg and kill the user's editor. This has happened in prior runs."

**Tests (RED first)** in `internal/ops/browser_test.go`:

- `TestRecoverFork_ReadsPidfileAndKillsProcessGroup` — fake `~/.agent-browser/default.pid`, assert syscall.Kill called with negative PID
- `TestRecoverFork_RemovesStalePidfileAndSocket` — assert files deleted after recovery
- `TestRecoverFork_FallbackPkillUsesExactFlag` — spy on exec.Command, assert `--exact` flag present on chrome-variant pkills (NOT `-f`)
- `TestBrowserBatch_ForceReset_CallsRecoverForkBeforeRun` — assert recover called once, then Run, with grace sleep
- `TestBrowserBatch_CDPTimeoutInSteps_TriggersRecovery` — scripted stdout with a step error "CDP command timed out: DOM.enable", assert recoverCalls == 1
- `TestBrowserBatch_TargetClosedInSteps_TriggersRecovery` — same shape for "Target closed"
- `TestBrowserBatch_ForceReset_AddsPreRollGrace` — assert total elapsed duration ≥ grace + Run duration

**What this does NOT touch**: the mutex contract, the canonical batch shape, the 1 MiB output cap, or the ctx-aware lock acquisition. Pure expansion of the recovery mechanism.

**Expected effect on v28 close-step browser-walk**:

- First call wedges → tool returns `forkRecoveryAttempted: true` + CDP-timeout message
- Second call with `forceReset: true` → pidfile-based group kill + socket cleanup + fresh daemon → succeeds
- Total close-browser-walk wall: ~2-3 min (was 7 min in v27)
- Zero risk of killing the user's editor (no raw `pkill -f 'chrome'` anywhere in the stack)

---

### Fix 4 — RecipePlan stringification schema error upgrade (cleanup)

**Scope**: `internal/tools/workflow.go` error handler for MCP jsonschema validation.

When the validator returns `type: {json-looking-content}` for a field typed as object, wrap the error with a structured explanation:

```go
// in the MCP tool input-validation error path
if strings.Contains(errMsg, `validating /properties/recipePlan: type: {`) {
    return platform.Error(platform.INVALID_PARAMETER,
        `recipePlan must be passed as a JSON object, not a JSON-formatted string. `+
        `Example: recipePlan: { "slug": "...", ... }  NOT  recipePlan: "{\"slug\":\"...\"}"`,
    )
}
```

Generalize to any `/properties/{field}` that the tool's schema typed as `object` but received `string`.

**Test**:
- `internal/tools/workflow_validator_test.go`:
  - `TestSchemaError_StringifiedObjectField_EmitsFriendlyRemediation`

Minor cleanup. Prevents the 35-second two-retry stringification cost every time a model instance hits this footgun.

---

## 4. Verification bar

### Must-pass (regression)

- `make lint-local` clean
- Full test suite green with `-race`
- `make test` includes the new RED tests from §3
- v25 baseline smoke: run `zcp` against a mock mirror of the v25 recipePlan — must accept (shapeClassification=api-first matches nestjs-minimal.md showcaseShape=api-first)

### Must-fail (v27 regression guard)

- Submit v27's recipePlan (shapeClassification omitted, shape=full-stack shared-worker targets) → validator must reject with clear error
- Submit a recipePlan with shapeClassification="full-stack" for nestjs-minimal predecessor → validator must reject with predecessor-mismatch error
- Replay v27's initial zerops.yaml against the completeness check → must emit 3 fail issues (initCommands, build cmd, worker env)

### Shape validation (live run bar for v28)

Run the nestjs-showcase recipe with the fixes applied. Accept if:

- `shapeClassification: api-first` appears in the submitted recipePlan, **first try**, no string-submission attempts
- Recipe shape is `apidev + appdev + workerdev` (3 codebases)
- 3 scaffold subagents dispatched in parallel during generate
- Generate-complete's zerops.yaml completeness check passes first try (dev initCommands present, dev build runs `npm run build`, worker setup has SEARCH_* env vars)
- Wall ≤ 90 min total (matches v20/v25 baseline; deviation by more than 20% indicates a new regression class)

---

## 5. Explicit non-goals

The following are out of scope for this release — attempting them would restart the v8.78-v8.86 machinery-accumulation failure pattern the rollback undid:

- **No new content-quality checks.** No `gotcha_causal_anchor`, `content_reality`, `claude_readme_consistency`, `comment_ratio`, or similar. The rolled-back machinery stays rolled back.
- **No post-writer dispatch gates.** If README writer output is suboptimal, that's an editorial concern, not a gate concern. v23's 23-minute fix loop is the cautionary tale.
- **No framework-specific lookup tables in classification code.** `nestjs`/`laravel`/`rails`/etc. as token literals in Go code is prohibited. Framework identity lives in the minimal recipe's frontmatter, nowhere else.
- **No scaffold-hygiene re-enablement** unless its v8.80 implementation can be proven to fire only on real leaks (v21's 209 MB reproducibly, nothing else). The current state-of-play is: bash_guard middleware + pkill self-kill classifier + scaffold-brief SSH preamble are the tools against the v17/v21 class. These held through v25 and v27. Do not add more.
- **No `zerops_record_fact` integration into checks.** The tool is orphan; agents may or may not call it. Do not make it load-bearing.

---

## 6. Commit shape

Suggested commit sequence (one commit per fix):

1. `feat(knowledge): showcaseShape frontmatter on *-minimal.md predecessors`
2. `feat(research): RecipePlan.shapeClassification field + validator gate`
3. `refactor(recipe-md): classification is predecessor-tag-driven, not inferred`
4. `feat(recipe-md): single-codebase showcase dispatches 1 scaffold subagent`
5. `feat(scaffold-brief): lazy client construction rule + eager ctor failure teaching`
6. `feat(ops): zerops_yaml_completeness static check at generate-complete`
7. `feat(tools): friendly schema error for stringified object fields`
8. `docs(recipe-log): v28 baseline bar — shape, wall, dispatch shape, completeness pass`

Each commit independently revertible. If §3.3 (completeness check) produces false positives on a framework the implementor tests against, revert that one commit only — the shape classification + delegation fixes stand alone.

---

## 7. Why this set, and not more

The rollback (v24 → v25) established that the checker-machinery path produces strictly-monotone regression for five consecutive runs. v25 + v27 confirm: **the agent's load-bearing decisions are where the value is**. Checks over content are theater. Schemas over shape are load-bearing.

The three fixes in §3 are all at the shape/schema/delegation layer. None of them reach into content quality. None of them add dispatch gates. None of them hardcode frameworks. They make three decisions inspectable that were previously paragraph-bound:

- Classification decision (was: prose rule) → static tag + validator
- Delegation decision (was: routing by parallelism heuristic) → always-delegate authoring
- zerops.yaml consistency (was: deploy-time discovery) → generate-time static check

That's the whole release.
