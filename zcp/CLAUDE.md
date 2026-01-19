# Zerops Platform

**Fix errors on dev. Stage is for final validation, not debugging.**

## Starting Work — RUN FOR EACH TASK

| Answer | Command | Use for |
|--------|---------|---------|
| **Deploying?** | `/var/www/.claude/workflow.sh init` | Features, fixes, config changes |
| **Exploring?** | `/var/www/.claude/workflow.sh --quick` | Investigating, reading, dev-only work |

Run one of the above BEFORE doing anything else.

---

## Reference

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

Every service can access:
- Own vars: `$VAR` (unprefixed)
- Other services' vars: `${hostname}_VAR` (prefixed with their hostname)

```bash
echo "$hostname"              # own hostname
echo "${appdev_PORT}"         # appdev's PORT
echo "${db_password}"         # db service's password
```

⚠️ Vars captured at service start. If missing, read from target: `ssh appdev "echo \$PORT"`
⚠️ `zeropsSubdomain` is full URL — don't prepend `https://`

### zcli

Run `zcli login` before any other `zcli` command (safe to run multiple times):
```bash
zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "$ZAGENTS_API_KEY"
```

### Tools

```bash
/var/www/.claude/workflow.sh                     # Decision guidance (run first if unsure)
/var/www/.claude/workflow.sh init                # Enforced workflow (has gates)
/var/www/.claude/workflow.sh --quick             # Quick mode (no gates)
/var/www/.claude/workflow.sh --help              # Full platform reference
/var/www/.claude/status.sh [--wait {service}]    # Deployment status
/var/www/.claude/verify.sh {service} {port} /... # Endpoint testing
```
