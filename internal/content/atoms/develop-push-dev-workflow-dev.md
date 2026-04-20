---
id: develop-push-dev-workflow-dev
priority: 3
phases: [develop-active]
deployStates: [deployed]
modes: [dev]
strategies: [push-dev]
environments: [container]
title: "Push-dev iteration cycle (dev mode, container)"
---

### Development workflow

Edit code on `/var/www/{hostname}/`. Iteration cycle:

1. Edit files on the mount — changes appear instantly inside the container.
2. Restart the server over SSH (see the start atom for the exact command).
   Run with `run_in_background=true` so the agent isn't blocked.
3. Hit a local endpoint with curl over the same SSH connection to confirm.
4. Repeat until the change works.

**Code-only changes:** restart the server — no redeploy required.

**`zerops.yaml` changes** (env vars, ports, entries): redeploy required. The
redeploy kills the container, which drops every open SSH session. Always
open a **new** SSH to `{hostname}` after a redeploy (old sessions exit 255)
and re-run the start command.
