# ZCP Scenarios — Exhaustive Walkthrough

> **Scope**: Every valid state, phase, and transition the ZCP MCP server
> must handle, with envelope → plan → atoms → user-output mapping.

This document enumerates every scenario ZCP must handle. For each scenario it specifies:

- The trigger (user intent or tool call).
- The resulting `StateEnvelope` (key fields).
- The `Plan` produced (Primary / Secondary / Alternatives).
- The knowledge atoms synthesized.
- The rendered output the LLM sees.

Executable counterpart: `internal/workflow/scenarios_test.go` pins the canonical flows as table-driven tests. Any scenario not listed here is either out of scope (§9) or must be added before it is implemented.

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
- **Atoms**: `idle-bootstrap-entry` (idle-phase entry atom, filtered on `phases=[idle]`).
- **User sees**: *"Phase: idle. Services: none. Next: Start bootstrap."*

### 1.2 Managed-only project (DB exists, no runtime)

- **Envelope**: `Services=[{db, managed, ACTIVE}]`.
- **Plan.Primary** = `{start bootstrap, rationale: "No runtime to host application code; only managed dependency exists."}`
- **Plan.Alternatives** = `[{action: start, workflow: develop, rationale: "If you only need to manage the managed service (scale/env), start develop targeting it."}]`
- **Atoms**: `idle-bootstrap-entry` with the managed-only idle scenario variant (`idleScenario=empty` path covers the no-runtime branch).

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
| `discover` | `route=recipe, step=discover` | `zerops_workflow action=iterate workflow=bootstrap step=provision` once the match is confirmed | `bootstrap-intro`, `bootstrap-recipe-match`, `bootstrap-route-options` |
| `provision` | `route=recipe, step=provision` | `zerops_import args={yaml: <recipe-import-body>}` (once) → poll via `zerops_discover` until services are RUNNING | `bootstrap-recipe-import`, `bootstrap-wait-active`, `bootstrap-env-var-discovery` |
| `close` | `route=recipe, step=close` | `zerops_workflow action=close workflow=bootstrap` | `bootstrap-recipe-close`, `bootstrap-write-metas` |

**After close** → envelope returns to `idle` with services bootstrapped (strategy unset) → §1.6 takes over; develop's first-deploy branch drives scaffolding and first deploy.

### 2.2 Route `classic` — Classic infra

**Precondition**: no viable recipe match OR user chose `classic` from `routeOptions`. Plan is user-confirmed before provision.

| Step | Envelope | Plan.Primary | Atoms |
|---|---|---|---|
| `discover` | `route=classic, step=discover` | `zerops_workflow action=iterate workflow=bootstrap step=provision` after plan confirmation | `bootstrap-intro`, `bootstrap-classic-plan-dynamic` or `bootstrap-classic-plan-static`, `bootstrap-runtime-classes`, `bootstrap-mode-prompt` |
| `provision` | `route=classic, step=provision` | `zerops_import args={yaml: <generated-import>}` → poll via `zerops_discover` | `bootstrap-provision-rules`, `bootstrap-wait-active`, `bootstrap-env-var-discovery` |
| `close` | `route=classic, step=close` | `zerops_workflow action=close workflow=bootstrap` | `bootstrap-close`, `bootstrap-write-metas` |

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
| `close` | `route=adopt, step=close` | `zerops_workflow action=close workflow=bootstrap` | `bootstrap-close`, `bootstrap-write-metas` |

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
- **Plan.Primary** = `{zerops_deploy, args: {hostname: first-service}, rationale: "Ready for first deploy. Ensure edits are complete."}`
- **Atoms** depend on each service's `{mode, strategy, runtime-class, environment}` cell. See matrix below.

### 3.2 Strategy × runtime × environment matrix

Synthesizer runs a per-service pass; each service's atoms are filtered by that service's axis tuple `{modes, strategies, runtimes, environments, deployStates}`. Axis values come from `internal/workflow/envelope.go` constants; an atom fires only when a single service satisfies every declared service-scoped axis.

Services with `deployStates=never-deployed` route to the first-deploy branch atoms (`develop-first-deploy-*`). Services with `deployStates=deployed` route to the edit-loop branch.

Valid cells (invalid combinations error at envelope validation):

| Mode | Strategy | Runtime | Env | Key atoms (edit-loop branch) |
|---|---|---|---|---|
| dev | push-dev | dynamic | container | `develop-push-dev-deploy`, `develop-dynamic-runtime-start-container`, `develop-platform-rules-container` |
| dev | push-dev | dynamic | local | `develop-push-dev-deploy`, `develop-dynamic-runtime-start-local`, `develop-local-workflow` |
| dev | push-dev | static | any | `develop-push-dev-deploy`, `develop-static-workflow` (no post-deploy start) |
| dev | push-dev | implicit-webserver | any | `develop-push-dev-deploy`, `develop-implicit-webserver` |
| stage | push-git | any | any | `develop-push-git-deploy` |
| simple | push-dev | dynamic | any | `develop-push-dev-workflow-simple`, `develop-dynamic-runtime-start-*` (env-scoped) |
| any | manual | any | any | `develop-manual-deploy` — no `zerops_deploy` call; user deploys externally, then `zerops_verify` |
| any | unset | any | any | `develop-strategy-review` (on `deployStates=deployed`) — Plan.Primary stays at the envelope-shape dispatch; the atom body surfaces `zerops_workflow action="strategy"` as the gate the agent must honour before attempting a subsequent deploy |
| any | any | managed | any | no deploy atoms; managed services are not targets of `zerops_deploy` |

Invalid combinations (e.g. stage + push-dev, dev + push-git, simple + stage) are blocked at pre-flight with an explicit error.

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
- **Atoms**: `develop-closed-auto` (filtered on `phases=[develop-closed-auto]`).

### 3.5 Iteration-cap close

- **Envelope**: `Phase=idle` (session closed); previous `WorkSession.CloseReason=iteration-cap`.
- **Plan.Primary** = `{zerops_logs, rationale: "Review full attempt history before restart."}`
- **Plan.Secondary** = `{start develop, rationale: "Restart only after understanding the failure mode."}`
- The tier-3 STOP atom body is surfaced via `BuildIterationDelta` when the cap is reached; on the subsequent idle-phase call the session summary comes from the closed WorkSession record on disk.

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

## 5. Phase: `recipe-active`

Entered via `zerops_workflow action=start workflow=recipe`. Used for *building* recipe repo files — distinct from bootstrap `route=recipe` which *consumes* recipes for infrastructure.

Six steps run sequentially, each with sub-step orchestration (see `RecipeSubStep` in `internal/workflow/`):

| Step | Plan.Primary |
|---|---|
| `research` | LLM fills `RecipePlan` via `zerops_workflow action=record-recipe-plan` |
| `provision` | `zerops_import` + `zerops_discover` poll until services are RUNNING |
| `generate` | LLM writes app code + `zerops.yaml` + per-codebase READMEs onto the mount |
| `deploy` | `zerops_deploy` (dev), `zerops_verify`, cross-deploy dev→stage, `zerops_verify` (stage) |
| `finalize` | Generate 6 `import.yaml` variants + 7 README files + strip placeholders |
| `close` | `zerops_workflow action=close workflow=recipe` after subagent review |

Guidance is pulled on demand via `zerops_guidance` rather than pushed as a single monolithic block; each sub-step carries its own validation gates.

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
- **Response**: `Phase=idle`, base instructions append carries the "You are running on '{hostname}'" hint from `internal/server/instructions.go` when the self-service is known; when empty the hint is omitted and the LLM resolves identity through `zerops_discover`.
- **Plan.Primary** = the usual idle-branch plan; no special handler.

### 6.9 SSHFS mount out of date after deploy (container env)

- **Detection**: post-deploy, re-read of `zerops_discover` drives the atom pipeline; mounted files survive restart but not deploy (see `develop-platform-rules-common` atom).
- **Response**: `develop-platform-rules-container` atom body carries the remount guidance in the next develop-active turn.
- **Plan.Primary** = unchanged; remount is advisory, not a gated action.

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
      [bootstrap-recipe-match atom body]

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
      - laraveldev (php-nginx@8.3): mode=dev, strategy=push-dev
      - laravelstage (php-nginx@8.3): mode=stage, strategy=push-git, stage-of=laraveldev
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
      [BuildIterationDelta tier-2 systematic-check text]
      [develop-push-dev-deploy atom body]
      [develop-platform-rules-container atom body]

    (remaining session attempts: 2)
```

At iteration 5 → STOP + session closes with `iteration-cap`.

---

## 9. Out of scope

- **Multi-project** workflows within one zcp invocation (PID state is per-project).
- **Parallel deploys** to multiple services at once — plan is sequential per service.
- **Recipe authoring UI** (`zcp sync recipe create-repo` etc.) — separate command surface.
- **Eval pipeline** (`internal/eval`) — runs outside the workflow envelope.
- **Stale WorkSession garbage collection** policy — handled by registry prune, separate from envelope logic.
