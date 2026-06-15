# Provision phase — bring up the working project

Provision creates the single Zerops project this recipe run iterates
against. Scaffold + feature phases deploy code into it; finalize
generates the 6 published tier yamls separately — **provision does not
create any of those 6 tiers as live projects**. It creates one workspace.

## The two distinct YAML shapes — do not conflate

- **Workspace YAML** (this phase): services-only, no `project:` block,
  dev runtimes `startWithoutCode: true` so they come up empty for you
  to write code into via SSH/mount, stage runtimes wait at
  `READY_TO_DEPLOY`, no `buildFromGit` (repos don't exist yet), no
  `zeropsSetup`, no preprocessor expressions. Submitted inline via
  `zerops_import content=<yaml>`.

- **Deliverable YAMLs** (6 files, produced at finalize): full `project:`
  block per tier with `envVariables`, every runtime has
  `zeropsSetup: dev|prod` + `buildFromGit` pointing at the published
  codebase repos, shared secrets use `<@generateRandomString(<32>)>`
  templates so every end-user's click-deploy gets a fresh value.

The workspace yaml you submit here is NOT one of the 6 deliverables. Do
not try to pass a deliverable yaml to `zerops_import` — the repos don't
exist yet and it would fail at the clone step.

## Steps

1. **Emit the workspace yaml**:
   `zerops_recipe action=emit-yaml slug=<slug> shape=workspace`

   Returns services-only yaml with dev+stage pairs per codebase + all
   managed services. No disk write — the yaml string is the response.

2. **Provision the workspace**:
   `zerops_import content=<yaml from step 1>` (pass the string inline,
   do not write it to disk first). Wait for every service to reach its
   expected state:
   - Dev runtimes → `RUNNING` (via `startWithoutCode: true`)
   - Stage runtimes → `READY_TO_DEPLOY` (wait for first cross-deploy)
   - Managed services → `RUNNING` / `ACTIVE`

3. **Set project-level shared secrets** (if `Research.NeedsAppSecret=true`):
   ```
   zerops_env project=true action=set \
     variables=["<AppSecretKey>=<@generateRandomString(<32>)>"]
   ```
   The preprocessor runs once, the actual secret value lands on the
   live project, and dependent services restart with it. **This is the
   real secret your workspace uses.** The 6 deliverable yamls emit their
   own `<@generateRandomString>` template at finalize — each end-user's
   click-deploy gets a different value, which is correct.

4. **Set cross-service URL constants** (when the recipe has a frontend
   that bakes URLs at build time, OR an API that needs CORS allow-list):

   Project-scope URL constants resolve at provision time (before any
   peer service deploys), so the SPA's build-time `VITE_API_URL` bake
   and the api's `CORS_ORIGINS` runtime allow-list both have real values
   from the very first deploy. Cross-service refs (`${api_zeropsSubdomain}`)
   only mint after the peer's first deploy — too late for a
   fresh-project build.

   Construction template (4 keys for dev-pair tiers; the engine drops
   `DEV_*` at single-slot tier emit):

   ```
   zerops_env project=true action=set variables=[
     "API_URL=https://<apistage-host>-${zeropsSubdomainHost}-<api-port>.prg1.zerops.app",
     "FRONTEND_URL=https://<appstage-host>-${zeropsSubdomainHost}<-port-or-empty>.prg1.zerops.app",
     "DEV_API_URL=https://<apidev-host>-${zeropsSubdomainHost}-<api-port>.prg1.zerops.app",
     "DEV_FRONTEND_URL=https://<appdev-host>-${zeropsSubdomainHost}-<dev-port>.prg1.zerops.app"
   ]
   ```

   Port mapping by role:
   - NestJS / Express / Fastify api → `-3000`
   - Vite dev server → `-5173`
   - `base: static` (production frontend) → no port suffix
   - Worker → no URL constant (no public surface)

   Then record into the plan so the engine emits them in tier yamls'
   `project.envVariables` block at finalize:

   ```
   zerops_recipe action=update-plan slug=<slug> plan='{
     "projectEnvVars": {
       "0": {"API_URL": "...", "FRONTEND_URL": "...", "DEV_API_URL": "...", "DEV_FRONTEND_URL": "..."},
       "1": {"API_URL": "...", "FRONTEND_URL": "...", "DEV_API_URL": "...", "DEV_FRONTEND_URL": "..."}
     }
   }'
   ```

   Two channels (live workspace + plan record) are required:
   - `zerops_env action=set` populates the workspace project's live env
     so scaffold sub-agents bake real URLs into bundles immediately.
   - `update-plan projectEnvVars` records the same constants for tier
     0/1 emit at finalize. The engine's `rewriteURLsForSingleSlot`
     handles tier 2-5 reshape automatically (drops `DEV_*`, rewrites
     slot-named hostnames to bare).

   Workerless / api-only recipes skip this step. Static-only recipes
   that don't have an API skip too.

   ### Tier-conditional meaning of the keys

   - **Workspace + tier 0-1 (dev-pair tiers)**: `API_URL` resolves to
     the **stage-side / production-setup** API (`apistage-...`);
     `DEV_API_URL` resolves to the **dev-setup** API (`apidev-...`).
     Codebase yaml's `appdev/zerops.yaml prod setup` references
     `${API_URL}`; the same file's `dev setup` references `${DEV_API_URL}`.
   - **Tier 2-5 (single-slot tiers)**: `API_URL` is **the only api**
     (no dev/stage distinction at single-slot). The engine's
     `rewriteURLsForSingleSlot` drops `DEV_API_URL` / `DEV_FRONTEND_URL`
     keys at emit and rewrites the slot-named hostname (`apistage-` →
     `api-`, `appstage-` → `app-`) in the value.

   Naming logic: the key `API_URL` is the project's API URL constant
   at this tier. At dev-pair tiers the value happens to be the
   stage-side URL because dev has its own dedicated key; at single-slot
   tiers the value is the only available URL.

5. **Mount dev codebases** (one per non-worker-shared codebase):
   `zerops_mount serviceHostname=<codebase>dev`. SSHFS mounts land on
   the `startWithoutCode` dev containers.

6. **Catalog cross-service env var keys**:
   `zerops_discover includeEnvs=true`. Record the authoritative env-var
   keys each managed service exposes — `${db_hostname}`, `${db_user}`,
   `${cache_hostname}`, etc. Scaffold sub-agents reference these in each
   codebase's `zerops.yaml run.envVariables`, never raw values.

7. **Complete the phase**:
   `zerops_recipe action=complete-phase slug=<slug>`.

8. **Advance to scaffold**:
   `zerops_recipe action=enter-phase slug=<slug> phase=scaffold`.
   `complete-phase` does NOT auto-advance — it only marks the
   current phase done. Without the explicit `enter-phase` the
   session stays at `phase=provision` and the next `complete-phase`
   re-runs provision gates.

## What NOT to do

- Do NOT emit a deliverable yaml at provision. Deliverable shape has
  `buildFromGit` pointing at repos that don't exist yet.
- Do NOT write the workspace yaml to disk. `zerops_import` takes
  `content` inline.
- Do NOT declare shared secrets in the workspace yaml's `envVariables`
  (there is no `project:` block in workspace shape). Use `zerops_env
  project=true action=set` after import.
- Do NOT bake your workspace's real secret value into anything that
  flows to finalize. Finalize emits `<@generateRandomString>` templates
  for reproducibility.
- Do NOT call `zerops_import` with a hand-written yaml. Use the
  engine-emitted workspace shape. If the emitter produces invalid yaml,
  record a fact and fix the emitter via PR.
