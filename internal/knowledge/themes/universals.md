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
  - **Laravel/Symfony**: `TRUSTED_PROXIES="*"` env var + middleware config
  - **Django**: `CSRF_TRUSTED_ORIGINS`, `SECURE_PROXY_SSL_HEADER`
  - **.NET**: `ForwardedHeadersOptions` middleware with `KnownNetworks`
  - **Rails**: `config.hosts` + `config.action_dispatch.trusted_proxies`
  - **Express/Node**: `app.set('trust proxy', true)`
  - **Spring Boot**: `server.forward-headers-strategy=framework`
  - **Go**: Read `X-Forwarded-For` / `X-Forwarded-Proto` headers directly

## Filesystem

- Container filesystem is **VOLATILE**. Only files listed in `deployFiles` survive a deploy. All persistent data MUST use: database, object storage (S3 API), or shared storage.
- Sessions, cache, temp files, uploads: MUST use an external store (database, Valkey, object storage). Filesystem-based sessions and cache WILL be lost on every deploy or container restart.
- `deployFiles` is MANDATORY in every `zerops.yml` build section. Without it, nothing is deployed — the run container starts empty.

## Environment Variables

- Zerops injects env vars as OS environment variables at container start. No `.env` files. No dotenv loading libraries.
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
