# Valkey on Zerops

## Keywords
valkey, redis, cache, key-value, in-memory, session, 6379, 6380, 7000, 7001, redis alternative

## TL;DR
Valkey is the recommended Redis replacement on Zerops; HA mode uses custom traffic forwarding (not native Valkey) where ports 6379/6380 on replicas forward to master.

## Zerops-Specific Behavior
- Versions: **7.2** (use `valkey@7.2` — other versions may pass validation but fail at import)
- Ports:
  - **6379** — Read/write, non-TLS (master)
  - **6380** — Read/write, TLS (master)
  - **7000** — Read-only, non-TLS (replicas, HA only)
  - **7001** — Read-only, TLS (replicas, HA only)
- Redis-compatible: Drop-in replacement for all Redis clients
- Env vars: `hostname`, `port`, `password`, `connectionString`

## HA Mode (3 nodes)
- 1 master + 2 replicas
- **Zerops-specific forwarding**: Ports 6379/6380 on replica nodes forward traffic to current master (not native Valkey behavior)
- Ports 7000/7001 are local to each replica (direct read access)
- Automatic master promotion on failure
- DNS updated seamlessly
- Replication: **Async** (brief data loss possible on failover)

## NON_HA Mode
- Single node on ports 6379 (non-TLS) and 6380 (TLS)
- No backup beyond infrastructure reliability
- Data persists unless hardware node fails

## Connection Pattern
```
# Internal (non-TLS)
redis://${user}:${password}@${hostname}:6379

# Internal (TLS)
rediss://${user}:${password}@${hostname}:6380

# Read replicas (HA only)
redis://${user}:${password}@${hostname}:7000
```

## Configuration
```yaml
# import.yaml
services:
  - hostname: cache
    type: valkey@7.2
    mode: HA
```

## Gotchas
1. **Version must be 7.2**: `valkey@8` passes dry-run validation but fails at actual import — always use `valkey@7.2`
2. **Port forwarding is Zerops-specific**: Replicas forward 6379/6380 to master — not standard Redis/Valkey behavior
3. **Async replication**: Brief data loss possible during master failover
4. **Use port 7000/7001 for read scaling**: Direct replica access, no forwarding
5. **TLS only for external/VPN**: Don't use TLS ports (6380/7001) for internal service-to-service communication

## See Also
- zerops://decisions/choose-cache
- zerops://services/keydb
- zerops://services/_common-database
- zerops://examples/connection-strings
