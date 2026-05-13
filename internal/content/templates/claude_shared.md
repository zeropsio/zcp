Zerops has its own syntax. Don't guess — look up via `zerops_knowledge`, inspect live state via `zerops_*`. Runtime code runs in Zerops containers, not here.

## Route every user turn

| Intent | First action | Don't |
|---|---|---|
| Build/edit/scaffold/fix/deploy/debug a service | `zerops_discover`/`zerops_workflow action="status"` first if target/session unclear, then `zerops_workflow action="start" workflow="develop" intent="..." scope=["<host>"]` | Write code, run Bash/npx/SSH, or scaffold to scratch dirs before workflow start |
| No service yet, or infra/topology change | `zerops_workflow action="start" workflow="bootstrap" intent="..."` | Write app code in bootstrap |
| Create/publish a recipe (user said "recipe" or named a slug) | `zerops_recipe action="start" slug="..." outputRoot="..."` | Start develop/bootstrap inside recipe |
| Read or set platform state — logs/env/status/scale/subdomain/manage/events/verify | matching `zerops_*` tool | Guess values when live state exists |
| Promote existing dev/stage to a SEPARATE production project ("go live", "deploy to prod", "launch prod", "nasaď to na prod") | `zerops_workflow action="start" workflow="launch-production" intent="..." targetService="<dev-hostname>"` | Use `zcli project create` or hand-roll import.yaml — the workflow owns bundle composition, source-control mutation, one-shot token gate, and post-launch checklist |
| Pure concept Q unrelated to this project | prose, no tool | Re-route when user pivots to build/change |

## Discovery floor

Before service-scoped work: `zerops_workflow action="status"` if a session may exist (post-compact), else `zerops_discover`. User didn't name service + multiple plausible targets → ask once. Never invent hostnames, env keys, service types, subdomain URLs.

## Smells — catch & re-route

- Multi-section prose analysis (framework cmp, IA, "let me first analyze") for service-shaped task → workflow start IS the analysis surface (returns plan + atoms scoped to your `intent`). Pick a sensible default, start, react to the response. User saying "analyze first" / "make a plan" doesn't bypass.
- Writing code or `zerops.yaml` before workflow/status/discover selected service.
- Files in `/tmp` or random scratch dirs for app code.
- Asking whether to deploy to Zerops when ZCP is already bound to this project.
- Bash/SSH for platform ops covered by `zerops_*` (env, logs, scale, restart, etc.).
- Diagnosing live errors/502s/build failures from prose instead of `zerops_verify`/`zerops_logs`/`zerops_events`/`zerops_env`.
- Writing `import.yaml` or running `zcli project create` for a "promote to prod" / "go live" / "launch production" intent. `workflow="launch-production"` is the dedicated surface — handles bundle composition, source-control mutation, one-shot token gate, and post-launch checklist. zcli for new prod projects is the manual fallback, not the default.

## Workflow detail

- `develop` — service code edit. `scope` = runtime services this touches; get from `zerops_discover`, don't invent. `intent` = one-line proposal; workflow returns the plan, react to that. 1 task = 1 session; new `intent` auto-closes prior.
- `bootstrap` — provision services / change infra. Closes → continue in develop. Mid-develop infra side-trip: start bootstrap; develop session persists.
- `recipe` — self-contained pipeline; atoms guide every step.
- `launch-production` — promote an existing healthy dev/stage project to a SEPARATE production Zerops project. Bundle composition with HA managed deps + prod scaling tier; user-supplied account-wide one-shot token (validated, never persisted); source-control mutation (`setup: prod` block in `zerops.yaml`); post-launch checklist (delete the key, attach domain). Multi-call narrowing: scope-prompt → classify-prompt → ready-to-launch → launching → configuring-pipeline → launched. `targetService` is the dev-half hostname; stage-half input is rejected with a corrective blocker.

## Recovery

Phase unclear (post-compact, mid-task): `zerops_workflow action="status"` (or `zerops_recipe action="status"`). Returns envelope, plan, next action.

## Tool errors

Shape: `{code, error, suggestion?, apiCode?, diagnostic?, apiMeta?, checks?, recovery?}`. `code`+`error` always present. `recovery` set → call before retry/ask. Absent → fall back to `zerops_workflow action="status"`. `checks` = multi-check failures (`kind` + optional `preAttestCmd`/`expectedExit`).
