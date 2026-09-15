# ZCP Scenarios — Exhaustive Walkthrough

> **Scope**: Every valid state, phase, and transition the ZCP MCP server
> must handle, with envelope → plan → atoms → user-output mapping.

This document enumerates every scenario ZCP must handle. For each scenario it specifies:

- The trigger (user intent or tool call).
- The resulting `StateEnvelope` (key fields).
- The `Plan` produced (Primary / Secondary / Alternatives).
- The knowledge atoms synthesized.
- The rendered output the LLM sees.

Executable counterpart: `internal/workflow/scenarios_test.go` pins the canonical flows as table-driven tests. Any scenario not listed here is either out of scope (§8) or must be added before it is implemented.

Vocabulary: `StateEnvelope`, `Plan`, `NextAction`, `KnowledgeAtom`, `AxisVector`, `RecipeMatch`, `Route` — see `docs/spec-workflows.md` §1.

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
- **Atoms**: `idle-bootstrap-entry` (idle-phase entry atom, filtered on `phases=[idle]`).
- **User sees**: *"Phase: idle. Services: none. Next: Start bootstrap."*

### 1.2 Managed-only project (DB exists, no runtime)

- **Envelope**: `Services=[{db, managed, ACTIVE}]`.
- **Plan.Primary** = `{start bootstrap, rationale: "No runtime to host application code; only managed dependency exists."}`
- **Plan.Alternatives** = `[{action: start, workflow: develop, rationale: "If you only need to manage the managed service (scale/env), start develop targeting it."}]`
- **Atoms**: `idle-bootstrap-entry` with the managed-only idle scenario variant (`idleScenario=empty` path covers the no-runtime branch).

### 1.3 All services bootstrapped (ServiceMeta complete for every non-managed service)

- **Envelope**: `Services=[{laraveldev, mode=dev, closeDeployMode=auto}, {laravelstage, mode=stage, closeDeployMode=auto, gitPushState=configured}, {db, managed}]` — a service's `closeDeployMode` can never read back as `git-push` (legacy value folds to `auto` at meta read; delivery is derived from `gitPushState`, not stored as a close-mode).
- **Plan.Primary** = `{start develop, rationale: "All services ready for code work."}`
- **Plan.Alternatives** = `[{start bootstrap, rationale: "Add more services."}]`
- **Atoms**: `idle-develop-entry`.

### 1.4 Mixed — some bootstrapped, some unmanaged

- **Envelope**: `Services=[{laraveldev, bootstrapped}, {newservice, no meta, unmanaged}, {db, managed}]`.
- **Plan.Primary** = `{start develop, args.intent="...", rationale: "Bootstrapped services ready for code tasks; unmanaged runtime auto-adopts on develop start."}`
- **Plan.Secondary** = `nil`.
- **Plan.Alternatives** = `[{adopt unmanaged runtimes, rationale: "Write ServiceMeta without opening a develop session."}, {start bootstrap, rationale: "Add more services before working."}]`
- **Atoms**: `idle-develop-entry` (adoption is surfaced through the develop-entry atom; auto-adoption happens inside the develop start handler).

### 1.5 Unmanaged-only (all runtimes lack meta)

- **Envelope**: `Services=[{svc1, no meta}, {svc2, no meta}]`.
- **Plan.Primary** = `{adopt unmanaged runtimes, args: {action: start, workflow: develop, intent: "adopt"}, rationale: "Existing runtime services have no bootstrap metadata yet."}`
- **Plan.Alternatives** = `[]` (adopt is the only viable entry).

### 1.6 Bootstrapped but strategy unset

- **Envelope**: `Services=[{svc, bootstrapped, strategy=unset}]`.
- **Plan.Primary** = `{start develop, rationale: "Service ready; strategy gate fires once the develop session is open."}`
- **Plan.Alternatives** = `[{add more services, rationale: "Add additional managed or runtime services."}]`
- **Atoms**: `develop-strategy-review` (filtered on `deployStates=[deployed], strategies=[unset]`) surfaces the gate in the develop-active guidance body once the session starts — no idle-phase atom.

  *Note: `BuildPlan` stays a pure envelope-shape dispatch; strategy-required is expressed through the atom layer, not through a distinct branch in `planIdle`.*

---

## 2. Phase: `bootstrap-active`

Entered via `zerops_workflow action=start workflow=bootstrap`. Entry is a two-call discovery+commit flow: the first call (no `route` argument) returns a `routeOptions[]` list ranked `resume` > `adopt` > `recipe` (top matches above `MinRecipeConfidence`) > `classic`. The second call supplies `route=<chosen>` and opens a session. Bootstrap is infrastructure-only; it writes ServiceMeta + zerops.yaml scaffolding for verification but no application code and no first deploy.

Bootstrap runs three steps: `discover`, `provision`, `close`. `discover` and `provision` are mandatory; `close` is always reachable. Bootstrap is infrastructure-only — application code scaffolding and first deploy are owned by the develop workflow's first-deploy branch (`deployStates=[never-deployed]`).

### 2.1 Route `recipe` — Recipe match

**Precondition**: intent ↔ recipe slug match with `Confidence ≥ MinRecipeConfidence` AND `Viable == true`.

| Step | Envelope | Plan.Primary | Atoms |
|---|---|---|---|
| `discover` | `route=recipe, step=discover` | `zerops_workflow action=iterate workflow=bootstrap step=provision` once the match is confirmed | `bootstrap-intro`, `bootstrap-route-options` (recipe shape + rename flow injected by `formatRecipeImportYAMLForGuide`) |
| `provision` | `route=recipe, step=provision` | `zerops_import args={yaml: <recipe-import-body>}` (once) → poll via `zerops_discover` until services are RUNNING | `bootstrap-recipe-import`, `bootstrap-wait-active`, `bootstrap-env-var-discovery` |
| `close` | `route=recipe, step=close` | `zerops_workflow action=close workflow=bootstrap` | `bootstrap-recipe-close` |

**After close** → envelope returns to `idle` with services bootstrapped (strategy unset) → §1.6 takes over; develop's first-deploy branch drives scaffolding and first deploy.

### 2.2 Route `classic` — Classic infra

**Precondition**: no viable recipe match OR user chose `classic` from `routeOptions`. Plan is user-confirmed before provision.

| Step | Envelope | Plan.Primary | Atoms |
|---|---|---|---|
| `discover` | `route=classic, step=discover` | `zerops_workflow action=iterate workflow=bootstrap step=provision` after plan confirmation | `bootstrap-intro`, `bootstrap-classic-plan-dynamic` or `bootstrap-classic-plan-static`, `bootstrap-runtime-classes`, `bootstrap-mode-prompt` |
| `provision` | `route=classic, step=provision` | `zerops_import args={yaml: <generated-import>}` → poll via `zerops_discover` | `bootstrap-provision-rules`, `bootstrap-wait-active`, `bootstrap-env-var-discovery` |
| `close` | `route=classic, step=close` | `zerops_workflow action=close workflow=bootstrap` | `bootstrap-close` |

**After close** → idle with `strategy=unset` on runtimes (develop's first-deploy atoms fire on `deployStates=[never-deployed]`).

#### 2.2.1 Managed-only target set

Edge case: plan generates `[postgresql@16]` with no runtimes.

- `provision` validates the managed services reached RUNNING and discovered env vars.
- `close` writes metas only for services that actually exist (managed services do not receive a BootstrapMode).
- **User sees**: *"Bootstrap complete. No runtime to develop against — add one via bootstrap, or use `zerops_manage` to configure the managed service."*

### 2.3 Route `adopt` — Adopt existing

**Precondition**: ≥1 non-system, non-managed service without complete ServiceMeta.

| Step | Envelope | Plan.Primary | Atoms |
|---|---|---|---|
| `discover` | `route=adopt, step=discover` | `zerops_workflow action=iterate workflow=bootstrap step=provision` after mode selection | `bootstrap-adopt-discover`, `bootstrap-mode-prompt` |
| `provision` | `route=adopt, step=provision` | Adoption fast-path: `plan.IsAllExisting()` skips `zerops_import` and jumps straight toward close after `zerops_discover` confirms state | `bootstrap-provision-rules`, `bootstrap-env-var-discovery` |
| `close` | `route=adopt, step=close` | `zerops_workflow action=close workflow=bootstrap` | `bootstrap-close` |

### 2.4 Route `resume` — Resume interrupted session

**Precondition**: registry carries a dead-PID bootstrap session for this project. The engine auto-claims it on first call; `route=resume` is surfaced with the `sessionId` that will be reattached.

- **Plan.Primary** = `{iterate bootstrap at the step where the previous session stopped, rationale: "Continue the prior bootstrap at its last completed step."}`
- **Atoms**: `bootstrap-resume`.

### 2.5 Recipe viability gate rejection

- **Trigger**: intent matches a recipe slug above confidence threshold, but recipe is rejected as non-viable (missing sections, too short, or gated by `recipe_lint`).
- **Flow**: `routeOptions` drops `recipe` for this slug; user picks another route.
- **Envelope after start**: `Route=classic` (or other chosen route), `RecipeMatch={slug, confidence, viable: false, reasons: [...]}`.
- **User sees**: *"Matched '{slug}' but recipe is not viable: {reasons}. Continuing with {route} bootstrap."*

### 2.6 Bootstrap active, user calls `start` again

- **Result**: `ErrWorkflowActive` error with typed payload.
- **Plan.Primary** = `{close current bootstrap, rationale: "Only one workflow active per PID."}`
- **Plan.Secondary** = `{continue (no tool needed — current state is already bootstrap-active), rationale: "Resume where you left off."}`

### 2.7 Bootstrap provision failure

Provision is single-shot: if a step checker fails, the session surfaces the error and escalates to the user. Bootstrap does not iterate; develop's iteration tiers and `defaultMaxIterations=5` do not apply to bootstrap.

- **Envelope**: `Phase=bootstrap-active, Bootstrap={route, step=provision, error}`.
- **Plan.Primary** = `{zerops_logs or zerops_discover, rationale: "Diagnose the provision failure."}`
- **Plan.Secondary** = `{close bootstrap, rationale: "Abandon this session and restart after fixing inputs."}`

---

## 3. Phase: `develop-active`

Entered via `zerops_workflow action=start workflow=develop intent="..."`. Produces a `WorkSession` keyed by PID.

Flow: edit code → `zerops_deploy` (strategy-dispatched) → `zerops_verify` → pass (auto-close eligible) or fail (iteration tier).

### 3.1 Start — envelope after start

- **Envelope**: `Phase=develop-active, WorkSession={intent, services, deploys={}, verifies={}, created_at}`.
- **Plan.Primary** = `{zerops_deploy, args: {targetService: first-service}, rationale: "Ready for first deploy. Ensure edits are complete."}`
- **Atoms** depend on each service's `{mode, strategy, runtime-class, environment}` cell. See matrix below.

### 3.2 Strategy × runtime × environment matrix

Synthesizer runs a per-service pass; each service's atoms are filtered by that service's axis tuple `{modes, closeDeployModes, gitPushStates, buildIntegrations, runtimes, environments, deployStates}`. Axis values come from `internal/workflow/envelope.go` constants; an atom fires only when a single service satisfies every declared service-scoped axis.

Services with `deployStates=never-deployed` route to the first-deploy branch atoms (`develop-first-deploy-*`). Services with `deployStates=deployed` route to the edit-loop branch (close-mode-specific atoms).

Valid cells (invalid combinations error at envelope validation):

| Mode | CloseDeployMode | Runtime | Env | Key atoms (edit-loop branch) |
|---|---|---|---|---|
| dev | auto | dynamic | container | `develop-close-mode-auto-deploy-container`, `develop-close-mode-auto-dev`, `develop-dynamic-runtime-start-container`, `develop-platform-rules-container` |
| dev | auto | dynamic | local | `develop-close-mode-auto-deploy-local`, `develop-dynamic-runtime-start-local`, `develop-local-workflow` |
| dev | auto | static | any | env-scoped `develop-close-mode-auto-deploy-{container,local}`, `develop-static-workflow` (no post-deploy start) |
| dev | auto | implicit-webserver | any | env-scoped `develop-close-mode-auto-deploy-{container,local}`, `develop-implicit-webserver` |
| standard \| simple \| local-stage \| local-only | auto (gitPush=configured) | any | any | `develop-git-push-delivery` — push is the delivery; direct-deploy walkthrough atoms are gated off by `gitPushStates:[unconfigured, broken]` |
| simple | auto | dynamic | any | `develop-close-mode-auto-simple`, `develop-dynamic-runtime-start-*` (env-scoped) |
| any | manual | any | any | `develop-close-mode-manual` — informs only; deploy tools remain callable but ZCP does not auto-deploy at close |
| any | unset | any | any | `develop-strategy-review` (on `deployStates=deployed`) — Plan.Primary stays at the envelope-shape dispatch; the atom body offers the three close-mode options and points at `action="close-mode"` as the commit step |
| any | any | managed | any | no deploy atoms; managed services are not targets of `zerops_deploy` |

Invalid combinations are blocked at the close-mode setter (e.g. `auto` for `local-only` has no Zerops runtime to push to — `workflow_close_mode.go` rejects with `INVALID_PARAMETER` and points at `action="adopt-local"`). Push delivery is derived from capability, not chosen as a close-mode: `gitPushStates=configured` fires `develop-git-push-delivery`, `broken` fires `develop-git-push-broken` (repair via `action="git-push-setup"`), and `unconfigured` fires neither — `setup-git-push-{container,local}` (strategy-setup phase) carries the walkthrough when the user asks for the capability.

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
| pre-deploy | `zerops_deploy args={targetService}` |
| deploy in progress | `zerops_process args={process-id}` (wait) |
| deployed, verify pending | `zerops_verify args={serviceHostname}` |
| verified ∧ all services green | `zerops_workflow action=close workflow=develop` (auto-close) |
| verified ∧ others pending | `zerops_deploy` or `zerops_verify` on next service |
| failed deploy, any iter | `zerops_deploy args={targetService}` — retry same action, atom body carries tier guidance |
| failed verify, any iter | `zerops_verify args={serviceHostname}` — retry same action, atom body carries tier guidance |

Iteration tier (tier-1 diagnose, tier-2 systematic check, tier-3 STOP) rides along via atoms — the Plan.Primary does not change shape as iterations accumulate. On iteration 5 the session auto-closes with `CloseReason=iteration-cap`, and the next status call reverts to the idle-phase dispatch.

### 3.4 Auto-close

Triggers when every `WorkSession.Services` has `Deploys[last].Success=true AND Verifies[last].Success=true`.

- **Envelope**: `Phase=develop-closed-auto, CloseReason=auto-complete`.
- **Plan.Primary** = `{close, rationale: "Task complete."}`
- **Plan.Secondary** = `{start develop with new intent, rationale: "Begin next task."}`
- **Atoms**: `develop-closed-auto` (filtered on `phases=[develop-closed-auto]`).

### 3.5 Iteration-cap close

- **Envelope**: `Phase=idle` (session closed); previous `WorkSession.CloseReason=iteration-cap`.
- **Plan.Primary** = `{zerops_logs, rationale: "Review full attempt history before restart."}`
- **Plan.Secondary** = `{start develop, rationale: "Restart only after understanding the failure mode."}`
- The STOP-tier guidance is delivered via the deploy-iteration atoms when the iteration cap (`defaultMaxIterations`) is reached; on the subsequent idle-phase call the session summary comes from the closed WorkSession record on disk.

### 3.6 Explicit close (user-initiated mid-work)

- **Envelope**: `Phase=idle, WorkSession.CloseReason=explicit`.
- **Plan.Primary** = depending on service state (§1.3–1.5).

### 3.7 `start develop` while develop-active

- **Result**: `ErrWorkflowActive`.
- **Plan.Primary** = `{close current develop, rationale: "Only one develop session per PID."}`
- **Plan.Secondary** = `{continue current session, rationale: "Resume in-progress intent: <intent>"}`

### 3.8 Compaction recovery (same PID, context compressed)

- **State dir read**: `WorkSession` for current PID reloaded from disk.
- **Envelope**: full reconstruction — phase, services, progress, deploy/verify attempts.
- **Plan.Primary** = recomputed from envelope.
- **Atoms**: whatever the current envelope selects; `action="status"` is the canonical recovery call taught by the MCP init instructions.

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

## 5. Error & edge cases

### 6.1 Auth failure

- **Trigger**: any tool with `ZCP_API_KEY` missing or invalid.
- **Response**: `Error{code: AUTH_REQUIRED, message: ..., plan: {Primary: "Set ZCP_API_KEY and retry"}}`.
- **No fallback**: we do not substitute public API or skip the call.

### 6.2 Project not found / not bound

- **Trigger**: tool call without active project context.
- **Envelope**: `Phase=idle, Project={ID: empty, Name: empty}, Services=[]`.
- **Plan.Primary** = `{zerops_discover, rationale: "Resolve project context from the API-key scope; ZCP derives the bound project from the token, not from a manage action."}`

### 6.3 Platform API rate-limit or transient failure

- **Response**: error propagated with `retry-after` if present.
- **Plan.Primary** = `{retry same tool call, rationale: "Platform transient error. Back off and retry."}`
- **No silent retry in-tool**: LLM decides when to retry based on rationale.

### 6.4 State dir corrupted or unreadable

- **Response**: error `WORK_SESSION_CORRUPT` with the work-session file path.
- **Plan.Primary** = `{zerops_workflow action="reset" workflow="develop", rationale: "Work session file is corrupt. Discard it and start fresh; code work survives in git/filesystem, only attempt history is lost."}`
- The reset discards the corrupt session file; no separate confirmation step.

### 6.5 Service deleted externally mid-work-session

- **Envelope**: live API shows service absent; `WorkSession.Services`
  still references it. `ComputeEnvelope` does not currently diff
  scope-vs-live (no `GhostServices` field) — the missing service
  simply does not appear in `env.Services`.
- **Plan.Primary**: regular develop-active dispatch — agent observes
  the missing service via `zerops_discover` and decides whether to
  close the work session or restart bootstrap.
- **Future hardening (not implemented)**: `ComputeEnvelope` could
  attach a typed `GhostServices []string` field and `BuildPlan`
  could surface a close-or-recreate Primary, replacing the inferred
  recovery with an explicit branch. Open issue — pin with a unit
  test if/when implemented.

### 6.6 zerops.yaml missing at repo root (local env)

- **Trigger**: `zerops_deploy` in local env with no `zerops.yaml` (and no `zerops.yml` fallback).
- **Response**: error `INVALID_PARAMETER`, message names both extensions (`zerops.yaml not found in <dir> (also tried zerops.yml)`).
- **Plan.Primary** = `{create zerops.yaml, rationale: "zerops.yaml is required to deploy. Use zerops_knowledge for examples."}`

### 6.7 zcli not installed (local env)

- **Response**: error `PREREQUISITE_MISSING` (`zcli not found in PATH`) with install instructions.
- **Plan.Primary** = `{install zcli, rationale: "Deploy requires zcli locally."}`
- No fallback to alternative deploy method.

### 6.8 Container env, self-service not registered

- **Trigger**: container mode detected but `SelfService.Hostname` empty.
- **Response**: `Phase=idle`, normal envelope. The "You are running on
  the ZCP control-plane container `{{.SelfHostname}}`" identity hint
  lives in the rendered `CLAUDE.md` (from `claude_container.md`
  template), not in the MCP `Instructions` field. When self-hostname
  is empty the rendered template still works (placeholder substitutes
  to empty string) but the LLM cannot pin its host identity until
  it calls `zerops_discover`.
- **Plan.Primary** = the usual idle-branch plan; no special handler.

### 6.9 SSHFS mount out of date after deploy (container env)

- **Detection**: post-deploy, re-read of `zerops_discover` drives the atom pipeline; mounted files survive restart but not deploy (see `develop-platform-rules-common` atom).
- **Response**: `develop-platform-rules-container` atom body carries the remount guidance in the next develop-active turn.
- **Plan.Primary** = unchanged; remount is advisory, not a gated action.

### 6.10 Close-mode mismatch with mode

- **Trigger**: user calls `zerops_workflow action="close-mode" closeMode={"localonly": "auto"}`. `local-only` services have no Zerops runtime to push to with the default zcli mechanism.
- **Response**: error `INVALID_PARAMETER` from `workflow_close_mode.go` with hint pointing at `action="adopt-local"` (link a runtime) or switching to `closeMode=git-push` / `closeMode=manual` (which don't need a stage).
- **Plan.Primary** = `{retry with valid close-mode, rationale: "local-only supports git-push or manual only."}`

### 6.11 Managed service deploy attempt

- **Trigger**: `zerops_deploy args={targetService: db}` where db is PostgreSQL.
- **Response**: managed services carry no deploy ServiceMeta and are not `zerops_deploy` targets — there is no dedicated managed-deploy code; the develop-scope validator rejects a managed hostname with `INVALID_PARAMETER` (`unknown or non-deployable hostnames`).
- **Plan.Primary** = `{zerops_manage for managed service operations, rationale: "Managed services have no deploy; use lifecycle/scale/env instead."}`

---

## 6. Cross-cutting concerns

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

## 7. End-to-end walkthroughs

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

LLM calls: zerops_workflow action=start workflow=bootstrap
  → Response:
    ## Status
    Phase: idle — routeOptions=[{route: recipe, slug: laravel-dashboard, confidence: 0.91}, {route: classic}]
    Next:
      ▸ Primary: Commit route — zerops_workflow action="start" workflow="bootstrap" route="recipe" recipeSlug="laravel-dashboard"

LLM calls: zerops_workflow action=start workflow=bootstrap route=recipe recipeSlug=laravel-dashboard
  → Response:
    ## Status
    Phase: bootstrap-active (route: recipe, step: discover)
    Next:
      ▸ Primary: Iterate to provision — zerops_workflow action="iterate" workflow="bootstrap" step="provision"

    Guidance:
      [bootstrap-intro atom body]
      [recipe shape + omit-plan/rename flow — injected by formatRecipeImportYAMLForGuide]

LLM calls: zerops_workflow action=iterate workflow=bootstrap step=provision → zerops_import → zerops_discover polls
  → (proceeds to close)

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
      - laraveldev (php-nginx@8.3): bootstrapped=true, mode=dev, closeMode=auto, gitPush=unconfigured, buildIntegration=none
      - laravelstage (php-nginx@8.3): bootstrapped=true, mode=stage, closeMode=auto, gitPush=configured, buildIntegration=actions, stage=laraveldev
      - newservice (nodejs@20): not bootstrapped — auto-adopted on develop start
      - db (mariadb@11): managed
    Next:
      ▸ Primary: Start develop — zerops_workflow action="start" workflow="develop" intent="..."
      ◦ Alternatives:
          - Add services — zerops_workflow action="start" workflow="bootstrap"

    Guidance:
      [idle-develop-entry atom body]
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
      [deploy-iteration atom tier-2 systematic-check text]
      [develop-close-mode-auto-deploy-container atom body]
      [develop-platform-rules-container atom body]

    (remaining session attempts: 2)
```

At iteration 5 → STOP + session closes with `iteration-cap`.

---

## 8. Out of scope

- **Multi-project** workflows within one zcp invocation (PID state is per-project).
- **Parallel deploys** to multiple services at once — plan is sequential per service.
- **Recipe authoring UI** (`zcp sync recipe create-repo` etc.) — separate command surface.
- **Eval pipeline mechanics** (`internal/eval`) — run outside the workflow envelope; the scenario matrix it runs is §9.
- **Stale WorkSession garbage collection** policy — handled by registry prune, separate from envelope logic.

---

## 9. Eval core matrix — the anchored scenario set the farm runs

This section is the single source for WHICH behavioral scenarios the eval farm
(`spec-eval-farm.md`) runs and what each must prove. `eval/farm/gate-set.txt` is
derived from the table in §9.3 and pinned by `TestEvalMatrix_GateSetMatchesSpec`;
a hand edit to either side fails the test. Historical scenarios outside this
table are inspiration, not corpus.

### 9.1 Axes

What actually varies for a user of zcp. Every value appears in at least one cell;
every pair the code branches on (route × topology, delivery × close-mode,
env-channel × topology, access × verify, runtime-class × develop) appears in at
least one cell. The matrix is a covering set, not the full product.

| Axis | Values | Home |
|---|---|---|
| Pre-state | brand-new · managed-only · unmanaged (adopt) · mixed · bootstrapped · +git-push · +CI · launched-to-prod | §1 |
| Route | recipe · classic · adopt · resume | §2 |
| Topology | simple · dev-only · standard-pair · (local-stage · local-only: deferred) | spec-workflows §4 |
| Runtime class | dynamic · static · implicit-webserver | §3.2 |
| Managed deps | none · db · db+cache+storage | platform |
| Repo shape | monorepo multi-`setup:` (platform default) · one repo per service | platform |
| Access | public subdomain · internal-only (worker/backend, `http://host:port`) | platform |
| Delivery | zcli push (auto) · git-push configured · GitHub Actions · webhook · manual | spec-workflows §4.3, §11 |
| Env channel | service env (restart) · `run.envVariables` (redeploy) · `build.envVariables` (next build) · project env / cross-ref | atoms `develop-env-var-*` |
| Failure | build failed · build stuck · READY_TO_DEPLOY · stale mount · idle worker · compaction · dead-PID resume · bad import yaml | §3.3-3.9, §6 |
| Environment | container · (local: deferred) | `runtime.Detect` |

### 9.2 Scenario contract — every core cell

1. **Starting state** is written down: services, apps, data; what works; what is
   deliberately broken and how.
2. **Preparation is versioned**: `seed.fixture` + `seed.ref` pin a sha/tag of a
   repository this project owns; a buildFromGit URL inside a fixture carries a ref
   (`TestEvalScenarioFixtures_BuildFromGitPinned`). Preseed scripts stay for
   host-side breakage.
3. **Preparation is asserted by a machine**: `seed.expect` names the exact state
   (service statuses, process outcomes, a probe). Evaluated once, before the agent
   starts, no AI. Mismatch ⇒ run-level `blocked` with reason `preparation`
   (`spec-eval-farm.md §4.5`); the run never counts against zcp or the agent.
   A bare `seed: settled` is not an accepted preparation for a recovery cell.
4. **Task is in the user's voice** and complete: everything needed is in the
   prompt or discoverable from the environment; no hidden service names or
   magic strings; no transcript replays.
5. **Outcome is function, not status**: at least one oracle that proves the app
   does the thing (record round-trip, marker rendered, env value reaches the
   process) AND `unchanged` for what must not move. `ACTIVE` or HTTP 200 alone
   never satisfies a cell. `noFailedProcesses` on every cell that mutates the
   project (adopt-only, export-only and onboarding cells are exempt).
6. **Decisions are pinned**: the runner injects `never: [zerops_import{override=true}]`
   on every cell; a cell lifts it only with `allow: {call, reason}`
   (`spec-eval-farm.md §4.2`). At least one further `never` or `toolArg` row per cell.
7. **Resources are prepared**: tokens, permissions, and the usersim persona's
   answers for every `askWhen` the cell expects.
8. `verification.mode: required`, `verification.spec` points at the section the
   cell proves. One cell, one scenario; a second scenario for one cell needs a
   written reason in this table.

Principle: a deliberately broken app is a correctly prepared test; a wrongly
prepared test must never be graded as an agent or zcp failure.

### 9.3 The matrix

`status` is the definition of *runnable*: `gate` = the scenario file exists,
carries `mode: required`, and every oracle family the row names is a field the
runner evaluates today; `pending: <family>` = the runner does not evaluate that
family yet, the row is NOT in the gate set; `promote: <family>` = the runner
evaluates the family but the scenario does not carry it yet (or the file does
not exist), NOT in the gate set; `gate · promote: <family>` = in the gate set
with what it has, and must adopt the named family. `pending: scenario` /
`pending: local mode in farm` name a missing scenario, not a family. Two tests
keep the markers honest: `TestEvalMatrix_PendingFamily_NotYetARunnerField`
fails when a `pending` family becomes a runner field (flip it to `promote`), and
`TestEvalMatrix_PromoteFamily_ExistsAndScenarioLacksIt` fails when a `promote`
family is not a runner field or the scenario already carries it (flip to `gate`). Oracle families: `expectedServices liveness subdomainProbe
nodePostgresRecord unchanged noFailedProcesses never askWhen launchShape
artifactPromotion noFabricatedSecret` (today) · `seedExpect internalLiveness
containerCheck meta schemaValid toolArg toolResult mustOffer allow` (added by the
manifest v2 slices).

Legend: ↑ existing scenario to promote · ✚ scenario to write.

#### A. First contact and bootstrap

| cell | scenario id | pre-state · route · topology · stack · deps | task | oracle families | status |
|---|---|---|---|---|---|
| A1 | `api-node-postgres-classic-dev` | brand-new · classic · dev-only · node · db | small API with one table | expectedServices liveness nodePostgresRecord toolArg(max 0 zerops_subdomain) never | gate |
| A2 | `classic-static-nginx-simple` | brand-new · classic · simple · static · none | public landing page | expectedServices subdomainProbe never | gate |
| A3 ↑ | `greenfield-fullstack-multi-runtime` | brand-new · classic · standard-pair · node+static · db+cache | API + SPA dashboard | expectedServices liveness containerCheck never | gate |
| A4 ↑ | `classic-php-mariadb-standard` | brand-new · classic · standard-pair · php (implicit-webserver) · mariadb | | expectedServices liveness never | gate |
| A5 ↑ | `recipe-nestjs-minimal-standard` | brand-new · recipe · standard-pair · node · db | NestJS API | expectedServices liveness mustOffer never | gate |
| A5b ✚ | `recipe-nonviable-falls-to-classic` | brand-new · recipe named, non-viable | | mustOffer(classic) never | promote: mustOffer |
| A5c | `greenfield-node-postgres-dev-stage` | brand-new · recipe · standard-pair · node · db | dashboard with records | expectedServices liveness nodePostgresRecord never | gate |
| A6 ↑ | `recipe-laravel-showcase-fullstack` | brand-new · recipe · standard-pair · php · db+cache+storage+search | | expectedServices liveness containerCheck never | gate |
| A7 ↑ | `recipe-first-deploy-race-adopt` | building · adopt · standard-pair | user starts while first build runs | expectedServices toolArg(≤1 import) never | gate |
| A8 | `adopt-existing-standard-pair` | unmanaged · adopt · standard-pair · node · db | connect to what I have | expectedServices unchanged meta never | gate |
| A9 ✚ | `managed-only-add-runtime` | managed-only · classic · dev-only | I have a DB, add an app | expectedServices unchanged nodePostgresRecord never | pending: scenario |
| A10 ↑ | `existing-simple-mode-node-add-endpoint` | unmanaged simple svc · adopt · simple | add an endpoint | liveness meta never | gate |
| A11 ✚ | `bootstrap-managed-only-target` | brand-new · classic · plan = db only | just a Postgres for now | expectedServices meta never | promote: meta |

#### B. Developing (seed: deployed fixture)

| cell | scenario id | variation | task | oracle families | status |
|---|---|---|---|---|---|
| B1 | `develop-add-managed-dep-to-existing` | pair + add cache | | expectedServices unchanged containerCheck never(deploy on managed) | gate |
| B3 | `env-service-scope-pair` | standard-pair, service env feature flag | turn FEATURE_X on for dev and stage | containerCheck toolResult(restartedServices) never | gate |
| B4 ✚ | `env-yaml-baked-dev-only` | dev-only, key in `run.envVariables` | change the baked value | containerCheck never(manage reload as fix) | promote: containerCheck |
| B5 ✚ | `env-project-scope-shared` | pair, project var + cross-ref | one secret shared by dev and stage | containerCheck noFabricatedSecret toolArg never | promote: containerCheck |
| B13 ✚ | `env-build-time-simple` | simple, `build.envVariables` | the build needs a token | containerCheck never | promote: containerCheck |
| B6 | `mount-edit-deploy` (absorbed `existing-standard-appdev-only-reminders`, `develop-edit-path-vs-deploy-source`) | pair, SSHFS | edit in the mount and ship dev only | liveness unchanged toolArg(targetService=appdev always; Bash ln -s never) never | gate |
| B7 ✚ | `mount-stale-recovery` | pair, preseed breaks the mount after deploy | continue editing | containerCheck liveness never | promote: containerCheck |
| B8 | `cross-deploy-stage-promote-from-dev` | pair, promote | | artifactPromotion mustOffer(no-rebuild) never | gate |
| B9 | `internal-only-worker` (absorbed D7 idle-worker verify) | pair + worker, no subdomain anywhere | add a queue worker | internalLiveness expectedServices never(subdomain touched) | gate |
| B10 ↑ | `develop-loop-after-bootstrap` | strategy unset → review gate | | meta expectedServices never | gate |
| B11 ✚ | `git-push-configured-manual-close` | pair, git-push configured, close-mode manual | don't push for me | meta unchanged toolArg(no deploy after edits) never | promote: meta |
| B12 ✚ | `develop-static-redeploy` | simple, static | change the page | liveness toolArg(no post-deploy start) never | promote: toolArg |
| B14 ✚ | `subdomain-user-disabled-stays-off` | dev-only, subdomain auto-enabled once then user-disabled | change the response text, redeploy | expectedServices(subdomainAccess false) internalLiveness meta(intent none) toolArg(max 0 zerops_subdomain) never | gate |
| B15 ✚ | `custom-domain-present` | simple, custom domain routed instead of a subdomain | verify + report reachability | expectedServices toolArg(max 0 zerops_subdomain) toolResult(public_domain) mustOffer(domain) never | gate |

#### C. Shipping (seed: deployed)

| cell | scenario id | variation | oracle families | status |
|---|---|---|---|---|
| C1 ↑ | `git-push-setup-then-actions` (+ phase 2 absorbs `launch-with-existing-cicd`) | git-push + actions; usersim supplies the prepared PAT | meta containerCheck(.github/workflows) toolArg(no non-git-push deploy after setup) askWhen(GIT_TOKEN_MISSING) never | gate |
| C6 ✚ | `git-push-setup-empty-remote` | git-push on a brand-new, genuinely EMPTY GitHub repo (farm creates + deletes it per run, no baseline commit) — first push must be a clean fast-forward, never the shared-repo sibling's non-fast-forward friction | meta containerCheck(.github/workflows) toolResult(remote state=empty) toolArg never | gate |
| C2 | `launch-production-from-standard-pair` | new prod project | launchShape noFabricatedSecret never | gate |
| C3 ↑ | `launch-to-existing-prod-project` | existing prod project token | launchShape toolResult(TOKEN_SCOPE_MISMATCH once) never | gate |
| C4 ✚ | `webhook-delivery` | dashboard webhook on push | containerCheck(no Actions file) meta(buildIntegration=webhook) never | promote: meta |
| C5 ↑ | `export-buildfromgit-self-snapshot` | | schemaValid toolResult(secret classes) never | gate |

#### D. Something went wrong (seed: prepared dev service + `seed.expect`)

The broken app lives on a MOUNTED dev service (fixture pushes pinned source;
no buildFromGit), so the agent has a place to fix source. `allowFailed` explicit.

| cell | scenario id | prepared break | oracle families | status |
|---|---|---|---|---|
| D1 | `recover-failed-buildfromgit-missing-dep` | build OK, START fails (db env missing) — today the BUILD fails; re-prepare | seedExpect liveness never | gate |
| D2 | `recover-build-failed` | build fails (bad dep) | seedExpect toolResult(failureClass) liveness never | gate |
| D3 | `launch-failure-build-stuck` | | noFailedProcesses noFabricatedSecret never | gate |
| D4 ✚ | `ready-to-deploy-stuck` | runtime imported without startWithoutCode | allow(override, reason: only path) mustOffer(DIAGNOSIS_REQUIRED before override) unchanged | promote: allow |
| D5 | `resume-after-compaction` (absorbs `resume-status-not-discover`) | | expectedServices unchanged never | gate |
| D6 ↑ | `discover-adoption-state-resumable-uses-sessionid` | dead-PID bootstrap session | expectedServices toolArg(≤1 import) never | gate |
| D8 ✚ | `bootstrap-import-fails` | bad yaml at provision | toolArg(≤1 import) askWhen never | promote: toolArg |

#### E. First contact surfaces

| cell | scenario id | | oracle families | status |
|---|---|---|---|---|
| E1 ↑ | `onboard-trigger-fresh` | fixed onboard prompt replayed | toolArg(workflow start bootstrap) never | gate |
| E2 ↑ | `onboard-populated` | populated project | toolArg(discover first, no bootstrap) never | gate |
| E3 ↑ | `onboard-trigger-negative` | unrelated ask | toolArg(no workflow start) never | gate |
| E4 ↑ | `onboard-guided-on` | guided marker on | toolArg(bootstrap reached) never | gate |

#### F. Named gaps — a cell, no gate entry until the feature lands

| cell | what | status |
|---|---|---|
| F1 | multi-repo: web + api, each its own repo, each pair its own `RemoteURL`, both git-push | pending: scenario (baseline batch first) |
| F2 | monorepo multi-`setup:` from the classic route | pending: scenario (baseline batch first) |
| F3 | service deleted externally (§6.5 known gap) | pending: scenario |
| L1-L3 | local-stage first deploy · local + managed over VPN · local-only adopt | pending: local mode in farm |

#### G. Git foundation (docs/spec-workflows.md §4.9, §12, Git Lifecycle §8 GLC; a row enters the gate set once its scenario carries every family it names)

| cell | scenario id | pre-state · task | oracle families | status |
|---|---|---|---|---|
| G1 ✚ | `repo-always-bootstrap` | brand-new classic pair · build a small API | expectedServices liveness containerCheck(HEAD reachable; identity set; no zcp tag; any `.gitignore` not robot-committed) never | gate |
| G2 ✚ | `repo-always-adopt-baseline` | unmanaged buildFromGit pair with history + planted user cargo (preseed) · connect to what I have | expectedServices unchanged containerCheck(cargo untouched: history, branch, tag, ref, dirty+untracked, exclude line, identity, origin; HEAD unmoved; no zcp tag) meta(repo.provenance=existing) never | gate |
| G3 ✚ | `deploy-from-commit-stage` | pair, both buildFromGit-deployed, dev carries user cargo (preseed) · ship a specific commit to stage | toolArg(targetService=appstage) toolResult(sha/appVersionId) liveness containerCheck(dev checkout untouched — same cargo set as G2) never | gate |
| G4 ✚ | `dev-self-deploy-keeps-repo` | pair with one previous stage deploy + user cargo on dev (preseed) · change the response text, ship dev | liveness toolArg(targetService=appdev) containerCheck(cargo travelled into the replacement container: history, branch, tag, ref, uncommitted content, exclude line, identity, origin; no zcp tag) never | gate |
| G5 ✚ | `rollback-stage-from-ledger` | stage with 2 recorded deploys (preseed) · roll back to the previous version | seedExpect toolArg(targetService=appstage; appVersion=<id>, never latest; max 0 import; max 0 sha) mustOffer(no rebuild) never | pending: activeAppVersion |
| G6 ✚ | `repo-adopt-initialized-no-git` | unmanaged pair, dev has NO repository, files + secret `.env` on disk (preseed) · connect to what I have | expectedServices unchanged containerCheck(initialized: cargo + `.env` both on disk and untracked, no commit carries either, no tag) meta(repo.provenance=initialized) never | gate |

Deliberately not covered: recipe × simple (no simple-capable recipe in the
catalog); iteration-cap auto-close (internal state, unit-tested, not a journey).

### 9.4 Trusting the loop

Before a batch verdict is used to change zcp, a **mutation batch** must have
passed once per oracle family: a candidate with one planted regression per family
(env restart broken → B3; override skips DIAGNOSIS_REQUIRED → D4; mount resolved
from cwd → B6; recipe offer skipped → A5; subdomain auto-enable dropped → A2, only
once `internalLiveness` exists so the other liveness cells do not fail together).
Every planted regression lands `failed` on its intended ROW. The "nowhere else"
half is read per row, not per cell: identical batches flip 4-8 of 29 cells on
agent nondeterminism, timeouts and seed timing, so a cell that flips outside the
intended row is variance unless the same row flips in two or more repeats. A
single batch is a signal; a problem `recurring` across two or more baseline
batches is evidence. Mutation and negative batches are archived (`farm
archive`) so they never feed problem statistics.

Run 2026-09-13 (spec-eval-farm.md §3.3 batches `mut-01..04` vs `matrix-3/4`):
3 of 4 regressions detected on the intended row (B3, B6, A8); the subdomain
auto-enable regression escaped — every current cell gets its subdomain from the
import flag, so no cell depends on the deploy handler's auto-enable.

That gap is closed: A1 (dev-only, deferred-start) and B14 (subdomain
auto-enabled once, then user-disabled) both depend on the deploy/dev-server
auto-enable hook rather than the import flag, so a regression in that hook
now lands on an intended row instead of escaping.
