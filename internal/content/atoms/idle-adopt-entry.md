---
id: idle-adopt-entry
priority: 1
phases: [idle]
idleScenarios: [adopt]
title: "Adopt existing unmanaged services"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.BootstrapRouteOption.AdoptServices]
---

Runtime services exist in this project that ZCP is not tracking —
the Services block shows one or more as `not bootstrapped`. Adopt
them to enable ZCP deploy and verify workflows.

Start with discovery so the engine inspects the live state:

```
zerops_workflow action="start" workflow="bootstrap" intent="adopt existing"
```

The response surfaces an `adopt` option at the top of
`routeOptions[]` with `adoptServices[]` listing the hostnames. Commit
the adoption with:

```
zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="adopt existing"
```

After close, the envelope shows each adopted hostname with `bootstrapped: true` and the existing mode preserved. Close-mode + git-push capability stay empty (develop configures them on first use).
