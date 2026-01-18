# Zerops Platform

## Starting Work — RUN FOR EACH TASK

**Will this work be deployed (now or later)?**

| Answer | Command | Use for |
|--------|---------|---------|
| **YES** | `/var/www/.claude/workflow.sh init` | Features, fixes to ship, config changes |
| **NO** | `/var/www/.claude/workflow.sh --quick` | Investigating, exploring, dev-only testing |
| **UNCERTAIN** | `/var/www/.claude/workflow.sh init` | Default to safety — stop at any phase |

Run one of the above commands BEFORE doing anything else. The workflow script provides all necessary guidance.

---

## Reference (read after starting workflow)

### Context

**Zerops** is a PaaS. Each project has services (containers) on a shared private network.

**ZCP** (Zerops Control Plane) is your workspace — a privileged Ubuntu container with:
- SSHFS mounts to all service filesystems
- SSH access to execute inside any container
- Direct network access to all services
- Full tooling: `rg`, `fd`, `bat`, `jq`, `yq`, `http`, `psql`, `mysql`, `redis-cli`, `agent-browser`

### Your Position

| Access | Method | Example |
|--------|--------|---------|
| Files | SSHFS mount | `/var/www/{service}/` |
| Commands | SSH | `ssh {service} "command"` |
| Network | Direct | `curl http://{service}:{port}/` |

### Variables

**Rule:** ZCP has vars prefixed with service name. Containers have unprefixed vars.

- Service vars on ZCP: `${servicename}_VAR` (e.g., `${appdev_PORT}`)
- Database vars on ZCP: `$db_hostname`, `$db_password`
- Inside container via SSH: `$VAR` (no prefix)

⚠️ `zeropsSubdomain` is already full URL — don't prepend `https://`

### Tools

```bash
/var/www/.claude/workflow.sh                     # Decision guidance (run first if unsure)
/var/www/.claude/workflow.sh init                # Enforced workflow (has gates)
/var/www/.claude/workflow.sh --quick             # Quick mode (no gates)
/var/www/.claude/workflow.sh --help              # Full platform reference
/var/www/.claude/status.sh [--wait {service}]    # Deployment status
/var/www/.claude/verify.sh {service} {port} /... # Endpoint testing
```
