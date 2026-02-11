# MariaDB on Zerops

## Keywords
mariadb, mysql, sql, relational, database, maxscale, replication, 3306, mysql compatible

## TL;DR
MariaDB on Zerops uses MaxScale for HA routing with async replication on port 3306; choose it only when you specifically need MySQL wire protocol compatibility.

## Zerops-Specific Behavior
- Versions: 10.6
- Port: **3306** (fixed, no separate replica port)
- HA routing: MaxScale (smart read/write splitting)
- Replication: Async
- Default database: Named after service hostname
- Env vars: `hostname`, `port`, `user`, `password`, `connectionString`

## HA Mode
- MaxScale routing layer handles read/write splitting
- Async replication across nodes
- Automatic failover

## Connection Pattern
```
# Internal
mysql://${user}:${password}@${hostname}:3306/${dbname}
```

## Configuration
```yaml
# import.yaml
services:
  - hostname: db
    type: mariadb@10.6
    mode: HA
```

## Gotchas
1. **No separate replica port**: Unlike PostgreSQL, MariaDB uses MaxScale for routing — single port 3306
2. **HA mode is immutable**: Cannot change after creation
3. **No internal TLS**: Use plain TCP for internal connections
4. **Don't modify `zps` user**: System maintenance account

## See Also
- zerops://decisions/choose-database
- zerops://services/_common-database
- zerops://services/postgresql
- zerops://examples/connection-strings
