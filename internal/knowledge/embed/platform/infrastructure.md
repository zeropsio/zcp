# Infrastructure on Zerops

## Keywords
infrastructure, project, service, container, vxlan, lightweight, serious, core, ipv4, ipv6, pipeline, build, deploy, readiness check

## TL;DR
Zerops uses Incus-based full Linux containers (not serverless), with private VXLAN networking per project and two core plans (Lightweight and Serious).

## Hierarchy
**Project → Services → Containers**

Each project gets an isolated VXLAN private network. Services communicate internally via hostname.

## Core Plans

| Feature | Lightweight | Serious |
|---------|------------|---------|
| Infrastructure | Single container | Multi-container (HA) |
| Build time | 15 hours | 150 hours |
| Backup storage | 5 GB | 25 GB |
| Egress | 100 GB | 3 TB |
| IPv6 | Free, dedicated | Free, dedicated |
| Shared IPv4 | Free | Free |
| Dedicated IPv4 | $3/30 days | $3/30 days |

### Core Upgrade (Lightweight → Serious)
- Cost: $10 one-time
- **Partially destructive**: ~35 seconds network unavailability (can be longer)
- Resets free resource limits
- IP addresses unchanged

## Build Pipeline

### Build Container Resources
- CPU: 1-5 cores (scales as needed)
- RAM: 8 GB (fixed)
- Disk: 1-100 GB
- **Time limit: 1 hour** (then terminated)

### Deploy Phases
1. **Build** — creates artifact
2. **Runtime prepare** (optional) — creates custom runtime image
3. **Deploy** — deploys to containers

### Application Versions
- Keeps 10 most recent versions
- Can restore any previous version

### Readiness Checks
- `httpGet`: URL must return 2xx (5s timeout, follows 3xx redirects)
- `exec.command`: Must return exit code 0 (5s timeout)
- Retry window: 5 minutes, then container marked as failed

## Network
- Private VXLAN per project (isolated)
- Internal: `http://hostname:port` (never HTTPS internally)
- Shared IPv4: Free, HTTP/HTTPS only, limited connections, shorter timeouts
- Dedicated IPv4: $3/30 days, full protocol support

## CI Skip
Include `ci skip` or `skip ci` in commit message (case-insensitive) to skip pipeline trigger.

## Gotchas
1. **Core upgrade causes downtime**: ~35s network unavailability during Lightweight → Serious upgrade
2. **1-hour build limit**: Build is terminated after 60 minutes — optimize slow builds
3. **10 versions kept**: Older versions are automatically deleted
4. **Hostname is immutable**: Cannot change after service creation — delete and recreate

## See Also
- zerops://platform/scaling
- zerops://platform/backup
- zerops://networking/overview
- zerops://config/zerops-yml
