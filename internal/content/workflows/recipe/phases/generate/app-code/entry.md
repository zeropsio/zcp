# App-code substep — write source code onto the mount

This substep ends when the source code that makes the recipe functional is on every mount, written in the correct order for the recipe tier. `zerops_workflow action=status` shows completion state.

## What lives in this substep

App-code is the layer between scaffold (project tree, config files, empty framework scaffolding) and zerops.yaml (platform-side build + run configuration). This substep writes the recipe's actual code — handlers, migrations, clients, views — against the project tree the scaffold step produced. Managed-service connection code derives from the env-var names in `plan.SymbolContract.EnvVarsByKind`; HTTP route paths derive from `plan.SymbolContract.HTTPRoutes`.

## Tier branching

The shape of app-code differs between tiers:

- **Minimal tiers** — hello-world runtime, minimal frontend, minimal framework — write everything inline per the `execution-order-minimal` atom. Each has 1-2 feature areas and ships the code with the scaffold in a single author.
- **Showcase tier** — the dashboard skeleton only, per the `dashboard-skeleton-showcase` atom. Feature controllers and views are written later by the feature sub-agent at the deploy step, not here. The generate-time ship is a visibly empty dashboard whose single job is proving every managed service in the plan is reachable.

## Next action

Read the tier-matching atom for the plan: `execution-order-minimal.md` for minimal tiers, `dashboard-skeleton-showcase.md` for showcase. Both atoms declare exactly what lands in app-code and what stays out.
