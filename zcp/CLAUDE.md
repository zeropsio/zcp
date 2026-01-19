# Zerops Platform

**Fix errors on dev. Stage is for final validation, not debugging.**

## Start Here — RUN ONE

| What are you doing? | Command | When to use |
|---------------------|---------|-------------|
| **Deploying?** | `.zcp/workflow.sh init` | Features, fixes, any change going to stage |
| **Exploring?** | `.zcp/workflow.sh --quick` | Reading code, investigating, no deploy intent |
| **Prototyping?** | `.zcp/workflow.sh init --dev-only` | Iterating on dev, not ready to deploy |
| **Hotfix?** | `.zcp/workflow.sh init --hotfix` | Urgent fix, skip dev verification |

**Run one. Follow its output.** The script guides each phase and enforces gates.

## Context

**Zerops** is a PaaS. Projects contain services (containers) on a shared private network.

**ZCP** (Zerops Control Plane) is your workspace — a privileged container with:
- SSHFS mounts to all service filesystems
- SSH access to execute commands inside any container
- Direct network access to all services
- Tooling: `jq`, `yq`, `psql`, `mysql`, `redis-cli`, `zcli`

## Your Position

You are on ZCP, not inside the app containers.

| To... | Do |
|-------|----|
| Edit files | `/var/www/{service}/path` (SSHFS) |
| Run commands | `ssh {service} "command"` |
| Reach services | `http://{service}:{port}` |

## Variables

```bash
$projectId                  # Project ID (ZCP has this)
$ZEROPS_ZAGENT_API_KEY      # Auth key for zcli
${service_VAR}              # Other service's var: prefix with hostname
ssh svc 'echo $VAR'         # Inside service: no prefix
```

⚠️ `zeropsSubdomain` is a full URL — never prepend `https://`
⚠️ Vars captured at service start. New service? Read via SSH: `ssh db 'echo $password'`

## Gotchas

| Symptom | Cause | Fix |
|---------|-------|-----|
| `https://https://...` | zeropsSubdomain is already full URL | Don't prepend protocol |
| Files missing on stage | Not in deployFiles | Update zerops.yaml, redeploy |
| SSH hangs forever | Foreground process | `run_in_background=true` |
| Variable empty | Wrong prefix or new service | `${hostname}_VAR` or SSH to read |
| Can't transition phase | Missing evidence | `.zcp/workflow.sh show` |

## Reference

```bash
.zcp/workflow.sh show           # Current phase, what's blocking
.zcp/workflow.sh --help         # Full platform reference
.zcp/workflow.sh --help discover   # Find services, record IDs
.zcp/workflow.sh --help develop    # Build, test, iterate on dev
.zcp/workflow.sh --help deploy     # Push to stage, deployFiles
.zcp/workflow.sh --help verify     # Test stage, browser checks
.zcp/workflow.sh --help vars       # Environment variable patterns
.zcp/workflow.sh --help trouble    # Common errors and fixes
.zcp/workflow.sh --help gates      # Phase requirements
.zcp/workflow.sh --help extend     # Add services mid-project
```
