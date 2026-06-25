# Phase 3 — Vertical-slice DAG + bootstrap = runway

Goal: cut the PRD into thin **vertical slices** — each one end-to-end working software a user can touch, not a horizontal layer. Then provision all infra upfront so the slices have a runway to deploy onto.

## What a vertical slice is

A slice is the **thinnest end-to-end path that produces a reaction-worthy result**: data model → API → minimal UI → it works at a URL. Not "build the database" (horizontal), but "you can create and list tasks" (vertical, touches every layer thinly). Each slice:

- is independently buildable + verifiable (it runs on the already-provisioned dev runtime + services and the user can react to it);
- has explicit **acceptance criteria** (what the user can now do) and a **test seam** (inherited from the PRD);
- declares **blocked-by** edges (which earlier slice it depends on) — that's the DAG.

**Slice 1 is special: walking skeleton = all PRD infra + the thinnest vertical feature.** It wires every provisioned service into a single end-to-end path that runs on the dev runtime and serves something real. Everything after it is additive.

## DAG depth scales with tier (no gold-plating)

| Tier | DAG shape | Design pass |
|---|---|---|
| **experiment** | **single slice, no DAG** — architecture is already `f(decision set)` | none |
| **real-but-lean** (default) | a few slices: walking skeleton → 1–2 feature slices | seam chosen inline |
| **production-business** | full DAG with parent edges | optional design-twice for *novel* features only |

A workout tracker gets ONE slice. A real-estate PM gets a skeleton + a couple of feature slices. Only a production-business app with a genuinely novel feature earns a DAG with parallel design. **Design-it-twice is off by default** — reach for it only when a feature has no obvious shape and getting it wrong is expensive.

## Write each slice as `.zcp/guided/slices/NN-*.md`

The slice file **is** the brief handed to the build subagent (phase 4). Write it so a fresh subagent with no other context can build it:

```markdown
# Slice NN — <name>

## Build
<what to build, end-to-end: the data model touched, the API/use-case, the minimal UI>

## Acceptance
<what the user can do when this is done — the concrete, demoable result>

## Test seam
<where the acceptance test sits (from PRD Testing decisions) — the red→green contract>

## Services
<which provisioned services this slice touches; how it wires to them (env refs, not literals)>

## Blocked-by
<earlier slice(s) this depends on, or "none">
```

Number slices in dependency order. Keep each one thin — if a slice can't deploy-and-be-reacted-to on its own, it's too horizontal; split it.

## Codebase-design discipline (the seam that makes correction cheap)

Foundational, one-way decisions get a **clean seam** so a wrong inference costs an additive change, not a rebuild:

- Persistence behind a thin repository, not scattered raw queries.
- A real `owner_id` column + one server-side authorization layer from slice 1, so single-tenant → multi-tenant is an additive migration, not a teardown.
- Default-deny reads, so private → public is a one-line flip.
- Integrations behind an interface, so a provider swap is local.

Narrate these as vetoable assumptions (they're already in the PRD's inferred-assumptions). The seam is the difference between "others should log in too" costing a migration vs a rewrite. Spend design effort here; stay lean on everything reversible (which DB engine, cache, replicas — decided live, defaulted lean).

## Bootstrap = runway (infra-first, never a product slice)

Before building slice 1's *feature*, provision **all PRD infra upfront** via bootstrap (`zerops_workflow`) — never service-by-service across infra. Bootstrap is a **runway prerequisite**, not a product slice — it stands up every service in the topology chapter so each subsequent slice has a ready platform to build on.

- Provision the **dev/stage pair** (bootstrap's standard mode) plus every managed service in the topology chapter, all at once — not service-by-service per slice.
- Run the bootstrap/provision/close pipeline; don't invent a guided-specific provisioning path.
- Application code, `zerops.yaml`, and the first build stay in develop (phase 4) — bootstrap is infrastructure only.

When this is done: the slice files exist under `.zcp/guided/slices/`, the dev/stage pair + all managed services are provisioned, and you're ready to build slice 1 on the dev runtime (phase 4).
