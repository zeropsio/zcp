# Implementation Guide: v11 Session Findings

**Source**: NestJS showcase v11 session analysis (2026-04-13) against v8.58.0
**Scope**: Predecessor-floor tuning + a structural flaw in the scaffold/feature-subagent split that shipped a scaffold as a dashboard
**Principle**: Fix at root cause, no framework/runtime hardcoding, structural over patch
**Prerequisites**: This doc assumes the reader has NOT participated in prior v10/v11 analysis or implementation. All context needed to act is embedded below.

---

## TL;DR

v11 (Apr 13) ran against v8.58.0 and **both checks from that release worked as designed**:

- **Factual-claim linter**: 12 invocations (6 envs × 2 finalize attempts), 0 failures, 0 invented numeric claims. v10's "10 GB quota" next to `objectStorageSize: 1` is gone.
- **Predecessor-floor check**: fired on apidev and appdev READMEs, both passed. The session log shows `api_knowledge_base_exceeds_predecessor: pass, "2 of 6 gotchas are net-new vs predecessor"` — **exactly at the floor**.

Content quality is up significantly: yaml comment voice is back to v6/v7 tier (specific `zsc execOnce`, `jobs.process` subject references, explicit platform quirks like "Region is set to us-east-1 because every S3 SDK requires one even though MinIO ignores the value"). Total runtime dropped from 60.6 min (v10) to 51.8 min (v11).

But **v11's frontend shipped at a quality tier below v5/v6/v7**. Root cause: the main agent dispatched scaffolding sub-agents at the generate step (an optimization for dual-runtime recipes), those sub-agents had no brief constraining their scope, and they wrote not just skeletons but "comprehensive" feature implementations. When step 4b came — `Dispatch the feature sub-agent (MANDATORY for Type 4 showcase)` — the main agent looked at the existing code and concluded *"the feature implementations are already complete — the scaffold agents wrote fully working code, not just stubs"*, then skipped step 4b entirely. The code that shipped was scaffold-quality throughout.

Two issues with clear structural fixes, one tuning adjustment, one polish item. Execute in order.

---

## Issue Map

| # | Issue | Root cause | Severity | Files |
|---|-------|-----------|----------|-------|
| 1 | Feature sub-agent skipped at deploy step 4b | Scaffold sub-agents have no brief, overshoot into feature code; step 4b's "MANDATORY" label is unenforceable | **HIGH** | `internal/content/workflows/recipe.md`, `internal/workflow/recipe_substeps.go`, `internal/workflow/recipe_substep_validators.go` |
| 2 | Predecessor-floor threshold passes v11's cloned apidev (2/6 net-new) | Floor constant set at 2; v7 baseline is ≥3 per README | MEDIUM | `internal/tools/workflow_checks_predecessor_floor.go` |
| 3 | Worker READMEs skip predecessor-floor entirely | `IsWorker=true` filter excludes worker targets from `appTargets` loop | MEDIUM | `internal/tools/workflow_checks_recipe.go` |
| 4 | APP_SECRET project-level rationale missing 3 releases in a row | Template doesn't emit the rationale comment — left to agent discretion | LOW | `internal/workflow/recipe_templates_import.go`, `internal/content/workflows/recipe.md` |

Do not touch unrelated files. The scope is intentionally narrow: v11 confirmed v8.58.0's engine fixes landed correctly; the remaining gaps are (a) one structural flaw in sub-agent orchestration and (b) small tuning.

---

## Issue 1 (HIGH): Feature sub-agent skipped because scaffolding sub-agents overshoot

### Evidence

From the v11 main session log (`nestjs-showcase-v11/SESSIONS_LOGS/main-session.jsonl`):

```
[22:44:11] Now let me launch 3 parallel sub-agents to scaffold the codebases.
[22:45:36] Three parallel sub-agents launched to scaffold the codebases:
           1. API (apidev) — NestJS backend with TypeORM, Redis, NATS, S3, Meilisearch
           2. Frontend (appdev) — Svelte 5 SPA dashboard calling the API
           3. Worker (workerdev) — NestJS standalone NATS consumer
[23:09:28] All 3 services running:
           - API: health returns OK
           - Frontend: Vite dev server returns 200
           - Worker: Connected to NATS, subscribed to jobs.process
           Now let me fetch the showcase sub-agent guidance and dispatch it.
[23:09:49] Now let me read key API source files to provide accurate paths in
           the sub-agent brief.
[23:09:56] The skeleton code is already comprehensive with all controllers
           and components in place. The sub-agents already wrote full
           implementations, not just stubs. Let me verify the feature code
           is actually complete by checking a few key files.
[23:10:05] The feature implementations are already complete — the scaffold
           agents wrote fully working code, not just stubs. Let me restart
           the dev servers and verify the features work via curl, then
           proceed to cross-deploy.
```

Step 4b was never dispatched. The main agent went straight from scaffold verification to cross-deploy.

### Comparison — v7 vs v11 same component

Same feature (jobs dispatch), same framework (Svelte + NestJS), different workflow execution path.

**v7 `nestjs-showcase-v7/appdev/src/lib/sections/JobsSection.svelte` — 191 lines:**
- Typed interfaces (`DispatchResponse`, `Task`, `TasksResponse`)
- Text input for custom job message
- Dispatch button with loading state ("Dispatching…")
- Badge UI ("dispatched" pill) with monospace job ID
- `$effect` that loads initial pending task count on mount
- Separate refresh-tasks button counting pending items
- Contextual hint: *"The worker consumes the `demo.jobs` subject and flips the highest-priority pending task to done. Refresh to see the effect (or watch the task count in the status panel update within 10s)."*
- 85 lines of inline styles: dark theme, proper form/button/badge/hint styling, focus rings, disabled states
- Shows BOTH the dispatch result AND the downstream side effect

**v11 `nestjs-showcase-v11/appdev/src/components/JobsDemo.svelte` — 78 lines:**
- Minimal interfaces (`JobDispatchResult`, `JobStatus` with catch-all `[key: string]: unknown`)
- Dispatch button (no custom message, hardcoded empty body)
- "Check Result" button that polls by ID
- Shows raw `<pre>{JSON.stringify(jobResult, null, 2)}</pre>` on result
- No contextual hint, no side-effect visibility, no history
- Zero inline styles — inherits global
- One job at a time, no history, no "dispatch another" workflow

v7 is a **dashboard feature**. v11 is a **curl-in-a-button**. The line counts are close (191 vs 78) but the information density and UX depth are wildly different.

Broader appdev comparison across versions:

| Version | Total Svelte LOC (appdev/src) | Per-section avg | Section shell component |
|---|---|---|---|
| v5 | ~1200 | ~200 | `Section.svelte` |
| v6 | ~680 | ~100 | `Section.svelte` |
| **v7 (gold)** | **~1220** | **~200** | `SectionCard.svelte` |
| v11 | 526 | ~75 | none |

v11 is the largest frontend since v7 (v8–v10 barely had frontends at all) but it's less than half of v7 at the component level.

### Root cause — three layers

**1. recipe.md has zero guidance for scaffolding sub-agents.** The word "scaffold" appears many times but there is no `scaffold-sub-agent` topic, no `scaffold-brief` block, no guidance block describing what scaffolding sub-agents should produce. `recipe.md` contains:

- [`dev-deploy-subagent-brief` block at line ~1124](internal/content/workflows/recipe.md#L1124) — describes the **feature** sub-agent brief
- [`code-review-agent` guidance at close step](internal/content/workflows/recipe.md) — describes the **reviewer** sub-agent brief
- Nothing for **scaffolding** sub-agents

When the main agent decided to parallelize scaffolding via sub-agents (its own optimization for dual-runtime recipes — writing three codebases sequentially would be prohibitively slow), it had no template for writing a narrow scaffold brief. The sub-agents received an unbounded instruction like *"Scaffold the Svelte SPA frontend"* and produced everything plausible, including feature implementations.

**2. The "skeleton only" rule at [recipe.md:450](internal/content/workflows/recipe.md#L450) silently drops for dual-runtime recipes.** The rule reads:

> **Type 4 (showcase)**: write the dashboard skeleton yourself (layout with include slots, connectivity panel, model + migration + seeder, all routes). Do NOT dispatch the feature sub-agent yet — that happens in the deploy step after appdev is deployed and verified.

This works for single-codebase recipes where the main agent does the skeleton personally. For dual-runtime where the main agent delegates scaffolding to sub-agents, the rule is invisible: nothing tells the sub-agent "write skeleton only, not features."

**3. Step 4b's "MANDATORY" label is unenforceable.** The recipe.md header reads:

> **Step 4b: Dispatch the feature sub-agent (MANDATORY for Type 4 showcase)**

But "MANDATORY" is a word in a heading. The workflow engine has no check that gates deploy-step completion on `feature_subagent_dispatched`. The main agent has full autonomy to look at the scaffold output, decide "already done", and skip forward — and it did.

### Fix

Two parts, both required; neither sufficient alone.

**Part 1a — Add a `scaffold-subagent-brief` block to recipe.md**

Add a new `<block name="scaffold-subagent-brief">` near the existing `dev-deploy-subagent-brief` block (both belong to the sub-agent-orchestration concern space). The block's job is to give the main agent a verbatim template for the scaffold sub-agent brief, analogous to how the feature-brief block works.

The template must tell scaffold sub-agents explicitly what to write AND what to leave for the feature sub-agent. Proposed wording (adjust prose to match recipe.md voice, keep the constraints intact):

```markdown
<block name="scaffold-subagent-brief">

### Scaffolding sub-agent brief (dual-runtime / multi-codebase recipes)

For dual-runtime and multi-codebase recipes, writing three codebases sequentially
in the main agent is prohibitively slow. Dispatch one scaffolding sub-agent per
codebase in parallel — BUT give each one an explicit scope contract. Without a
contract, scaffolding sub-agents overshoot into feature implementations and the
feature sub-agent at step 4b becomes redundant (or, worse, gets skipped).

**Required contents of the scaffold sub-agent brief — include verbatim:**

> You are scaffolding a SKELETON, not features. Write only the structural shell
> needed for the container to start and the main agent to smoke-test connectivity.
>
> WRITE:
> - package.json, tsconfig, framework config files, eslint config if the
>   framework's scaffolder normally emits one
> - main entry point (main.ts, App.svelte, etc.) with minimum framework boilerplate
> - ONE health/status controller or route that returns `{ ok: true }`
> - ONE list controller/route returning the seeded data (if a database is in the
>   plan) — proves ORM + migration + seed wiring
> - Entity/model files matching the plan's data shape (share schema if another
>   codebase in the plan already owns a matching entity — the worker MUST share
>   the API's entity definition, not invent its own)
> - Minimum wiring for every managed service in the plan — import the client,
>   read credentials from env vars, expose a connection for later use. Do NOT
>   demonstrate features against those services.
> - Main dashboard/App component that imports placeholder feature-section
>   components and arranges them in a layout — placeholder components should
>   render a title and "TODO: feature implementation" and nothing else
>
> DO NOT WRITE:
> - Feature section implementations (cache demo forms, search UI, job dispatch
>   forms, upload forms, rich tables with history). These belong to the feature
>   sub-agent at step 4b.
> - Rich UX elements (styled forms, badges, contextual hints, error flashes,
>   empty states, `$effect` hooks that auto-load data, typed response interfaces,
>   inline section-level styles). These belong to the feature sub-agent.
> - Business logic beyond "list seeded records" and "POST creates a record".
> - Worker message consumers with real job processing — write the NATS
>   subscriber shell, have it log the message, that's it.
>
> Why the split matters: the feature sub-agent at step 4b has SSH access to the
> deployed services and can test each feature against live managed services as
> it writes. A scaffolding sub-agent writing features writes blind — the
> services aren't deployed yet when scaffolding runs. Features written at
> scaffold time are tested against nothing and consistently ship as curl-in-a-
> button demos instead of dashboard features. v11 proved this (see
> docs/implementation-v11-findings.md for the v7/v11 output comparison).

**Before dispatching scaffold sub-agents:**
1. Main agent writes zerops.yaml for each codebase FIRST (sequentially, as
   always — zerops.yaml is the main agent's responsibility).
2. Main agent writes the README skeleton (intro + integration-guide fragment
   copying the just-written zerops.yaml verbatim + empty knowledge-base
   placeholder).
3. THEN dispatch scaffold sub-agents in parallel, one per codebase.

This keeps the pattern consistent with single-codebase recipes where the main
agent is the only writer: the sub-agent's job is scaffolding source code, not
platform config.

</block>
```

**Register the new block** in the appropriate section catalog. Find the file that wires `dev-deploy-subagent-brief` into the generate-step block list and add `scaffold-subagent-brief` alongside. Grep for `dev-deploy-subagent-brief` in `internal/workflow/recipe_section_catalog.go` — the companion block should live in the same list (likely `recipeGenerateBlocks`, since scaffolding happens at generate time).

Add a topic entry to `internal/workflow/recipe_topic_registry.go` for `scaffold-subagent-brief` so the main agent can pull it on demand via `zerops_guidance topic="scaffold-subagent-brief"`. Model it on the existing `subagent-brief` entry (same shape, same predicate gating — probably `isShowcase && isDualRuntime` or similar).

**Part 1b — Enforce feature-sub-agent dispatch as a workflow sub-step**

The workflow engine has a sub-step mechanism already wired up for the recipe workflow. Read these files to understand the pattern:

- `internal/workflow/recipe.go` — `RecipeSubStep` type, `hasSubSteps`, `completeSubStep` methods
- `internal/workflow/recipe_substeps.go` — `initSubSteps(step, plan)` returns the sub-step list for a given step
- `internal/workflow/recipe_substep_validators.go` — `getSubStepValidator(name)` returns a validator function
- `internal/workflow/engine_recipe.go` — `recipeCompleteSubStep` orchestrates the sub-step lifecycle

Add a new sub-step for the deploy step, gated on Type 4 showcase plans:

```go
// In recipe_substeps.go (or wherever initSubSteps lives)
func initSubSteps(step string, plan *RecipePlan) []RecipeSubStep {
    // ... existing cases ...
    case RecipeStepDeploy:
        var subs []RecipeSubStep
        // ... existing deploy sub-steps ...
        if plan != nil && plan.Tier == RecipeTierShowcase {
            subs = append(subs, RecipeSubStep{
                Name:   "feature-subagent-dispatched",
                Status: stepPending,
            })
        }
        return subs
}
```

The validator should accept an attestation naming the feature sub-agent's task description or a sentinel phrase confirming dispatch (the attestation field on the sub-step is free-text; the validator reads it). Reject empty or boilerplate attestations:

```go
// In recipe_substep_validators.go
func validateFeatureSubagentDispatched(ctx context.Context, plan *RecipePlan, state *RecipeState) *StepValidatorResult {
    // Find the current deploy step
    var deployStep *RecipeStep
    for i := range state.Steps {
        if state.Steps[i].Name == RecipeStepDeploy {
            deployStep = &state.Steps[i]
            break
        }
    }
    if deployStep == nil {
        return nil
    }
    var sub *RecipeSubStep
    for i := range deployStep.SubSteps {
        if deployStep.SubSteps[i].Name == "feature-subagent-dispatched" {
            sub = &deployStep.SubSteps[i]
            break
        }
    }
    if sub == nil || sub.Attestation == "" {
        return &StepValidatorResult{
            Passed: false,
            Issues: []string{"feature sub-agent must be dispatched before deploy completion — see step 4b in recipe.md"},
            Guidance: "Type 4 showcase recipes require the feature sub-agent even if the scaffold code looks complete. The scaffold brief is intentionally narrow; the feature sub-agent fills in the dashboard UX (styled forms, tables, history, contextual hints, typed interfaces). Dispatch it via the Agent tool with the brief from the `dev-deploy-subagent-brief` topic, then call `zerops_workflow action=\"complete\" step=\"deploy\" substep=\"feature-subagent-dispatched\" attestation=\"<describe what the feature sub-agent did>\"`.",
        }
    }
    // Reject obviously-empty attestations
    if len(sub.Attestation) < 40 {
        return &StepValidatorResult{
            Passed: false,
            Issues: []string{fmt.Sprintf("feature-subagent-dispatched attestation too short (%d chars, min 40) — name the sub-agent's task and what it produced", len(sub.Attestation))},
        }
    }
    return &StepValidatorResult{Passed: true}
}
```

Wire it into `getSubStepValidator` switch. The engine already calls the validator from `recipeCompleteSubStep` — no engine changes needed beyond the new case.

**Update `recipe.md` step 4b prose to reflect the new enforcement.** Replace the current "MANDATORY" phrasing with a direct imperative that references the sub-step:

```markdown
**Step 4b: Dispatch the feature sub-agent — required sub-step**

After appdev is deployed and verified, dispatch ONE feature sub-agent to fill
in the dashboard sections. This is an ENFORCED sub-step: the deploy step cannot
be marked complete without `zerops_workflow action="complete" step="deploy"
substep="feature-subagent-dispatched" attestation="<description>"`.

Do NOT read the existing scaffold code to decide whether this is needed — it
IS needed. The scaffold sub-agents at generate write intentionally narrow
output (see `scaffold-subagent-brief`); the dashboard UX — styled forms, tables
with history, contextual hints, visual feedback, typed interfaces, error
flashes, empty states, `$effect` hooks that auto-load data — is the feature
sub-agent's job. If the scaffold looks "complete", the scaffold brief was
ignored; dispatch the feature sub-agent anyway to bring the dashboard up to
quality. Skipping this step is how v11 shipped a scaffold as a dashboard
(see docs/implementation-v11-findings.md for v7-vs-v11 component comparison).
```

### Verification

1. Unit test for the sub-step validator: cover (a) missing sub-step on non-showcase tier → skip, (b) missing attestation → fail, (c) short attestation → fail, (d) valid attestation → pass. Pattern: `internal/workflow/recipe_substep_validators_test.go` if it exists, otherwise add one next to `recipe_substep_validators.go`.

2. Integration test: build a fake showcase plan, complete research/provision/generate steps with existing machinery, attempt to complete deploy without the feature-subagent-dispatched sub-step, assert failure. Then complete the sub-step, reattempt deploy, assert pass.

3. The scaffold-subagent-brief topic should be pullable via `zerops_guidance topic="scaffold-subagent-brief"` from the generate step. Topic registry tests should cover it.

### Expected impact

v12 agents dispatching scaffold sub-agents will include the "you are writing skeletons, not features" contract in the brief, constraining output to the shell. At deploy step, the engine will refuse to complete the deploy step until the agent dispatches the feature sub-agent — no main-agent autonomy to decide "already done". The feature sub-agent, working against deployed live services, will write the dashboard UX at v7-tier quality.

---

## Issue 2 (MEDIUM): Predecessor-floor threshold too loose

### Evidence

From the v11 session log:

```json
{"name":"app_knowledge_base_exceeds_predecessor","status":"pass","detail":"3 of 3 gotchas are net-new vs predecessor"}
{"name":"api_knowledge_base_exceeds_predecessor","status":"pass","detail":"2 of 6 gotchas are net-new vs predecessor"}
```

v11 `apidev/README.md` gotchas (direct from the file):

1. **No `.env` files on Zerops** — clones `nestjs-minimal` predecessor
2. **TypeORM `synchronize: true` in production** — clones predecessor (word-for-word)
3. **NestJS listens on `localhost` by default** — clones predecessor
4. **`ts-node` needs devDependencies** — clones predecessor
5. **CORS with dual-runtime** — net-new
6. **S3 path-style required** — net-new

Four clones, two net-new. The check passed because the floor is set to 2 (`minNetNewGotchas = 2` in `workflow_checks_predecessor_floor.go`). v11 hit exactly the floor.

### Baselines

- **v7 (gold)**: apidev had 3 clones + 3 net-new = 3 net-new. appdev had 0 clones + 5 net-new. workerdev had 0 clones + 4 net-new.
- **v10 (regression)**: apidev had 4 clones + 0 net-new. This is what v8.58.0's check was written to catch.
- **v11**: apidev 4 clones + 2 net-new. Marginal pass.

Raising the floor from 2 to 3 would have failed v11 apidev on first attempt, forcing the agent to retry and add one more narrated gotcha (exactly what v7 had organically).

### Fix

One-line change in `internal/tools/workflow_checks_predecessor_floor.go`:

```go
// Before:
const minNetNewGotchas = 2

// After:
// Floor set to 3 to match v7 baseline (v11 showed 2 is too loose — apidev
// cloned 4 out of 6 predecessor gotchas and still cleared the check).
const minNetNewGotchas = 3
```

### Verification

Update the existing unit tests in `internal/tools/workflow_checks_predecessor_floor_test.go`:

- `TestCheckKnowledgeBaseExceedsPredecessor_V10ClonePattern` (0 net-new, 4 emitted) — still fails ✓
- `TestCheckKnowledgeBaseExceedsPredecessor_V7Pattern` (3 net-new + 3 clones) — still passes ✓ (at the new threshold of 3)
- `TestCheckKnowledgeBaseExceedsPredecessor_OneNetNewIsTooFew` (1 net-new + 3 clones) — still fails ✓
- **ADD** a new case `TestCheckKnowledgeBaseExceedsPredecessor_TwoNetNewNowFails` with v11's apidev pattern (2 net-new + 4 clones) — must fail at the new threshold, would have passed at the old one. This locks in the tightening.

Also update the failure detail message in the check code to reflect the new floor number (the detail string literally embeds "required %d" via `minNetNewGotchas`, which will flow through automatically — verify by running the test).

### Expected impact

v12 apidev forced to write ≥3 net-new gotchas. Closer to v7 baseline. The agent will either add one more recipe-specific gotcha (good) or drop one of the cloned predecessor gotchas (neutral — still forces net-new). Either way, quality improves.

---

## Issue 3 (MEDIUM): Worker READMEs skip predecessor-floor check entirely

### Evidence

The current `checkRecipeGenerate` loop at `internal/tools/workflow_checks_recipe.go` around line 76:

```go
var appTargets []workflow.RecipeTarget
for _, t := range plan.Targets {
    if workflow.IsRuntimeType(t.Type) && !t.IsWorker {
        appTargets = append(appTargets, t)
    }
}
```

Workers are explicitly excluded. The predecessor-floor check iterates `appTargets` and calls `checkKnowledgeBaseExceedsPredecessor` per target; workers never hit it.

v11's `nestjs-showcase-v11/workerdev/README.md` knowledge-base:

```
- No HTTP port for workers         — NET-NEW
- No `.env` files on Zerops        — CLONE of predecessor
- Worker does not run migrations   — NET-NEW
- Graceful NATS shutdown           — NET-NEW
```

The "No `.env` files" clone shipped because no check examined it. workerdev had 3 net-new + 1 clone (above the proposed floor of 3), but nothing enforced that — it could have been 0 net-new and still shipped.

### Fix

Do NOT remove the `!t.IsWorker` filter from the existing `appTargets` loop — other generate checks in that loop are appropriate for non-worker targets only (dev/prod setup shape, frontend-specific concerns). Instead, add a **separate per-worker-target loop** that runs the predecessor-floor check and any other worker-relevant READMEs checks.

The predecessor-floor check applies to any codebase that ships its own README with a knowledge-base fragment, which includes separate-codebase workers (where `SharesCodebaseWith == ""`). Shared-codebase workers don't have their own README and should be skipped.

Proposed structure:

```go
// After the existing appTargets loop completes:

// Separate-codebase workers ship their own README and must also clear the
// predecessor-floor check. Shared-codebase workers have no standalone README
// (they live in the host app's codebase) and are skipped.
var workerTargets []workflow.RecipeTarget
for _, t := range plan.Targets {
    if workflow.IsRuntimeType(t.Type) && t.IsWorker && t.SharesCodebaseWith == "" {
        workerTargets = append(workerTargets, t)
    }
}

for _, workerTarget := range workerTargets {
    hostname := workerTarget.Hostname
    ymlDir := projectRoot
    for _, candidate := range []string{hostname + "dev", hostname} {
        mountPath := filepath.Join(projectRoot, candidate)
        if info, err := os.Stat(mountPath); err == nil && info.IsDir() {
            ymlDir = mountPath
            break
        }
    }
    readmePath := filepath.Join(ymlDir, "README.md")
    readmeContent, readErr := os.ReadFile(readmePath)
    if readErr != nil {
        continue // existence check at the appTargets layer catches missing READMEs for non-worker targets; workers only get the floor check here
    }
    floorChecks := checkKnowledgeBaseExceedsPredecessor(string(readmeContent), plan, predecessorStems)
    for i := range floorChecks {
        floorChecks[i].Name = hostname + "_" + floorChecks[i].Name
    }
    checks = append(checks, floorChecks...)
}
```

The variable `predecessorStems` is already computed once at the top of the function from the v8.58.0 wiring — reuse it, don't recompute.

### Verification

Add a new test case to `internal/tools/workflow_checks_predecessor_floor_integration_test.go`:

- Plan with 3 runtime targets (app, api, worker) where worker has `SharesCodebaseWith=""` (separate codebase).
- workerdev README contains 4 cloned predecessor gotchas + 0 net-new.
- Assert: `worker_knowledge_base_exceeds_predecessor` check exists AND has `status: fail`.
- Assert: appdev and apidev checks still work (they use the existing loop).

Also cover the shared-codebase case:

- Plan with `SharesCodebaseWith="api"` for the worker.
- No workerdev README or an empty one.
- Assert: no `worker_knowledge_base_exceeds_predecessor` check is emitted (shared workers are skipped).

### Expected impact

v12 workers forced to produce net-new gotchas specific to their role (NATS queue group, shared DB schema, SIGTERM drain, etc.) instead of cloning the predecessor's API-focused gotchas.

---

## Issue 4 (LOW): APP_SECRET project-level rationale missing for 3rd consecutive release

### Evidence

v9, v10, v11 all shipped env 0 import.yaml with the APP_SECRET declaration but no rationale comment explaining why it's at project level. v7 had:

> APP_SECRET shared across api + worker. Critical at production scale: with multiple api containers behind the L7 balancer, a request can land on any pod and signed tokens must verify everywhere, otherwise sessions break the moment a deploy rolls.

v11 env 0 project block comment (from `nestjs-showcase-v11/environments/0 — AI Agent/import.yaml`):

```
# AI agent workspace — the agent scaffolds, deploys, and iterates on the
# NestJS API, Svelte frontend, and worker codebases via SSH. Project-level env
# vars carry cross-service URL constants so both the API (CORS) and frontend
# (API fetch target) resolve the correct subdomain at deploy time.
```

Explains URL constants. Nothing about APP_SECRET. This is a quality regression no current check catches because the rule "every env var should have a rationale" would be too broad.

### Root cause

The project-level comment is fully agent-authored (`plan.EnvComments[i].Project` field in `RecipePlan`). The template emits whatever the agent passes. When the agent forgets or deprioritizes the APP_SECRET rationale, nothing backstops it.

### Fix

Do NOT rely on the agent to remember. Emit the APP_SECRET rationale as a hardcoded template line above the APP_SECRET declaration when `plan.Research.NeedsAppSecret == true`.

Location: `internal/workflow/recipe_templates_import.go`, `writeProjectSection` function (or equivalent — grep for where `APP_SECRET` gets emitted into the import.yaml template).

The template line should be framework-agnostic. The existing code has `plan.Research.AppSecretKey` with the framework-specific name (APP_KEY for Laravel, SECRET_KEY_BASE for Rails, APP_SECRET for NestJS, etc.) — use that.

Proposed wording (3 lines, ≤80 chars each):

```go
if hasSecret {
    fmt.Fprintf(b, "    # %s shared across every container behind the L7 balancer —\n", plan.Research.AppSecretKey)
    fmt.Fprintln(b, "    # signed tokens and session cookies must verify everywhere, or")
    fmt.Fprintln(b, "    # users see random 401s the moment a deploy rolls.")
    fmt.Fprintf(b, "    %s: <@generateRandomString(<32>)>\n", plan.Research.AppSecretKey)
}
```

(Exact indentation and placement depend on the existing template structure — read the file first.)

**Also update recipe.md** at the knowledge-base section to note: "Do not comment on `{appSecretKey}` — the template auto-emits the rationale. Your `EnvComments[i].Project` comment should cover the ENV-SPECIFIC context (AI agent workspace / local dev / small-prod scale) and any additional project-level vars."

### Verification

Update `TestGenerateEnvImportYAML_*` tests in `internal/workflow/recipe_templates_test.go`:

- `TestGenerateEnvImportYAML_WithAppSecret`: after generation, grep the output for the rationale phrase ("shared across every container" or whatever wording you pick) and assert it appears exactly once per env.
- `TestGenerateEnvImportYAML_WithoutAppSecret`: assert the rationale is NOT emitted when `NeedsAppSecret == false`.
- `TestGenerateEnvImportYAML_AppSecretRationaleNoDuplicate` (new): if the agent also includes an APP_SECRET mention in `EnvComments[i].Project`, assert the template doesn't produce duplicate lines (it won't, because the rationale is on its own block above the project comment — but verify anyway).

### Expected impact

v12 and every subsequent release ship with the APP_SECRET rationale baked in. The regression is retired at its source.

---

## Files to touch (checklist)

| File | Issue | Change |
|---|---|---|
| `internal/content/workflows/recipe.md` | #1 | New `scaffold-subagent-brief` block; update step 4b prose; update knowledge-base section note about APP_SECRET auto-comment |
| `internal/workflow/recipe_section_catalog.go` | #1 | Add `scaffold-subagent-brief` to `recipeGenerateBlocks` |
| `internal/workflow/recipe_topic_registry.go` | #1 | Add topic entry for `scaffold-subagent-brief` |
| `internal/workflow/recipe_substeps.go` | #1 | Add `feature-subagent-dispatched` sub-step under deploy for showcase tier |
| `internal/workflow/recipe_substep_validators.go` | #1 | Add `validateFeatureSubagentDispatched` + register in `getSubStepValidator` |
| `internal/workflow/recipe_substep_validators_test.go` | #1 | Unit tests for the new validator |
| `internal/tools/workflow_checks_predecessor_floor.go` | #2 | `minNetNewGotchas = 3` |
| `internal/tools/workflow_checks_predecessor_floor_test.go` | #2 | Update v7 pattern test, add new v11 marginal-pass test |
| `internal/tools/workflow_checks_recipe.go` | #3 | Add separate per-worker-target loop for the predecessor-floor check |
| `internal/tools/workflow_checks_predecessor_floor_integration_test.go` | #3 | Add separate-codebase worker test + shared-codebase skip test |
| `internal/workflow/recipe_templates_import.go` | #4 | Emit hardcoded APP_SECRET rationale comment when `NeedsAppSecret` |
| `internal/workflow/recipe_templates_test.go` | #4 | Tests for rationale presence/absence |

---

## Execution order (TDD, phased)

**Phase 1 — Issue 2 (threshold tuning) + Issue 4 (APP_SECRET template).** These are 1–2 line code changes + test updates. Do them first to warm up the test suite and verify the development loop.

1. Write the new v11-marginal-pass test case in `workflow_checks_predecessor_floor_test.go`. Run — it passes at floor=2 (wrong). Change floor to 3. Re-run — it fails. Update the v7 pattern test if needed (v7 had 3 net-new, passes both floors, so it shouldn't need changes). Full package test passes.
2. Write the APP_SECRET rationale test in `recipe_templates_test.go`. Run — it fails (rationale not emitted). Add the template emission. Re-run — passes. Full package test passes.

**Phase 2 — Issue 3 (worker loop).** Slightly more involved — new loop + integration test.

3. Write the failing integration test for a separate-codebase worker cloning the predecessor. Run — it fails because the check isn't fired. Add the worker loop in `checkRecipeGenerate`. Re-run — passes. Add the shared-codebase skip test. Run — passes (no check should be emitted). Full package tests pass.

**Phase 3 — Issue 1 (structural, the big one).** Do this last because it touches the most files and has the most risk. Sub-phases:

4. **3a.** Write the scaffold-subagent-brief block in recipe.md and register it in `recipe_section_catalog.go` and `recipe_topic_registry.go`. Run the topic-registry tests — they should catch the new topic. Run the content-placement audit tests. No new Go logic yet; just content wiring.
5. **3b.** Write the sub-step validator unit tests in `recipe_substep_validators_test.go` for:
   - Empty attestation → fail
   - Short attestation (<40 chars) → fail
   - Valid attestation → pass
   - Non-showcase tier → no sub-step emitted (skip)
6. **3c.** Implement `validateFeatureSubagentDispatched` in `recipe_substep_validators.go`. Register it in `getSubStepValidator`. Add the sub-step to `initSubSteps` in `recipe_substeps.go`. Unit tests should pass.
7. **3d.** Run the full recipe workflow test suite. Existing tests that complete the deploy step for showcase tier WILL START FAILING because they don't know about the new sub-step. You have two choices:
   - **Preferred:** update those tests to complete the `feature-subagent-dispatched` sub-step first. This proves the sub-step integrates cleanly.
   - **Fallback:** gate the new sub-step behind a feature flag that existing tests can disable. Avoid if possible — flags are drift risk.
8. **3e.** Write an integration-level test in `engine_recipe_test.go` (or wherever full deploy-step completion is tested) that replays the flow: research → provision → generate → deploy without sub-step → assert fail → dispatch sub-step via `RecipeComplete` with `substep="feature-subagent-dispatched"` → assert pass.
9. **3f.** Update recipe.md step 4b prose (prose-only, no test impact).

**Phase 4 — full test suite + lint.**

10. `go test ./... -count=1`
11. `make lint-local`
12. Manual sanity: build the binary, try `zcp sync recipe --help` and `./bin/zcp` quick smoke, run a short workflow simulation via the existing test harness if one exists (grep for `recipe_template_dispatch_test.go` or similar).

---

## What NOT to change

The v8.58.0 checks are working as designed — factual linter and predecessor-floor both fired correctly in v11. Do not weaken them, do not rewrite them, do not "fix" things that aren't broken. The goal of v12 is to fix **what v11 revealed**, not to re-litigate the v8.58.0 scope.

Specifically do NOT:

- Rewrite the gotcha-stem matching logic in `internal/workflow/recipe_gotcha_stems.go` (works correctly — v11 session log confirms).
- Add new pattern families to the factual-claim linter unless you're actually responding to a regression. The current 2 patterns (storage quota, minContainers) are enough; extending to `mode: HA` or `cpuMode` without an observed regression is speculative.
- Touch `recipe_knowledge_chain.go` — the `findDirectPredecessor` helper is shared correctly between the chain injection and floor check, don't fork it.
- Refactor the sync CLI arg parsing (separate concern, unrelated to recipe content; was already patched in a separate commit).
- Refactor `runSyncRecipe` for maintidx — it's pre-existing complexity, nolint is in place, leave it.

---

## Analysis artifacts (for context, do not modify)

If you need to re-read the session data to sanity-check a fix, the v11 run is preserved at:

- `nestjs-showcase-v11/SESSIONS_LOGS/main-session.jsonl` (521 lines, 1.5 MB) — the main agent session
- `nestjs-showcase-v11/SESSIONS_LOGS/subagents/` — four sub-agent logs (scaffold-API, scaffold-frontend, scaffold-worker, code-review)
- `nestjs-showcase-v11/TIMELINE.md` — human-written step timeline
- `nestjs-showcase-v11/environments/*/import.yaml` — the 6 generated import.yaml files
- `nestjs-showcase-v11/{apidev,appdev,workerdev}/README.md` — the 3 codebase READMEs
- `nestjs-showcase-v11/{apidev,appdev,workerdev}/src/` — the scaffold sub-agents' output

Reference comparisons:

- `nestjs-showcase-v7/` — the gold-standard frontend baseline (v7 is the last pre-topic-system run; see `docs/implementation-v9-findings.md` for why)
- `nestjs-showcase-v10/` — the regression baseline that prompted v8.58.0

Analysis scripts:

- `/tmp/analyze_v9.py` — JSONL parser for Claude Code sessions (reusable for v11, v12, etc.). Run with `python3 /tmp/analyze_v9.py <SESSIONS_LOGS_dir>`.

---

## Commit + release template

Phase 1 + 2 can be one commit (threshold + template + worker loop are all small adjustments). Phase 3 should be its own commit because it's the structural work.

Commit 1 suggested message:

```
fix(recipe): v11 findings — floor tuning + APP_SECRET template + worker loop

Three targeted adjustments from v11 session analysis:

1. Raise predecessor-floor from 2 to 3 (matches v7 baseline). v11 apidev
   cloned 4 of 6 predecessor gotchas and still cleared the old floor.
2. Auto-emit APP_SECRET rationale comment in the import.yaml template
   when NeedsAppSecret=true. Retires the v9/v10/v11 regression where the
   agent kept forgetting to comment on the project-level secret.
3. Run predecessor-floor check on separate-codebase worker READMEs too.
   v11 workerdev cloned "No .env files on Zerops" verbatim and nothing
   caught it because the worker wasn't in the checked loop.
```

Commit 2 suggested message:

```
feat(recipe): scaffold-subagent brief contract + enforced feature-subagent sub-step

Structural fix for the v11 failure mode where scaffolding sub-agents,
with no scope contract, overshot into feature implementations, after
which the main agent skipped step 4b's "MANDATORY" feature sub-agent
dispatch because the code "looked complete." Result: v11 shipped a
scaffold-quality frontend (526 LOC) instead of a feature-quality
dashboard (v7: ~1220 LOC, 2x information density).

Two parts:

1. New `scaffold-subagent-brief` block in recipe.md + topic registry +
   section catalog. Gives the main agent a verbatim brief template
   telling scaffold sub-agents to write skeletons only, not features.
   Explicit list of what to write and what to defer to the feature
   sub-agent at step 4b.

2. New `feature-subagent-dispatched` sub-step under deploy for
   showcase-tier plans. Validator requires a non-trivial attestation
   (≥40 chars describing what the feature sub-agent did). Deploy step
   cannot be marked complete without it. Removes the main agent's
   ability to skip step 4b based on "scaffold code looks complete."

Updated step 4b prose in recipe.md to reflect the enforcement and to
cite the v11 failure as a negative example. See
docs/implementation-v11-findings.md for the full v7-vs-v11 component
comparison that motivated this fix.
```

After both commits, run `make release` to tag and publish. Version bump: v8.59.0 for commit 1, v8.60.0 for commit 2 (or bundle as v8.59.0 if shipping together — user's call).

---

## Known scope gaps — deliberately NOT in v12

These came up during v11 analysis but are either too speculative, too large, or outside the recipe-content concern:

- **Semantic schema contract check between API and worker codebases.** v11's close-step code review caught 2 CRITICAL issues (worker entity mismatch, phantom columns). A static check diffing entity definitions would retire that class, but it's ~100 lines of new Go for a pattern that hasn't shown up before v11. Defer until a second recipe hits it.
- **Frontend-API response-shape contract check.** v11 had 3 WRONG findings about frontend components expecting shapes the API didn't return. A real fix is contract testing at close step — too big for v12. Alternative: teach the feature sub-agent brief to share a `types.ts` file between codebases (cheaper, partial coverage). Consider if issue #1 (feature sub-agent enforcement) doesn't already fix this naturally — a feature sub-agent working against LIVE services will hit the shape mismatch immediately and fix it.
- **Comment ratio floor raise from 30% → 35%.** v11 had 3 envs at 28/29/30% first attempt, triggered the standard 1-attempt retry. Not a regression, not blocking, no user-visible impact. Defer until it becomes a pattern.
- **ToolSearch / deferred tools cache.** v11 made 7 ToolSearch calls for built-in tools like TodoWrite. Bandwidth overhead, not blocking. Out of scope.

---

## Contact for questions

If any fix lands differently than described, or you find the evidence in `nestjs-showcase-v11/` contradicts this guide, trust the session logs and re-derive. This guide is a snapshot from 2026-04-13 based on:

- Session log grep extraction showing the skip decision at 23:09:56 and 23:10:05
- Direct file comparison of v7 JobsSection.svelte vs v11 JobsDemo.svelte
- v11 main-session.jsonl check-result extraction confirming both v8.58.0 checks fired and passed
- v11 TIMELINE.md step-by-step reconstruction

The goal is v12 matching v7's content tier while keeping v8.58.0's engine discipline. The engine works. The orchestration needs one structural tightening.
