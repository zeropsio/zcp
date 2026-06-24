# Phase 1 — Align (evaluate + resolve + grill the residue)

Goal: turn the plain story into the resolved decision set + a concrete service plan, asking the user only what you genuinely cannot infer. Output of this phase is the input to the PRD (phase 2).

## Step 1 — evaluate what exists

Scan the repo first. **Greenfield** (empty / only ZCP scaffolding) → design fresh. **Brownfield** (real app code, a `zerops.yaml`, a deployed service) → the existing code/data is the truth; do not greenfield over it (this is the `adopt-path` of CLASSIFY). Read `zerops_workflow action="status"` to see what already exists on the platform.

## Step 2 — CLASSIFY (run first; non-buildable branches STOP normal inference)

| Route | Story signals | Action |
|---|---|---|
| `buildable` | concrete noun + verb: "track workouts", "manage tasks", "book appointments", "upload documents" | run the engine |
| `platform-reject` | GPU/CUDA training, hard-realtime control, large transcoding, live-streaming infra, mining — not in the catalog | STOP, say plainly why it can't run here; no masking fallback |
| `tripwire` | legal/medical/tax/financial advice, PHI/PCI, law-firm or client docs, payroll, minors, gov IDs, residency, charging cards / public payments, destructive public actions | harm-gate question FIRST; no auto public subdomain until cleared |
| `needs-scoping` | "all-in-one", "ERP", CRM + marketplace + HR + billing in one ask | ask the wedge (the one thing to build first) |
| `adopt-path` | "use my repo", "migrate my database", "deploy this GitHub project" | existing code/data is the truth — adopt, don't greenfield |
| `insufficient-signal` | "make me an app", "Uber for X", "AI-powered dashboard", one buzzword/noun, no process | ask ONE product-story question; emit no services yet |

## Step 3 — infer the decisions (the engine)

**Confidence:** **High** — words say it → infer silently (unless a tripwire fires). **Medium** — strong prior, not stated → Path A narrates the assumption; Path B may confirm if one-way. **Low** — analogy/buzzword/vague → ask only if it gates a one-way or harm-bearing decision; else default lean/off.

**D1 Statefulness:**
| Value | Signals |
|---|---|
| stateless | landing page, calculator, converter, read-only; "no login / no saving" |
| single-writer | "my workouts", notes, journal, admin-owned CRUD; "track / save / manage" |
| multi-writer | team tasks, assignments, bookings, orders, submissions, sign-ups; "team / customers / book / assign / upload" |

*Any non-stateless ⇒ a managed durable surface. Structured data → managed database. Files → object-storage.*

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

**Deceptive-signal guards:** strong analogies are LOW signal ("Uber for dog walking" does NOT imply maps/payments/dispatch/tracking → `insufficient-signal`, ask the process). Buzzwords are not capabilities ("AI-powered" ≠ vector/search unless the process needs generation/embeddings/retrieval). Named tech (Mongo/Firebase/GPU/Redis) is a *disposition item*, not an instruction — map it to a Zerops equivalent only when the capability is clear; if there's no equivalent and it's a hard requirement, ask or reject, never silently substitute.

## Step 4 — story → services (`kolik a jaké`)

You ALWAYS land on a concrete service set. This table resolves **D6 Decomposition** (DERIVED from D5 tier × D3 core-capability, clamped by D1): each capability lands *in-process* at the cheap tier and *promotes to a dedicated service* as tier rises or when it is the product's core. Floor overrides (D1 stateful, files bit) force a managed service regardless of tier.

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

**D7 Grade** (DERIVED from D5 tier, clamped by D1/D2 floor) — tier → knobs: runtime scale (experiment min=max=1 small → lean autoscale 1..2 → production minContainers ≥ 2 + headroom); managed mode `:single`→`:single`→`:ha` on stateful; env pipeline dev → dev (+stage if releases are user-visible) → dev+stage; backups off-unless-irreplaceable → on → on+retention.

**Floor clamps (independent of tier — budget sets the ceiling, never the floor):** durable data → managed database (+backups if irreplaceable); multi-user → auth + server-side authorization **in app code** (no Zerops knob guarantees this — the design-quality gap guided exists to close); never runtime-disk persistence; files → object-storage.

**Route every service choice to its owner — never hardcode a version:**
| Need | Consult |
|---|---|
| runtime / language base | `zerops_knowledge uri="zerops://decisions/choose-runtime-base"` |
| database / persistence / analytics DB | `zerops_knowledge uri="zerops://decisions/choose-database"` |
| cache / sessions / pub-sub | `zerops_knowledge uri="zerops://decisions/choose-cache"` |
| async / message queue | `zerops_knowledge uri="zerops://decisions/choose-queue"` |
| search / full-text / vector | `zerops_knowledge uri="zerops://decisions/choose-search"` |
| object-storage / shared-storage (no decision owner) | `zerops_knowledge uri="zerops://themes/services"` (the service card) |

## Step 5 — the two paths (both end in the same concrete service set + the wedge)

Resolve CLASSIFY + D1/D2/D3/D5, then offer the user a choice. **Path A asks nothing** unless a gate blocks; **Path B asks one at a time, max 8, each pre-filled** — only the residue you couldn't infer.

The grill extends Path B from infra-only to **product**: beyond audience/tier, confirm the **core wedge** (the one feature that defines the product), **what slice 1 is** (the thinnest end-to-end thing), and **what's explicitly out of scope**. These three are the load-bearing product decisions a layperson *can* answer; everything else you infer.

### Path A — "I'll describe how I'd build it" (default)
Infer everything, then narrate plainly:
> "I'm treating this as **[audience / tier]**. It remembers **[data]** (so a database), keeps **[photos → object-storage]**, built as **[N services]**. I'll build **[slice 1]** first; **[X / Y]** come after; I'm leaving out **[Z]** for now. If that's off, tell me."

Then proceed to the PRD (phase 2) and the slice DAG (phase 3). One question at most, unless a gate blocks.

### Path B — "Want to walk through it? (max 8 questions)" (opt-in)
One question at a time, each pre-filled with your inferred answer so the user mostly confirms. The set (only the residue is actually asked):
1. Kdo to bude používat — jen ty / tým / veřejnost? *(D2)*
2. Rychlovka, nebo na tom poběží firma? *(D5)*
3. Má si to pamatovat data mezi návštěvami? *(D1)*
4. Co je ta hlavní věc, kvůli které to lidi otevřou? *(the core wedge)*
5. Co kdyby fungovalo nejdřív jen tohle jedno — stačilo by to vyzkoušet? *(slice 1)*
6. Co teď schválně NEřešíme? *(out-of-scope)*
7. Nahrávat soubory/fotky, hledat ve větším množství, něco na pozadí? *(D3 bits)*
8. Něco citlivého (zdravotní data, platby, osobní údaje)? *(tripwire / stakes)*

## The tripwire — the one asymmetric brake

Guided's default reflex is infer-proceed-narrate-never-ask. Override it ONLY on a high-harm signal (regulated/sensitive data on a public URL, public payments, destructive public actions, professional/legal/tax/medical authority, someone else's data). On a tripwire: (a) do **NOT** auto-enable the public subdomain or touch real sensitive data until the user reacts; (b) ask the honest scoped question ("I can do X; I cannot *guarantee* Y; confirm Z"); (c) **never narrate a guarantee** (compliance, residency, correctness) you didn't make — offer the smallest credible slice, not a fake-complete one.

## Worked examples (story → decisions → concrete set)

- **Real-estate PM** ("our firm, enter tasks, daily overview"): buildable · multi-writer · multi-actor-private · no bits (daily overview is a page) · real-but-lean → **app runtime + 1 managed database (staging) + auth & roles in app code.** No storage/search/worker until photos / full-text / emailed reports are stated.
- **Workout tracker** ("track my workouts"): single-writer · single-actor · no bits · experiment → **app runtime + 1 managed database (hobby).** Nothing else. No question asked. Single slice (see `phases/slices.md`).
- **Stateless converter**: stateless → **1 runtime, zero data services.** A *complete* record — say so.
- **Law-firm cases/documents**: **tripwire** (client documents) → harm-gate first; then multi-writer · files · production → app (+stage) + managed DB `:ha` + object-storage; search only if stated.
- **"Uber for dog walking"**: **insufficient-signal** → ask the one booking-process question; emit NO services until the spine is answered.

When this phase is done you hold: the resolved decision set, the concrete service set, the core wedge, and what slice 1 is. Carry them into `phases/prd.md`.
