# KeyDB on Zerops

## Keywords
keydb, redis, cache, deprecated, multithreaded redis, key-value

## TL;DR
**KeyDB is deprecated on Zerops** — development activity has stalled. Use Valkey for all new Redis-compatible deployments.

## Deprecation Notice
KeyDB development has slowed significantly. **Valkey is the recommended replacement** — it's actively maintained and fully Redis-compatible.

## Zerops-Specific Behavior
- Port: **6379** (fixed)
- Redis-compatible: Same wire protocol
- Env vars: `hostname`, `port`, `password`, `connectionString`

## Migration to Valkey
1. Create a new Valkey service
2. Migrate data using `redis-cli --pipe` or application-level migration
3. Update connection strings (same protocol, different hostname)
4. Delete KeyDB service

## Configuration
```yaml
# import.yaml — DON'T USE FOR NEW PROJECTS
services:
  - hostname: cache
    type: keydb@6
    mode: NON_HA
```

## Gotchas
1. **Use Valkey instead**: KeyDB is deprecated — no guarantee of future updates
2. **Same port as Valkey**: Migration is straightforward — only hostname changes

## See Also
- zerops://decisions/choose-cache
- zerops://services/valkey
