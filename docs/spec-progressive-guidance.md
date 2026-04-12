# Spec: Progressive Guidance Delivery

**Status**: Implementation-ready
**Author**: Opus-level analysis session, 2026-04-12
**Scope**: Phases A, B, C — transforms recipe guidance from push-all-upfront to pull-on-demand + sub-step orchestration + adaptive delivery

---

## Problem Statement

The recipe workflow delivers guidance as a single monolithic string per step via `DetailedGuide` in the `RecipeResponse`. The dual-runtime showcase generate step is 39.7 KB; deploy is 40 KB. Finalize + close deliver 29.9 KB with zero shape differentiation.

The agent sees the full guide at step start and must hold it in attention while writing code. This causes:

1. **Attention dilution** — 40 KB of guidance competes with the code the agent is actively writing. Critical rules (execOnce trap, port collision, URL pattern) get buried.
2. **Dead weight** — A hello-world agent reads 1.5 KB of execOnce trap documentation at deploy start, but only 10% of runs encounter it. A hello-world gets 29.9 KB of finalize+close that includes showcase service-key enumerations and dual-runtime export examples.
3. **Duplication** — "Where commands run" appears 3 times (deploy subagent brief, close subagent prompt, generate execution order). Browser walk rules are taught at deploy Step 4c and re-taught at close 1b.
4. **No feedback loop** — The guide is static. If the agent wrote zerops.yaml correctly, it still gets the 2.9 KB dual-runtime-consumption block on retry. If it failed on comment ratio, the retry delta doesn't know that.

## Solution Architecture

Three phases, each independently valuable, each building on the previous:

```
Phase A: Pull-based guidance via zerops_guidance tool
Phase B: Sub-step orchestration with validation gates
Phase C: Adaptive guidance based on agent state
```

### Current flow (push-all-upfront)

```
Engine                          Agent
  │                               │
  ├── RecipeComplete(research) ──>│
  │<── plan ──────────────────────│
  │                               │
  ├── BuildResponse(generate) ──->│  ← 40 KB DetailedGuide
  │                               │  (agent must parse everything)
  │                               │  ... writes code for 20 minutes ...
  │<── RecipeComplete(generate) ──│
```

### Target flow (Phase A + B + C)

```
Engine                            Agent                    zerops_guidance
  │                                 │                           │
  ├── BuildResponse(generate) ───>  │  ← 3 KB skeleton          │
  │                                 │                           │
  │                                 ├── zerops_guidance ──────> │
  │                                 │   topic="zerops-yaml"     │
  │                                 │<── 3 KB rules ─────────── │
  │                                 │   (writes zerops.yaml)    │
  │                                 │                           │
  ├── RecipeComplete(generate,      │                           │
  │     substep="zerops-yaml") ──>  │                           │
  │   [validates: setup count,      │                           │
  │    comment ratio, field names]  │                           │
  │                                 │                           │
  ├── BuildResponse (next substep)─>│  ← 2 KB app-code rules   │
  │                                 │   (writes app code)       │
  │                                 │                           │
  │                                 ├── zerops_guidance ──────> │
  │                                 │   topic="smoke-test"      │
  │                                 │<── 1 KB instructions ──── │
  │                                 │   (runs smoke test)       │
  │                                 │                           │
  ├── RecipeComplete(generate) ───> │                           │
```

---

## Phase A: Pull-Based Guidance via `zerops_guidance`

### Goal

Replace the 40 KB monolithic `DetailedGuide` with a ~3-5 KB **execution skeleton** + an on-demand `zerops_guidance` MCP tool that returns individual blocks filtered through plan predicates.

### What changes

1. **New MCP tool**: `zerops_guidance` — returns a single guidance block by topic, filtered through the active recipe plan's predicates.
2. **Recipe.md restructured**: blocks get stable topic IDs (they already have `<block name="...">` tags — these become the topic IDs).
3. **`resolveRecipeGuidance` refactored**: for generate/deploy/finalize/close, returns a compact skeleton that references topics instead of inlining block content.
4. **Existing predicate system unchanged** — the same predicates gate the same content, just delivered on-demand instead of upfront.

### New tool: `zerops_guidance`

**File**: `internal/tools/guidance.go` (new file)

```go
// RegisterGuidance registers the zerops_guidance MCP tool.
// It provides on-demand access to recipe workflow guidance blocks,
// filtered through the active plan's predicates.
func RegisterGuidance(srv *mcp.Server, engine *workflow.Engine)
```

**Input schema**:
```json
{
  "topic": "string — guidance topic ID (e.g. 'zerops-yaml-rules', 'smoke-test', 'subagent-brief')",
  "step":  "string — optional, defaults to current recipe step"
}
```

**Behavior**:
1. Load active recipe session from engine (`engine.RecipeSession()`)
2. If no active session → error: "No active recipe session"
3. Look up topic in the **topic registry** (see below)
4. If topic has a predicate, evaluate it against the plan — if false, return: "This topic does not apply to your recipe shape ({shape reason})"
5. Extract the block content from recipe.md (or from a dedicated guidance store)
6. Return the block content as text

**Annotations**:
```go
Annotations: &mcp.ToolAnnotations{
    Title:        "Recipe Guidance",
    ReadOnlyHint: true,
}
```

### Topic registry

**File**: `internal/workflow/recipe_topic_registry.go` (new file)

The topic registry maps topic IDs to their source block and predicate. It is derived from the existing `recipe_section_catalog.go` catalogs but adds:
- A human-readable description (for the skeleton's inline references)
- The step scope (which step this topic belongs to)
- An optional compound key for topics that span multiple blocks

```go
type guidanceTopic struct {
    ID          string                  // stable topic ID, matches <block name="...">
    Step        string                  // which recipe step this belongs to
    Description string                  // one-line summary for skeleton references
    Predicate   func(*RecipePlan) bool  // nil = always available
    // BlockNames lists the <block> tags to extract and concatenate.
    // Most topics map 1:1 to a block; compound topics merge related blocks.
    BlockNames  []string
}
```

**Topic mapping — generate step** (derived from `recipeGenerateBlocks`):

| Topic ID | Block(s) | Predicate | Description |
|----------|----------|-----------|-------------|
| `container-state` | container-state | nil | What's available vs unavailable during generate |
| `where-to-write` | where-to-write-files-single OR where-to-write-files-multi | auto (hasMultipleCodebases) | File placement rules for your recipe shape |
| `recipe-types` | what-to-generate-showcase | isShowcase | What to generate per recipe type (showcase) |
| `import-yaml-kinds` | two-kinds-of-import-yaml | nil | Workspace vs recipe import.yaml distinction |
| `execution-order` | execution-order | nil | Mandatory write sequence |
| `zerops-yaml-rules` | zerops-yaml-header, setup-dev-rules, setup-prod-rules, shared-across-setups, generate-schema-pointer | nil (compound) | Complete zerops.yaml writing rules |
| `dual-runtime-urls` | dual-runtime-url-shapes, dual-runtime-consumption, project-env-vars-pointer, dual-runtime-what-not-to-do | isDualRuntime (compound) | Dual-runtime URL pattern and consumption |
| `serve-only-dev` | serve-only-dev-override | hasServeOnlyProd | Dev-base override for serve-only prod targets |
| `multi-base-dev` | dev-dep-preinstall | needsMultiBaseGuidance | Secondary runtime dependency preinstall |
| `dev-server-hostcheck` | dev-server-host-check | hasBundlerDevServer | Dev-server host-check allow-list |
| `worker-setup` | worker-setup-block | hasWorker | Worker setup shape (shared vs separate) |
| `dashboard-skeleton` | dashboard-skeleton | isShowcase | What to write in the skeleton vs what the subagent writes |
| `env-conventions` | env-example-preservation, framework-env-conventions | nil | .env.example and framework env var naming |
| `asset-pipeline` | asset-pipeline-consistency | nil | Build pipeline ↔ view consistency |
| `readme-fragments` | readme-with-fragments | nil | README structure with extract fragments |
| `code-quality` | code-quality, pre-deploy-checklist | nil | Comment ratio, pre-deploy verification |
| `smoke-test` | on-container-smoke-test | nil | On-container validation before deploy |
| `fragment-quality` | (generate-fragments section) | planNeedsFragmentsDeepDive | Fragment writing-style deep-dive |

**Topic mapping — deploy step**:

| Topic ID | Block(s) | Predicate | Description |
|----------|----------|-----------|-------------|
| `deploy-flow` | deploy-framing, dev-deploy-flow-core | nil | Core deploy execution flow |
| `subagent-brief` | dev-deploy-subagent-brief | isShowcase | Feature sub-agent dispatch and brief |
| `browser-walk` | dev-deploy-browser-walk | isShowcase | Browser verification flow |
| `stage-deploy` | stage-deployment-flow | nil | Stage cross-deploy flow |
| `deploy-failures` | reading-deploy-failures, common-deployment-issues | nil | Failure diagnosis reference |

**Topic mapping — finalize step** (blocks to be extracted — currently finalize has NO block catalog):

| Topic ID | Block(s) | Predicate | Description |
|----------|----------|-----------|-------------|
| `env-comments` | (finalize preamble + Step 1 core) | nil | Comment writing instructions |
| `project-env-vars` | (Step 1b) | isDualRuntime | projectEnvVariables for dual-runtime |
| `showcase-service-keys` | (within Step 1) | isShowcase | Service key lists by worker shape |
| `comment-style` | (Comment style section) | nil | Writing style reference |

**Topic mapping — close step** (blocks to be extracted — currently close has NO block catalog):

| Topic ID | Block(s) | Predicate | Description |
|----------|----------|-----------|-------------|
| `code-review-agent` | (1a) | nil | Static code review sub-agent brief |
| `close-browser-walk` | (1b) | isShowcase | Post-review browser verification |
| `export-publish` | (2) | nil | Export and publish pipeline |

### Skeleton format

The skeleton replaces the current monolithic guide. It is a **compact, imperative document** that:
- Lists the execution steps in order
- States the critical constraints (2-3 lines each)
- References topics by ID with a one-line description

**Example skeleton for generate step (dual-runtime showcase)**:

```markdown
## Generate — App Code & Configuration

### Constraints
- Dev containers are RUNNING but env vars NOT active until deploy
- Each codebase is independent — never cross-scaffold between mounts
- Comment ratio >= 30% in zerops.yaml (aim 35%)

### Execution order
1. Scaffold each codebase on its mount [topic: where-to-write]
2. Write zerops.yaml — YOU, not a sub-agent [topic: zerops-yaml-rules]
   - Dual-runtime URL pattern applies [topic: dual-runtime-urls]
3. Write app code — skeleton only for showcase [topic: dashboard-skeleton]
4. Write README per codebase with 3 fragments [topic: readme-fragments]
5. On-container smoke test [topic: smoke-test]
6. Git init + commit

### Fetch guidance
Call `zerops_guidance topic="{id}"` before each sub-task for detailed rules.
All topics are filtered to your recipe shape — irrelevant content is excluded.
```

**How to implement**: Modify `resolveRecipeGuidance()` for the generate, deploy, finalize, and close cases. Instead of calling `composeSection(body, catalog, plan)` which assembles all blocks into one string, build the skeleton string directly.

The skeleton is **also stored in recipe.md** — as a new `<section name="generate-skeleton">` (and deploy-skeleton, finalize-skeleton, close-skeleton). This keeps the content in the same file as the blocks, editable by the same author, testable by the same harness.

### Implementation details

**`resolveRecipeGuidance` change** (in `recipe_guidance.go`):

```go
case RecipeStepGenerate:
    // Phase A: return skeleton instead of composed blocks.
    // The skeleton references topics; the agent fetches them via zerops_guidance.
    if body := ExtractSection(md, "generate-skeleton"); body != "" {
        return composeSkeleton(body, recipeGenerateTopics, plan)
    }
    // Fallback: compose blocks as before (safety net during migration).
    // Remove this fallback after skeleton is proven stable.
    ...
```

`composeSkeleton` is a new function that:
1. Reads the skeleton template from recipe.md
2. Processes `[topic: {id}]` markers — for each marker, checks the topic's predicate against the plan
3. If predicate is false, removes the line containing the marker (and its parent bullet if the whole bullet is just a topic reference)
4. Returns the filtered skeleton

This preserves the existing predicate system — the same predicates gate the same content, just at the reference level instead of the block level.

**Block extraction for `zerops_guidance`** (in `recipe_topic_registry.go`):

```go
// ResolveTopic returns the guidance content for a topic, filtered by plan.
func ResolveTopic(topicID, step string, plan *RecipePlan) (string, error) {
    topic, ok := topicRegistry[topicID]
    if !ok {
        return "", fmt.Errorf("unknown guidance topic %q", topicID)
    }
    if topic.Step != "" && topic.Step != step {
        return "", fmt.Errorf("topic %q belongs to step %q, not %q", topicID, topic.Step, step)
    }
    if topic.Predicate != nil && !topic.Predicate(plan) {
        return "", nil // topic doesn't apply to this plan shape
    }

    md, err := content.GetWorkflow("recipe")
    if err != nil {
        return "", err
    }
    sectionName := stepToSectionName(topic.Step) // "generate" → "generate"
    body := ExtractSection(md, sectionName)

    var parts []string
    for _, blockName := range topic.BlockNames {
        if block := extractBlock(body, blockName); block != "" {
            parts = append(parts, block)
        }
    }
    return strings.Join(parts, "\n\n"), nil
}
```

`extractBlock` already exists implicitly inside `composeSection` — factor it out.

### Backward compatibility

- The `zerops_guidance` tool is additive — it doesn't break any existing flow
- The skeleton still works if the agent ignores `zerops_guidance` calls (it just has less detail)
- The `[topic: id]` markers in the skeleton are plain text — if the agent doesn't know about the tool, it reads them as documentation hints
- Retry deltas (`buildDeployRetryDelta`, `buildGenerateRetryDelta`) are unchanged — they already surface focused reminders, not full blocks
- Knowledge injection (`assembleRecipeKnowledge`) is unchanged — it appends after the skeleton just as it appends after the composed guide

### What to test

1. **Skeleton coverage test**: Every topic referenced in a skeleton must exist in the topic registry. Every topic in the registry must have at least one skeleton reference.
2. **Topic predicate parity**: The predicate on each topic must match the predicate on its source block(s) in the catalog. Table-driven test comparing `topicRegistry[id].Predicate` vs `recipeGenerateBlocks[name].Predicate` for every mapping.
3. **Size regression test**: Skeleton + knowledge injection must be under 8 KB for every shape (down from 20-46 KB). Individual topic responses must be under 5 KB each.
4. **Block extraction test**: `ResolveTopic` returns the same content as `composeSection` for single-block topics.
5. **Predicate filtering test**: `ResolveTopic` returns empty string when predicate is false.
6. **Cap sweep update**: Replace the per-step byte caps with skeleton caps (much smaller) + topic caps (per-topic).

### Finalize + close block catalogs

Phase A also wires `recipeFinalizeBlocks` and `recipeCloseBlocks` — currently both are `nil` (empty slices), so `composeSection` returns the raw markdown unchanged.

**Finalize blocks** to extract (in `recipe.md`'s finalize section):

```
<block name="finalize-preamble">...</block>        — always-on
<block name="env-comment-rules">...</block>         — always-on
<block name="showcase-service-keys">...</block>     — isShowcase
<block name="project-env-vars">...</block>          — isDualRuntime
<block name="review-readmes">...</block>            — always-on
<block name="comment-style">...</block>             — always-on
<block name="finalize-completion">...</block>       — always-on
```

**Close blocks** to extract:

```
<block name="close-preamble">...</block>            — always-on
<block name="code-review-subagent">...</block>      — always-on
<block name="close-browser-walk">...</block>        — isShowcase
<block name="export-publish">...</block>            — always-on (prose self-gates on user request)
<block name="close-completion">...</block>          — always-on
```

**Within `export-publish`**, the multi-codebase content is self-gating (the agent reads its plan shape from the codebase-count table). No further block split needed — the table is ~0.5 KB and is useful as a reference even for single-codebase plans.

### Deploy `dev-deploy-flow-core` split

The 13 KB monolith is split into sub-blocks:

```
<block name="deploy-core-universal">...</block>     — always-on (~7 KB)
<block name="deploy-api-first">...</block>          — isDualRuntime (~2.5 KB)
<block name="deploy-asset-dev-server">...</block>   — hasBundlerDevServer (~0.8 KB)
<block name="deploy-worker-process">...</block>     — hasWorker (~1.2 KB)
<block name="deploy-target-verification">...</block>— always-on (~1.5 KB)
```

The `deploy-flow` topic in the registry maps to: deploy-core-universal + (conditionally) deploy-api-first, deploy-asset-dev-server, deploy-worker-process, deploy-target-verification.

### Content deduplication

Three duplicated concepts become **single-source blocks** referenced from multiple topics:

1. **"Where commands run"** — lives in deploy as `<block name="where-commands-run">`. Topic `where-commands-run`. Referenced from both `subagent-brief` topic and `code-review-agent` topic via: "For the command execution model, see `zerops_guidance topic='where-commands-run'`."

2. **Browser walk rules** — lives in deploy as the existing `dev-deploy-browser-walk` block. Topic `browser-walk`. Close 1b becomes: "Run the same 3-phase browser walk from deploy. Fetch rules: `zerops_guidance topic='browser-walk'`. Close-specific additions: (a) don't delegate to subagent, (b) don't complete until both walks clean, (c) counts toward 3-iteration limit."

3. **Comment style** — lives in finalize as `<block name="comment-style">`. Topic `comment-style`. Generate's `code-quality` block references it: "For the full writing-style voice, see `zerops_guidance topic='comment-style'` at finalize."

### Recipe.md changes summary

1. Add `<section name="generate-skeleton">`, `<section name="deploy-skeleton">`, `<section name="finalize-skeleton">`, `<section name="close-skeleton">`
2. Wrap finalize content in `<block>` tags (7 blocks)
3. Wrap close content in `<block>` tags (5 blocks)
4. Split `dev-deploy-flow-core` into 5 sub-blocks
5. Extract "Where commands run" into its own block (remove duplicates from subagent brief and close)
6. No content is deleted — all blocks remain accessible via `zerops_guidance`

---

## Phase B: Sub-Step Orchestration

### Goal

The engine validates the agent's work at sub-step boundaries within generate and deploy, providing focused feedback instead of waiting for full step completion.

### What changes

1. **RecipeStep gains sub-steps**: `SubSteps []RecipeSubStep` field tracks progress within a step
2. **RecipeComplete accepts optional `substep` parameter**: completes a sub-step instead of the full step
3. **Sub-step validation hooks**: each sub-step has a validator that checks the agent's output
4. **Next-substep guidance**: on sub-step completion, the response includes the next sub-step's skeleton (not the full step skeleton)

### Sub-step definitions

**Generate sub-steps** (in order):

| SubStep | Validates | Next guidance |
|---------|-----------|---------------|
| `scaffold` | Mount directories exist, framework project initialized | "Write zerops.yaml" |
| `zerops-yaml` | Setup count correct, comment ratio >= 30%, field names valid against schema, dev/prod mode flags differ | "Write app code" |
| `app-code` | Key files exist (controllers, views, routes), no TODO/PLACEHOLDER strings | "Write README" |
| `readme` | All 3 fragments present, integration-guide contains zerops.yaml copy, comment ratio in YAML block | "Run smoke test" |
| `smoke-test` | Agent reports test passed (attestation) | "Complete generate" |

**Deploy sub-steps** (in order — the order adapts based on plan shape):

| SubStep | Validates | Next guidance |
|---------|-----------|---------------|
| `deploy-dev` | Deploy returned ACTIVE for all dev targets | "Start processes" |
| `start-processes` | Agent reports processes started | "Verify" |
| `verify-dev` | zerops_verify called for all dev targets, subdomain enabled | "Check initCommands" (or "Dispatch subagent" for showcase) |
| `init-commands` | Agent read logs and confirmed initCommands ran | "Iterate or advance" |
| `subagent` | (showcase) Feature files exist, git committed | "Browser walk" |
| `browser-walk` | (showcase) Agent reports clean walk | "Cross-deploy" |
| `cross-deploy` | Deploy returned ACTIVE for all stage targets | "Verify stage" |
| `verify-stage` | zerops_verify called for all stage targets | "Complete deploy" |

### State model

```go
type RecipeSubStep struct {
    Name        string `json:"name"`
    Status      string `json:"status"`      // pending, in_progress, complete, skipped
    Attestation string `json:"attestation,omitempty"`
    CompletedAt string `json:"completedAt,omitempty"`
}
```

Added to `RecipeStep`:
```go
type RecipeStep struct {
    // ... existing fields ...
    SubSteps       []RecipeSubStep `json:"subSteps,omitempty"`
    CurrentSubStep int             `json:"currentSubStep,omitempty"`
}
```

### Engine changes

**`RecipeComplete` with substep** (in `engine_recipe.go`):

```go
func (e *Engine) RecipeComplete(ctx context.Context, step, attestation string, 
    checker RecipeStepChecker, substep string) (*RecipeResponse, error) {
    
    if substep != "" {
        // Complete sub-step, validate, advance to next sub-step
        return e.recipeCompleteSubStep(ctx, step, substep, attestation, checker)
    }
    // Existing full-step completion logic
    ...
}
```

**`recipeCompleteSubStep`** (new method):
1. Validate substep name matches current sub-step
2. Run sub-step-specific validator
3. If validation fails → return error with diagnostic (what failed, how to fix)
4. Mark sub-step complete, advance to next
5. If all sub-steps complete → complete the parent step
6. Return response with next sub-step's guidance

### Sub-step validators

**File**: `internal/workflow/recipe_substep_validators.go` (new)

Each validator is a function with signature:
```go
type SubStepValidator func(ctx context.Context, plan *RecipePlan, state *RecipeState) *SubStepValidationResult

type SubStepValidationResult struct {
    Passed    bool     `json:"passed"`
    Issues    []string `json:"issues,omitempty"`    // what failed
    Guidance  string   `json:"guidance,omitempty"`   // targeted fix guidance
}
```

**zerops-yaml validator** (most impactful):
1. Read zerops.yaml from each codebase mount (SSHFS paths from plan targets)
2. Count setups — must match expected count (2 for minimal, 3 for shared-codebase worker)
3. Verify `setup: dev` and `setup: prod` exist
4. Check comment ratio >= 30%
5. Verify dev/prod envVariables differ on mode flags
6. If schema cache available, validate field names against live schema

**readme validator**:
1. Read README from each codebase mount
2. Check all 3 extract fragments present
3. Check integration-guide contains YAML code block
4. Check intro is 1-3 lines with no headings

**smoke-test validator**: Trust agent attestation (the smoke test is interactive and the agent reports results).

### Guidance per sub-step

When the agent completes a sub-step, `BuildResponse` returns guidance for the NEXT sub-step only — not the full step skeleton.

```go
func (r *RecipeState) buildSubStepGuide(step string, subStep string, kp knowledge.Provider) string {
    // Return the relevant topic content for this sub-step
    // This is equivalent to the agent calling zerops_guidance, but pre-loaded
    switch step {
    case RecipeStepGenerate:
        switch subStep {
        case "zerops-yaml":
            return ResolveTopic("zerops-yaml-rules", step, r.Plan)
        case "app-code":
            return ResolveTopic("dashboard-skeleton", step, r.Plan) // showcase
            // OR minimal: just "Write app code per execution order"
        case "readme":
            return ResolveTopic("readme-fragments", step, r.Plan)
        case "smoke-test":
            return ResolveTopic("smoke-test", step, r.Plan)
        }
    }
    return ""
}
```

### Tool schema change

The `zerops_workflow` tool's `complete` action gains an optional `substep` parameter:

```json
{
  "action": "complete",
  "step": "generate",
  "substep": "zerops-yaml",
  "attestation": "zerops.yaml written with dev+prod setups, 37% comment ratio"
}
```

If `substep` is omitted, the existing full-step completion logic runs (backward compatible).

### Backward compatibility

- Sub-steps are optional — if the agent calls `RecipeComplete(step="generate")` without substep, it works exactly as today
- Sub-step definitions are step-specific — only generate and deploy have sub-steps initially
- The response format is unchanged — `RecipeStepInfo` already has `DetailedGuide` which carries the sub-step guidance
- Existing tests continue to pass because they call RecipeComplete without substep

### What to test

1. **Sub-step progression**: Table-driven test walking through all sub-steps for each shape
2. **Validator accuracy**: Unit tests for each validator (mock SSHFS mounts via `recipeMountBaseOverride`)
3. **Backward compatibility**: Existing RecipeComplete calls without substep still work
4. **Sub-step guidance content**: Each sub-step returns guidance under 5 KB
5. **Validation failure → targeted fix**: When validator fails, the response includes actionable fix guidance

---

## Phase C: Adaptive Guidance

### Goal

The engine tracks which guidance the agent has seen and what it's struggled with, adapting subsequent guidance delivery.

### What changes

1. **Guidance access tracking**: Record which topics the agent fetched via `zerops_guidance`
2. **Failure pattern tracking**: When a sub-step validator fails, record the failure pattern
3. **Adaptive retry delta**: On retry, only surface guidance relevant to the failure pattern
4. **Adaptive topic expansion**: When returning a topic, include related topics if the agent's history suggests they're needed

### State additions

```go
type RecipeState struct {
    // ... existing fields ...
    GuidanceAccess  []GuidanceAccessEntry  `json:"guidanceAccess,omitempty"`
    FailurePatterns []FailurePattern       `json:"failurePatterns,omitempty"`
}

type GuidanceAccessEntry struct {
    TopicID   string `json:"topicId"`
    Step      string `json:"step"`
    Timestamp string `json:"timestamp"`
}

type FailurePattern struct {
    SubStep   string   `json:"subStep"`
    Issues    []string `json:"issues"`
    Iteration int      `json:"iteration"`
    Timestamp string   `json:"timestamp"`
}
```

### Adaptive retry delta

Replace `buildGenerateRetryDelta` and `buildDeployRetryDelta` with a single adaptive function:

```go
func (r *RecipeState) buildAdaptiveRetryDelta(step string, iteration int) string {
    var sb strings.Builder
    
    // 1. Generic escalation tier (existing)
    sb.WriteString(BuildIterationDelta(step, iteration, nil, r.lastAttestation()))
    
    // 2. Failure-specific guidance — only for patterns the agent actually hit
    for _, fp := range r.FailurePatterns {
        if fp.SubStep == "" {
            continue
        }
        sb.WriteString(fmt.Sprintf("\n## Previous failure: %s\n\n", fp.SubStep))
        for _, issue := range fp.Issues {
            sb.WriteString(fmt.Sprintf("- %s\n", issue))
        }
        // Include the relevant topic reference
        sb.WriteString(fmt.Sprintf("\nFetch updated rules: `zerops_guidance topic=\"%s\"`\n", 
            subStepToTopic(fp.SubStep)))
    }
    
    // 3. Topics the agent DIDN'T fetch but should have
    missing := r.missingCriticalTopics(step)
    if len(missing) > 0 {
        sb.WriteString("\n## Topics you may have missed\n\n")
        for _, t := range missing {
            sb.WriteString(fmt.Sprintf("- `zerops_guidance topic=\"%s\"` — %s\n", t.ID, t.Description))
        }
    }
    
    return sb.String()
}
```

### Topic expansion

When the agent fetches a topic, the system can append related topics based on the plan shape and failure history:

```go
func (r *RecipeState) expandTopic(topicID string) []string {
    // If the agent fetches zerops-yaml-rules and has a dual-runtime plan,
    // automatically append dual-runtime-urls (they're almost always needed together)
    expansions := topicExpansionRules[topicID]
    var result []string
    for _, expansion := range expansions {
        if expansion.Predicate == nil || expansion.Predicate(r.Plan) {
            if !r.hasAccessed(expansion.TopicID) {
                result = append(result, expansion.TopicID)
            }
        }
    }
    return result
}
```

**Expansion rules** (data-driven, not hardcoded):

| When agent fetches | Also include if... | Rationale |
|---|---|---|
| `zerops-yaml-rules` | `isDualRuntime` → `dual-runtime-urls` | 90% of dual-runtime zerops.yaml failures are URL-pattern errors |
| `zerops-yaml-rules` | `hasWorker` → `worker-setup` | Worker setup block is required in zerops.yaml |
| `deploy-flow` | `isShowcase` → `subagent-brief` (summary only) | Agent needs to know subagent dispatch is coming |
| `smoke-test` | (always) → `code-quality` | Pre-deploy checklist should be verified alongside smoke test |

### Implementation order within Phase C

1. **Guidance access tracking** — simplest, just log to state
2. **Failure pattern tracking** — requires Phase B validators
3. **Adaptive retry delta** — replaces existing retry delta builders
4. **Topic expansion** — optional optimization, implement last

### What to test

1. **Access tracking**: Verify `zerops_guidance` calls are recorded in state
2. **Failure tracking**: Verify sub-step validation failures are recorded
3. **Adaptive delta content**: Table-driven test with different failure patterns → different delta content
4. **Topic expansion**: Verify expansion rules fire correctly and don't expand already-accessed topics
5. **No regression**: Existing retry delta tests adapted to the new adaptive format

---

## Implementation Sequence

### Phase A (estimated: 3-4 files new, 4-5 files modified)

**Step 1: Wire finalize + close block catalogs**
- Modify `recipe.md`: wrap finalize content in `<block>` tags
- Modify `recipe.md`: wrap close content in `<block>` tags
- Modify `recipe_section_catalog.go`: populate `recipeFinalizeBlocks` and `recipeCloseBlocks`
- Modify `recipe_guidance.go`: finalize/close cases use `composeSection` instead of raw `ExtractSection`
- Update `recipe_section_catalog_test.go`: add coverage for new catalogs
- Update `recipe_guidance_test.go`: adjust caps (finalize/close will shrink for narrow shapes)

**Step 2: Split `dev-deploy-flow-core`**
- Modify `recipe.md`: split the monolith into 5 sub-blocks
- Modify `recipe_section_catalog.go`: replace single `dev-deploy-flow-core` entry with 5 entries + predicates
- Update tests: caps, content placement

**Step 3: Deduplicate cross-step content**
- Modify `recipe.md`: extract "where-commands-run" as standalone block, replace duplicates with references
- Modify `recipe.md`: close browser walk references deploy's block
- Modify `recipe.md`: generate code-quality references finalize comment-style
- Update content placement tests

**Step 4: Build topic registry**
- New file: `recipe_topic_registry.go`
- New file: `recipe_topic_registry_test.go` — topic ↔ block parity test

**Step 5: Build `zerops_guidance` tool**
- New file: `internal/tools/guidance.go`
- Modify `internal/server/server.go`: register the tool
- New file: `internal/tools/guidance_test.go`

**Step 6: Build skeletons**
- Modify `recipe.md`: add skeleton sections
- New function: `composeSkeleton` in `recipe_guidance.go`
- Modify `resolveRecipeGuidance`: return skeleton for generate/deploy/finalize/close
- Update all guidance tests: new cap values, skeleton content assertions

### Phase B (estimated: 2-3 files new, 3-4 files modified)

**Step 7: Sub-step state model**
- Modify `recipe.go`: add SubSteps to RecipeStep
- New function: `initSubSteps(step string, plan *RecipePlan)` — generates sub-step list based on step + plan shape
- Modify `BuildResponse`: include sub-step progress

**Step 8: Sub-step completion**
- Modify `engine_recipe.go`: RecipeComplete handles substep parameter
- New method: `recipeCompleteSubStep` on Engine
- Modify `workflow_recipe.go`: pass substep from tool input

**Step 9: Sub-step validators**
- New file: `recipe_substep_validators.go`
- Validators for: zerops-yaml, readme, scaffold, app-code
- Tests using `recipeMountBaseOverride` for SSHFS mock

**Step 10: Sub-step guidance delivery**
- Modify `buildGuide` on RecipeState: if sub-steps active, return sub-step guidance
- Test sub-step guidance content

### Phase C (estimated: 1-2 files new, 2-3 files modified)

**Step 11: Tracking**
- Modify `recipe.go`: add GuidanceAccess and FailurePatterns to RecipeState
- Modify `tools/guidance.go`: record access on every call
- Modify `recipe_substep_validators.go`: record failure patterns

**Step 12: Adaptive retry**
- New method: `buildAdaptiveRetryDelta` on RecipeState
- Replace `buildDeployRetryDelta` and `buildGenerateRetryDelta` calls
- Port existing retry delta tests to adaptive format

**Step 13: Topic expansion**
- New data: `topicExpansionRules` in topic registry
- Modify `ResolveTopic`: append expanded topics
- Test expansion behavior

---

## Critical Invariants (Must Hold After Each Phase)

1. **No framework/runtime hardcoding** — All guidance is structural. No block mentions a specific framework name, runtime version, or package manager by name. The agent fills those from plan research data. Test: `TestRecipe_NoFrameworkHardcoding` (existing) must pass.

2. **Predicate parity** — Every topic's predicate must match its source block's predicate. Test: table-driven comparison.

3. **Monotonicity** — Narrower shapes' total guidance (skeleton + all topic fetches) ≤ wider shapes'. Test: adapt existing `TestRecipe_DetailedGuide_MonotonicityInvariant`.

4. **Self-containedness** — Each topic response must be independently actionable without requiring another topic to be fetched first. Exception: compound topics that explicitly aggregate related blocks.

5. **Backward compatibility** — An agent that ignores `zerops_guidance` and reads only `DetailedGuide` must still be able to complete the workflow. The skeleton must be sufficient as a standalone (even if suboptimal).

6. **No content loss** — Every sentence in the current recipe.md must be reachable via some path (skeleton inline, topic fetch, or retry delta). Test: hash every block in the current guide, verify each hash appears in either skeleton, a topic response, or is explicitly deleted with justification.

7. **Agent perspective** — The agent using ZCP sees `zerops_guidance` as a read-only reference tool, like `zerops_knowledge`. It is not required to call it — but the skeleton makes it obvious when to call it and what to ask for. The tool is the "smart man's manual page" — you look it up when you need it.

---

## Size Budget (Target After Phase A)

| Shape | Skeleton | Max topic | Est. total fetched | Current |
|-------|----------|-----------|-------------------|---------|
| hello-world generate | ~2.5 KB | ~3 KB | ~12 KB (4-5 fetches) | 19.4 KB |
| dual-runtime generate | ~3.5 KB | ~4 KB | ~25 KB (8-10 fetches) | 39.7 KB |
| hello-world deploy | ~2 KB | ~3 KB | ~10 KB (3-4 fetches) | 22.5 KB |
| dual-runtime deploy | ~3 KB | ~4 KB | ~28 KB (8-10 fetches) | 40 KB |
| hello-world finalize | ~1.5 KB | ~3 KB | ~8 KB (3 fetches) | 15.9 KB |
| hello-world close | ~1 KB | ~3 KB | ~6 KB (2-3 fetches) | 14.1 KB |

**Key insight**: The total bytes the agent sees over the course of the step may be similar to or even higher than today — but they arrive **when needed**, in **3 KB chunks**, competing with nothing else in the context window. The agent's attention at any moment is focused on 3 KB of relevant rules, not 40 KB of everything.

---

## Files Modified/Created Summary

### Phase A
| Action | File |
|--------|------|
| **Modify** | `internal/content/workflows/recipe.md` — add block tags (finalize, close), split deploy monolith, add skeletons, deduplicate |
| **Modify** | `internal/workflow/recipe_section_catalog.go` — populate finalize/close catalogs, update deploy catalog |
| **Modify** | `internal/workflow/recipe_guidance.go` — skeleton composition, topic resolution |
| **Modify** | `internal/workflow/recipe_guidance_test.go` — new caps, skeleton assertions |
| **Modify** | `internal/workflow/recipe_section_catalog_test.go` — new catalog coverage |
| **Modify** | `internal/workflow/recipe_content_placement_test.go` — update placement assertions |
| **Modify** | `internal/server/server.go` — register zerops_guidance |
| **New** | `internal/workflow/recipe_topic_registry.go` — topic → block mapping |
| **New** | `internal/workflow/recipe_topic_registry_test.go` — parity tests |
| **New** | `internal/tools/guidance.go` — zerops_guidance MCP tool handler |
| **New** | `internal/tools/guidance_test.go` — tool handler tests |

### Phase B
| Action | File |
|--------|------|
| **Modify** | `internal/workflow/recipe.go` — SubSteps on RecipeStep |
| **Modify** | `internal/workflow/engine_recipe.go` — sub-step completion |
| **Modify** | `internal/tools/workflow_recipe.go` — substep parameter |
| **New** | `internal/workflow/recipe_substep_validators.go` — validators |
| **New** | `internal/workflow/recipe_substep_validators_test.go` — validator tests |

### Phase C
| Action | File |
|--------|------|
| **Modify** | `internal/workflow/recipe.go` — tracking fields |
| **Modify** | `internal/workflow/recipe_guidance.go` — adaptive retry |
| **Modify** | `internal/tools/guidance.go` — access recording |
| **Modify** | `internal/workflow/recipe_topic_registry.go` — expansion rules |

---

## Agent Experience: Before and After

### Before (current system)

```
Agent receives RecipeResponse for generate step:
{
  "current": {
    "detailedGuide": "## Generate — App Code & Configuration\n\n### Container state during generate\n\nThe dev service is RUNNING...\n\n### WHERE to write files\n\n**Multi-codebase plans**...\n\n### What to generate per recipe type\n\n**Type 4 (showcase):**...\n\n[... 39,700 bytes of continuous text ...]"
  }
}
```

Agent must:
1. Read 40 KB of text
2. Find the relevant sections for its current sub-task
3. Hold rules in memory while writing code
4. Hope it doesn't forget the execOnce trap 200 lines later

### After (Phase A)

```
Agent receives RecipeResponse for generate step:
{
  "current": {
    "detailedGuide": "## Generate — App Code & Configuration\n\n### Constraints\n- Dev containers RUNNING but env vars NOT active\n- Each codebase independent — never cross-scaffold\n- Comment ratio >= 30%\n\n### Execution order\n1. Scaffold on mount [topic: where-to-write]\n2. Write zerops.yaml [topic: zerops-yaml-rules]\n   - Dual-runtime URLs [topic: dual-runtime-urls]\n3. Write app code [topic: dashboard-skeleton]\n4. Write README [topic: readme-fragments]\n5. Smoke test [topic: smoke-test]\n6. Git init + commit\n\nFetch guidance: zerops_guidance topic=\"{id}\""
  }
}
```

Agent:
1. Reads 3 KB skeleton — understands the plan
2. Before writing zerops.yaml: `zerops_guidance topic="zerops-yaml-rules"` → gets 3 KB of rules
3. For dual-runtime: `zerops_guidance topic="dual-runtime-urls"` → gets 2.5 KB of URL pattern
4. Before smoke test: `zerops_guidance topic="smoke-test"` → gets 1 KB of instructions
5. Each fetch is focused, timely, and in context

### After (Phase A + B)

```
Agent completes scaffold sub-step:
{
  "action": "complete",
  "step": "generate",
  "substep": "scaffold",
  "attestation": "Project scaffolded on /var/www/apidev and /var/www/appdev"
}

Engine validates and responds:
{
  "current": {
    "name": "generate",
    "detailedGuide": "### Next: Write zerops.yaml\n\nWrite ALL setups in each codebase's zerops.yaml...\n[2.5 KB of zerops.yaml rules, pre-filtered for dual-runtime]"
  }
}
```

Agent:
1. Gets exactly the guidance it needs for the next sub-task
2. Engine validated the scaffold exists before moving on
3. If zerops.yaml fails validation: engine returns specific issues ("Missing setup: worker in apidev/zerops.yaml, comment ratio is 22% (need 30%)")
4. No wasted attention, no forgotten rules, no 40 KB scroll

---

## Decision Log

| Decision | Rationale |
|----------|-----------|
| Topics map to existing block names | Preserves the block-as-unit-of-composition principle; no content restructuring needed |
| Skeletons live in recipe.md | Same file, same authors, same test harness — avoids content/code split |
| `zerops_guidance` is a separate tool from `zerops_knowledge` | Different purpose: knowledge is platform reference, guidance is step-specific workflow rules |
| Sub-step validators read SSHFS mounts | The mounts are already available; reading files is idempotent and fast |
| Phase C is optional | Phases A and B deliver 90% of the value; C is optimization |
| Compound topics aggregate blocks | The agent should get "all zerops.yaml rules" in one fetch, not 5 separate calls |
| Backward compatibility via skeleton fallback | An agent that doesn't know about `zerops_guidance` still gets a useful skeleton |
