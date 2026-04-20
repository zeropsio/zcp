# ZCP Scenarios — Exhaustive Walkthrough

> **Status**: Authoritative. Phase 7 of the instruction-delivery rewrite landed; this document is the acceptance reference for the envelope → plan → atoms pipeline.
> **Scope**: Every valid state, phase, and transition the ZCP MCP server must handle, with envelope → plan → atoms → user-output mapping.
> **Date**: 2026-04-19
> **Companion**: `plans/instruction-delivery-rewrite.md` (architecture reference and implementation history).

This document enumerates every scenario ZCP must handle. For each scenario it specifies:

- The trigger (user intent or tool call).
- The resulting `StateEnvelope` (key fields).
- The `Plan` produced (Primary / Secondary / Alternatives).
- The knowledge atoms synthesized.
- The rendered output the LLM sees.

Executable counterpart: `internal/workflow/scenarios_test.go` pins the canonical S1–S13 flows as table-driven tests. Any scenario not listed here is either out of scope (§9) or must be added before it is implemented.

Vocabulary: `StateEnvelope`, `Plan`, `NextAction`, `KnowledgeAtom`, `AxisVector`, `RecipeMatch`, `Route` — see `plans/instruction-delivery-rewrite.md` §4.

---

## 0. State Machine

```
                   ┌──── zerops_workflow action=start workflow=bootstrap
                   ▼
 ┌──────┐   start   ┌───────────────────┐   auto-close    ┌──────┐
 │ idle │──────────▶│ bootstrap-active │────────────────▶│ idle │
 └──────┘           └───────────────────┘                 └──────┘
     │                       │                                ▲
     │ start workflow=develop│ abort/error                    │
     ▼                       ▼                                │
 ┌──────────────────┐   auto-complete   ┌──────────────────┐  │
 │ develop-active   │──────────────────▶│ develop-closed-  │──┤
 │                  │                   │ auto             │  │
 └──────────────────┘   explicit close  └──────────────────┘  │
     │                       ▲            │                   │
     │ iteration-cap         │            │ close             │
     ▼                       │            ▼                   │
 (session closed with        │        ┌──────┐                │
  CloseReason=iteration-cap) │        │ idle │────────────────┘
                             │        └──────┘
                             │
 ┌──────┐  start workflow=recipe   ┌──────────────────┐       │
 │ idle │────────────────────────▶│ recipe-active    │───────┘
 └──────┘                          └──────────────────┘
```

**Invariant**: Only one non-idle workflow per PID at a time. A second `start` while non-idle returns `ErrWorkflowActive` with a typed `Plan` offering close-vs-continue. We never silently run parallel sessions in one PID.

---

## 1. Phase: `idle`

User's first action in any session is always `zerops_workflow action=status`. The response shape:

- `envelope.Phase = idle`
- `guidance` = atoms filtered on `phases=[idle]`
- `plan` = branch determined by `envelope.Services`

### 1.1 No services in project (brand-new)

- **Envelope**: `Services=[], Project.ID=set|empty`.
- **Plan.Primary** = `{tool: zerops_workflow, args: {action: start, workflow: bootstrap}, rationale: "Project has no services."}`
- **Plan.Alternatives** = `[]` (no realistic alternative).
- **Atoms** (~2): `idle-bootstrap-entry`, `idle-{container|local}-context`.
- **User sees**: *"Phase: idle. Services: none. Next: Start bootstrap."*

### 1.2 Managed-only project (DB exists, no runtime)

- **Envelope**: `Services=[{db, managed, ACTIVE}]`.
- **Plan.Primary** = `{start bootstrap, rationale: "No runtime to host application code; only managed dependency exists."}`
- **Plan.Alternatives** = `[{action: start, workflow: develop, rationale: "If you only need to manage the managed service (scale/env), start develop targeting it."}]`
- **Atoms**: `idle-managed-only-hint`.

### 1.3 All services bootstrapped (ServiceMeta complete for every non-managed service)

- **Envelope**: `Services=[{laraveldev, mode=dev, strategy=push-dev}, {laravelstage, mode=stage, strategy=push-git}, {db, managed}]`.
- **Plan.Primary** = `{start develop, rationale: "All services ready for code work."}`
- **Plan.Alternatives** = `[{start bootstrap, rationale: "Add more services."}]`
- **Atoms**: `idle-develop-entry`.

### 1.4 Mixed — some bootstrapped, some unmanaged

- **Envelope**: `Services=[{laraveldev, bootstrapped}, {newservice, no meta, unmanaged}, {db, managed}]`.
- **Plan.Primary** = `{start develop, args.intent="...", rationale: "Bootstrapped services ready for code tasks; unmanaged runtime auto-adopts on develop start."}`
- **Plan.Secondary** = `nil`.
- **Plan.Alternatives** = `[{adopt unmanaged runtimes, rationale: "Write ServiceMeta without opening a develop session."}, {start bootstrap, rationale: "Add more services before working."}]`
- **Atoms**: `idle-adopt-on-develop-start`.

### 1.5 Unmanaged-only (all runtimes lack meta)

- **Envelope**: `Services=[{svc1, no meta}, {svc2, no meta}]`.
- **Plan.Primary** = `{adopt unmanaged runtimes, args: {action: start, workflow: develop, intent: "adopt"}, rationale: "Existing runtime services have no bootstrap metadata yet."}`
- **Plan.Alternatives** = `[]` (adopt is the only viable entry).

### 1.6 Bootstrapped but strategy unset

- **Envelope**: `Services=[{svc, bootstrapped, strategy=unset}]`.
- **Plan.Primary** = `{start develop, rationale: "Service ready; strategy gate fires once the develop session is open."}`
- **Plan.Alternatives** = `[{add more services, rationale: "Add additional managed or runtime services."}]`
- **Atoms**: `develop-strategy-unset` surfaces the gate in the develop-active guidance body once the session starts — no idle-phase atom.

  *Note: `BuildPlan` stays a pure envelope-shape dispatch; strategy-required is expressed through the atom layer, not through a distinct branch in `planIdle`.*

---

## 2. Phase: `bootstrap-active`

Entered via `zerops_workflow action=start workflow=bootstrap intent="..."`. `Route` is selected once at start and persists for the session.

Route selection (§8.1 of the plan):

1. If existing has any non-system, non-managed service → `RouteAdopt`.
2. If intent matches a recipe with `Confidence ≥ 0.85 AND Viable == true` → `RouteRecipe`.
3. Otherwise → `RouteClassic`.

### 2.1 Route A — Recipe match

**Precondition**: intent ↔ recipe slug match with `Confidence ≥ 0.85 AND Viable == true`. Example: `"weather dashboard in Laravel"` → `laravel-dashboard` @ 0.91, 412 lines, has overview+deploy+verify sections.

Steps executed sequentially:

| Step | Envelope.BootstrapStep | Plan.Primary | Atoms shown |
|---|---|---|---|
| `import` | `route=recipe, step=import` | `zerops_import args={yaml: <recipe-body>}` | `recipe-import-overview`, `recipe-{slug}-overview` |
| `wait-active` | `step=wait-active, pending=[list]` | `zerops_discover args={}` (poll) | `wait-active-hint` |
| `verify-deploy` | `step=verify-deploy` | `zerops_deploy args={hostname: runtime-svc}` | `bootstrap-verify-server-{mode}` |
| `verify` | `step=verify` | `zerops_verify args={hostname: runtime-svc}` | `bootstrap-verify-checks` |
| `close` | `step=close, all-green=true` | `zerops_workflow action=close workflow=bootstrap` | `bootstrap-close-summary` |

**After close** → envelope returns to `idle` with services bootstrapped → §1.3 takes over.

#### 2.1.1 Route A with step failure

Example: `verify` fails because managed DB is unreachable from runtime.

- **Envelope**: `step=verify, StepProgress.Failures=1`.
- **Plan.Primary** = `{zerops_logs, args: {hostname: runtime, severity: error, since: 5m}, rationale: "Diagnose the verify failure."}`
- **Plan.Secondary** = `{zerops_verify, args: {...}, rationale: "Re-run verify after fix."}`
- **Atoms**: iteration tier 1 atom (`deploy-iter1-diagnose`).
- On 3rd failure → systematic tier (tier 2).
- On 5th failure → session closes with `CloseReason=iteration-cap`; user must restart explicitly.

### 2.2 Route B — Classic Infra

**Precondition**: no viable recipe match, no existing non-managed services. Example: `"Rust service with custom protocols"`.

| Step | Envelope.BootstrapStep | Plan.Primary | Notes |
|---|---|---|---|
| `plan` | `step=plan, proposed=[rust@1.82, redis@7]` | `zerops_import args={yaml: <generated>}` after user approval | Plan generated from intent via internal heuristic; user confirms or edits. |
| `import` | `step=import` | poll via `discover` | |
| `wait-active` | `step=wait-active` | `zerops_discover` | |
| `verify-deploy-per-runtime` | `step=verify-deploy, current=rust` | `zerops_deploy args={hostname: rust}` | Iterates across dynamic runtimes in scope. Managed services skipped. |
| `verify` | `step=verify` | `zerops_verify` | |
| `write-metas` | `step=write-metas` | (internal, no LLM action) | Writes `ServiceMeta` per runtime: `mode=dev, strategy=unset`. |
| `close` | `step=close` | `zerops_workflow action=close workflow=bootstrap` | |

**After close** → back to idle with `strategy=unset` on runtimes → §1.6 kicks in.

#### 2.2.1 Route B — only managed services proposed

Edge case: intent = `"I need a PostgreSQL instance"`. Plan step generates `[postgresql@16]`, no runtimes.

- `verify-deploy-per-runtime` is a no-op (no runtime to deploy).
- `verify` checks DB is ACTIVE and accepts connections from the project network.
- `write-metas` skipped (no runtime needs meta).
- **User sees**: *"Bootstrap complete. No runtime to develop against — add one via bootstrap, or use `zerops_manage` to configure the managed service."*
- **Plan.Primary** at close = `{start bootstrap, rationale: "Add a runtime to host application code."}`.

### 2.3 Route C — Adopt Existing

**Precondition**: ≥1 non-system, non-managed service without complete ServiceMeta.

| Step | Envelope.BootstrapStep | Plan.Primary | Notes |
|---|---|---|---|
| `discover` | `step=discover, adoptable=[svc1, svc2]` | `zerops_discover` | |
| `prompt-modes` | `step=prompt-modes, pending=[svc1]` | `zerops_workflow action=adopt-answer args={hostname: svc1, mode: dev, strategy: push-dev}` | One iteration per adoptable service. |
| `write-metas` | `step=write-metas` | (internal) | |
| `verify` | `step=verify` | `zerops_verify args={hostname: svc1}` | Runs against existing code, no new deploy. |
| `close` | `step=close` | `zerops_workflow action=close workflow=bootstrap` | |

#### 2.3.1 Route C — fast-path (all services managed)

**Envelope**: `Services=[{db, managed}, {cache, managed}]`.

- `discover`, `prompt-modes`, `write-metas` all no-op.
- `verify` step confirms managed services are ACTIVE.
- `close` immediate.
- **User sees**: *"Adopt complete. No runtimes to bootstrap. Next: add a runtime via bootstrap, or manage existing services directly."*

### 2.4 Recipe viability gate rejection (Route A → B fallthrough)

- **Trigger**: intent matches `laravel-minimal` @ 0.92, but recipe is 6 lines → `Viable=false`.
- **Flow**: Route A rejected, fall through to Route B.
- **Envelope after start**: `Phase=bootstrap-active, Route=classic, RecipeMatch={slug: laravel-minimal, confidence: 0.92, viable: false, reasons: ["content under 200 lines", "missing sections: deploy, verify"]}`.
- **User sees**: *"Matched 'laravel-minimal' but recipe is incomplete. Continuing with classic bootstrap."*
- **No silent failure**: rejection reason is surfaced to user.

### 2.5 Bootstrap active, user calls `start` again

- **Result**: `ErrWorkflowActive` error with typed payload.
- **Plan.Primary** = `{close current bootstrap, rationale: "Only one workflow active per PID."}`
- **Plan.Secondary** = `{continue (no tool needed — current state is already bootstrap-active), rationale: "Resume where you left off."}`
- **No fallback**: we never silently start a second session.

### 2.6 Bootstrap active, iteration cap reached

- At iteration 5, tier 3 atom renders (STOP).
- Session closes with `BootstrapSession.ClosedAt=now, CloseReason=iteration-cap`.
- **Envelope**: `Phase=idle`, service in partial state.
- **Plan.Primary** = `{start bootstrap, rationale: "Previous session aborted at iteration cap. Review logs and restart."}`
- **User sees**: summary of 5 attempts + error from each + request for manual decision.

---

## 3. Phase: `develop-active`

Entered via `zerops_workflow action=start workflow=develop intent="..."`. Produces a `WorkSession` keyed by PID.

Flow: edit code → `zerops_deploy` (strategy-dispatched) → `zerops_verify` → pass (auto-close eligible) or fail (iteration tier).

### 3.1 Start — envelope after start

- **Envelope**: `Phase=develop-active, WorkSession={intent, services, deploys={}, verifies={}, created_at}`.
- **Plan.Primary** = `{zerops_deploy, args: {hostname: first-service}, rationale: "Ready for first deploy. Ensure edits are complete."}`
- **Atoms** depend on each service's `{mode, strategy, runtime-class, environment}` cell. See matrix below.

### 3.2 Strategy × runtime × environment matrix

Synthesizer runs a per-service pass; each service's atoms are prepended with its hostname.

Valid cells (invalid combinations error at envelope validation):

| Cell | Mode | Strategy | Runtime | Env | Key atoms |
|---|---|---|---|---|---|
| C1 | dev | push-dev | dynamic | container | `develop-push-dev-ssh-container`, `develop-dynamic-start-after-deploy-container` |
| C2 | dev | push-dev | dynamic | local | `develop-push-dev-ssh-local`, `develop-dynamic-start-after-deploy-local` |
| C3 | dev | push-dev | static | container | `develop-push-dev-static-container` (no post-deploy start) |
| C4 | dev | push-dev | static | local | `develop-push-dev-static-local` |
| C5 | dev | push-dev | implicit-webserver | container | `develop-push-dev-phpnginx-container` (php-nginx auto-starts via apache) |
| C6 | dev | push-dev | implicit-webserver | local | `develop-push-dev-phpnginx-local` |
| C7 | stage | push-git | dynamic | container | `develop-push-git-container`, `develop-stage-build-verification` |
| C8 | stage | push-git | dynamic | local | `develop-push-git-local`, `develop-stage-build-verification` |
| C9 | stage | push-git | static | container | `develop-push-git-container-static` |
| C10 | stage | push-git | static | local | `develop-push-git-local-static` |
| C11 | simple | push-dev | dynamic | container | `develop-simple-mode-overview`, `develop-dynamic-start-after-deploy-container` |
| C12 | simple | push-dev | dynamic | local | `develop-simple-mode-overview`, `develop-dynamic-start-after-deploy-local` |
| C13 | any | manual | any dynamic | any | `develop-manual-strategy-external-deploy` (no `zerops_deploy` call; user deploys externally then `zerops_verify`) |
| C14 | any | unset | any | any | `develop-strategy-unset` — Plan.Primary stays at the envelope-shape dispatch (deploy / verify / close); the atom body surfaces `zerops_manage action="strategy"` as the gate the agent must honour before attempting a deploy |
| C15 | n/a | n/a | managed | any | `develop-managed-no-deploy` — no deploy, verify = connection check |

Invalid combinations (e.g. stage + push-dev, dev + push-git, simple + stage) are blocked at pre-flight with explicit error.

### 3.3 Deploy state transitions per service

```
┌────────────┐  deploy   ┌────────────┐  verify  ┌────────────┐
│ pre-deploy │──────────▶│ deployed   │─────────▶│ verified   │
└────────────┘           └────────────┘          └────────────┘
     ▲                         │                       │
     │                         │ verify fail           │ all green
     │                         ▼                       ▼
     │                  ┌────────────┐          (auto-close
     │                  │ failed     │           eligible)
     │                  │ iter N     │
     │                  └────────────┘
     │                         │
     │  iter < cap: fix+retry  │
     └─────────────────────────┘
```

`BuildPlan` inspects last attempt per service:

| Deploy state | Plan.Primary |
|---|---|
| pre-deploy | `zerops_deploy args={hostname}` |
| deploy in progress | `zerops_process args={process-id}` (wait) |
| deployed, verify pending | `zerops_verify args={hostname}` |
| verified ∧ all services green | `zerops_workflow action=close workflow=develop` (auto-close) |
| verified ∧ others pending | `zerops_deploy` or `zerops_verify` on next service |
| failed deploy, any iter | `zerops_deploy args={hostname}` — retry same action, atom body carries tier guidance |
| failed verify, any iter | `zerops_verify args={hostname}` — retry same action, atom body carries tier guidance |

Iteration tier (tier-1 diagnose, tier-2 systematic check, tier-3 STOP) rides along via atoms — the Plan.Primary does not change shape as iterations accumulate. On iteration 5 the session auto-closes with `CloseReason=iteration-cap`, and the next status call reverts to the idle-phase dispatch.

### 3.4 Auto-close

Triggers when every `WorkSession.Services` has `Deploys[last].Success=true AND Verifies[last].Success=true`.

- **Envelope**: `Phase=develop-closed-auto, CloseReason=auto-complete`.
- **Plan.Primary** = `{close, rationale: "Task complete."}`
- **Plan.Secondary** = `{start develop with new intent, rationale: "Begin next task."}`
- **Atoms**: `develop-closed-auto-summary`.

### 3.5 Iteration-cap close

- **Envelope**: `Phase=idle` (session closed); previous `WorkSession.CloseReason=iteration-cap`.
- **Plan.Primary** = `{zerops_logs, rationale: "Review full attempt history before restart."}`
- **Plan.Secondary** = `{start develop, rationale: "Restart only after understanding the failure mode."}`
- **Atom**: `develop-iter-cap-postmortem` renders summary of 5 attempts.

### 3.6 Explicit close (user-initiated mid-work)

- **Envelope**: `Phase=idle, WorkSession.CloseReason=user-close`.
- **Plan.Primary** = depending on service state (§1.3–1.5).

### 3.7 `start develop` while develop-active

- **Result**: `ErrWorkflowActive`.
- **Plan.Primary** = `{close current develop, rationale: "Only one develop session per PID."}`
- **Plan.Secondary** = `{continue current session, rationale: "Resume in-progress intent: <intent>"}`

### 3.8 Compaction recovery (same PID, context compressed)

- **State dir read**: `WorkSession` for current PID reloaded from disk.
- **Envelope**: full reconstruction — phase, services, progress, deploy/verify attempts.
- **Plan.Primary** = recomputed from envelope.
- **Atoms**: `develop-compaction-recovery-hint`.

Compaction-safety invariant: envelope is deterministic, so recovered guidance is byte-identical to pre-compaction guidance.

### 3.9 Cross-PID (new process, same project)

- **State dir read**: no `WorkSession` for this PID.
- **Envelope**: `Phase=idle` from this PID's POV.
- **Plan.Primary** = idle branch logic.
- Stale sessions from other PIDs are garbage-collected by age (separate concern).

---

## 4. Phase: `develop-closed-auto`

Transient phase between auto-close trigger and explicit close tool call. Envelope persists until user acts.

- **Plan.Primary** = close.
- **Plan.Secondary** = start next develop (fresh intent).
- **Plan.Alternatives** = `[{stop here, rationale: "No further action needed."}]`.
- **Rendered Next**:
  ```
  Next:
    ▸ Primary: Close — zerops_workflow action="close" workflow="develop"
    ◦ Secondary: Start next task — zerops_workflow action="start" workflow="develop" intent="..."
  ```

---

## 5. Phase: `recipe-active`

Entered via `zerops_workflow action=start workflow=recipe slug=<recipe>`. Used for *building* recipe repo files — distinct from bootstrap Route A which *consumes* recipes for infrastructure.

| Step | Plan.Primary | Atoms |
|---|---|---|
| `inspect` | `zerops_discover` | `recipe-inspect-overview` |
| `generate-files` | (internal writes; LLM reviews + edits) | `recipe-file-generation-rules` |
| `verify-structure` | `zerops_verify` or internal structural check | `recipe-structure-checks` |
| `close` | `zerops_workflow action=close workflow=recipe` | |

Iteration tier applies identically to any failure in verify.

---

## 6. Error & edge cases

### 6.1 Auth failure

- **Trigger**: any tool with `ZCP_API_KEY` missing or invalid.
- **Response**: `Error{code: AUTH_REQUIRED, message: ..., plan: {Primary: "Set ZCP_API_KEY and retry"}}`.
- **No fallback**: we do not substitute public API or skip the call.

### 6.2 Project not found / not bound

- **Trigger**: tool call without active project context.
- **Envelope**: `Phase=idle, Project={ID: empty, Name: empty}, Services=[]`.
- **Plan.Primary** = `{zerops_manage action=bind-project args={id|name}, rationale: "Bind a project before workflow actions."}`

### 6.3 Platform API rate-limit or transient failure

- **Response**: error propagated with `retry-after` if present.
- **Plan.Primary** = `{retry same tool call, rationale: "Platform transient error. Back off and retry."}`
- **No silent retry in-tool**: LLM decides when to retry based on rationale.

### 6.4 State dir corrupted or unreadable

- **Response**: error `STATE_CORRUPT` with path.
- **Plan.Primary** = `{zerops_manage action=reset-state args={confirm: true}, rationale: "State dir unreadable at <path>. Reset required."}`
- User must explicitly confirm reset.

### 6.5 Service deleted externally mid-work-session

- **Envelope**: live API shows service absent; `WorkSession.Services` still references it.
- **Detection**: `ComputeEnvelope` flags missing services as `GhostServices=[<names>]`.
- **Plan.Primary** = `{close current develop, rationale: "Referenced services no longer exist on platform."}`
- **Plan.Secondary** = `{bootstrap to recreate, rationale: "Rebuild infrastructure if accidental."}`

### 6.6 zerops.yaml missing at repo root (local env)

- **Trigger**: `zerops_deploy` in local env with no `zerops.yaml`.
- **Response**: error `CONFIG_MISSING`.
- **Plan.Primary** = `{start bootstrap, rationale: "zerops.yaml is generated by bootstrap. Cannot deploy without it."}`

### 6.7 zcli not installed (local env)

- **Response**: error `ZCLI_MISSING` with install instructions.
- **Plan.Primary** = `{install zcli, rationale: "Deploy requires zcli locally."}`
- No fallback to alternative deploy method.

### 6.8 Container env, self-service not registered

- **Trigger**: container mode detected but `SelfService.Hostname` empty.
- **Response**: `Phase=idle` with warning atom `env-container-self-unknown-warning`.
- **Plan.Primary** = `{zerops_manage action=set-self-service, rationale: "Identify this container before other workflow actions."}`

### 6.9 SSHFS mount out of date after deploy (container env)

- **Detection**: post-deploy, `zerops_discover` hash differs from mount contents.
- **Response**: atom `env-container-remount-after-deploy` rendered in develop-active guidance.
- **Plan.Primary** = unchanged; secondary atom advises remount.

### 6.10 Strategy mismatch with mode

- **Trigger**: user calls `zerops_manage action=set-strategy args={hostname: devservice, strategy: push-git}`. Dev mode services don't use push-git.
- **Response**: error `STRATEGY_INVALID_FOR_MODE` with allowed set.
- **Plan.Primary** = `{retry with valid strategy, rationale: "Dev mode supports push-dev or manual only."}`

### 6.11 Managed service deploy attempt

- **Trigger**: `zerops_deploy args={hostname: db}` where db is PostgreSQL.
- **Response**: error `NO_DEPLOY_FOR_MANAGED`.
- **Plan.Primary** = `{zerops_manage for managed service operations, rationale: "Managed services have no deploy; use scale/env instead."}`

---

## 7. Cross-cutting concerns

### 7.1 Environment detection

Detected once at server init. Drives:

- Layer 1 CLAUDE.md is identical either way (only platform invariants).
- Layer 2 atoms filter on `environments=[...]`.
- Tool registration varies: container registers `RegisterDeploySSH` + `RegisterDevServer`; local registers `RegisterDeployLocal`.
- `SelfService` populated only in container env.

### 7.2 Deterministic envelope serialization

- Services sorted by hostname.
- Attempt lists sorted by `At` timestamp.
- Maps encoded with sorted keys.
- Timestamps in RFC3339 UTC.

Enables two-call identity: `Synthesize(env)` twice returns byte-identical output.

### 7.3 Placeholder substitution

Atoms containing `{hostname}`, `{stage-hostname}`, `{project-name}`, `{service-mode}`, `{service-strategy}`, `{intent}` are rendered with envelope values. Unknown placeholder = render-time error.

Per-service rendering passes substitute each service's values into per-service atoms. Same atom may render N times with different substitutions.

### 7.4 Work session durability

- `WorkSession` persists to `state-dir/work/{pid}.json` on every envelope-changing operation.
- Multiple zcp processes = multiple PID files; they don't interact.
- Stale sessions (PID no longer running) detected on status read and pruned.

### 7.5 Tool annotations unchanged

`internal/tools/annotations.go` metadata (`read_only`, `destructive`, `idempotent`, `open_world`) is preserved. Rewrite changes tool response *shapes*, not tool *metadata*.

### 7.6 `zerops_knowledge` after rewrite

Tool accepts a query or axis-tuple, runs `Synthesize` against a synthetic envelope matching the query, returns rendered atoms. This is how the LLM retrieves on-demand knowledge not already in the status response.

---

## 8. End-to-end walkthroughs

### 8.1 Fresh project, Laravel dashboard intent

```
User: "Create a weather dashboard in Laravel"

LLM calls: zerops_workflow action=status
  → Response:
    ## Status
    Phase: idle
    Services: none
    Next:
      ▸ Primary: Start bootstrap — zerops_workflow action="start" workflow="bootstrap" intent="..."

LLM calls: zerops_workflow action=start workflow=bootstrap intent="weather dashboard in Laravel"
  → Response:
    ## Status
    Phase: bootstrap-active (route: recipe, match: laravel-dashboard @ 0.91)
    Next:
      ▸ Primary: Import recipe — zerops_import args={yaml: ...}

    Guidance:
      [recipe-import-overview atom body]
      [laravel-dashboard-overview atom body]

LLM calls: zerops_import ...
  → (proceeds through wait-active, verify-deploy, verify, close)

Eventually → Phase=idle, services bootstrapped → LLM starts develop.
```

### 8.2 Existing container env, mixed services

```
LLM calls: zerops_workflow action=status (first call in container env)
  → Response:
    ## Status
    Phase: idle
    Self: zcp-host
    Services:
      - laraveldev (php-nginx@8.3): mode=dev, strategy=push-dev
      - laravelstage (php-nginx@8.3): mode=stage, strategy=push-git, stage-of=laraveldev
      - newservice (nodejs@20): not bootstrapped — auto-adopted on develop start
      - db (mariadb@11): managed
    Next:
      ▸ Primary: Start develop — zerops_workflow action="start" workflow="develop" intent="..."
      ◦ Alternatives:
          - Add services — zerops_workflow action="start" workflow="bootstrap"

    Guidance:
      [idle-adopt-on-develop-start atom body]
      [env-container-sshfs-hint atom body]
```

### 8.3 Develop-active iteration 3 failure

```
LLM calls: zerops_verify args={hostname: laraveldev}
  → Response:
    ## Status
    Phase: develop-active (intent: "fix login flow bug")
    Progress:
      - laraveldev: deployed OK @ 3, verified FAIL @ 3 (iter 3)
    Next:
      ▸ Primary: Systematic check — review env vars, ports, bindings, deployFiles

    Guidance:
      [deploy-iter2-systematic-check atom body — tier-2 copy]
      [develop-push-dev-ssh-container atom body]

    (remaining session attempts: 2)
```

At iteration 5 → STOP + session closes with `iteration-cap`.

---

## 9. Out of scope

- **Multi-project** workflows within one zcp invocation (PID state is per-project).
- **Parallel deploys** to multiple services at once — plan is sequential per service.
- **CI/CD pipelines** as a primary workflow — `cicd.md` refactor scheduled in Phase 7.2, not core rewrite.
- **Recipe authoring UI** (`zcp sync recipe create-repo` etc.) — unaffected.
- **Eval pipeline** (`internal/eval`) — unaffected.
- **Stale WorkSession garbage collection** policy — handled separately from envelope logic.
