# ZCP — Team Introduction

> **Audience**: anyone joining the project or about to start internal testing.
> **Reading time**: ~20 minutes for the whole thing; sections 1–6 are the must-read core.
> **Authoritative sources** (this doc summarises them, doesn't replace them): `docs/spec-workflows.md`, `docs/spec-architecture.md`, `docs/spec-knowledge-distribution.md`, `docs/spec-work-session.md`, `docs/spec-scenarios.md`.

---

## 1. The 30-second pitch

**Zerops** is a PaaS that runs your services on real Linux containers — not Kubernetes, not Docker Compose, not Helm. You describe what you want in `import.yaml` (project-level) and `zerops.yaml` (per-service build/run config), and Zerops provisions runtimes (Node, Go, PHP, …) plus managed services (Postgres, Redis, …).

**ZCP** (Zerops Control Plane) is a single Go binary that exposes Zerops to LLM-driven agents through an MCP server (`stdio` JSON-RPC) and a CLI. It does three things:

1. **Wraps the Zerops API** — every Zerops operation (deploy, scale, env-var read, log fetch, …) becomes an MCP tool the agent can call.
2. **Owns workflow state** — a per-process file-system "evidence layer" tracking which services are under management, what mode they're in, how they should close out a development task.
3. **Delivers axis-filtered platform knowledge** — ~80 markdown atoms with structured metadata; the engine selects exactly the ones relevant to the current state and ships them in tool responses so the agent always has the right runtime/mode/deploy guidance without retraining.

Without ZCP, an LLM trying to use Zerops would have to memorise hundreds of pages of docs and re-discover state on every turn. With ZCP, every tool response carries fresh state + the right knowledge atoms + a typed next-action plan.

---

## 2. What you need to know about Zerops first

Before ZCP makes sense, the team should grok these Zerops-specific concepts:

### 2.1 Service stack types and runtime classes

A Zerops service has a **stack type** like `nodejs@22`, `php-apache@8.3`, `postgresql@16`, `static@1.0`. ZCP groups stack types into five **runtime classes** (`internal/topology/types.go`):

| Class | Examples | Behavior |
|---|---|---|
| `dynamic` | nodejs, bun, deno, go, python, java, rust, ruby, dotnet | Process-based runtimes; need an explicit `start:` command |
| `static` | static@1.0 | Files served as-is by Zerops nginx (no `start:`) |
| `implicit-webserver` | php-apache, php-nginx | Web server is part of the runtime; `start:` is omitted in `zerops.yaml` |
| `managed` | postgresql, valkey, mariadb, kafka, … | Zerops-operated; ZCP never deploys code, only reads env vars |
| `unknown` | (fallback) | Anything ZCP doesn't classify yet |

### 2.2 Modes (how a runtime service is wired)

A runtime service runs in one of six **modes** (`topology.Mode`):

| Mode | Layout | Container? |
|---|---|---|
| `dev` | One service. Hostname ends in `dev`. Used for live editing via SSHFS mount. | container |
| `stage` | Companion to a `dev` service. Real start command, healthcheck, build artifact. | container |
| `standard` | The pair `{name}dev` + `{name}stage` — represents a single ServiceMeta. | container |
| `simple` | One service, no dev/stage split. Real start, healthcheck. | container |
| `local-stage` | Local-environment counterpart to stage (developer's machine pushes via `zcli push`). | local |
| `local-only` | Local-environment counterpart to dev (no platform stage half). | local |

The three "container" modes assume the agent runs **inside** a `zcp` service in the project (with SSHFS mounts of every dev runtime at `/var/www/{hostname}/`). The two "local" modes assume the agent runs on a developer's machine.

### 2.3 The dev/stage convention

Standard mode bakes in a Zerops best practice: every service has a **dev** half (where you iterate, with live mounts and manual server lifecycle) and a **stage** half (immutable, healthcheck-driven, gets the production-shaped build artifact via cross-deploy from dev). One ServiceMeta represents the pair (see §6 below).

### 2.4 The two YAMLs

- `import.yaml` — project-level. Lists services to create, their types, dependencies, scaling. Used once per project (or per `zerops_import` call).
- `zerops.yaml` — repository-level, per service. Defines `setup:` blocks (one per role: `dev`, `prod`/`stage`, …) with `build`, `run`, `deployFiles`. Read at every deploy.

These are different files with different schemas. Don't confuse them.

---

## 3. The single mental model — two phases per service

Every service Zerops manages goes through exactly two phases over its lifetime:

```
┌─────────────────────────┐     ┌──────────────────────────────────┐
│ Phase 1 — Enter Evidence│  →  │ Phase 2 — Development Lifecycle  │
│   (happens once)        │     │   (repeats for every task)       │
│                         │     │                                  │
│ Bootstrap or Adoption   │     │ knowledge → work → deploy → close│
│   ↓                     │     │                                  │
│ ServiceMeta file written│     │ Reads ServiceMeta on every turn  │
└─────────────────────────┘     └──────────────────────────────────┘
```

The boundary is strict:

- **Phase 1 provisions infrastructure only**: creates services on Zerops with `startWithoutCode: true`, mounts dev runtime filesystems (container env), discovers env-var names, writes the ServiceMeta evidence file. **No code is written, nothing is deployed, no HTTP probe runs.** The runtimes sit at status RUNNING with empty content directories.
- **Phase 2 owns all application code AND the first deploy**: scaffolds the real `zerops.yaml`, writes the user's actual application, runs the first deploy (which stamps `FirstDeployedAt`), verifies, then iterates.

This separation (called "Option A" since v8.100+) is the foundational architectural decision. Why:

- **Fault isolation** — when verification fails, the bug is infrastructure (env vars, deps, ports). When app fails in develop, infra is already proven healthy.
- **Universal flow** — same arc regardless of mode. No mode-specific edge cases in the conductor.
- **Reduced blast radius** — verification server is tiny; application complexity rides on a stable foundation.
- **Faster iteration** — develop can assume env vars resolve, dependencies connect, ports work.

An agent that writes app code during bootstrap violates this and loses all four benefits. Bootstrap is for plumbing; develop is for everything else.

---

## 4. ServiceMeta — the evidence file

`ServiceMeta` is a single JSON file at `.zcp/state/services/{hostname}.json` per managed runtime (managed services are API-authoritative, so they get no meta). It's the persistent proof that ZCP owns this service.

```
ServiceMeta {
  Hostname                 string           // service identifier (the dev half in standard pairs)
  Mode                     Mode             // standard | dev | simple | local-stage | local-only
  StageHostname            string           // the stage half (standard mode only)
  CloseDeployMode          CloseDeployMode  // unset | auto | git-push | manual — develop session's delivery pattern (drives auto-close gating + per-mode atoms)
  CloseDeployModeConfirmed bool             // true after user explicitly set close-mode
  GitPushState             GitPushState     // unconfigured | configured | broken | unknown — capability axis
  RemoteURL                string           // configured git remote (when GitPushState=configured)
  BuildIntegration         BuildIntegration // none | webhook | actions — CI shape (requires GitPushState=configured)
  BootstrapSession         string           // 16-hex session ID, OR EMPTY (= adoption marker)
  BootstrappedAt           string           // date — empty means bootstrap in progress
  FirstDeployedAt          string           // stamped on first real deploy in develop
}
```

A few invariants worth memorising (full list in `spec-workflows.md §8`):

- **E1**: every managed runtime has a meta with `Mode` and `BootstrappedAt`.
- **E3 / E7**: `BootstrapSession=""` (empty) marks an *adopted* service; combined with `BootstrappedAt` non-empty, this is `IsAdopted()`.
- **E8**: meta is **pair-keyed** in standard mode — one file represents two live hostnames (dev + stage). Always iterate `m.Hostnames()` or use `workflow.ManagedRuntimeIndex`; never key on `m.Hostname` alone.
- **B7**: bootstrap leaves `CloseDeployMode`, `GitPushState`, `BuildIntegration` empty. Develop is what populates them.

---

## 5. Phase 1 — Bootstrap & Adoption

### 5.1 The four bootstrap routes

`zerops_workflow workflow="bootstrap"` is a **two-call** entry. The first call returns a ranked list of routes; the second call commits one. The routes are:

| Route | When offered | What it means |
|---|---|---|
| `resume` | An incomplete session is tagged to a prior PID (process died mid-bootstrap) | Claim the dead session and continue from current step |
| `adopt` | Project has runtime services without `ServiceMeta` files | Register what already exists; no provision call |
| `recipe` | Agent's intent matches one of the curated recipes (e.g. "Laravel + Postgres") with confidence ≥ threshold | ZCP supplies the canonical `import.yaml` template; agent provisions from it |
| `classic` | Always present as the explicit override | Free-form plan; agent constructs the import.yaml from scratch |

> **Naming note**: there is no `route="import"`. If a teammate says "I want to import an app," they mean either `recipe` (use a template) or `classic` + `zerops_import` (paste your own YAML).

### 5.2 The three bootstrap steps

Once a route is committed, bootstrap runs three steps **in strict order**, never iterating:

```
discover  →  provision  →  close
```

| Step | Purpose | Output |
|---|---|---|
| **discover** | Classify what services need to exist; pick mode (standard / dev / simple); submit a `ServicePlan` | Stored plan; agent hasn't touched the platform yet |
| **provision** | Generate `import.yaml` (each runtime carries `startWithoutCode: true`); call `zerops_import`; for container env, auto-mount each dev runtime at `/var/www/{hostname}/` and `git init` inside it | Services exist on Zerops with empty content; partial ServiceMeta written (no `BootstrappedAt` yet); env-var names discovered |
| **close** | Confirm services are RUNNING; finalise ServiceMeta with `BootstrappedAt`; append reflog | Bootstrap done; develop owns from here |

If provision fails, bootstrap **hard-stops** (no auto-iterate — re-running a half-failed import almost never recovers cleanly). The agent reports to the user.

`close` is skippable in two narrow cases:
1. **Managed-only** — the plan has no runtime targets (the project is just databases).
2. **Pure-adoption** — every runtime in the plan was `IsExisting=true`. Provision wrote complete metas inline.

### 5.3 What's actually written during a happy bootstrap

After `close` succeeds:
- One ServiceMeta per runtime target, with `Mode`, `BootstrappedAt`, and (for standard) `StageHostname` set.
- All three deploy-config fields (`CloseDeployMode`, `GitPushState`, `BuildIntegration`) **empty**. They're chosen later in develop.
- `FirstDeployedAt` empty. Develop will stamp it on the first successful deploy.
- `/var/www/.git/` initialised inside each dev runtime container, owned by `zerops:zerops`, with identity `agent@zerops.io / Zerops Agent`.
- Runtime services are RUNNING but content-less (`startWithoutCode: true`). No process is serving HTTP.

The agent is told to start a develop workflow next.

---

## 6. Phase 2 — Develop workflow

### 6.1 The arc

Develop is a per-task lifecycle that wraps **all** code work on runtime services:

```
START → WORK → PRE-DEPLOY → DEPLOY → VERIFY → (loop on failure) → CLOSE
```

| Sub-phase | What happens |
|---|---|
| **START** | Read ServiceMeta. Tell the agent the close-mode + git-push + build-integration state. Surface platform knowledge atoms scoped to the runtime/mode/env. |
| **WORK** | Agent edits code. ZCP stays out of the way (no tool calls required). |
| **PRE-DEPLOY** | Re-read meta. Validate `zerops.yaml` (deploy mode invariants — see §7). Re-discover env-var names. |
| **DEPLOY** | `zerops_deploy` blocks until build completes. Auto-enables L7 subdomain on first deploy for eligible modes (dev, stage, simple, standard, local-stage). Returns DEPLOYED or BUILD_FAILED with a structured `failureClassification`. |
| **VERIFY** | `zerops_verify` checks HTTP + logs + start state per target. |
| **Iterate** | Up to 5 attempts. Tier 1–2 = DIAGNOSE (logs + targeted fix). Tier 3–4 = SYSTEMATIC (env/bind/deployFiles checklist). Tier 5 = STOP (present to user). |
| **CLOSE** | What runs here depends on the close-mode (see §7). |

### 6.2 First-deploy branch vs steady-state iteration

The Plan dispatcher branches the first time it sees a service with `Deployed: false`:

- **First-deploy branch** (`develop-first-deploy-*` atoms) — scaffold `zerops.yaml`, write the actual application, deploy, verify. `FirstDeployedAt` is stamped at the moment the first successful deploy is recorded (in `RecordDeployAttempt`), not after verify. The first deploy **always** uses default self-deploy regardless of the meta's `CloseDeployMode` (invariant **D2a**) — `git-push` and `manual` need state that doesn't exist before the first deploy lands.
- **Steady-state iteration** (`develop-checklist-*`, `develop-close-mode-*` atoms) — agent edits, deploys, verifies. The close-mode atoms tell the agent *what to do before close* (run a final deploy, push to git, or yield to the user); `action="close"` itself is a session-teardown call, not a dispatcher.

### 6.3 Auto-close

The Work Session auto-closes when **every** scope service has at least one succeeded deploy AND one passed verify. The Plan then surfaces "close + start next". The auto-close gate refuses to close while any pair has `CloseDeployMode=unset` — the agent must pick first.

### 6.4 What `action="close"` actually does

`zerops_workflow action="close" workflow="develop"` deletes the current PID's Work Session file, unregisters from the registry, and returns `Work session closed.` That's all. **It does not call `zerops_deploy` or `zerops_verify` for you.** The close-mode atoms scope what the agent should *itself* do *before* invoking close — the close call is just the teardown.

### 6.5 What develop never does

- Touch infrastructure topology (creating new services). That's bootstrap or `zerops_workflow action="adopt-local"`.
- Persist close-mode in the Work Session. Close-mode is **always** read fresh from `ServiceMeta` (invariant **D3**). Agents can flip it mid-task via `zerops_workflow action="close-mode"` and the next deploy honours it.

---

## 7. The three orthogonal deploy axes

Deploy configuration is **not one strategy enum**. It's three independent fields on `ServiceMeta`:

| Axis | Field | Values | Drives |
|---|---|---|---|
| **Close mode** | `CloseDeployMode` | `unset`, `auto`, `git-push`, `manual` | What `action="close"` does at end of develop |
| **Git-push capability** | `GitPushState` (+ `RemoteURL`) | `unconfigured`, `configured`, `broken`, `unknown` | Whether `zerops_deploy strategy="git-push"` works |
| **Build integration** | `BuildIntegration` | `none`, `webhook`, `actions` | ZCP-managed CI shape (only meaningful when GitPushState=configured) |

They're **orthogonal**: `GitPushState=configured` can coexist with `CloseDeployMode=auto` (capability provisioned, but close still uses zcli push). Switching `CloseDeployMode=git-push` later doesn't require re-setup.

Three actions, one axis each:

```
zerops_workflow action="close-mode"        closeMode={"appdev":"auto"}
zerops_workflow action="git-push-setup"    service="appdev" remoteUrl="git@github.com:org/repo.git"
zerops_workflow action="build-integration" service="appdev" integration="webhook"
```

### 7.1 The three close-mode behaviours

Close-mode shapes the **atoms** the agent reads before calling `action="close"` — it does NOT make the close handler dispatch a deploy. The agent is the one that runs the final action; `action="close"` only tears down the session afterward.

| Close-mode | What the close-mode atoms tell the agent to do before close | Good for |
|---|---|---|
| `auto` | Run `zerops_deploy targetService=<host>` (zcli push from `/var/www/{host}/`) one last time, then close | Tight iteration loops, single-developer projects |
| `git-push` | Commit + push to `RemoteURL`. Zerops or your CI sees the push and runs the build. Then close. | Team development, CI/CD pipelines |
| `manual` | Yield. ZCP records evidence of whatever deploys/verifies the user runs themselves. The agent just closes the session. | Experienced users, bespoke workflows |

### 7.2 Build integration — only with git-push capability

| `BuildIntegration` | After the push |
|---|---|
| `webhook` | Zerops dashboard pulls the repo and runs the build pipeline |
| `actions` | A GitHub Actions workflow runs `zcli push` from CI |
| `none` | Push is archived at remote; no ZCP-managed build fires |

---

## 8. The knowledge pipeline — how the agent learns what to do

The lifecycle status response (`zerops_workflow action="status"`) is built by the same four-stage pipeline every time. Mutation tool responses (start, complete, close, deploy, verify, …) MAY be terse leaf payloads — invariant **P4** lets them omit the envelope+plan+guidance because `action="status"` is the canonical recovery primitive after context compaction. **Pure functions over JSON state** — same envelope in, byte-identical guidance out.

```
ComputeEnvelope  →  BuildPlan  →  Synthesize  →  RenderStatus
  (state I/O)       (pure)        (pure)         (markdown)
```

| Stage | Purpose | Code |
|---|---|---|
| **ComputeEnvelope** | Single entry point for state gathering. Reads platform API + ServiceMeta files + bootstrap session + Work Session + runtime detection. Parallel I/O. | `internal/workflow/compute_envelope.go` |
| **BuildPlan** | Pure 9-case dispatch over `Phase + envelope shape`. Returns a typed `Plan{Primary, Secondary?, Alternatives[]}`. No I/O. | `internal/workflow/build_plan.go` |
| **Synthesize** | Pure. Filters the 80-atom corpus by axis match against the envelope, sorts by `(priority, id)`, substitutes placeholders, returns ordered bodies. | `internal/workflow/synthesize.go` |
| **RenderStatus** | Composes `Envelope + Guidance + Plan` into the markdown status block the LLM sees. | `internal/workflow/render.go` |

### 8.1 Atoms — the knowledge corpus

~80 markdown files in `internal/content/atoms/*.md`. Each carries YAML frontmatter declaring its **axis vector** and a markdown body:

```yaml
---
id: develop-dynamic-runtime-start-container
priority: 3
phases: [develop-active]
runtimes: [dynamic]
environments: [container]
modes: [dev, standard]
title: "Dynamic runtime — start dev server (container)"
---
After a dev-mode dynamic-runtime deploy the container runs `zsc noop`. ...
```

**Axes** the engine filters on:

| Axis | Values | Empty means |
|---|---|---|
| `phases` | idle, bootstrap-active, develop-active, develop-closed-auto, recipe-active, strategy-setup, export-active | MUST be non-empty |
| `modes` | dev, stage, simple, standard, local-stage, local-only | any mode |
| `environments` | container, local | either |
| `closeDeployModes` | unset, auto, git-push, manual | any |
| `gitPushStates` | unconfigured, configured, broken, unknown | any |
| `buildIntegrations` | none, webhook, actions | any |
| `runtimes` | dynamic, static, implicit-webserver, managed, unknown | any |
| `routes` | recipe, classic, adopt, resume | bootstrap-only; any |
| `steps` | discover, provision, close | bootstrap-only; any |
| `deployStates` | never-deployed, deployed | any |
| `idleScenarios` | empty, bootstrapped, adopt, incomplete | idle-only; any |

Service-scoped axes (`modes`, `runtimes`, `closeDeployModes`, …) match if **any** service in the envelope satisfies them; the atom then renders **once per matched service**, with `{hostname}` and `{stage-hostname}` substituted from that service.

### 8.2 The compaction-safety contract

Two LLM tool calls with the same `StateEnvelope` JSON MUST produce the same atoms in the same order. This is what lets the agent recover after a context compaction by calling `zerops_workflow action="status"` — same envelope, same plan, same atoms, same recovery prompt. No timestamps, no map iteration order, no randomness leaks into atom bodies.

---

## 9. Sessions & state — what lives where

ZCP has **two independent session kinds**, owned by different layers:

### 9.1 Infrastructure sessions (bootstrap / recipe)

`.zcp/state/sessions/{16-hex-id}.json`. Lifetime = workflow duration. Survives process restart via registry claim-on-boot. The `resume` bootstrap route exists for this case.

### 9.2 Work Sessions (develop)

`.zcp/state/work/{pid}.json`. **One per process**. Lifetime = one LLM task. Does NOT survive process restart — code work survives in git and on disk; the in-flight session metadata is disposable. Dead-PID files are pruned on engine boot.

```
WorkSession {
  PID, ProjectID, Environment, Intent, Services[],
  Deploys      map[hostname][]DeployAttempt   // capped at 10
  Verifies     map[hostname][]VerifyAttempt   // capped at 10
  ClosedAt, CloseReason
}
```

The Work Session stores **only intent + scope + deploy/verify history** — never close-mode, mode, or service status. Those are read fresh from ServiceMeta + the platform every turn (invariant **W2 / D3**).

### 9.3 Registry

`.zcp/state/registry.json` — the single source of session ownership, with `.registry.lock` for concurrency. Tracks both infra sessions and Work Sessions. Auto-prunes dead PIDs and sessions older than 24h on every new-session creation.

---

## 10. Container vs Local — the two environments

ZCP detects which environment it's in via the `serviceId` env var. Most testing happens in **container** mode.

| Aspect | Container | Local |
|---|---|---|
| Detection | `serviceId` env var present | Absent |
| ZCP runs | Inside a `zcp` service in the project | On the developer's machine |
| Code access | SSHFS mounts at `/var/www/{hostname}/` | Working directory |
| Default deploy | SSH into target service, `zcli push` from inside | `zcli push` from local machine |
| Dev-server start | `zerops_dev_server action=start` (for `zsc noop` services) | Background task primitive (e.g. `Bash run_in_background=true`) |
| Mounts | Available | Not available |

Atoms are environment-scoped: `environments: [container]` only, `[local]` only, or empty for both. The synthesiser picks the right combination.

---

## 11. Recipes vs Export — two different things

People often confuse them. They share primitives (`buildFromGit`, `zerops.yaml` at root) but the user-facing intent is different.

### 11.1 Recipes (`zerops_recipe` / bootstrap `route=recipe`)

A **multi-repo curated template**:
- An app repo (source code + recipe-shaped `zerops.yaml`).
- A recipe repo (the canonical `import.yaml` template).
- A registry entry (slug, intent description, runtime tags).

72 recipes ship today (in `internal/knowledge/recipes/` after `zcp sync pull recipes`). Examples: `nodejs-hello-world`, `laravel-minimal`, `nextjs-ssr-hello-world`, `vue-static-hello-world`. Used to bootstrap a new project from a known-good template.

### 11.2 Export (`zerops_workflow workflow="export"`)

A **single-repo self-referential snapshot**: source code + `zerops.yaml` + `zerops-project-import.yaml` all live in ONE repo, and the import yaml's `buildFromGit:` URL points at THAT same repo.

Used to capture a working deployed service into a re-importable bundle (so a colleague can spin up an identical project with one `zcli project project-import`).

---

## 12. A concrete walkthrough — "deploy a Node.js photo-uploader to Zerops"

To make the above concrete, here's what an agent does end-to-end:

```
[user] "I want to make a photo-upload app in Node.js with Postgres."

→ Agent calls zerops_workflow action="status"
  ZCP returns: Phase=idle, IdleScenario=empty, Plan.Primary=start bootstrap

→ Agent calls zerops_workflow action="start" workflow="bootstrap" intent="photo-upload Node.js app with Postgres"
  ZCP returns: routeOptions=[recipe(nodejs-hello-world, conf=0.7), classic, ...]

→ Agent picks classic (no recipe matches photo-upload exactly), commits route
→ DISCOVER step: agent submits ServicePlan { runtime: appdev/nodejs@22 mode=standard stage=appstage, deps: [db/postgresql@16 EXISTS=false] }
→ PROVISION step: agent generates import.yaml from plan, calls zerops_import (blocks until services RUNNING)
   ZCP auto-mounts /var/www/appdev/ and writes /var/www/.git/
→ CLOSE step: agent verifies services running, ZCP writes ServiceMeta with Mode=standard, BootstrappedAt=today.

[Phase 1 done. ServiceMeta written. CloseDeployMode unset, GitPushState unconfigured. No app code yet.]

→ Agent calls zerops_workflow action="start" workflow="develop" scope=["appdev"] intent="implement photo upload"
  ZCP returns Work Session + atoms: develop-first-deploy-intro, scaffold-zerops-yaml, develop-first-deploy-write-app,
  develop-first-deploy-execute, develop-first-deploy-verify, develop-first-deploy-promote-stage.

→ Agent scaffolds zerops.yaml, writes the photo-upload app code on /var/www/appdev/ (live mount).
→ Agent calls zerops_deploy targetService="appdev" (no strategy — D2a).
   ZCP runs SSH-into-container build, auto-enables subdomain, returns DEPLOYED + subdomainUrl.
   ZCP records the deploy attempt and stamps FirstDeployedAt=today on the meta (in RecordDeployAttempt).
→ Agent calls zerops_dev_server action="start" hostname="appdev" command="node server.js" port=3000 healthPath="/health"
→ Agent calls zerops_verify serviceHostname="appdev". Passes.
→ Agent writes the stage entry in zerops.yaml (real start, healthCheck, deployFiles=build output).
→ Agent calls zerops_deploy sourceService="appdev" targetService="appstage" setup="prod" (cross-deploy).
→ Agent calls zerops_verify serviceHostname="appstage". Passes.

[Both halves green. Work Session auto-close blocked by CloseDeployMode=unset → develop-strategy-review atom fires
 on the next zerops_workflow action="status" call.]

→ Agent asks user which close-mode. User says "auto, this is just a prototype".
→ Agent calls zerops_workflow action="close-mode" closeMode={"appdev":"auto"}
   ServiceMeta.CloseDeployMode is now "auto", CloseDeployModeConfirmed=true.

→ Agent reads atoms: develop-close-mode-auto says "before close, run a final zerops_deploy".
→ Agent calls zerops_deploy targetService="appdev" one last time. Verifies. (Same for stage if there are unflushed edits.)
→ Agent calls zerops_workflow action="close" workflow="develop"
   ZCP deletes the Work Session file, unregisters from registry, returns "Work session closed." That's all the close call does.

[Phase 2 done. ServiceMeta has CloseDeployMode=auto, FirstDeployedAt=today.]
```

Subsequent tasks just open a new Work Session, edit, deploy, verify, close — meta is read fresh, close-mode atoms drive the agent's behaviour before each close call.

---

## 13. What the team should test first

The 72 recipes form a 5-runtime-class × 6-mode × 2-env matrix. Testing every cell is wasteful. These **9 scenarios** cover the cross-product of all interesting axes:

| # | Recipe | Runtime class | Mode | Env | Why |
|---|---|---|---|---|---|
| 1 | `bun-hello-world` | dynamic | simple | container | Newest dynamic runtime |
| 2 | `nodejs-hello-world` | dynamic | simple | local | Most-common runtime, local env |
| 3 | `nextjs-ssr-hello-world` | dynamic (SSR) | simple | container | Asset-pipeline atoms |
| 4 | `vue-static-hello-world` | static | simple | container | Static class |
| 5 | `php-hello-world` | implicit-webserver | simple | container | PHP class — no `start:` |
| 6 | `laravel-minimal` | implicit-webserver + managed | standard | container | Multi-service, dev/stage pair, env-var resolution |
| 7 | `nestjs-minimal` | dynamic + managed | standard | container | Multi-service Node + DB, cross-deploy |
| 8 | `vue-static-hello-world` | static | simple | local | Local-env static (no SSH) |
| 9 | `nodejs-hello-world` | dynamic | dev | container | `zsc noop` start + `zerops_dev_server` lifecycle |

Run these against `eval-zcp` (the playground project per `CLAUDE.local.md`) using direct SSH access (`ssh <hostname>` with `StrictHostKeyChecking=no`).

For each scenario, the testing question is: **does the agent get the right knowledge at the right moment to do the obvious thing without guessing?** If not, file the friction — atoms are easy to fix.

### 13.1 Known issues to fix BEFORE the first test session

`docs/audit-prerelease-internal-testing-2026-04-29.md` lists 1 CRITICAL + 3 HIGH bugs that should land first:

- **C1** — stale `action="strategy"` vocabulary in 3 atoms + 5 eval/test files (handler accepts only `action="close-mode"`).
- **H1** — Plan loses `sourceService` for stage cross-deploy.
- **H2** — first-deploy stage promotion atom missing `setup="prod"`.
- **H3** — no token cap on status responses (multi-service envelopes hit 32 KB).

Until C1 lands, every internal test session will pay a "Unknown action" round-trip the moment the agent tries to set close-mode. Eval results will be polluted (false negatives) because eval scaffolding documents the wrong API.

### 13.2 The simulator — re-run after every atom edit

`internal/workflow/lifecycle_matrix_test.go` walks 45 canonical lifecycle scenarios and dumps `internal/workflow/testdata/lifecycle-matrix.md` showing which atoms fire, what the Plan is, and any anomalies (placeholder leaks, oversize briefings, legacy vocab).

```sh
ZCP_RUN_MATRIX=1 go test ./internal/workflow -run TestLifecycleMatrixDump -v -count=1
```

Diff the output against the prior commit after any atom or pipeline change.

---

## 14. Where to dive deeper

- **Workflows + invariants** — `docs/spec-workflows.md` (1300 lines, the source of truth for behaviour).
- **Architecture** — `docs/spec-architecture.md` (the 4-layer + cross-cutting model: topology / platform / ops+workflow peers / entry points).
- **Knowledge corpus authoring** — `docs/spec-knowledge-distribution.md` (atom contract, axis semantics).
- **Per-phase walkthroughs** — `docs/spec-scenarios.md` (S1-S13, pinned by `internal/workflow/scenarios_test.go`).
- **Work Session design** — `docs/spec-work-session.md`.
- **Local vs container** — `docs/spec-local-dev.md`.
- **Recipe content quality** — `docs/spec-content-surfaces.md`.
- **Live Zerops schemas** — pinned in `CLAUDE.md`:
  - import: `https://api.app-prg1.zerops.io/api/rest/public/settings/import-project-yml-json-schema.json`
  - zerops.yaml: `https://api.app-prg1.zerops.io/api/rest/public/settings/zerops-yml-json-schema.json`

---

## 15. Glossary

| Term | Definition |
|---|---|
| **Atom** | A markdown knowledge file in `internal/content/atoms/` with axis-vector frontmatter |
| **Axis** | A dimension of envelope variation an atom can filter on (phase, mode, runtime, …) |
| **BuildPlan** | Pure function that computes the typed `Plan` from the envelope |
| **Close-mode** | `CloseDeployMode` field — develop session's delivery pattern (auto/git-push/manual). Drives auto-close gating + which `develop-close-mode-*` atoms fire. Does NOT alter the `action="close"` handler (which is always pure session-teardown). |
| **ComputeEnvelope** | Single state-gathering entry point; produces `StateEnvelope` |
| **Container env** | ZCP runs inside a `zcp` service in the project; SSHFS mounts available |
| **Cross-deploy** | `sourceService != targetService` — typical for dev → stage promotion |
| **Develop flow** | Phase 2; the per-task code work + deploy lifecycle |
| **Envelope** | `StateEnvelope` JSON — single typed snapshot passed through the pipeline |
| **Evidence** | The ServiceMeta file proving ZCP manages a service |
| **First-deploy branch** | Develop atoms that fire when `Deployed=false` (scaffold + write + first deploy) |
| **Local env** | ZCP runs on developer's machine; uses `zcli push`, no SSHFS |
| **Mode** | Runtime topology (dev / stage / standard / simple / local-stage / local-only) |
| **Plan** | Typed next-action triple `{Primary, Secondary?, Alternatives[]}` |
| **Recipe** | Multi-repo curated template; bootstrap `route=recipe` consumes one |
| **Route** | Bootstrap entry-point selector (classic / recipe / adopt / resume) |
| **Runtime class** | dynamic / static / implicit-webserver / managed / unknown |
| **Self-deploy** | `sourceService == targetService` — typical first deploy |
| **ServiceMeta** | Per-runtime evidence file at `.zcp/state/services/{hostname}.json` |
| **Synthesize** | Pure function that turns envelope + atom corpus into ordered guidance bodies |
| **Verification server** | The 3-endpoint hello-world bootstrap writes; NOT the user's app |
| **Work Session** | Per-PID develop session at `.zcp/state/work/{pid}.json`; doesn't survive restart |
| **WorkflowState** | Bootstrap/recipe session at `.zcp/state/sessions/{id}.json`; survives restart |

---

*This guide intentionally omits implementation specifics that drift fast (file paths under `internal/`, exact function names). For those, read the spec docs in §14 — they're maintained in lockstep with the code.*
