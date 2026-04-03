# Analysis: Workflow Consolidation — Should debug/deploy merge? Should configure be removed?

**Date**: 2026-03-30
**Task**: Analyze whether (1) configure workflow should be removed, (2) debug and deploy are the same thing, (3) they should be unified under a universal name
**Task type**: codebase-analysis
**Complexity**: Deep (ultrathink)

---

## Current State

5 workflows, 2 categories:

| Workflow | Type | Session | Lines | What it returns |
|----------|------|---------|-------|-----------------|
| bootstrap | Orchestrated | Yes (5 steps) | 1110 | Step-by-step conductor with state |
| deploy | Orchestrated | Yes (3 steps) | 218 | Step-by-step conductor with state |
| debug | Immediate | No | 170 | Markdown reference guide |
| configure | Immediate | No | 133 | Markdown reference guide |
| cicd | Immediate | No | 152 | Markdown reference guide |

Code locations:
- Immediate workflow map: `internal/workflow/state.go:21`
- Dispatch: `internal/tools/workflow.go:56-81`
- Router offerings: `internal/workflow/router.go:169-191` (debug, scale, configure at priority 5)
- Content loading: `internal/content/content.go` (embed `workflows/*.md`)

---

## Question 1: Should configure be removed?

### What configure actually provides

A reference card for 3 operations:
1. **View env vars** → `zerops_discover includeEnvs=true` (tool already has description)
2. **Set/delete env vars** → `zerops_env action="set|delete"` (tool already has description)
3. **Enable subdomain** → `zerops_subdomain action="enable"` (tool already has description)

Plus domain knowledge:
- "After env change, reload the service" (4s reload vs 14s restart)
- Cross-service reference syntax (`${hostname_varName}`)
- Subdomain activation quirk (import.yml doesn't activate, must call after first deploy)

### Assessment: YES, remove

**The configure workflow is a tool documentation wrapper.** Every operation it describes maps 1:1 to a direct MCP tool. The LLM already sees tool descriptions for `zerops_env`, `zerops_subdomain`, `zerops_discover`.

The 3 pieces of domain knowledge it adds are genuinely useful, but belong in:
- `zerops_env` tool description: "After setting vars, reload service via zerops_manage action=reload (4s)"
- `zerops_subdomain` tool description: "Must call after first deploy to activate"
- `zerops_knowledge`: cross-service reference syntax

**Cost of keeping**: Marginal (~133 lines markdown, 3 lines in `immediateWorkflows` map, 1 line in router). But it creates a **routing decision** the LLM must make: "Should I use configure workflow or just call zerops_env directly?" This routing tax is the real cost.

**Recommendation**: Remove `configure` as a workflow. Move the 3 domain knowledge pieces into tool descriptions and knowledge base. Remove from `immediateWorkflows` map, router offerings, and MCP instructions.

---

## Question 2: Are debug and deploy the same thing?

### The user's observation

> "When I write 'fix something' or 'do an audit', it starts deploy."

This reveals the core problem: the LLM consumer faces a **routing decision** that's often wrong because the intents overlap.

### What each workflow actually does

**Debug** (stateless, returns a guide):
```
investigate → diagnose → (suggest fix) → "After debugging: start deploy workflow"
```

**Deploy** (stateful, 3-step conductor):
```
prepare → execute → verify → (if failed: investigate → fix → redeploy → re-verify)
```

### The overlap is structural

Let me trace what happens in practice for 4 real scenarios:

**Scenario A: "Deploy my code"**
1. Start deploy workflow → prepare → execute → verify
2. If verify fails → **debug pattern kicks in**: check logs, events, diagnose
3. Fix → redeploy → re-verify

**Scenario B: "My app returns 502, fix it"**
1. Start debug workflow → get guide → investigate → diagnose
2. Find issue (e.g., wrong port in zerops.yml)
3. Fix code/config
4. **Now need deploy** → start deploy workflow → execute → verify
5. That's TWO workflow starts for one task

**Scenario C: "Do an audit of my services"**
1. Should start debug → investigate all services
2. Find issues → fix each
3. For each fix → **need deploy** → deploy → verify
4. Again TWO workflows, or the LLM just calls tools directly and ignores workflows

**Scenario D: "Update the env vars and redeploy"**
1. Should start configure? Or deploy? Both?
2. In practice: just call zerops_env directly, then zerops_deploy
3. The workflow distinction adds friction, not value

### The fundamental insight

**Debug and deploy are not the same operation, but they are the same LOOP:**

```
investigate (optional)
    ↓
diagnose (optional)
    ↓
fix (optional)
    ↓
deploy
    ↓
verify
    ↓
(if failed → back to investigate)
```

- **Debug** = enter the loop at "investigate"
- **Deploy** = enter the loop at "deploy"
- But both loops converge to the same cycle

The current separation forces the LLM to:
1. Choose an entry point (debug vs deploy) based on intent parsing
2. Switch workflows mid-task when one leads to the other
3. Lose context between the two workflows (debug has no session, deploy does)

### Evidence from code

Deploy's verify section (`deploy.md:132-168`) already contains a full debug pattern:
- "check `zerops_logs severity="error" since="5m"`"
- "verify managed service RUNNING, check hostname/port"
- Common fix patterns table (identical to debug.md's table at lines 66-73)

Debug's ending (`debug.md:167-169`) explicitly redirects to deploy:
- "Issue fixed in code? → zerops_workflow action="start" workflow="deploy""

**They reference each other because they're halves of the same operation.**

---

## Question 3: Should they be unified?

### YES — but how?

Three design options:

### Option A: Merge into unified "operate" workflow (stateful)

Add an optional "investigate" step to the deploy state machine:

```
Steps: [investigate] → prepare → execute → verify
         (optional)
```

- "Fix my broken app" → starts at investigate
- "Deploy my code" → starts at prepare (skip investigate)
- "Do an audit" → starts at investigate, may stop before deploy

**Pros**: Clean state machine, single entry point, full context preservation
**Cons**: Adds complexity to DeployState, optional steps need skip logic

### Option B: Remove debug workflow, enhance deploy verify loop (minimal change)

- Delete `debug.md` as a workflow
- Fold debug investigation guidance into deploy.md's verify/iteration section
- For standalone investigation ("audit this"), the LLM uses tools directly (zerops_discover, zerops_logs, zerops_events) — no workflow needed

**Pros**: Minimal code change, no new state machine complexity
**Cons**: Standalone investigation tasks have no workflow guidance

### Option C: Remove debug AND deploy workflows, replace with single "operate" guide (stateless)

- Both become a single markdown guide: "How to operate Zerops services"
- Covers: investigation, configuration, deployment, verification, iteration
- LLM uses tools directly, guide provides the mental model
- Bootstrap remains the only stateful workflow

**Pros**: Maximum simplification, eliminates all routing decisions for post-bootstrap work
**Cons**: Loses deploy's stateful iteration tracking (but is that tracking actually valuable?)

### Evaluating deploy statefulness

The deploy workflow's session tracks:
- `DeployState.Targets` — which services to deploy to
- `DeployState.Mode` — standard/dev/simple
- `DeployState.Strategy` — push-dev/ci-cd/manual
- `DeployState.Steps` — progress (prepare/execute/verify)
- Iteration counter

**Key question**: Does the LLM actually need this state?

The LLM already knows:
- Which services exist (from zerops_discover)
- Service modes (from ServiceMeta)
- Deploy strategies (from ServiceMeta)
- What step it's on (from conversation context)

The deploy session state is **redundant with ServiceMeta + LLM conversation context**. The iteration counter prevents infinite loops, but `maxIterations` could be enforced at the tool level (zerops_deploy call count) rather than session level.

**Verdict**: Deploy's statefulness provides marginal value over what ServiceMeta + conversation context already give.

---

## Recommendation

### Option B+ (pragmatic, evidence-backed)

**Remove 3 of 5 workflows. Keep 2.**

| Current | Action | Rationale |
|---------|--------|-----------|
| bootstrap | **KEEP** | Genuine multi-step orchestration with complex state (plan, env vars, strategies). No redundancy. |
| deploy | **KEEP but simplify** | Rename to something broader (see below). Fold debug investigation into verify loop. Make investigate a lightweight entry mode, not a separate step. |
| debug | **REMOVE** | Its content becomes part of deploy's verify/iteration guidance + zerops_knowledge entry |
| configure | **REMOVE** | Tool documentation wrapper. Move domain knowledge to tool descriptions + knowledge |
| cicd | **KEEP or REMOVE** | Borderline. It guides external system setup (GitHub/GitLab) which is genuinely different. Could stay as immediate workflow or move to knowledge. |

### Naming

If deploy absorbs debug, the name "deploy" is too narrow. Options:
- **"operate"** — covers investigation + deployment + verification
- **"work"** — too generic
- **"run"** — conflicts with zerops.yml `run:` section
- **"deploy"** — keep the name, just widen the scope (deploy already implies verify, verify implies debug)

**Recommendation**: Keep "deploy" as the name. The LLM instruction changes from:
```
- Deploy code: zerops_workflow action="start" workflow="deploy"
- Debug issues: zerops_workflow action="start" workflow="debug"
- Configure: zerops_workflow action="start" workflow="configure"
```
to:
```
- Deploy, fix issues, or operate services: zerops_workflow action="start" workflow="deploy"
```

One workflow, one entry point, no routing decision. The deploy guide includes investigation as its first-resort when verification fails.

### Implementation impact

**Files to modify**:
1. `internal/workflow/state.go` — remove "debug" and "configure" from `immediateWorkflows`
2. `internal/workflow/router.go:169-191` — remove "configure" from `appendUtilities`, update "debug" to point at deploy
3. `internal/tools/workflow.go:136-155` — update immediate workflow handling
4. `internal/content/workflows/deploy.md` — add investigation section (from debug.md)
5. `internal/content/workflows/debug.md` — **delete**
6. `internal/content/workflows/configure.md` — **delete**
7. MCP instructions (system prompt) — simplify routing
8. Tool descriptions for `zerops_env`, `zerops_subdomain` — add domain knowledge from configure.md

**Estimated effort**: ~100 lines changed, ~300 lines removed (debug.md + configure.md)

### What about cicd?

CI/CD is genuinely different — it configures external systems (GitHub Actions, GitLab webhooks) rather than operating Zerops services. It doesn't overlap with debug or deploy.

**Options**:
- Keep as immediate workflow (current)
- Move to knowledge base (accessible via `zerops_knowledge query="cicd setup"`)

Either works. It's low-priority to change since it doesn't create routing confusion (nobody says "fix my app" and gets CI/CD workflow).

---

## Summary

| # | Finding | Severity | Evidence |
|---|---------|----------|----------|
| F1 | Debug and deploy share the same investigate→fix→deploy→verify loop | CRITICAL | deploy.md:132-168 duplicates debug.md:59-73 patterns; debug.md:167 redirects to deploy |
| F2 | Configure is a tool documentation wrapper with zero orchestration | MAJOR | All 3 operations map 1:1 to existing MCP tools (zerops_env, zerops_subdomain, zerops_discover) |
| F3 | 5 workflows create unnecessary routing decisions for the LLM consumer | MAJOR | User reports LLM starts deploy instead of debug for "fix" intents |
| F4 | Deploy session state is largely redundant with ServiceMeta + conversation context | MINOR | DeployState.Mode/Strategy/Targets all derivable from ServiceMeta |
| F5 | Debug ends with "start deploy workflow" — proving they're halves of one operation | CRITICAL | debug.md:167-169 explicitly chains to deploy |

**Key insight**: The 3 immediate workflows (debug, configure, cicd) are not workflows — they're knowledge documents served through the workflow API. Debug and deploy are two entry points into the same operational loop. Unifying eliminates routing friction for the LLM consumer.

**Next step**: Decide whether to keep cicd as immediate workflow or move to knowledge. Then implement Option B+ (remove debug + configure, fold into deploy + tool descriptions).
