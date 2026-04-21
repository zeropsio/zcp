# Implementation guide: recipe workflow delivery refactor

**Reader**: opus-level implementor running against the ZCP codebase with no prior conversation context.

**Scope**: refactor *how* the recipe workflow delivers guidance to the agent, not just *how much*. The previous version of this file attacked size by trimming prose and lost the forest for the trees — every recipe paid for every other recipe's guidance because assembly is a flat string concat with two boolean gates. This rewrite replaces the delivery method with plan-aware conditional composition, refactors chain recipe extraction so ancestors deliver real gotchas instead of intro filler, and caches the 116 KB workflow file that currently re-reads on every MCP call. The secondary effect is size reduction; the primary effect is that an agent building a *narrow* recipe sees a narrow guide, and an agent building a *wide* recipe sees a wide guide — and neither sees content written for a different shape of recipe.

**Prerequisites**: the recipe-workflow reshuffle (v8.54.0, all phases of [docs/implementation-recipe-workflow-reshuffle.md](./implementation-recipe-workflow-reshuffle.md)) is merged. Content is in the right lifecycle step; this guide does not move content between steps.

## Ground truth reference — symbols and types the plan relies on

Verified against the current tree. Use these as the source of truth if any example in this document conflicts.

| Symbol | Location | Notes |
|---|---|---|
| `RecipeTierMinimal`, `RecipeTierShowcase` | [recipe.go:15-16](../internal/workflow/recipe.go#L15) | **No `RecipeTierHelloWorld` constant exists.** Hello-world plans have `Tier: ""` and are detected via slug suffix (`strings.HasSuffix(slug, "-hello-world")`) or via `recipeTierRank` in [recipe_knowledge_chain.go:16](../internal/workflow/recipe_knowledge_chain.go#L16). **Phase 0 must add `RecipeTierHelloWorld = "hello-world"` as a new constant** alongside the existing two if we want the fixtures to use it symbolically; otherwise the fixtures set `Tier: ""` and the rest of the code keeps using `recipeTierRank` for actual tier comparisons. This plan adopts the **add-the-constant** approach — simpler, consistent. |
| `RecipeStepResearch`, `RecipeStepProvision`, `RecipeStepGenerate`, `RecipeStepDeploy`, `RecipeStepFinalize`, `RecipeStepClose` | [recipe_steps.go:5](../internal/workflow/recipe_steps.go#L5) | String constants. |
| `RecipeState`, `RecipePlan`, `RecipeTarget` | [recipe.go](../internal/workflow/recipe.go) | `RecipePlan` has: `Framework`, `Tier`, `Slug`, `RuntimeType`, `BuildBases []string`, `Decisions`, `Research`, `Targets []RecipeTarget`. `RecipeTarget` has: `Hostname`, `Type`, `IsWorker`, `Role`, `SharesCodebaseWith`. |
| `(*RecipeState).BuildResponse` | [recipe.go:289](../internal/workflow/recipe.go#L289) | Signature: `(sessionID, intent string, iteration int, env Environment, kp knowledge.Provider) *RecipeResponse`. Used by audit test. |
| `EnvLocal` | [bootstrap_outputs.go](../internal/workflow/bootstrap_outputs.go) | Environment enum constant. |
| `knowledge.GetEmbeddedStore()` | [engine.go:47](../internal/knowledge/engine.go#L47) | Returns `(*Store, error)`. `*Store` implements `knowledge.Provider`. |
| `Document` | [documents.go:17](../internal/knowledge/documents.go#L17) | **Fields**: `Path`, `URI`, `Title`, `Content` (the body), `Description`. **There is no `Body` field.** The content lives in `Document.Content`. |
| `(*Document).H2Sections()` | [documents.go:30](../internal/knowledge/documents.go#L30) | Returns `map[string]string`. Already `sync.Once`-cached. |
| `ExtractSection(md, name string) string` | [bootstrap_guidance.go:143](../internal/workflow/bootstrap_guidance.go#L143) | String-index-based `<section name="X">` extractor. Reused by block parser. |
| `BuildIterationDelta(step string, iteration int, _ *ServicePlan, lastAttestation string) string` | [bootstrap_guidance.go:108](../internal/workflow/bootstrap_guidance.go#L108) | Bootstrap's escalation delta. Reused by `buildRecipeIterationDelta` in [recipe_guidance.go:192](../internal/workflow/recipe_guidance.go#L192). |
| `(*RecipeState).buildGuide` | [recipe_guidance.go:14](../internal/workflow/recipe_guidance.go#L14) | Current entry. Deploy iteration delta gate is at line 16. Generate gate is added in Phase 10. |
| `assembleRecipeKnowledge` | [recipe_guidance.go:113](../internal/workflow/recipe_guidance.go#L113) | Switch on step; injects chain recipe + env var catalog + core sections + multi-base. Phase 8 edits this. |
| `resolveRecipeGuidance` | [recipe_guidance.go:45](../internal/workflow/recipe_guidance.go#L45) | Switch on step; returns concatenated recipe.md sections. Phase 5-7 route each case through `composeSection`. |
| `recipeKnowledgeChain` | [recipe_knowledge_chain.go:35](../internal/workflow/recipe_knowledge_chain.go#L35) | Walks ancestry; Phase 4 edits it. |
| `extractKnowledgeSections` | [recipe_knowledge_chain.go:146](../internal/workflow/recipe_knowledge_chain.go#L146) | Current blunt extractor; Phase 4 deletes it. |
| `content.GetWorkflow(name)` | [content.go:20](../internal/content/content.go#L20) | Reads from `embed.FS` (not disk), returns `(string, error)`. Phase 1 caches. |
| `content.ListWorkflows()` | [content.go:40](../internal/content/content.go#L40) | Uses the same `embed.FS`. |
| `showcaseStepCaps` | [recipe_guidance_test.go:39](../internal/workflow/recipe_guidance_test.go#L39) | Currently `map[string]int` (flat, single-fixture). Phase 0 converts to `map[RecipeShape]map[string]int` (nested, per-shape). |

**If any snippet in this plan conflicts with a symbol here, trust this table.** These were verified by grep + Read against the current tree on the date this plan was written.

---

## TL;DR — what changes, in one page

| Change | Mechanism | Why it matters |
|---|---|---|
| **`<block>` subsections inside `<section>`** in [recipe.md](../internal/content/workflows/recipe.md) | New lightweight parser `ExtractBlocks(sectionBody)` returns `map[name]body`. A Go-side catalog pairs each block name with a predicate `func(*RecipePlan) bool`. | Blocks that don't apply to the current plan's shape aren't injected at all. Dual-runtime URL rules land on dual-runtime recipes only. Worker rules land on recipes with workers only. |
| **Chain recipe extraction refactor** | `extractForShowcase` returns `## Gotchas` + the YAML code fence from `## 1. Adding zerops.yaml`; `extractForAncestor` returns `## Gotchas` only, or empty. The old stop-at-first-numbered-section rule is deleted. | Direct predecessors drop from ~7 KB full content to ~2.5 KB (Gotchas + YAML template). Ancestors stop emitting 400 B of title-intro filler when they have no `## Gotchas` H2. |
| **On-demand schema pointer** | Provision and generate stop injecting full `import.yaml Schema` / `zerops.yaml Schema` H2 sections from `core.md` and inline a ~200 B field list with a `zerops_knowledge scope="theme"` pointer for exotic fields. | Saves 4.9 KB at provision and 5-11 KB at generate without losing reachability. |
| **Drop `envSecrets` / `dotEnvSecrets` / preprocessor from provision guidance** | Provision creates service shells; agent activates secrets via `zerops_env` during iteration. Deliverable `envSecrets` + `<@generateRandomString>` belong at finalize where the full env set is known. | User correction on v1 of this plan. Eliminates a class of "agent tried to stuff runtime secrets into workspace import" failures. |
| **H3-granular core subsection API** | Add `Document.H3Section(h2, h3)` + `getCoreSubsection(kp, h2, h3)`. | Unblocks surgical injection (e.g., only `verticalAutoscaling` H3 when recipe declares it). Used lightly now, heavily by future work. |
| **Workflow file caching** | `content.GetWorkflow` reads the embedded file once via `sync.Once`. | Eliminates repeated 116 KB reads per MCP call. Zero behavior change. |
| **Iteration-aware generate delta** | Mirror deploy's existing `iteration > 0` gate. On retry, replace the 30 KB generate section with a 3-4 KB failure-focused delta. | An iterating agent doesn't re-read the full skeleton-write guide every retry. |
| **Verified duplication cuts (2 total)** | `projectEnvVariables` handoff at generate → pointer to finalize; "where commands run" at close sub-agent prompt → pointer to deploy. | The only prose trims in this plan, because every other candidate is already handled by conditional composition. |

**Net effect** (estimated, to be verified by the audit harness):

| Step | Before | After (hello-world) | After (backend-minimal) | After (dual-runtime showcase) |
|---|---|---|---|---|
| research | 8.3 KB | 5 KB | 6 KB | 7 KB |
| provision | 20.5 KB | 7 KB | 10 KB | 12 KB |
| generate | 44.5 KB | 8 KB | 14 KB | 22 KB |
| deploy | 33.6 KB | 11 KB | 17 KB | 22 KB |
| finalize | 15.7 KB | 8 KB | 11 KB | 13 KB |
| close | 14.2 KB | 6 KB | 9 KB | 10 KB |
| **total** | **136.8 KB** | **~45 KB** | **~67 KB** | **~86 KB** |

The dual-runtime showcase case ("worst case") drops by 37% and **every step fits inside a single agent tool response** without chunked reads. Narrow recipes drop by 67%. Previous plan targeted ~92 KB total for all cases; this plan is strictly better on the worst case and dramatically better on the narrow cases it didn't differentiate.

---

## Architectural principles behind this plan

Read these before touching code — every phase decision derives from them.

1. **Content is shape-dependent; delivery must be too.** A recipe is defined by its plan — tier, targets, runtime, framework, whether dual-runtime, whether it has a worker, whether it has a bundler dev server. Guidance that applies to all recipes belongs in `<section>` preamble. Guidance that applies to some recipes belongs in `<block>` with a predicate.

2. **The chain recipe is the source of truth for framework patterns; prose duplicates are waste.** If the injected `nestjs-minimal` recipe demonstrates `zsc noop --silent`, dev mode flags, and cross-service refs in working YAML, recipe.md should not restate those rules in prose. recipe.md's job is the shape-level rules the chain cannot teach (dual-runtime URL shapes, dev-server host-check, worker shared-codebase topology).

3. **Eager injection is a tax on every session; pointers to on-demand retrieval are free for sessions that don't need them.** The `zerops_knowledge` tool exists, works, and has search + scope + recipe modes. A pointer of the form `zerops_knowledge scope="theme" query="import.yaml Schema"` costs ~80 bytes and pays full schema cost only when the agent hits an edge case. Currently recipe.md pays full schema cost every session.

4. **Attestations carry forward what the agent just learned; re-deriving knowledge is waste.** The provision step's attestation already records the env var catalog. The generate step can point at that attestation instead of re-emitting the catalog as a fresh markdown table. Same applies to anything the agent just did.

5. **Iteration is a different workload than first attempt.** A retrying agent has already read the full guide; what it needs is a targeted delta about what to change. Deploy already has this (`iteration > 0` → retry guide). Generate should have it too.

6. **Simplest correct mechanism.** We are not introducing a templating engine. `<block>` + predicate catalog is two small parsers, one small registry, pure functions — minus the templating footgun and with strong typing on predicates.

7. **Every cut must be reversible by one git revert.** No phase in this guide deletes content; it either moves content to on-demand retrieval (recoverable), wraps content in a predicate gate (recoverable), or extracts a subset of content (the original is still in the chain recipe source of truth). Rollback restores the pre-phase behavior at every boundary.

---

## Measurement harness

The authoritative test is `TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap` in [internal/workflow/recipe_guidance_test.go](../internal/workflow/recipe_guidance_test.go). This guide expands it into a **per-fixture** cap sweep so narrow and wide recipes have separate budgets.

### Fixture shapes (to be added in Phase 0)

```go
// internal/workflow/recipe_guidance_test.go

type RecipeShape int

const (
    ShapeHelloWorld          RecipeShape = iota // nodejs-hello-world — tier 0, 2 services
    ShapeBackendMinimal                         // laravel-minimal    — tier 1, full-stack, no worker
    ShapeFullStackShowcase                      // laravel-showcase   — tier 2, full-stack + worker + 5 managed
    ShapeDualRuntimeShowcase                    // nestjs-showcase    — tier 2, API-first, separate worker codebase
)

func fixtureForShape(s RecipeShape) *RecipePlan { /* ... */ }
```

`ShapeDualRuntimeShowcase` is the existing `testDualRuntimePlan()` in [recipe_templates_test.go:82](../internal/workflow/recipe_templates_test.go#L82). The other three are new, built from live-tier recipe fixtures in the knowledge store.

### Per-shape caps (final targets, wired in Phase 11)

```go
var showcaseStepCaps = map[RecipeShape]map[string]int{
    ShapeHelloWorld: {
        RecipeStepResearch:  6 * 1024,
        RecipeStepProvision: 8 * 1024,
        RecipeStepGenerate:  10 * 1024,
        RecipeStepDeploy:    12 * 1024,
        RecipeStepFinalize:  9 * 1024,
        RecipeStepClose:     7 * 1024,
    },
    ShapeBackendMinimal: {
        RecipeStepResearch:  7 * 1024,
        RecipeStepProvision: 11 * 1024,
        RecipeStepGenerate:  16 * 1024,
        RecipeStepDeploy:    18 * 1024,
        RecipeStepFinalize:  12 * 1024,
        RecipeStepClose:     10 * 1024,
    },
    ShapeFullStackShowcase: {
        RecipeStepResearch:  8 * 1024,
        RecipeStepProvision: 13 * 1024,
        RecipeStepGenerate:  20 * 1024,
        RecipeStepDeploy:    22 * 1024,
        RecipeStepFinalize:  14 * 1024,
        RecipeStepClose:     11 * 1024,
    },
    ShapeDualRuntimeShowcase: {
        RecipeStepResearch:  9 * 1024,
        RecipeStepProvision: 14 * 1024,
        RecipeStepGenerate:  24 * 1024,
        RecipeStepDeploy:    24 * 1024,
        RecipeStepFinalize:  15 * 1024,
        RecipeStepClose:     12 * 1024,
    },
}
```

These are targets **after** every phase lands. Caps are loose through phases 0-10 and tightened only in phase 11.

### Audit composition test (permanent, build-tag gated)

```go
//go:build audit
// +build audit

package workflow

// TestAuditComposition dumps per-step, per-part, per-subsection byte
// composition across every fixture shape. Not compiled in normal builds.
//
// Run: go test -tags audit ./internal/workflow -run TestAuditComposition -v
```

Replaces the old plan's "create temp file, remember to delete" approach. Always available, never ships in normal builds.

### Commands

```bash
go test ./internal/workflow/ -run TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap -v
go test -tags audit ./internal/workflow/ -run TestAuditComposition -v
```

---

## Phase dependency graph

```
P0 ─── P1 ─── P2 ─── P3 ─── P4 ─── P5a ─── P5b
                             │              │
                             ├───── P6a ────┴── P6b
                             │              │
                             └───── P7a ────┴── P7b ─── P8 ─── P9 ─── P10 ─── P11 ─── P12
```

- **P0**: audit infrastructure + fixtures (no behavior change)
- **P1**: `content.GetWorkflow` caching (independent, no behavior change)
- **P2**: H3-granular core subsection API (infrastructure, consumed by P8)
- **P3**: `<block>` parser + predicate catalog infrastructure (infrastructure, consumed by P5-P7)
- **P4**: chain recipe extraction refactor (changes generate composition)
- **P5a**: wrap generate section in `<block>` tags with all-nil predicates (pure refactor)
- **P5b**: set real predicates on generate blocks (behavior change, cap measurement)
- **P6a/b**: same for provision
- **P7a/b**: same for deploy
- **P8**: on-demand schema pointer + provision field cleanup (per user correction)
- **P9**: verified duplication cuts (2 only)
- **P10**: iteration-aware generate delta (mirrors deploy's existing mechanism)
- **P11**: per-shape cap tightening + acceptance sweep
- **P12**: archive

Each phase leaves the tree green. Each has an explicit rollback. Each commits independently.

---

## Phase 0 — audit infrastructure + fixture shapes

**Goal**: permanent, build-gated composition audit + named fixture shapes for cap-sweep tests.

**Why first**: every subsequent phase relies on measuring delta across shapes, and the current single-fixture cap test cannot see narrow-recipe regressions.

**Files touched**: 2
- [internal/workflow/recipe_guidance_audit_test.go](../internal/workflow/recipe_guidance_audit_test.go) (new, `//go:build audit`)
- [internal/workflow/recipe_guidance_test.go](../internal/workflow/recipe_guidance_test.go) (add fixtures + sweep)

### 0.1 Create the audit test

```go
//go:build audit
// +build audit

package workflow

import (
    "fmt"
    "regexp"
    "strings"
    "testing"

    "github.com/zeropsio/zcp/internal/knowledge"
)

func TestAuditComposition(t *testing.T) {
    store, err := knowledge.GetEmbeddedStore()
    if err != nil {
        t.Fatalf("store: %v", err)
    }
    shapes := []struct {
        name string
        shape RecipeShape
    }{
        {"hello-world", ShapeHelloWorld},
        {"backend-minimal", ShapeBackendMinimal},
        {"fullstack-showcase", ShapeFullStackShowcase},
        {"dual-runtime-showcase", ShapeDualRuntimeShowcase},
    }
    steps := []string{
        RecipeStepResearch, RecipeStepProvision, RecipeStepGenerate,
        RecipeStepDeploy, RecipeStepFinalize, RecipeStepClose,
    }

    for _, sh := range shapes {
        fmt.Printf("\n\n########## SHAPE: %s ##########\n", sh.name)
        plan := fixtureForShape(sh.shape)
        for _, step := range steps {
            rs := advanceShowcaseStateTo(step, plan)
            resp := rs.BuildResponse("x", "m", 0, EnvLocal, store)
            guide := resp.Current.DetailedGuide
            fmt.Printf("\n=== %s === %d B (%.1f KB)\n", strings.ToUpper(step), len(guide), float64(len(guide))/1024)
            parts := strings.Split(guide, "\n\n---\n\n")
            for i, p := range parts {
                first := strings.SplitN(p, "\n", 2)[0]
                if len(first) > 80 {
                    first = first[:80]
                }
                fmt.Printf("  [part %d] %6d B  %s\n", i, len(p), first)
            }
            if len(parts) > 0 {
                dumpH3Breakdown(parts[0])
            }
        }
    }
}

func dumpH3Breakdown(body string) {
    h3re := regexp.MustCompile(`^### `)
    h4re := regexp.MustCompile(`^#### `)
    type bucket struct {
        name  string
        bytes int
    }
    var buckets []bucket
    cur := bucket{name: "[preamble]"}
    for _, l := range strings.Split(body, "\n") {
        switch {
        case h3re.MatchString(l):
            buckets = append(buckets, cur)
            cur = bucket{name: "    ### " + strings.TrimPrefix(l, "### "), bytes: len(l) + 1}
        case h4re.MatchString(l):
            buckets = append(buckets, cur)
            cur = bucket{name: "      #### " + strings.TrimPrefix(l, "#### "), bytes: len(l) + 1}
        default:
            cur.bytes += len(l) + 1
        }
    }
    buckets = append(buckets, cur)
    for _, b := range buckets {
        if b.bytes < 200 {
            continue
        }
        name := b.name
        if len(name) > 80 {
            name = name[:80]
        }
        fmt.Printf("      %-80s  %6d B\n", name, b.bytes)
    }
}
```

### 0.2 Add the missing `RecipeTierHelloWorld` constant

In [recipe.go:14-17](../internal/workflow/recipe.go#L14) the tier constants are:

```go
const (
    RecipeTierMinimal  = "minimal"  // type 3
    RecipeTierShowcase = "showcase" // type 4
)
```

There is no hello-world tier constant. Hello-world plans currently use `Tier: ""` and rely on slug-suffix matching via `recipeTierRank`. The plan adds the constant for symbolic consistency:

```go
const (
    RecipeTierHelloWorld = "hello-world" // type 1-2 (runtime and frontend hello-worlds)
    RecipeTierMinimal    = "minimal"     // type 3
    RecipeTierShowcase   = "showcase"    // type 4
)
```

Also update any plan-construction sites that currently set `Tier: ""` for hello-world recipes to set `Tier: RecipeTierHelloWorld` explicitly. Grep for `RecipeTier` and `plan.Tier ==` to find them:

```bash
grep -rn "RecipeTier\|plan.Tier ==\|\.Tier = \"\"" internal/workflow/ internal/tools/
```

The `recipeTierRank` function in [recipe_knowledge_chain.go:16](../internal/workflow/recipe_knowledge_chain.go#L16) already returns 0 for `-hello-world` slugs, so ranking logic is unaffected. This is a pure symbolic addition.

### 0.3 Add fixture shapes

In [recipe_guidance_test.go](../internal/workflow/recipe_guidance_test.go) above the existing `showcaseStepCaps`:

```go
type RecipeShape int

const (
    ShapeHelloWorld RecipeShape = iota
    ShapeBackendMinimal
    ShapeFullStackShowcase
    ShapeDualRuntimeShowcase
)

func fixtureForShape(s RecipeShape) *RecipePlan {
    switch s {
    case ShapeHelloWorld:
        return &RecipePlan{
            Slug:        "nodejs-hello-world",
            Framework:   "nodejs",
            RuntimeType: "nodejs@22",
            Tier:        RecipeTierHelloWorld,
            Targets: []RecipeTarget{
                {Hostname: "app", Type: "nodejs@22"},
                {Hostname: "db", Type: "postgresql@17"},
            },
        }
    case ShapeBackendMinimal:
        return &RecipePlan{
            Slug:        "laravel-minimal",
            Framework:   "laravel",
            RuntimeType: "php-nginx@8.3",
            Tier:        RecipeTierMinimal,
            Targets: []RecipeTarget{
                {Hostname: "app", Type: "php-nginx@8.3"},
                {Hostname: "db", Type: "postgresql@17"},
            },
        }
    case ShapeFullStackShowcase:
        return &RecipePlan{
            Slug:        "laravel-showcase",
            Framework:   "laravel",
            RuntimeType: "php-nginx@8.3",
            Tier:        RecipeTierShowcase,
            Targets: []RecipeTarget{
                {Hostname: "app", Type: "php-nginx@8.3"},
                {Hostname: "worker", Type: "php@8.3", IsWorker: true, SharesCodebaseWith: "app"},
                {Hostname: "db", Type: "postgresql@17"},
                {Hostname: "redis", Type: "valkey@8"},
                {Hostname: "queue", Type: "nats@2.12"},
                {Hostname: "storage", Type: "object-storage"},
                {Hostname: "search", Type: "meilisearch@1.10"},
            },
        }
    case ShapeDualRuntimeShowcase:
        return testDualRuntimePlan() // existing in recipe_templates_test.go
    }
    return nil
}
```

### 0.4 Convert `showcaseStepCaps` to nested per-shape map

Currently the cap map is flat:

```go
var showcaseStepCaps = map[string]int{
    RecipeStepResearch:  10 * 1024,
    RecipeStepProvision: 22 * 1024,
    RecipeStepGenerate:  56 * 1024,
    RecipeStepDeploy:    36 * 1024,
    RecipeStepFinalize:  16 * 1024,
    RecipeStepClose:     14 * 1024,
}
```

Convert to nested:

```go
var showcaseStepCaps = map[RecipeShape]map[string]int{
    ShapeHelloWorld: {
        RecipeStepResearch:  11 * 1024,
        RecipeStepProvision: 22 * 1024,
        RecipeStepGenerate:  56 * 1024,
        RecipeStepDeploy:    36 * 1024,
        RecipeStepFinalize:  16 * 1024,
        RecipeStepClose:     14 * 1024,
    },
    ShapeBackendMinimal:      { /* same loose values */ },
    ShapeFullStackShowcase:   { /* same loose values */ },
    ShapeDualRuntimeShowcase: { /* same loose values */ },
}
```

P0 uses **intentionally loose** caps for every shape so the sweep commits green. Phase 11 tightens per-shape based on measured post-P10 numbers.

### 0.5 Update the cap test to sweep fixtures

Convert to a subtests-per-shape sweep:

```go
func TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap(t *testing.T) {
    t.Parallel()
    store, err := knowledge.GetEmbeddedStore()
    if err != nil {
        t.Fatalf("store: %v", err)
    }
    shapes := []struct {
        name  string
        shape RecipeShape
    }{
        {"hello-world", ShapeHelloWorld},
        {"backend-minimal", ShapeBackendMinimal},
        {"fullstack-showcase", ShapeFullStackShowcase},
        {"dual-runtime-showcase", ShapeDualRuntimeShowcase},
    }
    for _, sh := range shapes {
        sh := sh
        t.Run(sh.name, func(t *testing.T) {
            t.Parallel()
            plan := fixtureForShape(sh.shape)
            caps := showcaseStepCaps[sh.shape]
            for step, capVal := range caps {
                rs := advanceShowcaseStateTo(step, plan)
                resp := rs.BuildResponse("x", "m", 0, EnvLocal, store)
                got := len(resp.Current.DetailedGuide)
                if got > capVal {
                    t.Errorf("%s/%s: %d B > cap %d B", sh.name, step, got, capVal)
                }
            }
        })
    }
}
```

(`capVal` not `cap` — avoids shadowing the Go builtin, keeps `golangci-lint` happy.)

### 0.5 Verify

```bash
go test ./internal/workflow/ -run TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap -v
go test -tags audit ./internal/workflow/ -run TestAuditComposition -v 2>&1 > /tmp/audit_p0.txt
grep -E "^===|^########## " /tmp/audit_p0.txt
```

**Expected**: cap sweep GREEN on all 4 shapes; audit dumps produce baseline numbers for each shape.

**Commit**: `test(recipe): permanent audit harness + per-shape fixture cap sweep`

**Rollback**: `git revert` — no runtime behavior change, only test infrastructure.

---

## Phase 1 — cache `content.GetWorkflow`

**Goal**: eliminate the 116 KB `embed.FS` read + string allocation per MCP call.

**Why now**: every phase from P5 onward adds read paths into the workflow file (block extraction, predicate evaluation, etc.). Caching first means none of those phases pay per-call allocation cost.

**Framing note**: `content.GetWorkflow` reads from an embedded `embed.FS`, not from disk. The cost is not a filesystem syscall — it's a `ReadFile` copy out of the embed table plus a `string()` conversion, ~116 KB allocated per call. On a hot path (every `zerops_workflow` tool invocation, multiplied by however many retries per session) that's measurable garbage. Caching once is obviously correct and costs nothing.

**Files touched**: 1
- [internal/content/content.go](../internal/content/content.go)

### 1.1 Add `sync.Once` caching

Current (simplified):

```go
func GetWorkflow(name string) (string, error) {
    bytes, err := fs.ReadFile(embedded, "workflows/"+name+".md")
    if err != nil {
        return "", err
    }
    return string(bytes), nil
}
```

Replace with:

```go
var (
    workflowCacheMu   sync.RWMutex
    workflowCacheInit sync.Once
    workflowCache     map[string]string
    workflowCacheErr  error
)

func initWorkflowCache() {
    cache := make(map[string]string)
    entries, err := fs.ReadDir(embedded, "workflows")
    if err != nil {
        workflowCacheErr = err
        return
    }
    for _, e := range entries {
        if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
            continue
        }
        b, err := fs.ReadFile(embedded, "workflows/"+e.Name())
        if err != nil {
            workflowCacheErr = err
            return
        }
        name := strings.TrimSuffix(e.Name(), ".md")
        cache[name] = string(b)
    }
    workflowCache = cache
}

func GetWorkflow(name string) (string, error) {
    workflowCacheInit.Do(initWorkflowCache)
    if workflowCacheErr != nil {
        return "", workflowCacheErr
    }
    workflowCacheMu.RLock()
    defer workflowCacheMu.RUnlock()
    s, ok := workflowCache[name]
    if !ok {
        return "", fmt.Errorf("workflow %q not found", name)
    }
    return s, nil
}
```

### 1.2 Assert single-read via test instrumentation

Add [internal/content/content_cache_test.go](../internal/content/content_cache_test.go):

```go
func TestGetWorkflow_CachedAcrossCalls(t *testing.T) {
    // Reset cache for test isolation.
    workflowCacheInit = sync.Once{}
    workflowCache = nil

    // Call many times.
    for i := 0; i < 100; i++ {
        s, err := GetWorkflow("recipe")
        if err != nil {
            t.Fatalf("iteration %d: %v", i, err)
        }
        if len(s) == 0 {
            t.Fatalf("iteration %d: empty", i)
        }
    }
    // All 100 reads should come from the cache — assert by replacing
    // the backing map and re-checking.
    workflowCacheMu.Lock()
    workflowCache["recipe"] = "SENTINEL"
    workflowCacheMu.Unlock()
    s, _ := GetWorkflow("recipe")
    if s != "SENTINEL" {
        t.Errorf("expected cache hit, got fresh read")
    }
}
```

### 1.3 Verify

```bash
go test ./internal/content/... -v
go test ./internal/workflow/... -count=1
```

**Expected**: new test GREEN, existing workflow tests GREEN.

**Commit**: `perf(content): cache embedded workflow files with sync.Once`

**Rollback**: `git revert` — behavior reverts to per-call read; nothing downstream depends on caching.

---

## Phase 2 — H3-granular core subsection API

**Goal**: enable surgical injection of specific H3 subsections from core.md and other themes.

**Why now**: Phase 8's on-demand schema pointer inlines a small common subset and points at `zerops_knowledge` for the rest; but for the "point at `zerops_knowledge` for the rest" case to work well, future phases (outside this guide) should be able to inject just the H3 the agent asked for rather than the full H2. Building the API here means Phase 8 can already use it for provision's `verticalAutoscaling` case.

**Files touched**: 3
- [internal/knowledge/documents.go](../internal/knowledge/documents.go)
- [internal/knowledge/documents_test.go](../internal/knowledge/documents_test.go)
- [internal/workflow/guidance.go](../internal/workflow/guidance.go)

### 2.1 Add `Document.H3Section`

Current `(*Document).H2Sections() map[string]string` at [documents.go:30](../internal/knowledge/documents.go#L30) parses by H2 and caches via `sync.Once`. Add an H3 helper that composes on top of it:

```go
// H3Section returns the body of a specific H3 subsection inside a specific H2.
// The H3 heading is matched by prefix (trailing modifiers are tolerated).
// Returns "" if either heading is not found.
func (d *Document) H3Section(h2, h3 string) string {
    body, ok := d.H2Sections()[h2]
    if !ok {
        return ""
    }
    return extractH3(body, h3)
}

func extractH3(h2Body, target string) string {
    lines := strings.Split(h2Body, "\n")
    var out []string
    inside := false
    for _, l := range lines {
        trimmed := strings.TrimPrefix(l, "### ")
        if len(trimmed) < len(l) {
            // H3 line
            if inside {
                break
            }
            if strings.HasPrefix(trimmed, target) {
                inside = true
                continue
            }
            continue
        }
        if strings.HasPrefix(l, "## ") {
            if inside {
                break
            }
            continue
        }
        if inside {
            out = append(out, l)
        }
    }
    return strings.TrimSpace(strings.Join(out, "\n"))
}
```

### 2.2 Add `getCoreSubsection` helper

In [internal/workflow/guidance.go](../internal/workflow/guidance.go):

```go
// getCoreSubsection returns the body of an H3 under an H2 in the core theme.
// Used for surgical injection when the full H2 is too broad.
func getCoreSubsection(kp knowledge.Provider, h2, h3 string) string {
    doc, err := kp.Get("zerops://themes/core")
    if err != nil {
        return ""
    }
    return doc.H3Section(h2, h3)
}
```

### 2.3 Table-driven test

Note: `Document` struct field is `Content`, not `Body` (see the ground-truth table at the top of this plan).

```go
func TestDocument_H3Section(t *testing.T) {
    t.Parallel()
    doc := &Document{Content: `## import.yaml Schema

### Service fields

- hostname
- type

### Project block

Fields for project-level configuration.

### verticalAutoscaling

minRam, maxRam, minCPU, maxCPU.
`}
    tests := []struct {
        name string
        h2   string
        h3   string
        want string
    }{
        {"exact match", "import.yaml Schema", "verticalAutoscaling", "minRam, maxRam, minCPU, maxCPU."},
        {"first subsection", "import.yaml Schema", "Service fields", "- hostname\n- type"},
        {"missing h2", "missing", "x", ""},
        {"missing h3", "import.yaml Schema", "missing", ""},
    }
    for _, tt := range tests {
        tt := tt
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            got := doc.H3Section(tt.h2, tt.h3)
            if got != tt.want {
                t.Errorf("got %q, want %q", got, tt.want)
            }
        })
    }
}
```

### 2.4 Verify

```bash
go test ./internal/knowledge/... -v
go test ./internal/workflow/... -count=1
```

**Expected**: new test GREEN, all existing tests GREEN, nothing calls `getCoreSubsection` yet.

**Commit**: `feat(knowledge): H3-granular subsection extraction API`

**Rollback**: safe — no existing caller.

---

## Phase 3 — `<block>` parser + predicate catalog infrastructure

**Goal**: introduce the conditional-composition primitive without converting any section yet.

**Why now**: the parser, the catalog type, and the predicate helpers must exist before any section can be converted. Landing this infrastructure in isolation means convert phases (P5-P7) become pure mechanical refactors.

**Files touched**: 5
- [internal/workflow/recipe_block_parser.go](../internal/workflow/recipe_block_parser.go) (new)
- [internal/workflow/recipe_block_parser_test.go](../internal/workflow/recipe_block_parser_test.go) (new)
- [internal/workflow/recipe_plan_predicates.go](../internal/workflow/recipe_plan_predicates.go) (new)
- [internal/workflow/recipe_plan_predicates_test.go](../internal/workflow/recipe_plan_predicates_test.go) (new)
- [internal/workflow/recipe_section_catalog.go](../internal/workflow/recipe_section_catalog.go) (new, empty catalogs)

### 3.1 Block parser

Simple: given a section body (everything between `<section name="X">` and `</section>`), return a map of `<block name="Y">` → body. Preamble (content before the first block) is stored under the empty key `""`.

```go
// internal/workflow/recipe_block_parser.go
package workflow

import (
    "regexp"
    "strings"
)

var (
    blockOpenRe  = regexp.MustCompile(`(?m)^<block name="([^"]+)">\s*$`)
    blockCloseRe = regexp.MustCompile(`(?m)^</block>\s*$`)
)

// ExtractBlocks parses a section body for <block name="..."> children and
// returns them as an ordered pair list (name, body). Content before the
// first block tag is returned as a synthetic block with name "" (preamble).
// Unknown/unmatched content is silently dropped — callers should assert
// the catalog covers every block via TestCatalog_CoversAllMarkdownBlocks.
func ExtractBlocks(sectionBody string) []Block {
    var blocks []Block

    matches := blockOpenRe.FindAllStringIndex(sectionBody, -1)
    if len(matches) == 0 {
        // No blocks — whole section is preamble.
        return []Block{{Name: "", Body: strings.TrimSpace(sectionBody)}}
    }

    firstOpen := matches[0][0]
    if firstOpen > 0 {
        preamble := strings.TrimSpace(sectionBody[:firstOpen])
        if preamble != "" {
            blocks = append(blocks, Block{Name: "", Body: preamble})
        }
    }

    for i, m := range matches {
        nameMatch := blockOpenRe.FindStringSubmatch(sectionBody[m[0]:m[1]])
        name := nameMatch[1]

        bodyStart := m[1]
        var bodyEnd int
        if i+1 < len(matches) {
            bodyEnd = matches[i+1][0]
        } else {
            bodyEnd = len(sectionBody)
        }

        bodyChunk := sectionBody[bodyStart:bodyEnd]
        // Strip the closing tag.
        if close := blockCloseRe.FindStringIndex(bodyChunk); close != nil {
            bodyChunk = bodyChunk[:close[0]]
        }

        blocks = append(blocks, Block{
            Name: name,
            Body: strings.TrimSpace(bodyChunk),
        })
    }

    return blocks
}

type Block struct {
    Name string // "" = preamble
    Body string
}
```

### 3.2 Parser tests

Table-driven, covers: no blocks, one block, preamble + blocks, nested markdown, edge whitespace.

```go
func TestExtractBlocks(t *testing.T) {
    t.Parallel()
    tests := []struct {
        name string
        in   string
        want []Block
    }{
        {
            name: "no blocks — whole body is preamble",
            in:   "## Heading\n\nBody text.",
            want: []Block{{Name: "", Body: "## Heading\n\nBody text."}},
        },
        {
            name: "preamble + two blocks",
            in: `## Heading

Preamble line.

<block name="a">
### Subheading A
body A
</block>

<block name="b">
### Subheading B
body B
</block>`,
            want: []Block{
                {Name: "", Body: "## Heading\n\nPreamble line."},
                {Name: "a", Body: "### Subheading A\nbody A"},
                {Name: "b", Body: "### Subheading B\nbody B"},
            },
        },
        {
            name: "block with code fence inside",
            in: `<block name="yaml-example">
` + "```yaml" + `
foo: bar
` + "```" + `
</block>`,
            want: []Block{{Name: "yaml-example", Body: "```yaml\nfoo: bar\n```"}},
        },
    }
    for _, tt := range tests {
        tt := tt
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            got := ExtractBlocks(tt.in)
            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("got %+v, want %+v", got, tt.want)
            }
        })
    }
}
```

### 3.3 Predicate functions

Pure, side-effect free, table-tested.

```go
// internal/workflow/recipe_plan_predicates.go
package workflow

import "strings"

// isDualRuntime returns true for API-first showcases that have both an
// app role (frontend/static) and an api role (backend runtime).
func isDualRuntime(p *RecipePlan) bool {
    if p == nil {
        return false
    }
    var hasApp, hasAPI bool
    for _, t := range p.Targets {
        switch t.Role {
        case "app":
            hasApp = true
        case "api":
            hasAPI = true
        }
    }
    return hasApp && hasAPI
}

// hasWorker returns true when the plan declares at least one worker target.
func hasWorker(p *RecipePlan) bool {
    if p == nil {
        return false
    }
    for _, t := range p.Targets {
        if t.IsWorker {
            return true
        }
    }
    return false
}

// hasSharedCodebaseWorker returns true when a worker shares its codebase
// with another target. Drives the `setup: worker` block injection.
func hasSharedCodebaseWorker(p *RecipePlan) bool {
    if p == nil {
        return false
    }
    for _, t := range p.Targets {
        if t.IsWorker && t.SharesCodebaseWith != "" {
            return true
        }
    }
    return false
}

// hasSeparateCodebaseWorker returns true for workers without a sharing host.
func hasSeparateCodebaseWorker(p *RecipePlan) bool {
    if p == nil {
        return false
    }
    for _, t := range p.Targets {
        if t.IsWorker && t.SharesCodebaseWith == "" {
            return true
        }
    }
    return false
}

// hasServeOnlyProd returns true when any prod-facing target runs on a
// serve-only base (static, nginx) and therefore needs a dev-base override.
func hasServeOnlyProd(p *RecipePlan) bool {
    if p == nil {
        return false
    }
    for _, t := range p.Targets {
        if t.IsWorker {
            continue
        }
        base, _, _ := strings.Cut(t.Type, "@")
        if base == "static" || base == "nginx" {
            return true
        }
    }
    return false
}

// hasBundlerDevServer returns true when the recipe runs a framework whose
// dev server enforces an HTTP Host-header allow-list (Vite family, webpack,
// Angular CLI, Next.js dev). Drives the dev-server allow-list block.
func hasBundlerDevServer(p *RecipePlan) bool {
    if p == nil {
        return false
    }
    fw := strings.ToLower(p.Framework)
    for _, prefix := range bundlerFrameworks {
        if strings.HasPrefix(fw, prefix) {
            return true
        }
    }
    return false
}

var bundlerFrameworks = []string{
    "react", "vue", "svelte", "sveltekit", "nuxt", "next", "nextjs",
    "astro", "qwik", "angular", "remix", "solid", "solidstart",
    "analog", "react-router",
}

// hasManagedServiceCatalog returns true when the plan has at least one
// managed service (db, cache, queue, storage, search) whose env vars will
// need cataloging at provision. Drives the env-var catalog block.
func hasManagedServiceCatalog(p *RecipePlan) bool {
    if p == nil {
        return false
    }
    for _, t := range p.Targets {
        base, _, _ := strings.Cut(t.Type, "@")
        switch base {
        case "postgresql", "mariadb", "mysql", "mongodb", "keydb", "valkey",
            "redis", "nats", "kafka", "rabbitmq", "meilisearch",
            "elasticsearch", "typesense", "object-storage":
            return true
        }
    }
    return false
}

// hasMultipleCodebases returns true when the recipe has more than one
// codebase to scaffold (dual-runtime, or separate-codebase worker).
func hasMultipleCodebases(p *RecipePlan) bool {
    if p == nil {
        return false
    }
    return isDualRuntime(p) || hasSeparateCodebaseWorker(p)
}

// isShowcase returns true for showcase-tier recipes.
func isShowcase(p *RecipePlan) bool {
    return p != nil && p.Tier == RecipeTierShowcase
}
```

Every predicate gets a table-driven test with ~5-6 fixtures covering edge cases (nil plan, empty targets, every true branch, every false branch).

### 3.4 Empty section catalog

Empty scaffolding — Phase 5/6/7 fill it.

```go
// internal/workflow/recipe_section_catalog.go
package workflow

// sectionBlock pairs a block name with a predicate. A nil predicate means
// "always include". The name must match a <block name="..."> tag in the
// corresponding section of recipe.md.
type sectionBlock struct {
    Name      string
    Predicate func(*RecipePlan) bool
}

// Registered catalogs — filled in Phases 5a/6a/7a. Empty catalogs are a
// no-op: callers fall back to returning the section body verbatim.
var (
    recipeResearchBlocks  []sectionBlock
    recipeProvisionBlocks []sectionBlock
    recipeGenerateBlocks  []sectionBlock
    recipeDeployBlocks    []sectionBlock
    recipeFinalizeBlocks  []sectionBlock
    recipeCloseBlocks     []sectionBlock
)

// composeSection takes the raw body of a <section> and a catalog, extracts
// blocks, filters by predicate, and returns the composed body.
// If the catalog is empty, returns the raw body unchanged (back-compat).
func composeSection(sectionBody string, catalog []sectionBlock, plan *RecipePlan) string {
    if len(catalog) == 0 {
        return sectionBody
    }
    blocks := ExtractBlocks(sectionBody)
    byName := make(map[string]string, len(blocks))
    for _, b := range blocks {
        byName[b.Name] = b.Body
    }
    var out []string
    if preamble, ok := byName[""]; ok && preamble != "" {
        out = append(out, preamble)
    }
    for _, sb := range catalog {
        body, ok := byName[sb.Name]
        if !ok || body == "" {
            continue
        }
        if sb.Predicate != nil && !sb.Predicate(plan) {
            continue
        }
        out = append(out, body)
    }
    return strings.Join(out, "\n\n")
}
```

### 3.5 Catalog-coverage test (scaffolding, activated in P5-P7)

Add a test that asserts every `<block name="X">` tag in recipe.md has a corresponding catalog entry, and vice versa. Initially the recipe.md has no blocks, so the test is trivially green; it starts enforcing as P5/6/7 land.

```go
func TestCatalog_CoversAllMarkdownBlocks(t *testing.T) {
    t.Parallel()
    md, err := content.GetWorkflow("recipe")
    if err != nil {
        t.Fatal(err)
    }
    type sec struct {
        name    string
        catalog []sectionBlock
    }
    sections := []sec{
        {"research-minimal", recipeResearchBlocks},
        {"research-showcase", recipeResearchBlocks},
        {"provision", recipeProvisionBlocks},
        {"generate", recipeGenerateBlocks},
        {"deploy", recipeDeployBlocks},
        {"finalize", recipeFinalizeBlocks},
        {"close", recipeCloseBlocks},
    }
    for _, s := range sections {
        body := ExtractSection(md, s.name)
        if body == "" {
            continue
        }
        blocks := ExtractBlocks(body)
        inCatalog := make(map[string]bool)
        for _, cb := range s.catalog {
            inCatalog[cb.Name] = true
        }
        for _, b := range blocks {
            if b.Name == "" {
                continue
            }
            if !inCatalog[b.Name] {
                t.Errorf("section %q has <block name=%q> not in catalog", s.name, b.Name)
            }
        }
    }
}
```

### 3.6 Verify

```bash
go test ./internal/workflow/ -run "TestExtractBlocks|TestIsDualRuntime|TestHas|TestCatalog_CoversAllMarkdownBlocks" -v
go test ./internal/workflow/... -count=1
```

**Expected**: new parser + predicate tests GREEN; catalog-coverage test trivially GREEN; full suite GREEN (no caller uses this infrastructure yet).

**Commit**: `feat(recipe): block parser + predicate catalog infrastructure`

**Rollback**: safe — no caller uses any of this yet.

---

## Phase 4 — chain recipe extraction refactor

**Goal**: replace the blunt "stop at `## 1.`" extractor with surgical extraction that returns `## Gotchas` + YAML code fence for direct predecessors and `## Gotchas` (or empty) for ancestors.

**Why now**: this refactor directly reduces the chain's injected size by ~4-5 KB on showcase recipes and fixes the "ancestor returns intro filler" bug. It must land before the generate section's cap is measured for tightening in Phase 11. Landing it before P5 also means the generate composition already has a smaller chain baked in when predicates are wired up.

**Files touched**: 2
- [internal/workflow/recipe_knowledge_chain.go](../internal/workflow/recipe_knowledge_chain.go)
- [internal/workflow/recipe_knowledge_chain_test.go](../internal/workflow/recipe_knowledge_chain_test.go)

### 4.1 Replace `extractKnowledgeSections`

Current function (at line 146) returns everything before `## 1.` — it captures filler intro prose when a recipe has no `## Gotchas` H2, which is the case for every hello-world in the store. Replace with two targeted extractors:

```go
// extractForShowcase extracts content relevant to a direct-predecessor
// (tier delta = 1) injection: the Gotchas H2 plus the YAML code fence
// from "## 1. Adding `zerops.yaml`". Integration-step prose (trust proxy,
// bind 0.0.0.0, env var wiring) is dropped because it teaches existing-app
// integration, not generation.
func extractForShowcase(content string) string {
    parts := []string{}
    if g := extractH2Section(content, "Gotchas"); g != "" {
        parts = append(parts, "## Gotchas\n\n"+g)
    }
    if tmpl := extractYAMLTemplate(content); tmpl != "" {
        parts = append(parts, "## zerops.yaml template (from minimal recipe)\n\n"+tmpl)
    }
    return strings.Join(parts, "\n\n")
}

// extractForAncestor extracts only the Gotchas H2 from an ancestor recipe
// (tier delta ≥ 2). Returns empty string if no Gotchas section exists —
// do NOT emit intro filler prose as fake gotchas.
func extractForAncestor(content string) string {
    g := extractH2Section(content, "Gotchas")
    if g == "" {
        return ""
    }
    return "## Gotchas (from ancestor recipe)\n\n" + g
}

// extractH2Section returns the body of the first H2 matching the given title
// (exact match after "## "). Returns "" if not found.
func extractH2Section(content, title string) string {
    lines := strings.Split(content, "\n")
    inside := false
    var out []string
    for _, l := range lines {
        if strings.HasPrefix(l, "## ") {
            if inside {
                break
            }
            if strings.TrimPrefix(l, "## ") == title {
                inside = true
                continue
            }
            continue
        }
        if strings.HasPrefix(l, "# ") {
            if inside {
                break
            }
            continue
        }
        if inside {
            out = append(out, l)
        }
    }
    return strings.TrimSpace(strings.Join(out, "\n"))
}

// extractYAMLTemplate finds the first "## 1. Adding `zerops.yaml`" H2 and
// returns the first fenced yaml code block inside it. Integration-step
// prose before/after the fence is dropped.
func extractYAMLTemplate(content string) string {
    h2Body := ""
    lines := strings.Split(content, "\n")
    inside := false
    for _, l := range lines {
        if strings.HasPrefix(l, "## 1.") || strings.HasPrefix(l, "## 1 ") {
            inside = true
            continue
        }
        if inside && strings.HasPrefix(l, "## ") {
            break
        }
        if inside {
            h2Body += l + "\n"
        }
    }
    if h2Body == "" {
        return ""
    }
    // Find the first yaml code fence.
    fenceRe := regexp.MustCompile("(?s)```ya?ml\\s*\\n(.*?)\\n```")
    m := fenceRe.FindStringSubmatch(h2Body)
    if m == nil {
        return ""
    }
    return "```yaml\n" + m[1] + "\n```"
}
```

### 4.2 Wire the extractors into `recipeKnowledgeChain`

Update the body of `recipeKnowledgeChain` (line 60-82):

```go
for _, c := range candidates {
    tierDelta := currentRank - c.rank

    content, err := kp.GetRecipe(c.name, "")
    if err != nil || content == "" {
        continue
    }

    if tierDelta == 1 {
        extracted := extractForShowcase(content)
        if extracted == "" {
            continue
        }
        header := fmt.Sprintf(
            "## %s Recipe Knowledge (predecessor)\n\n"+
            "Gotchas + zerops.yaml template from the direct predecessor recipe. "+
            "Use the template as your starting point; adapt keys/services to your targets.\n\n",
            c.name,
        )
        parts = append(parts, header+extracted)
    } else {
        extracted := extractForAncestor(content)
        if extracted == "" {
            // Silently skip — ancestor has no Gotchas section.
            continue
        }
        header := fmt.Sprintf(
            "## %s Platform Knowledge (ancestor gotchas)\n\n"+
            "Platform-specific gotchas from a more basic recipe in the same runtime. "+
            "zerops.yaml config is omitted — your recipe has its own.\n\n",
            c.name,
        )
        parts = append(parts, header+extracted)
    }
}
```

### 4.3 Tests

Three table-driven tests:

```go
func TestExtractForShowcase(t *testing.T) {
    // Real nestjs-minimal content → expect Gotchas H2 + yaml code fence,
    // NOT "## 2. Trust proxy and bind 0.0.0.0" prose.
}

func TestExtractForAncestor_NoGotchas_ReturnsEmpty(t *testing.T) {
    // Real nodejs-hello-world content → expect "" (no Gotchas H2).
    // Regression guard against emitting "This recipe demonstrates..." filler.
}

func TestRecipeKnowledgeChain_NestjsShowcase(t *testing.T) {
    // Plan with framework=nestjs, runtime=nodejs@22, tier=showcase.
    // Assert the returned chain:
    //   - contains "nestjs-minimal Recipe Knowledge (predecessor)"
    //   - contains a ```yaml fence
    //   - does NOT contain "## 2. Trust proxy"
    //   - does NOT contain "nodejs-hello-world" header (ancestor has no Gotchas)
    //   - total chain size under 3.5 KB
}
```

### 4.4 Verify

```bash
go test ./internal/workflow/ -run "TestExtractForShowcase|TestExtractForAncestor|TestRecipeKnowledgeChain" -v
go test ./internal/workflow/ -count=1
go test -tags audit ./internal/workflow/ -run TestAuditComposition -v 2>&1 | grep -E "chain|Chain|===.*GENERATE"
```

**Expected**: chain size for dual-runtime showcase drops from ~7 KB to ~2.5-3 KB. Audit dump shows the shrinking chain part. Cap test still GREEN (caps are loose).

**Commit**: `refactor(recipe): surgical chain recipe extraction (Gotchas + YAML template only)`

**Rollback**: safe — isolated to `recipe_knowledge_chain.go`, no schema change.

---

## Phase 5 — convert `generate` section to `<block>` composition

**Two sub-phases**: 5a wraps existing content in `<block>` tags with all-`nil` predicates (pure refactor, zero behavior change). 5b switches predicates to real values (behavior change, measurable).

**Files touched (5a)**: 2
- [internal/content/workflows/recipe.md](../internal/content/workflows/recipe.md)
- [internal/workflow/recipe_section_catalog.go](../internal/workflow/recipe_section_catalog.go)

**Files touched (5b)**: 1
- [internal/workflow/recipe_section_catalog.go](../internal/workflow/recipe_section_catalog.go)

### 5.1 Block design — generate section

Based on the measured seams (see "Measurement harness" above), the generate section gets these blocks:

| Block name | Covers | Predicate (set in 5b) | Size |
|---|---|---|---|
| `execution-order` | `### Execution order` | `nil` (always) | 1.9 KB |
| `container-state` | `### Container state during generate` | `nil` | 1.3 KB |
| `where-to-write-files-single` | `### WHERE to write files` — single-runtime variant | `!hasMultipleCodebases` | 0.8 KB |
| `where-to-write-files-multi` | `### WHERE to write files` — multi-codebase variant + per-codebase README rule | `hasMultipleCodebases` | 2.0 KB |
| `what-to-generate-showcase` | `### What to generate per recipe type` — showcase-tier content | `isShowcase` | 1.2 KB |
| `two-kinds-of-import-yaml` | `### Two kinds of import.yaml` — generic setup names rule | `nil` (1-line version — see 5b) | 0.3 KB |
| `zerops-yaml-header` | `### zerops.yaml — Write ALL setups at once` preamble | `nil` | 1.9 KB |
| `dual-runtime-url-shapes` | `#### Dual-runtime URL env-var pattern` — YAML shape blocks only | `isDualRuntime` | 3.5 KB |
| `dual-runtime-consumption` | `#### Dual-runtime URL env-var pattern` — consumption paragraph + setup:dev rules specific to dual | `isDualRuntime` | 1.0 KB |
| `serve-only-dev-override` | serve-only runtime dev override rule (currently inside the dual-runtime H4) | `hasServeOnlyProd` | 0.8 KB |
| `dev-server-host-check` | dev-server allow-list rule | `hasBundlerDevServer` | 1.2 KB |
| `dev-dep-preinstall` | multi-base dev buildCommands rule | `hasMultiBaseBuildCommand` | 0.7 KB |
| `worker-setup-block` | `setup: worker` shape (shared-codebase worker only) | `hasSharedCodebaseWorker` | 0.7 KB |
| `project-env-vars-pointer` | 1-line pointer to finalize Step 1b | `nil` | 0.1 KB |
| `env-example-preservation` | `### .env.example preservation` | `nil` | 0.5 KB |
| `dashboard-skeleton` | `### Write the dashboard skeleton` | `isShowcase` | 2.2 KB |
| `asset-pipeline-consistency` | `### Asset pipeline consistency` | `nil` | 0.6 KB |
| `readme-with-fragments` | `### App README with extract fragments` | `nil` | 1.1 KB |
| `gotchas-header` | `### Gotchas` H3 | `nil` | 0.6 KB |
| `code-quality` | `### Code Quality` | `nil` | 0.4 KB |
| `pre-deploy-checklist` | `### Pre-deploy checklist` | `nil` | 1.4 KB |

Note: the current 12.8 KB `#### Dual-runtime URL env-var pattern` H4 **is split** across 5 blocks in 5a — it's not a single block. The current prose conflates three independent concerns (dual-runtime URL shapes, serve-only dev override, dev-server host-check, dev-dep pre-install, setup:worker) and each conflation pays full cost on every session. Splitting along the natural concern boundaries is the single biggest generate-step win.

### 5.2 — 5a: mechanical wrap, no predicates

**Step 1**: edit [recipe.md](../internal/content/workflows/recipe.md) to wrap each seam from the table above in `<block name="X">...</block>`. Move zero words of content; only add tags. Use empty lines around tags so the markdown still renders correctly when read outside the parser.

**Step 2**: fill `recipeGenerateBlocks` catalog in [recipe_section_catalog.go](../internal/workflow/recipe_section_catalog.go) with every block name from the table, all predicates set to `nil`.

**Step 3**: update [recipe_guidance.go](../internal/workflow/recipe_guidance.go) `resolveRecipeGuidance` to route the generate case through `composeSection`:

```go
case RecipeStepGenerate:
    body := ExtractSection(md, "generate")
    parts = append(parts, composeSection(body, recipeGenerateBlocks, plan))
```

With every predicate `nil`, `composeSection` outputs every block in catalog order — same content as before, just re-concatenated.

### 5.3 — 5a verification

```bash
go test ./internal/workflow/ -run "TestCatalog_CoversAllMarkdownBlocks|TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap" -v
go test ./internal/workflow/ -count=1
go test -tags audit ./internal/workflow/ -run TestAuditComposition -v 2>&1 | grep -A 5 "=== GENERATE"
```

**Expected**: catalog-coverage test GREEN (every recipe.md block has catalog entry, every catalog entry has markdown block). Audit dump shows generate total within ±500 B of the pre-5a baseline (the difference is only the separator whitespace).

**Commit**: `refactor(recipe): wrap generate section in <block> tags (no predicate gating yet)`

**Rollback**: single git revert unwraps the tags and clears the catalog.

### 5.4 — 5b: switch predicates to real values

Edit the catalog entries to their real predicates per the table above. Example final shape:

```go
var recipeGenerateBlocks = []sectionBlock{
    {Name: "execution-order", Predicate: nil},
    {Name: "container-state", Predicate: nil},
    {Name: "where-to-write-files-single", Predicate: func(p *RecipePlan) bool { return !hasMultipleCodebases(p) }},
    {Name: "where-to-write-files-multi", Predicate: hasMultipleCodebases},
    {Name: "what-to-generate-showcase", Predicate: isShowcase},
    {Name: "two-kinds-of-import-yaml", Predicate: nil},
    {Name: "zerops-yaml-header", Predicate: nil},
    {Name: "dual-runtime-url-shapes", Predicate: isDualRuntime},
    {Name: "dual-runtime-consumption", Predicate: isDualRuntime},
    {Name: "serve-only-dev-override", Predicate: hasServeOnlyProd},
    {Name: "dev-server-host-check", Predicate: hasBundlerDevServer},
    {Name: "dev-dep-preinstall", Predicate: hasMultiBaseBuildCommand},
    {Name: "worker-setup-block", Predicate: hasSharedCodebaseWorker},
    {Name: "project-env-vars-pointer", Predicate: nil},
    {Name: "env-example-preservation", Predicate: nil},
    {Name: "dashboard-skeleton", Predicate: isShowcase},
    {Name: "asset-pipeline-consistency", Predicate: nil},
    {Name: "readme-with-fragments", Predicate: nil},
    {Name: "gotchas-header", Predicate: nil},
    {Name: "code-quality", Predicate: nil},
    {Name: "pre-deploy-checklist", Predicate: nil},
}
```

### 5.5 — 5b test shape

New targeted tests per block:

```go
func TestRecipeGenerate_HelloWorld_OmitsShowcaseBlocks(t *testing.T) {
    plan := fixtureForShape(ShapeHelloWorld)
    guide := resolveRecipeGuidance(RecipeStepGenerate, plan.Tier, plan)
    for _, shouldNotContain := range []string{
        "Dual-runtime URL env-var pattern",
        "dashboard skeleton",
        "setup: worker",
    } {
        if strings.Contains(guide, shouldNotContain) {
            t.Errorf("hello-world guide contains %q, should be omitted", shouldNotContain)
        }
    }
}

func TestRecipeGenerate_BackendMinimal_OmitsDualRuntimeContent(t *testing.T) {
    plan := fixtureForShape(ShapeBackendMinimal)
    guide := resolveRecipeGuidance(RecipeStepGenerate, plan.Tier, plan)
    for _, shouldNotContain := range []string{
        "Dual-runtime URL env-var pattern",
        "dev-server host-check allow-list", // laravel is not bundler
    } {
        if strings.Contains(guide, shouldNotContain) {
            t.Errorf("backend-minimal guide contains %q", shouldNotContain)
        }
    }
}

func TestRecipeGenerate_DualRuntimeShowcase_IncludesAllRelevant(t *testing.T) {
    plan := fixtureForShape(ShapeDualRuntimeShowcase)
    guide := resolveRecipeGuidance(RecipeStepGenerate, plan.Tier, plan)
    for _, mustContain := range []string{
        "Dual-runtime URL env-var pattern",
        "dev-server host-check",
        "setup: worker",
        "dashboard skeleton",
        "each codebase", // per-codebase README rule
    } {
        if !strings.Contains(guide, mustContain) {
            t.Errorf("dual-runtime-showcase guide missing %q", mustContain)
        }
    }
}
```

### 5.6 — 5b verification

```bash
go test ./internal/workflow/ -run TestRecipeGenerate -v
go test -tags audit ./internal/workflow/ -run TestAuditComposition -v 2>&1 | grep -E "^===|SHAPE"
```

**Expected**:

| Shape | Generate size (5a) | Generate size (5b) |
|---|---|---|
| hello-world | ~30 KB | **~8 KB** |
| backend-minimal | ~30 KB | **~14 KB** |
| full-stack showcase | ~30 KB | **~16 KB** |
| dual-runtime showcase | ~30 KB | **~21 KB** |

**Commit**: `feat(recipe): predicate-gated generate section composition`

**Rollback**: revert the predicate edits, leaving the block wrap in place. Caps relax but everything else stays.

---

## Phase 6 — convert `provision` section to `<block>` composition

**Same pattern as Phase 5.** Provision's current content is smaller and more uniform, but it still has shape-specific rules mixed with universal ones.

**Files touched (6a)**: 2 — recipe.md, recipe_section_catalog.go
**Files touched (6b)**: 1 — recipe_section_catalog.go

### 6.1 Block design — provision section

| Block name | Covers | Predicate | Size |
|---|---|---|---|
| `provision-framing` | preamble: the agent's goal at provision | `nil` | 0.4 KB |
| `import-yaml-generation` | `### 1. Generate import.yaml` common rules | `nil` | 2.0 KB |
| `import-yaml-managed-services` | managed service provisioning rules | `hasManagedServiceCatalog` | 1.0 KB |
| `import-yaml-dual-runtime` | dual-runtime workspace service wiring | `isDualRuntime` | 0.6 KB |
| `import-yaml-worker-separate` | separate-codebase worker provisioning | `hasSeparateCodebaseWorker` | 0.4 KB |
| `mount-setup` | `### 2. Mount dev filesystems` | `nil` | 0.5 KB |
| `git-config-mount` | `### 3a. Configure git on the mount` | `nil` (LOG2 bug 2) | 1.3 KB |
| `git-init-per-codebase` | git init on each codebase mount | `hasMultipleCodebases` | 0.4 KB |
| `env-var-discovery` | `### 4. Discover env vars` | `hasManagedServiceCatalog` (LOG2 bug 3) | 1.5 KB |
| `provision-attestation` | final attestation shape | `nil` | 0.3 KB |

### 6.2 6a + 6b + verification mirror Phase 5's steps.

**Expected after 6b**:

| Shape | Provision size |
|---|---|
| hello-world | ~7 KB |
| backend-minimal | ~10 KB |
| full-stack showcase | ~12 KB |
| dual-runtime showcase | ~13 KB |

**Commits**:
- `refactor(recipe): wrap provision section in <block> tags (no predicates)`
- `feat(recipe): predicate-gated provision section composition`

---

## Phase 7 — convert `deploy` section to `<block>` composition

The deploy section is the hardest conversion: its `### Dev deployment flow` H3 has 18 KB of preamble before any H4, holding Steps 1-5 + the sub-agent brief + UX contract + where-commands-run + browser walk intermixed. **This phase requires inserting new H4 seams before wrapping.**

**Files touched (7a)**: 2 — recipe.md (with new H4 seams), recipe_section_catalog.go
**Files touched (7b)**: 1 — recipe_section_catalog.go

### 7.1 Insert new H4 seams in Dev deployment flow

Before wrapping, restructure `### Dev deployment flow` to have these H4s (pure editorial — no word changes):

```
### Dev deployment flow
  #### Step 1: First deploy
  #### Step 2: SSH into appdev and install deps
  #### Step 3: Start the dev process
  #### Step 4a: Verify the dev process is running
  #### Step 4b: Dashboard feature sub-agent (showcase only)
  #### Step 4c: Browser walk (showcase only)
  #### Step 5: Catalog env vars and attest
```

This alone is a safe refactor — H4s are added but content stays identical. Run the cap test after to confirm.

### 7.2 Block design — deploy section

| Block name | Covers | Predicate | Size |
|---|---|---|---|
| `deploy-framing` | preamble | `nil` | 0.4 KB |
| `dev-deploy-steps-1-3` | first deploy, SSH, start dev | `nil` | 3.5 KB |
| `dev-deploy-verify` | Step 4a verify running process | `nil` | 1.5 KB |
| `dev-deploy-subagent-brief` | Step 4b sub-agent dispatch | `isShowcase` | 4.0 KB |
| `where-commands-run` | the principle block | `nil` (referenced from close block in Phase 9) | 2.5 KB |
| `ux-quality-contract` | UX contract bullets | `isShowcase` | 1.0 KB |
| `browser-walk-rules` | `#### Non-negotiable rules` | `isShowcase` | 2.2 KB |
| `browser-walk-vocabulary` | `#### Efficient command vocabulary` | `isShowcase` | 1.5 KB |
| `browser-walk-flow` | `#### Canonical verification flow` | `isShowcase` | 4.0 KB |
| `vite-collision-trap` | "already running" rule (LOG2 bug 13) | `hasBundlerDevServer` | 0.5 KB |
| `cataloging-env-vars` | Step 5 env var catalog | `hasManagedServiceCatalog` | 1.0 KB |
| `stage-deployment-flow` | `### Stage deployment flow` | `nil` | 4.0 KB |
| `parallel-cross-deploy` | stage parallel pattern | `hasMultipleCodebases` | 0.8 KB |
| `reading-deploy-failures` | `### Reading deploy failures` + `### Common deployment issues` (merged) | `nil` | 3.0 KB |
| `targetservice-warning` | LOG2 bug 12 (`Parameter naming`) | `nil` | 0.4 KB |
| `zsc-execonce-trap` | LOG2 bug 9 (`burn-on-failure`) | `nil` | 0.4 KB |

**Merge of `### Reading deploy failures` + `### Common deployment issues`**: these are currently two adjacent H3s with overlapping BUILD_FAILED content. Merging them into one block is the only prose consolidation in this phase — captured under `reading-deploy-failures`.

### 7.3 7a + 7b + verification mirror Phase 5's steps.

**Expected after 7b**:

| Shape | Deploy size |
|---|---|
| hello-world | ~11 KB |
| backend-minimal | ~16 KB |
| full-stack showcase | ~19 KB |
| dual-runtime showcase | ~22 KB |

**Commits**:
- `refactor(recipe): insert H4 seams in deploy Dev deployment flow`
- `refactor(recipe): wrap deploy section in <block> tags (no predicates)`
- `feat(recipe): predicate-gated deploy section composition`

---

## Phase 8 — on-demand schema pointer + provision field cleanup

**Goal**: replace eager injection of `import.yaml Schema` H2 (provision) and `zerops.yaml Schema` H2 (generate) with an inline common-field list + `zerops_knowledge scope="theme"` pointer. Simultaneously drop `envSecrets`, `dotEnvSecrets`, and preprocessor-function guidance from provision per the architectural correction (runtime secrets are set via `zerops_env` during iteration; deliverable secrets belong at finalize).

**Why now**: blocks are in place (Phases 5-7), so the replacement cleanly slots into the provision/generate catalogs without re-opening the parser or predicate questions.

**Files touched**: 3
- [internal/workflow/recipe_guidance.go](../internal/workflow/recipe_guidance.go)
- [internal/content/workflows/recipe.md](../internal/content/workflows/recipe.md) (two small additions)
- [internal/workflow/bootstrap_guidance_test.go](../internal/workflow/bootstrap_guidance_test.go) (update env var catalog assertions)

### 8.1 Stop injecting full schema H2s

In [recipe_guidance.go](../internal/workflow/recipe_guidance.go) `assembleRecipeKnowledge`:

**Remove** the `getCoreSection(kp, "import.yaml Schema")` call from the provision case (currently around line 130).

**Remove** the `getCoreSection(kp, "zerops.yaml Schema")` call from the generate case (currently around line 162). The chain recipe now carries the template via Phase 4's `extractForShowcase`.

### 8.2 Add `provision-schema-inline` block to recipe.md

New block inside `<section name="provision">`, registered in the catalog:

```markdown
<block name="provision-schema-inline">
### Workspace import.yaml fields you actually write here

The workspace import creates service shells inside an existing project. These are the fields that apply:

- `hostname` (string, max 40, [a-z0-9] only, immutable)
- `type` (`<runtime>@<version>`, pick highest from `availableStacks`)
- `mode` (`HA` | `NON_HA`, managed services only, immutable)
- `priority` (int; db/storage: `10` so they start first)
- `enableSubdomainAccess` (bool, true for publicly reachable dev services)
- `startWithoutCode` (bool, dev services only — container starts RUNNING without a deploy)
- `minContainers` (int, dev services = 1 — SSHFS needs single container)
- `objectStorageSize` (int, GB, object-storage services only)
- `verticalAutoscaling` (runtime + managed DB/cache; compiled runtimes need higher dev `minRam`)

**Not at provision**:
- `project:` block — the project already exists; API rejects.
- Project-level `envVariables` — cannot be added via workspace import. Set them via `zerops_env set` when you know what keys the app needs, or bake them into the deliverable `import.yaml` files at finalize.
- Service-level `envSecrets` / `dotEnvSecrets` — same reason. During iteration use `zerops_env set`; for the deliverable imports, finalize has the full set and writes them there.
- `zeropsSetup` / `buildFromGit` — deliverable-only fields, not workspace.
- Preprocessor functions (`<@generateRandomString>` etc.) — belong at finalize where the deliverable import is generated.

**Need more**: `zerops_knowledge scope="theme" query="import.yaml Schema"` returns the full reference with every exotic field.
</block>
```

Predicate: `nil`. Replaces the 4.9 KB core-section injection with ~1.2 KB of content that's closer to what the agent actually needs and is explicit about the deliberate omissions.

### 8.3 Add `generate-schema-pointer` block

Smaller — the chain recipe carries the template, so at generate we only need the pointer:

```markdown
<block name="generate-schema-pointer">
### zerops.yaml field reference

The injected chain recipe's `## zerops.yaml template` section is the primary source: it's the same shape you're writing, for a recipe in the same framework family. Exotic fields (buildFromGit, cache layers, per-environment overrides) are not in that template.

**Need more**: `zerops_knowledge scope="theme" query="zerops.yaml Schema"`.
</block>
```

Predicate: `nil`. Replaces the ~5-11 KB `zerops.yaml Schema` H2 with 400 bytes.

### 8.4 Replace env var catalog with pointer (subsumes P3 of the old plan)

In [bootstrap_guide_assembly.go](../internal/workflow/bootstrap_guide_assembly.go) `formatEnvVarsForGuide`, replace the full markdown table with a compact pointer to the provision-step attestation (agent already recorded the authoritative key names there):

```go
func formatEnvVarsForGuide(envVars map[string][]string) string {
    if len(envVars) == 0 {
        return ""
    }
    hostnames := make([]string, 0, len(envVars))
    for h := range envVars {
        hostnames = append(hostnames, h)
    }
    sort.Strings(hostnames)

    var sb strings.Builder
    sb.WriteString("## Discovered Env Var Catalog — attestation-recorded\n\n")
    sb.WriteString("Services with env vars discovered at provision: ")
    parts := make([]string, 0, len(hostnames))
    for _, h := range hostnames {
        parts = append(parts, fmt.Sprintf("`%s` (%d keys)", h, len(envVars[h])))
    }
    sb.WriteString(strings.Join(parts, ", "))
    sb.WriteString(".\n\n")
    sb.WriteString("The authoritative key names are in your prior-context provision attestation (`priorContext.Attestations.provision`). Quote them verbatim when writing `run.envVariables` cross-service refs — guessing key names fails silently (unknown `${hostname_varname}` resolves to literal string at runtime).\n\n")
    sb.WriteString("If the attestation doesn't quote key names, re-run `zerops_discover` at generate — it's idempotent and cheap.\n")
    return sb.String()
}
```

Update [bootstrap_guidance_test.go:612](../internal/workflow/bootstrap_guidance_test.go#L612) to match the new needle:

```go
if !strings.Contains(guide, "Discovered Env Var Catalog — attestation-recorded") {
    t.Error("generate guide should contain discovered env var catalog pointer")
}
if !strings.Contains(guide, "`db`") {
    t.Error("generate guide should mention discovered service hostnames")
}
```

### 8.5 Verify

```bash
go build ./...
go test ./internal/workflow/... -count=1
go test -tags audit ./internal/workflow/ -run TestAuditComposition -v 2>&1 | grep -E "SHAPE|^==="
```

**Expected**: provision drops by ~4.5 KB across all shapes; generate drops by ~5-8 KB across all shapes (more savings on recipes that previously got the full Schema H2 fallback when chain recipe was missing).

**Commit**: `refactor(recipe): on-demand schema pointers + drop runtime-secret guidance from provision`

**Rollback**: revert the three file changes; the removed `getCoreSection` calls come back.

---

## Phase 9 — drop the two verified duplications

**Goal**: eliminate the only two content duplications that survive after Phases 1-8.

After the block composition lands, almost every trim the old plan proposed becomes moot (either the content isn't injected for the current shape, or it's the only reference for the content). These two duplications remain:

1. **`projectEnvVariables` handoff example** — full block at generate (~1.5 KB) duplicates the authoritative version at finalize Step 1b.
2. **"CRITICAL — where commands run" block** inside close's 1a sub-agent prompt template (~2.6 KB, blockquoted) duplicates deploy's `where-commands-run` block.

**Files touched**: 1 — [recipe.md](../internal/content/workflows/recipe.md)

### 9.1 Replace the generate-step `projectEnvVariables` example with a pointer

Inside the `dual-runtime-consumption` block, replace the ~1.5 KB handoff example with:

```markdown
**For the 6 deliverable import.yaml files**: pass `projectEnvVariables` as a first-class input to `zerops_workflow action="generate-finalize"` at finalize — the full per-env shape lives in finalize Step 1b. Do NOT hand-edit the generated files; re-running `generate-finalize` re-renders from template.
```

Saves ~1.2 KB when the dual-runtime blocks fire.

### 9.2 Replace the close-step "where commands run" block with a pointer

The close section's `### 1a. Static Code Review Sub-Agent` sub-agent prompt template contains a 2.6 KB blockquoted "CRITICAL — where commands run" block that restates deploy's `where-commands-run` block in different wording. Replace with:

```markdown
> **CRITICAL — where commands run**: you are on the zcp orchestrator, not the target container. `{appDir}` is an SSHFS mount. All target-side commands (compilers, test runners, linters, package managers, framework CLIs, app-level `curl`) MUST run via `ssh {hostname} "cd /var/www && ..."`, not against the mount. Deploy step's `where-commands-run` block has the principle — follow it. If you see `fork failed: resource temporarily unavailable` or `pthread_create: Resource temporarily unavailable`, you ran a target-side command on zcp via the mount.
```

Saves ~2.0 KB. Sub-agent prompts are read as inline text by the sub-agent, so the deploy block is naturally available when the main agent pastes the full sub-agent prompt (which already happens).

### 9.3 Verify

```bash
go test ./internal/workflow/... -count=1
go test -tags audit ./internal/workflow/ -run TestAuditComposition -v 2>&1 | grep -E "^===.*GENERATE|^===.*CLOSE|SHAPE"
```

**Expected**: dual-runtime showcase generate drops another ~1.2 KB; all shapes' close drops ~2 KB.

**Commit**: `refactor(recipe): replace two verified duplications with cross-references`

**Rollback**: single revert restores both blocks.

---

## Phase 10 — iteration-aware generate delta

**Goal**: extend the existing retry-delta mechanism from deploy to generate. On retry, replace the full ~14-21 KB generate composition with a focused ~3-4 KB delta that surfaces only what failed last time.

**Why now**: with Phases 5-9 landed, the first-attempt guide is already narrow. The iteration delta makes retries narrower still — a 2-retry session that previously read ~130 KB of generate guidance total reads ~35 KB.

**Current state** — the gate already exists at [recipe_guidance.go:16](../internal/workflow/recipe_guidance.go#L16):

```go
func (r *RecipeState) buildGuide(step string, iteration int, kp knowledge.Provider) string {
    // Iteration delta for deploy retries — replaces normal guidance.
    if iteration > 0 && step == RecipeStepDeploy {
        if delta := buildRecipeIterationDelta(iteration, r.lastAttestation()); delta != "" {
            return delta
        }
    }
    // ...
}
```

`buildRecipeIterationDelta` ([recipe_guidance.go:192](../internal/workflow/recipe_guidance.go#L192)) currently delegates to `BuildIterationDelta` ([bootstrap_guidance.go:108](../internal/workflow/bootstrap_guidance.go#L108)) which emits generic escalation tiers. For generate, **generic escalation is not enough** — a retrying generate agent needs specifically: (a) a reminder of what they attested to last iteration, (b) the most-common failure modes for *this* plan shape, (c) a pointer back to the chain recipe template.

So Phase 10 adds a new, generate-specific delta function rather than reusing `BuildIterationDelta`. The deploy arm is unchanged.

**Files touched**: 2
- [internal/workflow/recipe_guidance.go](../internal/workflow/recipe_guidance.go)
- [internal/workflow/recipe_guidance_test.go](../internal/workflow/recipe_guidance_test.go)

### 10.1 Add the generate arm

Extend the existing gate at the top of `buildGuide`:

```go
func (r *RecipeState) buildGuide(step string, iteration int, kp knowledge.Provider) string {
    // Iteration delta for deploy retries — replaces normal guidance.
    if iteration > 0 && step == RecipeStepDeploy {
        if delta := buildRecipeIterationDelta(iteration, r.lastAttestation()); delta != "" {
            return delta
        }
    }
    // Iteration delta for generate retries — shape-aware, plan-specific.
    if iteration > 0 && step == RecipeStepGenerate {
        if delta := buildGenerateRetryDelta(r.Plan, r.lastAttestation()); delta != "" {
            return delta
        }
    }
    // ... existing assembly ...
}
```

### 10.2 Implement `buildGenerateRetryDelta`

New function in [recipe_guidance.go](../internal/workflow/recipe_guidance.go), placed near `buildRecipeIterationDelta`:

```go
// buildGenerateRetryDelta returns a focused delta for iteration > 0 at generate.
// The agent has already read the full generate composition once; on retry
// they need: (a) a reminder of what they attested to last time, (b) the
// most-common failure modes filtered by plan shape, (c) a pointer back to
// the chain recipe as the source of truth for zerops.yaml shape.
//
// Deliberately NOT using BuildIterationDelta — that's a generic escalation
// tier emitter suited for deploy where "try again with more focus" is the
// right posture. Generate retries benefit from shape-specific failure-mode
// reminders keyed off the predicates from recipe_plan_predicates.go.
func buildGenerateRetryDelta(plan *RecipePlan, lastAttestation string) string {
    var sb strings.Builder
    sb.WriteString("## Generate — Retry\n\n")
    sb.WriteString("You've already read the full generate guide this session. This is a focused delta.\n\n")

    if lastAttestation != "" {
        sb.WriteString("### What you attested to last iteration\n\n```\n")
        sb.WriteString(lastAttestation)
        sb.WriteString("\n```\n\n")
    }

    sb.WriteString("### Common retry causes\n\n")
    sb.WriteString("- Comment ratio <30% in zerops.yaml — recount, aim for 35%.\n")
    sb.WriteString("- Env var references used guessed names — the provision-step attestation has the authoritative list.\n")
    sb.WriteString("- Missing `setup: dev` block for at least one deployable target.\n")
    sb.WriteString("- dev and prod envVariables bit-identical — mode flags must differ (a structural check fails otherwise).\n")
    sb.WriteString("- README missing one of the three extract fragments.\n")

    if isDualRuntime(plan) {
        sb.WriteString("- Dual-runtime URL references in `run.envVariables` using hardcoded hosts instead of `${STAGE_*}` / `${DEV_*}`.\n")
    }
    if hasBundlerDevServer(plan) {
        sb.WriteString("- Dev-server host-check not updated — framework config still rejects `.zerops.app`.\n")
    }
    if hasSharedCodebaseWorker(plan) {
        sb.WriteString("- Missing `setup: worker` block in the host target's zerops.yaml.\n")
    }

    sb.WriteString("\n### Source of truth\n\n")
    sb.WriteString("The injected chain recipe's `## zerops.yaml template` section (from your first read-through this session) is authoritative for shape. Re-read it and diff against your output before submitting.\n")
    return sb.String()
}
```

### 10.3 Test

```go
func TestBuildGenerateRetryDelta_IsShort(t *testing.T) {
    t.Parallel()
    for _, shape := range []RecipeShape{ShapeHelloWorld, ShapeDualRuntimeShowcase} {
        shape := shape
        t.Run(fmt.Sprint(shape), func(t *testing.T) {
            t.Parallel()
            plan := fixtureForShape(shape)
            delta := buildGenerateRetryDelta(plan, "last attestation: wrote zerops.yaml for app+api+worker")
            if len(delta) > 5*1024 {
                t.Errorf("shape %v retry delta %d B > 5 KB cap", shape, len(delta))
            }
            if !strings.Contains(delta, "Retry") {
                t.Errorf("shape %v retry delta missing retry marker", shape)
            }
            if !strings.Contains(delta, "last attestation") {
                t.Errorf("shape %v retry delta missing last attestation passthrough", shape)
            }
        })
    }
}

func TestBuildGuide_Generate_Iteration1_ReturnsDelta(t *testing.T) {
    t.Parallel()
    plan := fixtureForShape(ShapeBackendMinimal)
    rs := advanceShowcaseStateTo(RecipeStepGenerate, plan)
    // Inject a prior-iteration attestation.
    for i := range rs.Steps {
        if rs.Steps[i].Name == RecipeStepGenerate {
            // Place a prior attestation on a preceding step so lastAttestation() finds it.
        }
    }
    full := rs.buildGuide(RecipeStepGenerate, 0, nil)
    delta := rs.buildGuide(RecipeStepGenerate, 1, nil)
    if len(delta) >= len(full) {
        t.Errorf("delta %d B should be smaller than full %d B", len(delta), len(full))
    }
}
```

### 10.4 Verify

```bash
go test ./internal/workflow/ -run TestGenerateRetryGuide -v
go test ./internal/workflow/... -count=1
```

**Expected**: retry guide test GREEN; nothing else affected.

**Commit**: `feat(recipe): iteration-aware generate retry delta`

**Rollback**: revert removes the `generateRetryGuide` arm; retries read the full composition again.

---

## Phase 11 — per-shape cap tightening + full acceptance sweep

**Goal**: set the final per-shape caps based on measured post-P10 numbers and run the full acceptance sweep.

**Files touched**: 1 — [recipe_guidance_test.go](../internal/workflow/recipe_guidance_test.go)

### 11.1 Measure

```bash
go test -tags audit ./internal/workflow/ -run TestAuditComposition -v 2>&1 > /tmp/audit_p11.txt
grep -E "^===|^########## " /tmp/audit_p11.txt
```

Expected numbers per shape (from the top-of-document table). Actual numbers go into the cap map.

### 11.2 Set caps with +1.5 KB headroom per step

```go
var showcaseStepCaps = map[RecipeShape]map[string]int{
    ShapeHelloWorld: {
        RecipeStepResearch:  6 * 1024,
        RecipeStepProvision: 8 * 1024,
        RecipeStepGenerate:  10 * 1024,
        RecipeStepDeploy:    12 * 1024,
        RecipeStepFinalize:  9 * 1024,
        RecipeStepClose:     7 * 1024,
    },
    // ... (see top-of-document for all four shapes)
}
```

### 11.3 Per-step-per-shape assertion

```go
func TestRecipe_DetailedGuide_UnderCaps(t *testing.T) {
    // Same as existing cap test, but asserts each shape's step against
    // its specific cap, and also asserts that NARROWER shapes have
    // STRICTLY smaller guides than wider shapes — regression guard
    // against a predicate accidentally defaulting to true.
    //
    // Invariant: hello-world <= backend-minimal <= full-stack-showcase
    //            <= dual-runtime-showcase for every step.
}
```

This **monotonicity invariant** catches future predicate bugs: if someone adds a block with a broken predicate that fires on hello-world, this test flags it.

### 11.4 LOG2-bug grep acceptance

Per the original plan's acceptance list:

```bash
grep -c "safe.directory" internal/content/workflows/recipe.md          # ≥2
grep -c "catalog the output\|Catalog the output" internal/content/workflows/recipe.md  # ≥1
grep -c "each codebase" internal/content/workflows/recipe.md          # ≥1
grep -c "run.envVariables" internal/content/workflows/recipe.md       # ≥2
grep -c "burn-on-failure" internal/content/workflows/recipe.md        # ≥1
grep -c "Parameter naming" internal/content/workflows/recipe.md       # ≥1
grep -c "already running" internal/content/workflows/recipe.md        # ≥1
```

Every defense still present. Moving content into `<block>` tags does not affect grep hits.

### 11.5 Manual read-through — the only step that matters more than the numbers

For each fixture shape, paste the full composed guide for each step into a scratch buffer and read it end-to-end. Specific checks per step:

**Generate (hello-world)**:
- [ ] Does the guide reference the chain recipe at all? It shouldn't — hello-world has no predecessor in the chain.
- [ ] Is there any mention of "dashboard" or "sub-agent"? Shouldn't be (showcase-only).
- [ ] Is the schema pointer present?

**Generate (backend-minimal)**:
- [ ] Is the chain recipe's `## Gotchas` present (from the hello-world ancestor — may be empty if no Gotchas)?
- [ ] Is any dual-runtime content present? Shouldn't be.
- [ ] Is any bundler dev-server content present? Laravel is not bundler — shouldn't be.

**Generate (dual-runtime showcase)**:
- [ ] Dual-runtime URL shapes present.
- [ ] Dev-server host-check present.
- [ ] Chain recipe `## zerops.yaml template` present.
- [ ] Per-codebase README rule present.
- [ ] Sub-agent brief appears only at deploy, not generate.

**Deploy (hello-world)**:
- [ ] Steps 1-3 + Step 5 present.
- [ ] Step 4b sub-agent brief absent (showcase-only).
- [ ] Browser walk absent.

**Deploy (dual-runtime showcase)**:
- [ ] Full flow present.
- [ ] `where-commands-run` present (referenced from close).

**Close (any shape)**:
- [ ] Sub-agent prompt template's "CRITICAL — where commands run" is the pointer form, not the full block.
- [ ] Browser walk points at deploy's `browser-walk-*` blocks.

### 11.6 Verify

```bash
go test ./internal/workflow/ -run TestRecipe_DetailedGuide_UnderCaps -v
go test ./... -count=1
make lint-local
```

**Expected**: 0 failures, 0 lint issues.

**Commit**: `test(recipe): tighten per-shape caps + monotonicity invariant`

**Rollback**: revert caps back to loose values; everything else stays.

---

## Phase 12 — archive

Once `v8.55.0` (or whichever release ships this refactor) is merged:

```
mkdir -p /Users/fxck/www/zcp/docs/archive
git mv docs/implementation-recipe-workflow-reshuffle.md docs/archive/
git mv docs/research-recipe-workflow-reshuffle.md docs/archive/
git mv docs/implementation-recipe-size-reduction.md docs/archive/
git mv docs/postmortem-v8.52.0-slimdown.md docs/archive/
```

Add a one-line note in [docs/archive/README.md](../docs/archive/README.md) (create if needed):
```
- recipe workflow: reshuffle (v8.54.0) + delivery refactor (v8.55.0) — commits XXXX..YYYY
```

---

## Non-goals

- **Templating engine.** `<block>` + predicate catalog is ~200 lines of Go including tests. A templating engine is a dependency and a foot-gun.
- **Deferred-load of the chain recipe via `zerops_knowledge`.** Phase 4 already cuts the chain to ~2.5 KB by extraction; deferred-load would save the remaining ~2.5 KB but removes the key framework pattern the agent writes against. Not worth it.
- **Moving content between lifecycle steps.** The v8.54.0 reshuffle placed every piece of content in the correct step; this plan does not touch that.
- **Mode-aware recipe guidance** (dev/standard/simple mode variants like bootstrap has). Predicates unlock this but wiring it requires a separate design pass for how recipe modes differ from bootstrap modes.
- **New conventions** (writing-style, comment-style) — Phase 4 of the v8.54.0 reshuffle moved voice content to finalize; leave it there.
- **Fragment-level splitting of recipe.md** (e.g., one file per section). The 116 KB single file is fine with caching (Phase 1); splitting adds filesystem complexity without benefit.
- **Adding new LOG2 bug coverage.** If you discover a missing defense while cutting, open a separate issue — do not mix it in.

---

## Rollback matrix

| Phase | Files reverted | Effect | Safe? |
|---|---|---|---|
| 0 | test file + fixture additions | cap test falls back to single-fixture sweep | ✓ |
| 1 | content.go | per-call 116 KB read returns | ✓ (correctness equivalent) |
| 2 | documents.go + guidance.go | no callers → dead code removed | ✓ |
| 3 | 5 new files | all infrastructure removed; no callers | ✓ |
| 4 | recipe_knowledge_chain.go | old stop-at-first-numbered-section returns | ✓ |
| 5a | recipe.md + catalog | `<block>` tags removed; resolution falls back to whole section | ✓ |
| 5b | catalog predicates | all predicates back to `nil`; every block always emitted | ✓ |
| 6a/b | same pattern | same | ✓ |
| 7a/b | same pattern | same | ✓ |
| 8 | recipe_guidance.go + recipe.md + bootstrap_guidance_test.go | full schema H2 injection returns | ✓ |
| 9 | recipe.md | two duplications return | ✓ |
| 10 | recipe_guidance.go | retry delta removed; full composition returns on retry | ✓ |
| 11 | recipe_guidance_test.go | caps loosen | ✓ |

**No phase in this plan deletes unique content.** Every cut either (a) gates content behind a predicate (content still present, just not always emitted), (b) delegates content to on-demand `zerops_knowledge` retrieval (reachable via agent call), (c) replaces a duplicate with a pointer to the authoritative copy, or (d) extracts a subset of chain recipe content (the full recipe is still in the knowledge store). Rollback at any phase returns the previous phase's behavior exactly.

---

## Acceptance criteria

The change is done when **all** of the following are true:

1. `go test ./... -count=1` passes.
2. `make lint-local` reports 0 issues.
3. `TestRecipe_DetailedGuide_UnderCaps` passes with the per-shape caps set in Phase 11.
4. `TestCatalog_CoversAllMarkdownBlocks` passes (every `<block>` tag in recipe.md has a catalog entry).
5. Monotonicity invariant test passes: for every step, `len(hello-world) ≤ len(backend-minimal) ≤ len(full-stack-showcase) ≤ len(dual-runtime-showcase)`.
6. `TestRecipeKnowledgeChain_NestjsShowcase` proves the chain extraction drop: chain size ≤ 3.5 KB for a dual-runtime showcase.
7. `TestExtractForAncestor_NoGotchas_ReturnsEmpty` proves the "no fake gotchas" regression guard.
8. `TestGenerateRetryGuide_IsShort` proves the retry delta cap.
9. Audit sweep across all four shapes shows every step at or below its target from the Phase 11 cap map.
10. Manual read-through (Phase 11.5) reports no orphaned references, no new duplication, no logical gaps, no shape-wrong content emitted (e.g., bundler rules on Laravel, dashboard content on hello-world).
11. LOG2 bug defenses preserved — all seven greps in Phase 11.4 return the required counts.
12. `TestGetWorkflow_CachedAcrossCalls` proves recipe.md is read exactly once per process lifetime.
13. Recipe content rendering test from [engine_recipe.go](../internal/workflow/engine_recipe.go) passes (existing test, regression guard for BuildResponse shape).

A partial land — for example, Phases 1-7 landing without Phase 8-10 — is acceptable and each commit leaves the tree green. **Phase 11 must not land until every phase that feeds a per-step cap has landed.** Landing Phase 11 early will fail caps because P8-P10 cuts aren't in yet.

---

## What this plan replaces in the previous version

| Previous plan phase | Fate in this plan |
|---|---|
| P1 — trim dual-runtime URL H4 verbatim | **Superseded by P5**. Dual-runtime blocks gate on `isDualRuntime`; narrow recipes pay 0 B. Dual-runtime recipes keep the full block. |
| P2 — delete "What to generate" / "Two kinds of import.yaml" | **Superseded by P5**. Both become predicate-gated blocks. |
| P3 — env var catalog pointer | **Absorbed into P8**. |
| P4 — compress generate-fragments | **Dropped**. No cap pressure after predicate gating. |
| P5 — compress small generate sections | **Dropped**. Same reason. |
| P6 — focused provision schema summary | **Superseded by P8**, with correction: drop `envSecrets`/`dotEnvSecrets`/preprocessor from provision entirely. |
| P7 — trim provision prose | **Dropped**. |
| P8 — merge deploy failure tables | **Absorbed into P7** (`reading-deploy-failures` block). |
| P9 — compress deploy Step 4b / browser walk | **Superseded by P7**. Browser walk blocks gate on `isShowcase`; hello-world sees zero bytes of them. |
| P10 — close "where commands run" pointer | **Absorbed into P9**. |
| P11 — tighten caps | **Kept as P11**, now per-shape with monotonicity invariant. |
| P12 — archive | **Kept as P12**. |

---

## What to verify before executing

Three small checks. Do not skip them.

1. **Block seam feasibility for deploy.** The `### Dev deployment flow` H3 has 18 KB of preamble before its first H4. Phase 7.1 inserts new H4 seams as a pure editorial move. Open the section, confirm Steps 1-5 + sub-agent brief + UX contract + where-commands-run + browser walk map cleanly to the 7 new H4s in the design. If any of them spans a seam awkwardly, refine the block boundaries before wrapping.

2. **`hasBundlerDevServer` framework list.** The predicate's `bundlerFrameworks` list covers: react, vue, svelte, sveltekit, nuxt, next, astro, qwik, angular, remix, solid, solidstart, analog, react-router. Confirm against [internal/knowledge/recipes/](../internal/knowledge/recipes/) — every recipe whose framework uses a Host-checking dev server must be in the list. Missing a framework means the dev-server host-check block doesn't fire and the agent hits LOG2 bug 15.

3. **`## Gotchas` H2 coverage across existing recipes.** Grep `## Gotchas` across `internal/knowledge/recipes/*.md`. Recipes without a `## Gotchas` H2 will have their ancestor injection return empty under Phase 4. That's correct (empty is better than filler) but worth knowing which recipes will have silent-skip injections.

```bash
for f in internal/knowledge/recipes/*.md; do
    if grep -q '^## Gotchas' "$f"; then
        echo "HAS:     $(basename "$f")"
    else
        echo "MISSING: $(basename "$f")"
    fi
done
```

If >80% of minimal/showcase recipes have Gotchas, Phase 4 is sound. If many are missing, consider a separate content pass to add Gotchas H2s to recipes where applicable.

---

## Appendix — final `detailedGuide` composition per shape

A reference for reviewers: what the agent actually sees after every phase lands.

### Hello-world (nodejs-hello-world) — generate step ≈ 8 KB

```
## Generate — App Code & Configuration
  [preamble]
<block execution-order>           1.9 KB    (always)
<block container-state>           1.3 KB    (always)
<block where-to-write-files-single> 0.8 KB  (!hasMultipleCodebases)
<block two-kinds-of-import-yaml>  0.3 KB    (always)
<block zerops-yaml-header>        1.9 KB    (always)
<block env-example-preservation>  0.5 KB    (always)
<block asset-pipeline-consistency> 0.6 KB   (always)
<block readme-with-fragments>     1.1 KB    (always)
<block gotchas-header>            0.6 KB    (always)
<block code-quality>              0.4 KB    (always)
<block pre-deploy-checklist>      1.4 KB    (always)
─────────
  static section total:          10.8 KB
+ chain recipe:                    0.0 KB   (no predecessor)
+ schema pointer:                  0.4 KB
+ env var catalog pointer:         0.3 KB
─────────
  TOTAL:                          ~11.5 KB  (well under 10 KB cap... hm.)
```

Wait — at 11.5 KB the hello-world overshoots the 10 KB cap. Either tighten the static-section blocks, or relax the hello-world generate cap to 12 KB. **Decision to make during Phase 11**: measure first, then choose. The table at the top of this doc is an estimate; the real per-shape caps come from measurement.

### Dual-runtime showcase (nestjs-showcase) — generate step ≈ 21 KB

```
## Generate — App Code & Configuration
  [preamble]
<block execution-order>           1.9 KB    (always)
<block container-state>           1.3 KB    (always)
<block where-to-write-files-multi> 2.0 KB   (hasMultipleCodebases)
<block what-to-generate-showcase> 1.2 KB    (isShowcase)
<block two-kinds-of-import-yaml>  0.3 KB    (always)
<block zerops-yaml-header>        1.9 KB    (always)
<block dual-runtime-url-shapes>   3.5 KB    (isDualRuntime)
<block dual-runtime-consumption>  1.0 KB    (isDualRuntime)
<block serve-only-dev-override>   0.8 KB    (hasServeOnlyProd: true — static frontend)
<block dev-server-host-check>     1.2 KB    (hasBundlerDevServer: true — nestjs via vite)
<block dev-dep-preinstall>        0.7 KB    (hasMultiBaseBuildCommand: depends)
<block worker-setup-block>        0.0 KB    (hasSharedCodebaseWorker: false — separate codebase)
<block project-env-vars-pointer>  0.1 KB    (always)
<block env-example-preservation>  0.5 KB    (always)
<block dashboard-skeleton>        2.2 KB    (isShowcase)
<block asset-pipeline-consistency> 0.6 KB   (always)
<block readme-with-fragments>     1.1 KB    (always)
<block gotchas-header>            0.6 KB    (always)
<block code-quality>              0.4 KB    (always)
<block pre-deploy-checklist>      1.4 KB    (always)
─────────
  static section total:          22.7 KB
+ chain recipe (post-P4):          2.5 KB   (nestjs-minimal Gotchas + YAML template)
+ schema pointer:                  0.4 KB
+ env var catalog pointer:         0.3 KB
─────────
  TOTAL:                         ~25.9 KB   (cap: 24 KB — need to tighten `zerops-yaml-header` or `pre-deploy-checklist`, or raise cap)
```

**Observation**: even with surgical composition, the dual-runtime showcase is at ~26 KB against a 24 KB cap. Two responses:

- **Accept higher cap.** 26 KB is still readable in one agent tool response; the v7 post-mortem's threshold was ~30 KB for chunked reads. Set the dual-runtime showcase generate cap to 28 KB and ship.
- **Tighten `zerops-yaml-header` preamble.** 1.9 KB of prose before the first dual-runtime block is the single biggest always-on block; trimming to 1 KB saves ~1 KB without losing rules (much of it is motivational narrative).

**Phase 11 decides based on measured numbers**, not pre-computed estimates. The principle: don't pre-optimize caps against estimates that might move ±2 KB once real numbers land.

---

## Notes for the implementor on pacing

- **Phases 0-3 are infrastructure.** No agent-visible behavior change. Land them in one session if possible.
- **Phase 4 is a content delivery change.** Verify against audit sweep, commit, move on.
- **Phases 5-7 are the big content shape changes.** Do them one at a time. Between each, run the audit sweep and read the generated guide for one fixture end-to-end. Do not batch the three section conversions into one commit — each one deserves its own audit pass.
- **Phase 8-10 are focused refinements.** Short commits, each one clearly tied to its mechanism.
- **Phase 11 is measurement-driven.** Don't pre-set caps before running the audit. Set them to 1.5 KB above measured peak per shape per step.
- **Phase 12 is a release-time action.** Don't archive until the release that ships this refactor is tagged.

If a phase's audit numbers come out significantly different from this plan's estimates (±15%), **stop and investigate** before moving to the next phase. Either the estimate is wrong (update the plan comment), the block wrap went wrong (recheck the markdown), or a predicate is misfiring (add a targeted test). Do not proceed with a mismatched baseline.

---

**This plan is complete. Execute in order. Do not skip the verify steps. Commit per phase. Read the guide end-to-end at Phase 11.5 — that is the one acceptance criterion that matters more than the byte counts.**

---

## Implementor readiness note

This plan was written against the tree state at the top commit of `main` as of the date the plan was committed. Before starting, verify:

1. **`main` is at or past** the commit that contains this plan (`git log docs/implementation-recipe-size-reduction.md -n 1`).
2. **Ground-truth table symbols match** — re-run the greps from the "Ground truth reference" table near the top of this plan. If any symbol has moved/renamed/changed signature, fix the plan's references **before** starting Phase 0.
3. **The existing `TestRecipe_DetailedGuide_ShowcaseEveryStepUnderCap` test is GREEN** on your tree. Phase 0 converts this test; if it's already broken, fix it first (not as part of Phase 0).
4. **Phase numbers commit message convention** — every commit message in this plan starts with a type (`refactor(recipe)`, `feat(recipe)`, `test(recipe)`, `perf(content)`) matching the tree's existing convention. Do not change it.

**Verified correct before handoff** (things that were wrong in draft and have been fixed):

- Added `RecipeTierHelloWorld` constant introduction in Phase 0.2 (previously assumed to exist; it does not).
- Fixed `Document` struct field reference in Phase 2.3 (was `Body`, correct field is `Content`).
- Clarified Phase 1 framing (`embed.FS` allocation, not disk read).
- Clarified Phase 10 relationship to existing `buildRecipeIterationDelta` — adds a new generate-specific delta, does not reuse the generic bootstrap escalation.
- Renamed `cap` loop variable to `capVal` in Phase 0.5 cap sweep test to avoid shadowing the Go builtin.
- Added an explicit cap-map type transition in Phase 0.4 (flat `map[string]int` → nested `map[RecipeShape]map[string]int`) so the type change is not ambiguous between Phase 0 and Phase 11.
- Added ground-truth symbol reference table at the top of this plan as an authoritative source for any snippet conflict.

**Known estimates that the plan explicitly leaves to measurement**:

- Per-shape cap values in the top-of-document table and in Appendix compositions are **estimates based on block-size arithmetic**, not measured numbers. Phase 11 is the measurement phase. Do not tighten caps before then.
- Hello-world generate target of 10 KB and dual-runtime-showcase generate target of 24 KB are both **possibly 1-2 KB optimistic**. Phase 11 decides whether to trim always-on blocks or raise caps.
- "Generate retry delta < 5 KB" cap in Phase 10.3 is an estimate; the function as written is ~1.2 KB for hello-world and ~2 KB for dual-runtime showcase. 5 KB is generous.

**The plan is ready for an opus-level implementor.** Remaining unknowns are explicitly called out and are measurement-driven decisions that the implementor makes during Phase 11, not architectural gaps that need fresh thought.
