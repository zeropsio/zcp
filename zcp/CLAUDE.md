# Zerops Platform

## Orientation

You are on **ZCP** (Zerops Control Plane), not inside containers.

| Operation | How | Example |
|-----------|-----|---------|
| Edit files | SSHFS direct | `/var/www/{service}/main.go` |
| Run commands | SSH | `ssh {service} "go build"` |

Service names are user-defined hostnames. Discover: `zcli service list -P $projectId`
Network: Services connect via `http://{hostname}:{port}` (hostname = service name).

## Variables

| Context | Pattern | Example |
|---------|---------|---------|
| ZCP → service | `${svc}_VAR` | `${appdev_PORT}` |
| ZCP → database | `$db_*` | `$db_hostname`, `$db_password` |
| Inside container | `$VAR` | `ssh {svc} "echo \$PORT"` |

⚠️ `zeropsSubdomain` is already full URL — don't prepend `https://`
⚠️ Services capture env vars at START TIME. New/changed vars → restart. If ZCP missing var: `ssh {svc} "echo \$VAR"`

## Tools

```bash
workflow.sh --help              # Full platform reference
workflow.sh init                # Start enforced workflow
workflow.sh --quick             # Quick mode (no enforcement)
status.sh                       # Check deployment state
status.sh --wait {svc}          # Wait for deploy completion
verify.sh {svc} {port} /paths   # Test endpoints
```

⚠️ **zcli requires authentication:** Run `zcli login` before any zcli commands (on ZCP or inside containers)

## Quick Start

```bash
# Significant changes → enforced workflow
/var/www/.claude/workflow.sh init

# Bug fixes, exploration → no enforcement
/var/www/.claude/workflow.sh --quick

# Lost context? Get full reference
/var/www/.claude/workflow.sh --help
```
