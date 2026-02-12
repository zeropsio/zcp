# Common Gotchas on Zerops

## Keywords
gotchas, common mistakes, troubleshooting, errors, problems, pitfalls, debugging, faq

## TL;DR
The top 3 Zerops gotchas: (1) never use HTTPS internally, (2) never listen on port 443, (3) always use Cloudflare Full (strict) SSL mode.

## Networking

1. **HTTPS for internal communication**: Use `http://hostname:port` between services — SSL terminates at the L7 balancer
2. **Listening on port 443**: Your app should listen on a standard port (3000, 8080) — Zerops handles SSL on 443
3. **Shared IPv4 + Cloudflare proxy**: Reverse AAAA lookup fails — use DNS-only or dedicated IPv4 with Cloudflare proxy
4. **Cloudflare SSL "Flexible"**: Causes redirect loops — always use "Full (strict)"
5. **Shared IPv4 without AAAA record**: SNI routing requires both A and AAAA records

## Build & Deploy

6. **`cache: false` expectation**: Files outside `/build/source` (Go modules, pip packages) remain cached regardless
7. **`prepareCommands` change**: Invalidates BOTH cache layers — expect longer rebuild
8. **`initCommands` for installation**: Runs on every container restart — use `prepareCommands` for package installation
9. **Missing `node_modules` in deployFiles**: Node.js apps without bundler need `node_modules` in deploy artifacts
10. **Docker `:latest` tag**: Cached and won't re-pull — always use specific version tags
11. **Docker without `--network=host`**: Container cannot receive traffic from Zerops routing
12. **`ci skip` in commit message**: Prevents pipeline trigger — case-insensitive, works with `skip ci` too

## Environment Variables

13. **Re-referencing project vars**: They're auto-available — creating a service var with same name shadows the project var
14. **Password sync**: Changing DB password in GUI doesn't update env vars (and vice versa) — sync manually
15. **Env vars via VPN**: Not available through VPN — use GUI or API to read them

## Databases

16. **HA mode immutable**: Cannot switch HA/NON_HA after creation — delete and recreate
17. **TLS for internal connections**: Never use SSL/TLS internally or via VPN — VPN already encrypts
18. **`postgresql://` vs `postgres://`**: Some libraries need `postgres://` — create a custom env var
19. **Modifying `zps` user**: System maintenance account — never change or delete
20. **Read replica lag**: PostgreSQL/Valkey HA uses async replication — don't read immediately after write on replica port

## CDN & Caching

21. **Cache-Control headers**: HTTP headers don't affect Zerops CDN (fixed 30-day TTL) — they only affect browsers
22. **Wildcard domains on static CDN**: `*.domain.com` not supported for static CDN

## Platform

23. **Core upgrade downtime**: Lightweight → Serious causes ~35s network unavailability
24. **Hostname immutable**: Cannot change service hostname after creation — delete and recreate
25. **1-hour build limit**: Build terminated after 60 minutes — optimize slow builds
26. **Docker vertical scaling**: Triggers VM restart — expect brief downtime
27. **Disk only grows**: Auto-scaling only increases disk — to reduce, recreate the service
28. **Subdomain on undeployed service**: `zerops_subdomain enable` fails with "Service stack is not http or https" on READY_TO_DEPLOY services — use `enableSubdomainAccess: true` in import YAML instead
29. **Valkey version**: Only `valkey@7.2` works — `valkey@8` passes dry-run but fails at actual import
30. **`mode` mandatory for databases/caches**: PostgreSQL, MariaDB, Valkey, KeyDB, shared-storage require explicit `mode: NON_HA` or `mode: HA` — omitting it passes dry-run but fails real import with "Mandatory parameter is missing"

## Deploy & Runtime

31. **`dist/~` tilde syntax**: Deploys directory **contents**, not the folder itself. Without `~`, you get nested `dist/dist/`
32. **Ghost CMS maxContainers**: Must be 1 — Ghost cannot scale horizontally (uses SQLite/local state)
33. **Next.js static export**: Requires `output: 'export'` in `next.config.mjs` — without it, `next build` produces SSR output
34. **SvelteKit static**: Requires `@sveltejs/adapter-static` + `export const prerender = true` in root layout
35. **Phoenix releases**: `PHX_SERVER=true` env var required to start the HTTP server in release mode
36. **Java Spring bind address**: `server.address=0.0.0.0` required — default binds to localhost only, unreachable from Zerops routing
37. **Python pip in containers**: Use `--no-cache-dir` flag and `build.addToRunPrepare` to persist packages in runtime
38. **Django/Laravel behind proxy**: Configure `CSRF_TRUSTED_ORIGINS` (Django) or `TrustedProxies` middleware (Laravel) — reverse proxy breaks CSRF validation
39. **Symfony sass-bundle**: Put `symfonycasts/sass-bundle` in `require` not `require-dev` — needed at runtime on Alpine
40. **`AWS_USE_PATH_STYLE_ENDPOINT: true`**: Required for Zerops Object Storage (MinIO) — virtual-hosted style does not work

## See Also
- zerops://networking/overview
- zerops://networking/cloudflare
- zerops://platform/build-cache
- zerops://platform/env-variables
- zerops://services/_common-database
- zerops://config/deploy-patterns
- zerops://operations/production-checklist
