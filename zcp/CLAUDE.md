# Zerops Platform

**Fix errors on dev. Stage is for final validation, not debugging.**

## Starting Work — RUN FOR EACH TASK

| Answer | Command | Use for |
|--------|---------|---------|
| **Deploying?** | `workflow.sh init` | Features, fixes, config changes |
| **Exploring?** | `workflow.sh --quick` | Investigating, reading, dev-only work |
| **Prototyping?** | `workflow.sh init --dev-only` | Dev iteration without deployment |
| **Hotfix?** | `workflow.sh init --hotfix` | Urgent fix (skips dev verification) |

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

Vars captured at service start. If missing, read from target: `ssh appdev "echo \$PORT"`
`zeropsSubdomain` is full URL — don't prepend `https://`

### zcli

Run `zcli login` before any other `zcli` command (safe to run multiple times):
```bash
zcli login --region=gomibako --regionUrl='https://api.app-gomibako.zerops.dev/api/rest/public/region/zcli' "$ZAGENTS_API_KEY"
```

### Tools

```bash
# Core workflow
workflow.sh                              # Decision guidance (run first if unsure)
workflow.sh init                         # Enforced workflow (has gates)
workflow.sh init --dev-only              # Dev mode (no deployment)
workflow.sh init --hotfix                # Hotfix mode (skip dev verification)
workflow.sh --quick                      # Quick mode (no gates)
workflow.sh --help                       # Full platform reference
workflow.sh --help {topic}               # Topic help: extend, bootstrap, gates...

# Workflow control
workflow.sh transition_to {phase}        # Advance phase
workflow.sh transition_to --back {phase} # Go backward (invalidates evidence)
workflow.sh show                         # Current status
workflow.sh complete                     # Verify evidence for completion
workflow.sh reset                        # Clear all state
workflow.sh reset --keep-discovery       # Clear state, preserve discovery

# Discovery
workflow.sh create_discovery {dev_id} {dev_name} {stage_id} {stage_name}
workflow.sh create_discovery --single {id} {name}  # Single-service mode
workflow.sh refresh_discovery            # Validate current discovery

# Project management
workflow.sh extend {import.yml}          # Add services to project
workflow.sh upgrade-to-full              # Upgrade dev-only to full deployment
workflow.sh record_deployment {stage}    # Manual deployment evidence

# Status and verification
status.sh                                # Deployment status
status.sh --wait {service}               # Wait for deployment (records evidence)
verify.sh {service} {port} / /api/...    # Endpoint testing
```

### Workflow Modes

| Mode | Flow | Use Case |
|------|------|----------|
| `init` | DISCOVER → DEVELOP → DEPLOY → VERIFY → DONE | Full deployment with all safety gates |
| `init --dev-only` | DISCOVER → DEVELOP → DONE | Prototyping, no deployment |
| `init --hotfix` | DEVELOP → DEPLOY → VERIFY → DONE | Urgent fixes, skips dev verification |
| `--quick` | Any phase, no gates | Investigation, exploration |

### Phase Gates

| Transition | Requirement |
|------------|-------------|
| DISCOVER → DEVELOP | `discovery.json` exists with current session |
| DEVELOP → DEPLOY | `dev_verify.json` with 0 failures |
| DEPLOY → VERIFY | `deploy_evidence.json` (from `status.sh --wait`) |
| VERIFY → DONE | `stage_verify.json` with 0 failures |

### Backward Transitions

Use `--back` flag when you need to go backward:
```bash
workflow.sh transition_to --back DEVELOP  # From VERIFY or DEPLOY
workflow.sh transition_to --back VERIFY   # From DONE
```

This invalidates stage evidence (you'll need to re-verify after redeploying).

### Adding Services

See `workflow.sh --help extend` for complete guide. Quick version:
```bash
cat > add-db.yml <<'YAML'
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
YAML
workflow.sh extend add-db.yml
```

New service vars require ZCP restart or SSH read: `ssh db 'echo $password'`
