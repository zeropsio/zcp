---
# Axis K candidates — abstraction-leak corpus scan (Phase 2)

Round: PRE-WORK / CORPUS-SCAN per §10.1 P2 row 1
Date: 2026-04-27
Reviewer: Codex
Plan: plans/atom-corpus-hygiene-followup-2026-04-27.md §3 Axis K (post-Phase-0 amendment 1)

## Methodology

Scanned all 79 atoms under internal/content/atoms/*.md. For each
atom, identified sentences/phrases that mention OUTSIDE-envelope
flows, mechanisms, or implementation details. Classified per the
§3 Axis K HIGH-risk signal list (#1-5). Default rule: when
uncertain, KEEP.

## Summary table

| # | atoms scanned | leaks found | DROP | KEEP-AS-GUARDRAIL | REPHRASE | est. recoverable bytes |
|---|---:|---:|---:|---:|---:|---:|
| Total | 79 | 79 | 8 | 60 | 11 | 2385 B |

## Candidates ranked by recoverable bytes (largest first per class)

### REPHRASE candidates

| # | atom-id | file | lines | pre-edit (verbatim, ≤150 chars) | signal-check | bytes recoverable | proposal |
|---|---|---|---|---|---|---:|---|
| 1 | develop-platform-rules-local | internal/content/atoms/develop-platform-rules-local.md | L52-L62 | **`strategy=git-push` needs a user-owned git repo.** Before calling `zerops_deploy strategy=git-push`, verify the working dir... | Signal #3/#5; guardrail is needed, rationale over-explains local git policy. | 260 | `For strategy=git-push, verify a committed local git repo first. If none exists, ask the user to initialize/commit; ZCP does not initialize user repos. Default push-dev needs no git state.` |
| 2 | export | internal/content/atoms/export.md | L19-L23 | **How buildFromGit works** — Zerops pulls code + `zerops.yaml` from the repo on deploy; `zeropsSetup` picks which `setup:`... | Signal #5 via Do NOT copy pipeline fields; mechanism detail can be compressed. | 220 | `For buildFromGit imports, keep only repo selector fields (`buildFromGit`, `zeropsSetup`) plus platform settings; build/run/deploy pipeline stays in repo zerops.yaml.` |
| 3 | develop-ready-to-deploy | internal/content/atoms/develop-ready-to-deploy.md | L28-L37 | **Re-import with `startWithoutCode: true` and `override=true`.** Use this when you need the container ACTIVE *before* deploying real... | Signal #4 recovery path; long API mechanism can be shorter. | 170 | `If you need ACTIVE before code exists, re-import the service with startWithoutCode: true and override=true; then resume normal deploy flow.` |
| 4 | develop-first-deploy-write-app | internal/content/atoms/develop-first-deploy-write-app.md | L48-L54 | **Don't run `git init` from the ZCP-side mount.** Push-dev deploy handlers manage the container-side git state — calling `git... | Signal #1/#4; recovery is load-bearing, mechanism can be shorter. | 150 | `Do not run git init on the SSHFS mount. If it happened, remove /var/www/.git over SSH; the next push-dev deploy recreates tool-owned git state.` |
| 5 | strategy-push-git-push-local | internal/content/atoms/strategy-push-git-push-local.md | L42-L50 | - ZCP never sets `GIT_TOKEN` on the project for the local path. A `GIT_TOKEN` set for the container's benefit... | Signal #2/#5; local/container contrast is needed but list is wordy. | 140 | `Local push-git uses local credentials only; ignore project GIT_TOKEN/.netrc. ZCP only pushes committed branch HEAD and reports uncommitted changes as warnings.` |
| 6 | develop-first-deploy-asset-pipeline-container | internal/content/atoms/develop-first-deploy-asset-pipeline-container.md | L36-L50 | **For iterative frontend work, start the dev server instead** — it watches files and survives template edits without repeated manual builds: | Signal #1/#3; HMR guardrail needed, example and rationale can shrink. | 125 | `For frontend iteration, start the dev server over SSH after deploy. Do NOT add npm run build to dev buildCommands; it defeats HMR-first dev setup.` |
| 7 | develop-first-deploy-asset-pipeline-local | internal/content/atoms/develop-first-deploy-asset-pipeline-local.md | L38-L51 | **For iterative frontend work, run Vite locally on your machine:** | Signal #1/#2; local-HMR guardrail needed, mechanism can shrink. | 120 | `For frontend iteration, run Vite locally and deploy when stable. Do NOT add npm run build to dev buildCommands; it breaks local-HMR-first setup.` |
| 8 | develop-manual-deploy | internal/content/atoms/develop-manual-deploy.md | L23-L40 | **Dev services (`zsc noop`):** Server does not auto-start after deploy. Start via `zerops_dev_server` in container env... | Signal #2/#3; mixed env guidance is useful but too broad for manual strategy. | 110 | `After any external deploy, dev services still need their environment-specific dev-server start path; stage/simple services auto-start. Code-only dev changes can restart the dev server without redeploy.` |
| 9 | develop-dev-server-triage | internal/content/atoms/develop-dev-server-triage.md | L21-L29 | \| `runtimeClass: implicit-webserver` \| Always live post-deploy \| Platform-owned — no manual start \| | Signal #2/#3; table is useful but repeats lifecycle taxonomy. | 95 | `Only dynamic dev-mode needs a manual dev-server action. Static, implicit-webserver, simple, and stage lifecycles are platform-owned after deploy.` |
| 10 | develop-static-workflow | internal/content/atoms/develop-static-workflow.md | L23-L27 | **Build step** (Tailwind, bundler, SSG like Astro or Eleventy): the build runs in the Zerops build container during deploy,... | Signal #2; local-vs-build mental model needed, wording can shrink. | 80 | `Asset builds run during Zerops deploy; local builds are only previews and Zerops rebuilds at deploy time.` |
| 11 | develop-dynamic-runtime-start-container | internal/content/atoms/develop-dynamic-runtime-start-container.md | L32-L35 | Response carries `running`, `healthStatus` (HTTP status of the health probe), `startMillis` (time from spawn to healthy),... | Partly implementation detail; keep only operational response fields. | 70 | `Use running, healthStatus, reason, and logTail from the response before making another call.` |

### DROP candidates (LOW-risk only)

| # | atom-id | file | lines | pre-edit (verbatim, ≤150 chars) | signal-check | bytes recoverable | rationale |
|---|---|---|---|---|---|---:|---|
| 1 | bootstrap-close | internal/content/atoms/bootstrap-close.md | L23-L26 | ServiceMeta records are on-disk evidence authored by bootstrap and adoption; their envelope projection is the `ServiceSnapshot` with `bootstrapped: true`, | Pure implementation storage/projection detail; no operational choice changes. | 180 | Agent only needs the envelope fields and next workflow action. |
| 2 | bootstrap-recipe-import | internal/content/atoms/bootstrap-recipe-import.md | L34-L35 | Recipes provision via `buildFromGit` — expect 2–5 minutes for first provision (vs ~30s for empty-container provisions). Poll with: | Comparative timing trivia; no HIGH-risk signal. | 150 | Polling instruction remains without cross-flow timing comparison. |
| 3 | idle-orphan-cleanup | internal/content/atoms/idle-orphan-cleanup.md | L22-L24 | Reset clears every meta whose live counterpart is gone (orphan diff against the live API), plus unregisters any dead bootstrap session. | Mechanism detail behind reset; no separate action choice. | 120 | The command and effect are enough. |
| 4 | develop-push-dev-workflow-dev | internal/content/atoms/develop-push-dev-workflow-dev.md | L37-L39 | Read `reason` on any failed start/restart — the code classifies the failure (connection refused, HTTP 5xx, spawn timeout, worker exit) | Implementation phrasing ("the code classifies") with no extra guardrail. | 95 | The atom already tells the agent to read `reason`. |
| 5 | strategy-push-git-trigger-actions | internal/content/atoms/strategy-push-git-trigger-actions.md | L90-L93 | The first push also fires the Actions workflow. Two builds happen on this push — Zerops's own (via `git-push`) and Actions's... | Informational implementation outcome; no different action required. | 90 | Redundant-first-build explanation is not needed to choose setup steps. |
| 6 | bootstrap-wait-active | internal/content/atoms/bootstrap-wait-active.md | L22-L23 | Repeat until every service reports `status: ACTIVE`. The polling itself is free — no side effects — so a tight loop (every few seconds)... | Pure polling implementation cost note; no HIGH-risk signal. | 80 | The required state and polling command remain sufficient. |
| 7 | develop-dynamic-runtime-start-container | internal/content/atoms/develop-dynamic-runtime-start-container.md | L33-L35 | `startMillis` (time from spawn to healthy), and on failure a concrete `reason` code plus `logTail` — diagnose without a follow-up call. | Response-field implementation detail; no outside operational path named. | 70 | Useful but redundant with reason-code atom. |
| 8 | export | internal/content/atoms/export.md | L232-L233 | If multiple services share this repo (dev + stage pair), a single push deploys both. | Standalone cross-flow repo topology note; no immediate task action. | 60 | Export report already names the pushed repo/branch. |

### KEEP-AS-GUARDRAIL candidates (zero-recovery; tracked for ledger completeness)

| # | atom-id | file | lines | pre-edit (verbatim, ≤150 chars) | signal-check | rationale |
|---|---|---|---|---|---|---|
| 1 | bootstrap-discover-local | internal/content/atoms/bootstrap-discover-local.md | L20-L24 | **Key rule** — no `{name}dev` service on Zerops in local mode. The user's machine replaces the dev service. | Signal #2/#5 | Prevents local-mode agent from provisioning a container dev service. |
| 2 | bootstrap-discover-local | internal/content/atoms/bootstrap-discover-local.md | L26-L28 | **VPN required** — the user runs `zcli vpn up <projectId>` to reach managed services from their machine. Env vars are not active... | Signal #2/#5 | Prevents assuming container-style env injection works locally. |
| 3 | bootstrap-provision-local | internal/content/atoms/bootstrap-provision-local.md | L16-L18 | \| Standard \| `{name}stage` only (no dev on Zerops) \| Yes — shared with container mode \| | Signal #2/#5 | Plan-shape guardrail for local standard mode. |
| 4 | bootstrap-provision-local | internal/content/atoms/bootstrap-provision-local.md | L22-L25 | - Do NOT set `startWithoutCode` — stage waits for first deploy | Signal #1/#5 | Prevents wrong stage import properties. |
| 5 | bootstrap-provision-local | internal/content/atoms/bootstrap-provision-local.md | L27-L28 | **No SSHFS** — `zerops_mount` is unavailable in local mode. Files live on the user's machine. | Signal #1/#2/#3 | Directly prevents mounting in local mode. |
| 6 | bootstrap-provision-local | internal/content/atoms/bootstrap-provision-local.md | L36-L38 | Guide the user to start VPN: `zcli vpn up <projectId>`. Needs sudo/admin; ZCP cannot start it. | Signal #3/#5 | Tool-selection guardrail: user starts VPN, ZCP cannot. |
| 7 | bootstrap-provision-rules | internal/content/atoms/bootstrap-provision-rules.md | L49-L53 | **Why `startWithoutCode: true`** — dev and simple services need to reach RUNNING before the first deploy; otherwise they sit at READY... | Signal #2/#4 | Explains import flag choice and avoids READY_TO_DEPLOY dead-end. |
| 8 | bootstrap-runtime-classes | internal/content/atoms/bootstrap-runtime-classes.md | L15-L19 | - **Dynamic** (nodejs, go, python, bun, ruby, …) — dev setup starts with `zsc noop`; the... | Signal #2/#3 | Runtime lifecycle split selects dev-server tool vs local background task. |
| 9 | bootstrap-verify | internal/content/atoms/bootstrap-verify.md | L11-L12 | Bootstrap is infra-only: no code, no deploy, no HTTP probe. Close must confirm the **platform layer** is healthy before develop starts. | Signal #2/#5 | Prevents develop-phase actions during bootstrap close. |
| 10 | bootstrap-verify | internal/content/atoms/bootstrap-verify.md | L29-L32 | Do **not** run `zerops_verify` here — that tool probes the app layer (HTTP reachability, `/status` endpoints) which... | Signal #1/#3/#5 | Explicit tool-selection guardrail. |
| 11 | develop-change-drives-deploy | internal/content/atoms/develop-change-drives-deploy.md | L15-L19 | - Dev-mode dynamic-runtime container: code-only changes pick up via `zerops_dev_server action=restart`; `zerops.yaml` changes need | Signal #3 | Correctly routes code-only vs config changes. |
| 12 | develop-checklist-dev-mode | internal/content/atoms/develop-checklist-dev-mode.md | L13-L16 | - Dev setup block in `zerops.yaml`: `start: zsc noop --silent`, **no** `healthCheck`. The platform keeps... | Signal #1/#2 | Prevents writing prod-style dev setup. |
| 13 | develop-checklist-dev-mode | internal/content/atoms/develop-checklist-dev-mode.md | L17-L19 | - Stage setup block (if a dev+stage pair exists): real `start:` command **plus** a `healthCheck`. Stage auto-starts... | Signal #2 | Cross-env contrast prevents stage dev-server reflex. |
| 14 | develop-checklist-simple-mode | internal/content/atoms/develop-checklist-simple-mode.md | L12-L14 | - The entry in `zerops.yaml` must have a real `start:` command **and** a `healthCheck` — simple services auto-start... | Signal #2/#5 | Prevents dev-mode lifecycle assumptions in simple mode. |
| 15 | develop-close-manual | internal/content/atoms/develop-close-manual.md | L12-L16 | **ZCP stays out of the deploy loop on manual strategy.** The user declared they orchestrate deploys themselves... Don't suggest `zerops_deploy`... | Signal #1/#3/#5 | Explicitly prevents tool-driven deploy at manual close. |
| 16 | develop-close-push-dev-dev | internal/content/atoms/develop-close-push-dev-dev.md | L25-L29 | Each deploy gives a new container with no dev server — check `action=status` first; if `running: false`, call `action=start`. | Signal #4 | Recovery/next-action guardrail after deploy. |
| 17 | develop-close-push-dev-local | internal/content/atoms/develop-close-push-dev-local.md | L14 | Local mode builds from your committed tree — no SSHFS, no dev container. | Signal #2 | Calibration anchor; prevents container reflex in local close. |
| 18 | develop-close-push-dev-local | internal/content/atoms/develop-close-push-dev-local.md | L22-L23 | For local+standard, `{hostname}` is the stage service — there is no separate dev container to cross-deploy from, so a single deploy... | Signal #2/#3 | Calibration anchor; prevents sourceService/cross-deploy misuse. |
| 19 | develop-close-push-dev-standard | internal/content/atoms/develop-close-push-dev-standard.md | L26-L32 | Cross-deploy packages the dev tree into stage with no second build; stage has a real `run.start` + `healthCheck`, so the... | Signal #2/#3/#4 | Prevents starting dev server on stage and gives status/start alternative. |
| 20 | develop-close-push-git-local | internal/content/atoms/develop-close-push-git-local.md | L13-L15 | The dev surface is your working directory; committed code pushes out via your own git credentials. ZCP invokes git — no `GIT_TOKEN`... | Signal #2/#5 | Prevents project-token/container credential reflex. |
| 21 | develop-close-push-git-local | internal/content/atoms/develop-close-push-git-local.md | L23-L32 | The tool's pre-flight refuses without committed code AND without an origin. If push-git isn't configured yet (missing trigger, no origin),... | Signal #4 | Recovery path to strategy setup flow. |
| 22 | develop-deploy-files-self-deploy | internal/content/atoms/develop-deploy-files-self-deploy.md | L10-L25 | Any service self-deploying (`sourceService == targetService` — the default when sourceService is omitted; typical pattern for dev services and simple... | Signal #2/#5 | Critical deployFiles guardrail; wrong reflex destroys target source. |
| 23 | develop-deploy-files-self-deploy | internal/content/atoms/develop-deploy-files-self-deploy.md | L31-L37 | Cross-deploy (`sourceService != targetService`, or `strategy=git-push`) ships build output to a **different** service — source... | Signal #2 | Necessary contrast to avoid applying self-deploy rule to cross-deploy. |
| 24 | develop-deploy-modes | internal/content/atoms/develop-deploy-modes.md | L10-L19 | `zerops_deploy` has two classes determined by source vs target: | Signal #2/#3 | Tool-selection mental model; borderline but default KEEP. |
| 25 | develop-deploy-modes | internal/content/atoms/develop-deploy-modes.md | L29-L39 | `deployFiles` is evaluated against the **build container's filesystem after `buildCommands` runs** — NOT your editor's working tree. | Signal #2/#5 | Prevents local path-existence/stat-check reflex. |
| 26 | develop-dev-server-reason-codes | internal/content/atoms/develop-dev-server-reason-codes.md | L20 | \| `spawn_timeout` \| The remote shell did not detach; stdio handle still owned by child. \| You likely hand-rolled `ssh ... "cmd &"`... | Signal #4 | Names recovery path away from SSH background anti-pattern. |
| 27 | develop-dev-server-reason-codes | internal/content/atoms/develop-dev-server-reason-codes.md | L22 | \| `health_probe_http_<code>` \| Server runs but returned `<code>` (e.g. 500, 404). \| Do NOT restart — it does... | Signal #1/#4/#5 | Prevents restart loop for app bugs. |
| 28 | develop-dev-server-triage | internal/content/atoms/develop-dev-server-triage.md | L33-L38 | # container env | Signal #2/#3 | Explicit env split for status check; prevents wrong tool in local/container. |
| 29 | develop-dev-server-triage | internal/content/atoms/develop-dev-server-triage.md | L46-L50 | - `running: true` with `healthStatus: 5xx` → server runs but is broken; read logs and response body; do NOT restart... | Signal #1/#4/#5 | Prevents restart loop and routes to code fix. |
| 30 | develop-dev-server-triage | internal/content/atoms/develop-dev-server-triage.md | L57-L63 | # container env | Signal #2/#3 | Explicit env split for start action. |
| 31 | develop-dev-server-triage | internal/content/atoms/develop-dev-server-triage.md | L65-L66 | After every redeploy the dev process is gone — re-run Step 2 before `zerops_verify`. | Signal #4 | Recovery/sequence guardrail after redeploy. |
| 32 | develop-dynamic-runtime-start-container | internal/content/atoms/develop-dynamic-runtime-start-container.md | L37-L42 | **After every redeploy, re-run `action=start` before `zerops_verify`** — the rebuild drops the dev process... | Signal #3/#4 | Prevents verify-before-start and SSH background anti-pattern. |
| 33 | develop-dynamic-runtime-start-local | internal/content/atoms/develop-dynamic-runtime-start-local.md | L13-L16 | In local env the dev server runs **on your machine**, not on a Zerops container. ZCP does not spawn local processes... | Signal #2/#3 | Local/container tool-selection guardrail. |
| 34 | develop-dynamic-runtime-start-local | internal/content/atoms/develop-dynamic-runtime-start-local.md | L49-L58 | **Managed-service env vars** (DATABASE_URL, REDIS_URL, …) come from Zerops. Generate `.env` in your working directory... | Signal #2/#3 | Prevents expecting injected container env vars locally. |
| 35 | develop-dynamic-runtime-start-local | internal/content/atoms/develop-dynamic-runtime-start-local.md | L60-L62 | **Do NOT use `zerops_dev_server`** — that tool is container-only (it SSHes into Zerops dev containers). In local env... | Signal #1/#3/#5 | Calibration anchor; explicit unavailable-tool guardrail. |
| 36 | develop-env-var-channels | internal/content/atoms/develop-env-var-channels.md | L17-L18 | \| `run.envVariables` \| Edit `zerops.yaml`, commit, deploy \| Full redeploy. `zerops_manage action="reload"` does NOT pick... | Signal #1/#3/#5 | Prevents wrong reload tool for YAML env changes. |
| 37 | develop-first-deploy-asset-pipeline-container | internal/content/atoms/develop-first-deploy-asset-pipeline-container.md | L14-L18 | Recipes whose backend is `php-nginx` / `php-apache` and whose frontend runs through a build pipeline... intentionally OMIT `npm run build`... | Signal #2/#5 | Explains why dev build command must not be "fixed." |
| 38 | develop-first-deploy-asset-pipeline-container | internal/content/atoms/develop-first-deploy-asset-pipeline-container.md | L48-L50 | **Do NOT add `npm run build` to dev `buildCommands`.** Every `zcli push` would then rebuild assets (~20–30 s... | Signal #1/#5 | Direct tool/action negation. |
| 39 | develop-first-deploy-asset-pipeline-local | internal/content/atoms/develop-first-deploy-asset-pipeline-local.md | L14-L17 | Recipes that ship a frontend asset pipeline (Laravel+Vite, Symfony+Encore, …) intentionally OMIT `npm run build` from the `dev`... | Signal #2/#5 | Local asset pipeline guardrail. |
| 40 | develop-first-deploy-asset-pipeline-local | internal/content/atoms/develop-first-deploy-asset-pipeline-local.md | L49-L51 | **Do NOT add `npm run build` to dev `buildCommands`.** Every `zerops_deploy` would then rebuild assets on Zerops... | Signal #1/#5 | Direct tool/action negation. |
| 41 | develop-first-deploy-execute | internal/content/atoms/develop-first-deploy-execute.md | L12-L16 | The Zerops container is empty until the deploy call lands, so probing its subdomain or (in container env) SSHing into it first will... | Signal #2/#5 | Prevents pre-deploy probe/SSH reflex. |
| 42 | develop-first-deploy-execute | internal/content/atoms/develop-first-deploy-execute.md | L22-L24 | On first-deploy success the response carries `subdomainAccessEnabled: true` and a `subdomainUrl` — no manual `zerops_subdomain`... | Signal #1/#3/#5 | Prevents manual subdomain tool call. |
| 43 | develop-first-deploy-intro | internal/content/atoms/develop-first-deploy-intro.md | L24-L27 | **Run `zerops_deploy targetService=<hostname>`** with NO `strategy` argument. Every first deploy uses the default push path;... | Signal #1/#3/#5 | Prevents first-deploy git-push reflex. |
| 44 | develop-first-deploy-intro | internal/content/atoms/develop-first-deploy-intro.md | L32-L33 | Don't skip to edits before the first deploy lands — SSHFS mounts can be empty and HTTP probes return errors before any code is delivered. | Signal #1/#5 | Prevents editing/probing before first deploy. |
| 45 | develop-first-deploy-promote-stage | internal/content/atoms/develop-first-deploy-promote-stage.md | L23-L26 | No second build — cross-deploy packages the dev tree straight into stage. Auto-close requires BOTH halves verified; see | Signal #2 | Prevents redundant rebuild and skipping stage verify. |
| 46 | develop-first-deploy-scaffold-yaml | internal/content/atoms/develop-first-deploy-scaffold-yaml.md | L41-L45 | - `dev` mode: `deployFiles: [.]`, build runs on SSHFS, `run.start` wakes the container — no stage pair to... | Signal #2/#5 | Mode-aware deployFiles/stage guardrail. |
| 47 | develop-http-diagnostic | internal/content/atoms/develop-http-diagnostic.md | L11-L13 | When the app returns 500 / 502 / empty body, follow this order. Stop at whichever step resolves the error — do **not**... | Signal #1/#3/#5 | Prevents defaulting to SSH curl. |
| 48 | develop-http-diagnostic | internal/content/atoms/develop-http-diagnostic.md | L18-L25 | **Subdomain URL** — format is `https://{hostname}-${zeropsSubdomainHost}.prg1.zerops.app/` for static... | Signal #5 | Prevents guessing wrong URL shape. |
| 49 | develop-http-diagnostic | internal/content/atoms/develop-http-diagnostic.md | L32-L35 | **Last resort: SSH + curl localhost** — only when the above miss something container-local... | Signal #3/#5 | Tool-order guardrail. |
| 50 | develop-implicit-webserver | internal/content/atoms/develop-implicit-webserver.md | L11-L14 | Apache or nginx is already running inside the container and auto-serves whatever's on disk. **Do not SSH in to start a server**... | Signal #1/#3/#5 | Prevents manual server start for implicit-webserver. |
| 51 | develop-implicit-webserver | internal/content/atoms/develop-implicit-webserver.md | L16-L22 | **`zerops.yaml` shape differences vs. dynamic runtimes:** | Signal #2/#5 | Prevents dynamic-runtime YAML reflex. |
| 52 | develop-local-workflow | internal/content/atoms/develop-local-workflow.md | L18-L20 | Test locally against the VPN-exposed managed services, then deploy when ready via `zerops_deploy`. There is no SSHFS mount in local mode... | Signal #2/#5 | Local-mode mental-model guardrail. |
| 53 | develop-platform-rules-common | internal/content/atoms/develop-platform-rules-common.md | L13-L14 | - **Deploy = new container.** Local files in the running container are lost; only content covered by `deployFiles` survives across deploys. | Signal #2/#5 | Prevents relying on undeployed container mutations. |
| 54 | develop-platform-rules-common | internal/content/atoms/develop-platform-rules-common.md | L18-L21 | - **Build ≠ run container.** Runtime packages → `run.prepareCommands`; build-only packages → `build.prepareCommands`. | Signal #2 | Prevents installing runtime deps in build-only layer. |
| 55 | develop-platform-rules-common | internal/content/atoms/develop-platform-rules-common.md | L22-L25 | - `envVariables` in `zerops.yaml` are declarative — **not live** until a deploy. `printenv` before deploy returns nothing for... | Signal #2/#5 | Prevents expecting undeployed YAML env vars at runtime. |
| 56 | develop-platform-rules-container | internal/content/atoms/develop-platform-rules-container.md | L16-L19 | - **Mount caveats.** Deploy rebuilds the container from mount; no transfer at deploy time. Never `ssh <hostname> cat/ls/tail... | Signal #1/#3/#5 | Prevents wrong file access method in container env. |
| 57 | develop-platform-rules-container | internal/content/atoms/develop-platform-rules-container.md | L20-L25 | - **Long-running dev processes → `zerops_dev_server`.** Don't hand-roll `ssh <hostname> "cmd &"` — backgrounded SSH holds... | Signal #1/#3/#5 | Prevents background SSH anti-pattern. |
| 58 | develop-push-dev-deploy-container | internal/content/atoms/develop-push-dev-deploy-container.md | L15-L19 | The dev container uses SSH push — `zerops_deploy` uploads the working tree from `/var/www/{hostname}/` straight into the service... | Signal #2/#3 | Container push-dev mechanism prevents git-remote credential reflex. |
| 59 | develop-push-dev-deploy-local | internal/content/atoms/develop-push-dev-deploy-local.md | L13-L17 | `zerops_deploy` runs `zcli push` from your working directory into the linked Zerops stage. Requires `zerops.yaml` at the... | Signal #2/#3 | Calibration-adjacent; local deploy has no sourceService/cross-deploy. |
| 60 | strategy-push-git-intro | internal/content/atoms/strategy-push-git-intro.md | L24-L27 | > **You can't pick "neither" as a push-git trigger.** A push-git service without a downstream trigger is functionally `manual`... | Signal #3/#5 | Prevents configuring push-git with no build trigger when manual is intended. |

## Notes / cross-cutting observations

- Local/container contrast is the dominant Axis K surface. Most such rows are KEEP because they prevent likely cross-flow reflexes around SSHFS, dev containers, local background tasks, and `zerops_dev_server`.
- Tool-selection negations are common and generally load-bearing: `zerops_verify` during bootstrap, `zerops_dev_server` in local env, manual `zerops_subdomain`, hand-rolled SSH background processes, and first-deploy `strategy=git-push`.
- The clearest DROP set is implementation trivia: on-disk meta projection, timing comparisons, redundant first-build explanation, and response-field implementation phrasing.
- Several REPHRASE candidates preserve guardrails but should become shorter operational rules rather than mechanism explanations.

## Phase 2 work-unit derivation

The DROP candidates feed axis-k-drops-ledger.md directly. The
REPHRASE candidates each become a Phase 2 work-unit. The
KEEP-AS-GUARDRAIL rows are ledger-only (no edit). The Phase 2
tracker rows are in 1:1 with this artifact's DROP + REPHRASE
candidates.

## Methodology footnotes

- Cited corpus examples per the §3 Axis K calibration anchors.
- Per memory rule feedback_codex_verify_specific_claims.md:
  every cited file:line will be grep-verified by the executor
  before acting.
---
