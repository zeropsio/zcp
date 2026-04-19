---
id: develop-push-git-workflow-container
priority: 3
phases: [develop-active]
strategies: [push-git]
environments: [container]
title: "Push-git workflow (container)"
---

### Development workflow

Edit code on `/var/www/{hostname}/`. When the change is ready:

1. Commit on the dev container:

   ```
   ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
     'cd /var/www && git add -A && git commit -m "<description>"'
   ```

2. Push via ZCP:

   ```
   zerops_deploy targetService="{hostname}" strategy="git-push"
   ```

3. If CI/CD is configured on the stage service, the stage deploy follows
   automatically after the push. If not, run `zerops_deploy` a second time
   against the stage hostname.
