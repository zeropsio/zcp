# Work Session — ZCP Development Lifecycle

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
  teach the LLM to call this when state is unclear — after compaction or
  between tasks.

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
5. **Survives compaction by default.** The MCP init instructions teach
   `zerops_workflow action="status"` as the canonical recovery call, so
   even a freshly-compacted LLM has a single, deterministic re-orientation
   step.
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
    Strategy    string `json:"strategy,omitempty"`    // "zcli" (default zcli push) | "git-push" | "record-deploy" (external bridge)
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
LLM to call `action="status"` when state is unclear — after compaction or
between tasks; the handler reads the current work session file, service
metas, and live service list, and renders one of the cases below.

Content depends on active entities for the current PID:

### 5.1 No activity

Omit the block entirely. Base instructions still guide "start a develop
workflow for any code task."

### 5.2 Work session only

```
## Lifecycle Status
Work session active (1h 23m) — intent: "add login form"
  Services: web (closeMode=auto), api (closeMode=auto)
  Deploys: web ✓ 3m ago | api ✗ 2 attempts (last: build timeout 1m ago)
  Verifies: web ✓ 2m ago | api —
  → Next: fix build error on api, redeploy
```

### 5.3 Work session + bootstrap (concurrent)

```
## Lifecycle Status
Active workflow: bootstrap | session abc123 | step 2/3: provision
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

Infrastructure session is rendered without a Work Session block.

---

## 6. Workflow Actions — Updated Semantics

### 6.1 `action="start" workflow="develop"`

1. Gather ServiceMetas. If empty and live services exist → auto-adopt
   (bootstrap fast-path, transparent).
2. Filter to complete runtime metas.
3. Create Work Session for current PID — strategy is **not** a gate. If
   one already exists:
   - Same intent → return fresh briefing, do not reset history.
   - Different intent → return current state + asks: "Close current
     session first: `zerops_workflow action=close workflow=develop`".
4. Register in registry (`work-{pid}` entry).
5. Write `.zcp/state/work/{pid}.json`.
6. Return briefing via the atom pipeline. Strategy surfaces through
   atoms, not via a pre-create gate:
   - `deployStates: [never-deployed]` services → first-deploy branch
     atoms guide the agent through scaffold + write + deploy. The
     first deploy always uses the default self-deploy mechanism
     (`zerops_deploy targetService=X` with no strategy argument),
     regardless of the eventual persistent close-mode.
   - `deployStates: [deployed] + closeDeployModes: [unset]` services →
     the `develop-strategy-review` atom asks the agent to confirm an
     ongoing close-mode (`auto` / `git-push` / `manual`). Leaving it
     unset keeps the default mechanism but re-fires the atom every
     session until confirmed.
   - Confirmed close-mode unlocks the close-mode-specific atoms
     (`develop-close-mode-{auto,git-push,manual}-*`).

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
    {"hostname": "web", "closeDeployMode": "auto", "mode": "dev",
     "deploys": {"attempted": 1, "succeeded": 1, "lastError": ""},
     "verifies": {"attempted": 1, "passed": 1}},
    {"hostname": "api", "closeDeployMode": "auto", "mode": "dev",
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

On success, `RecordVerifyAttempt` calls `MarkServiceDeployed(stateDir, hostname)` to stamp `FirstDeployedAt` on the ServiceMeta — the signal that exits the first-deploy branch on the next session. The hostname lookup resolves via `findMetaForHostname` (direct file match OR `StageHostname` field scan), so verifying the stage half of a container+standard pair stamps the same dev-keyed meta file. Standard-mode first-deploy therefore exits regardless of which half the agent verified first (see `spec-workflows.md` D2c).

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

Next step:

   zerops_workflow action="start" workflow="develop"
     intent="<what you'll implement>"

Close-mode is NOT chosen at bootstrap. Develop owns the first deploy
with the default self-deploy mechanism regardless of the (still-empty)
`CloseDeployMode`. After the first deploy lands the
`develop-strategy-review` atom (priority 2) prompts the agent to pick
an ongoing close-mode via `action="close-mode" closeMode={…}` — three
orthogonal axes follow:

   - `close-mode`        — develop session's delivery pattern
                           (drives auto-close gating + per-mode atoms;
                           `action="close"` itself is always teardown):
                           auto / git-push / manual
   - `git-push-setup`    — provisions GIT_TOKEN / .netrc / remote URL,
                           stamps `GitPushState=configured`
   - `build-integration` — wires ZCP-managed CI:
                           none / webhook / actions
                           (requires `GitPushState=configured`)
```

For adoption flow (auto-adopt inside `handleDevelopStart`), the same
flow holds: bootstrap-side metas land with empty close-mode + git-push
state + build-integration; develop briefing surfaces the review atoms
post-first-deploy.

---

## 9. Scenario Walkthroughs — Post-Redesign

### 9.1 Fresh recipe adoption → work session → deploy → close

1. User imports recipe. Opens Claude Code. `zcp` starts (PID 1001).
2. LLM sees base instructions, no lifecycle hint (no work session yet).
3. LLM calls `action="start" workflow="develop" intent="build the app"`.
4. Server: no metas → auto-adopt → metas written with empty close-mode +
   git-push state + build-integration (per spec-workflows.md E2 / S2).
5. Work session created immediately. File `work/1001.json`. Registered.
   Briefing surfaces `develop-first-deploy-intro` (deployStates=never-deployed).
6. LLM codes on mount, using `action="status"` whenever it needs to check
   where it is.
7. Compaction at 1h. LLM's next instruction-following step calls
   `action="status"` (taught by the init instructions); the Lifecycle
   Status block re-orients it and it continues.
8. Deploys web → succeeded. Verifies web → passed.
9. Deploys api → succeeded. Verifies api → passed.
10. Post-first-deploy briefing surfaces `develop-strategy-review` atom
    (deployStates=deployed, closeDeployModes=unset). LLM offers the agent
    `action="close-mode" closeMode={web:auto, api:auto}` to commit.
11. Auto-close fires (close-mode now set, every scope service green).
    Next `action="status"` renders the "task complete, close or next"
    variant.
12. LLM calls `action="close"` → file deleted, summary returned.
13. For next task → repeat from step 3 (close-mode already set on metas,
    briefing skips review atom).

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

### 9.7 auto → git-push close-mode switch mid-task

1. Work session active, CloseDeployMode=auto. Deployed web.
2. User decides to switch close to git-push. LLM calls `action="close-mode" closeMode={web: git-push}`.
3. ServiceMeta updated, `CloseDeployModeConfirmed=true`.
4. Work session unchanged (close-mode not stored there).
5. Next `action="status"` reads close-mode fresh → shows `web: git-push`.
6. The next chained guidance points at `action="git-push-setup"` if `GitPushState != configured`; otherwise close runs git-push directly.

---

## 10. Reliability Analysis

### 10.1 Compaction survival

**Signal path:** The static MCP `Instructions` (delivered once at client
init) teach the LLM that `zerops_workflow action="status"` is the
canonical recovery call when state is unclear. Because that text is part
of the client's permanent session state with the MCP server, it is not
lost to transcript compaction. `action="status"` assembles the Lifecycle
Status block from fresh reads of the work session file + ServiceMeta +
live services on every call.

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
time. Frozen session fields cannot diverge from truth because the session
does not carry that truth.

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

## 11. Explicit Non-Goals

- **Recipe state staleness on long runs** — separate concern, not addressed
  here.
- **Recipe 10-iteration hard cap** is out of scope for this spec.
- **Cross-instance `zerops_deploy` collision** — platform concern.
- **Enforcement gates on develop** — Work session does not gate develop;
  it is advisory and the LLM retains full discretion.
- **Claiming work sessions across PIDs** — work sessions do not cross PID
  boundaries; they die with their process. Code work survives in
  git/filesystem.

---

## 12. How It All Fits Together

A reliable init → work → deploy loop must survive long LLM work, context
compaction, and multiple parallel Claude Code instances. The model
delivers this with:

1. **Scripted init.** Bootstrap completion scripts strategy selection and
   work session creation as numbered steps. No ambiguity at the entry
   point.

2. **Observable work.** Every tool response carries the work session when
   one is active. After compaction, the LLM calls
   `zerops_workflow action="status"` to re-orient; `status` reads state
   fresh from disk on every call, so the signal cannot be erased.

3. **Recorded deploys.** Every `zerops_deploy` / `zerops_verify` appends
   to the work session. The LLM sees per-service progress without re-asking
   the platform.

4. **Explicit close with safety net.** `action="close"` is the intended
   terminator. Auto-close catches LLMs that forget. Next-task loop
   (`action="start"`) is a one-call restart.

5. **Safe parallelism.** Work sessions are per-PID files. The registry is
   the single source of session ownership, with proper locking. No
   cross-instance state confusion.

6. **Stable long work.** State persists across any number of tool calls.
   Compaction-safe. The only loss-vector is process crash, which loses a
   small history window (code survives in git).

The LLM can answer these three questions with one `action="status"` call
after compaction or between tasks:
- *What am I working on?* → Work session intent.
- *Where am I in the lifecycle?* → deploys + verifies + suggestedNext.
- *What's my next action?* → suggestedNext or closure nudge.
