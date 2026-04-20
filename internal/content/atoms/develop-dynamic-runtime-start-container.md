---
id: develop-dynamic-runtime-start-container
priority: 3
phases: [develop-active]
runtimes: [dynamic]
environments: [container]
modes: [dev, standard]
title: "Dynamic runtime — start over SSH after deploy (container)"
---

After a deploy to a dynamic-runtime service the container runs `zsc noop`.
Start the real server from the ZCP host container via SSH:

```
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
  'cd /var/www && {start-command}'
```

Replace `{start-command}` with the `run.start` value from `zerops.yaml`.
Host keys rotate on every deploy, so the `StrictHostKeyChecking=no` flag is
mandatory — do not remove it.
