# Phase 3 — Vertical-slice DAG + bootstrap = runway

Goal: cut the PRD into thin **vertical slices** — each one end-to-end working software a user can touch, not a horizontal layer. Then provision all infra upfront so the slices have a runway to deploy onto.

## What a vertical slice is

A slice is the **thinnest end-to-end path that produces a reaction-worthy result** (data model → API → minimal UI → works at a URL) — vertical, not horizontal: "you can create and list tasks", never "build the database". Each slice:

- is independently buildable + verifiable (it runs on the already-provisioned dev runtime + services and the user can react to it);
- has explicit **acceptance criteria** (what the user can now do), exercised through its **seam** — the one interface it is built behind and tested through (inherited from the PRD);
- declares **blocked-by** edges (which earlier slice it depends on) and **parallel safety** (whether it can run beside a sibling) — that's the DAG.

**Slice 1 is special: walking skeleton = all PRD infra + the thinnest vertical feature.** It wires every provisioned service into a single end-to-end path that runs on the dev runtime and serves something real.

- **On a recipe substrate** (bootstrap anchored a curated recipe), the `buildFromGit` skeleton already walks at the dev URL with the right `zerops.yaml`, env wiring, and gotchas. Slice 1 *adapts* it: strip the recipe's demo content (routes / views / sample features), keep the wiring + committed `zerops.yaml` + gotchas, build the product's thinnest vertical feature, and re-skin the demo look to the product's own theme — never ship a recipe showcase's identity as the user's app (look rules: `SKILL.md` "Make it look and feel right"). "Demo scaffolding removed" is a slice-1 acceptance item.
- **On a from-scratch substrate**, slice 1 builds that thinnest path on the blank runtime.

Either way it stands up the UI shell — the kit, the theme, and the nav — once, so every later slice builds against an established look. Everything after it is additive.

Prefer slices that are naturally independent after the walking skeleton, but do not distort the product slice to get concurrency. Parallelism is an optimization under the vertical-slice rule; if the only way to parallelize is to split "backend", "database", and "UI" into separate slices, keep it serial.

## DAG depth scales with tier (no gold-plating)

| Tier | DAG shape | Design pass |
|---|---|---|
| **experiment** | **single slice, no DAG** — architecture is already `f(decision set)` | none |
| **real-but-lean** (default) | a few slices: walking skeleton → 1–2 feature slices | seam chosen inline |
| **production-business** | full DAG with parent edges | optional design-twice for *novel* features only |

**Design-it-twice is off by default** — reach for it only when a feature has no obvious shape and getting it wrong is expensive.

## Write each slice as `.zcp/guided/slices/NN-*.md`

The slice file **is** the brief handed to the build subagent (phase 4). Write it so a fresh subagent with no other context can build it:

```markdown
# Slice NN — <name>

## Build
<what to build, end-to-end: the data model touched, the API/use-case, the minimal UI>

## Acceptance
<what the user can do when this is done — the concrete, demoable result; for every data view this slice adds, the loading + empty + error states are part of acceptance, not later polish>

## Preserve
<one existing flow/invariant this slice must not break; for slice 1 use the floor commitment being protected>

## Design seam
<the one interface this slice exposes — the seam it is built behind AND tested through (a real caller and the acceptance test cross the same interface). Name what stays behind the repository/adapter/interface, the floor invariants the interface owns (owner-scoped, default-deny — held by the model, not by an assertion's strength), this slice's surface type (inherits the app default, override per route), and any shared layout/theme it touches — slice 1 stands up the kit shell + theme + nav once, later slices are additive against it>

## Services
<which provisioned services this slice touches; how it wires to them (env refs, not literals)>

## Blocked-by
<earlier slice(s) this depends on, or "none">

## Parallel
<serial, or candidate with slice NN because write scopes/seams are disjoint; one-line reason>
```

Number slices in dependency order. Keep each one thin — if a slice can't deploy-and-be-reacted-to on its own, it's too horizontal; split it. If the Build / Acceptance / Preserve / Design seam cannot be stated compactly, the slice is unclear; split or redesign before coding.

## Parallelization audit (read-only, before implementation)

After writing the DAG, dispatch read-only subagents to check which slices can safely run in parallel. They inspect the PRD + slice files and return a compact lane plan; they do not edit code or slice files. Ask them to validate:

- `Blocked-by: none` or all blockers already verified — necessary, never sufficient.
- Disjoint **Design seam** and write scope: no shared migration/schema, auth/session, router, layout, shared UI state, generated client, or low-level service wiring.
- Independent acceptance tests: each slice can prove its result without depending on another in-flight slice.
- One living dev runtime stays understandable: reload/verify can be attributed to a specific completed slice, and any shared runtime state is reset or read-only.

Mark each slice's `## Parallel` field from that audit: `serial` or `candidate with <slice> — <reason>`. When uncertain, serial wins. Slice 1 is always serial.

## Codebase-design discipline (the seam that makes correction cheap)

Foundational, one-way decisions get a **clean seam** so a wrong inference costs an additive change, not a rebuild:

- Persistence behind a thin repository, not scattered raw queries.
- A real `owner_id` column + one server-side authorization layer from slice 1, so single-tenant → multi-tenant is an additive migration, not a teardown.
- Default-deny reads, so private → public is a one-line flip.
- Integrations behind an interface, so a provider swap is local.
- For a collapsed modular app, each slice declares the module(s) it may touch. Shared code is only low-volatility plumbing; domain knowledge stays in its owning boundary.

Narrate these as vetoable assumptions (they're already in the PRD's inferred-assumptions). Spend design effort here; stay lean on everything reversible (which DB engine, cache, replicas — decided live, defaulted lean).

Events/jobs are not second sources of truth: default to an ID plus the minimum context a consumer needs to decide what to do. Copy before/after data only when the consumer cannot safely re-read the owner, and record that consistency trade-off in the PRD decision row.

## Bootstrap = runway (infra-first, never a product slice)

Before building slice 1's *feature*, provision **all PRD infra upfront** via bootstrap (`zerops_workflow`) — never service-by-service. It stands up every service in the topology chapter so each later slice has a ready platform to build on.

- **Provision from the recipe Align anchored** (`route=recipe`): its import IS the runway — the dev/stage pair + managed deps — and its `buildFromGit` seeds the runtimes with a runnable skeleton. Submit a plan only to rename a colliding hostname or mark an already-present managed dep `EXISTS`; the recipe owns service types, modes, and pairing. A floor- or capability-required service the recipe lacks is a follow-on **"Add more services"** step on the provisioned project, never a from-scratch plan.
- **Only when Align found no framework-matching recipe**, provision from scratch (`route=classic`): the dev/stage pair (standard mode) plus every managed service in the topology chapter, all at once.
- Run the bootstrap/provision/close pipeline; don't invent a guided-specific provisioning path.
- On a from-scratch substrate, application code, `zerops.yaml`, and the first build stay in develop (phase 4) — bootstrap is infrastructure only. On a recipe substrate the skeleton's code + `zerops.yaml` arrive with the clone; develop adapts them in place.

When this is done: the slice files exist under `.zcp/guided/slices/`, the dev/stage pair + all managed services are provisioned, and you're ready to build slice 1 on the dev runtime (phase 4).
