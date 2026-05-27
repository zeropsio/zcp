---
id: idle-adopt-entry
priority: 1
phases: [idle]
idleScenarios: [adopt]
title: "Adopt existing unmanaged services"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.BootstrapRouteOption.AdoptServices]
---

Services in project, `not bootstrapped`. Two primary paths, both
legitimate; existing services stay independent either way:

1. **Adopt the listed services** — attach ZCP tracking to running
   services without changing their code, config, or scale.
2. **Create new services alongside** — pick non-colliding hostnames
   and bootstrap normally; existing services keep running untouched.

Re-importing or rewriting the existing services is **not** one of
these paths — `zerops_import override=true` is destructive and only
runs on explicit user request for a known service.

**Branch on intent.** Clear adopt intent ("adopt", "převzít", named
existing hostnames) → run adopt, no menu. Clear add-new intent
("add another", "new service") → use `classic`, no menu. Unclear →
offer routes, not workflows.

- ✅ "Adopt existing appdev/appstage, or create new alongside?"
- ❌ "Develop on appdev, finish staging, or something else?" — those
  workflows aren't reachable yet; framing presents the unreachable as
  available.

Start (route-menu, no session yet):

```
zerops_workflow action="start" workflow="bootstrap" intent="adopt existing"
```

Commit (opens session):

```
zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="adopt existing"
```

Type field in the plan carries the full identifier from
`zerops_discover` verbatim — `alpine/nodejs@22`, `postgresql:single@18`.
Legacy `os:` + `mode:` sibling fields still accepted for BC but the
composite `type` is canonical; don't split. Pair-OS mismatch
(ubuntu/alpine) accepted silently — dev half's type is what the
plan carries.

Close: each adopted hostname stamps `bootstrapped: true`, mode preserved.
Close-mode + git-push stay empty (develop configures on first use).

After adopt completes the runtime becomes a valid `launch-production`
source — the adopted ServiceMeta carries the pair identity + setup
cascade state, so `zerops_workflow workflow="launch-production"
targetService=<adopted-hostname>` lands on the canonical dev-half
without an extra normalization round-trip.
