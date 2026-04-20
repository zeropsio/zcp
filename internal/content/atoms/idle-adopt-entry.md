---
id: idle-adopt-entry
priority: 1
phases: [idle]
idleScenarios: [adopt]
title: "Adopt existing unmanaged services"
---

One or more runtime services exist in the project but have no ZCP
bootstrap metadata. Adopt them before deploying code — the adopt route
reads the service list, asks for mode + strategy per unmanaged
service, writes `ServiceMeta`, and verifies connectivity against the
current code without redeploying.

Start with discovery so the engine inspects the live state:

```
zerops_workflow action="start" workflow="bootstrap" intent="adopt existing"
```

The response surfaces an `adopt` option at the top of `routeOptions[]`
with `adoptServices` listing the hostnames. Commit the adoption with:

```
zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="adopt existing"
```
