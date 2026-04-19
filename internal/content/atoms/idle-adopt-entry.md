---
id: idle-adopt-entry
priority: 2
phases: [idle]
title: "Adopt existing unmanaged services"
---

One or more runtime services exist in the project but have no ZCP bootstrap
metadata. Adopt them before deploying code:

```
zerops_workflow action="start" workflow="bootstrap" intent="adopt existing"
```

The adopt route reads the service list, asks for mode + strategy per
unmanaged service, writes `ServiceMeta`, and verifies connectivity against
the current code without redeploying.
