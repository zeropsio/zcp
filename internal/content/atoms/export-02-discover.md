---
id: export-02-discover
priority: 2
phases: [export-active]
title: "Export — Discover current state"
---

## Discover — Assess Current State

### Step 1: Discover services

```
zerops_discover service="{devHostname}" includeEnvs=true
```

Note: service type, status, ports, env var keys (keys only), scaling config, mode.

### Step 2: Check git state on container

```bash
ssh {devHostname} "cd /var/www && git remote -v 2>/dev/null; ls zerops.yml zerops.yaml 2>/dev/null; test -d .git && echo GIT_EXISTS || echo NO_GIT"
```

Classify into one of three states:

| State | .git | Remote | Meaning |
|-------|------|--------|---------|
| **S0** | No | No | Never initialized — full setup needed |
| **S1** | Yes | No | Internal git from zerops_deploy — add remote + push |
| **S2** | Yes | Yes | Has remote — verify, generate import.yaml |

### Step 3: Export project configuration

```
zerops_export
```

Returns the platform export YAML (re-importable) plus service metadata.

### Step 4: Ask user intent

"What do you need?"
- **A) CI/CD** — automatic deploy on git push
- **B) Reproducible import.yaml** with buildFromGit for replication
- **C) Both** (recommended) — buildFromGit for initial deploy + CI/CD for ongoing

Default to C if user is unsure.
