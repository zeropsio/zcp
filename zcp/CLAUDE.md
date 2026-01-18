# Zerops Platform

## Orientation

You are on **ZCP** (Zerops Control Plane), not inside containers.

| Operation | How | Example |
|-----------|-----|---------|
| Edit files | SSHFS direct | `/var/www/{service}/main.go` |
| Run commands | SSH | `ssh {service} "go build"` |

Service names vary: appdev/appstage, apidev/apistage, webdev/webstage, etc.
Network: Hostname = service name. Services connect via `http://{service}:{port}`.

## Variables

| Context | Pattern | Example |
|---------|---------|---------|
| ZCP → service | `${svc}_VAR` | `${appdev_PORT}` |
| ZCP → database | `$db_*` | `$db_hostname`, `$db_password` |
| Inside container | `$VAR` | `ssh {svc} "echo \$PORT"` |

⚠️ `zeropsSubdomain` is already full URL — don't prepend `https://`
⚠️ For services deployed this session: `ssh {svc} "echo \$VAR"` (ZCP won't have them)

## Tools

```bash
workflow.sh --help              # Full platform reference
workflow.sh init                # Start enforced workflow
workflow.sh --quick             # Quick mode (no enforcement)
status.sh                       # Check deployment state
status.sh --wait {svc}          # Wait for deploy completion
verify.sh {svc} {port} /paths   # Test endpoints
```

## Quick Start

```bash
# Significant changes → enforced workflow
/var/www/.claude/workflow.sh init

# Bug fixes, exploration → no enforcement
/var/www/.claude/workflow.sh --quick

# Lost context? Get full reference
/var/www/.claude/workflow.sh --help
```
