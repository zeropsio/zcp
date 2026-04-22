---
id: develop-close-push-git-container
priority: 7
phases: [develop-active]
deployStates: [deployed]
strategies: [push-git]
environments: [container]
title: "Close task — push-git strategy (container)"
---

### Closing the task — container + push-git

If push-git is already set up (GIT_TOKEN in project env, remote
configured), commit on the dev container and push:

```
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
  'cd /var/www && git add -A && git commit -m "{your-description}"'
zerops_deploy targetService="{hostname}" strategy="git-push"
```

If push-git isn't fully set up yet, the central deploy-config action
returns the full setup flow (intro → push → trigger):

```
zerops_workflow action="strategy" strategies={"{hostname}":"push-git"}
```
