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

Then complete the discover step naming the services you want to adopt in `scope` (the
`adoptionState="adoptable"` hostnames from discover that THIS task needs):

```
zerops_workflow action="complete" step="discover" scope=["appdev","appstage"]
```

**Branch on what discover shows.** If discover flags exactly two adoptable runtimes that share a stack type (the dev/stage shape), skip `scope` and go straight to an explicit `plan=[...]` — a bare scope can't tell a standard dev/stage pair from two independent dev containers, so it rejects. The discover warning and the reject both hand you two ready-to-paste templates: a `standard` dev/stage pair where one container builds and the other receives the cross-deploy promote, vs two independent dev containers — pick the shape matching reality and submit it verbatim. In every other case (one runtime, or runtimes of different stacks), `scope` is enough: you do not hand-write the plan — `scope` is just the hostname list and the plan is derived for you.

Each named service becomes a tracked runtime target; `adoptionState="managed-dep"` services attach as shared dependencies. Naming the services keeps adoption scoped to YOUR task: in a project with other live work (or another agent session), an empty scope is ambiguous, so it returns the adoptable candidate list for you to pick from rather than silently adopting everything. The control-plane (`zcp-self` / `zcp@*`), already-`adopted`, and `resumable` (mid-bootstrap, owned by a prior session → use `resume`) services are never adopt targets. Hostnames stay verbatim — never rename an adopted service.
