---
id: export-05-prepare-init
priority: 2
phases: [export-active]
title: "Export — Repository creation, git init, and push"
---

## Prepare — Repository Creation and Push

### Repository Creation (states S0 and S1)

Ask user: "Where should I push the code?"
- **New GitHub repo** — guide user to create via GitHub UI or `gh repo create`
- **Existing repo** — user provides URL (e.g. `https://github.com/user/repo`)

### Git Init and Push (states S0 and S1)

Create .gitignore if missing:
```bash
ssh {devHostname} "cd /var/www && test -f .gitignore || echo 'node_modules/\nvendor/\n.env\n.env.*\n*.log\ndist/\nbuild/\n.cache/' > .gitignore"
```
Customize for framework: `zerops_knowledge query="{runtime} gitignore"`

Commit and push (tool handles git init, .netrc auth, remote setup):
```bash
ssh {devHostname} "cd /var/www && git add -A && git commit -m 'initial: export from Zerops'"
```
```
zerops_deploy targetService="{devHostname}" strategy="git-push" remoteUrl="{repoUrl}"
```

If push fails with auth error: verify GIT_TOKEN is set via `zerops_discover includeEnvs=true`.
If push fails with history conflict: push from a dev service (which preserves .git/).

### State S2: Verify Existing Remote

If service already has a git remote:
1. Note the remote URL — this is the repo for buildFromGit
2. Verify zerops.yaml is in the repo
3. Skip to Generate step
