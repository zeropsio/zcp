# Common Database Patterns on Zerops

## Keywords
database, common, ha, high availability, replication, connection, internal, tls, backup, database patterns

## TL;DR
All Zerops databases share common patterns: HA mode is immutable after creation, internal connections use HTTP (no TLS), and each has auto-generated credentials in env vars.

## Connection Pattern
- Internal: `protocol://hostname:port` (no TLS, no HTTPS)
- External: Use TLS ports where available (e.g., PostgreSQL 6432, Valkey 6380)
- VPN: Same as internal — hostname resolves via VPN DNS (append `.zerops` if needed)

## HA Mode
- Set at creation time — **cannot be changed later**
- HA = multiple nodes with automatic failover
- NON_HA = single node, no automatic recovery
- Database horizontal scaling is fixed (not auto-scaled like runtimes)

## Auto-Generated Credentials
Every database service generates env vars:
- `hostname` — internal hostname
- `port` — primary port
- `user` / `password` — auto-generated credentials
- `connectionString` — ready-to-use connection string (format varies by DB)

## Cross-Service Reference
```yaml
envVariables:
  DATABASE_URL: ${db_connectionString}
  DB_HOST: ${db_hostname}
  DB_PASSWORD: ${db_password}
```

## Backup
Supported for: PostgreSQL, MariaDB, Elasticsearch, Meilisearch, Qdrant, NATS.
Not supported for: Valkey, KeyDB (in-memory), ClickHouse (use SQL BACKUP).

## Gotchas
1. **HA is immutable**: Cannot change after creation — must delete and recreate
2. **No internal TLS**: Never use SSL/TLS for internal or VPN connections — VPN already encrypts
3. **Password sync is manual**: Changing password in DB GUI doesn't update env vars
4. **Don't modify `zps` user**: System maintenance account — never change or delete
5. **VPN hostname resolution**: Append `.zerops` to hostname when connecting via VPN

## See Also
- zerops://decisions/choose-database
- zerops://platform/backup
- zerops://platform/env-variables
- zerops://networking/overview
