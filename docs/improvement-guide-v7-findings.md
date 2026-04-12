# Recipe Workflow Improvement Guide — nestjs-showcase-v7 Findings

**Context.** This document captures every structural weakness found by deep analysis of the `nestjs-showcase-v7` recipe session (758 messages, 278 tool calls, ~90 minutes of agent time). The session was the first live run after the v8.54.0 dimension-based block rewrite. The recipe succeeded end-to-end (3 codebases, 8 services, 6 environment tiers, browser-verified dev+stage) but exposed 7 content gaps and 2 code-side issues that cost the agent significant wasted work.

**Audience.** An Opus implementator in a fresh Claude Code session with access to the full ZCP codebase. Every finding lists the root cause, the files to change, and the exact edit. No ambiguity, no exploratory research needed.

**Codebase entry points:**
- Recipe markdown: `internal/content/workflows/recipe.md`
- Section catalogs: `internal/workflow/recipe_section_catalog.go`
- Plan predicates: `internal/workflow/recipe_plan_predicates.go`
- Guidance assembly: `internal/workflow/recipe_guidance.go`
- Test fixtures: `internal/workflow/recipe_guidance_test.go` (fixtureForShape, showcaseStepCaps)
- Audit harness: `internal/workflow/recipe_guidance_audit_test.go` (//go:build audit)

**Mandatory workflow.** TDD: RED → GREEN → REFACTOR. `go test ./... -count=1 -short` must pass. `make lint-local` must report 0 issues. Run the audit harness (`go test -tags audit ./internal/workflow/... -run TestAuditComposition -v`) after every recipe.md edit to verify monotonicity and per-shape byte profiles. Shape caps in `showcaseStepCaps` may need adjustment — bump the specific cell, never reduce.

---

## Finding 1: Serve-only targets must be provisioned with a toolchain-capable service type

### What happened

The agent's research plan declared the frontend target as `{"hostname": "app", "type": "static", "role": "app"}`. At provision, it faithfully created the workspace import with `hostname: appdev, type: static`. This gave appdev a container running Nginx with no Node.js runtime. The agent could not SSH in to scaffold Vite, could not run `npm install`, could not run `svelte-check` or `vite build`. It was forced to:

1. Scaffold Vite in `/tmp` on the ZCP host (not on any container).
2. `cp -r /tmp/vite-svelte-scaffold/. /var/www/appdev/` via the SSHFS mount.
3. `cd /var/www/appdev && npm install` on the ZCP host (SSHFS path, ZCP's own Node).
4. Five attempts to run `svelte-check` / `vite build` on the ZCP host before finding `node node_modules/vite/bin/vite.js build` worked.

The zerops.yaml's `setup: dev` correctly overrides `run.base: nodejs@22` — but that override only activates after the first `zerops_deploy`. Between provision and first deploy, the container has only Nginx.

### Root cause

The guide's `import-yaml-standard-mode` block says "each runtime gets a `{name}dev` + `{name}stage` pair" but never says the dev service type should differ from the plan target's type. The `serve-only-dev-override` block exists but only addresses the zerops.yaml `run.base` field — it doesn't teach the workspace import service type.

The plan's `type` field represents the prod runtime. For serve-only bases (`static`, `nginx`), the dev workspace service must use a toolchain-capable type (typically the same runtime listed in `build.base` for that target's zerops.yaml, e.g. `nodejs@22` for a Vite/Svelte SPA whose prod is `static`).

### Fix

**File: `internal/content/workflows/recipe.md`**

In the `import-yaml-standard-mode` block (inside `<section name="provision">`), after the sentence about `startWithoutCode`, add a new paragraph:

```
**Serve-only targets need a toolchain-capable service type for dev.** If the plan's
target type is a serve-only base (static, nginx), the `{name}dev` service must use a
different type that can host the framework's dev toolchain — typically the same runtime
the zerops.yaml's `build.base` will use (e.g. `nodejs@22` for a Vite/Svelte SPA). The
serve-only base is a prod-only concern (the zerops.yaml's `setup: prod` uses
`run.base: static`); the dev container needs a shell, a package manager, and the dev
server binary. The `{name}stage` service keeps the plan's target type because stage
runs the prod setup via cross-deploy. Example: plan target `type: static` →
`appdev type: nodejs@22` + `appstage type: static`.
```

Also add a matching note in the `import-yaml-static-frontend` block, which already fires on `hasServeOnlyProd`. After "Dev still gets `startWithoutCode: true` for the build container", add:

```
**Service type for dev**: use the toolchain runtime (typically `nodejs@22` or `bun@1`)
as the service type for `{name}dev`, not `static`. The dev container must host the
framework's dev server (Vite, webpack, etc.) over SSH — a static/Nginx container
has no shell, no package manager, and no Node. The `{name}stage` service keeps
`type: static` because it runs `setup: prod` (cross-deploy from dev builds the
bundle, Nginx serves it).
```

**File: `internal/workflow/recipe_guidance_test.go`**

The `ShapeDualRuntimeShowcase` fixture at `fixtureForShape` already has `{Hostname: "app", Type: "static", Role: "app"}`. This correctly represents the plan's target type. No fixture change needed — the plan type IS static; the workspace import should translate it. The translation happens at the guide content level (teaching the agent), not at the predicate level.

### Verification

After editing recipe.md, run `go test -tags audit ./internal/workflow/... -run TestAuditComposition -v`. The provision step for all 4 shapes should show the new paragraph in the output. Check that `dual-runtime-showcase` and `fullstack-showcase` (which fire `hasServeOnlyProd`) show both the standard-mode note and the static-frontend note. Hello-world and backend-minimal should show neither (no serve-only target).

---

## Finding 2: URL constants with port suffixes must be taught at provision, not only at generate

### What happened

The agent set project-level URL constants at provision (correct timing per the guide), but used the wrong URL shape — no port suffix:

```
DEV_API_URL=https://apidev-2121.prg1.zerops.app        # wrong: missing -3000
STAGE_API_URL=https://apistage-2121.prg1.zerops.app     # wrong: missing -3000
DEV_APP_URL=https://appdev-2121.prg1.zerops.app          # wrong: missing -5173
```

It only discovered the correct shape (with port suffixes) at deploy, after enabling subdomains and seeing the actual URLs. It then had to call `zerops_env set` three more times to correct the values, each time triggering container restarts that killed all SSH-launched processes.

### Root cause

The URL format with `{port}` segments is only documented in the generate section's `dual-runtime-url-shapes` block (lines 448-477 of recipe.md). But the guide tells the agent to set the URL constants at provision — before generate is reached. The agent must look ahead into a step it hasn't been given yet to get the format right.

### Fix

**File: `internal/content/workflows/recipe.md`**

In the `import-yaml-dual-runtime` block (inside `<section name="provision">`, fires on `isDualRuntime`), add a URL constant subsection. Currently this block teaches the `zerops_env project=true` invocation pattern. After the existing content, add:

```
**URL shape — port suffix rule.** Dynamic runtime services (nodejs, php-nginx, go, etc.)
include the port in their subdomain URL: `https://{hostname}-${zeropsSubdomainHost}-{port}.prg1.zerops.app`.
Serve-only/static services omit the port segment: `https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app`.
The port comes from the target's `run.ports[0].port` in zerops.yaml — you're writing
zerops.yaml at the next step but you already know the port from the plan's `httpPort`
research field (e.g. 3000 for NestJS, 5173 for Vite dev, 80 for static). Set the URL
constants with the correct port suffix NOW, at provision, to avoid costly re-sets later
(each `zerops_env set` restarts all affected containers, killing any SSH-launched
processes). Static frontends in dev mode (Vite on port 5173) use the dev server port,
not port 80 — the dev setup overrides the static base with a toolchain runtime.

The generate step's "Dual-runtime URL env-var pattern" section has the full 6-env
breakdown (DEV_* + STAGE_* for envs 0-1, STAGE_* only for envs 2-5). At provision
you only need the workspace pair: set DEV_* and STAGE_* with the correct port suffixes.
```

### Verification

After the edit, run the audit harness. The `dual-runtime-showcase` provision output should show the new URL shape paragraph. `fullstack-showcase` should NOT show it (not dual-runtime, no `import-yaml-dual-runtime` block). Hello-world and backend-minimal should not show it either.

---

## Finding 3: `.git` ownership trap on SSHFS-mounted directories

### What happened

The agent ran `git init` and `git add -A && git commit` on each SSHFS mount (from the ZCP host, as root). This created `.git/` directories owned by `root:root`. When `zerops_deploy` triggered, the deploy process (running as user `zerops` uid 2023 inside the container) could not lock `.git/config`, failing with `fatal: could not lock config file .git/config: Permission denied`.

The agent diagnosed and fixed it with `ssh {hostname} "sudo chown -R zerops:zerops /var/www/.git"` on all 3 containers. This took 3 tool calls and 2 minutes of wall time.

### Root cause

The guide's `git-config-mount` block teaches `git config --global --add safe.directory` for both zcp-side and container-side but does not mention file ownership. SSHFS mounts are root-owned because the MCP agent runs as root on the ZCP host. The deploy process runs as `zerops` inside the container. Git operations during deploy (the build pipeline invokes git to detect the working tree) fail on permission.

### Fix

**File: `internal/content/workflows/recipe.md`**

In the `git-config-mount` block (inside `<section name="provision">`), after the existing content about safe.directory, add:

```
**Ownership fix after git init.** SSHFS-created files are owned by `root` (the MCP
agent's user). The deploy process runs as `zerops` (uid 2023) inside the container and
must be able to lock `.git/config`. After `git init` + first commit on the SSHFS mount,
run `sudo chown -R zerops:zerops /var/www/.git` on each dev container via SSH. Without
this, the first `zerops_deploy` fails with `fatal: could not lock config file
.git/config: Permission denied`. Do this once per mount, immediately after the initial
commit — subsequent SSHFS writes to tracked files don't touch `.git/` internals.
```

**File: `internal/content/workflows/recipe.md`**

Also add a row to the `common-deployment-issues` table in the deploy section:

```
| Permission denied on `.git/config` | `.git/` created by root (SSHFS), deploy runs as `zerops` | `ssh {hostname} "sudo chown -R zerops:zerops /var/www/.git"` on each dev container before first deploy |
```

### Verification

Audit harness: provision and deploy byte counts will each grow by ~200-400 bytes across all shapes (these blocks are always-on). Adjust caps if needed. The new content should appear in every shape's provision and deploy output.

---

## Finding 4: `deployFiles: ./dist/~` tilde syntax needs explicit teaching for static-base prod

### What happened

The agent wrote `deployFiles: ./dist` (no tilde) for appdev's prod setup. Static-base Nginx serves from `/var/www/`, so `./dist` shipped the directory itself → files landed at `/var/www/dist/index.html` → 404 at root. The agent diagnosed this at stage cross-deploy (msg 535: `ssh appstage "ls /var/www/dist/"` revealed the nested structure), then fixed with `./dist/~`.

### Root cause

The tilde suffix is a Zerops-specific convention not documented in Node.js or Vite docs. The chain recipe's zerops.yaml template (nestjs-minimal) doesn't use a static base, so the tilde pattern isn't in the injected knowledge. The guide's `setup-prod-rules` block says "Follow the chain recipe's prod setup as a baseline" but the chain recipe doesn't have a static-base prod setup to follow.

### Fix

**File: `internal/content/workflows/recipe.md`**

In the `serve-only-dev-override` block (fires on `hasServeOnlyProd`), which currently only talks about the dev setup's base override, add a prod-specific note:

```
- **Serve-only prod `deployFiles` — use the tilde suffix.** When `setup: prod` uses
  `run.base: static` (or `nginx`), the build step compiles assets into a subdirectory
  (e.g. `./dist/`). Nginx serves from `/var/www/`, so `deployFiles: ./dist` ships the
  directory wrapper and files land at `/var/www/dist/index.html` — a 404 at root. The
  tilde suffix (`./dist/~`) strips the parent directory prefix: files land directly at
  `/var/www/index.html`. This is a platform convention, not documented in framework
  guides. Always use `./dist/~` (or the equivalent output path) for static-base prod
  setups.
```

This block fires on `hasServeOnlyProd`, so only shapes with static/nginx targets see it.

### Verification

Audit harness: `fullstack-showcase` and `dual-runtime-showcase` generate output should contain the tilde rule. Hello-world and backend-minimal should not (no serve-only prod target).

---

## Finding 5: Service-level env var shadow-loop risk during debugging

### What happened

When the agent discovered that `S3_ENDPOINT=http://${storage_apiHost}` was wrong (MinIO needs HTTPS), its first fix was `zerops_env action="set" serviceHostname="apidev" variables=["S3_ENDPOINT=https://storage-prg1.zerops.io"]` — a service-level env var with a hardcoded URL. It then also fixed the zerops.yaml to use `${storage_apiUrl}` and redeployed.

The intermediate state was dangerous: a service-level `S3_ENDPOINT` shadows the zerops.yaml's `run.envVariables.S3_ENDPOINT` after deploy. The platform resolves service-level vars first, so the hardcoded URL would persist even after the zerops.yaml fix was deployed. In this case the redeploy's env vars happened to have the same effect (the apiUrl resolves to the same HTTPS endpoint), so no visible bug — but for a different var it could have caused a silent shadow-loop.

### Root cause

The guide's `dual-runtime-what-not-to-do` block warns about shadowing project-level vars but doesn't generalize to "never set service-level env vars as a debugging shortcut." The common issues table says "After `zerops_env set`, restart the service" but doesn't warn that the service-level var will shadow the zerops.yaml's declaration on subsequent deploys.

### Fix

**File: `internal/content/workflows/recipe.md`**

In the `common-deployment-issues` block (deploy section, always-on), add a row:

```
| Env var not updating after zerops.yaml fix | Service-level env var (set via `zerops_env`) shadows zerops.yaml `run.envVariables` | Delete the service-level var (`zerops_env action="delete"`) before redeploying. Never use `zerops_env set serviceHostname=...` as a debugging shortcut for vars that belong in zerops.yaml — the service-level var takes precedence on every subsequent deploy, silently ignoring your zerops.yaml fix. Fix the zerops.yaml and redeploy; if you need to verify a value quickly, read it from logs after deploy, don't inject it as a service-level var. |
```

### Verification

Audit harness: deploy output for all shapes grows by one table row (~300 bytes). Adjust caps if needed.

---

## Finding 6: `zerops_verify` underused — agent relied on curl + browser walks

### What happened

The agent called `zerops_verify` exactly once (msg 413, apidev) across the entire session. For appdev, workerdev, and all stage targets, it used `curl` + `zerops_browser` instead. `zerops_verify` runs a standardized check suite (service status, subdomain reachability, HTTP response, health endpoint) that catches things manual curl doesn't — like readiness probe misconfiguration, env var binding failures, and service state inconsistencies.

### Root cause

The guide's deploy section mentions `zerops_verify` in Step 3 and Step 3-API but doesn't emphasize it as mandatory for every deployable target. The stage flow (Step 7) shows `zerops_verify serviceHostname="appstage"` but in an example block that also lists `zerops_subdomain`, and the agent pattern-matched the subdomain enable but skipped the verify.

### Fix

**File: `internal/content/workflows/recipe.md`**

In the `stage-deployment-flow` block, in the Step 7 section, strengthen the language. After the subdomain enables, add an explicit mandate:

```
**`zerops_verify` is mandatory for every runtime target after every deploy — dev and stage.**
It runs a standardized check suite that catches readiness-probe misconfiguration, env-var
binding failures, and container state inconsistencies that `curl` alone misses. Call it
for every `{name}dev` after self-deploy, and for every `{name}stage` after cross-deploy.
Worker targets without HTTP: skip `zerops_verify` (it checks HTTP endpoints), verify via
`zerops_logs` instead.
```

### Verification

This is a prose-only change in an always-on block. Audit harness shows the new paragraph in every shape's deploy output. Minimal byte impact (~400 bytes).

---

## Finding 7: Agent wasted 5 Bash calls trying to run vite/svelte-check on ZCP host

### What happened

After `npm install` on the SSHFS mount (ZCP host context), the agent tried:
1. `npm run check` → `sh: 1: svelte-check: not found`
2. `npx svelte-check --tsconfig ./tsconfig.app.json` → `sh: 1: svelte-check: not found`
3. `ls node_modules/.bin/ | grep svelte` → found `svelte-check`
4. `node ./node_modules/svelte-check/bin/svelte-check.js` → wrong path
5. `node node_modules/svelte-check/bin/svelte-check` → success

Then for `vite build`:
1. `npm run build` → `sh: 1: vite: not found`
2. `node node_modules/vite/bin/vite.js build` → success

This is a consequence of Finding 1 (static-base provisioning). If appdev had been provisioned as `nodejs@22`, the agent would have run `ssh appdev "npm run check"` and `ssh appdev "npm run build"` without any PATH issues.

### Root cause

When running npm scripts on the ZCP host via SSHFS mount, `node_modules/.bin/` is not on `PATH` (the npm run wrapper normally handles this). The agent had to manually resolve the bin path. This is a toolchain gap that only manifests when the dev container can't host the operations.

### Fix

This is fully addressed by Finding 1. Once the dev service type is correct, all npm operations run via `ssh {hostname} "cd /var/www && npm run ..."` and the container's `PATH` includes `node_modules/.bin/` via npm's script runner.

No additional guide change needed beyond Finding 1.

---

## Finding 8: Multiple `zerops_env set` calls cause cascading container restarts

### What happened

The agent called `zerops_env set project=true` three times during the session:
1. JWT_SECRET (provision) — restarts all containers (no processes running yet, harmless)
2. DEV_API_URL + DEV_APP_URL + STAGE_API_URL + STAGE_APP_URL (provision) — restarts all, kills SSH processes
3. DEV_API_URL with port suffix (deploy, after discovering the actual URL) — restarts all, kills all 3 dev server processes
4. STAGE_API_URL with port suffix (deploy) — restarts all again
5. DEV_APP_URL with port suffix (deploy) — restarts all again

Each project-level env set triggers restarts of ALL running dev containers. Between calls 3-5, the agent had to restart all 3 SSH processes each time. That's 3 × 3 = 9 process restarts for what should have been one batch update.

### Root cause

Two issues: (a) the agent didn't know the correct URL shape at provision time (Finding 2), and (b) it didn't batch the corrections into a single `zerops_env set` call with all 4 variables.

### Fix

Finding 2 addresses the root cause (teach the URL shape at provision so the first set is correct). Additionally, in the `import-yaml-dual-runtime` block, after the URL shape note from Finding 2, add:

```
**Batch all project-level env vars into a single `zerops_env set` call.** Each call
restarts every container that reads project-level vars. Multiple calls in sequence
trigger multiple cascading restarts, each killing any SSH-launched processes. Set
JWT_SECRET, all DEV_* URLs, and all STAGE_* URLs in one invocation.
```

### Verification

Prose-only change in a dual-runtime-gated block. Minimal byte impact.

---

## Finding 9 (code-side): `needsMultiBaseGuidance` should also check `hasServeOnlyProd` targets with build.base

### What happened

This is not from the v7 session directly but from the predicate analysis during the fix cycle. `needsMultiBaseGuidance` in `recipe_multibase.go` detects multi-base by checking for JS package-manager invocations in BuildCommands on a non-JS primary runtime. But it doesn't cover the case where the primary runtime IS JS (nodejs) yet a static-base frontend target needs its own `build.base: nodejs@22` because its service type is `static`.

In the v7 session, the appdev target's zerops.yaml has `build.base: nodejs@22` for both setups — this is the standard pattern for SPA frontends on Zerops. But the multi-base detection never fires for this shape because the primary runtime is nodejs and both targets are nodejs-family. This isn't a bug (there's no multi-base asymmetry), but it's worth documenting that the tilde-syntax and dev-base-override guidance is the serve-only path, not the multi-base path.

### Fix

No code change. Document in `recipe_plan_predicates.go` near the `hasServeOnlyProd` docstring:

```
// Note: a serve-only prod target with build.base: nodejs@22 is NOT multi-base —
// it's a single-base build (nodejs) with a different run.base (static/nginx).
// The multi-base path (needsMultiBaseGuidance) covers cross-runtime builds
// like php@8.3 + nodejs@22. The serve-only path covers same-runtime builds
// where the prod container is serve-only and the dev container overrides to
// the toolchain runtime.
```

---

## Implementation order

1. **Finding 1** (serve-only service type) — highest impact, root cause of most wasted work
2. **Finding 3** (.git ownership) — hits every recipe, easy fix
3. **Finding 4** (tilde syntax) — hits every static-base recipe
4. **Finding 2** (URL port suffixes at provision) — hits every dual-runtime recipe
5. **Finding 5** (shadow-loop warning) — defensive, prevents silent bugs
6. **Finding 8** (batch env vars) — one sentence, prevents cascading restarts
7. **Finding 6** (zerops_verify mandate) — prose strengthening
8. **Finding 9** (docstring clarification) — informational

All edits are to `recipe.md` (content) + one docstring in `recipe_plan_predicates.go`. No predicate changes, no catalog changes, no new blocks. Expected byte impact: ~2-4 KB across provision, generate, and deploy for showcase shapes. Adjust `showcaseStepCaps` as needed after measuring via the audit harness.

---

## What went right (preserve these patterns)

- **Dual-runtime deploy ordering was correct on first attempt.** The guide's deploy section taught apidev-first clearly; the agent followed it.
- **Sub-agent dispatch at deploy Step 4b, not at generate.** Feature implementation happened against live services — the Phase 10 design is validated.
- **3 zerops.yaml files with correct setup shapes.** The 4-row table in `zerops-yaml-header` was the decisive reference; the agent found it and used it.
- **All env vars use `${cross_service}` references.** Zero hardcoded credentials. The provision-step env-var discovery + attestation pattern works.
- **Comment ratios 35-45%.** Well above the 30% floor. The injected chain recipe set the right example.
- **The dimension-based blocks didn't confuse the agent.** No "3 codebases (apidev, appdev, workerdev)" hardcoding in any block — the generic phrasing worked as intended.
- **ESM SDK pragmatic substitution.** The agent replaced the meilisearch SDK with fetch() calls when it hit an ESM/CJS incompatibility. This is the kind of framework-specific judgment the recipe workflow can't prescribe — and the agent handled it well.
