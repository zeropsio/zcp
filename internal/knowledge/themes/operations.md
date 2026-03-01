# Zerops Operations & Production

## TL;DR
Operational guides covering networking, CI/CD, logging, monitoring, scaling, production hardening, and service selection decisions. Covers everything beyond core YAML configuration.

## Keywords
operations, networking, cloudflare, firewall, vpn, ssh, logging, monitoring, ci-cd, github actions, scaling, production, checklist, smtp, cdn, debug, backup, rbac, choose, database, cache, queue, search, object storage, s3

## Networking

Every Zerops project gets an isolated VXLAN private network. Services discover each other by hostname: `http://hostname:port`. Hostnames are immutable. No cross-project internal communication.

### L7 Balancer
- 4000 worker connections, 30-second keepalive timeout
- Max upload: 512 MB (custom domain), 50 MB (zerops.app subdomain)
- Round-robin load balancing with health checks
- 2 HA containers per project for custom domain access

### Public Access

| IP Type | Cost | Protocol | Notes |
|---------|------|----------|-------|
| Shared IPv4 | Free | HTTP/HTTPS only | Limited connections, shorter timeouts |
| Dedicated IPv4 | $3/30 days | All protocols | Non-refundable, auto-renews |
| IPv6 | Free | All protocols | Dedicated per project |

DNS: Point `A` record to Dedicated IPv4, `AAAA` record to IPv6. **Shared IPv4 requires both A and AAAA records** for SNI routing.

### Cloudflare Integration

**SSL/TLS mode must be Full (strict).** "Flexible" causes infinite redirect loops.

| IP Type | Record | Proxy |
|---------|--------|-------|
| IPv6 only | `AAAA` | Proxied |
| Dedicated IPv4 | `A` | Proxied |
| Shared IPv4 | **Not recommended** | Reverse AAAA issues |

Settings: Enable "Always Use HTTPS". WAF exception for `/.well-known/acme-challenge/`. Cloudflare Free plan does not proxy wildcard subdomains.

### Firewall

TCP ports 1-1024 restricted. Allowed: 22 (SSH), 53 (DNS), 80 (HTTP), 123 (NTP), 443 (HTTPS), 587 (SMTP/STARTTLS). Blocked: 25 (SMTP), 465 (SMTPS deprecated), all other 1-1024. UDP ports: no restrictions. TCP 1025-65535: no restrictions.

### VPN Access

Services accessible by appending `.zerops`: `db.zerops`, `api.zerops`. Only one project VPN at a time. **Env vars NOT available through VPN.**

### SSH Isolation

`sshIsolation` rules: `vpn` (default), `project`, `service`, `service@<name>`. Block rules with `-` prefix. **SSHFS mounts require `vpn project`**. Web Terminal always works. SSH isolation is immutable after creation.

## CI/CD Integration

**Webhook Triggers (GitHub/GitLab):** Setup via GUI: Service detail -> Build, Deploy, Run Pipeline Settings -> Connect with repository. Trigger: **New tag** (optional regex) or **Push to branch**.

**GitHub Actions:**
```yaml
name: Deploy
on: push
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: zeropsio/actions@main
        with:
          access-token: ${{ secrets.ZEROPS_TOKEN }}
          service-id: <service-id>
```

Include `ci skip` or `skip ci` in commit message to prevent triggering.

## Logging & Monitoring

Three log types: build, prepare runtime, and runtime. Use syslog format for severity filtering; plain stdout appears as "info".

**Log Forwarding**: Better Stack, Papertrail, self-hosted ELK. Zerops forwards via **UDP syslog**. Custom syslog-ng **must** use source name `s_src` (not `s_sys`).

**Prometheus**: Expose `/metrics` endpoint, set `ZEROPS_PROMETHEUS_PORT=8080`.

**ELK APM**: Set `ELASTIC_APM_ACTIVE: "true"`, `ELASTIC_APM_SERVER_URL`, `ELASTIC_APM_SECRET_TOKEN`.

## SMTP

Only port **587** (STARTTLS) is allowed. Ports 25 and 465 are permanently blocked.

## CDN

6 global regions. DNS TTL: 30 seconds. Object Storage CDN: `${storageCdnUrl}`. Static CDN: `${staticCdnUrl}` -- wildcard domains NOT supported. Cache TTL fixed at 30 days. Purge: `/*` (all), `/dir/*` (directory), `/file$` (exact).

## Scaling

**CPU**: Shared (1/10 to 10/10 core) or Dedicated. Mode change limited to once per hour. Max 40 cores.
**RAM**: Min step 0.125 GB, max 32 GB. Dual-threshold: `minFreeRamGB` (absolute) and `minFreeRamPercent` (percentage).
**Disk**: Min step 0.5 GB, max 128 GB. **Disk can only grow.** Recreate to reduce.
**Horizontal**: 1-10 containers. HA databases: fixed 3 nodes.

## RBAC

Four user roles: Owner > Admin > Developer > Guest. Project access: Full or Read Only (env vars REDACTED). Integration tokens cannot exceed creator's permissions.

## Object Storage Integration

S3-compatible, backed by MinIO. One bucket per service, 1-100 GB quota. `forcePathStyle: true` REQUIRED. Region `us-east-1` (required but ignored).

**Code examples** (PHP, Node.js, Python, Java) all require path-style configuration. Use `AWS_USE_PATH_STYLE_ENDPOINT: true` for framework integrations.

## Production Checklist

### Database High Availability
HA mode is **immutable after creation**. To go HA, delete and recreate.

| Item | Dev | Production |
|------|-----|------------|
| Mode | `NON_HA` | `HA` |
| Backups | Optional | Enabled |
| Connection | Single primary | Primary + read replicas |

### Application Scaling
Set `minContainers: 2` or higher for zero-downtime deploys. Enable health checks.

### Remove Development Services
Remove Mailpit and Adminer. Replace with production SMTP and VPN-based DB access.

### Persistent File Storage
Use Object Storage (S3) for any files that must survive restarts. Containers are volatile.

### Session and Cache
Use Valkey for sessions and cache when running multiple containers.

### Framework Production Settings
- **Laravel**: `APP_ENV: production`, `APP_DEBUG: "false"`, `TRUSTED_PROXIES: 127.0.0.1,10.0.0.0/8`
- **Symfony**: `APP_ENV: prod`, `TRUSTED_PROXIES: 127.0.0.1,10.0.0.0/8`
- **Django**: `DEBUG: "false"`, `CSRF_TRUSTED_ORIGINS: https://your-domain.com`
- **Spring Boot**: `server.address: 0.0.0.0`, set `-Xmx` to ~75% container RAM
- **Phoenix**: `PHX_SERVER: "true"`, `SECRET_KEY_BASE` via preprocessor

### DNS and SSL
Cloudflare with **Full (strict)** SSL. WAF exception for ACME challenge. Both A and AAAA records for shared IPv4.

## Service Selection Decisions

### Choose Database
**Default: PostgreSQL.** Full HA, read replicas, pgBouncer.

| Need | Choice | Reason |
|------|--------|--------|
| General-purpose relational | **PostgreSQL** | Full HA (3 nodes), read replicas, pgBouncer |
| MySQL wire-protocol compat | **MariaDB** | MaxScale routing, async replication |
| Analytics / OLAP / time-series | **ClickHouse** | Columnar storage, ReplicatedMergeTree |

### Choose Cache
**Default: Valkey.** KeyDB is deprecated.

| Need | Choice | Reason |
|------|--------|--------|
| Any caching need | **Valkey** | Active development, full HA, Redis-compatible |
| Legacy KeyDB migration | **KeyDB** | Same wire protocol; only hostname changes |

### Choose Queue
**Default: NATS** for most use cases.

| Need | Choice | Reason |
|------|--------|--------|
| General messaging | **NATS** | Simple auth, JetStream, fast |
| Enterprise event streaming | **Kafka** | SASL auth, 3-broker HA, ordering |
| ~~AMQP (RabbitMQ)~~ | **NATS** | RabbitMQ deprecated — use NATS with JetStream |

### Choose Search
**Default: Meilisearch** for simple full-text.

| Need | Choice | Reason |
|------|--------|--------|
| Simple full-text | **Meilisearch** | Instant setup, typo-tolerant |
| Advanced queries + HA | **Elasticsearch** | Cluster, plugins, JVM tuning |
| Autocomplete + HA | **Typesense** | 3-node Raft, CORS built-in |
| Vector / AI similarity | **Qdrant** | gRPC + HTTP, cluster replication |

### Choose Runtime Base
- Go, Rust: compile to static binary, no `run.base` needed
- Python, Ruby: same runtime for build and run
- PHP: build `php@X`, run `php-nginx@X` (different bases)
- Elixir: build `elixir@X`, run `alpine@latest` (compiled release)
- Static sites: build `nodejs@22`, run `static`

## Tool Access Patterns

The agent has two execution contexts: the **MCP server** (ZCP) and **runtime containers** (via SSH). Most operations are done through MCP tools — SSH is only for code editing, server management, and deployment.

**Tool availability by location:**

| Tool | MCP server (ZCP) | Dev containers (via SSH) |
|------|----|----|
| MCP tools (zerops_discover, zerops_logs, zerops_manage, zerops_scale, ...) | Yes (primary) | No |
| jq, yq | Yes | No |
| psql, redis-cli, mysql | Yes | No |
| curl, wget | Yes | Yes |
| netstat, ss | No | Yes |
| zcli | No | Yes (deployment only) |

**zcli is ONLY available inside dev containers** and ONLY used for `zcli push` to deploy to stage. All other operations (logs, scaling, restart, discovery, env vars) use MCP tools directly.

**Correct piping pattern**: `ssh appdev "curl -s localhost:8080/api" | jq .` — pipe OUTSIDE SSH, not inside.
**Wrong**: `ssh appdev "curl -s localhost:8080/api | jq ."` — jq doesn't exist in the container.

**Database access**: Run from ZCP directly (MCP tools or direct CLI), never via SSH:
- `psql "$db_connectionString"` (from ZCP)
- `redis-cli -u "$cache_connectionString"` (from ZCP)

**Deployment from dev container** (source files live there):
- `ssh appdev "cd /var/www && zcli push <stage_service_id>"`

## Dev vs Stage

| Aspect | Dev service | Stage service |
|--------|------------|---------------|
| Purpose | Debug, iterate, fix errors | Final validation before production |
| Server start | Manual via SSH | Auto-starts on deploy (`run.start` in zerops.yml) |
| Code placement | Source in SSHFS `/var/www/{dev}/` | Built artifact deployed via `zcli push` (from dev container) |
| Fix errors | HERE — do not deploy broken code | Never — go back to dev, fix, redeploy |
| Logs | `TaskOutput task_id=... block=false` (from background SSH task) | `zerops_logs` MCP tool |
| Deployment | N/A (code runs directly) | `ssh appdev "cd /var/www && zcli push <stage_id>"` |

**Log access differs because**: in dev, the agent starts the server via SSH with `run_in_background=true` — output streams to `TaskOutput`. In stage, Zerops manages the process — use `zerops_logs` MCP tool.

**zcli usage**: `zcli` is available ONLY inside dev containers. The only `zcli` command the agent uses is `zcli push` to deploy from dev to stage. All other operations (logs, scaling, restarts, env vars, discovery) are done through MCP tools (`zerops_logs`, `zerops_scale`, `zerops_manage`, `zerops_env`, `zerops_discover`).

## Verification & Attestation

Verification is attestation-based: the agent verifies using tools, then records what was verified. Automated testing is NOT a substitute.

**Good attestations (specific, provable):**
- `"tsc passed, /health returns 200 with {status:ok}, tail -30 logs clean"`
- `"go build -n clean, /events returns text/event-stream header, no errors in log"`
- `"process running (ps aux), processed 3 test messages, no panics in log"`

**Bad attestations (vague, will fail gate):**
- `"looks good"` / `"tested"` / `"works"`

**Verification tools by runtime:**

| Runtime | Type check | HTTP check | Log check |
|---------|-----------|------------|-----------|
| Go | `ssh dev "go build -n ."` | `curl -s http://localhost:8080/` | `TaskOutput task_id=... block=false` |
| Bun | `ssh dev "bun x tsc --noEmit"` | same | same |
| Node.js | `ssh dev "npx tsc --noEmit"` | same | same |
| Python | `ssh dev "python -m py_compile *.py"` | same | same |

**HTTP 200 is NOT sufficient** — always check:
1. Response content (not just status code)
2. Content-Type headers (e.g., `text/event-stream` for SSE)
3. Logs for errors/warnings after request

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Service stuck in `READY_TO_DEPLOY` | Missing `buildFromGit` or `startWithoutCode` in import.yml | Delete service, fix import.yml, re-import |
| HTTP 000 (connection refused) | Server not running on dev service | Start the server via SSH first |
| SSH hangs after starting server | Expected on Zerops — SSH session stays open while server runs | Use `run_in_background=true` on the Bash call. Read output via `TaskOutput task_id=... block=false`. Stop with `TaskStop`. |
| SSH repeatedly fails | Container OOM/restarting | Check: `zerops_logs` MCP tool, scale: `zerops_scale` MCP tool |
| `jq: command not found` via SSH | jq not available inside containers | Pipe outside: `ssh dev "curl ..." \| jq .` |
| `psql: command not found` via SSH | DB tools only on ZCP | Run from ZCP: `psql "$db_connectionString"` |
| `https://https://...` double protocol | `subdomainUrls` from enable already include `https://` | Use the URL directly, don't prepend |
| HTTP 502 despite app running + subdomain enabled | Routing not activated or wrong URL | First verify internally: `curl http://{hostname}:{port}/health`. If internal works, call `zerops_subdomain action="enable"` — use `subdomainUrls` from the response |
| SSHFS stale after deploy | Container replaced during deploy (runtime-1→2→3) | SSHFS auto-reconnects after deploy — no explicit remount needed. Only truly stale during stop (container not running). |
| Logs flooded with SSH isolation warnings | sshIsolation: vpn rejects non-VPN connections | Normal behavior. Filter with `search="exec"` or `search="listening"` to find app logs |
| Gate fails after verification | Empty or vague attestation | Include specific description of what was verified |
| Empty env variable | Variable not discovered or wrong name | Check `zerops_discover includeEnvs=true` first |
