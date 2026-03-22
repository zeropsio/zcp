# Zerops Platform Universals

Platform truths that apply to ALL services deployed on Zerops. These are non-negotiable requirements — violating any of them causes failures.

## Keywords

zerops, platform, universals, networking, filesystem, environment, deploy, lifecycle

## TL;DR

Core platform constraints every Zerops app must satisfy: bind 0.0.0.0, use deployFiles, external storage for state, OS env vars (no .env), and zsc execOnce for migrations.

## Networking

- Apps MUST bind `0.0.0.0` (not `localhost` or `127.0.0.1`). L7 load balancer routes to the container's VXLAN IP, not loopback. Binding localhost = 502 Bad Gateway.
- SSL terminates at the L7 balancer. All internal traffic is plain HTTP. Service-to-service calls: `http://hostname:port`, NEVER `https://`.
- Apps MUST trust proxy headers (`X-Forwarded-For`, `X-Forwarded-Proto`, `X-Forwarded-Host`). Without this: CSRF failures, mixed-content warnings, incorrect redirect URLs.
  - **Laravel/Symfony**: `TRUSTED_PROXIES="127.0.0.1,10.0.0.0/8"` env var + middleware config
  - **Django**: `CSRF_TRUSTED_ORIGINS`, `SECURE_PROXY_SSL_HEADER`
  - **.NET**: `ForwardedHeadersOptions` middleware with `KnownNetworks`
  - **Rails**: `config.hosts` + `config.action_dispatch.trusted_proxies`
  - **Express/Node**: `app.set('trust proxy', true)`
  - **Spring Boot**: `server.forward-headers-strategy=framework`
  - **Go**: Read `X-Forwarded-For` / `X-Forwarded-Proto` headers directly

## Filesystem

- Container filesystem is **per-container and survives restarts** (reload, restart, stop/start, vertical scaling all keep the same container). Files are only lost when **a new container is created**: deploy, scale-up (new container is fresh), or scale-down (removed container loses data). All persistent data MUST use: database, object storage (S3 API), or shared storage.
- Sessions, cache, temp files, uploads: MUST use an external store (database, Valkey, object storage) when running multiple containers. Filesystem sessions break with round-robin load balancing and are lost on every deploy.
- `deployFiles` is MANDATORY in every `zerops.yml` build section. Without it, nothing is deployed — the run container starts empty.

## Environment Variables

- Zerops injects env vars as OS environment variables at container start. Do NOT create `.env` files — empty values shadow OS vars. Dotenv libraries are harmless if present (they fall back to OS vars when no .env exists).
- Cross-service reference syntax: `${hostname_varname}` — resolved by Zerops before injection. Example: `${db_connectionString}`.
- `import.yml` service-level secrets use `envSecrets` (NOT `envVariables`). `envSecrets` are write-once and not visible in plaintext after creation.
- If `import.yml` uses `<@generateRandomString(...)>` or other preprocessor functions, the file MUST have `#yamlPreprocessor=on` as the first line.

## Build/Deploy Lifecycle

- Build and Run are SEPARATE containers with separate base images and separate filesystems. The ONLY bridge between them is `deployFiles`.
- Build artifacts not listed in `deployFiles` do not exist in the run container.
- `base` in build section can differ from run section (e.g., build with `php@8.4`, run with `php-nginx@8.4`).

## Multi-Container Safety

- Database migrations: use `zsc execOnce ${appVersionId} -- <command>` in `initCommands` for idempotent per-version execution. Do NOT use static keys — they prevent re-running after code changes.
- Sessions MUST use an external store (Valkey, database) when running multiple containers. Filesystem sessions break with round-robin load balancing.
- Cron jobs: use `zsc cronOnce` or ensure idempotency — multiple containers may execute the same cron.

## Recipe Conventions

- **deployFiles is for stage/production** — recipes show the optimized deploy pattern for cross-deploy targets or git-based builds. For self-deploying services (dev or simple mode), use `deployFiles: [.]` so source + zerops.yml survive the deploy.
- **healthCheck is for stage/production only** — recipes show the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
