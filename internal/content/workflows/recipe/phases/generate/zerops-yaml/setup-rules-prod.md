# setup: prod — rules

The prod setup is what cross-deploys from dev to stage and ends up as the end-user production target in the published recipe. The injected chain recipe's prod setup is the baseline to adapt; the rules below are the recipe-specific additions on top.

## Baseline

Take the chain recipe's prod setup as the starting shape. Adapt it to the current recipe's managed services (from `plan.Research`) and framework version.

## initCommands — migrations, seeds, and search-index population

When a search engine is in the plan, `initCommands` includes the framework's search-index import command after the seed step runs. The ORM's auto-index-on-create may work during seeding, but an explicit import is the safety net: when the seeder's idempotency guard skips creation (records already exist from a prior deploy), auto-indexing fires zero events and search returns nothing. The explicit import keeps the search index populated.

The `seed-execonce-keys.md` atom covers the `zsc execOnce` key shapes that match the lifetime of each `initCommands` entry — migrations use one shape, seeds another.

## Run configuration

- `prepareCommands` in `run` installs a secondary runtime only when the prod start command needs it at runtime (for example SSR with Node). When a secondary runtime is build-only, it lives in `build.base` — adding it to `run.prepareCommands` adds 30 seconds or more to every container start for no operational gain. Dev has `prepareCommands` for the dev server; prod does not.
- Framework mode flags set to prod values (`APP_ENV=production`, `NODE_ENV=production`, `DEBUG=false`, log level raised, and the framework's equivalents). These are the only lines in `envVariables` that differ from dev.
- Cross-service reference keys in `envVariables` are identical to dev — the platform's deploy-time resolution differs, not the names.

## deployFiles

`deployFiles` on prod names the build output the container serves — typically a compiled `dist/` or `build/` directory, with the tilde-suffix convention `./dist/~` or `./build/~` when the prod runtime is serve-only (see `setup-rules-static-frontend.md`). Dynamic-runtime prod setups keep `deployFiles: [.]` when the runtime executes from the source tree; compiled runtimes name the compiled artifact only.

## What does not go in prod envVariables

Prod `envVariables` carries the same two kinds of entries as dev per `env-var-model.md`: cross-service references and mode flags. `envSecrets` never appears in `envVariables` because the platform injects secret values automatically. Build-time-baked client-side variables belong in `build.envVariables`, not `run.envVariables` — the dual-runtime atom covers that distinction.
