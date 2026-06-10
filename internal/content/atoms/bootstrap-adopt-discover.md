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

You do not hand-write the nested adopt plan — `scope` is just the hostname list; the plan is derived for you UNLESS the pairing is ambiguous: when exactly two adoptable runtimes share a stack type, the call returns two ready-made `plan=[...]` templates (standard dev/stage pair vs independent dev containers) — pick the one matching reality and re-call with it verbatim (each named service becomes a tracked runtime target, `adoptionState="managed-dep"` services attach as shared dependencies). Naming the services keeps adoption scoped to YOUR task: in a project with other live work (or another agent session), an empty scope is ambiguous, so it returns the adoptable candidate list for you to pick from rather than silently adopting everything. The control-plane (`zcp-self` / `zcp@*`), already-`adopted`, and `resumable` (mid-bootstrap, owned by a prior session → use `resume`) services are never adopt targets. Hostnames stay verbatim — never rename an adopted service.

When exactly two adoptable runtimes share one runtime type (`ServiceStackTypeVersionName`) — the dev/stage shape — the response hands back two ready-to-paste plan templates rather than one default: a `standard` dev/stage pair (one container builds, the other receives the cross-deploy promote) and two independent dev containers. Pick the shape matching the user's intent and resubmit it as `plan=[...]`.
