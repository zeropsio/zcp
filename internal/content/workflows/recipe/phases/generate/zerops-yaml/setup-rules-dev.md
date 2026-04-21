# setup: dev — rules

Follow the injected chain recipe (the working zerops.yaml from the predecessor) as the primary reference. For hello-world tiers with no predecessor, follow the injected zerops.yaml schema. Platform lifecycle and deploy semantics were taught during provision — consult `zerops_knowledge` when a specific rule is needed. These rules are the recipe-specific additions on top of platform defaults.

## Dev is the agent's iterable container

`setup: dev` gives the agent a container that hosts the framework's dev toolchain — shell, package manager, and the dev process (`npm run dev`, `php artisan serve`, `bun --hot`, `cargo watch`, or the framework's equivalent). The agent iterates here over SSH.

## Run configuration

- **Dynamic runtimes** (`nodejs`, `python`, `php-nginx`, `go`, `rust`, `bun`, `ubuntu`, …): `run.base` is the same as prod; `deployFiles: [.]` preserves source across deploys. Anything other than `[.]` destroys the source tree and belongs to prod.
- `start: zsc noop --silent` — use this on every runtime that requires a `start` entry. Implicit-webserver runtimes (`php-nginx`, `php-apache`, `nginx`, `static`) omit the `start` key entirely.
- Omit `run.os`. The agent operates from the host via SSH, so the dev container needs only the runtime and its package manager — both are in the base image. Build and run stay on the same default OS, eliminating native-binding mismatches. Add `run.os` only as a reactive exception: when the smoke test reveals a hard glibc-only dependency and the dev server crashes on the default OS, add `run.os` with the appropriate OS **and** add the package manager's rebuild command to `initCommands`.
- No `healthCheck`, no `readinessCheck` on dev — the agent controls the lifecycle; platform-side checks would restart the container during iteration.

## Ports and subdomain access

If `setup: dev` runs a dev-server process (any framework with a bundler, HMR server, or dev-mode HTTP server on a non-standard port), declare `ports` with the dev server's port from research data and `httpSupport: true`. Without that declaration, subdomain access cannot be enabled and the platform returns `serviceStackIsNotHttp` when `zerops_subdomain action=enable` runs. This applies specifically when the dev setup's runtime differs from the prod setup's runtime — for example, prod is serve-only while dev runs a bundler on an explicit port.

## Dev-dependency pre-install

When the build base includes a secondary runtime for an asset pipeline, dev `buildCommands` includes the dependency-install step for that secondary runtime's package manager. The dev container then ships with dependencies pre-populated — an SSH session can start the dev server immediately without a manual install. Omit the asset compilation step; that belongs to prod. Dev uses the live dev server.

## Cross-service references and mode flags

Cross-service references in `envVariables` are the same names the prod setup uses — only mode flags differ between dev and prod. `envVariables` carries two kinds of entries per `env-var-model.md`: cross-service references and mode flags. Dev mode flag values are verbose / `development` / `local` / `DEBUG: "true"`.

## Setup-name conventions

Use only `setup: dev` and `setup: prod` as generic names across recipes. The dev setup is the agent's iterable target; the prod setup is what cross-deploys to stage. Setup names `stage` and runtime-variant names do not appear in zerops.yaml — deploys select a setup by name via `zerops_deploy setup=dev` or `zerops_deploy setup=prod`, and stage uses `setup: prod`.

Cross-service URL building uses the project-scope `${zeropsSubdomainHost}` variable plus the constant URL format from the dual-runtime atom. Building URLs from another service's `${hostname}_zeropsSubdomain` is out-of-scope for this recipe's zerops.yaml.
