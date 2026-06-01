---
id: bootstrap-adopt-discover
priority: 2
phases: [bootstrap-active]
routes: [adopt]
steps: [discover]
title: "Adopt — discover existing services"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.ServiceSnapshot.Mode, workflow.ServiceSnapshot.CloseDeployMode, ops.ServiceInfo.AdoptionState]
---

### Adopting existing services

Adoption attaches ZCP tracking to an existing runtime service without touching its code, configuration, or scale. After adopt close, the envelope reports each adopted hostname with `bootstrapped: true` and an empty close-mode / git-push capability — populated later when the develop session needs them.

If you reached this atom by way of the `ADOPT_REQUIRED` rejection on a service-scoped tool, the right reflex was to fire adopt directly from the discover warning. The bootstrap-adopt session opens with a single committed call:

```
zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="<one-line user task summary>"
```

Use the SAME `intent` string you'd pass to `workflow="develop"` afterwards — the intent threads through so you don't re-type it on the next workflow call. Placeholder strings (`"<task>"`, `"adopt existing"`) lose user context and break the develop-session continuity heuristic; phrase it as the actual scope the user requested.

List what's there:

```
zerops_discover
```

Then complete the discover step with **no `plan`**:

```
zerops_workflow action="complete" step="discover"
```

You do not hand-write the adopt plan. Every service marked `adoptionState="adoptable"` becomes a tracked runtime target; `adoptionState="managed-dep"` services attach as shared dependencies. The remaining states are excluded for you: `"adopted"` (already tracked), the control-plane `"zcp-self"`, and `"resumable"` (mid-bootstrap, owned by a prior session — that routes through `resume`, not `adopt`). Hostnames stay verbatim — never rename an adopted service.

When exactly two adoptable runtimes share one runtime type (`ServiceStackTypeVersionName`) — the dev/stage shape — the response hands back two ready-to-paste plan templates rather than one default: a `standard` dev/stage pair (one container builds, the other receives the cross-deploy promote) and two independent dev containers. Pick the shape matching the user's intent and resubmit it as `plan=[...]`.
