# Zerops Platform

## Context

**Zerops** is a PaaS. Each project has services (containers) on a shared private network.

**ZCP** (Zerops Control Plane) is your workspace — a privileged Ubuntu container with:
- SSHFS mounts to all service filesystems
- SSH access to execute inside any container
- Direct network access to all services
- Full tooling: `rg`, `fd`, `bat`, `jq`, `yq`, `http`, `psql`, `mysql`, `redis-cli`, `agent-browser`

## Your Position

| Access | Method | Example |
|--------|--------|---------|
| Files | SSHFS mount | `/var/www/{service}/` |
| Commands | SSH | `ssh {service} "command"` |
| Network | Direct | `curl http://{service}:{port}/` |

Service names are user-defined hostnames. Discover: `zcli service list -P $projectId`

## Variables

**Rule:** ZCP has vars prefixed with service name. Containers have unprefixed vars.

**Discovery pattern:**
```bash
# On ZCP: Find service-specific vars
env | grep "^${servicename}_"

# Inside container: All vars unprefixed
ssh {servicename} "env | grep PORT"
```

**Common examples:**
- Service vars: `${appdev_PORT}`, `${myapi_zeropsSubdomain}` (your service name + underscore)
- Database vars: `$db_hostname`, `$db_password`, `$db_port` (always `db_` prefix)
- Inside container: `$PORT`, `$zeropsSubdomain` (no prefix)

⚠️ `zeropsSubdomain` is already full URL — don't prepend `https://`
⚠️ Services capture env vars at START TIME. New/changed vars → restart service.

## Starting Work

**Will this work be deployed (now or later)?**

| Answer | Command | Use for |
|--------|---------|---------|
| **YES** | `/var/www/.claude/workflow.sh init` | Features, fixes to ship, config changes |
| **NO** | `/var/www/.claude/workflow.sh --quick` | Investigating, exploring, dev-only testing |
| **UNCERTAIN** | `/var/www/.claude/workflow.sh init` | Default to safety — stop at any phase |

## Tools
```bash
/var/www/.claude/workflow.sh                     # Decision guidance (run first if unsure)
/var/www/.claude/workflow.sh init                # Enforced workflow (has gates)
/var/www/.claude/workflow.sh --quick             # Quick mode (no gates)
/var/www/.claude/workflow.sh --help              # Full platform reference
/var/www/.claude/status.sh [--wait {service}]    # Deployment status
/var/www/.claude/verify.sh {service} {port} /... # Endpoint testing
```

⚠️ Run `zcli login` before any zcli commands
