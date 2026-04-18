# Work Session — ZCP Development Lifecycle

> Authoritative spec. Last edited 2026-04-17. Supersedes the pre-refactor
> develop-flow design (now in `archive/spec-develop-flow-current.md`) and
> closes the structural gaps documented in
> `archive/spec-lifecycle-analysis.md` / `archive/audit-session-design.md`.

---

## 0. Philosophical Foundation

ZCP runs inside an LLM harness that **forgets**. Context windows compact,
processes restart, users run multiple Claude Code instances at once. Every
design decision in this spec answers one question: *what must be true so
the LLM can pick up a long development task mid-flight without re-deriving
its own lifecycle position?*

Four load-bearing principles follow from that:

1. **Post-compaction re-orientation must be a single tool call.** The MCP
   `Instructions` field is delivered once at client init and cannot be
   re-emitted per response. Tool-call history is truncated under
   compaction, so the transcript is also unreliable. The contract is
   therefore: base instructions (delivered at init) teach the LLM a
   single canonical recovery call — `zerops_workflow action="status"` —
   which returns the **Lifecycle Status** block assembled on demand from
   fresh reads of the work session file + ServiceMeta + live services.
   That one call owns the answers to *what am I doing, where am I,
   what's next?*.

2. **One fact, one home.** Strategy lives in `ServiceMeta`. Deploy
   outcomes live in Work Session. Service status lives on the platform.
   Copying any of these into a second store creates drift the moment
   reality changes out-of-band (another instance deploys, user edits from
   the UI, platform rebuilds a service). The Work Session intentionally
   stores *only* what cannot be recovered from another source — intent,
   the local history of attempts, and the services in scope.

3. **State is layered by lifetime, not by feature.** Three layers —
   Infrastructure (project-lifetime), Work (one task per process), Action
   (single call) — are isolated so the volatility of one does not
   contaminate the others. A crashed MCP process loses a task's attempt
   history but cannot damage `ServiceMeta` or bootstrap progress. A new
   task cannot corrupt a bootstrap session running alongside it.

4. **The LLM is advised, never gated.** Work Session is observable, not
   enforcing. It tells the LLM what's been tried and what's still
   pending; it does not block tool calls. Hard gates remain only where
   correctness demands them (bootstrap step ordering, hostname locks).
   Everywhere else the LLM keeps discretion — guidance beats prohibition
   when the environment is already under an intelligent agent's control.

The refactor this spec describes is not a feature addition. It is the
removal of an earlier step-machine model whose contradictions (manual
strategy deadlock, strategy-blind execute, deploy decoupling) forced a
stateless-briefing workaround that in turn erased lifecycle visibility.
The Work Session restores visibility without bringing back the step
machine.

---

## 1. Executive Summary

The **Work Session** is a per-PID artifact at `.zcp/state/work/{pid}.json`
with three responsibilities:

- Record the LLM's stated intent and the services in scope for the
  current task.
- Append a capped history of deploy and verify attempts as tool
  side-effects.
- Expose a **Lifecycle Status** block via `zerops_workflow action="status"`,
  assembled on demand from the work session file + ServiceMeta + live
  services. The base MCP `Instructions` (delivered once at client init)
  teach the LLM to call this first on every new task and after compaction.

It closes explicitly (`zerops_workflow action="close" workflow="develop"`)
or auto-closes when every service in scope has a succeeded deploy and a
passed verify. It does not survive process restart — code work survives
in git and on disk; carrying stale task intent across processes creates
more confusion than it resolves.

---

## 1. Design Goals (Ranked)

1. **Reliability over convenience.** No last-write-wins files, no silent
   state drift, no lost lifecycle position.
2. **Single source of truth per fact.** Strategy lives in ServiceMeta. Mode
   lives in ServiceMeta. Work progress lives in Work Session. Never duplicate.
3. **Every state transition is observable to the LLM.** If it happened, the
   LLM can see it in the next MCP response or via `action="status"`.
4. **Each Claude Code instance is isolated.** PID-scoped state. Zero
   cross-instance interference for work-layer state. Infrastructure-layer
   state (bootstrap, recipe) uses registry-backed ownership.
5. **Survives compaction by default.** The MCP init instructions point
   every LLM at `zerops_workflow action="status"` as the canonical
   orientation call, so even a freshly-compacted LLM has a single,
   deterministic recovery step.
6. **Survives process restart per layer semantics.** Bootstrap/recipe claim
   on boot (existing). Work sessions are per-process — restart = fresh start.
7. **Advisory, not enforcing.** Work Session informs the LLM; it does not
   gate tool calls. Hard gates stay only where they belong (bootstrap step
   checkers, hostname locks).
8. **Loop closure is explicit AND auto-heuristic.** LLM is guided to call
   `action="close"`. Failing that, sessions auto-close when all services are
   deployed+verified.
9. **Terminology stays familiar.** "Develop workflow" remains the user-facing
   name. "Work Session" is the internal artifact.

---

## 2. Three-Layer Architecture

| Layer | Entity | Lifetime | Source of truth for | Storage |
|-------|--------|----------|---------------------|---------|
| Infrastructure | `ServiceMeta`, bootstrap/recipe sessions | Project lifetime / workflow duration | Service existence, strategy, mode, pairing | `.zcp/state/services/`, `.zcp/state/sessions/` |
| Work | **Work Session (new)** | One LLM task per process | Task intent, deploy attempts, verify results, activity timeline | `.zcp/state/work/{pid}.json` |
| Action | Tool invocations | Single call | Deploy result, verify result, mount state | Platform API + Work Session side-effects |

**Invariant:** Every piece of data lives in exactly one layer. Layers read
down (Work can read Infrastructure), never up.

---

## 3. Work Session Data Model

```go
// internal/workflow/work_session.go (new file)
type WorkSession struct {
    Version        string                             `json:"version"`         // "1"
    PID            int                                `json:"pid"`
    ProjectID      string                             `json:"projectId"`
    Environment    string                             `json:"environment"`     // container | local
    Intent         string                             `json:"intent"`
    Services       []string                           `json:"services"`        // hostnames in scope
    CreatedAt      string                             `json:"createdAt"`
    LastActivityAt string                             `json:"lastActivityAt"`
    Deploys        map[string][]DeployAttempt         `json:"deploys"`         // per hostname, history capped at 10
    Verifies       map[string][]VerifyAttempt         `json:"verifies"`        // per hostname, capped at 10
    ClosedAt       string                             `json:"closedAt,omitempty"`
    CloseReason    string                             `json:"closeReason,omitempty"` // explicit | auto-complete | abandoned
}

type DeployAttempt struct {
    AttemptedAt string `json:"attemptedAt"`
    SucceededAt string `json:"succeededAt,omitempty"`
    Setup       string `json:"setup,omitempty"`       // dev | prod
    Strategy    string `json:"strategy,omitempty"`    // push-dev | git-push | manual
    Error       string `json:"error,omitempty"`
}

type VerifyAttempt struct {
    AttemptedAt string `json:"attemptedAt"`
    PassedAt    string `json:"passedAt,omitempty"`
    Summary     string `json:"summary,omitempty"`     // "HTTP 200", "logs clean", "error: ..."
    Passed      bool   `json:"passed"`
}
```

**Explicitly NOT stored** (read fresh from ServiceMeta/API when needed):
strategy per service, mode per service, runtime type, port configuration,
env vars, service status. This prevents the stale-snapshot class of bugs
documented in `archive/audit-session-design.md`.

---

## 4. Registry as Single Source of Session Ownership

The current `.zcp/state/active_session` file is replaced by registry-backed
lookup. Registry (`.zcp/state/registry.json`) already tracks all live
entities with PID. Remove the side-channel file entirely.

**Registry entry extended:**

```go
type SessionEntry struct {
    SessionID string `json:"sessionId"`
    PID       int    `json:"pid"`
    Workflow  string `json:"workflow"`     // bootstrap | recipe | work
    ProjectID string `json:"projectId"`
    CreatedAt string `json:"createdAt"`
}
```

**Work sessions register with `Workflow: "work"` and `SessionID: "work-{pid}"`.**

`NewEngine()` scans registry for PID match:
- Own PID alive → load our session
- Other PID dead → claim (bootstrap/recipe only — work sessions are
  per-process, not claimable)
- No match → no active session

**Benefits:**
- Zero chance of `active_session` file collision between parallel instances
- Single place to enumerate all live sessions (useful for admin/debug)
- Claim logic unchanged (already uses registry)

---

## 5. Lifecycle Status Block — Delivered via `action="status"`

The **Lifecycle Status** block is assembled on demand by
`zerops_workflow action="status"`. MCP's `Instructions` field is delivered
once at client init and cannot be re-emitted per response, so per-call
state cannot live there. Instead, the static init instructions direct the
LLM to call `action="status"` first on every new task and after
compaction; the handler reads the current work session file, service
metas, and live service list, and renders one of the cases below.

Content depends on active entities for the current PID:

### 5.1 No activity

Omit the block entirely. Base instructions still guide "start a develop
workflow for any code task."

### 5.2 Work session only

```
## Lifecycle Status
Work session active (1h 23m) — intent: "add login form"
  Services: web (push-dev), api (push-dev)
  Deploys: web ✓ 3m ago | api ✗ 2 attempts (last: build timeout 1m ago)
  Verifies: web ✓ 2m ago | api —
  → Next: fix build error on api, redeploy
```

### 5.3 Work session + bootstrap (concurrent)

```
## Lifecycle Status
Active workflow: bootstrap | session abc123 | step 3/5: generate
  ← PRIMARY — complete bootstrap before resuming work
Work session (backgrounded): intent="..." — services: web, api
  (does not include newly bootstrapped services yet)
```

### 5.4 Work session — auto-close pending

When all services have `succeededAt` deploy + `passedAt` verify:
```
## Lifecycle Status
Work session — task complete (all services deployed + verified)
  → Close: zerops_workflow action="close" workflow="develop"
  → Next task: zerops_workflow action="start" workflow="develop"
```

### 5.5 Bootstrap / recipe only (no work session)

Existing behavior, unchanged.

---

## 6. Workflow Actions — Updated Semantics

### 6.1 `action="start" workflow="develop"`

1. Gather ServiceMetas. If empty and live services exist → auto-adopt
   (bootstrap fast-path, transparent).
2. Filter to complete runtime metas.
3. Strategy check:
   - If any service has unconfirmed strategy → briefing prompts strategy
     first. **Work session is NOT created yet.**
   - If all strategies set → proceed.
4. Create Work Session for current PID. If one already exists:
   - Same intent → return fresh briefing, do not reset history.
   - Different intent → return current state + asks: "Close current session
     (zerops_workflow action=close) or provide intent=force to overwrite?".
5. Register in registry (`work-{pid}` entry).
6. Write `.zcp/state/work/{pid}.json`.
7. Return briefing (existing format) plus "Work session created" confirmation.

### 6.2 `action="status"` (no workflow argument)

Routing precedence:
1. If work session exists for current PID → return Work Session status.
2. Else if bootstrap session active → existing behavior.
3. Else if recipe session active → existing behavior.
4. Else → "No active workflow. Start one with action=start workflow=develop."

**Work Session status response:**

```json
{
  "workflow": "develop",
  "intent": "add login form",
  "services": [
    {"hostname": "web", "strategy": "push-dev", "mode": "dev",
     "deploys": {"attempted": 1, "succeeded": 1, "lastError": ""},
     "verifies": {"attempted": 1, "passed": 1}},
    {"hostname": "api", "strategy": "push-dev", "mode": "dev",
     "deploys": {"attempted": 2, "succeeded": 0, "lastError": "build timeout"},
     "verifies": {"attempted": 0, "passed": 0}}
  ],
  "createdAt": "...", "lastActivityAt": "...",
  "suggestedNext": "Fix build timeout on api, then redeploy.",
  "canClose": false
}
```

Strategy and mode on each service are **read fresh from ServiceMeta** at
response-build time.

`suggestedNext` is computed by a pure function over deploy/verify state +
live strategy:
- Some service has unsucceeded deploy attempts → "Fix and redeploy [hostname]"
- All deployed, some not verified → "Verify [hostnames]"
- All deployed + verified → "Task complete. Close session or start next task."
- Idle > 4h → "Session is stale. Close if task is done."

### 6.3 `action="close" workflow="develop"` (NEW)

1. Load work session for current PID.
2. Write `closedAt = now`, `closeReason = "explicit"`.
3. Delete the work session file (after writing a terminal summary to stderr
   for debug).
4. Unregister from registry.
5. Return summary:
   ```
   Work session closed.
   Summary:
     web: deployed ✓, verified ✓
     api: deployed ✓, verified ✓
   Duration: 1h 32m.
   For next task: zerops_workflow action="start" workflow="develop"
   ```

### 6.4 `action="iterate"` — removed for develop

Develop is stateless. No iteration counter. LLM simply retries the failing
step; deploy attempts accumulate in `deploys[hostname]` for visibility.

The 10-iteration cap stays in bootstrap/recipe (existing behavior).

### 6.5 `action="reset"` for work session

Deletes the work session file, unregisters, returns "work session reset".
Equivalent to close without summary. Intended for recovery after
inconsistency, not as a normal flow step.

---

## 7. Tool Side-Effects into Work Session

### 7.1 `zerops_deploy`

On invocation:
1. `requireWorkflow(engine)` — now specifically checks for Work Session
   (current PID) OR stateful session. Hard error otherwise.
2. Record attempt immediately: `deploys[hostname]` append `DeployAttempt{AttemptedAt: now, Setup, Strategy}`.
3. Execute deploy as today.
4. On success: update last attempt `SucceededAt = now`.
5. On failure: update last attempt `Error = err.Message`.
6. Update `LastActivityAt = now`.
7. Cap `deploys[hostname]` at last 10 entries.
8. Check auto-close heuristic.

### 7.2 `zerops_verify`

Same pattern. Records `verifies[hostname]`:
- Per-check success → `verifies[hostname]` append `VerifyAttempt{PassedAt, Passed: true}`.
- Failure → `Passed: false, Summary: reason`.

### 7.3 `zerops_mount`

Requires Work Session. Adds a mount indication to `LastActivityAt`, no
separate field (mounts are idempotent, not a lifecycle event).

### 7.4 `zerops_import`

Requires Work Session OR bootstrap session. No work session side-effect.
(Imports are usually bootstrap-time.)

### 7.5 Auto-close heuristic

After any tool updates the work session, evaluate:

```
autoClose = len(Services) > 0 &&
    for all s in Services:
        len(deploys[s]) > 0 && last(deploys[s]).SucceededAt != "" &&
        len(verifies[s]) > 0 && last(verifies[s]).Passed == true
```

If true:
- Set `ClosedAt = now`, `CloseReason = "auto-complete"`.
- File stays on disk for one grace period (e.g., until next `action=start` or
  process exit), so the LLM's next `action="status"` surfaces the closure
  hint.
- `action="status"` switches to the task-complete variant:
  ```
  Work session — task complete. All services deployed + verified.
    For next task: zerops_workflow action="start" workflow="develop"
  ```
- `action="close"` from LLM is idempotent (already closed, returns summary).

---

## 8. Bootstrap → Work Session Transition

`BuildTransitionMessage` in `internal/workflow/bootstrap_guide_assembly.go` is
updated to explicitly script the three-step handoff:

```
Bootstrap complete.

Services:
  - web (nodejs, standard mode)
  - api (go, standard mode)

Next steps (do these in order):

1. Set deploy strategy for each service:
     zerops_workflow action="strategy"
       strategies={"web": "push-dev", "api": "push-dev"}
   Options:
     - push-dev — self-deploy from dev container (recommended for iterative work)
     - push-git — git push triggers CI/CD
     - manual — user controls deployments

2. Start a work session for your first task:
     zerops_workflow action="start" workflow="develop"
       intent="<what you'll implement>"
```

For adoption flow (auto-adopt inside `handleDevelopStart`), the same
two-step is implied but collapsed: briefing prompts strategy, LLM sets it,
calls develop again, work session created.

---

## 9. Scenario Walkthroughs — Post-Redesign

### 9.1 Fresh recipe adoption → work session → deploy → close

1. User imports recipe. Opens Claude Code. `zcp` starts (PID 1001).
2. LLM sees base instructions, no lifecycle hint (no work session yet).
3. LLM calls `action="start" workflow="develop" intent="build the app"`.
4. Server: no metas → auto-adopt → metas written with empty strategy.
5. Server: strategy unset on all services → briefing prompts strategy.
   **Work session NOT created yet.**
6. LLM discusses with user, calls `action="strategy" strategies={web: push-dev, api: push-dev}`.
7. LLM calls `action="start" workflow="develop" intent="build the app"`.
8. Work session created. File `work/1001.json`. Registered.
9. LLM codes on mount, using `action="status"` whenever it needs to check
   where it is.
10. Compaction at 1h. LLM's next instruction-following step calls
    `action="status"` (taught by the init instructions); the Lifecycle
    Status block re-orients it and it continues.
11. Deploys web → succeeded. Verifies web → passed.
12. Deploys api → succeeded. Verifies api → passed.
13. Auto-close fires. Next `action="status"` renders the "task complete,
    close or next" variant.
14. LLM calls `action="close"` → file deleted, summary returned.
15. For next task → repeat from step 3 (strategy already set, briefing direct).

### 9.2 Deploy-fail-fix-redeploy

1. Work session active. Deploy api → build fails. `deploys.api = [{AttemptedAt, Error: "build timeout"}]`.
2. Next `action="status"` shows: `api ✗ 1 attempt (last: build timeout)`.
3. LLM reads logs, fixes code.
4. Deploy api → success. `deploys.api = [{fail}, {AttemptedAt, SucceededAt}]`.
5. Next `action="status"` shows: `api ✓ (after 2 attempts)`.

### 9.3 Mid-work infrastructure change (add redis)

1. Work session active for web, api.
2. User: "add redis". LLM calls `action="start" workflow="bootstrap" intent="add redis"`.
3. Bootstrap session created (stateful). Work session still alive — different layer.
4. `action="status"` shows both, bootstrap primary.
5. Bootstrap completes → transition message prompts strategy for redis (if non-managed) and resume/restart work session.
6. LLM calls `action="start" workflow="develop" intent="wire redis into api"`.
7. Work session file overwritten. New services list includes redis. History from web/api deploys is lost (different intent). Graceful — it's a new task.

### 9.4 Parallel instances

1. Instance A (PID 1001): work session file `work/1001.json`, registry entry
   `work-1001`.
2. Instance B (PID 1002): `work/1002.json`, registry entry `work-1002`.
3. Each call to `action="status"` reads `work/{os.Getpid()}.json`
   exclusively, so each instance sees only its own session.
4. Zero cross-contamination in work layer. Infrastructure layer
   (bootstrap/recipe) uses existing hostname locks.
5. If both try `zerops_deploy web`: Zerops platform handles (last deploy wins
   on service; MCP does not mediate). Out of MCP scope.

### 9.5 Next-day re-entry

1. Day 1 evening: user closes Claude Code. Work session file `work/{oldpid}.json` persists.
2. Day 2 morning: user opens Claude Code. New PID 2001.
3. `NewEngine()` scans registry, finds `work-1001` with dead PID.
4. **Work sessions are NOT claimed.** Cleanup: file deleted, registry entry removed. Log: "Cleaned orphan work session from PID 1001".
5. LLM starts fresh briefing for day 2 task.
6. Rationale: work session is ephemeral task state; work-in-progress survives
   in git and on filesystem. Carrying stale intent across days is worse than
   starting clean.

### 9.6 Process crash mid-work session (same user session)

1. Claude Code session ongoing. MCP process crashes mid-deploy.
2. Claude Code restarts MCP process — new PID 1050.
3. `NewEngine()` sees dead `work-1001` entry. Cleans it.
4. LLM's next tool call gets "no work session" guidance.
5. LLM calls `action="start"` → work session re-created. History lost.
6. **Trade-off accepted:** losing deploy history on crash is better than the
   complexity of claim-on-boot for work sessions (would need PID-lineage
   tracking which is infeasible in Claude Code's model).
7. The actual code work survives on SSHFS mount; the deploy status can be
   re-verified via `zerops_verify`.

### 9.7 push-dev → cicd switch mid-task

1. Work session active, strategy=push-dev. Deployed web.
2. User decides to set up CI/CD. LLM calls `action="strategy" strategies={web: push-git}`.
3. ServiceMeta updated, `StrategyConfirmed=true`.
4. Work session unchanged (strategy not stored there).
5. Next `action="status"` reads strategy fresh → shows `web: push-git`.
6. `suggestedNext` in status updates: "push-git strategy — use zerops_workflow action=start workflow=cicd next".

---

## 10. Reliability Analysis

### 10.1 Compaction survival

**Signal path:** The static MCP `Instructions` (delivered once at client
init) always teach the LLM to call `zerops_workflow action="status"`
first. Because that text is part of the client's permanent session state
with the MCP server, it is not lost to transcript compaction.
`action="status"` assembles the Lifecycle Status block from fresh reads
of the work session file + ServiceMeta + live services on every call.

**Recovery mechanism:** `action="status"` is the single deterministic
recovery call. A post-compaction LLM consults the unchanged init
instructions, issues `action="status"`, and receives structured state
including `suggestedNext`. No per-response injection path is needed and
none is architecturally available under the current MCP protocol.

### 10.2 Restart survival

| Entity | Survives PID restart? | Mechanism |
|--------|----------------------|-----------|
| ServiceMeta | Yes | File on disk, read on every call |
| Bootstrap session | Yes | Registry claim-on-boot (existing) |
| Recipe session | Yes | Registry claim-on-boot (existing) |
| Work session | **No — by design** | Orphan cleanup. Work survives in git/filesystem. |

This is an explicit design choice. Work sessions are per-LLM-task. Trying to
reclaim them across process restarts creates more confusion than value.

### 10.3 Parallel instance safety

| Shared resource | Protection | Outcome |
|-----------------|-----------|---------|
| `.zcp/state/registry.json` | flock (exclusive, 5s timeout) | Safe under contention |
| `.zcp/state/services/{hostname}.json` | flock write lock (added) | Safe; last write wins for same hostname, but with lock ordering |
| `.zcp/state/work/{pid}.json` | Per-PID, own file | Zero collision |
| `.zcp/state/sessions/{sessionID}.json` | Per-session owner | Existing ownership semantics |
| Hostname locks (bootstrap) | Registry-based | Existing |

**Removed:** `.zcp/state/active_session` file (source of collision today).

### 10.4 State drift prevention

Work Session stores **only** what does not live elsewhere. Strategy, mode,
service status, env vars, ports are read fresh from ServiceMeta + API every
time. The class of bugs catalogued in `archive/audit-session-design.md`
(frozen session fields
diverging from truth) cannot occur.

### 10.5 Silent failure prevention

Every tool side-effect is logged to the Work Session. Deploy failures are
visible in `deploys[hostname].Error`. Verify failures in
`verifies[hostname].Summary`. The LLM always has a complete log of what
was attempted and what failed.

### 10.6 Loop closure guarantees

Explicit close: `action="close" workflow="develop"` — clean, intentional.

Auto-close heuristic: when every service in scope has a succeeded deploy
and passed verify, session marks itself closed. Next `action="status"`
call nudges the LLM toward the next task.

Fallback: LLM abandons session silently. On next-day restart, orphan is
cleaned. No permanent state pollution.

---

## 11. Migration & Implementation Plan

Phased to keep the main branch green at every step.

### Phase 1 — Infrastructure (no behavior change)

1. Add `WorkSession` type + `.zcp/state/work/` directory.
2. Add registry support for `work-{pid}` entries.
3. Add `BuildWorkSessionBlock` (renders the Lifecycle Status block; called
   by `action="status"`, not by the init-only `Instructions` path).
4. Migration: on first boot, scan `.zcp/state/develop/*.json` (old markers),
   delete them. Ignore any `.zcp/state/active_session` file — delete on boot.

Tests: WorkSession atomic write, registry round-trip, block rendering.

### Phase 2 — Engine integration

1. Wire `action="start" workflow="develop"` to create Work Session.
2. Wire `action="status"` (no workflow arg) to dispatch to Work Session
   when present.
3. Wire `action="close" workflow="develop"` — new action.
4. Update `NewEngine()` registry scan to prune dead work entries.
5. Remove `active_session` file writes/reads (use registry exclusively).

Tests: full session lifecycle (create, update, auto-close, explicit close),
multi-PID isolation, registry cleanup on boot.

### Phase 3 — Tool side-effects

1. `zerops_deploy` writes `DeployAttempt`.
2. `zerops_verify` writes `VerifyAttempt`.
3. Require Work Session for mount + deploy + verify (reject with clear
   error if absent).
4. Evaluate auto-close heuristic after each update.

Tests: each tool records correctly, auto-close triggers correctly, failure
paths recorded.

### Phase 4 — Status block + briefing

1. Wire `handleLifecycleStatus` to compose the Lifecycle Status block
   from the work session + ServiceMeta + live services (no init-only
   instructions path — MCP's `Instructions` field is static per
   connection).
2. Update `BuildTransitionMessage` (bootstrap → develop) to script strategy
   + start.
3. Update briefing to return same text + "Work session created" confirmation.

Tests: status block snapshot tests, briefing text verified per
strategy.

### Phase 5 — Test suite reconciliation

1. Delete tests asserting `DeployState` / `DeployTarget` (dead structures).
2. Update tool tests for new side-effects.
3. Integration tests for full flow (bootstrap → strategy → work session →
   deploy → verify → auto-close → next task).
4. E2E tests for: compaction simulation, parallel instance isolation,
   process restart.

### Phase 6 — Documentation

1. Promote this doc to `docs/spec-work-session.md` (done).
2. Archive obsolete lifecycle docs to `docs/archive/` (done):
   - `spec-develop-flow-current.md` — pre-refactor.
   - `spec-lifecycle-analysis.md` — bugs 2/3/4, gaps 9.1–9.6 resolved by
     this spec.
   - `audit-session-design.md` — findings implemented.
   - `audit-lifecycle-team-findings.md` — 5-agent audit report.
3. Update `docs/spec-bootstrap-flow-current.md` for the new transition
   message (done).
4. Update `CLAUDE.md` Architecture table: add `WorkSession` reference,
   drop the legacy `DeployState` entry (done).

---

## 12. Explicit Non-Goals

- **Recipe state staleness on long runs** — separate concern, not addressed
  here.
- **Recipe 10-iteration hard cap** — retained unchanged.
- **Bug 1 (abandoned bootstrap orphan metas)** — separate fix, pre-existing.
- **Cross-instance `zerops_deploy` collision** — platform concern.
- **Enforcement gates on develop** — explicitly rejected. Work session is
  advisory; LLM retains full discretion.
- **Claiming work sessions across PIDs** — rejected. Work sessions die with
  their process; code work survives in git/filesystem.

---

## 13. How It All Fits Together

The user's stated goal is **a reliable init → work → deploy loop that
survives long LLM work, context compaction, and multiple parallel Claude
Code instances**. The redesign delivers this by:

1. **Init is scripted.** Bootstrap completion message walks the LLM through
   strategy selection and work session creation as numbered steps. No
   ambiguity at the entry point.

2. **Work is observable.** `zerops_workflow action="status"` is the single
   canonical re-orientation call; the MCP init instructions teach the LLM
   to invoke it first. Compaction cannot erase the signal because the
   init instructions are owned by the MCP client session, not the
   transcript, and `status` reads state fresh from disk on every call.

3. **Deploy is recorded.** Every `zerops_deploy` / `zerops_verify` appends
   to the work session. The LLM sees per-service progress without re-asking
   the platform.

4. **Close is explicit with safety net.** `action="close"` is the intended
   terminator. Auto-close catches LLMs that forget. Next-task loop
   (`action="start"`) is a one-call restart.

5. **Parallelism is safe.** Work sessions are per-PID files. Registry is
   the single source of session ownership, with proper locking. No
   `active_session` collision. No cross-instance state confusion.

6. **Long work is stable.** State persists across any number of tool calls.
   Compaction-safe. The only loss-vector is process crash, which loses a
   small history window (code survives in git).

The result is a model where the LLM can **always answer these three
questions with one `action="status"` call** — a call the static init
instructions teach it to make first on every new task and after
compaction:
- *What am I working on?* → Work session intent.
- *Where am I in the lifecycle?* → deploys + verifies + suggestedNext.
- *What's my next action?* → suggestedNext or closure nudge.

Current architecture has the init instructions explicitly point at
`action="status"` as the canonical orientation call, so all three
questions resolve in one tool round-trip even after full compaction.
