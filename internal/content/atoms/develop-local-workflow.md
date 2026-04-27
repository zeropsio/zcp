---
id: develop-local-workflow
priority: 3
phases: [develop-active]
environments: [local]
title: "Local development workflow"
---

### Development workflow

Edit code locally in your checkout. Managed services (databases, caches,
object storage) are reachable over Zerops VPN:

```
zcli vpn up
```

Test locally against the VPN-exposed managed services, then deploy when
ready via `zerops_deploy`. There is no SSHFS mount in local mode — the
build runs from your committed tree.
