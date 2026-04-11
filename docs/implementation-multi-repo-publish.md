# Implementation guide: multi-repo publish for recipes

**Reader**: opus-level implementor running against the ZCP codebase with no prior conversation context.
**Outcome**: `zcp sync recipe` can publish a dynamic number of GitHub repos per recipe — one per codebase the plan defines — instead of the current hardcoded single `{slug}-app` repo.
**Prerequisite**: the recipe-workflow reshuffle (v8.54.0) is merged. This guide builds on its Phase 9 changes (`OverlayRealREADMEs`, per-codebase README scaffolding in `BuildFinalizeOutput`).

---

## Why this exists

Recipe creation supports recipes whose source tree spans multiple codebases:

| Shape | Codebases | Mounts |
|---|---|---|
| Single-runtime + shared worker (Laravel Horizon, Rails Sidekiq, Django+Celery) | 1 | `appdev` |
| Single-runtime + separate worker | 2 | `appdev`, `workerdev` |
| Dual-runtime + shared worker (worker in API) | 2 | `appdev` (frontend), `apidev` |
| Dual-runtime + separate worker (3-repo showcase, API-first default) | 3 | `appdev`, `apidev`, `workerdev` |

At the close step the agent calls `zcp sync recipe create-repo` + `push-app` to land source on GitHub under `zerops-recipe-apps/`. Both commands are hardcoded in [internal/sync/publish_recipe.go:189-253](../internal/sync/publish_recipe.go#L189-L253):

```go
func PushAppSource(cfg *Config, slug, appDir string, dryRun bool) (PushResult, error) {
    org := cfg.Push.Recipes.Org
    repoName := slug + "-app"   // HARDCODED
    ...
}

func CreateRecipeRepo(cfg *Config, slug string, dryRun bool) (PushResult, error) {
    org := cfg.Push.Recipes.Org
    repoName := slug + "-app"   // HARDCODED
    ...
}
```

For a dual-runtime + separate-worker recipe like `nestjs-showcase`, the agent has three distinct codebases at `/var/www/apidev`, `/var/www/appdev`, `/var/www/workerdev`. With the current CLI it can only publish one. There is no way to publish `nestjs-showcase-api` or `nestjs-showcase-worker` — passing a different slug would double-suffix to `nestjs-showcase-api-app`.

The reshuffle scope-limited this to "one repo per recipe, multi-codebase layout inside" (Phase 6 Path A). This guide delivers the proper fix.

---

## End state

The CLI takes a new optional `--repo-suffix <name>` flag on both `create-repo` and `push-app`. When present, the GitHub repo name is `{slug}-{suffix}` instead of `{slug}-app`. When absent, the behavior is unchanged (`{slug}-app`) — all existing recipes continue to work.

At close time the agent iterates the plan's runtime targets and dispatches one `create-repo` + one `push-app` per codebase, using the target hostname as the suffix. For a 3-codebase showcase:

```
# Run in parallel as three pairs of tool calls
zcp sync recipe create-repo nestjs-showcase --repo-suffix app
zcp sync recipe push-app   nestjs-showcase /var/www/appdev    --repo-suffix app

zcp sync recipe create-repo nestjs-showcase --repo-suffix api
zcp sync recipe push-app   nestjs-showcase /var/www/apidev    --repo-suffix api

zcp sync recipe create-repo nestjs-showcase --repo-suffix worker
zcp sync recipe push-app   nestjs-showcase /var/www/workerdev --repo-suffix worker
```

Result: three GitHub repos — `zerops-recipe-apps/nestjs-showcase-app`, `nestjs-showcase-api`, `nestjs-showcase-worker` — each with its own source tree and its own README.

For a single-runtime minimal recipe (Laravel, Django, etc.), the agent omits `--repo-suffix` entirely and gets the existing `{slug}-app` repo. No migration burden on existing recipes.

---

## The "which codebases need repos" rule

A plan's codebases are defined by its runtime targets. The rule, derived from [internal/workflow/recipe.go:88-109](../internal/workflow/recipe.go#L88-L109):

```
A codebase needs its own repo iff EITHER:
  (a) It is a non-worker runtime target (IsRuntimeType(Type) && !IsWorker), OR
  (b) It is a worker target with SharesCodebaseWith == "" (separate codebase).
```

A worker with `SharesCodebaseWith != ""` shares another target's codebase and gets NO repo of its own — its code lives in the host target's repo.

This is **byte-identical** to the logic Phase 9 uses in `BuildFinalizeOutput` at [internal/workflow/recipe_templates.go:55-68](../internal/workflow/recipe_templates.go#L55-L68) for per-codebase README scaffolding. Keep them in lock-step.

Worked examples:

| Plan targets | Codebases → repo suffixes |
|---|---|
| `app: php-nginx@8.4`, `db: postgresql@17` | `app` |
| `app: php-nginx@8.4`, `worker: php-nginx@8.4 (shares="app")`, `db`, `redis`, `queue` | `app` |
| `app: nodejs@22`, `worker: nodejs@22 (shares="")`, `db`, `queue` | `app`, `worker` |
| `app: static`, `api: nodejs@22`, `worker: nodejs@22 (shares="api")`, `db`, `redis`, `queue`, `storage`, `search` | `app`, `api` |
| `app: static`, `api: nodejs@22`, `worker: nodejs@22 (shares="")`, `db`, `redis`, `queue`, `storage`, `search` | `app`, `api`, `worker` |

The suffix is **always the hostname** of the codebase owner. For the shared-codebase dual-runtime case, the worker doesn't appear — its repo suffix is the host target's (`api`), not `worker`.

**⚠ Pre-existing divergence to fix first** — the reshuffle's per-codebase README scaffolding at [internal/workflow/recipe_templates.go:61-66](../internal/workflow/recipe_templates.go#L61-L66) does NOT match this rule today:

```go
for _, target := range plan.Targets {
    if !IsRuntimeType(target.Type) || target.IsWorker {   // ← skips ALL workers, incl. separate
        continue
    }
    files[target.Hostname+"dev/README.md"] = GenerateAppREADME(plan)
}
```

Separate-codebase workers (`IsWorker && SharesCodebaseWith == ""`) DO own their own codebase — and per the multi-repo-publish plan, they will own their own GitHub repo too — but the loop above skips every worker unconditionally. Result: for `nestjs-showcase` with a separate `worker` codebase, no `workerdev/README.md` is scaffolded, but the agent's push-app call will commit the directory to its own repo without a landing doc.

This is a pre-existing gap from the reshuffle's Phase 9, not caused by the multi-repo change — but the multi-repo change is the first thing to make it visible. **Phase 0.5 fixes it** before the Phase 1 CLI work exposes the regression.

---

## Phase 0 — tests first

### 0.1 Create `internal/sync/publish_recipe_test.go`

No publish tests exist today. Add a new file. Use `httptest` to stub out the `gh` binary or (simpler) lift the GH client behind an interface the tests can mock.

Inspect [internal/sync/publish_recipe.go](../internal/sync/publish_recipe.go#L1) — `CreateRecipeRepo` and `PushAppSource` call `gh.RepoExists` / `gh.CreateOrgRepo` / `runGit`. For tests, extract a `repoNameForPublish(slug, suffix string) string` pure helper and unit-test that directly first. The network-facing path needs a separate integration layer; don't block on it.

Seed test (RED before implementation):

```go
package sync

import "testing"

func TestRepoNameForPublish(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name   string
        slug   string
        suffix string
        want   string
    }{
        {"default suffix preserves backward compat", "laravel-minimal", "", "laravel-minimal-app"},
        {"explicit app suffix equals default",      "laravel-minimal", "app", "laravel-minimal-app"},
        {"api suffix for dual-runtime backend",     "nestjs-showcase", "api", "nestjs-showcase-api"},
        {"worker suffix for separate worker",       "nestjs-showcase", "worker", "nestjs-showcase-worker"},
        {"frontend suffix",                         "svelte-showcase", "app", "svelte-showcase-app"},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            got := repoNameForPublish(tt.slug, tt.suffix)
            if got != tt.want {
                t.Errorf("repoNameForPublish(%q, %q) = %q, want %q", tt.slug, tt.suffix, got, tt.want)
            }
        })
    }
}
```

(No `tt := tt` loop-capture shim — obsolete under Go 1.22+ loop semantics; `make lint-local` flags it as `forvar`.)

All 5 cases are RED until Phase 1 lands the helper. Run:
```
go test ./internal/sync/ -run TestRepoNameForPublish -v
```

---

## Phase 0.5 — fix the per-codebase README scaffolding loop

**Goal**: align [internal/workflow/recipe_templates.go:61-66](../internal/workflow/recipe_templates.go#L61-L66) with the "which codebases need repos" rule from the top of this plan — so separate-codebase workers get their own scaffolded README before Phase 1 exposes them via the CLI.

**Why first**: otherwise the first dual-runtime + separate-worker recipe that runs the new 6-call publish sequence commits a `workerdev/` directory with no `workerdev/README.md`, which is a visible user-facing regression. The bug predates this plan (it's from reshuffle Phase 9) but this plan is the first change that makes it reachable.

**Files touched**: 2
- [internal/workflow/recipe_templates.go](../internal/workflow/recipe_templates.go)
- [internal/workflow/recipe_templates_test.go](../internal/workflow/recipe_templates_test.go)

### 0.5.1 Fix the loop

Current:

```go
// Per-codebase README scaffolds. Each non-worker runtime target with its
// own codebase gets its own README at {hostname}dev/README.md so every
// codebase in a dual-runtime recipe has a matching landing doc. The agent
// fills in integration-guide and knowledge-base content for each. Shared-
// codebase workers (SharesCodebaseWith set) don't get their own README —
// the host target owns it.
for _, target := range plan.Targets {
    if !IsRuntimeType(target.Type) || target.IsWorker {
        continue
    }
    files[target.Hostname+"dev/README.md"] = GenerateAppREADME(plan)
}
```

Replacement:

```go
// Per-codebase README scaffolds. A target owns its own README iff EITHER:
//   (a) it's a non-worker runtime target, OR
//   (b) it's a worker with SharesCodebaseWith == "" (separate codebase).
// Shared-codebase workers (SharesCodebaseWith set) don't get their own
// README — the host target owns it. This matches the "codebase count
// rule" used by the multi-repo publish flow — see
// docs/implementation-multi-repo-publish.md.
for _, target := range plan.Targets {
    if !IsRuntimeType(target.Type) {
        continue
    }
    if target.IsWorker && target.SharesCodebaseWith != "" {
        continue
    }
    files[target.Hostname+"dev/README.md"] = GenerateAppREADME(plan)
}
```

### 0.5.2 Test

Add to [internal/workflow/recipe_templates_test.go](../internal/workflow/recipe_templates_test.go):

```go
// TestBuildFinalizeOutput_SeparateWorkerHasOwnREADME asserts that a
// separate-codebase worker gets its own {hostname}dev/README.md scaffolded
// alongside the app/api READMEs. Before this test was added, the loop in
// recipe_templates.go skipped all workers unconditionally — separate
// workers ended up with a committable codebase but no landing doc. The
// multi-repo publish CLI change exposed the gap; this test guards against
// a revert.
func TestBuildFinalizeOutput_SeparateWorkerHasOwnREADME(t *testing.T) {
    t.Parallel()

    // 3-codebase showcase: static frontend + nodejs API + separate nodejs worker.
    plan := testDualRuntimePlan()
    // Ensure the worker is separate-codebase (plan's default can be shared).
    for i := range plan.Targets {
        if plan.Targets[i].IsWorker {
            plan.Targets[i].SharesCodebaseWith = ""
        }
    }

    files := BuildFinalizeOutput(plan)

    for _, want := range []string{
        "appdev/README.md",
        "apidev/README.md",
        "workerdev/README.md",
    } {
        if _, ok := files[want]; !ok {
            t.Errorf("expected %q in finalize output, got keys: %v", want, sortedKeys(files))
        }
    }
}

// TestBuildFinalizeOutput_SharedWorkerHasNoREADME is the negative counterpart:
// a shared-codebase worker MUST NOT get its own README — the host target owns
// it. Catches a regression where the loop stops filtering shared workers.
func TestBuildFinalizeOutput_SharedWorkerHasNoREADME(t *testing.T) {
    t.Parallel()

    plan := testDualRuntimePlan()
    // Force shared: worker rides on the api codebase.
    for i := range plan.Targets {
        if plan.Targets[i].IsWorker {
            plan.Targets[i].SharesCodebaseWith = "api"
        }
    }

    files := BuildFinalizeOutput(plan)

    if _, ok := files["workerdev/README.md"]; ok {
        t.Errorf("shared-codebase worker should NOT get its own README, but workerdev/README.md was scaffolded")
    }
    // Host and frontend still get theirs.
    for _, want := range []string{"appdev/README.md", "apidev/README.md"} {
        if _, ok := files[want]; !ok {
            t.Errorf("expected %q in finalize output", want)
        }
    }
}
```

If `sortedKeys` isn't already a test helper in this file, add it locally or inline the key-set dump. Check the existing file for the pattern before writing new test helpers.

### 0.5.3 Verify

```
go test ./internal/workflow/ -run TestBuildFinalizeOutput_ -v
go test ./internal/workflow/ -count=1
```

Both new tests GREEN after the loop fix. Full workflow suite stays GREEN.

**Commit**: `fix(recipe): scaffold README for separate-codebase workers`

**Rollback**: revert the loop change + test additions. Only the README scaffolding changes; no consumers downstream.

---

## Phase 1 — extract the naming helper and thread the suffix through

### 1.1 Add `repoNameForPublish`

**File**: [internal/sync/publish_recipe.go](../internal/sync/publish_recipe.go#L1)

Add near the top of the file (after the `applyPlaceholders` helper, before `PublishRecipe`):

```go
// repoNameForPublish computes the GitHub repo name for a recipe codebase.
// The default suffix is "app" (backward compat with single-codebase recipes).
// Multi-codebase recipes pass the hostname of the codebase owner as the
// suffix (e.g. "api", "worker") to get distinct repos per codebase.
func repoNameForPublish(slug, suffix string) string {
    if suffix == "" {
        suffix = "app"
    }
    return slug + "-" + suffix
}
```

### 1.2 Thread `suffix` through `CreateRecipeRepo`

**File**: [internal/sync/publish_recipe.go:234](../internal/sync/publish_recipe.go#L234)

Change the signature and body:

```go
// CreateRecipeRepo creates a new public repo in the recipe apps org.
// suffix selects the codebase name (e.g. "app", "api", "worker"). Empty
// suffix defaults to "app" for backward compatibility with existing recipes.
func CreateRecipeRepo(cfg *Config, slug, suffix string, dryRun bool) (PushResult, error) {
    org := cfg.Push.Recipes.Org
    repoName := repoNameForPublish(slug, suffix)
    fullRepo := org + "/" + repoName

    gh := &GH{}
    if gh.RepoExists(fullRepo) {
        return PushResult{Slug: slug, Status: Skipped, Reason: fullRepo + " already exists"}, nil
    }

    if dryRun {
        return PushResult{Slug: slug, Status: DryRun, Diff: "would create " + fullRepo}, nil
    }

    if err := gh.CreateOrgRepo(org, repoName); err != nil {
        return PushResult{Slug: slug, Status: Error}, fmt.Errorf("create repo: %w", err)
    }

    return PushResult{Slug: slug, Status: Created, PRURL: "https://github.com/" + fullRepo}, nil
}
```

### 1.3 Thread `suffix` through `PushAppSource`

**File**: [internal/sync/publish_recipe.go:189](../internal/sync/publish_recipe.go#L189)

Same pattern:

```go
// PushAppSource pushes the app source directory to the recipe app repo.
// suffix selects which codebase is being pushed (e.g. "app", "api", "worker").
// Empty suffix defaults to "app" for backward compat.
func PushAppSource(cfg *Config, slug, suffix, appDir string, dryRun bool) (PushResult, error) {
    org := cfg.Push.Recipes.Org
    repoName := repoNameForPublish(slug, suffix)
    fullRepo := org + "/" + repoName
    repoURL := "https://github.com/" + fullRepo + ".git"

    if dryRun {
        return PushResult{Slug: slug, Status: DryRun, Diff: fmt.Sprintf("would push %s to %s", appDir, fullRepo)}, nil
    }

    if !hasGitDir(appDir) {
        return PushResult{Slug: slug, Status: Error}, fmt.Errorf("no .git in %s — run git init first", appDir)
    }

    _ = runGit(appDir, "remote", "add", "origin", repoURL)
    _ = runGit(appDir, "remote", "set-url", "origin", repoURL)
    _ = runGit(appDir, "add", "-A")
    _ = runGit(appDir, "diff-index", "--quiet", "HEAD", "--")
    _ = runGit(appDir, "commit", "-q", "-m", "recipe: "+slug)

    if err := runGit(appDir, "push", "-u", "origin", "HEAD"); err != nil {
        return PushResult{Slug: slug, Status: Error}, fmt.Errorf("git push: %w", err)
    }

    return PushResult{Slug: slug, Status: Created, PRURL: "https://github.com/" + fullRepo}, nil
}
```

### 1.4 Verify Phase 1

```
go test ./internal/sync/ -run TestRepoNameForPublish -v
go build ./...
```

All 5 subtests GREEN. Build clean. Existing single-codebase recipes still resolve to `{slug}-app` because the default behavior is preserved.

Commit: `refactor(sync): extract repoNameForPublish + thread suffix through publish helpers`

---

## Phase 2 — CLI flag wiring

### 2.1 Update `runSyncRecipe` dispatch

**File**: [cmd/zcp/sync.go:196-207](../cmd/zcp/sync.go#L196-L207)

Current `create-repo` case:

```go
case "create-repo":
    if len(args) < 2 {
        fmt.Fprintln(os.Stderr, "usage: zcp sync recipe create-repo <slug> [--dry-run]")
        os.Exit(1)
    }
    slug := args[1]
    result, err := sync.CreateRecipeRepo(cfg, slug, dryRun)
    ...
```

Replace with flag-parsing shape matching the `publish` case's convention:

```go
case "create-repo":
    if len(args) < 2 {
        fmt.Fprintln(os.Stderr, "usage: zcp sync recipe create-repo <slug> [--repo-suffix <name>] [--dry-run]")
        os.Exit(1)
    }
    slug := args[1]
    suffix := ""
    for i := 2; i < len(args); i++ {
        if args[i] == "--repo-suffix" && i+1 < len(args) {
            suffix = args[i+1]
            i++
        }
    }
    result, err := sync.CreateRecipeRepo(cfg, slug, suffix, dryRun)
    if err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
    printRecipeResult(result)
```

### 2.2 Update `push-app` case

**File**: [cmd/zcp/sync.go:276-288](../cmd/zcp/sync.go#L276-L288)

Same pattern:

```go
case "push-app":
    if len(args) < 3 {
        fmt.Fprintln(os.Stderr, "usage: zcp sync recipe push-app <slug> <app-dir> [--repo-suffix <name>] [--dry-run]")
        os.Exit(1)
    }
    slug := args[1]
    appDir := args[2]
    suffix := ""
    for i := 3; i < len(args); i++ {
        if args[i] == "--repo-suffix" && i+1 < len(args) {
            suffix = args[i+1]
            i++
        }
    }
    result, err := sync.PushAppSource(cfg, slug, suffix, appDir, dryRun)
    if err != nil {
        fmt.Fprintf(os.Stderr, "error: %v\n", err)
        os.Exit(1)
    }
    printRecipeResult(result)
```

### 2.3 Update `printRecipeUsage` help text

**File**: [cmd/zcp/sync.go:345-358](../cmd/zcp/sync.go#L345-L358)

Grep for the usage strings and update them to include `--repo-suffix` on both commands. Find the current help block and change:

```
  create-repo <slug>                Create app repo in zerops-recipe-apps org
  push-app <slug> <app-dir>         Push app source to the app repo
```

to:

```
  create-repo <slug> [--repo-suffix <name>]           Create app repo in zerops-recipe-apps org (suffix defaults to "app")
  push-app    <slug> <app-dir> [--repo-suffix <name>] Push app source to the app repo (suffix must match create-repo)
```

And the example block:

```
  zcp sync recipe create-repo laravel-minimal
  zcp sync recipe push-app   laravel-minimal /var/www/appdev

  # Dual-runtime showcase with separate worker (3 repos):
  zcp sync recipe create-repo nestjs-showcase --repo-suffix app
  zcp sync recipe push-app    nestjs-showcase /var/www/appdev    --repo-suffix app
  zcp sync recipe create-repo nestjs-showcase --repo-suffix api
  zcp sync recipe push-app    nestjs-showcase /var/www/apidev    --repo-suffix api
  zcp sync recipe create-repo nestjs-showcase --repo-suffix worker
  zcp sync recipe push-app    nestjs-showcase /var/www/workerdev --repo-suffix worker
```

### 2.4 Verify Phase 2

```
go build ./... && ./bin/zcp sync recipe --help
./bin/zcp sync recipe create-repo laravel-minimal --dry-run
./bin/zcp sync recipe create-repo nestjs-showcase --repo-suffix api --dry-run
./bin/zcp sync recipe push-app nestjs-showcase /tmp/nonexistent --repo-suffix worker --dry-run
```

Expected:
- Help text lists `--repo-suffix` on both commands.
- First dry-run prints `would create zerops-recipe-apps/laravel-minimal-app`.
- Second dry-run prints `would create zerops-recipe-apps/nestjs-showcase-api`.
- Third dry-run prints `would push /tmp/nonexistent to zerops-recipe-apps/nestjs-showcase-worker`.

Commit: `feat(cli): add --repo-suffix flag to sync recipe create-repo / push-app`

---

## Phase 3 — update recipe.md close-step guidance

### 3.1 Rewrite the "Create app repo and push source" block

**File**: [internal/content/workflows/recipe.md](../internal/content/workflows/recipe.md) → `<section name="close">` → `### 2. Export & Publish`

Find the current block (left behind by the reshuffle's Phase 6 Path A):

```markdown
**Create app repo and push source**:

Currently the publish CLI creates a single `{slug}-app` repo. For a dual-runtime showcase, users land on one repo containing multiple codebases as top-level subdirectories (`apidev/`, `appdev/`, `workerdev/`):

...
```

Replace with:

````markdown
**Create app repo(s) and push source**:

Each codebase in the recipe becomes its own GitHub repo under `zerops-recipe-apps/`. The number of `create-repo` + `push-app` pairs equals the number of codebases the plan has — NOT the number of services. Pass `--repo-suffix <hostname>` on both commands so every call lands on its own repo.

**Codebase count rule** — one codebase exists per runtime target that owns its own source tree:
- Every non-worker runtime target (`IsWorker: false`) owns a codebase.
- Every worker target with empty `sharesCodebaseWith` owns a codebase (separate-codebase worker).
- A worker with `sharesCodebaseWith` set owns NO codebase — it lives inside the host target's repo.

The shape depends on the research-step worker decision:

| Plan shape | Codebases | Publish calls |
|---|---|---|
| Single-runtime minimal (`app` + `db`) | 1 | `app` |
| Single-runtime + shared worker (Laravel Horizon, Rails Sidekiq, Django+Celery) | 1 | `app` |
| Single-runtime + separate worker | 2 | `app`, `worker` |
| Dual-runtime + shared worker (worker in API) | 2 | `app`, `api` |
| Dual-runtime + separate worker (3-repo showcase, API-first default) | 3 | `app`, `api`, `worker` |

**Shape of each call pair** — the `--repo-suffix` MUST match the codebase owner's hostname, and the `push-app` path MUST be the mount for that codebase:

```
zcp sync recipe create-repo {slug} --repo-suffix {hostname}
zcp sync recipe push-app    {slug} /var/www/{hostname}dev --repo-suffix {hostname}
```

**Dispatch all pairs in parallel** — the 6 calls (for a 3-repo showcase) have no ordering constraint between each other. Run them as parallel tool calls in a single message. Example for `nestjs-showcase` (dual-runtime + separate worker):

```
zcp sync recipe create-repo nestjs-showcase --repo-suffix app
zcp sync recipe push-app    nestjs-showcase /var/www/appdev    --repo-suffix app

zcp sync recipe create-repo nestjs-showcase --repo-suffix api
zcp sync recipe push-app    nestjs-showcase /var/www/apidev    --repo-suffix api

zcp sync recipe create-repo nestjs-showcase --repo-suffix worker
zcp sync recipe push-app    nestjs-showcase /var/www/workerdev --repo-suffix worker
```

Each repo ends up with its own `README.md` (the 3 fragments you wrote at generate for that codebase), its own `zerops.yaml`, and its own source tree — all three codebases were committed independently at generate.

For a single-codebase recipe you can omit `--repo-suffix` entirely; the default is `app` and the result is `{slug}-app`.
````

### 3.2 Update the `<section name="deploy">` footnote (optional — likely a no-op)

**File**: [internal/content/workflows/recipe.md](../internal/content/workflows/recipe.md) → `<section name="deploy">`

Grep for the "Currently the publish CLI creates a single" phrasing. If anything surfaces outside the close section, remove it.

```
grep -n 'Currently the publish CLI creates a single' internal/content/workflows/recipe.md
```

As of this plan's last ground-truth check, the only occurrence is inside `<section name="close">` at line 1554 — already handled by 3.1. Deploy has no remaining reference, so this step is expected to be a no-op. Run the grep anyway; if the count changes before this phase executes, remove the new occurrence.

### 3.3 Update `docs/sync-recipe.md` if it exists

```
ls /Users/fxck/www/zcp/docs/sync-recipe.md
```

If the file exists, update its `create-repo` / `push-app` sections to match the new flag. If it doesn't, skip.

### 3.4 Verify Phase 3

```
go test ./internal/workflow/ -run TestRecipe_ 2>&1 | tail -20
```

The existing placement tests still pass (no placement changes). The recipe.md content is prose — assert presence via grep:

```
go test ./internal/workflow/ -run TestRecipe_SubAgentBriefPlacement -v
```

Commit: `docs(recipe): document multi-repo publish at close step`

---

## Phase 4 — content-placement test for the new close block

### 4.1 Add to `internal/workflow/recipe_content_placement_test.go`

```go
// TestRecipe_CloseMultiRepoPublish asserts the close step documents the
// multi-repo publish shape after the --repo-suffix CLI addition landed.
// The codebase count rule must be visible (so the agent iterates the plan
// correctly) and the example must include all three shapes.
func TestRecipe_CloseMultiRepoPublish(t *testing.T) {
    t.Parallel()

    close := sectionContent(t, "close")

    // The codebase count rule must be present.
    for _, p := range []string{
        "Codebase count rule",
        "IsWorker: false",
        "sharesCodebaseWith",
        "--repo-suffix",
    } {
        if !strings.Contains(close, p) {
            t.Errorf("close missing multi-repo publish phrase %q", p)
        }
    }

    // Path-A scope-limit language must be GONE — it predated the CLI fix.
    // Only include phrases that ACTUALLY existed in recipe.md before this
    // plan rewrote the close block; asserting phrases that never existed
    // is a dead assertion that looks like a guard but catches nothing.
    forbidden := []string{
        "Currently the publish CLI creates a single",
        "future CLI extension",
    }
    for _, f := range forbidden {
        if strings.Contains(close, f) {
            t.Errorf("close still contains pre-fix scope-limit phrase %q — should be removed", f)
        }
    }
}
```

### 4.2 Verify Phase 4

```
go test ./internal/workflow/ -run TestRecipe_CloseMultiRepoPublish -v
```

Expected: GREEN after Phase 3 content lands.

Commit: `test(recipe): assert close step documents multi-repo publish`

---

## Phase 5 — end-to-end verification (manual, gated on real GitHub access)

### 5.1 Dry-run a dual-runtime publish

Make a throwaway plan with 3 codebases (or use the nestjs-showcase test fixture) and run:

```
./bin/zcp sync recipe create-repo test-multirepo-$(date +%s) --repo-suffix app    --dry-run
./bin/zcp sync recipe create-repo test-multirepo-$(date +%s) --repo-suffix api    --dry-run
./bin/zcp sync recipe create-repo test-multirepo-$(date +%s) --repo-suffix worker --dry-run
```

Each should print a distinct `would create zerops-recipe-apps/test-multirepo-XXX-{suffix}` line.

### 5.2 Optional: live publish against a throwaway org

If you have access to a non-production GitHub org, run the full 6-call sequence against it with real `git init`'d directories and confirm three distinct repos appear.

Do NOT publish against `zerops-recipe-apps` until a real recipe is ready.

### 5.3 Backward compat check

Run an existing single-codebase recipe (e.g. `laravel-minimal`) with NO `--repo-suffix` flag:

```
./bin/zcp sync recipe create-repo laravel-minimal --dry-run
./bin/zcp sync recipe push-app laravel-minimal /var/www/appdev --dry-run
```

Expected output must still reference `laravel-minimal-app` (not `laravel-minimal-` or anything else). This is the backward-compat invariant.

---

## Phase 6 — lint + final tests

```
go test ./... -count=1
make lint-local
```

Expected: 0 failures, 0 lint issues.

---

## Rollback

Each phase is a single commit. Roll back one at a time:

| Phase | Reverting effect |
|---|---|
| 0.5 | README scaffolding loop reverts to skipping every worker. Separate-codebase workers lose their scaffolded README. Pre-existing bug returns. Safe to revert independently — no downstream phase depends on the loop shape. |
| 1 | `CreateRecipeRepo` / `PushAppSource` lose the suffix arg — CLI call in Phase 2 won't compile. Revert Phase 2 first. |
| 2 | CLI flag goes away — recipe.md guidance in Phase 3 becomes non-executable. Revert Phase 3 first. |
| 3 | recipe.md reverts to Path A scope-limit wording. Content tests in Phase 4 fail. Revert Phase 4 first. |
| 4 | Tests removed. |

If you need to roll back all the way, revert Phase 4 → 3 → 2 → 1 in that order. Phase 0.5 is independent and can be reverted at any point.

No phase adds state to persistent storage; no phase creates migrations; no phase touches session state. Rollback is purely local.

---

## Non-goals for this change

These are deliberately out of scope. Track them in separate tickets if needed:

- **Workflow-side automation.** The close step does NOT auto-iterate and dispatch the N pairs for the agent. The agent does it manually from recipe.md guidance. A future `zcp sync recipe publish-all` that reads the session plan and fans out is a separate change.
- **Per-repo different template.** Every published repo gets the same README scaffold today (`GenerateAppREADME` is called once with the full plan). Per-target template specialization (`GenerateAPIREADME` etc.) is a separate change tracked in reshuffle Phase 9.4 notes.
- **Repo visibility.** All repos are created public by `CreateOrgRepo`. Private-repo support is a separate change.
- **Rename existing repos.** If a recipe previously published as `laravel-showcase-app` and the team wants to split it, this change does NOT rename. You'd manually rename on GitHub and re-run with `--repo-suffix`.

---

## Acceptance criteria

The change is done when ALL of the following are true:

1. `go test ./...` passes (including new `TestRepoNameForPublish`, `TestBuildFinalizeOutput_SeparateWorkerHasOwnREADME`, `TestBuildFinalizeOutput_SharedWorkerHasNoREADME`, and `TestRecipe_CloseMultiRepoPublish`).
2. `make lint-local` reports 0 issues.
3. `./bin/zcp sync recipe create-repo --help` (or equivalent) documents `--repo-suffix`.
4. `./bin/zcp sync recipe create-repo existing-recipe --dry-run` (no flag) produces `existing-recipe-app`. Backward compat preserved.
5. `./bin/zcp sync recipe create-repo nestjs-showcase --repo-suffix api --dry-run` produces `nestjs-showcase-api`.
6. `recipe.md` close step documents the codebase count rule and the parallel dispatch pattern with all 4 shapes in the table.
7. No recipe.md section still contains the Path A scope-limit wording (`"Currently the publish CLI creates a single"`).
8. `BuildFinalizeOutput` for a dual-runtime + separate-worker plan produces `appdev/README.md`, `apidev/README.md`, AND `workerdev/README.md` — the per-codebase scaffolding rule matches the multi-repo publish rule exactly.

A partial land — for example, the CLI flag without the recipe.md guidance, or the CLI flag without the Phase 0.5 README scaffolding fix — is NOT acceptable. The agent won't discover the new flag from training data; it must be in the close-step instructions or it doesn't exist. And the scaffolding rule must match the publish rule or the first separate-worker recipe hits the gap in production.
