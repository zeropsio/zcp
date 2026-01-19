# Zerops Platform

**Fix errors on dev. Stage is for final validation, not debugging.**

## Start Here — RUN ONE

| Will you write/change code? | Command | Examples |
|-----------------------------|---------|----------|
| **Yes, deploy to stage** | `.zcp/workflow.sh init` | Build feature, fix bug, any code change |
| **Yes, dev only** | `.zcp/workflow.sh init --dev-only` | Prototype, experiment, not ready for stage |
| **Yes, urgent hotfix** | `.zcp/workflow.sh init --hotfix` | Production broken, skip dev verification |
| **No, just looking** | `.zcp/workflow.sh --quick` | Read logs, investigate, understand codebase |

**Run one. Follow its output.** The script guides each phase and enforces gates.

## Context

**Zerops** is a PaaS. Projects contain services (containers) on a shared private network.

**ZCP** (Zerops Control Plane) is your workspace — a privileged container with:
- SSHFS mounts to all service filesystems
- SSH access to execute commands inside any container
- Direct network access to all services
- Tooling: `jq`, `yq`, `psql`, `mysql`, `redis-cli`, `zcli`, `agent-browser`

## Your Position

You are on ZCP, not inside the app containers.

| To... | Do |
|-------|----|
| Edit files | `/var/www/{service}/path` (SSHFS) |
| Run commands | `ssh {service} "command"` |
| Reach services | `http://{service}:{port}` |
| Test frontend | `agent-browser open "$URL"` |

## Variables

```bash
$projectId                  # Project ID (ZCP has this)
$ZEROPS_ZAGENT_API_KEY      # Auth key for zcli
${service_VAR}              # Other service's var: prefix with hostname
ssh svc 'echo $VAR'         # Inside service: no prefix
```

## Gotchas

| Symptom | Cause | Fix |
|---------|-------|-----|
| `https://https://...` | zeropsSubdomain is full URL | Don't prepend protocol |
| Files missing on stage | Not in deployFiles | Update zerops.yaml, redeploy |
| SSH hangs forever | Foreground process | `run_in_background=true` |
| Variable empty | Wrong prefix | Use `${hostname}_VAR` |
| Variable empty | New service added | SSH to read: `ssh db 'echo $password'` |
| Can't transition phase | Missing evidence | `.zcp/workflow.sh show` |

## Reference

```bash
.zcp/workflow.sh show              # Current phase, what's blocking
.zcp/workflow.sh --help            # Full platform reference
.zcp/workflow.sh --help discover   # Find services, record IDs
.zcp/workflow.sh --help develop    # Build, test, iterate on dev
.zcp/workflow.sh --help deploy     # Push to stage, deployFiles checklist
.zcp/workflow.sh --help verify     # Test stage, browser checks
.zcp/workflow.sh --help vars       # Environment variable patterns
.zcp/workflow.sh --help services   # Service naming, hostnames vs types
.zcp/workflow.sh --help trouble    # Common errors and fixes
.zcp/workflow.sh --help gates      # Phase transition requirements
.zcp/workflow.sh --help extend     # Add services mid-project
```
