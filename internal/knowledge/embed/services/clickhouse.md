# ClickHouse on Zerops

## Keywords
clickhouse, columnar, analytics, olap, replicated, merge tree, time series, sql analytics, data warehouse

## TL;DR
ClickHouse on Zerops requires `ReplicatedMergeTree` engine in HA mode (3 nodes, replication factor 3) and exposes 4 protocol ports including MySQL and PostgreSQL wire compatibility.

## Zerops-Specific Behavior
- Ports:
  - **9000** — Native TCP (`port` / `portNative`)
  - **8123** — HTTP/HTTPS (`portHttp`)
  - **9004** — MySQL protocol (`portMysql`)
  - **9005** — PostgreSQL protocol (`portPostgresql`)
- Default users: `zerops` (app user), `super` (admin/cluster management)
- Default database: Named after service hostname
- Env vars: `password` (zerops user), `superUserPassword` (admin)

## HA Mode (3 nodes)
- Replication factor: 3
- Default cluster name: `zerops` (1 shard, 3 replicas)
- **Must use `Replicated` database engine** and `ReplicatedMergeTree` table engine
- Node access: `node-stable-<1..3>.db.<hostname>.zerops:<port>`
- Automatic monitoring and node repair

## NON_HA Mode
- Single node, no replication
- Data loss risk due to container volatility
- Development/testing only

## Configuration
```yaml
# import.yaml
services:
  - hostname: analytics
    type: clickhouse@25.3
    mode: HA
```

## Backup
Use SQL `BACKUP ALL` command with super user credentials. Format: `tar.gz`.

## Gotchas
1. **HA requires ReplicatedMergeTree**: Standard `MergeTree` won't replicate — data loss on node failure
2. **4 protocol ports**: Choose the right one — native (9000) for CLI/drivers, HTTP (8123) for REST, MySQL/PostgreSQL for compatibility
3. **Super user required for backups**: The `zerops` user doesn't have backup permissions

## See Also
- zerops://decisions/choose-database
- zerops://services/_common-database
- zerops://platform/backup
