# Atom Authoring Contract — Per-Atom Rewrite Specifications

> **Companion to**: `plans/atom-authoring-contract.md` (read it first for context, scope, phases).
> **Purpose**: Frozen, executable rewrite spec for each HIGH-severity atom identified in the third-pass audit. One block per atom. Execute Phase 0 against this file; do not re-derive prose.

## Index

28 HIGH-severity atoms + 2 new consolidation atoms + 1 DELETE + 4 spec-ID strip. Ordered by family so Phase 0 can execute family-by-family commits.

### Phase -2 demolition (spec-ID strip, done before atom rewrites)

1. `develop-deploy-modes` — strip DM-2 / DM-3 / DM-4 / DM-5 / docs/spec-workflows.md citations
2. `develop-deploy-files-self-deploy` — strip DM-2 citations in title + body
3. `develop-first-deploy-scaffold-yaml` — strip DM-5 + docs/spec citation
4. `develop-push-dev-deploy-container` — strip (DM-2) inline
5. `internal/knowledge/bases/static.md:3` — strip DM-5 + spec pointer (non-atom knowledge base)
6. `internal/knowledge/recipes/dotnet-hello-world.md:204` — strip (DM-2 in Zerops' spec)

### Phase 0 — atom rewrite families

**Family A — bootstrap (7 atoms)**
- B-1. `bootstrap-adopt-discover` — strip ServiceMeta prose
- B-2. `bootstrap-close` — **NEEDS-REVISION** (adopt-route overlap; requires route-scoped phrasing)
- B-3. `bootstrap-mode-prompt` — observable via ServiceSnapshot.Mode
- B-4. `bootstrap-recipe-close` — observable via Bootstrapped + Strategy
- B-5. `bootstrap-resume` — via IdleScenario.IdleIncomplete + RouteOption fields
- B-6. `bootstrap-route-options` — via BootstrapDiscoveryResponse fields
- B-7. `bootstrap-write-metas` — **DELETE** (fold 1 paragraph into bootstrap-close)

**Family B — first-deploy (6 atoms)**
- FD-1. `develop-first-deploy-execute` — DeployResult fields
- FD-2. `develop-first-deploy-verify` — VerifyResult + WorkSession close
- FD-3. `develop-first-deploy-intro` — DeployState + VerifyResult
- FD-4. `develop-first-deploy-promote-stage` — consolidate to auto-close atom
- FD-5. `develop-first-deploy-write-app` — observable git setup
- FD-6. `develop-first-deploy-scaffold-yaml` — (already in Phase -2, re-confirmed here)

**Family C — develop close / push / strategy / deploy-modes (8 atoms)**
- C-1. `develop-change-drives-deploy` — **NEEDS-REVISION** (iteration-cap close reason missing)
- C-2. `develop-close-push-dev-dev` — **NEEDS-REVISION** (worker no-HTTP branch)
- C-3. `develop-closed-auto` — Phase + WorkSession fields
- C-4. `develop-env-var-channels` — envChangeResult fields
- C-5. `develop-mode-expansion` — envelope.ServiceSnapshot projection
- C-6. `develop-push-dev-workflow-dev` — DevServerResult Reason codes
- C-7. `develop-strategy-awareness` — Strategy / Trigger fields
- C-8. `develop-push-dev-deploy-container` — (already in Phase -2, re-confirmed)

**Family D — dynamic runtime / checklist / idle / export / strategy-push-git (7 atoms)**
- D-1. `develop-dynamic-runtime-start-container` — DevServerResult fields, strip anthropomorphism
- D-2. `develop-dev-server-triage` — **NEEDS-REVISION** (worker no-HTTP-probe branch)
- D-3. `develop-checklist-dev-mode` — strip anthropomorphism
- D-4. `develop-platform-rules-container` — DevServerResult fields
- D-5. `idle-adopt-entry` — via BootstrapRouteOption.AdoptServices
- D-6. `idle-develop-entry` — via Phase + WorkSession
- D-7. `export` — ExportResult + DeployResult fields
- D-8. `strategy-push-git-push-container` — APIError + DeployResult
- D-9. `strategy-push-git-push-local` — DeployResult + Warnings

### Phase 4 consolidation (2 new atoms)

- CON-1. `develop-auto-close-semantics` — shared by FD-2, FD-4, C-1, C-3
- CON-2. `develop-dev-server-reason-codes` — shared by D-1, D-2, C-2, C-6

---

## Phase -2 — Spec-ID strip (4 atoms + 2 knowledge-base files)

### P-2.1 · develop-deploy-modes

**Action**: strip 4 spec-ID tokens + final "Reference" line.

**Edits** (exact):

| Line | Current | New |
|---|---|---|
| 16 | `destroy the target's source (DM-2). Typical` | `destroy the target's source. Typical` |
| 30 | `\| `[.]` \| DM-2; anything narrower destroys target on deploy. \|` | `\| `[.]` \| Anything narrower destroys target on deploy. \|` |
| 43 | `cross-deploy (DM-3 / DM-4). The Zerops builder` | `cross-deploy. The Zerops builder` |
| 48 | `**Reference**: `docs/spec-workflows.md` §8 Deploy Modes (DM-1…DM-5).` | **DELETE LINE** |

### P-2.2 · develop-deploy-files-self-deploy

**Action**: strip DM-2 from title + 3 body occurrences + Reference line.

| Line | Current | New |
|---|---|---|
| 5 | `title: "Self-deploy requires deployFiles: [.] — DM-2"` | `title: "Self-deploy requires deployFiles: [.] — narrower patterns destroy the target"` |
| 8 | `### Self-deploy invariant (DM-2)` | `### Self-deploy invariant` |
| 27 | `Client-side pre-flight rejects DM-2 violations with` | `Client-side pre-flight rejects this with` |
| 36 | `cherry-picks (`./out`, `./dist`, `./build`) and DM-2 does NOT apply.` | `cherry-picks (`./out`, `./dist`, `./build`).` |
| 39 | `**Reference**: `docs/spec-workflows.md` §8 Deploy Modes.` | **DELETE LINE** |

### P-2.3 · develop-first-deploy-scaffold-yaml

**Action**: rewrite final sentence of tilde/ContentRootPath tip.

Find: `See `develop-deploy-modes` atom for the full decision rule and DM-5 in `docs/spec-workflows.md` §8.`
Replace: `See `develop-deploy-modes` for the full decision rule.`

### P-2.4 · develop-push-dev-deploy-container

**Action**: strip `(DM-2)` parenthetical.

Find: `self-deploy needs `[.]` (DM-2), cross-deploy`
Replace: `self-deploy needs `[.]`, cross-deploy`

### P-2.5 · static.md (knowledge base, not atom)

`internal/knowledge/bases/static.md:3` — strip `(DM-5 in docs/spec-workflows.md §8)` parenthetical. Replace with functional tilde-extract summary.

### P-2.6 · dotnet-hello-world.md (recipe knowledge)

`internal/knowledge/recipes/dotnet-hello-world.md:204` — find `(DM-2 in Zerops' spec)` and strip the parenthetical. Keep the surrounding destruction-warning context.

---

## Family A — bootstrap (7 atoms)

### B-1 · bootstrap-adopt-discover

**Drift**: INVISIBLE-STATE — "Adopt writes `ServiceMeta` per service. It does NOT touch code"
**Action**: REWRITE-PARTIAL

**Proposed prose** (replaces the "Adopt writes ServiceMeta" sentence):
> Adoption attaches ZCP tracking to an existing runtime service without touching its code, configuration, or scale. After adopt close, the envelope reports each adopted hostname with `bootstrapped: true`; the rendered Services block shows them with their existing mode and strategy state.

**Frontmatter**:
```yaml
references-fields:
  - workflow.ServiceSnapshot.Bootstrapped
  - workflow.ServiceSnapshot.Mode
  - workflow.ServiceSnapshot.Strategy
```

**Verdict**: APPROVED (Option A needed for `bootstrapped: true` visibility)

---

### B-2 · bootstrap-close [NEEDS-REVISION]

**Drift**: INVISIBLE-STATE + BEHAVIOR-CLAIM — "writing `ServiceMeta` records with `BootstrappedAt`" + "stamps `FirstDeployedAt`"
**Action**: REWRITE-PARTIAL with adopt-route overlap handled
**Edge**: Recipe-route close may deploy during bootstrap; adopt-route writes `deployed: true` via IsAdopted+ACTIVE. Prose must scope "deployed: false" to the never-deployed classic path.

**Proposed prose**:
> Bootstrap is infrastructure-only. After you call `action="complete" step="close"`, every planned runtime appears in the envelope with `bootstrapped: true` — infrastructure is provisioned (managed services RUNNING, runtimes registered, dev containers SSH-mount-ready, managed env vars discoverable). For classic and recipe-with-first-deploy-later routes the same services show `deployed: false` and enter the develop first-deploy branch. For adopt-route services and recipes that deployed during bootstrap the envelope shows `deployed: true` directly. No application code is written, no `zerops.yaml` generated, and no deploy runs as part of bootstrap close itself. Next step: `zerops_workflow action="start" workflow="develop"`. Direct tools (`zerops_scale`, `zerops_env`, `zerops_subdomain`, `zerops_discover`) remain callable without a workflow wrapper.

**Frontmatter**:
```yaml
references-fields:
  - workflow.ServiceSnapshot.Bootstrapped
  - workflow.ServiceSnapshot.Deployed
  - workflow.DeployState
```

**Absorbed paragraph from bootstrap-write-metas** (B-7 DELETE; one sentence moved here):
> ServiceMeta records are on-disk evidence authored by bootstrap and adoption; their envelope projection is the `ServiceSnapshot` with `bootstrapped: true`, the chosen mode, and stage pairing where applicable.

**Render-gap**: A
**Verdict**: APPROVED with adopt-route scoping

---

### B-3 · bootstrap-mode-prompt

**Drift**: INVISIBLE-STATE — "Record the confirmed choice in the `ServiceMeta.Mode` field"
**Action**: REWRITE-PARTIAL

**Proposed prose**:
> Every runtime service needs a mode; confirm with the user before submitting the plan. **dev** — single mutable container, SSHFS-mountable, no stage pair; best for active iteration. **standard** — dev + stage pair; the envelope reports `stageHostname` on the dev snapshot and a separate snapshot with `mode: stage` for the stage service. **simple** — single container that starts real code on every deploy; no SSHFS mutation lifecycle. **stage** is never bootstrapped alone — it is the stage half of a standard pair. Default to **dev** for services under active iteration, **simple** for immutable workers. The plan commits the mode when you submit it; after bootstrap closes, the envelope exposes the chosen mode as `ServiceSnapshot.Mode`. Changing mode later requires the mode-expansion flow (`develop-mode-expansion`).

**Frontmatter**:
```yaml
references-fields:
  - workflow.ServiceSnapshot.Mode
  - workflow.ServiceSnapshot.StageHostname
  - workflow.Mode
```

**Verdict**: APPROVED

---

### B-4 · bootstrap-recipe-close

**Drift**: INVISIBLE-STATE + BEHAVIOR-CLAIM — "`ServiceMeta` records … are written automatically" + "CLAUDE.md log entry"
**Action**: REWRITE-PARTIAL

**Proposed prose**:
> Complete the close step via `zerops_workflow action="complete" step="close" attestation="Recipe {slug} bootstrapped — services active and verified"`. After close, every service the recipe provisioned appears in the envelope with `bootstrapped: true` and `strategy: unset` — strategy is not chosen at bootstrap; develop picks it on first use. `zerops_workflow action="status"` summarises the transition and points at the primary follow-ups: `develop` (iterate on the code the recipe provided) and `cicd` (wire git-based deploys).

**Frontmatter**:
```yaml
references-fields:
  - workflow.ServiceSnapshot.Bootstrapped
  - workflow.ServiceSnapshot.Strategy
  - workflow.StrategyUnset
```

**Render-gap**: A (bootstrapped token)
**Verdict**: APPROVED

---

### B-5 · bootstrap-resume

**Drift**: INVISIBLE-STATE — "`BootstrapSession` but carries no `BootstrappedAt`"
**Action**: REWRITE-FULL
**Render-gap**: B (idleScenario already surfaces it)

**Proposed prose**:
> The envelope reports `idleScenario: incomplete` — the project has at least one runtime service whose snapshot carries `resumable: true`, meaning a prior bootstrap session wrote partial state and died before close. **Do not classic-bootstrap over these services** — a new session clashes with the existing partial records. Preferred path: `zerops_workflow action="start" workflow="bootstrap" intent="<anything>"` and read `routeOptions[]`. The `resume` entry carries `resumeSession` (the session ID) and `resumeServices` (the hostnames that will be reclaimed). Dispatch with `zerops_workflow action="start" workflow="bootstrap" route="resume" sessionId="<resumeSession>"`. Resume picks up at the step that was in flight when the earlier session ended. If the partial state is stale, delete `.zcp/state/services/<hostname>.json` to abandon it and make the services adoptable in the normal flow.

**Frontmatter**:
```yaml
references-fields:
  - workflow.IdleScenario
  - workflow.ServiceSnapshot.Resumable
  - workflow.BootstrapRouteOption.ResumeSession
  - workflow.BootstrapRouteOption.ResumeServices
```

**Verdict**: APPROVED

---

### B-6 · bootstrap-route-options

**Drift**: INVISIBLE-STATE — "`BootstrapSession` tags" + "without complete `ServiceMeta`"
**Action**: REWRITE-PARTIAL

**Proposed prose**:
> Bootstrap starts with a discovery pass: call `zerops_workflow action="start" workflow="bootstrap" intent="<one-sentence>"` without a `route` parameter. The response is a `BootstrapDiscoveryResponse` with `routeOptions[]` ordered by priority:
> - **resume** — present when at least one service snapshot has `resumable: true`; carries `resumeSession` + `resumeServices`; dispatch via `route="resume" sessionId="<resumeSession>"`.
> - **adopt** — present when runtime services exist without a matching bootstrap record (the envelope's Services block renders them as `not bootstrapped`); carries `adoptServices[]` with hostnames to be adopted.
> - **recipe** — up to three entries, each carrying `recipeSlug`, `confidence`, `collisions[]`; recipe collisions are recoverable inside the recipe route via runtime rename or same-type managed `resolution: EXISTS`.
> - **classic** — always present, always last; the manual plan path.
>
> Pick one and call `start` again with the chosen `route` (and `recipeSlug` / `sessionId` as relevant). An explicit `route` on the first call bypasses discovery entirely. Collision enforcement runs at plan submission, not at discovery.

**Frontmatter**:
```yaml
references-fields:
  - workflow.BootstrapDiscoveryResponse.RouteOptions
  - workflow.BootstrapRouteOption.Route
  - workflow.BootstrapRouteOption.ResumeSession
  - workflow.BootstrapRouteOption.AdoptServices
  - workflow.BootstrapRouteOption.RecipeSlug
  - workflow.BootstrapRouteOption.Collisions
```

**Verdict**: APPROVED (no render extension needed — rely on existing `not bootstrapped` fallback)

---

### B-7 · bootstrap-write-metas · **DELETE**

**Rationale**: Atom is 100% invisible-state documentation of on-disk ServiceMeta fields (`Hostname`, `Mode`, `StageHostname`, `BootstrapSession`, `BootstrappedAt`). Agent cannot observe any of these directly — only derived booleans via ServiceSnapshot. No agent-actionable content.

**Migration**: one sentence folded into bootstrap-close (see B-2 above):
> "ServiceMeta records are on-disk evidence authored by bootstrap and adoption; their envelope projection is the `ServiceSnapshot` with `bootstrapped: true`, the chosen mode, and stage pairing where applicable."

**Action**: `git rm internal/content/atoms/bootstrap-write-metas.md`

**Impact**:
- Corpus shrinks 75 → 74.
- No atom-cross-reference renames needed (atom was not referenced by any other atom per F3 scan).
- Update `corpus_coverage_test.go` if it references this atom by ID.

**Verdict**: DELETE

---

## Family B — first-deploy (6 atoms)

### FD-1 · develop-first-deploy-execute

**Drift**: BEHAVIOR-CLAIM — "The deploy handler activates the L7 subdomain automatically on first deploy"
**Action**: REWRITE-PARTIAL

**Proposed prose**:
> Run `zerops_deploy targetService="{hostname}"`. The Zerops container is empty until this call lands, so probing its subdomain or SSHing into it first will fail or hit a platform placeholder — deploy first, then inspect. `zerops_deploy` batches build + container provision + start; expect 30–90 seconds for dynamic runtimes and longer for `php-nginx` / `php-apache`. If `status` is non-success, read `buildLogs` / `runtimeLogs` / `failedPhase` before retrying — a second attempt on the same broken `zerops.yaml` burns another deploy slot without new information. On first-deploy success the response carries `subdomainAccessEnabled: true` and a `subdomainUrl` — no manual `zerops_subdomain` call is needed in the happy path.

**Frontmatter**:
```yaml
references-fields:
  - ops.DeployResult.SubdomainAccessEnabled
  - ops.DeployResult.SubdomainURL
  - ops.DeployResult.Status
  - ops.DeployResult.BuildLogs
  - ops.DeployResult.RuntimeLogs
  - ops.DeployResult.FailedPhase
```

**Verdict**: APPROVED

---

### FD-2 · develop-first-deploy-verify

**Drift**: BEHAVIOR-CLAIM + INVISIBLE-STATE + "auto-closes"
**Action**: REWRITE-PARTIAL, consolidate auto-close via CON-1

**Proposed prose**:
> Run `zerops_verify serviceHostname="{hostname}"`. The returned `status` is `healthy`, `degraded`, or `unhealthy`; scan `checks[]` for any with `status: fail` and read its `detail` for the specific failure. A passing verify corresponds to: service `status=ACTIVE` in `zerops_discover`, HTTP 200 from the subdomain root (or configured `/status`), and every declared env var present at runtime. If unhealthy: run `zerops_logs severity="error" since="5m"` and check the common first-deploy misconfigs — app bound to `localhost` instead of `0.0.0.0`, `run.start` invoking a build command, `run.ports.port` mismatched with actual listen port, or env var name drift. Fix in place, redeploy, re-verify; stop after 5 failed attempts. Auto-close behavior is described in `develop-auto-close-semantics`.

**Frontmatter**:
```yaml
references-fields:
  - ops.VerifyResult.Status
  - ops.VerifyResult.Checks
  - ops.CheckResult.Status
  - ops.CheckResult.Detail
references-atoms:
  - develop-auto-close-semantics
```

**Verdict**: APPROVED

---

### FD-3 · develop-first-deploy-intro

**Drift**: INVISIBLE-STATE — "passing verify marks the service deployed"
**Action**: REWRITE-PARTIAL
**Render-gap**: A

**Proposed prose**:
> You're in the develop first-deploy branch because the envelope reports at least one in-scope service with `deployed: false` (bootstrapped but never received code). Finish that here: scaffold `zerops.yaml`, write the app, deploy, verify. For each never-deployed runtime: (1) scaffold `zerops.yaml` from the planned runtime + env-var catalog from `zerops_discover` (see `develop-first-deploy-scaffold-yaml`). (2) Write real application code, not placeholder. (3) Run `zerops_deploy targetService=<hostname>` with no `strategy` argument — every first deploy uses the default push path; `strategy=git-push` requires `GIT_TOKEN` + committed code (container) or a configured git remote (local), neither ready yet. (4) Run `zerops_verify serviceHostname=<hostname>`; a passing verify combined with a recorded successful deploy flips the envelope's `deployed: true` on the next envelope build. Do not skip to edits before the first deploy lands — SSHFS mounts can be empty and HTTP probes return errors before any code is delivered.

**Frontmatter**:
```yaml
references-fields:
  - workflow.ServiceSnapshot.Deployed
  - workflow.DeployState
  - ops.VerifyResult.Status
references-atoms:
  - develop-first-deploy-scaffold-yaml
```

**Verdict**: APPROVED

---

### FD-4 · develop-first-deploy-promote-stage

**Drift**: BEHAVIOR-CLAIM — "auto-closes once both halves show a passing verify"
**Action**: REWRITE-PARTIAL, reference CON-1

**Proposed prose**:
> Standard mode pairs dev + stage. After `{hostname}` verifies, cross-deploy to `{stage-hostname}`:
>
> ```
> zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"
> zerops_verify serviceHostname="{stage-hostname}"
> ```
>
> No second build — cross-deploy packages the dev tree straight into stage. Auto-close requires BOTH halves verified; see `develop-auto-close-semantics`. Skipping stage leaves the session active and blocks auto-close.

**Frontmatter**:
```yaml
references-fields:
  - workflow.ServiceSnapshot.StageHostname
references-atoms:
  - develop-auto-close-semantics
```

**Verdict**: APPROVED

---

### FD-5 · develop-first-deploy-write-app

**Drift**: BEHAVIOR-CLAIM — "Bootstrap already initialized `/var/www/.git/` container-side"
**Action**: REWRITE-PARTIAL

**Proposed prose** (targets only the git-init sentence; rest of atom preserved):
> Do not run `git init` from the ZCP-side mount. Push-dev deploy handlers manage the container-side git state — calling `git init` on the SSHFS mount (`cd /var/www/{hostname}/ && git init`) creates `.git/objects/` owned by root, which breaks the container-side `git add` the deploy handler runs. Recovery if this already happened: `ssh {hostname} "sudo rm -rf /var/www/.git"` — the next deploy re-initializes it.

**Frontmatter**: (no new response fields; pitfall-only atom)
```yaml
references-fields: []
```

**Verdict**: APPROVED

---

### FD-6 · develop-first-deploy-scaffold-yaml

**See Phase -2 (P-2.3)** — spec-ID strip. After Phase -2, atom body is clean. Additional rewrite (observe-form alignment):

**Proposed addition to content-root tip** (replaces stripped DM-5 reference):
> For runtimes that expect assets at `ContentRootPath = CWD` (e.g. ASP.NET's `wwwroot/` lookup at `/var/www/wwwroot/`), use `./out/~` tilde-extract so contents land at `/var/www/` instead of `/var/www/out/`. Preserve (`./out`) when `start` names an explicit path like `./out/app/App.dll`.

**Frontmatter**:
```yaml
references-fields:
  - ops.DiscoverResult.Services
  - workflow.ServiceSnapshot.Mode
  - workflow.ServiceSnapshot.StageHostname
references-atoms:
  - develop-deploy-modes
```

**Verdict**: APPROVED

---

## Family C — develop close / push / deploy-modes (8 atoms)

### C-1 · develop-change-drives-deploy [NEEDS-REVISION]

**Drift**: BEHAVIOR-CLAIM — "auto-closes"
**Edge**: iteration-cap close reason missing. Prose must handle `CloseReasonIterationCap`.

**Proposed prose**:
> Editing files on the SSHFS mount (or locally in local mode) persists only inside the current container — the next deploy rebuilds the container from scratch, and anything not covered by `deployFiles` is discarded. The rule is: edit → deploy (via the active strategy) → verify. Auto-close semantics are described in `develop-auto-close-semantics`; `closeReason` values you can observe are `auto-complete` (every in-scope service passed) and `iteration-cap` (retry ceiling hit). Explicit `zerops_workflow action="close" workflow="develop"` emits the same closed state; it's rarely needed because starting a new task with a different `intent` replaces the session. Session close is cleanup, not commitment — close always succeeds.

**Frontmatter**:
```yaml
references-atoms:
  - develop-auto-close-semantics
```

**Verdict**: APPROVED (iteration-cap edge case handled)

---

### C-2 · develop-close-push-dev-dev [NEEDS-REVISION]

**Drift**: BEHAVIOR-CLAIM — handler-internal language about spawn/probe
**Edge**: worker services (no HTTP probe): `Port=0`, `HealthStatus=0`. Prose must handle.

**Proposed prose**:
> Dev mode has no stage pair: deploy the single runtime container, start the dev server, verify.
>
> ```
> zerops_deploy targetService="{hostname}" setup="dev"
> zerops_dev_server action=start hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"
> zerops_verify serviceHostname="{hostname}"
> ```
>
> After each deploy the container is new and the previous dev server is gone — check `zerops_dev_server action=status` first. If `running: true`, proceed to verify. Otherwise call `action=start`; the response carries `healthStatus` (HTTP status from the probe), `startMillis` (time-to-healthy), `logTail` (last log lines), and on failure a `reason` code for diagnosis (see `develop-dev-server-reason-codes`). For no-HTTP workers (no `port`/`healthPath`), `running` is derived from the post-spawn liveness check; `healthStatus` stays 0 — use `action=logs` to confirm consumption.

**Frontmatter**:
```yaml
references-fields:
  - ops.DevServerResult.Running
  - ops.DevServerResult.HealthStatus
  - ops.DevServerResult.StartMillis
  - ops.DevServerResult.Reason
  - ops.DevServerResult.LogTail
references-atoms:
  - develop-dev-server-reason-codes
```

**Verdict**: APPROVED (worker case handled; consolidation via CON-2)

---

### C-3 · develop-closed-auto

**Drift**: BEHAVIOR-CLAIM — "auto-closed; the work is durable"
**Action**: REWRITE-FULL, reference CON-1

**Proposed prose**:
> The envelope's `phase: develop-closed-auto` is set because every in-scope service has a successful deploy and a passing verify, and the session's `closeReason` is `auto-complete`. Work is durable — code is in git, infrastructure on the platform. Start the next task with `zerops_workflow action="start" workflow="develop" intent="{next-task}"` (replaces this session) or explicitly close via `zerops_workflow action="close" workflow="develop"`. Until one of those happens, further deploy attempts attach to this already-completed session. Full auto-close semantics: `develop-auto-close-semantics`.

**Frontmatter**:
```yaml
references-fields:
  - workflow.Phase
  - workflow.WorkSessionSummary.ClosedAt
  - workflow.WorkSessionSummary.CloseReason
references-atoms:
  - develop-auto-close-semantics
```

**Verdict**: APPROVED

---

### C-4 · develop-env-var-channels

**Drift**: BEHAVIOR-CLAIM — "Tool auto-restarts the affected service(s)"
**Action**: REWRITE-PARTIAL

**Proposed prose**:
> Two channels set env vars, and the channel determines when the value goes live.
>
> | Channel | Set with | When live |
> |---|---|---|
> | Service-level env | `zerops_env action="set"` | Response's `restartedServices` lists hostnames whose containers were cycled; `restartedProcesses` has platform Process details. |
> | `run.envVariables` | Edit `zerops.yaml`, commit, deploy | Full redeploy. `zerops_manage action="reload"` does NOT pick them up. |
> | `build.envVariables` | Edit `zerops.yaml`, commit, deploy | Next build uses them; not visible at runtime. |
>
> To suppress the service-level restart, pass `skipRestart=true` — the response then reports `restartSkipped: true` and `nextActions` tells you to restart manually before the value is live. Partial failures surface in `restartWarnings`. Read `stored` to confirm exactly which keys landed.
>
> **Shadow-loop pitfall**: a service-level env var set via `zerops_env` shadows the same key declared in `run.envVariables`. If you set `DB_HOST` via `zerops_env` and later fix it in `zerops.yaml`, redeploys will not change the live value. Delete the service-level key first (`zerops_env action="delete"`), then redeploy.

**Frontmatter**:
```yaml
references-fields:
  - tools.envChangeResult.RestartedServices
  - tools.envChangeResult.RestartWarnings
  - tools.envChangeResult.RestartSkipped
  - tools.envChangeResult.RestartedProcesses
  - tools.envChangeResult.Stored
```

**Verdict**: APPROVED

---

### C-5 · develop-mode-expansion

**Drift**: INVISIBLE-STATE — "Preserve your deploy strategy, `BootstrappedAt`, and first-deploy timestamp on the upgraded `ServiceMeta`"
**Action**: REWRITE-PARTIAL
**Render-gap**: A (needs deployed/bootstrapped tokens)

**Proposed prose**:
> The envelope reports your current service with `mode: dev` or `mode: simple` (single-slot). Expanding to **standard** adds a stage sibling without touching the existing service. Expansion is an infrastructure change — it runs through the bootstrap workflow, not develop. Start `zerops_workflow action="start" workflow="bootstrap" intent="expand {hostname} to standard — add stage"`, then submit a plan that flags the existing runtime (`isExisting: true`, `bootstrapMode: "standard"`) and names the new `stageHostname`. Bootstrap leaves the existing service's code and container untouched, creates the new stage service via `zerops_import`, and at close the envelope shows both the original (now `mode: standard` with `stageHostname` set, `bootstrapped: true`, `deployed: true`, strategy intact) and the new stage snapshot (`mode: stage`, `bootstrapped: true`, `deployed: false`). After close, run a dev→stage cross-deploy to verify the pair end-to-end.

**Frontmatter**:
```yaml
references-fields:
  - workflow.ServiceSnapshot.Mode
  - workflow.ServiceSnapshot.Strategy
  - workflow.ServiceSnapshot.StageHostname
  - workflow.ServiceSnapshot.Bootstrapped
  - workflow.ServiceSnapshot.Deployed
```

**Verdict**: APPROVED

---

### C-6 · develop-push-dev-workflow-dev

**Drift**: BEHAVIOR-CLAIM — "`action=restart` is stop + start composed. Port-free polling handles SO_REUSEADDR linger"
**Action**: REWRITE-PARTIAL, reference CON-2

**Proposed prose**:
> Edit code on `/var/www/{hostname}/` — changes appear instantly inside the container. After each edit, run `zerops_dev_server action=restart hostname="{hostname}" command="{start-command}" port={port} healthPath="{path}"`; the response has `running`, `healthStatus`, `startMillis`, and on failure a `reason` code (see `develop-dev-server-reason-codes`). For code-only changes, `action=restart` is enough — no redeploy. For `zerops.yaml` changes (env vars, ports, run-block fields), run `zerops_deploy` first: the redeploy replaces the container, so follow with `action=start` (not `restart`) on the new container. If iteration goes sideways, `action=logs logLines=60` returns the log ring.

**Frontmatter**:
```yaml
references-fields:
  - ops.DevServerResult.Reason
  - ops.DevServerResult.Running
  - ops.DevServerResult.HealthStatus
  - ops.DevServerResult.LogTail
  - ops.DevServerResult.StartMillis
references-atoms:
  - develop-dev-server-reason-codes
```

**Verdict**: APPROVED

---

### C-7 · develop-strategy-awareness

**Drift**: INVISIBLE-STATE — "per-service metas track them independently"
**Action**: REWRITE-PARTIAL

**Proposed prose**:
> Each runtime service in the envelope has a `strategy` field: `push-dev` (SSH self-deploy from the dev container), `push-git` (push committed code to an external git remote — carries a `trigger: webhook|actions|unset` sub-field), `manual` (you orchestrate every deploy yourself), or `unset` (bootstrap-written placeholder; develop picks one on first use). The rendered Services block shows this as `strategy=push-dev|push-git|manual|unset`. Switch at any time without closing the session: `zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}`. Mixed strategies across services in one project are fine — each service's strategy is independent in the envelope.

**Frontmatter**:
```yaml
references-fields:
  - workflow.ServiceSnapshot.Strategy
  - workflow.ServiceSnapshot.Trigger
  - workflow.DeployStrategy
  - workflow.PushGitTrigger
  - workflow.StrategyUnset
```

**Verdict**: APPROVED

---

### C-8 · develop-push-dev-deploy-container

**See Phase -2 (P-2.4)** — spec-ID strip first. After Phase -2, atom observable-aligned:

**Proposed prose**:
> The dev container uses SSH push — `zerops_deploy` uploads the working tree from `/var/www/{hostname}/` straight into the service without a git remote. No credentials on your side: the tool SSHes using ZCP's container-internal key. The response's `mode` is `ssh`; `sourceService` and `targetService` identify the deploy class.
> - Self-deploy (single service): `zerops_deploy targetService="{hostname}"` — `sourceService == targetService`, class is self.
> - Cross-deploy (dev → stage): `zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"` — class is cross.
>
> `deployFiles` discipline differs per class: self-deploy needs `[.]` (narrower patterns destroy the target's source); cross-deploy cherry-picks build output. See `develop-deploy-modes` for the full rule and `develop-deploy-files-self-deploy` for the self-deploy invariant.

**Frontmatter**:
```yaml
references-fields:
  - ops.DeployClass
  - ops.DeployResult.Mode
  - ops.DeployResult.SourceService
  - ops.DeployResult.TargetService
references-atoms:
  - develop-deploy-modes
  - develop-deploy-files-self-deploy
```

**Verdict**: APPROVED

---

## Family D — dynamic / checklist / idle / export / push-git (9 atoms)

### D-1 · develop-dynamic-runtime-start-container

**Drift**: ANTHROPOMORPHISM + BEHAVIOR-CLAIM — "dev process is agent-owned" + "The tool detaches (ssh -T -n + setsid + stdio redirect), bounds every phase with a tight budget (spawn 8 s, probe waitSeconds+5 s, tail 5 s)"
**Action**: REWRITE-FULL

**Proposed prose**:
> Dev-mode dynamic-runtime containers start running `zsc noop` after deploy — no dev process is live until you start one. Use `zerops_dev_server action=start hostname={hostname} command="{start-command}" port={port} healthPath="{path}"`. The response has `running`, `healthStatus` (HTTP status of the health probe), `startMillis` (time from spawn to healthy), and on failure a concrete `reason` code plus `logTail` so you can diagnose without a follow-up call. Use `action=status` before `action=start` when uncertain — avoids spawning a duplicate listener. Use `action=restart` for config/code changes that survived the deploy. Use `action=logs logLines=40` for diagnosis, `action=stop` to free the port. After every redeploy the container is new and the previous dev process is gone — call `action=start` again before `zerops_verify`. Do not hand-roll `ssh {hostname} "cmd &"`: backgrounded commands hold the SSH channel open until the 120-second timeout because the child still owns stdio. See `develop-dev-server-reason-codes` for diagnosing `reason` values.

**Frontmatter**:
```yaml
references-fields:
  - ops.DevServerResult.Running
  - ops.DevServerResult.HealthStatus
  - ops.DevServerResult.StartMillis
  - ops.DevServerResult.Reason
  - ops.DevServerResult.LogTail
  - ops.DevServerResult.Port
  - ops.DevServerResult.HealthPath
references-atoms:
  - develop-dev-server-reason-codes
```

**Verdict**: APPROVED

---

### D-2 · develop-dev-server-triage [NEEDS-REVISION]

**Drift**: ANTHROPOMORPHISM — table cell "**Agent** — starts via tool"
**Edge**: no-HTTP-probe workers — prose must not force HTTP status check

**Proposed prose**:
> Before deploying, verifying, or iterating on a runtime service, run the triage instead of blind-starting a process.
>
> **Step 1 — Determine the expectation** from `runtimeClass` + `mode` in the envelope:
>
> | Envelope shape | Deployed runtime shape | Dev-server lifecycle |
> |---|---|---|
> | `runtimeClass: implicit-webserver` | Always live post-deploy | Platform-owned — no manual start |
> | `runtimeClass: dynamic`, `mode: dev` | `zsc noop` idle container | You start it via `zerops_dev_server action=start` |
> | `runtimeClass: dynamic`, `mode: simple\|stage` | Foreground binary with `healthCheck` | Platform auto-starts and probes |
>
> If the envelope reports implicit-webserver, static, or simple/stage-mode dynamic, triage ends — platform owns lifecycle.
>
> **Step 2 — Check current state** for dev-mode dynamic:
>
> ```
> # container env
> zerops_dev_server action=status hostname="{hostname}" port={port} healthPath="{path}"
> # local env — runs on your machine
> Bash command="curl -s -o /dev/null -w '%{http_code}' --max-time 2 http://localhost:{port}{path}"
> ```
>
> Read the response: `running: true` with HTTP 2xx/3xx/4xx `healthStatus` → proceed to `zerops_verify`. `running: false` with `reason: health_probe_connection_refused` → start (step 3). `running: true` with `healthStatus: 5xx` → server runs but is broken; read logs and response body; do NOT restart (it does not fix bugs); edit code then deploy.
>
> For workers with no HTTP surface (`port=0`, `healthPath=""`), skip HTTP status; call `zerops_logs` to confirm consumption.
>
> **Step 3 — Act on the delta.** In container env: `zerops_dev_server action=start …`. In local env: `Bash run_in_background=true command="{start-command}"`. After every redeploy the dev process is gone — re-run Step 2 before `zerops_verify`.

**Frontmatter**:
```yaml
references-fields:
  - workflow.ServiceSnapshot.RuntimeClass
  - workflow.RuntimeClass
  - workflow.ServiceSnapshot.Mode
  - ops.DevServerResult.Running
  - ops.DevServerResult.HealthStatus
  - ops.DevServerResult.Reason
references-atoms:
  - develop-dev-server-reason-codes
```

**Verdict**: APPROVED (worker case handled)

---

### D-3 · develop-checklist-dev-mode

**Drift**: ANTHROPOMORPHISM — "(agent owns the dev process and starts it via `zerops_dev_server` after each deploy)"
**Action**: REWRITE-PARTIAL

**Proposed prose**:
> Dev-mode checklist extras (container env):
> - Dev setup block in `zerops.yaml`: `start: zsc noop --silent`, **no** `healthCheck`. The platform keeps the container idle; you start the dev process yourself via `zerops_dev_server action=start` after each deploy.
> - Stage setup block (if a dev+stage pair exists): real `start:` command **plus** a `healthCheck`. Stage auto-starts on deploy and the platform probes it on its configured interval.

**Frontmatter**:
```yaml
references-fields:
  - workflow.ServiceSnapshot.Mode
```

**Verdict**: APPROVED

---

### D-4 · develop-platform-rules-container

**Drift**: BEHAVIOR-CLAIM — "It detaches the process correctly, bounds every phase with a tight budget, and returns structured `{running, healthStatus, reason, logTail}`"
**Action**: REWRITE-PARTIAL

**Proposed prose** (targets the dev-server paragraph specifically):
> Dev server lifecycle in container env: `zerops_dev_server` (actions start/status/stop/restart/logs) is the canonical primitive. The response has `running`, `healthStatus`, `startMillis`, `reason`, and `logTail` — all needed for diagnosing start failures without a follow-up call. Do not hand-roll `ssh ... "cmd &"` — backgrounded SSH commands hold the channel open until the 120 s timeout. See `develop-dynamic-runtime-start-container` for the canonical start recipe and `develop-dev-server-reason-codes` for `reason` value triage.

**Frontmatter**:
```yaml
references-fields:
  - ops.DevServerResult.Running
  - ops.DevServerResult.HealthStatus
  - ops.DevServerResult.StartMillis
  - ops.DevServerResult.Reason
  - ops.DevServerResult.LogTail
references-atoms:
  - develop-dynamic-runtime-start-container
  - develop-dev-server-reason-codes
```

**Verdict**: APPROVED

---

### D-5 · idle-adopt-entry

**Drift**: INVISIBLE-STATE + BEHAVIOR-CLAIM — "have no ZCP bootstrap metadata. Adopt them … writes `ServiceMeta`"
**Action**: REWRITE-PARTIAL

**Proposed prose**:
> Runtime services exist in this project that ZCP is not tracking — the Services block shows one or more as `not bootstrapped`. Adopt them to enable ZCP deploy and verify workflows: `zerops_workflow action="start" workflow="bootstrap"` → read `routeOptions[]` → dispatch `route="adopt"`. After close, the envelope shows each adopted hostname with `bootstrapped: true` and its existing mode/strategy preserved.

**Frontmatter**:
```yaml
references-fields:
  - workflow.ServiceSnapshot.Bootstrapped
  - workflow.BootstrapRouteOption.AdoptServices
```

**Verdict**: APPROVED

---

### D-6 · idle-develop-entry

**Drift**: BEHAVIOR-CLAIM — "The develop workflow tracks deploys and verifies, and auto-closes when the task is complete"
**Action**: POLISH

**Proposed prose**:
> The project has at least one bootstrapped service ready to receive code. Start a develop session: `zerops_workflow action="start" workflow="develop" intent="{task}" scope=["{hostname}",…]`. The envelope will flip to `phase: develop-active`; subsequent status calls show `workSession.deploys[]` and `workSession.verifies[]` as you iterate. Auto-close semantics: `develop-auto-close-semantics`.

**Frontmatter**:
```yaml
references-fields:
  - workflow.Phase
  - workflow.WorkSessionSummary.Deploys
  - workflow.WorkSessionSummary.Verifies
references-atoms:
  - develop-auto-close-semantics
```

**Verdict**: APPROVED

---

### D-7 · export

**Drift**: BEHAVIOR-CLAIM — "The tool handles `.netrc` auth from `GIT_TOKEN`, remote add/update, push, and cleanup"
**Action**: REWRITE-PARTIAL (targets section 10 of atom)

**Proposed prose** (replaces the push-section drift):
> After the commit on the container lands, push via `zerops_deploy`:
>
> ```
> zerops_deploy targetService="{targetHostname}" strategy="git-push" remoteUrl="{repoUrl}" branch="main"
> ```
>
> For later pushes (remote already on container), drop `remoteUrl`. The response's `status` confirms success; `warnings[]` surface non-fatal issues. On failure: if `platform.APIError.code` is `GIT_TOKEN_MISSING`, set the token via `zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]` and retry; if `PREREQUISITE_MISSING: requires committed code`, the container has no commit — re-run the earlier commit step. Do not run `git init`, `git config user.*`, or `git remote add` manually — the deploy tool owns the git-push shape.

**Frontmatter**:
```yaml
references-fields:
  - ops.ExportResult.ExportYAML
  - ops.ExportResult.Services
  - ops.DeployResult.Status
  - ops.DeployResult.Warnings
  - platform.APIError.Code
```

**Verdict**: APPROVED

---

### D-8 · strategy-push-git-push-container

**Drift**: BEHAVIOR-CLAIM — "`zerops_deploy` handles `.netrc` provisioning, `git remote add`, and cleanup automatically"
**Action**: REWRITE-PARTIAL

**Proposed prose**:
> The container has no user credentials, so pushes to the external git remote run under `GIT_TOKEN`. Wire the pieces:
> 1. Set `GIT_TOKEN` as a project env var. Token scopes: GitHub fine-grained needs `Contents: Read and write` (add `Secrets` + `Workflows` if pairing with Actions); GitLab personal access needs `write_repository` (add `api` for Webhook).
>    ```
>    zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]
>    ```
> 2. Commit + first push:
>    ```
>    ssh {targetHostname} "cd /var/www && git add -A && git commit -m 'initial commit'"
>    zerops_deploy targetService="{targetHostname}" strategy="git-push" remoteUrl="{repoUrl}" branch="main"
>    ```
>
> The response's `status` confirms the push; if `platform.APIError.code` is `PREREQUISITE_MISSING: requires committed code`, the container's `/var/www` has no commit — run the ssh commit step again and retry. Ongoing pushes omit `remoteUrl`: `zerops_deploy targetService="{targetHostname}" strategy="git-push"`. Do not run `git init`, `.netrc`-configuration, or `git remote add` manually — the deploy tool owns the git-push shape.

**Frontmatter**:
```yaml
references-fields:
  - ops.DeployResult.Status
  - ops.DeployResult.Warnings
  - platform.APIError.Code
```

**Verdict**: APPROVED

---

### D-9 · strategy-push-git-push-local

**Drift**: BEHAVIOR-CLAIM — "`zerops_deploy` runs `git push origin <branch>` with `GIT_TERMINAL_PROMPT=0`"
**Action**: REWRITE-PARTIAL

**Proposed prose**:
> Local env pushes use the user's own git credentials (SSH keys, Keychain, credential manager) — no `GIT_TOKEN`, no `.netrc`. ZCP does not manage credentials on the local path; it orchestrates `git push`. Confirm repo + origin first:
>
> ```
> git -C <your-project-dir> rev-parse HEAD          # must have a commit
> git -C <your-project-dir> remote get-url origin   # should match intended repo
> ```
>
> Add an origin if missing (`git remote add origin <url>`) or pass `remoteUrl=<url>` on the first deploy — the tool refuses to silently rewrite a mismatched existing origin. Commit and push:
>
> ```
> git -C <your-project-dir> add -A
> git -C <your-project-dir> commit -m "your message"
> zerops_deploy targetService="{targetHostname}" strategy="git-push"
> ```
>
> A passphrase-protected key without an agent fails fast — the response's `status` is non-success and `warnings` identifies the auth problem (`ssh-add -l` for SSH agents, `git credential fill` for the credential manager). ZCP never sets `GIT_TOKEN` on the project for local path, never runs `git init`, never writes `git config`. Uncommitted changes are WARNED about via `warnings[]` but not pushed — pushed state is the branch HEAD.

**Frontmatter**:
```yaml
references-fields:
  - ops.DeployResult.Status
  - ops.DeployResult.Warnings
  - ops.DeployResult.Message
```

**Verdict**: APPROVED

---

## Phase 4 — Consolidation (2 new atoms)

### CON-1 · develop-auto-close-semantics (NEW)

**Purpose**: single source of truth for work session close behavior, replacing duplication in FD-2, FD-4, C-1, C-3.

**Full atom file** (create as `internal/content/atoms/develop-auto-close-semantics.md`):

```markdown
---
id: develop-auto-close-semantics
priority: 4
phases: [develop-active, develop-closed-auto]
title: "Work session auto-close semantics"
references-fields:
  - workflow.WorkSessionSummary.ClosedAt
  - workflow.WorkSessionSummary.CloseReason
  - workflow.Phase
  - workflow.CloseReasonAutoComplete
  - workflow.CloseReasonIterationCap
---

### Work session auto-close

Work sessions close automatically when either of two conditions hold:
- **`auto-complete`** — every service in scope has both a successful deploy and a passing verify. The envelope's `workSession.closedAt` becomes set, `closeReason: auto-complete`, and `phase` flips to `develop-closed-auto`.
- **`iteration-cap`** — the workflow's retry ceiling was hit. Same close-state shape; `closeReason: iteration-cap`.

Explicit `zerops_workflow action="close" workflow="develop"` emits the same closed state manually and is rarely needed — starting a new task with a different `intent` replaces the session.

For standard-mode pairs, "every service in scope" includes BOTH halves — skipping the stage cross-deploy leaves the session active. For dev-only or simple services, a single successful deploy + verify is enough.

Close is cleanup, not commitment. Work itself is durable — code is in git, infrastructure is on the platform.
```

**Verdict**: CREATE (new atom; referenced by 4 existing atoms via `references-atoms` frontmatter)

---

### CON-2 · develop-dev-server-reason-codes (NEW)

**Purpose**: single source of truth for `DevServerResult.Reason` value triage, replacing duplication in D-1, D-2, C-2, C-6.

**Full atom file** (create as `internal/content/atoms/develop-dev-server-reason-codes.md`):

```markdown
---
id: develop-dev-server-reason-codes
priority: 4
phases: [develop-active]
runtimes: [dynamic]
title: "zerops_dev_server reason codes"
references-fields:
  - ops.DevServerResult.Reason
  - ops.DevServerResult.Running
  - ops.DevServerResult.HealthStatus
  - ops.DevServerResult.LogTail
---

### `reason` values (DevServerResult)

When `zerops_dev_server` actions fail, the response's `reason` field classifies the failure so you don't need a follow-up call to diagnose. Dispatch table:

| `reason` | Meaning | Action |
|---|---|---|
| `spawn_timeout` | The remote shell did not detach; stdio handle still owned by child. | You likely hand-rolled `ssh ... "cmd &"` — re-run through `zerops_dev_server action=start`. |
| `health_probe_connection_refused` | Spawn succeeded but nothing is listening on `port`. | Check that your app binds to `0.0.0.0` (not `localhost`), that `port` matches `run.ports[0].port`, and that your start command actually starts a server. Read `logTail` for crash output. |
| `health_probe_http_<code>` | Server runs but returned `<code>` (e.g. 500, 404). | Do NOT restart — it does not fix bugs. Read `logTail` + response body, edit code, deploy. |
| `post_spawn_exit` | No-probe-mode process died after spawn (port=0/healthPath=""). | `action=logs` for consumption errors; typical for worker crashes. |

Observable always: `running` (bool), `healthStatus` (HTTP status when `port` set, 0 otherwise), `startMillis` (time from spawn to healthy), `logTail` (last log lines). Use these to confirm state without a second tool call.
```

**Verdict**: CREATE (new atom; referenced by 4 existing atoms)

---

## Summary counts

- **Phase -2**: 4 atoms × spec-ID strip + 2 knowledge-base edits = 6 edits
- **Phase 0 Family A**: 6 atom rewrites + 1 DELETE (bootstrap-write-metas)
- **Phase 0 Family B**: 6 atom rewrites
- **Phase 0 Family C**: 8 atom rewrites
- **Phase 0 Family D**: 9 atom rewrites
- **Phase 4 consolidation**: 2 new atoms created
- **NEEDS-REVISION resolved**: 5 (B-2 adopt scope, B-7 delete, C-1 iteration-cap, C-2 no-HTTP worker, D-2 no-HTTP worker)

Corpus size progression: 75 (today) → 75 (Phase -2 strip) → 74 (Phase 0 B-7 delete) → 76 (Phase 4 create 2).

Final state: 76 atoms, all observer-form, all with `references-fields` where they reference response/envelope fields, cross-references between atoms via `references-atoms` frontmatter, zero spec-ID citations, zero invisible-state claims, zero anthropomorphism.
