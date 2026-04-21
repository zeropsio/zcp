# Implementation v8.90 — Workflow State Coherence

**Status**: plan, not yet implemented.
**Branch target**: main, post-rollback (commits after `dd0642a`).
**Estimated scope**: ~350 LOC production + ~450 LOC tests; 1 new MCP error code; no new checkers, no new gates, no content machinery.
**Calibration bar**: v26 ships with 0 subagent workflow-start rejections, 0 out-of-order substep attestations, substep-scoped briefs delivered in-phase.

This document is **standalone for a fresh Opus implementor**. It assumes you have never read the v25 analysis or the surrounding conversation. Read end-to-end before writing any code.

---

## Table of contents

1. [Why this exists](#1-why-this-exists)
2. [The two defects](#2-the-two-defects)
3. [Design principles that constrain the fix](#3-design-principles-that-constrain-the-fix)
4. [Fix A — subagent `zerops_workflow` rejection at source](#fix-a--subagent-zerops_workflow-rejection-at-source)
5. [Fix B — substep-scoped briefs delivered via substep-complete only](#fix-b--substep-scoped-briefs-delivered-via-substep-complete-only)
6. [Fix C — subagent tool-use policy in every subagent brief](#fix-c--subagent-tool-use-policy-in-every-subagent-brief)
7. [Fix D — substep-discipline teaching in deploy step-entry](#fix-d--substep-discipline-teaching-in-deploy-step-entry)
8. [Test plan](#7-test-plan)
9. [Validation against prior session logs](#8-validation-against-prior-session-logs)
10. [What this does NOT add](#9-what-this-does-not-add)
11. [Rollout checklist](#10-rollout-checklist)
12. [Appendix — relevant file/line references](#appendix--relevant-fileline-references)

---

## 1. Why this exists

ZCP is a Zerops Control Plane Go binary that hosts MCP tools an LLM agent uses to orchestrate managed services. The agent runs *inside* Claude Code. When the agent creates a recipe (per `internal/content/workflows/recipe.md`), it runs a 6-step orchestrated session: research → provision → generate → deploy → finalize → close.

The `deploy` step has 12 sub-steps (showcase tier). Each sub-step has scoped guidance the agent is supposed to receive by calling `zerops_workflow action=complete step=deploy substep=X`. The response of that call carries the guidance for substep X+1 in its `current.detailedGuide` field.

Sub-steps whose briefs matter most:

- **`init-commands` → subagent-brief** — 14 KB of guidance for dispatching the feature sub-agent (contract rules, installed-package verification, credential formats, cross-codebase contract discipline).
- **`feature-sweep-stage` → readme-fragments** — 17 KB of guidance for writing the published READMEs (fragment marker format, gotcha-distinct-from-guide rule, authenticity rules).

In v25 (the first post-rollback run, 2026-04-17), the main agent completed all deploy work in 40 minutes (20:28 → 21:06:55) **without calling `complete substep=X` once**. Then it backfilled 13 substep completions in 2 minutes at the end. Every substep brief therefore arrived *after* the phase it was meant to govern.

Symptoms caused by the bypass:

1. **Feature sub-agent dispatched without its substep brief** — it improvised by calling `zerops_workflow action=start workflow=develop`, which the server rejected with `PREREQUISITE_MISSING: Run bootstrap first` (a **misleading** error — the real state is "you're inside a recipe subagent, do not start another workflow"). The subagent rationalised and proceeded, but had to re-derive operational rules from the high-level recipe context instead of the 14KB targeted brief.
2. **README writer sub-agent dispatched without its substep brief** — shipped 6 files that failed 6 content checks at the deploy-complete gate. A fix-subagent round ran for ~6 minutes to correct the content. This 6 minutes is **directly attributable to the substep-bypass**: the `readme-fragments` brief encodes exactly the rules the writer violated.
3. **Main-agent substep attestation arrived out-of-order** on first try (`expected sub-step "deploy-dev", got "subagent"`). Recovery cost: ~14 seconds + one `status` call. Gate behaved correctly but the agent's in-memory model of workflow state was clearly not maintained.
4. **Scaffold sub-agent** (first tool call of its life) called `zerops_workflow action=start workflow=develop` and got the same misleading error. Recovery worked because the subagent was sensible; a less sensible subagent following the suggestion literally would have attempted `action=start workflow=bootstrap` — corrupting the recipe session.

The bypass was possible because the **`subagent-brief`, `where-commands-run`, and `readme-fragments` topics are all marked `Eager: true`** in `internal/workflow/recipe_topic_registry.go`. Eager topics are injected into the step-entry guide (the 37 KB response the agent receives when `generate` completes). The agent therefore has the subagent-brief **before** it has completed any deploy sub-step — so there's no behavioural pressure to attest substeps in order.

Fixing eager injection closes the bypass at its source. The remaining fixes reinforce it at the brief layer and add a precise server error for the subagent workflow-start case.

---

## 2. The two defects

### Defect 1 — subagent-side: wrong mental model at spawn, misleading server error when it misfires

**Evidence**:

```
# v25 main-session.jsonl, 20:20:46 — appdev scaffold subagent, first tool call
TOOL_USE mcp__zerops__zerops_workflow
  input: {"action":"start","workflow":"develop","intent":"Scaffold Svelte + Vite SPA..."}
TOOL_RESULT is_error=true:
  {"code":"PREREQUISITE_MISSING","error":"No bootstrapped services found",
   "suggestion":"Run bootstrap first: action=\"start\" workflow=\"bootstrap\""}
```

```
# v25 main-session.jsonl, 20:34:30 — feature subagent
TOOL_USE mcp__zerops__zerops_workflow
  input: {}
TOOL_RESULT is_error=true:
  {"code":"INVALID_PARAMETER","error":"No workflow specified","suggestion":"..."}

# Retry 7s later:
  input: {"action":"start","workflow":"develop","intent":"Implement 5 features..."}
TOOL_RESULT is_error=true: (same PREREQUISITE_MISSING as above)
```

**Root cause**: `handleStart` in `internal/tools/workflow.go:188` dispatches to workflow-specific handlers based on `input.Workflow` without first checking whether a session is already active. When `workflow=develop` is called inside a running recipe session, the develop handler runs its bootstrap-prereq check and returns the wrong diagnosis.

**Blast radius** (this run: bounded; general case: latent corruption):

- If a subagent follows the suggestion literally and calls `action=start workflow=bootstrap`, it enters `handleStart` → `engine.BootstrapStart(projectID, intent)` at workflow.go:214. Whether that succeeds or errors depends on `engine.BootstrapStart`'s handling of an active-recipe state. If it errors with `ErrWorkflowActive` (line 217) the blast is contained. If not, the recipe session state is clobbered by a bootstrap start.
- A future subagent type with a more literal instruction-following profile might follow the suggestion.

### Defect 2 — main-agent-side: substep ordering maintained only by retroactive attestation

**Evidence**:

```
# v25 main-session.jsonl substep-complete timestamps:
20:28:06  complete step=generate               — deploy step-entry guide delivered (37 KB, includes subagent-brief + readme-fragments eagerly)
20:34:26  Agent: Implement all 5 showcase features end-to-end  — feature subagent dispatched
21:00:06  Agent: Write README.md + CLAUDE.md for 3 codebases   — README writer dispatched
21:06:55  complete step=deploy substep=subagent               — FIRST substep-complete call, 38 minutes after deploy step started
          → INVALID_PARAMETER: expected "deploy-dev", got "subagent"
21:06:57  action=status
21:07:09  complete substep=deploy-dev       (canonical order starts here)
21:07:18  complete substep=start-processes
21:07:23  complete substep=verify-dev
21:07:30  complete substep=init-commands     ← 14 KB subagent-brief returned HERE, 33 min after the feature subagent already finished
21:07:49  complete substep=subagent
21:07:56  complete substep=snapshot-dev
21:07:59  complete substep=feature-sweep-dev
21:08:25  complete substep=browser-walk
21:08:32  complete substep=cross-deploy
21:08:42  complete substep=verify-stage
21:08:45  complete substep=feature-sweep-stage  ← 17 KB readme-fragments returned HERE, after README writer shipped
21:08:59  complete substep=readmes
21:09:06  complete step=deploy              ← 6 content check failures on the README writer's output
```

**Root cause**: `subagent-brief` and `readme-fragments` are marked `Eager: true` in `internal/workflow/recipe_topic_registry.go:229` and `:293` — both get injected into the deploy step-entry guide. The agent therefore has the entire deploy roadmap (including both subagent briefs) in context **before** completing any substep. Substeps become retroactive checklist items instead of guidance-delivery triggers.

**Blast radius** (this run: ~6 min README fix-cycle; general case: compaction fragility):

- The 6-min README fix-cycle at 21:10:27 → 21:16:38 is directly caused by the writer having no `readme-fragments` brief at write-time.
- In longer runs where context compaction fires mid-deploy, the step-entry guide gets summarised. The substep system was the reinjection mechanism. With eager bypass, there is no reinjection — compaction would leave the agent blind from that point on.
- The agent's in-memory model of "where am I in the substeps" is clearly not maintained between work and attestation. Any future failure that needs to be scoped to a specific substep (e.g. a retry that targets only `snapshot-dev`) relies on attestation ordering; the bypass makes that ordering notional.

---

## 3. Design principles that constrain the fix

1. **Fix at the source**. The rollback that produced the current substrate explicitly deleted ~6000 LOC of content-check machinery. The lesson was: post-hoc verification can't save bad-upstream output. This plan follows the same discipline — every fix is upstream (at dispatch, at brief delivery) not downstream (after-the-fact checker).

2. **No new content checks**. If this plan ever feels like it's growing a content-quality check, that's the wrong direction. The surviving content checks (`gotcha_distinct_from_guide`, `comment_ratio`, `comment_specificity`, `cross_readme_gotcha_uniqueness`) are v8.67-era and were intentionally kept by the rollback. Do not add to the list.

3. **No new state-gate tools**. The existing `CompleteStep`, `CompleteSubStep`, `ExpectedSubStep` gates already exist and behave correctly (they rejected the out-of-order attestation constructively). Do not add parallel gating machinery.

4. **The server's error messages are load-bearing**. An agent reading `Run bootstrap first` will do exactly that. Precision of error messages is part of the API contract.

5. **Remove eager injection from two topics, keep it on `where-commands-run`**. `where-commands-run` is about the `zerops_dev_server` tool and the SSH-vs-zcp boundary — needed from the very first substep (`deploy-dev`) onwards. Its eager injection has proven load-bearing across v17–v25. Keep it. The other two topics are delegation briefs — they should only land when the agent is actually about to delegate.

6. **Changes must be visible in session logs**. Every behaviour change this plan introduces should be diff-able by reading a v26 session log side-by-side with v25. The eager removal + substep-ordered guidance delivery will be immediately visible in the `complete substep=X` response sizes.

---

## Fix A — subagent `zerops_workflow` rejection at source

### Problem

`handleStart` at `internal/tools/workflow.go:188` does not check whether a session is already active before dispatching to the workflow-specific handler. When a subagent inside a running recipe calls `action=start workflow=develop`, the develop handler returns `PREREQUISITE_MISSING: Run bootstrap first` — a misleading suggestion.

### Solution

Reject `action=start` at the top of `handleStart` when any workflow is already active, with a precise error message. Use the already-existing `detectActiveWorkflow(engine)` helper (workflow.go:248).

### New error code

Add to `internal/platform/errors.go`:

```go
const (
    // ... existing codes ...
    ErrSubagentMisuse = "SUBAGENT_MISUSE"  // caller misuses a tool meant for the main agent
)
```

### Implementation

In `internal/tools/workflow.go`, modify `handleStart` (line 188):

```go
func handleStart(ctx context.Context, projectID string, engine *workflow.Engine, client platform.Client, cache *ops.StackTypeCache, input WorkflowInput, mounter ops.Mounter, selfHostname string) (*mcp.CallToolResult, any, error) {
    // NEW: reject action=start when a session is already active. This closes
    // the subagent-misuse path: a subagent inside a running recipe calling
    // action=start workflow=develop should not be told "Run bootstrap first"
    // — the recipe session is the active workflow, the subagent should not
    // start a second workflow.
    //
    // Immediate workflows (cicd, export) are stateless — they don't create a
    // session, so the active-session check doesn't apply to them.
    if !workflow.IsImmediateWorkflow(input.Workflow) {
        if active := detectActiveWorkflow(engine); active != "" && active != input.Workflow {
            return convertError(platform.NewPlatformError(
                platform.ErrSubagentMisuse,
                fmt.Sprintf(
                    "A %q workflow session is already active — cannot start a %q workflow inside it.",
                    active, input.Workflow,
                ),
                "If you are a sub-agent spawned by the main agent inside a recipe session, " +
                    "do NOT call zerops_workflow. The main agent holds workflow state. " +
                    "Perform your scoped task using the tools listed in your dispatch brief and return.",
            )), nil, nil
        }
    }

    // ... existing handleStart body continues unchanged ...
}
```

### Why this is safer than alternatives

- **Alternative 1: detect the caller is a subagent.** Not viable — MCP tools flow through one STDIO channel; the server cannot distinguish main from subagent. We use "is a session active" as the proxy for "caller is probably a subagent" because main agents don't start nested workflows.
- **Alternative 2: reject any workflow-start when a session is active.** Too aggressive — `action=start workflow=recipe` may be a legitimate re-issue after `reset`. The check above permits `input.Workflow == active` (no-op restart protection is elsewhere).
- **The `active != input.Workflow` guard** allows an active-recipe agent to re-call `workflow=recipe` (returns the existing session's current state via `handleRecipeStart`'s idempotency path).

### Test

`internal/tools/workflow_start_test.go` — new file:

```go
func TestHandleStart_RejectsStartInsideActiveRecipe(t *testing.T) {
    tests := []struct {
        name           string
        sessionActive  string // "recipe", "bootstrap", or "" for fresh
        requestWorkflow string
        wantCode       string
        wantErrorContains string
    }{
        {"subagent calls develop inside recipe",
         "recipe", "develop", "SUBAGENT_MISUSE", "recipe"},
        {"subagent calls bootstrap inside recipe",
         "recipe", "bootstrap", "SUBAGENT_MISUSE", "recipe"},
        {"fresh session allows bootstrap start",
         "", "bootstrap", "", ""},
        {"fresh session allows recipe start",
         "", "recipe", "", ""},
        {"active recipe re-starts recipe is not rejected here",
         // This case falls through to handleRecipeStart which has its own
         // idempotency handling — the TOP-LEVEL check does not reject.
         "recipe", "recipe", "", ""},
        {"immediate workflow cicd is exempt",
         "recipe", "cicd", "", ""},
    }
    // ...
}
```

Also update `internal/tools/workflow_develop.go` if needed — the existing `PREREQUISITE_MISSING` check at line 62 should still fire for the case where develop is called on a fresh session with no bootstrapped services. The new check in `handleStart` only fires when there's an OTHER workflow active; bootstrap-prereq for develop remains the develop-specific safeguard.

### Files touched by Fix A

- `internal/platform/errors.go` — add `ErrSubagentMisuse` constant (~1 line)
- `internal/tools/workflow.go` — add active-session check in `handleStart` (~14 lines)
- `internal/tools/workflow_start_test.go` — new test file (~60 lines)

---

## Fix B — substep-scoped briefs delivered via substep-complete only

### Problem

`subagent-brief` and `readme-fragments` topics are marked `Eager: true` in `internal/workflow/recipe_topic_registry.go`. Eager topics get inlined into the step-entry guide at `generate` complete (via `InjectEagerTopics` in `recipe_guidance.go:141`). The agent therefore has both delegation briefs in hand before completing any deploy substep. Substeps stop being delivery mechanisms.

### Solution

Remove `Eager: true` from both topics. Replace the in-line reference in `deploy-skeleton` with a pointer that tells the agent: *the authoritative brief for substep X is the `complete substep=X-1` response; do not dispatch until you have completed the preceding substep.*

Keep `where-commands-run` eager. It encodes the SSH-vs-zcp boundary and the `zerops_dev_server` tool discipline — needed from the FIRST substep (`deploy-dev`) onwards. Its eager injection has been load-bearing across v17–v25 (no port-collision incidents, no 120s SSH-backgrounding hangs). Do not remove.

### Implementation — code changes

File: `internal/workflow/recipe_topic_registry.go`

**Edit 1** — `subagent-brief` (around line 215):

```go
// BEFORE
{
    ID: "subagent-brief", Step: RecipeStepDeploy,
    Description: "Feature sub-agent dispatch and brief",
    Predicate:   isShowcase,
    BlockNames:  []string{"dev-deploy-subagent-brief"},
    // Eager: v13 fetched this topic exactly once at post-generate, then
    // the main agent treated the platform rules inside it [...]
    Eager: true,
},

// AFTER
{
    ID: "subagent-brief", Step: RecipeStepDeploy,
    Description: "Feature sub-agent dispatch and brief",
    Predicate:   isShowcase,
    BlockNames:  []string{"dev-deploy-subagent-brief"},
    // NOT eager — v8.90. The brief is delivered via substep-complete for
    // substep=init-commands, which is the substep immediately preceding
    // substep=subagent (the dispatch substep). Injecting eagerly at step-
    // entry let the agent keep the brief in context across 33+ minutes of
    // other work (v25), dispatching the feature subagent without re-reading
    // the current brief and without signalling to the workflow that the
    // dispatch was about to happen. Substep-scoped delivery re-binds the
    // brief to the phase where the delegation actually occurs.
    //
    // The v13 regression that originally motivated eager injection (agent
    // fetched the topic at post-generate then forgot to act on its rules)
    // is now addressed by substep-ordered delivery: the brief arrives in
    // the `complete substep=init-commands` response immediately before
    // dispatch, not 30+ minutes earlier.
},
```

**Edit 2** — `readme-fragments` (around line 283):

```go
// BEFORE
{
    ID: "readme-fragments", Step: RecipeStepDeploy,
    Description: "Per-codebase README structure with extract fragments (post-verify `readmes` sub-step)",
    BlockNames:  []string{"readme-with-fragments"},
    // Eager: the fragment marker format is enforced byte-literally by the
    // deploy-step checker. [...]
    Eager: true,
},

// AFTER
{
    ID: "readme-fragments", Step: RecipeStepDeploy,
    Description: "Per-codebase README structure with extract fragments (post-verify `readmes` sub-step)",
    BlockNames:  []string{"readme-with-fragments"},
    // NOT eager — v8.90. The brief is delivered via substep-complete for
    // substep=feature-sweep-stage, which is the substep immediately
    // preceding substep=readmes. v25 evidence: the README writer was
    // dispatched with step-entry content 33 minutes old; 6 content checks
    // failed; a fix-subagent round cost ~6 minutes. Substep-scoped
    // delivery lands the brief in the writer-dispatch phase where it
    // governs the writer's output.
    //
    // The v14 regression that originally motivated eager injection (agent
    // invented fragment markers from imagination because it hadn't fetched
    // the topic) is now addressed by substep-ordered delivery: the brief
    // arrives in the `complete substep=feature-sweep-stage` response
    // immediately before the `readmes` substep, not 40+ minutes earlier.
},
```

**Edit 3** — `where-commands-run` (around line 232): **no change.** Keep `Eager: true` with its existing comment. Optionally append a one-line note:

```go
    // v8.90: subagent-brief and readme-fragments de-eagered; where-commands-run
    // stays eager because the SSH/zcp boundary applies from substep=deploy-dev
    // onwards and the zerops_dev_server tool discipline needs to be in context
    // at every substep, not just the one that delivers the topic.
    Eager: true,
```

### Implementation — content changes

File: `internal/content/workflows/recipe.md`

**Edit 1** — `<section name="deploy-skeleton">` (line 2495): update the execution-order list so steps that reference de-eagered topics make the substep-delivery contract explicit. Replace lines 2504–2518 with:

```markdown
### Execution order
1. Deploy apidev + workerdev + appdev dev containers [topic: deploy-flow]
   - API-first: deploy apidev FIRST [topic: deploy-api-first]
   - `complete step=deploy substep=deploy-dev` when all three dev containers are ACTIVE
2. Start ALL dev processes
   - Asset dev server [topic: deploy-asset-dev-server]
   - Worker process [topic: deploy-worker-process]
   - `complete step=deploy substep=start-processes` when every `zerops_dev_server` start returned OK
3. Enable subdomain + verify [topic: deploy-target-verification]
   - `complete step=deploy substep=verify-dev`
4. Run init commands (migrations + seed)
   - `complete step=deploy substep=init-commands` — **the response to THIS call delivers the feature-subagent-brief**. Do NOT dispatch the feature sub-agent until you have received that response.
5. Dispatch feature sub-agent (showcase only)
   - Brief content arrives in the `complete substep=init-commands` response. Use the `current.detailedGuide` verbatim as the core of the Agent dispatch prompt.
   - `complete step=deploy substep=subagent` when the sub-agent returns
6. Snapshot dev (showcase) — re-deploy dev to persist feature-sub-agent output into the deployed artifact.
   - `complete step=deploy substep=snapshot-dev`
7. Feature sweep dev [topic: feature-sweep-dev]
   - `complete step=deploy substep=feature-sweep-dev`
8. Browser verification (showcase) [topic: browser-walk]
   - `complete step=deploy substep=browser-walk`
9. Cross-deploy to stage [topic: stage-deploy]
   - `complete step=deploy substep=cross-deploy`
10. Verify stage [topic: deploy-target-verification]
    - `complete step=deploy substep=verify-stage`
11. Feature sweep stage [topic: feature-sweep-stage]
    - `complete step=deploy substep=feature-sweep-stage` — **the response to THIS call delivers the readme-with-fragments brief**. Do NOT dispatch the README writer sub-agent until you have received that response.
12. Write per-codebase READMEs (narrate gotchas from the debug rounds you just lived through)
    - Brief content arrives in the `complete substep=feature-sweep-stage` response. Use the `current.detailedGuide` verbatim in the writer dispatch prompt.
    - `complete step=deploy substep=readmes` when the writer returns
13. Handle failures [topic: deploy-failures]

### Substep-complete discipline (v8.90)

Each `complete step=deploy substep=X` call returns the next substep's scoped brief in its `current.detailedGuide` field. Call them **in order, as you work** — do `complete substep=deploy-dev` BEFORE calling `zerops_dev_server`; do `complete substep=init-commands` BEFORE dispatching the feature sub-agent. Do NOT batch substep completions at the end of the step.

The `substep=subagent` brief (14 KB) and the `substep=readmes` brief (17 KB) are **delivery-on-demand**: they are NOT inlined in this step-entry guide. If you dispatch a sub-agent without first completing the preceding substep, the sub-agent will run on stale or absent brief content, and content checks will flag the output at the full-step gate.

### Fetch guidance
Call `zerops_guidance topic="{id}"` on-demand for any topic you need to re-consult.
```

### Why pointers, not inlined content

A pointer ("the brief arrives via substep-complete") is honest — it tells the agent the substep call is load-bearing. An inlined copy with a warning ("prefer the substep-delivered version") is ignored; the in-context copy wins.

### Files touched by Fix B

- `internal/workflow/recipe_topic_registry.go` — remove two `Eager: true` flags + update comments (~20 lines diff)
- `internal/content/workflows/recipe.md` — rewrite `deploy-skeleton` section (~50 lines diff)
- `internal/workflow/recipe_topic_registry_test.go` — add assertions that subagent-brief and readme-fragments are NOT in the eager set; where-commands-run IS (~30 lines test)

---

## Fix C — subagent tool-use policy in every subagent brief

### Problem

The scaffold subagent brief (`recipe.md:782` block `scaffold-subagent-brief`) and the feature subagent brief (`recipe.md:1359` block `dev-deploy-subagent-brief`) do not explicitly forbid `zerops_workflow`, `zerops_import`, `zerops_env`, `zerops_deploy`. Subagents therefore try them out of curiosity, which is how v25's bootstrap confusion arose.

### Solution

Add a `⚠ TOOL-USE POLICY` block to the first 1 KB of every subagent brief, listing both permitted and forbidden tools explicitly. The block is terse, framework-agnostic, and belt-and-braces with Fix A's server-side rejection.

### Content to add

Insert this block as the first fenced content after the brief's opening sentence, in all three subagent briefs (scaffold, feature, readme-writer, code-review):

```markdown
**⚠ TOOL-USE POLICY — read before your first tool call.**

You are a sub-agent spawned by the main agent inside a Zerops recipe session. The main agent holds workflow state. Your job is narrow, scoped to this dispatch brief.

**Permitted tools:**
- File ops: `Read`, `Edit`, `Write`, `Grep`, `Glob` against the SSHFS-mounted paths listed in this brief
- `Bash` — but ONLY via `ssh <hostname> "<command>"` patterns as taught in the "where commands run" block below (NEVER `cd /var/www/<host> && ...`)
- `mcp__zerops__zerops_dev_server` — start/stop/status/logs/restart for dev processes
- `mcp__zerops__zerops_knowledge` — on-demand platform knowledge queries
- `mcp__zerops__zerops_logs` — read container logs
- `mcp__zerops__zerops_discover` — introspect service shape
- `mcp__zerops__zerops_browser` — (feature sub-agent + close-step reviewer only, if the brief explicitly authorises it)

**Forbidden tools — calling any of these is a sub-agent-misuse bug:**
- `mcp__zerops__zerops_workflow` — workflow state is main-agent-only. Never call `action=start`, `action=complete`, `action=status`, `action=reset`, `action=iterate`, `action=generate-finalize`.
- `mcp__zerops__zerops_import` — service provisioning is main-agent-only.
- `mcp__zerops__zerops_env` — env-var management is main-agent-only.
- `mcp__zerops__zerops_deploy` — deploy orchestration is main-agent-only.
- `mcp__zerops__zerops_subdomain` — subdomain management is main-agent-only.
- `mcp__zerops__zerops_mount` — mount lifecycle is main-agent-only.
- `mcp__zerops__zerops_verify` — step verification is main-agent-only.

If you feel a need to call a forbidden tool, the brief is incomplete — stop, report the gap in your return message, and let the main agent decide.

**If the server rejects a call with `SUBAGENT_MISUSE`**: you are the cause. Do not retry with a different workflow name; do not call `bootstrap`. Return to your scoped task.
```

### Placement

Each subagent brief is already fairly long. The tool-use policy block goes **before** the first imperative ("You are scaffolding..." / "You are implementing..." / "You are reviewing..."). Putting it at the very top ensures the agent reads it before making any tool-call decision.

### Files touched by Fix C

- `internal/content/workflows/recipe.md`:
  - `scaffold-subagent-brief` block (line 782) — insert tool-use policy at top (~40 lines)
  - `dev-deploy-subagent-brief` block (line 1359) — insert tool-use policy at top (~40 lines)
  - `readme-with-fragments` block (line 1825) — if this block is used as a brief preamble, insert policy (~40 lines)
  - `code-review-agent` block (find by grep) — insert policy (~40 lines)

---

## Fix D — substep-discipline teaching in deploy step-entry

### Problem

Even with Fixes A, B, and C in place, the deploy step-entry guide currently frames substeps as a checklist. An agent doing 40 minutes of deploy work will still naturally batch attestations at the end unless the step-entry guide explicitly teaches otherwise.

### Solution

Augment the `deploy-skeleton` section (already rewritten in Fix B) with a **Substep-Complete Discipline** subsection at the top, plus a visible reminder at the bottom of each substep-relevant topic block (deploy-flow, feature-sweep-dev, etc.) that the substep-complete call is the gate.

### Content to add

This is partially covered by the Fix B rewrite. The additional piece is a **prominent header-level note** at the very top of the deploy step-entry (before the Constraints block).

In `internal/content/workflows/recipe.md`, `<section name="deploy-skeleton">`, insert immediately after the `## Deploy — Build, Start, Verify & Narrate` line:

```markdown
## Deploy — Build, Start, Verify & Narrate

### ⚠ Substep-Complete is load-bearing (v8.90)

The deploy step has 12 sub-steps (showcase tier). **Each `zerops_workflow action=complete step=deploy substep=X` call returns the next substep's scoped guide in its `current.detailedGuide` field.** Sub-step briefs are NOT inlined in this step-entry guide (except where noted). You receive them only by completing substeps in order.

- The `init-commands` → `subagent` boundary delivers the feature-sub-agent brief (~14 KB).
- The `feature-sweep-stage` → `readmes` boundary delivers the README-writer brief (~17 KB).

**Anti-pattern (v25 failure mode — do not repeat)**: do 40 minutes of deploy work across every substep silently, then backfill all substep completions in a 2-minute burst at the end. The substep briefs you'd receive are useless once the work is done. Two consequences of the anti-pattern:
1. The feature sub-agent is dispatched without its substep brief and has to improvise.
2. The README writer is dispatched without its substep brief and ships content that fails the full-step content checks, forcing a ~6-minute fix-subagent round.

**Correct pattern**: as you complete each substep's work, call `complete substep=X` before starting the next substep. The response carries substep X+1's brief.

### Constraints
...
```

### Files touched by Fix D

- `internal/content/workflows/recipe.md` — insert the discipline note at the top of `deploy-skeleton` (~25 lines)

---

## 7. Test plan

Unit tests first (RED), then implementation (GREEN), per project TDD convention.

### Fix A tests

File: `internal/tools/workflow_start_test.go` (new file)

1. `TestHandleStart_SubagentMisuse_RecipeActive_DevelopStartRejected` — session state shows active recipe; call `action=start workflow=develop`; expect `SUBAGENT_MISUSE` error code and error text containing both workflow names.
2. `TestHandleStart_SubagentMisuse_RecipeActive_BootstrapStartRejected` — same shape for `workflow=bootstrap`.
3. `TestHandleStart_SubagentMisuse_BootstrapActive_RecipeStartRejected` — inverse: bootstrap active, reject recipe-start attempt.
4. `TestHandleStart_ImmediateWorkflow_NotRejected` — `workflow=cicd` must NOT be rejected even with active session (immediate workflows are stateless).
5. `TestHandleStart_FreshSession_AnyStartAllowed` — no active session; all workflow starts succeed (or hit their own prereq checks, not `SUBAGENT_MISUSE`).
6. `TestHandleStart_SameWorkflowReStart_FallsThroughToSpecificHandler` — recipe active, call `workflow=recipe`; the top-level check allows it and `handleRecipeStart` handles idempotency.
7. `TestSubagentMisuseError_MessageShape` — error `suggestion` contains "do NOT call zerops_workflow" and "scoped task".

### Fix B tests

File: `internal/workflow/recipe_topic_registry_test.go` (edit existing)

1. `TestRecipeDeployTopics_EagerSet_v8_90` — exactly one deploy topic is eager: `where-commands-run`. Explicitly assert `subagent-brief.Eager == false` and `readme-fragments.Eager == false`.
2. `TestRecipeDeployTopics_SubagentBriefDeliveredBySubstep` — `subStepToTopic(RecipeStepDeploy, SubStepInitCommands, showcasePlan) != ""` (existing mapping) AND the mapped topic is `deploy-flow` (where brief CURRENTLY goes). Wait — read the existing mapping first. The `SubStepInitCommands → deploy-flow` mapping means completing init-commands returns the deploy-flow topic, not the subagent-brief. **The mapping needs to change too.**

   **Additional edit in `recipe_guidance.go:542`** — remap `SubStepInitCommands` to return `subagent-brief` for showcase plans:

   ```go
   case RecipeStepDeploy:
       switch subStep {
       case SubStepDeployDev, SubStepStartProcs:
           return topicDeployFlow
       case SubStepVerifyDev:
           return "deploy-target-verification"
       case SubStepInitCommands:
           // v8.90: deliver the subagent-brief when init-commands completes,
           // because the next substep IS subagent. For non-showcase plans
           // there is no subagent substep, so fall back to deploy-flow.
           if isShowcase(plan) {
               return "subagent-brief"
           }
           return topicDeployFlow
       case SubStepSubagent:
           // The subagent substep itself returns the snapshot-dev topic
           // (or deploy-flow since they share flow). The brief was
           // delivered on init-commands's complete.
           return topicDeployFlow
       // ... other substeps ...
       case SubStepFeatureSweepStage:
           // v8.90: deliver readme-fragments brief here; the next substep
           // is readmes, and the agent needs the fragment rules BEFORE
           // dispatching the README writer.
           return "readme-fragments"
       case SubStepReadmes:
           // readmes substep itself: thin next-step guide, the brief was
           // delivered on feature-sweep-stage's complete.
           return ""  // or a short "call complete step=deploy" prompt
       }
   ```

3. `TestSubStepToTopic_InitCommands_ShowcaseReturnsSubagentBrief` — table test for the mapping change.
4. `TestSubStepToTopic_FeatureSweepStage_ReturnsReadmeFragments` — table test.
5. `TestSubStepGuide_InitCommandsResponse_ContainsSubagentBrief` — integration: complete `init-commands`, read the response, assert it contains the byte-literal string from `dev-deploy-subagent-brief` block.
6. `TestSubStepGuide_FeatureSweepStageResponse_ContainsReadmeFragments` — same shape for the other brief.
7. `TestInjectEagerTopics_DoesNotIncludeSubagentBrief` — the step-entry guide composition no longer contains the `dev-deploy-subagent-brief` block.
8. `TestInjectEagerTopics_DoesNotIncludeReadmeFragments` — ditto for `readme-with-fragments`.
9. `TestInjectEagerTopics_StillIncludesWhereCommandsRun` — the one eager topic that remains.

### Fix C tests

File: `internal/content/workflows/recipe_blocks_test.go` (likely already exists; find or create)

1. `TestScaffoldSubagentBrief_ContainsToolUsePolicy` — the block contains the substring `TOOL-USE POLICY` and the list of forbidden tools including `zerops_workflow`.
2. `TestDevDeploySubagentBrief_ContainsToolUsePolicy` — same.
3. `TestCodeReviewAgentBrief_ContainsToolUsePolicy` — same.
4. `TestReadmeWithFragmentsBrief_ContainsToolUsePolicy` — same (if this block is used as a brief).
5. `TestToolUsePolicy_ExplicitForbiddenTools` — a dedicated constant lists the forbidden tools; every subagent-brief block contains every item from the constant.

### Fix D tests

1. `TestDeploySkeleton_ContainsSubstepDisciplineNote` — the rendered step-entry for deploy contains the substring `Substep-Complete is load-bearing` and names the two load-bearing brief deliveries.
2. `TestDeploySkeleton_MentionsAntiPattern` — contains the substring `backfill all substep completions` or equivalent.

### Integration test — full substep-complete sequence

File: `internal/workflow/recipe_substep_briefs_integration_test.go` (new)

Single flow: simulate a showcase recipe from `start` through every deploy substep-complete, capture every response's `current.detailedGuide` size, assert:

1. Step-entry guide (at `complete generate`) is < 30 KB (was ~37 KB with 2 eager topics gone).
2. `complete substep=init-commands` response is > 10 KB and contains feature-subagent-brief's distinctive phrases (e.g., "Installed-package verification rule", "Contract-first rule").
3. `complete substep=feature-sweep-stage` response is > 12 KB and contains readme-fragments's distinctive phrases (e.g., `#ZEROPS_EXTRACT_START:knowledge-base`).
4. Step-entry guide does NOT contain those distinctive phrases (the eager copies are gone).
5. `where-commands-run` block IS in the step-entry guide (still eager).

This integration test is the tightest regression guard against someone re-eagering a topic in a future commit.

### Full test run

```bash
go test ./... -count=1 -race
make lint-local
```

All green before merge. No `-short` skip for the integration test.

---

## 8. Validation against prior session logs

Before committing, shadow-test the changes by running the new `subStepToTopic` mapping against prior session logs' `complete substep=` calls and verifying the would-have-been-delivered briefs match intent.

Specifically, replay v22, v23, v24, v25's deploy substep-complete sequences through the patched mapping function. For each run:

- v25: `complete substep=init-commands` at 21:07:30 — patched mapping returns `subagent-brief` (14 KB). Confirm no downstream consumer of the response relied on the old `deploy-flow` content.
- v25: `complete substep=feature-sweep-stage` at 21:08:45 — patched mapping returns `readme-fragments` (17 KB). Confirm the Fix B removal of eager injection doesn't leave the old payload orphaned somewhere.
- v22/v23/v24: same replay; their substep ordering was similar enough to serve as regression signals.

The replay lives in the integration test above — no separate tooling needed.

---

## 9. What this does NOT add

Enumerated so future contributors don't bolt these on:

- **No new content-quality checks.** The surviving v8.67-era checks (`gotcha_distinct_from_guide`, `comment_ratio`, `comment_specificity`, `cross_readme_gotcha_uniqueness`, `knowledge_base_authenticity`) stay as they are. Do not add new ones.
- **No dispatch-required gate.** v8.81's `content_fix_dispatch_required` gate was rolled back for good reason; do not resurrect it.
- **No substep-attestation content validators.** The existing attestation-non-empty and attestation-length checks stay; no semantic parsing of attestation text.
- **No tool-permission enforcement at the MCP layer beyond Fix A.** Fix A's check is session-state-based, not caller-identity-based. Do not attempt to detect "this is a subagent" from some MCP header — it's not a real signal.
- **No new subagent types.** Continue using `general-purpose` for all recipe sub-agents. The tool-use policy in the brief (Fix C) does the work.
- **No changes to the rolled-back machinery.** The rollback branch is the current main. This plan strictly post-dates it and does not reintroduce any v8.78–v8.86 code.

---

## 10. Rollout checklist

Execute in this order:

1. **[ ]** Branch from current main (post-`dd0642a`).
2. **[ ]** Add `ErrSubagentMisuse` to `internal/platform/errors.go`.
3. **[ ]** Write Fix A tests (RED).
4. **[ ]** Implement Fix A in `internal/tools/workflow.go`.
5. **[ ]** Confirm Fix A tests pass (GREEN).
6. **[ ]** Write Fix B tests — eager-set assertions + substep-brief-delivery integration test (RED).
7. **[ ]** Remove `Eager: true` from `subagent-brief` and `readme-fragments` in `recipe_topic_registry.go`.
8. **[ ]** Update `subStepToTopic` in `recipe_guidance.go` — `init-commands → subagent-brief` (showcase) and `feature-sweep-stage → readme-fragments`.
9. **[ ]** Rewrite `deploy-skeleton` section in `recipe.md` per Fix B's spec.
10. **[ ]** Confirm Fix B tests pass (GREEN).
11. **[ ]** Write Fix C tests (RED).
12. **[ ]** Insert tool-use policy block into every subagent brief in `recipe.md` (scaffold, feature, readme-writer, code-review).
13. **[ ]** Confirm Fix C tests pass (GREEN).
14. **[ ]** Write Fix D tests (RED).
15. **[ ]** Add substep-discipline note to `deploy-skeleton`.
16. **[ ]** Confirm Fix D tests pass (GREEN).
17. **[ ]** Full `go test ./... -count=1 -race` — green.
18. **[ ]** `make lint-local` — green.
19. **[ ]** Dry-run: parse v25's main-session.jsonl through the updated code path, verify no regression (the response sizes for init-commands and feature-sweep-stage substep-completes should jump to 14+ KB and 17+ KB respectively, step-entry should drop by ~30 KB).
20. **[ ]** Commit as three atomic commits: (a) Fix A (server + error), (b) Fix B (topic registry + guide + skeleton), (c) Fixes C+D (brief content).
21. **[ ]** Update `docs/recipe-version-log.md` with a new "v8.90" entry in the milestones table naming the three fixes.
22. **[ ]** Kick off v26 run. Expected observable differences from v25:
    - Zero `SUBAGENT_MISUSE`/`PREREQUISITE_MISSING` tool results from subagent-spawned `zerops_workflow` calls (subagents no longer call it — Fix C).
    - If a subagent does misfire, error now reads `SUBAGENT_MISUSE: A "recipe" workflow session is already active...` instead of `Run bootstrap first`.
    - Substep-complete attestations fire interleaved with work (Fix B + D teach this), not backfilled at step end.
    - `complete substep=init-commands` response ≥ 14 KB, contains subagent-brief distinctive strings.
    - `complete substep=feature-sweep-stage` response ≥ 17 KB, contains readme-fragments distinctive strings.
    - Fewer full-step README check failures (expected: 0–2 vs v25's 6).
    - Fix-subagent dispatches at deploy-complete: fewer (expected: 0–1 vs v25's 1).
    - Wall-clock change: neutral or -5 min. v8.90's value is in state coherence, not direct time savings.

---

## Appendix — relevant file/line references

Current working tree (commit `dd0642a`-based):

| File | Purpose | Key lines |
|---|---|---:|
| `internal/tools/workflow.go` | `zerops_workflow` MCP handler, `handleStart` dispatch | 70, 188, 248 |
| `internal/tools/workflow_develop.go` | develop-workflow prereq check | 62, 83 |
| `internal/platform/errors.go` | error-code constants | 48 |
| `internal/workflow/recipe_guidance.go` | `buildGuide`, `buildSubStepGuide`, `subStepToTopic` | 14, 468, 524 |
| `internal/workflow/recipe_topic_registry.go` | topic definitions, eager flags | 185–295 |
| `internal/workflow/recipe_substeps.go` | `SubStep*` constants, `CompleteSubStep` gate | 34–49, 163 |
| `internal/content/workflows/recipe.md` | agent-facing guidance content | 782, 1359, 1449, 1825, 2495 |

### Eager-injection trace (to verify removal correctness)

- Topic flagged `Eager: true` → added to the eager list in `recipe_topic_registry.go` (AllTopicsForStep iteration).
- Eager list consumed by `InjectEagerTopics` in `recipe_guidance.go:141` (for deploy) and `:112` (for generate).
- `InjectEagerTopics` concatenates each eager topic's resolved content to the step-entry guide response.
- Verify post-fix: `InjectEagerTopics(recipeDeployTopics, showcasePlan)` output contains `where-commands-run`'s content and does NOT contain `dev-deploy-subagent-brief` or `readme-with-fragments` content.

### Substep-guide trace (to verify delivery correctness)

- `CompleteSubStep` in `recipe_substeps.go:163` validates name and advances state.
- After the state advance, the response is composed by `buildGuide` in `recipe_guidance.go:14`.
- `buildGuide` sees an active substep (the NEXT one), calls `buildSubStepGuide(step, currentSubStepName)` at line 43.
- `buildSubStepGuide` at line 472 calls `subStepToTopic` at line 524 and `ResolveTopic` to produce the brief content.
- Verify post-fix: `buildSubStepGuide(RecipeStepDeploy, SubStepSubagent)` returns the content of the `dev-deploy-subagent-brief` block — because `subStepToTopic(deploy, init-commands, showcase)` now returns `subagent-brief`, and the response delivered AT `complete substep=init-commands` is the brief for the NEXT substep (which IS `subagent`). Read `buildGuide` carefully — the response ordering is subtle: completing substep X returns the brief for substep X+1 because after the CompleteSubStep call, `currentSubStepName()` has advanced.

### Naming

- Version tag: **v8.90** (post-rollback; v8.89 was the pre-rollback reference point, v8.86 was the last pre-rollback code change).
- Commit prefix: `feat(workflow): v8.90 — state coherence (substep-scoped briefs + subagent misuse rejection)`.

---

End of implementation guide. Fresh Opus agents: read this file end-to-end before touching code. If any file/line reference in the appendix doesn't match current working tree, stop and re-verify via `grep`/`Read` before proceeding — the doc was accurate at write-time but the tree drifts.
