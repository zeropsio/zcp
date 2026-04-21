# Improvement Guide — v8 Findings (nestjs-showcase on v8.54.1)

## Session Facts

- **Duration**: 96 min (09:17–10:54 CEST)
- **Main agent**: 507 messages, 182 tool calls
- **Subagents**: 5 (3 generate-scaffold, 1 feature-implementation, 1 code-review), 463 messages total
- **Deploy calls**: 19 total — 7 failures (37% failure rate)
- **Net deploy failures**: apidev×3, appdev×2, workerdev×1, workerstage×1
- **Root cause of all 7 failures**: code/config problems that would have been caught by running the app on the dev container before deploying

## Architecture Problem

The recipe workflow treats `zerops_deploy` as the first validation step. The agent writes code to the SSHFS mount, commits, and immediately triggers a deploy — which sends the code to the Zerops build pipeline. If `npm install` fails (bad dependency), if TypeScript doesn't compile (type error), or if the app crashes at startup (native binding mismatch), the agent only learns about it after a full build cycle (30s–3min).

**The dev containers are live development environments.** They have the runtime, the package manager, and SSH access. The correct flow is to develop ON the container — install deps, compile, start the server, verify — then deploy to snapshot the known-working state.

**Execution model**: The agent (zcp) runs on Ubuntu and has full tooling (apt, curl, git, vim). SSH to dev containers provides `{hostname}:{port}` access from zcp. Commands like `npm install`, `git init`, and file operations run from zcp via SSHFS or SSH proxy. Only the dev SERVER process needs to run ON the container itself. The agent can `curl appdev:3000` from zcp without needing any tooling on the container. This means `run.os: ubuntu` is NOT needed during recipe creation — the agent has everything it needs on the zcp side. (The recipe output may include `os: ubuntu` for end-user CDE experience, but that's a recipe-output concern, not a recipe-creation concern.)

### Current flow (v8)

```
generate → write files to SSHFS → git commit → zerops_deploy → BUILD_FAILED
  → fix → git commit → zerops_deploy → BUILD_FAILED
  → fix → git commit → zerops_deploy → DEPLOY_FAILED
  → fix → git commit → zerops_deploy → success (finally)
```

### Correct flow

```
generate → write files to SSHFS → ssh container "npm install" (deps resolve?)
  → ssh container "npx tsc --noEmit" (code compiles?)
  → ssh container "npm run start:dev &" (server starts? endpoints respond?)
  → git commit → zerops_deploy → success (first try)
```

Every failed deploy in v8 was a problem that `npm install` or `npm run start:dev` on the container would have caught in seconds.

## Findings

### Finding 1: No on-container validation before deploy

**Impact**: 6 of 7 deploy failures. ~15 min wasted on build cycles.

**What happened**: The agent wrote package.json with hallucinated dependencies (`cache-manager-ioredis-yet@^3.0.0` — doesn't exist on npm), TypeScript code with type errors (`process.env.X` without non-null assertions), and zerops.yaml missing required fields (`ports`, `initCommands`). All were discovered only after `zerops_deploy` triggered a full build cycle.

**Specific failures that on-container validation would have caught**:

| Deploy # | Service | Error | Smoke test step that catches it |
|----------|---------|-------|---------------------------------|
| apidev-1 | apidev | Hallucinated package version (not on registry) | Step 1: dependency install → immediate failure |
| apidev-2 | apidev | Peer dependency conflict | Step 1: dependency install → immediate failure |
| apidev-3 | apidev | Type error in standalone scripts | Step 2: compile check → immediate failure |
| workerdev-1 | workerdev | Same hallucinated package | Step 1: dependency install → immediate failure |
| appdev-1 | appdev | Native binding crash (build OS ≠ run OS) | Step 3: start dev server → immediate crash (also eliminated by Finding 2: no OS override) |
| appdev-2 | appdev | No ports in zerops.yaml → subdomain fails | Not caught by smoke test — zerops.yaml structural error, prevented at generate by Finding 3 |

5 of 6 are pure code/dependency problems caught by smoke test steps 1–3. The 6th is a zerops.yaml omission prevented by generate-time guidance (Finding 3).

**Structural fix**: Add an **on-container smoke test** step at the boundary between generate and deploy. This is NOT a new workflow step — it's the final act of generate, after all files are written and before the generate attestation.

The smoke test is framework-agnostic — it uses the plan's research data (package manager, compile command, start command, port) to derive the three steps:

1. **Install dependencies** on the container via SSH using the plan's package manager install command. This validates the dependency tree resolves — hallucinated packages, version conflicts, and peer dep mismatches surface here in seconds instead of after a 30s–3min build cycle.

2. **Compile/check** if the framework has a compilation step (the plan's build commands, or a framework-specific dry-run compile command). This catches type errors, syntax errors, and missing imports before deploy.

3. **Start the dev server** on the container and verify it binds to the expected port. Connection errors to managed services are expected and acceptable — env vars aren't active yet. The goal is "process starts and binds to the port from the plan's research data", not "app serves requests." If the process crashes immediately (native binding mismatch, missing module, config error), this catches it.

These commands use only the base image's tools (runtime + package manager) and don't need `run.envVariables`. The constraint "do not run commands that bootstrap the framework" in the current guide means "don't try to connect to databases" — it does NOT mean "don't validate your dependency tree resolves or your code compiles."

If the smoke test catches a dependency or compilation error, fix it on the mount and re-run. Only proceed to `zerops_deploy` when the smoke test passes.

**What this does NOT change**: The deploy step still runs `zerops_deploy`. The build pipeline still runs `buildCommands`. The smoke test is additive — it's a fast pre-flight check, not a replacement for the build pipeline.

### Finding 2: Build/run OS mismatch produces native binding crash

**Impact**: 2 appdev deploy cycles + ~8 min debugging.

**What happened**: The appdev zerops.yaml used `build.base: nodejs@22` (default OS: Alpine) and `run.os: ubuntu`. The build container compiled native modules (Rolldown/Vite 8) for Alpine/musl. `deployFiles: ./` shipped those binaries to the Ubuntu/glibc runtime. Vite started, tried to load the native Rolldown module, and crashed.

**Root cause**: The agent added `os: ubuntu` to dev setups "for richer tooling (apt, curl, git, vim)." This reasoning is wrong on two levels:

1. **The agent (zcp) already runs on Ubuntu** with full tooling. It SSHes into containers and has `{hostname}:{port}` access for testing. File editing happens via SSHFS on the zcp side. Git operations happen on the zcp side. Log reading pipes through SSH. The container itself doesn't need any tooling beyond the runtime and its package manager — those are already in the base image (Alpine includes them).

2. **End users don't need it either.** CDE users connect via VS Code Remote SSH or similar — their IDE handles editing, their terminal handles git/curl. The container just needs to run the dev server. Even `apt` is unnecessary: the base image's package manager (npm, composer, pip) handles application dependencies, and system packages are the base image's responsibility, not the user's.

The only legitimate case for `run.os: ubuntu` is when a specific runtime or native dependency has a hard glibc requirement with no musl build. That's a per-framework exception discovered during the on-container smoke test (Finding 1), not the default.

**Structural fix**: The guide should teach that `setup: dev` does NOT set `run.os` unless a specific runtime requires it. The default OS (matching the build base) is correct. This eliminates the build/run OS mismatch entirely — no mismatch means no native binding crash, no need for rebuild commands in initCommands, no Finding 2.

If a framework's smoke test reveals a glibc-only dependency (the dev server crashes on Alpine), THEN add `run.os: ubuntu` AND the appropriate rebuild command to `initCommands`. This is a reactive exception, not a proactive default.

### Finding 3: Appdev zerops.yaml missing `ports` for dev setup

**Impact**: 1 deploy cycle + subdomain enable failure.

**What happened**: The generated zerops.yaml for appdev's `setup: dev` had no `ports` declaration. The Vite dev server ran on port 5173, but without `ports: [{port: 5173, httpSupport: true}]`, the platform couldn't enable subdomain access (`serviceStackIsNotHttp` error).

**Why**: The guide's zerops.yaml authoring section teaches `ports` for prod setups but doesn't explicitly require them for dev setups of SPA targets. The agent wrote the dev setup without ports because the prod setup uses `run.base: static` (which implicitly serves on port 80).

**Structural fix**: The zerops.yaml authoring guidance for `setup: dev` must state: if the dev container runs a dev server (Vite, webpack, etc.), declare `ports` with the dev server's port and `httpSupport: true`. Without it, subdomain access cannot be enabled.

This follows from the same principle as the serve-only dev override: the dev setup is a different runtime shape than prod. Prod static serves on implicit port 80; dev runs a Node.js dev server on an explicit port. The `ports` declaration makes this explicit to the platform.

### Finding 4: runtimeType validation still fails on first attempt

**Impact**: 1 wasted workflow call. Trivial in time but reveals the guide text is insufficient.

**What happened**: The agent submitted `runtimeType: "nodejs"` (bare, no @version suffix) at research completion. Got `INVALID_PARAMETER`. Fixed to `"nodejs@24"` on retry.

**Context**: This was already fixed in v8.54.2 by adding text to the research section's Type bullet. But the agent still failed on first attempt — meaning the text either wasn't prominent enough or the agent didn't read it carefully.

**Structural fix**: This is better solved at the validation layer (the `zerops_workflow` tool's plan validation) than at the guide layer. The guide text was added, the agent still failed. The tool should provide a more helpful error message or auto-suggest the correction. Not a guide change.

### Finding 5: Recipe output has `appdev: type: static` in env0/env1

**Impact**: BLOCKER for recipe correctness. Users importing env0 (AI Agent) or env1 (Remote CDE) get a static service for appdev — no Node.js, can't run Vite dev server.

**What happened**: The finalize step generated import.yaml for environments 0 and 1 with `appdev: type: static`. During the actual build, the agent provisioned appdev as `nodejs@22` (correct). But the finalize template used the plan's target type (`static` — the prod type) instead of the dev service type.

**Context**: The plan's target for the app has `type: static` because that's the prod runtime. For environments that include dev services, the import.yaml must use the dev-appropriate type (e.g. `nodejs@22`), not the prod type. The zerops.yaml handles this via `setup: dev` having `run.base: nodejs@22`, but the import.yaml's `type` field determines the SERVICE type — and a `type: static` service cannot run Node.js.

**Structural fix**: The finalize step's import.yaml generation must distinguish between dev and prod service types for serve-only targets. When generating an environment that includes a dev service (env0, env1), and the plan target's prod type is serve-only (`static`, `nginx`), the import.yaml must use the dev setup's `run.base` as the service type instead.

This is already the pattern the guide teaches for zerops.yaml (serve-only dev override) — it just needs to extend to import.yaml generation at finalize.

### Finding 6: zerops_verify not called for 3 of 7 runtime targets

**Impact**: Reduced confidence in deploy correctness. Missing: appdev (dev), workerdev (dev), workerstage (prod).

**What happened**: The agent called `zerops_verify` for apidev (×2), apistage, and appstage. Skipped appdev, workerdev, and workerstage. The guide says "mandatory for every runtime target."

**Structural fix**: The deploy step guidance should enumerate which targets need verification, keyed to the plan shape. For the dual-runtime-with-worker shape, that's: apidev, appdev, workerdev (dev); apistage, appstage, workerstage (stage). Workers without HTTP ports can't be verified via subdomain — the guide should specify `zerops_logs` as the verification method for non-HTTP targets.

### Finding 7: Feature subagent dev-server restart contention

**Impact**: ~4 min of process management overhead (kill/restart/sleep/curl loops).

**What happened**: The feature subagent needed to restart NestJS after adding new modules (cache, storage, search, jobs). The old NestJS process (started by the main agent) was holding port 3000. The subagent's `pkill -f 'nest start'` failed (exit 255 — different SSH session semantics), then went through 3 rounds of kill/restart/verify before using `fuser -k 3000/tcp` to force-kill the port holder.

**Root cause**: The main agent started `npm run start:dev` as a background SSH process. The feature subagent (a different agent) can't reliably kill processes started by the main agent because SSH background processes survive session disconnection differently on the container.

**Structural fix**: The main agent should kill the dev server before dispatching the feature subagent, and the subagent's prompt should include the instruction to start fresh. Alternatively, the subagent should always kill the port holder first (`fuser -k {port}/tcp`) before starting the dev server.

### Finding 8: Managed service auth patterns not in knowledge base

**Impact**: 1 workerstage failure + feature agent debugging time.

**What happened**: The NATS client connection was initially configured with credentials embedded in the URL (`nats://user:pass@host:port`). The NATS client library silently ignores URL-embedded credentials — they must be passed as separate connection options. The feature agent discovered this for the API after debugging Authorization Violation errors, but the same fix wasn't applied to the worker until workerstage failed.

This is a pattern problem, not a framework problem. Every managed service has connection quirks that aren't in the framework docs — they're in the Zerops platform's interaction with that service's client library. These quirks are predictable and documentable.

**Structural fix**: The knowledge delivery system should provide managed-service connection patterns when the plan includes those services. The knowledge should cover auth format, connection string construction, and known pitfalls for each managed service type. This is platform knowledge, not framework knowledge — it applies regardless of which framework connects to NATS/Redis/S3/etc.

The pattern should be injected into the generate subagent prompt so the correct connection code is written from the start, not discovered through deploy failures.

### Finding 9: `execOnce` key burned on failed deploys

**Impact**: Agent had to run seed manually via SSH after the successful deploy.

**What happened**: `zsc execOnce ${appVersionId}` gates on a unique key per deploy version. A deploy that fails AFTER `initCommands` start running can partially burn the key — the migration might run but the seed might not. On the next deploy attempt (new appVersionId), a fresh key means everything reruns. But if a deploy partially succeeds and then the container is replaced, the key is burned for commands that did run, while commands that didn't run are skipped.

The TIMELINE records: "execOnce burn-on-failure: seed didn't run because key burned on failed deploy. Ran manually via SSH."

**Structural fix**: Two layers:

1. **Prevention (primary)**: Finding 1's on-container smoke test eliminates the deploy failures that cause key burn in the first place. If the code validates before deploy, `initCommands` only run against known-good code on the first attempt.

2. **Detection (secondary)**: The deploy verification step should check that `initCommands` actually completed (e.g., seed data exists, search index populated). If they didn't, the agent runs them manually via SSH. The deploy guide should document this as a post-deploy verification step, not assume `initCommands` always succeed.

## Implementation Priority

### Phase 1: On-container smoke test (Finding 1) — highest impact

Add to the generate step, between "Pre-deploy checklist" and "Completion":

A new block that teaches the on-container validation pattern. The block should:
1. State the principle: validate on the container before deploying
2. List the three commands (install, compile-check, start) as framework-agnostic patterns using the plan's research data
3. Explain that connection errors to managed services are expected (env vars not active yet) — the goal is "process starts and binds to port", not "app serves requests"
4. State that the smoke test failure means "fix on the mount and re-run", not "deploy and see"

The block should be gated to always-emit (no predicate) since it applies to every recipe type.

### Phase 2: Dev setup correctness (Findings 2, 3)

Extend the zerops.yaml authoring guidance for `setup: dev`:

1. **No `run.os` override by default**: The agent operates from zcp (Ubuntu) via SSH proxy. The dev container needs only the runtime and package manager — both are in the base image. Omitting `run.os` means build and run share the same OS → no native binding mismatch. Only add `run.os` if the on-container smoke test reveals a hard glibc dependency (and then also add `initCommands` with the package manager's rebuild command).

2. **Dev ports rule**: If the dev container runs a dev server (any framework with a bundler or HMR server), declare `ports` with the dev server's port and `httpSupport: true`. Without it, subdomain access fails.

Both rules should be in the zerops.yaml authoring block for `setup: dev`.

### Phase 3: Finalize serve-only type override (Finding 5)

The finalize step's import.yaml generation must use the dev service type (from zerops.yaml `setup: dev` → `run.base`) for serve-only targets in dev-containing environments (env0, env1). This is a code change in the finalize workflow, not a guide change.

### Phase 4: Verification completeness + subagent hygiene (Findings 6, 7)

1. The deploy verification section should enumerate all targets that need verification, distinguishing HTTP targets (zerops_verify + subdomain) from non-HTTP targets (zerops_logs only).

2. The feature subagent dispatch should include: (a) main agent kills dev servers before dispatch, (b) subagent prompt includes "kill port holders before starting dev server" instruction.

### Phase 5: Knowledge delivery for managed service patterns (Findings 8, 9)

1. Managed service connection patterns: the knowledge delivery system should inject auth format, connection string construction, and known client-library pitfalls for each managed service type in the plan. This is platform knowledge (how Zerops-provisioned services expose credentials), not framework knowledge.

2. Post-deploy initCommands verification: the deploy step should verify that initCommands completed successfully (check for expected data/state), not assume they ran. Document the `execOnce` key-burn risk in deploy retry guidance so the agent knows to check and re-run manually if needed.

## Verification Checklist

After implementation, the next test run should show:

- [ ] Agent runs the package manager's install command on each dev container before the first `zerops_deploy`
- [ ] Agent runs the framework's compile-check command (if any) before deploy
- [ ] Agent starts the dev server on the container and verifies it binds before deploy
- [ ] Zero BUILD_FAILED deploys from dependency or compilation errors
- [ ] Dev setup zerops.yaml has `ports` for dev-server targets from initial generate (not added during deploy retries)
- [ ] Dev setup zerops.yaml does NOT have `run.os` override (no build/run OS mismatch)
- [ ] Dev-containing environment import.yaml uses the dev-appropriate service type for serve-only targets (not the prod type)
- [ ] `zerops_verify` or `zerops_logs` called for every runtime target (HTTP and non-HTTP)
- [ ] Feature subagent finds clean port (main agent kills dev servers before dispatch)
- [ ] Managed service auth patterns (NATS, etc.) correct from generate, not discovered at deploy
- [ ] runtimeType submitted with @version suffix on first attempt
