# Design: Manual Deploy Strategy + Post-Bootstrap Orientation

**Date**: 2026-03-23
**Status**: Approved — ready for implementation
**Scope**: Manual strategy redesign + post-bootstrap project-aware system prompt

---

## 1. Problem Statement

Three related problems:

### A. Manual strategy is broken
- Claims "ZCP won't guide deploys" but runs identical 3-step workflow as push-dev
- Strategy selection text (`workflow_strategy.go:134-138`) contradicts actual behavior
- deploy.md section is a 3-line stub, router output identical to push-dev
- See `plans/analysis-manual-deploy-strategy.analysis-1.md` for full evidence

### B. No post-bootstrap orientation
- After bootstrap, LLM agent gets the same generic routing instructions as before
- Agent doesn't know: service modes, strategies, dev server lifecycle, when to deploy vs restart
- Knowledge (zerops_knowledge, runtime docs) isn't surfaced proactively
- Agent doesn't understand the state of each service (deployed? ready-to-deploy? dev mode?)

### C. Base instructions teach by example instead of principle
- `containerEnvironment` (instructions.go:24-25) lists specific tools for SSH: "npm install, go mod download, pip install, cargo build, composer install"
- Agent generalizes from the list: if a tool isn't listed (git, server processes, debugging tools), agent tries it locally on zcpx instead of via SSH
- **Observed failure**: Agent edited files via SSHFS mount correctly, ran composer via SSH correctly, but then tried `git init` locally on zcpx — needed 3 corrections from user
- Root cause: instructions say "run heavy commands via SSH" (implying SSH = performance optimization) instead of "SSHFS = file access only, SSH = all commands and processes"
- This is a general principle problem, not a per-tool problem — fixing it prevents ALL similar errors, not just git

---

## 2. Design Decisions (from discussion)

| # | Decision | Rationale |
|---|----------|-----------|
| D1 | Manual strategy does NOT start deploy workflow | Workflows guide multi-step operations. Manual deploy is not multi-step from ZCP perspective. |
| D2 | `handleDeployStart()` returns redirect for manual | Uses existing soft-gate pattern (like missing-strategy gate at line 60-61). No session created. |
| D3 | Knowledge lives in system instructions, not workflow | User can say "deploy this" at any time. Knowledge must be always available, not gated behind workflow start. |
| D4 | Post-bootstrap orientation = enhanced `buildProjectSummary()` | Reuses existing mechanism. When ServiceMetas exist, generates richer per-service guidance. |
| D5 | Volatile disclaimer is gentle, not scary | Data loss happens only on deploy (new container) — rare during dev. Restarts are safe. Don't overstate. |
| D6 | Strategy change is always available, not pushed | Manual users chose their style. Mention option to switch, don't push it. |
| D7 | Each strategy = different interaction model | push-dev = deploy workflow, ci-cd = cicd workflow, manual = direct tools. Clean architectural separation. |

---

## 3. Architecture

### 3.1 Strategy Interaction Models

```
push-dev  → zerops_workflow action="start" workflow="deploy" → 3-step session
ci-cd     → zerops_workflow action="start" workflow="cicd"   → 3-step session
manual    → zerops_deploy targetService="..."                → direct tool call
```

### 3.2 Knowledge Delivery

```
                         ┌─────────────────────┐
                         │   System Prompt      │
                         │   (instructions.go)  │
                         │                      │
                         │ Section A: Base       │
                         │ Section B: Workflow   │
                         │ Section C: Environment│
                         │ Section D: Project ◄──┼── Enhanced with post-bootstrap orientation
                         │   - Per-service guide │
                         │   - Strategy guidance │
                         │   - Knowledge pointers│
                         │   - Dev lifecycle     │
                         └─────────────────────┘
                                    │
                     Always available to LLM agent
```

### 3.3 handleDeployStart() Flow (updated)

```
handleDeployStart()
  ├─ Gate 1: Metas exist?                    → error: run bootstrap
  ├─ Gate 2: Metas complete?                 → error: finish bootstrap
  ├─ Gate 3: Runtime services?               → error: nothing to deploy
  ├─ Gate 4: Strategy set?                   → soft: strategy selection guidance
  ├─ Gate 5: Strategy = manual?              → soft: manual redirect response [NEW]
  ├─ Gate 6: Mixed strategies?               → error: deploy one at a time
  └─ Create deploy session (push-dev only)
```

---

## 4. Base Instructions Fix (containerEnvironment)

### 4.0 Problem

Current `containerEnvironment` (instructions.go:15-37) teaches SSH by listing specific tools:

```
Run heavy commands (npm install, go mod download, pip install, cargo build, composer install) via SSH
```

This implies SSH is a **performance optimization** for "heavy" commands. Agent concludes: light commands (git, curl, server start) can run locally. Wrong — they must also run on the service.

### 4.1 Fix: Principle-Based Instructions

Replace the tool-specific list with a clear principle:

```
### Code Access — Two Mechanisms

**SSHFS mount** (`/var/www/{hostname}/`): For reading and writing files only.
  Changes appear instantly on the service container. Use Write/Edit tools normally.
  IMPORTANT: /var/www/ (no hostname) is THIS container — not a service.

**SSH** (`ssh {hostname} "command"`): For ALL commands and processes on services.
  Package installs, builds, git operations, server management, debugging —
  everything that isn't file read/write goes through SSH.
  Example: ssh appdev "cd /var/www && npm install"

Rule: If it's a file → mount. If it's a command → SSH.
```

### 4.2 Why This Works Generally

- **Git**: `ssh docs "cd /var/www && git init"` — not `git init` on zcpx
- **Server start**: `ssh appdev "cd /var/www && node index.js &"` — not locally
- **Package install**: `ssh appdev "cd /var/www && npm install"` — not over SSHFS
- **Debugging**: `ssh appdev "curl localhost:3000/health"` — not from zcpx
- **Any future tool**: The principle covers it without listing every possible command

### 4.3 Persistence Model Update

Current text hardcodes deploy workflow:
```
After completing code changes, you MUST deploy to persist them permanently.
Start a deploy workflow: zerops_workflow action="start" workflow="deploy"
```

Replace with strategy-aware text (generated dynamically in post-bootstrap orientation, static fallback for pre-bootstrap):
```
### Persistence
File edits on mount survive restarts but not deploys (deploy = new container, only deployFiles persists).
Deploy when: zerops.yml changes, clean rebuild needed, or promote dev → stage.
Code-only changes on dev: just restart the server via SSH — no redeploy needed.
```

The specific deploy command ("use workflow" vs "call zerops_deploy directly") moves to the per-service orientation in Section 5, where it's strategy-aware.

---

## 5. Post-Bootstrap Orientation

### 5.1 When to Activate

`buildProjectSummary()` generates orientation when ALL of:
- ServiceMetas exist (at least one)
- At least one meta has `BootstrappedAt` set (bootstrap completed)
- Live services match metas (not stale)

### 5.2 Content Structure

The orientation replaces the current simple service list + state line with:

```markdown
## Your Project — Bootstrapped

ZCP helps you manage this project. Key tools:
- zerops_knowledge query="..." — runtime docs, recipes, schemas
- zerops_discover — current service state and env vars
- zerops_workflow — guided workflows (debug, configure, bootstrap)

### appdev (nodejs@22) — RUNNING, dev mode
Mount: /var/www/appdev/
Server: manual start via SSH (uses zsc noop — no auto-start)
Deploy: zerops_deploy targetService="appdev"
After deploy: new container — restart server via SSH, re-enable subdomain
Code changes on mount: restart server (no redeploy needed)
Redeploy only when: zerops.yml changes or clean rebuild needed
Knowledge: zerops_knowledge query="nodejs"

### appstage (nodejs@22) — READY_TO_DEPLOY, stage
Deploy from dev: zerops_deploy sourceService="appdev" targetService="appstage"
Auto-starts after deploy (healthCheck monitors)

### db (postgresql@16) — RUNNING
Env vars: zerops_discover includeEnvs=true

### Deploy Strategy: manual
You control when to deploy. Call zerops_deploy directly.
To switch to guided deploys: zerops_workflow action="strategy" strategies={"appdev":"push-dev"}

### Operations
- Debug: zerops_workflow action="start" workflow="debug"
- Configure: zerops_workflow action="start" workflow="configure"
- Scale: zerops_scale serviceHostname="..."
```

### 5.3 Mode-Specific Service Info

| Mode | What to show |
|------|-------------|
| **Dev** (standard/dev-only) | Mount path, SSH server management, "no auto-start", deploy command, restart vs redeploy |
| **Stage** (standard) | Cross-deploy command, auto-start note, healthCheck |
| **Simple** | Deploy command, auto-start note, healthCheck |
| **Managed** | Status only, env vars pointer |

### 5.4 Strategy-Specific Guidance

| Strategy | System prompt guidance | Router offering |
|----------|----------------------|-----------------|
| **manual** | "You control when to deploy. Call zerops_deploy directly." | No deploy workflow. Utilities only. |
| **push-dev** | "Deploy via guided workflow." | deploy (P1) |
| **ci-cd** | "Deploys happen via git webhook." | cicd (P1), deploy (P2) |
| **not set** | "Set a deploy strategy first." | strategy selection |

### 5.5 Volatile Container Disclaimer (gentle)

> Code on your SSHFS mount survives restarts, reloads, and scaling. Only a deploy creates a fresh container — mount edits won't be in it (only `deployFiles` content persists). During development this is rare — you choose when to deploy.

Not a warning, just a fact. Placed in the dev service section, not as a separate scary block.

---

## 6. Manual Strategy Gate

### 6.1 handleDeployStart() — New Gate

When all services have `StrategyManual`, return a response (not an error) with:

```json
{
  "action": "manual_deploy",
  "message": "Deploy strategy is manual. Deploy directly when ready.",
  "services": [
    {
      "hostname": "appdev",
      "mode": "dev",
      "command": "zerops_deploy targetService=\"appdev\"",
      "postDeploy": "Start server via SSH. Enable subdomain."
    }
  ],
  "switchStrategy": "zerops_workflow action=\"strategy\" strategies={...}"
}
```

### 6.2 Router Update

`strategyOfferings()` for manual:
```go
case StrategyManual:
    return nil // No deploy/cicd workflow offering. Utilities appended by caller.
```

### 6.3 Strategy Selection Text Update

`buildStrategySelectionResponse()` — manual description:
```
### manual
You control when and what to deploy. No guided workflow.
- **How it works**: Edit code, call zerops_deploy when ready. ZCP validates on request.
- **Good for**: Experienced users, external CI/CD, custom workflows.
- **Trade-off**: No guided prepare/verify cycle — you manage the deploy process.
```

---

## 7. Implementation Plan

### Phase 1: Base Instructions Fix (~30 lines)
**Files**: `instructions.go`
**TDD**: RED first — test that container environment text contains principle-based SSH guidance

1. Rewrite `containerEnvironment` "Code Access" + "Commands on Services" sections
2. Replace tool-specific SSH list with general principle: "mount = files, SSH = commands"
3. Remove hardcoded "Start a deploy workflow" from persistence model (moves to orientation)
4. Tests: `instructions_test.go` — verify SSH principle text, verify no hardcoded deploy workflow command

### Phase 2: Manual Strategy Gate (~50 lines)
**Files**: `workflow_deploy.go`, `workflow_strategy.go`, `router.go`, `deploy.md`
**TDD**: RED first — test that manual strategy returns redirect, not session

1. `workflow_deploy.go`: Add manual gate after strategy check (before `engine.DeployStart`)
2. `workflow_strategy.go`: Update `buildStrategySelectionResponse()` text for manual
3. `router.go`: `strategyOfferings()` returns nil for manual
4. `deploy.md`: Rewrite `deploy-manual` section
5. Tests: strategy gate test, router test, guidance test

### Phase 3: Post-Bootstrap Orientation (~150 lines)
**Files**: `instructions.go`, new `instructions_orientation.go`
**TDD**: RED first — test that bootstrapped project generates per-service guidance

1. New `buildPostBootstrapOrientation()` function
2. Called from `buildProjectSummary()` when ServiceMetas with BootstrappedAt exist
3. Generates per-service guidance based on:
   - ServiceMeta (mode, strategy, hostname, stage)
   - Live API state (status from services list)
   - Runtime type (from API)
4. Per-service block includes: mount path, SSH command pattern, deploy command, server lifecycle
5. Strategy-specific section
6. Knowledge pointers personalized to runtime types
7. Operations section (debug, configure, scale)
8. Tests: table-driven for each mode × strategy combination

### Phase 4: Spec + Content Updates (~30 lines)
**Files**: `spec-bootstrap-deploy.md`, `deploy.md`
1. Update spec section 4.2 (strategy gate) to document manual redirect
2. Update spec strategy table
3. Update deploy.md deploy-manual section

---

## 8. Test Plan

| Test | Layer | What it verifies |
|------|-------|-----------------|
| `TestContainerEnvironment_SSHPrinciple` | Unit | SSH instruction is principle-based ("all commands"), not tool-list |
| `TestContainerEnvironment_NoPersistenceHardcode` | Unit | No hardcoded "start deploy workflow" in persistence text |
| `TestDeployStart_ManualStrategy_ReturnsRedirect` | Tool | Manual gate returns response, not session |
| `TestDeployStart_ManualStrategy_NoSession` | Tool | No session file created |
| `TestRouter_ManualStrategy_NoDeployOffering` | Unit | Router returns nil for manual |
| `TestStrategySelection_ManualDescription` | Tool | Updated manual description text |
| `TestOrientation_DevMode_ManualStrategy` | Unit | Orientation includes SSH start, deploy command |
| `TestOrientation_StandardMode_DevAndStage` | Unit | Both dev and stage services described |
| `TestOrientation_SimpleMode` | Unit | Auto-start noted, no SSH management |
| `TestOrientation_ManagedOnly` | Unit | No runtime guidance, just env var pointer |
| `TestOrientation_MixedStrategies` | Unit | Each service shows its own strategy guidance |
| `TestOrientation_NoMetas` | Unit | Falls back to current simple list |

---

## 9. Out of Scope

- Changing how push-dev or ci-cd workflows work (they stay as-is)
- Adding new MCP tools (reuses existing zerops_deploy, zerops_verify, etc.)
- Modifying bootstrap flow (strategy is still set post-bootstrap via action=strategy)
- Knowledge engine changes (zerops_knowledge stays as-is, just referenced in prompts)
