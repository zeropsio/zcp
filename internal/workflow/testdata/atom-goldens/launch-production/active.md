---
id: launch-production/active
atomIds: [launch-delete-key, launch-intro, launch-pipeline-configure-dashboard, launch-classify-prompt, launch-post-checklist, launch-scope-prompt, launch-classify-platform-envs, launch-ha-assessment, launch-mutation-key-required, launch-pipeline-configuring, launch-existing-project-conflict, launch-pipeline-configured, launch-pipeline-skipped, launch-source-control-required, launch-status-recovery, launch-write-prod-setup]
description: "Launch-production workflow mid-flow on a source project — bundle composed, awaiting token acquisition (delegated mint or launch key) for the mutation pipeline."
---
### Close the launch window (confirm-production)

The production project is live, but the launch window STAYS OPEN until production is verified fully functional — keep it open while wiring delivery, shipping the first release, and fixing anything that surfaces. The staged `ZCP_LAUNCH_TOKEN` secret on the source push service is the single working copy of the launch token: prod-ops, pipeline re-checks and reset read it server-side, so you never re-send the value.

When everything works end-to-end:

1. Ask the user to confirm production is fully functional (first release live on the production runtimes + smoke check passed).
2. Call `zerops_workflow action="confirm-production" productionProjectName="<name>" confirmFunctional=true`. This **deletes the staged secret** — the window closes physically: launch-window calls have nothing left to read.
3. Surface the response's `tokenLifecycle` note to the user: the integration token itself stays valid (GitHub Actions keeps its repo-secret copy). Recommended hygiene — regenerate the token in [Settings → Access Tokens Management](https://app.zerops.io/settings/token-management); regeneration keeps all settings and immediately invalidates the old value everywhere (including every copy this conversation ever saw). The user then updates the GitHub repo secret with the new value in their own terminal — the fresh value never enters the conversation.

**Tearing the project down instead (test launch, wrong name, abandoned)?** Do NOT reach for zcli or the raw token value — `zerops_workflow action="reset" workflow="launch-production" productionProjectName="<name>"` deletes the production project (the staged token is read server-side), the staged secret, and the launch state, behind a diagnose-then-ack gate. This works only while the launch window is open: after `confirm-production` the staged token is gone and the project can be deleted only from the dashboard.

---

### Launch production — overview

You are launching the source project to a separate Zerops production project. ZCP prepares the bundle, source-control changes, and verification steps; the token that creates the production project AND drives the production pipeline is acquired once for the whole lifecycle — either ZCP mints it itself from a one-time platform delegation on your explicit go-ahead, or you (the user) generate one manually as the fallback — and the launch window closes explicitly once production is verified working.

The launch creates infrastructure only: production runtimes come up ACTIVE with EMPTY containers, and the application arrives with the FIRST RELEASE TAG through the production pipeline — the same mechanism every later release uses. Nothing is platform-cloned at import time, so private repos work the same as public ones.

The launch narrows through these statuses (custom domains are post-launch dashboard work, not a launch input):

| Status | Means |
|---|---|
| `scope-prompt` | ZCP needs: production project name, region, scaling overrides. |
| `source-control-required` | Scope is complete, but a promoted runtime fails the git gate — wire git-push-setup / push HEAD / align the remote, then re-call. |
| `classify-prompt` | Project envs need bucketing (infrastructure / auto-secret / external-secret / plain-config / exclude). |
| `ready-to-launch` | Bundle composed, source-control changes pushed, schema clean, blockers cleared. Awaiting token acquisition — delegated mint on confirmation, or the manual launch token as fallback. |
| `launching` | Launch token in use; ZCP is creating the project + importing services. No build runs at import time. |
| `failed` | A mutation step failed; `blockers[]` describes recovery. |
| `launched` | Infrastructure live; the APPLICATION is not running yet. Follow the `firstRelease` block: wire the production delivery, push the first release tag, watch it land, verify the app works, THEN close the window via `action="confirm-production"`. External secrets + custom domain are set in Zerops UI. |

**Single-token lifecycle.** Token acquisition happens once, at the `action="start"` re-call that advances `ready-to-launch` into the mutation (there is no separate publish action). When a platform delegation is available, ZCP mints the token itself on your explicit `confirmLaunch=true` — no token value ever crosses the conversation. Otherwise the manual walkthrough applies: the value crosses the conversation exactly once as `launchKey`; it never lands in state, logs, or the audit trail. Either way the mutation immediately stages the resolved token as the `ZCP_LAUNCH_TOKEN` service secret on the source push service, and that staged secret becomes the single working copy: pipeline re-checks, prod-ops, reset and the close all read it server-side, and the GitHub repo-secret wiring copies it secret-to-secret (neither read passes through the conversation). Do NOT re-send a value on later calls — re-pass `launchKey` only if the staged secret is gone and no delegation is available.

**Launch-window lifecycle.** The window stays OPEN through delivery wiring, the first releases, and any recovery — CI/CD problems remain fixable, and a botched project can be deleted and relaunched (`action="reset"`), all through the staged secret without re-acquiring the token. Once the user confirms production is fully functional, close the window: `action="confirm-production" confirmFunctional=true` deletes the staged secret, leaving launch-window calls nothing to read. The token itself stays valid — regeneration, not expiry, is what invalidates it — and GitHub Actions keeps its repo-secret copy. Recommended hygiene at close: regenerate the token in the Zerops dashboard — regeneration keeps all settings and immediately invalidates the old value everywhere, including every copy this conversation ever saw — then the user updates the repo secret with the new value in their own terminal.

---

### Configure CD pipeline in Zerops dashboard

The production runtime has no CD pipeline yet — and the pipeline is what delivers EVERY production build, including the FIRST one (the launched runtimes are empty until the first release tag). Configure it once via dashboard. (ZCP cannot do this through the launch-window key; see `plans/backlog/launch-pipeline-close-loop-oauth.md` for the Path A future.)

For each runtime listed in the `pipeline-not-configured-*` blockers:

1. Open the **deep-link** from the blocker (`https://app.zerops.io/service-stack/<svcID>/deploy`).
2. Click **Connect to GitHub** (or GitLab). Authorize Zerops if asked — uses your existing org-level grant, no extra setup.
3. Select the source repository listed in the blocker's `recommendation.repositoryFullName`.
4. Set the trigger:
   - **Event type:** `Tag`
   - **Tag regex:** the value from `recommendation.tagRegex` (default `^v\d+\.\d+\.\d+$` per Zerops production-checklist).
   - **Zerops YAML setup:** the value from `recommendation.zeropsYamlSetup` (the setup block the launch bundle references — typically `prod`, but follow the recommendation field, not a guess).
5. Save.

Repeat for each runtime in the blockers list. When done, re-call `workflow="launch-production"` — no need to re-send the launch token, ZCP reads the staged secret server-side; it reads the live integration status and clears the blockers from the response.

To deploy after setup: `git tag v1.0.0 && git push --tags` (matching your tag regex).

---

### Launch classify — bucket source envs before production publish

You are at `status="classify-prompt"`. The launch composer needs every source `project.envVariables` entry classified into one of five buckets — `infrastructure`, `auto-secret`, `external-secret`, `plain-config`, `exclude` — before it can emit the production import bundle.

**Call shape — `action="start"` always.** Launch-production is stateless multi-call narrowing: every advance is another `zerops_workflow action="start" workflow="launch-production"` with the FULL accumulated `inputs` block from the prior response plus `envClassifications`. There is NO classify action. There is NO `action="complete"` step (that's bootstrap). Re-call `action="start"` with the accumulated inputs and the new classification map:

```
zerops_workflow action="start" workflow="launch-production" \
  productionProjectName="<from inputs>" \
  targetService="<from inputs>" \
  region="<from inputs>" \
  envClassifications={"APP_KEY":"auto-secret","DB_HOST":"infrastructure","STRIPE_KEY":"external-secret"}
```

If you skip an env, the next response re-prompts with the remaining unclassified keys. Extra keys that don't match any source env are informational — the composer ignores them.

## The five buckets

| Bucket | Detection signal | Emit in production project |
|---|---|---|
| `infrastructure` | Value (or component) resolves from a managed-service reference (`${db_*}`, `${redis_*}`, `${mongo_*}`, plus per-service prefixes). Includes app-built compound URLs assembled at runtime from `${...}` components. | DROP from `project.envVariables`. The reference still lives in `zerops.yaml`'s `run.envVariables`; the re-imported managed service emits a fresh value at boot. |
| `auto-secret` | Source code uses the var as a local encryption / signing key (framework owns the call; rarely visible in app code). | `<@generateRandomString(<32>)>`. Each launch gets a fresh secret. |
| `external-secret` | Source calls a third-party SDK with the var (Stripe, OpenAI, Mailgun, GitHub, …). Includes aliased imports + webhook verification secrets. | Comment + `<@pickRandom(["REPLACE_ME"])>`. New project's owner pastes the real key into the dashboard before deploy. |
| `plain-config` | Source uses the var as literal runtime config (LOG_LEVEL, NODE_ENV, FEATURE_FLAGS, …). | Literal value verbatim. |
| `exclude` | The env is STALE — nothing in the source tree or `zerops.yaml` references it anymore (leftover from a removed feature or an earlier framework). Verify with a grep over the source plus the discover response before excluding. | DROPPED entirely — no value, no reference. A warning fires if `zerops.yaml`'s `run.envVariables` still references it. |

`zerops_workflow` returns each unclassified env's key but NOT its value — fetch values via `zerops_discover service="{targetHostname}" includeEnvs=true includeEnvValues=true`, then grep them against the mounted source tree (when accessible) before bucketing.

Every row carries `suggestedBucket` + `rationale` computed server-side from the env key NAME alone (never the value, per the no-leak invariant). Treat the suggestion as a starting point — the four-bucket detection table below remains authoritative when you override. Common reasons to override: a credential-pattern match (`*_KEY`, `*_TOKEN`) that's actually plain-config in your app, or a plain-config name (`DB_HOST`) whose value resolves to a managed-service reference (`${db_*}`) and should bucket `infrastructure`.

## Worked examples per bucket

### Infrastructure

```
DB_HOST=${db_hostname}
REDIS_URL=${redis_connectionString}
```

Both resolve from managed-service references — bucket `infrastructure`. The new prod project's `db` and `redis` services emit fresh values at boot. Compound case: `DATABASE_URL` assembled in app code from `${DB_USER}`, `${DB_PASSWORD}` — the COMPONENT envs are `infrastructure`. If `DATABASE_URL` is itself a project env resolving to managed refs, bucket it `infrastructure`; if assembled manually with literal credentials, bucket `external-secret`.

### Auto-secret

```
APP_KEY=existing-key    # Laravel — encrypts cookies/session
SECRET_KEY=django…      # Django — signs sessions, CSRF
JWT_SECRET=long-bytes   # Node — signs tokens
```

Framework convention drives detection: Laravel `APP_KEY`, Django `SECRET_KEY`, Rails `SECRET_KEY_BASE`, Express `SESSION_SECRET` / `JWT_SECRET`. **Stability warning**: if persisted state (encrypted cookies, signed tokens, encrypted DB columns) depends on the existing key, regenerating breaks it. Ask the user before bucketing `auto-secret` for a non-greenfield prod migration — the alternative is `plain-config` (carry the existing key forward).

### External secret

```
STRIPE_SECRET=sk_live_xyz…
OPENAI_API_KEY=sk-proj-…
MAILGUN_API_KEY=key-…
GITHUB_TOKEN=ghp_…
```

Source contains the SDK call (`stripe(env.STRIPE_SECRET)`, etc.). Aliased imports still count: `from stripe import Stripe as PaymentProvider; PaymentProvider(env.SECRET)`. Webhook-verification secrets (`stripe.webhooks.constructEvent`) also bucket `external-secret`. Empty / sentinel values (`STRIPE_SECRET=`, `disabled`, `sk_test_*`, `test_xxx`, `none`) are review-required — `REPLACE_ME` breaks startup if the app validates on init. Bucket `external-secret` only if a real prod value is needed; otherwise `plain-config` keeps the existing.

### Plain config

```
LOG_LEVEL=info
NODE_ENV=production
FEATURE_FLAGS=experiments_v2,beta_signups
APP_URL=${zeropsSubdomainHost}
```

Literal runtime config. Privacy flag: real emails (`MAIL_FROM_ADDRESS=ops@acme.com`), customer names, internal domain names, sender identities are technically `plain-config` but emitting them into a fresh prod project leaks PII. Surface to the user before bucketing — they may want to redact or rotate.

## Platform-injected tokens

`GIT_TOKEN` and `ZCP_API_KEY` appear in source-project envs but are ZCP-side infrastructure (re-injected by the launch handler for the new project's git push + MCP session). Bucket both as `infrastructure` — they will be DROPPED from `project.envVariables` and the prod project re-receives them via its own launch flow. Do NOT bucket them as `external-secret` (`REPLACE_ME` would break the prod project's first git push).

## Common mis-classification traps

- **APP_KEY across a stateful app** (M3): auto-generating breaks existing encrypted columns / session cookies. If state continuity matters, bucket `plain-config` and carry the existing value forward.
- **`STRIPE_SECRET=` empty in staging** (M4): `REPLACE_ME` placeholder breaks startup if the app validates on init. Bucket `external-secret` only if a real prod value is needed; otherwise `plain-config`.
- **Compound `DATABASE_URL` with literal credentials** (M2): looks like infrastructure but it's a hand-rolled URL. Bucket `external-secret`.
- **`MAIL_FROM_ADDRESS=ops@acme.com`** (M5): literal config, but the email is real. Flag privacy; consider placeholder before launch.
- **Test-fixture values** (`TEST_API_KEY=test_xxx` consumed only by tests, M6): bucket `plain-config` only if read at runtime; if every reference is inside a test file, bucket `exclude` — the production project never receives it.
- **Stale env after a refactor** (M8): an env that once served the app (e.g. an `APP_KEY` whose consumer was removed) still sits in the source project. Don't force it into a semantic bucket — grep the source tree for the key; zero runtime references means `exclude`.
- **Non-default managed-service prefixes** (M7): a custom Mongo/Postgres/MySQL may emit envs as `${mongo_connectionString}` / `${postgres_*}` / `${mysql_*}` instead of `${db_*}`. Inspect the discover response's `services[].envs` array — false-negative `plain-config` here emits literal hostname/password into the prod project.

If a row is genuinely ambiguous, the safest default is `plain-config` (carries the existing value) plus a follow-up review with the user — wrong-direction errors there are fixable post-launch without breaking deploy.

---

### Launch complete — remaining steps to a running application

ZCP has created the production project and imported the services. The runtimes are ACTIVE with EMPTY containers — the application is NOT running until the first release deploys through the production pipeline. Work the steps in order; the `firstRelease` block on the launched response carries the family-specific commands.

**Production L7 exposure baseline — production has NO HTTP access enabled by default.**

`appdev_zeropsSubdomain` env vars are populated on every HTTP-eligible runtime (platform always emits them), but the launch composer strips `enableSubdomainAccess` from the production import YAML per P-PROD-2 — so no L7 backend is registered. `curl` to that URL returns 502 until you either attach a custom domain OR explicitly enable the zerops.app subdomain in the prod project's dashboard.

1. **Set external secrets FIRST** — open the production project, navigate to each service that needs Stripe/OpenAI/SMTP/etc. values, and set them under Env Variables → Secret. ZCP listed the keys needed in the prior response. Do this before the first release so the application boots with real values.
2. **Wire the production delivery** — per the `firstRelease.deliveryFamily`:
   - **actions** — run the `prodCd.secret.command`: it reads the staged `ZCP_LAUNCH_TOKEN` secret and sets it as the `ZEROPS_TOKEN_PROD` GitHub repo secret (secret-to-secret — no value is pasted and nobody is re-asked for the token). Then write `prodCd.workflowFile` at `.github/workflows/zerops-prod.yml`, commit + push.
   - *Hardening (actions, recommend to the user):* a plain repo secret is effectively readable by ANY collaborator with write access — a workflow edit can exfiltrate it. Where the GitHub plan allows, move `ZEROPS_TOKEN_PROD` to a `production` **environment** secret with required reviewers and pin the deploy job with `environment: production` (environments on private repos need Pro/Team; required reviewers on private repos need Enterprise; public repos get both on any plan).
   - **webhook** — configure the dashboard TAG integration on each production runtime per the `pipeline-not-configured-*` blockers (deep-link + recommended values).
   - **none** — ask the user which of the two to wire; never pick silently.
3. **First release** — `zerops_workflow action="release" service="<source-runtime-hostname>"` (or `git tag v1.0.0 && git push --tags`, matching the tag regex, default `^v\d+\.\d+\.\d+$`). This is the FIRST production build — the pipeline builds your pushed HEAD and deploys it into the empty runtimes. (`service` is required — it is the source push hostname the firstRelease block names.)
4. **Watch it land** — `action="prod-ops"` shows the production services as the release deploys (the launch-window token is read from the staged secret; no launchKey re-send); build logs are in the GitHub Actions run (actions) or the prod project's dashboard (webhook).
5. **Establish HTTP exposure (MANDATORY before smoke test)** — pick one:
   - **Custom domain (recommended for prod)** — Project → Public Access → HTTP Routing → Add Domain in the prod project's dashboard. The dashboard shows the DNS records to create (TXT verification + A/AAAA); add them at the registrar, click Verify. Domain attachment is operator-owned — ZCP does not touch production routing.
   - **zerops.app subdomain (explicit opt-in)** — Project → Service → Public Access → Enable Subdomain in the prod project's dashboard. ZCP cannot do this from the source-project MCP session because `zerops_subdomain` is bound to the current project; explicit enable requires either a new MCP session against the prod project (with a project-scoped `ZCP_API_KEY` for that project) or the dashboard click-through.
   - **No public access** — leave the runtime reachable only via internal hostname for backend / worker services. Skip step 6.
6. **Smoke test** — hit the URL from step 5 with a known request shape; check response and logs in dashboard.
7. **Close the launch window** — once the user confirms production is fully functional, call `zerops_workflow action="confirm-production" productionProjectName="<name>" confirmFunctional=true`. The staged `ZCP_LAUNCH_TOKEN` secret is deleted (launch-window calls have nothing left to read); the response carries the token-hygiene note — the token itself stays valid for GitHub Actions, and regenerating it in the dashboard (then refreshing the repo secret in the user's own terminal) invalidates every copy this conversation ever saw.

After step 7, the launch is complete. For ongoing prod iteration: generate a separate project-scoped `ZCP_API_KEY` (Custom access per project, this one project, Full access) and configure a fresh ZCP MCP session against the production project. Every later release ships the same way as step 3 — tag, pipeline builds, production updates.

---

### Launch scope — collect production target details

**This workflow is stateless multi-call narrowing.** Every response's `inputs` block is the running accumulator: pass all previously-accepted parameters forward on every next `action="start"` call. `action="complete"` is reserved for bootstrap and returns `BOOTSTRAP_NOT_ACTIVE` here.

#### First — identify the launch path

launch-production has two mutation paths in one workflow. Pick which one matches the user's intent BEFORE collecting scope params; the choice surfaces in `inputs` and dispatches the right mutation at the `ready-to-launch` step.

| User intent signal | Path | Required token params |
|---|---|---|
| "Create new prod project", "launch to fresh project", or no existing project mentioned | **NEW-PROJECT** | Token acquisition surfaces at the `ready-to-launch` step: `delegatedLaunch.available` on that response says whether ZCP can mint the launch token itself from a one-time platform delegation on `confirmLaunch=true` (no value crosses the conversation); otherwise `launchKey` (integration token with project-creation permission, passed ONCE) is the fallback. Either way the rest of the launch window reads the staged secret. |
| "I have existing prod project", explicit project ID/token supplied, "deploy into project X" | **EXISTING-PROJECT** | `existingProjectId` + `existingProdToken` (project-scoped token from target project's dashboard) |

If the user explicitly hands you an existing project ID OR a project-scoped token, pass `existingProjectId` + `existingProdToken` on this first `action="start"` call alongside the scope params below — both will land in the `inputs` accumulator and the workflow will skip the `launchKey` prompt at `ready-to-launch`. Otherwise default to NEW-PROJECT and let the workflow ask for `launchKey` later.

#### Then — apply suggestions from `sourceContext`

- **`productionProjectName`** — `sourceContext.suggestedTargetName` (`<source>-dev` / `<source>-stage` → `<source>-prod`, else `<source>-prod` appended). Confirm name with user; don't silently rename.
- **`targetService`** — `sourceContext.promotionHeadline` when single. For standard-mode pairs the headline is the stage hostname (validated last-known-good); `devHostname` field discloses the iteration half. Either half is accepted as input — the handler normalizes internally. When the canonical post-normalization differs, `sourceContext.targetServiceCanonical` echoes the form the bundle composer will use.
- **`promotables`** — multi-runtime promotion. Pass an array of `{hostname, prodHostname?, prodSetupNameOverride?}` entries when more than one runtime is being promoted into the same prod project (monorepo with app + worker, or separate-repos with multiple services). Empty/absent → falls back to single-runtime from `targetService`. Production hostname derivation: `appdev`/`appstage` → `app`, `workerstage` → `worker`. Pass `prodHostname` to override.
- **`region`** — optional, default `eu-central`. Custom domains are attached by the operator in the Zerops dashboard after launch (Project → Public Access → HTTP Routing) — they are not a launch input.
- **`managedDeps`** — `sourceContext.managedDeps` lists the source's managed services (databases, KV stores); all are bundled by default. Per-dep decisions travel as `managedDeps={"<hostname>":"exclude"}` — the `ready-to-launch` preview marks each dep `referenced=true/false` (whether anything wires `${<host>_*}`) and recommends exclusions, so defer the decision there unless the user already named deps to drop.
- **`keepNonHA`** — optional `[]hostname` to keep at `NON_HA` (default: all managed deps go `HA`).
- **`runtimeScaling`** — optional per-runtime container counts `{"<hostname>":{"minContainers":N,"maxContainers":M}}`. Production default is a 2-container floor per runtime; an explicit `minContainers: 1` is accepted as a consented single-container run (surfaces as a warning, not a block). Ask the user about expected load before overriding either way.
- **`skipStageRecommendation`** — when the source is a dev-only / local shape, the scope response may carry a stage recommendation block (create a validated stage first). Pass `skipStageRecommendation=true` only after the user explicitly declines it.

When `sourceContext.availableRuntimes` has multiple entries, the user must pick. Use `AskUserQuestion` if your harness exposes it (structured choice UI); else surface the choice inline and wait for the user's next turn. For multi-runtime, ask the user whether to promote all or pick a subset — defensive default is "primary runtime + infra now, other runtimes as separate additive launches" unless promotables share the same source repo (monorepo).

After scope is complete, ZCP runs the source-control gate — every promoted runtime must carry `meta.GitPushState=configured` + a live remote that matches the recorded value + a clean working tree with pushed HEAD. Unresolved blockers surface as `source-control-required` with per-runtime Recovery hints chaining `git-push-setup` / `zerops_deploy strategy=git-push` / `build-integration`. Once green, ZCP advances to `classify-prompt` for env classification.

---

### Launch classify — platform envs auto-handled

The `classifications` rows in the `classify-prompt` response carry only envs that need your judgment. Two separate mechanisms handle platform / control-plane envs without asking, so you classify ONLY your app's own envs:

1. **Type=SYSTEM → dropped (by type, not by name).** Platform-injected envs — the subdomain pair (`zeropsSubdomainHost`/`String`), isolation settings (`envIsolation`/`sshIsolation`), CDN URLs, and any other server-set value — carry `Type=SYSTEM`. The classifier drops them universally because the new prod project re-emits its own equivalents at boot. This is an OPEN set: a platform SYSTEM env you've never seen drops the same way — there is no name list to maintain.

2. **ZCP control-plane credentials → infrastructure (by exact key).** A small closed allowlist of dev-side credentials (the `ZCP_*` control-plane keys, `GIT_TOKEN`, and the staged launch token) is filtered to `infrastructure`: the destination re-emits its own at init / git-push-setup, and the composed import YAML is agent-visible, so carrying the source's live value forward would leak it. This match is by **exact key only** — a stray user-named `ZCP_CUSTOM_USER_THING` is NOT absorbed; it falls through to your classification with the default bias.

You will not see either group in the row table. Everything else — your app's config + secrets — appears as a row for you to bucket (infrastructure / auto-secret / external-secret / plain-config / exclude).

---

### Assess whether the app is ready to run on multiple containers

Production defaults every promoted runtime to a 2-container floor (`minContainers: 2`) — requests round-robin across containers, each with its OWN filesystem and memory. An app that worked on one dev container can break at scale 2 in ways the build never shows. BEFORE settling `runtimeScaling`, walk this checklist against the source code — each row is a grep-able question, not a guess:

| Check | Question to answer from the source | Failure at scale ≥ 2 |
|---|---|---|
| In-memory state | Are sessions, rate-limit counters, caches, or job queues held in process memory (e.g. default express-session MemoryStore, Laravel `SESSION_DRIVER=file`)? | A request lands on container B and the session from container A doesn't exist — random logouts, lost carts. Move state to the managed db/redis dep. |
| Local-disk writes | Does code write uploads, SQLite files, or generated assets to its own disk paths? | Each container has its own disk — files exist on one container and 404 on the other. Use object storage or a shared-storage mount. |
| Migrations on boot | Do schema migrations run on every container start (init commands, ORM sync-on-boot)? | Two containers boot in parallel and race the migration — duplicate-column / lock errors. Migrations must be idempotent or run once per deploy, not per container. |
| Scheduled / queue work | Do in-process cron jobs or queue consumers run inside the web app? | Every container runs its own copy — emails sent twice, jobs double-processed. Move to a single-container worker service or use a locking scheme. |
| Realtime connections | Do WebSocket / SSE clients broadcast through in-process state? | Clients connected to container A never see events published on container B. Route pub/sub through the redis/valkey dep. |

**The decision belongs to the user, framed by load:** ask what traffic production expects. Two outcomes:

- App passes the checklist (stateless, externalized state) → keep the 2-container default; raise `maxContainers` for headroom under load.
- App fails a row and fixing it now is out of scope → a consented single container is a legitimate launch: pass `runtimeScaling={"<hostname>":{"minContainers":1,"maxContainers":1}}` — it surfaces as a warning, not a block — and record the failed row as the follow-up work before scaling up.

Don't silently pick either path. The checklist result + the user's load answer ARE the consent conversation; per-runtime counts land in the `ready-to-launch` preview (`containers` field) for the final confirm.

---

### Manual token fallback to create the production project

**This is the FALLBACK.** The primary path is a platform delegation: when one is available, ZCP mints the launch token itself on the user's explicit confirmation and no token value ever crosses the conversation — check `delegatedLaunch.available` on the `ready-to-launch` response first, and use `confirmLaunch=true` instead of any of this when it is `true`. The walkthrough below applies only when no delegation is available (never granted, already consumed, or revoked).

**Note**: this guidance applies to the **NEW-PROJECT** launch path only. If you're deploying into an existing prod project (the user supplied `existingProjectId` + `existingProdToken` at the scope-prompt step), you'll have advanced past this point — the workflow uses the project-scoped token instead and goes straight to `launching`. See the scope-prompt's path-selection table for which params trigger which path.

**Before asking for the key, walk the `bundlePreview` with the user** — this is the consent moment. Three fields demand an explicit answer when present: `setupProvenanceHint` (production's build recipe resolved from the dev setup or a legacy default — confirm which `zerops.yaml` setup production builds with, or pass `prodSetupNameOverride`), `managedDepHint` (a managed dep nothing references — exclude or wire it), and per-runtime `containers` (the production scale being paid for). Silence on any of these means the user learns about it from the invoice or the first prod build.

ZCP cannot create a NEW production project with its standing token (project-scoped, no project-creation permission), and no delegation is available to mint one itself. Walk the user through generating the launch integration token manually — ONE token for the whole lifecycle: it creates the project, covers the bring-up window, and drives the GitHub Actions pipeline. Wait for them to paste the value back before calling the workflow again:

1. Open [Settings → Access Tokens Management](https://app.zerops.io/settings/token-management).
2. Click **Create token**. Name it `zcp-launch-<production-project-name>`.
3. Under **Primary Access**, select **Custom access per project**.
4. Turn ON the **Allow creating projects** toggle that appears below — this is the gate that lets the token create the new prod project. Without it, the launch call will fail at create-project.
5. Leave **Per Project Access Customization** empty — the token only needs project-creation; it gains access to the projects it creates and needs no access to existing ones.
6. Copy the token value (shown once).
7. Paste the value back into the conversation — this is the ONLY time the value crosses it.

When the value lands, re-call the launch workflow with the SAME `action="start"` call shape and the same accumulated inputs, adding the token value as `launchKey` (there is no separate publish action — the launchKey-bearing call IS the mutation call). Do NOT invent or guess a value, and do NOT proceed without it — the key is the gate.

The mutation stages the token as the `ZCP_LAUNCH_TOKEN` service secret on the source push service; every later launch-window call (prod-ops, pipeline re-check, reset, confirm-production) reads the staged copy, so do NOT re-send the value. The token never lands in state, logs, or the audit trail. Once production is verified fully functional, the window closes via `action="confirm-production"` — the launched response carries the checklist and the token-hygiene note (regenerate recommended).

---

### Checking pipeline integration for ongoing CD

ZCP is verifying whether each runtime service in the new production project has a CD pipeline integration configured. This reads-only — ZCP never mutates pipeline config (Path B trust model, see `docs/spec-launch-production-platform-spike.md §B.3`).

Possible outcomes:

- **Configured** → ongoing builds will fire on the integration's trigger (tag-push for prod-recommended setup).
- **Not configured** → response will carry a `pipeline-not-configured-<hostname>` blocker with a Zerops dashboard deep-link and the recommended config payload (`repositoryFullName`, `eventType=TAG`, `tagRegex`, `zeropsYamlSetup=prod`). User configures via dashboard, then re-calls `workflow="launch-production"` to recheck — no need to re-send the launch token, ZCP reads the staged secret server-side.

---

### Existing-project conflicts — ask per-service skip / replace

Existing-project launch (you passed `existingProjectId` + `existingProdToken`) detected services in the target project whose hostnames collide with what the launch bundle would create. **Production must not silently overwrite the user's existing services.** Ask the user per conflict; re-call with `mergeStrategy` populated.

**Per-blocker shape.** Each blocker's `id` is `existing-project-conflict-<hostname>`. The `message` carries the existing service's type + status so the user has context to choose. AskUserQuestion the user per conflict with these options:

| Choice | What it does | Re-call shape |
|---|---|---|
| **Skip** (additive launch — recommended default) | Drop this entry from the bundle. The existing target service is left untouched. Other promotables still get created. | `mergeStrategy={"<hostname>": "skip"}` |
| **Replace** | Overwrite the existing target service with what's in the source. Destructive — requires explicit `confirmDestructive` ack. | `mergeStrategy={"<hostname>": "replace"}` + `confirmDestructive={operation: "launch-production-replace", acknowledgedTargets: ["<hostname>", ...]}` |
| **Cancel** | Abort the launch. No mutation. | Stop calling; pass the cancellation back to the user. |

**Replace requires destructive ack.** The diagnose-before-destruct invariant extends here: you cannot merge `replace` into the launch without a populated `confirmDestructive` whose `acknowledgedTargets` lists EVERY hostname flagged `replace`. The launch handler refuses with `existing-project-replace-needs-ack` until both align.

**Worked example.** Source has `app`, `worker`, `db`, `redis`; target already has `app`, `db`. User wants to additively promote `worker` + `redis`, keep existing `app` + `db`:

```
zerops_workflow action="start" workflow="launch-production"
  productionProjectName=<from inputs>
  existingProjectId=<from inputs>
  existingProdToken=<from inputs>
  promotables=[{hostname: appstage}, {hostname: workerstage}]
  envClassifications=<from inputs>
  mergeStrategy={"app": "skip", "db": "skip"}
```

After this re-call, the composer drops the `app` runtime + `db` managed dep from the bundle and the launch advances to classify-prompt (or directly to ready-to-launch, depending on prior progress).

**For the same scenario but the user wants to replace `app`:**

```
zerops_workflow action="start" workflow="launch-production"
  ... (same as above) ...
  mergeStrategy={"app": "replace", "db": "skip"}
  confirmDestructive=<operation: launch-production-replace, acknowledgedTargets: [app]>
```

**Do not** invent a strategy without asking the user. The conflict prompt is the explicit "what do you want to do" handoff; ZCP refuses to advance without per-conflict resolution.

After every conflict has a `mergeStrategy` entry (and every `replace` is ack'd via `confirmDestructive`), re-call launch — the handler proceeds through the canonical mutation pipeline.

---

### Pipeline integration confirmed

ZCP read each runtime's `external-repository-integration-status` and saw all configured. Ongoing CD is wired:

- Tag pushes matching the configured regex trigger Zerops builds automatically.
- Push to deploy: `git tag v1.0.0 && git push --tags` (substituting your version).

State file (`.zcp/state/launch-production/<launchID>.json`) records the live config under `pipelineConfigurations` for audit.

---

### Pipeline configuration skipped

`skipPipelineSetup=true` told ZCP not to check or recommend pipeline integration. The production project is live — but its runtimes are EMPTY (startWithoutCode) and stay empty until something delivers a build: with the pipeline skipped, NOTHING deploys the application, including the first time. Options:

- **Manual `zcli push`** from local or CI per release (`zcli login <prod-scoped token>` + `zcli push --service-id <prod service ID> --setup <setup>`).
- **Add integration later** in Zerops dashboard (`Project → Service → Source code → Connect to GitHub/GitLab`). Set the event type to `Tag`, the tag regex to `^v\d+\.\d+\.\d+$` (or your release-version convention), and the Zerops YAML setup to `prod`.

Re-run `workflow="launch-production"` if you want ZCP to verify integration setup — no need to re-send the launch token, ZCP reads the staged secret server-side; that lifts the skip and runs the configuring-pipeline check.

---

### Source-control prerequisites — resolve before launch advances

Launch refuses to advance past scope-prompt while any promoted runtime fails the source-control gate. Production builds from your repo via the production pipeline; the recorded remote must point at a repo you own AND match the live origin in `/var/www`, NOT the recipe template the service was bootstrapped from.

**Resolve blockers top-down — one re-call between each step.** The gate re-runs on every re-call and surfaces only the still-failing blockers.

| Blocker ID | What it means | Recovery |
|---|---|---|
| `git-push-unconfigured-<hostname>` | `meta.GitPushState != configured` — no probe-proven remote is wired for this service yet. Production cannot build from "whatever happens to be in the bootstrapped git config"; that's how recipe templates accidentally end up as the production source. | `zerops_workflow action="git-push-setup" service="<hostname>" remoteUrl="<url>" gitToken="<PAT>"` (container) or `... remoteUrl="<url>"` (local). The handler probes the remote BEFORE writing project state. Then re-call launch. |
| `remote-mismatch-<hostname>` | Live `git remote get-url origin` differs from the recorded `meta.RemoteURL`. Could be a manual rewrite, a recipe-template leftover, or drift since last setup. | Re-run `zerops_workflow action="git-push-setup" service="<hostname>" remoteUrl="<corrected-URL>" gitToken="<PAT>"` — the handler probes the new URL and syncs origin on success. Then re-call launch. |
| `dev-tree-dirty-<hostname>` | `git status --porcelain` on the dev push source is non-empty — uncommitted / staged / untracked changes. Those changes will NOT make it to production (Zerops clones the remote's HEAD; git push only pushes commits). git-push will NOT stage or commit them for you — it warns, then pushes the committed HEAD only; the commit step is yours, and this launch gate blocks until the tree is clean. | Commit the working tree first, then push: `ssh <hostname> "cd /var/www && git add -A && git commit -m '<msg>'"` (container) or `git -C <workingDir> add -A && git -C <workingDir> commit -m '<msg>'` (local). Then `zerops_deploy targetService="<hostname>" strategy="git-push"`. Then re-call launch. |
| `head-not-pushed-<hostname>` | Local HEAD on the push source does not match the remote HEAD (or remote HEAD unreachable). Local commits are ahead of the configured remote; production would build stale code. | `zerops_deploy targetService="<hostname>" strategy="git-push"` pushes the existing commits. If HEAD is reachable on the remote but the SHAs differ, you have unpushed local commits — `git log --oneline origin/HEAD..HEAD` shows them. Then re-call launch. |
| `build-integration-recommended-<hostname>` (warn) | `meta.BuildIntegration=none` — stage has no auto-build pipeline. Recommended to set up before promoting: the choice also selects the PRODUCTION delivery family (the launched runtimes are empty until the first release arrives through the production pipeline that mirrors this integration). Optional — does not block. | Ask the user: configure now (recommended) or skip? On configure: `zerops_workflow action="build-integration" service="<hostname>" integration="actions"` (or `webhook` for GitLab / policy-constrained repos). On skip: re-call launch with `skipBuildIntegration=["<hostname>"]` to acknowledge the choice; subsequent calls will not re-surface the warn. |
| `service-not-bootstrapped` | No `ServiceMeta` exists for the chosen `targetService`. Bootstrap never ran (or the meta got deleted). | `zerops_workflow action="start" workflow="bootstrap" route="adopt"` to adopt the existing services, then re-call launch. |

**Multi-runtime promotion.** When `Promotables` lists more than one runtime, each runtime's blockers appear with its hostname suffix. Resolve them in the order the gate emits — one chained call per step, then re-call launch. The handler is stateless; passing the same accumulated inputs each turn is sufficient.

**Trust boundary.** All chained actions above run with the standing project-scoped `ZCP_API_KEY`. Launch-window token acquisition happens only at `ready-to-launch`, after every source-control blocker is cleared: ZCP mints one itself from a one-time platform delegation on explicit confirmation when available (no value crosses the conversation), or the user supplies `launchKey` once as the fallback — the rest of the launch window reads the staged `ZCP_LAUNCH_TOKEN` secret either way.

**Prefer the orchestrated flow.** `git-push-setup` probes auth before writing project state; `zerops_deploy strategy="git-push"` pushes already-committed code via the project-level `GIT_TOKEN`. Running `git push` directly from outside this flow bypasses the gate's source-of-truth checks (meta.RemoteURL vs live origin) — the next launch re-call may still surface `remote-mismatch` until `git-push-setup` re-syncs the meta.

After every blocker clears, re-call:

```
zerops_workflow action="start" workflow="launch-production"
  productionProjectName="<from inputs>"
  targetService="<from inputs>"
  envClassifications=<from inputs>
  [skipBuildIntegration=[...]]  // only when user explicitly opted out
```

The next response advances to `classify-prompt` (envs classification) and onward through the canonical state machine.

---

### Launch status — recovery

`action="status"` surfaces one of three launch-production envelope shapes when a state file exists for this source project. Conversation context was likely lost (compaction, restart) — the envelope carries enough state to resume or terminate cleanly.

#### `kind: "launch-active"` — mid-flight

A non-terminal launch is in progress. Resume via `action="start"` with the same `productionProjectName`.

| Field | Use |
|---|---|
| `targetProjectName` | Pass back as `productionProjectName` on the resume call. |
| `status` | Phase to expect next response (`ready-to-launch` advertises `delegatedLaunch.available` — advance with `confirmLaunch=true` when `true`, `launchKey` as the fallback; `launching` / `configuring-pipeline` are polling). |
| `lastUpdate` | Sanity-check freshness — minutes old = active; days old = user may have abandoned (ask before resuming). |
| `ambiguousChoices` | Multiple non-terminal launches exist; pick a `productionProjectName` before resume. |

Resume call shape:

```
zerops_workflow action="start" workflow="launch-production" productionProjectName="<from envelope>"
```

`action="start"` is required on every call — launch-production is stateless multi-call narrowing, `action="start"` is the only orchestration entry (no classify action, no `action="complete"`). The handler re-reads accumulated state from `productionProjectName` and advances to the next phase.

The `launchKey` is NOT required at the status step. When the workflow re-enters `ready-to-launch` and you intend to advance to `launching`, check `delegatedLaunch.available` first — if a platform delegation is available, re-call with `confirmLaunch=true` instead. The fallback when no delegation is available: ask the user to generate a launch token in the dashboard and pass it as `launchKey` — never create or guess a token value yourself. Status is read-only; ZCP never constructs a project-admin client on this path.

#### `kind: "launch-failed"` — terminal failure; recovery depends on `targetProjectId`

The most-recent launch for this source project ended in `failed` (e.g. schema validation rejected the import, mutation API error). The envelope's `nextCall` carries the correct recovery — the split:

- **`targetProjectId` EMPTY — retry directly.** No production project was created; re-call `action="start"` with the same `productionProjectName`. When `tokenAcquisition` is `"delegated"`, retry with `confirmLaunch=true`: with `targetServiceHostname` present the prior attempt staged the token and the retry reuses it (no new delegation needed or consumed); without it the retry re-checks delegation availability and otherwise falls back to the manual path. Do NOT reset here unless you intend to ABANDON the launch: reset deletes any staged token, the one-time delegation is already spent, and the manual dashboard walkthrough becomes the only remaining path. `mintedTokenName` names the standing token visible in the dashboard.
- **`targetProjectId` PRESENT — reset required.** A partial production project exists; a blind retry would duplicate services or collide on project envs. Clear it first:

```
zerops_workflow action="reset" workflow="launch-production" productionProjectName="<from envelope>"
```

`action="reset"` is destructive — the first call returns a `wouldDestroy` diagnostic listing what will be removed; the second call must echo it back as a structured `confirmDestructive={"operation":"launch-production-reset","acknowledgedTargets":[...]}` ack (matching the `wouldDestroy` payload) to clear the gate. The orphan production project is deleted too when the launch token resolves — from the staged `ZCP_LAUNCH_TOKEN` secret on the source push service (no re-send needed), or via an explicit `launchKey` when the staged secret is gone. Without any token, reset only clears local state and leaves the billable orphan for manual dashboard deletion. (Old binaries without reset support: manually delete `.zcp/state/launch-production/<launchID>.json` and follow up with `zerops_manage` on orphan project envs.)

#### `kind: "launch-completed"` — terminal success; keep it or tear it down

The launch finished cleanly (`status="launched"`). The production project exists at `targetProjectId`; no further `action="start"` calls are needed for this launch. Work the launch's checklist (returned at the original `launched` response): wire delivery, ship the first release, verify, then close the launch window via `action="confirm-production"` (deletes the staged `ZCP_LAUNCH_TOKEN` secret; the token-hygiene note recommends regenerating the token afterwards). To launch ANOTHER prod project from the same source, pick a different `productionProjectName` to derive a fresh `launchID`.

To TEAR the project DOWN instead (test launch, wrong name, abandoned): `action="reset"` deletes the production project (the staged token is read server-side — never reach for zcli or the raw token value), the staged secret, and the launch state, behind the same diagnose-then-ack gate described above. Works only while the window is open; after `confirm-production` the project is deleted only from the dashboard.

---

### Write the production setup block to source zerops.yaml

Launch needs the production `setup:` block in the source repo's `zerops.yaml` **before** publishing.
Production builds from the same git URL as dev/stage; the prod-specific build/run commands live under
a separate `setup:` entry the launch bundle references.

**Use the proposed block from the response — do not author one from scratch.** When the block is
missing, the launch response carries a concrete `setup:` block DERIVED from the repo's existing
dev/stage setup (same build/run shape, production-adjusted). Two paths, in preference order:

1. **Apply the proposed block**: append it to the top-level `zerops:` list in `zerops.yaml`, review
   the derivation notes (healthCheck, prod-only deps, production env), commit, push to the configured
   remote.
2. **Target an existing setup block**: when the yaml already has a production-suitable setup under a
   different name, re-call the launch workflow with `prodSetupNameOverride="<name>"` instead of
   writing anything.

Include `run.healthCheck` — strongly recommended for production: a container receives traffic only once its readiness check passes, so without one a half-started container can serve requests.

After commit + push, re-call the launch workflow (same start call, same accumulated inputs); the
workflow re-probes and advances once the block resolves.
