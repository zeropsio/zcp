---
id: idle-adopt-entry
priority: 1
phases: [idle]
idleScenarios: [adopt]
title: "Adopt existing unmanaged services"
references-fields: [workflow.ServiceSnapshot.Bootstrapped, workflow.BootstrapRouteOption.AdoptServices, ops.ServiceInfo.AdoptionState]
---

Per-service `adoptionState` in `zerops_discover` output classifies each
service into one of five states: `adopted` (ZCP-tracked, ready for
develop/deploy), `adoptable` (live runtime without ServiceMeta — call
bootstrap route=adopt), `resumable` (mid-bootstrap, owned by prior
session — call bootstrap route=resume with the session ID surfaced in
the warning), `managed-dep` (db/cache/storage, no adoption concept),
`zcp-self` (control-plane container, never adopted). The discover
response also surfaces directive warnings naming exact recovery calls
per state — read them before deriving anything from per-service flags.

**FIRST CALL when discover surfaces adoptable services:** open the
bootstrap-adopt session immediately, with the SAME intent string you'd
pass to develop/deploy later. Do NOT probe with `workflow="develop"`
expecting an ADOPT_REQUIRED redirect — the redirect works but costs a
wasted round-trip + clutters the session log with a rejected start.

Concrete shape (commits the route on the first call — skip the menu
when adopt intent is already clear from discover output):

```
zerops_workflow action="start" workflow="bootstrap" route="adopt" intent="<one-line user task summary>"
```

Replace `<one-line user task summary>` with the actual task intent
(e.g. `intent="redesign appdev homepage as tech blog"`) — NOT a
placeholder, NOT a generic "adopt existing". The intent threads
through to the develop session that follows, so phrasing it as the
real task scope avoids a re-typed intent on the next call.

Service-scoped tools (`workflow="develop"`, `zerops_deploy`,
`zerops_verify`) reject with `ADOPT_REQUIRED` until adoption completes.
That gate is structural backstop, not the primary path — read the
warning, fire adopt directly.

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
