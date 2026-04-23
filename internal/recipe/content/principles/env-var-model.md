# Env-var lifecycle — three timelines

Recipes have three distinct env-var timelines. Conflating them produces
security leaks (author's secret in every end-user's deploy), dead URLs
(author's subdomain baked into the template), or provision failure
(workspace yaml trying to clone repos that don't exist yet). Keep them
separate.

## 1. Workspace (live, the author's single iteration project)

- **Where values live**: on the Zerops project, set after
  `zerops_import content=<workspace yaml>` via:
  ```
  zerops_env project=true action=set \
    variables=["<KEY>=<@generateRandomString(<32>)>"]
  ```
  Preprocessor runs once, the actual secret value lands on the project,
  dependent services restart with it.
- **Syntax**: real values. `<@generateRandomString>` is evaluated now,
  not preserved as a template.
- **Cross-service keys**: catalog via `zerops_discover includeEnvs=true`
  after services are `RUNNING`. These are auto-injected — scaffolded
  code reads `${db_hostname}` / `${db_user}` / etc. from the process
  env, never from declarations.

## 2. Scaffold (per-codebase `zerops.yaml run.envVariables`)

- **Where values live**: in each codebase's `zerops.yaml`, committed
  into the repo that ends up published as
  `github.com/zerops-recipe-apps/<slug>-<hostname>`.
- **Syntax**: `${hostname_key}` references to the cross-service
  auto-inject, plus mode flags (`NODE_ENV: production`, `LOG_LEVEL`).
  Never raw secret values — the platform resolves `${...}` at deploy
  time against each project's actual service set.
- **Self-shadow trap**: do NOT declare `DB_HOST: ${db_hostname}` as an
  alias — the platform-injected `${db_hostname}` collides with the
  alias and blanks. Cite the `env-var-model` zerops_knowledge guide if
  the recipe needs aliasing.

## 3. Deliverable (6 finalize yamls, each end-user clicks to deploy)

- **Where values live**: in the published
  `zeropsio/recipes/<slug>/<env>/import.yaml` files'
  `project.envVariables` block. Populated by passing
  `project_env_vars` in the writer completion payload; engine merges
  into `plan.ProjectEnvVars` and regenerates the yamls at
  `stitch-content`.
- **Syntax**:
  - Shared secrets: `<KEY>: <@generateRandomString(<32>)>` — evaluated
    once per **end-user** at their click-deploy import. Your workspace's
    real secret value from timeline 1 stays on your workspace; it does
    not appear in the deliverable.
  - URLs: `<KEY>: https://<host>-${zeropsSubdomainHost}.prg1.zerops.app`
    — `${zeropsSubdomainHost}` stays literal, platform substitutes at
    end-user's import.
  - Anything else emits verbatim.
- **Per-env shape**:
  - Envs 0-1 (AI Agent, Remote/CDE — dev-pair slots exist): declare
    both `DEV_*` and `STAGE_*` URL constants with hostnames
    `apidev`/`apistage`/`appdev`/`appstage`.
  - Envs 2-5 (Local, Stage, Small-Prod, HA-Prod — single-slot): declare
    `STAGE_*` only with hostnames `api`/`app`.

## The leak rule

- Timeline 1 values **never** flow into timeline 3. If the writer's
  `project_env_vars` for any env has a literal 32-char hex string, it's
  a leak — replace with `<@generateRandomString(<32>)>`.
- Timeline 1 URLs **never** flow into timeline 3. If a URL in
  `project_env_vars` resolves the author's subdomain, it's a leak —
  replace with `${zeropsSubdomainHost}`.

The recipe is a template. Timeline 3 is what users get; timeline 1 is
what you iterate against. They must differ by design.
