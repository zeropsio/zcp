---
id: develop-push-git-deploy
priority: 2
phases: [develop-active]
strategies: [push-git]
title: "Push-git strategy — deploy via git push"
---

### Push-Git Deploy Strategy

Push committed code from the dev container to an external git repository (GitHub/GitLab).

**First time setup** (once per service):
1. Get a GitHub/GitLab token from the user (Contents: Read and write for GitHub, write_repository for GitLab)
2. Store as project env var: `zerops_env action="set" project=true variables=["GIT_TOKEN={token}"]`
3. Commit: `ssh {hostname} "cd /var/www && git add -A && git commit -m 'initial commit'"`
4. Push with remote URL: `zerops_deploy targetService="{hostname}" strategy="git-push" remoteUrl="{url}"`

**Subsequent deploys:**
1. Commit with a descriptive message:
   `ssh {hostname} "cd /var/www && git add -A && git commit -m '{what changed}'"`
2. Push to remote:
   `zerops_deploy targetService="{hostname}" strategy="git-push"`
3. If CI/CD is configured: build triggers automatically.
   Monitor: `zerops_events serviceHostname="{stage-hostname}"`
4. If no CI/CD: deploy to stage manually:
   `zerops_deploy sourceService="{hostname}" targetService="{stage-hostname}"`

**Set up CI/CD:** `zerops_workflow action="start" workflow="cicd"`
**Export with import.yaml:** `zerops_workflow action="start" workflow="export"`
**Switch strategy:** `zerops_workflow action="strategy" strategies={"{hostname}":"push-dev"}`
