# Zerops Rules & Pitfalls

## Keywords
rules, pitfalls, gotchas, mistakes, always, never, do, dont, troubleshooting, common errors, best practices, constraints, validation

## TL;DR
Actionable DO/DON'T rules for Zerops, each with a causal reason. Sourced from production incidents, eval failures, and platform constraints. If you follow these, you avoid 95% of deployment failures.

---

## Networking

- **ALWAYS** bind `0.0.0.0` (all interfaces). REASON: L7 LB routes to VXLAN IP, not localhost. Binding 127.0.0.1 → 502 Bad Gateway
- **ALWAYS** use `http://` for internal service-to-service communication. REASON: SSL terminates at the LB; internal traffic is plain HTTP over VXLAN
- **NEVER** listen on port 443 or 80 (exception: PHP uses 80). REASON: Zerops reserves 80/443 for SSL termination. Use 3000, 8080, etc.
- **ALWAYS** use port range 10-65435. REASON: ports outside this range are reserved by the platform
- **NEVER** use `https://` for internal service URLs. REASON: no TLS certificates exist on internal network; connection will fail
- **ALWAYS** set Cloudflare SSL to "Full (strict)" when using Cloudflare proxy. REASON: "Flexible" causes infinite redirect loops

## Build & Deploy

- **ALWAYS** specify `deployFiles` in zerops.yml. REASON: nothing auto-deploys; build artifacts don't transfer to run container without explicit specification
- **ALWAYS** include `node_modules` in `deployFiles` for Node.js apps (unless bundled). REASON: runtime container doesn't run `npm install`
- **ALWAYS** deploy fat/uber JARs for Java. REASON: build and run are separate containers; thin JARs lose their dependencies
- **ALWAYS** use Maven/Gradle wrapper (`./mvnw`, `./gradlew`) or install build tools via `prepareCommands`. REASON: build container has JDK only — Maven, Gradle are NOT pre-installed
- **NEVER** reference `/var/www/` in `run.prepareCommands`. REASON: deploy files arrive AFTER prepareCommands execute; `/var/www` is empty during prepare
- **ALWAYS** use `addToRunPrepare` + `/home/zerops/` path for files needed in `run.prepareCommands`. REASON: this is the only way to get files from build into the prepare phase
- **ALWAYS** match `deployFiles` layout to `run.start` path. Two valid patterns for build output directories:
  - **Directory preserved** (NO tilde): `deployFiles: [dist, package.json]` → files at `/var/www/dist/` → `start: bun dist/index.js`
  - **Contents extracted** (WITH tilde): `deployFiles: dist/~` → files at `/var/www/` → `start: bun index.js`
  REASON: tilde strips the directory prefix. If start command references the subdirectory (e.g., `dist/index.js`), tilde BREAKS it because the file is at `/var/www/index.js`, not `/var/www/dist/index.js`. Use tilde for static sites (no start command) or when start command matches the flattened layout
- **NEVER** use `initCommands` for package installation. REASON: initCommands run on every container restart; use `prepareCommands` for one-time setup
- **ALWAYS** use `--no-cache-dir` for pip in containers. REASON: prevents wasted disk space on ephemeral containers
- **ALWAYS** use `--ignore-platform-reqs` for Composer on Alpine. REASON: musl libc may not satisfy platform requirements checks
- **ALWAYS** require a git repository before `zerops_deploy`. REASON: `zcli push` requires git. Run `git init && git add -A && git commit -m "deploy"` first

## Base Image & OS

- **NEVER** use `apt-get` on Alpine. REASON: Alpine uses `apk`; apt-get doesn't exist. "command not found"
- **NEVER** use `apk` on Ubuntu. REASON: Ubuntu uses `apt-get`; apk doesn't exist
- **ALWAYS** use `sudo apk add --no-cache` on Alpine. REASON: prevents stale package index caching; sudo required as containers run as `zerops` user
- **ALWAYS** use `sudo apt-get update && sudo apt-get install -y` on Ubuntu. REASON: package index not pre-populated; sudo required as containers run as `zerops` user
- **NEVER** set `run.base: alpine@*` for Go. REASON: causes glibc/musl mismatch for CGO-linked binaries → 502. Omit `run.base` or use `run.base: go@latest`
- **ALWAYS** use `os: ubuntu` for Deno and Gleam. REASON: these runtimes are not available on Alpine

## Environment Variables

- **NEVER** re-reference project-level env vars in service vars. REASON: project vars are auto-inherited; creating a service var with the same name shadows the project var
- **ALWAYS** use `envSecrets` for passwords, tokens, API keys. REASON: blurred in GUI by default, proper security practice
- **ALWAYS** use cross-service reference syntax `${hostname_varname}` (dashes→underscores). REASON: this is the only way to wire services; direct values break on service recreation
- **NEVER** rely on GUI password changes updating env vars. REASON: changing DB password in GUI does NOT update connection string env vars (manual sync required)

## Import & Service Creation

- **ALWAYS** use `valkey@7.2` (not `valkey@8`). REASON: v8 passes dry-run validation but fails actual import
- **ALWAYS** set explicit `mode: NON_HA` or `mode: HA` for managed services (DB, cache, shared-storage). REASON: omitting mode passes dry-run but fails real import with "Mandatory parameter is missing"
- **NEVER** set `minContainers`/`maxContainers` for managed services. REASON: managed services have fixed container counts (NON_HA=1, HA=3); setting these causes import failure
- **NEVER** set `verticalAutoscaling` for shared-storage or object-storage. REASON: these service types don't support vertical scaling; setting it causes import failure
- **ALWAYS** set `priority: 10` for databases/storage services. REASON: ensures they start before application services that depend on them
- **ALWAYS** use `enableSubdomainAccess: true` in import.yml instead of calling `zerops_subdomain` after. REASON: calling subdomain API on READY_TO_DEPLOY service returns an error
- **NEVER** use Docker `:latest` tag. REASON: cached and won't re-pull; always use specific version tags
- **ALWAYS** use `--network=host` for Docker services. REASON: without it, container cannot receive traffic from Zerops routing
- **ALWAYS** use `forcePathStyle: true` / `AWS_USE_PATH_STYLE_ENDPOINT: true` for Object Storage. REASON: MinIO backend doesn't support virtual-hosted style

## Runtime-Specific

- **ALWAYS** set `server.address=0.0.0.0` for Java Spring Boot. REASON: Spring Boot defaults to localhost binding → unreachable from LB
- **ALWAYS** set `TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"` for PHP Laravel. REASON: reverse proxy breaks CSRF validation without trusted proxy config
- **ALWAYS** set `CSRF_TRUSTED_ORIGINS` for Django behind proxy. REASON: reverse proxy changes the origin header; Django blocks requests
- **ALWAYS** set `PHX_SERVER=true` for Phoenix/Elixir releases. REASON: without it, the HTTP server doesn't start in release mode
- **ALWAYS** use `cargo b --release` for Rust. REASON: debug builds are 10-100x slower
- **ALWAYS** use `CGO_ENABLED=0 go build` when unsure about CGO dependencies. REASON: produces static binary compatible with any base
- **ALWAYS** use `sudo apk add --no-cache php84-<ext>` for Alpine PHP extensions. REASON: version prefix must match PHP major+minor (e.g., php84-redis for PHP 8.4); sudo required

## Scaling & Platform

- **NEVER** attempt to change HA/NON_HA mode after creation. REASON: mode is immutable; must delete and recreate service
- **NEVER** attempt to change hostname after creation. REASON: hostname is immutable; it becomes the internal DNS name
- **NEVER** expect disk to shrink. REASON: auto-scaling only increases disk; to reduce, recreate the service
- **ALWAYS** use `zsc execOnce <key> -- <cmd>` for migrations in HA. REASON: prevents duplicate execution across multiple containers
- **NEVER** modify `zps`/`zerops`/`super` system users in managed services. REASON: these are system maintenance accounts

## Event Monitoring

- **ALWAYS** filter `zerops_events` by `serviceHostname`. REASON: project-level events include stale builds from other services
- **NEVER** keep polling after `stack.build` shows `FINISHED`. REASON: FINISHED means build is complete; `appVersion` ACTIVE means deployed and running
- **ALWAYS** check `stack.build` process for build status, NOT `appVersion`. REASON: these are different events; `appVersion` ACTIVE ≠ still building

## See Also
- zerops://foundation/platform-model — conceptual model (WHY these rules exist)
- zerops://foundation/grammar — YAML schema reference
- zerops://foundation/runtimes — runtime-specific deltas
- zerops://guides/deployment-lifecycle — build/deploy pipeline details
