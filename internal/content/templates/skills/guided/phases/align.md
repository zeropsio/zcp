# Phase 1 — Align (evaluate + resolve + grill the residue)

Goal: turn the plain story into the resolved decision set + a concrete service plan, asking the user only what you genuinely cannot infer. Output of this phase is the input to the PRD (phase 2).

## Step 1 — evaluate what exists

Scan the repo first. **Greenfield** (empty / only ZCP scaffolding) → design fresh. **Brownfield** (real app code, a `zerops.yaml`, a deployed service) → the existing code/data is the truth; do not greenfield over it (this is the `adopt-path` of CLASSIFY). Read `zerops_workflow action="status"` to see what already exists on the platform.

## Step 2 — CLASSIFY (run first; non-buildable branches STOP normal inference)

| Route | Story signals | Action |
|---|---|---|
| `buildable` | concrete noun + verb: "track workouts", "manage tasks", "book appointments", "upload documents" | infer the decisions (Step 3) |
| `platform-reject` | GPU/CUDA training, hard-realtime control, large transcoding, live-streaming infra, mining — not in the catalog | STOP, say plainly why it can't run here; no masking fallback |
| `tripwire` | legal/medical/tax/financial advice, PHI/PCI, law-firm or client docs, payroll, minors, gov IDs, residency, charging cards / public payments, destructive public actions | harm-gate question FIRST; no auto public subdomain until cleared |
| `needs-scoping` | "all-in-one", "ERP", CRM + marketplace + HR + billing in one ask | ask the wedge (the one thing to build first) |
| `adopt-path` | "use my repo", "migrate my database", "deploy this GitHub project" | existing code/data is the truth — adopt, don't greenfield |
| `insufficient-signal` | "make me an app", "Uber for X", "AI-powered dashboard", one buzzword/noun, no process | ask ONE product-story question; emit no services yet |

## Step 3 — infer the decisions

**Confidence:** **High** — words say it → infer silently (unless a tripwire fires). **Medium** — strong prior, not stated → Path A narrates the assumption; Path B may confirm if one-way. **Low** — analogy/buzzword/vague → ask only if it gates a one-way or harm-bearing decision; else default lean/off.

**D1 Statefulness:**
| Value | Signals |
|---|---|
| stateless | landing page, calculator, converter, read-only; "no login / no saving" |
| single-writer | "my workouts", notes, journal, admin-owned CRUD; "track / save / manage" |
| multi-writer | team tasks, assignments, bookings, orders, submissions, sign-ups; "team / customers / book / assign / upload" |

**D2 Audience × stakes:**
| Value | Signals | Floor it forces |
|---|---|---|
| single-actor | "my / for me"; default if no one else named | a cheap auth seam, nothing more |
| multi-actor-private | "our firm / team", staff, roles | auth + server-side authorization (app code) |
| public/multi-tenant | customers/clients log in, subscribers, sign-up | auth + tenant isolation + availability + backups |

**Stakes modifiers** (payments, legal, medical, tax, private documents, contracts, IDs, children, payroll, irreversible actions) raise the grade or fire the tripwire **even if the user said "cheap".** Stakes beat budget on the floor.

**D3 Capability bits** (default OFF; each present/absent):
| Bit | Turns on when | Don't be fooled |
|---|---|---|
| `search` | "search / find / filter / lookup / browse many / full-text" | a simple list filter ≠ dedicated search |
| `async` | "send email/SMS, notify, reminders, nightly report, import, scrape, sync, webhook" | "daily overview" alone = a page, not a job |
| `files` | "upload / attach / photos / PDFs / documents / receipts" | **FLOOR if present** — object-storage even at experiment tier |
| `real-time` | "live chat, presence, collaborative, live map/tracking, websocket" | "notifications" alone = async; hard realtime = platform-reject |
| `integrations` | "Stripe/payments, Calendar, Slack, email/SMS provider, maps, CRM sync" | usually just secrets/env; payment/webhook reliability may also pull async + a worker |

**D5 Tier:**
| Value | Signals |
|---|---|
| experiment | "quick / prototype / demo / personal / cheap / for me" |
| real-but-lean | "internal tool, small team, regular use" — **default for an unclear-but-serious app** |
| production-business | "run the business on it, customers/public, paid, firm-critical, money/PII, launch, high traffic" |

**Architecture drivers:** after D1/D2/D3/D5, write at most 3 drivers that actually shape the architecture, each as `trigger → action → failure mode` (e.g. durable private team data → managed DB + server-side auth → runtime-disk/client-only checks lose or leak data). A driver must change a service choice, data owner, seam, release gate, or next slice; otherwise cut it.

**Deceptive-signal guards:** strong analogies are LOW signal ("Uber for dog walking" does NOT imply maps/payments/dispatch/tracking → `insufficient-signal`, ask the process). Buzzwords are not capabilities ("AI-powered" ≠ vector/search unless the process needs generation/embeddings/retrieval). Named tech (Mongo/Firebase/GPU/Redis) is a *disposition item*, not an instruction — map it to a Zerops equivalent only when the capability is clear; if there's no equivalent and it's a hard requirement, ask or reject, never silently substitute.

## Step 4 — story → recipe fit → services (how many and which)

**Start from a curated recipe.** Once you've resolved the runtime + framework (route-to-owner table below), your decision set is a stack spec. Consult the bootstrap recipe catalog — `zerops_workflow` discover, passing the resolved stack (e.g. `php laravel postgres`) as the intent — and anchor on the curated recipe that fits it. A recipe is a working dev/stage pair + managed deps + a `buildFromGit` skeleton carrying a committed `zerops.yaml` and pre-solved framework gotchas: the substrate the product is built on. The matrix below is the fit-judge and delta-resolver, not a blank-sheet blueprint.

Pick the recipe in this order:

1. **Framework gate (hard).** Anchor only on a recipe whose app **framework matches your resolved framework** — read the catalog's `fw/lang` column, not its dependency-fit badge (a recipe scores "fit: exact" on databases alone while its framework is wrong). A framework-mismatched recipe is a trap: its committed `zerops.yaml` is tuned for the wrong framework. No framework match → there is no substrate for this stack; resolve services from the matrix below (the fallback) and pull any same-stack gotchas as knowledge.
2. **Grade gate (hard).** A recipe creates its managed services at a fixed mode (`:single`) and **mode is immutable after create.** Anchor in place only when that grade meets your D7 floor — true for **experiment** and **real-but-lean**, where `:single` is the floor. A **production-business** `:ha`+backups floor is NOT reached by mutating a recipe; build the dev/stage pair from the recipe at its grade and take production HA through the launch-production flow.
3. **Floor reconciliation (mandatory).** From your resolved D1/D3 the floor-required services are fixed: durable data → managed database; **files bit → object-storage, even at experiment tier.** Every floor-required service the recipe lacks is a mandatory add, not an optional extra.
4. **Smallest-covering.** Among framework-matching, grade-OK recipes, anchor on the smallest-covering recipe (a minimal/hello-world over a full showcase): you add what the product needs and never carry services it clearly doesn't.

**Adjust the recipe to the spec, never the spec to the recipe.** If you reach for a recipe to avoid resolving D1/D2, an input is under-specified — resolve the input. The resolved decision set governs; the recipe is the starting material.

**The delta:**
- **Add** a floor- or capability-required service the recipe lacks as an explicit "Add more services" step on the provisioned project (phase 3) — never re-derive the whole topology.
- **Trim by selection, not in place.** Drop an unneeded service by choosing a smaller recipe that never declared it. A recipe's managed services back its app's `${host_*}` wiring and cannot be cleanly stripped after provision; if the only framework-matching recipe over-provisions for this product, resolve services from the matrix instead and carry its gotchas as knowledge.

You ALWAYS land on a concrete service set. The matrix below resolves **D6 Decomposition** (DERIVED from D5 tier × D3 core-capability, clamped by D1) and judges recipe fit + the add-delta: each capability lands *in-process* at the cheap tier and *promotes to a dedicated service* as tier rises or when it is the product's core. Floor overrides (D1 stateful, files bit) force a managed service regardless of tier.

Before physical services, name only the **logical components** the story really needs (1-7, never padded for count) from workflows or actors, not entities: component → responsibility → owned data → physical home. Default physical home is the app module. Entity-shaped buckets like `TaskManager` / `UserHandler` are a warning sign unless the workflow boundary is clear.

**D6 gate:** if the app shares one operational profile, one data lifecycle, and no independent scale/deploy/fault/security need, choose `collapsed`: one app dev/stage pair as a **collapsed modular app** with internal domain modules. Promote to a dedicated runtime only when a driver needs independent scaling, fault isolation, security isolation, a separate deployment cadence, heavy background work, or a true separate SPA. If a core user flow needs ACID across two candidate services, merge them back into one module/coarse service; do not invent sagas unless the PRD explicitly adds retry/status/failure handling as a slice.

| Capability | experiment (collapse) | real-but-lean (default) | production-business (dedicate) |
|---|---|---|---|
| Web (UI + API) | 1 runtime (monolith) | 1 runtime | 1 runtime; front/back split (static FE + API runtime) **only with a real separate SPA** — a server-rendered app stays 1 runtime |
| **Persist** *(floor if stateful)* | managed DB `:single` (hobby) | managed DB `:single` (staging) + backups | managed DB `:ha` (production) + backups |
| **Files** *(floor if files bit)* | object-storage | object-storage | object-storage |
| Search | in-proc DB query / FTS | DB FTS, or a dedicated search service `:single` if core | dedicated search `:ha` |
| Async | in-proc timer | in-proc, or a message service `:single` + worker if heavy | message service `:ha` + dedicated worker runtime |
| Cache / sessions | none | cache service `:single` if hot reads / multi-container | cache `:ha` (**required once minContainers ≥ 2 holds sessions**) |
| Real-time | in-proc (1 container) | cache/message pub-sub | message service `:ha` pub-sub + connection runtime |
| Vector (AI) | pgvector in the database | dedicated vector service `:single` | vector `:ha` |
| Analytics | in the database | dedicated analytics DB `:single` if real analytics | analytics `:ha` |

**D7 Grade** (DERIVED from D5 tier, clamped by D1/D2 floor) — the runtime is always a **dev/stage pair**; tier sets the knobs, not whether the pair exists: runtime scale (experiment min=max=1 small → lean autoscale 1..2 → production minContainers ≥ 2 + headroom); managed mode `:single`→`:single`→`:ha` on stateful; backups off-unless-irreplaceable → on → on+retention.

**Floor clamps (independent of tier — budget sets the ceiling, never the floor):** durable data → managed database (+backups if irreplaceable); multi-user → auth + server-side authorization **in app code** (no Zerops knob guarantees this — it's app-code work you own); never runtime-disk persistence; files → object-storage.

**Request-first:** async/jobs are off unless the story says notify/email/SMS, webhook, import/sync/scrape, scheduled report, burst/backpressure, or "result later is OK." Experiment keeps it in-process; production-business promotes irreplaceable work to queue + worker with retry/idempotency/status/log path.

**Route every service choice to its owner — never hardcode a version:**
| Need | Consult |
|---|---|
| runtime / language base | `zerops_knowledge uri="zerops://decisions/choose-runtime-base"` |
| database / persistence / analytics DB | `zerops_knowledge uri="zerops://decisions/choose-database"` |
| cache / sessions / pub-sub | `zerops_knowledge uri="zerops://decisions/choose-cache"` |
| async / message queue | `zerops_knowledge uri="zerops://decisions/choose-queue"` |
| search / full-text / vector | `zerops_knowledge uri="zerops://decisions/choose-search"` |
| object-storage / shared-storage (no decision owner) | `zerops_knowledge uri="zerops://themes/services"` (the service card) |

**When the story doesn't pin a framework, prefer one with a curated recipe** (the catalog at the top of this step). A layperson cares about the product, not the framework — resolve toward a stack you can start from, so recipe-first has a substrate to anchor on.

## Step 4b — surface type (derived; the UI's primary job)

One more derived read, for the UI rather than the services. From D2 × the story verb:

- **product** — manage / track / review / configure / browse: dashboards, CRUD, internal tools. **The default when unclear.** Usability + consistency win; lean on the stack's idiomatic kit.
- **brand** — announce / showcase / convert / present: a public landing or marketing page. Distinctiveness is the point; go bespoke.

It is an **app-level default each slice inherits and may override per route** — a public landing slice is `brand` next to the `product` admin behind it. Aspirational words ("beautiful", "modern", "slick") describe the quality bar, **not** the surface type — a dashboard called beautiful is still `product`.

## Step 5 — the two paths (both end in the same concrete service set + the wedge)

Resolve CLASSIFY + D1/D2/D3/D5, then offer the user a choice. **Path A asks nothing** unless a gate blocks; **Path B asks one at a time, max 8, each pre-filled** — only the residue you couldn't infer.

The grill extends Path B from infra-only to **product**: beyond audience/tier, confirm the **core wedge** (the one feature that defines the product), **what slice 1 is** (the thinnest end-to-end thing), and **what's explicitly out of scope** — everything else you infer.

### Path A — "I'll describe how I'd build it" (default)
Infer everything, then narrate the plan **in the user's terms** — what it is, what they'll be able to do, what you're leaving for later. Not the stack, not a service count, not "slice": say only — in a clause, not a method lecture — that you'll build it in stages they can try one at a time, and name the first one plainly. For example:
> "Here's what I'll build: **a shared board for your team** — everyone logs in and moves task cards across To-Do / Doing / Done. I'll build it in stages you can try one at a time, starting with **logging in and one working board you can add and move cards on**, then the rest. Leaving comments, attachments and notifications for later. Want it like this, or change anything?"

Then **stop and wait** — this is the one direction check, before any code exists. Ask via `AskUserQuestion` if the harness exposes it ("Build it like this?" → "Yes, build it" / "I'd change something"; the free-text option lets them say what), else inline in plain words, and wait for their reply. On a go → proceed to the PRD (phase 2) and the slice DAG (phase 3). On a change → fold it in, re-narrate the adjusted plan, confirm again. After this one check they react to working software, not docs.

### Path B — "Want to walk through it? (max 8 questions)" (opt-in)
One question at a time, each pre-filled with your inferred answer so the user mostly confirms. The set (only the residue is actually asked):
1. Who will use this — just you / a team / the public? *(D2)*
2. A quick experiment, or will the business run on it? *(D5)*
3. Should it remember data between visits? *(D1)*
4. What's the one thing people open it for? *(the core wedge)*
5. If only that one thing worked first — would that be enough to try? *(slice 1)*
6. What are we deliberately NOT doing for now? *(out-of-scope)*
7. Upload files/photos, search across a lot, anything in the background? *(D3 bits)*
8. Anything sensitive (health data, payments, personal info)? *(tripwire / stakes)*

End the same way as Path A: present the assembled plan in plain words and confirm direction (go or change) before building.

## The tripwire — when to stop and ask

Break the infer-and-proceed default ONLY on a high-harm signal (signals: the `tripwire` CLASSIFY row + D2 stakes-modifiers). On a tripwire: (a) do **NOT** auto-enable the public subdomain or touch real sensitive data until the user reacts; (b) ask the honest scoped question ("I can do X; I cannot *guarantee* Y; confirm Z"); (c) **never narrate a guarantee** (compliance, residency, correctness) you didn't make — offer the smallest credible slice, not a fake-complete one.

## Worked examples (story → decisions → concrete set)

- **Law-firm cases/documents**: **tripwire** (client documents) → harm-gate first; then multi-writer · files · production → app dev/stage pair + managed DB `:ha` + object-storage; search only if stated.
- **"Uber for dog walking"**: **insufficient-signal** → ask the one booking-process question; emit NO services until the spine is answered.

When this phase is done you hold: the resolved decision set, the concrete service set, the core wedge, and what slice 1 is. Carry them into `phases/prd.md`.
