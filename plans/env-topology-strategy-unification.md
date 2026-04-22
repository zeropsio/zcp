# Plan: Env × Topology × Strategy unification

**Status**: Draft — awaiting approval before Task creation and implementation.

**Scope**: Complete cleanup of legacy artefacts in how ZCP models environment
(container/local), service topology (dev/standard/simple/local-stage/local-only),
deploy strategy (push-dev/push-git/manual), and the vocabularies supporting
them. Collapses duplicated constants, drops artificial env-based tool
differences, introduces auto-adoption for local env, and aligns the atom
corpus to a single consistent vocabulary.

**Authorship principle**: single canonical path. Every axis should have one
typed enum, one source of truth, one dispatch point. Variants are bug
vectors (per CLAUDE.local.md `feedback_single_path_engineering.md`).

**Time budget**: unbounded. Quality over speed.

---

## 1. Why — the current mess

Over the project's lifetime, ZCP accumulated artefacts from earlier stricter
designs. Three dominant failure modes:

**Artefact class A — container-centric vocabulary leaking into local**.
Local env was added as a second-class citizen: its ServiceMeta layout is a
hack on top of the container one (`invertLocalHostname` swaps Hostname with
StageHostname). `PrimaryRole()`, `resolveEnvelopeMode()`, atom filters all
carry special-cases for local+standard. Local+managed-only has no
ServiceMeta at all, creating a bimodal state.

**Artefact class B — duplicated/drifting vocabularies**. `DeployRole*`
constants exist in TWO packages (workflow + ops, with ops missing Simple).
`PlanMode* / Mode* / DeployRole*` are three parallel enums for the same
concept. `DeployStrategy` is untyped in ServiceMeta but typed in envelope.
Setup names in zerops.yaml (`dev|prod|worker`) coincidentally share strings
with ZCP mode values (`dev|standard|simple`) but mean different things.

**Artefact class C — the strict-suffix era**. Originally ZCP required
service names to end in `dev`/`stage`. This proved unmaintainable for
adoption of pre-existing services (users named things arbitrarily), so
arbitrary names became allowed. But the heuristic
(`strings.CutSuffix(hostname, "dev")` and friends) remained scattered on
**5 places** across the codebase as **primary** signals that silently
pattern-match. Adopting a service named `frontend-app` silently does the
wrong thing on several of those paths. Per CLAUDE.md rule "Never add
fallbacks — they mask bugs that compound silently", these heuristics are
deleted outright. Pairing/role must come from explicit signals (plan,
meta, user answer), never from string patterns.

**Artefact class D — "IsDeployed" as a persistent flag**. `FirstDeployedAt`
is stamped only when ZCP-driven `zerops_verify` passes. Adopted services
that were already running before ZCP touched them have `FirstDeployedAt=""`
forever, and downstream gates (`handleGitPush`, `export.md`, atom filter
`deployStates: [never-deployed]`) fail false-positive.

Beyond these four classes: tool schema asymmetry (`DeploySSHInput` vs
`DeployLocalInput`), `Environment` field persisted in meta despite being
runtime-detected, and `WorkSession` reaching across boundaries to mutate
`ServiceMeta`.

---

## 2. Target model (authoritative)

### 2.1 Three axes

1. **Environment** — runtime-detected: `container` (ZCP runs as zcpx inside
   Zerops project) or `local` (ZCP runs on user's stroj). Never persisted
   in per-service state — derived from `Engine.environment` at read time.
2. **Mode / Topology** — per-service. Container enum: `dev | standard |
   simple`. Local enum: `local-stage | local-only`. No overlap; local
   values never written in container env and vice versa.
3. **DeployStrategy** — per-service. Enum: `push-dev | push-git | manual`.
   Symmetric across env; implementation differs per env (container: SSH
   mechanics; local: local exec).

### 2.2 Orthogonal sub-axes

- **PushGitTrigger** — valid only when `DeployStrategy == push-git`. Enum:
  `webhook | actions`. Recorded in ServiceMeta.
- **Managed presence** — derived from `client.ListServices()`. Not stored.
- **VPN status** — runtime probe. Not stored. Only meaningful in local env
  when managed services need dev-time reach.

### 2.3 ServiceMeta — unified shape

```go
type ServiceMeta struct {
    // Identity
    Hostname      string  // container: Zerops service name
                          // local: Zerops project name (from API)
    StageHostname string  // container+standard: paired stage Zerops hostname
                          // local-stage:         linked Zerops stage hostname
                          // all others:          ""

    // Topology
    Mode Mode            // typed enum, one per service; see §2.5

    // Deploy decisions
    DeployStrategy    DeployStrategy  // typed; see §2.5
    PushGitTrigger    PushGitTrigger  // typed; see §2.5
    StrategyConfirmed bool

    // Provenance
    BootstrapSession string  // empty for adopted services (incl. auto-adopted local)
    BootstrappedAt   string  // timestamp
    // FirstDeployedAt — DROPPED; see §2.6
    // Environment    — DROPPED; derived at read time from Engine.environment
}
```

### 2.4 Axiom for local env

In local env, "dev" is always the user's working directory. It is never
provisioned on Zerops, never has a ServiceMeta of its own. ServiceMeta in
local represents **the project** (identified by Zerops project name),
with optional linkage to a Zerops-side stage runtime via `StageHostname`.

### 2.5 Unified typed enums

Replace the three parallel enums (`PlanMode*`, `Mode*`, `DeployRole*`) with
one typed enum housed in `workflow/` and imported everywhere that needs it:

```go
// workflow/topology.go (new file; renames deploy.go)
type Mode string
const (
    // Container topologies:
    ModeDev      Mode = "dev"      // single dev container, no stage pair
    ModeStandard Mode = "standard" // dev + stage pair; dev is primary
    ModeStage    Mode = "stage"    // stage half of a standard pair (envelope-only projection)
    ModeSimple   Mode = "simple"   // single prod-flavored container

    // Local topologies:
    ModeLocalStage Mode = "local-stage" // user dir + Zerops stage runtime
    ModeLocalOnly  Mode = "local-only"  // user dir + optional managed; no Zerops runtime
)

type DeployStrategy string
const (
    StrategyUnset   DeployStrategy = ""
    StrategyPushDev DeployStrategy = "push-dev"
    StrategyPushGit DeployStrategy = "push-git"
    StrategyManual  DeployStrategy = "manual"
)

type PushGitTrigger string
const (
    TriggerUnset   PushGitTrigger = ""
    TriggerWebhook PushGitTrigger = "webhook"
    TriggerActions PushGitTrigger = "actions"
)
```

The old `PlanMode*` string constants are **deleted**. Any code that was
using them gets the typed equivalent via import.

The old `DeployRole*` duplicate in `ops/deploy_validate.go` is **deleted**.
`ops` imports from `workflow` (reverse-direction import check: `workflow`
doesn't import `ops`, so this is safe).

### 2.6 "IsDeployed" — derived, not persistent

Current `ServiceMeta.FirstDeployedAt` is dropped. Replacement:

- `StateEnvelope.ServiceSnapshot.Deployed` is derived at envelope build time:

  ```
  Deployed := platformService.Status == "ACTIVE"
           OR hasSuccessfulDeployAttempt(ws, hostname)
  ```

- Atom filter `deployStates: [never-deployed|deployed]` evaluates against
  the derived envelope value, not against meta.
- `handleGitPush` precondition changes from `meta.IsDeployed()` to "repo
  has committed code at working dir" (`git log -1` succeeds). See §5.2.
- `develop` first-deploy branch detection uses the same derived predicate
  from envelope — matches what atoms see.
- `RecordVerifyAttempt` no longer calls `MarkServiceDeployed`. Cross-
  boundary write removed.

### 2.7 Unified deploy input schema

Single `DeployInput` type replaces `DeploySSHInput` and `DeployLocalInput`:

```go
// tools/deploy.go
type DeployInput struct {
    TargetService string
    SourceService string   // optional — cross-deploy; rejected in local
    Setup         string
    WorkingDir    string   // container: defaults to /var/www; local: defaults to CWD
    Strategy      string   // "", "git-push" — "manual" rejected
    RemoteURL     string   // for git-push
    Branch        string   // for git-push
    // IncludeGit — DROPPED as user-facing param; auto-decided internally
}
```

One `RegisterDeploy` function. Inside handler: env-aware dispatch into
shared `ops.Deploy()` which takes an injected executor (SSH or local).

### 2.8 Hostname suffix heuristic — deleted outright

All direct usages of `strings.CutSuffix(hostname, "dev")` /
`strings.Contains(hostname, "stage")` in **discovery** contexts are
**deleted**, not centralized. Pairing and role must come from explicit
signals:

| Signal source | Used by |
|---|---|
| `ServiceMeta.StageHostname` (explicit user decision) | router, envelope, deploy role lookup |
| Plan input (user-provided during bootstrap) | bootstrap validation |
| Explicit user answer during adoption | `LocalAutoAdopt`, container adopt |
| `Role` parameter passed by caller | `ops.ValidateZeropsYml` |

If a signal is missing, the code **refuses to guess** and surfaces a clear
error pointing to the explicit signal path. No "fallback" path.

Recipe code (where ZCP generates hostnames under its own convention) may
continue using its internal naming templates (e.g. `{base}dev`,
`{base}stage`) — those are not heuristics but authored conventions within
ZCP-controlled output. The distinction: recipe templates WRITE names, so
they know them; discovery READS names, so it must not guess them.

Concrete deletions (Phase B.4):
- `workflow/adopt.go:61,77` — suffix-based pairing heuristic → deleted;
  adoption now requires user to confirm pairing explicitly.
- `workflow/validate.go:62` — `CutSuffix(r.DevHostname, "dev")` → deleted;
  stage hostname must be declared on `RuntimeTarget` explicitly or not
  exist.
- `workflow/symbol_contract.go:351` — deleted.
- `ops/deploy_validate.go:78-79` — `strings.Contains(hostname, "dev"/"stage")`
  → deleted; `role` parameter becomes required (caller supplies from
  meta), not optional with hostname fallback.
- `ops/checks/symbol_contract_env.go:108` — reviewed case-by-case; if
  genuine symbol-contract check within ZCP-authored output, keep; if
  discovery, delete.

### 2.9 Auto-adoption on server init (local env)

Auto-adoption happens **eagerly at server startup** in `server.New()`,
before the MCP init handshake completes and before any tool handler runs.

**Invariant**: in local env, the user's working directory IS the project's
dev service. It always exists, it's always adopted. `server.New` writes
exactly one ServiceMeta for the local project on first run, representing
this fact. Re-runs are no-ops.

Rationale for eager:
- No intermediate "not adopted" state is ever observable to the LLM.
- One adoption point instead of N hooks scattered across tool handlers.
- Adoption IS part of ZCP initialization.
- ZCP already does API calls at startup (`auth.NewInfo` verifies token
  scope). Adding `GetProject` + `ListServices` is consistent.

Steps inside `server.New`:

1. `project, err := client.GetProject(ctx, projectID)` → fail-fast on err.
2. Read `ListServiceMetas(stateDir)`:
   - Non-empty → run legacy migration (§8.2); skip adoption.
   - Empty → continue to step 3.
3. `services, err := client.ListServices(ctx, projectID)` → fail-fast.
4. Classify Zerops-side runtimes:
   - Zero runtimes → `Mode=local-only`, `StageHostname=""`,
     `DeployStrategy=""` (unset; router prompts user to pick `push-git`
     or `manual`).
   - Exactly one runtime → `Mode=local-stage`,
     `StageHostname=<runtime-hostname>`, `DeployStrategy=""` (unset; router
     prompts for strategy choice among all three).
   - Multiple runtimes → `Mode=local-only`, `StageHostname=""`,
     `DeployStrategy=""`. The local dir is still adopted (always). User
     can later link one runtime as stage via explicit subaction
     (`zerops_workflow action="adopt-local" targetService=<hostname>`),
     upgrading Mode to `local-stage` and unlocking `push-dev` as an
     option.
5. Always write ServiceMeta: `Hostname=project.Name`,
   `BootstrapSession=""`, `BootstrappedAt=now`.
6. Adoption outcome (mode + any stage-link info + runtime enumeration when
   user has a choice) feeds into MCP instructions (§2.10).

Adoption **always succeeds** unless the API is unreachable (fail-fast).
There is no "ambiguity blocks adoption" case — ambiguity only concerns
optional stage linkage, which user resolves at their pace via the
subaction.

Failure policy:
- **API unreachable at startup** → fail-fast. `server.New` returns error.
  Same class as existing auth verification failures.
- **Anything else** → adoption completes. Server starts. Stage linkage
  decision (if any) handled via in-session subaction.

**Container env is NOT auto-adopted** — container bootstrap is explicit
(plans, modes, mounts), user drives via workflow. Only local gets this.

### 2.10 Adoption note in MCP instructions

When adoption runs on a fresh project (no existing local meta), the note
is appended to the MCP server instructions text. Three shapes depending
on classification:

**Stage auto-linked** (exactly one Zerops runtime existed):
```
<baseInstructions>
<localEnvironment>

Adopted project 'myproject' as local-stage (linked to myapp). Managed
services detected: db, cache. Run `zcli vpn up <projectId>` on your
machine for dev-time access.
```

**Stage linkage available but not auto-linked** (multiple runtimes):
```
<baseInstructions>
<localEnvironment>

Adopted project 'myproject' as local-only. Multiple Zerops runtime
services exist (myapp-api, myapp-web, myapp-worker) — none linked as
stage. Strategy options: `push-git` (push to external remote, ZCP
doesn't track what happens next) or `manual` (nothing automated).
`push-dev` requires linking one runtime as stage first:

  zerops_workflow action="adopt-local" targetService="<chosen-hostname>"
```

**No Zerops runtime** (managed-only or empty project):
```
<baseInstructions>
<localEnvironment>

Adopted project 'myproject' as local-only. No Zerops runtime services
exist. Strategy options: `push-git` (push to external remote — whatever
happens downstream is user's setup, ZCP doesn't track) or `manual`.
Managed: db, cache. Run `zcli vpn up` for dev-time access.
```

When meta already exists (not a fresh run), no note is emitted —
instructions stay clean (`<baseInstructions><localEnvironment>`).

### 2.11 Summary matrix

| env | mode | `Hostname` | `StageHostname` | Allowed strategies |
|---|---|---|---|---|
| container | dev | Zerops svc (e.g. `appdev`) | `""` | push-dev, push-git, manual |
| container | standard | Zerops dev svc | Zerops stage svc | push-dev, push-git, manual |
| container | simple | Zerops svc | `""` | push-dev, push-git, manual |
| local | local-stage | Zerops project name | Zerops stage svc | push-dev, push-git, manual |
| local | local-only | Zerops project name | `""` | **push-git, manual** (no push-dev — no stage target) |

Rationale for `push-git` being valid in `local-only`: the user can push
code to a git remote and have some downstream (Zerops webhook in a
different project, third-party CI, another team's build) do whatever.
ZCP doesn't care what happens after the push — it only needs to know
"run `git push` when user triggers a deploy". Zerops-side stage linkage
is not a prerequisite for pushing to git.

`push-dev` is the only strategy that IS gated on stage linkage — it
needs a deploy target (`zcli push`-able service), which `local-only`
doesn't have.

---

## 3. Scope

### 3.1 In scope

- Data model: `ServiceMeta` shape, enums, drop `Environment` + `FirstDeployedAt`.
- Tool surface: unified `DeployInput`, strategy dispatch in local, handleLocalGitPush.
- Auto-adoption for local env.
- Hostname suffix heuristic → deleted; explicit signals required at every call site.
- Atom corpus: split strategy-push-git (×5), env-aware first-deploy atoms,
  local platform-rules atom, env-aware close atoms, drop zcli leakage in
  tool descriptions and server instructions.
- Router: offer export only in container (local export deferred).
- VPN probe in `zerops_env generate-dotenv`.
- Migration for existing local meta files (eager one-shot at first server start after upgrade).

### 3.2 Out of scope (deferred to a later plan)

- Local-native export atom (requires separate design for user-local git flow).
- Splitting cross-deploy into its own tool (`zerops_promote`).
- Generalization of session files (bootstrap/work/recipe sessions remain distinct).
- `setup` fallback simplification (breaking, requires separate deprecation).
- `IsAdopted()` audit / removal.
- Monorepo in local (multiple stage targets per project — postpone, MVP = 1 stage per project).

### 3.3 Release split

To bound blast radius, split into **two releases**:

- **Release A — Topology & auto-adopt**. Everything relating to local env
  topology, auto-adoption, ServiceMeta shape, IsDeployed fix, deploy tool
  unification. Preserves current vocabularies elsewhere.
- **Release B — Vocabulary consolidation**. Drop duplicated DeployRole,
  unify Mode enum, delete hostname heuristic, drop Environment field,
  corpus vocabulary sweep.

Each release is self-contained and reversible. Release B can only start
after Release A stabilizes in production (minimum one release cycle of
user monitoring).

---

## 4. Data model changes (complete)

### 4.1 `ServiceMeta` fields — before/after

| Field | Before | After | Notes |
|---|---|---|---|
| `Hostname` | container: Zerops svc; local: Zerops stage (inverted) | container: Zerops svc; **local: Zerops project name** | Breaking semantic change for local |
| `StageHostname` | container+standard: stage; local: "" | container+standard: stage; **local-stage: Zerops runtime hostname** | Semantic change for local-stage |
| `Mode` | string: "dev"|"standard"|"simple" | typed `Mode`; adds `"local-stage"`, `"local-only"` | Rename read on disk |
| `DeployStrategy` | string | typed `DeployStrategy` | Same wire values |
| `PushGitTrigger` | — | **new** typed `PushGitTrigger` | |
| `StrategyConfirmed` | bool | bool | unchanged |
| `Environment` | string | **dropped** | Derived from Engine at read |
| `BootstrapSession` | string (empty=adopted) | string | unchanged |
| `BootstrappedAt` | string (date) | string | unchanged |
| `FirstDeployedAt` | string | **dropped** | Replaced by derived `Deployed` in envelope |

### 4.2 On-disk file layout

No change to file naming — still `.zcp/state/services/{hostname}.json`
keyed by `ServiceMeta.Hostname`. For local that means the filename is the
Zerops project name.

### 4.3 Legacy read-path tolerance

`ReadServiceMeta` and `parseMeta` accept legacy JSON and normalize in-memory:

- Legacy `Environment` field: read and discard (JSON decode ignores unknown
  fields via default struct marshalling, so no error).
- Legacy local meta with `Hostname=<stageHost>` and `Mode ∈ {standard,simple,dev}`:
  - Detect via `Mode` ∈ container set AND `Environment="local"` (legacy field still parsed for ONE release).
  - Migrate in-memory: `StageHostname = Hostname; Hostname = project.Name; Mode = local-stage`.
  - Persist migration on next `WriteServiceMeta` for this meta.
- Legacy `FirstDeployedAt`: read and discard. `Deployed` is now derived.

Migration happens lazily on read. One release cycle of tolerance, then
Release B removes the tolerance code.

### 4.4 Constants cleanup

`internal/workflow/bootstrap.go`:
- Delete: `PlanModeStandard`, `PlanModeDev`, `PlanModeSimple` string constants.
- Replace with imports from `workflow/topology.go` Mode enum.

`internal/workflow/deploy.go` (renamed → `workflow/topology.go`):
- Move typed enums here: `Mode`, `DeployStrategy`, `PushGitTrigger`.
- `DeployRole*` is **collapsed** into `Mode` (roles are modes from service's perspective).

`internal/ops/deploy_validate.go`:
- Delete local `DeployRoleDev`, `DeployRoleStage`.
- Import from `workflow` package.

`internal/workflow/envelope.go`:
- Keep existing typed `Mode`, `DeployStrategy` but source from new topology.go.
- Drop `PlanMode*` references in doc comments.

---

## 5. Tool behavior changes (complete)

### 5.1 `zerops_deploy` — unified registration

`server.go`: collapse `if sshDeployer != nil { RegisterDeploySSH } else { RegisterDeployLocal }`
into single `RegisterDeploy(srv, ..., sshDeployer, ...)` call. Inside
handler, dispatch on `env` (derived from `rtInfo.InContainer`).

Shared `ops.Deploy()` takes an `Executor` interface:

```go
// ops/deploy.go (new)
type Executor interface {
    Push(ctx context.Context, target platform.Service, params PushParams) (*DeployResult, error)
    GitPush(ctx context.Context, target platform.Service, params GitPushParams) (*DeployResult, error)
}

// ops/executor_ssh.go — wraps existing DeploySSH logic
// ops/executor_local.go — wraps existing DeployLocal logic
```

Backends handle push mechanics. `ops.Deploy()` handles validation,
resolution, result shaping, and attempt recording.

### 5.2 `zerops_deploy strategy="git-push"` — revised precondition

**Drop** the `meta.IsDeployed()` gate (deploy_git_push.go:82-93). Replace
with:

```
precondition: target.WorkingDir has .git directory AND git log -1 succeeds
fail → ErrPrerequisiteMissing("git-push requires committed code at <workingDir>;
                               commit before retrying")
```

For local env: additionally requires `origin` remote resolvable (or
`RemoteURL` param provided — see handleLocalGitPush in §5.4).

### 5.3 Strategy parameter validation

```
strategy ∈ {"", "push-dev", "git-push"} → accept (subject to mode gates below)
strategy == "manual" → ErrInvalidParameter:
    "'manual' is a ServiceMeta declaration, not a deploy tool option.
     Use zerops_workflow action=strategy to mark a service as manual;
     do not call zerops_deploy on it."
strategy other → ErrInvalidParameter
```

**Mode gates** (applied after strategy enum check):

| Effective strategy | local-only | local-stage | container (any) |
|---|:---:|:---:|:---:|
| `push-dev` (or empty default) | ErrPrerequisiteMissing — no stage linked | OK | OK |
| `git-push` | OK (pushes user's local git; ZCP doesn't track downstream) | OK | OK |

`local-only` + `push-dev` error message:
```
ErrPrerequisiteMissing:
  "No Zerops stage linked to this project. push-dev needs a deploy
   target. Either:
     - link a stage: zerops_workflow action='adopt-local' targetService=<hostname>
     - or use git-push: zerops_deploy strategy='git-push'"
```

Note: ZCP never introspects what happens at the git remote after a
`git-push` (webhook firing, Actions running, cross-project builds — all
user's concern). The tool's responsibility ends at `git push` success.

### 5.4 `handleLocalGitPush` — full flow

```
// tools/deploy_local_git.go (new)
1. Validate git repo at WorkingDir:
   git -C {WorkingDir} rev-parse --is-inside-work-tree
   Fail → ErrPrerequisiteMissing(<cwd> not a git repository)

2. Validate HEAD has commits:
   git -C {WorkingDir} rev-parse HEAD
   Fail → ErrPrerequisiteMissing(no commits; commit first)

3. Resolve origin URL:
   current := git -C {WorkingDir} remote get-url origin
   - current == "" AND input.RemoteURL != "" → git remote add origin {RemoteURL}
   - current == "" AND input.RemoteURL == "" → ErrPrerequisiteMissing(no origin; pass remoteUrl)
   - current != "" AND input.RemoteURL != "" AND current != input.RemoteURL → ErrConflict
   - otherwise → continue

4. Resolve branch:
   branch := input.Branch
   branch == "" → git -C {WorkingDir} rev-parse --abbrev-ref HEAD

5. Warn on dirty tree (non-blocking):
   dirty := git -C {WorkingDir} status --porcelain
   dirty != "" → response.Warnings = append(..., "uncommitted changes present")

6. Push with GIT_TERMINAL_PROMPT=0:
   git -C {WorkingDir} push origin {branch}
   - stderr contains "Everything up-to-date" → status=NOTHING_TO_PUSH
   - exit non-zero → ErrDeployFailed(last 5 lines of stderr)
   - success → status=PUSHED

7. If meta.PushGitTrigger == "" → response.Warnings += "no trigger configured; push succeeded but Zerops will not auto-build"

8. Record DeployAttempt to work session (success or failure)
```

### 5.5 `zerops_workflow action="strategy"` — rewrite

Input gains optional `Trigger` field for push-git setup:

```go
type WorkflowInput struct {
    ...
    Strategies map[string]DeployStrategy  // typed now
    Trigger    PushGitTrigger             // optional; valid only with push-git
}
```

Dispatch:
- `strategies=={X:"push-dev"}` or `{X:"manual"}` → write meta, return short guidance.
- `strategies=={X:"push-git"}` without `trigger` → write meta (`DeployStrategy=push-git`, `PushGitTrigger=""`), synthesize `strategy-push-git-intro` atom asking for trigger choice.
- `strategies=={X:"push-git"}` with `trigger=webhook` → write meta (`PushGitTrigger=webhook`), synthesize push atom (env-appropriate) + trigger-webhook atom.
- `strategies=={X:"push-git"}` with `trigger=actions` → same, with trigger-actions atom.

Validation: `Mode=local-only` services accept `push-git` or `manual`;
`push-dev` rejected with guidance to either link a stage first or
choose `push-git`. All other mode × strategy combinations (§2.11 matrix)
are accepted.

### 5.6 `zerops_env action="generate-dotenv"` — VPN probe

After generating `.env` content, probe any managed host referenced:

```
for each managedHost in content:
    ctx, cancel := WithTimeout(2s)
    conn, err := net.DialTimeout("tcp", managedHost + ":" + inferredPort, 2s)
    if err → vpnHint = "Connect via: zcli vpn up " + projectID; break
```

`vpnHint` appended to response if any probe failed. Non-blocking — dotenv
file is still returned.

### 5.7 Eager auto-adopt at server init

No per-tool hooks. Adoption runs exactly once, at `server.New()` startup,
before the MCP init handshake completes. See §2.9 for the full decision
rationale and §7 for the execution detail.

Sketch (in `server.New`, before `mcp.NewServer` call):

```go
var adoptionNote string
if rtInfo.InContainer == false {  // local env only
    metas, err := workflow.ListServiceMetas(stateDir)
    if err != nil {
        return nil, fmt.Errorf("server init: read metas: %w", err)
    }
    if len(metas) == 0 {
        adopted, adoptErr := workflow.LocalAutoAdopt(ctx, client, projectID, stateDir)
        if adoptErr != nil {
            return nil, fmt.Errorf("server init: auto-adopt failed: %w", adoptErr)
        }
        adoptionNote = formatAdoptionNote(adopted)
    } else {
        // Legacy migration check — eager, one-shot.
        if err := workflow.MigrateLegacyLocalMetas(ctx, client, projectID, stateDir, metas); err != nil {
            return nil, fmt.Errorf("server init: legacy migration failed: %w", err)
        }
    }
}

instructions := BuildInstructions(rtInfo)
if adoptionNote != "" {
    instructions += "\n\n" + adoptionNote
}

srv := mcp.NewServer(
    &mcp.Implementation{Name: "zcp", Version: Version},
    &mcp.ServerOptions{Instructions: instructions, ...},
)
```

Fail-fast on every error — no retry, no pending state flags, no partial
init. If API is down, server refuses to start (same class as existing
auth verification failures).

---

## 6. Atom corpus — complete change list

### 6.1 Content fixes (non-structural)

| File | Change |
|---|---|
| `instructions.go` `localEnvironment` | Drop "Deploy via zcli push"; point to `zerops_deploy` |
| `deploy_ssh.go:42` strategy description | Drop "default zcli push" wording |
| `deploy_ssh.go:112` invalid-strategy error | Drop "default zcli push" wording |
| `deploy_git_push.go:91` recovery text | Drop "default self-deploy" jargon |
| `develop-first-deploy-intro.md` | Env-neutral: "Run `zerops_deploy targetService=...` without `strategy` argument" |
| `develop-first-deploy-execute.md` | Drop container-only SSH assumptions |
| `develop-strategy-review.md` | Drop "`zerops_deploy` calls stay valid" for manual; ZCP is out of the loop |

### 6.2 Env-aware splits

| Original atom | New split |
|---|---|
| `develop-first-deploy-asset-pipeline.md` | `-container.md` (SSHFS build) + `-local.md` (user runs locally) |
| `develop-push-dev-deploy.md` | `-container.md` (SSH push) + `-local.md` (zcli push from CWD) |
| `develop-close-push-git.md` | `-container.md` (commit + `zerops_deploy strategy=git-push` via GIT_TOKEN) + `-local.md` (commit + `zerops_deploy strategy=git-push` via user's git) |

Each new atom has `environments: [container]` or `[local]` filter.

### 6.3 New atoms

| Atom | Filters | Purpose |
|---|---|---|
| `develop-platform-rules-local.md` | `environments: [local]` | VPN, .env bridge, localhost health, no SSHFS |
| `strategy-push-git-intro.md` | `phases: [strategy-setup]` | Repo URL confirmation, trigger choice (webhook/actions) |
| `strategy-push-git-push-container.md` | `phases: [strategy-setup]`, `environments: [container]` | GIT_TOKEN, .netrc, first push from container |
| `strategy-push-git-push-local.md` | `phases: [strategy-setup]`, `environments: [local]` | No tokens. Guide: ensure origin configured, ZCP can push via `zerops_deploy strategy=git-push` |
| `strategy-push-git-trigger-webhook.md` | `phases: [strategy-setup]`, `triggers: [webhook]` | Zerops dashboard walkthrough |
| `strategy-push-git-trigger-actions.md` | `phases: [strategy-setup]`, `triggers: [actions]` | `.github/workflows/deploy.yml` + `ZEROPS_TOKEN` secret |
| `develop-close-manual.md` (rewrite) | existing filters | "Changes complete. ZCP stays out of the deploy loop on manual. Inform user." — no tool suggestions |

### 6.4 Filter schema additions

`workflow/atom.go` — extend frontmatter parsing:

- `triggers: [webhook | actions]` — filter for push-git setup sub-atoms.
- `topologies: [local-stage | local-only]` — optional extra filter if the
  `modes:` filter isn't expressive enough. (First pass: try using `modes:`
  with new values `local-stage`/`local-only`; add `topologies:` only if needed.)

### 6.5 Deleted atoms

- `strategy-push-git.md` (the old monolith) — superseded by 5 new atoms.
- `develop-checklist-dev-mode.md` / `develop-checklist-simple-mode.md` —
  add `environments: [container]` filter (they assume SSH start semantics).
  Not deleted, just filter-restricted.

### 6.6 Export gate

`export.md` receives `environments: [container]` filter. Local-export is
deferred (separate plan). Router offering for `export` gated to container.

---

## 7. Auto-adoption (design detail)

### 7.1 Trigger point

**Eager** — at `server.New()` startup, before MCP init handshake
completes. Exactly once per server process lifetime. See §2.9 for
rationale and §5.7 for the sketch.

### 7.2 Execution

```go
// workflow/adopt.go (new function)
func LocalAutoAdopt(
    ctx context.Context,
    client platform.Client,
    projectID string,
    stateDir string,
) ([]*ServiceMeta, error) {
    project, err := client.GetProject(ctx, projectID)
    if err != nil {
        return nil, fmt.Errorf("get project: %w", err)
    }

    services, err := client.ListServices(ctx, projectID)
    if err != nil {
        return nil, fmt.Errorf("list services: %w", err)
    }

    var runtimes, managed []platform.ServiceStack
    for _, s := range services {
        if s.IsSystem() { continue }
        if IsManagedService(s.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
            managed = append(managed, s)
        } else {
            runtimes = append(runtimes, s)
        }
    }

    // Case A: no runtimes — local-only project.
    // Strategy unset: router prompts user to pick push-git or manual.
    if len(runtimes) == 0 {
        meta := &ServiceMeta{
            Hostname:         project.Name,
            Mode:             ModeLocalOnly,
            DeployStrategy:   StrategyUnset,
            BootstrapSession: "",              // adopted
            BootstrappedAt:   time.Now().UTC().Format("2006-01-02"),
        }
        if err := WriteServiceMeta(stateDir, meta); err != nil {
            return nil, fmt.Errorf("write local-only meta: %w", err)
        }
        return []*ServiceMeta{meta}, nil
    }

    // Case B: exactly one runtime — auto-link
    if len(runtimes) == 1 {
        meta := &ServiceMeta{
            Hostname:         project.Name,
            StageHostname:    runtimes[0].Name,
            Mode:             ModeLocalStage,
            DeployStrategy:   StrategyUnset,  // router tlačí na strategy volbu
            BootstrapSession: "",
            BootstrappedAt:   time.Now().UTC().Format("2006-01-02"),
        }
        if err := WriteServiceMeta(stateDir, meta); err != nil {
            return nil, fmt.Errorf("write local-stage meta: %w", err)
        }
        return []*ServiceMeta{meta}, nil
    }

    // Case C: multiple runtimes — meta IS written as local-only (local dir
    // is always adopted). Runtime list is returned so caller can include
    // enumeration in adoption note. User links a stage later via
    // `action=adopt-local` subaction if they want push-dev; push-git and
    // manual are available immediately without any stage linkage.
    meta := &ServiceMeta{
        Hostname:         project.Name,
        Mode:             ModeLocalOnly,
        StageHostname:    "",
        DeployStrategy:   StrategyUnset,  // router prompts: push-git or manual
        BootstrapSession: "",
        BootstrappedAt:   time.Now().UTC().Format("2006-01-02"),
    }
    if err := WriteServiceMeta(stateDir, meta); err != nil {
        return nil, fmt.Errorf("write local-only meta (multi-runtime): %w", err)
    }
    runtimeNames := make([]string, len(runtimes))
    for i, r := range runtimes { runtimeNames[i] = r.Name }
    return &AdoptionResult{
        Meta:                meta,
        UnlinkedRuntimes:    runtimeNames,  // for note composition
    }, nil
}
```

`LocalAutoAdopt` return signature is `(*AdoptionResult, error)` where the
result always contains a meta on success. No error path for "ambiguous" —
that's just a meta with empty `StageHostname` plus an enumeration for the
note.

### 7.3 Stage linkage — separate, optional step

Adoption itself is never blocked. The local dir is always registered as
a ServiceMeta at first server start. Linking a Zerops runtime as stage is
a **separate, optional** step that happens either:

- **Automatically** at startup, when exactly one runtime exists (Case B):
  `StageHostname` set, `Mode=local-stage`.
- **Explicitly** when user picks one of multiple runtimes (Case C
  aftermath): via `zerops_workflow action="adopt-local" targetService=X`.
  Meta gets upgraded: `StageHostname=X`, `Mode` transitions
  `local-only → local-stage`.
- **Never** in the managed-only / empty project case (Case A): no runtime
  exists to link, `Mode=local-only` stays.

First release supports one Zerops runtime linked per local project
(multi-stage / monorepo deferred, §3.2).

Rationale for not auto-linking in Case C: picking "primary" runtime by
any priority (exact-match, alphabetical, first-in-API-response) would be
a heuristic that silently masks user intent — forbidden per no-fallback
principle. User must choose.

The same rule extends to container adoption of ambiguous dev/stage pairs
(Phase B.4).

### 7.4 Adoption note delivery

The note is injected into MCP instructions at `server.New` time, not into
individual tool responses. See §2.10 for the three shapes (stage-linked /
stage-linkage-available / no-runtime).

Why instructions rather than response payload: instructions are part of
the LLM's system prompt for the entire session and arrive before any
tool call. Response-level adoption note would require the LLM to have
already decided to call a tool, which means its prior reasoning was
based on a pre-adoption mental model. Instructions-level delivery
eliminates that window.

Format: plain English, 2-4 sentences, deterministic (same adoption
outcome → same note). Test harness asserts exact strings. When meta
already existed at startup (no adoption happened), no note is emitted.

---

## 8. Migration strategy for existing deployments

### 8.1 Existing container users

Container metas retain their current shape:
- `Hostname` = Zerops service hostname (unchanged)
- `StageHostname` = paired stage (unchanged)
- `Mode` ∈ {dev, standard, simple} (values unchanged; now typed)
- `DeployStrategy`, `StrategyConfirmed`, `BootstrapSession`, `BootstrappedAt` (unchanged)
- `Environment` field: present in legacy files, read and discarded.
- `FirstDeployedAt` field: present in legacy files, read and discarded.
- `PushGitTrigger`: absent in legacy — treated as empty; if `DeployStrategy=push-git`,
  router emits one-time warning + offers re-setup via `action=strategy`.

### 8.2 Existing local users (with meta)

Legacy local meta pattern: `Hostname=<stageHost>`, `StageHostname=""`,
`Mode ∈ {standard, simple, dev}`, `Environment="local"`.

Eager migration at `server.New()` — single pass, runs before MCP
instructions are built. Aligns with §2.9 eager adoption: all state
bootstrapping happens in one place.

```go
// workflow/adopt.go
func MigrateLegacyLocalMetas(
    ctx context.Context,
    client platform.Client,
    projectID, stateDir string,
    metas []*ServiceMeta,
) error {
    // Detection: legacy local meta has Environment="local" (will be dropped in
    // Release B) + container-era Mode + Hostname holds stage hostname.
    var needsMigration []*ServiceMeta
    for _, m := range metas {
        if m.Environment == "local" &&
            (m.Mode == "standard" || m.Mode == "simple" || m.Mode == "dev") {
            needsMigration = append(needsMigration, m)
        }
    }
    if len(needsMigration) == 0 {
        return nil
    }

    project, err := client.GetProject(ctx, projectID)
    if err != nil {
        return fmt.Errorf("migrate legacy metas: get project: %w", err)
    }

    for _, m := range needsMigration {
        // Current Hostname holds the Zerops stage hostname. Rewrite.
        stageHost := m.Hostname
        oldFile := m.Hostname  // file is keyed by current Hostname
        m.StageHostname = stageHost
        m.Hostname = project.Name
        m.Mode = string(ModeLocalStage)

        // Write new-name file.
        if err := WriteServiceMeta(stateDir, m); err != nil {
            return fmt.Errorf("migrate legacy meta %q: write: %w", stageHost, err)
        }
        // Delete old-name file.
        if err := DeleteServiceMeta(stateDir, oldFile); err != nil {
            return fmt.Errorf("migrate legacy meta %q: delete old: %w", stageHost, err)
        }
    }
    return nil
}
```

Runs exactly once per upgrade — idempotent (second run finds no
legacy-shaped metas). Fail-fast on errors. No lazy path, no tolerance
window in read code.

### 8.3 Existing managed-only users (no meta)

At server startup after upgrade, `LocalAutoAdopt` detects empty state
and creates a `local-only` meta (§7.2 Case A). User sees adoption note in
MCP instructions; no data loss.

### 8.4 Rollback strategy

Eager migration in Release A IS forward-only for meta files (legacy
`Hostname=<stage-host>` layout is rewritten to new layout, old file
deleted). Rollback requires either:

- Restoring `.zcp/state/services/` from backup (users should have `.zcp/`
  in `.gitignore` anyway — it's local state, low rollback cost).
- Running old ZCP against new state — old code won't recognize
  `Mode=local-stage`, will likely treat meta as incomplete or error out.
  Not recommended as rollback path.

To mitigate: Phase A.4 ships with a one-time backup step. Before
migrating any legacy metas, copy `.zcp/state/services/` →
`.zcp/state/services-backup-<timestamp>/`. User can restore manually
if needed.

Later phases (A.5 through A.9) are logic/content changes without
forward-only data mutations — commits are independently revertable.

Release B changes to ServiceMeta shape (drop `Environment`,
`FirstDeployedAt` fields) are cosmetic in legacy files (those fields
already ignored post-Release A); safely revertable.

---

## 9. Implementation phases (commit-by-commit)

Each phase = one commit. Every commit: compiles, tests green, deployable.
Tests written RED first, then implementation (per CLAUDE.md TDD mandate).
All phases are per-layer — impact analysis before every change per
CLAUDE.md "Change impact — tests FIRST at ALL affected layers".

### Release A

#### Phase A.1 — Tool schema leak cleanup (foundational, non-breaking)

**Scope**: Remove `zcli push` leakage from LLM-facing text surfaces.

**Files**:
- `internal/server/instructions.go` — rewrite `localEnvironment` to point to `zerops_deploy`.
- `internal/tools/deploy_ssh.go:42,112` — drop "zcli push" from strategy description and error.
- `internal/tools/deploy_git_push.go:91` — drop "default self-deploy" text.
- `internal/content/atoms/develop-first-deploy-intro.md` — env-neutral framing.
- `internal/content/atoms/develop-first-deploy-execute.md` — drop container assumption.

**RED tests** (update first, then implementation):
- `internal/server/instructions_test.go:33` — update expected string (no "zcli push").
- `internal/server/server_test.go:207-209` — update expected local-env wording.
- `internal/tools/deploy_ssh_test.go` — update strategy-param error expectations.
- `internal/content/atoms_test.go` — atom content assertions if any.

**Verification**: `make lint-fast && go test ./... -count=1 -short`.

#### Phase A.2 — Drop `FirstDeployedAt` gate in handleGitPush

**Scope**: Replace `meta.IsDeployed()` gate with "repo has committed code"
check.

**Files**:
- `internal/tools/deploy_git_push.go:82-93` — replace gate logic.
- `internal/content/atoms/export.md:207` — drop `PREREQUISITE_MISSING` recovery row.

**RED tests**:
- `internal/tools/deploy_ssh_test.go:1047-1073` — invert expectation: adopted
  service (no FirstDeployedAt) should now pass through to git-push handler,
  failing on the NEW precondition (missing commit) if no commit.
- New test: adopted service WITH committed code → git-push proceeds.

**Verification**: unit + tool tests green.

#### Phase A.3 — IsDeployed derivation refactor

**Scope**: Drop `FirstDeployedAt` from `ServiceMeta`. Replace with derived
`Deployed` in envelope. Remove cross-boundary write in
`RecordVerifyAttempt`.

**Files**:
- `internal/workflow/service_meta.go` — drop `FirstDeployedAt` field, drop
  `IsDeployed()` method, drop `MarkServiceDeployed` func.
- `internal/workflow/compute_envelope.go` — rewrite `Deployed` derivation:
  `platformService.Status == "ACTIVE" || hasSuccessfulDeploy(ws, hostname)`.
- `internal/workflow/work_session.go:223-227` — drop `MarkServiceDeployed` call
  in `RecordVerifyAttempt`.
- `internal/workflow/bootstrap_outputs.go:137-138` — drop `FirstDeployedAt` preservation in merge.

**RED tests**:
- `internal/workflow/compute_envelope_test.go` — update tests expecting
  `Deployed` derivation from meta → now from platform status or ws.
- `internal/workflow/work_session_test.go` — update RecordVerifyAttempt tests.
- Atom filter tests (`scenarios_test.go`): adopted-already-deployed service
  should fire `deployStates: [deployed]` atoms correctly.

**Verification**: full test suite + scenario tests green.

#### Phase A.4 — Auto-adoption for local env

**Scope**: New `LocalAutoAdopt` + `MigrateLegacyLocalMetas` + eager
invocation at `server.New()` startup (§2.9, §5.7). Includes eager
migration for legacy local metas.

**Files**:
- `internal/workflow/adopt.go` — new `LocalAutoAdopt` function.
- `internal/workflow/adopt.go` — new `MigrateLegacyLocalMetas` function.
- `internal/workflow/adopt.go` — new `AdoptionResult` struct + `formatAdoptionNote` helper (3 shapes per §2.10).
- `internal/platform/client.go` — ensure `GetProject` method exists (verify; add if missing).
- `internal/server/server.go:48` `New` — invoke adoption/migration before `mcp.NewServer`; inject note into Instructions (§5.7 sketch).
- `internal/server/instructions.go` — expose a `BuildInstructions(rt, adoptionNote)` signature so note can be appended after base text.
- `internal/tools/workflow.go` — add `action="adopt-local"` subaction for explicit ambiguity resolution (§7.3).
- `internal/workflow/router.go filterStaleMetas` — skip live-check for `Mode=local-*`.

Note: NO per-tool auto-adopt hooks. Adoption is exclusively a server-init
concern.

**RED tests**:
- `internal/workflow/adopt_test.go` — unit tests for `LocalAutoAdopt`:
  - No runtime services → meta with `Mode=local-only, StageHostname=""`.
  - Exactly one runtime → meta with `Mode=local-stage, StageHostname=<r>`.
  - Multiple runtimes → meta with `Mode=local-only, StageHostname=""`
    AND `AdoptionResult.UnlinkedRuntimes` populated with enumeration.
    Meta IS written (adoption always succeeds).
- `internal/workflow/adopt_test.go` — unit tests for `MigrateLegacyLocalMetas`:
  - Legacy local meta (Environment=local + Mode=standard/simple/dev) →
    rewritten to ModeLocalStage layout; old file deleted; new file created.
  - Non-legacy metas → untouched.
  - Idempotent: second run on already-migrated state = no-op.
- `internal/server/server_test.go` — server init behavior:
  - Local env + empty state + zero runtimes → meta created (local-only);
    instructions include Case A note.
  - Local env + empty state + one runtime → meta created (local-stage);
    instructions include Case B note.
  - Local env + empty state + multiple runtimes → meta created
    (local-only); instructions include Case C note enumerating runtimes.
  - Local env + existing meta → no adoption; instructions have no note.
  - Local env + legacy metas → migration runs; instructions have no note
    (silent upgrade).
  - Local env + API unreachable → `server.New` returns error (fail-fast).
  - Container env → no adoption attempted; existing flow unchanged.
- `internal/tools/workflow_test.go` — `adopt-local` subaction:
  - `action=adopt-local targetService=X` on local-only meta with X in
    live services → upgrades to local-stage (sets StageHostname, updates Mode).
  - `action=adopt-local` when meta is already local-stage → error
    ("stage already linked; manual relink required").
  - `action=adopt-local targetService=X` when X not in live services → error.
  - `action=adopt-local` in container env → error (container uses bootstrap).
- `internal/workflow/router_test.go` — filterStaleMetas keeps local metas
  even when their Hostname (= project name) isn't in live services.

**Verification**: unit + tool + integration tests green; server init
behaves correctly under each state scenario.

#### Phase A.5 — Unified DeployInput schema

**Scope**: Collapse `DeploySSHInput` + `DeployLocalInput` into single
`DeployInput`. Dispatch internally.

**Files**:
- `internal/tools/deploy.go` (new) — unified `DeployInput`, `RegisterDeploy`.
- `internal/tools/deploy_ssh.go` — becomes `executor_ssh.go` under tools
  (or moved into ops).
- `internal/tools/deploy_local.go` — becomes `executor_local.go`.
- `internal/tools/deploy_git_push.go` — handler signature updated to use unified input.
- `internal/tools/deploy_local_git.go` (new) — `handleLocalGitPush` impl.
- `internal/ops/deploy.go` (new) — shared `Deploy()` entry with injected `Executor`.
- `internal/ops/executor_ssh.go` (refactor from `ops/deploy_ssh.go`).
- `internal/ops/executor_local.go` (refactor from `ops/deploy_local.go`).
- `internal/server/server.go:110-124` — single `RegisterDeploy` call.

**Refined input**:
```go
type DeployInput struct {
    TargetService string
    SourceService string
    Setup         string
    WorkingDir    string
    Strategy      string
    RemoteURL     string
    Branch        string
    // IncludeGit dropped from schema — auto-decided by executor
}
```

Env-specific constraints enforced in handler:
- Local + SourceService!="" → `ErrInvalidParameter("cross-deploy not supported in local env")`.
- Strategy="manual" → rejected (§5.3).
- Strategy="git-push" in local → routes to `handleLocalGitPush`.
- Strategy="git-push" in container → routes to `handleGitPush`.

**RED tests**:
- `internal/tools/deploy_test.go` (new) — unified tool registration and dispatch.
- Existing `deploy_ssh_test.go` and `deploy_local_test.go` — merge into
  `deploy_test.go`, cover both environments.
- New tests for `handleLocalGitPush` pre-flight matrix (§5.4).

**Verification**: full tool test layer green; integration tests cover both envs.

#### Phase A.6 — Strategy setup atom split

**Scope**: Replace monolithic `strategy-push-git.md` with 5 atoms (intro,
2 push per env, 2 trigger per type). Update `handleStrategy` to
orchestrate.

**Files**:
- Delete `internal/content/atoms/strategy-push-git.md`.
- Create `internal/content/atoms/strategy-push-git-intro.md`.
- Create `internal/content/atoms/strategy-push-git-push-container.md`.
- Create `internal/content/atoms/strategy-push-git-push-local.md`.
- Create `internal/content/atoms/strategy-push-git-trigger-webhook.md`.
- Create `internal/content/atoms/strategy-push-git-trigger-actions.md`.
- `internal/workflow/atom.go` — add `triggers:` filter parsing.
- `internal/workflow/synthesize.go` — honor `triggers:` filter.
- `internal/workflow/envelope.go` — add `Trigger` field to envelope
  (sourced from `meta.PushGitTrigger`).
- `internal/tools/workflow_strategy.go` — extend `handleStrategy` with
  optional `Trigger` input, orchestrate new atoms.
- `internal/tools/workflow.go` — `WorkflowInput.Trigger` field addition.

**RED tests**:
- `internal/content/atoms_test.go` — validate new atoms' frontmatter schema.
- `internal/workflow/scenarios_test.go` — scenarios for each env × trigger combo.
- `internal/tools/workflow_strategy_test.go` — test trigger input handling.
- `internal/workflow/atom_test.go` — triggers filter parsing.

**Verification**: full scenario coverage; atoms validate; synthesis produces
expected content per combo.

#### Phase A.7 — Env-aware atom splits (first-deploy, push-dev, close-push-git)

**Scope**: Split atoms that conflate container/local behavior.

**Files**:
- Split `develop-first-deploy-asset-pipeline.md` → `-container.md`, `-local.md`.
- Split `develop-push-dev-deploy.md` → `-container.md`, `-local.md`.
- Split `develop-close-push-git.md` → `-container.md`, `-local.md`.
- Create `develop-platform-rules-local.md`.
- Add `environments: [container]` filter to `develop-checklist-dev-mode.md`, `develop-checklist-simple-mode.md`.
- Rewrite `develop-close-manual.md` (no tool suggestions, ZCP out of loop).
- Adjust `develop-strategy-review.md` manual language.
- Add local variant to `develop-ready-to-deploy.md` (or rewrite env-neutral).

**RED tests**:
- `internal/workflow/scenarios_test.go` — new scenarios covering local+push-dev, local+push-git close, local+never-deployed.
- `internal/workflow/corpus_coverage_test.go` — ensure every env×mode×strategy combo has required atoms.
- `internal/content/atoms_test.go` — atom frontmatter + content validity.

**Verification**: corpus coverage test green; synthesis produces appropriate atoms per combo.

#### Phase A.8 — Export gate to container + VPN probe

**Scope**: Add container-only filter to export; add VPN probe in generate-dotenv.

**Files**:
- `internal/content/atoms/export.md` — add `environments: [container]` filter.
- `internal/workflow/router.go strategyOfferings` — condition `export` offering on container env.
- `internal/ops/env.go` (wherever `generate-dotenv` lives) — add managed host probe.
- `internal/ops/net_probe.go` (new) — lightweight TCP connect probe helper.

**RED tests**:
- `internal/workflow/router_test.go` — local env does not offer export.
- `internal/ops/env_test.go` — generate-dotenv includes vpnHint when probe fails.
- `internal/ops/net_probe_test.go` — probe helper with fake dialer.

**Verification**: router test green; ops test green.

#### Phase A.9 — Docs refresh

**Scope**: Update spec-local-dev.md, spec-workflows.md with new model.

**Files**:
- `docs/spec-local-dev.md` — sections 4 (Topology), 6 (ServiceMeta), 11 (Strategies).
- `docs/spec-workflows.md` — strategy section, develop close section.
- `CLAUDE.md` — no changes expected; verify conventions still accurate.
- `plans/env-topology-strategy-unification.md` — update status to Implemented after all phases merge.

**Verification**: docs build (if any); manual read-through.

### Release B (after Release A stabilizes ≥1 release cycle)

#### Phase B.1 — Typed Mode enum consolidation

**Scope**: Collapse `PlanMode*`, `Mode*`, `DeployRole*` into single typed enum.

**Files**:
- `internal/workflow/topology.go` (rename from `deploy.go`) — single `Mode` typed enum.
- `internal/workflow/bootstrap.go` — delete `PlanMode*` string consts, import from topology.
- `internal/workflow/envelope.go` — `Mode` type source from topology.
- `internal/ops/deploy_validate.go` — delete local `DeployRole*` consts, import.
- All callers updated (likely ~30 files; mechanical).

**RED tests**:
- Existing tests mostly fine (enum values unchanged); compile errors drive updates.
- `internal/workflow/topology_test.go` (new) — enum exhaustiveness.

#### Phase B.2 — Typed DeployStrategy + PushGitTrigger in ServiceMeta

**Scope**: `ServiceMeta.DeployStrategy` becomes typed. Add `PushGitTrigger` typed.

**Files**:
- `internal/workflow/service_meta.go` — typed fields.
- JSON marshal compatibility (string ↔ typed): straightforward.
- All caller sites updated.

**RED tests**:
- `internal/workflow/service_meta_test.go` — round-trip marshaling.
- Existing tests: compile errors lead through updates.

#### Phase B.3 — Drop `Environment` field from ServiceMeta

**Scope**: Delete `Environment` field; derive from `Engine.environment`.

**Files**:
- `internal/workflow/service_meta.go` — drop field.
- `internal/workflow/bootstrap_outputs.go` — drop assignment.
- All read sites that compared `meta.Environment` — refactor to use passed-in env context.

**RED tests**:
- Unit tests for all callers that previously read `meta.Environment`.

#### Phase B.4 — Delete hostname suffix heuristic outright

**Scope**: Remove all discovery-side suffix pattern matching. Replace
with explicit signals (meta fields, plan inputs, required params). Code
paths that used the heuristic refuse to operate without the explicit
signal.

**Files** (each usage reviewed individually: keep if ZCP-authored output,
delete if discovery):

- `internal/workflow/adopt.go:61,77` — **delete**. `InferServicePairing`
  rewritten to require pairing info from explicit user selection during
  adoption (container adopt workflow gains interactive pairing step).
  Services without explicit pairing answer → not paired, each becomes
  independent target.
- `internal/workflow/validate.go:62` — **delete**. `RuntimeTarget` must
  carry explicit `StageHostname()` when standard mode; no derivation.
- `internal/workflow/symbol_contract.go:351` — case-by-case: review
  context. If recipe-generated, keep (ZCP owns the name); if discovery,
  delete.
- `internal/ops/deploy_validate.go:78-79` — **delete**. `role` param
  becomes required; caller (deploy tool handler) passes
  `meta.RoleFor(target)` explicitly.
- `internal/ops/checks/symbol_contract_env.go:108` — case-by-case review;
  likely recipe-output validation so may keep as template convention.

**Container adopt workflow update**: the adopt path presents a list of
discovered services to the user and asks for pairing decisions. Current
Pass 1 + Pass 2 in `InferServicePairing` becomes a pure "list services"
function; pairing is user-driven via new `action="adopt-pair"` subaction
on the workflow tool.

**RED tests**:
- `internal/workflow/adopt_test.go` — `InferServicePairing` returns each
  service as independent target with no pairing; pairing happens via
  explicit subaction.
- `internal/ops/deploy_validate_test.go` — calls without `role` param
  return error (no hostname-based fallback).
- Existing adopt/validate tests updated to provide explicit pairing/role
  where the heuristic used to supply it silently.

#### Phase B.5 — Drop `invertLocalHostname` hack + related special-cases

**Scope**: Remove `invertLocalHostname`, `PrimaryRole` special-case for
local+standard, `resolveEnvelopeMode` local+standard branch, legacy local
meta read tolerance.

**Files**:
- `internal/workflow/bootstrap_outputs.go` — remove `invertLocalHostname`.
- `internal/workflow/service_meta.go:77` — simplify `PrimaryRole`.
- `internal/workflow/compute_envelope.go:297-298` — simplify `resolveEnvelopeMode`.
- `internal/workflow/service_meta.go` read-path — drop legacy normalization (Release A did eager migration; should be complete).

**RED tests**:
- Compile errors drive refactor.
- Verify all local-env tests still green (they should never have hit the hack post-Release A).

#### Phase B.6 — Drop `includeGit` from tool surface

**Scope**: Auto-decide `.git` inclusion internally. User doesn't need to know.

**Files**:
- `internal/tools/deploy.go` — drop `IncludeGit` field from `DeployInput`.
- `internal/ops/deploy.go` — auto-decide: always include for self-deploy,
  exclude for cross-deploy (preserves current behavior).
- `internal/ops/executor_local.go` — same.

**RED tests**:
- Existing tests: update to drop `IncludeGit` from calls.

#### Phase B.7 — Docs update for Release B

- Update `docs/spec-local-dev.md`, `docs/spec-workflows.md` with new
  vocabulary.
- Update `CLAUDE.md` architecture table if new/removed packages.
- Move this plan to `plans/archive/` with final status.

---

## 10. Test strategy per phase

Every phase's RED/GREEN/REFACTOR cycle covers:

- **Unit** (`internal/workflow/*_test.go`, `internal/ops/*_test.go`,
  `internal/platform/*_test.go`): behavior of individual functions.
- **Tool** (`internal/tools/*_test.go`): MCP handler dispatch, input
  validation, output shape.
- **Integration** (`integration/*_test.go`): multi-tool flows in a mocked
  project (e.g., local-adopt → develop → deploy → verify).
- **E2E** (`e2e/*_test.go`, `-tags e2e`): real Zerops API — one test per
  critical path per env. Use existing `ZCP_API_KEY` fixture.

Per CLAUDE.md: *"A change is not complete until all affected layers pass."*
Each phase PR includes failing tests at every affected layer BEFORE
implementation (RED), then implementation turns them green.

### E2E coverage matrix

| Scenario | Test | Env |
|---|---|---|
| Local auto-adopt of managed-only project | `e2e/local_adopt_managed_only_test.go` | local |
| Local auto-adopt of project with stage | `e2e/local_adopt_with_stage_test.go` | local |
| Local + push-dev deploy | `e2e/local_push_dev_test.go` | local |
| Local + push-git deploy (manual git push) | `e2e/local_push_git_test.go` | local |
| Container + push-dev (existing) | `e2e/container_push_dev_test.go` | container |
| Container + push-git (existing) | `e2e/container_push_git_test.go` | container |
| Strategy setup flow (all 5 atoms) | `e2e/strategy_setup_test.go` | both |
| Export from container | `e2e/export_container_test.go` | container |
| IsDeployed derivation (adopt-then-push-git) | `e2e/adopted_deployed_test.go` | container |

Some E2E are new; others extend existing.

---

## 11. Risks & guardrails

| Risk | Likelihood | Severity | Mitigation |
|---|---|---|---|
| Legacy local meta migration loses strategy preferences | low | high | Eager migration preserves all fields; unit test round-trip; manual smoke test |
| Auto-adopt fires for projects user didn't intend to "register" | low | medium | Clear adoption note in MCP instructions at startup; idempotent (no harm re-running); user can `rm -rf .zcp/state/services/` and restart to reset. Local state is explicitly local and `.gitignored` — reset is cheap. |
| Unified DeployInput breaks existing LLM prompts | medium | medium | Maintain backward compat in schema: all old fields accepted; new ones optional |
| Atom split breaks synthesis for edge cases | medium | medium | Scenario tests cover full matrix; corpus coverage test enforces ≥1 atom per required axis |
| Multi-stage local users (have >1 Zerops runtime per project) lose functionality | low | medium | MVP refuses auto-adopt on ambiguity (no guessing), requires user to explicit-pick via `adopt-local` subaction; full multi-stage deferred to separate plan |
| Container adoption becomes more interactive (pairing step) | medium | low | Guidance atom enumerates options clearly; one-time cost per project; alternative (guessing) masks bugs silently — not worth the UX tradeoff |
| VPN probe blocks generate-dotenv response | low | low | Probe has 2s timeout; runs best-effort; dotenv returned regardless |
| `GIT_TERMINAL_PROMPT=0` hides legitimate credential setup | low | low | Error message says "check your git credentials (SSH keys, credential manager)" |
| Release B vocabulary refactor causes compile errors elsewhere in workspace | medium | low | Phases are mechanical renames; CI catches at compile time; incremental merging |
| Platform `client.GetProject` doesn't exist or returns differently than expected | low | high | Verify API in Phase A.4 unit test; fall back to env-var ZCP_PROJECT_NAME override if API unavailable |

### Guardrails

1. **No phase merges without all four test layers green** (where applicable).
2. **Each phase = one commit**, split per logical concern (per CLAUDE.local.md `feedback_commits_as_llm_reflog.md`).
3. **Release A sits in production ≥1 release cycle** before Release B starts.
4. **Eager migration is idempotent** — running twice doesn't corrupt.
5. **No silent data loss** — legacy fields read and discarded, not overwritten without migration.
6. **Feature flags not used** — per CLAUDE.md "No feature flags or backwards-compat shims when you can just change the code."

---

## 12. Locked-in decisions

From the design discussion, these are committed:

1. **Strategy enum**: `push-dev | push-git | manual` — three values, no `push-git-manual` intermediate.
2. **`manual` not valid `zerops_deploy` strategy param** — ZCP is out of the loop; tool refuses.
3. **`push-git` requires `PushGitTrigger`** (`webhook` or `actions`); without trigger, user should pick `manual`.
4. **Local push-git uses user's local git config** — no `GIT_TOKEN`, no `.netrc`. Credentials are user's own.
5. **Container push-git continues using `GIT_TOKEN`** in Zerops project env + `.netrc`.
6. **Develop workflow runs regardless of strategy**; only close atom is strategy-aware.
7. **Router always offers develop for bootstrapped/adopted services**; strategy doesn't gate this.
8. **Export gated to container env** in Release A; local export deferred.
9. **VPN probe runs on-demand inside `generate-dotenv`** (not a separate tool, not at server startup). The probe fires only when a managed hostname is actually generated into the dotenv — no wasted work when not needed.
10. **Env is never persisted** per-service — runtime-detected and passed down.
11. **`IsDeployed` derived from platform state or session history**, not persistent flag.
12. **Auto-adoption is eager**, triggered at `server.New()` startup before MCP init handshake. Idempotent: check for existing local meta, create if missing, otherwise do nothing. Adoption ALWAYS succeeds (meta is always written on fresh state) unless API is unreachable (fail-fast). Multi-runtime scenarios don't block adoption — they just defer stage linkage to an explicit `adopt-local` subaction. Notes delivered via MCP instructions text — never via tool response payload.
13. **Auto-adoption uses Zerops project name** as `Hostname` for local meta.
14. **MVP = one Zerops runtime per local project**; multi-stage deferred.
15. **Release split**: A = topology + tool + atoms; B = vocabulary cleanup.
16. **Hostname suffix heuristic is deleted outright** (not centralized as fallback) — per CLAUDE.md "Never add fallbacks". All discovery paths require explicit signals (meta, plan, user answer); refuse to operate on ambiguity.
17. **Auto-adoption always succeeds**; stage linkage is a separate concern. Multiple runtimes → local meta created as `local-only` with empty `StageHostname`; user links a stage later via explicit `adopt-local` subaction if desired. No silent "pick primary" heuristic; no blocking.
19. **`local-only` supports `push-git` and `manual` strategies** (not just manual). User can configure git push without linking a Zerops stage — ZCP doesn't care what happens at the remote (user's webhook / CI / cross-project build / nothing). Only `push-dev` is gated on stage linkage because it's the only strategy that needs a `zcli push`-target.
18. **`role` parameter in `ops.ValidateZeropsYml` becomes required** — no hostname-substring fallback.

---

## 13. Open decisions (for review before Task creation)

These are NOT locked in; sanity-check before implementation:

- **O1** — `IncludeGit` drop in Phase B.6: are there any production users relying on it? (Existing tool tests set it; if exposed via LLM prompts, keep as deprecated field during transition.)
- **O2** — **Resolved**: auto-adoption AND legacy migration are both eager at `server.New()` startup, aligned with the single-state-bootstrap-point principle (§2.9).
- **O3** — Multi-stage local MVP: **resolved** (§7.3) — auto-adopt refuses ambiguity, user picks via explicit `adopt-local` subaction. Confirm this subaction's name/signature before implementation.
- **O4** — Release cycle soak before Release B: 1 release sufficient, or wait 2? (1 standard.)
- **O5** — `setup` fallback in `deploy_preflight.go` (`setup` param empty → infer from hostname+zerops.yaml): this is a no-fallback violation of the same class as the suffix heuristic. Should Phase B.4 also delete this fallback (require `setup` explicit when multi-setup yaml)? Currently deferred as breaking, but the principle says delete. Recommend: add to Phase B.4 scope.

---

## 14. Post-refactor cleanup (deferred to later plans)

These are known but out of this plan's scope:

- Local-native export atom + router local export offering.
- `zerops_promote` as a separate tool split out from `zerops_deploy`.
- Generalization of session file machinery.
- `setup` fallback simplification.
- `IsAdopted()` audit.
- Monorepo support in local (multi-stage per project).
- Bootstrap session vs ServiceMeta.BootstrapSession field dedup.
- Wildcard content generator: `containerEnvironment`/`localEnvironment` text synthesized from one template.

---

## 15. References

- `CLAUDE.md` — project conventions, TDD mandate.
- `CLAUDE.local.md` — Zerops-platform rules, local-dev rules.
- `docs/spec-local-dev.md` — current local mode spec (to be updated).
- `docs/spec-workflows.md` — workflow spec.
- `plans/deploy-config-central-point.md` — predecessor plan that
  consolidated `workflow=cicd` into `action=strategy`.
- Memory: `feedback_single_path_engineering.md`,
  `feedback_commits_as_llm_reflog.md`.

---

## 16. Execution checklist

Once this plan is approved, implementation proceeds:

- [ ] Create tasks (one per phase) via TaskCreate.
- [ ] Execute phases in order A.1 → A.9, then (after soak) B.1 → B.7.
- [ ] After each phase: commit with message documenting WHY, not just WHAT.
- [ ] Update this plan's Status header as phases complete.
- [ ] After Release A final phase: tag release, monitor ≥1 cycle.
- [ ] After Release B final phase: move plan to `plans/archive/`.

---

**Awaiting approval. Do not begin implementation.**
