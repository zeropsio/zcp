# Per-tier authoring workflow

You author env-level surfaces (root + per-tier intros + import-comments)
across 6 tiers (0..5). The brief carries:

## Tier names are canonical, not marketing

Use exactly these labels in root README rows, env intros, and tier
README headings:

| Index | Label |
|-------|-------|
| 0 | AI Agent |
| 1 | Remote (CDE) |
| 2 | Local |
| 3 | Stage |
| 4 | Small Production |
| 5 | Highly-available Production |

Don't rewrite them — no marketing prefixes ("Include Coding Agents",
"Cloud IDE Workspace"), no synonyms ("Production-HA", "AI-agent dev
tier"), no abbreviations ("HA-Prod"). Porters scanning recipes expect
identical taxonomy across the family. Folder paths
(`0 — AI Agent`/`1 — Remote (CDE)`/...) are also canonical and engine-
emitted; you read them, you don't rewrite them.



- Per-tier capability matrix (already computed)
- Cross-tier deltas from `tiers.go::Diff`
- Engine-emitted `tier_decision` facts (one per cross-tier whole-tier
  delta + one per per-service variant change)
- The plan snapshot (codebases + services)
- Parent-recipe pointer (when present)

## Surface caps to self-check before record

The validator at `internal/authoring/recipe/slot_shape.go` enforces these
structural caps on every `record-fragment` call. Self-check your
draft body's length BEFORE invoking `record-fragment` — each
rejection costs a round-trip on information already provided.

| Surface | Cap | Spec anchor |
|---|---|---|
| `root/intro` | 1 sentence, 500 chars; no markdown headings | `docs/spec-content-surfaces.md` Surface 1 |
| `env/<N>/intro` | 1-2 sentences, 350 chars; no `## ` headings; no `<!-- #ZEROPS_EXTRACT_*` markers | `docs/spec-content-surfaces.md` Surface 2 |
| `env/<N>/import-comments/project` | ≤ 8 lines | `docs/spec-content-surfaces.md` Surface 3 |
| `env/<N>/import-comments/<host>` | ≤ 8 lines | `docs/spec-content-surfaces.md` Surface 3 |

When you draft a per-host import-comments block, count the lines
before `record-fragment`. When you draft an `env/<N>/intro`, count
the chars. Run-32's env-content agent burned three round-trips on
caps the brief teaches but didn't surface where the agent composes:
14-line broker block (cap 8), 353-char intro (cap 350).

## Forbidden tokens that need attesting facts

The validator at `internal/authoring/recipe/slot_shape_authoring.go` refuses
import-comment bodies that invoke certain framing tokens without an
attesting fact in the run's `facts.jsonl`. Pre-empt these tokens
when no scaffold/feature-time fact attests their use:

- **JetStream framing** — tokens `JetStream`, `quorum-replicated
  stream(s)`, `durable consumer(s)`. Attestation: a fact with topic
  prefix `nats-jetstream` (e.g. `nats-jetstream-enabled`,
  `nats-jetstream-streams`). When the recipe uses ONLY core NATS
  pub/sub (no `jetstream(nc)` / `jsm.streams.add` calls in code),
  import-comments and KB MUST NOT invoke JetStream framing — there
  is no stream to discuss. If your draft body contains any of these
  tokens, check the FactsLog: if no `nats-jetstream-*` topic exists,
  rewrite without the token rather than learn this from a refusal.

The validator-regex source (`jetStreamTokenRE` and peers) is the
authority; this list reflects the tokens currently in scope.

## Workflow

1. **Author root/intro** — one sentence that frames what the recipe IS
   (stack + scenario), not what Zerops does with it. ≤ 500 chars, no
   markdown headings.
2. **For each tier 0..5**:
   - Author `env/<N>/intro` (1-2 sentences naming who this tier is for
     + the cost/availability tradeoff). ≤ 350 chars, no `## ` headings,
     no `<!-- #ZEROPS_EXTRACT_*` tokens (engine stamps those at stitch).
   - For each service block in the tier import.yaml that has a causal
     story, author `env/<N>/import-comments/<host>` (≤ 8 lines per block).
   - Author `env/<N>/import-comments/project` for the project-level
     block (secrets, corePackage).
3. **Cross-reference parent** when present — read parent's per-tier
   intros and dedup before authoring.

## Lead pattern — `**TierName**` + verb + audience purpose (intros) / `Deploy/Spin up X, used by Y` (per-service)

This section sets the **lead voice** for every env-content surface.
The voice rules below (friendly-authority phrasings, within-tier
adapt knobs, closure-of-expectation guards) describe SECONDARY
shape — what comes after the lead. **Get the lead right first; the
rest is decoration.**

`derived_rules.md` V1 + T2 + TY1 + TY2 score the assembled output
against the jetstream-golden voice — names what the recipe service
IS in plain English first, reaches for mechanism precision (yaml
field names, scaling math) only when porter-actionable. Run-34 +
run-35 evidence: tier intros + per-service yaml comments shipped
with implementation-shape lead ("Single-node PostgreSQL — restoring
from snapshot means downtime…", "Production setup on shared CPU
with single-instance managed services and 0.25 GB free-RAM
headroom…") instead of the jetstream lead pattern. Refinement
caught some; many shipped because the authoring brief's PASS
examples were the same engineering-spec voice. This section
realigns authoring with the scoring substrate.

### Tier README intro lead pattern (Surface 2 — `env/<N>/intro`)

Lead with `**Tier name** environment <verb> <audience purpose>`.
Verb is one of `provides` / `allows` / `supports` / `uses` /
`offers` (porter-card framing). The **audience purpose** comes
first; implementation specs (single-instance, dedicated CPU,
managed-RAM floor) come second only when porter-actionable at this
surface.

Worked GOOD (jetstream tier 3):

> ***Stage** environment uses the same configuration as production,
> but runs on the lowest scaling settings.*

Worked GOOD (jetstream tier 0):

> ***AI Agent** environment provides a development space for AI
> agents to build and version the app. Comes with a dev service
> with the source code and necessary development tools, a staging
> service, email & SMTP testing tool, and a low-resource databases
> and storage.*

Worked GOOD (jetstream tier 4):

> ***Small production** environment offers a production-ready
> setup optimized for moderate throughput.*

Worked BAD (run-35 tier 3):

> *Production setup on shared CPU with single-instance managed
> services and a 0.25 GB free-RAM headroom buffer on every
> container, sized for rehearsal traffic and QA runs against
> snapshots-only durability.*

Worked BAD (run-35 tier 0):

> *Agent workspace shape — single-instance managed services on
> shared CPU with the smallest managed RAM, dev-runtime containers
> for every codebase, and project-level secrets generated once at
> import. Sized for end-to-end iteration without juggling
> production-shaped scale.*

The BAD intros lead with implementation-shape descriptors and
state specs as facts. The GOOD intros lead with the tier name in
boldface + a verb that says what the tier DOES + the audience the
tier is FOR. Specs come second, briefly, when porter-actionable.

### Tier yaml head comment lead pattern (Surface 3 — `env/<N>/import-comments/project`)

The top-of-file yaml comment **mirrors the tier README intro**.
Same body, same voice — the README has markdown bold (`**Stage**`),
the yaml comment elides the bold but keeps the same prose.

Worked GOOD (jetstream tier 3 yaml head):

```yaml
# Stage environment uses the same configuration as production,
# but runs on the lowest scaling settings.
```

Worked BAD (run-35 tier 3 yaml head — dives into project-envVars):

```yaml
# Same single-slot project envs as tier 2 — APP_SECRET regenerated
# at import; API_URL / FRONTEND_URL resolve to the bare api/app
# subdomains. Replace these with your own hostnames once you swap
# subdomain access for a custom stage domain.
```

The BAD shape skips the tier mission and dives into a project-envs
block explanation. That belongs in the project-envVars body, NOT
the file head. The head is the tier's identity card. **Mirror the
README intro.**

### Per-service yaml comment lead pattern (Surface 3 — `env/<N>/import-comments/<host>`)

Lead with imperative + service identity + role/relationship.
Verb is `Deploy` / `Spin up` / (or the service-name shape `Single
node X`/`Single-instance X` ONLY when followed immediately by
`used by Y for Z`). The **what-it's-for-the-porter** clause comes
first; mechanism precision (priority, vertical autoscaling
fields) comes second only when porter-actionable.

Worked GOOD (jetstream tier 3 db):

```yaml
# Deploy single node PostgreSQL database,
# used by the Laravel app to store data. Automatic,
# encrypted backups are enabled by default.
```

Worked GOOD (jetstream tier 3 cache):

```yaml
# Spin up Valkey in single-node mode.
# Valkey is an open-source Redis-compatible
# key/value storage for caching, sessions, queues, etc.
```

Worked GOOD (jetstream tier 0 db, with relationship):

```yaml
# Deploy low-resource PostgreSQL database,
# used by the app to store data. Both the staged
# 'appstage' service and AI agents from 'appdev'
# typically connect to this database.
```

Worked BAD (run-35 tier 3 db — descriptor + tradeoff lead):

```yaml
# Single-instance Postgres — restoring from snapshot still means
# downtime, the rehearsal-grade tradeoff at stage. The 0.25 GB
# minFreeRamGB headroom keeps query bursts off the swap path;
# bump verticalAutoscaling.minRam when stage workload pushes
# query latency past your SLO.
```

The BAD lead opens with the tradeoff (`restoring from snapshot
still means downtime`) and the scaling math (`minFreeRamGB
headroom keeps query bursts off the swap path`). The porter
hasn't been told what this service is FOR before the comment
dives into the operational tradeoff. The GOOD lead names the
imperative (`Deploy`), the service identity (`single node
PostgreSQL database`), the role (`used by the Laravel app to
store data`), and ONE porter-relevant fact (`encrypted backups
are enabled by default`). Tradeoffs and bump-knob tails come
SECOND when they apply — never first.

### Acceptable MIX shape (lead with descriptor + immediate role)

When the imperative `Deploy/Spin up X` shape would feel awkward
(e.g. tier 0/1 dev-pair runtime where the operational concern
genuinely is the workspace shape), the acceptable MIX shape leads
with the service descriptor + an em-dash + an immediate role
clause:

```yaml
# Single-node PostgreSQL — used by the api codebase to store
# items + job logs. Snapshots-only durability at this rehearsal
# tier; bump `verticalAutoscaling.minRam` if your stage workload
# pushes query latency.
```

The descriptor leads, but the very next clause names what the
service is FOR. Adapt-knob tail (`bump verticalAutoscaling.minRam
if…`) comes last. Run-32 sim-3 used this MIX shape on most
managed-service blocks. Acceptable; not as crisp as jetstream
GOOD but porter-comprehensible. The BAD shape — descriptor +
em-dash + tradeoff/scaling-math lead with no role clause — is
forbidden.

## Voice

Friendly authority, declarative, present-tense, porter-facing.
**Lead with what the service IS in the porter's vocabulary** (see
Lead pattern section above); state the platform mechanism, name
the tradeoff, end on the porter signal that triggers a change.
Never "we chose X", never "during scaffold".

PASS — production-tier APP_KEY block (project-envs body, not file head):

> *APP_KEY — production encryption key shared across all app and
> worker containers. Critical for session validity when the L7
> balancer distributes requests across multiple containers.*

Mechanism (encryption key shared) → consequence (session validity
under L7 balancing). Two sentences, no field narration. The file
head still mirrors the README intro per the Lead pattern section
above; this PASS shape applies inside the project-envs body where
each project-level env carries its own causal block.

PASS — tier-0 PostgreSQL block (jetstream-GOOD lead):

> *Deploy low-resource PostgreSQL database, used by the api
> codebase to store the items table and per-deploy migration
> markers. Both the agent workstation (`apidev`) and the staged
> service (`apistage`) connect to this database; priority 10
> guarantees Postgres accepts connections before any
> initCommand-driven migration runs.*

Imperative (`Deploy`) → service identity (`low-resource
PostgreSQL database`) → role (`used by the api codebase to store
…`) → relationship (`Both A and B connect`) → priority's effect.
The within-tier-knob (`bump verticalAutoscaling.minRam if…`) is
omitted here because tier 0 is the workspace tier where porter
adapt paths don't apply (see "Tier-specific carve-outs" below).

PASS — tier-4 small-prod app block (jetstream-GOOD lead):

> *Run two app replicas on shared CPU. minContainers: 2 gives
> the service capacity for concurrent traffic and absorbs a single
> container crash without dropping requests. Bump
> verticalAutoscaling.minRam when monitoring shows steady-state
> RAM usage saturating the current floor.*

Imperative (`Run two app replicas`) → mechanism (`minContainers: 2
gives the service capacity… and absorbs a single container crash`)
→ adapt-knob tail (`bump verticalAutoscaling.minRam when…`) tied to
a within-tier porter signal. The mechanism statement and the
adapt-knob are separate sentences — em-dashes welding two distinct
thoughts read as one fused thought; two sentences signal that
mechanism and adapt path are independent.

**Rolling-deploy mechanism is separate from `minContainers`.** Zero-
downtime cutover is platform default (`temporaryShutdown: false`):
new container spins up, readiness probe passes, then old container
is removed. This works the same at `minContainers: 1` and
`minContainers: 2`. `minContainers≥2` is the capacity-and-crash-
tolerance axis, NOT the deploy-cutover axis. Don't write
*"minContainers: 2 enables rolling deploys"* / *"one serves traffic
while the other rebuilds"* — both describe the wrong mechanism.

**Note**: `minContainers` is NOT a within-tier adapt knob — the
within-tier-scope rules below (line ~370) reserve `minContainers`
for cross-tier ladder design points. The PASS lead names tier-4's
`minContainers: 2` mechanism but does NOT invite the porter to bump
it; bumping past the ladder design point graduates the porter to
tier 5 by re-importing, not by editing this yaml. The adapt knob in
the PASS lead is `verticalAutoscaling.minRam`, a field the yaml
actually sets (so the porter can locate and tune it). Don't gesture
at fields the yaml doesn't set (e.g. `verticalAutoscaling.maxRam`
"ceiling bumping" when no ceiling is declared) — the porter looks
for the field, doesn't find it, and the adapt-path collapses.

## Friendly-authority phrasing — the adapt-path contract

Refinement walks these import-comments against the V1-V4 voice rules in
`derived_rules.md` (porter-clones-and-runs framing, porter-actionable
phrasing). Engineering-spec prose ("Single instance with the smallest
managed RAM. Snapshots run, but there is no replica…") describes the
shape correctly but doesn't tell the porter what to change. **Land
friendly-authority phrasing at first write** — at least one per service
block, each tied to a concrete porter signal — not engineering-spec
prose that refinement then has to lift. Voice rewrites across 50+
blocks at refinement time exceed the F-27 threshold; voice belongs in
env-content authoring.

### Canonical phrasing patterns

- *"Feel free to ..."*
- *"Configure this to ..."*
- *"Replace ... with ..."*
- *"Disabling ... is recommended ..."* / *"Enabling ... is recommended ..."*
- *"Adapt this ..."* / *"Adjust this ..."*
- *"Bump ... if ..."* / *"Switch ... when ..."*
- *"... once you ..."* (conditional adapt)

Each phrasing MUST name a concrete porter signal: a numeric threshold,
a configuration state, or a named external condition (custom domain
configured, monitoring shows X approaching ceiling, steady-state
traffic spikes saturate fan-out, dataset size crosses N GB). Without a
signal, it's a hedge — not friendly authority — and doesn't count.

**Forbidden hedge phrasings** — weasel words that name no signal:

- *"you might want to consider ..."*
- *"perhaps this could ..."*
- *"in some cases ..."*
- *"depending on your needs ..."* (without a named need)

The voice is *"this is the choice; here's the mechanism; you can
change it for your needs IF X"* — declarative + invitation, never
hedge.

## Porter signals MUST be reachable within THIS tier's deployment shape

A friendly-authority phrasing names an adapt path — a knob the porter
turns to handle a different deployment context. The knob must be
something the porter changes WITHIN this tier's yaml, not a path that
crosses the tier ladder.

**Within-tier knobs (adapt paths — porter signals OK):**

- `verticalAutoscaling.minRam` / `maxRam` — RAM headroom adjustments
- `minFreeRamGB` — spike-buffer tuning
- `objectStorageSize` — storage quota
- `backup` retention windows
- `enableSubdomainAccess` toggle
- `initCommands` script keys (`.v1` → `.v2` re-run lever)
- Per-service environment-variable values

**Cross-tier moves (NOT adapt paths — DO NOT NAME):**

- HA-ness change (`:single` ↔ `:ha` type variant)
- `minContainers` jumps that cross the tier ladder's design points
- `corePackage` tier changes
- Runtime base swaps (`@22` → newer LTS at a tier rollover)
- Adding/removing a service from the canonical set

The recipe's tier ladder ALREADY encodes the cross-tier moves — the
porter graduates by re-importing a higher tier yaml, not by editing
this one. A comment that says *"Switch to the `:ha` variant at tier 5 if X"*
names a path the porter takes by changing tiers, not by editing this
yaml; that's tier-promotion narrative, not friendly-authority.

### Worked example — db block at tier 3

**BAD** — names a cross-tier knob as an adapt path:

```yaml
- hostname: db
  type: postgresql:single@18
  # Single instance — restoring means downtime; acceptable at stage.
  # Switch to the `:ha` variant once you need failover without a manual-restore
  # window.
  verticalAutoscaling:
    minRam: 0.25
    minFreeRamGB: 0.25
```

The "Switch to the `:ha` variant" sentence reaches across the tier ladder — porter
graduates to tier 5 to get HA, doesn't edit this yaml. Forbidden.

**GOOD** — porter signal stays within-tier:

```yaml
- hostname: db
  type: postgresql:single@18
  # Single instance — restoring means downtime; acceptable at stage.
  # `minFreeRamGB` headroom prevents swap thrash during rehearsal load
  # spikes; bump verticalAutoscaling.minRam if QA load runs push the
  # working set above 0.25 GB.
  verticalAutoscaling:
    minRam: 0.25
    minFreeRamGB: 0.25
```

The adapt path is `minRam` — a knob the porter changes in THIS yaml.
Cross-tier variant flips have no porter-signal home in service blocks.

### Worked example — db block at tier 4 (parenthetical cross-tier slip)

The forbidden phrasing isn't only the imperative verb shape ("Switch ...
to HA"). Cross-tier references slip in as parenthetical narrative too —
"arrives at tier N", "available at tier N", "comes online at tier N",
"unlocks at tier N". The verb is calmer, the slot is parenthetical, the
porter signal in the same comment looks within-tier; the cross-tier
reference still smuggles the tier-ladder narrative into the service
block.

**BAD** — parenthetical "arrives at tier N" cross-tier reference:

```yaml
- hostname: db
  type: postgresql:single@18
  # Still single-instance (`:single` variant) — restoring from snapshot means
  # downtime, the small-prod tradeoff (true failover arrives at
  # tier 5 with the `:ha` variant). The `minFreeRamGB: 0.25` headroom
  # absorbs production load spikes; bump verticalAutoscaling.minRam
  # when monitoring shows query latency creeping up.
  verticalAutoscaling:
    minRam: 0.5
    minFreeRamGB: 0.25
```

The within-tier `minRam` adapt path is correct; the parenthetical "true
failover arrives at tier 5" reaches across the tier ladder. Forbidden.

**GOOD** — elide the cross-tier reference entirely; the tier table in
the root README is where the porter learns tier 5's shape:

```yaml
- hostname: db
  type: postgresql:single@18
  # Single-instance (`:single` variant) — restoring from snapshot means downtime,
  # the small-prod tradeoff. The `minFreeRamGB: 0.25` headroom absorbs
  # production load spikes; bump verticalAutoscaling.minRam when
  # monitoring shows query latency creeping up under steady-state
  # working set.
  verticalAutoscaling:
    minRam: 0.5
    minFreeRamGB: 0.25
```

The porter doesn't need the service-block comment to tell them tier 5
has HA — they read the tier table in the root README and re-import the
higher tier yaml when they want failover. The yaml comment owns
within-tier knobs only.

### Worked example — closure-of-expectation phrasings (no-move framing)

The forbidden shape isn't only the imperative ("Switch ...") or the
parenthetical-narrative ("arrives at tier N"). Closure-of-expectation
phrasings — "no `:ha` variant at any tier", "stays single-shape across every
tier", "holds at `:single` even at this tier", "no replica option exists
in the family" — assert that NO move is available. They don't name a
move the porter could take. They still pull the porter's attention to
the cross-tier scope ladder; the porter reads "every tier" and does
the cross-tier comparison the brief tells them not to do. The
service-family invariant (no `:ha` variant for this family) is a research /
IG-level concern; the service-block comment owns within-tier knobs
only.

**BAD** — closure-of-expectation tail at tier 4 search block:

```yaml
- hostname: search
  type: meilisearch:single@1.20
  # Single-node Meilisearch — meilisearch on Zerops has no `:ha` variant.
  # Bump verticalAutoscaling.minRam if production search
  # latency correlates with index growth.
  verticalAutoscaling:
    minRam: 0.5
```

The within-tier `minRam` adapt path is correct; the "no `:ha` variant"
tail asserts a service-family invariant that pulls the porter
across the tier ladder. Forbidden.

**BAD** — closure-of-expectation tail at tier 5 storage block:

```yaml
- hostname: storage
  type: object-storage
  # Object storage stays single-shape across every tier — there is no
  # `:ha` variant for this service type (single-shape only). Bump
  # objectStorageSize when uploads outgrow the current quota.
  objectStorageSize: 5
```

"Stays single-shape across every tier" is the same anti-shape with
calmer vocabulary. Forbidden.

**BAD** — closure-of-expectation tail at tier 5 search block:

```yaml
- hostname: search
  type: meilisearch:single@1.20
  # Meilisearch holds at `:single` even at this tier — the platform has
  # no `:ha` variant for this service family. Bump verticalAutoscaling.minRam
  # if working-set growth pushes latency past your SLO.
  verticalAutoscaling:
    minRam: 1
```

"Holds at `:single` even at this tier" smuggles the cross-tier scope into
the comment. Forbidden.

**GOOD** — within-tier knob only:

```yaml
- hostname: search
  type: meilisearch:single@1.20
  # Single-node Meilisearch — bump verticalAutoscaling.minRam if production
  # search latency correlates with index growth past the 0.25 GB ceiling.
  verticalAutoscaling:
    minRam: 0.25
```

The comment names ONLY a within-tier knob (`verticalAutoscaling.minRam`)
tied to a porter signal (search latency vs. index growth). No
service-family assertion, no cross-tier comparative tail. The
service-family invariant ("no `:ha` variant for meilisearch") lives in
research / IG and in the service-family HOLD list further down this
brief — it is not the service-block comment's job to restate it.

### Tier README intros — closure-of-expectation guard

The same closure-of-expectation rule applies to the tier README intros
you author at workflow step 2 first bullet (`env/<N>/intro`). The
intro is a tier-summary surface — it names who the tier is for and the
within-tier deployment shape. Cross-scope tails — even parenthetical,
even framed as "no move available" / "no option in those families" —
must not appear. They pull the porter's attention to the tier ladder
the intro is supposed to summarize, not contrast.

The vocabulary of the rule for this surface: porter signals describe
the within-tier shape (single-instance, dedicated CPU, 1 GB managed-
RAM floor, doubled spike headroom). Service-family invariants ("no `:ha`
variant for meilisearch", "no `:ha` variant for object-storage") belong in
research / IG, not the tier-summary intro.

**BAD** — tier 5 intro with closure-of-expectation parenthetical:

> *Tier 5 — highly-available production. Dedicated CPU, SERIOUS
> corePackage on the project, the `:ha` variant on db/cache/broker (Meilisearch
> and object-storage stay single-shape — no `:ha` variant in those
> families), 1 GB managed-RAM floor, doubled spike headroom.*

The parenthetical "no `:ha` variant in those families" is the same closure-
of-expectation shape forbidden in service-block comments. It pulls the
porter into a cross-family comparison — the porter reads "no `:ha` variant"
and asks "what about other families? what about other tiers?" — when
the intro's job is to summarize THIS tier's shape.

**GOOD** — within-tier shape only:

> *Tier 5 — highly-available production. Dedicated CPU, SERIOUS
> corePackage on the project, the `:ha` variant on db/cache/broker, single-
> instance Meilisearch and object-storage with the 1 GB managed-RAM
> floor, doubled spike headroom.*

The same factual content reaches the porter — Meilisearch and object-
storage at tier 5 are single-instance — without the cross-family
"no `:ha` variant" assertion. The porter learns the shape; the family
invariant stays in research / IG where it belongs.

### When the only friendly-authority candidate is cross-tier

Some service blocks at lower tiers genuinely have no within-tier adapt
path — single-instance redis at tier 2/3 with a fixed RAM target, no
backup config, no auto-scaling. **One adapt phrasing tied to a real
within-tier knob is fine** even if it's the only one; if NO within-tier
knob exists, the block scores 7.0 (mechanism-only) and that's the
honest outcome. Better to ship 7.0 than to invent a cross-tier
graduation as fake friendly-authority.

### Worked example — runtime block (api/worker/app)

**Body cap reminder**: ≤ 8 lines per `env/<N>/import-comments/<host>` block (see Surface caps at top). Run-32 cc-env tripped a 14-line block at first record — count lines before recording.

**BEFORE** — engineering-spec, 7.0 floor:

```yaml
# Two containers minimum on dedicated CPU — predictable latency
# under load, no shared-CPU jitter from neighbour services. The
# L7 balancer distributes requests across containers.
- hostname: api
  type: nodejs@22
  zeropsSetup: prod
  enableSubdomainAccess: true
  minContainers: 2
  verticalAutoscaling:
    minRam: 0.5
    minFreeRamGB: 0.25
```

The mechanism is named, but the porter doesn't know which knob to
turn when their own traffic shape diverges.

**AFTER** — friendly authority, 9.0 anchor:

```yaml
# Two containers minimum on dedicated CPU — predictable latency
# under load, no shared-CPU jitter from neighbour services. Bump
# minContainers to 3 if your steady-state traffic spikes saturate
# the two-replica fan-out; switch verticalAutoscaling.maxRam upward
# when monitoring shows containers approaching the current ceiling.
# Subdomain access is on by default — disable it once you have a
# custom domain configured.
- hostname: api
  type: nodejs@22
  zeropsSetup: prod
  enableSubdomainAccess: true
  minContainers: 2
  verticalAutoscaling:
    minRam: 0.5
    minFreeRamGB: 0.25
```

Three friendly-authority phrasings (*"Bump … if"*, *"switch … when"*,
*"… once you"*); each tied to a concrete porter signal (steady-state
saturation; monitoring shows ceiling approach; custom domain
configured). The mechanism still leads.

### Worked example — managed-service block (db/cache/broker/search)

**Body cap reminder**: ≤ 8 lines per block (Surface caps at top). For NATS-shaped brokers, do NOT introduce `JetStream` framing tokens unless an attesting fact lives in `facts.jsonl` — the slot-shape validator refuses the record otherwise (Forbidden tokens at top).

**BEFORE** — engineering-spec, 7.0 floor:

```yaml
# Single instance with the smallest managed RAM. Snapshots run,
# but there is no replica — restoring means downtime, which is
# acceptable because tier-3 stage data is rehearsal-grade.
- hostname: db
  type: postgresql:single@18
  priority: 10
  verticalAutoscaling:
    minRam: 0.25
```

**AFTER** — friendly authority, 8.5 anchor:

```yaml
# Single instance with the smallest managed RAM. Snapshots run,
# but there is no replica — restoring means downtime, which is
# acceptable because tier-3 stage data is rehearsal-grade. Switch
# to the `:ha` variant once you need failover without a manual-restore
# window; bump verticalAutoscaling.minRam if working-set growth
# pushes query latency past your stage SLO.
- hostname: db
  type: postgresql:single@18
  priority: 10
  verticalAutoscaling:
    minRam: 0.25
```

Two friendly-authority phrasings (*"Switch … once you"*, *"bump …
if"*); each names the porter signal that triggers the adapt
(failover-without-downtime requirement; working-set-vs-SLO).

### Worked example — project-level block (secrets, URL constants)

**BEFORE** — engineering-spec, 7.0 floor:

```yaml
# APP_SECRET is generated once at import and shared across api +
# worker so JWT verification holds across the L7 balancer.
# API_URL and FRONTEND_URL are the single-slot stage
# URLs that frontends and CORS allow-lists consume.
project:
  name: <recipe-slug>-stage
  envVariables:
    APP_SECRET: <@generateRandomString(<32>)>
    API_URL: https://api-stage.example.com
    FRONTEND_URL: https://app-stage.example.com
```

**AFTER** — friendly authority, 8.5 anchor:

```yaml
# APP_SECRET is generated once at import and shared across api +
# worker so JWT verification holds across the L7 balancer. Rotate
# this via the Zerops UI's project envs once you suspect leakage
# — every container picks up the new value on next restart.
# Replace API_URL and FRONTEND_URL with your own
# stage hostnames once subdomain access is swapped for a custom
# domain.
project:
  name: <recipe-slug>-stage
  envVariables:
    APP_SECRET: <@generateRandomString(<32>)>
    API_URL: https://api-stage.example.com
    FRONTEND_URL: https://app-stage.example.com
```

Two friendly-authority phrasings (*"Rotate … once you"*, *"Replace
… once"*); each tied to a porter signal (suspected leakage;
custom-domain swap).

### Tier-specific carve-outs (HOLDs)

Some tier × service combinations don't carry a clean adapt path.
Be explicit rather than forcing a phrasing that strains:

- **Tier 0 / tier 1 (workspace tiers)** — the porter is iterating,
  not adapting. Friendly-authority phrasings strain because there's
  no production-shaped knob to turn yet. Use **orientation framing**
  ("This single instance with smallest managed RAM is enough for
  agent / human-porter iteration; production scale arrives at
  tier 4") rather than adapt-path framing. The voice score softens
  here by design.
- **`object-storage`** — no `:ha` variant (single-shape only), no replica option, no
  `verticalAutoscaling`. The only adapt path is `objectStorageSize`.
  **One adapt phrasing is fine** ("Bump objectStorageSize when
  uploads outgrow the current quota").
- **`meilisearch`** — no `:ha` variant at any tier (platform contract).
  Adapt path is `verticalAutoscaling.minRam` if index size grows.
  Don't force a `:single`→`:ha` variant-flip phrasing; it doesn't exist.
- **Single-valued variants** — at tier 5 db, the type is the `:ha`
  variant and there is no further variant upgrade. **HOLD on the
  variant-flip phrasing** for that service; pivot to `verticalAutoscaling`
  adapt paths or backup retention adapt paths in the same block.

When all adapt paths in a block fall under a HOLD, the block can
land at the 7.0 voice floor without penalty — orientation framing
is the correct shape.

## One causal teaching per block, deduped across services + tiers

Each block teaches a non-obvious choice. Two different dedup rules
operate at two different scopes — confusing them strips per-tier
flavor and leaves the porter without operational framing.

### Cross-tier dedup is for the canonical-set teaching, NOT for per-tier flavor

The **canonical service set** (which services exist, what each one
is for, the `${db_*}`/`${queue_*}` env-injection contract) is
authored once at tier 0 and not re-explained at every subsequent
tier. That's the canonical-set dedup — it strips repetition.

The **per-tier flavor** (what the runtime / variant / minContainers
shape MEANS for THIS service at THIS tier — single instance vs
`:single`-with-snapshots vs `:ha`-with-failover; what the operational
reality is for the porter at this scale) is per-tier teaching.
**Keep at least 1-2 lines of flavor framing per service block at
every tier**, even when no field changes from the previous tier.

Concretely: at tier 1 (Remote CDE), the postgres block looks
field-identical to tier 0 (same `:single` variant, same priority). But
the FLAVOR is different — tier 0 is the agent's workspace where
data is replaceable; tier 1 is the human-porter dev workspace
where data is also replaceable but the porter expectation (single
instance for CDE iteration, snapshot for safety net) is worth
naming. Keep that 1-2 line framing on the postgres block at tier 1.
Drop the canonical-set "Postgres stores the items table" prose
(already in tier 0).

Example tier-1 postgres block flavor (1-2 lines, kept):

```yaml
# Same `:single` shape as tier 0 — single instance for the human-
# porter dev workspace; snapshots cover restoration if needed.
- hostname: db
  ...
```

Tier 0 ships full per-block teaching (canonical-set + flavor)
because there's no prior tier to inherit from. Tiers 1-5 inherit
the canonical-set silently AND ship per-tier flavor comments on
every block. A bump (`:single`→`:ha` variant flip, corePackage upgrade, minContainers
bump) gets fuller teaching (3-5 lines naming the change + its
operational consequence); identical-shape blocks still get the
1-2 line flavor framing.

The run-17 baseline (100-135 indented `#` lines per tier) was
canonical-set repetition — the same `:single`-with-snapshots paragraph
on db, cache, broker, search at every tier. The run-22 baseline
(6 lines per tier at tiers 1-3) over-corrected — every service
lost its per-tier flavor. The target is ~15-25 lines per tier:
canonical-set stripped, per-tier flavor preserved.

### Cross-service dedup within a tier

If all four managed services share the same `:single`-with-snapshots-
and-no-replica story at tier 0, the project-level and per-service
blocks each carry the part the porter needs at THAT scope — the
project block names project-wide invariants (secret scope,
corePackage), and each service block names ONLY what's specific
to that service (RAM target, priority, peer dependency).

## Anti-patterns

The run-17 baseline shipped 100-135 indented `#` lines per tier. Every
class of waste shows up in the diff against the reference (~22 lines
per tier). Avoid:

**Field narration** — restating the directive name in prose:

```
# minContainers: 2
# This sets the minimum number of containers to 2.
```

The directive value already says "2"; the comment says nothing the
yaml doesn't. Replace with mechanism + reason:

```
# Two containers always running gives capacity for concurrent
# traffic and absorbs a single-container crash without dropping
# requests. Rolling deploys are zero-downtime at any
# minContainers value via the platform default
# (temporaryShutdown: false).
```

**Repetition across services** — copy-pasting the same `:single`-with-
snapshots paragraph onto db, cache, broker, and search blocks at tier
0. Write it once at the project level (or on the first service block),
and the per-service blocks carry only what's specific to that service.

**Repetition across tiers** — explaining `verticalAutoscaling` on every
single tier. Explain it once, where it first appears or where it
changes. Subsequent tiers' service blocks carry no comment when the
configuration is identical to the prior tier's.

**Tier-promotion narratives** — *"Promote to tier 1 once a human porter
takes over"*, *"Outgrow this when..."*, *"Upgrade from tier N to N+1
when..."*. The contrast between tier yamls is the promotion signal —
the porter scrolling through tiers sees what changes. Don't narrate
the promotion path in prose.

**Authoring voice** — *"we chose X over Y"*, *"during scaffold"*,
*"recipe author decision"*. These are sub-agent process language; the
porter doesn't operate as part of the authoring run.

**"See the X guide" trailing slugs** — *"See: env-var-model guide."*,
*"see `init-commands`"*. The agent's `zerops_knowledge` tool slugs
don't resolve as docs URLs for porters. Cite by inline prose mention
or omit the citation.

**Platform-internal claims that exceed the corpus** — *"already
redundant at the platform layer"*, *"the bucket lives on a replicated
backend"*, *"Zerops automatically synchronizes…"*. The corpus
(`themes/services.md` + `themes/core.md`) is the authoritative source
for what the platform does and does not promise. If a service has no
`:ha` variant, name that fact as the porter sees it (single instance, no
replica option, recovery via X) — do NOT extrapolate into hidden
platform behaviour the corpus does not document.

**FAIL** (run-21 tier-5 storage block):

```yaml
# Object storage is already redundant at the platform layer — the
# bucket lives on a replicated backend regardless of variant, so no
# service-variant upgrade exists to flip.
```

The "already redundant at the platform layer" + "replicated backend"
claims are stronger than `services.md` supports — the corpus only
states "no `:ha` variant" and "S3-compatible MinIO backend".

**PASS** (corpus-grounded shape):

```yaml
# Object storage stays on the same shape every tier — there is no
# `:ha` variant for this service type (single-shape only) and no replica
# option to flip. Production scaling here means raising
# objectStorageSize; durability and availability are whatever the
# managed S3-compatible backend provides, not a recipe knob.
```

The mechanism (no `:ha` variant, no replica option) is named without
inventing platform-internal redundancy; the porter knob (objectStorageSize)
is the correct adapt path.

**Authoring-tool names in published comments** — `zerops_*`,
`zsc <subcommand>`, `zcli`, `zcp` — the porter operates with framework-
canonical commands, not the agent's tool inventory.

## Per-block density target

3-5 lines per block, every line carries mechanism or reason. Skip
"# Base image" / "# Run command" labels — those say nothing the yaml
doesn't. The 8-line per-block cap is the validator's hard limit;
3-5 is the quality target.

A block under the cap is not the goal. The goal is: every line teaches
a non-obvious choice. If you cannot say something non-obvious about a
block, skip the comment.

## Self-validate

`zerops_recipe` is an **MCP tool** — invoke it as a JSON tool call,
not a shell command. Invoke `zerops_recipe` with
`action: complete-phase` and `phase: env-content` to run EnvGates()
validators. Fix violations by re-invoking `zerops_recipe` with
`action: record-fragment` and `mode: replace` until the gate passes.
