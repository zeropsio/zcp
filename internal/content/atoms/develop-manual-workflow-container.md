---
id: develop-manual-workflow-container
priority: 3
phases: [develop-active]
strategies: [manual]
environments: [container]
title: "Manual strategy workflow (container)"
---

### Development workflow

Edit code on the SSHFS mount at `/var/www/{hostname}/`. When the change is
ready **inform the user** that the code is staged and awaiting their
decision — the user controls deploy timing on manual strategy.

Reference deploy commands (do not run them unless the user asks):

```
zerops_deploy targetService="{hostname}"
zerops_verify  serviceHostname="{hostname}"
```
