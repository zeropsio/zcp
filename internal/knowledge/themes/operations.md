# Zerops Operations & Production

Operational guides covering networking, CI/CD, logging, monitoring, scaling, and production hardening. Covers everything beyond core YAML configuration.

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
name: Deploy to Zerops
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install zcli
        run: |
          curl -sSL https://zerops.io/zcli/install.sh | sh
          echo "$HOME/.local/bin" >> $GITHUB_PATH
      - name: Deploy
        run: zcli push --serviceId <service-id> --setup <setup-name>
        env:
          ZEROPS_TOKEN: ${{ secrets.ZEROPS_TOKEN }}
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
Use Object Storage (S3) for any files that must survive deploys or container replacement. Local filesystem survives restarts but is lost on deploy.

### Session and Cache
Use Valkey for sessions and cache when running multiple containers.

### Framework Production Settings
- Set the framework's production mode flag (disables debug output, stack traces, verbose errors)
- Configure proxy trust settings — Zerops L7 balancer terminates SSL, so frameworks with CSRF/origin validation need to trust the proxy (check framework docs for the specific setting)
- Bind `0.0.0.0` — frameworks that default to localhost must be reconfigured
- For JVM-based runtimes, set max heap (`-Xmx`) to ~75% of container RAM to leave room for native memory

### DNS and SSL
Cloudflare with **Full (strict)** SSL. WAF exception for ACME challenge. Both A and AAAA records for shared IPv4.

