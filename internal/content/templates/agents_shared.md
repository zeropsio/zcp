Zerops has its own syntax. Don't guess — look up via `zerops_knowledge`, inspect live state via `zerops_*`. Runtime code runs in Zerops containers, not here.

## Route every user turn

| Intent | First action | Don't |
|---|---|---|
| Build/edit/scaffold/fix/deploy/debug a service | `zerops_discover`/`zerops_workflow action="status"` first if target/session unclear, then `zerops_workflow action="start" workflow="develop" intent="..." scope=["<host>"]` | Write code, run Bash/npx/SSH, or scaffold to scratch dirs before workflow start |
| No service yet, or infra/topology change — INCLUDING "deploy / set up / scaffold from existing recipe X" (user names a recipe slug like `zerops-laravel-minimal`) | `zerops_workflow action="start" workflow="bootstrap" intent="..."` — the route-menu surfaces the matching recipe; pick `route="recipe"` with the named slug | Write app code in bootstrap |
| Read or set platform state — logs/env/status/scale/subdomain/manage/events/verify | matching `zerops_*` tool | Guess values when live state exists |
| Promote dev/stage to a separate prod project ("go live", "deploy to prod", "nasaď na prod") | `zerops_workflow action="start" workflow="launch-production" intent="..." targetService="<host>"` | `zcli project create` or hand-rolled import.yaml |
| Pure concept Q unrelated to this project | prose, no tool | Re-route when user pivots to build/change |

## Discovery floor

Before service-scoped work: `zerops_workflow action="status"` if a session may exist (post-compact), else `zerops_discover`. User didn't name service + multiple plausible targets → ask once. Never invent hostnames, env keys, service types, subdomain URLs.

## Connection vars & secrets

Reference by name, never paste the value. `zerops_env`/`zerops_discover` read env KEYS and set STATE; a value you need in a command is `$VAR` — the shell expands it at exec time, so the value never enters your context. Pulling a credential value to paste into a command, file, or commit is the leak.

## Smells — catch & re-route

- Multi-section prose analysis (framework cmp, IA, "let me first analyze") for service-shaped task → workflow start IS the analysis surface (returns plan + atoms scoped to your `intent`). Pick a sensible default, start, react to the response. User saying "analyze first" / "make a plan" doesn't bypass.
- Writing code or `zerops.yaml` before workflow/status/discover selected service.
- Files in `/tmp` or random scratch dirs for app code.
- Asking whether to deploy to Zerops when ZCP is already bound to this project.
- Bash/SSH for platform ops covered by `zerops_*` (env, logs, scale, restart, etc.).
- Diagnosing live errors/502s/build failures from prose instead of `zerops_verify`/`zerops_logs`/`zerops_events`/`zerops_env`.
- Hand-rolling `import.yaml` or `zcli project create` for a "promote to prod" / "go live" intent → `workflow="launch-production"`.

## Workflow detail

- `develop` — service code edit. `scope` = runtime services this touches; get from `zerops_discover`, don't invent. `intent` = one-line proposal; workflow returns the plan, react to that. 1 task = 1 session; new `intent` auto-closes prior.
- `bootstrap` — provision services / change infra. Closes → continue in develop. Mid-develop infra side-trip: start bootstrap; develop session persists.
- `launch-production` — promote dev/stage to a SEPARATE prod project. Stateless multi-call: `scope-prompt` → `classify-prompt` → `ready-to-launch` → `launching` → `configuring-pipeline` → `launched`. Each call passes the accumulated `inputs` block forward (no `action="complete"` — that's bootstrap-only). At `ready-to-launch`, `delegatedLaunch.available` says whether ZCP can mint the launch-window token itself from a one-time platform delegation on `confirmLaunch=true` (no value crosses the conversation); otherwise the user supplies one manually (Custom access per project + Allow creating projects toggle ON) as `launchKey`. ZCP never persists it either way. `targetService` accepts either half of a standard pair.

## Recovery

Phase unclear (post-compact, mid-task): `zerops_workflow action="status"`. Returns envelope, plan, next action.

## Tool errors

Shape: `{code, error, suggestion?, apiCode?, diagnostic?, apiMeta?, checks?, recovery?}`. `code`+`error` always present. `recovery` set → call before retry/ask. Absent → fall back to `zerops_workflow action="status"`. `checks` = multi-check failures (`kind` + optional `preAttestCmd`/`expectedExit`).
