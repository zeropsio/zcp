# Zerops Services Reference

## Runtime services
nodejs@22, php@8.3, python@3.12, go@1.22, rust@1.78, java@21, dotnet@8, elixir@1.17, gleam@1, bun@1, deno@1

## Database services
postgresql@16 (default), mariadb@11, clickhouse@24

## Cache services
valkey@7.2 (default, redis-compatible), keydb@6 (deprecated)

## Search services
meilisearch@1.10 (default), elasticsearch@8, typesense@27, qdrant@1 (internal-only)

## Queue services
nats@2.10 (default), kafka@3

## Storage services
object-storage (S3/MinIO), shared-storage (POSIX)

## Web services
nginx, static (SPA-ready)

## Defaults (use unless user specifies otherwise)
- postgresql@16, valkey@7.2, meilisearch@1.10, nats@2.10
- alpine base, NON_HA, SHARED CPU

## Networking rules
- Internal: ALWAYS http://, NEVER https:// (SSL terminates at L7 balancer)
- Ports: 10-65435 only (0-9 and 65436+ reserved)
- Cross-service env refs: ${service_hostname} (underscore, not dash)
- Cloudflare: MUST use "Full (strict)" SSL mode
- No localhost — services communicate via hostname
