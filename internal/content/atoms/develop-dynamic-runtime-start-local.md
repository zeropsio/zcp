---
id: develop-dynamic-runtime-start-local
priority: 2
phases: [develop-active]
runtimes: [dynamic]
environments: [local]
title: "Dynamic runtime — start over SSH after deploy (local)"
---

From the local machine, start the real server on a dynamic-runtime service
after deploy. VPN must be up (`zcli vpn up`) or the SSH hop fails:

```
ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null {hostname} \
  'cd /var/www && {start-command}'
```

Replace `{start-command}` with the `run.start` value from `zerops.yaml`.
