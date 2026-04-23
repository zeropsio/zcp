# Finalize phase — writer dispatch, stitch payload, regenerate all 6 tiers

Scaffold + feature built a working deploy in the single workspace
project. Finalize turns that into the 6-tier publishable artifact each
end-user clicks to deploy.

## The env-var template model (critical)

The 6 deliverable yamls are a **template**. Each end-user's click-deploy
creates their own project with their own subdomains and their own
secrets. That means:

- **Shared secrets** (APP_KEY, JWT_SECRET, session key) emit as
  `<@generateRandomString(<32>)>` templates — evaluated once per
  end-user at their import. Your workspace's real secret value stays on
  your workspace; **it is NOT copied into the deliverable**.
- **URLs** use `${zeropsSubdomainHost}` as a literal — the platform
  substitutes the end-user's subdomain at their click-deploy. Do NOT
  bake your workspace's resolved URL.
- **Per-env shape differs**:
  - Envs 0-1 (AI Agent, Remote/CDE — dev-pair slots `apidev`/`apistage`
    exist): carry both `DEV_*` and `STAGE_*` URL constants
  - Envs 2-5 (Local, Stage, Small-Prod, HA-Prod — single-slot `api`/`app`):
    carry `STAGE_*` only, with hostnames `api`/`app` (not `apistage`/`appstage`)

## Steps

1. **Build the writer brief**:
   `zerops_recipe action=build-brief slug=<slug> briefKind=writer`.
   Brief walks the surface registry, filters facts by surface hint,
   inlines example banks, returns ~8-10 KB of guidance.

2. **Dispatch the writer** via `Agent`. Pass the brief body verbatim.
   Description: `writer-<slug>`. The writer reads `zerops_workspace_manifest`
   to see the full run state without your debug history polluting its
   context.

3. **Writer returns a structured completion payload** — see
   `completion_payload.md` for the schema. Keys include:
   - `root_readme`, `env_readmes`, `codebase_readmes`, `codebase_claude`
     — surface bodies
   - `env_import_comments` — per-env `{project, service: {host →
     comment}}` merged into `plan.EnvComments`
   - `project_env_vars` — per-env env var maps merged into
     `plan.ProjectEnvVars`. Must use `${zeropsSubdomainHost}` for URLs
     and `<@generateRandomString>` for shared secrets (see shape above)
   - `citations`, `manifest` — gate-check inputs

4. **Stitch**:
   `zerops_recipe action=stitch-content slug=<slug> payload=<writer JSON>`.
   The engine:
   - Archives the raw payload at `<outputRoot>/.writer-payload.json`
   - Merges `env_import_comments` → `plan.EnvComments`
   - Merges `project_env_vars` → `plan.ProjectEnvVars`
   - Regenerates all 6 `<outputRoot>/<tier.Folder>/import.yaml` files
     with writer-authored comments + env vars
   - Writes root README, env READMEs, per-codebase READMEs (IG + KB
     fragments), per-codebase CLAUDE.md files

5. **Gates**: `zerops_recipe action=complete-phase slug=<slug>`. Checks
   include env-imports-present (all 6 files on disk), citation
   timestamps, required fact fields, completion-payload schema,
   main-agent-rewrote-writer-path violations.

6. **Fix-dispatch** (if gates fail): diff the writer's output against
   the brief schema, compose a targeted correction prompt, re-dispatch
   the writer. Do NOT hand-edit writer-owned files — that trips the
   main-agent-rewrote-writer-path gate.

## What NOT to do

- Do NOT re-run `emit-yaml shape=workspace` at finalize — that shape is
  provision-only.
- Do NOT pass your live workspace's secret value as a
  `project_env_vars` entry. Use `<@generateRandomString(<32>)>`.
- Do NOT resolve `${zeropsSubdomainHost}` to a literal URL. It must
  stay a template for the end-user's platform to substitute.
- Do NOT edit the stitched files by hand. `stitch-content` is the only
  supported write path; any subsequent edit by the main agent trips the
  authorship gate.
