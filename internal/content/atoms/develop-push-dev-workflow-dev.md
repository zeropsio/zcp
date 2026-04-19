---
id: develop-push-dev-workflow-dev
priority: 3
phases: [develop-active]
modes: [dev]
strategies: [push-dev]
environments: [container]
title: "Push-dev iteration cycle (dev mode, container)"
---

### Development workflow

Edit code on `/var/www/{hostname}/`. Iteration cycle:

1. Edit files on the mount — changes appear instantly inside the container.
2. Start or restart the server over SSH (background):

   ```
   ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
     'cd /var/www && {start-command}'
   ```

   Run this with the Bash tool using `run_in_background=true` so the agent
   isn't blocked.
3. Hit a local endpoint to confirm:

   ```
   ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
     'curl -s localhost:<port>/health' | jq .
   ```

4. Repeat until the change works.

**Code-only changes:** restart the server — no redeploy required.

**`zerops.yaml` changes** (env vars, ports, entries): redeploy required. The
redeploy kills a container and brings up a new one, which drops every open
SSH session. Always open a **new** SSH to `{hostname}` after a redeploy (the
old sessions exit 255) and re-run the start command.
