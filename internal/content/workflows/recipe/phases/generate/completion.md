# Generate — completion predicate (pre-deploy checklist)

The generate phase counts as complete when every substep predicate holds **and** the pre-deploy checklist below is satisfied on every codebase mount. `zerops_workflow action=status` shows the authoritative substep list.

## Pre-deploy checklist

- `.gitignore` exists and covers build artifacts, dependencies, and env files appropriate to the framework (for example `dist/`, `node_modules/`, `vendor/`, `.env`, `*.pyc`). Framework CLIs sometimes skip generating one — verify before `git add`.
- Both `setup: dev` and `setup: prod` are present in `zerops.yaml` under the generic names. Showcase with a shared-codebase worker adds `setup: worker` in the host target's zerops.yaml (the target named by `sharesCodebaseWith`); showcase with a separate-codebase worker carries that worker's own zerops.yaml with its own `dev` + `prod` setups.
- dev and prod `envVariables` maps differ on mode flags — identical maps fail the structural check.
- Every env-var reference name comes from `zerops_discover` output — no guessed names.
- When prod `buildCommands` compiles assets, the primary view loads them via the framework's asset helper — not inline CSS or JS.
- When the dev build base includes a secondary runtime, dev `buildCommands` includes that runtime's package-manager install step.
- `.env.example` is preserved (with the generated `.env` removed) and covers every env var the recipe references.
- **Showcase only** — the dashboard is the health-skeleton shape from `dashboard-skeleton-showcase.md`: `<StatusPanel />` rendering one dot per managed service, plus `/api/health` and `/api/status`. No item CRUD, no cache-demo, no search UI, no jobs dispatch, no storage upload. Feature sections belong to the feature sub-agent at the deploy step.
- **Showcase only** — the seeder populates 3 to 5 rows of primary-model sample data. The feature sub-agent expands seeds when the features it builds require more.
- **Showcase only** — search-index population belongs to the feature sub-agent at deploy, not here. The scaffold leaves search wiring initialized but no sync step.

## Attestation shape

Attest when every substep (scaffold, app-code, smoke-test, zerops-yaml) is complete **and** every pre-deploy checklist predicate holds on every mount. The attestation text states: app code and zerops.yaml written to every dev mount; on-container smoke test passed on every dev mount; READMEs deferred to the deploy step's `readmes` substep.
