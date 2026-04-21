# zerops-yaml substep — completion predicate

The zerops-yaml substep counts as complete when, for every codebase in the plan:

- A single `zerops.yaml` file exists on the mount with every setup the codebase requires (`dev` + `prod`, plus `worker` when the shared-codebase-worker shape applies).
- Comment ratio across the file is at or above 30 percent, with comments in the one form declared by `comment-style-positive.md`.
- `envVariables` carries only cross-service references and mode flags; a grep for same-name self-shadow lines returns empty per `env-var-model.md`.
- `initCommands` uses the `execOnce` key shape that matches each command's lifetime per `seed-execonce-keys.md`.
- For dual-runtime recipes, the zerops.yaml half of URL baking is wired per `dual-runtime-consumption.md`, and the source-code API helper was already written during the app-code substep.
- For static-frontend prod, `deployFiles` uses the tilde-suffix path per `setup-rules-static-frontend.md`.

Attest only when every predicate above holds across every codebase.
