---
id: idle-adopt-entry
priority: 1
phases: [idle]
idleScenarios: [adopt]
title: "Adopt existing unmanaged services"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.BootstrapRouteOption.AdoptServices]
---

Runtime services exist in this project that ZCP is not tracking —
the Services block shows one or more as `not bootstrapped`. Develop,
deploy, and verify on these services require ZCP service metadata
that bootstrap creates; until bootstrap closes, those workflows are
not reachable as direct next-actions.

Two valid options to surface:
- **adopt** — take the existing services into ZCP (common when user
  said "adopt", "převzít", "nastav ZCP integration", or named the
  existing hostnames).
- **classic** — ignore the existing services and create new ones in
  parallel (valid when user wants a fresh slate or named different
  hostnames). Recipe is also valid when user mentions a framework.

If the user's intent is unambiguous (e.g. "adopt the project"), state
directly *"I'll adopt them first, then we can develop"* and start
adopt without asking. If intent is ambiguous, ask between routes —
NOT between post-bootstrap workflows. Wrong framing: *"develop on
appdev, finish staging, or something else"* — those aren't reachable
yet. Right framing: *"Adopt the existing appdev/appstage, or create
new services in parallel?"*

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
